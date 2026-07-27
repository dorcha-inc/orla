package backends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// ErrNotFound is returned when no backend exists for the given name.
var ErrNotFound = errors.New("backends: not found")

// ErrAlreadyExists is returned when Insert is called with a name that
// is already registered.
var ErrAlreadyExists = errors.New("backends: already exists")

// Registry is the interface used by the API handlers and (later) the
// proxy. The Postgres implementation is in this file, tests use the
// in-memory FakeRegistry.
type Registry interface {
	// Insert creates a backend. Returns ErrAlreadyExists if a backend
	// with the same name is already registered.
	Insert(ctx context.Context, b *Backend) (*Backend, error)

	// Get returns the backend, or ErrNotFound.
	Get(ctx context.Context, name string) (*Backend, error)

	// List returns all backends, ordered by name.
	List(ctx context.Context) ([]*Backend, error)

	// Patch applies a partial update. Returns ErrNotFound if the
	// backend does not exist.
	Patch(ctx context.Context, name string, p PatchRequest) (*Backend, error)

	// Delete removes the backend. Returns ErrNotFound if it did not
	// exist.
	Delete(ctx context.Context, name string) error
}

// PostgresRegistry is the Postgres-backed Registry.
type PostgresRegistry struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewPostgresRegistry constructs the registry against the supplied
// connection pool.
func NewPostgresRegistry(pool *pgxpool.Pool) *PostgresRegistry {
	return &PostgresRegistry{
		pool:    pool,
		queries: db.New(pool),
	}
}

// Compile-time interface check.
var _ Registry = (*PostgresRegistry)(nil)

