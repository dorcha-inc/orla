package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/mappings"
	"github.com/harvard-cns/orla/internal/provider"
	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/stages"
)

// stageMapperEnv wires a proxy with two LLM backends, a stage mapped
// to the first, and a stage mapper served by an httptest server.
type stageMapperEnv struct {
	srv      *Server
	stages   *stages.FakeRegistry
	variants *mappings.FakeRegistry
	metrics  *fakeProxyMetrics
	decided  *mappings.DecideRequest
}

func newStageMapperEnv(t *testing.T, policyHandler http.HandlerFunc) *stageMapperEnv {
	t.Helper()
	respond := func(name string) *provider.MockProvider {
		return provider.NewMockProvider().WithName(name).
			WithResponse(&openai.ChatCompletion{
				ID:    "chatcmpl-" + name,
				Model: name,
				Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{Role: "assistant", Content: "hi from " + name},
				}},
			})
	}
	mocks := map[string]*provider.MockProvider{
		"static-backend":  respond("static-backend"),
		"decided-backend": respond("decided-backend"),
	}
	sched := scheduler.New(
		func(b *backends.Backend) provider.Backend { return mocks[b.Name] },
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	t.Cleanup(func() { _ = sched.Shutdown(context.Background()) })
	quality := 0.9
	sched.Register(&backends.Backend{
		Name: "static-backend", Endpoint: "x", ModelID: new("openai:a"),
		MaxConcurrency: 2, Quality: &quality, Kind: backends.KindLLM,
	})
	sched.Register(&backends.Backend{
		Name: "decided-backend", Endpoint: "x", ModelID: new("openai:b"),
		MaxConcurrency: 2, Kind: backends.KindLLM,
	})

	stageReg := stages.NewFakeRegistry()
	_, err := stageReg.Replace(context.Background(), &stages.Stage{ID: "hop", Backend: "static-backend"})
	require.NoError(t, err)

	env := &stageMapperEnv{stages: stageReg, variants: mappings.NewFakeRegistry(), metrics: &fakeProxyMetrics{}}

	policySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req mappings.DecideRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		env.decided = &req
		policyHandler(w, r)
	}))
	t.Cleanup(policySrv.Close)

	holder := &mappings.MapperHolder{}
	holder.Set(mappings.NewHTTPMapper(policySrv.URL, time.Second))

	env.srv = NewServer(ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	RegisterProxyRoutes(env.srv.Router(), ProxyDeps{
		Stages:      stageReg,
		Mappings:    env.variants,
		Scheduler:   sched,
		Metrics:     env.metrics,
		StageMapper: holder,
	})
	return env
}

func (e *stageMapperEnv) post(t *testing.T, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		bytes.NewReader(bodyForChat("hi")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(HeaderStage, "hop")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	e.srv.Router().ServeHTTP(rr, req)
	return rr
}

func servedBy(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		Model string `json:"model"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	return resp.Model
}

func TestProxy_StageMapperRoutesToDecision(t *testing.T) {
	env := newStageMapperEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"backend": "decided-backend"}`))
	})

	rr := env.post(t, map[string]string{"X-Orla-Tag-Tenant": "acme"})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "decided-backend", servedBy(t, rr))
	assert.Contains(t, env.metrics.mapperDecisions, "ok")

	// The policy saw the stage, the tags, the static mapping, and both
	// candidates with their runtime state.
	require.NotNil(t, env.decided)
	assert.Equal(t, "hop", env.decided.Stage)
	assert.Equal(t, "static-backend", env.decided.Current)
	assert.Equal(t, "acme", env.decided.Tags["tenant"])
	require.Len(t, env.decided.Candidates, 2)
}

func TestProxy_StageMapperDeclineKeepsStaticMapping(t *testing.T) {
	env := newStageMapperEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"backend": ""}`))
	})

	rr := env.post(t, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "static-backend", servedBy(t, rr))
	assert.Contains(t, env.metrics.mapperDecisions, "declined")
}

func TestProxy_StageMapperInvalidChoiceFallsBack(t *testing.T) {
	env := newStageMapperEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"backend": "never-registered"}`))
	})

	rr := env.post(t, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "static-backend", servedBy(t, rr))
	assert.Contains(t, env.metrics.mapperDecisions, "fallback_invalid")
}

func TestProxy_StageMapperErrorFallsBack(t *testing.T) {
	env := newStageMapperEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	rr := env.post(t, nil)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "static-backend", servedBy(t, rr))
	assert.Contains(t, env.metrics.mapperDecisions, "fallback_error")
}

func TestProxy_VariantOverrideWinsOverStageMapper(t *testing.T) {
	env := newStageMapperEnv(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"backend": "decided-backend"}`))
	})
	_, err := env.variants.Put(context.Background(), "pin-static", map[string]string{
		"hop": "static-backend",
	})
	require.NoError(t, err)

	rr := env.post(t, map[string]string{HeaderMapping: "pin-static"})
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	assert.Equal(t, "static-backend", servedBy(t, rr))
	assert.Empty(t, env.metrics.mapperDecisions,
		"an explicit variant pin must not consult the policy at all")
}
