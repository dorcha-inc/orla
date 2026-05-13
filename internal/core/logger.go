package core

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// InitLogger initializes the global structured logger.
//   - pretty=true uses a human-friendly text handler with colors-via-level-names;
//     pretty=false uses JSON for machine ingestion.
//   - level is one of "debug", "info", "warn", "error"; empty means "info".
//
// All output goes to stderr so it doesn't collide with tool stdout (MCP/stdio).
func InitLogger(pretty bool, level string) error {
	lvl, err := parseLevel(level)
	if err != nil {
		return err
	}
	opts := &slog.HandlerOptions{Level: lvl}
	var handler slog.Handler
	if pretty {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
	return nil
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	case "fatal":
		// slog has no Fatal; map to Error and rely on Fatal() helper for os.Exit.
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("invalid log level %q", level)
}

// Fatal logs at error level and exits with status 1. Use sparingly — most error
// paths should return the error to the caller.
func Fatal(msg string, args ...any) {
	slog.LogAttrs(context.Background(), slog.LevelError, msg, asAttrs(args)...)
	os.Exit(1)
}

func asAttrs(args []any) []slog.Attr {
	out := make([]slog.Attr, 0, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			continue
		}
		out = append(out, slog.Any(k, args[i+1]))
	}
	return out
}

// LogDeferredError takes a function that returns an error, calls it, and logs
// the error if it is not nil.
func LogDeferredError(fn func() error) {
	if err := fn(); err != nil {
		slog.Error("Deferred error", "error", err)
	}
}

// LogDeferredError1 takes a function that returns an error, calls it with the
// given argument, and logs the error if it is not nil.
func LogDeferredError1[T any](fn func(T) error, arg T) {
	if err := fn(arg); err != nil {
		slog.Error("Deferred error", "error", err)
	}
}
