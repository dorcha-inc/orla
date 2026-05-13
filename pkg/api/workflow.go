package orla

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/docker/docker/pkg/namesgenerator"
	"log/slog"
)

// Workflow is a DAG of Stages with dependency-aware scheduling.
// Independent stages execute concurrently; dependent stages wait for
// their upstream stages to complete. Use AddStage and AddDependency
// to build the DAG, then Execute to run it.
type Workflow struct {
	client       *OrlaClient
	stages       map[string]*Stage
	dependencies map[string][]string // stageID -> depends on []stageID
	memoryPolicy MemoryPolicy
}

// NewWorkflow creates an empty workflow bound to the given client.
func NewWorkflow(client *OrlaClient) *Workflow {
	return &Workflow{
		client:       client,
		stages:       make(map[string]*Stage),
		dependencies: make(map[string][]string),
	}
}

// SetMemoryPolicy sets the workflow-level MemoryPolicy used by the Memory Manager
// to decide cache actions at stage transitions. If not set, the default policy
// (preserve on small increment + flush at boundary) is used.
func (w *Workflow) SetMemoryPolicy(policy MemoryPolicy) {
	w.memoryPolicy = policy
}

// MemoryPolicyOrDefault returns the configured MemoryPolicy or the default.
func (w *Workflow) MemoryPolicyOrDefault() MemoryPolicy {
	if w.memoryPolicy != nil {
		return w.memoryPolicy
	}
	return NewDefaultMemoryPolicy()
}

// AddStage registers a stage in the workflow's DAG. Sets stage.Client automatically.
func (w *Workflow) AddStage(s *Stage) error {
	if s == nil {
		return fmt.Errorf("stage cannot be nil")
	}
	if s.ID == "" {
		return fmt.Errorf("stage id is required")
	}
	if _, exists := w.stages[s.ID]; exists {
		return fmt.Errorf("stage %q already exists", s.ID)
	}
	s.Client = w.client
	w.stages[s.ID] = s
	return nil
}

// AddDependency declares that stageID depends on dependsOnStageID
// (dependsOnStageID must finish before stageID starts).
func (w *Workflow) AddDependency(stageID, dependsOnStageID string) error {
	if _, ok := w.stages[stageID]; !ok {
		return fmt.Errorf("stage %q not found", stageID)
	}
	if _, ok := w.stages[dependsOnStageID]; !ok {
		return fmt.Errorf("dependency stage %q not found", dependsOnStageID)
	}
	w.dependencies[stageID] = append(w.dependencies[stageID], dependsOnStageID)
	return nil
}

// Stages returns all DAG stages keyed by ID.
func (w *Workflow) Stages() map[string]*Stage {
	out := make(map[string]*Stage, len(w.stages))
	maps.Copy(out, w.stages)
	return out
}

// notifyWorkflowComplete sends a best-effort notification to the server so the
// Memory Manager can flush caches and clean up workflow tracking.
func (w *Workflow) notifyWorkflowComplete(ctx context.Context, workflowID string) {
	if w.client == nil {
		return
	}
	backends := make(map[string]struct{})
	for _, stage := range w.stages {
		if stage.Backend != nil && stage.Backend.Name != "" {
			backends[stage.Backend.Name] = struct{}{}
		}
	}
	if len(backends) == 0 {
		return
	}
	backendList := make([]string, 0, len(backends))
	for b := range backends {
		backendList = append(backendList, b)
	}
	if err := w.client.WorkflowComplete(ctx, workflowID, backendList); err != nil {
		slog.Debug("Memory manager: workflow complete notification failed",
			"workflow_id", workflowID,
			"error", err)
	}
}

