package settings

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// MapperConfig is the active stage mapper. An empty URL means every
// stage routes by its static mapping. Timeout bounds a single routing
// decision.
type MapperConfig struct {
	URL     string
	Timeout time.Duration
}

// Enabled reports whether an external stage mapper is configured.
func (c MapperConfig) Enabled() bool {
	return c.URL != ""
}

// MapperStore reads and writes the singleton stage mapper. The
// Postgres implementation is in this file, tests use FakeMapperStore.
type MapperStore interface {
	// Get returns the current stage mapper. A fresh database returns
	// the zero-URL default, which is static routing.
	Get(ctx context.Context) (MapperConfig, error)

	// Set replaces the stored stage mapper.
	Set(ctx context.Context, cfg MapperConfig) error
}

// PostgresMapperStore is the Postgres-backed MapperStore.
type PostgresMapperStore struct {
	queries *db.Queries
}

// NewPostgresMapperStore constructs the store against the supplied pool.
func NewPostgresMapperStore(pool *pgxpool.Pool) *PostgresMapperStore {
	return &PostgresMapperStore{queries: db.New(pool)}
}

// Compile-time interface check.
var _ MapperStore = (*PostgresMapperStore)(nil)

func (s *PostgresMapperStore) Get(ctx context.Context) (MapperConfig, error) {
	row, err := s.queries.GetStageMapper(ctx)
	if err != nil {
		return MapperConfig{}, err
	}
	return MapperConfig{
		URL:     row.Url,
		Timeout: time.Duration(row.TimeoutMs) * time.Millisecond,
	}, nil
}

func (s *PostgresMapperStore) Set(ctx context.Context, cfg MapperConfig) error {
	return s.queries.UpsertStageMapper(ctx, db.UpsertStageMapperParams{
		Url:       cfg.URL,
		TimeoutMs: int32(cfg.Timeout.Milliseconds()),
	})
}
