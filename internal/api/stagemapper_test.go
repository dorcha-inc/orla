package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/mappings"
	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/wire"
)

func newStageMapperTestServer(t *testing.T) (*chi.Mux, *settings.FakeMapperStore, *mappings.MapperHolder) {
	t.Helper()
	store := &settings.FakeMapperStore{}
	holder := &mappings.MapperHolder{}
	r := chi.NewRouter()
	RegisterStageMapperRoutes(r, StageMapperDeps{Store: store, Holder: holder})
	return r, store, holder
}

func TestStageMapper_GetDefaultsToDisabled(t *testing.T) {
	r, _, _ := newStageMapperTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/stage-mapper", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var got wire.StageMapper
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.False(t, got.Enabled)
	assert.Empty(t, got.URL)
}

func TestStageMapper_SetPersistsAndInstalls(t *testing.T) {
	r, store, holder := newStageMapperTestServer(t)

	body := mustJSON(t, wire.StageMapper{URL: "http://mapper:8091/decide", TimeoutMS: 80})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/stage-mapper", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	var got wire.StageMapper
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.True(t, got.Enabled)
	assert.Equal(t, 80, got.TimeoutMS)

	stored, err := store.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "http://mapper:8091/decide", stored.URL)
	assert.Equal(t, 80*time.Millisecond, stored.Timeout)

	assert.NotNil(t, holder.Get(), "a set mapper must be live on the proxy immediately")
}

func TestStageMapper_EmptyURLDisables(t *testing.T) {
	r, _, holder := newStageMapperTestServer(t)
	holder.Set(mappings.NewHTTPMapper("http://mapper:8091/decide", time.Second))

	body := mustJSON(t, wire.StageMapper{URL: ""})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/stage-mapper", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	assert.Nil(t, holder.Get(), "an empty URL must uninstall the live mapper")
}

func TestStageMapper_RejectsBadURL(t *testing.T) {
	r, _, _ := newStageMapperTestServer(t)

	body := mustJSON(t, wire.StageMapper{URL: "ftp://mapper/decide"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/stage-mapper", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
}

func TestStageMapper_RejectsNegativeTimeout(t *testing.T) {
	r, _, _ := newStageMapperTestServer(t)

	body := mustJSON(t, wire.StageMapper{URL: "http://mapper:8091/decide", TimeoutMS: -1})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/stage-mapper", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code, rr.Body.String())
}

func TestStageMapper_ZeroTimeoutGetsDefault(t *testing.T) {
	r, store, _ := newStageMapperTestServer(t)

	body := mustJSON(t, wire.StageMapper{URL: "http://mapper:8091/decide"})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/stage-mapper", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())

	stored, err := store.Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, defaultMapperTimeout, stored.Timeout)
}
