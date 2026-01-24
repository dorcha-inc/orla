package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"github.com/dorcha-inc/orla/internal/config"
	"github.com/dorcha-inc/orla/internal/core"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// NOTE(jadidbourbaki): I found the official OpenAI SDK client to be not very user friendly and kind of too bloated for our use case.
// So we're going to use the go-openai library to make requests to the OpenAI-compatible API.

// OpenAIProvider implements the Provider interface for OpenAI-compatible APIs.
// This provider is intended to work with any server that implements the OpenAI Chat Completions API format
// such as LM Studio, vLLM, and even ollama (even though we have a separate Ollama provider). For ollama,
// this goes through Ollama's Open-AI compatible API [1].
// [1] https://docs.ollama.com/api/openai-compatibility
type OpenAIProvider struct {
	modelName string
	client    *openai.Client
	cfg       *config.OrlaConfig
}

// NewOpenAIProvider creates a new OpenAI-compatible provider.
// This works with any server that implements the OpenAI Chat Completions API format.
func NewOpenAIProvider(modelName string, cfg *config.OrlaConfig) (*OpenAIProvider, error) {
	baseURL, apiKey, err := getOpenAICompatibleEndpoint(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenAI-compatible endpoint: %w", err)
	}

	// Configure the OpenAI client with custom base URL
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL

	// Create OpenAI client
	client := openai.NewClientWithConfig(config)

	return &OpenAIProvider{
		modelName: modelName,
		client:    client,
		cfg:       cfg,
	}, nil
}

// getOpenAICompatibleEndpoint determines the OpenAI-compatible endpoint URL and API key.
func getOpenAICompatibleEndpoint(cfg *config.OrlaConfig) (string, string, error) {
	if cfg == nil || cfg.LLMBackend == nil {
		return "", "", fmt.Errorf("llm_backend configuration is required for OpenAI-compatible API")
	}

	if cfg.LLMBackend.Endpoint == "" {
		return "", "", fmt.Errorf("llm_backend.endpoint is required")
	}

	if cfg.LLMBackend.Type == "" {
		return "", "", fmt.Errorf("llm_backend.type is required if llm_backend is set")
	}

	if cfg.LLMBackend.Type != core.LLMInferenceAPITypeOpenAI {
		return "", "", fmt.Errorf("[BUG] llm_backend.type must be %s, got '%s': we should not be using this function for non-openai inference servers", core.LLMInferenceAPITypeOpenAI, cfg.LLMBackend.Type)
	}

	apiKey := core.GetEnv(cfg.LLMBackend.APIKeyEnvVar)
	if apiKey == "" {
		return "", "", fmt.Errorf("API key is required for OpenAI-compatible API at %s. Configure llm_backend.api_key_env_var", cfg.LLMBackend.Endpoint)
	}

	return cfg.LLMBackend.Endpoint, apiKey, nil
}

// EnsureReady is a no-op for the OpenAI-compatible provider.
func (p *OpenAIProvider) EnsureReady(ctx context.Context) error {
	return nil
}

// Chat sends a chat request to the OpenAI-compatible API. This works with any server implementing the OpenAI Chat Completions API format.
func (p *OpenAIProvider) Chat(ctx context.Context, messages []Message, tools []*mcp.Tool, stream bool) (*Response, <-chan StreamEvent, error) {
	// Ensure the OpenAI-compatible API is ready. This is a no-op for the OpenAI-compatible provider, but
	// might as well check anyway in case we add health checks in the future.
	readyErr := p.EnsureReady(ctx)
	if readyErr != nil {
		return nil, nil, fmt.Errorf("failed to ensure OpenAI-compatible API is ready: %w", readyErr)
	}

	// Convert messages to go-openai format
	openAIMessages := make([]openai.ChatCompletionMessage, len(messages))
	for i, msg := range messages {
		openAIMessages[i] = convertMessageToOpenAI(msg)
	}

	// Build request
	req := openai.ChatCompletionRequest{
		Model:    p.modelName,
		Messages: openAIMessages,
		Stream:   stream,
	}

	// Add tools if provided
	if len(tools) > 0 {
		openAITools, err := convertToolsToOpenAIFormat(tools)
		if err != nil {
			return nil, nil, err
		}
		req.Tools = openAITools
		req.ToolChoice = "auto" // Let the model decide when to use tools
	}

	if stream {
		return p.handleStreamingChat(ctx, req)
	}

	return p.handleNonStreamingChat(ctx, req)
}

