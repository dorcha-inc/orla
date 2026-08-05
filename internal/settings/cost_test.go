package settings_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/settings"
)

// runCostContract exercises the surface both CostStore implementations
// must share.
func runCostContract(t *testing.T, store settings.CostStore) {
	t.Helper()
	ctx := context.Background()

	// A fresh store polls at the default cadence.
	cfg, err := store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultCostRefreshInterval, cfg.Interval())

	// Set round-trips.
	require.NoError(t, store.Set(ctx, settings.CostConfig{RefreshInterval: 15 * time.Second}))
	cfg, err = store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 15*time.Second, cfg.Interval())

	// Set overwrites rather than appends.
	require.NoError(t, store.Set(ctx, settings.CostConfig{RefreshInterval: 5 * time.Minute}))
	cfg, err = store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Minute, cfg.Interval())

	// A non-positive interval stores the default rather than a value
	// the poller would spin on.
	require.NoError(t, store.Set(ctx, settings.CostConfig{RefreshInterval: 0}))
	cfg, err = store.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, settings.DefaultCostRefreshInterval, cfg.Interval())
}

func TestPostgresCostStore_Contract(t *testing.T) {
	runCostContract(t, settings.NewPostgresCostStore(freshStore(t)))
}

func TestFakeCostStore_Contract(t *testing.T) {
	runCostContract(t, &settings.FakeCostStore{})
}

func TestCostConfig_Interval(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{name: "positive is kept", in: 30 * time.Second, want: 30 * time.Second},
		{name: "zero defaults", in: 0, want: settings.DefaultCostRefreshInterval},
		{name: "negative defaults", in: -time.Second, want: settings.DefaultCostRefreshInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := settings.CostConfig{RefreshInterval: tt.in}
			assert.Equal(t, tt.want, cfg.Interval())
		})
	}
}
