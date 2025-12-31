package core

import (
	"fmt"
	"io"

	"go.uber.org/zap"
)

// MustFprintf is a wrapper around fmt.Fprintf that exits the program if it fails.
func MustFprintf(w io.Writer, format string, a ...any) {
	_, err := fmt.Fprintf(w, format, a...)
	if err != nil {
		zap.L().Fatal("Failed to fprintf", zap.Error(err), zap.String("format", format), zap.Any("a", a))
	}
}
