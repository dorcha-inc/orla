package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/packages/respjson"
	"github.com/openai/openai-go/shared"
	"go.uber.org/zap"

	"github.com/harvard-cns/orla/internal/core"
)

const defaultStreamBufferSize = 255

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
// This provider is intended to work with any server that implements the OpenAI Chat Completions API format
// such as LM Studio, vLLM, and Ollama. For Ollama, use endpoint http://host:11434/v1.
type OpenAIProvider struct {
	modelName  string
	client     openai.Client
	llmBackend *core.LLMBackend
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
func NewOpenAIProvider(modelName string, llmBackend *core.LLMBackend) (*OpenAIProvider, error) {
	baseURL, apiKey, err := getOpenAICompatibleEndpoint(llmBackend)
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenAI-compatible endpoint: %w", err)
	}
	opts := []option.RequestOption{option.WithBaseURL(baseURL)}
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	}
	return &OpenAIProvider{
		modelName:  modelName,
		client:     openai.NewClient(opts...),
		llmBackend: llmBackend,
	}, nil
}

// getOpenAICompatibleEndpoint determines the OpenAI-compatible endpoint URL and API key.
func getOpenAICompatibleEndpoint(llmBackend *core.LLMBackend) (string, string, error) {
	if llmBackend == nil {
		return "", "", fmt.Errorf("llm_backend configuration is required for OpenAI-compatible API")
	}
	if llmBackend.Endpoint == "" {
		return "", "", fmt.Errorf("llm_backend.endpoint is required")
	}
	if llmBackend.Type == "" {
		return "", "", fmt.Errorf("llm_backend.type is required if llm_backend is set")
	}
	switch llmBackend.Type {
	case core.LLMInferenceAPITypeOpenAI, core.LLMInferenceAPITypeSGLang:
		// Both expose an OpenAI-compatible /v1/chat/completions endpoint.
	default:
		return "", "", fmt.Errorf("[BUG] llm_backend.type must be %s or %s, got '%s': we should not be using this function for non-openai-compatible inference servers", core.LLMInferenceAPITypeOpenAI, core.LLMInferenceAPITypeSGLang, llmBackend.Type)
	}
	if llmBackend.APIKeyEnvVar == "" {
		zap.L().Debug("llm_backend.api_key_env_var is not set, skipping authentication for OpenAI-compatible API",
			zap.String("llm_backend.endpoint", llmBackend.Endpoint))
		return llmBackend.Endpoint, "", nil
	}
	apiKey := core.GetEnv(llmBackend.APIKeyEnvVar)
	if apiKey == "" {
		return "", "", fmt.Errorf("API key is required for OpenAI-compatible API at %s. Configure llm_backend.api_key_env_var", llmBackend.Endpoint)
	}
	return llmBackend.Endpoint, apiKey, nil
}

// Name returns the provider name.
func (p *OpenAIProvider) Name() string { return "openai" }

// EnsureReady is a no-op for the OpenAI-compatible provider.
func (p *OpenAIProvider) EnsureReady(ctx context.Context) error { return nil }

// Chat sends a chat request to the OpenAI-compatible API.
func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message, tools []*Tool, opts InferenceOptions) (*Response, <-chan StreamEvent, error) {
	if err := p.EnsureReady(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to ensure OpenAI-compatible API is ready: %w", err)
	}

	convertedMessages, err := convertMessagesToOpenAI(messages)
	if err != nil {
		return nil, nil, err
	}
	convertedTools, err := convertToolsToOpenAIParams(tools)
	if err != nil {
		return nil, nil, err
	}

	params := openai.ChatCompletionNewParams{
		Model:    p.modelName,
		Messages: convertedMessages,
	}
	if len(convertedTools) > 0 {
		params.Tools = convertedTools
		params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("auto"),
		}
	}
	if opts.MaxTokens != nil {
		params.MaxTokens = param.NewOpt(int64(*opts.MaxTokens))
	}
	if opts.Temperature != nil {
		params.Temperature = param.NewOpt(*opts.Temperature)
	}
	if opts.TopP != nil {
		params.TopP = param.NewOpt(*opts.TopP)
	}
	if opts.ReasoningEffort != "" {
		params.ReasoningEffort = shared.ReasoningEffort(opts.ReasoningEffort)
	}
	if opts.ResponseFormat != nil {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   opts.ResponseFormat.Name,
					Strict: param.NewOpt(opts.ResponseFormat.Strict),
					Schema: opts.ResponseFormat.Schema,
				},
			},
		}
	}

	var requestOpts []option.RequestOption
	if len(opts.ChatTemplateKwargs) > 0 {
		// Vendor extension (SGLang/vLLM). Inject as an extra body field.
		requestOpts = append(requestOpts, option.WithJSONSet("chat_template_kwargs", opts.ChatTemplateKwargs))
	}

	if opts.Stream {
		params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		}
		return p.handleStreaming(ctx, params, requestOpts)
	}
	return p.handleNonStreaming(ctx, params, requestOpts)
}

