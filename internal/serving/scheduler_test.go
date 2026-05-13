package serving

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectNextStageKey_FCFSPrefersOldestAcrossStages(t *testing.T) {
	now := time.Now()
	stageQueues := map[string][]*scheduledRequest{
		"stage-new": {
			{
				stageName:  "stage-new",
				enqueuedAt: now.Add(-1 * time.Second),
				opts:       model.InferenceOptions{SchedulingPolicy: model.SchedulingPolicyFCFS},
			},
		},
		"stage-old": {
			{
				stageName:  "stage-old",
				enqueuedAt: now.Add(-5 * time.Second),
				opts:       model.InferenceOptions{SchedulingPolicy: model.SchedulingPolicyFCFS},
			},
		},
	}

	selected := selectNextStageKey(stageQueues, model.SchedulingPolicyFCFS)
	assert.Equal(t, "stage-old", selected)
}

func TestSelectNextStageKey_PriorityPolicyPrefersHigherPriority(t *testing.T) {
	low := 1
	high := 9
	now := time.Now()
	stageQueues := map[string][]*scheduledRequest{
		"light": {
			{
				enqueuedAt: now.Add(-1 * time.Second),
				stageName:  "light",
				opts: model.InferenceOptions{
					SchedulingPolicy: model.SchedulingPolicyPriority,
					SchedulingHints:  &model.SchedulingHints{Priority: &low},
				},
			},
		},
		"heavy": {
			{
				enqueuedAt: now.Add(-1 * time.Second),
				stageName:  "heavy",
				opts: model.InferenceOptions{
					SchedulingPolicy: model.SchedulingPolicyPriority,
					SchedulingHints:  &model.SchedulingHints{Priority: &high},
				},
			},
		},
	}

	selected := selectNextStageKey(stageQueues, model.SchedulingPolicyPriority)
	assert.Equal(t, "heavy", selected)
}

func TestSelectNextStageKey_PriorityTieBreaksByOldestHead(t *testing.T) {
	now := time.Now()
	priority := 5
	stageQueues := map[string][]*scheduledRequest{
		"older": {
			{
				enqueuedAt: now.Add(-3 * time.Second),
				stageName:  "older",
				opts: model.InferenceOptions{
					SchedulingPolicy: model.SchedulingPolicyPriority,
					SchedulingHints:  &model.SchedulingHints{Priority: &priority},
				},
			},
		},
		"newer": {
			{
				enqueuedAt: now.Add(-1 * time.Second),
				stageName:  "newer",
				opts: model.InferenceOptions{
					SchedulingPolicy: model.SchedulingPolicyPriority,
					SchedulingHints:  &model.SchedulingHints{Priority: &priority},
				},
			},
		},
	}

	selected := selectNextStageKey(stageQueues, model.SchedulingPolicyPriority)
	assert.Equal(t, "older", selected)
}

// --- selectNextRequest tests ---

func TestSelectNextRequest_FCFSDefault(t *testing.T) {
	now := time.Now()
	queue := []*scheduledRequest{
		{enqueuedAt: now.Add(-2 * time.Second), opts: model.InferenceOptions{}},
		{enqueuedAt: now.Add(-1 * time.Second), opts: model.InferenceOptions{}},
	}
	assert.Equal(t, 0, selectNextRequest(queue))
}

func TestSelectNextRequest_PriorityPicksHighest(t *testing.T) {
	now := time.Now()
	low := 1
	high := 9
	queue := []*scheduledRequest{
		{
			enqueuedAt: now.Add(-2 * time.Second),
			opts: model.InferenceOptions{
				RequestSchedulingPolicy: model.RequestSchedulingPolicyPriority,
				SchedulingHints:         &model.SchedulingHints{RequestPriority: &low},
			},
		},
		{
			enqueuedAt: now.Add(-1 * time.Second),
			opts: model.InferenceOptions{
				RequestSchedulingPolicy: model.RequestSchedulingPolicyPriority,
				SchedulingHints:         &model.SchedulingHints{RequestPriority: &high},
			},
		},
	}
	assert.Equal(t, 1, selectNextRequest(queue))
}

