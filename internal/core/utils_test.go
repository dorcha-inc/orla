package core

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMustFprintf_Success(t *testing.T) {
	var buf bytes.Buffer
	format := "Hello %s"
	arg := "World"

	MustFprintf(&buf, format, arg)

	expected := fmt.Sprintf(format, arg)
	assert.Equal(t, expected, buf.String())
}

func TestMustFprintf_WithMultipleArgs(t *testing.T) {
	var buf bytes.Buffer
	format := "Count: %d, Name: %s"
	count := 42
	name := "test"

	MustFprintf(&buf, format, count, name)

	expected := fmt.Sprintf(format, count, name)
	assert.Equal(t, expected, buf.String())
}

func TestJoinMapKeys_StringKeys(t *testing.T) {
	m := map[string]struct{}{
		"key1": {},
		"key2": {},
		"key3": {},
	}

	result := JoinMapKeys(m)
	// Result should contain all keys, order may vary
	assert.Contains(t, result, "key1")
	assert.Contains(t, result, "key2")
	assert.Contains(t, result, "key3")
}

func TestJoinMapKeys_IntKeys(t *testing.T) {
	m := map[int]struct{}{
		1: {},
		2: {},
		3: {},
	}

	result := JoinMapKeys(m)
	// Result should contain all keys as strings
	assert.Contains(t, result, "1")
	assert.Contains(t, result, "2")
	assert.Contains(t, result, "3")
}

func TestJoinMapKeys_EmptyMap(t *testing.T) {
	m := map[string]struct{}{}

	result := JoinMapKeys(m)
	assert.Empty(t, result)
}

func TestJoinMapKeys_SingleKey(t *testing.T) {
	m := map[string]struct{}{
		"only": {},
	}

	result := JoinMapKeys(m)
	assert.Equal(t, "only", result)
}
