package serving

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving/cost"
	"github.com/harvard-cns/orla/internal/serving/memory"
	"github.com/harvard-cns/orla/internal/serving/metrics"
	"go.uber.org/zap"
)

type scheduledRequest struct {
	ctx       context.Context
	backend   string
	stageName string
	messages  []model.Message
	tools     []*model.Tool
	opts      model.InferenceOptions

	enqueuedAt time.Time

	// Memory Manager metadata
	workflowID  string
	cachePolicy string

	resultCh chan scheduledResult
}

type scheduledResult struct {
	response *model.Response
	streamCh <-chan model.StreamEvent
	err      error
}

// backendExecutor dispatches requests for one backend and applies a scheduling policy.
// It runs a configurable number of worker goroutines; concurrency defaults to 1 (serial).
type backendExecutor struct {
	backendName    string
	manager        *LLMBackendManager
	memoryManager  *memory.DefaultManager
	maxConcurrency int

	mu          sync.Mutex
	cond        *sync.Cond
	stageQueues map[string][]*scheduledRequest
	queueLen    int
	capacity    int
	policy      model.SchedulingPolicy
	policySet   bool
	closed      bool
}

func newBackendExecutor(backendName string, manager *LLMBackendManager, maxConcurrency, queueCapacity int, mm *memory.DefaultManager) *backendExecutor {
	exec := &backendExecutor{
		backendName:    backendName,
		manager:        manager,
		memoryManager:  mm,
		maxConcurrency: maxConcurrency,
		capacity:       queueCapacity,
		stageQueues:    make(map[string][]*scheduledRequest),
	}
	exec.cond = sync.NewCond(&exec.mu)
	for range maxConcurrency {
		go exec.worker()
	}
	return exec
}

func (e *backendExecutor) enqueue(req *scheduledRequest) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return errors.New("backend executor is closed")
	}
	if e.queueLen >= e.capacity {
		return fmt.Errorf("backend queue is full for backend %q", e.backendName)
	}
	if !e.policySet {
		e.policy = req.opts.GetSchedulingPolicy()
		e.policySet = true
	}
	// Keep scheduling policy consistent per backend executor.
	req.opts.SchedulingPolicy = e.policy

	stage := req.stageName
	if stage == "" {
		stage = "default"
	}
	e.stageQueues[stage] = append(e.stageQueues[stage], req)
	e.queueLen++
	metrics.QueueDepth.WithLabelValues(e.backendName).Set(float64(e.queueLen))
	e.cond.Signal()
	return nil
}

func (e *backendExecutor) close() {
	e.mu.Lock()
	e.closed = true
	e.cond.Broadcast()
	e.mu.Unlock()
}

