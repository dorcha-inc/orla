package registry

import (
	"fmt"
	"os/exec"
)

// GitRunner is an interface for running git commands, allowing for testing with mocks
type GitRunner interface {
	Clone(url, targetPath string) error
	Pull(repoPath string) error
}

// execGitRunner implements GitRunner using exec.Command
type execGitRunner struct{}

func (e *execGitRunner) Clone(url, targetPath string) error {
	cmd := exec.Command("git", "clone", "--depth", "1", url, targetPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to clone registry repository: %w, output: %s", err, string(output))
	}
	return nil
}

func (e *execGitRunner) Pull(repoPath string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath
	return cmd.Run()
}

// defaultGitRunner is the default GitRunner implementation
var defaultGitRunner GitRunner = &execGitRunner{}

// GetDefaultGitRunner returns the default GitRunner (exported for testing)
func GetDefaultGitRunner() GitRunner {
	return defaultGitRunner
}

// SetGitRunner sets the GitRunner implementation (used for testing)
func SetGitRunner(runner GitRunner) {
	defaultGitRunner = runner
}

// MockGitRunner is a mock implementation of GitRunner for testing
// It can be used across packages to test code that depends on GitRunner
type MockGitRunner struct {
	CloneErr   error
	PullErr    error
	CloneCalls []struct{ URL, TargetPath string }
	PullCalls  []string
	CloneFunc  func(url, targetPath string) error
	PullFunc   func(repoPath string) error
}

func (m *MockGitRunner) Clone(url, targetPath string) error {
	m.CloneCalls = append(m.CloneCalls, struct{ URL, TargetPath string }{url, targetPath})
	if m.CloneFunc != nil {
		return m.CloneFunc(url, targetPath)
	}
	return m.CloneErr
}

func (m *MockGitRunner) Pull(repoPath string) error {
	m.PullCalls = append(m.PullCalls, repoPath)
	if m.PullFunc != nil {
		return m.PullFunc(repoPath)
	}
	return m.PullErr
}

// Interface guard
var _ GitRunner = &MockGitRunner{}