// handleNonStreamingChat handles non-streaming chat requests
func (p *OpenAIProvider) handleNonStreamingChat(ctx context.Context, req openai.ChatCompletionRequest) (*Response, <-chan StreamEvent, error) {
	completion, err := p.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenAI-compatible API error: %w", err)
	}

	if len(completion.Choices) == 0 {
		return nil, nil, fmt.Errorf("OpenAI-compatible API returned no choices")
	}

	choice := completion.Choices[0]
	response := &Response{
		Content: choice.Message.Content,
	}

	// Parse tool calls if present
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

// handleStreamingChat handles streaming chat requests
func (p *OpenAIProvider) handleStreamingChat(ctx context.Context, req openai.ChatCompletionRequest) (*Response, <-chan StreamEvent, error) {
	stream, err := p.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenAI-compatible API error: %w", err)
	}

	ch := make(chan StreamEvent, defaultStreamBufferSize)
	response := &Response{
		Content:   "",
		Thinking:  "",
		ToolCalls: []ToolCallWithID{},
	}

	go func() {
		defer close(ch)
		defer core.LogDeferredError(stream.Close)

		var accumulatedToolCalls []openai.ToolCall
		chunkCount := 0
		contentChunks := 0

		for {
			chunk, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				zap.L().Error("Error reading stream", zap.Error(err))
				break
			}

			chunkCount++

			if len(chunk.Choices) == 0 {
				continue
			}

			delta := chunk.Choices[0].Delta

			// Accumulate content
			if delta.Content != "" {
				response.Content += delta.Content
				contentChunks++
				ch <- &ContentEvent{Content: delta.Content}
			}

			// Accumulate tool calls if present
			if len(delta.ToolCalls) > 0 {
				// OpenAI sends tool calls incrementally, we need to accumulate them
				for _, toolCallDelta := range delta.ToolCalls {
					if toolCallDelta.Index == nil {
						continue
					}

					// Index is a pointer, so we need to dereference it
					index := *toolCallDelta.Index
					if index < 0 {
						zap.L().Error("Invalid tool call index (negative)", zap.Int("index", index), zap.Any("toolCallDelta", toolCallDelta))
						continue
					}

					// Expand slice if needed for this index
					if index >= len(accumulatedToolCalls) {
						for len(accumulatedToolCalls) <= index {
							idx := len(accumulatedToolCalls)
							accumulatedToolCalls = append(accumulatedToolCalls, openai.ToolCall{
								Index: &idx,
								ID:    "",
								Type:  openai.ToolTypeFunction,
								Function: openai.FunctionCall{
									Name:      "",
									Arguments: "",
								},
							})
						}
					}

					// Accumulate tool call data
					if toolCallDelta.ID != "" {
						accumulatedToolCalls[index].ID = toolCallDelta.ID
					}
					if toolCallDelta.Type != "" {
						accumulatedToolCalls[index].Type = toolCallDelta.Type
					}
					if toolCallDelta.Function.Name != "" {
						accumulatedToolCalls[index].Function.Name = toolCallDelta.Function.Name
					}
					if toolCallDelta.Function.Arguments != "" {
						accumulatedToolCalls[index].Function.Arguments += toolCallDelta.Function.Arguments
					}
				}
			}
		}

		// Convert accumulated tool calls to our format (after stream completes)
		if len(accumulatedToolCalls) > 0 {
			toolCalls, err := convertOpenAIToolCallsFromStream(accumulatedToolCalls)
			if err != nil {
				zap.L().Error("Failed to convert tool calls from stream", zap.Error(err))
			}
			response.ToolCalls = toolCalls
		}
	}()

	return response, ch, nil
}

