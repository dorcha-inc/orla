package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving"
	"github.com/harvard-cns/orla/internal/serving/access"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newProxyTestServer(t *testing.T) (*AgenticServer, *model.MockLLMServer) {
	t.Helper()
	srv := model.NewMockLLMServer().ReturnContent("hello from mock").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("cheap", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: srv.URL() + "/v1", APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m")
	return NewAgenticServer(layer, ":0", nil), srv
}

func doChatCompletion(t *testing.T, server *AgenticServer, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	bs, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, req)
	return resp
}

func TestChatCompletions_HappyPath(t *testing.T) {
	server, _ := newProxyTestServer(t)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "cheap",
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
		},
		map[string]string{headerOrlaStage: "planning"},
	)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var result chatCompletion
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	assert.Equal(t, "chat.completion", result.Object)
	assert.Equal(t, "cheap", result.Model)
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "assistant", result.Choices[0].Message.Role)
	assert.Equal(t, "hello from mock", result.Choices[0].Message.Content)
	assert.Equal(t, "stop", result.Choices[0].FinishReason)
}

func TestChatCompletions_MissingModel(t *testing.T) {
	server, _ := newProxyTestServer(t)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{Messages: []chatMessage{{Role: "user", Content: "hi"}}},
		map[string]string{headerOrlaStage: "planning"},
	)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "model is required")
}

func TestChatCompletions_MissingMessages(t *testing.T) {
	server, _ := newProxyTestServer(t)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{Model: "cheap"},
		map[string]string{headerOrlaStage: "planning"},
	)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "messages is required")
}

func TestChatCompletions_MissingStage(t *testing.T) {
	server, _ := newProxyTestServer(t)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "cheap",
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
		},
		nil,
	)

	require.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), "stage is required")
}

func TestChatCompletions_StageFromMetadataBody(t *testing.T) {
	server, _ := newProxyTestServer(t)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "cheap",
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
			Metadata: &chatRequestMeta{Orla: &orlaMetaInBody{Stage: "planning"}},
		},
		nil,
	)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

func TestChatCompletions_AccessDeniedByPolicy(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("ok").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("strong", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: srv.URL() + "/v1", APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m")
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "intern-cheap-only", Subjects: []string{"tenant:interns"}, Resources: []string{"backend:cheap"}, Action: access.ActionAllow,
	}))
	server := NewAgenticServer(layer, ":0", nil)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "strong",
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
		},
		map[string]string{
			headerOrlaStage:           "planning",
			headerOrlaTagPrefix + "Tenant": "interns",
		},
	)

	require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), "backend")
}

func TestChatCompletions_DataLabelDenied(t *testing.T) {
	srv := model.NewMockLLMServer().ReturnContent("ok").Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("ext", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: srv.URL() + "/v1", APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m")
	require.NoError(t, layer.PolicyStore.Add(&access.Policy{
		Name: "no-pii-ext", Subjects: []string{"backend:ext"}, Resources: []string{"data:pii"}, Action: access.ActionDeny,
	}))
	server := NewAgenticServer(layer, ":0", nil)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "ext",
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
		},
		map[string]string{
			headerOrlaStage:      "planning",
			headerOrlaDataLabels: "pii, secret",
		},
	)

	require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), "data access denied")
}

