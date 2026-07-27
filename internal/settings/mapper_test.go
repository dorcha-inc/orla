package settings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/settings"
)

// runMappingContract exercises the surface both MapperStore
// implementations must share.
func runMappingContract(t *testing.T, store settings.MapperStore) {
	t.Helper()
	ctx := context.Background()

	// A fresh store routes statically.
	cfg, err := store.Get(ctx)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled(), "a fresh store must not have a policy")

	// Set round-trips.
	want := settings.MapperConfig{URL: "http://mapper:8091/decide", Timeout: 80 * time.Millisecond}
	require.NoError(t, store.Set(ctx, want))
	cfg, err = store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, want.URL, cfg.URL)
	assert.Equal(t, want.Timeout, cfg.Timeout)
	assert.True(t, cfg.Enabled())

	// Set overwrites rather than appends, so an empty URL disables.
	require.NoError(t, store.Set(ctx, settings.MapperConfig{URL: "", Timeout: 50 * time.Millisecond}))
	cfg, err = store.Get(ctx)
	require.NoError(t, err)
	assert.False(t, cfg.Enabled())
}

func TestPostgresMapperStore_Contract(t *testing.T) {
	runMappingContract(t, settings.NewPostgresMapperStore(freshStore(t)))
}

func TestFakeMapperStore_Contract(t *testing.T) {
	runMappingContract(t, &settings.FakeMapperStore{})
}
