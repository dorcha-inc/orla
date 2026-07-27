package costs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/settings"
)

func TestStore_GetSetRetain(t *testing.T) {
	s := NewStore()

	_, ok := s.Get("a")
	assert.False(t, ok)

	s.Set("a", Price{InputPerMtoken: 1, OutputPerMtoken: 2})
	s.Set("b", Price{InputPerMtoken: 3, OutputPerMtoken: 4})

	p, ok := s.Get("a")
	require.True(t, ok)
	assert.Equal(t, Price{InputPerMtoken: 1, OutputPerMtoken: 2}, p)

	s.Retain(map[string]bool{"b": true})
	_, ok = s.Get("a")
	assert.False(t, ok)
	_, ok = s.Get("b")
	assert.True(t, ok)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func priceServer(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fastPolicy returns a cost policy that polls quickly enough for a
// test to observe several rounds.
func fastPolicy(t *testing.T) *settings.FakeCostStore {
	t.Helper()
	s := &settings.FakeCostStore{}
	require.NoError(t, s.Set(context.Background(),
		settings.CostConfig{RefreshInterval: 10 * time.Millisecond}))
	return s
}

func sourcedBackend(name, url string) *backends.Backend {
	model := "openai:gpt-test"
	return &backends.Backend{
		Name:       name,
		Endpoint:   "http://unused",
		Kind:       backends.KindLLM,
		ModelID:    &model,
		CostSource: &url,
	}
}

func TestPoller_FetchesAndStoresPrice(t *testing.T) {
	srv := priceServer(t, `{"input_cost_per_mtoken": 0.5, "output_cost_per_mtoken": 1.5}`, http.StatusOK)

	reg := backends.NewFakeRegistry()
	_, err := reg.Insert(context.Background(), sourcedBackend("b1", srv.URL))
	require.NoError(t, err)

	store := NewStore()
	p := NewPoller(PollerConfig{Registry: reg, Store: store, Policy: &settings.FakeCostStore{}, Logger: testLogger()})
	p.poll()

	price, ok := store.Get("b1")
	require.True(t, ok)
	assert.Equal(t, Price{InputPerMtoken: 0.5, OutputPerMtoken: 1.5}, price)
}

func TestPoller_KeepsLastPriceOnFetchFailure(t *testing.T) {
	var status atomic.Int32
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		code := int(status.Load())
		w.WriteHeader(code)
		if code == http.StatusOK {
			_, _ = w.Write([]byte(`{"input_cost_per_mtoken": 2, "output_cost_per_mtoken": 4}`))
		}
	}))
	t.Cleanup(srv.Close)

	reg := backends.NewFakeRegistry()
	_, err := reg.Insert(context.Background(), sourcedBackend("b1", srv.URL))
	require.NoError(t, err)

	store := NewStore()
	p := NewPoller(PollerConfig{Registry: reg, Store: store, Policy: &settings.FakeCostStore{}, Logger: testLogger()})
	p.poll()
	_, ok := store.Get("b1")
	require.True(t, ok)

	status.Store(http.StatusInternalServerError)
	p.poll()
	price, ok := store.Get("b1")
	require.True(t, ok, "a failed fetch must keep the last known price")
	assert.Equal(t, Price{InputPerMtoken: 2, OutputPerMtoken: 4}, price)
}

