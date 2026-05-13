package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/harvard-cns/orla/internal/core"
	"github.com/harvard-cns/orla/internal/model"
	"github.com/harvard-cns/orla/internal/serving"
	"log/slog"
)

const (
	headerOrlaStage       = "X-Orla-Stage"
	headerOrlaWorkflowRun = "X-Orla-Workflow-Run"
	headerOrlaDataLabels  = "X-Orla-Data-Labels"
	headerOrlaTagPrefix   = "X-Orla-Tag-"

	tagKeyStage       = "stage"
	tagKeyWorkflowRun = "workflow_run"
)

// chatCompletionRequest is the subset of the OpenAI chat completions request we accept.
// The `model` field carries the Orla backend name.
type chatCompletionRequest struct {
	Model       string              `json:"model"`
	Messages    []chatMessage       `json:"messages"`
	Tools       []chatTool          `json:"tools,omitempty"`
	Stream      bool                `json:"stream,omitempty"`
	MaxTokens   *int                `json:"max_tokens,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	TopP        *float64            `json:"top_p,omitempty"`
	Metadata    *chatRequestMeta    `json:"metadata,omitempty"`
	Response    *openAIResponseFmt  `json:"response_format,omitempty"`
}

type chatRequestMeta struct {
	Orla *orlaMetaInBody `json:"orla,omitempty"`
}

type orlaMetaInBody struct {
	Stage       string            `json:"stage,omitempty"`
	WorkflowRun string            `json:"workflow_run,omitempty"`
	DataLabels  []string          `json:"data_labels,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

type chatMessage struct {
	Role       string             `json:"role"`
	Content    string             `json:"content,omitempty"`
	Name       string             `json:"name,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
	ToolCalls  []chatMessageToolCall `json:"tool_calls,omitempty"`
}

type chatMessageToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function chatToolFunctionCall `json:"function"`
}

type chatToolFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string per OpenAI spec
}

type chatTool struct {
	Type     string            `json:"type"`
	Function chatToolFunction  `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type openAIResponseFmt struct {
	Type       string          `json:"type"`
	JSONSchema *openAIJSONSchema `json:"json_schema,omitempty"`
}

type openAIJSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict,omitempty"`
	Schema json.RawMessage `json:"schema"`
}

// chatCompletion is the non-streaming response shape.
type chatCompletion struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []chatChoice   `json:"choices"`
	Usage   chatUsage      `json:"usage"`
}

type chatChoice struct {
	Index        int          `json:"index"`
	Message      chatMessage  `json:"message"`
	FinishReason string       `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// chatCompletionChunk is the streaming chunk shape.
type chatCompletionChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []chatChunkChoice  `json:"choices"`
}

type chatChunkChoice struct {
	Index        int          `json:"index"`
	Delta        chatDelta    `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

type chatDelta struct {
	Role      string                  `json:"role,omitempty"`
	Content   string                  `json:"content,omitempty"`
	ToolCalls []chatMessageToolCall   `json:"tool_calls,omitempty"`
}

func (s *AgenticServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var req chatCompletionRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, fmt.Sprintf("failed to decode request: %v", err))
		return
	}
	if req.Model == "" {
		writeOpenAIError(w, http.StatusBadRequest, "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "messages is required")
		return
	}

	stage, workflowRun, dataLabels, tags := extractOrlaContext(r, &req)
	if stage == "" {
		writeOpenAIError(w, http.StatusBadRequest, "stage is required (set X-Orla-Stage header or metadata.orla.stage)")
		return
	}

	messages, err := convertMessagesToInternal(req.Messages)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	tools, err := convertToolsToInternal(req.Tools)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	opts := model.InferenceOptions{
		Stream:      req.Stream,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
	if rf := convertResponseFormat(req.Response); rf != nil {
		opts.ResponseFormat = rf
	}
	chatOpts := serving.ChatOptions{WorkflowID: workflowRun}

	// Effective data labels merge explicit labels with those inherited from
	// upstream stages in the registered workflow DAG.
	effectiveLabels := dataLabels
	if workflowRun != "" {
		labels, lerr := s.layer.WorkflowManager.EffectiveLabels(workflowRun, stage, dataLabels)
		if lerr != nil {
			writeOpenAIError(w, http.StatusBadRequest, lerr.Error())
			return
		}
		effectiveLabels = labels
	}

	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Name
	}
	if d := s.layer.ValidateAccess(tags, req.Model, toolNames, effectiveLabels); !d.Allowed {
		writeOpenAIError(w, http.StatusForbidden, d.Reason)
		return
	}

	ctx := r.Context()
	if req.Stream {
		s.streamChatCompletion(w, ctx, req.Model, stage, messages, tools, opts, chatOpts)
		return
	}

	response, err := s.layer.Execute(ctx, req.Model, stage, messages, tools, opts, chatOpts)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	completion := buildChatCompletion(req.Model, response)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	core.WriteJSONResponse(w, completion)
}

