package orla

import "log/slog"

// LogDeferredError takes a function that returns an error, calls it, and logs the error if it is not nil
func LogDeferredError(fn func() error) {
	if err := fn(); err != nil {
		slog.Error("Deferred error", "error", err)
	}
}
