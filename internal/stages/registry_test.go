package stages

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry(t *testing.T) *Registry {
	t.Helper()
	store, err := storage.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })
	return NewRegistry(store.DB())
}

func TestRegistry_GetMissingReturnsNotFound(t *testing.T) {
	r := newTestRegistry(t)
	_, err := r.Get(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRegistry_UpsertThenGet(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	priority := 7
	require.NoError(t, r.Upsert(ctx, &Stage{
		ID:              "planning",
		Backend:         "gpt4o",
		Priority:        &priority,
		FlushOnComplete: true,
		Labels:          map[string]string{"owner": "platform"},
	}))

	got, err := r.Get(ctx, "planning")
	require.NoError(t, err)
	assert.Equal(t, "planning", got.ID)
	assert.Equal(t, "gpt4o", got.Backend)
	require.NotNil(t, got.Priority)
	assert.Equal(t, 7, *got.Priority)
	assert.True(t, got.FlushOnComplete)
	assert.Equal(t, "platform", got.Labels["owner"])
	assert.False(t, got.CreatedAt.IsZero())
	assert.False(t, got.UpdatedAt.IsZero())
}

func TestRegistry_GetOrCreateAutoCreatesDefault(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()

	s, err := r.GetOrCreate(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, "fresh", s.ID)
	assert.Empty(t, s.Backend)
	assert.False(t, s.FlushOnComplete)
	assert.Empty(t, s.Labels)

	// Second call returns the existing record.
	again, err := r.GetOrCreate(ctx, "fresh")
	require.NoError(t, err)
	assert.Equal(t, s.CreatedAt.Unix(), again.CreatedAt.Unix())
}

func TestRegistry_PatchOnlyUpdatesPresentFields(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	priority := 5
	require.NoError(t, r.Upsert(ctx, &Stage{
		ID:              "summarize",
		Backend:         "old-backend",
		Priority:        &priority,
		ReasoningEffort: "high",
		Labels:          map[string]string{"team": "alpha"},
	}))

	newBackend := "new-backend"
	updated, err := r.Patch(ctx, "summarize", Patch{Backend: &newBackend})
	require.NoError(t, err)
	assert.Equal(t, "new-backend", updated.Backend)
	// Untouched fields preserved.
	require.NotNil(t, updated.Priority)
	assert.Equal(t, 5, *updated.Priority)
	assert.Equal(t, "high", updated.ReasoningEffort)
	assert.Equal(t, "alpha", updated.Labels["team"])
}

func TestRegistry_PatchMissingReturnsNotFound(t *testing.T) {
	r := newTestRegistry(t)
	backend := "x"
	_, err := r.Patch(context.Background(), "ghost", Patch{Backend: &backend})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRegistry_Delete(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "tmp"}))

	require.NoError(t, r.Delete(ctx, "tmp"))
	_, err := r.Get(ctx, "tmp")
	assert.ErrorIs(t, err, ErrNotFound)

	// Deleting again is a no-op.
	assert.NoError(t, r.Delete(ctx, "tmp"))
}

func TestRegistry_List(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "b"}))
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "a"}))
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "c"}))

	got, err := r.List(ctx)
	require.NoError(t, err)
	ids := make([]string, len(got))
	for i, s := range got {
		ids[i] = s.ID
	}
	assert.Equal(t, []string{"a", "b", "c"}, ids)
}

func TestRegistry_UpsertOverwrites(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "x", Backend: "v1"}))
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "x", Backend: "v2"}))

	got, err := r.Get(ctx, "x")
	require.NoError(t, err)
	assert.Equal(t, "v2", got.Backend)
}

func TestRegistry_ConcurrentReadsSafe(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "x", Backend: "v1"}))

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := r.Get(ctx, "x")
			require.NoError(t, err)
			require.NotNil(t, s)
		}()
	}
	wg.Wait()
}

func TestRegistry_UpsertWithEmptyIDErrors(t *testing.T) {
	r := newTestRegistry(t)
	err := r.Upsert(context.Background(), &Stage{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stage id is required")
}

func TestRegistry_UpsertNilStageErrors(t *testing.T) {
	r := newTestRegistry(t)
	err := r.Upsert(context.Background(), nil)
	require.Error(t, err)
}

func TestRegistry_PatchClearLabels(t *testing.T) {
	r := newTestRegistry(t)
	ctx := context.Background()
	require.NoError(t, r.Upsert(ctx, &Stage{ID: "x", Labels: map[string]string{"a": "1"}}))

	cleared := map[string]string{}
	updated, err := r.Patch(ctx, "x", Patch{Labels: &cleared})
	require.NoError(t, err)
	assert.Empty(t, updated.Labels)
}

func TestErrNotFoundIsSentinel(t *testing.T) {
	wrapped := errors.Join(errors.New("outer"), ErrNotFound)
	assert.ErrorIs(t, wrapped, ErrNotFound)
}
