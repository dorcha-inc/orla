package main

import (
	"fmt"
	"os/exec"
)

func ensureOrlaBinary() (string, error) {
	// orla must be in the PATH
	if _, err := exec.LookPath("orla"); err != nil {
		return "", fmt.Errorf("orla binary not found in PATH")
	}
	return "orla", nil
}
