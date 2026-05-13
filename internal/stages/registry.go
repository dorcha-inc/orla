package stages

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/harvard-cns/orla/internal/core"
)

// ErrNotFound is returned when a stage lookup misses.
var ErrNotFound = errors.New("stage not found")

// Registry stores stage records in SQLite with an in-memory read cache.
// Reads served from cache; writes go through to disk synchronously then
// invalidate.
type Registry struct {
	db *sql.DB

	mu    sync.RWMutex
	cache map[string]*Stage
}

// NewRegistry returns a Registry backed by db. The caller is responsible for
// having run migrations beforehand.
func NewRegistry(db *sql.DB) *Registry {
	return &Registry{db: db, cache: make(map[string]*Stage)}
}

// Get returns the stage with id, or ErrNotFound if absent.
func (r *Registry) Get(ctx context.Context, id string) (*Stage, error) {
	r.mu.RLock()
	if s, ok := r.cache[id]; ok {
		r.mu.RUnlock()
		return s.clone(), nil
	}
	r.mu.RUnlock()

	s, err := r.loadFromDB(ctx, id)
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.cache[id] = s
	r.mu.Unlock()
	return s.clone(), nil
}

// GetOrCreate returns the stage with id, creating a default record on miss.
// The default has empty fields and the current timestamps; the platform
// engineer is expected to update it via Upsert/Patch later.
func (r *Registry) GetOrCreate(ctx context.Context, id string) (*Stage, error) {
	s, err := r.Get(ctx, id)
	if err == nil {
		return s, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	def := &Stage{ID: id, Labels: map[string]string{}}
	if err := r.Upsert(ctx, def); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// Upsert inserts or replaces s. CreatedAt is preserved on update; UpdatedAt is
// set to now.
func (r *Registry) Upsert(ctx context.Context, s *Stage) error {
	if s == nil || s.ID == "" {
		return fmt.Errorf("stage id is required")
	}
	now := time.Now()
	labels := s.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	labelsJSON, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}

	// CreatedAt preservation: if the row already exists, retain its created_at.
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO stages (
			id, backend, scheduling_policy, request_scheduling_policy,
			priority, request_priority, max_concurrency,
			reasoning_effort, flush_on_complete, reasoning_hint,
			labels_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			backend=excluded.backend,
			scheduling_policy=excluded.scheduling_policy,
			request_scheduling_policy=excluded.request_scheduling_policy,
			priority=excluded.priority,
			request_priority=excluded.request_priority,
			max_concurrency=excluded.max_concurrency,
			reasoning_effort=excluded.reasoning_effort,
			flush_on_complete=excluded.flush_on_complete,
			reasoning_hint=excluded.reasoning_hint,
			labels_json=excluded.labels_json,
			updated_at=excluded.updated_at;
	`,
		s.ID, s.Backend, s.SchedulingPolicy, s.RequestSchedulingPolicy,
		nullableInt(s.Priority), nullableInt(s.RequestPriority), nullableInt(s.MaxConcurrency),
		s.ReasoningEffort, boolToInt(s.FlushOnComplete), s.ReasoningHint,
		string(labelsJSON), now.Unix(), now.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert stage %q: %w", s.ID, err)
	}

	r.mu.Lock()
	delete(r.cache, s.ID)
	r.mu.Unlock()
	return nil
}

// Patch updates only the fields present in the provided struct. Pointer
// fields with non-nil values are written; nil pointers leave the stored
// value unchanged. The empty string in a non-pointer field also means
// "unchanged"; use Upsert to explicitly clear those.
type Patch struct {
	Backend                 *string
	SchedulingPolicy        *string
	RequestSchedulingPolicy *string
	Priority                *int
	RequestPriority         *int
	MaxConcurrency          *int
	ReasoningEffort         *string
	FlushOnComplete         *bool
	ReasoningHint           *string
	Labels                  *map[string]string
}

// Patch applies p to the existing stage with id. Returns the updated stage,
// or ErrNotFound if id doesn't exist.
func (r *Registry) Patch(ctx context.Context, id string, p Patch) (*Stage, error) {
	s, err := r.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if p.Backend != nil {
		s.Backend = *p.Backend
	}
	if p.SchedulingPolicy != nil {
		s.SchedulingPolicy = *p.SchedulingPolicy
	}
	if p.RequestSchedulingPolicy != nil {
		s.RequestSchedulingPolicy = *p.RequestSchedulingPolicy
	}
	if p.Priority != nil {
		v := *p.Priority
		s.Priority = &v
	}
	if p.RequestPriority != nil {
		v := *p.RequestPriority
		s.RequestPriority = &v
	}
	if p.MaxConcurrency != nil {
		v := *p.MaxConcurrency
		s.MaxConcurrency = &v
	}
	if p.ReasoningEffort != nil {
		s.ReasoningEffort = *p.ReasoningEffort
	}
	if p.FlushOnComplete != nil {
		s.FlushOnComplete = *p.FlushOnComplete
	}
	if p.ReasoningHint != nil {
		s.ReasoningHint = *p.ReasoningHint
	}
	if p.Labels != nil {
		s.Labels = *p.Labels
	}
	if err := r.Upsert(ctx, s); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

// List returns all known stages ordered by ID.
func (r *Registry) List(ctx context.Context) ([]*Stage, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+stageColumns+` FROM stages ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list stages: %w", err)
	}
	defer core.LogDeferredError(rows.Close)
	out := []*Stage{}
	for rows.Next() {
		s, err := scanStage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Delete removes the stage with id. No error if absent.
func (r *Registry) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM stages WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete stage %q: %w", id, err)
	}
	r.mu.Lock()
	delete(r.cache, id)
	r.mu.Unlock()
	return nil
}

const stageColumns = `id, backend, scheduling_policy, request_scheduling_policy,
	priority, request_priority, max_concurrency,
	reasoning_effort, flush_on_complete, reasoning_hint,
	labels_json, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanStage(row rowScanner) (*Stage, error) {
	s := &Stage{}
	var priority, requestPriority, maxConcurrency sql.NullInt64
	var flushOnComplete int
	var labelsJSON string
	var createdAt, updatedAt int64
	if err := row.Scan(
		&s.ID, &s.Backend, &s.SchedulingPolicy, &s.RequestSchedulingPolicy,
		&priority, &requestPriority, &maxConcurrency,
		&s.ReasoningEffort, &flushOnComplete, &s.ReasoningHint,
		&labelsJSON, &createdAt, &updatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan stage: %w", err)
	}
	if priority.Valid {
		v := int(priority.Int64)
		s.Priority = &v
	}
	if requestPriority.Valid {
		v := int(requestPriority.Int64)
		s.RequestPriority = &v
	}
	if maxConcurrency.Valid {
		v := int(maxConcurrency.Int64)
		s.MaxConcurrency = &v
	}
	s.FlushOnComplete = flushOnComplete != 0
	if labelsJSON != "" {
		if err := json.Unmarshal([]byte(labelsJSON), &s.Labels); err != nil {
			return nil, fmt.Errorf("unmarshal labels for stage %q: %w", s.ID, err)
		}
	}
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	s.CreatedAt = time.Unix(createdAt, 0).UTC()
	s.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return s, nil
}

func (r *Registry) loadFromDB(ctx context.Context, id string) (*Stage, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+stageColumns+` FROM stages WHERE id = ?`, id)
	s, err := scanStage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
