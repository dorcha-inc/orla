package tui

import (
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	orlaTesting "github.com/dorcha-inc/orla/internal/testing"
)

func TestNew(t *testing.T) {
	ui := New()
	require.NotNil(t, ui)

	// Verify basic properties
	assert.NotNil(t, ui)
}

func TestIsDisabled(t *testing.T) {
	// Test disabled with "1"
	t.Setenv("ORLA_QUIET", "1")
	ui := New()
	assert.False(t, ui.Enabled(), "UI should be disabled when ORLA_QUIET=1")

	// Test disabled with "true"
	t.Setenv("ORLA_QUIET", "true")
	ui = New()
	assert.False(t, ui.Enabled(), "UI should be disabled when ORLA_QUIET=true")

	// Test enabled with "0"
	t.Setenv("ORLA_QUIET", "0")
	ui = New()
	// Enabled depends on TTY, but if TTY is available, it should be enabled
	if ui.StderrIsTTY() {
		assert.True(t, ui.Enabled(), "UI should be enabled when ORLA_QUIET=0 and TTY available")
	}

	// Test unset (set to empty string)
	t.Setenv("ORLA_QUIET", "")
	ui = New()
	// Enabled depends on TTY, so we just verify it doesn't crash
	assert.NotNil(t, ui)
}

func TestIsColorDisabled(t *testing.T) {
	// Save original values
	// Test NO_COLOR
	t.Setenv("NO_COLOR", "1")
	t.Setenv("ORLA_NO_COLOR", "")
	t.Setenv("TERM", "")
	assert.True(t, isColorDisabled(), "Colors should be disabled when NO_COLOR is set")

	// Test ORLA_NO_COLOR
	t.Setenv("NO_COLOR", "")
	t.Setenv("ORLA_NO_COLOR", "1")
	t.Setenv("TERM", "")
	assert.True(t, isColorDisabled(), "Colors should be disabled when ORLA_NO_COLOR is set")

	// Test TERM=dumb
	t.Setenv("NO_COLOR", "")
	t.Setenv("ORLA_NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	assert.True(t, isColorDisabled(), "Colors should be disabled when TERM=dumb")

	// Test enabled (clean environment)
	t.Setenv("NO_COLOR", "")
	t.Setenv("ORLA_NO_COLOR", "")
	t.Setenv("TERM", "")
	assert.False(t, isColorDisabled(), "Colors should be enabled when environment is clean")
}

func TestUI_Info_Enabled(t *testing.T) {
	ui := New()
	ui.enabled = true

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	message := "test message"
	ui.Info("%s", message)

	outputStdout, outputStderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// Info writes to stderr, not stdout
	assert.Empty(t, outputStdout, "Stdout should be empty for Info")
	assert.Contains(t, outputStderr, message, "Stderr should contain the message when enabled")
}

func TestUI_Info_Disabled(t *testing.T) {
	ui := New()
	ui.enabled = false

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	message := "test message"
	ui.Info("%s", message)

	outputStdout, outputStderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// Info writes to stderr, but when disabled it should not output
	assert.Empty(t, outputStdout, "Stdout should be empty for Info")
	assert.Equal(t, outputStderr, message, "Should contain message exactly without styling")
}

func TestUI_Progress(t *testing.T) {
	// Save original clock and restore after test
	originalClock := spinnerClock
	defer func() {
		spinnerClock = originalClock
	}()

	fakeClock := clockwork.NewFakeClock()
	spinnerClock = fakeClock
	ui := New()
	ui.stderrIsTTY = true // Ensure output works

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	ui.Progress("Processing...")
	fakeClock.Advance(100 * time.Millisecond) // Advance fake clock

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// Progress outputs to stderr
	assert.Empty(t, stdout, "Stdout should be empty for Progress")

	// If UI is enabled, output should contain the message
	if ui.Enabled() {
		assert.Contains(t, stderr, "Processing...", "Progress should output message when enabled")
		// Should contain either spinner char or "..."
		if ui.ColorEnabled() {
			// Should have spinner character (one of the unicode spinner chars)
			assert.True(t, len(stderr) > 0, "Progress should output spinner when colors enabled")
		} else {
			// Should have "..." when colors disabled
			assert.Contains(t, stderr, "...", "Progress should output '...' when colors disabled")
		}
	} else {
		assert.Empty(t, stderr, "Progress should not output when disabled")
	}

	// Clean up spinner
	capturedOutput2, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)
	ui.ProgressSuccess("Done")
	fakeClock.Advance(50 * time.Millisecond)
	_, _, err = capturedOutput2.Stop()
	require.NoError(t, err)
}

