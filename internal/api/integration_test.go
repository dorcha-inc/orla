package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/harvard-cns/orla/internal/api"
	"github.com/harvard-cns/orla/internal/backends"
	"github.com/harvard-cns/orla/internal/metrics"
	"github.com/harvard-cns/orla/internal/provider"
	"github.com/harvard-cns/orla/internal/scheduler"
	"github.com/harvard-cns/orla/internal/settings"
	"github.com/harvard-cns/orla/internal/stages"
	"github.com/harvard-cns/orla/internal/storage"
	"github.com/harvard-cns/orla/internal/telemetry"
	"github.com/harvard-cns/orla/internal/wire"
)

// fakeOpenAIUpstream is an httptest.Server that mimics an OpenAI-compatible
// chat completions endpoint. Used by the integration test to exercise the
// full proxy path end-to-end without hitting the real openai-go HTTP stack.
func fakeOpenAIUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-fake-" + time.Now().Format(time.RFC3339Nano),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "fake-model",
			"choices": []map[string]any{{
				"index":         0,
				"finish_reason": "stop",
				"message":       map[string]any{"role": "assistant", "content": "hi"},
			}},
			"usage": map[string]any{
				"prompt_tokens": 5, "completion_tokens": 8, "total_tokens": 13,
			},
		})
	})
	return httptest.NewServer(mux)
}

// orlaStack stands up everything serve.go wires: storage, registries,
// scheduler, writers, metrics, and the chi server with all routes.
type orlaStack struct {
	ts            *httptest.Server
	store         *storage.Store
	scheduler     *scheduler.Scheduler
	completionW   *telemetry.CompletionWriter
	feedbackW     *telemetry.FeedbackWriter
	cleanupCalled bool
}

func (s *orlaStack) Close() {
	if s.cleanupCalled {
		return
	}
	s.cleanupCalled = true
	s.ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.scheduler.Shutdown(ctx)
	_ = s.completionW.Close(ctx)
	_ = s.feedbackW.Close(ctx)
	s.store.Close()
}

func setupOrlaStack(t *testing.T) *orlaStack {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
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
	t.Cleanup(func() { _ = pgC.Terminate(context.Background()) })

	dsn, err := pgC.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := storage.Open(ctx, storage.OpenConfig{DatabaseURL: dsn, Logger: logger})
	require.NoError(t, err)

	stageReg := stages.NewPostgresRegistry(store.Pool())
	backendReg := backends.NewPostgresRegistry(store.Pool())

	// Kind-aware factory. Tool backends get a fake ToolProvider so the
	// tool-dispatch route can be exercised end-to-end. serve.go registers
	// no real tool providers.
	sched := scheduler.New(func(b *backends.Backend) provider.Backend {
		if b.Kind == backends.KindTool {
			tk := ""
			if b.ToolKind != nil {
				tk = *b.ToolKind
			}
			return fakeToolProvider{name: b.Name, toolKind: tk}
		}
		return provider.NewOpenAI(b)
	}, logger)

	promReg := prometheus.NewRegistry()
	m := metrics.New(promReg)
	promReg.MustRegister(metrics.NewSchedulerCollector(sched))

	srv := api.NewServer(api.ServerConfig{
		ListenAddress: "127.0.0.1:0",
		Logger:        logger,
		Ready:         store.Ping,
		PromRegistry:  promReg,
	})
	policyStore := settings.NewPostgresStore(store.Pool())
	api.RegisterStageRoutes(srv.Router(), stageReg)
	api.RegisterBackendRoutes(srv.Router(), api.BackendDeps{
		Registry:  backendReg,
		Lifecycle: sched,
	})
	api.RegisterSchedulerRoutes(srv.Router(), api.SchedulerDeps{
		Store:     policyStore,
		Scheduler: sched,
	})

	completionW := telemetry.NewCompletionWriter(telemetry.CompletionWriterConfig{
		Pool:     store.Pool(),
		Logger:   logger,
		BatchSize: 1, // flush eagerly so tests see records quickly
		Interval:  20 * time.Millisecond,
	})
	feedbackW := telemetry.NewFeedbackWriter(telemetry.FeedbackWriterConfig{
		Pool:     store.Pool(),
		Logger:   logger,
		BatchSize: 1,
		Interval:  20 * time.Millisecond,
	})
	promReg.MustRegister(metrics.NewBatchWriterCollector(map[string]metrics.BatchWriterStats{
		"completion_records": completionW,
		"feedback":           feedbackW,
	}))

	api.RegisterProxyRoutes(srv.Router(), api.ProxyDeps{
		Stages:         stageReg,
		Scheduler:      sched,
		CompletionSink: completionW,
		Metrics:        m,
	})
	api.RegisterFeedbackRoutes(srv.Router(), api.FeedbackDeps{
		Sink:    feedbackW,
		Metrics: m,
	})
	api.RegisterMapperRoutes(srv.Router(), api.MapperDeps{
		Reader: telemetry.NewReader(store.Pool()),
	})
	api.RegisterToolRoutes(srv.Router(), api.ToolDeps{
		Stages:         stageReg,
		Scheduler:      sched,
		Backends:       backendReg,
		CompletionSink: completionW,
		Metrics:        m,
	})

	ts := httptest.NewServer(srv.Router())

	stack := &orlaStack{
		ts:          ts,
		store:       store,
		scheduler:   sched,
		completionW: completionW,
		feedbackW:   feedbackW,
	}
	t.Cleanup(stack.Close)
	return stack
}

