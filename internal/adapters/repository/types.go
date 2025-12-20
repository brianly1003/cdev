// Package repository provides repository file indexing and search functionality.
// It uses SQLite with FTS5 for fast file search across the codebase.
package repository

import (
	"context"
	"time"
)

// SearchMode defines the type of search to perform.
type SearchMode string

const (
	// SearchModeFuzzy uses FTS5 for fuzzy matching.
	SearchModeFuzzy SearchMode = "fuzzy"
	// SearchModeExact matches the exact query string.
	SearchModeExact SearchMode = "exact"
	// SearchModePrefix matches files starting with the query.
	SearchModePrefix SearchMode = "prefix"
	// SearchModeExtension filters by file extension.
	SearchModeExtension SearchMode = "extension"
)

// IndexStatus represents the current state of the repository index.
type IndexStatus struct {
	Status            string    `json:"status"` // ready, indexing, error
	TotalFiles        int       `json:"total_files"`
	IndexedFiles      int       `json:"indexed_files"`
	TotalSizeBytes    int64     `json:"total_size_bytes"`
	LastFullScan      time.Time `json:"last_full_scan"`
	LastUpdate        time.Time `json:"last_update"`
	DatabaseSizeBytes int64     `json:"database_size_bytes"`
	IsGitRepo         bool      `json:"is_git_repo"`
	ErrorMessage      string    `json:"error_message,omitempty"`
}

// FileInfo represents metadata about a file in the repository.
type FileInfo struct {
	ID          int64     `json:"-"`
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Directory   string    `json:"directory"`
	Extension   string    `json:"extension,omitempty"`
	SizeBytes   int64     `json:"size_bytes"`
	ModifiedAt  time.Time `json:"modified_at"`
	IndexedAt   time.Time `json:"indexed_at,omitempty"`
	IsBinary    bool      `json:"is_binary"`
	IsSymlink   bool      `json:"is_symlink"`
	IsSensitive bool      `json:"is_sensitive"`
	GitTracked  bool      `json:"git_tracked"`
	GitIgnored  bool      `json:"git_ignored"`
	ContentHash string    `json:"-"`
	LineCount   int       `json:"line_count,omitempty"`
	MatchScore  float64   `json:"match_score,omitempty"` // For search results
	FileID      uint64    `json:"-"`                     // Inode (Unix) or File ID (Windows) for rename detection
}

// DirectoryInfo represents metadata about a directory.
type DirectoryInfo struct {
	Path           string    `json:"path"`
	Name           string    `json:"name"`
	FileCount      int       `json:"file_count"`
	TotalSizeBytes int64     `json:"total_size_bytes"`
	LastModified   time.Time `json:"last_modified,omitempty"`
}

// DirectoryTree represents a hierarchical directory structure.
type DirectoryTree struct {
	Path           string          `json:"path"`
	Name           string          `json:"name"`
	Type           string          `json:"type"` // "file" or "directory"
	Children       []DirectoryTree `json:"children,omitempty"`
	SizeBytes      *int64          `json:"size_bytes,omitempty"`
	Extension      *string         `json:"extension,omitempty"`
	FileCount      *int            `json:"file_count,omitempty"`
	TotalSizeBytes *int64          `json:"total_size_bytes,omitempty"`
}

// SearchQuery defines parameters for searching files.
type SearchQuery struct {
	Query           string     `json:"query"`
	Mode            SearchMode `json:"mode"`
	Limit           int        `json:"limit"`
	Offset          int        `json:"offset"`
	Extensions      []string   `json:"extensions,omitempty"`
	ExcludeBinaries bool       `json:"exclude_binaries"`
	GitTrackedOnly  bool       `json:"git_tracked_only"`
	MinSize         int64      `json:"min_size,omitempty"`
	MaxSize         int64      `json:"max_size,omitempty"`
}

// SearchResult contains the results of a file search.
type SearchResult struct {
	Query     string     `json:"query"`
	Mode      SearchMode `json:"mode"`
	Results   []FileInfo `json:"results"`
	Total     int        `json:"total"`
	ElapsedMS int64      `json:"elapsed_ms"`
}

// ListOptions defines parameters for listing files.
type ListOptions struct {
	Directory  string   `json:"directory"`
	Recursive  bool     `json:"recursive"`
	Limit      int      `json:"limit"`
	Offset     int      `json:"offset"`
	SortBy     string   `json:"sort_by"`    // name, size, modified
	SortOrder  string   `json:"sort_order"` // asc, desc
	Extensions []string `json:"extensions,omitempty"`
	MinSize    int64    `json:"min_size,omitempty"`
	MaxSize    int64    `json:"max_size,omitempty"`
}

