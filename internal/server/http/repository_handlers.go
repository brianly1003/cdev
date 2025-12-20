package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/brianly1003/cdev/internal/adapters/repository"
)

// SetRepositoryIndexer sets the repository indexer for the server.
func (s *Server) SetRepositoryIndexer(indexer *repository.SQLiteIndexer) {
	s.repoIndexer = indexer
}

// handleRepositoryIndexStatus handles GET /api/repository/index/status
//
//	@Summary		Get repository index status
//	@Description	Returns the current status of the repository index
//	@Tags			repository
//	@Produce		json
//	@Success		200	{object}	repository.IndexStatus
//	@Failure		503	{object}	ErrorResponse	"Repository indexer not available"
//	@Router			/api/repository/index/status [get]
func (s *Server) handleRepositoryIndexStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.repoIndexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Repository indexer not available",
		})
		return
	}

	status := s.repoIndexer.GetStatus()
	writeJSON(w, http.StatusOK, status)
}

// handleRepositorySearch handles GET /api/repository/search
//
//	@Summary		Search files in repository
//	@Description	Search for files using fuzzy, exact, prefix, or extension matching
//	@Tags			repository
//	@Produce		json
//	@Param			q					query		string	true	"Search query"
//	@Param			mode				query		string	false	"Search mode: fuzzy, exact, prefix, extension"	default(fuzzy)
//	@Param			limit				query		int		false	"Maximum results"								default(50)
//	@Param			offset				query		int		false	"Pagination offset"								default(0)
//	@Param			extensions			query		string	false	"Comma-separated file extensions to filter"
//	@Param			exclude_binaries	query		bool	false	"Exclude binary files"							default(true)
//	@Param			git_tracked_only	query		bool	false	"Only include git-tracked files"				default(false)
//	@Success		200					{object}	repository.SearchResult
//	@Failure		400					{object}	ErrorResponse	"Missing query parameter"
//	@Failure		503					{object}	ErrorResponse	"Repository indexer not available"
//	@Router			/api/repository/search [get]
func (s *Server) handleRepositorySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.repoIndexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Repository indexer not available",
		})
		return
	}

	// Parse query parameters
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Query parameter 'q' is required",
		})
		return
	}

	query := repository.SearchQuery{
		Query:           q,
		Mode:            repository.SearchMode(r.URL.Query().Get("mode")),
		Limit:           parseIntParam(r, "limit", repository.DefaultSearchLimit),
		Offset:          parseIntParam(r, "offset", 0),
		ExcludeBinaries: parseBoolParam(r, "exclude_binaries", true),
		GitTrackedOnly:  parseBoolParam(r, "git_tracked_only", false),
	}

	// Parse extensions
	if exts := r.URL.Query().Get("extensions"); exts != "" {
		query.Extensions = strings.Split(exts, ",")
	}

	// Parse size filters
	if minSize := r.URL.Query().Get("min_size"); minSize != "" {
		if size, err := strconv.ParseInt(minSize, 10, 64); err == nil {
			query.MinSize = size
		}
	}
	if maxSize := r.URL.Query().Get("max_size"); maxSize != "" {
		if size, err := strconv.ParseInt(maxSize, 10, 64); err == nil {
			query.MaxSize = size
		}
	}

	result, err := s.repoIndexer.Search(r.Context(), query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRepositoryFilesList handles GET /api/repository/files/list
//
//	@Summary		List files in directory
//	@Description	Returns a paginated list of files in a directory with filtering and sorting options
//	@Tags			repository
//	@Produce		json
//	@Param			directory	query		string	false	"Directory path (empty for root)"
//	@Param			recursive	query		bool	false	"Include subdirectories"	default(false)
//	@Param			limit		query		int		false	"Maximum results"			default(100)
//	@Param			offset		query		int		false	"Pagination offset"			default(0)
//	@Param			sort		query		string	false	"Sort by: name, size, modified"	default(name)
//	@Param			order		query		string	false	"Sort order: asc, desc"		default(asc)
//	@Param			extensions	query		string	false	"Comma-separated file extensions to filter"
//	@Success		200			{object}	repository.FileList
//	@Failure		503			{object}	ErrorResponse	"Repository indexer not available"
//	@Router			/api/repository/files/list [get]
func (s *Server) handleRepositoryFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.repoIndexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Repository indexer not available",
		})
		return
	}

	opts := repository.ListOptions{
		Directory: r.URL.Query().Get("directory"),
		Recursive: parseBoolParam(r, "recursive", false),
		Limit:     parseIntParam(r, "limit", repository.DefaultListLimit),
		Offset:    parseIntParam(r, "offset", 0),
		SortBy:    r.URL.Query().Get("sort"),
		SortOrder: r.URL.Query().Get("order"),
	}

	// Parse extensions
	if exts := r.URL.Query().Get("extensions"); exts != "" {
		opts.Extensions = strings.Split(exts, ",")
	}

	// Parse size filters
	if minSize := r.URL.Query().Get("min_size"); minSize != "" {
		if size, err := strconv.ParseInt(minSize, 10, 64); err == nil {
			opts.MinSize = size
		}
	}
	if maxSize := r.URL.Query().Get("max_size"); maxSize != "" {
		if size, err := strconv.ParseInt(maxSize, 10, 64); err == nil {
			opts.MaxSize = size
		}
	}

	result, err := s.repoIndexer.ListFiles(r.Context(), opts)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleRepositoryTree handles GET /api/repository/files/tree