// TestIntegration_FullLoop exercises the developer + mapper journey end
// to end: register backend, map stage, send chat completion via proxy,
// submit feedback, then query the mapper read endpoints and verify the
// completion + feedback + metrics rolled up correctly.
func TestIntegration_FullLoop(t *testing.T) {
	upstream := fakeOpenAIUpstream(t)
	t.Cleanup(upstream.Close)

	stack := setupOrlaStack(t)
	base := stack.ts.URL

	// 1. Register a backend pointing at the fake upstream.
	createBackend := mustMarshal(t, map[string]any{
		"name":            "fake-backend",
		"endpoint":        upstream.URL,
		"model_id":        "openai:fake-model",
		"max_concurrency": 4,
		"quality":         0.9,
	})
	resp := postJSON(t, base+"/api/v1/backends", createBackend)
	require.Equal(t, http.StatusCreated, resp.StatusCode, readBody(t, resp))
	_ = resp.Body.Close()

	// 2. Map the stage to the backend.
	putStage := mustMarshal(t, map[string]any{"backend": "fake-backend"})
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/stages/planning", bytes.NewReader(putStage))
	req.Header.Set("Content-Type", "application/json")
	resp = doRequest(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode, readBody(t, resp))
	_ = resp.Body.Close()

	// 3. Send a chat completion via the proxy.
	chat := mustMarshal(t, map[string]any{
		"model": "fake-model",
		"messages": []map[string]string{
			{"role": "user", "content": "hello"},
		},
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(chat))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.HeaderStage, "planning")
	resp = doRequest(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, readBody(t, resp))
	}
	var chatResp map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&chatResp))
	_ = resp.Body.Close()
	assert.Equal(t, "fake-backend", chatResp["model"], "response model must be backend name")
	completionID := chatResp["id"].(string)
	require.NotEmpty(t, completionID)

	// 4. Submit feedback for that completion.
	fb := mustMarshal(t, map[string]any{
		"completion_id": completionID,
		"stage_id":      "planning",
		"rating":        0.8,
		"labels":        []string{"accurate"},
	})
	resp = postJSON(t, base+"/v1/feedback", fb)
	assert.Equal(t, http.StatusAccepted, resp.StatusCode, readBody(t, resp))
	_ = resp.Body.Close()

	// 5. Give the BatchWriters a moment to flush before reading.
	require.Eventually(t, func() bool {
		return stack.completionW.Flushes() >= 1 && stack.feedbackW.Flushes() >= 1
	}, 3*time.Second, 50*time.Millisecond)

	// 6. The mapper reads completions, feedback, metrics.
	listCompletions := getResp(t, base+"/api/v1/stages/planning/completions")
	require.Equal(t, http.StatusOK, listCompletions.StatusCode)
	var compBody struct {
		Completions []*telemetry.CompletionRecord `json:"completions"`
	}
	require.NoError(t, json.NewDecoder(listCompletions.Body).Decode(&compBody))
	_ = listCompletions.Body.Close()
	require.Len(t, compBody.Completions, 1)
	got := compBody.Completions[0]
	assert.Equal(t, completionID, got.CompletionID)
	assert.Equal(t, "fake-backend", got.Backend)
	assert.Equal(t, "success", got.Status)
	require.NotNil(t, got.PromptTokens)
	assert.Equal(t, 5, *got.PromptTokens)

	listFeedback := getResp(t, base+"/api/v1/stages/planning/feedback")
	require.Equal(t, http.StatusOK, listFeedback.StatusCode)
	var fbBody struct {
		Feedback []*telemetry.Feedback `json:"feedback"`
	}
	require.NoError(t, json.NewDecoder(listFeedback.Body).Decode(&fbBody))
	_ = listFeedback.Body.Close()
	require.Len(t, fbBody.Feedback, 1)
	assert.Equal(t, completionID, fbBody.Feedback[0].CompletionID)
	require.NotNil(t, fbBody.Feedback[0].Rating)
	assert.InDelta(t, 0.8, *fbBody.Feedback[0].Rating, 1e-9)

	mResp := getResp(t, base+"/api/v1/stages/planning/metrics")
	require.Equal(t, http.StatusOK, mResp.StatusCode)
	var mBody struct {
		Metrics []*telemetry.CompletionMetrics `json:"metrics"`
	}
	require.NoError(t, json.NewDecoder(mResp.Body).Decode(&mBody))
	_ = mResp.Body.Close()
	require.Len(t, mBody.Metrics, 1)
	assert.Equal(t, "fake-backend", mBody.Metrics[0].Backend)
	assert.Equal(t, int64(1), mBody.Metrics[0].Count)
	assert.Equal(t, int64(0), mBody.Metrics[0].ErrorCount)

	// 7. /metrics endpoint contains orla_requests_total for this combo.
	promResp := getResp(t, base+"/metrics")
	require.Equal(t, http.StatusOK, promResp.StatusCode)
	promBody, _ := io.ReadAll(promResp.Body)
	_ = promResp.Body.Close()
	assert.True(t, strings.Contains(string(promBody),
		`orla_requests_total{backend="fake-backend",stage="planning",status="success"} 1`),
		"expected requests_total to reflect the dispatch:\n%s", string(promBody))
}

