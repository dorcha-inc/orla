package orla

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func captureSlog(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	return buf, func() { slog.SetDefault(old) }
}

func TestLogDeferredError_NoError(t *testing.T) {
	buf, restore := captureSlog(t)
	t.Cleanup(restore)

	LogDeferredError(func() error { return nil })

	assert.Empty(t, buf.String())
}

func TestLogDeferredError_WithError(t *testing.T) {
	buf, restore := captureSlog(t)
	t.Cleanup(restore)

	LogDeferredError(func() error { return errors.New("test error") })

	assert.Contains(t, buf.String(), "Deferred error")
	assert.Contains(t, buf.String(), "test error")
}