func (e *backendExecutor) worker() {
	for {
		req, schedulerDecisionMs, ok := e.dequeue()
		if !ok {
			return
		}
		if req.ctx.Err() != nil {
			metrics.RequestsTotal.WithLabelValues(req.backend, "error").Inc()
			req.resultCh <- scheduledResult{err: req.ctx.Err()}
			continue
		}

		provider, err := e.manager.GetModelProvider(req.ctx, req.backend)
		if err != nil {
			metrics.RequestsTotal.WithLabelValues(req.backend, "error").Inc()
			req.resultCh <- scheduledResult{err: fmt.Errorf("failed to get provider for server '%s': %w", req.backend, err)}
			continue
		}

		requestID := fmt.Sprintf("%s-%s-%d", req.backend, req.stageName, req.enqueuedAt.UnixNano())
		if e.memoryManager != nil && req.workflowID != "" {
			e.memoryManager.RegisterWorkflow(req.workflowID)
			e.memoryManager.RecordInflight(core.InflightRequest{
				RequestID:  requestID,
				WorkflowID: req.workflowID,
				StageID:    req.stageName,
				Backend:    req.backend,
				Streaming:  req.opts.Stream,
				StartedAt:  time.Now(),
			})
			modelID := e.manager.GetModelID(req.backend)
			e.memoryManager.OnTransition(req.ctx, memory.StageTransition{
				TransitionType: memory.TransitionStageStart,
				WorkflowID:     req.workflowID,
				StageID:        req.stageName,
				Backend:        req.backend,
				Model:          modelID,
				CachePolicy:    req.cachePolicy,
			})
		}

		dispatchStart := time.Now()
		response, streamCh, err := chatWithRetry(req.ctx, provider, req.messages, req.tools, req.opts)
		dispatchMs := time.Since(dispatchStart).Milliseconds()
		queueWaitMs := dispatchStart.Sub(req.enqueuedAt).Milliseconds()

		if e.memoryManager != nil && req.workflowID != "" && !req.opts.Stream {
			e.memoryManager.ClearInflight(req.backend, requestID)
			e.memoryManager.OnTransition(req.ctx, memory.StageTransition{
				TransitionType: memory.TransitionStageComplete,
				WorkflowID:     req.workflowID,
				StageID:        req.stageName,
				Backend:        req.backend,
				Model:          e.manager.GetModelID(req.backend),
				CachePolicy:    req.cachePolicy,
			})
		}

		if err != nil {
			metrics.RequestsTotal.WithLabelValues(req.backend, "error").Inc()
			metrics.BackendLatencySeconds.WithLabelValues(req.backend).Observe(float64(dispatchMs) / 1000)
			if e.memoryManager != nil && req.workflowID != "" && req.opts.Stream {
				e.memoryManager.ClearInflight(req.backend, requestID)
			}
			req.resultCh <- scheduledResult{err: fmt.Errorf("inference failed on server '%s': %w", req.backend, err)}
			continue
		}

		metrics.RequestsTotal.WithLabelValues(req.backend, "success").Inc()
		metrics.QueueWaitSeconds.WithLabelValues(req.backend).Observe(float64(queueWaitMs) / 1000)
		metrics.BackendLatencySeconds.WithLabelValues(req.backend).Observe(float64(dispatchMs) / 1000)

		// For non-streaming, set metrics immediately. For streaming, the provider's goroutine
		// populates response (including Metrics) concurrently. We must not race with it.
		// We add queue/scheduler metrics after the stream completes (see below).
		if !req.opts.Stream {
			if response.Metrics == nil {
				response.Metrics = &model.ResponseMetrics{}
			}
			response.Metrics.QueueWaitMs = queueWaitMs
			response.Metrics.SchedulerDecisionMs = schedulerDecisionMs
			response.Metrics.DispatchMs = dispatchMs
			response.Metrics.BackendLatencyMs = &dispatchMs
			estCost, err := cost.EstimatedCostUSD(
				response.Metrics.PromptTokens, response.Metrics.CompletionTokens,
				e.manager.GetCostModel(req.backend))
			if err != nil {
				zap.L().Warn("cost estimation failed", zap.String("backend", req.backend), zap.Error(err))
			}
			response.Metrics.EstimatedCostUSD = estCost
			if estCost != nil {
				metrics.EstimatedCostTotal.WithLabelValues(req.backend).Add(*estCost)
				metrics.EstimatedCostPerRequest.WithLabelValues(req.backend).Observe(*estCost)
			}
		}

		// For streaming requests, proxy the channel through a wrapper so we
		// can block until the stream completes. This enforces maxConcurrency
		// for streaming: the worker won't dequeue the next request until the
		// backend finishes generating for the current one.
		var streamDone <-chan struct{}
		if req.opts.Stream && streamCh != nil {
			done := make(chan struct{})
			proxyCh := make(chan model.StreamEvent, 32)
			sourceCh := streamCh // capture before reassignment; goroutine closure would see proxyCh otherwise
			go func() {
				defer close(proxyCh)
				defer close(done)
				for ev := range sourceCh {
					select {
					case proxyCh <- ev:
					case <-req.ctx.Done():
						for range sourceCh {
						}
						return
					}
				}
			}()
			streamCh = proxyCh
			streamDone = done
		}

		req.resultCh <- scheduledResult{
			response: response,
			streamCh: streamCh,
			err:      nil,
		}

		if streamDone != nil {
			<-streamDone
			// Provider's goroutine has finished; safe to add queue/scheduler metrics.
			if response.Metrics == nil {
				response.Metrics = &model.ResponseMetrics{}
			}
			response.Metrics.QueueWaitMs = queueWaitMs
			response.Metrics.SchedulerDecisionMs = schedulerDecisionMs
			response.Metrics.DispatchMs = dispatchMs
			estCost, costErr := cost.EstimatedCostUSD(
				response.Metrics.PromptTokens, response.Metrics.CompletionTokens,
				e.manager.GetCostModel(req.backend))
			if costErr != nil {
				zap.L().Warn("cost estimation failed", zap.String("backend", req.backend), zap.Error(costErr))
			}
			response.Metrics.EstimatedCostUSD = estCost
			if estCost != nil {
				metrics.EstimatedCostTotal.WithLabelValues(req.backend).Add(*estCost)
				metrics.EstimatedCostPerRequest.WithLabelValues(req.backend).Observe(*estCost)
			}
			if e.memoryManager != nil && req.workflowID != "" {
				e.memoryManager.ClearInflight(req.backend, requestID)
				e.memoryManager.OnTransition(req.ctx, memory.StageTransition{
					TransitionType: memory.TransitionStageComplete,
					WorkflowID:     req.workflowID,
					StageID:        req.stageName,
					Backend:        req.backend,
					Model:          e.manager.GetModelID(req.backend),
					CachePolicy:    req.cachePolicy,
				})
			}
		}
	}
}

