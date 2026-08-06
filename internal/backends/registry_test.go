package backends_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/storage"
)

func freshPool(t *testing.T) *pgxpool.Pool {
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

func TestPostgresRegistry_InsertGet(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	ctx := context.Background()

	want := &backends.Backend{
		Name:                "gpt4o",
		Endpoint:            "https://api.openai.com/v1",
		ModelID:             new("openai:gpt-4o"),
		APIKeyEnvVar:        "OPENAI_API_KEY",
		MaxConcurrency:      8,
		InputCostPerMtoken:  new(2.5),
		OutputCostPerMtoken: new(10.0),
		Quality:             new(0.85),
	}
	got, err := reg.Insert(ctx, want)
	require.NoError(t, err)
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.MaxConcurrency, got.MaxConcurrency)
	require.NotNil(t, got.Quality)
	assert.InDelta(t, 0.85, *got.Quality, 1e-9)
	assert.False(t, got.CreatedAt.IsZero())

	gotAgain, err := reg.Get(ctx, "gpt4o")
	require.NoError(t, err)
	assert.Equal(t, want.ModelID, gotAgain.ModelID)
}

func TestPostgresRegistry_InsertDuplicateReturnsAlreadyExists(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	ctx := context.Background()

	b := &backends.Backend{
		Name: "gpt4o", Endpoint: "x", ModelID: new("openai:gpt-4o"), MaxConcurrency: 1,
	}
	_, err := reg.Insert(ctx, b)
	require.NoError(t, err)
	_, err = reg.Insert(ctx, b)
	assert.ErrorIs(t, err, backends.ErrAlreadyExists)
}

func TestPostgresRegistry_GetMissing(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	_, err := reg.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, backends.ErrNotFound)
}

func TestPostgresRegistry_Patch_OnlyChangesProvidedFields(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	ctx := context.Background()

	_, err := reg.Insert(ctx, &backends.Backend{
		Name: "gpt4o", Endpoint: "https://api.openai.com/v1",
		ModelID: new("openai:gpt-4o"), MaxConcurrency: 4,
		InputCostPerMtoken: new(2.5),
		Quality:            new(0.85),
	})
	require.NoError(t, err)

	got, err := reg.Patch(ctx, "gpt4o", backends.PatchRequest{
		MaxConcurrency: new(int32(16)),
		Quality:        new(0.90),
	})
	require.NoError(t, err)
	assert.Equal(t, int32(16), got.MaxConcurrency)
	require.NotNil(t, got.Quality)
	assert.InDelta(t, 0.90, *got.Quality, 1e-9)
	require.NotNil(t, got.InputCostPerMtoken, "untouched")
	assert.InDelta(t, 2.5, *got.InputCostPerMtoken, 1e-9)
	require.NotNil(t, got.ModelID)
	assert.Equal(t, "openai:gpt-4o", *got.ModelID, "model_id remains immutable through PATCH")
}

func TestPostgresRegistry_Patch_NotFound(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	_, err := reg.Patch(context.Background(), "missing", backends.PatchRequest{})
	assert.ErrorIs(t, err, backends.ErrNotFound)
}

func TestPostgresRegistry_ListOrderedByName(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	ctx := context.Background()
	for _, n := range []string{"zeta", "alpha", "mu"} {
		_, err := reg.Insert(ctx, &backends.Backend{
			Name: n, Endpoint: "x", ModelID: new("openai:gpt-4o"), MaxConcurrency: 1,
		})
		require.NoError(t, err)
	}
	got, err := reg.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"alpha", "mu", "zeta"},
		[]string{got[0].Name, got[1].Name, got[2].Name})
}

func TestPostgresRegistry_Delete(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	ctx := context.Background()
	_, err := reg.Insert(ctx, &backends.Backend{
		Name: "x", Endpoint: "y", ModelID: new("openai:gpt-4o"), MaxConcurrency: 1,
	})
	require.NoError(t, err)
	require.NoError(t, reg.Delete(ctx, "x"))
	assert.True(t, errors.Is(reg.Delete(ctx, "x"), backends.ErrNotFound))
}

func TestPostgresRegistry_CheckConstraintRejectsZeroConcurrency(t *testing.T) {
	reg := backends.NewPostgresRegistry(freshPool(t))
	_, err := reg.Insert(context.Background(), &backends.Backend{
		Name: "x", Endpoint: "y", ModelID: new("openai:gpt-4o"), MaxConcurrency: 0,
	})
	require.Error(t, err, "DB CHECK should reject max_concurrency=0")
}

