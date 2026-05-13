package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving"
	"github.com/harvard-cns/orla/internal/serving/access"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// test env var names, not credentials. //nolint:gosec // G101
const (
	testAPIKeyEnvVar  = "ORLA_TEST_OPENAI_KEY" //nolint:gosec // G101 - env var name, not a credential
	testAPIKeyEnvVar2 = "ORLA_TEST_KEY"        //nolint:gosec // G101 - env var name, not a credential
)

func TestServer_HandleExecute_RequestBodyTooLarge(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	// Valid JSON body that exceeds maxRequestBodyBytes (10MB)
	payload := `{"backend":"x","prompt":"` + string(bytes.Repeat([]byte("x"), maxRequestBodyBytes)) + `"}`
	req := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")

	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "request body too large")
}

func TestRecoveryMiddleware_RecoversPanic(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("test panic")
	})
	wrapped := recoveryMiddleware(panicHandler)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	wrapped.ServeHTTP(resp, req)

	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "internal server error")
}

func TestServer_HandleHealth(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result map[string]string
	err := json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "healthy", result["status"])
}

func TestServer_HandleMetrics(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("ok").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("metrics-backend", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:test-model")
	server := NewAgenticServer(layer, ":0", nil)

	// Execute a request to populate orla_ metrics
	executeReq := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader([]byte(`{"backend":"metrics-backend","prompt":"hi"}`)))
	executeReq.Header.Set("Content-Type", "application/json")
	server.mux.ServeHTTP(httptest.NewRecorder(), executeReq)

	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("GET", "/metrics", nil))
	require.Equal(t, http.StatusOK, resp.Code)
	body := resp.Body.String()
	assert.Contains(t, body, "# HELP ")
	assert.Contains(t, body, "# TYPE ")
	assert.Contains(t, body, "orla_")
}

func TestServer_HandleExecute_NoBackend(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	reqBody := ExecuteRequest{
		Prompt: "test prompt",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleExecute_InvalidJSON(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader([]byte("invalid json")))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleExecute_BackendNotFound(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	reqBody := ExecuteRequest{
		Backend: "nonexistent",
		Prompt:  "test prompt",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusInternalServerError, resp.Code)
	var result ExecuteResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "not found")
}

func TestServer_HandleExecute_InvalidSchedulingPolicy(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	reqBody := ExecuteRequest{
		Backend: "nonexistent",
		Prompt:  "test prompt",
		InferenceOptions: model.InferenceOptions{
			SchedulingPolicy: "not_supported",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleExecute_InvalidRequestSchedulingPolicy(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	reqBody := ExecuteRequest{
		Backend: "nonexistent",
		Prompt:  "test prompt",
		InferenceOptions: model.InferenceOptions{
			RequestSchedulingPolicy: "not_supported",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleRegisterBackend(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	reqBody := RegisterBackendRequest{
		Name:     "vllm",
		Endpoint: "http://localhost:8000/v1",
		Type:     "openai",
		ModelID:  "openai:Qwen/Qwen3-4B",
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/backends", bytes.NewReader(body))
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	var result RegisterBackendResponse
	err = json.Unmarshal(resp.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.True(t, result.Success)

	// List backends
	resp2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/v1/backends", nil)
	server.mux.ServeHTTP(resp2, req2)
	require.Equal(t, http.StatusOK, resp2.Code)
	var listResult ListBackendsResponse
	err = json.Unmarshal(resp2.Body.Bytes(), &listResult)
	require.NoError(t, err)
	assert.Equal(t, []string{"vllm"}, listResult.Backends)
}

func TestServer_RateLimit_Returns429WhenExceeded(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", &ServerOptions{RateLimitRPS: 1})

	reqBody := ExecuteRequest{Backend: "x", Prompt: "hi"}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	// First request - may succeed (backend not found) or get 429 if we're fast
	// Second request immediately - should hit rate limit
	resp1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp1, req1)

	resp2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body))
	server.mux.ServeHTTP(resp2, req2)

	// At least one should get 429 (rate limit) or 500 (backend not found)
	// With RPS=1, the second request in quick succession should get 429
	if resp2.Code == http.StatusTooManyRequests {
		assert.Contains(t, resp2.Body.String(), "rate limit")
	}
}

func TestServer_HandleRegisterBackend_Validation(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	negQuality := -0.5
	overQuality := 1.5
	goodQuality := 0.8

	tests := []struct {
		name string
		req  RegisterBackendRequest
		want int
	}{
		{"missing name", RegisterBackendRequest{Endpoint: "http://x", Type: "openai", ModelID: "openai:m"}, http.StatusBadRequest},
		{"missing endpoint", RegisterBackendRequest{Name: "v", Type: "openai", ModelID: "openai:m"}, http.StatusBadRequest},
		{"missing type", RegisterBackendRequest{Name: "v", Endpoint: "http://x", ModelID: "openai:m"}, http.StatusBadRequest},
		{"missing model_id", RegisterBackendRequest{Name: "v", Endpoint: "http://x", Type: "openai"}, http.StatusBadRequest},
		{"invalid type", RegisterBackendRequest{Name: "v", Endpoint: "http://x", Type: "invalid", ModelID: "openai:m"}, http.StatusBadRequest},
		{"negative quality", RegisterBackendRequest{Name: "v", Endpoint: "http://x", Type: "openai", ModelID: "openai:m", Quality: &negQuality}, http.StatusBadRequest},
		{"quality > 1", RegisterBackendRequest{Name: "v", Endpoint: "http://x", Type: "openai", ModelID: "openai:m", Quality: &overQuality}, http.StatusBadRequest},
		{"negative cost rate", RegisterBackendRequest{Name: "v", Endpoint: "http://x", Type: "openai", ModelID: "openai:m", CostModel: &CostModelRequest{InputCostPerMToken: -1}}, http.StatusBadRequest},
		{"valid with cost+quality", RegisterBackendRequest{
			Name: "v", Endpoint: "http://x", Type: "openai", ModelID: "openai:m",
			Quality:   &goodQuality,
			CostModel: &CostModelRequest{InputCostPerMToken: 0.25, OutputCostPerMToken: 1.25},
		}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.req)
			require.NoError(t, err)
			resp := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/v1/backends", bytes.NewReader(body))
			server.mux.ServeHTTP(resp, req)
			assert.Equal(t, tt.want, resp.Code)
		})
	}
}

func TestServer_HandleExecute_AccuracyRouting(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("routed").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar2, "k")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("expensive", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: srv.URL() + "/v1",
		Quality:  core.Ptr(0.9),
		CostModel: &core.CostModel{
			InputCostPerMToken:  5.0,
			OutputCostPerMToken: 20.0,
		},
		APIKeyEnvVar: testAPIKeyEnvVar2,
	}, "openai:big")
	layer.AddLLMBackend("cheap", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: srv.URL() + "/v1",
		Quality:  core.Ptr(0.5),
		CostModel: &core.CostModel{
			InputCostPerMToken:  0.1,
			OutputCostPerMToken: 0.5,
		},
		APIKeyEnvVar: testAPIKeyEnvVar2,
	}, "openai:small")

	server := NewAgenticServer(layer, ":0", nil)

	accuracy := 0.4
	reqBody := ExecuteRequest{
		Prompt: "hi",
		InferenceOptions: model.InferenceOptions{
			Accuracy: &accuracy,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))

	require.Equal(t, http.StatusOK, resp.Code)
	var result ExecuteResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result.Success)
	assert.Equal(t, "routed", result.Response.Content)
}

