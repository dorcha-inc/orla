// Package settings stores daemon-wide operational settings that the
// control plane manages at runtime, as opposed to the bootstrap
// configuration in package config that is fixed at process start. The
// scheduling policy is the first such setting.
package settings

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// PolicyConfig is the active scheduling policy. An empty URL means
// first-come-first-served. Timeout bounds a single scheduling decision.
type PolicyConfig struct {
	URL     string
	Timeout time.Duration
}

// Enabled reports whether an external scheduling policy is configured.
func (c PolicyConfig) Enabled() bool {
	return c.URL != ""
}

// PolicyStore reads and writes the singleton scheduling policy. The
// Postgres implementation is in this file, tests use FakePolicyStore.
type PolicyStore interface {
	// Get returns the current policy. A fresh database returns the
	// zero-URL default, which is first-come-first-served.
	Get(ctx context.Context) (PolicyConfig, error)

	// Set replaces the stored policy.
	Set(ctx context.Context, cfg PolicyConfig) error
}

// PostgresStore is the Postgres-backed PolicyStore.
type PostgresStore struct {
	queries *db.Queries
}

// NewPostgresStore constructs the store against the supplied pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{queries: db.New(pool)}
}

// Compile-time interface check.
var _ PolicyStore = (*PostgresStore)(nil)

func (s *PostgresStore) Get(ctx context.Context) (PolicyConfig, error) {
	row, err := s.queries.GetSchedulerPolicy(ctx)
	if err != nil {
		return PolicyConfig{}, err
	}
	return PolicyConfig{
		URL:     row.Url,
		Timeout: time.Duration(row.TimeoutMs) * time.Millisecond,
	}, nil
}

func (s *PostgresStore) Set(ctx context.Context, cfg PolicyConfig) error {
	return s.queries.UpsertSchedulerPolicy(ctx, db.UpsertSchedulerPolicyParams{
		Url:       cfg.URL,
		TimeoutMs: int32(cfg.Timeout.Milliseconds()),
	})
}
