package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DiscoveredRepo represents a discovered Git repository
type DiscoveredRepo struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	RemoteURL    string    `json:"remote_url,omitempty"`
	LastModified time.Time `json:"last_modified"`
	IsConfigured bool      `json:"is_configured"` // Already added as workspace
}

// getRepoInfo extracts information about a Git repository (standalone function)
func getRepoInfo(repoPath string) DiscoveredRepo {
	repo := DiscoveredRepo{
		Path:      repoPath,
		Name:      filepath.Base(repoPath),
		RemoteURL: "",
	}

	// Get last modified time
	if info, err := os.Stat(repoPath); err == nil {
		repo.LastModified = info.ModTime()
	}

	// Try to get remote URL
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoPath
	if output, err := cmd.Output(); err == nil {
		repo.RemoteURL = strings.TrimSpace(string(output))
	}

	// Try to extract a better name from remote URL
	if repo.RemoteURL != "" {
		if name := extractRepoNameFromURL(repo.RemoteURL); name != "" {
			repo.Name = name
		}
	}

	return repo
}

// GetRepoInfo is the exported version for external use
func GetRepoInfo(repoPath string) DiscoveredRepo {
	return getRepoInfo(repoPath)
}

// extractRepoNameFromURL extracts repository name from a Git URL
func extractRepoNameFromURL(url string) string {
	// Remove trailing .git
	url = strings.TrimSuffix(url, ".git")

	// Extract last path component
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return ""
}
