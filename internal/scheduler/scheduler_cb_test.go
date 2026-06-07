package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/provider"
)

// cbSchedThreshold matches the threshold wired in newExecutor.
const cbSchedThreshold = 5

func cbLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func cbFactory(mocks map[string]provider.LLMProvider) ProviderFactory {
	return func(b *backends.Backend) provider.Backend {
		if p, ok := mocks[b.Name]; ok {
			return p
		}
		return provider.NewMockProvider().WithName(b.Name)
	}
}

func cbBackend(name string, concurrency int32) *backends.Backend {
	return &backends.Backend{
		Name:           name,
		Endpoint:       "x",
		ModelID:        new("openai:m"),
		MaxConcurrency: concurrency,
	}
}

// rewindSchedulerCB advances the named executor's circuit-open timestamp
// into the past, simulating the open timeout elapsing without sleeping.
func rewindSchedulerCB(s *Scheduler, name string, by time.Duration) {
	s.mu.RLock()
	exec, ok := s.executors[name]
	s.mu.RUnlock()
	if ok {
		rewindOpen(exec.cb, by)
	}
}

func TestScheduler_CircuitOpensAfterBackendFailures(t *testing.T) {
	backendErr := errors.New("connection refused")
	mock := provider.NewMockProvider().WithName("b").WithError(backendErr)
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 2))

	for range cbSchedThreshold {
		_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
		require.Error(t, err)
		var cbErr *CircuitOpenError
		require.False(t, errors.As(err, &cbErr), "circuit should not be open yet")
	}

	_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.Error(t, err)
	var cbErr *CircuitOpenError
	require.True(t, errors.As(err, &cbErr))
	assert.Equal(t, "b", cbErr.Backend)
	assert.Equal(t, "open", s.CircuitState("b"))
}

func TestScheduler_CircuitOpensAfterAPIServerError(t *testing.T) {
	// Use a typed *openai.Error with a 5xx status to exercise the
	// errors.AsType branch in isBackendError, distinct from the generic
	// errors.New path tested in TestScheduler_CircuitOpensAfterBackendFailures.
	serverErr := &openai.Error{
		StatusCode: http.StatusInternalServerError,
		Request:    &http.Request{Method: http.MethodPost},
		Response:   &http.Response{StatusCode: http.StatusInternalServerError},
	}
	mock := provider.NewMockProvider().WithName("b").WithError(serverErr)
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 2))

	for range cbSchedThreshold {
		_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
		require.Error(t, err)
		var cbErr *CircuitOpenError
		require.False(t, errors.As(err, &cbErr), "circuit should not be open yet")
	}

	_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.Error(t, err)
	var cbErr *CircuitOpenError
	require.True(t, errors.As(err, &cbErr))
	assert.Equal(t, "open", s.CircuitState("b"))
}

func TestScheduler_CircuitDoesNotOpenOnClientError(t *testing.T) {
	clientErr := &openai.Error{
		StatusCode: http.StatusBadRequest,
		Request:    &http.Request{Method: http.MethodPost},
		Response:   &http.Response{StatusCode: http.StatusBadRequest},
	}
	mock := provider.NewMockProvider().WithName("b").WithError(clientErr)
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 2))

	for range cbSchedThreshold + 5 {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	}

	assert.Equal(t, "closed", s.CircuitState("b"))
}

func TestScheduler_CircuitDoesNotOpenOnContextCancel(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithError(context.Canceled)
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 2))

	for range cbSchedThreshold + 5 {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	}

	assert.Equal(t, "closed", s.CircuitState("b"))
}

func TestScheduler_FastFailWhenCircuitOpen(t *testing.T) {
	var calls atomic.Int32
	mock := provider.NewMockProvider().WithName("b").WithChatFunc(
		func(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			calls.Add(1)
			return nil, errors.New("connection refused")
		},
	)
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 2))

	for range cbSchedThreshold {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	}
	require.Equal(t, "open", s.CircuitState("b"))

	before := calls.Load()
	_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.Error(t, err)
	var cbErr *CircuitOpenError
	assert.True(t, errors.As(err, &cbErr), "expected CircuitOpenError")
	assert.Equal(t, before, calls.Load(), "provider must not be called when circuit is open")
}

func TestScheduler_HalfOpenProbeSuccess_CircuitCloses(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithError(errors.New("connection refused"))
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 1))

	for range cbSchedThreshold {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	}
	require.Equal(t, "open", s.CircuitState("b"))

	rewindSchedulerCB(s, "b", 61*time.Second)

	mock.WithResponse(&openai.ChatCompletion{ID: "ok"})

	resp, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.ID)
	assert.Equal(t, "closed", s.CircuitState("b"), "successful probe must close the circuit")
}

func TestScheduler_HalfOpenProbeFailure_CircuitReopens(t *testing.T) {
	mock := provider.NewMockProvider().WithName("b").WithError(errors.New("connection refused"))
	s := New(cbFactory(map[string]provider.LLMProvider{"b": mock}), cbLogger())
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.Register(cbBackend("b", 1))

	for range cbSchedThreshold {
		_, _ = s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	}
	require.Equal(t, "open", s.CircuitState("b"))

	rewindSchedulerCB(s, "b", 61*time.Second)

	_, err := s.Dispatch(context.Background(), "b", openai.ChatCompletionNewParams{})
	require.Error(t, err)
	assert.Equal(t, "open", s.CircuitState("b"), "failed probe must reopen the circuit")
}