// TestIntegration_SchedulerPolicy wires the real Postgres-backed policy
// store, the scheduler, and the HTTP handler, then round-trips a policy
// through PUT and GET and confirms it persists for a later reader, the
// way the daemon reloads it on restart.
func TestIntegration_SchedulerPolicy(t *testing.T) {
	stack := setupOrlaStack(t)
	base := stack.ts.URL

	// A fresh install is first-come-first-served.
	var got wire.SchedulerPolicy
	resp := getResp(t, base+"/api/v1/scheduler/policy")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&got))
	_ = resp.Body.Close()
	assert.False(t, got.Enabled)

	// Set one over HTTP.
	body := mustMarshal(t, wire.SchedulerPolicy{URL: "http://sched:8090/next", TimeoutMS: 75})
	req, err := http.NewRequest(http.MethodPut, base+"/api/v1/scheduler/policy", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	putResp := doRequest(t, req)
	require.Equal(t, http.StatusOK, putResp.StatusCode, readBody(t, putResp))

	// GET reflects it.
	resp2 := getResp(t, base+"/api/v1/scheduler/policy")
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&got))
	_ = resp2.Body.Close()
	assert.True(t, got.Enabled)
	assert.Equal(t, "http://sched:8090/next", got.URL)
	assert.Equal(t, 75, got.TimeoutMS)

	// It is durable: a fresh store over the same database sees it, which
	// is what the daemon does when it reloads the policy on restart.
	reloaded, err := settings.NewPostgresStore(stack.store.Pool()).Get(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "http://sched:8090/next", reloaded.URL)
	assert.Equal(t, 75*time.Millisecond, reloaded.Timeout)
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// doRequest is a tiny helper that fails the test on a transport error
// and returns the *http.Response.
func doRequest(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func getResp(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url) //nolint:bodyclose // closed by caller in tests
	require.NoError(t, err)
	return resp
}

