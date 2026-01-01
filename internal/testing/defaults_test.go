package testing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetTestModelName(t *testing.T) {
	assert.Equal(t, GetTestModelName(), testModelName)
}
