package orla

import (
	"context"
	"fmt"

	"github.com/docker/docker/pkg/namesgenerator"
)

// ExecutionMode controls how a stage executes within a workflow DAG.
type ExecutionMode string

const (
	ExecutionModeSingleShot ExecutionMode = "single_shot"
	ExecutionModeAgentLoop  ExecutionMode = "agent_loop"
)

// StageResult wraps the output of a stage execution.
type StageResult struct {
	Response *InferenceResponse
	Messages []Message // full conversation history (populated for agent-loop stages)
}

// StagePromptBuilder builds a prompt from upstream dependency results for DAG execution.
type StagePromptBuilder func(results map[string]*StageResult) (string, error)

// StageMessagesBuilder builds messages from upstream dependency results for DAG execution.
type StageMessagesBuilder func(results map[string]*StageResult) ([]Message, error)

// StreamHandler is an optional callback invoked for each stream event.
// ConsumeStream always accumulates and returns the full InferenceResponse; the handler is for side effects only.
type StreamHandler func(event StreamEvent) error

// Stage is the primary execution unit in Orla. It holds backend, inference options,
// tools, execution mode, and scheduling configuration. Each Stage has a globally
// unique ID and can execute LLM inference calls directly.
type Stage struct {
	// ID is a globally unique identifier, auto-generated if not set.
	ID   string
	Name string

	// Client is required for execution methods (Execute, ExecuteStream, etc.).
	// Set automatically when the stage is added to a Workflow.
	Client  *OrlaClient
	Backend *LLMBackend

	Tools         map[string]*Tool
	ExecutionMode ExecutionMode
	MaxTurns      int // max turns for agent-loop mode; 0 means default (100)

	Prompt          string
	PromptBuilder   StagePromptBuilder
	MessagesBuilder StageMessagesBuilder

	MaxTokens          *int
	Temperature        *float64
	TopP               *float64
	ResponseFormat     *StructuredOutputRequest
	ChatTemplateKwargs map[string]any
	ReasoningEffort    string

	StageSchedulingPolicy   string
	RequestSchedulingPolicy string
	SchedulingHints         *SchedulingHints

	CachePolicy string      // "preserve", "flush", or "" (auto/default)
	CacheHnts   *CacheHints // per-stage cache hint overrides

	Stream bool // when true, workflow uses streaming execution to capture TTFT/TPOT

	workflowID string // set internally by Workflow, not user-facing
}

func randomStageID() string {
	return namesgenerator.GetRandomName(0)
}

// NewStage returns a new Stage with a globally unique ID and the given backend.
func NewStage(name string, backend *LLMBackend) *Stage {
	return &Stage{
		ID:            randomStageID(),
		Name:          name,
		Backend:       backend,
		Tools:         make(map[string]*Tool),
		ExecutionMode: ExecutionModeSingleShot,
	}
}

func (s *Stage) SetMaxTokens(n int)                              { s.MaxTokens = &n }
func (s *Stage) SetTemperature(f float64)                        { s.Temperature = &f }
func (s *Stage) SetTopP(f float64)                               { s.TopP = &f }
func (s *Stage) SetResponseFormat(r *StructuredOutputRequest)    { s.ResponseFormat = r }
func (s *Stage) SetChatTemplateKwargs(kwargs map[string]any)     { s.ChatTemplateKwargs = kwargs }
func (s *Stage) SetReasoningEffort(effort string)                { s.ReasoningEffort = effort }
func (s *Stage) SetSchedulingPolicy(policy string)               { s.StageSchedulingPolicy = policy }
func (s *Stage) SetRequestSchedulingPolicy(policy string)        { s.RequestSchedulingPolicy = policy }
func (s *Stage) SetSchedulingHints(hints *SchedulingHints)       { s.SchedulingHints = hints }
func (s *Stage) SetExecutionMode(mode ExecutionMode)             { s.ExecutionMode = mode }
func (s *Stage) SetMaxTurns(n int)                               { s.MaxTurns = n }
func (s *Stage) SetPromptBuilder(builder StagePromptBuilder)     { s.PromptBuilder = builder }
func (s *Stage) SetMessagesBuilder(builder StageMessagesBuilder) { s.MessagesBuilder = builder }
func (s *Stage) SetCachePolicy(policy string)                    { s.CachePolicy = policy }
func (s *Stage) SetCacheHints(hints *CacheHints)                 { s.CacheHnts = hints }
func (s *Stage) SetStream(enabled bool)                          { s.Stream = enabled }

// AddTool adds a tool to this stage. Returns an error if t is nil.
func (s *Stage) AddTool(t *Tool) error {
	if t == nil {
		return fmt.Errorf("tool cannot be nil")
	}
	s.Tools[t.Name] = t
	return nil
}

