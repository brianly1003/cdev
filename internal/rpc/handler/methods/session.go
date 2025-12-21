package methods

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// SessionInfo represents a unified session from any AI CLI.
type SessionInfo struct {
	// SessionID is the unique identifier.
	SessionID string `json:"session_id"`

	// AgentType is the type of agent (claude, gemini, codex).
	AgentType string `json:"agent_type"`

	// Summary is a brief description of the session.
	Summary string `json:"summary,omitempty"`

	// MessageCount is the number of messages in the session.
	MessageCount int `json:"message_count"`

	// StartTime is when the session started.
	StartTime time.Time `json:"start_time"`

	// LastUpdated is when the session was last updated.
	LastUpdated time.Time `json:"last_updated"`

	// Branch is the git branch (if available).
	Branch string `json:"branch,omitempty"`

	// ProjectPath is the project this session belongs to.
	ProjectPath string `json:"project_path,omitempty"`
}

// SessionMessage represents a unified message from any AI CLI session.
type SessionMessage struct {
	// ID is the unique message identifier.
	ID string `json:"id"`

	// SessionID is the parent session ID.
	SessionID string `json:"session_id"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`

	// Role is the message role (user, assistant, tool).
	Role string `json:"role"`

	// Content is the message content.
	Content string `json:"content,omitempty"`

	// ToolCalls contains tool invocations (if any).
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// Thinking contains agent reasoning (if available).
	Thinking string `json:"thinking,omitempty"`

	// Model is the model used (if known).
	Model string `json:"model,omitempty"`

	// Tokens contains token usage info (if available).
	Tokens *TokenUsage `json:"tokens,omitempty"`
}

// ToolCall represents a tool invocation.
type ToolCall struct {
	// ID is the tool use ID.
	ID string `json:"id"`

	// Name is the tool name.
	Name string `json:"name"`

	// Input is the tool input arguments.
	Input map[string]interface{} `json:"input,omitempty"`

	// Result is the tool result (if completed).
	Result string `json:"result,omitempty"`

	// Status is the tool call status.
	Status string `json:"status,omitempty"`
}

// TokenUsage represents token usage for a message.
type TokenUsage struct {
	Input    int `json:"input,omitempty"`
	Output   int `json:"output,omitempty"`
	Cached   int `json:"cached,omitempty"`
	Thinking int `json:"thinking,omitempty"`
	Total    int `json:"total,omitempty"`
}

// SessionElement represents a pre-parsed UI element for rendering.
type SessionElement struct {
	// ID is the element identifier.
	ID string `json:"id"`

	// Type is the element type (user_input, assistant_text, tool_call, tool_result, diff, thinking).
	Type string `json:"type"`

	// Timestamp is when the element was created.
	Timestamp string `json:"timestamp"`

	// Content is the type-specific content.
	Content interface{} `json:"content"`
}

// SessionProvider defines the interface for accessing sessions.
// This is CLI-agnostic and can be implemented for Claude, Gemini, Codex, etc.
type SessionProvider interface {
	// ListSessions returns available sessions for the current project.
	ListSessions(ctx context.Context) ([]SessionInfo, error)

	// GetSession returns detailed session info.
	GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)

	// GetSessionMessages returns messages for a session.
	GetSessionMessages(ctx context.Context, sessionID string, limit, offset int) ([]SessionMessage, int, error)

	// GetSessionElements returns pre-parsed UI elements for a session.
	GetSessionElements(ctx context.Context, sessionID string, limit int, beforeID, afterID string) ([]SessionElement, int, error)

	// DeleteSession deletes a specific session.
	DeleteSession(ctx context.Context, sessionID string) error

	// DeleteAllSessions deletes all sessions.
	DeleteAllSessions(ctx context.Context) (int, error)

	// AgentType returns the type of agent this provider is for.
	AgentType() string
}

// SessionService provides session-related RPC methods.
type SessionService struct {
	providers map[string]SessionProvider // keyed by agent type
}

// NewSessionService creates a new session service.
func NewSessionService() *SessionService {
	return &SessionService{
		providers: make(map[string]SessionProvider),
	}
}

// RegisterProvider registers a session provider for an agent type.
func (s *SessionService) RegisterProvider(provider SessionProvider) {
	s.providers[provider.AgentType()] = provider
}

