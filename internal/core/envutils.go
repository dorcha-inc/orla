package core

import "os"

// GetEnv retrieves an environment variable, checking both the standard name
// and an ORLA-prefixed version. Returns the first non-empty value found.
// This allows environment variables to be set with or without the ORLA_ prefix.
func GetEnv(key string) string {
	// Check standard environment variable first
	if val := os.Getenv(key); val != "" {
		return val
	}
	// Check ORLA-prefixed version
	return os.Getenv("ORLA_" + key)
}
