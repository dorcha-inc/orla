package stages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// ErrNotFound is returned by Get, Patch, and Delete when no stage with
// the given id exists.
var ErrNotFound = errors.New("stages: not found")

// Registry is the interface the API handlers and proxy depend on. The
// Postgres implementation is in this file, tests can use the in-memory
// FakeRegistry in fake_registry.go.
type Registry interface {
	// GetOrCreate inserts a default (empty) row if one does not exist
	// for id, and returns the (possibly existing) row. This is the
	// auto-create-on-first-sighting hook used by the proxy.
	GetOrCreate(ctx context.Context, id string) (*Stage, error)

	// Get returns the stage record, or ErrNotFound.
	Get(ctx context.Context, id string) (*Stage, error)

	// List returns all stage records ordered by id.
	List(ctx context.Context) ([]*Stage, error)

	// Replace performs a full upsert. All scalar fields are replaced
	// with the supplied values. Labels is fully overwritten with no
	// merge against the existing row.
	Replace(ctx context.Context, s *Stage) (*Stage, error)

	// Patch applies a partial update: nil pointers and nil maps in
	// PatchRequest leave the corresponding fields unchanged. Returns
	// ErrNotFound if the stage does not exist.
	Patch(ctx context.Context, id string, p PatchRequest) (*Stage, error)

	// Delete removes the stage. Returns ErrNotFound if the row did not
	// exist.
	Delete(ctx context.Context, id string) error
}

// PostgresRegistry is the Postgres-backed Registry.
type PostgresRegistry struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPostgresRegistry constructs the registry against the supplied pool.
func NewPostgresRegistry(pool *pgxpool.Pool) *PostgresRegistry {
	return &PostgresRegistry{
		pool:    pool,
		queries: db.New(pool),
	}
}

// Compile-time interface check.
var _ Registry = (*PostgresRegistry)(nil)

func (r *PostgresRegistry) GetOrCreate(ctx context.Context, id string) (*Stage, error) {
	row, err := r.queries.UpsertStageDefault(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("stages: upsert default: %w", err)
	}
	return toStage(row)
}

func (r *PostgresRegistry) Get(ctx context.Context, id string) (*Stage, error) {
	row, err := r.queries.GetStage(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stages: get: %w", err)
	}
	return toStage(row)
}

func (r *PostgresRegistry) List(ctx context.Context) ([]*Stage, error) {
	rows, err := r.queries.ListStages(ctx)
	if err != nil {
		return nil, fmt.Errorf("stages: list: %w", err)
	}
	out := make([]*Stage, 0, len(rows))
	for _, row := range rows {
		s, err := toStage(row)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func (r *PostgresRegistry) Replace(ctx context.Context, s *Stage) (*Stage, error) {
	labels := s.Labels
	if labels == nil {
		labels = map[string]any{}
	}
	labelBytes, err := json.Marshal(labels)
	if err != nil {
		return nil, fmt.Errorf("stages: marshal labels: %w", err)
	}
	row, err := r.queries.ReplaceStage(ctx, db.ReplaceStageParams{
		ID:              s.ID,
		Backend:         s.Backend,
		ReasoningEffort: s.ReasoningEffort,
		Prompt:          s.Prompt,
		CaptureIo:       s.CaptureIO,
		Labels:          labelBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("stages: replace: %w", err)
	}
	return toStage(row)
}

// Patch is read-modify-write inside a transaction so two concurrent
// PATCHes can't lose each other's writes.
func (r *PostgresRegistry) Patch(ctx context.Context, id string, p PatchRequest) (*Stage, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("stages: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op on commit

	q := r.queries.WithTx(tx)

	current, err := q.GetStage(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("stages: patch: read: %w", err)
	}

	if p.Backend != nil {
		current.Backend = *p.Backend
	}
	if p.ReasoningEffort != nil {
		current.ReasoningEffort = *p.ReasoningEffort
	}
	if p.Prompt != nil {
		current.Prompt = *p.Prompt
	}
	if p.CaptureIO != nil {
		current.CaptureIo = *p.CaptureIO
	}
	if p.Labels != nil {
		b, err := json.Marshal(p.Labels)
		if err != nil {
			return nil, fmt.Errorf("stages: patch: marshal labels: %w", err)
		}
		current.Labels = b
	}

	updated, err := q.ReplaceStage(ctx, db.ReplaceStageParams{
		ID:              current.ID,
		Backend:         current.Backend,
		ReasoningEffort: current.ReasoningEffort,
		Prompt:          current.Prompt,
		CaptureIo:       current.CaptureIo,
		Labels:          current.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("stages: patch: write: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("stages: patch: commit: %w", err)
	}

	return toStage(updated)
}

func (r *PostgresRegistry) Delete(ctx context.Context, id string) error {
	n, err := r.queries.DeleteStage(ctx, id)
	if err != nil {
		return fmt.Errorf("stages: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// toStage converts the sqlc-generated row into the public Stage type.
// Labels are unmarshaled into map[string]any so callers don't have to
// deal with raw JSON bytes.
func toStage(row db.Stage) (*Stage, error) {
	var labels map[string]any
	if len(row.Labels) == 0 {
		labels = map[string]any{}
	} else if err := json.Unmarshal(row.Labels, &labels); err != nil {
		return nil, fmt.Errorf("stages: unmarshal labels for %q: %w", row.ID, err)
	}
	return &Stage{
		ID:              row.ID,
		Backend:         row.Backend,
		ReasoningEffort: row.ReasoningEffort,
		Prompt:          row.Prompt,
		CaptureIO:       row.CaptureIo,
		Labels:          labels,
		CreatedAt:       row.CreatedAt.Time,
		UpdatedAt:       row.UpdatedAt.Time,
	}, nil
}
