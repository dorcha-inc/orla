package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// captureStderr captures stderr output and returns it as a string
func captureStderr(fn func()) (string, error) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		return "", err
	}
	os.Stderr = w

	done := make(chan string)

	wasError := false
	errMessage := ""

	go func() {
		var buf strings.Builder
		_, copyErr := io.Copy(&buf, r)
		if copyErr != nil {
			wasError = true
			errMessage = copyErr.Error()
			return
		}
		done <- buf.String()
	}()

	fn()

	err = w.Close()
	if err != nil {
		return "", err
	}

	output := <-done
	os.Stderr = oldStderr

	if wasError {
		return "", fmt.Errorf("captureStderr: %s", errMessage)
	}

	return output, nil
}

func TestNew(t *testing.T) {
	ui := New()
	require.NotNil(t, ui)

	// Verify basic properties
	assert.NotNil(t, ui)
}

func TestIsDisabled(t *testing.T) {
	// Save original value
	original := os.Getenv("ORLA_QUIET")
	defer func() {
		if original == "" {
			require.NoError(t, os.Unsetenv("ORLA_QUIET"))
		} else {
			require.NoError(t, os.Setenv("ORLA_QUIET", original))
		}
	}()

	// Test disabled with "1"
	require.NoError(t, os.Setenv("ORLA_QUIET", "1"))
	ui := New()
	assert.False(t, ui.Enabled(), "UI should be disabled when ORLA_QUIET=1")

	// Test disabled with "true"
	require.NoError(t, os.Setenv("ORLA_QUIET", "true"))
	ui = New()
	assert.False(t, ui.Enabled(), "UI should be disabled when ORLA_QUIET=true")

	// Test enabled with "0"
	require.NoError(t, os.Setenv("ORLA_QUIET", "0"))
	ui = New()
	// Enabled depends on TTY, but if TTY is available, it should be enabled
	if ui.StderrIsTTY() {
		assert.True(t, ui.Enabled(), "UI should be enabled when ORLA_QUIET=0 and TTY available")
	}

	// Test unset
	require.NoError(t, os.Unsetenv("ORLA_QUIET"))
	ui = New()
	// Enabled depends on TTY, so we just verify it doesn't crash
	assert.NotNil(t, ui)
}

func TestIsColorDisabled(t *testing.T) {
	// Save original values
	noColor := os.Getenv("NO_COLOR")
	orlaNoColor := os.Getenv("ORLA_NO_COLOR")
	term := os.Getenv("TERM")
	defer func() {
		if noColor == "" {
			require.NoError(t, os.Unsetenv("NO_COLOR"))
		} else {
			require.NoError(t, os.Setenv("NO_COLOR", noColor))
		}
		if orlaNoColor == "" {
			require.NoError(t, os.Unsetenv("ORLA_NO_COLOR"))
		} else {
			require.NoError(t, os.Setenv("ORLA_NO_COLOR", orlaNoColor))
		}
		if term == "" {
			require.NoError(t, os.Unsetenv("TERM"))
		} else {
			require.NoError(t, os.Setenv("TERM", term))
		}
	}()

	// Test NO_COLOR
	require.NoError(t, os.Setenv("NO_COLOR", "1"))
	require.NoError(t, os.Unsetenv("ORLA_NO_COLOR"))
	require.NoError(t, os.Unsetenv("TERM"))
	ui := New()
	assert.False(t, ui.ColorEnabled(), "Colors should be disabled when NO_COLOR is set")

	// Test ORLA_NO_COLOR
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	require.NoError(t, os.Setenv("ORLA_NO_COLOR", "1"))
	require.NoError(t, os.Unsetenv("TERM"))
	ui = New()
	assert.False(t, ui.ColorEnabled(), "Colors should be disabled when ORLA_NO_COLOR is set")

	// Test TERM=dumb
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	require.NoError(t, os.Unsetenv("ORLA_NO_COLOR"))
	require.NoError(t, os.Setenv("TERM", "dumb"))
	ui = New()
	assert.False(t, ui.ColorEnabled(), "Colors should be disabled when TERM=dumb")

	// Test enabled (clean environment)
	require.NoError(t, os.Unsetenv("NO_COLOR"))
	require.NoError(t, os.Unsetenv("ORLA_NO_COLOR"))
	require.NoError(t, os.Unsetenv("TERM"))
	ui = New()
	// Color enabled depends on TTY and enabled state, so we just verify it doesn't crash
	assert.NotNil(t, ui)
}

func TestUI_Info(t *testing.T) {
	ui := New()
	output, err := captureStderr(func() {
		ui.Info("test message\n")
	})
	require.NoError(t, err)

	// If UI is enabled, output should be in buffer
	if ui.Enabled() {
		assert.Contains(t, output, "test message", "Info should output message when enabled")
	} else {
		assert.Empty(t, output, "Info should not output when disabled")
	}
}

func TestUI_Progress(t *testing.T) {
	ui := New()
	output, err := captureStderr(func() {
		ui.Progress("Processing...")
	})
	require.NoError(t, err)

	// If UI is enabled, output should contain the message
	if ui.Enabled() {
		assert.Contains(t, output, "Processing...", "Progress should output message when enabled")
		// Should contain either spinner char or "..."
		if ui.ColorEnabled() {
			// Should have spinner character (one of the unicode spinner chars)
			assert.True(t, len(output) > 0, "Progress should output spinner when colors enabled")
		} else {
			// Should have "..." when colors disabled
			assert.Contains(t, output, "...", "Progress should output '...' when colors disabled")
		}
	} else {
		assert.Empty(t, output, "Progress should not output when disabled")
	}

	// Clean up spinner
	_, err = captureStderr(func() {
		ui.ProgressSuccess("Done")
	})

	require.NoError(t, err)
}

