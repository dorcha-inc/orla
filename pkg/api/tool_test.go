package orla

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTool_Success(t *testing.T) {
	t.Parallel()
	run := func(ctx context.Context, input ToolSchema) (*ToolResult, error) {
		return &ToolResult{OutputValues: input}, nil
	}
	tool, err := NewTool("t1", "desc", ToolSchema{"x": "string"}, ToolSchema{"y": "string"}, run)
	require.NoError(t, err)
	require.NotNil(t, tool)
	assert.Equal(t, "t1", tool.Name)
	assert.Equal(t, "desc", tool.Description)
	assert.Equal(t, ToolSchema{"x": "string"}, tool.InputSchema)
	assert.Equal(t, ToolSchema{"y": "string"}, tool.OutputSchema)
	assert.NotNil(t, tool.Run)
}

func TestNewTool_NilRunner(t *testing.T) {
	t.Parallel()
	tool, err := NewTool("t1", "desc", nil, nil, nil)
	require.Error(t, err)
	assert.Nil(t, tool)
	assert.Contains(t, err.Error(), "runner cannot be nil")
}

func TestToolRunnerFromSchema_Success(t *testing.T) {
	t.Parallel()
	fn := func(ctx context.Context, input ToolSchema) (ToolSchema, error) {
		return ToolSchema{"out": input["in"]}, nil
	}
	runner := ToolRunnerFromSchema(fn)
	result, err := runner(context.Background(), ToolSchema{"in": "val"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Equal(t, ToolSchema{"out": "val"}, result.OutputValues)
}

func TestToolRunnerFromSchema_ErrorSetsIsError(t *testing.T) {
	t.Parallel()
	fn := func(ctx context.Context, input ToolSchema) (ToolSchema, error) {
		return nil, errors.New("tool failed")
	}
	runner := ToolRunnerFromSchema(fn)
	result, err := runner(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.IsError)
	assert.Equal(t, "tool failed", result.Error)
}

func TestToolRunnerFromSchema_NilOutBecomesEmpty(t *testing.T) {
	t.Parallel()
	fn := func(ctx context.Context, input ToolSchema) (ToolSchema, error) {
		return nil, nil
	}
	runner := ToolRunnerFromSchema(fn)
	result, err := runner(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.NotNil(t, result.OutputValues)
	assert.Empty(t, result.OutputValues)
}

func TestTool_toWire(t *testing.T) {
	t.Parallel()
	tool, err := NewTool("a_tool", "A desc", ToolSchema{"type": "object"}, nil, func(ctx context.Context, in ToolSchema) (*ToolResult, error) {
		return &ToolResult{OutputValues: in}, nil
	})
	require.NoError(t, err)
	wire, err := tool.toWire()
	require.NoError(t, err)
	require.NotNil(t, wire)
	assert.Equal(t, "a_tool", wire.Name)
	assert.Equal(t, "A desc", wire.Description)
	assert.JSONEq(t, `{"type":"object"}`, string(wire.Parameters))
}

func TestNewToolCallFromRawToolCall_Valid(t *testing.T) {
	t.Parallel()
	// Wire shape: id, name, arguments (JSON-encoded).
	raw := RawToolCall(`{"id":"call_1","name":"my_tool","arguments":{"key":"value"}}`)
	tc, err := NewToolCallFromRawToolCall(raw)
	require.NoError(t, err)
	require.NotNil(t, tc)
	assert.Equal(t, "call_1", tc.ID)
	assert.Equal(t, "my_tool", tc.Name)
	assert.Equal(t, ToolSchema{"key": "value"}, tc.InputArguments)
}

func TestNewToolCallFromRawToolCall_EmptyArguments(t *testing.T) {
	t.Parallel()
	raw := RawToolCall(`{"id":"call_2","name":"no_args","arguments":{}}`)
	tc, err := NewToolCallFromRawToolCall(raw)
	require.NoError(t, err)
	require.NotNil(t, tc)
	assert.Equal(t, "call_2", tc.ID)
	assert.Equal(t, "no_args", tc.Name)
	assert.NotNil(t, tc.InputArguments)
	assert.Empty(t, tc.InputArguments)
}

func TestNewToolCallFromRawToolCall_EmptyRaw(t *testing.T) {
	t.Parallel()
	raw := RawToolCall(nil)
	tc, err := NewToolCallFromRawToolCall(raw)
	require.Error(t, err)
	assert.Nil(t, tc)
	assert.Contains(t, err.Error(), "empty")
}

func TestNewToolCallFromRawToolCall_EmptySlice(t *testing.T) {
	t.Parallel()
	raw := RawToolCall([]byte{})
	tc, err := NewToolCallFromRawToolCall(raw)
	require.Error(t, err)
	assert.Nil(t, tc)
}

func TestNewToolCallFromRawToolCall_InvalidJSON(t *testing.T) {
	t.Parallel()
	raw := RawToolCall(`{invalid}`)
	tc, err := NewToolCallFromRawToolCall(raw)
	require.Error(t, err)
	assert.Nil(t, tc)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestToolResult_ToMessage_Success(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		ID:           "id1",
		Name:         "tool1",
		OutputValues: ToolSchema{"result": "ok"},
	}
	msg, err := r.ToMessage()
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "tool", msg.Role)
	assert.Equal(t, "id1", msg.ToolCallID)
	assert.Equal(t, "tool1", msg.ToolName)
	assert.Equal(t, `{"result":"ok"}`, msg.Content)
}

func TestToolResult_ToMessage_IsError(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		ID:      "id1",
		Name:    "tool1",
		IsError: true,
		Error:   "something broke",
	}
	msg, err := r.ToMessage()
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "tool", msg.Role)
	assert.Equal(t, "tool execution error: something broke", msg.Content)
}

func TestToolResult_ToMessage_IsErrorNoMessage(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		ID:      "id1",
		Name:    "tool1",
		IsError: true,
	}
	msg, err := r.ToMessage()
	require.NoError(t, err)
	require.NotNil(t, msg)
	assert.Equal(t, "tool execution error", msg.Content)
}

func TestToolResult_ToMessage_EmptyOutputValuesError(t *testing.T) {
	t.Parallel()
	r := ToolResult{
		ID:           "id1",
		Name:         "tool1",
		OutputValues: ToolSchema{},
	}
	msg, err := r.ToMessage()
	require.Error(t, err)
	assert.Nil(t, msg)
	assert.Contains(t, err.Error(), "output values are empty")
}