func (s *AgenticServer) streamChatCompletion(
	w http.ResponseWriter,
	ctx context.Context,
	backend, stage string,
	messages []model.Message,
	tools []*model.Tool,
	opts model.InferenceOptions,
	chatOpts serving.ChatOptions,
) {
	response, eventCh, err := s.layer.ExecuteStream(ctx, backend, stage, messages, tools, opts, chatOpts)
	if err != nil {
		writeOpenAIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("flusher not supported")
		return
	}

	id := newCompletionID()
	created := time.Now().Unix()

	writeOpenAISSE(w, flusher, chatCompletionChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: backend,
		Choices: []chatChunkChoice{{Index: 0, Delta: chatDelta{Role: "assistant"}}},
	})

	toolIdx := 0
	if eventCh != nil {
		for ev := range eventCh {
			switch e := ev.(type) {
			case *model.ContentEvent:
				writeOpenAISSE(w, flusher, chatCompletionChunk{
					ID: id, Object: "chat.completion.chunk", Created: created, Model: backend,
					Choices: []chatChunkChoice{{Index: 0, Delta: chatDelta{Content: e.Content}}},
				})
			case *model.ToolCallEvent:
				argsBytes, err := json.Marshal(e.Arguments)
				if err != nil {
					slog.Error("failed to marshal tool call arguments", "error", err)
					continue
				}
				idx := toolIdx
				toolIdx++
				writeOpenAISSE(w, flusher, chatCompletionChunk{
					ID: id, Object: "chat.completion.chunk", Created: created, Model: backend,
					Choices: []chatChunkChoice{{Index: 0, Delta: chatDelta{
						ToolCalls: []chatMessageToolCall{{
							ID:       fmt.Sprintf("call_%s_%d", id, idx),
							Type:     "function",
							Function: chatToolFunctionCall{Name: e.Name, Arguments: string(argsBytes)},
						}},
					}}},
				})
			}
		}
	}

	finish := finishReason(response)
	writeOpenAISSE(w, flusher, chatCompletionChunk{
		ID: id, Object: "chat.completion.chunk", Created: created, Model: backend,
		Choices: []chatChunkChoice{{Index: 0, Delta: chatDelta{}, FinishReason: &finish}},
	})
	if _, err := fmt.Fprint(w, "data: [DONE]\n\n"); err != nil {
		slog.Error("failed to write SSE DONE", "error", err)
	}
	flusher.Flush()
}

func extractOrlaContext(r *http.Request, req *chatCompletionRequest) (stage, workflowRun string, dataLabels []string, tags map[string]string) {
	tags = map[string]string{}

	stage = r.Header.Get(headerOrlaStage)
	workflowRun = r.Header.Get(headerOrlaWorkflowRun)
	if v := r.Header.Get(headerOrlaDataLabels); v != "" {
		for _, label := range strings.Split(v, ",") {
			if label = strings.TrimSpace(label); label != "" {
				dataLabels = append(dataLabels, label)
			}
		}
	}
	for name, values := range r.Header {
		if !strings.HasPrefix(name, headerOrlaTagPrefix) || len(values) == 0 {
			continue
		}
		key := strings.ToLower(strings.TrimPrefix(name, headerOrlaTagPrefix))
		if key == "" {
			continue
		}
		tags[key] = values[0]
	}

	if req.Metadata != nil && req.Metadata.Orla != nil {
		m := req.Metadata.Orla
		if stage == "" {
			stage = m.Stage
		}
		if workflowRun == "" {
			workflowRun = m.WorkflowRun
		}
		if len(dataLabels) == 0 {
			dataLabels = m.DataLabels
		}
		for k, v := range m.Tags {
			if _, exists := tags[k]; !exists {
				tags[k] = v
			}
		}
	}

	if stage != "" {
		tags[tagKeyStage] = stage
	}
	if workflowRun != "" {
		tags[tagKeyWorkflowRun] = workflowRun
	}
	return stage, workflowRun, dataLabels, tags
}

