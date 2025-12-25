// Package methods provides JSON-RPC method implementations for the session-based architecture.
package methods

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/session"
)

// SessionFocusProvider handles session focus tracking for multi-device awareness.
type SessionFocusProvider interface {
	SetSessionFocus(clientID, workspaceID, sessionID string) (interface{}, error)
}

// SessionManagerService handles session-related JSON-RPC methods for the new architecture.
// This replaces the old workspace start/stop with session-based management.
type SessionManagerService struct {
	manager       *session.Manager
	focusProvider SessionFocusProvider
}

// NewSessionManagerService creates a new session manager service.
func NewSessionManagerService(manager *session.Manager) *SessionManagerService {
	return &SessionManagerService{
		manager: manager,
	}
}

// SetFocusProvider sets the session focus provider for multi-device awareness.
// This allows workspace/session/watch to also update focus tracking.
func (s *SessionManagerService) SetFocusProvider(provider SessionFocusProvider) {
	s.focusProvider = provider
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
		Summary:     "Get historical Claude sessions for a workspace (legacy, use workspace/session/history)",
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

	registry.RegisterWithMeta("workspace/session/history", s.History, handler.MethodMeta{
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
		Description: "Returns paginated messages from a Claude session file for the specified workspace. Use workspace/session/history to get available session IDs.",
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

	registry.RegisterWithMeta("workspace/session/activate", s.ActivateSession, handler.MethodMeta{
		Summary:     "Set the active session for a workspace",
		Description: "Sets the active session for a workspace. This allows iOS clients to switch which session they are viewing/interacting with. The active session ID will be included in workspace/list responses.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "activate_result",
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

	// File methods
	registry.RegisterWithMeta("workspace/files/list", s.FilesList, handler.MethodMeta{
		Summary:     "List files in a workspace directory",
		Description: "Returns a paginated list of files and directories in a workspace. Matches the format of /api/repository/files/list.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "directory", Required: false, Schema: map[string]interface{}{"type": "string", "default": ""}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 100}},
			{Name: "offset", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "files_list",
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
// Also updates session focus tracking so the client appears in the session's viewers list.
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

	// Also update focus tracking so this client appears in the session's viewers list
	if s.focusProvider != nil {
		clientID, _ := ctx.Value(handler.ClientIDKey).(string)
		if clientID != "" {
			s.focusProvider.SetSessionFocus(clientID, p.WorkspaceID, p.SessionID)
		}
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

// FileInfo represents a file in the listing (matches HTTP API format).
type FileInfo struct {
	Path        string    `json:"path"`
	Name        string    `json:"name"`
	Directory   string    `json:"directory"`
	Extension   string    `json:"extension,omitempty"`
	SizeBytes   int64     `json:"size_bytes"`
	ModifiedAt  time.Time `json:"modified_at"`
	IndexedAt   time.Time `json:"indexed_at,omitempty"`
	IsBinary    bool      `json:"is_binary"`
	IsSymlink   bool      `json:"is_symlink"`
	IsSensitive bool      `json:"is_sensitive"`
	GitTracked  bool      `json:"git_tracked"`
	GitIgnored  bool      `json:"git_ignored"`
}

// DirectoryInfo represents a directory in the listing (matches HTTP API format).
type DirectoryInfo struct {
	Path           string    `json:"path"`
	Name           string    `json:"name"`
	FileCount      int       `json:"file_count"`
	TotalSizeBytes int64     `json:"total_size_bytes"`
	LastModified   time.Time `json:"last_modified,omitempty"`
}

// PaginationInfo contains pagination metadata.
type PaginationInfo struct {
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// FileListResult matches the HTTP API /api/repository/files/list response format.
type FileListResult struct {
	Directory        string          `json:"directory"`
	Files            []FileInfo      `json:"files"`
	Directories      []DirectoryInfo `json:"directories"`
	TotalFiles       int             `json:"total_files"`
	TotalDirectories int             `json:"total_directories"`
	Pagination       PaginationInfo  `json:"pagination"`
}

// FilesList lists files and directories in a workspace.
// Response format matches /api/repository/files/list HTTP endpoint.
func (s *SessionManagerService) FilesList(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Directory   string `json:"directory"`
		Limit       int    `json:"limit"`
		Offset      int    `json:"offset"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Default limit
	if p.Limit <= 0 {
		p.Limit = 100
	}
	if p.Limit > 500 {
		p.Limit = 500
	}

	// Get workspace to find the path
	ws, err := s.manager.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Build full path
	basePath := ws.Definition.Path
	targetPath := basePath
	if p.Directory != "" {
		targetPath = filepath.Join(basePath, p.Directory)
	}

	// Validate path is within workspace
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, message.NewError(message.InternalError, "invalid path: "+err.Error())
	}
	absBase, _ := filepath.Abs(basePath)
	if !strings.HasPrefix(absTarget, absBase) {
		return nil, message.NewError(message.InvalidParams, "path is outside workspace")
	}

	// Read directory
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, message.NewError(message.InvalidParams, "directory not found: "+p.Directory)
		}
		return nil, message.NewError(message.InternalError, "failed to read directory: "+err.Error())
	}

	// Separate files and directories
	var files []FileInfo
	var dirs []DirectoryInfo

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files starting with . (except .gitignore, etc.)
		if strings.HasPrefix(name, ".") && name != ".gitignore" && name != ".env.example" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		relPath := name
		if p.Directory != "" {
			relPath = filepath.Join(p.Directory, name)
		}

		if entry.IsDir() {
			// Count files in directory
			fileCount, totalSize := countDirContents(filepath.Join(targetPath, name))
			dirs = append(dirs, DirectoryInfo{
				Path:           relPath,
				Name:           name,
				FileCount:      fileCount,
				TotalSizeBytes: totalSize,
				LastModified:   info.ModTime(),
			})
		} else {
			ext := ""
			if idx := strings.LastIndex(name, "."); idx > 0 {
				ext = strings.ToLower(name[idx+1:])
			}

			files = append(files, FileInfo{
				Path:        relPath,
				Name:        name,
				Directory:   p.Directory,
				Extension:   ext,
				SizeBytes:   info.Size(),
				ModifiedAt:  info.ModTime(),
				IsBinary:    isBinaryExtension(ext),
				IsSymlink:   info.Mode()&fs.ModeSymlink != 0,
				IsSensitive: isSensitiveFile(name),
				GitTracked:  false, // Would need git integration to determine
				GitIgnored:  false, // Would need git integration to determine
			})
		}
	}

	// Sort directories and files by name
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	// Apply pagination
	totalDirs := len(dirs)
	totalFiles := len(files)

	// Paginate directories first, then files
	startIdx := p.Offset
	endIdx := p.Offset + p.Limit

	var paginatedDirs []DirectoryInfo
	var paginatedFiles []FileInfo

	if startIdx < totalDirs {
		dirEnd := endIdx
		if dirEnd > totalDirs {
			dirEnd = totalDirs
		}
		paginatedDirs = dirs[startIdx:dirEnd]
		remaining := p.Limit - len(paginatedDirs)
		if remaining > 0 && len(files) > 0 {
			fileEnd := remaining
			if fileEnd > len(files) {
				fileEnd = len(files)
			}
			paginatedFiles = files[0:fileEnd]
		}
	} else {
		fileStart := startIdx - totalDirs
		if fileStart < totalFiles {
			fileEnd := fileStart + p.Limit
			if fileEnd > totalFiles {
				fileEnd = totalFiles
			}
			paginatedFiles = files[fileStart:fileEnd]
		}
	}

	hasMore := (p.Offset + len(paginatedDirs) + len(paginatedFiles)) < (totalDirs + totalFiles)

	return FileListResult{
		Directory:        p.Directory,
		Files:            paginatedFiles,
		Directories:      paginatedDirs,
		TotalFiles:       totalFiles,
		TotalDirectories: totalDirs,
		Pagination: PaginationInfo{
			Limit:   p.Limit,
			Offset:  p.Offset,
			HasMore: hasMore,
		},
	}, nil
}

// countDirContents counts files and total size in a directory (non-recursive for performance).
func countDirContents(path string) (int, int64) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0
	}

	var count int
	var size int64
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
			if info, err := entry.Info(); err == nil {
				size += info.Size()
			}
		}
	}
	return count, size
}

// isBinaryExtension checks if a file extension indicates a binary file.
func isBinaryExtension(ext string) bool {
	binaryExts := map[string]bool{
		"exe": true, "dll": true, "so": true, "dylib": true, "a": true, "o": true,
		"zip": true, "tar": true, "gz": true, "bz2": true, "xz": true, "7z": true, "rar": true,
		"png": true, "jpg": true, "jpeg": true, "gif": true, "bmp": true, "ico": true, "webp": true,
		"mp3": true, "mp4": true, "avi": true, "mov": true, "mkv": true, "wav": true,
		"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true,
		"wasm": true, "pyc": true, "class": true, "jar": true,
		"ttf": true, "otf": true, "woff": true, "woff2": true,
		"db": true, "sqlite": true, "sqlite3": true,
	}
	return binaryExts[ext]
}

// isSensitiveFile checks if a filename indicates sensitive content.
func isSensitiveFile(name string) bool {
	sensitivePatterns := []string{
		".env", "credentials", "secrets", ".key", ".pem", "id_rsa", "id_dsa", ".netrc", ".npmrc",
	}
	lower := strings.ToLower(name)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// ActivateSession sets the active session for a workspace.
// This allows iOS clients to switch which session they are viewing/interacting with.
func (s *SessionManagerService) ActivateSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
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

	if err := s.manager.ActivateSession(p.WorkspaceID, p.SessionID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success":      true,
		"workspace_id": p.WorkspaceID,
		"session_id":   p.SessionID,
		"message":      "Session activated",
	}, nil
}