func TestFakeRegistry_ContractMatches(t *testing.T) {
	ctx := context.Background()
	reg := backends.NewFakeRegistry()

	_, err := reg.Get(ctx, "missing")
	assert.ErrorIs(t, err, backends.ErrNotFound)

	b := &backends.Backend{
		Name: "x", Endpoint: "y", ModelID: new("openai:gpt-4o"), MaxConcurrency: 4,
	}
	_, err = reg.Insert(ctx, b)
	require.NoError(t, err)
	_, err = reg.Insert(ctx, b)
	assert.ErrorIs(t, err, backends.ErrAlreadyExists)

	_, err = reg.Patch(ctx, "missing", backends.PatchRequest{})
	assert.ErrorIs(t, err, backends.ErrNotFound)
}

// costSourceRoundTrip is the shared contract for the cost_source
// field: insert stores it, patch overwrites it, and patching the
// empty string clears it back to nil.
func costSourceRoundTrip(t *testing.T, reg backends.Registry) {
	t.Helper()
	ctx := context.Background()

	_, err := reg.Insert(ctx, &backends.Backend{
		Name: "priced", Endpoint: "x", ModelID: new("openai:gpt-4o"),
		MaxConcurrency: 1,
		CostSource:     new("http://localhost:9090/price"),
	})
	require.NoError(t, err)

	got, err := reg.Get(ctx, "priced")
	require.NoError(t, err)
	require.NotNil(t, got.CostSource)
	assert.Equal(t, "http://localhost:9090/price", *got.CostSource)

	got, err = reg.Patch(ctx, "priced", backends.PatchRequest{
		CostSource: new("http://localhost:9191/price"),
	})
	require.NoError(t, err)
	require.NotNil(t, got.CostSource)
	assert.Equal(t, "http://localhost:9191/price", *got.CostSource)

	got, err = reg.Patch(ctx, "priced", backends.PatchRequest{CostSource: new("")})
	require.NoError(t, err)
	assert.Nil(t, got.CostSource, "patching the empty string clears cost_source")

	got, err = reg.Patch(ctx, "priced", backends.PatchRequest{Quality: new(0.5)})
	require.NoError(t, err)
	assert.Nil(t, got.CostSource, "an unrelated patch leaves a cleared cost_source nil")
}

func TestPostgresRegistry_CostSourceRoundTrip(t *testing.T) {
	costSourceRoundTrip(t, backends.NewPostgresRegistry(freshPool(t)))
}

func TestFakeRegistry_CostSourceRoundTrip(t *testing.T) {
	costSourceRoundTrip(t, backends.NewFakeRegistry())
}

// cacheReadCostRoundTrip is the shared contract for the
// cache_read_cost_per_mtoken field: insert stores it, patch overwrites
// it, and an unrelated patch leaves it alone.
func cacheReadCostRoundTrip(t *testing.T, reg backends.Registry) {
	t.Helper()
	ctx := context.Background()

	_, err := reg.Insert(ctx, &backends.Backend{
		Name: "cached", Endpoint: "x", ModelID: new("openai:gpt-4o"),
		MaxConcurrency:         1,
		InputCostPerMtoken:     new(3.0),
		CacheReadCostPerMtoken: new(0.30),
	})
	require.NoError(t, err)

	got, err := reg.Get(ctx, "cached")
	require.NoError(t, err)
	require.NotNil(t, got.CacheReadCostPerMtoken)
	assert.InDelta(t, 0.30, *got.CacheReadCostPerMtoken, 1e-9)

	got, err = reg.Patch(ctx, "cached", backends.PatchRequest{
		CacheReadCostPerMtoken: new(0.10),
	})
	require.NoError(t, err)
	require.NotNil(t, got.CacheReadCostPerMtoken)
	assert.InDelta(t, 0.10, *got.CacheReadCostPerMtoken, 1e-9)

	got, err = reg.Patch(ctx, "cached", backends.PatchRequest{Quality: new(0.5)})
	require.NoError(t, err)
	require.NotNil(t, got.CacheReadCostPerMtoken)
	assert.InDelta(t, 0.10, *got.CacheReadCostPerMtoken, 1e-9,
		"an unrelated patch leaves cache_read_cost_per_mtoken unchanged")
}

func TestPostgresRegistry_CacheReadCostRoundTrip(t *testing.T) {
	cacheReadCostRoundTrip(t, backends.NewPostgresRegistry(freshPool(t)))
}

func TestFakeRegistry_CacheReadCostRoundTrip(t *testing.T) {
	cacheReadCostRoundTrip(t, backends.NewFakeRegistry())
}