// Execute runs the workflow's stage DAG with dependency-aware scheduling.
// Independent stages execute concurrently; context is passed between stages
// via PromptBuilder/MessagesBuilder. Returns results keyed by stage ID.
func (w *Workflow) Execute(ctx context.Context) (map[string]*StageResult, error) {
	if len(w.stages) == 0 {
		return map[string]*StageResult{}, nil
	}

	workflowID := namesgenerator.GetRandomName(0)
	for _, s := range w.stages {
		s.setWorkflowID(workflowID)
	}

	for id, deps := range w.dependencies {
		for _, depID := range deps {
			if _, ok := w.stages[depID]; !ok {
				return nil, fmt.Errorf("stage %q depends on unknown stage %q", id, depID)
			}
		}
	}

	dependents := make(map[string][]string, len(w.stages))
	remainingDeps := make(map[string]int, len(w.stages))
	for id := range w.stages {
		for _, depID := range w.dependencies[id] {
			dependents[depID] = append(dependents[depID], id)
		}
		remainingDeps[id] = len(w.dependencies[id])
	}

	results := make(map[string]*StageResult, len(w.stages))
	var resultsMu sync.RWMutex
	var remainingMu sync.Mutex

	type stageOutcome struct {
		id        string
		err       error
		unblocked []string
	}

	outcomeCh := make(chan stageOutcome, len(w.stages))
	readyCh := make(chan string, len(w.stages))

	startStage := func(stageID string) {
		go func() {
			stage := w.stages[stageID]

			resultsMu.RLock()
			depSnapshot := make(map[string]*StageResult, len(results))
			maps.Copy(depSnapshot, results)
			resultsMu.RUnlock()

			result, err := w.executeStageInDAG(ctx, stage, depSnapshot)
			if err != nil {
				outcomeCh <- stageOutcome{id: stageID, err: fmt.Errorf("stage %q: %w", stageID, err)}
				return
			}

			resultsMu.Lock()
			results[stageID] = result
			resultsMu.Unlock()

			remainingMu.Lock()
			var unblocked []string
			for _, dep := range dependents[stageID] {
				remainingDeps[dep]--
				if remainingDeps[dep] == 0 {
					unblocked = append(unblocked, dep)
				}
			}
			remainingMu.Unlock()

			outcomeCh <- stageOutcome{id: stageID, unblocked: unblocked}
		}()
	}

	remainingMu.Lock()
	for id, deps := range remainingDeps {
		if deps == 0 {
			readyCh <- id
		}
	}
	remainingMu.Unlock()

	dispatched := 0
	completed := 0
	for {
		for {
			select {
			case stageID := <-readyCh:
				startStage(stageID)
				dispatched++
			default:
				goto waitOutcome
			}
		}

	waitOutcome:
		if completed == len(w.stages) {
			break
		}
		if dispatched == completed {
			select {
			case stageID := <-readyCh:
				startStage(stageID)
				dispatched++
				continue
			default:
				return nil, fmt.Errorf("workflow: stage DAG has a cycle; completed %d/%d stages", completed, len(w.stages))
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case outcome := <-outcomeCh:
			if outcome.err != nil {
				return nil, outcome.err
			}
			completed++
			for _, next := range outcome.unblocked {
				readyCh <- next
			}
		}
	}

	if dispatched != len(w.stages) {
		return nil, fmt.Errorf("workflow: stage DAG has a cycle; dispatched %d/%d stages", dispatched, len(w.stages))
	}

	w.notifyWorkflowComplete(ctx, workflowID)
	return results, nil
}

// --- Stage execution within the DAG ---

const defaultMaxAgentLoopTurns = 100

func (w *Workflow) executeStageInDAG(ctx context.Context, stage *Stage, depResults map[string]*StageResult) (*StageResult, error) {
	switch stage.ExecutionMode {
	case ExecutionModeAgentLoop:
		return w.executeAgentLoopStage(ctx, stage, depResults)
	default:
		return w.executeSingleShotStage(ctx, stage, depResults)
	}
}

func (w *Workflow) executeSingleShotStage(ctx context.Context, stage *Stage, depResults map[string]*StageResult) (*StageResult, error) {
	if stage.MessagesBuilder != nil {
		msgs, err := stage.MessagesBuilder(depResults)
		if err != nil {
			return nil, fmt.Errorf("messages builder: %w", err)
		}
		if stage.Stream {
			stream, err := stage.ExecuteStreamWithMessages(ctx, msgs)
			if err != nil {
				return nil, err
			}
			resp, err := stage.ConsumeStream(ctx, stream, nil)
			if err != nil {
				return nil, err
			}
			return &StageResult{Response: resp, Messages: msgs}, nil
		}
		resp, err := stage.ExecuteWithMessages(ctx, msgs)
		if err != nil {
			return nil, err
		}
		return &StageResult{Response: resp, Messages: msgs}, nil
	}

	prompt := stage.Prompt
	if stage.PromptBuilder != nil {
		built, err := stage.PromptBuilder(depResults)
		if err != nil {
			return nil, fmt.Errorf("prompt builder: %w", err)
		}
		prompt = built
	}
	if prompt == "" {
		return nil, fmt.Errorf("prompt is empty")
	}
	if stage.Stream {
		stream, err := stage.ExecuteStream(ctx, prompt)
		if err != nil {
			return nil, err
		}
		resp, err := stage.ConsumeStream(ctx, stream, nil)
		if err != nil {
			return nil, err
		}
		return &StageResult{Response: resp}, nil
	}
	resp, err := stage.Execute(ctx, prompt)
	if err != nil {
		return nil, err
	}
	return &StageResult{Response: resp}, nil
}

func (w *Workflow) executeAgentLoopStage(ctx context.Context, stage *Stage, depResults map[string]*StageResult) (*StageResult, error) {
	var messages []Message

	if stage.MessagesBuilder != nil {
		msgs, err := stage.MessagesBuilder(depResults)
		if err != nil {
			return nil, fmt.Errorf("messages builder: %w", err)
		}
		messages = msgs
	} else {
		prompt := stage.Prompt
		if stage.PromptBuilder != nil {
			built, err := stage.PromptBuilder(depResults)
			if err != nil {
				return nil, fmt.Errorf("prompt builder: %w", err)
			}
			prompt = built
		}
		if prompt == "" {
			return nil, fmt.Errorf("prompt is empty")
		}
		messages = []Message{{Role: "user", Content: prompt}}
	}

	maxTurns := stage.MaxTurns
	if maxTurns <= 0 {
		maxTurns = defaultMaxAgentLoopTurns
	}

	var lastResp *InferenceResponse
	for turn := range maxTurns {
		_ = turn
		resp, err := stage.ExecuteWithMessages(ctx, messages)
		if err != nil {
			return nil, fmt.Errorf("turn %d: %w", turn+1, err)
		}
		lastResp = resp

		messages = append(messages, Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})

		if len(resp.ToolCalls) == 0 {
			break
		}

		toolMsgs, err := stage.RunToolCallsInResponse(ctx, resp)
		if err != nil {
			return nil, fmt.Errorf("turn %d tool calls: %w", turn+1, err)
		}
		for _, msg := range toolMsgs {
			messages = append(messages, *msg)
		}
	}

	return &StageResult{Response: lastResp, Messages: messages}, nil
}
