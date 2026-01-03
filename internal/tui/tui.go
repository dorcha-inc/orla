// Package tui provides terminal UI utilities using charmbracelet libraries.
// It automatically detects terminal capabilities and disables rich output when piping or redirecting.
// Note(jadidbourbaki): our inspiration here is uv/uvx's clean UI. http://astral.sh/uv
//
// The TUI package is designed to be script-friendly:
//   - Progress messages only appear when stderr is a TTY
//   - Colors are automatically disabled when piping or when NO_COLOR is set
//   - Markdown rendering for model output
//
// Environment Variables:
//   - NO_COLOR or ORLA_NO_COLOR: Disable colors (respects https://no-color.org/)
//   - TERM=dumb: Disable colors
//   - ORLA_QUIET: Disable all UI output (progress messages)
//
// Example usage:
//
//	// Progress message (only shown in TTY)
//	tui.Progress("Connecting to tools...")
//	tui.ProgressSuccess("Connected successfully")
//
//	// Info message (only shown in TTY)
//	tui.Info("Found %d tool(s)\n", count)
//
//	// Render markdown content (perfect for model output!)
//	rendered, _ := tui.RenderMarkdown(markdownContent, 80)
//	fmt.Print(rendered)
package tui

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jonboulle/clockwork"
	"go.uber.org/zap"
	"golang.org/x/term"
)

// Color definitions using ansi package
var (
	colorGreen = lipgloss.ANSIColor(2) // ANSI green
	colorBlue  = lipgloss.ANSIColor(4) // ANSI blue
	colorGray  = lipgloss.ANSIColor(8) // ANSI gray (bright black)
)

// UI provides terminal UI functionality with automatic TTY detection
type UI struct {
	// stdoutIsTTY indicates if stdout is connected to a terminal
	stdoutIsTTY bool
	// stderrIsTTY indicates if stderr is connected to a terminal
	stderrIsTTY bool
	// enabled indicates if UI output should be shown (TTY + not disabled)
	enabled bool
	// colorEnabled indicates if colors should be used
	colorEnabled bool
	// showProgress forces progress messages to be shown even when UI is disabled
	showProgress bool
	// currentSpinner tracks the current spinner state
	currentSpinner *spinnerState
	// markdownRenderer for rendering markdown content
	markdownRenderer *glamour.TermRenderer
}

type spinnerState struct {
	started time.Time
	ticker  clockwork.Ticker
	message string
	done    chan struct{}
}

var (
	// defaultUI is the default UI instance
	defaultUI    *UI
	spinnerClock clockwork.Clock = clockwork.NewRealClock()

	// stderrRenderer is a renderer that uses stderr for TTY detection
	// This allows colors to work on stderr even when stdout is piped
	stderrRenderer = lipgloss.NewRenderer(os.Stderr)

	// Style definitions using ansi package
	// Use stderrRenderer for styles that will be written to stderr
	successStyle  = lipgloss.NewStyle().Renderer(stderrRenderer).Foreground(colorGreen).Bold(true)
	thinkingStyle = lipgloss.NewStyle().Renderer(stderrRenderer).Foreground(colorGray)
)

func init() {
	defaultUI = New()
}

// New creates a new UI instance with automatic TTY detection
func New() *UI {
	stdoutIsTTY := IsTerminal(os.Stdout)
	stderrIsTTY := IsTerminal(os.Stderr)
	stdinIsTTY := IsTerminal(os.Stdin)

	// UI is enabled if stderr is a TTY (we use stderr for progress messages)
	// However, if stdin is piped (not a TTY), suppress UI for clean pipeable output
	// This follows Unix conventions: piped input usually means script/pipeline usage
	enabled := stderrIsTTY && stdinIsTTY && !isDisabled()

	// Colors are enabled if stderr is a TTY and colors aren't explicitly disabled
	// Colors are independent of UI enabled state (you might want colored output even without progress)
	colorEnabled := stderrIsTTY && !isColorDisabled()

	ui := &UI{
		stdoutIsTTY:  stdoutIsTTY,
		stderrIsTTY:  stderrIsTTY,
		enabled:      enabled,
		colorEnabled: colorEnabled,
		// showProgress overrides the enabled state for progress messages
		showProgress: false,
	}

	// Initialize markdown renderer if colors are enabled
	if colorEnabled && stdoutIsTTY {
		// Get terminal width, default to 80
		width := 80
		if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
			width = w
		}

		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err == nil {
			ui.markdownRenderer = renderer
		}
	}

	return ui
}

