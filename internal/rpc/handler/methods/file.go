package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// FileContentProvider provides file content retrieval.
type FileContentProvider interface {
	// GetFileContent returns the content of a file.
	// Returns content, truncated flag, and error.
	GetFileContent(ctx context.Context, path string, maxSizeKB int) (string, bool, error)
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
