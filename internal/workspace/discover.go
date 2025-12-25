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

	// Note: We keep Name as the folder name (filepath.Base) since the iOS app
	// already displays the git remote URL on a separate line. This allows users
	// to distinguish between repos with the same git origin but different folder names.

	return repo
}

// GetRepoInfo is the exported version for external use
func GetRepoInfo(repoPath string) DiscoveredRepo {
	return getRepoInfo(repoPath)
}