// IsTerminal checks if a file descriptor is connected to a terminal
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// isDisabled checks if UI is explicitly disabled via environment variables
func isDisabled() bool {
	// Check ORLA_QUIET or similar env vars
	if val := os.Getenv("ORLA_QUIET"); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
		return true // Any non-empty value means disabled
	}
	return false
}

// isColorDisabled checks if colors are explicitly disabled
func isColorDisabled() bool {
	// Check standard NO_COLOR environment variable (https://no-color.org/)
	if os.Getenv("NO_COLOR") != "" {
		return true
	}
	// Check ORLA_NO_COLOR
	if os.Getenv("ORLA_NO_COLOR") != "" {
		return true
	}
	// Check TERM=dumb
	if os.Getenv("TERM") == "dumb" {
		return true
	}
	return false
}

// Enabled returns whether UI output should be shown
func (u *UI) Enabled() bool {
	return u.enabled
}

// ColorEnabled returns whether colors should be used
func (u *UI) ColorEnabled() bool {
	return u.colorEnabled
}

// StdoutIsTTY returns whether stdout is a terminal
func (u *UI) StdoutIsTTY() bool {
	return u.stdoutIsTTY
}

// StderrIsTTY returns whether stderr is a terminal
func (u *UI) StderrIsTTY() bool {
	return u.stderrIsTTY
}

// SetShowProgress sets whether progress messages should be shown even when UI is disabled
// This is useful when show_progress config option is set
func (u *UI) SetShowProgress(show bool) {
	u.showProgress = show
}

// Progress shows a progress message with a spinner
// Uses charmbracelet's spinner styles, similar to uv/uvx's clean UI
// The spinner animates automatically in the background
// Example: "Connecting to tools..."
func (u *UI) Progress(message string) {
	if !u.showProgress || !u.enabled {
		return
	}

	// If spinner already exists with same message, just update the frame
	if u.currentSpinner != nil && u.currentSpinner.message == message {
		// Update spinner frame
		elapsed := time.Since(u.currentSpinner.started)
		frame := int(elapsed/spinner.Line.FPS) % len(spinner.Line.Frames)
		spinnerChar := spinner.Line.Frames[frame]

		if !u.colorEnabled {
			spinnerChar = "..."
		}

		// Use stderrRenderer for spinner style so colors work even when stdout is piped
		spinnerStyle := lipgloss.NewStyle().Renderer(stderrRenderer).Foreground(colorBlue)
		if u.colorEnabled {
			fmt.Fprintf(os.Stderr, "\r%s %s", spinnerStyle.Render(spinnerChar), message)
		} else {
			fmt.Fprintf(os.Stderr, "\r%s %s", spinnerChar, message)
		}
		return
	}

	// If spinner exists with different message, stop it first
	if u.currentSpinner != nil {
		// Stop the old spinner
		if u.currentSpinner.ticker != nil {
			u.currentSpinner.ticker.Stop()
		}
		if u.currentSpinner.done != nil {
			close(u.currentSpinner.done)
		}
		// Clear the spinner line
		fmt.Fprint(os.Stderr, "\r", ansi.EraseLine(2))
		u.currentSpinner = nil
		// Small delay to ensure goroutine has stopped
		time.Sleep(10 * time.Millisecond)
	}

	// Initialize new spinner with animation goroutine
	u.currentSpinner = &spinnerState{
		started: time.Now(),
		message: message,
		done:    make(chan struct{}),
		ticker:  spinnerClock.NewTicker(100 * time.Millisecond),
	}

	// Start animation goroutine
	// Capture spinner state in closure to avoid race conditions
	spinnerState := u.currentSpinner

	var printSpinnerFrame = func() {
		elapsed := time.Since(spinnerState.started)
		frame := int(elapsed/spinner.Line.FPS) % len(spinner.Line.Frames)
		spinnerChar := spinner.Line.Frames[frame]

		if !u.colorEnabled {
			spinnerChar = "..."
		}

		// Use stderrRenderer for spinner style so colors work even when stdout is piped
		spinnerStyle := lipgloss.NewStyle().Renderer(stderrRenderer).Foreground(colorBlue)
		if u.colorEnabled {
			fmt.Fprintf(os.Stderr, "\r%s %s", spinnerStyle.Render(spinnerChar), spinnerState.message)
		} else {
			fmt.Fprintf(os.Stderr, "\r%s %s", spinnerChar, spinnerState.message)
		}
	}

	// Print initial spinner frame immediately (don't wait for first tick)
	printSpinnerFrame()

	go func() {
		for {
			select {
			case <-spinnerState.ticker.Chan():
				printSpinnerFrame()
			case <-spinnerState.done:
				return
			}
		}
	}()
}