// FileList contains a paginated list of files.
type FileList struct {
	Directory   string          `json:"directory"`
	Files       []FileInfo      `json:"files"`
	Directories []DirectoryInfo `json:"directories"`
	TotalFiles  int             `json:"total_files"`
	TotalDirs   int             `json:"total_directories"`
	Pagination  PaginationInfo  `json:"pagination"`
}

// PaginationInfo contains pagination metadata.
type PaginationInfo struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// RepositoryStats contains aggregate statistics about the repository.
type RepositoryStats struct {
	TotalFiles        int            `json:"total_files"`
	TotalDirectories  int            `json:"total_directories"`
	TotalSizeBytes    int64          `json:"total_size_bytes"`
	FilesByExtension  map[string]int `json:"files_by_extension"`
	LargestFiles      []FileInfo     `json:"largest_files"`
	GitTrackedFiles   int            `json:"git_tracked_files"`
	GitIgnoredFiles   int            `json:"git_ignored_files"`
	BinaryFiles       int            `json:"binary_files"`
	SensitiveFiles    int            `json:"sensitive_files"`
}

// Indexer defines the interface for repository indexing operations.
type Indexer interface {
	// Lifecycle
	Start(ctx context.Context) error
	Stop() error
	IsReady() bool

	// Indexing
	FullScan(ctx context.Context) error
	IndexFile(ctx context.Context, path string) error
	RemoveFile(ctx context.Context, path string) error

	// Queries
	Search(ctx context.Context, query SearchQuery) (*SearchResult, error)
	ListFiles(ctx context.Context, opts ListOptions) (*FileList, error)
	GetTree(ctx context.Context, rootPath string, depth int) (*DirectoryTree, error)
	GetStats(ctx context.Context) (*RepositoryStats, error)

	// Status
	GetStatus() IndexStatus
}

// Security constants
const (
	// MaxFileSize is the maximum file size to index (100MB).
	MaxFileSize = 100 * 1024 * 1024
	// MaxTotalIndexSize is the maximum total size of indexed content (1GB).
	MaxTotalIndexSize = 1 * 1024 * 1024 * 1024
	// MaxFilesPerScan is the maximum number of files to scan.
	MaxFilesPerScan = 1000000
	// ScanTimeout is the maximum time for a full scan.
	ScanTimeout = 5 * time.Minute
	// DefaultSearchLimit is the default number of search results.
	DefaultSearchLimit = 50
	// MaxSearchLimit is the maximum number of search results.
	MaxSearchLimit = 500
	// DefaultListLimit is the default number of files in a list.
	DefaultListLimit = 100
	// MaxListLimit is the maximum number of files in a list.
	MaxListLimit = 1000
	// MaxTreeDepth is the maximum depth for tree queries.
	MaxTreeDepth = 10
)

// Sensitive file patterns that should be flagged.
var SensitivePatterns = []string{
	".env",
	".env.*",
	"*.env",
	"credentials.json",
	"credentials.yaml",
	"credentials.yml",
	"secrets.json",
	"secrets.yaml",
	"secrets.yml",
	"*.key",
	"*.pem",
	"*.p12",
	"*.pfx",
	"*.jks",
	"id_rsa",
	"id_dsa",
	"id_ecdsa",
	"id_ed25519",
	".aws/credentials",
	".netrc",
	".npmrc",
	".pypirc",
}

// SkipDirectories is the default list of directories to skip during scanning.
// These are typically dependency, build output, or IDE directories.
// Users can override this via config.yaml indexer.skip_directories setting.
var SkipDirectories = []string{
	".git",
	".svn",
	".hg",
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

// Binary file extensions to skip content indexing.
var BinaryExtensions = []string{
	".exe", ".dll", ".so", ".dylib", ".a", ".o", ".obj",
	".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar",
	".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp", ".svg",
	".mp3", ".mp4", ".avi", ".mov", ".mkv", ".wav", ".flac",
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".wasm", ".pyc", ".pyo", ".class", ".jar", ".war",
	".ttf", ".otf", ".woff", ".woff2", ".eot",
	".db", ".sqlite", ".sqlite3",
}
