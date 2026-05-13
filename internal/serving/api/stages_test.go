package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/serving"
	"github.com/harvard-cns/orla/internal/stages"
	"github.com/harvard-cns/orla/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStageTestServer(t *testing.T) *AgenticServer {
	t.Helper()
	store, err := storage.Open(context.Background(), ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { core.LogDeferredError(store.Close) })
	reg := stages.NewRegistry(store.DB())
	layer := serving.NewAgenticLayer(serving.AgenticLayerOptions{StageRegistry: reg})
	return NewAgenticServer(layer, ":0", nil)
}

func doStageRequest(t *testing.T, server *AgenticServer, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(bs)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, req)
	return resp
}

func TestStages_PutCreates(t *testing.T) {
	server := newStageTestServer(t)
	priority := 5

	resp := doStageRequest(t, server, http.MethodPut, "/api/v1/stages/planning",
		stageRequestBody{
			Backend:         "gpt4o",
			Priority:        &priority,
			FlushOnComplete: true,
			Labels:          map[string]string{"team": "platform"},
		})

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got stages.Stage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	assert.Equal(t, "planning", got.ID)
	assert.Equal(t, "gpt4o", got.Backend)
	require.NotNil(t, got.Priority)
	assert.Equal(t, 5, *got.Priority)
	assert.True(t, got.FlushOnComplete)
	assert.Equal(t, "platform", got.Labels["team"])
}

func TestStages_PutMissingID(t *testing.T) {
	server := newStageTestServer(t)
	// PUT to /api/v1/stages with no id is not routed (404).
	resp := doStageRequest(t, server, http.MethodPut, "/api/v1/stages",
		stageRequestBody{Backend: "x"})
	assert.Equal(t, http.StatusMethodNotAllowed, resp.Code)
}

func TestStages_GetReturnsStage(t *testing.T) {
	server := newStageTestServer(t)
	require.Equal(t, http.StatusOK, doStageRequest(t, server, http.MethodPut, "/api/v1/stages/x",
		stageRequestBody{Backend: "be"}).Code)

	resp := doStageRequest(t, server, http.MethodGet, "/api/v1/stages/x", nil)
	require.Equal(t, http.StatusOK, resp.Code)
	var got stages.Stage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	assert.Equal(t, "be", got.Backend)
}

func TestStages_GetMissingReturns404(t *testing.T) {
	server := newStageTestServer(t)
	resp := doStageRequest(t, server, http.MethodGet, "/api/v1/stages/nope", nil)
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestStages_PatchUpdatesOnlyProvidedFields(t *testing.T) {
	server := newStageTestServer(t)
	priority := 9
	require.Equal(t, http.StatusOK, doStageRequest(t, server, http.MethodPut, "/api/v1/stages/x",
		stageRequestBody{Backend: "be", Priority: &priority, ReasoningEffort: "high"}).Code)

	resp := doStageRequest(t, server, http.MethodPatch, "/api/v1/stages/x",
		map[string]any{"backend": "be2"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var got stages.Stage
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	assert.Equal(t, "be2", got.Backend)
	require.NotNil(t, got.Priority)
	assert.Equal(t, 9, *got.Priority, "untouched field should be preserved")
	assert.Equal(t, "high", got.ReasoningEffort)
}

func TestStages_PatchUnknownFieldRejected(t *testing.T) {
	server := newStageTestServer(t)
	require.Equal(t, http.StatusOK, doStageRequest(t, server, http.MethodPut, "/api/v1/stages/x",
		stageRequestBody{Backend: "be"}).Code)

	resp := doStageRequest(t, server, http.MethodPatch, "/api/v1/stages/x",
		map[string]any{"bogus_field": 1})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "unknown field")
}

func TestStages_PatchMissingReturns404(t *testing.T) {
	server := newStageTestServer(t)
	resp := doStageRequest(t, server, http.MethodPatch, "/api/v1/stages/ghost",
		map[string]any{"backend": "be"})
	assert.Equal(t, http.StatusNotFound, resp.Code)
}

func TestStages_List(t *testing.T) {
	server := newStageTestServer(t)
	require.Equal(t, http.StatusOK, doStageRequest(t, server, http.MethodPut, "/api/v1/stages/b",
		stageRequestBody{Backend: "be"}).Code)
	require.Equal(t, http.StatusOK, doStageRequest(t, server, http.MethodPut, "/api/v1/stages/a",
		stageRequestBody{Backend: "be"}).Code)

	resp := doStageRequest(t, server, http.MethodGet, "/api/v1/stages", nil)
	require.Equal(t, http.StatusOK, resp.Code)
	var got ListStagesResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Len(t, got.Stages, 2)
	assert.Equal(t, "a", got.Stages[0].ID)
	assert.Equal(t, "b", got.Stages[1].ID)
}

func TestStages_Delete(t *testing.T) {
	server := newStageTestServer(t)
	require.Equal(t, http.StatusOK, doStageRequest(t, server, http.MethodPut, "/api/v1/stages/x",
		stageRequestBody{Backend: "be"}).Code)

	delResp := doStageRequest(t, server, http.MethodDelete, "/api/v1/stages/x", nil)
	require.Equal(t, http.StatusOK, delResp.Code)

	getResp := doStageRequest(t, server, http.MethodGet, "/api/v1/stages/x", nil)
	assert.Equal(t, http.StatusNotFound, getResp.Code)
}

func TestStages_ServiceUnavailableWhenRegistryNil(t *testing.T) {
	layer := serving.NewAgenticLayer(serving.AgenticLayerOptions{})
	server := NewAgenticServer(layer, ":0", nil)

	resp := doStageRequest(t, server, http.MethodGet, "/api/v1/stages/x", nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.Code)
}