func TestUI_ProgressSuccess(t *testing.T) {
	ui := New()
	output, err := captureStderr(func() {
		ui.Progress("Testing...")
		ui.ProgressSuccess("Success!")
	})
	require.NoError(t, err)

	// If UI is enabled, should see success message
	if ui.Enabled() {
		assert.Contains(t, output, "Success!", "ProgressSuccess should output message when enabled")
		assert.Contains(t, output, "✓", "ProgressSuccess should include checkmark")
	}
}

func TestUI_RenderMarkdown(t *testing.T) {
	ui := New()

	markdown := "# Hello\n\nThis is **bold** text."
	rendered, err := ui.RenderMarkdown(markdown, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, rendered)

	// If not TTY or colors disabled, should return original content
	if !ui.StdoutIsTTY() || !ui.ColorEnabled() {
		assert.Equal(t, markdown, rendered, "Should return original content when TTY/colors disabled")
	} else {
		// If TTY and colors enabled, should be rendered (different from original)
		assert.NotEqual(t, markdown, rendered, "Should render markdown when TTY and colors enabled")
		// Rendered output should still contain the text content
		assert.Contains(t, rendered, "Hello", "Rendered markdown should contain original text")
		assert.Contains(t, rendered, "bold", "Rendered markdown should contain original text")
	}
}

func TestUI_RenderMarkdown_Complex(t *testing.T) {
	ui := New()

	markdown := `# Title

## Subtitle

- List item 1
- List item 2

**Bold** and *italic* text.

` + "`code`" + ` and ` + "```code block```" + `
`

	rendered, err := ui.RenderMarkdown(markdown, 80)
	require.NoError(t, err)
	assert.NotEmpty(t, rendered)

	// Should contain the original text content regardless of rendering
	assert.Contains(t, rendered, "Title", "Should contain title")
	assert.Contains(t, rendered, "Subtitle", "Should contain subtitle")
	assert.Contains(t, rendered, "List item 1", "Should contain list items")
	assert.Contains(t, rendered, "Bold", "Should contain bold text")
}

func TestConvenienceFunctions(t *testing.T) {
	// Test that convenience functions work and don't crash
	Info("test\n")
	Progress("test")
	ProgressSuccess("done")

	// Test markdown rendering
	_, err := RenderMarkdown("# Test\nHello world", 80)
	assert.NoError(t, err)
}

func TestReset(t *testing.T) {
	original := Default()
	Reset()
	newUI := Default()
	// They should be different instances
	assert.NotSame(t, original, newUI)
}

func TestUI_RenderThinking(t *testing.T) {
	ui := New()

	content := "This is thinking content"
	rendered := ui.RenderThinking(content)

	// Should return content (may be styled if colors enabled)
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "This is thinking content")

	// If colors disabled or not TTY, should return original
	if !ui.ColorEnabled() || !ui.StdoutIsTTY() {
		assert.Equal(t, content, rendered)
	}
}

func TestRenderThinking_Convenience(t *testing.T) {
	content := "test thinking"
	rendered := RenderThinking(content)
	assert.NotEmpty(t, rendered)
	assert.Contains(t, rendered, "test thinking")
}

func TestUI_ProgressSuccess_WithoutSpinner(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "UI enabled",
			enabled: true,
		},
		{
			name:    "UI disabled",
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ui := New()
			ui.enabled = tt.enabled

			// Set up observer to capture logs
			core, logs := observer.New(zap.ErrorLevel)
			logger := zap.New(core)
			zap.ReplaceGlobals(logger)
			defer zap.ReplaceGlobals(zap.NewNop()) // Restore default logger

			// ProgressSuccess without a spinner should not crash
			ui.ProgressSuccess("test")

			// Verify behavior based on enabled state
			if ui.enabled {
				require.GreaterOrEqual(t, logs.Len(), 1, "Should log error when UI is enabled and no spinner exists")
				entry := logs.All()[0]
				assert.Equal(t, "ProgressSuccess called without a spinner", entry.Message)
				assert.Equal(t, zap.ErrorLevel, entry.Level)
			} else {
				// If UI is disabled, ProgressSuccess returns early and doesn't log
				assert.Equal(t, 0, logs.Len(), "Should not log when UI is disabled")
			}
		})
	}
}

func TestUI_Progress_UpdateExisting(t *testing.T) {
	tests := []struct {
		name    string
		enabled bool
	}{
		{
			name:    "UI enabled",
			enabled: true,
		},
		{
			name:    "UI disabled",
			enabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ui := New()
			ui.enabled = tt.enabled
			// Also set stderrIsTTY to true so output works (captureStderr handles the actual capture)
			ui.stderrIsTTY = true

			// Start a spinner - capture all output in one go since spinner runs in goroutine
			output, err := captureStderr(func() {
				ui.Progress("first message")
				// Give spinner time to output
				time.Sleep(150 * time.Millisecond)

				// Update with same message (should update frame, not create new spinner)
				ui.Progress("first message")
				time.Sleep(50 * time.Millisecond)

				// Update with different message
				ui.Progress("second message")
				time.Sleep(50 * time.Millisecond)

				// Complete
				ui.ProgressSuccess("done")
				time.Sleep(50 * time.Millisecond)
			})
			require.NoError(t, err)

			if ui.enabled {
				assert.Contains(t, output, "first message", "Should output first message when enabled")
				assert.Contains(t, output, "second message", "Should output second message")
				assert.Contains(t, output, "done", "Should output success message")
				assert.Contains(t, output, "✓", "Should include checkmark")
			} else {
				assert.Empty(t, output, "Should not output when disabled")
			}
		})
	}
}
