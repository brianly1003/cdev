package methods

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
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

	// FirstPrompt is the first user message (for Codex sessions).
	FirstPrompt string `json:"first_prompt,omitempty"`

	// MessageCount is the number of messages in the session.
	MessageCount int `json:"message_count"`

	// StartTime is when the session started.
	StartTime time.Time `json:"start_time"`

	// LastUpdated is when the session was last updated.
	LastUpdated time.Time `json:"last_updated"`

	// Branch is the git branch (if available).
	Branch string `json:"branch,omitempty"`

	// GitCommit is the git commit hash (if available).
	GitCommit string `json:"git_commit,omitempty"`

	// GitRepo is the repository URL (if available).
	GitRepo string `json:"git_repo,omitempty"`

	// ProjectPath is the project this session belongs to.
	ProjectPath string `json:"project_path,omitempty"`

	// ModelProvider is the AI provider (openai, anthropic, etc.).
	ModelProvider string `json:"model_provider,omitempty"`

	// Model is the specific model used.
	Model string `json:"model,omitempty"`

	// CLIVersion is the CLI version that created the session.
	CLIVersion string `json:"cli_version,omitempty"`

	// FileSize is the session file size in bytes.
	FileSize int64 `json:"file_size,omitempty"`

	// FilePath is the full path to the session file.
	FilePath string `json:"file_path,omitempty"`
}

// SessionMessage represents a raw cached message matching the HTTP API format.
// This preserves the full Claude API response in the Message field.
// IMPORTANT: This struct must match CachedMessage in sessioncache/messages.go
type SessionMessage struct {
	// ID is the database row ID.
	ID int64 `json:"id"`

	// SessionID is the parent session ID.
	SessionID string `json:"session_id"`

	// Type is the message type (user, assistant).
	Type string `json:"type"`

	// UUID is the unique message identifier.
	UUID string `json:"uuid,omitempty"`

	// Timestamp is when the message was created (ISO 8601 string).
	Timestamp string `json:"timestamp,omitempty"`

	// GitBranch is the git branch when message was created.
	GitBranch string `json:"git_branch,omitempty"`

	// Message is the raw Claude API message (content, role, model, usage, etc).
	Message json.RawMessage `json:"message"`

	// IsContextCompaction is true when this is an auto-generated message
	// created by Claude Code when the context window was maxed out.
	IsContextCompaction bool `json:"is_context_compaction,omitempty"`

	// IsMeta is true for system-generated metadata messages (e.g., command caveats).
	IsMeta bool `json:"is_meta,omitempty"`
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

	// Type is the element type (user_input, assistant_text, tool_call, tool_result, diff, thinking, context_compaction, interrupted).
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
	// If projectPath is provided, filters sessions to that project path.
	ListSessions(ctx context.Context, projectPath string) ([]SessionInfo, error)

	// GetSession returns detailed session info.
	GetSession(ctx context.Context, sessionID string) (*SessionInfo, error)

	// GetSessionMessages returns messages for a session.
	// Order can be "asc" or "desc" (default: "asc").
	GetSessionMessages(ctx context.Context, sessionID string, limit, offset int, order string) ([]SessionMessage, int, error)

	// GetSessionElements returns pre-parsed UI elements for a session.
	GetSessionElements(ctx context.Context, sessionID string, limit int, beforeID, afterID string) ([]SessionElement, int, error)

	// DeleteSession deletes a specific session.
	DeleteSession(ctx context.Context, sessionID string) error

	// DeleteAllSessions deletes all sessions.
	DeleteAllSessions(ctx context.Context) (int, error)

	// AgentType returns the type of agent this provider is for.
	AgentType() string
}

// SessionStreamer defines the interface for real-time session watching.
type SessionStreamer interface {
	// WatchSession starts watching a session for new messages.
	WatchSession(sessionID string) error

	// UnwatchSession stops watching the current session.
	UnwatchSession()

	// GetWatchedSession returns the currently watched session ID.
	GetWatchedSession() string
}

// WorkspacePathResolver resolves workspace path from workspace ID.
type WorkspacePathResolver interface {
	GetWorkspacePath(workspaceID string) (string, error)
}