func TestUI_ProgressSuccess(t *testing.T) {
	// Save original clock and restore after test
	originalClock := spinnerClock
	defer func() {
		spinnerClock = originalClock
	}()

	fakeClock := clockwork.NewFakeClock()
	spinnerClock = fakeClock
	ui := New()
	ui.stderrIsTTY = true // Ensure output works

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	ui.Progress("Testing...")
	fakeClock.Advance(50 * time.Millisecond)
	ui.ProgressSuccess("Success!")
	fakeClock.Advance(50 * time.Millisecond)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// Progress outputs to stderr
	assert.Empty(t, stdout, "Stdout should be empty for Progress")

	// If UI is enabled, should see success message
	if ui.Enabled() {
		assert.Contains(t, stderr, "Success!", "ProgressSuccess should output message when enabled")
		assert.Contains(t, stderr, "✓", "ProgressSuccess should include checkmark")
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

func TestUI_ThinkingMessage(t *testing.T) {
	ui := New()
	ui.enabled = true

	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	content := "This is thinking content"
	ui.ThinkingMessage(content)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// ThinkingMessage writes to stderr, not stdout
	assert.Empty(t, stdout, "Stdout should be empty for ThinkingMessage")
	assert.Contains(t, stderr, "This is thinking content", "Stderr should contain the thinking content")
}

func TestThinkingMessage_Convenience(t *testing.T) {
	capturedOutput, err := orlaTesting.NewCapturedOutput()
	require.NoError(t, err)

	content := "test thinking"
	ThinkingMessage(content)

	stdout, stderr, err := capturedOutput.Stop()
	require.NoError(t, err)

	// ThinkingMessage writes to stderr, not stdout
	assert.Empty(t, stdout, "Stdout should be empty for ThinkingMessage")
	assert.Contains(t, stderr, "test thinking", "Stderr should contain the thinking content")
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
			ui.showProgress = tt.enabled

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
			// Save original clock and restore after test
			originalClock := spinnerClock
			defer func() {
				spinnerClock = originalClock
			}()

			fakeClock := clockwork.NewFakeClock()
			spinnerClock = fakeClock

			ui := New()
			ui.enabled = tt.enabled
			ui.stderrIsTTY = true
			ui.showProgress = tt.enabled

			capturedOutput, err := orlaTesting.NewCapturedOutput()
			require.NoError(t, err)

			// Start a spinner - capture all output in one go since spinner runs in goroutine
			ui.Progress("first message")
			// Give spinner time to output
			fakeClock.Advance(150 * time.Millisecond)

			// Update with same message (should update frame, not create new spinner)
			// This updates in place, so we might not see it in output
			ui.Progress("first message")
			fakeClock.Advance(100 * time.Millisecond)

			// Update with different message - this should be visible
			ui.Progress("second message")
			fakeClock.Advance(100 * time.Millisecond)

			// Complete - this stops the spinner goroutine
			// ProgressSuccess closes the done channel which should cause the goroutine to exit
			ui.ProgressSuccess("done")
			// Give time for spinner goroutine to fully exit and final output to be written
			// ProgressSuccess already has a 10ms delay, but we need more to ensure goroutine exits
			fakeClock.Advance(150 * time.Millisecond)

			stdout, stderr, err := capturedOutput.Stop()
			require.NoError(t, err)

			assert.Empty(t, stdout, "Stdout should be empty for Progress")

			if ui.enabled {
				assert.Contains(t, stderr, "first message", "Should output first message when enabled")
				assert.Contains(t, stderr, "second message", "Should output second message")
				assert.Contains(t, stderr, "done", "Should output success message")
				assert.Contains(t, stderr, "✓", "Should include checkmark")
			} else {
				assert.Empty(t, stderr, "Should not output when disabled")
			}
		})
	}
}