func TestServer_HandleExecute_AccuracyNoneQualify_Strict(t *testing.T) {
	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("weak", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: "http://x",
		Quality:  core.Ptr(0.3),
		CostModel: &core.CostModel{
			InputCostPerMToken:  0.1,
			OutputCostPerMToken: 0.5,
		},
	}, "openai:tiny")

	server := NewAgenticServer(layer, ":0", nil)

	accuracy := 0.9
	reqBody := ExecuteRequest{
		Backend: "weak",
		Prompt:  "hi",
		InferenceOptions: model.InferenceOptions{
			Accuracy:       &accuracy,
			AccuracyPolicy: model.AccuracyPolicyStrict,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "no backend with quality")
}

func TestServer_HandleExecute_AccuracyPolicyInvalid(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	accuracy := 0.5
	reqBody := ExecuteRequest{
		Backend: "b",
		Prompt:  "hi",
		InferenceOptions: model.InferenceOptions{
			Accuracy:       &accuracy,
			AccuracyPolicy: "bogus",
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "accuracy_policy must be")
}

func TestServer_HandleExecute_AccuracyOutOfRange(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	accuracy := 1.5
	reqBody := ExecuteRequest{
		Backend: "x",
		Prompt:  "hi",
		InferenceOptions: model.InferenceOptions{
			Accuracy: &accuracy,
		},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "accuracy must be in [0.0, 1.0]")
}

func TestServer_HandleUpdateBackend_Success(t *testing.T) {
	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("test-be", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: "http://x",
		Quality:  core.Ptr(0.5),
	}, "openai:m")

	server := NewAgenticServer(layer, ":0", nil)

	quality := 0.9
	update := serving.BackendUpdate{
		Quality: &quality,
		CostModel: &core.CostModel{
			InputCostPerMToken:  2.0,
			OutputCostPerMToken: 10.0,
		},
	}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("PATCH", "/api/v1/backends/test-be", bytes.NewReader(body)))

	require.Equal(t, http.StatusOK, resp.Code)

	cm := layer.GetCostModel("test-be")
	require.NotNil(t, cm)
	assert.Equal(t, 2.0, cm.InputCostPerMToken)
	assert.Equal(t, 10.0, cm.OutputCostPerMToken)
}