// --- Request building ---

func (s *Stage) buildRequest(prompt string) (*ExecuteRequest, error) {
	if s.Backend == nil {
		return nil, fmt.Errorf("stage %q: backend is nil", s.Name)
	}
	r := &ExecuteRequest{
		Backend: s.Backend.Name,
		StageID: s.ID,
		Prompt:  prompt,
	}
	s.applyInferenceOptions(r)
	return r, nil
}

func (s *Stage) buildRequestWithMessages(messages []Message) (*ExecuteRequest, error) {
	if s.Backend == nil {
		return nil, fmt.Errorf("stage %q: backend is nil", s.Name)
	}
	r := &ExecuteRequest{
		Backend:  s.Backend.Name,
		StageID:  s.ID,
		Messages: messages,
	}
	s.applyInferenceOptions(r)
	if len(s.Tools) > 0 {
		tools, err := s.toolsToWire()
		if err != nil {
			return nil, err
		}
		r.Tools = tools
	}
	return r, nil
}

// setWorkflowID is called by the Workflow to propagate workflow context into requests.
func (s *Stage) setWorkflowID(wfID string) { s.workflowID = wfID }

func (s *Stage) applyInferenceOptions(r *ExecuteRequest) {
	r.MaxTokens = s.MaxTokens
	r.Temperature = s.Temperature
	r.TopP = s.TopP
	r.ResponseFormat = s.ResponseFormat
	r.ChatTemplateKwargs = s.ChatTemplateKwargs
	r.ReasoningEffort = s.ReasoningEffort
	r.SchedulingPolicy = s.StageSchedulingPolicy
	r.RequestSchedulingPolicy = s.RequestSchedulingPolicy
	r.SchedulingHints = s.SchedulingHints
	r.CachePolicy = s.CachePolicy
	r.CacheHints = s.CacheHnts
	r.WorkflowID = s.workflowID
}

func (s *Stage) toolsToWire() ([]*toolPayload, error) {
	out := make([]*toolPayload, 0, len(s.Tools))
	for _, t := range s.Tools {
		w, err := t.toWire()
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// --- Execution methods ---

// Execute runs a single non-streaming inference with the given prompt.
func (s *Stage) Execute(ctx context.Context, prompt string) (*InferenceResponse, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("stage %q: client is nil", s.Name)
	}
	req, err := s.buildRequest(prompt)
	if err != nil {
		return nil, err
	}
	return s.Client.Execute(ctx, req)
}

// ExecuteStream runs inference with streaming; returns a channel of events.
func (s *Stage) ExecuteStream(ctx context.Context, prompt string) (<-chan StreamEvent, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("stage %q: client is nil", s.Name)
	}
	req, err := s.buildRequest(prompt)
	if err != nil {
		return nil, err
	}
	return s.Client.ExecuteStream(ctx, req)
}

// ExecuteWithMessages runs a single non-streaming inference with the given message list and any attached tools.
func (s *Stage) ExecuteWithMessages(ctx context.Context, messages []Message) (*InferenceResponse, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("stage %q: client is nil", s.Name)
	}
	req, err := s.buildRequestWithMessages(messages)
	if err != nil {
		return nil, err
	}
	return s.Client.Execute(ctx, req)
}

// ExecuteStreamWithMessages runs streaming inference with the given message list and any attached tools.
func (s *Stage) ExecuteStreamWithMessages(ctx context.Context, messages []Message) (<-chan StreamEvent, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("stage %q: client is nil", s.Name)
	}
	req, err := s.buildRequestWithMessages(messages)
	if err != nil {
		return nil, err
	}
	return s.Client.ExecuteStream(ctx, req)
}

// ConsumeStream reads a stream until "done", accumulates content/thinking/metrics, and returns the result.
func (s *Stage) ConsumeStream(ctx context.Context, stream <-chan StreamEvent, handler StreamHandler) (*InferenceResponse, error) {
	response := &InferenceResponse{Metrics: &InferenceResponseMetrics{}}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("stream closed without done")
			}
			if handler != nil {
				if err := handler(event); err != nil {
					return nil, err
				}
			}
			switch event.Type {
			case "content":
				response.Content += event.Content
			case "thinking":
				response.Thinking += event.Thinking
			case "tool_call":
				// Streaming tool_call deltas are for display; final ToolCalls come in "done"
			case "done":
				if event.Response != nil {
					response.Content = event.Response.Content
					response.Thinking = event.Response.Thinking
					response.ToolCalls = event.Response.ToolCalls
					if event.Response.Metrics != nil {
						response.Metrics = event.Response.Metrics
					}
				}
				return response, nil
			}
		}
	}
}

