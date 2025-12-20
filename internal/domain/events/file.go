package events

// FileChangeType represents the type of file change.
type FileChangeType string

const (
	FileChangeCreated  FileChangeType = "created"
	FileChangeModified FileChangeType = "modified"
	FileChangeDeleted  FileChangeType = "deleted"
	FileChangeRenamed  FileChangeType = "renamed"
)

// FileChangedPayload is the payload for file_changed events.
type FileChangedPayload struct {
	Path    string         `json:"path"`
	Change  FileChangeType `json:"change"`
	Size    int64          `json:"size,omitempty"`
	OldPath string         `json:"old_path,omitempty"`
}

// FileContentPayload is the payload for file_content response events.
type FileContentPayload struct {
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	Encoding  string `json:"encoding"`
	Truncated bool   `json:"truncated"`
	Size      int64  `json:"size"`
	Error     string `json:"error,omitempty"`
}

// NewFileChangedEvent creates a new file_changed event.
func NewFileChangedEvent(path string, change FileChangeType, size int64) *BaseEvent {
	return NewEvent(EventTypeFileChanged, FileChangedPayload{
		Path:   path,
		Change: change,
		Size:   size,
	})
}

// NewFileRenamedEvent creates a new file_changed event for renamed files.
func NewFileRenamedEvent(oldPath, newPath string) *BaseEvent {
	return NewEvent(EventTypeFileChanged, FileChangedPayload{
		Path:    newPath,
		Change:  FileChangeRenamed,
		OldPath: oldPath,
	})
}

// NewFileContentEvent creates a new file_content response event.
func NewFileContentEvent(path, content string, size int64, truncated bool, requestID string) *BaseEvent {
	return NewEventWithRequestID(EventTypeFileContent, FileContentPayload{
		Path:      path,
		Content:   content,
		Encoding:  "utf-8",
		Truncated: truncated,
		Size:      size,
	}, requestID)
}

// NewFileContentErrorEvent creates a new file_content error response event.
func NewFileContentErrorEvent(path, errMsg string, requestID string) *BaseEvent {
	return NewEventWithRequestID(EventTypeFileContent, FileContentPayload{
		Path:  path,
		Error: errMsg,
	}, requestID)
}