func (r *PostgresRegistry) Insert(ctx context.Context, b *Backend) (*Backend, error) {
	// Default Kind to LLM, older callers and existing demos leave it unset.
	kind := b.Kind
	if kind == "" {
		kind = KindLLM
	}
	ratesBytes, err := marshalRates(b.Rates)
	if err != nil {
		return nil, fmt.Errorf("backends: insert: encode rates: %w", err)
	}
	row, err := r.queries.InsertBackend(ctx, db.InsertBackendParams{
		Name:                b.Name,
		Endpoint:            b.Endpoint,
		ModelID:             b.ModelID,
		ApiKeyEnvVar:        b.APIKeyEnvVar,
		MaxConcurrency:      b.MaxConcurrency,
		InputCostPerMtoken:  b.InputCostPerMtoken,
		OutputCostPerMtoken: b.OutputCostPerMtoken,
		Quality:             b.Quality,
		RatePerSecond:       b.RatePerSecond,
		Kind:                string(kind),
		ToolKind:            b.ToolKind,
		Rates:               ratesBytes,
		CostSource:          b.CostSource,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("backends: insert: %w", err)
	}
	return toBackend(row)
}

func (r *PostgresRegistry) Get(ctx context.Context, name string) (*Backend, error) {
	row, err := r.queries.GetBackend(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("backends: get: %w", err)
	}
	return toBackend(row)
}

func (r *PostgresRegistry) List(ctx context.Context) ([]*Backend, error) {
	rows, err := r.queries.ListBackends(ctx)
	if err != nil {
		return nil, fmt.Errorf("backends: list: %w", err)
	}
	out := make([]*Backend, 0, len(rows))
	for _, row := range rows {
		b, err := toBackend(row)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// Patch is read-modify-write inside a transaction so concurrent updates
// don't lose each other's writes.
func (r *PostgresRegistry) Patch(ctx context.Context, name string, p PatchRequest) (*Backend, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("backends: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.queries.WithTx(tx)

	current, err := q.GetBackend(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("backends: patch: read: %w", err)
	}

	if p.Endpoint != nil {
		current.Endpoint = *p.Endpoint
	}
	if p.APIKeyEnvVar != nil {
		current.ApiKeyEnvVar = *p.APIKeyEnvVar
	}
	if p.MaxConcurrency != nil {
		current.MaxConcurrency = *p.MaxConcurrency
	}
	if p.InputCostPerMtoken != nil {
		current.InputCostPerMtoken = p.InputCostPerMtoken
	}
	if p.OutputCostPerMtoken != nil {
		current.OutputCostPerMtoken = p.OutputCostPerMtoken
	}
	if p.Quality != nil {
		current.Quality = p.Quality
	}
	if p.RatePerSecond != nil {
		current.RatePerSecond = p.RatePerSecond
	}
	if p.Rates != nil {
		// *p.Rates may be nil or empty (caller wants to clear) or
		// populated (caller wants to overwrite). Both produce valid
		// JSONB via marshalRates.
		b, err := marshalRates(*p.Rates)
		if err != nil {
			return nil, fmt.Errorf("backends: patch: encode rates: %w", err)
		}
		current.Rates = b
	}
	if p.CostSource != nil {
		if *p.CostSource == "" {
			current.CostSource = nil
		} else {
			current.CostSource = p.CostSource
		}
	}

	updated, err := q.UpdateBackend(ctx, db.UpdateBackendParams{
		Name:                current.Name,
		Endpoint:            current.Endpoint,
		ModelID:             current.ModelID,
		ApiKeyEnvVar:        current.ApiKeyEnvVar,
		MaxConcurrency:      current.MaxConcurrency,
		InputCostPerMtoken:  current.InputCostPerMtoken,
		OutputCostPerMtoken: current.OutputCostPerMtoken,
		Quality:             current.Quality,
		RatePerSecond:       current.RatePerSecond,
		Kind:                current.Kind,
		ToolKind:            current.ToolKind,
		Rates:               current.Rates,
		CostSource:          current.CostSource,
	})
	if err != nil {
		return nil, fmt.Errorf("backends: patch: write: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("backends: patch: commit: %w", err)
	}

	return toBackend(updated)
}

func (r *PostgresRegistry) Delete(ctx context.Context, name string) error {
	n, err := r.queries.DeleteBackend(ctx, name)
	if err != nil {
		return fmt.Errorf("backends: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation tests for the Postgres unique-constraint SQLSTATE.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func toBackend(row db.Backend) (*Backend, error) {
	rates, err := unmarshalRates(row.Rates)
	if err != nil {
		return nil, fmt.Errorf("backends: decode rates for %q: %w", row.Name, err)
	}
	return &Backend{
		Name:                row.Name,
		Endpoint:            row.Endpoint,
		ModelID:             row.ModelID,
		APIKeyEnvVar:        row.ApiKeyEnvVar,
		MaxConcurrency:      row.MaxConcurrency,
		InputCostPerMtoken:  row.InputCostPerMtoken,
		OutputCostPerMtoken: row.OutputCostPerMtoken,
		Quality:             row.Quality,
		RatePerSecond:       row.RatePerSecond,
		Kind:                Kind(row.Kind),
		ToolKind:            row.ToolKind,
		Rates:               rates,
		CostSource:          row.CostSource,
		CreatedAt:           row.CreatedAt.Time,
		UpdatedAt:           row.UpdatedAt.Time,
	}, nil
}

// marshalRates encodes a rates map to the JSONB bytes the database
// column wants. Nil and empty maps both round-trip as "{}".
func marshalRates(m map[string]float64) ([]byte, error) {
	if len(m) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(m)
}

// unmarshalRates decodes a rates JSONB column into a map. Empty bytes
// or "{}" returns (nil, nil) so callers can range over a nil map
// without a nil-check. Malformed bytes return an error so callers can
// distinguish "no rates configured" from "rates corrupted on disk".
func unmarshalRates(b []byte) (map[string]float64, error) {
	if len(b) == 0 || string(b) == "{}" {
		return nil, nil
	}
	var m map[string]float64
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("malformed rates JSONB: %w", err)
	}
	return m, nil
}
