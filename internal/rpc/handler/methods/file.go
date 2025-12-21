package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// FileEntry represents a file or directory entry.
type FileEntry struct {
	// Name is the file/directory name.
	Name string `json:"name"`

	// Type is either "file" or "directory".
	Type string `json:"type"`

	// Size is the file size in bytes (only for files).
	Size *int64 `json:"size,omitempty"`

	// Modified is the last modification time (only for files).
	Modified *string `json:"modified,omitempty"`

	// ChildrenCount is the number of children (only for directories).
	ChildrenCount *int `json:"children_count,omitempty"`
}

// FileContentProvider provides file content retrieval.
type FileContentProvider interface {
	// GetFileContent returns the content of a file.
	// Returns content, truncated flag, and error.
	GetFileContent(ctx context.Context, path string, maxSizeKB int) (string, bool, error)

	// ListDirectory returns entries in a directory.
	// Returns entries, error.
	ListDirectory(ctx context.Context, path string) ([]FileEntry, error)
}

// FileService provides file-related RPC methods.
type FileService struct {
	provider  FileContentProvider
	maxSizeKB int
}

// NewFileService creates a new file service.
func NewFileService(provider FileContentProvider, maxSizeKB int) *FileService {
	return &FileService{
		provider:  provider,
		maxSizeKB: maxSizeKB,
	}
}

// RegisterMethods registers all file methods with the registry.
func (s *FileService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("file/get", s.GetFile, handler.MethodMeta{
		Summary:     "Get file content",
		Description: "Returns the content of a file from the repository.",
		Params: []handler.OpenRPCParam{
			{Name: "path", Description: "File path relative to repository root", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "FileGetResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/FileGetResult"}},
		Errors: []string{"FileNotFound"},
	})

	r.RegisterWithMeta("file/list", s.ListDirectory, handler.MethodMeta{
		Summary:     "List directory contents",
		Description: "Returns the contents of a directory in the repository. Supports browsing the file tree.",
		Params: []handler.OpenRPCParam{
			{Name: "path", Description: "Relative path from repo root (empty for root directory)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "FileListResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/FileListResult"}},
		Errors: []string{"FileNotFound"},
	})
}

// GetFileParams for file/get method.
type GetFileParams struct {
	// Path is the file path relative to the repository root.
	Path string `json:"path"`
}

// GetFileResult for file/get method.
type GetFileResult struct {
	// Path is the file path.
	Path string `json:"path"`

	// Content is the file content.
	Content string `json:"content"`

	// Encoding is the content encoding (always "utf-8").
	Encoding string `json:"encoding"`

	// Truncated indicates if the content was truncated due to size limits.
	Truncated bool `json:"truncated"`

	// Size is the content size in bytes.
	Size int `json:"size"`
}

// GetFile returns the content of a file.
// This consolidates logic from:
// - app.go:435-447 (WebSocket handler)
// - http/server.go:838-875 (HTTP handler)
func (s *FileService) GetFile(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrInternalError("File provider not available")
	}

	// Parse params
	var p GetFileParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	// Validate
	if p.Path == "" {
		return nil, message.ErrInvalidParams("path is required")
	}

	// Get file content
	content, truncated, err := s.provider.GetFileContent(ctx, p.Path, s.maxSizeKB)
	if err != nil {
		return nil, message.ErrFileNotFound(p.Path)
	}

	return GetFileResult{
		Path:      p.Path,
		Content:   content,
		Encoding:  "utf-8",
		Truncated: truncated,
		Size:      len(content),
	}, nil
}

// ListDirectoryParams for file/list method.
type ListDirectoryParams struct {
	// Path is the directory path relative to the repository root.
	// If empty, lists the root directory.
	Path string `json:"path,omitempty"`
}

// ListDirectoryResult for file/list method.
type ListDirectoryResult struct {
	// Path is the directory path.
	Path string `json:"path"`

	// Entries is the list of files and directories.
	Entries []FileEntry `json:"entries"`

	// TotalCount is the number of entries.
	TotalCount int `json:"total_count"`
}

// ListDirectory returns the contents of a directory.
func (s *FileService) ListDirectory(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrInternalError("File provider not available")
	}

	// Parse params
	var p ListDirectoryParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.ErrInvalidParams("invalid params: " + err.Error())
		}
	}

	// List directory contents
	entries, err := s.provider.ListDirectory(ctx, p.Path)
	if err != nil {
		return nil, message.ErrFileNotFound(p.Path)
	}

	return ListDirectoryResult{
		Path:       p.Path,
		Entries:    entries,
		TotalCount: len(entries),
	}, nil
}
