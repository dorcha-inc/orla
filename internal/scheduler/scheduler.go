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
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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
// backend.Kind. The serve command's factory returns provider.NewOpenAI.
// A ToolProvider is plugged in there when a tool implementation exists.
type ProviderFactory func(b *backends.Backend) provider.Backend

// ReleaseFunc returns a slot back to the executor. Always call it,
// even on error. Calling twice is a no-op.
type ReleaseFunc func()

// defaultDecisionTimeout bounds a single scheduling-policy call. LLM
// dispatches run for seconds, so a policy that takes longer than this
// is not worth waiting for and the executor falls back to fcfs.
const defaultDecisionTimeout = 50 * time.Millisecond

// policyHandle holds the scheduling policy and decision timeout shared
// by every executor. It is swapped atomically by SetPolicy so a control
// plane change applies to all backends at once. The zero policy is
// first-come-first-served.
type policyHandle struct {
	mu      sync.RWMutex
	policy  SchedulingPolicy
	timeout time.Duration
}

func (h *policyHandle) get() (SchedulingPolicy, time.Duration) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.policy, h.timeout
}

func (h *policyHandle) set(p SchedulingPolicy, timeout time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.policy = p
	if timeout > 0 {
		h.timeout = timeout
	}
}

// Scheduler owns the per-backend executors.
type Scheduler struct {
	mu        sync.RWMutex
	executors map[string]*executor
	factory   ProviderFactory
	logger    *slog.Logger

	policies      *policyHandle
	policyMetrics PolicyMetrics
}

// Option configures a Scheduler at construction.
type Option func(*Scheduler)

// WithPolicy installs the initial scheduling policy. Without it, or with
// a later SetPolicy(nil, ...), the scheduler serves first-come-first-served.
func WithPolicy(p SchedulingPolicy) Option {
	return func(s *Scheduler) { s.policies.policy = p }
}

// WithPolicyMetrics records policy decision outcomes and latency.
func WithPolicyMetrics(m PolicyMetrics) Option {
	return func(s *Scheduler) { s.policyMetrics = m }
}

// WithDecisionTimeout overrides the initial per-decision timeout.
// Non-positive values are ignored.
func WithDecisionTimeout(d time.Duration) Option {
	return func(s *Scheduler) {
		if d > 0 {
			s.policies.timeout = d
		}
	}
}

