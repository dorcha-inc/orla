package registry

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOrlaHomeDir(t *testing.T) {
	homeDir, err := GetOrlaHomeDir()
	require.NoError(t, err)
	assert.Contains(t, homeDir, ".orla")
}

func TestGetInstalledToolsDir(t *testing.T) {
	dir, err := GetInstalledToolsDir()
	require.NoError(t, err)
	assert.Contains(t, dir, ".orla")
	assert.Contains(t, dir, "tools")
}

func TestGetRegistryCacheDir(t *testing.T) {
	dir, err := GetRegistryCacheDir()
	require.NoError(t, err)
	assert.Contains(t, dir, ".orla")
	assert.Contains(t, dir, "cache")
	assert.Contains(t, dir, "registry")
}