// convertMessageToOpenAI converts our Message type to go-openai format
func convertMessageToOpenAI(msg Message) openai.ChatCompletionMessage {
	result := openai.ChatCompletionMessage{
		Role:    string(msg.Role),
		Content: msg.Content,
	}

	// Handle tool messages (they need a ToolCallID for OpenAI API)
	if msg.Role == MessageRoleTool {
		result.Role = openai.ChatMessageRoleTool
		if msg.ToolCallID != "" {
			result.ToolCallID = msg.ToolCallID
		} else {
			// Log a warning if ToolCallID is missing - this might cause issues with some servers
			zap.L().Warn("Tool message missing ToolCallID",
				zap.String("tool_name", msg.ToolName))
		}
	}

	return result
}

// normalizeSchemaToMap converts the InputSchema of a tool to a map[string]any, or returns an error if it fails
func normalizeSchemaToMap(tool *mcp.Tool) (map[string]any, error) {
	if tool.InputSchema == nil {
		return nil, fmt.Errorf("tool input schema is nil for tool %s", tool.Name)
	}

	schemaMap, ok := tool.InputSchema.(map[string]any)
	if ok {
		return schemaMap, nil
	}

	var schemaBytes []byte

	switch s := tool.InputSchema.(type) {
	case json.RawMessage:
		schemaBytes = s
	case []byte:
		schemaBytes = s
	default:
		b, jsonErr := json.Marshal(tool.InputSchema)
		if jsonErr != nil {
			return nil, fmt.Errorf("failed to marshal Tool InputSchema for tool %s: %w", tool.Name, jsonErr)
		}
		schemaBytes = b
	}

	err := json.Unmarshal(schemaBytes, &schemaMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal Tool InputSchema for tool %s into map: %w", tool.Name, err)
	}

	return schemaMap, nil
}

// convertToolsToOpenAIFormat converts mcp.Tool slice to go-openai format
func convertToolsToOpenAIFormat(tools []*mcp.Tool) ([]openai.Tool, error) {
	openAITools := make([]openai.Tool, len(tools))

	for i, tool := range tools {
		schemaMap, err := normalizeSchemaToMap(tool)
		if err != nil {
			return nil, fmt.Errorf("failed to normalize tool input schema for tool %s: %w", tool.Name, err)
		}

		openAITools[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  schemaMap,
			},
		}
	}
	return openAITools, nil
}

// convertOpenAiToolCall converts a go-openai tool call to orla's format.
// idx is used as a fallback to generate a stable ID when OpenAI doesn't provide one.
func convertOpenAiToolCall(call openai.ToolCall, idx int) (*ToolCallWithID, error) {
	args := make(map[string]any)

	// Parse arguments JSON string
	if call.Function.Arguments != "" {
		jsonErr := json.Unmarshal([]byte(call.Function.Arguments), &args)
		if jsonErr != nil {
			return nil, fmt.Errorf("failed to unmarshal tool call arguments for tool %s: %w", call.Function.Name, jsonErr)
		}
	}

	// Use OpenAI's ID if provided, otherwise use index
	id := call.ID
	if id == "" {
		id = fmt.Sprintf("call_%d", idx)
	}

	return &ToolCallWithID{
		ID: id,
		McpCallToolParams: mcp.CallToolParams{
			Name:      call.Function.Name,
			Arguments: args,
		},
	}, nil
}

// convertOpenAIToolCalls converts go-openai tool calls to orla's format (from non-streaming response)
func convertOpenAIToolCalls(openAICalls []openai.ToolCall) ([]ToolCallWithID, error) {
	toolCalls := make([]ToolCallWithID, len(openAICalls))

	for i, call := range openAICalls {
		toolCall, err := convertOpenAiToolCall(call, i)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool call for tool %s: %w", call.Function.Name, err)
		}
		toolCalls[i] = *toolCall
	}

	return toolCalls, nil
}

// convertOpenAIToolCallsFromStream converts go-openai tool calls from stream to our format
func convertOpenAIToolCallsFromStream(openAICalls []openai.ToolCall) ([]ToolCallWithID, error) {
	toolCalls := make([]ToolCallWithID, 0, len(openAICalls))
	var errs []error

	for i, call := range openAICalls {
		toolCall, err := convertOpenAiToolCall(call, i)
		if err != nil {
			// Best-effort: keep other tool calls; surface error for logging.
			errs = append(errs, fmt.Errorf("failed to convert tool call for tool %s: %w", call.Function.Name, err))
			continue
		}
		toolCalls = append(toolCalls, *toolCall)
	}

	return toolCalls, errors.Join(errs...)
}
