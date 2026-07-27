package settings

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/harvard-cns/orla/internal/storage/db"
)

// DefaultCostRefreshInterval is the polling cadence a store returns
// when nothing has configured one.
const DefaultCostRefreshInterval = time.Minute

// CostConfig governs how the daemon refreshes prices from backend cost
// sources.
type CostConfig struct {
	// RefreshInterval is the time between polling rounds. A
	// non-positive value means DefaultCostRefreshInterval.
	RefreshInterval time.Duration
}

// Interval returns the configured cadence, substituting the default
// for a non-positive value.
func (c CostConfig) Interval() time.Duration {
	if c.RefreshInterval <= 0 {
		return DefaultCostRefreshInterval
	}
	return c.RefreshInterval
}

// CostStore reads and writes the singleton cost policy. The Postgres
// implementation is in this file, tests use FakeCostStore.
type CostStore interface {
	// Get returns the current cost policy. A fresh database returns
	// DefaultCostRefreshInterval.
	Get(ctx context.Context) (CostConfig, error)

	// Set replaces the stored cost policy.
	Set(ctx context.Context, cfg CostConfig) error
}

// PostgresCostStore is the Postgres-backed CostStore.
type PostgresCostStore struct {
	queries *db.Queries
}

// NewPostgresCostStore constructs the store against the supplied pool.
func NewPostgresCostStore(pool *pgxpool.Pool) *PostgresCostStore {
	return &PostgresCostStore{queries: db.New(pool)}
}

// Compile-time interface check.
var _ CostStore = (*PostgresCostStore)(nil)

func (s *PostgresCostStore) Get(ctx context.Context) (CostConfig, error) {
	row, err := s.queries.GetCostPolicy(ctx)
	if err != nil {
		return CostConfig{}, err
	}
	return CostConfig{
		RefreshInterval: time.Duration(row.RefreshIntervalMs) * time.Millisecond,
	}, nil
}

func (s *PostgresCostStore) Set(ctx context.Context, cfg CostConfig) error {
	return s.queries.UpsertCostPolicy(ctx, int32(cfg.Interval().Milliseconds()))
}
