// Package config provides centralized default configuration values.
package config

// DefaultSkipDirectories is the canonical list of directories to skip during
// file watching, indexing, and directory traversal. This is the single source
// of truth - all components should use these values.
//
// Users can override via config.yaml: indexer.skip_directories or watcher.ignore_patterns
var DefaultSkipDirectories = []string{
	".git",
	".svn",
	".hg",
	".cdev",   // cdev logs and cache
	".claude", // Claude CLI cache
	"node_modules",
	"vendor",
	"__pycache__",
	".pytest_cache",
	".mypy_cache",
	".tox",
	".venv",
	"venv",
	".idea",
	".vscode",
	".vs",
	"dist",
	"build",
	"target",
	"coverage",
	".coverage",
	".nyc_output",
	".next",
	".nuxt",
	".cache",
	".parcel-cache",
	".turbo",
}

// DefaultWatcherIgnorePatterns is the canonical list of patterns for file watcher.
// These include both directories and file patterns (e.g., *.pyc, *.swp).
var DefaultWatcherIgnorePatterns = []string{
	".git",
	".cdev",   // cdev agent logs and sessions
	".claude", // Claude CLI config
	"node_modules",
	".venv",
	"venv",
	"__pycache__",
	"*.pyc",
	".DS_Store",
	"Thumbs.db",
	"dist",
	"build",
	"coverage",
	".next",
	".nuxt",
	"*.log",
	".idea",
	".vscode",
	"*.swp",
	"*.swo",
	"*~",
}

// SkipDirectoriesSet returns a map for O(1) lookups of skip directories.
// Uses the provided list if non-empty, otherwise falls back to defaults.
func SkipDirectoriesSet(customDirs []string) map[string]bool {
	dirs := DefaultSkipDirectories
	if len(customDirs) > 0 {
		dirs = customDirs
	}

	set := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		set[d] = true
	}
	return set
}
