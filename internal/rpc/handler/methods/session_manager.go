// Package methods provides JSON-RPC method implementations for the session-based architecture.
package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/session"
)

// SessionManagerService handles session-related JSON-RPC methods for the new architecture.
// This replaces the old workspace start/stop with session-based management.
type SessionManagerService struct {
	manager *session.Manager
}

// NewSessionManagerService creates a new session manager service.
func NewSessionManagerService(manager *session.Manager) *SessionManagerService {
	return &SessionManagerService{
		manager: manager,
	}
}

// RegisterMethods registers all session management methods with the handler.
func (s *SessionManagerService) RegisterMethods(registry *handler.Registry) {
	// Session lifecycle methods
	registry.RegisterWithMeta("session/start", s.Start, handler.MethodMeta{
		Summary:     "Start a Claude session for a workspace",
		Description: "Starts a new Claude CLI session for the specified workspace. Only one session per workspace is allowed.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "session",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/stop", s.Stop, handler.MethodMeta{
		Summary:     "Stop a Claude session",
		Description: "Stops a running Claude CLI session.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/send", s.Send, handler.MethodMeta{
		Summary:     "Send a prompt to a session",
		Description: "Sends a prompt to the Claude CLI in the specified session.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "prompt", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "mode", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"new", "continue"}}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/respond", s.Respond, handler.MethodMeta{
		Summary:     "Respond to a permission or question",
		Description: "Responds to a pending permission request or interactive question from Claude.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "type", Required: true, Schema: map[string]interface{}{"type": "string", "enum": []string{"permission", "question"}}},
			{Name: "response", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/active", s.Active, handler.MethodMeta{
		Summary:     "List active sessions",
		Description: "Returns a list of all active sessions, optionally filtered by workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "sessions",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/info", s.Info, handler.MethodMeta{
		Summary:     "Get session info",
		Description: "Returns detailed information about a specific session.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "session",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/state", s.State, handler.MethodMeta{
		Summary:     "Get session runtime state for reconnection",
		Description: "Returns the full runtime state of a session including Claude state, pending tool use, and waiting status. Use this to sync state when reconnecting from a mobile device.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "state",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/history", s.History, handler.MethodMeta{
		Summary:     "Get historical Claude sessions for a workspace",
		Description: "Returns a list of historical Claude sessions from the session cache for the specified workspace. Sessions are stored at ~/.claude/projects/<encoded-path>.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "history",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/messages", s.GetSessionMessages, handler.MethodMeta{
		Summary:     "Get messages from a historical Claude session",
		Description: "Returns paginated messages from a Claude session file for the specified workspace. Use session/history to get available session IDs.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "offset", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
			{Name: "order", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "messages",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/watch", s.WatchSession, handler.MethodMeta{
		Summary:     "Start watching a session for real-time updates",
		Description: "Starts watching a session file for new messages. The client will receive claude_message events when new messages are added. Only one session can be watched at a time.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "watch_info",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/unwatch", s.UnwatchSession, handler.MethodMeta{
		Summary:     "Stop watching the current session",
		Description: "Stops watching the currently watched session. No more claude_message events will be sent for that session.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "unwatch_info",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	// Git methods with workspace context
	registry.RegisterWithMeta("workspace/git/status", s.GitStatus, handler.MethodMeta{
		Summary:     "Get git status for a workspace",
		Description: "Returns the git status for the specified workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "status",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/diff", s.GitDiff, handler.MethodMeta{
		Summary:     "Get git diff for a workspace",
		Description: "Returns the git diff for the specified workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "staged", Required: false, Schema: map[string]interface{}{"type": "boolean"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "diff",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/stage", s.GitStage, handler.MethodMeta{
		Summary:     "Stage files for a workspace",
		Description: "Stages the specified files for commit in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "paths", Required: true, Schema: map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/unstage", s.GitUnstage, handler.MethodMeta{
		Summary:     "Unstage files for a workspace",
		Description: "Unstages the specified files in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "paths", Required: true, Schema: map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/discard", s.GitDiscard, handler.MethodMeta{
		Summary:     "Discard changes for a workspace",
		Description: "Discards uncommitted changes to the specified files in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "paths", Required: true, Schema: map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/commit", s.GitCommit, handler.MethodMeta{
		Summary:     "Commit staged changes for a workspace",
		Description: "Creates a commit with the staged changes in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "message", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "push", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/push", s.GitPush, handler.MethodMeta{
		Summary:     "Push commits for a workspace",
		Description: "Pushes commits to the remote repository for the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "force", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
			{Name: "set_upstream", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
			{Name: "remote", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "branch", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/pull", s.GitPull, handler.MethodMeta{
		Summary:     "Pull changes for a workspace",
		Description: "Pulls changes from the remote repository for the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "rebase", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/branches", s.GitBranches, handler.MethodMeta{
		Summary:     "List branches for a workspace",
		Description: "Returns the list of git branches for the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/git/checkout", s.GitCheckout, handler.MethodMeta{
		Summary:     "Checkout a branch for a workspace",
		Description: "Checks out the specified branch in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "branch", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "create", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
}

// Start starts a new Claude session for a workspace.
func (s *SessionManagerService) Start(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	sess, err := s.manager.StartSession(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return sess.ToInfo(), nil
}

// Stop stops a running session.
func (s *SessionManagerService) Stop(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	if err := s.manager.StopSession(p.SessionID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success": true,
		"message": "Session stopped",
	}, nil
}

// Send sends a prompt to a session.
func (s *SessionManagerService) Send(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
		Prompt    string `json:"prompt"`
		Mode      string `json:"mode"` // "new" or "continue"
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}
	if p.Prompt == "" {
		return nil, message.NewError(message.InvalidParams, "prompt is required")
	}

	if err := s.manager.SendPrompt(p.SessionID, p.Prompt, p.Mode); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"status": "sent",
	}, nil
}

// Respond responds to a permission or question.
func (s *SessionManagerService) Respond(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
		Type      string `json:"type"`     // "permission" or "question"
		Response  string `json:"response"` // "yes"/"no" for permission, or text for question
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}
	if p.Type == "" {
		return nil, message.NewError(message.InvalidParams, "type is required")
	}
	if p.Response == "" {
		return nil, message.NewError(message.InvalidParams, "response is required")
	}

	var err error
	switch p.Type {
	case "permission":
		allow := p.Response == "yes" || p.Response == "true" || p.Response == "allow"
		err = s.manager.RespondToPermission(p.SessionID, allow)
	case "question":
		err = s.manager.RespondToQuestion(p.SessionID, p.Response)
	default:
		return nil, message.NewError(message.InvalidParams, "type must be 'permission' or 'question'")
	}

	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"status": "responded",
	}, nil
}

// Active returns a list of active sessions.
func (s *SessionManagerService) Active(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	// Params are optional
	json.Unmarshal(params, &p)

	sessions := s.manager.ListSessions(p.WorkspaceID)
	return map[string]interface{}{
		"sessions": sessions,
	}, nil
}

// Info returns detailed information about a session.
func (s *SessionManagerService) Info(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	sess, err := s.manager.GetSession(p.SessionID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return sess.ToInfo(), nil
}

// State returns the full runtime state of a session for reconnection sync.
func (s *SessionManagerService) State(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	sess, err := s.manager.GetSession(p.SessionID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return sess.ToRuntimeState(), nil
}

// History returns historical Claude sessions for a workspace.
func (s *SessionManagerService) History(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Limit       int    `json:"limit"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Default limit
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}

	history, err := s.manager.ListHistory(p.WorkspaceID, limit)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"sessions": history,
		"total":    len(history),
	}, nil
}

// GetSessionMessages returns messages from a historical Claude session.
func (s *SessionManagerService) GetSessionMessages(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"`
		Limit       int    `json:"limit"`
		Offset      int    `json:"offset"`
		Order       string `json:"order"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	// Default values
	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	order := p.Order
	if order == "" {
		order = "asc"
	}

	result, err := s.manager.GetSessionMessages(p.WorkspaceID, p.SessionID, limit, p.Offset, order)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitStatus returns git status for a workspace.
// Returns the same structured format as git/status with branch info and categorized files.
func (s *SessionManagerService) GitStatus(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Use enhanced status to get structured format with branch info
	status, err := s.manager.GetGitEnhancedStatus(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return status, nil
}

// GitDiff returns git diff for a workspace.
// Returns format consistent with git/diff: {path, diff, is_staged, is_new}
func (s *SessionManagerService) GitDiff(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
		Staged      bool   `json:"staged"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Get diff with metadata (same as git/diff)
	diff, isStaged, isNew, err := s.manager.GetGitDiffWithMeta(p.WorkspaceID, p.Path)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"path":      p.Path,
		"diff":      diff,
		"is_staged": isStaged,
		"is_new":    isNew,
	}, nil
}

// GitStage stages files for a workspace.
func (s *SessionManagerService) GitStage(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string   `json:"workspace_id"`
		Paths       []string `json:"paths"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if len(p.Paths) == 0 {
		return nil, message.NewError(message.InvalidParams, "paths is required")
	}

	if err := s.manager.GitStage(p.WorkspaceID, p.Paths); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success": true,
		"staged":  p.Paths,
	}, nil
}

// GitUnstage unstages files for a workspace.
func (s *SessionManagerService) GitUnstage(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string   `json:"workspace_id"`
		Paths       []string `json:"paths"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if len(p.Paths) == 0 {
		return nil, message.NewError(message.InvalidParams, "paths is required")
	}

	if err := s.manager.GitUnstage(p.WorkspaceID, p.Paths); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success":  true,
		"unstaged": p.Paths,
	}, nil
}

// GitDiscard discards changes for a workspace.
func (s *SessionManagerService) GitDiscard(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string   `json:"workspace_id"`
		Paths       []string `json:"paths"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if len(p.Paths) == 0 {
		return nil, message.NewError(message.InvalidParams, "paths is required")
	}

	if err := s.manager.GitDiscard(p.WorkspaceID, p.Paths); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success":   true,
		"discarded": p.Paths,
	}, nil
}

// GitCommit commits staged changes for a workspace.
func (s *SessionManagerService) GitCommit(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Message     string `json:"message"`
		Push        bool   `json:"push"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Message == "" {
		return nil, message.NewError(message.InvalidParams, "message is required")
	}

	result, err := s.manager.GitCommit(p.WorkspaceID, p.Message, p.Push)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitPush pushes commits for a workspace.
func (s *SessionManagerService) GitPush(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Force       bool   `json:"force"`
		SetUpstream bool   `json:"set_upstream"`
		Remote      string `json:"remote"`
		Branch      string `json:"branch"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitPush(p.WorkspaceID, p.Force, p.SetUpstream, p.Remote, p.Branch)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitPull pulls changes for a workspace.
func (s *SessionManagerService) GitPull(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Rebase      bool   `json:"rebase"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitPull(p.WorkspaceID, p.Rebase)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitBranches lists branches for a workspace.
func (s *SessionManagerService) GitBranches(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitBranches(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitCheckout checks out a branch for a workspace.
func (s *SessionManagerService) GitCheckout(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Branch      string `json:"branch"`
		Create      bool   `json:"create"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Branch == "" {
		return nil, message.NewError(message.InvalidParams, "branch is required")
	}

	result, err := s.manager.GitCheckout(p.WorkspaceID, p.Branch, p.Create)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// WatchSession starts watching a session for real-time message updates.
// Returns watch info including the workspace_id, session_id, and watching status.
func (s *SessionManagerService) WatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	info, err := s.manager.WatchWorkspaceSession(p.WorkspaceID, p.SessionID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"status":       "watching",
		"watching":     info.Watching,
		"workspace_id": info.WorkspaceID,
		"session_id":   info.SessionID,
	}, nil
}

// UnwatchSession stops watching the current session.
// Returns the previous watch info.
func (s *SessionManagerService) UnwatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	info := s.manager.UnwatchWorkspaceSession()

	return map[string]interface{}{
		"status":       "unwatched",
		"watching":     info.Watching,
		"workspace_id": info.WorkspaceID,
		"session_id":   info.SessionID,
	}, nil
}
