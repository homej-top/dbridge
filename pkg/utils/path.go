package utils

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidatePath validates that a path does not contain directory traversal sequences
// and is safe to use. Returns the cleaned path or an error if invalid.
func ValidatePath(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	// Check for directory traversal attempts
	if strings.Contains(path, "..") {
		return "", fmt.Errorf("path contains invalid characters: ..")
	}

	// Check for absolute paths
	if filepath.IsAbs(path) || strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	// Normalize the path (clean up . and redundant separators)
	cleaned := filepath.Clean(path)

	// After cleaning, check again for traversal
	if strings.Contains(cleaned, "..") {
		return "", fmt.Errorf("path resolves to parent directory")
	}

	// Ensure path doesn't start with /
	if strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("normalized path is absolute")
	}

	return cleaned, nil
}
