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
