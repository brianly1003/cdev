package ports

import "context"

// GitFileStatus represents the status of a file in git.
type GitFileStatus struct {
	Path        string `json:"path"`
	Status      string `json:"status"` // M, A, D, R, ??, etc.
	IsStaged    bool   `json:"is_staged"`
	IsUntracked bool   `json:"is_untracked"`
}

// GitTracker defines the contract for git operations.
type GitTracker interface {
	// Status returns the current git status.
	Status(ctx context.Context) ([]GitFileStatus, error)

	// Diff returns the diff for a specific file.
	Diff(ctx context.Context, path string) (string, error)

	// DiffStaged returns the staged diff for a specific file.
	DiffStaged(ctx context.Context, path string) (string, error)

	// DiffNewFile generates a diff-like output for untracked/new files.
	DiffNewFile(ctx context.Context, path string) (string, error)

	// DiffAll returns diffs for all changed files.
	DiffAll(ctx context.Context) (map[string]string, error)

	// IsGitRepo checks if the configured path is a git repository.
	IsGitRepo() bool

	// GetRepoRoot returns the root path of the git repository.
	GetRepoRoot() string

	// GetRepoName returns the name of the repository.
	GetRepoName() string
}