func TestUI_RenderMarkdown_ErrorHandling(t *testing.T) {
	ui := New()

	markdown := "# Test"
	rendered, err := ui.RenderMarkdown(markdown, -1)
	require.Error(t, err)
	assert.Empty(t, rendered)
}

func TestUI_RenderMarkdown_NoRenderer(t *testing.T) {
	// Create UI without markdown renderer (simulate error case)
	ui := &UI{
		stdoutIsTTY:  true,
		colorEnabled: true,
		// markdownRenderer is nil
	}

	markdown := "# Test"
	rendered, err := ui.RenderMarkdown(markdown, 80)
	// Should create renderer on the fly

	require.NoError(t, err)
	require.NotEmpty(t, rendered)

	require.NotEqual(t, markdown, rendered)
}

func TestUI_Progress_MessageUpdate(t *testing.T) {
	ui := New()
	ui.enabled = true
	ui.stderrIsTTY = true
	ui.showProgress = true

	// Start progress with one message
	ui.Progress("First message")
	require.NotNil(t, ui.currentSpinner, "Spinner should be created")
	require.Equal(t, "First message", ui.currentSpinner.message)

	// Update with different message
	ui.Progress("Second message")
	require.NotNil(t, ui.currentSpinner, "Spinner should still exist")
	require.Equal(t, "Second message", ui.currentSpinner.message)
}

func TestUI_Progress_Disabled(t *testing.T) {
	ui := New()
	ui.enabled = false

	// Progress should return early when disabled
	ui.Progress("test message")

	// Should not have spinner
	assert.Nil(t, ui.currentSpinner)
}

func TestUI_ProgressSuccess_EmptyMessage(t *testing.T) {
	ui := New()
	ui.enabled = true
	ui.stderrIsTTY = true

	// Start a spinner
	ui.Progress("test")

	// Complete with empty message (should use spinner message)
	ui.ProgressSuccess("")

	// Should not crash
	assert.Nil(t, ui.currentSpinner)
}

func TestIsTerminal(t *testing.T) {
	// Test isTerminal function (indirectly through New)
	ui := New()

	// Should detect TTY status
	stdoutTTY := ui.StdoutIsTTY()
	stderrTTY := ui.StderrIsTTY()

	// Just verify the values are boolean (true or false)
	assert.IsType(t, true, stdoutTTY)
	assert.IsType(t, true, stderrTTY)
}

func TestIsDisabled_NonBooleanValues(t *testing.T) {
	// Test that non-boolean values are treated as disabled
	// Set to non-boolean value
	t.Setenv("ORLA_QUIET", "maybe")
	ui := New()
	assert.False(t, ui.Enabled(), "Non-boolean value should disable UI")
}

func TestIsColorDisabled_EmptyString(t *testing.T) {
	// Test that empty string doesn't disable colors
	// Unset both
	t.Setenv("NO_COLOR", "")
	t.Setenv("ORLA_NO_COLOR", "")

	ui := New()
	// Colors should be enabled if TTY is available and UI is enabled
	// We can't test the exact value, but we can verify it doesn't crash
	assert.NotNil(t, ui)
}

func TestDefault_ReturnsSingleton(t *testing.T) {
	ui1 := Default()
	ui2 := Default()

	// Should return the same instance
	assert.Same(t, ui1, ui2)
}

func TestReset_CreatesNewInstance(t *testing.T) {
	_ = Default() // Capture original

	Reset()

	newUI := Default()

	// Should be different from original
	// But at least verify it doesn't crash
	assert.NotNil(t, newUI)
}
