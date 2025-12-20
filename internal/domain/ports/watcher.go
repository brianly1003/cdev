package ports

import "context"

// FileWatcher defines the contract for file system monitoring.
type FileWatcher interface {
	// Start begins watching the specified directory.
	Start(ctx context.Context) error

	// Stop terminates file watching.
	Stop() error

	// AddIgnorePattern adds a pattern to the ignore list.
	AddIgnorePattern(pattern string)

	// RemoveIgnorePattern removes a pattern from the ignore list.
	RemoveIgnorePattern(pattern string)

	// IsRunning returns true if the watcher is active.
	IsRunning() bool
}