//
//	@Summary		Get directory tree
//	@Description	Returns a hierarchical tree structure of the repository
//	@Tags			repository
//	@Produce		json
//	@Param			path	query		string	false	"Root path"			default("")
//	@Param			depth	query		int		false	"Maximum depth"		default(2)
//	@Success		200		{object}	repository.DirectoryTree
//	@Failure		503		{object}	ErrorResponse	"Repository indexer not available"
//	@Router			/api/repository/files/tree [get]
func (s *Server) handleRepositoryTree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.repoIndexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Repository indexer not available",
		})
		return
	}

	rootPath := r.URL.Query().Get("path")
	depth := parseIntParam(r, "depth", 2)

	tree, err := s.repoIndexer.GetTree(r.Context(), rootPath, depth)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, tree)
}

// handleRepositoryStats handles GET /api/repository/stats
//
//	@Summary		Get repository statistics
//	@Description	Returns aggregate statistics about the repository
//	@Tags			repository
//	@Produce		json
//	@Success		200	{object}	repository.RepositoryStats
//	@Failure		503	{object}	ErrorResponse	"Repository indexer not available"
//	@Router			/api/repository/stats [get]
func (s *Server) handleRepositoryStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.repoIndexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Repository indexer not available",
		})
		return
	}

	stats, err := s.repoIndexer.GetStats(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleRepositoryRebuild handles POST /api/repository/index/rebuild
//
//	@Summary		Rebuild repository index
//	@Description	Triggers a full re-index of the repository
//	@Tags			repository
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Failure		503	{object}	ErrorResponse	"Repository indexer not available"
//	@Router			/api/repository/index/rebuild [post]
func (s *Server) handleRepositoryRebuild(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.repoIndexer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Repository indexer not available",
		})
		return
	}

	// Start rebuild in background
	go func() {
		s.repoIndexer.FullScan(r.Context())
	}()

	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "started",
		"message": "Repository index rebuild started",
	})
}

// parseBoolParam parses a boolean query parameter with a default value.
func parseBoolParam(r *http.Request, name string, defaultVal bool) bool {
	valStr := r.URL.Query().Get(name)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.ParseBool(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

// RepositoryIndexStatusResponse is the API response type for index status.
type RepositoryIndexStatusResponse struct {
	Status            string `json:"status" example:"ready"`
	TotalFiles        int    `json:"total_files" example:"1234"`
	IndexedFiles      int    `json:"indexed_files" example:"1234"`
	TotalSizeBytes    int64  `json:"total_size_bytes" example:"45829381"`
	LastFullScan      string `json:"last_full_scan" example:"2025-12-19T10:30:00Z"`
	LastUpdate        string `json:"last_update" example:"2025-12-19T11:45:23Z"`
	DatabaseSizeBytes int64  `json:"database_size_bytes" example:"2458234"`
	IsGitRepo         bool   `json:"is_git_repo" example:"true"`
}

// RepositorySearchRequest represents a search request.
type RepositorySearchRequest struct {
	Query           string   `json:"q" example:"main.go"`
	Mode            string   `json:"mode" example:"fuzzy" enums:"fuzzy,exact,prefix,extension"`
	Limit           int      `json:"limit" example:"50"`
	Offset          int      `json:"offset" example:"0"`
	Extensions      []string `json:"extensions,omitempty" example:"go,js,ts"`
	ExcludeBinaries bool     `json:"exclude_binaries" example:"true"`
	GitTrackedOnly  bool     `json:"git_tracked_only" example:"false"`
}

// RepositoryFileInfo represents a file in search results.
type RepositoryFileInfo struct {
	Path        string  `json:"path" example:"internal/adapters/repository/indexer.go"`
	Name        string  `json:"name" example:"indexer.go"`
	Directory   string  `json:"directory" example:"internal/adapters/repository"`
	Extension   string  `json:"extension,omitempty" example:"go"`
	SizeBytes   int64   `json:"size_bytes" example:"12453"`
	ModifiedAt  string  `json:"modified_at" example:"2025-12-19T10:30:00Z"`
	IsBinary    bool    `json:"is_binary" example:"false"`
	IsSensitive bool    `json:"is_sensitive" example:"false"`
	GitTracked  bool    `json:"git_tracked" example:"true"`
	MatchScore  float64 `json:"match_score,omitempty" example:"0.95"`
}

// RepositorySearchResponse represents a search response.
type RepositorySearchResponse struct {
	Query     string               `json:"query" example:"indexer"`
	Mode      string               `json:"mode" example:"fuzzy"`
	Results   []RepositoryFileInfo `json:"results"`
	Total     int                  `json:"total" example:"5"`
	ElapsedMS int64                `json:"elapsed_ms" example:"23"`
}
