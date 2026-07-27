package api

import (
	"fmt"
	"math"
	"net/url"
)

// validateHTTPURL rejects a URL orla could never call out to, so a bad
// value is caught at the API boundary rather than failing silently on
// every request that needs it. The message names the constraint but
// not the field, so callers wrap it with the field name.
func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host, got %q", raw)
	}
	return nil
}

// isFiniteNonNegative reports whether v is a finite non-negative
// number. NaN, +Inf, -Inf, and negative values fail.
func isFiniteNonNegative(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

// validateRates checks that every value in the rates map is a
// non-negative finite number. Returns an empty string on success and
// a human-readable error message otherwise.
func validateRates(m map[string]float64) string {
	for k, v := range m {
		if !isFiniteNonNegative(v) {
			return fmt.Sprintf("rates[%q] must be a non-negative finite number, got %v", k, v)
		}
	}
	return ""
}
