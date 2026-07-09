package scheduler_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/provider"
	"github.com/harvard-cns/orla/internal/scheduler"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func mockFactory(mocks map[string]provider.LLMProvider) scheduler.ProviderFactory {
	return func(b *backends.Backend) provider.Backend {
		if p, ok := mocks[b.Name]; ok {
			return p
		}
		return provider.NewMockProvider().WithName(b.Name)
	}
}

func newBackend(name string, concurrency int32) *backends.Backend {
	return &backends.Backend{
		Name:           name,
		Endpoint:       "x",
		ModelID:        new("openai:m"),
		MaxConcurrency: concurrency,
	}
}

func TestScheduler_RegisterAndDispatch(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithResponse(&openai.ChatCompletion{ID: "ok"})
	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	defer func() { _ = s.Shutdown(context.Background()) }()

	s.Register(newBackend("b", 2))

	resp, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.ID)
}

func TestScheduler_UnknownBackend(t *testing.T) {
	s := scheduler.New(nil, quietLogger())
	_, err := s.Dispatch(context.Background(), "missing", openai.ChatCompletionNewParams{})
	assert.ErrorIs(t, err, scheduler.ErrUnknownBackend)
}

// Verify the concurrency cap: with max_concurrency=2 and a chat that
// holds for 100ms, the 3rd request must wait at least 100ms (for one
// of the first two to finish) before dispatching.
func TestScheduler_ConcurrencyCapEnforced(t *testing.T) {
	gate := make(chan struct{})
	var inflight atomic.Int32
	var maxObserved atomic.Int32

	mock := provider.NewMockProvider().WithName("b").WithChatFunc(
		func(ctx context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			n := inflight.Add(1)
			defer inflight.Add(-1)
			// Track the peak of simultaneous in-flight calls.
			for {
				m := maxObserved.Load()
				if n <= m || maxObserved.CompareAndSwap(m, n) {
					break
				}
			}
			<-gate
			return &openai.ChatCompletion{ID: "ok"}, nil
		},
	)

	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	defer func() { _ = s.Shutdown(context.Background()) }()
	s.Register(newBackend("b", 2))

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			_, _ = s.Dispatch(context.Background(), "b",
				openai.ChatCompletionNewParams{
					Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
				})
		})
	}

	// Give the scheduler a moment to stack requests, capacity=2 means
	// 3 should be queued.
	require.Eventually(t, func() bool {
		stats := s.Stats()
		if len(stats) != 1 {
			return false
		}
		return stats[0].InFlight == 2 && stats[0].QueueDepth == 3
	}, time.Second, 5*time.Millisecond,
		"expected 2 in-flight and 3 queued")

	close(gate)
	wg.Wait()
	assert.LessOrEqual(t, maxObserved.Load(), int32(2),
		"observed peak in-flight must not exceed max_concurrency")
}

func TestScheduler_AcquireCancelInQueue(t *testing.T) {
	hold := make(chan struct{})
	mock := provider.NewMockProvider().WithName("b").WithChatFunc(
		func(ctx context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			<-hold
			return &openai.ChatCompletion{}, nil
		},
	)

	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	t.Cleanup(func() {
		close(hold) // unblock the chatFunc first
		_ = s.Shutdown(context.Background())
	})
	s.Register(newBackend("b", 1))

	// First request takes the only slot.
	go func() {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
		})
	}()
	require.Eventually(t, func() bool {
		return s.Stats()[0].InFlight == 1
	}, time.Second, 5*time.Millisecond)

	// Second request enters the queue, we cancel immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := s.Acquire(ctx, "b", scheduler.RequestInfo{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestScheduler_ShutdownDrainsInFlight(t *testing.T) {
	gate := make(chan struct{})
	mock := provider.NewMockProvider().WithName("b").WithChatFunc(
		func(ctx context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			<-gate
			return &openai.ChatCompletion{ID: "ok"}, nil
		},
	)
	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	s.Register(newBackend("b", 1))

	done := make(chan error, 1)
	go func() {
		_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
		})
		done <- err
	}()

	require.Eventually(t, func() bool {
		return s.Stats()[0].InFlight == 1
	}, time.Second, 5*time.Millisecond)

	// Shutdown should wait for the in-flight dispatch to finish.
	close(gate)
	require.NoError(t, s.Shutdown(context.Background()))
	require.NoError(t, <-done)
	assert.False(t, s.HasBackend("b"))
}