func postJSON(t *testing.T, url string, body []byte) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader(body)) //nolint:bodyclose // closed by caller in tests
	require.NoError(t, err)
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return string(b)
}

// fakeToolProvider is a ToolProvider stand-in for the tool-dispatch
// tests. It returns a fixed payload and a small gpu_seconds usage so
// cost computation is deterministic.
type fakeToolProvider struct {
	name     string
	toolKind string
}

func (f fakeToolProvider) Name() string     { return f.name }
func (f fakeToolProvider) ToolKind() string { return f.toolKind }

func (f fakeToolProvider) Invoke(context.Context, provider.ToolRequest) (*provider.ToolResponse, error) {
	return &provider.ToolResponse{
		Payload: []byte(`{"structure_cif":"data_test\n#"}`),
		Usage:   map[string]float64{"gpu_seconds": 5.0},
	}, nil
}

// TestIntegration_ToolDispatch_StructurePrediction exercises the
// /v1/tools/structure-prediction route end-to-end: register a tool
// backend, map a stage to it, POST a prediction request, verify the
// response + the completion record + the computed cost.
func TestIntegration_ToolDispatch_StructurePrediction(t *testing.T) {
	stack := setupOrlaStack(t)
	base := stack.ts.URL

	// 1. Register a tool backend pointing at the fake upstream.
	createBackend := mustMarshal(t, map[string]any{
		"name":            "fake-boltz",
		"kind":            "tool",
		"tool_kind":       "structure-prediction",
		"endpoint":        "http://tool.local",
		"max_concurrency": 1,
		"rates":           map[string]float64{"gpu_seconds": 0.001}, // $/s
	})
	resp := postJSON(t, base+"/api/v1/backends", createBackend)
	require.Equal(t, http.StatusCreated, resp.StatusCode, readBody(t, resp))
	_ = resp.Body.Close()

	// 2. Map the stage.
	putStage := mustMarshal(t, map[string]any{"backend": "fake-boltz"})
	req, _ := http.NewRequest(http.MethodPut, base+"/api/v1/stages/predict",
		bytes.NewReader(putStage))
	req.Header.Set("Content-Type", "application/json")
	resp = doRequest(t, req)
	require.Equal(t, http.StatusOK, resp.StatusCode, readBody(t, resp))
	_ = resp.Body.Close()

	// 3. POST a structure-prediction request.
	body := mustMarshal(t, map[string]any{
		"sequences": []string{"MKTV"},
	})
	req, _ = http.NewRequest(http.MethodPost,
		base+"/v1/tools/structure-prediction", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(api.HeaderStage, "predict")
	resp = doRequest(t, req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, readBody(t, resp))
	}

	completionID := resp.Header.Get("X-Orla-Completion-Id")
	require.NotEmpty(t, completionID)

	var toolResp provider.ToolResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&toolResp))
	_ = resp.Body.Close()
	assert.InDelta(t, 5.0, toolResp.Usage["gpu_seconds"], 1e-9)
	assert.Contains(t, string(toolResp.Payload), "data_test")

	// 4. Verify the completion record landed with the right kind / cost.
	require.Eventually(t, func() bool {
		return stack.completionW.Flushes() >= 1
	}, 3*time.Second, 50*time.Millisecond)

	completionsResp := getResp(t, base+"/api/v1/stages/predict/completions")
	require.Equal(t, http.StatusOK, completionsResp.StatusCode)
	var compBody struct {
		Completions []*telemetry.CompletionRecord `json:"completions"`
	}
	require.NoError(t, json.NewDecoder(completionsResp.Body).Decode(&compBody))
	_ = completionsResp.Body.Close()

	require.Len(t, compBody.Completions, 1)
	got := compBody.Completions[0]
	assert.Equal(t, completionID, got.CompletionID)
	assert.Equal(t, "fake-boltz", got.Backend)
	assert.Equal(t, "success", got.Status)
	assert.Equal(t, "structure-prediction", got.ToolKind)
	assert.InDelta(t, 5.0, got.Usage["gpu_seconds"], 1e-9)
	require.NotNil(t, got.CostUSD)
	// cost = 5.0 s × $0.001/s = $0.005
	assert.InDelta(t, 0.005, *got.CostUSD, 1e-9)

	// 5. Prom /metrics includes a orla_requests_total bump for this stage+backend.
	promResp := getResp(t, base+"/metrics")
	require.Equal(t, http.StatusOK, promResp.StatusCode)
	promBody, _ := io.ReadAll(promResp.Body)
	_ = promResp.Body.Close()
	assert.Contains(t, string(promBody),
		`orla_requests_total{backend="fake-boltz",stage="predict",status="success"} 1`)
}

