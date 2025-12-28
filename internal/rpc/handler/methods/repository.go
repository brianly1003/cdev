// Package methods provides JSON-RPC method implementations.
package methods

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/brianly1003/cdev/internal/adapters/repository"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// RepositoryIndexer defines the interface for repository indexing operations.
type RepositoryIndexer interface {
	GetStatus() repository.IndexStatus
	Search(ctx context.Context, query repository.SearchQuery) (*repository.SearchResult, error)
	ListFiles(ctx context.Context, opts repository.ListOptions) (*repository.FileList, error)
	GetTree(ctx context.Context, rootPath string, depth int) (*repository.DirectoryTree, error)
	GetStats(ctx context.Context) (*repository.RepositoryStats, error)
	FullScan(ctx context.Context) error
}

// RepositoryService handles repository indexing and search operations via JSON-RPC.
type RepositoryService struct {
	indexer RepositoryIndexer
}

// NewRepositoryService creates a new repository service.
func NewRepositoryService(indexer RepositoryIndexer) *RepositoryService {
	return &RepositoryService{
		indexer: indexer,
	}
}

// RegisterMethods registers all repository methods with the handler.
func (s *RepositoryService) RegisterMethods(registry *handler.Registry) {
	registry.RegisterWithMeta("repository/index/status", s.IndexStatus, handler.MethodMeta{
		Summary:     "Get repository index status",
		Description: "Returns the current status of the repository index including file counts, sizes, and last scan time.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "status",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("repository/search", s.Search, handler.MethodMeta{
		Summary:     "Search files in repository",
		Description: "Search for files using fuzzy, exact, prefix, or extension matching. Returns paginated results with match scores.",
		Params: []handler.OpenRPCParam{
			{Name: "query", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Search query string"}},
			{Name: "mode", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"fuzzy", "exact", "prefix", "extension"}, "default": "fuzzy"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "offset", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
			{Name: "extensions", Required: false, Schema: map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
			{Name: "exclude_binaries", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": true}},
			{Name: "git_tracked_only", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
			{Name: "min_size", Required: false, Schema: map[string]interface{}{"type": "integer", "description": "Minimum file size in bytes"}},
			{Name: "max_size", Required: false, Schema: map[string]interface{}{"type": "integer", "description": "Maximum file size in bytes"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("repository/files/list", s.ListFiles, handler.MethodMeta{
		Summary:     "List files in directory",
		Description: "Returns a paginated list of files in a directory with filtering and sorting options.",
		Params: []handler.OpenRPCParam{
			{Name: "directory", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Directory path (empty for root)"}},
			{Name: "recursive", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 100}},
			{Name: "offset", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
			{Name: "sort_by", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"name", "size", "modified", "path"}, "default": "name"}},
			{Name: "sort_order", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
			{Name: "extensions", Required: false, Schema: map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
			{Name: "min_size", Required: false, Schema: map[string]interface{}{"type": "integer"}},
			{Name: "max_size", Required: false, Schema: map[string]interface{}{"type": "integer"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("repository/files/tree", s.GetTree, handler.MethodMeta{
		Summary:     "Get directory tree",
		Description: "Returns a hierarchical tree structure of the repository with files and directories.",
		Params: []handler.OpenRPCParam{
			{Name: "path", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Root path (empty for repository root)"}},
			{Name: "depth", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 2, "description": "Maximum depth to traverse"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "tree",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("repository/stats", s.GetStats, handler.MethodMeta{
		Summary:     "Get repository statistics",
		Description: "Returns aggregate statistics about the repository including file counts by extension, largest files, and totals.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "stats",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("repository/index/rebuild", s.Rebuild, handler.MethodMeta{
		Summary:     "Rebuild repository index",
		Description: "Triggers a full re-index of the repository in the background. Returns immediately.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
}

// IndexStatus returns the current status of the repository index.
func (s *RepositoryService) IndexStatus(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.indexer == nil {
		return nil, message.NewError(message.InternalError, "Repository indexer not available")
	}

	status := s.indexer.GetStatus()
	return status, nil
}

// Search searches for files in the repository.
func (s *RepositoryService) Search(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.indexer == nil {
		return nil, message.NewError(message.InternalError, "Repository indexer not available")
	}

	var p struct {
		Query           string   `json:"query"`
		Mode            string   `json:"mode"`
		Limit           int      `json:"limit"`
		Offset          int      `json:"offset"`
		Extensions      []string `json:"extensions"`
		ExcludeBinaries *bool    `json:"exclude_binaries"`
		GitTrackedOnly  bool     `json:"git_tracked_only"`
		MinSize         int64    `json:"min_size"`
		MaxSize         int64    `json:"max_size"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.Query == "" {
		return nil, message.NewError(message.InvalidParams, "query is required")
	}

	// Build search query with defaults
	query := repository.SearchQuery{
		Query:          p.Query,
		Mode:           repository.SearchMode(p.Mode),
		Limit:          p.Limit,
		Offset:         p.Offset,
		Extensions:     p.Extensions,
		GitTrackedOnly: p.GitTrackedOnly,
		MinSize:        p.MinSize,
		MaxSize:        p.MaxSize,
	}

	// Default exclude_binaries to true if not specified
	if p.ExcludeBinaries == nil {
		query.ExcludeBinaries = true
	} else {
		query.ExcludeBinaries = *p.ExcludeBinaries
	}

	// Apply defaults for limit
	if query.Limit <= 0 {
		query.Limit = repository.DefaultSearchLimit
	}

	result, err := s.indexer.Search(ctx, query)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// ListFiles returns a paginated list of files in a directory.
func (s *RepositoryService) ListFiles(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.indexer == nil {
		return nil, message.NewError(message.InternalError, "Repository indexer not available")
	}

	var p struct {
		Directory  string   `json:"directory"`
		Recursive  bool     `json:"recursive"`
		Limit      int      `json:"limit"`
		Offset     int      `json:"offset"`
		SortBy     string   `json:"sort_by"`
		SortOrder  string   `json:"sort_order"`
		Extensions []string `json:"extensions"`
		MinSize    int64    `json:"min_size"`
		MaxSize    int64    `json:"max_size"`
	}

	// Params are optional
	_ = json.Unmarshal(params, &p)

	opts := repository.ListOptions{
		Directory:  p.Directory,
		Recursive:  p.Recursive,
		Limit:      p.Limit,
		Offset:     p.Offset,
		SortBy:     p.SortBy,
		SortOrder:  p.SortOrder,
		Extensions: p.Extensions,
		MinSize:    p.MinSize,
		MaxSize:    p.MaxSize,
	}

	// Apply defaults
	if opts.Limit <= 0 {
		opts.Limit = repository.DefaultListLimit
	}

	result, err := s.indexer.ListFiles(ctx, opts)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GetTree returns a hierarchical directory tree.
func (s *RepositoryService) GetTree(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.indexer == nil {
		return nil, message.NewError(message.InternalError, "Repository indexer not available")
	}

	var p struct {
		Path  string `json:"path"`
		Depth int    `json:"depth"`
	}

	// Params are optional
	_ = json.Unmarshal(params, &p)

	// Default depth
	if p.Depth <= 0 {
		p.Depth = 2
	}

	tree, err := s.indexer.GetTree(ctx, p.Path, p.Depth)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return tree, nil
}

// GetStats returns aggregate statistics about the repository.
func (s *RepositoryService) GetStats(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.indexer == nil {
		return nil, message.NewError(message.InternalError, "Repository indexer not available")
	}

	stats, err := s.indexer.GetStats(ctx)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return stats, nil
}

// Rebuild triggers a full re-index of the repository.
func (s *RepositoryService) Rebuild(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.indexer == nil {
		return nil, message.NewError(message.InternalError, "Repository indexer not available")
	}

	// Start rebuild in background
	go func() {
		// Use background context since the request context will be cancelled
		_ = s.indexer.FullScan(context.Background())
	}()

	return map[string]interface{}{
		"status":  "started",
		"message": "Repository index rebuild started",
	}, nil
}

// Ensure Extensions field works with comma-separated string for backwards compatibility
//
//nolint:unused
func parseExtensions(extensions interface{}) []string {
	switch v := extensions.(type) {
	case []string:
		return v
	case string:
		if v == "" {
			return nil
		}
		return strings.Split(v, ",")
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}
