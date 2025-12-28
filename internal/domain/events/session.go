package events

import "time"

// SessionStartPayload is the payload for session_start events.
type SessionStartPayload struct {
	SessionID    string `json:"session_id"`
	RepoPath     string `json:"repo_path"`
	RepoName     string `json:"repo_name"`
	AgentVersion string `json:"agent_version"`
}

// SessionEndPayload is the payload for session_end events.
type SessionEndPayload struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

// StatusResponsePayload is the payload for status_response events.
type StatusResponsePayload struct {
	ClaudeState      ClaudeState `json:"claude_state"`
	ConnectedClients int         `json:"connected_clients"`
	RepoPath         string      `json:"repo_path"`
	RepoName         string      `json:"repo_name"`
	UptimeSeconds    int64       `json:"uptime_seconds"`
	AgentVersion     string      `json:"agent_version"`
	WatcherEnabled   bool        `json:"watcher_enabled"`
	GitEnabled       bool        `json:"git_enabled"`
}

// ErrorPayload is the payload for error events.
type ErrorPayload struct {
	Code      string                 `json:"code"`
	Message   string                 `json:"message"`
	RequestID string                 `json:"request_id,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// HeartbeatPayload is the payload for heartbeat events.
// Heartbeats are sent periodically to allow clients to detect connection issues
// at the application level (beyond WebSocket ping/pong frames).
type HeartbeatPayload struct {
	ServerTime   string `json:"server_time"`
	Sequence     int64  `json:"sequence"`
	ClaudeStatus string `json:"claude_status"`
	Uptime       int64  `json:"uptime_seconds"`
}

// SessionJoinedPayload is the payload for session_joined events.
// Sent when a device joins a session that other devices are viewing.
type SessionJoinedPayload struct {
	JoiningClientID string   `json:"joining_client_id"`
	OtherViewers    []string `json:"other_viewers"`
	ViewerCount     int      `json:"viewer_count"`
}

// SessionLeftPayload is the payload for session_left events.
// Sent when a device leaves a session that other devices are viewing.
type SessionLeftPayload struct {
	LeavingClientID string   `json:"leaving_client_id"`
	RemainingViewers []string `json:"remaining_viewers"`
	ViewerCount     int      `json:"viewer_count"`
}

// NewSessionStartEvent creates a new session_start event.
func NewSessionStartEvent(sessionID, repoPath, repoName, agentVersion string) *BaseEvent {
	return NewEvent(EventTypeSessionStart, SessionStartPayload{
		SessionID:    sessionID,
		RepoPath:     repoPath,
		RepoName:     repoName,
		AgentVersion: agentVersion,
	})
}

// NewSessionEndEvent creates a new session_end event.
func NewSessionEndEvent(sessionID, reason string) *BaseEvent {
	return NewEvent(EventTypeSessionEnd, SessionEndPayload{
		SessionID: sessionID,
		Reason:    reason,
	})
}

// NewStatusResponseEvent creates a new status_response event.
func NewStatusResponseEvent(payload StatusResponsePayload, requestID string) *BaseEvent {
	return NewEventWithRequestID(EventTypeStatusResponse, payload, requestID)
}

// NewErrorEvent creates a new error event.
func NewErrorEvent(code, message string, requestID string, details map[string]interface{}) *BaseEvent {
	return NewEventWithRequestID(EventTypeError, ErrorPayload{
		Code:      code,
		Message:   message,
		RequestID: requestID,
		Details:   details,
	}, requestID)
}

// NewHeartbeatEvent creates a new heartbeat event.
func NewHeartbeatEvent(sequence int64, claudeStatus string, uptimeSeconds int64) *BaseEvent {
	return NewEvent(EventTypeHeartbeat, HeartbeatPayload{
		ServerTime:   time.Now().UTC().Format(time.RFC3339),
		Sequence:     sequence,
		ClaudeStatus: claudeStatus,
		Uptime:       uptimeSeconds,
	})
}

// NewSessionJoinedEvent creates a new session_joined event.
// Sent when a device joins a session that other devices are viewing.
func NewSessionJoinedEvent(joiningClientID, workspaceID, sessionID string, otherViewers []string) *BaseEvent {
	event := NewEventWithContext(EventTypeSessionJoined, SessionJoinedPayload{
		JoiningClientID: joiningClientID,
		OtherViewers:    otherViewers,
		ViewerCount:     len(otherViewers) + 1,
	}, workspaceID, sessionID)
	return event
}

// NewSessionLeftEvent creates a new session_left event.
// Sent when a device leaves a session that other devices are viewing.
func NewSessionLeftEvent(leavingClientID, workspaceID, sessionID string, remainingViewers []string) *BaseEvent {
	event := NewEventWithContext(EventTypeSessionLeft, SessionLeftPayload{
		LeavingClientID:  leavingClientID,
		RemainingViewers: remainingViewers,
		ViewerCount:      len(remainingViewers),
	}, workspaceID, sessionID)
	return event
}

// SessionStoppedPayload is the payload for session_stopped events.
// Emitted when a session is stopped - allows all connected devices to sync their UI.
type SessionStoppedPayload struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
}

// NewSessionStoppedEvent creates a new session_stopped event.
// This is broadcast to all connected clients so they can update their UI (e.g., show "idle" instead of "active").
func NewSessionStoppedEvent(workspaceID, sessionID string) *BaseEvent {
	return NewEventWithContext(EventTypeSessionStopped, SessionStoppedPayload{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
	}, workspaceID, sessionID)
}

// SessionIDResolvedPayload is the payload for session_id_resolved events.
// Emitted when the real Claude session ID is detected from .claude/projects.
type SessionIDResolvedPayload struct {
	TemporaryID  string `json:"temporary_id"`  // The temporary ID generated by cdev
	RealID       string `json:"real_id"`       // The real session ID from Claude
	WorkspaceID  string `json:"workspace_id"`
	SessionFile  string `json:"session_file"`  // Path to .jsonl file
}

// NewSessionIDResolvedEvent creates a new session_id_resolved event.
func NewSessionIDResolvedEvent(temporaryID, realID, workspaceID, sessionFile string) *BaseEvent {
	return NewEventWithContext(EventTypeSessionIDResolved, SessionIDResolvedPayload{
		TemporaryID: temporaryID,
		RealID:      realID,
		WorkspaceID: workspaceID,
		SessionFile: sessionFile,
	}, workspaceID, realID)
}

// SessionIDTimeoutPayload is the payload for session_id_timeout events.
// Emitted when the watcher times out waiting for a real session ID.
type SessionIDTimeoutPayload struct {
	TemporaryID   string `json:"temporary_id"`
	WorkspaceID   string `json:"workspace_id"`
	TimeoutSeconds int   `json:"timeout_seconds"`
	Reason        string `json:"reason"` // "timeout" or "cancelled"
}

// NewSessionIDTimeoutEvent creates a new session_id_timeout event.
func NewSessionIDTimeoutEvent(temporaryID, workspaceID string, timeoutSeconds int, reason string) *BaseEvent {
	return NewEventWithContext(EventTypeSessionIDTimeout, SessionIDTimeoutPayload{
		TemporaryID:   temporaryID,
		WorkspaceID:   workspaceID,
		TimeoutSeconds: timeoutSeconds,
		Reason:        reason,
	}, workspaceID, temporaryID)
}

// SessionIDFailedPayload is the payload for session_id_failed events.
// Emitted when the user declines trust folder or Claude exits without creating a session.
type SessionIDFailedPayload struct {
	TemporaryID string `json:"temporary_id"`
	WorkspaceID string `json:"workspace_id"`
	Reason      string `json:"reason"` // "trust_declined", "claude_exited", "error"
	Message     string `json:"message,omitempty"`
}

// NewSessionIDFailedEvent creates a new session_id_failed event.
func NewSessionIDFailedEvent(temporaryID, workspaceID, reason, message string) *BaseEvent {
	return NewEventWithContext(EventTypeSessionIDFailed, SessionIDFailedPayload{
		TemporaryID: temporaryID,
		WorkspaceID: workspaceID,
		Reason:      reason,
		Message:     message,
	}, workspaceID, temporaryID)
}
