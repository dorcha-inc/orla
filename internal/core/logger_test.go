package core

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitLogger_PrettyMode(t *testing.T) {
	require.NoError(t, InitLogger(true, ""))
	slog.Info("test message", "k", "v")
}

func TestInitLogger_JSONMode(t *testing.T) {
	require.NoError(t, InitLogger(false, ""))
	slog.Info("test message", "k", "v")
}

func TestInitLogger_InvalidLevelErrors(t *testing.T) {
	require.Error(t, InitLogger(false, "bogus"))
}

func TestInitLogger_Levels(t *testing.T) {
	for _, l := range []string{"debug", "info", "warn", "error", "fatal", ""} {
		require.NoError(t, InitLogger(false, l))
	}
}

// captureSlog installs a JSON handler writing to buf as the default logger.
// Returns a restore function.
func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(old) }
}

func TestLogDeferredError_WithError(t *testing.T) {
	buf, restore := captureSlog(t)
	t.Cleanup(restore)

	LogDeferredError(func() error { return errors.New("deferred error") })

	assert.Contains(t, buf.String(), "Deferred error")
	assert.Contains(t, buf.String(), `"error"`)
}

func TestLogDeferredError_NoError(t *testing.T) {
	buf, restore := captureSlog(t)
	t.Cleanup(restore)

	LogDeferredError(func() error { return nil })

	assert.Empty(t, buf.String())
}

func TestLogDeferredError1_PropagatesArg(t *testing.T) {
	buf, restore := captureSlog(t)
	t.Cleanup(restore)

	called := ""
	LogDeferredError1(func(s string) error {
		called = s
		return errors.New("boom")
	}, "hello")

	assert.Equal(t, "hello", called)
	assert.Contains(t, buf.String(), "Deferred error")
}

func TestFatal_LogsErrorLevel(t *testing.T) {
	// We can't actually call core.Fatal in a unit test (it would os.Exit).
	// Instead, exercise the same path via slog.LogAttrs with asAttrs to make
	// sure the attribute conversion is correct.
	buf, restore := captureSlog(t)
	t.Cleanup(restore)

	slog.LogAttrs(context.Background(), slog.LevelError, "fatal-like", asAttrs([]any{"key", "value"})...)

	assert.Contains(t, buf.String(), "fatal-like")
	assert.Contains(t, buf.String(), `"key":"value"`)
}