func (p *OpenAIProvider) handleNonStreaming(ctx context.Context, params openai.ChatCompletionNewParams, requestOpts []option.RequestOption) (*Response, <-chan StreamEvent, error) {
	completion, err := p.client.Chat.Completions.New(ctx, params, requestOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenAI-compatible API error: %w", err)
	}
	if len(completion.Choices) == 0 {
		return nil, nil, fmt.Errorf("OpenAI-compatible API returned no choices")
	}

	choice := completion.Choices[0]
	response := &Response{
		Content:  choice.Message.Content,
		Thinking: extractReasoningContent(choice.Message.JSON.ExtraFields),
		Metrics: &ResponseMetrics{
			PromptTokens:     int(completion.Usage.PromptTokens),
			CompletionTokens: int(completion.Usage.CompletionTokens),
		},
	}

	if len(choice.Message.ToolCalls) > 0 {
		toolCalls, err := convertOpenAIToolCalls(choice.Message.ToolCalls)
		if err != nil {
			return nil, nil, err
		}
		response.ToolCalls = toolCalls
		zap.L().Debug("Parsed tool calls", zap.Int("count", len(toolCalls)))
	}

	return response, nil, nil
}

func (p *OpenAIProvider) handleStreaming(ctx context.Context, params openai.ChatCompletionNewParams, requestOpts []option.RequestOption) (*Response, <-chan StreamEvent, error) {
	stream := p.client.Chat.Completions.NewStreaming(ctx, params, requestOpts...)

	ch := make(chan StreamEvent, defaultStreamBufferSize)
	response := &Response{
		Content:   "",
		Thinking:  "",
		ToolCalls: []ToolCall{},
	}

	go func() {
		defer close(ch)
		defer core.LogDeferredError(stream.Close)

		start := time.Now()
		var firstContentAt, lastContentAt time.Time
		var promptTokens, completionTokens int64
		// Tool calls arrive incrementally indexed by delta.Index; accumulate by index.
		type accCall struct {
			id   string
			name string
			args string
		}
		var accumulated []accCall

		for stream.Next() {
			chunk := stream.Current()

			if chunk.Usage.PromptTokens > 0 {
				promptTokens = chunk.Usage.PromptTokens
			}
			if chunk.Usage.CompletionTokens > 0 {
				completionTokens = chunk.Usage.CompletionTokens
			}

			if len(chunk.Choices) == 0 {
				continue
			}
			delta := chunk.Choices[0].Delta

			if delta.Content != "" {
				response.Content += delta.Content
				now := time.Now()
				if firstContentAt.IsZero() {
					firstContentAt = now
				}
				lastContentAt = now
				ch <- &ContentEvent{Content: delta.Content}
			}

			// reasoning_content is a vendor extension (Ollama, DeepSeek). Read from extra fields.
			if reasoning := extractDeltaReasoning(delta.JSON.ExtraFields); reasoning != "" {
				response.Thinking += reasoning
				ch <- &ThinkingEvent{Content: reasoning}
			}

			for _, tcDelta := range delta.ToolCalls {
				idx := int(tcDelta.Index)
				if idx < 0 {
					zap.L().Error("Invalid tool call index (negative)", zap.Int("index", idx))
					continue
				}
				for len(accumulated) <= idx {
					accumulated = append(accumulated, accCall{})
				}
				if tcDelta.ID != "" {
					accumulated[idx].id = tcDelta.ID
				}
				if tcDelta.Function.Name != "" {
					accumulated[idx].name = tcDelta.Function.Name
				}
				if tcDelta.Function.Arguments != "" {
					accumulated[idx].args += tcDelta.Function.Arguments
				}
			}
		}
		if err := stream.Err(); err != nil && !errors.Is(err, io.EOF) {
			zap.L().Error("Error reading stream", zap.Error(err))
		}

		// Finalize tool calls.
		if len(accumulated) > 0 {
			response.ToolCalls = make([]ToolCall, 0, len(accumulated))
			for i, c := range accumulated {
				if c.id == "" {
					zap.L().Error("stream tool call missing id", zap.Int("index", i), zap.String("name", c.name))
					continue
				}
				tc := ToolCall{ID: c.id, Name: c.name}
				if c.args != "" {
					tc.Arguments = json.RawMessage(c.args)
				}
				response.ToolCalls = append(response.ToolCalls, tc)
			}
		}

		// Metrics.
		response.Metrics = &ResponseMetrics{
			PromptTokens:     int(promptTokens),
			CompletionTokens: int(completionTokens),
		}
		if !firstContentAt.IsZero() {
			ttft := firstContentAt.Sub(start).Milliseconds()
			response.Metrics.TTFTMs = &ttft
		}
		if completionTokens > 0 && !firstContentAt.IsZero() && !lastContentAt.IsZero() {
			decodeMs := lastContentAt.Sub(firstContentAt).Milliseconds()
			tpot := decodeMs / completionTokens
			if tpot == 0 && decodeMs > 0 {
				tpot = 1
			}
			response.Metrics.TPOTMs = &tpot
		}
	}()

	return response, ch, nil
}