// SessionService provides session-related RPC methods.
type SessionService struct {
	providers         map[string]SessionProvider // keyed by agent type
	streamer          SessionStreamer            // default streamer (legacy Claude)
	streamers         map[string]SessionStreamer // per-agent streamers
	workspaceResolver WorkspacePathResolver      // resolves workspace ID to path
}

func isNilSessionStreamer(streamer SessionStreamer) bool {
	if streamer == nil {
		return true
	}
	v := reflect.ValueOf(streamer)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// NewSessionService creates a new session service.
func NewSessionService(streamer SessionStreamer) *SessionService {
	if isNilSessionStreamer(streamer) {
		streamer = nil
	}
	return &SessionService{
		providers: make(map[string]SessionProvider),
		streamer:  streamer,
		streamers: make(map[string]SessionStreamer),
	}
}

// SetWorkspaceResolver sets the workspace path resolver.
func (s *SessionService) SetWorkspaceResolver(resolver WorkspacePathResolver) {
	s.workspaceResolver = resolver
}

// RegisterProvider registers a session provider for an agent type.
func (s *SessionService) RegisterProvider(provider SessionProvider) {
	s.providers[provider.AgentType()] = provider
}

// RegisterStreamer registers a streamer for a specific agent type.
func (s *SessionService) RegisterStreamer(agentType string, streamer SessionStreamer) {
	if agentType == "" || isNilSessionStreamer(streamer) {
		return
	}
	s.streamers[agentType] = streamer
}

// RegisterMethods registers all session methods with the registry.
func (s *SessionService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("session/list", s.ListSessions, handler.MethodMeta{
		Summary:     "List sessions",
		Description: "Returns a list of available sessions from all configured AI agents. For Codex, sessions include rich metadata (git info, model, first prompt).",
		Params: []handler.OpenRPCParam{
			{Name: "agent_type", Description: "Filter by agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "project_path", Description: "Filter by project path (optional, for Codex sessions)", Required: false, Schema: map[string]interface{}{"type": "string"}},
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
			{Name: "order", Description: "Sort order: 'asc' or 'desc' (default 'asc')", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
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

	r.RegisterWithMeta("session/watch", s.WatchSession, handler.MethodMeta{
		Summary:     "Start watching a session",
		Description: "Starts watching a session for real-time updates. The client will receive notifications when new messages are added.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Description: "Session ID to watch", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Description: "Agent type (optional)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionWatchResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionWatchResult"}},
		Errors: []string{"SessionNotFound"},
	})

	r.RegisterWithMeta("session/unwatch", s.UnwatchSession, handler.MethodMeta{
		Summary:     "Stop watching session",
		Description: "Stops watching the current session.",
		Params: []handler.OpenRPCParam{
			{Name: "agent_type", Description: "Agent type to unwatch (optional, unwatchs all when omitted)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "SessionUnwatchResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/SessionUnwatchResult"}},
	})
}

// ListSessionsParams for session/list method.
type ListSessionsParams struct {
	// AgentType filters by agent type (optional, empty = all).
	AgentType string `json:"agent_type,omitempty"`

	// WorkspaceID filters by workspace (server resolves path automatically).
	WorkspaceID string `json:"workspace_id,omitempty"`

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

	// Resolve project path from workspace ID if provided
	var projectPath string
	if p.WorkspaceID != "" && s.workspaceResolver != nil {
		if resolvedPath, err := s.workspaceResolver.GetWorkspacePath(p.WorkspaceID); err == nil {
			projectPath = resolvedPath
		}
	}

	var allSessions []SessionInfo

	// Collect sessions from all providers (or filtered by agent type)
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		sessions, err := provider.ListSessions(ctx, projectPath)
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
	Order     string `json:"order,omitempty"` // "asc" or "desc" (default: "asc")
}

// GetSessionMessagesResult for session/messages method.
type GetSessionMessagesResult struct {
	SessionID   string           `json:"session_id"`
	Messages    []SessionMessage `json:"messages"`
	Total       int              `json:"total"`
	Limit       int              `json:"limit"`
	Offset      int              `json:"offset"`
	HasMore     bool             `json:"has_more"`
	QueryTimeMs float64          `json:"query_time_ms"`
}

// GetSessionMessages returns messages for a session.
func (s *SessionService) GetSessionMessages(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	startTime := time.Now()

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

	// Default order is "asc"
	order := p.Order
	if order == "" {
		order = "asc"
	}

	// Try each provider until we find the session
	for agentType, provider := range s.providers {
		if p.AgentType != "" && p.AgentType != agentType {
			continue
		}

		messages, total, err := provider.GetSessionMessages(ctx, p.SessionID, limit, p.Offset, order)
		if err == nil {
			queryTimeMs := float64(time.Since(startTime).Microseconds()) / 1000.0
			return GetSessionMessagesResult{
				SessionID:   p.SessionID,
				Messages:    messages,
				Total:       total,
				Limit:       limit,
				Offset:      p.Offset,
				HasMore:     p.Offset+len(messages) < total,
				QueryTimeMs: queryTimeMs,
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

// WatchSessionParams for session/watch method.
type WatchSessionParams struct {
	SessionID string `json:"session_id"`
	AgentType string `json:"agent_type,omitempty"`
}

// WatchSessionResult for session/watch method.
type WatchSessionResult struct {
	Status   string `json:"status"`
	Watching bool   `json:"watching"`
}

// WatchSession starts watching a session for real-time updates.
// This method validates the session exists and starts the file watcher.
func (s *SessionService) WatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p WatchSessionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("session_id is required")
	}

	// Verify session exists by trying to get it
	sessionFound := false
	matchedAgentType := ""
	agentType := strings.TrimSpace(p.AgentType)

	if agentType != "" {
		provider, ok := s.providers[agentType]
		if !ok {
			return nil, message.ErrInvalidParams("invalid agent_type: " + agentType)
		}
		session, err := provider.GetSession(ctx, p.SessionID)
		if err == nil && session != nil {
			sessionFound = true
			matchedAgentType = agentType
		}
	} else {
		for _, provider := range s.providers {
			session, err := provider.GetSession(ctx, p.SessionID)
			if err == nil && session != nil {
				sessionFound = true
				matchedAgentType = provider.AgentType()
				break
			}
		}
	}

	if !sessionFound {
		return nil, message.ErrSessionNotFound(p.SessionID)
	}

	// Start watching the session file for real-time updates
	streamer := s.streamers[matchedAgentType]
	if isNilSessionStreamer(streamer) {
		streamer = s.streamer
	}
	if !isNilSessionStreamer(streamer) {
		if err := streamer.WatchSession(p.SessionID); err != nil {
			return nil, message.ErrInternalError("failed to watch session: " + err.Error())
		}
	}

	return WatchSessionResult{
		Status:   "watching",
		Watching: true,
	}, nil
}

// UnwatchSessionResult for session/unwatch method.
type UnwatchSessionResult struct {
	Status   string `json:"status"`
	Watching bool   `json:"watching"`
}

// UnwatchSession stops watching the current session.
func (s *SessionService) UnwatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		AgentType string `json:"agent_type,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.ErrInvalidParams("invalid params: " + err.Error())
		}
	}

	agentType := strings.TrimSpace(p.AgentType)
	if agentType != "" {
		if streamer, ok := s.streamers[agentType]; ok && !isNilSessionStreamer(streamer) {
			streamer.UnwatchSession()
		}
		if agentType == "claude" && !isNilSessionStreamer(s.streamer) {
			s.streamer.UnwatchSession()
		}
	} else {
		// Legacy behavior: stop all streamers when agent_type is omitted.
		if !isNilSessionStreamer(s.streamer) {
			s.streamer.UnwatchSession()
		}
		for _, streamer := range s.streamers {
			if !isNilSessionStreamer(streamer) {
				streamer.UnwatchSession()
			}
		}
	}

	return UnwatchSessionResult{
		Status:   "unwatched",
		Watching: false,
	}, nil
}
