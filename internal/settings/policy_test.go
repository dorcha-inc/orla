package settings_test

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

	"github.com/harvard-cns/orla/internal/settings"
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

// runContract exercises the surface both PolicyStore implementations
// must share.
func runContract(t *testing.T, store settings.PolicyStore) {
	t.Helper()
	ctx := context.Background()

	// A fresh store is first-come-first-served.
	cfg, err := store.Get(ctx)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled(), "a fresh store must not have a policy")

	// Set round-trips.
	want := settings.PolicyConfig{URL: "http://scheduler:8090/next", Timeout: 120 * time.Millisecond}
	require.NoError(t, store.Set(ctx, want))
	got, err := store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, want.URL, got.URL)
	assert.Equal(t, want.Timeout, got.Timeout)
	assert.True(t, got.Enabled())

	// Set overwrites rather than appends, so an empty URL disables.
	require.NoError(t, store.Set(ctx, settings.PolicyConfig{URL: "", Timeout: 50 * time.Millisecond}))
	got, err = store.Get(ctx)
	require.NoError(t, err)
	assert.False(t, got.Enabled())
}

func TestPostgresStore_Contract(t *testing.T) {
	runContract(t, settings.NewPostgresStore(freshStore(t)))
}

func TestFakePolicyStore_Contract(t *testing.T) {
	runContract(t, &settings.FakePolicyStore{})
}