// convertMessagesToOpenAI maps internal Messages into openai-go param unions.
func convertMessagesToOpenAI(messages []Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for i, msg := range messages {
		switch msg.Role {
		case MessageRoleUser:
			out = append(out, openai.UserMessage(msg.Content))
		case MessageRoleSystem:
			out = append(out, openai.SystemMessage(msg.Content))
		case MessageRoleTool:
			if msg.ToolCallID == "" {
				zap.L().Warn("Tool message missing ToolCallID", zap.String("tool_name", msg.ToolName))
			}
			out = append(out, openai.ToolMessage(msg.Content, msg.ToolCallID))
		case MessageRoleAssistant:
			assistant := openai.ChatCompletionAssistantMessageParam{}
			if msg.Content != "" {
				assistant.Content.OfString = param.NewOpt(msg.Content)
			}
			for _, tc := range msg.ToolCalls {
				args := string(tc.Arguments)
				if args == "" {
					args = "{}"
				}
				assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallParam{
					ID: tc.ID,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      tc.Name,
						Arguments: args,
					},
				})
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		default:
			return nil, fmt.Errorf("message[%d]: unsupported role %q", i, msg.Role)
		}
	}
	return out, nil
}

// convertToolsToOpenAIParams converts model.Tool slice into openai.ChatCompletionToolParam.
// Parameters (JSON Schema) is unmarshaled once into FunctionParameters (map[string]any)
// because that's what the openai-go param type requires.
func convertToolsToOpenAIParams(tools []*Tool) ([]openai.ChatCompletionToolParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openai.ChatCompletionToolParam, len(tools))
	for i, t := range tools {
		fn := shared.FunctionDefinitionParam{Name: t.Name}
		if t.Description != "" {
			fn.Description = param.NewOpt(t.Description)
		}
		if len(t.Parameters) > 0 {
			var schema shared.FunctionParameters
			if err := json.Unmarshal(t.Parameters, &schema); err != nil {
				return nil, fmt.Errorf("tool %q: unmarshal parameters: %w", t.Name, err)
			}
			fn.Parameters = schema
		}
		out[i] = openai.ChatCompletionToolParam{Function: fn}
	}
	return out, nil
}

// convertOpenAIToolCalls translates non-streaming tool calls into model.ToolCall.
func convertOpenAIToolCalls(calls []openai.ChatCompletionMessageToolCall) ([]ToolCall, error) {
	out := make([]ToolCall, len(calls))
	for i, c := range calls {
		if c.ID == "" {
			return nil, fmt.Errorf("tool call at index %d has empty id (tool %s)", i, c.Function.Name)
		}
		tc := ToolCall{ID: c.ID, Name: c.Function.Name}
		if c.Function.Arguments != "" {
			tc.Arguments = json.RawMessage(c.Function.Arguments)
		}
		out[i] = tc
	}
	return out, nil
}

// extractReasoningContent pulls "reasoning_content" from a message's extra fields
// (Ollama, DeepSeek vendor extension; not part of standard OpenAI).
func extractReasoningContent(extra map[string]respjson.Field) string {
	return extractExtraField(extra, "reasoning_content")
}

// extractDeltaReasoning is the streaming counterpart for chunk deltas.
func extractDeltaReasoning(extra map[string]respjson.Field) string {
	return extractExtraField(extra, "reasoning_content")
}

func extractExtraField(extra map[string]respjson.Field, key string) string {
	f, ok := extra[key]
	if !ok || !f.Valid() {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(f.Raw()), &s); err != nil {
		return ""
	}
	return s
}
