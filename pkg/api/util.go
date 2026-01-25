package orla

import "go.uber.org/zap"

// LogDeferredError takes a function that returns an error, calls it, and logs the error if it is not nil
func LogDeferredError(fn func() error) {
	if err := fn(); err != nil {
		zap.L().Error("Deferred error", zap.Error(err), zap.Stack("stack_trace"))
	}
}