// RegisterMethods registers all session methods with the registry.
func (s *SessionService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("session/list", s.ListSessions, handler.MethodMeta{
		Summary:     "List sessions",
		Description: "Returns a list of available sessions from all configured AI agents.",
		Params: []handler.OpenRPCParam{
			{Name: "agent_type", Description: "Filter by agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Description: "Maximum number of sessions to return", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionListResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionListResult"}},
	})

	r.RegisterWithMeta("session/get", s.GetSession, handler.MethodMeta{
		Summary:     "Get session details",
		Description: "Returns detailed information about a specific session.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Description: "Session ID to retrieve", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Description: "Agent type (optional, searches all if not specified)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionInfo", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionInfo"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/messages", s.GetSessionMessages, handler.MethodMeta{
		Summary:     "Get session messages",
		Description: "Returns paginated messages for a specific session.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Description: "Session ID to get messages from", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Description: "Agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Description: "Maximum messages to return (default 50, max 500)", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "offset", Description: "Offset for pagination", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionMessagesResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionMessagesResult"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/elements", s.GetSessionElements, handler.MethodMeta{
		Summary:     "Get session UI elements",
		Description: "Returns pre-parsed UI elements for a session, ready for rendering in mobile apps.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Description: "Session ID to get elements from", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Description: "Agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Description: "Maximum elements to return (default 50, max 100)", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "before", Description: "Return elements before this ID (for pagination)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "after", Description: "Return elements after this ID (for catch-up)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionElementsResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionElementsResult"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/delete", s.DeleteSession, handler.MethodMeta{
		Summary:     "Delete session(s)",
		Description: "Deletes a specific session or all sessions if no session_id is provided.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Description: "Session ID to delete (optional, deletes all if not provided)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Description: "Agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionDeleteResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionDeleteResult"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/watch", s.WatchSession, handler.MethodMeta{
		Summary:     "Start watching a session",
		Description: "Starts watching a session for real-time updates. The client will receive notifications when new messages are added.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Description: "Session ID to watch", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionWatchResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionWatchResult"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/unwatch", s.UnwatchSession, handler.MethodMeta{
		Summary:     "Stop watching session",
		Description: "Stops watching the current session.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "SessionUnwatchResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionUnwatchResult"}},
	})
}

// ListSessionsParams for session/list method.
type ListSessionsParams struct {
	// AgentType filters by agent type (optional, empty = all).
	AgentType string `json:"agent_type,omitempty"`

	// Limit is the max number of sessions to return.
	Limit int `json:"limit,omitempty"`
}

// ListSessionsResult for session/list method.
type ListSessionsResult struct {
	Sessions []SessionInfo `json:"sessions"`
	Total    int           `json:"total"`
}

// ListSessions returns available sessions.
func (s *SessionService) ListSessions(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p ListSessionsParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.ErrInvalidParams("invalid params: " + err.Error())
		}
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}

	var allSessions []SessionInfo

	// Collect sessions from all providers (or filtered by agent type)
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		sessions, err := provider.ListSessions(ctx)
		if err != nil {
			continue // Skip providers that fail
		}
		allSessions = append(allSessions, sessions...)
	}

	// Sort by last updated (most recent first)
	sortSessionsByLastUpdated(allSessions)

	// Apply limit
	total := len(allSessions)
	if len(allSessions) > limit {
		allSessions = allSessions[:limit]
	}

	return ListSessionsResult{
		Sessions: allSessions,
		Total:    total,
	}, nil
}

// GetSessionParams for session/get method.
type GetSessionParams struct {
	SessionID string `json:"session_id"`
	AgentType string `json:"agent_type,omitempty"`
}

// GetSession returns detailed session info.
func (s *SessionService) GetSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p GetSessionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("sessionId is required")
	}

	// Try each provider until we find the session
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		session, err := provider.GetSession(ctx, p.SessionID)
		if err == nil && session != nil {
			return session, nil
		}
	}

	return nil, message.ErrSessionNotFound(p.SessionID)
}

// GetSessionMessagesParams for session/messages method.
type GetSessionMessagesParams struct {
	SessionID string `json:"session_id"`
	AgentType string `json:"agent_type,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// GetSessionMessagesResult for session/messages method.
type GetSessionMessagesResult struct {
	Messages []SessionMessage `json:"messages"`
	Total    int              `json:"total"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
	HasMore  bool             `json:"has_more"`
}

// GetSessionMessages returns messages for a session.
func (s *SessionService) GetSessionMessages(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p GetSessionMessagesParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("sessionId is required")
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	// Try each provider until we find the session
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		messages, total, err := provider.GetSessionMessages(ctx, p.SessionID, limit, p.Offset)
		if err == nil {
			return GetSessionMessagesResult{
				Messages: messages,
				Total:    total,
				Limit:    limit,
				Offset:   p.Offset,
				HasMore:  p.Offset+len(messages) < total,
			}, nil
		}
	}

	return nil, message.ErrSessionNotFound(p.SessionID)
}

// sortSessionsByLastUpdated sorts sessions by last updated time (most recent first).
func sortSessionsByLastUpdated(sessions []SessionInfo) {
	for i := 0; i < len(sessions)-1; i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].LastUpdated.After(sessions[i].LastUpdated) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}
}