func (e *backendExecutor) dequeue() (*scheduledRequest, int64, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for e.queueLen == 0 && !e.closed {
		e.cond.Wait()
	}
	if e.closed {
		return nil, 0, false
	}

	decisionStart := time.Now()
	stage := selectNextStageKey(e.stageQueues, e.policy)
	stageQueue := e.stageQueues[stage]

	idx := selectNextRequest(stageQueue)
	req := stageQueue[idx]
	stageQueue = append(stageQueue[:idx], stageQueue[idx+1:]...)
	if len(stageQueue) == 0 {
		delete(e.stageQueues, stage)
	} else {
		e.stageQueues[stage] = stageQueue
	}
	e.queueLen--
	metrics.QueueDepth.WithLabelValues(e.backendName).Set(float64(e.queueLen))
	decisionMs := time.Since(decisionStart).Milliseconds()
	return req, decisionMs, true
}

// selectNextRequest returns the index of the next request to dequeue from a stage queue.
// If the head request's RequestSchedulingPolicy is "priority", picks the highest-priority
// request in the queue (tie-breaking by oldest enqueue time). Otherwise FCFS (index 0).
func selectNextRequest(queue []*scheduledRequest) int {
	if len(queue) <= 1 {
		return 0
	}
	head := queue[0]
	if head.opts.RequestSchedulingPolicy != model.RequestSchedulingPolicyPriority {
		return 0
	}
	bestIdx := 0
	bestPriority := head.opts.SchedulingHints.GetRequestPriority()
	for i, req := range queue[1:] {
		p := req.opts.SchedulingHints.GetRequestPriority()
		if p > bestPriority || (p == bestPriority && req.enqueuedAt.Before(queue[bestIdx].enqueuedAt)) {
			bestIdx = i + 1
			bestPriority = p
		}
	}
	return bestIdx
}

func selectNextStageKey(stageQueues map[string][]*scheduledRequest, policy model.SchedulingPolicy) string {
	// Stage-level scheduling only: requests are always FCFS within each stage queue.
	// Between stages, apply the backend policy:
	// 1) priority     -> highest priority head request (tie: older request first), or
	// 2) fcfs/default    -> oldest head request across stages.

	var bestStage string
	var bestReq *scheduledRequest
	for stage, q := range stageQueues {
		if len(q) == 0 {
			continue
		}
		head := q[0]
		if bestReq == nil {
			bestStage = stage
			bestReq = head
			continue
		}
		if policy == model.SchedulingPolicyPriority {
			headPriority := head.opts.SchedulingHints.GetPriority()
			bestPriority := bestReq.opts.SchedulingHints.GetPriority()
			if headPriority > bestPriority || (headPriority == bestPriority && head.enqueuedAt.Before(bestReq.enqueuedAt)) {
				bestStage = stage
				bestReq = head
			}
			continue
		}
		if head.enqueuedAt.Before(bestReq.enqueuedAt) {
			bestStage = stage
			bestReq = head
		}
	}
	return bestStage
}