func TestPoller_RejectsInvalidBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `not json`},
		{name: "missing output cost", body: `{"input_cost_per_mtoken": 1}`},
		{name: "missing input cost", body: `{"output_cost_per_mtoken": 1}`},
		{name: "nan", body: `{"input_cost_per_mtoken": "NaN", "output_cost_per_mtoken": 1}`},
		{name: "negative", body: `{"input_cost_per_mtoken": -1, "output_cost_per_mtoken": 1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := priceServer(t, tt.body, http.StatusOK)
			reg := backends.NewFakeRegistry()
			_, err := reg.Insert(context.Background(), sourcedBackend("b1", srv.URL))
			require.NoError(t, err)

			store := NewStore()
			p := NewPoller(PollerConfig{Registry: reg, Store: store, Policy: &settings.FakeCostStore{}, Logger: testLogger()})
			p.poll()

			_, ok := store.Get("b1")
			assert.False(t, ok, "an invalid body must not install a price")
		})
	}
}

func TestPoller_DropsPriceWhenSourceRemoved(t *testing.T) {
	srv := priceServer(t, `{"input_cost_per_mtoken": 1, "output_cost_per_mtoken": 1}`, http.StatusOK)

	reg := backends.NewFakeRegistry()
	_, err := reg.Insert(context.Background(), sourcedBackend("b1", srv.URL))
	require.NoError(t, err)

	store := NewStore()
	p := NewPoller(PollerConfig{Registry: reg, Store: store, Policy: &settings.FakeCostStore{}, Logger: testLogger()})
	p.poll()
	_, ok := store.Get("b1")
	require.True(t, ok)

	clear := ""
	_, err = reg.Patch(context.Background(), "b1", backends.PatchRequest{CostSource: &clear})
	require.NoError(t, err)

	p.poll()
	_, ok = store.Get("b1")
	assert.False(t, ok, "clearing cost_source must return the backend to static costs")
}

func TestPoller_SkipsToolBackends(t *testing.T) {
	srv := priceServer(t, `{"input_cost_per_mtoken": 1, "output_cost_per_mtoken": 1}`, http.StatusOK)

	toolKind := "structure-prediction"
	b := sourcedBackend("t1", srv.URL)
	b.Kind = backends.KindTool
	b.ModelID = nil
	b.ToolKind = &toolKind

	reg := backends.NewFakeRegistry()
	_, err := reg.Insert(context.Background(), b)
	require.NoError(t, err)

	store := NewStore()
	p := NewPoller(PollerConfig{Registry: reg, Store: store, Policy: &settings.FakeCostStore{}, Logger: testLogger()})
	p.poll()

	_, ok := store.Get("t1")
	assert.False(t, ok)
}

func TestPoller_StartAndCloseJoinTheGoroutine(t *testing.T) {
	srv := priceServer(t, `{"input_cost_per_mtoken": 1, "output_cost_per_mtoken": 1}`, http.StatusOK)

	reg := backends.NewFakeRegistry()
	_, err := reg.Insert(context.Background(), sourcedBackend("b1", srv.URL))
	require.NoError(t, err)

	store := NewStore()
	p := NewPoller(PollerConfig{Registry: reg, Store: store, Policy: fastPolicy(t), Logger: testLogger()})
	p.Start()

	require.Eventually(t, func() bool {
		_, ok := store.Get("b1")
		return ok
	}, 2*time.Second, 5*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.NoError(t, p.Close(ctx))
	assert.NoError(t, p.Close(ctx), "a second Close must be a no-op")
}

func TestPoller_DefaultsToPolicyIntervalWhenUnset(t *testing.T) {
	p := NewPoller(PollerConfig{
		Registry: backends.NewFakeRegistry(),
		Store:    NewStore(),
		Policy:   &settings.FakeCostStore{},
		Logger:   testLogger(),
	})
	assert.Equal(t, settings.DefaultCostRefreshInterval, p.refreshInterval())
}

func TestPoller_PicksUpIntervalChange(t *testing.T) {
	policy := fastPolicy(t)
	p := NewPoller(PollerConfig{
		Registry: backends.NewFakeRegistry(),
		Store:    NewStore(),
		Policy:   policy,
		Logger:   testLogger(),
	})
	require.Equal(t, 10*time.Millisecond, p.refreshInterval())

	require.NoError(t, policy.Set(context.Background(),
		settings.CostConfig{RefreshInterval: 90 * time.Second}))
	assert.Equal(t, 90*time.Second, p.refreshInterval(),
		"a control-plane change must take effect without a restart")
}

func TestPoller_KeepsIntervalWhenPolicyReadFails(t *testing.T) {
	p := NewPoller(PollerConfig{
		Registry: backends.NewFakeRegistry(),
		Store:    NewStore(),
		Policy:   failingPolicy{},
		Logger:   testLogger(),
	})
	assert.Equal(t, settings.DefaultCostRefreshInterval, p.refreshInterval())
}

type failingPolicy struct{}

func (failingPolicy) Get(context.Context) (settings.CostConfig, error) {
	return settings.CostConfig{}, assert.AnError
}
