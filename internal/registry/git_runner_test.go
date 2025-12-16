package registry

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecGitRunner_Clone(t *testing.T) {
	t.Run("successful clone", func(t *testing.T) {
		runner := &execGitRunner{}
		tmpDir := t.TempDir()
		targetPath := filepath.Join(tmpDir, "cloned-repo")

		// Create a temporary git repository to clone
		sourceRepo := t.TempDir()
		cmd := exec.Command("git", "init")
		cmd.Dir = sourceRepo
		require.NoError(t, cmd.Run())

		// Create a file and commit it
		// #nosec G306 -- test file permissions are acceptable for temporary test files
		require.NoError(t, os.WriteFile(filepath.Join(sourceRepo, "test.txt"), []byte("test"), 0644))
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = sourceRepo
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "commit", "-m", "initial commit")
		cmd.Dir = sourceRepo
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		require.NoError(t, cmd.Run())

		// Clone using file:// URL (works on all platforms)
		sourceURL := "file://" + sourceRepo
		err := runner.Clone(sourceURL, targetPath)
		require.NoError(t, err)

		// Verify the clone was successful
		assert.DirExists(t, targetPath)
		assert.FileExists(t, filepath.Join(targetPath, "test.txt"))
	})

	t.Run("clone error - invalid URL", func(t *testing.T) {
		runner := &execGitRunner{}
		tmpDir := t.TempDir()
		targetPath := filepath.Join(tmpDir, "cloned-repo")

		err := runner.Clone("https://invalid-url-that-does-not-exist.example.com/repo", targetPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to clone registry repository")
	})

	t.Run("clone error - invalid target path", func(t *testing.T) {
		runner := &execGitRunner{}
		// Use a path that cannot be created (e.g., root on Unix systems)
		invalidPath := "/root/invalid/path/should/fail"

		err := runner.Clone("https://example.com/repo", invalidPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to clone registry repository")
	})
}

func TestExecGitRunner_Pull(t *testing.T) {
	t.Run("successful pull", func(t *testing.T) {
		runner := &execGitRunner{}
		repoDir := t.TempDir()

		// Initialize git repository
		cmd := exec.Command("git", "init")
		cmd.Dir = repoDir
		require.NoError(t, cmd.Run())

		// Create initial commit
		// #nosec G306 -- test file permissions are acceptable for temporary test files
		require.NoError(t, os.WriteFile(filepath.Join(repoDir, "file1.txt"), []byte("content1"), 0644))
		cmd = exec.Command("git", "add", ".")
		cmd.Dir = repoDir
		require.NoError(t, cmd.Run())

		cmd = exec.Command("git", "commit", "-m", "initial commit")
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com")
		require.NoError(t, cmd.Run())

		// Create a remote repository to pull from
		remoteRepo := t.TempDir()
		cmd = exec.Command("git", "init", "--bare")
		cmd.Dir = remoteRepo
		require.NoError(t, cmd.Run())

		// Add remote and fetch
		cmd = exec.Command("git", "remote", "add", "origin", remoteRepo)
		cmd.Dir = repoDir
		require.NoError(t, cmd.Run())

		// Get the default branch name
		cmd = exec.Command("git", "branch", "--show-current")
		cmd.Dir = repoDir
		branchOutput, err := cmd.Output()
		require.NoError(t, err)
		branchName := strings.TrimSpace(string(branchOutput))
		if branchName == "" {
			// If no branch name, try to determine default branch
			cmd = exec.Command("git", "symbolic-ref", "--short", "HEAD")
			cmd.Dir = repoDir
			branchOutput, err = cmd.Output()
			if err == nil {
				branchName = strings.TrimSpace(string(branchOutput))
			} else {
				// Fallback to common branch names
				branchName = "main"
			}
		}

		// Push to remote
		cmd = exec.Command("git", "push", "-u", "origin", branchName)
		cmd.Dir = repoDir
		require.NoError(t, cmd.Run())

		// Now pull should succeed (no-op since already up to date)
		assert.NoError(t, runner.Pull(repoDir))
	})

	t.Run("pull error - not a git repository", func(t *testing.T) {
		runner := &execGitRunner{}
		tmpDir := t.TempDir()

		assert.Error(t, runner.Pull(tmpDir))
	})

	t.Run("pull error - non-existent directory", func(t *testing.T) {
		runner := &execGitRunner{}
		nonExistentPath := "/non/existent/path/should/fail"

		assert.Error(t, runner.Pull(nonExistentPath))
	})
}

func TestMockGitRunner_Clone(t *testing.T) {
	t.Run("uses CloneFunc when provided", func(t *testing.T) {
		mock := &MockGitRunner{
			CloneFunc: func(url, targetPath string) error {
				assert.Equal(t, "https://example.com/repo", url)
				assert.Equal(t, "/tmp/target", targetPath)
				return nil
			},
		}

		err := mock.Clone("https://example.com/repo", "/tmp/target")
		assert.NoError(t, err)
		assert.Len(t, mock.CloneCalls, 1)
		assert.Equal(t, "https://example.com/repo", mock.CloneCalls[0].URL)
		assert.Equal(t, "/tmp/target", mock.CloneCalls[0].TargetPath)
	})

	t.Run("uses CloneErr when CloneFunc is nil", func(t *testing.T) {
		expectedErr := assert.AnError
		mock := &MockGitRunner{
			CloneErr: expectedErr,
		}

		err := mock.Clone("https://example.com/repo", "/tmp/target")
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Len(t, mock.CloneCalls, 1)
	})

	t.Run("returns nil when both CloneFunc and CloneErr are nil", func(t *testing.T) {
		mock := &MockGitRunner{}

		err := mock.Clone("https://example.com/repo", "/tmp/target")
		assert.NoError(t, err)
		assert.Len(t, mock.CloneCalls, 1)
	})

	t.Run("tracks multiple clone calls", func(t *testing.T) {
		mock := &MockGitRunner{}

		assert.NoError(t, mock.Clone("https://example.com/repo1", "/tmp/target1"))
		assert.NoError(t, mock.Clone("https://example.com/repo2", "/tmp/target2"))

		assert.Len(t, mock.CloneCalls, 2)
		assert.Equal(t, "https://example.com/repo1", mock.CloneCalls[0].URL)
		assert.Equal(t, "/tmp/target1", mock.CloneCalls[0].TargetPath)
		assert.Equal(t, "https://example.com/repo2", mock.CloneCalls[1].URL)
		assert.Equal(t, "/tmp/target2", mock.CloneCalls[1].TargetPath)
	})
}

func TestMockGitRunner_Pull(t *testing.T) {
	t.Run("uses PullFunc when provided", func(t *testing.T) {
		mock := &MockGitRunner{
			PullFunc: func(repoPath string) error {
				assert.Equal(t, "/tmp/repo", repoPath)
				return nil
			},
		}

		err := mock.Pull("/tmp/repo")
		assert.NoError(t, err)
		assert.Len(t, mock.PullCalls, 1)
		assert.Equal(t, "/tmp/repo", mock.PullCalls[0])
	})

	t.Run("uses PullErr when PullFunc is nil", func(t *testing.T) {
		expectedErr := assert.AnError
		mock := &MockGitRunner{
			PullErr: expectedErr,
		}

		err := mock.Pull("/tmp/repo")
		assert.Error(t, err)
		assert.Equal(t, expectedErr, err)
		assert.Len(t, mock.PullCalls, 1)
	})

	t.Run("returns nil when both PullFunc and PullErr are nil", func(t *testing.T) {
		mock := &MockGitRunner{}

		err := mock.Pull("/tmp/repo")
		assert.NoError(t, err)
		assert.Len(t, mock.PullCalls, 1)
	})

	t.Run("tracks multiple pull calls", func(t *testing.T) {
		mock := &MockGitRunner{}

		assert.NoError(t, mock.Pull("/tmp/repo1"))
		assert.NoError(t, mock.Pull("/tmp/repo2"))

		assert.Len(t, mock.PullCalls, 2)
		assert.Equal(t, "/tmp/repo1", mock.PullCalls[0])
		assert.Equal(t, "/tmp/repo2", mock.PullCalls[1])
	})
}

func TestGetDefaultGitRunner(t *testing.T) {
	runner := GetDefaultGitRunner()
	assert.NotNil(t, runner)

	// Verify it's an execGitRunner
	_, ok := runner.(*execGitRunner)
	assert.True(t, ok, "default runner should be execGitRunner")
}

func TestSetGitRunner(t *testing.T) {
	originalRunner := GetDefaultGitRunner()

	// Set a mock runner
	mockRunner := &MockGitRunner{}
	SetGitRunner(mockRunner)

	// Verify the default runner was changed
	currentRunner := GetDefaultGitRunner()
	assert.Equal(t, mockRunner, currentRunner)

	// Restore original runner
	SetGitRunner(originalRunner)

	// Verify it was restored
	restoredRunner := GetDefaultGitRunner()
	assert.Equal(t, originalRunner, restoredRunner)
}
