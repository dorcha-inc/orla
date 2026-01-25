package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRuntimeMode_String(t *testing.T) {
	assert.Equal(t, "simple", string(RuntimeModeSimple))
	assert.Equal(t, "capsule", string(RuntimeModeCapsule))
}

func TestHotLoadMode_String(t *testing.T) {
	assert.Equal(t, "restart", string(HotLoadModeRestart))
}

func TestLLMInferenceAPIType_String(t *testing.T) {
	assert.Equal(t, "ollama", string(LLMInferenceAPITypeOllama))
	assert.Equal(t, "openai", string(LLMInferenceAPITypeOpenAI))
	assert.Equal(t, "sglang", string(LLMInferenceAPITypeSGLang))
}

func TestHotLoadConfig_Empty(t *testing.T) {
	config := &HotLoadConfig{}
	assert.Empty(t, config.Watch)
	assert.Empty(t, config.Mode)
	assert.Zero(t, config.DebounceMs)
}

func TestHotLoadConfig_WithValues(t *testing.T) {
	config := &HotLoadConfig{
		Watch:      []string{"file1.go", "file2.go"},
		Mode:       HotLoadModeRestart,
		DebounceMs: 100,
	}

	assert.Len(t, config.Watch, 2)
	assert.Equal(t, HotLoadModeRestart, config.Mode)
	assert.Equal(t, 100, config.DebounceMs)
}

func TestRuntimeConfig_Empty(t *testing.T) {
	config := &RuntimeConfig{}
	assert.Empty(t, config.Mode)
	assert.Zero(t, config.StartupTimeoutMs)
	assert.Nil(t, config.HotLoad)
	assert.Empty(t, config.Env)
	assert.Empty(t, config.Args)
}

func TestRuntimeConfig_WithValues(t *testing.T) {
	config := &RuntimeConfig{
		Mode:             RuntimeModeCapsule,
		StartupTimeoutMs: 5000,
		HotLoad: &HotLoadConfig{
			Mode: HotLoadModeRestart,
		},
		Env:  map[string]string{"KEY": "value"},
		Args: []string{"--flag", "value"},
	}

	assert.Equal(t, RuntimeModeCapsule, config.Mode)
	assert.Equal(t, 5000, config.StartupTimeoutMs)
	assert.NotNil(t, config.HotLoad)
	assert.Len(t, config.Env, 1)
	assert.Len(t, config.Args, 2)
}

func TestMCPConfig_Empty(t *testing.T) {
	config := &MCPConfig{}
	assert.Nil(t, config.InputSchema)
	assert.Nil(t, config.OutputSchema)
}

func TestMCPConfig_WithValues(t *testing.T) {
	config := &MCPConfig{
		InputSchema:  map[string]any{"type": "object"},
		OutputSchema: map[string]any{"type": "string"},
	}

	assert.NotNil(t, config.InputSchema)
	assert.NotNil(t, config.OutputSchema)
	assert.Equal(t, "object", config.InputSchema["type"])
	assert.Equal(t, "string", config.OutputSchema["type"])
}

func TestToolManifest_RequiredFields(t *testing.T) {
	manifest := &ToolManifest{
		Name:        "test-tool",
		Version:     "1.0.0",
		Description: "A test tool",
		Entrypoint:  "bin/tool",
	}

	assert.Equal(t, "test-tool", manifest.Name)
	assert.Equal(t, "1.0.0", manifest.Version)
	assert.Equal(t, "A test tool", manifest.Description)
	assert.Equal(t, "bin/tool", manifest.Entrypoint)
}

func TestToolManifest_OptionalFields(t *testing.T) {
	manifest := &ToolManifest{
		Name:         "test-tool",
		Version:      "1.0.0",
		Description:  "A test tool",
		Entrypoint:   "bin/tool",
		Author:       "Test Author",
		License:      "MIT",
		Repository:   "https://github.com/test/tool",
		Homepage:     "https://test.com",
		Keywords:     []string{"test", "tool"},
		Dependencies: []string{"dep1", "dep2"},
	}

	assert.Equal(t, "test-tool", manifest.Name)
	assert.Equal(t, "1.0.0", manifest.Version)
	assert.Equal(t, "A test tool", manifest.Description)
	assert.Equal(t, "bin/tool", manifest.Entrypoint)

	assert.Equal(t, "Test Author", manifest.Author)
	assert.Equal(t, "MIT", manifest.License)
	assert.Equal(t, "https://github.com/test/tool", manifest.Repository)
	assert.Equal(t, "https://test.com", manifest.Homepage)
	assert.Len(t, manifest.Keywords, 2)
	assert.Len(t, manifest.Dependencies, 2)
}

func TestLLMBackend_Empty(t *testing.T) {
	backend := &LLMBackend{}
	assert.Empty(t, backend.Endpoint)
	assert.Empty(t, backend.Type)
	assert.Empty(t, backend.APIKeyEnvVar)
}

func TestLLMBackend_WithValues(t *testing.T) {
	backend := &LLMBackend{
		Endpoint:     "http://localhost:8080",
		Type:         LLMInferenceAPITypeOllama,
		APIKeyEnvVar: "API_KEY",
	}

	assert.Equal(t, "http://localhost:8080", backend.Endpoint)
	assert.Equal(t, LLMInferenceAPITypeOllama, backend.Type)
	assert.Equal(t, "API_KEY", backend.APIKeyEnvVar)
}
