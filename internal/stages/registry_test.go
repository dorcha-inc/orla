package stages_test

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

	"github.com/harvard-cns/orla/internal/stages"
	"github.com/harvard-cns/orla/internal/storage"
)

// freshStore brings up a Postgres container, runs migrations, and
// returns a pool. Each test gets its own container so concurrent runs
// don't share state.
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
	t.Cleanup(func() {
		_ = pgC.Terminate(context.Background())
	})

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := storage.Open(ctx, storage.OpenConfig{
		DatabaseURL: dsn,
	})
	require.NoError(t, err)
	t.Cleanup(store.Close)

	return store.Pool()
}

func TestPostgresRegistry_GetOrCreate_AutoCreatesDefault(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	ctx := context.Background()

	s, err := reg.GetOrCreate(ctx, "planning")
	require.NoError(t, err)
	assert.Equal(t, "planning", s.ID)
	assert.Empty(t, s.Backend)
	assert.Empty(t, s.ReasoningEffort)
	assert.Empty(t, s.Prompt)
	assert.Equal(t, map[string]any{}, s.Labels)
	assert.False(t, s.CreatedAt.IsZero())
}

func TestPostgresRegistry_GetOrCreate_ReturnsExistingRow(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	ctx := context.Background()

	_, err := reg.Replace(ctx, &stages.Stage{
		ID:      "planning",
		Backend: "gpt-4o",
	})
	require.NoError(t, err)

	got, err := reg.GetOrCreate(ctx, "planning")
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", got.Backend, "existing row must not be overwritten")
}

func TestPostgresRegistry_Get_NotFound(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	_, err := reg.Get(context.Background(), "missing")
	require.ErrorIs(t, err, stages.ErrNotFound)
}

func TestPostgresRegistry_ReplaceThenGet(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	ctx := context.Background()

	want := &stages.Stage{
		ID:              "planning",
		Backend:         "gpt-4o",
		ReasoningEffort: "high",
		Prompt:          "You are a careful planner.",
		Labels:          map[string]any{"owner": "core", "epsilon": 0.1},
	}
	_, err := reg.Replace(ctx, want)
	require.NoError(t, err)

	got, err := reg.Get(ctx, "planning")
	require.NoError(t, err)
	assert.Equal(t, want.Backend, got.Backend)
	assert.Equal(t, want.ReasoningEffort, got.ReasoningEffort)
	assert.Equal(t, want.Prompt, got.Prompt)
	assert.Equal(t, "core", got.Labels["owner"])
	// JSON numbers come back as float64.
	assert.InDelta(t, 0.1, got.Labels["epsilon"], 0.0001)
}

func TestPostgresRegistry_Patch_PartialUpdate(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	ctx := context.Background()

	_, err := reg.Replace(ctx, &stages.Stage{
		ID:              "planning",
		Backend:         "gpt-4o",
		ReasoningEffort: "low",
		Prompt:          "seed prompt",
		Labels:          map[string]any{"k": "v"},
	})
	require.NoError(t, err)

	backend := "gpt-4o-mini"
	got, err := reg.Patch(ctx, "planning", stages.PatchRequest{
		Backend: &backend,
	})
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", got.Backend)
	assert.Equal(t, "low", got.ReasoningEffort, "untouched field preserved")
	assert.Equal(t, "seed prompt", got.Prompt, "untouched prompt preserved")
	assert.Equal(t, "v", got.Labels["k"], "untouched labels preserved")

	prompt := "patched prompt"
	got, err = reg.Patch(ctx, "planning", stages.PatchRequest{Prompt: &prompt})
	require.NoError(t, err)
	assert.Equal(t, "patched prompt", got.Prompt)
	assert.Equal(t, "gpt-4o-mini", got.Backend, "untouched backend preserved")
}

func TestPostgresRegistry_Patch_NotFound(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	_, err := reg.Patch(context.Background(), "missing", stages.PatchRequest{})
	require.ErrorIs(t, err, stages.ErrNotFound)
}

func TestPostgresRegistry_List_OrderedByID(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	ctx := context.Background()

	for _, id := range []string{"zeta", "alpha", "mu"} {
		_, err := reg.Replace(ctx, &stages.Stage{ID: id})
		require.NoError(t, err)
	}

	got, err := reg.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "alpha", got[0].ID)
	assert.Equal(t, "mu", got[1].ID)
	assert.Equal(t, "zeta", got[2].ID)
}

func TestPostgresRegistry_Delete(t *testing.T) {
	pool := freshStore(t)
	reg := stages.NewPostgresRegistry(pool)
	ctx := context.Background()

	_, err := reg.Replace(ctx, &stages.Stage{ID: "to-delete"})
	require.NoError(t, err)

	require.NoError(t, reg.Delete(ctx, "to-delete"))
	require.True(t, errors.Is(reg.Delete(ctx, "to-delete"), stages.ErrNotFound),
		"second delete returns NotFound")
}

func TestFakeRegistry_BehavesLikePostgresAtTheBoundaries(t *testing.T) {
	// Sanity check that the FakeRegistry exhibits the same surface
	// behavior the API handlers depend on. This is a contract test, not
	// an exhaustive sweep.
	ctx := context.Background()
	reg := stages.NewFakeRegistry()

	_, err := reg.Get(ctx, "missing")
	assert.ErrorIs(t, err, stages.ErrNotFound)

	s, err := reg.GetOrCreate(ctx, "auto")
	require.NoError(t, err)
	assert.Equal(t, "auto", s.ID)

	_, err = reg.Patch(ctx, "missing", stages.PatchRequest{})
	assert.ErrorIs(t, err, stages.ErrNotFound)

	prompt := "fake prompt"
	patched, err := reg.Patch(ctx, "auto", stages.PatchRequest{Prompt: &prompt})
	require.NoError(t, err)
	assert.Equal(t, "fake prompt", patched.Prompt)

	require.NoError(t, reg.Delete(ctx, "auto"))
	assert.ErrorIs(t, reg.Delete(ctx, "auto"), stages.ErrNotFound)
}