func convertMessagesToInternal(in []chatMessage) ([]model.Message, error) {
	out := make([]model.Message, 0, len(in))
	for i, m := range in {
		role := model.MessageRole(m.Role)
		switch role {
		case model.MessageRoleUser, model.MessageRoleAssistant, model.MessageRoleSystem, model.MessageRoleTool:
		default:
			return nil, fmt.Errorf("message[%d]: unsupported role %q", i, m.Role)
		}
		msg := model.Message{Role: role, Content: m.Content, ToolCallID: m.ToolCallID, ToolName: m.Name}
		for j, tc := range m.ToolCalls {
			var args json.RawMessage
			if tc.Function.Arguments != "" {
				if !json.Valid([]byte(tc.Function.Arguments)) {
					return nil, fmt.Errorf("message[%d].tool_calls[%d].function.arguments: invalid JSON", i, j)
				}
				args = json.RawMessage(tc.Function.Arguments)
			}
			msg.ToolCalls = append(msg.ToolCalls, model.ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
		out = append(out, msg)
	}
	return out, nil
}

func convertToolsToInternal(in []chatTool) ([]*model.Tool, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]*model.Tool, 0, len(in))
	for i, t := range in {
		if t.Type != "" && t.Type != "function" {
			return nil, fmt.Errorf("tools[%d]: unsupported type %q (only \"function\" is supported)", i, t.Type)
		}
		if t.Function.Name == "" {
			return nil, fmt.Errorf("tools[%d].function.name is required", i)
		}
		tool := &model.Tool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
		}
		if len(t.Function.Parameters) > 0 {
			if !json.Valid(t.Function.Parameters) {
				return nil, fmt.Errorf("tools[%d].function.parameters: invalid JSON", i)
			}
			tool.Parameters = t.Function.Parameters
		}
		out = append(out, tool)
	}
	return out, nil
}

func convertResponseFormat(rf *openAIResponseFmt) *model.StructuredOutputOptions {
	if rf == nil || rf.JSONSchema == nil {
		return nil
	}
	return &model.StructuredOutputOptions{
		Name:   rf.JSONSchema.Name,
		Strict: rf.JSONSchema.Strict,
		Schema: rf.JSONSchema.Schema,
	}
}

func buildChatCompletion(backend string, resp *model.Response) chatCompletion {
	msg := chatMessage{Role: "assistant", Content: resp.Content}
	for _, tc := range resp.ToolCalls {
		args := string(tc.Arguments)
		if args == "" {
			args = "{}"
		}
		msg.ToolCalls = append(msg.ToolCalls, chatMessageToolCall{
			ID:       tc.ID,
			Type:     "function",
			Function: chatToolFunctionCall{Name: tc.Name, Arguments: args},
		})
	}
	completion := chatCompletion{
		ID:      newCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   backend,
		Choices: []chatChoice{{Index: 0, Message: msg, FinishReason: finishReason(resp)}},
	}
	if resp.Metrics != nil {
		completion.Usage = chatUsage{
			PromptTokens:     resp.Metrics.PromptTokens,
			CompletionTokens: resp.Metrics.CompletionTokens,
			TotalTokens:      resp.Metrics.PromptTokens + resp.Metrics.CompletionTokens,
		}
	}
	return completion
}

func finishReason(resp *model.Response) string {
	if resp != nil && len(resp.ToolCalls) > 0 {
		return "tool_calls"
	}
	return "stop"
}

func newCompletionID() string {
	return "chatcmpl-" + uuid.NewString()
}

func writeOpenAISSE(w http.ResponseWriter, flusher http.Flusher, chunk chatCompletionChunk) {
	payload, err := json.Marshal(chunk)
	if err != nil {
		slog.Error("failed to marshal chat chunk", "error", err)
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
		slog.Error("failed to write chat chunk", "error", err)
		return
	}
	if flusher != nil {
		flusher.Flush()
	}
}

func writeOpenAIError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	core.WriteJSONResponse(w, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    httpStatusToOpenAIErrorType(status),
		},
	})
}

func httpStatusToOpenAIErrorType(status int) string {
	switch {
	case status == http.StatusBadRequest:
		return "invalid_request_error"
	case status == http.StatusForbidden:
		return "permission_denied"
	case status == http.StatusTooManyRequests:
		return "rate_limit_exceeded"
	case status >= 500:
		return "server_error"
	default:
		return "api_error"
	}
}
