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
	SessionID string `json:"sessionId"`

	// AgentType is the type of agent (claude, gemini, codex).
	AgentType string `json:"agentType"`

	// Summary is a brief description of the session.
	Summary string `json:"summary,omitempty"`

	// MessageCount is the number of messages in the session.
	MessageCount int `json:"messageCount"`

	// StartTime is when the session started.
	StartTime time.Time `json:"startTime"`

	// LastUpdated is when the session was last updated.
	LastUpdated time.Time `json:"lastUpdated"`

	// Branch is the git branch (if available).
	Branch string `json:"branch,omitempty"`

	// ProjectPath is the project this session belongs to.
	ProjectPath string `json:"projectPath,omitempty"`
}

// SessionMessage represents a unified message from any AI CLI session.
type SessionMessage struct {
	// ID is the unique message identifier.
	ID string `json:"id"`

	// SessionID is the parent session ID.
	SessionID string `json:"sessionId"`

	// Timestamp is when the message was created.
	Timestamp time.Time `json:"timestamp"`

	// Role is the message role (user, assistant, tool).
	Role string `json:"role"`

	// Content is the message content.
	Content string `json:"content,omitempty"`

	// ToolCalls contains tool invocations (if any).
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`

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

// SessionProvider defines the interface for accessing sessions.
// This is CLI-agnostic and can be implemented for Claude, Gemini, Codex, etc.
type SessionProvider interface {
	// ListSessions returns available sessions for the current project.
	ListSessions(ctx context.Context) ([]SessionInfo, error)

	// GetSession returns detailed session info.
	GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)

	// GetSessionMessages returns messages for a session.
	GetSessionMessages(ctx context.Context, sessionID string, limit, offset int) ([]SessionMessage, int, error)

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
			{Name: "agentType", Description: "Filter by agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Description: "Maximum number of sessions to return", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionListResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionListResult"}},
	})

	r.RegisterWithMeta("session/get", s.GetSession, handler.MethodMeta{
		Summary:     "Get session details",
		Description: "Returns detailed information about a specific session.",
		Params: []handler.OpenRPCParam{
			{Name: "sessionId", Description: "Session ID to retrieve", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agentType", Description: "Agent type (optional, searches all if not specified)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionInfo", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionInfo"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/messages", s.GetSessionMessages, handler.MethodMeta{
		Summary:     "Get session messages",
		Description: "Returns paginated messages for a specific session.",
		Params: []handler.OpenRPCParam{
			{Name: "sessionId", Description: "Session ID to get messages from", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agentType", Description: "Agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Description: "Maximum messages to return (default 50, max 500)", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "offset", Description: "Offset for pagination", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionMessagesResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionMessagesResult"}},
		Errors: []string{"SessionNotFound"},
	})
}

// ListSessionsParams for session/list method.
type ListSessionsParams struct {
	// AgentType filters by agent type (optional, empty = all).
	AgentType string `json:"agentType,omitempty"`

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
	SessionID string `json:"sessionId"`
	AgentType string `json:"agentType,omitempty"`
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
	SessionID string `json:"sessionId"`
	AgentType string `json:"agentType,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	Offset    int    `json:"offset,omitempty"`
}

// GetSessionMessagesResult for session/messages method.
type GetSessionMessagesResult struct {
	Messages []SessionMessage `json:"messages"`
	Total    int              `json:"total"`
	Limit    int              `json:"limit"`
	Offset   int              `json:"offset"`
	HasMore  bool             `json:"hasMore"`
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
