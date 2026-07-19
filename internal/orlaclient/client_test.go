package orlaclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/wire"
)

func TestClient_CreateBackend(t *testing.T) {
	var gotMethod, gotPath, gotContentType string
	var gotBody wire.CreateBackendRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotContentType = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(wire.Backend{Name: gotBody.Name, Kind: "llm", ModelID: new(gotBody.ModelID)})
	}))
	defer srv.Close()

	b, err := New(srv.URL).CreateBackend(context.Background(), wire.CreateBackendRequest{
		Name: "qwen-05b", Endpoint: "http://ollama/v1", ModelID: "ollama:qwen2.5:0.5b", MaxConcurrency: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/api/v1/backends", gotPath)
	assert.Equal(t, "application/json", gotContentType)
	assert.Equal(t, "qwen-05b", gotBody.Name)
	assert.Equal(t, "ollama:qwen2.5:0.5b", gotBody.ModelID)
	assert.Equal(t, "qwen-05b", b.Name)
}

func TestClient_ListBackends(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/backends", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"backends": []wire.Backend{{Name: "a", Kind: "llm"}, {Name: "b", Kind: "tool"}},
		})
	}))
	defer srv.Close()

	bs, err := New(srv.URL).ListBackends(context.Background())
	require.NoError(t, err)
	require.Len(t, bs, 2)
	assert.Equal(t, "a", bs[0].Name)
	assert.Equal(t, "b", bs[1].Name)
}

func TestClient_MapStage(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody wire.MapStageRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_ = json.NewEncoder(w).Encode(wire.Stage{ID: "plan", Backend: gotBody.Backend})
	}))
	defer srv.Close()

	s, err := New(srv.URL).MapStage(context.Background(), "plan", wire.MapStageRequest{Backend: "qwen-05b"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/api/v1/stages/plan", gotPath)
	assert.Equal(t, "qwen-05b", gotBody.Backend)
	assert.Equal(t, "qwen-05b", s.Backend)
}

func TestClient_DeleteBackend(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v1/backends/qwen-05b", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	require.NoError(t, New(srv.URL).DeleteBackend(context.Background(), "qwen-05b"))
}

func TestClient_ErrorStatusSurfacesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
	}))
	defer srv.Close()

	_, err := New(srv.URL).CreateBackend(context.Background(), wire.CreateBackendRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "name is required")
}

func TestClient_SubmitFeedback(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody wire.FeedbackRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "accepted"})
	}))
	defer srv.Close()

	rating := 1.0
	err := New(srv.URL).SubmitFeedback(context.Background(), wire.FeedbackRequest{
		CompletionID: "cmpl-1", StageID: "answer", Rating: &rating,
	})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v1/feedback", gotPath)
	assert.Equal(t, "cmpl-1", gotBody.CompletionID)
	assert.Equal(t, "answer", gotBody.StageID)
	require.NotNil(t, gotBody.Rating)
	assert.InDelta(t, 1.0, *gotBody.Rating, 1e-9)
}

// Exercises the cobra wiring end to end: flags parse, the address flag
// routes to the daemon, and the right HTTP call is made.
func TestRootCmd_StageMap(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody wire.MapStageRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(wire.Stage{ID: "plan", Backend: "qwen-05b"})
	}))
	defer srv.Close()

	root := NewRootCmd()
	root.SetArgs([]string{"--addr", srv.URL, "stage", "map", "plan", "qwen-05b"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	require.NoError(t, root.Execute())
	assert.Equal(t, http.MethodPut, gotMethod)
	assert.Equal(t, "/api/v1/stages/plan", gotPath)
	assert.Equal(t, "qwen-05b", gotBody.Backend)
}