func TestSelectNextRequest_PriorityTieBreaksByOldest(t *testing.T) {
	now := time.Now()
	same := 5
	queue := []*scheduledRequest{
		{
			enqueuedAt: now.Add(-1 * time.Second),
			opts: model.InferenceOptions{
				RequestSchedulingPolicy: model.RequestSchedulingPolicyPriority,
				SchedulingHints:         &model.SchedulingHints{RequestPriority: &same},
			},
		},
		{
			enqueuedAt: now.Add(-3 * time.Second),
			opts: model.InferenceOptions{
				RequestSchedulingPolicy: model.RequestSchedulingPolicyPriority,
				SchedulingHints:         &model.SchedulingHints{RequestPriority: &same},
			},
		},
	}
	assert.Equal(t, 1, selectNextRequest(queue))
}

func TestSelectNextRequest_SingleElement(t *testing.T) {
	queue := []*scheduledRequest{{opts: model.InferenceOptions{}}}
	assert.Equal(t, 0, selectNextRequest(queue))
}

// --- backendExecutor concurrency tests ---

type delayProvider struct {
	delay      time.Duration
	inflight   atomic.Int32
	maxSeen    atomic.Int32
	callCount  atomic.Int32
}

func (p *delayProvider) Name() string { return "delay" }

func (p *delayProvider) Chat(_ context.Context, _ []model.Message, _ []*model.Tool, _ model.InferenceOptions) (*model.Response, <-chan model.StreamEvent, error) {
	p.callCount.Add(1)
	cur := p.inflight.Add(1)
	for {
		old := p.maxSeen.Load()
		if cur <= old || p.maxSeen.CompareAndSwap(old, cur) {
			break
		}
	}
	time.Sleep(p.delay)
	p.inflight.Add(-1)
	return &model.Response{Content: "ok"}, nil, nil
}

func (p *delayProvider) EnsureReady(_ context.Context) error { return nil }

func TestBackendExecutor_DefaultConcurrencyIsSerial(t *testing.T) {
	manager := NewLLMBackendManager(nil)
	manager.AddLLMBackend("b", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: "http://localhost:11434/v1",
	}, "openai:m")

	dp := &delayProvider{delay: 50 * time.Millisecond}
	manager.mu.Lock()
	manager.providers["b"] = dp
	manager.mu.Unlock()

	const n = 4
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, _, err := manager.ScheduleChat(context.Background(), "b", "s", []model.Message{{Role: "user", Content: "hi"}}, nil, model.InferenceOptions{})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(n), dp.callCount.Load())
	assert.Equal(t, int32(1), dp.maxSeen.Load(), "default concurrency should be serial (max 1 in-flight)")
}

func TestBackendExecutor_ConcurrencyRespected(t *testing.T) {
	const concurrency = 3
	manager := NewLLMBackendManager(nil)
	manager.AddLLMBackend("b", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: "http://localhost:11434/v1",
		MaxConcurrency: core.Ptr(concurrency),
	}, "openai:m")

	dp := &delayProvider{delay: 100 * time.Millisecond}
	manager.mu.Lock()
	manager.providers["b"] = dp
	manager.mu.Unlock()

	const n = 6
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, _, err := manager.ScheduleChat(context.Background(), "b", "s", []model.Message{{Role: "user", Content: "hi"}}, nil, model.InferenceOptions{})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(n), dp.callCount.Load())
	require.LessOrEqual(t, dp.maxSeen.Load(), int32(concurrency), "should not exceed max concurrency")
	assert.Greater(t, dp.maxSeen.Load(), int32(1), "should utilize multiple workers")
}

func TestEffectiveMaxConcurrency_NilDefaultsToOne(t *testing.T) {
	b := &core.LLMBackend{}
	assert.Equal(t, 1, b.EffectiveMaxConcurrency())
}

func TestEffectiveMaxConcurrency_ZeroDefaultsToOne(t *testing.T) {
	b := &core.LLMBackend{MaxConcurrency: core.Ptr(0)}
	assert.Equal(t, 1, b.EffectiveMaxConcurrency())
}

func TestEffectiveMaxConcurrency_NegativeDefaultsToOne(t *testing.T) {
	b := &core.LLMBackend{MaxConcurrency: core.Ptr(-5)}
	assert.Equal(t, 1, b.EffectiveMaxConcurrency())
}

func TestEffectiveQueueCapacity_NilDefaultsToConstant(t *testing.T) {
	b := &core.LLMBackend{}
	assert.Equal(t, core.DefaultBackendQueueCapacity, b.EffectiveQueueCapacity())
}

func TestEffectiveQueueCapacity_ZeroDefaultsToConstant(t *testing.T) {
	b := &core.LLMBackend{QueueCapacity: core.Ptr(0)}
	assert.Equal(t, core.DefaultBackendQueueCapacity, b.EffectiveQueueCapacity())
}