// ProgressSuccess stops the spinner and shows success message
// Uses uv-style checkmark (✓) for success
func (u *UI) ProgressSuccess(message string) {
	if !u.showProgress || !u.enabled {
		return
	}

	if u.currentSpinner == nil {
		zap.L().Error("ProgressSuccess called without a spinner")
		return
	}

	// Save message before stopping spinner
	displayMessage := message
	if displayMessage == "" {
		displayMessage = u.currentSpinner.message
	}

	// Stop the spinner animation (stop ticker first to prevent race)
	if u.currentSpinner.ticker != nil {
		u.currentSpinner.ticker.Stop()
	}
	if u.currentSpinner.done != nil {
		close(u.currentSpinner.done)
	}

	// Small delay to ensure goroutine has stopped printing
	time.Sleep(10 * time.Millisecond)

	// Clear spinner line: move to start of line and erase entire line
	fmt.Fprint(os.Stderr, "\r", ansi.EraseLine(2))
	u.currentSpinner = nil

	// Use uv-style checkmark (only if we have a message)
	if displayMessage != "" {
		symbol := "✓"
		if u.colorEnabled {
			fmt.Fprintf(os.Stderr, "%s %s\n", successStyle.Render(symbol), displayMessage)
		} else {
			fmt.Fprintf(os.Stderr, "%s %s\n", symbol, displayMessage)
		}
	}
}

// Info prints an informational message to stderr
// Writes to stderr even when not a TTY (e.g., when piping output)
// Respects ORLA_QUIET environment variable
func (u *UI) Info(format string, args ...any) {
	if isDisabled() {
		return
	}
	fmt.Fprintf(os.Stderr, format, args...)
}

// RenderMarkdown renders markdown content using glamour (perfect for model output!)
// Returns plain text if not in TTY or if rendering fails
func (u *UI) RenderMarkdown(content string, width int) (string, error) {
	if width <= 0 {
		return "", fmt.Errorf("width must be greater than 0")
	}

	// If not a TTY or colors disabled, return plain text
	if !u.stdoutIsTTY || !u.colorEnabled {
		return content, nil
	}

	// Use cached renderer if available, otherwise create a new one
	renderer := u.markdownRenderer
	if renderer == nil {
		var err error
		renderer, err = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return content, err
		}
	}

	return renderer.Render(content)
}

// ThinkingMessage renders thinking trace content with a distinct style to stderr
func (u *UI) ThinkingMessage(content string) {
	if !u.enabled || !u.colorEnabled || !u.stderrIsTTY {
		// Plain text output when colors disabled or not a TTY
		fmt.Fprint(os.Stderr, content)
		return
	}

	// Styled output when colors are enabled
	styledContent := thinkingStyle.Render(content)
	fmt.Fprint(os.Stderr, styledContent)
}

// Default returns the default UI instance
func Default() *UI {
	return defaultUI
}

// Reset resets the default UI instance (useful for testing)
func Reset() {
	defaultUI = New()
}

// Convenience functions that use the default UI instance

// Info prints an informational message using the default UI
func Info(format string, args ...any) {
	defaultUI.Info(format, args...)
}

// SetShowProgress sets whether progress messages should be shown even when UI is disabled
// This uses the default UI instance
func SetShowProgress(show bool) {
	defaultUI.SetShowProgress(show)
}

// Progress prints a progress message using the default UI
func Progress(message string) {
	defaultUI.Progress(message)
}

// ProgressSuccess stops spinner and shows success using the default UI
func ProgressSuccess(message string) {
	defaultUI.ProgressSuccess(message)
}

// RenderMarkdown renders markdown content using the default UI
func RenderMarkdown(content string, width int) (string, error) {
	return defaultUI.RenderMarkdown(content, width)
}

// ThinkingMessage renders thinking trace content using the default UI
func ThinkingMessage(content string) {
	defaultUI.ThinkingMessage(content)
}