// GetSessionElementsParams for session/elements method.
type GetSessionElementsParams struct {
	SessionID string `json:"session_id"`
	AgentType string `json:"agent_type,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
}

// SessionElementsPagination represents pagination info for elements.
type SessionElementsPagination struct {
	Total         int    `json:"total"`
	Returned      int    `json:"returned"`
	HasMoreBefore bool   `json:"has_more_before"`
	HasMoreAfter  bool   `json:"has_more_after"`
	OldestID      string `json:"oldest_id,omitempty"`
	NewestID      string `json:"newest_id,omitempty"`
}

// GetSessionElementsResult for session/elements method.
type GetSessionElementsResult struct {
	SessionID  string                    `json:"session_id"`
	Elements   []SessionElement          `json:"elements"`
	Pagination SessionElementsPagination `json:"pagination"`
}

// GetSessionElements returns pre-parsed UI elements for a session.
func (s *SessionService) GetSessionElements(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p GetSessionElementsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("session_id is required")
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	// Try each provider until we find the session
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		elements, total, err := provider.GetSessionElements(ctx, p.SessionID, limit, p.Before, p.After)
		if err == nil {
			pagination := SessionElementsPagination{
				Total:    total,
				Returned: len(elements),
			}

			if len(elements) > 0 {
				pagination.OldestID = elements[0].ID
				pagination.NewestID = elements[len(elements)-1].ID

				// Determine if there's more before/after
				// This is a simplified heuristic - providers should implement proper pagination
				if p.Before != "" {
					pagination.HasMoreBefore = true // There's always more before if we used before cursor
				}
				if p.After != "" || len(elements) < total {
					pagination.HasMoreAfter = len(elements)+getOffsetFromCursor(p.After, elements) < total
				}
			}

			return GetSessionElementsResult{
				SessionID:  p.SessionID,
				Elements:   elements,
				Pagination: pagination,
			}, nil
		}
	}

	return nil, message.ErrSessionNotFound(p.SessionID)
}

// getOffsetFromCursor is a helper to estimate offset from cursor position.
func getOffsetFromCursor(afterID string, elements []SessionElement) int {
	if afterID == "" {
		return 0
	}
	// Simple heuristic - in real implementation, providers track actual positions
	for i, elem := range elements {
		if elem.ID == afterID {
			return i
		}
	}
	return 0
}

// DeleteSessionParams for session/delete method.
type DeleteSessionParams struct {
	SessionID string `json:"session_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

// DeleteSessionResult for session/delete method.
type DeleteSessionResult struct {
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	Deleted   int    `json:"deleted,omitempty"`
}

// DeleteSession deletes a specific session or all sessions.
func (s *SessionService) DeleteSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p DeleteSessionParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.ErrInvalidParams("invalid params: " + err.Error())
		}
	}

	if p.SessionID != "" {
		// Delete specific session
		for agentType, provider := range s.providers {
			if p.AgentType != "" && p.AgentType != agentType {
				continue
			}

			if err := provider.DeleteSession(ctx, p.SessionID); err == nil {
				return DeleteSessionResult{
					Status:    "deleted",
					SessionID: p.SessionID,
					Deleted:   1,
				}, nil
			}
		}
		return nil, message.ErrSessionNotFound(p.SessionID)
	}

	// Delete all sessions
	totalDeleted := 0
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		deleted, err := provider.DeleteAllSessions(ctx)
		if err == nil {
			totalDeleted += deleted
		}
	}

	return DeleteSessionResult{
		Status:  "deleted",
		Deleted: totalDeleted,
	}, nil
}

// WatchSessionParams for session/watch method.
type WatchSessionParams struct {
	SessionID string `json:"session_id"`
}

// WatchSessionResult for session/watch method.
type WatchSessionResult struct {
	Status   string `json:"status"`
	Watching bool   `json:"watching"`
}

// WatchSession starts watching a session for real-time updates.
// Note: The actual watching is handled by the WebSocket connection management.
// This method validates the session exists and signals intent to watch.
func (s *SessionService) WatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p WatchSessionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("session_id is required")
	}

	// Verify session exists by trying to get it
	for _, provider := range s.providers {
		session, err := provider.GetSession(ctx, p.SessionID)
		if err == nil && session != nil {
			// Session exists - watching is handled by WebSocket layer
			return WatchSessionResult{
				Status:   "watching",
				Watching: true,
			}, nil
		}
	}

	return nil, message.ErrSessionNotFound(p.SessionID)
}

// UnwatchSessionResult for session/unwatch method.
type UnwatchSessionResult struct {
	Status string `json:"status"`
}

// UnwatchSession stops watching the current session.
// Note: The actual unwatching is handled by the WebSocket connection management.
func (s *SessionService) UnwatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	// Unwatching is handled by the WebSocket layer
	// This method just acknowledges the request
	return UnwatchSessionResult{
		Status: "unwatched",
	}, nil
}
