// Package core implements the core functionality for orla that is shared across all components.
package core

import (
	"fmt"
	"io/fs"
	"os"
)

// IsExecutable checks if a file mode has any executable bits set.
// It checks the executable bits for owner, group, and others (0111).
func IsExecutable(info fs.FileInfo) bool {
	permissions := info.Mode().Perm()
	return permissions&0111 != 0
}

// CopyDirectory recursively copies a directory using os.Root and fs.WalkDir. Skips directories in the skipDirs slice.
// This provides secure directory copying with automatic path traversal prevention.
func CopyDirectory(src, dst string, skipDirs []string) error {
	root, err := os.OpenRoot(src)
	if err != nil {
		return fmt.Errorf("failed to open source directory: %w", err)
	}
	defer LogDeferredError(root.Close)

	dstRoot, err := os.OpenRoot(dst)
	if err != nil {
		return fmt.Errorf("failed to open destination directory: %w", err)
	}
	defer LogDeferredError(dstRoot.Close)

	// Build skip map for efficient lookup
	skipMap := make(map[string]bool)
	for _, dir := range skipDirs {
		skipMap[dir] = true
	}

	// Use fs.WalkDir to walk the directory tree safely
	return fs.WalkDir(root.FS(), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip configured directories
		if d.IsDir() && skipMap[d.Name()] {
			return fs.SkipDir
		}

		// Get file/directory info to preserve permissions
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %w", path, err)
		}

		if d.IsDir() {
			// Create destination directory with preserved permissions
			return dstRoot.MkdirAll(path, info.Mode().Perm())
		}

		// Copy file using os.Root (automatically prevents path traversal)
		data, err := root.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		// Preserve original file permissions
		if err := dstRoot.WriteFile(path, data, info.Mode().Perm()); err != nil {
			return fmt.Errorf("failed to write file %s: %w", path, err)
		}

		return nil
	})
}
