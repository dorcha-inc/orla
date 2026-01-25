package core

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testVar = "TEST_VAR"

func TestGetEnv_StandardEnvVar(t *testing.T) {
	expectedValue := "test-value"
	require.NoError(t, os.Setenv(testVar, expectedValue))
	defer LogDeferredError1(os.Unsetenv, testVar)

	result := GetEnv(testVar)
	assert.Equal(t, expectedValue, result)
}

func TestGetEnv_ORLAPrefixed(t *testing.T) {
	expectedValue := "orla-prefixed-value"
	require.NoError(t, os.Unsetenv(testVar)) // Make sure standard version is not set
	require.NoError(t, os.Setenv("ORLA_"+testVar, expectedValue))
	defer LogDeferredError1(os.Unsetenv, "ORLA_"+testVar)

	result := GetEnv(testVar)
	assert.Equal(t, expectedValue, result)
}

func TestGetEnv_StandardTakesPrecedence(t *testing.T) {
	standardValue := "standard-value"
	orlaValue := "orla-value"
	require.NoError(t, os.Setenv(testVar, standardValue))
	require.NoError(t, os.Setenv("ORLA_"+testVar, orlaValue))
	defer func() {
		require.NoError(t, os.Unsetenv(testVar))
		require.NoError(t, os.Unsetenv("ORLA_"+testVar))
	}()

	result := GetEnv(testVar)
	assert.Equal(t, standardValue, result) // Standard should take precedence
}

func TestGetEnv_NotSet(t *testing.T) {
	key := "NONEXISTENT_VAR"
	require.NoError(t, os.Unsetenv(key))
	require.NoError(t, os.Unsetenv("ORLA_"+key))

	result := GetEnv(key)
	assert.Empty(t, result)
}

func TestIsLocalMachine(t *testing.T) {
	// This test depends on the actual runtime environment
	// On macOS, it should return true; on other platforms, false
	result := IsLocalMachine()
	if runtime.GOOS == "darwin" {
		assert.True(t, result)
	} else {
		assert.False(t, result)
	}
}
