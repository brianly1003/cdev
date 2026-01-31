// Package events defines all event types used in cdev.
package events

import (
	"encoding/json"
	"time"
)

// EventType represents the type of event.
type EventType string

const (
	// Claude events
	EventTypeClaudeLog         EventType = "claude_log"
	EventTypeClaudeMessage     EventType = "claude_message"
	EventTypeClaudeStatus      EventType = "claude_status"
	EventTypeClaudeWaiting     EventType = "claude_waiting"
	EventTypeClaudePermission  EventType = "claude_permission"
	EventTypeClaudeSessionInfo EventType = "claude_session_info"

	// File events
	EventTypeFileChanged EventType = "file_changed"

	// Git events
	EventTypeGitDiff              EventType = "git_diff"
	EventTypeGitStatusChanged     EventType = "git_status_changed"
	EventTypeGitOperationComplete EventType = "git_operation_completed"
	EventTypeGitBranchChanged     EventType = "git_branch_changed"

	// Session events
	EventTypeSessionStart        EventType = "session_start"
	EventTypeSessionEnd          EventType = "session_end"
	EventTypeSessionStopped      EventType = "session_stopped" // Session stopped (for multi-device sync)
	EventTypeSessionWatchChanged EventType = "session_watch_changed"
	EventTypeSessionJoined       EventType = "session_joined"
	EventTypeSessionLeft         EventType = "session_left"
	EventTypeSessionIDResolved   EventType = "session_id_resolved" // Real session ID from .claude/projects
	EventTypeSessionIDTimeout    EventType = "session_id_timeout"  // Timeout waiting for real session ID
	EventTypeSessionIDFailed     EventType = "session_id_failed"   // Failed to get real session ID (user declined trust)

	// Workspace events
	EventTypeWorkspaceRemoved EventType = "workspace_removed"

	// Response events
	EventTypeStatusResponse EventType = "status_response"
	EventTypeFileContent    EventType = "file_content"
	EventTypeError          EventType = "error"

	// Connection events
	EventTypeHeartbeat EventType = "heartbeat"

	// Stream events
	EventTypeStreamReadComplete EventType = "stream_read_complete" // JSONL file reader caught up to end

	// Claude Hook events (from external Claude sessions via hooks)
	EventTypeClaudeHookSession    EventType = "claude_hook_session"    // SessionStart hook
	EventTypeClaudeHookPermission EventType = "claude_hook_permission" // Permission prompt notification
	EventTypeClaudeHookToolStart  EventType = "claude_hook_tool_start" // PreToolUse hook
	EventTypeClaudeHookToolEnd    EventType = "claude_hook_tool_end"   // PostToolUse hook
)

// Event is the base interface for all events.
type Event interface {
	// Type returns the event type.
	Type() EventType

	// Timestamp returns when the event occurred.
	Timestamp() time.Time

	// ToJSON serializes the event to JSON.
	ToJSON() ([]byte, error)

	// GetWorkspaceID returns the workspace ID (may be empty).
	GetWorkspaceID() string

	// GetSessionID returns the session ID (may be empty).
	GetSessionID() string
}

// BaseEvent contains common fields for all events.
type BaseEvent struct {
	EventType   EventType   `json:"event"`
	EventTime   time.Time   `json:"timestamp"`
	WorkspaceID string      `json:"workspace_id,omitempty"`
	SessionID   string      `json:"session_id,omitempty"`
	Payload     interface{} `json:"payload"`
	RequestID   string      `json:"request_id,omitempty"`
}

// SetContext sets the workspace and session context for an event.
func (e *BaseEvent) SetContext(workspaceID, sessionID string) {
	e.WorkspaceID = workspaceID
	e.SessionID = sessionID
}

// GetWorkspaceID returns the workspace ID.
func (e *BaseEvent) GetWorkspaceID() string {
	return e.WorkspaceID
}

// GetSessionID returns the session ID.
func (e *BaseEvent) GetSessionID() string {
	return e.SessionID
}

// Type returns the event type.
func (e *BaseEvent) Type() EventType {
	return e.EventType
}

// Timestamp returns when the event occurred.
func (e *BaseEvent) Timestamp() time.Time {
	return e.EventTime
}

