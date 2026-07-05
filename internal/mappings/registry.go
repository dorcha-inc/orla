package mappings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// ErrNotFound is returned by Get and Delete when no variant with the
// given name exists.
var ErrNotFound = errors.New("mappings: not found")

// Registry is the interface the proxy and API handlers depend on. The
// Postgres implementation is in this file, tests use FakeRegistry.
type Registry interface {
	// Resolve returns the backend override for one (variant, stageID)
	// and whether one exists. A missing variant or a stage absent from
	// the variant both report false, and the caller falls back to the
	// stage's live backend.
	Resolve(ctx context.Context, variant, stageID string) (string, bool)

	// Put creates or replaces a variant with the supplied overrides.
	// The stored variant is the overrides exactly, prior overrides for
	// the name are removed.
	Put(ctx context.Context, name string, overrides map[string]string) (*Variant, error)

	// Get returns the variant, or ErrNotFound.
	Get(ctx context.Context, name string) (*Variant, error)

	// List returns all variants ordered by name.
	List(ctx context.Context) ([]*Variant, error)

	// Delete removes the variant. Returns ErrNotFound if it did not
	// exist.
	Delete(ctx context.Context, name string) error
}

// PostgresRegistry is the Postgres-backed Registry.
type PostgresRegistry struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPostgresRegistry constructs the registry against the supplied pool.
func NewPostgresRegistry(pool *pgxpool.Pool) *PostgresRegistry {
	return &PostgresRegistry{pool: pool, queries: db.New(pool)}
}

// Compile-time interface check.
var _ Registry = (*PostgresRegistry)(nil)

func (r *PostgresRegistry) Resolve(ctx context.Context, variant, stageID string) (string, bool) {
	if variant == "" {
		return "", false
	}
	backend, err := r.queries.GetMappingOverride(ctx, db.GetMappingOverrideParams{
		Name:    variant,
		StageID: stageID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			// A real lookup failure, not a missing override. Fall back to
			// the live mapping so the request still serves, but log so the
			// silent fallback is observable.
			slog.Default().Warn("mappings: resolve failed, falling back to live mapping",
				"variant", variant,
				"stage", stageID,
				"error", err,
			)
		}
		return "", false
	}
	return backend, true
}

// Put replaces the variant atomically: delete the old overrides, then
// insert the new set, so a concurrent reader never sees a half-written
// variant.
func (r *PostgresRegistry) Put(ctx context.Context, name string, overrides map[string]string) (*Variant, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("mappings: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after commit

	q := r.queries.WithTx(tx)
	if _, err := q.DeleteVariant(ctx, name); err != nil {
		return nil, fmt.Errorf("mappings: put: clear: %w", err)
	}
	for stageID, backend := range overrides {
		if err := q.UpsertMappingOverride(ctx, db.UpsertMappingOverrideParams{
			Name:    name,
			StageID: stageID,
			Backend: backend,
		}); err != nil {
			return nil, fmt.Errorf("mappings: put: write %q: %w", stageID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("mappings: put: commit: %w", err)
	}
	return r.Get(ctx, name)
}

func (r *PostgresRegistry) Get(ctx context.Context, name string) (*Variant, error) {
	rows, err := r.queries.ListVariantOverrides(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("mappings: get: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	return rowsToVariant(name, rows), nil
}

func (r *PostgresRegistry) List(ctx context.Context) ([]*Variant, error) {
	rows, err := r.queries.ListAllVariantOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("mappings: list: %w", err)
	}
	byName := make(map[string][]db.MappingVariant)
	order := make([]string, 0)
	for _, row := range rows {
		if _, seen := byName[row.Name]; !seen {
			order = append(order, row.Name)
		}
		byName[row.Name] = append(byName[row.Name], row)
	}
	out := make([]*Variant, 0, len(order))
	for _, name := range order {
		out = append(out, rowsToVariant(name, byName[name]))
	}
	return out, nil
}

func (r *PostgresRegistry) Delete(ctx context.Context, name string) error {
	n, err := r.queries.DeleteVariant(ctx, name)
	if err != nil {
		return fmt.Errorf("mappings: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// rowsToVariant folds override rows for one name into a Variant. It
// takes the earliest CreatedAt and the latest UpdatedAt across the
// rows so the variant carries a coherent lifespan.
func rowsToVariant(name string, rows []db.MappingVariant) *Variant {
	v := &Variant{Name: name, Overrides: make(map[string]string, len(rows))}
	for i, row := range rows {
		v.Overrides[row.StageID] = row.Backend
		if i == 0 || row.CreatedAt.Time.Before(v.CreatedAt) {
			v.CreatedAt = row.CreatedAt.Time
		}
		if row.UpdatedAt.Time.After(v.UpdatedAt) {
			v.UpdatedAt = row.UpdatedAt.Time
		}
	}
	return v
}
