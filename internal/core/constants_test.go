package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBugReportMessage(t *testing.T) {
	// i know this test makes no sense, but it's here for coverage purposes and
	// also to ensure that the BugReport has the maintainer link in it
	bugReport := BugReportMessage()
	assert.Contains(t, bugReport, MaintainerLink)
}