// ToJSON serializes the event to JSON.
func (e *BaseEvent) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// NewEvent creates a new base event with the given type and payload.
func NewEvent(eventType EventType, payload interface{}) *BaseEvent {
	return &BaseEvent{
		EventType: eventType,
		EventTime: time.Now().UTC(),
		Payload:   payload,
	}
}

// NewEventWithRequestID creates a new event with a request ID for correlation.
func NewEventWithRequestID(eventType EventType, payload interface{}, requestID string) *BaseEvent {
	return &BaseEvent{
		EventType: eventType,
		EventTime: time.Now().UTC(),
		Payload:   payload,
		RequestID: requestID,
	}
}

// NewEventWithContext creates a new event with workspace and session context.
func NewEventWithContext(eventType EventType, payload interface{}, workspaceID, sessionID string) *BaseEvent {
	return &BaseEvent{
		EventType:   eventType,
		EventTime:   time.Now().UTC(),
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Payload:     payload,
	}
}

// --- Git Event Payloads ---

// GitStatusChangedPayload represents the payload for git_status_changed events.
type GitStatusChangedPayload struct {
	Branch        string   `json:"branch"`
	Ahead         int      `json:"ahead"`
	Behind        int      `json:"behind"`
	StagedCount   int      `json:"staged_count"`
	UnstagedCount int      `json:"unstaged_count"`
	UntrackedCount int     `json:"untracked_count"`
	HasConflicts  bool     `json:"has_conflicts"`
	ChangedFiles  []string `json:"changed_files,omitempty"`
}

// GitOperationCompletedPayload represents the payload for git_operation_completed events.
type GitOperationCompletedPayload struct {
	Operation string `json:"operation"` // stage, unstage, discard, commit, push, pull, checkout
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	// Operation-specific fields
	SHA            string   `json:"sha,omitempty"`             // for commit
	Branch         string   `json:"branch,omitempty"`          // for checkout
	FilesAffected  int      `json:"files_affected,omitempty"`  // for stage/unstage/discard/commit
	CommitsPushed  int      `json:"commits_pushed,omitempty"`  // for push
	CommitsPulled  int      `json:"commits_pulled,omitempty"`  // for pull
	ConflictedFiles []string `json:"conflicted_files,omitempty"` // for pull with conflicts
}

// GitBranchChangedPayload represents the payload for git_branch_changed events.
type GitBranchChangedPayload struct {
	FromBranch string `json:"from_branch"`
	ToBranch   string `json:"to_branch"`
	SessionID  string `json:"session_id,omitempty"`
}

// NewGitBranchChangedEvent creates a new git_branch_changed event.
func NewGitBranchChangedEvent(workspaceID, fromBranch, toBranch, sessionID string) *BaseEvent {
	return NewEventWithContext(EventTypeGitBranchChanged, GitBranchChangedPayload{
		FromBranch: fromBranch,
		ToBranch:   toBranch,
		SessionID:  sessionID,
	}, workspaceID, sessionID)
}

// --- Workspace Event Payloads ---

// WorkspaceRemovedPayload represents the payload for workspace_removed events.
type WorkspaceRemovedPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// NewWorkspaceRemovedEvent creates a new workspace_removed event.
func NewWorkspaceRemovedEvent(id, name, path string) *BaseEvent {
	return NewEventWithContext(EventTypeWorkspaceRemoved, WorkspaceRemovedPayload{
		ID:   id,
		Name: name,
		Path: path,
	}, id, "") // workspace_id = id, session_id = empty
}

// --- Stream Event Payloads ---

// StreamReadCompletePayload represents the payload for stream_read_complete events.
// This is emitted when the JSONL file reader catches up to the current end of the file.
type StreamReadCompletePayload struct {
	SessionID       string `json:"session_id"`
	MessagesEmitted int    `json:"messages_emitted"` // Number of messages read in this batch
	FileOffset      int64  `json:"file_offset"`      // Current position in file
	FileSize        int64  `json:"file_size"`        // Total file size when read
}

// NewStreamReadCompleteEvent creates a new stream_read_complete event.
func NewStreamReadCompleteEvent(sessionID string, messagesEmitted int, fileOffset, fileSize int64) *BaseEvent {
	return NewEventWithContext(EventTypeStreamReadComplete, StreamReadCompletePayload{
		SessionID:       sessionID,
		MessagesEmitted: messagesEmitted,
		FileOffset:      fileOffset,
		FileSize:        fileSize,
	}, "", sessionID)
}