// New returns a Scheduler with no backends registered. factory may be
// nil, provider.NewOpenAI is used by default (treating every backend
// as KindLLM). serve.go installs a factory that branches by Kind.
func New(factory ProviderFactory, logger *slog.Logger, opts ...Option) *Scheduler {
	if factory == nil {
		factory = func(b *backends.Backend) provider.Backend {
			return provider.NewOpenAI(b)
		}
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Scheduler{
		executors: make(map[string]*executor),
		factory:   factory,
		logger:    logger,
		policies:  &policyHandle{timeout: defaultDecisionTimeout},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// SetPolicy swaps the scheduling policy for every backend, live. A nil
// policy reverts to first-come-first-served. A non-positive timeout
// keeps the current one. Backends registered later pick it up too.
func (s *Scheduler) SetPolicy(policy SchedulingPolicy, timeout time.Duration) {
	s.policies.set(policy, timeout)
}

func (s *Scheduler) execDeps() execDeps {
	return execDeps{
		policies: s.policies,
		metrics:  s.policyMetrics,
		logger:   s.logger,
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
	s.executors[b.Name] = newExecutor(b, s.factory(b), s.execDeps())
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
func (s *Scheduler) Acquire(ctx context.Context, name string, info RequestInfo) (provider.Backend, ReleaseFunc, error) {
	s.mu.RLock()
	exec, ok := s.executors[name]
	s.mu.RUnlock()
	if !ok {
		return nil, nil, ErrUnknownBackend
	}
	return exec.acquire(ctx, info)
}

// AcquireLLM is like Acquire but returns an LLMProvider, returning
// ErrWrongKind if the backend isn't an LLM.
func (s *Scheduler) AcquireLLM(ctx context.Context, name string, info RequestInfo) (provider.LLMProvider, ReleaseFunc, error) {
	p, release, err := s.Acquire(ctx, name, info)
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
func (s *Scheduler) AcquireTool(ctx context.Context, name string, info RequestInfo) (provider.ToolProvider, ReleaseFunc, error) {
	p, release, err := s.Acquire(ctx, name, info)
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
	p, release, err := exec.acquire(ctx, RequestInfo{})
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

// ReportOutcome records the result of a dispatch against the named backend's
// circuit breaker. A nil err is a success that resets the breaker. A backend
// error such as a 5xx, a 429, or a connection failure counts as a failure.
// Client errors and cancellations are ignored. The call is a no-op when the
// backend is not registered.
//
// Callers that acquire a provider through Acquire, AcquireLLM, or AcquireTool
// and run the request themselves must call this after the provider call.
// Otherwise the breaker only sees Dispatch outcomes and never trips on
// streaming or tool failures.
func (s *Scheduler) ReportOutcome(name string, err error) {
	s.mu.RLock()
	exec, ok := s.executors[name]
	s.mu.RUnlock()
	if !ok {
		// The backend was deregistered between Acquire and now, so its
		// breaker no longer exists and there is nothing to record.
		return
	}
	exec.recordOutcome(err)
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

// waiter is one queued request. ready is closed by the scheduling
// goroutine when the waiter is admitted, at which point a slot has
// already been reserved on its behalf. granted guards the race between
// admission and the waiter's own context cancellation and is read and
// written only under executor.qmu.
type waiter struct {
	id         string
	info       RequestInfo
	enqueuedAt time.Time
	ready      chan struct{}
	granted    bool
}

// executor is the per-backend worker pool. The provider is kind-
// agnostic, callers via AcquireLLM / AcquireTool downcast at the API
// boundary.
//
// slots is a counting semaphore. A filled buffer entry is one reserved
// worker. Selection order among queued waiters is not the arrival order
// of the runtime. A single scheduling goroutine owns admission and,
// each time a slot is free, asks the policy which waiter to admit.
type executor struct {
	backend  *backends.Backend
	provider provider.Backend
	slots    chan struct{}
	limiter  *rate.Limiter // nil when no rate limit configured
	cb       *circuitBreaker
	policies *policyHandle
	metrics  PolicyMetrics
	log      *slog.Logger

	qmu     sync.Mutex
	waiters []*waiter

	queueDepth atomic.Int64
	inflight   atomic.Int64
	dispatched atomic.Int64

	wake      chan struct{}
	closed    atomic.Bool
	closeOnce sync.Once
	closeCh   chan struct{}
	done      chan struct{}
}

// execDeps carries the scheduler-wide settings shared by every executor.
type execDeps struct {
	policies *policyHandle
	metrics  PolicyMetrics
	logger   *slog.Logger
}

func newExecutor(b *backends.Backend, p provider.Backend, deps execDeps) *executor {
	capacity := max(int(b.MaxConcurrency), 1)
	var limiter *rate.Limiter
	if b.RatePerSecond != nil && *b.RatePerSecond > 0 {
		rps := *b.RatePerSecond
		burst := max(int(math.Ceil(rps)), 1)
		limiter = rate.NewLimiter(rate.Limit(rps), burst)
	}
	e := &executor{
		backend:  b,
		provider: p,
		slots:    make(chan struct{}, capacity),
		limiter:  limiter,
		cb:       newCircuitBreaker(5, 60*time.Second),
		policies: deps.policies,
		metrics:  deps.metrics,
		log:      deps.logger,
		wake:     make(chan struct{}, 1),
		closeCh:  make(chan struct{}),
		done:     make(chan struct{}),
	}
	go e.schedule()
	return e
}

func (e *executor) acquire(ctx context.Context, info RequestInfo) (provider.Backend, ReleaseFunc, error) {
	if e.closed.Load() {
		return nil, nil, ErrUnknownBackend
	}
	if !e.cb.allow() {
		return nil, nil, &CircuitOpenError{Backend: e.backend.Name}
	}
	// Rate limit before queueing so a stalled limiter doesn't hold a
	// waiter slot. queueDepth is incremented after the limit fires so
	// the metric reflects pressure on the queue, not the limiter.
	if e.limiter != nil {
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, nil, err
		}
	}

	w := &waiter{
		id:         uuid.NewString(),
		info:       info,
		enqueuedAt: time.Now(),
		ready:      make(chan struct{}),
	}
	e.qmu.Lock()
	if e.closed.Load() {
		e.qmu.Unlock()
		return nil, nil, ErrUnknownBackend
	}
	e.waiters = append(e.waiters, w)
	e.qmu.Unlock()
	e.queueDepth.Add(1)
	e.signalWake()

	select {
	case <-w.ready:
		// Admitted. The scheduling goroutine already reserved a slot and
		// bumped inflight and dispatched on our behalf.
		return e.provider, makeRelease(e), nil

	case <-ctx.Done():
		return nil, nil, e.abandon(w, ctx.Err())

	case <-e.closeCh:
		return nil, nil, e.abandon(w, ErrUnknownBackend)
	}
}

// abandon removes a waiter that gave up before admission and returns
// cause. If the scheduling goroutine granted the waiter concurrently,
// abandon honors the grant and releases the slot so it is not leaked,
// still returning cause to the caller.
func (e *executor) abandon(w *waiter, cause error) error {
	e.qmu.Lock()
	if w.granted {
		e.qmu.Unlock()
		<-w.ready
		makeRelease(e)()
		return cause
	}
	if i := slices.Index(e.waiters, w); i >= 0 {
		e.waiters = slices.Delete(e.waiters, i, i+1)
	}
	e.qmu.Unlock()
	e.queueDepth.Add(-1)
	return cause
}

// schedule owns admission for one executor. It wakes on a new waiter,
// a released slot, or close, and admits as many waiters as there are
// free slots. It exits when closeCh is closed, joined by close via done.
func (e *executor) schedule() {
	defer close(e.done)
	for {
		select {
		case <-e.closeCh:
			return
		case <-e.wake:
			e.admitReady()
		}
	}
}

// admitReady reserves free slots one at a time and, for each, asks the
// policy which waiter to admit. It returns when no slot is free, no
// waiter is queued, or the executor is closing.
func (e *executor) admitReady() {
	for {
		if e.closed.Load() {
			return
		}
		select {
		case e.slots <- struct{}{}:
			// Reserved a slot.
		default:
			return // no free slot
		}

		e.qmu.Lock()
		if len(e.waiters) == 0 {
			e.qmu.Unlock()
			<-e.slots // nothing to admit, hand the slot back
			return
		}
		pending := e.snapshotPendingLocked()
		e.qmu.Unlock()

		id := e.pickNext(pending)

		e.qmu.Lock()
		w := e.grantLocked(id)
		e.qmu.Unlock()
		if w == nil {
			// The chosen waiter vanished between snapshot and grant.
			// Release the reserved slot and re-evaluate.
			<-e.slots
			continue
		}
		e.inflight.Add(1)
		e.dispatched.Add(1)
		e.queueDepth.Add(-1)
		close(w.ready)
	}
}

func (e *executor) snapshotPendingLocked() []PendingRequest {
	out := make([]PendingRequest, len(e.waiters))
	for i, w := range e.waiters {
		out[i] = PendingRequest{ID: w.id, Info: w.info, EnqueuedAt: w.enqueuedAt}
	}
	return out
}

// grantLocked removes the waiter with the given id, marks it granted,
// and returns it. It returns nil if no queued waiter has that id.
func (e *executor) grantLocked(id string) *waiter {
	i := slices.IndexFunc(e.waiters, func(w *waiter) bool { return w.id == id })
	if i < 0 {
		return nil
	}
	w := e.waiters[i]
	e.waiters = slices.Delete(e.waiters, i, i+1)
	w.granted = true
	return w
}

// pickNext returns the id of the waiter to admit next. With no policy
// it is the oldest waiter. With a policy it is the policy's choice,
// falling back to the oldest waiter on timeout, error, or an unknown
// id so a misbehaving service degrades to first-come-first-served.
func (e *executor) pickNext(pending []PendingRequest) string {
	if len(pending) == 0 {
		return ""
	}
	oldest := pending[0].ID
	policy, timeout := e.policies.get()
	if policy == nil {
		return oldest
	}

	// The decision serves every queued waiter, not one request, so it is
	// detached from any caller context and bounded only by the policy
	// timeout. close joins the scheduling goroutine, so an in-flight
	// decision delays shutdown by at most this timeout.
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	start := time.Now()
	id, err := policy.Next(ctx, e.backendView(), pending)
	e.observePolicy(time.Since(start).Seconds())

	outcome := "ok"
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		outcome, id = "fallback_timeout", oldest
	case err != nil:
		outcome, id = "fallback_error", oldest
	case !slices.ContainsFunc(pending, func(p PendingRequest) bool { return p.ID == id }):
		outcome, id = "fallback_invalid", oldest
	}
	if outcome != "ok" {
		e.logger().Warn("scheduler: policy decision fell back to fcfs",
			"backend", e.backend.Name, "outcome", outcome, "error", err)
	}
	e.incPolicyDecision(outcome)
	return id
}

func (e *executor) backendView() BackendView {
	return BackendView{
		Name:       e.backend.Name,
		Capacity:   cap(e.slots),
		InFlight:   e.inflight.Load(),
		QueueDepth: e.queueDepth.Load(),
	}
}

func (e *executor) incPolicyDecision(outcome string) {
	if e.metrics != nil {
		e.metrics.IncPolicyDecision(e.backend.Name, outcome)
	}
}

func (e *executor) observePolicy(seconds float64) {
	if e.metrics != nil {
		e.metrics.ObservePolicyDecision(e.backend.Name, seconds)
	}
}

func (e *executor) logger() *slog.Logger {
	if e.log != nil {
		return e.log
	}
	return slog.Default()
}

func (e *executor) signalWake() {
	select {
	case e.wake <- struct{}{}:
	default:
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
// release and then explicitly release on a happy-path branch. Releasing
// frees the reserved slot and wakes the scheduling goroutine so the
// next waiter can be admitted.
func makeRelease(e *executor) ReleaseFunc {
	var once sync.Once
	return func() {
		once.Do(func() {
			e.inflight.Add(-1)
			<-e.slots
			e.signalWake()
		})
	}
}

// close stops accepting new acquires, stops the scheduling goroutine,
// and waits for in-flight slots to release. Queued waiters unblock via
// closeCh and return ErrUnknownBackend. After close, every acquire
// returns ErrUnknownBackend.
func (e *executor) close() {
	if !e.closed.CompareAndSwap(false, true) {
		return
	}
	e.closeOnce.Do(func() { close(e.closeCh) })
	e.signalWake()
	<-e.done
	// Fill all slots to wait for in-flight to release.
	capacity := cap(e.slots)
	for range capacity {
		e.slots <- struct{}{}
	}
}
