// Package methods provides JSON-RPC method implementations for the session-based architecture.
package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/events"
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
		Summary:     "Start or attach to a Claude session for a workspace",
		Description: "If session_id is provided, validates it exists in .claude/projects and returns it for LIVE session attachment. If session_id doesn't exist, returns empty to let user select from history. If no session_id provided, starts a new managed session.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Optional session ID to attach to. If provided, validates against .claude/projects source of truth."}},
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
		Description: "Sends a prompt to the Claude CLI. If session_id is provided, sends to that session. If only workspace_id is provided with mode='new', auto-creates a new session and sends the prompt.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Session ID to send to. If empty, workspace_id must be provided to auto-create a session."}},
			{Name: "workspace_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Workspace ID. Required when session_id is empty to auto-create a new session."}},
			{Name: "prompt", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "mode", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"new", "continue"}, "default": "new", "description": "Session mode. 'new' starts fresh conversation (default), 'continue' resumes existing."}},
			{Name: "permission_mode", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"default", "acceptEdits", "bypassPermissions", "plan", "interactive"}, "default": "default", "description": "Permission handling mode. Use 'acceptEdits' to auto-accept file edits, 'bypassPermissions' to skip all permission checks, 'interactive' to use PTY mode for true terminal-like permission prompts."}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/input", s.Input, handler.MethodMeta{
		Summary:     "Send input to an interactive session",
		Description: "Sends keyboard input to a session running in interactive (PTY) mode. Use this to respond to permission prompts. Either 'input' (raw text) or 'key' (special key name) must be provided.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "input", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Raw text input to send (e.g., '1' for Yes, '2' for Yes all, 'n' for No). A carriage return is auto-appended for text input."}},
			{Name: "key", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"enter", "escape", "up", "down", "left", "right", "tab", "backspace", "delete", "home", "end", "pageup", "pagedown", "space"}, "description": "Special key name to send. Use 'enter' to confirm prompts, arrow keys for navigation."}},
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
	registry.RegisterWithMeta("git/status", s.GitStatus, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/diff", s.GitDiff, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/stage", s.GitStage, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/unstage", s.GitUnstage, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/discard", s.GitDiscard, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/commit", s.GitCommit, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/push", s.GitPush, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/pull", s.GitPull, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/branches", s.GitBranches, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/checkout", s.GitCheckout, handler.MethodMeta{
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

	registry.RegisterWithMeta("git/branch/delete", s.GitDeleteBranch, handler.MethodMeta{
		Summary:     "Delete a branch for a workspace",
		Description: "Deletes the specified branch in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "branch", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "force", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/fetch", s.GitFetch, handler.MethodMeta{
		Summary:     "Fetch from remote for a workspace",
		Description: "Fetches updates from the remote repository.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "remote", Required: false, Schema: map[string]interface{}{"type": "string", "default": "origin"}},
			{Name: "prune", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/log", s.GitLog, handler.MethodMeta{
		Summary:     "Get commit log for a workspace",
		Description: "Returns the commit history with optional graph layout for visualization.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "skip", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
			{Name: "branch", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "path", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "graph", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	// Stash methods
	registry.RegisterWithMeta("git/stash", s.GitStash, handler.MethodMeta{
		Summary:     "Create a stash for a workspace",
		Description: "Stashes changes in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "message", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "include_untracked", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/stash/list", s.GitStashList, handler.MethodMeta{
		Summary:     "List stashes for a workspace",
		Description: "Returns the list of stashes in the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/stash/apply", s.GitStashApply, handler.MethodMeta{
		Summary:     "Apply a stash for a workspace",
		Description: "Applies a stash without removing it from the stash list.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "index", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/stash/pop", s.GitStashPop, handler.MethodMeta{
		Summary:     "Pop a stash for a workspace",
		Description: "Applies and removes a stash from the stash list.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "index", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/stash/drop", s.GitStashDrop, handler.MethodMeta{
		Summary:     "Drop a stash for a workspace",
		Description: "Removes a stash from the stash list without applying it.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "index", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	// Merge methods
	registry.RegisterWithMeta("git/merge", s.GitMerge, handler.MethodMeta{
		Summary:     "Merge a branch for a workspace",
		Description: "Merges the specified branch into the current branch.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "branch", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "no_ff", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
			{Name: "message", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/merge/abort", s.GitMergeAbort, handler.MethodMeta{
		Summary:     "Abort a merge for a workspace",
		Description: "Aborts an in-progress merge operation.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	// Repository setup methods
	registry.RegisterWithMeta("git/init", s.GitInit, handler.MethodMeta{
		Summary:     "Initialize a git repository for a workspace",
		Description: "Initializes a new git repository in the workspace directory.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "initial_branch", Required: false, Schema: map[string]interface{}{"type": "string", "default": "main"}},
			{Name: "initial_commit", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
			{Name: "commit_message", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/remote/add", s.GitRemoteAdd, handler.MethodMeta{
		Summary:     "Add a remote to a workspace",
		Description: "Adds a remote repository to the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "name", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "url", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "fetch", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": true}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/remote/list", s.GitRemoteList, handler.MethodMeta{
		Summary:     "List remotes for a workspace",
		Description: "Returns the list of configured remotes.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/remote/remove", s.GitRemoteRemove, handler.MethodMeta{
		Summary:     "Remove a remote from a workspace",
		Description: "Removes a remote repository from the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "name", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/upstream/set", s.GitSetUpstream, handler.MethodMeta{
		Summary:     "Set upstream for a branch",
		Description: "Sets the upstream tracking branch for the specified branch.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "branch", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "upstream", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("git/get_status", s.GitGetStatus, handler.MethodMeta{
		Summary:     "Get comprehensive git status for a workspace",
		Description: "Returns comprehensive git status including state (noGit, gitInit, noRemote, noPush, synced, diverged, conflict), branch info, and file changes.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
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

	registry.RegisterWithMeta("workspace/file/get", s.FileGet, handler.MethodMeta{
		Summary:     "Get file content from a workspace",
		Description: "Returns the content of a file from a workspace. Use this for multi-workspace mode instead of file/get.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "path", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "max_size_kb", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 500}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "file_content",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
}

// Start starts or attaches to a Claude session for a workspace.
// If session_id is provided, validates it exists in .claude/projects (source of truth).
// If session_id exists, returns it for LIVE session attachment.
// If session_id doesn't exist, returns empty session_id to let user select from history.
// If no session_id provided, starts a new managed session.
func (s *SessionManagerService) Start(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"` // Optional: attach to existing LIVE session
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// If session_id is provided, validate against .claude/projects
	if p.SessionID != "" {
		exists, err := s.manager.SessionFileExists(p.WorkspaceID, p.SessionID)
		if err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}

		if exists {
			// Session exists in .claude/projects - activate it for LIVE attachment
			_ = s.manager.ActivateSession(p.WorkspaceID, p.SessionID)
			return map[string]interface{}{
				"session_id":   p.SessionID,
				"workspace_id": p.WorkspaceID,
				"source":       "live",
				"status":       "attached",
				"message":      "Session found in .claude/projects - ready for LIVE interaction",
			}, nil
		} else {
			// Session doesn't exist - return empty to let user select from history
			return map[string]interface{}{
				"session_id":   "",
				"workspace_id": p.WorkspaceID,
				"source":       "",
				"status":       "not_found",
				"message":      "Session not found in .claude/projects. Use workspace/session/history to select a valid session.",
			}, nil
		}
	}

	// No session_id provided - get the latest session from .claude/projects
	latestSessionID, err := s.manager.GetLatestSessionID(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	if latestSessionID != "" {
		// Activate the latest session for LIVE attachment
		_ = s.manager.ActivateSession(p.WorkspaceID, latestSessionID)
		return map[string]interface{}{
			"session_id":   latestSessionID,
			"workspace_id": p.WorkspaceID,
			"source":       "live",
			"status":       "attached",
			"message":      "Latest session found in .claude/projects - ready for LIVE interaction",
		}, nil
	}

	// Check if there's already an active managed session for this workspace
	// This handles the case where iOS app was closed and reopened while Claude
	// was still waiting for trust folder approval
	if activeSessionID := s.manager.GetActiveSession(p.WorkspaceID); activeSessionID != "" {
		if existingSession, err := s.manager.GetSession(activeSessionID); err == nil {
			status := existingSession.GetStatus()
			if status == session.StatusRunning || status == session.StatusStarting {
				// Check if there's a pending permission (e.g., trust folder prompt)
				result := map[string]interface{}{
					"session_id":         activeSessionID,
					"workspace_id":       p.WorkspaceID,
					"source":             "managed",
					"status":             "existing",
					"message":            "Returning existing active session (Claude still running)",
					"has_pending_permission": false,
				}

				// If there's a pending permission, re-emit the pty_permission event
				// so the reconnecting client can show the permission dialog
				if cm := existingSession.ClaudeManager(); cm != nil {
					if pendingPerm := cm.GetPendingPTYPermission(); pendingPerm != nil {
						result["has_pending_permission"] = true
						result["pending_permission_type"] = string(pendingPerm.Type)
						result["pending_permission_target"] = pendingPerm.Target

						// Re-emit the pty_permission event for the reconnecting client
						options := make([]events.PTYPromptOption, len(pendingPerm.Options))
						for i, opt := range pendingPerm.Options {
							options[i] = events.PTYPromptOption{
								Key:         opt.Key,
								Label:       opt.Label,
								Description: opt.Description,
								Selected:    opt.Selected,
							}
						}
						s.manager.PublishEvent(events.NewPTYPermissionEventWithSession(
							string(pendingPerm.Type),
							pendingPerm.Target,
							pendingPerm.Description,
							pendingPerm.Preview,
							activeSessionID,
							options,
						))
					}
				}

				return result, nil
			}
		}
	}

	// No sessions found in .claude/projects - start a new managed session
	// This starts Claude in interactive mode (PTY) waiting for user input
	newSession, err := s.manager.StartSession(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to start session: "+err.Error())
	}

	// Get workspace for path
	ws, err := s.manager.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to get workspace: "+err.Error())
	}

	// Always watch for new session file creation for PTY sessions
	// Claude creates a NEW session file with a NEW UUID every time it starts
	// The temporary ID generated by cdev is internal only - we need to detect
	// the real session ID from Claude and emit session_id_resolved event
	// so iOS can switch to watching the real session file
	go s.manager.WatchForNewSessionFile(ctx, p.WorkspaceID, newSession.ID, ws.Definition.Path)

	// Start Claude in interactive PTY mode (no initial prompt)
	claudeManager := newSession.ClaudeManager()
	if claudeManager != nil {
		// Set up callback to detect when Claude exits without creating a session
		// This handles the case where user declines trust folder (clicks "No")
		temporaryID := newSession.ID
		workspaceID := p.WorkspaceID
		claudeManager.SetOnPTYComplete(func(sid string) {
			// Check if there's still an active watcher - if so, Claude exited
			// without creating a session file (user likely declined trust)
			if s.manager.HasActiveSessionFileWatcher(workspaceID) {
				s.manager.FailSessionIDResolution(
					workspaceID,
					temporaryID,
					"trust_declined",
					"Claude exited without creating a session. User may have declined trust folder.",
				)
			}
		})

		go func() {
			// Start Claude with empty prompt for interactive mode
			if err := claudeManager.StartWithPTY(ctx, "", "new", newSession.ID); err != nil {
				// Log error but don't fail - session is still created
				// The user can send a prompt via session/send
				fmt.Printf("Warning: failed to start Claude in interactive mode: %v\n", err)
			}
		}()
	}

	return map[string]interface{}{
		"session_id":   newSession.ID,
		"workspace_id": p.WorkspaceID,
		"source":       "managed",
		"status":       "started",
		"message":      "New Claude session started in interactive mode",
	}, nil
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
// If session_id is empty but workspace_id is provided, auto-creates a new session.
func (s *SessionManagerService) Send(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID      string `json:"session_id"`
		WorkspaceID    string `json:"workspace_id"`
		Prompt         string `json:"prompt"`
		Mode           string `json:"mode"`            // "new" or "continue"
		PermissionMode string `json:"permission_mode"` // "default", "acceptEdits", "bypassPermissions", "plan", "interactive"
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.Prompt == "" {
		return nil, message.NewError(message.InvalidParams, "prompt is required")
	}

	// Validate permission_mode if provided
	if p.PermissionMode != "" {
		validModes := map[string]bool{"default": true, "acceptEdits": true, "bypassPermissions": true, "plan": true, "interactive": true}
		if !validModes[p.PermissionMode] {
			return nil, message.NewError(message.InvalidParams, "permission_mode must be one of: default, acceptEdits, bypassPermissions, plan, interactive")
		}
	}

	// Default mode to "new" if not specified
	if p.Mode == "" {
		p.Mode = "new"
	}

	// Auto-create session if session_id is empty but workspace_id is provided
	if p.SessionID == "" {
		if p.WorkspaceID == "" {
			return nil, message.NewError(message.InvalidParams, "either session_id or workspace_id is required")
		}

		// Auto-create a new session for this workspace
		newSession, err := s.manager.StartSession(p.WorkspaceID)
		if err != nil {
			return nil, message.NewError(message.InternalError, "failed to auto-create session: "+err.Error())
		}

		p.SessionID = newSession.ID

		// Get workspace for path (needed for session file watcher)
		ws, err := s.manager.GetWorkspace(p.WorkspaceID)
		if err != nil {
			return nil, message.NewError(message.InternalError, "failed to get workspace: "+err.Error())
		}

		// Always watch for new session file creation for PTY sessions
		// Claude creates a NEW session file with a NEW UUID every time
		go s.manager.WatchForNewSessionFile(ctx, p.WorkspaceID, newSession.ID, ws.Definition.Path)

		// Start Claude with the prompt immediately (don't wait for user input)
		claudeManager := newSession.ClaudeManager()
		if claudeManager != nil {
			// Set up callback to detect when Claude exits without creating a session
			temporaryID := newSession.ID
			workspaceID := p.WorkspaceID
			claudeManager.SetOnPTYComplete(func(sid string) {
				if s.manager.HasActiveSessionFileWatcher(workspaceID) {
					s.manager.FailSessionIDResolution(
						workspaceID,
						temporaryID,
						"trust_declined",
						"Claude exited without creating a session. User may have declined trust folder.",
					)
				}
			})

			// Start Claude with PTY and the prompt
			go func() {
				if err := claudeManager.StartWithPTY(ctx, p.Prompt, "new", newSession.ID); err != nil {
					// Log error but session was created
					fmt.Printf("Warning: failed to start Claude with prompt: %v\n", err)
				}
			}()
		}

		return map[string]interface{}{
			"status":       "sent",
			"session_id":   p.SessionID,
			"workspace_id": p.WorkspaceID,
			"auto_created": true,
			"message":      "New session created and prompt sent",
		}, nil
	}

	// Session ID provided - send to existing session
	if err := s.manager.SendPrompt(p.SessionID, p.Prompt, p.Mode, p.PermissionMode); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"status":     "sent",
		"session_id": p.SessionID,
	}, nil
}

// Input sends keyboard input to an interactive (PTY) session.
// Supports special key names: "enter", "escape", "up", "down", "left", "right", "tab", "backspace"
// For special keys, uses SendKey which handles platform-specific key codes for LIVE sessions.
// For text input, uses SendInput which sends raw text.
func (s *SessionManagerService) Input(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
		Input     string `json:"input"`
		Key       string `json:"key"` // Special key name (alternative to input)
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	// If a special key is provided, use SendKey (handles key codes for LIVE sessions)
	if p.Key != "" {
		// Validate key name
		validKeys := map[string]bool{
			"enter": true, "return": true, "escape": true, "esc": true,
			"up": true, "down": true, "left": true, "right": true,
			"tab": true, "backspace": true, "delete": true,
			"home": true, "end": true, "pageup": true, "pagedown": true, "space": true,
		}
		if !validKeys[p.Key] {
			return nil, message.NewError(message.InvalidParams, "unknown key: "+p.Key+". Valid keys: enter, escape, up, down, left, right, tab, backspace, delete, home, end, pageup, pagedown, space")
		}

		if err := s.manager.SendKey(p.SessionID, p.Key); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}

		// Emit pty_permission_resolved event so other devices can dismiss their permission popups
		clientID, _ := ctx.Value(handler.ClientIDKey).(string)
		s.manager.EmitPermissionResolved(p.SessionID, clientID, p.Key)

		return map[string]interface{}{
			"status": "sent",
			"key":    p.Key,
		}, nil
	}

	// If text input is provided, use SendInput
	if p.Input != "" {
		if err := s.manager.SendInput(p.SessionID, p.Input); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}

		// Emit pty_permission_resolved event so other devices can dismiss their permission popups
		clientID, _ := ctx.Value(handler.ClientIDKey).(string)
		s.manager.EmitPermissionResolved(p.SessionID, clientID, p.Input)

		return map[string]interface{}{
			"status": "sent",
		}, nil
	}

	return nil, message.NewError(message.InvalidParams, "either 'input' or 'key' is required")
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
	_ = json.Unmarshal(params, &p)

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

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("commit", result.Error)
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

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("push", result.Error)
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

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("pull", result.Error)
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

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("checkout", result.Error)
	}

	return result, nil
}

// GitDeleteBranch deletes a branch for a workspace.
func (s *SessionManagerService) GitDeleteBranch(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Branch      string `json:"branch"`
		Force       bool   `json:"force"`
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

	result, err := s.manager.GitDeleteBranch(p.WorkspaceID, p.Branch, p.Force)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("delete_branch", result.Error)
	}

	return result, nil
}

// GitFetch fetches from a remote for a workspace.
func (s *SessionManagerService) GitFetch(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Remote      string `json:"remote"`
		Prune       bool   `json:"prune"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Remote == "" {
		p.Remote = "origin"
	}

	result, err := s.manager.GitFetch(p.WorkspaceID, p.Remote, p.Prune)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("fetch", result.Error)
	}

	return result, nil
}

// GitLog returns the commit log for a workspace.
func (s *SessionManagerService) GitLog(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Limit       int    `json:"limit"`
		Skip        int    `json:"skip"`
		Branch      string `json:"branch"`
		Path        string `json:"path"`
		Graph       bool   `json:"graph"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}

	result, err := s.manager.GitLog(p.WorkspaceID, p.Limit, p.Skip, p.Branch, p.Path, p.Graph)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitStash creates a stash for a workspace.
func (s *SessionManagerService) GitStash(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID      string `json:"workspace_id"`
		Message          string `json:"message"`
		IncludeUntracked bool   `json:"include_untracked"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitStash(p.WorkspaceID, p.Message, p.IncludeUntracked)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("stash", result.Error)
	}

	return result, nil
}

// GitStashList lists stashes for a workspace.
func (s *SessionManagerService) GitStashList(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitStashList(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitStashApply applies a stash for a workspace.
func (s *SessionManagerService) GitStashApply(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Index       int    `json:"index"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitStashApply(p.WorkspaceID, p.Index)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("stash_apply", result.Error)
	}

	return result, nil
}

// GitStashPop pops a stash for a workspace.
func (s *SessionManagerService) GitStashPop(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Index       int    `json:"index"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitStashPop(p.WorkspaceID, p.Index)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("stash_pop", result.Error)
	}

	return result, nil
}

// GitStashDrop drops a stash for a workspace.
func (s *SessionManagerService) GitStashDrop(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Index       int    `json:"index"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitStashDrop(p.WorkspaceID, p.Index)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("stash_drop", result.Error)
	}

	return result, nil
}

// GitMerge merges a branch for a workspace.
func (s *SessionManagerService) GitMerge(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID   string `json:"workspace_id"`
		Branch        string `json:"branch"`
		NoFastForward bool   `json:"no_ff"`
		Message       string `json:"message"`
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

	result, err := s.manager.GitMerge(p.WorkspaceID, p.Branch, p.NoFastForward, p.Message)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed (but not for conflicts - those return success with has_conflicts)
	if !result.Success && !result.HasConflicts {
		return nil, message.ErrGitOperationFailed("merge", result.Error)
	}

	return result, nil
}

// GitMergeAbort aborts a merge for a workspace.
func (s *SessionManagerService) GitMergeAbort(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitMergeAbort(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("merge_abort", result.Error)
	}

	return result, nil
}

// GitInit initializes a git repository for a workspace.
func (s *SessionManagerService) GitInit(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID   string `json:"workspace_id"`
		InitialBranch string `json:"initial_branch"`
		InitialCommit bool   `json:"initial_commit"`
		CommitMessage string `json:"commit_message"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.InitialBranch == "" {
		p.InitialBranch = "main"
	}

	result, err := s.manager.GitInit(p.WorkspaceID, p.InitialBranch, p.InitialCommit, p.CommitMessage)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("init", result.Error)
	}

	return result, nil
}

// GitRemoteAdd adds a remote to a workspace.
func (s *SessionManagerService) GitRemoteAdd(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Name        string `json:"name"`
		URL         string `json:"url"`
		Fetch       bool   `json:"fetch"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Name == "" {
		return nil, message.NewError(message.InvalidParams, "name is required")
	}
	if p.URL == "" {
		return nil, message.NewError(message.InvalidParams, "url is required")
	}

	result, err := s.manager.GitRemoteAdd(p.WorkspaceID, p.Name, p.URL, p.Fetch)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("remote_add", result.Error)
	}

	return result, nil
}

// GitRemoteList lists remotes for a workspace.
func (s *SessionManagerService) GitRemoteList(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitRemoteList(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return result, nil
}

// GitRemoteRemove removes a remote from a workspace.
func (s *SessionManagerService) GitRemoteRemove(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Name        string `json:"name"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Name == "" {
		return nil, message.NewError(message.InvalidParams, "name is required")
	}

	result, err := s.manager.GitRemoteRemove(p.WorkspaceID, p.Name)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("remote_remove", result.Error)
	}

	return result, nil
}

// GitSetUpstream sets the upstream for a branch.
func (s *SessionManagerService) GitSetUpstream(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Branch      string `json:"branch"`
		Upstream    string `json:"upstream"`
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
	if p.Upstream == "" {
		return nil, message.NewError(message.InvalidParams, "upstream is required")
	}

	result, err := s.manager.GitSetUpstream(p.WorkspaceID, p.Branch, p.Upstream)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Return JSON-RPC error if git operation failed
	if !result.Success {
		return nil, message.ErrGitOperationFailed("set_upstream", result.Error)
	}

	return result, nil
}

// GitGetStatus returns the comprehensive git status for a workspace.
func (s *SessionManagerService) GitGetStatus(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	result, err := s.manager.GitGetStatus(p.WorkspaceID)
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

	// Get client ID for tracking watchers
	clientID, _ := ctx.Value(handler.ClientIDKey).(string)

	info, err := s.manager.WatchWorkspaceSession(clientID, p.WorkspaceID, p.SessionID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Also update focus tracking so this client appears in the session's viewers list
	if s.focusProvider != nil && clientID != "" {
		_, _ = s.focusProvider.SetSessionFocus(clientID, p.WorkspaceID, p.SessionID)
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
	// Get client ID for tracking watchers
	clientID, _ := ctx.Value(handler.ClientIDKey).(string)

	info := s.manager.UnwatchWorkspaceSession(clientID)

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
	Path             string    `json:"path"`
	Name             string    `json:"name"`
	FolderCount      int       `json:"folder_count"`       // Direct subdirectories count
	FileCount        int       `json:"file_count"`         // Recursive file count
	TotalSizeBytes   int64     `json:"total_size_bytes"`
	TotalSizeDisplay string    `json:"total_size_display"` // Human readable: "35 KB"
	LastModified     time.Time `json:"last_modified,omitempty"`
	ModifiedDisplay  string    `json:"modified_display,omitempty"` // Relative: "2 hours ago"
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
			stats := countDirContents(filepath.Join(targetPath, name))
			modTime := info.ModTime()
			dirs = append(dirs, DirectoryInfo{
				Path:             relPath,
				Name:             name,
				FolderCount:      stats.FolderCount,
				FileCount:        stats.FileCount,
				TotalSizeBytes:   stats.TotalSize,
				TotalSizeDisplay: formatBytes(stats.TotalSize),
				LastModified:     modTime,
				ModifiedDisplay:  formatRelativeTime(modTime),
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

// FileGet returns the content of a file from a workspace.
func (s *SessionManagerService) FileGet(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Path        string `json:"path"`
		MaxSizeKB   int    `json:"max_size_kb"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}
	if p.Path == "" {
		return nil, message.NewError(message.InvalidParams, "path is required")
	}

	// Default max size
	if p.MaxSizeKB <= 0 {
		p.MaxSizeKB = 500 // 500KB default
	}
	if p.MaxSizeKB > 10000 {
		p.MaxSizeKB = 10000 // 10MB max
	}

	// Get workspace to find the path
	ws, err := s.manager.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Build full path
	basePath := ws.Definition.Path
	targetPath := filepath.Join(basePath, p.Path)

	// Validate path is within workspace (security check)
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return nil, message.NewError(message.InternalError, "invalid path: "+err.Error())
	}
	absBase, _ := filepath.Abs(basePath)
	if !strings.HasPrefix(absTarget, absBase) {
		return nil, message.NewError(message.InvalidParams, "path is outside workspace")
	}

	// Check if file exists
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, message.NewError(message.InvalidParams, "file not found: "+p.Path)
		}
		return nil, message.NewError(message.InternalError, "failed to stat file: "+err.Error())
	}

	if info.IsDir() {
		return nil, message.NewError(message.InvalidParams, "path is a directory, not a file")
	}

	// Check file size
	maxSizeBytes := int64(p.MaxSizeKB * 1024)
	truncated := false
	readSize := info.Size()
	if readSize > maxSizeBytes {
		readSize = maxSizeBytes
		truncated = true
	}

	// Read file content
	file, err := os.Open(targetPath)
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to open file: "+err.Error())
	}
	defer func() { _ = file.Close() }()

	content := make([]byte, readSize)
	n, err := file.Read(content)
	if err != nil && err.Error() != "EOF" {
		return nil, message.NewError(message.InternalError, "failed to read file: "+err.Error())
	}
	content = content[:n]

	return map[string]interface{}{
		"path":      p.Path,
		"content":   string(content),
		"encoding":  "utf-8",
		"truncated": truncated,
		"size":      len(content),
	}, nil
}

// DirStats holds directory statistics.
type DirStats struct {
	FolderCount int   // Direct subdirectories
	FileCount   int   // Recursive file count
	TotalSize   int64 // Total size in bytes
}

// countDirContents counts folders, files and total size in a directory using filepath.WalkDir.
// Uses iterative traversal (not recursive) for better performance.
// Limits: max 10 levels deep, max 10000 files, skips ignored directories.
func countDirContents(root string) DirStats {
	const maxDepth = 10
	const maxFiles = 10000

	// Use centralized skip directories from config
	ignoredDirs := config.SkipDirectoriesSet(nil)

	rootDepth := strings.Count(root, string(filepath.Separator))
	var stats DirStats

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}

		// Check depth limit
		currentDepth := strings.Count(path, string(filepath.Separator)) - rootDepth
		if currentDepth > maxDepth {
			return fs.SkipDir
		}

		// Skip ignored directories
		if d.IsDir() {
			name := d.Name()
			if ignoredDirs[name] || (len(name) > 0 && name[0] == '.' && name != ".") {
				return fs.SkipDir
			}
			// Count direct subdirectories only (depth == 1)
			if currentDepth == 1 {
				stats.FolderCount++
			}
			return nil // Continue into directory
		}

		// Count file
		stats.FileCount++
		if stats.FileCount > maxFiles {
			return fs.SkipAll // Stop walking, we have enough
		}

		if info, err := d.Info(); err == nil {
			stats.TotalSize += info.Size()
		}
		return nil
	})

	return stats
}

// formatBytes converts bytes to human readable format.
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes) / float64(div)
	if value < 10 {
		return fmt.Sprintf("%.1f %s", value, units[exp])
	}
	return fmt.Sprintf("%.0f %s", value, units[exp])
}

// formatRelativeTime converts a time to relative format like "2 hours ago".
func formatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 30*24*time.Hour:
		weeks := int(diff.Hours() / 24 / 7)
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	default:
		return t.Format("Jan 2, 2006")
	}
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
