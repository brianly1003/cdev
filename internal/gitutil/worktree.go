package gitutil

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// NormalizePath returns a cleaned absolute path when possible.
func NormalizePath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolvedPath
	}
	return filepath.Clean(absPath)
}

// IsWithinPath reports whether candidate is the same path as root or is nested under it.
func IsWithinPath(root, candidate string) bool {
	root = NormalizePath(root)
	candidate = NormalizePath(candidate)
	if root == "" || candidate == "" {
		return false
	}
	if root == candidate {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// GitCommonDir returns the absolute git common dir for a repo or worktree path.
func GitCommonDir(path string) (string, error) {
	path = NormalizePath(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	cmd := exec.Command("git", "-C", path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(output)), nil
}

// SameRepoFamily reports whether two paths belong to the same git repo family.
func SameRepoFamily(pathA, pathB string) bool {
	pathA = NormalizePath(pathA)
	pathB = NormalizePath(pathB)
	if pathA == "" || pathB == "" {
		return false
	}
	if pathA == pathB {
		return true
	}

	commonA, errA := GitCommonDir(pathA)
	commonB, errB := GitCommonDir(pathB)
	if errA != nil || errB != nil {
		return false
	}

	return commonA != "" && commonA == commonB
}

// ListWorktreePaths returns all worktree roots for the repo family containing path.
func ListWorktreePaths(path string) ([]string, error) {
	path = NormalizePath(path)
	if path == "" {
		return nil, fmt.Errorf("path is empty")
	}

	cmd := exec.Command("git", "-C", path, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	worktreePaths := make([]string, 0, 4)

	for _, line := range bytes.Split(output, []byte{'\n'}) {
		text := strings.TrimSpace(string(line))
		if !strings.HasPrefix(text, "worktree ") {
			continue
		}

		worktreePath := NormalizePath(strings.TrimSpace(strings.TrimPrefix(text, "worktree ")))
		if worktreePath == "" {
			continue
		}
		if _, exists := seen[worktreePath]; exists {
			continue
		}
		seen[worktreePath] = struct{}{}
		worktreePaths = append(worktreePaths, worktreePath)
	}

	return worktreePaths, nil
}
