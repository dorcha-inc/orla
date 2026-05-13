package serving

import (
	"context"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackendPersistence_SurvivesReload(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	q := 0.7
	cm := &core.CostModel{InputCostPerMToken: 0.5, OutputCostPerMToken: 1.5}
	concurrency := 4

	// First instance: register a backend.
	m1 := NewLLMBackendManager(nil, store.DB())
	require.NoError(t, m1.AddLLMBackend("cheap", &core.LLMBackend{
		Endpoint:       "http://localhost:11434/v1",
		APIKeyEnvVar:   "K",
		MaxConcurrency: &concurrency,
		CostModel:      cm,
		Quality:        &q,
	}, "openai:qwen3:0.6b"))

	// Fresh instance: rehydrate from disk.
	m2 := NewLLMBackendManager(nil, store.DB())
	require.NoError(t, m2.LoadPersisted(ctx))

	names := m2.ListLLMBackends()
	require.Equal(t, []string{"cheap"}, names)
	assert.Equal(t, "openai:qwen3:0.6b", m2.GetModelID("cheap"))
	gotCM := m2.GetCostModel("cheap")
	require.NotNil(t, gotCM)
	assert.Equal(t, 0.5, gotCM.InputCostPerMToken)
	assert.Equal(t, 1.5, gotCM.OutputCostPerMToken)
}

func TestBackendPersistence_UpdatePropagates(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	m1 := NewLLMBackendManager(nil, store.DB())
	require.NoError(t, m1.AddLLMBackend("be", &core.LLMBackend{Endpoint: "http://x"}, "openai:m"))

	q := 0.9
	require.NoError(t, m1.UpdateBackend("be", BackendUpdate{Quality: &q}))

	m2 := NewLLMBackendManager(nil, store.DB())
	require.NoError(t, m2.LoadPersisted(ctx))
	gotCM := m2.GetCostModel("be")
	assert.Nil(t, gotCM, "cost model wasn't set")
}

func TestBackendPersistence_RemoveErasesFromStore(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })

	m1 := NewLLMBackendManager(nil, store.DB())
	require.NoError(t, m1.AddLLMBackend("be", &core.LLMBackend{Endpoint: "http://x"}, "openai:m"))
	require.NoError(t, m1.RemoveLLMBackend("be"))

	m2 := NewLLMBackendManager(nil, store.DB())
	require.NoError(t, m2.LoadPersisted(ctx))
	assert.Empty(t, m2.ListLLMBackends())
}

func TestBackendPersistence_NilDBIsNoOp(t *testing.T) {
	m := NewLLMBackendManager(nil, nil)
	require.NoError(t, m.LoadPersisted(context.Background()))
	require.NoError(t, m.AddLLMBackend("be", &core.LLMBackend{Endpoint: "http://x"}, "openai:m"))
	assert.Equal(t, []string{"be"}, m.ListLLMBackends())
	require.NoError(t, m.RemoveLLMBackend("be"))
	assert.Empty(t, m.ListLLMBackends())
}
