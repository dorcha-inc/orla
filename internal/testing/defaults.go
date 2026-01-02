// Package testing provides default values and utilities for testing orla.
package testing

const (
	testModelName = "qwen3:0.6b"
)

func GetTestModelName() string {
	// note(jadidbourbaki): the reason this is a function is to allow
	// for modification based on the test environment and hardware limitations.
	return testModelName
}