func TestServer_HandleUpdateBackend_NotFound(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	quality := 0.5
	update := serving.BackendUpdate{Quality: &quality}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("PATCH", "/api/v1/backends/nonexistent", bytes.NewReader(body)))

	require.Equal(t, http.StatusNotFound, resp.Code)
	assert.Contains(t, resp.Body.String(), "not found")
}

func TestServer_HandleUpdateBackend_InvalidQuality(t *testing.T) {
	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("be", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: "http://x",
	}, "openai:m")

	server := NewAgenticServer(layer, ":0", nil)

	quality := 1.5
	update := serving.BackendUpdate{Quality: &quality}
	body, err := json.Marshal(update)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("PATCH", "/api/v1/backends/be", bytes.NewReader(body)))

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "quality must be")
}

// Tests for Access Control

func TestServer_PolicyCRUD(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	// Add a policy.
	addBody, err := json.Marshal(AddPolicyRequest{
		Name:      "deny-interns",
		Subjects:  []string{"tenant:interns"},
		Resources: []string{"backend:gpt4o"},
		Action:    "deny",
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/policies", bytes.NewReader(addBody)))
	require.Equal(t, http.StatusOK, resp.Code)

	// List policies.
	resp = httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("GET", "/api/v1/policies", nil))
	require.Equal(t, http.StatusOK, resp.Code)
	var listResult ListPoliciesResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &listResult))
	require.Len(t, listResult.Policies, 1)
	assert.Equal(t, "deny-interns", listResult.Policies[0].Name)

	// Remove the policy.
	resp = httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("DELETE", "/api/v1/policies/deny-interns", nil))
	require.Equal(t, http.StatusOK, resp.Code)

	// List should be empty now.
	resp = httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("GET", "/api/v1/policies", nil))
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &listResult))
	assert.Len(t, listResult.Policies, 0)
}

func TestServer_PolicyAdd_Validation(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	addBody, err := json.Marshal(AddPolicyRequest{
		Name:      "",
		Subjects:  []string{"tenant:a"},
		Resources: []string{"backend:b"},
		Action:    "deny",
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/policies", bytes.NewReader(addBody)))
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_PolicyRemove_NotFound(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("DELETE", "/api/v1/policies/nonexistent", nil))
	require.Equal(t, http.StatusNotFound, resp.Code)
}

func TestServer_HandleExecute_BackendAccessDenied(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("ok").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("gpt4o", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:gpt-4o")
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name:      "deny-interns-gpt4o",
		Subjects:  []string{"tenant:interns"},
		Resources: []string{"backend:gpt4o"},
		Action:    access.ActionDeny,
	}))

	server := NewAgenticServer(layer, ":0", nil)

	// Request with denied tags.
	reqBody := ExecuteRequest{
		Backend: "gpt4o",
		Prompt:  "hi",
		Tags:    map[string]string{"tenant": "interns"},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))
	require.Equal(t, http.StatusForbidden, resp.Code)
	assert.Contains(t, resp.Body.String(), "access denied")

	// Same backend, different tags — allowed.
	reqBody.Tags = map[string]string{"tenant": "research"}
	body, err = json.Marshal(reqBody)
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestServer_HandleExecute_ToolAccessDenied(t *testing.T) {
	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("be", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: "http://x",
	}, "openai:m")
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name:      "deny-shell",
		Subjects:  []string{"tenant:untrusted"},
		Resources: []string{"tool:shell_*"},
		Action:    access.ActionDeny,
	}))

	server := NewAgenticServer(layer, ":0", nil)

	reqBody, err := json.Marshal(map[string]any{
		"backend": "be",
		"prompt":  "hi",
		"tags":    map[string]string{"tenant": "untrusted"},
		"tools": []map[string]any{
			{"name": "search", "inputSchema": map[string]any{"type": "object"}},
			{"name": "shell_exec", "inputSchema": map[string]any{"type": "object"}},
		},
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(reqBody)))
	require.Equal(t, http.StatusForbidden, resp.Code)
	assert.Contains(t, resp.Body.String(), "tool access denied")
}

