// Package scheduler owns one FCFS executor per registered backend.
//
// Requests arrive at a backend's executor and dispatch immediately if
// a worker slot is free. If not, they queue until one is released.
// The concurrency cap is enforced per backend per instance with a
// buffered channel sized to backend.max_concurrency. Fairness across
// tenants or workflows is not implemented.
//
// Streaming and non-streaming requests both consume one slot. The
// caller holds the slot until the response or stream is fully
// delivered. See Acquire.
package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openai/openai-go"
	"golang.org/x/time/rate"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/provider"
)

// ErrUnknownBackend is returned when Dispatch or Acquire is called
// with a backend name not currently registered.
var ErrUnknownBackend = errors.New("scheduler: unknown backend")

// ProviderFactory constructs a provider for a backend and returns
// the kind-agnostic Backend interface. The caller, or the scheduler's
// typed accessors, downcasts to LLMProvider or ToolProvider based on
// backend.Kind. The serve command's factory branches by Kind to
// return provider.NewOpenAI for KindLLM, or structurepred.New for
// KindTool with ToolKind "structure-prediction".
type ProviderFactory func(b *backends.Backend) provider.Backend

// ReleaseFunc returns a slot back to the executor. Always call it,
// even on error. Calling twice is a no-op.
type ReleaseFunc func()

// Scheduler owns the per-backend executors.
type Scheduler struct {
	mu        sync.RWMutex
	executors map[string]*executor
	factory   ProviderFactory
	logger    *slog.Logger
}

