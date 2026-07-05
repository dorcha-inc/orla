package mappings_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/harvard-cns/orla/internal/mappings"
	"github.com/harvard-cns/orla/internal/storage"
)

func freshStore(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping storage-backed test in -short mode")
	}
	ctx := context.Background()

	pgC, err := postgres.Run(ctx,
		"postgres:17",
		postgres.WithDatabase("orla"),
		postgres.WithUsername("orla"),
		postgres.WithPassword("orla"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := storage.Open(ctx, storage.OpenConfig{DatabaseURL: dsn})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	return store.Pool()
}

// runContract exercises the surface both implementations must share.
func runContract(t *testing.T, reg mappings.Registry) {
	t.Helper()
	ctx := context.Background()

	_, err := reg.Get(ctx, "missing")
	assert.ErrorIs(t, err, mappings.ErrNotFound)

	_, ok := reg.Resolve(ctx, "missing", "planning")
	assert.False(t, ok, "resolve on absent variant reports not found")

	_, ok = reg.Resolve(ctx, "", "planning")
	assert.False(t, ok, "empty variant is the live mapping, never resolves")

	v, err := reg.Put(ctx, "cand", map[string]string{"queryBuilder": "gpt-4.1"})
	require.NoError(t, err)
	assert.Equal(t, "cand", v.Name)
	assert.Equal(t, map[string]string{"queryBuilder": "gpt-4.1"}, v.Overrides)

	backend, ok := reg.Resolve(ctx, "cand", "queryBuilder")
	assert.True(t, ok)
	assert.Equal(t, "gpt-4.1", backend)

	_, ok = reg.Resolve(ctx, "cand", "orchestratorAgent")
	assert.False(t, ok, "a stage absent from the variant falls through to live")

	// Put replaces rather than merges.
	_, err = reg.Put(ctx, "cand", map[string]string{"orchestratorAgent": "gpt-4.1-mini"})
	require.NoError(t, err)
	_, ok = reg.Resolve(ctx, "cand", "queryBuilder")
	assert.False(t, ok, "prior overrides are cleared on replace")
	backend, ok = reg.Resolve(ctx, "cand", "orchestratorAgent")
	assert.True(t, ok)
	assert.Equal(t, "gpt-4.1-mini", backend)

	got, err := reg.Get(ctx, "cand")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"orchestratorAgent": "gpt-4.1-mini"}, got.Overrides)

	require.NoError(t, reg.Delete(ctx, "cand"))
	assert.ErrorIs(t, reg.Delete(ctx, "cand"), mappings.ErrNotFound)
	_, err = reg.Get(ctx, "cand")
	assert.ErrorIs(t, err, mappings.ErrNotFound)
}

func TestPostgresRegistry_Contract(t *testing.T) {
	pool := freshStore(t)
	runContract(t, mappings.NewPostgresRegistry(pool))
}

func TestFakeRegistry_Contract(t *testing.T) {
	runContract(t, mappings.NewFakeRegistry())
}

func TestPostgresRegistry_ListOrdersByName(t *testing.T) {
	pool := freshStore(t)
	reg := mappings.NewPostgresRegistry(pool)
	ctx := context.Background()

	_, err := reg.Put(ctx, "b-cand", map[string]string{"queryBuilder": "gpt-4.1"})
	require.NoError(t, err)
	_, err = reg.Put(ctx, "a-cand", map[string]string{"engineRanker": "gpt-4.1-mini"})
	require.NoError(t, err)

	got, err := reg.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a-cand", got[0].Name)
	assert.Equal(t, "b-cand", got[1].Name)
}