// --- Tool execution ---

// RunToolCall runs a single tool call against this stage's tools.
func (s *Stage) RunToolCall(ctx context.Context, toolCall *ToolCall) (*ToolResult, error) {
	if toolCall == nil {
		return nil, fmt.Errorf("tool call cannot be nil")
	}
	tool, ok := s.Tools[toolCall.Name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", toolCall.Name)
	}
	toolResult, err := tool.Run(ctx, toolCall.InputArguments)
	if err != nil {
		return nil, fmt.Errorf("failed to run tool call: %w", err)
	}
	if toolResult == nil {
		return nil, fmt.Errorf("tool result is nil")
	}
	toolResult.ID = toolCall.ID
	toolResult.Name = toolCall.Name
	return toolResult, nil
}

// RunToolCallsInResponseAndGetToolResults parses tool calls from the response, runs each, and returns results.
func (s *Stage) RunToolCallsInResponseAndGetToolResults(ctx context.Context, response *InferenceResponse) ([]*ToolResult, error) {
	toolResults := make([]*ToolResult, 0, len(response.ToolCalls))
	for _, call := range response.ToolCalls {
		toolCall, err := NewToolCallFromRawToolCall(call)
		if err != nil {
			return nil, fmt.Errorf("failed to parse tool call: %w", err)
		}
		toolResult, err := s.RunToolCall(ctx, toolCall)
		if err != nil {
			return nil, fmt.Errorf("failed to run tool call: %w", err)
		}
		toolResults = append(toolResults, toolResult)
	}
	return toolResults, nil
}

// RunToolCallsInResponse runs the tool calls in the response and returns tool result messages.
func (s *Stage) RunToolCallsInResponse(ctx context.Context, response *InferenceResponse) ([]*Message, error) {
	toolResults, err := s.RunToolCallsInResponseAndGetToolResults(ctx, response)
	if err != nil {
		return nil, fmt.Errorf("failed to run tool calls: %w", err)
	}
	toolMessages := make([]*Message, 0, len(toolResults))
	for _, toolResult := range toolResults {
		toolMessage, err := toolResult.ToMessage()
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool result to message: %w", err)
		}
		toolMessages = append(toolMessages, toolMessage)
	}
	return toolMessages, nil
}

// --- Stage Mappers ---

// StageMapper maps a prompt to an execution stage.
type StageMapper interface {
	MapStage(ctx context.Context, prompt string) (*Stage, error)
}

// OneBitStageMapper uses a one-bit predictor to map prompts to one of two stages.
type OneBitStageMapper struct {
	OneBitPredictor *OneBitPredictor
	StageOne        *Stage
	StageTwo        *Stage
	Prompt          string
}

// NewOneBitStageMapper returns a new one-bit stage mapper.
func NewOneBitStageMapper(client *OrlaClient, backend *LLMBackend, stageOne *Stage, stageTwo *Stage) *OneBitStageMapper {
	return &OneBitStageMapper{OneBitPredictor: NewOneBitPredictor(client, backend), StageOne: stageOne, StageTwo: stageTwo}
}

// MapStage maps the stage based on the prompt.
func (m *OneBitStageMapper) MapStage(ctx context.Context, prompt string) (*Stage, error) {
	prediction, err := m.OneBitPredictor.Predict(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("failed to predict stage: %w", err)
	}
	if prediction {
		return m.StageOne, nil
	}
	return m.StageTwo, nil
}

// PromptScorer computes a routing score for a prompt.
type PromptScorer func(prompt string) float64

// ThresholdStageMapper routes prompts to one of two stages by comparing score to a threshold.
type ThresholdStageMapper struct {
	Threshold float64
	LowStage  *Stage
	HighStage *Stage
	ScoreFn   PromptScorer
}

// NewThresholdStageMapper creates a stage mapper that routes by score threshold.
func NewThresholdStageMapper(threshold float64, lowStage, highStage *Stage, scoreFn PromptScorer) *ThresholdStageMapper {
	return &ThresholdStageMapper{
		Threshold: threshold,
		LowStage:  lowStage,
		HighStage: highStage,
		ScoreFn:   scoreFn,
	}
}

// MapStage maps prompt to stage based on score threshold.
func (m *ThresholdStageMapper) MapStage(_ context.Context, prompt string) (*Stage, error) {
	scoreFn := m.ScoreFn
	if scoreFn == nil {
		scoreFn = func(p string) float64 { return float64(len(p)) }
	}
	if scoreFn(prompt) >= m.Threshold {
		return m.HighStage, nil
	}
	return m.LowStage, nil
}