// New returns a Scheduler with no backends registered. factory may be
// nil, provider.NewOpenAI is used by default (treating every backend
// as KindLLM). serve.go installs a factory that branches by Kind.
func New(factory ProviderFactory, logger *slog.Logger) *Scheduler {
	if factory == nil {
		factory = func(b *backends.Backend) provider.Backend {
			return provider.NewOpenAI(b)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Scheduler{
		executors: make(map[string]*executor),
		factory:   factory,
		logger:    logger,
	}
}

// Register installs an executor for b. If a backend with the same name
// is already registered, the prior executor is torn down first.
func (s *Scheduler) Register(b *backends.Backend) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.executors[b.Name]; ok {
		prev.close()
	}
	s.executors[b.Name] = newExecutor(b, s.factory(b))
}

// Deregister removes a backend's executor and waits for in-flight
// dispatches against it to drain.
func (s *Scheduler) Deregister(name string) {
	s.mu.Lock()
	exec, ok := s.executors[name]
	delete(s.executors, name)
	s.mu.Unlock()
	if ok {
		exec.close()
	}
}

// HasBackend reports whether a backend is currently registered.
func (s *Scheduler) HasBackend(name string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.executors[name]
	return ok
}

// ErrWrongKind is returned when AcquireLLM / AcquireTool is called
// on a backend of the other kind.
var ErrWrongKind = errors.New("scheduler: backend kind mismatch")

// Acquire blocks until a worker slot frees on the named backend's
// executor, then returns its provider (kind-agnostic Backend interface)
// and a ReleaseFunc. Callers downcast to LLMProvider or ToolProvider
// based on the backend's kind. The caller MUST call ReleaseFunc after
// the request completes (including after a streaming response is fully
// drained).
//
// If ctx expires while waiting in the queue, ctx.Err() is returned and
// no slot is held.
func (s *Scheduler) Acquire(ctx context.Context, name string) (provider.Backend, ReleaseFunc, error) {
	s.mu.RLock()
	exec, ok := s.executors[name]
	s.mu.RUnlock()
	if !ok {
		return nil, nil, ErrUnknownBackend
	}
	return exec.acquire(ctx)
}

// AcquireLLM is like Acquire but returns an LLMProvider, returning
// ErrWrongKind if the backend isn't an LLM.
func (s *Scheduler) AcquireLLM(ctx context.Context, name string) (provider.LLMProvider, ReleaseFunc, error) {
	p, release, err := s.Acquire(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	llm, ok := p.(provider.LLMProvider)
	if !ok {
		release()
		return nil, nil, ErrWrongKind
	}
	return llm, release, nil
}

// AcquireTool is like Acquire but returns a ToolProvider, returning
// ErrWrongKind if the backend isn't a tool.
func (s *Scheduler) AcquireTool(ctx context.Context, name string) (provider.ToolProvider, ReleaseFunc, error) {
	p, release, err := s.Acquire(ctx, name)
	if err != nil {
		return nil, nil, err
	}
	tool, ok := p.(provider.ToolProvider)
	if !ok {
		release()
		return nil, nil, ErrWrongKind
	}
	return tool, release, nil
}

// Dispatch is sugar over AcquireLLM + Chat + Release for non-streaming
// LLM requests. Tool dispatches go through AcquireTool + Invoke
// directly from the proxy handler.
func (s *Scheduler) Dispatch(ctx context.Context, name string, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	s.mu.RLock()
	exec, ok := s.executors[name]
	s.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownBackend
	}
	p, release, err := exec.acquire(ctx)
	if err != nil {
		return nil, err
	}
	llm, ok := p.(provider.LLMProvider)
	if !ok {
		release()
		return nil, ErrWrongKind
	}
	defer release()
	resp, chatErr := llm.Chat(ctx, params)
	exec.recordOutcome(chatErr)
	return resp, chatErr
}


// CircuitState returns "closed", "open", or "half-open" for the named
// backend. Returns "closed" if the backend is not registered.
func (s *Scheduler) CircuitState(name string) string {
	s.mu.RLock()
	exec, ok := s.executors[name]
	s.mu.RUnlock()
	if !ok {
		return "closed"
	}
	return circuitStateOf(exec)
}

// circuitStateOf reads the circuit breaker state of an executor without
// acquiring the scheduler lock. The caller must ensure exec is not nil.
func circuitStateOf(e *executor) string {
	e.cb.mu.Lock()
	defer e.cb.mu.Unlock()
	switch e.cb.state {
	case cbOpen:
		return "open"
	case cbHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Shutdown closes every executor. ctx bounds how long Shutdown is
// willing to wait for in-flight dispatches to drain.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	execs := make([]*executor, 0, len(s.executors))
	for _, e := range s.executors {
		execs = append(execs, e)
	}
	s.executors = make(map[string]*executor)
	s.mu.Unlock()

	done := make(chan struct{})
	go func() {
		for _, e := range execs {
			e.close()
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("scheduler shutdown: %w", ctx.Err())
	}
}

// Stats is a point-in-time view of an executor's queue, in-flight
// counters, and circuit breaker state. Used for /metrics and test inspection.
type Stats struct {
	Backend      string
	QueueDepth   int64
	InFlight     int64
	Capacity     int
	Dispatched   int64
	CircuitState string
}

// BackendOf returns the backend record the scheduler was registered
// with for name, or (nil, false) if no executor exists. The returned
// pointer is shared with the executor and must not be mutated. It is
// stable until the next Register or Deregister call for this name.
func (s *Scheduler) BackendOf(name string) (*backends.Backend, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	exec, ok := s.executors[name]
	if !ok {
		return nil, false
	}
	return exec.backend, true
}

// Stats returns a snapshot for every registered backend.
func (s *Scheduler) Stats() []Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Stats, 0, len(s.executors))
	for name, e := range s.executors {
		out = append(out, Stats{
			Backend:      name,
			QueueDepth:   e.queueDepth.Load(),
			InFlight:     e.inflight.Load(),
			Capacity:     cap(e.slots),
			Dispatched:   e.dispatched.Load(),
			CircuitState: circuitStateOf(e),
		})
	}
	return out
}

// executor is the per-backend worker pool. The provider is kind-
// agnostic, callers via AcquireLLM / AcquireTool downcast at the API
// boundary.
type executor struct {
	backend  *backends.Backend
	provider provider.Backend
	slots    chan struct{}
	limiter  *rate.Limiter // nil when no rate limit configured
	cb       *circuitBreaker

	queueDepth atomic.Int64
	inflight   atomic.Int64
	dispatched atomic.Int64

	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{}
}

func newExecutor(b *backends.Backend, p provider.Backend) *executor {
	capacity := max(int(b.MaxConcurrency), 1)
	var limiter *rate.Limiter
	if b.RatePerSecond != nil && *b.RatePerSecond > 0 {
		rps := *b.RatePerSecond
		burst := max(int(math.Ceil(rps)), 1)
		limiter = rate.NewLimiter(rate.Limit(rps), burst)
	}
	return &executor{
		backend:  b,
		provider: p,
		slots:    make(chan struct{}, capacity),
		limiter:  limiter,
		cb:       newCircuitBreaker(5, 60*time.Second),
		closeCh:  make(chan struct{}),
	}
}

func (e *executor) acquire(ctx context.Context) (provider.Backend, ReleaseFunc, error) {
	if e.closed.Load() {
		return nil, nil, ErrUnknownBackend
	}
	// Check if circuitBreaker allows new requests, else return error
	if !e.cb.allow() {
		return nil, nil, &CircuitOpenError{Backend: e.backend.Name}
	}
	// Rate limit before taking a slot so a stalled limiter doesn't
	// hold a worker. queueDepth is incremented after the limit fires so
	// the metric reflects pressure on slots, not the limiter.
	if e.limiter != nil {
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, nil, err
		}
	}
	e.queueDepth.Add(1)

	select {
	case e.slots <- struct{}{}:
		e.queueDepth.Add(-1)
		// Race: close was signaled after we won the slot. Hand it back
		// and fail so callers don't dispatch into a tearing-down
		// executor.
		if e.closed.Load() {
			<-e.slots
			return nil, nil, ErrUnknownBackend
		}
		e.inflight.Add(1)
		e.dispatched.Add(1)
		return e.provider, makeRelease(e), nil

	case <-e.closeCh:
		e.queueDepth.Add(-1)
		return nil, nil, ErrUnknownBackend

	case <-ctx.Done():
		e.queueDepth.Add(-1)
		return nil, nil, ctx.Err()
	}
}

// isBackendError reports whether err counts as a backend failure for
// circuit-breaker purposes. 5xx, 429, and connection-level errors count.
// Client errors (other 4xx) and context cancellations do not.
func isBackendError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if apiErr, ok := errors.AsType[*openai.Error](err); ok {
		return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
	}
	return true
}

func (e *executor) recordOutcome(err error) {
	if err == nil {
		e.cb.recordSuccess()
		return
	}
	if isBackendError(err) {
		e.cb.recordFailure()
	}
}

// makeRelease returns a ReleaseFunc that's idempotent, calling twice
// is harmless. This matters because some streaming code paths defer
// release and then explicitly release on a happy-path branch.
func makeRelease(e *executor) ReleaseFunc {
	var once sync.Once
	return func() {
		once.Do(func() {
			e.inflight.Add(-1)
			<-e.slots
		})
	}
}

// close stops accepting new acquires and waits for in-flight slots to
// release. After close, every acquire returns ErrUnknownBackend.
func (e *executor) close() {
	if !e.closed.CompareAndSwap(false, true) {
		return
	}
	e.closeOnce.Do(func() { close(e.closeCh) })
	// Fill all slots to wait for in-flight to release.
	capacity := cap(e.slots)
	for range capacity {
		e.slots <- struct{}{}
	}
}
