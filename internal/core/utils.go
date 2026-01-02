package core

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"go.uber.org/zap"
)

// MustFprintf is a wrapper around fmt.Fprintf that exits the program if it fails.
func MustFprintf(w io.Writer, format string, a ...any) {
	_, err := fmt.Fprintf(w, format, a...)
	if err != nil {
		zap.L().Fatal("Failed to fprintf", zap.Error(err), zap.String("format", format), zap.Any("a", a))
	}
}

// JoinMapKeys joins the keys of a map into a comma-separated string.
// Useful for error messages that need to list valid values.
func JoinMapKeys[T comparable](m map[T]struct{}) string {
	keys := slices.Collect(maps.Keys(m))
	sliceStrings := make([]string, len(keys))
	for i, k := range keys {
		sliceStrings[i] = fmt.Sprintf("%v", k)
	}
	return strings.Join(sliceStrings, ", ")
}