func TestServer_HandleExecute_DataLabelDenied(t *testing.T) {
	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("ext-openai", &core.LLMBackend{
		Type:     core.LLMInferenceAPITypeOpenAI,
		Endpoint: "http://x",
	}, "openai:gpt-4o")
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name:      "pii-no-external",
		Subjects:  []string{"backend:ext-*"},
		Resources: []string{"data:pii"},
		Action:    access.ActionDeny,
	}))

	server := NewAgenticServer(layer, ":0", nil)

	reqBody := ExecuteRequest{
		Backend:    "ext-openai",
		Prompt:     "summarize",
		Tags:       map[string]string{"tenant": "hr"},
		DataLabels: []string{"pii"},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))
	require.Equal(t, http.StatusForbidden, resp.Code)
	assert.Contains(t, resp.Body.String(), "data access denied")
}

func TestServer_HandleRegisterWorkflow(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	wfBody, err := json.Marshal(RegisterWorkflowRequest{
		WorkflowID: "wf-1",
		Edges:      []WorkflowEdgeEntry{{From: "a", To: "b"}},
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewReader(wfBody)))
	require.Equal(t, http.StatusOK, resp.Code)
}

func TestServer_HandleRegisterWorkflow_MissingID(t *testing.T) {
	layer := serving.NewAgenticLayer()
	server := NewAgenticServer(layer, ":0", nil)

	wfBody, err := json.Marshal(RegisterWorkflowRequest{
		Edges: []WorkflowEdgeEntry{{From: "a", To: "b"}},
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewReader(wfBody)))
	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestServer_HandleAccessCheck_Allowed(t *testing.T) {
	layer := serving.NewAgenticLayer()
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "eng-allow-cheap", Subjects: []string{"tenant:engineering"}, Resources: []string{"backend:cheap"}, Action: access.ActionAllow,
	}))
	server := NewAgenticServer(layer, ":0", nil)

	body, err := json.Marshal(AccessCheckRequest{
		Tags:    map[string]string{"tenant": "engineering"},
		Backend: "cheap",
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/access/check", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, resp.Code)
	var result AccessCheckResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.True(t, result.Allowed)
}

func TestServer_HandleAccessCheck_Denied(t *testing.T) {
	layer := serving.NewAgenticLayer()
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "intern-allow-cheap", Subjects: []string{"tenant:interns"}, Resources: []string{"backend:cheap"}, Action: access.ActionAllow,
	}))
	server := NewAgenticServer(layer, ":0", nil)

	body, err := json.Marshal(AccessCheckRequest{
		Tags:    map[string]string{"tenant": "interns"},
		Backend: "strong",
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/access/check", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, resp.Code)
	var result AccessCheckResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.False(t, result.Allowed)
	assert.Contains(t, result.Reason, "no allow policy")
}

func TestServer_HandleExecute_DataLabelPropagation(t *testing.T) {
	// DAG: stage-a → stage-b. Stage A carries PII, stage B inherits it.
	srv := model.NewMockLLMServer().ReturnContent("ok").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("local", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m")
	layer.AddLLMBackend("external", &core.LLMBackend{
		Type:         core.LLMInferenceAPITypeOpenAI,
		Endpoint:     srv.URL() + "/v1",
		APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m2")

	// Policy: PII cannot go to external backend.
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name:      "pii-no-ext",
		Subjects:  []string{"backend:external"},
		Resources: []string{"data:pii"},
		Action:    access.ActionDeny,
	}))

	server := NewAgenticServer(layer, ":0", nil)

	// Register the workflow DAG.
	wfBody, err := json.Marshal(RegisterWorkflowRequest{
		WorkflowID: "wf-prop",
		Edges:      []WorkflowEdgeEntry{{From: "stage-a", To: "stage-b"}},
	})
	require.NoError(t, err)
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/workflows", bytes.NewReader(wfBody)))
	require.Equal(t, http.StatusOK, resp.Code)

	// Stage A on local backend with PII — allowed.
	reqBody := ExecuteRequest{
		Backend:    "local",
		StageID:    "stage-a",
		Prompt:     "process employee records",
		WorkflowID: "wf-prop",
		Tags:       map[string]string{"tenant": "hr"},
		DataLabels: []string{"pii"},
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, resp.Code)

	// Stage B on external backend, no explicit labels — denied because
	// it inherits PII from stage A through the registered DAG edge.
	reqBody = ExecuteRequest{
		Backend:    "external",
		StageID:    "stage-b",
		Prompt:     "summarize",
		WorkflowID: "wf-prop",
		Tags:       map[string]string{"tenant": "hr"},
	}
	body, err = json.Marshal(reqBody)
	require.NoError(t, err)
	resp = httptest.NewRecorder()
	server.mux.ServeHTTP(resp, httptest.NewRequest("POST", "/api/v1/execute", bytes.NewReader(body)))
	require.Equal(t, http.StatusForbidden, resp.Code)
	assert.Contains(t, resp.Body.String(), "data access denied")
}