// TestIntegration_ChatAndToolCoexist verifies that a single orla
// instance simultaneously hosts an LLM backend at /v1/chat/completions
// and a tool backend at /v1/tools/structure-prediction. Same stage
// registry, same scheduler, different routes per backend kind.
func TestIntegration_ChatAndToolCoexist(t *testing.T) {
	llmUpstream := fakeOpenAIUpstream(t)
	t.Cleanup(llmUpstream.Close)

	stack := setupOrlaStack(t)
	base := stack.ts.URL

	// Register both backends.
	for _, body := range [][]byte{
		mustMarshal(t, map[string]any{
			"name": "gpt", "endpoint": llmUpstream.URL,
			"model_id": "openai:fake", "max_concurrency": 1,
		}),
		mustMarshal(t, map[string]any{
			"name": "boltz", "kind": "tool", "tool_kind": "structure-prediction",
			"endpoint": "http://tool.local", "max_concurrency": 1,
			"rates": map[string]float64{"gpu_seconds": 0.001},
		}),
	} {
		resp := postJSON(t, base+"/api/v1/backends", body)
		require.Equal(t, http.StatusCreated, resp.StatusCode, readBody(t, resp))
		_ = resp.Body.Close()
	}

	// Map two stages.
	for _, sb := range []struct{ stage, backend string }{
		{"chat-stage", "gpt"},
		{"predict-stage", "boltz"},
	} {
		body := mustMarshal(t, map[string]any{"backend": sb.backend})
		req, _ := http.NewRequest(http.MethodPut,
			base+"/api/v1/stages/"+sb.stage, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp := doRequest(t, req)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		_ = resp.Body.Close()
	}

	// Dispatch an LLM call.
	chatBody := mustMarshal(t, map[string]any{
		"model": "fake", "messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	})
	chatReq, _ := http.NewRequest(http.MethodPost,
		base+"/v1/chat/completions", bytes.NewReader(chatBody))
	chatReq.Header.Set("Content-Type", "application/json")
	chatReq.Header.Set(api.HeaderStage, "chat-stage")
	chatResp := doRequest(t, chatReq)
	require.Equal(t, http.StatusOK, chatResp.StatusCode, readBody(t, chatResp))
	_ = chatResp.Body.Close()

	// Dispatch a tool call.
	toolBody := mustMarshal(t, map[string]any{"sequences": []string{"MKTV"}})
	toolReq, _ := http.NewRequest(http.MethodPost,
		base+"/v1/tools/structure-prediction", bytes.NewReader(toolBody))
	toolReq.Header.Set("Content-Type", "application/json")
	toolReq.Header.Set(api.HeaderStage, "predict-stage")
	toolResp := doRequest(t, toolReq)
	require.Equal(t, http.StatusOK, toolResp.StatusCode, readBody(t, toolResp))
	_ = toolResp.Body.Close()

	// Cross-kind misrouting: a tool route asking for the LLM stage's
	// backend should be rejected. This catches any future regression
	// where the kind-validation check is removed.
	misRoute, _ := http.NewRequest(http.MethodPost,
		base+"/v1/tools/structure-prediction", bytes.NewReader(toolBody))
	misRoute.Header.Set("Content-Type", "application/json")
	misRoute.Header.Set(api.HeaderStage, "chat-stage") // points at LLM
	misResp := doRequest(t, misRoute)
	assert.Equal(t, http.StatusBadRequest, misResp.StatusCode)
	_ = misResp.Body.Close()
}