func TestChatCompletions_ToolsRoundtrip(t *testing.T) {
	srv := model.NewMockLLMServer().
		ReturnToolCall("search", `{"q":"orla"}`).
		Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("cheap", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: srv.URL() + "/v1", APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m")
	server := NewAgenticServer(layer, ":0", nil)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "cheap",
			Messages: []chatMessage{{Role: "user", Content: "search please"}},
			Tools: []chatTool{{
				Type: "function",
				Function: chatToolFunction{
					Name:        "search",
					Description: "do a search",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`),
				},
			}},
		},
		map[string]string{headerOrlaStage: "planning"},
	)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var result chatCompletion
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
	require.Len(t, result.Choices, 1)
	assert.Equal(t, "tool_calls", result.Choices[0].FinishReason)
	require.Len(t, result.Choices[0].Message.ToolCalls, 1)
	tc := result.Choices[0].Message.ToolCalls[0]
	assert.Equal(t, "search", tc.Function.Name)
	assert.Equal(t, "function", tc.Type)
	assert.Contains(t, tc.Function.Arguments, `"q"`)
}

func TestChatCompletions_BackendNotFound(t *testing.T) {
	server, _ := newProxyTestServer(t)

	resp := doChatCompletion(t, server,
		chatCompletionRequest{
			Model:    "does-not-exist",
			Messages: []chatMessage{{Role: "user", Content: "hi"}},
		},
		map[string]string{headerOrlaStage: "planning"},
	)

	require.Equal(t, http.StatusInternalServerError, resp.Code)
	assert.Contains(t, resp.Body.String(), "does-not-exist")
}

func TestChatCompletions_Streaming(t *testing.T) {
	srv := model.NewMockLLMServer().
		ReturnStreamChunks([]string{"hello ", "from ", "mock"}).
		Start()
	t.Cleanup(srv.Close)
	t.Setenv(testAPIKeyEnvVar, "test-key")

	layer := serving.NewAgenticLayer()
	layer.AddLLMBackend("cheap", &core.LLMBackend{
		Type: core.LLMInferenceAPITypeOpenAI, Endpoint: srv.URL() + "/v1", APIKeyEnvVar: testAPIKeyEnvVar,
	}, "openai:m")
	server := NewAgenticServer(layer, ":0", nil)

	body, err := json.Marshal(chatCompletionRequest{
		Model:    "cheap",
		Messages: []chatMessage{{Role: "user", Content: "hi"}},
		Stream:   true,
	})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerOrlaStage, "planning")

	resp := httptest.NewRecorder()
	server.mux.ServeHTTP(resp, req)

	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Equal(t, "text/event-stream", resp.Header().Get("Content-Type"))

	chunks, done := parseSSEChunks(t, resp.Body.String())
	assert.True(t, done, "stream did not end with [DONE]")
	require.NotEmpty(t, chunks)

	assert.Equal(t, "assistant", chunks[0].Choices[0].Delta.Role, "first chunk should announce role")

	var content strings.Builder
	for _, c := range chunks {
		if len(c.Choices) > 0 {
			content.WriteString(c.Choices[0].Delta.Content)
		}
	}
	assert.Equal(t, "hello from mock", content.String())

	last := chunks[len(chunks)-1]
	require.NotNil(t, last.Choices[0].FinishReason)
	assert.Equal(t, "stop", *last.Choices[0].FinishReason)
}

func TestExtractOrlaContext_HeadersAndTags(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set(headerOrlaStage, "planning")
	req.Header.Set(headerOrlaWorkflowRun, "run-abc")
	req.Header.Set(headerOrlaTagPrefix+"Tenant", "engineering")
	req.Header.Set(headerOrlaDataLabels, "pii,secret")

	stage, run, labels, tags := extractOrlaContext(req, &chatCompletionRequest{})
	assert.Equal(t, "planning", stage)
	assert.Equal(t, "run-abc", run)
	assert.Equal(t, []string{"pii", "secret"}, labels)
	assert.Equal(t, "engineering", tags["tenant"])
	assert.Equal(t, "planning", tags[tagKeyStage])
	assert.Equal(t, "run-abc", tags[tagKeyWorkflowRun])
}

func TestExtractOrlaContext_BodyFallback(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	body := &chatCompletionRequest{
		Metadata: &chatRequestMeta{Orla: &orlaMetaInBody{
			Stage:       "planning",
			WorkflowRun: "run-1",
			DataLabels:  []string{"pii"},
			Tags:        map[string]string{"tenant": "eng"},
		}},
	}
	stage, run, labels, tags := extractOrlaContext(req, body)
	assert.Equal(t, "planning", stage)
	assert.Equal(t, "run-1", run)
	assert.Equal(t, []string{"pii"}, labels)
	assert.Equal(t, "eng", tags["tenant"])
}

func TestExtractOrlaContext_HeaderWinsOverBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set(headerOrlaStage, "from-header")
	req.Header.Set(headerOrlaTagPrefix+"tenant", "header-tenant")

	body := &chatCompletionRequest{
		Metadata: &chatRequestMeta{Orla: &orlaMetaInBody{
			Stage: "from-body",
			Tags:  map[string]string{"tenant": "body-tenant"},
		}},
	}
	stage, _, _, tags := extractOrlaContext(req, body)
	assert.Equal(t, "from-header", stage)
	assert.Equal(t, "header-tenant", tags["tenant"])
}

func TestConvertToolsToInternal_Validation(t *testing.T) {
	_, err := convertToolsToInternal([]chatTool{{Type: "code_interpreter"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported type")

	_, err = convertToolsToInternal([]chatTool{{Type: "function", Function: chatToolFunction{}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestConvertMessagesToInternal_ToolCallsArgsParsed(t *testing.T) {
	out, err := convertMessagesToInternal([]chatMessage{
		{Role: "assistant", ToolCalls: []chatMessageToolCall{{
			ID:       "call_1",
			Type:     "function",
			Function: chatToolFunctionCall{Name: "search", Arguments: `{"q":"foo"}`},
		}}},
	})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].ToolCalls, 1)
	assert.Equal(t, "call_1", out[0].ToolCalls[0].ID)
	assert.Equal(t, "search", out[0].ToolCalls[0].McpCallToolParams.Name)
	args, ok := out[0].ToolCalls[0].McpCallToolParams.Arguments.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "foo", args["q"])
}

func TestConvertMessagesToInternal_UnsupportedRole(t *testing.T) {
	_, err := convertMessagesToInternal([]chatMessage{{Role: "developer", Content: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported role")
}

// parseSSEChunks parses an OpenAI-style data-only SSE stream into typed chunks
// and reports whether the stream terminated with "[DONE]".
func parseSSEChunks(t *testing.T, raw string) ([]chatCompletionChunk, bool) {
	t.Helper()
	var chunks []chatCompletionChunk
	done := false
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			done = true
			continue
		}
		var c chatCompletionChunk
		require.NoError(t, json.Unmarshal([]byte(payload), &c))
		chunks = append(chunks, c)
	}
	require.NoError(t, scanner.Err())
	return chunks, done
}