func TestScheduler_ShutdownTimeout(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	mock := provider.NewMockProvider().WithName("b").WithChatFunc(
		func(ctx context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			<-hold
			return &openai.ChatCompletion{}, nil
		},
	)
	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	s.Register(newBackend("b", 1))

	go func() {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
		})
	}()
	require.Eventually(t, func() bool {
		return s.Stats()[0].InFlight == 1
	}, time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	err := s.Shutdown(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestScheduler_DeregisterRemovesExecutor(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithResponse(&openai.ChatCompletion{ID: "ok"})
	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	defer func() { _ = s.Shutdown(context.Background()) }()
	s.Register(newBackend("b", 1))
	require.True(t, s.HasBackend("b"))

	s.Deregister("b")
	assert.False(t, s.HasBackend("b"))

	_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	assert.ErrorIs(t, err, scheduler.ErrUnknownBackend)
}

func TestScheduler_RateLimitThrottlesDispatch(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithResponse(&openai.ChatCompletion{ID: "ok"})
	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	// 5 rps means roughly one dispatch every 200ms after the burst.
	rps := 5.0
	s.Register(&backends.Backend{
		Name: "b", Endpoint: "x", ModelID: new("openai:m"),
		MaxConcurrency: 8, RatePerSecond: &rps,
	})

	// Burst capacity ~= ceil(5) = 5. Fire 10 sequential dispatches and
	// verify the last few took noticeably longer than the first batch.
	start := time.Now()
	for range 10 {
		_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
		})
		require.NoError(t, err)
	}
	elapsed := time.Since(start)

	// 10 dispatches at 5 rps with burst 5: first 5 instant, next 5
	// spaced ~200ms apart, so total should be at least ~1s.
	assert.GreaterOrEqual(t, elapsed, 800*time.Millisecond,
		"rate limit should slow down dispatch")
}

func TestScheduler_RateLimitContextCancelInWait(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithResponse(&openai.ChatCompletion{ID: "ok"})
	s := scheduler.New(mockFactory(map[string]provider.LLMProvider{"b": mock}), quietLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })

	rps := 1.0
	s.Register(&backends.Backend{
		Name: "b", Endpoint: "x", ModelID: new("openai:m"),
		MaxConcurrency: 1, RatePerSecond: &rps,
	})

	// First dispatch eats the burst.
	_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	require.NoError(t, err)

	// Second dispatch should wait ~1s, we cancel before that. The
	// rate.Limiter.Wait error message contains "rate" or wraps a
	// context error, we just verify the call returned promptly with
	// some error (cancellation worked).
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = s.Dispatch(ctx, "b", openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
	})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 500*time.Millisecond,
		"cancel should fire well before the 1s rate-limit wait completes")
}

func TestScheduler_ReregisterReplacesExecutor(t *testing.T) {
	first := provider.NewMockProvider().WithName("b").WithResponse(&openai.ChatCompletion{ID: "first"})
	second := provider.NewMockProvider().WithName("b").WithResponse(&openai.ChatCompletion{ID: "second"})

	calls := 0
	factory := func(b *backends.Backend) provider.Backend {
		calls++
		if calls == 1 {
			return first
		}
		return second
	}
	s := scheduler.New(factory, quietLogger())
	defer func() { _ = s.Shutdown(context.Background()) }()

	s.Register(newBackend("b", 1))
	resp1, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.NoError(t, err)
	assert.Equal(t, "first", resp1.ID)

	s.Register(newBackend("b", 1)) // re-register replaces
	resp2, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.NoError(t, err)
	assert.Equal(t, "second", resp2.ID)
}
