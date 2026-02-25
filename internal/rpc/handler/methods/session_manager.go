// Package methods provides JSON-RPC method implementations for the session-based architecture.
package methods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/codex"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/creack/pty"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// SessionFocusProvider handles session focus tracking for multi-device awareness.
type SessionFocusProvider interface {
	SetSessionFocus(clientID, workspaceID, sessionID string) (interface{}, error)
}

// SessionManagerService handles session-related JSON-RPC methods for the new architecture.
// This replaces the old workspace start/stop with session-based management.
type SessionManagerService struct {
	manager        *session.Manager
	focusProvider  SessionFocusProvider
	sessionService *SessionService

	codexMu              sync.Mutex
	codexSessions        map[string]*codexPTYSession
	codexWatchers        map[string]session.WatchInfo
	codexLastPTYLogLine  map[string]string
	codexSessionWatchers map[string]context.CancelFunc
	runtimeDispatch      map[string]sessionRuntimeDispatch
}

const (
	sessionManagerAgentClaude = "claude"
	sessionManagerAgentCodex  = "codex"

	// Codex PTY can emit very high-frequency TUI output. Batch lines briefly to
	// reduce hub pressure and avoid dropping bursts of pty_output events.
	codexPTYOutputFlushInterval = 80 * time.Millisecond
	codexPTYOutputMaxLines      = 12
	codexPTYOutputMaxBytes      = 8 * 1024

	// Keep PTY state strings aligned with PTYState enum consumed by clients.
	ptyStateThinking = "thinking"
	ptyStateIdle     = "idle"
	ptyStateError    = "error"

	codexContextWindowErrorNeedle = "codex ran out of room in the model's context window"
)

var (
	codexANSIRegex = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[PX^_][^\x1b]*\x1b\\|\x1b[\(\)][AB012]|\x1b[>=]`)
	// Strip non-printing control chars while keeping newline/tab/carriage-return.
	codexControlRegex = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1a\x1c-\x1f\x7f]`)
)

type codexPTYSession struct {
	sessionIDMu sync.RWMutex
	sessionID   string
	workspaceID string
	workspace   string
	cmd         *exec.Cmd
	ptmx        *os.File
}

func (c *codexPTYSession) SessionID() string {
	c.sessionIDMu.RLock()
	defer c.sessionIDMu.RUnlock()
	return c.sessionID
}

func (c *codexPTYSession) SetSessionID(sessionID string) {
	c.sessionIDMu.Lock()
	c.sessionID = sessionID
	c.sessionIDMu.Unlock()
}

// NewSessionManagerService creates a new session manager service.
func NewSessionManagerService(manager *session.Manager) *SessionManagerService {
	service := &SessionManagerService{
		manager:              manager,
		codexSessions:        make(map[string]*codexPTYSession),
		codexWatchers:        make(map[string]session.WatchInfo),
		codexLastPTYLogLine:  make(map[string]string),
		codexSessionWatchers: make(map[string]context.CancelFunc),
	}
	service.ensureRuntimeDispatch()
	return service
}

// SetFocusProvider sets the session focus provider for multi-device awareness.
// This allows workspace/session/watch to also update focus tracking.
func (s *SessionManagerService) SetFocusProvider(provider SessionFocusProvider) {
	s.focusProvider = provider
}

// SetSessionService sets the shared session service for runtime-scoped delegation.
func (s *SessionManagerService) SetSessionService(sessionService *SessionService) {
	s.sessionService = sessionService
}

// RegisterMethods registers all session management methods with the handler.
func (s *SessionManagerService) RegisterMethods(registry *handler.Registry) {
	// Session lifecycle methods
	registry.RegisterWithMeta("session/start", s.Start, handler.MethodMeta{
		Summary:     "Start or attach to a session for a workspace",
		Description: "Starts or attaches to a runtime session (Claude or Codex) for the workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Optional session ID to attach to."}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude", "description": "Agent runtime type."}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "session",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/stop", s.Stop, handler.MethodMeta{
		Summary:     "Stop a running session",
		Description: "Stops a running Claude or Codex session process.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude", "description": "Agent runtime type."}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/send", s.Send, handler.MethodMeta{
		Summary:     "Send a prompt to a session",
		Description: "Sends a prompt to the selected runtime. If session_id is provided, sends to that session. If only workspace_id is provided with mode='new', auto-creates a new session and sends the prompt.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Session ID to send to. If empty, workspace_id must be provided to auto-create a session."}},
			{Name: "workspace_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Workspace ID. Required when session_id is empty to auto-create a new session."}},
			{Name: "prompt", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "mode", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"new", "continue"}, "default": "new", "description": "Session mode. 'new' starts fresh conversation (default), 'continue' resumes existing."}},
			{Name: "permission_mode", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"default", "acceptEdits", "bypassPermissions", "plan", "interactive"}, "default": "default", "description": "Permission handling mode. Use 'acceptEdits' to auto-accept file edits, 'bypassPermissions' to skip all permission checks, 'interactive' to use PTY mode for true terminal-like permission prompts."}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude", "description": "Agent runtime type."}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/input", s.Input, handler.MethodMeta{
		Summary:     "Send input to an interactive session",
		Description: "Sends keyboard input to a session running in interactive (PTY) mode. Either 'input' (raw text) or 'key' (special key name) must be provided.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "input", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Raw text input to send (e.g., '1' for Yes, '2' for Yes all, 'n' for No). A carriage return is auto-appended for text input."}},
			{Name: "key", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"enter", "escape", "up", "down", "left", "right", "tab", "backspace", "delete", "home", "end", "pageup", "pagedown", "space"}, "description": "Special key name to send. Use 'enter' to confirm prompts, arrow keys for navigation."}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude", "description": "Agent runtime type."}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("session/respond", s.Respond, handler.MethodMeta{
		Summary:     "Respond to a permission or question",
		Description: "Responds to a pending permission request or interactive question for the selected runtime.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "type", Required: true, Schema: map[string]interface{}{"type": "string", "enum": []string{"permission", "question"}}},
			{Name: "response", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude", "description": "Agent runtime type."}},
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
		Summary:     "Get historical sessions for a workspace (legacy, use workspace/session/history)",
		Description: "Returns historical sessions for the selected runtime in the specified workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "history",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/history", s.History, handler.MethodMeta{
		Summary:     "Get historical sessions for a workspace",
		Description: "Returns historical sessions for the selected runtime in the specified workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "history",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/messages", s.GetSessionMessages, handler.MethodMeta{
		Summary:     "Get messages from a historical session",
		Description: "Returns paginated messages from a runtime session file for the specified workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "limit", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 50}},
			{Name: "offset", Required: false, Schema: map[string]interface{}{"type": "integer", "default": 0}},
			{Name: "order", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"asc", "desc"}, "default": "asc"}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "messages",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/watch", s.WatchSession, handler.MethodMeta{
		Summary:     "Start watching a session for real-time updates",
		Description: "Starts watching a session file for new messages for the selected runtime. A client may watch multiple sessions concurrently.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "watch_info",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/session/unwatch", s.UnwatchSession, handler.MethodMeta{
		Summary:     "Stop watching a session",
		Description: "Stops watching a session for the selected runtime. If session_id is omitted, legacy behavior removes one watched session deterministically.",
		Params: []handler.OpenRPCParam{
			{Name: "agent_type", Required: true, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}}},
			{Name: "session_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Optional session ID to unwatch."}},
		},
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

	registry.RegisterWithMeta("workspace/session/delete", s.DeleteHistorySession, handler.MethodMeta{
		Summary:     "Delete a historical session file",
		Description: "Deletes a runtime session file from local history storage for the specified workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Workspace ID"}},
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Session ID (UUID) to delete"}},
			{Name: "agent_type", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"claude", "codex"}, "default": "claude"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "delete_result",
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

// Start starts or attaches to a session for a workspace.
// Runtime behavior is selected by agent_type (claude or codex).
func (s *SessionManagerService) Start(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"` // Optional: attach to existing LIVE session
		AgentType   string `json:"agent_type"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	_, runtimeDispatch, dispatchErr := s.resolveRuntimeDispatch(p.AgentType)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	return runtimeDispatch.start(ctx, p.WorkspaceID, p.SessionID)
}

// Stop stops a running session.
func (s *SessionManagerService) Stop(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
		AgentType string `json:"agent_type"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	_, runtimeDispatch, dispatchErr := s.resolveRuntimeDispatch(p.AgentType)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	return runtimeDispatch.stop(ctx, p.SessionID)
}

// Send sends a prompt to a session.
// If session_id is empty but workspace_id is provided, auto-creates a new session.
// Runtime behavior is selected by agent_type (claude or codex).
func (s *SessionManagerService) Send(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID      string `json:"session_id"`
		WorkspaceID    string `json:"workspace_id"`
		Prompt         string `json:"prompt"`
		Mode           string `json:"mode"`            // "new" or "continue"
		PermissionMode string `json:"permission_mode"` // "default", "acceptEdits", "bypassPermissions", "plan", "interactive"
		AgentType      string `json:"agent_type"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.Prompt == "" {
		return nil, message.NewError(message.InvalidParams, "prompt is required")
	}

	_, runtimeDispatch, dispatchErr := s.resolveRuntimeDispatchForWorkspaceSession(p.AgentType, p.WorkspaceID, p.SessionID)
	if dispatchErr != nil {
		return nil, dispatchErr
	}

	if err := validatePermissionMode(p.PermissionMode); err != nil {
		return nil, err
	}

	// Default mode to "new" if not specified
	if p.Mode == "" {
		p.Mode = "new"
	}

	return runtimeDispatch.send(ctx, p.WorkspaceID, p.SessionID, p.Prompt, p.Mode, p.PermissionMode)
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
		AgentType string `json:"agent_type"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.SessionID == "" {
		return nil, message.NewError(message.InvalidParams, "session_id is required")
	}

	_, runtimeDispatch, dispatchErr := s.resolveRuntimeDispatch(p.AgentType)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	return runtimeDispatch.input(ctx, p.SessionID, p.Input, p.Key)
}

// Respond responds to a permission or question.
func (s *SessionManagerService) Respond(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		SessionID string `json:"session_id"`
		Type      string `json:"type"`     // "permission" or "question"
		Response  string `json:"response"` // "yes"/"no" for permission, or text for question
		AgentType string `json:"agent_type"`
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

	_, runtimeDispatch, dispatchErr := s.resolveRuntimeDispatch(p.AgentType)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	return runtimeDispatch.respond(ctx, p.SessionID, p.Type, p.Response)
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

// History returns historical sessions for a workspace.
func (s *SessionManagerService) History(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		Limit       int    `json:"limit"`
		AgentType   string `json:"agent_type,omitempty"`
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

	agentType, _, dispatchErr := s.resolveRuntimeDispatch(p.AgentType)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	if rpcErr := s.ensureSessionManagerConfigured("workspace/session/history"); rpcErr != nil {
		return nil, rpcErr
	}

	var (
		history []session.HistoryInfo
		err     error
	)

	switch agentType {
	case sessionManagerAgentCodex:
		history, err = s.listCodexHistory(p.WorkspaceID, limit)
	default:
		history, err = s.manager.ListHistory(p.WorkspaceID, limit)
	}
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"sessions": history,
		"total":    len(history),
	}, nil
}

// GetSessionMessages returns messages from a historical session.
func (s *SessionManagerService) GetSessionMessages(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"`
		Limit       int    `json:"limit"`
		Offset      int    `json:"offset"`
		Order       string `json:"order"`
		AgentType   string `json:"agent_type,omitempty"`
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

	agentType, _, dispatchErr := s.resolveRuntimeDispatchForWorkspaceSession(p.AgentType, p.WorkspaceID, p.SessionID)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	if rpcErr := s.ensureSessionManagerConfigured("workspace/session/messages"); rpcErr != nil {
		return nil, rpcErr
	}

	switch agentType {
	case sessionManagerAgentCodex:
		if _, err := s.resolveCodexSessionForWorkspace(p.WorkspaceID, p.SessionID); err != nil {
			if strings.Contains(err.Error(), "session not found") {
				return nil, message.ErrSessionNotFound(p.SessionID)
			}
			return nil, message.NewError(message.InternalError, err.Error())
		}

		result, rpcErr := s.getRuntimeSessionMessages(ctx, sessionManagerAgentCodex, p.SessionID, limit, p.Offset, order)
		if rpcErr != nil {
			return nil, rpcErr
		}
		return result, nil
	default:
		result, err := s.manager.GetSessionMessages(p.WorkspaceID, p.SessionID, limit, p.Offset, order)
		if err != nil {
			if strings.Contains(err.Error(), "session not found") {
				return nil, message.ErrSessionNotFound(p.SessionID)
			}
			return nil, message.NewError(message.InternalError, err.Error())
		}
		return result, nil
	}
}

// DeleteHistorySession deletes a historical session file.
func (s *SessionManagerService) DeleteHistorySession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
		SessionID   string `json:"session_id"`
		AgentType   string `json:"agent_type,omitempty"`
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

	agentType, _, dispatchErr := s.resolveRuntimeDispatchForWorkspaceSession(p.AgentType, p.WorkspaceID, p.SessionID)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	if rpcErr := s.ensureSessionManagerConfigured("workspace/session/delete"); rpcErr != nil {
		return nil, rpcErr
	}

	var err error
	switch agentType {
	case sessionManagerAgentCodex:
		err = s.deleteCodexWorkspaceSession(p.WorkspaceID, p.SessionID)
	default:
		err = s.manager.DeleteHistorySession(p.WorkspaceID, p.SessionID)
	}
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, message.NewError(message.SessionNotFound, err.Error())
		}
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"status":       "deleted",
		"workspace_id": p.WorkspaceID,
		"session_id":   p.SessionID,
	}, nil
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
		AgentType   string `json:"agent_type,omitempty"`
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

	agentType, _, dispatchErr := s.resolveRuntimeDispatchForWorkspaceSession(p.AgentType, p.WorkspaceID, p.SessionID)
	if dispatchErr != nil {
		return nil, dispatchErr
	}
	if rpcErr := s.ensureSessionManagerConfigured("workspace/session/watch"); rpcErr != nil {
		return nil, rpcErr
	}

	// Get client ID for tracking watchers
	clientID, _ := ctx.Value(handler.ClientIDKey).(string)

	switch agentType {
	case sessionManagerAgentCodex:
		if _, err := s.resolveCodexSessionForWorkspace(p.WorkspaceID, p.SessionID); err != nil {
			if strings.Contains(err.Error(), "session not found") {
				return nil, message.ErrSessionNotFound(p.SessionID)
			}
			return nil, message.NewError(message.InternalError, err.Error())
		}

		if rpcErr := s.watchRuntimeSession(ctx, sessionManagerAgentCodex, p.SessionID); rpcErr != nil {
			return nil, rpcErr
		}

		if clientID != "" {
			s.codexMu.Lock()
			s.codexWatchers[clientID] = session.WatchInfo{
				WorkspaceID: p.WorkspaceID,
				SessionID:   p.SessionID,
				Watching:    true,
			}
			s.codexMu.Unlock()
		}

		if s.focusProvider != nil && clientID != "" {
			_, _ = s.focusProvider.SetSessionFocus(clientID, p.WorkspaceID, p.SessionID)
		}

		return map[string]interface{}{
			"status":       "watching",
			"watching":     true,
			"workspace_id": p.WorkspaceID,
			"session_id":   p.SessionID,
		}, nil
	default:
		info, err := s.manager.WatchWorkspaceSession(clientID, p.WorkspaceID, p.SessionID)
		if err != nil {
			if strings.Contains(err.Error(), "session not found") {
				return nil, message.ErrSessionNotFound(p.SessionID)
			}
			return nil, message.NewError(message.InternalError, err.Error())
		}

		// Also update focus tracking so this client appears in the session's viewers list
		if s.focusProvider != nil && clientID != "" {
			_, _ = s.focusProvider.SetSessionFocus(clientID, p.WorkspaceID, p.SessionID)
		}

		// If there's a pending PTY permission prompt, re-emit it for this watcher.
		if existingSession, err := s.manager.GetSession(info.SessionID); err == nil {
			if cm := existingSession.ClaudeManager(); cm != nil {
				if pendingPerm := cm.GetPendingPTYPermission(); pendingPerm != nil {
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
						info.SessionID,
						options,
					))

					log.Info().
						Str("type", string(pendingPerm.Type)).
						Str("target", pendingPerm.Target).
						Int("options", len(pendingPerm.Options)).
						Str("session_id", info.SessionID).
						Msg("Re-emitted pending PTY permission for watcher")
				}
			}
		}

		return map[string]interface{}{
			"status":       "watching",
			"watching":     info.Watching,
			"workspace_id": info.WorkspaceID,
			"session_id":   info.SessionID,
		}, nil
	}
}

// UnwatchSession stops watching a session.
// Returns the previous watch info.
func (s *SessionManagerService) UnwatchSession(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		AgentType string `json:"agent_type"`
		SessionID string `json:"session_id,omitempty"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
		}
	}

	agentType := strings.ToLower(strings.TrimSpace(p.AgentType))
	if agentType == "" {
		return nil, message.NewError(message.InvalidParams, "agent_type is required")
	}

	if _, _, dispatchErr := s.resolveRuntimeDispatch(agentType); dispatchErr != nil {
		return nil, dispatchErr
	}

	// Get client ID for tracking watchers
	clientID, _ := ctx.Value(handler.ClientIDKey).(string)
	targetSessionID := strings.TrimSpace(p.SessionID)

	if agentType == sessionManagerAgentCodex {
		info := session.WatchInfo{Watching: false}
		remainingWatchers := 0
		removedWatcher := false
		stillWatching := false

		if clientID != "" {
			s.codexMu.Lock()
			if watchedInfo, ok := s.codexWatchers[clientID]; ok {
				info.WorkspaceID = watchedInfo.WorkspaceID
				info.SessionID = watchedInfo.SessionID
				stillWatching = true
				if targetSessionID == "" || watchedInfo.SessionID == targetSessionID {
					delete(s.codexWatchers, clientID)
					removedWatcher = true
					stillWatching = false
				}
			}
			remainingWatchers = len(s.codexWatchers)
			s.codexMu.Unlock()
		} else {
			s.codexMu.Lock()
			remainingWatchers = len(s.codexWatchers)
			s.codexMu.Unlock()
		}

		if removedWatcher && remainingWatchers == 0 {
			if rpcErr := s.unwatchRuntimeSession(ctx, sessionManagerAgentCodex); rpcErr != nil {
				return nil, rpcErr
			}
		}

		return map[string]interface{}{
			"status":       "unwatched",
			"watching":     stillWatching,
			"workspace_id": info.WorkspaceID,
			"session_id":   info.SessionID,
		}, nil
	}

	if rpcErr := s.ensureSessionManagerConfigured("workspace/session/unwatch"); rpcErr != nil {
		return nil, rpcErr
	}

	info := s.manager.UnwatchWorkspaceSession(clientID, targetSessionID)

	return map[string]interface{}{
		"status":       "unwatched",
		"watching":     info.Watching,
		"workspace_id": info.WorkspaceID,
		"session_id":   info.SessionID,
	}, nil
}

func (s *SessionManagerService) ensureSessionManagerConfigured(method string) *message.Error {
	if s.manager != nil {
		return nil
	}

	return message.NewErrorWithData(
		message.AgentNotConfigured,
		"session manager is not configured",
		map[string]string{
			"method": method,
		},
	)
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
	FolderCount      int       `json:"folder_count"` // Direct subdirectories count
	FileCount        int       `json:"file_count"`   // Recursive file count
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

	// Base64-encode binary files (images, PDFs, fonts, etc.) so they survive JSON marshaling.
	encoding := "utf-8"
	contentStr := string(content)
	if isBinaryFileExtension(filepath.Ext(p.Path)) {
		encoding = "base64"
		contentStr = base64.StdEncoding.EncodeToString(content)
	}

	return map[string]interface{}{
		"path":      p.Path,
		"content":   contentStr,
		"encoding":  encoding,
		"truncated": truncated,
		"size":      len(content),
	}, nil
}

// isBinaryFileExtension returns true for file extensions that indicate binary content
// which must be base64-encoded in the JSON-RPC response to survive marshaling.
func isBinaryFileExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".bmp", ".ico", ".tiff",
		".pdf",
		".zip", ".tar", ".gz", ".rar", ".7z",
		".exe", ".dll", ".so", ".dylib",
		".ttf", ".otf", ".woff", ".woff2":
		return true
	}
	return false
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

var errCodexSessionNotRunning = errors.New("codex session is not running")

func (s *SessionManagerService) startCodexSession(ctx context.Context, workspaceID, sessionID string) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewErrorWithData(
			message.AgentNotConfigured,
			"codex runtime requires session manager context",
			map[string]string{
				"agent_type": sessionManagerAgentCodex,
				"method":     "session/start",
			},
		)
	}

	ws, err := s.manager.GetWorkspace(workspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to get workspace: "+err.Error())
	}

	if sessionID != "" {
		if _, err := codex.GetGlobalIndexCache().FindSessionByID(sessionID); err != nil {
			return map[string]interface{}{
				"session_id":   "",
				"workspace_id": workspaceID,
				"source":       "",
				"status":       "not_found",
				"agent_type":   sessionManagerAgentCodex,
				"message":      "Session not found in Codex history.",
			}, nil
		}
		return map[string]interface{}{
			"session_id":   sessionID,
			"workspace_id": workspaceID,
			"source":       "history",
			"status":       "attached",
			"agent_type":   sessionManagerAgentCodex,
			"message":      "Codex session is ready for interaction",
		}, nil
	}

	entries, err := codex.GetGlobalIndexCache().GetSessionsForProject(ws.Definition.Path)
	if err != nil && !errors.Is(err, codex.ErrProjectNotFound) {
		return nil, message.NewError(message.InternalError, "failed to load Codex history: "+err.Error())
	}
	if len(entries) > 0 {
		return map[string]interface{}{
			"session_id":   entries[0].SessionID,
			"workspace_id": workspaceID,
			"source":       "history",
			"status":       "attached",
			"agent_type":   sessionManagerAgentCodex,
			"message":      "Latest Codex session found in history - ready for interaction",
		}, nil
	}

	if existing := s.getCodexSessionForWorkspace(workspaceID); existing != nil {
		return map[string]interface{}{
			"session_id":   existing.SessionID(),
			"workspace_id": workspaceID,
			"source":       "managed",
			"status":       "attached",
			"agent_type":   sessionManagerAgentCodex,
			"message":      "Returning existing active Codex session",
		}, nil
	}

	before := s.snapshotCodexSessionIDs(ws.Definition.Path)
	temporaryID := "codex-temp-" + uuid.NewString()
	if err := s.startCodexProcess(ctx, temporaryID, workspaceID, ws.Definition.Path, []string{}); err != nil {
		return nil, codexRuntimeError("session/start", err)
	}

	realID, err := s.resolveNewCodexSessionID(ws.Definition.Path, before, 10*time.Second)
	if err == nil && realID != "" {
		s.remapCodexSessionID(temporaryID, realID)
		evt := events.NewSessionIDResolvedEvent(temporaryID, realID, workspaceID, "")
		evt.SetAgentType(sessionManagerAgentCodex)
		s.manager.PublishEvent(evt)
		sessionID = realID
	} else {
		s.startCodexSessionIDWatcher(ctx, workspaceID, ws.Definition.Path, temporaryID, before)
		sessionID = temporaryID
	}

	return map[string]interface{}{
		"session_id":   sessionID,
		"workspace_id": workspaceID,
		"source":       "managed",
		"status":       "started",
		"agent_type":   sessionManagerAgentCodex,
		"message":      "New Codex session started in interactive mode",
	}, nil
}

func (s *SessionManagerService) sendCodexPrompt(ctx context.Context, workspaceID, sessionID, prompt, mode string) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewErrorWithData(
			message.AgentNotConfigured,
			"codex runtime requires session manager context",
			map[string]string{
				"agent_type": sessionManagerAgentCodex,
				"method":     "session/send",
			},
		)
	}
	if mode != "new" && mode != "continue" {
		return nil, message.NewError(message.InvalidParams, "mode must be one of: new, continue")
	}

	workspacePath := ""
	if workspaceID != "" {
		ws, err := s.manager.GetWorkspace(workspaceID)
		if err != nil {
			return nil, message.NewError(message.InternalError, "failed to get workspace: "+err.Error())
		}
		workspacePath = ws.Definition.Path
	}

	if sessionID != "" {
		entry, err := codex.GetGlobalIndexCache().FindSessionByID(sessionID)
		switch {
		case err != nil || entry == nil:
			log.Warn().
				Str("session_id", sessionID).
				Str("workspace_id", workspaceID).
				Str("workspace_path", workspacePath).
				Msg("codex session_id not found in codex history; falling back to workspace latest/new session")
			sessionID = ""
		case workspacePath != "" && !codexWorkspacePathsMatch(workspacePath, entry.ProjectPath):
			log.Warn().
				Str("session_id", sessionID).
				Str("workspace_id", workspaceID).
				Str("workspace_path", workspacePath).
				Str("session_project_path", entry.ProjectPath).
				Msg("codex session_id does not match workspace path; falling back to workspace latest/new session")
			sessionID = ""
		case workspacePath == "":
			workspacePath = entry.ProjectPath
		}
	}

	if sessionID == "" && mode == "continue" {
		if workspacePath == "" {
			return nil, message.NewError(message.InvalidParams, "workspace_id is required when mode is continue and session_id is empty")
		}
		entries, err := codex.GetGlobalIndexCache().GetSessionsForProject(workspacePath)
		if err != nil || len(entries) == 0 {
			return nil, message.NewError(message.SessionNotFound, "no Codex session found to continue")
		}
		sessionID = entries[0].SessionID
	}

	if sessionID != "" && workspacePath == "" {
		if entry, err := codex.GetGlobalIndexCache().FindSessionByID(sessionID); err == nil && entry != nil {
			workspacePath = entry.ProjectPath
		}
	}
	if workspacePath == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// For deterministic mobile behavior, session/send always launches a Codex process.
	// Interactive in-session typing remains available via session/input and session/respond.
	if sessionID == "" && mode == "new" {
		if existing := s.getCodexSessionForWorkspace(workspaceID); existing != nil {
			if err := s.sendCodexInput(existing.SessionID(), prompt); err != nil {
				return nil, codexRuntimeError("session/send", err)
			}
			return map[string]interface{}{
				"status":       "sent",
				"session_id":   existing.SessionID(),
				"workspace_id": workspaceID,
				"agent_type":   sessionManagerAgentCodex,
				"delivery":     "pty_input",
				"message":      "Codex prompt sent to active session",
			}, nil
		}
	}
	if sessionID != "" {
		_ = s.stopCodexSession(sessionID)
	}

	// New Codex session: start interactive codex with prompt and resolve session id from ~/.codex/sessions.
	if sessionID == "" {
		before := s.snapshotCodexSessionIDs(workspacePath)
		temporaryID := "codex-temp-" + uuid.NewString()
		args := buildCodexCLIArgs(workspacePath, "", prompt)
		if err := s.startCodexProcess(ctx, temporaryID, workspaceID, workspacePath, args); err != nil {
			return nil, codexRuntimeError("session/send", err)
		}

		realID, err := s.resolveNewCodexSessionID(workspacePath, before, 10*time.Second)
		if err == nil && realID != "" {
			s.remapCodexSessionID(temporaryID, realID)
			evt := events.NewSessionIDResolvedEvent(temporaryID, realID, workspaceID, "")
			evt.SetAgentType(sessionManagerAgentCodex)
			s.manager.PublishEvent(evt)
			sessionID = realID
		} else {
			s.startCodexSessionIDWatcher(ctx, workspaceID, workspacePath, temporaryID, before)
			sessionID = temporaryID
		}

		return map[string]interface{}{
			"status":       "sent",
			"session_id":   sessionID,
			"workspace_id": workspaceID,
			"auto_created": true,
			"agent_type":   sessionManagerAgentCodex,
			"delivery":     "new_process",
			"message":      "Codex prompt sent",
		}, nil
	}

	args := buildCodexCLIArgs(workspacePath, sessionID, prompt)
	if err := s.startCodexProcess(ctx, sessionID, workspaceID, workspacePath, args); err != nil {
		return nil, codexRuntimeError("session/send", err)
	}
	log.Debug().
		Str("session_id", sessionID).
		Str("workspace_id", workspaceID).
		Str("workspace_path", workspacePath).
		Str("mode", mode).
		Msg("started codex resume process for session/send")

	return map[string]interface{}{
		"status":     "sent",
		"session_id": sessionID,
		"agent_type": sessionManagerAgentCodex,
		"delivery":   "resume_process",
		"source":     "resume_command",
	}, nil
}

func (s *SessionManagerService) respondCodexSession(sessionID, responseType, response string) error {
	switch responseType {
	case "permission":
		normalized := strings.ToLower(strings.TrimSpace(response))
		switch normalized {
		case "yes", "true", "allow", "approved":
			return s.sendCodexInput(sessionID, "y")
		case "no", "false", "deny", "denied":
			return s.sendCodexInput(sessionID, "n")
		default:
			return s.sendCodexInput(sessionID, response)
		}
	case "question":
		return s.sendCodexInput(sessionID, response)
	default:
		return fmt.Errorf("type must be 'permission' or 'question'")
	}
}

func (s *SessionManagerService) stopCodexSession(sessionID string) error {
	codexSession := s.getCodexSession(sessionID)
	if codexSession == nil {
		return nil
	}

	if codexSession.cmd != nil && codexSession.cmd.Process != nil {
		_ = codexSession.cmd.Process.Signal(os.Interrupt)

		// Best-effort fallback if Codex ignores SIGINT.
		go func(expectedID string, proc *os.Process) {
			time.Sleep(2 * time.Second)
			s.codexMu.Lock()
			stillRunning := s.codexSessions[expectedID] != nil
			s.codexMu.Unlock()
			if stillRunning {
				_ = proc.Kill()
			}
		}(sessionID, codexSession.cmd.Process)
	}

	return nil
}

func (s *SessionManagerService) sendCodexInput(sessionID, input string) error {
	codexSession := s.getCodexSession(sessionID)
	if codexSession == nil || codexSession.ptmx == nil {
		return fmt.Errorf("%w: %s", errCodexSessionNotRunning, sessionID)
	}

	encoded := encodeCodexPTYInput(input)
	_, err := codexSession.ptmx.Write([]byte(encoded))
	if err != nil {
		return fmt.Errorf("failed to write Codex PTY input: %w", err)
	}
	return nil
}

func codexWorkspacePathsMatch(workspacePath, sessionProjectPath string) bool {
	workspacePath = strings.TrimSpace(workspacePath)
	sessionProjectPath = strings.TrimSpace(sessionProjectPath)
	if workspacePath == "" || sessionProjectPath == "" {
		return true
	}

	absWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		absWorkspace = filepath.Clean(workspacePath)
	}
	absSession, err := filepath.Abs(sessionProjectPath)
	if err != nil {
		absSession = filepath.Clean(sessionProjectPath)
	}

	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		absWorkspace = strings.ToLower(absWorkspace)
		absSession = strings.ToLower(absSession)
	}
	if absWorkspace == absSession {
		return true
	}
	workspacePrefix := absWorkspace + string(filepath.Separator)
	sessionPrefix := absSession + string(filepath.Separator)
	return strings.HasPrefix(absSession, workspacePrefix) || strings.HasPrefix(absWorkspace, sessionPrefix)
}

func encodeCodexPTYInput(input string) string {
	trimmed := strings.TrimSpace(strings.ToLower(input))
	keyNameToSequence := map[string]string{
		"enter": "\r", "return": "\r",
		"escape": "\x1b", "esc": "\x1b",
		"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
		"tab": "\t", "backspace": "\x7f", "space": " ",
	}
	if seq, ok := keyNameToSequence[trimmed]; ok {
		return seq
	}

	isSpecialKey := false
	if len(input) > 0 {
		first := input[0]
		if first < 0x20 || first == 0x7f {
			isSpecialKey = true
		}
	}
	if isSpecialKey || strings.HasSuffix(input, "\r") || strings.HasSuffix(input, "\n") {
		return input
	}
	return input + "\r"
}

func buildCodexCLIArgs(workspacePath, resumeSessionID, prompt string) []string {
	args := make([]string, 0, 7)
	_ = workspacePath // Workspace binding is handled via cmd.Dir.
	args = append(args, "exec")
	if resumeSessionID != "" {
		args = append(args, "resume", resumeSessionID)
	}
	// Preserve prompt verbatim (including leading "!") so Codex can interpret bash mode.
	if prompt != "" {
		args = append(args, prompt)
	}
	return args
}

func (s *SessionManagerService) startCodexProcess(ctx context.Context, mapSessionID, workspaceID, workspacePath string, args []string) error {
	if args == nil {
		args = []string{}
	}

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Dir = workspacePath

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return err
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})

	codexSession := &codexPTYSession{
		sessionID:   mapSessionID,
		workspaceID: workspaceID,
		workspace:   workspacePath,
		cmd:         cmd,
		ptmx:        ptmx,
	}

	s.codexMu.Lock()
	s.codexSessions[mapSessionID] = codexSession
	s.codexMu.Unlock()

	s.publishCodexPTYState(mapSessionID, ptyStateThinking)

	go s.streamCodexOutput(codexSession)
	go s.waitCodexProcess(codexSession)
	return nil
}

func (s *SessionManagerService) streamCodexOutput(codexSession *codexPTYSession) {
	chunksCh := make(chan string, 64)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunksCh)
		readBuf := make([]byte, 8*1024)

		for {
			n, err := codexSession.ptmx.Read(readBuf)
			if n > 0 {
				chunk := string(readBuf[:n])
				chunksCh <- chunk
			}

			if err != nil {
				if !errors.Is(err, io.EOF) {
					errCh <- err
				}
				return
			}
		}
	}()

	flushTicker := time.NewTicker(codexPTYOutputFlushInterval)
	defer flushTicker.Stop()

	incompleteLine := ""
	pending := make([]string, 0, codexPTYOutputMaxLines)
	pendingBytes := 0
	queueLine := func(line string) {
		trimmedLine := strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(trimmedLine) == "" {
			return
		}

		// Keep very large lines as immediate standalone events.
		if len(trimmedLine) >= codexPTYOutputMaxBytes {
			if len(pending) > 0 {
				s.publishCodexPTYOutput(codexSession.SessionID(), strings.Join(pending, "\n"), ptyStateThinking)
				pending = pending[:0]
				pendingBytes = 0
			}
			s.publishCodexPTYOutput(codexSession.SessionID(), trimmedLine, ptyStateThinking)
			return
		}

		pending = append(pending, trimmedLine)
		pendingBytes += len(trimmedLine) + 1 // + newline join cost
		if len(pending) >= codexPTYOutputMaxLines || pendingBytes >= codexPTYOutputMaxBytes {
			s.publishCodexPTYOutput(codexSession.SessionID(), strings.Join(pending, "\n"), ptyStateThinking)
			pending = pending[:0]
			pendingBytes = 0
		}
	}
	processChunk := func(chunk string) {
		if chunk == "" {
			return
		}

		incompleteLine += chunk
		for {
			newlineIdx := strings.IndexByte(incompleteLine, '\n')
			if newlineIdx < 0 {
				break
			}
			line := incompleteLine[:newlineIdx]
			incompleteLine = incompleteLine[newlineIdx+1:]
			queueLine(line)
		}
	}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		s.publishCodexPTYOutput(codexSession.SessionID(), strings.Join(pending, "\n"), ptyStateThinking)
		pending = pending[:0]
		pendingBytes = 0
	}

	for {
		select {
		case chunk, ok := <-chunksCh:
			if !ok {
				if strings.TrimSpace(incompleteLine) != "" {
					queueLine(incompleteLine)
					incompleteLine = ""
				}
				flush()
				select {
				case err := <-errCh:
					if err != nil && !errors.Is(err, os.ErrClosed) {
						log.Debug().Err(err).Str("session_id", codexSession.SessionID()).Msg("codex PTY read ended with error")
					}
				default:
				}
				return
			}
			processChunk(chunk)

		case <-flushTicker.C:
			// Emit partial line fragments periodically so UI sees progress even
			// when Codex prints without newline terminators.
			if strings.TrimSpace(incompleteLine) != "" {
				queueLine(incompleteLine)
				incompleteLine = ""
			}
			flush()
		}
	}
}

func (s *SessionManagerService) waitCodexProcess(codexSession *codexPTYSession) {
	waitErr := codexSession.cmd.Wait()
	_ = codexSession.ptmx.Close()

	s.codexMu.Lock()
	currentSessionID := codexSession.SessionID()
	for sessionID, current := range s.codexSessions {
		if current != codexSession {
			continue
		}
		delete(s.codexSessions, sessionID)
		delete(s.codexLastPTYLogLine, sessionID)
	}
	delete(s.codexLastPTYLogLine, currentSessionID)
	s.codexMu.Unlock()
	s.stopCodexSessionIDWatcher(currentSessionID)

	if waitErr != nil {
		s.publishCodexPTYOutput(currentSessionID, waitErr.Error(), ptyStateError)
	}
	s.publishCodexPTYState(currentSessionID, ptyStateIdle)
}

func (s *SessionManagerService) publishCodexPTYOutput(sessionID, text, state string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	cleanText := sanitizeCodexPTYOutputText(text)
	if strings.TrimSpace(cleanText) == "" {
		return
	}
	logLine := normalizeCodexPTYLogText(text)
	if logLine != "" && s.shouldLogCodexPTYOutput(sessionID, logLine) {
		log.Debug().
			Str("clean", truncateCodexPTYLog(logLine, 160)).
			Str("state", state).
			Str("session_id", sessionID).
			Msg("PTY output")
	}
	evt := events.NewPTYOutputEventWithSession(cleanText, text, state, sessionID)
	evt.SetAgentType(sessionManagerAgentCodex)
	s.manager.PublishEvent(evt)

	// Codex can fail before emitting structured session JSONL messages when the
	// context window is exhausted. Emit a synthetic claude_message so clients
	// still show this failure in the message timeline.
	if contextErrText := extractCodexContextWindowErrorText(cleanText); contextErrText != "" {
		var workspaceID string
		if codexSession := s.getCodexSession(sessionID); codexSession != nil {
			workspaceID = codexSession.workspaceID
		}

		msgEvt := events.NewClaudeMessageEventFull(events.ClaudeMessagePayload{
			SessionID: sessionID,
			Type:      "assistant",
			Role:      "assistant",
			Content: []events.ClaudeMessageContent{
				{Type: "text", Text: contextErrText},
			},
			IsContextCompaction: true,
			Timestamp:           time.Now().UTC().Format(time.RFC3339Nano),
		})
		msgEvt.SetAgentType(sessionManagerAgentCodex)
		msgEvt.SetContext(workspaceID, sessionID)
		s.manager.PublishEvent(msgEvt)
	}
}

func (s *SessionManagerService) shouldLogCodexPTYOutput(sessionID, clean string) bool {
	s.codexMu.Lock()
	defer s.codexMu.Unlock()

	lastLine := s.codexLastPTYLogLine[sessionID]
	if clean == lastLine {
		return false
	}
	s.codexLastPTYLogLine[sessionID] = clean
	return true
}

func truncateCodexPTYLog(text string, max int) string {
	normalized := strings.ReplaceAll(text, "\r", "")
	normalized = strings.ReplaceAll(normalized, "\n", `\n`)
	if max <= 0 || len(normalized) <= max {
		return normalized
	}
	return normalized[:max-3] + "..."
}

func normalizeCodexPTYLogText(text string) string {
	return strings.TrimSpace(sanitizeCodexPTYOutputText(text))
}

func sanitizeCodexPTYOutputText(text string) string {
	withoutANSI := codexANSIRegex.ReplaceAllString(text, "")
	withoutControl := codexControlRegex.ReplaceAllString(withoutANSI, "")
	withoutEscape := strings.ReplaceAll(withoutControl, "\x1b", "")
	normalizedCRLF := strings.ReplaceAll(withoutEscape, "\r\n", "\n")
	return strings.ReplaceAll(normalizedCRLF, "\r", "\n")
}

func extractCodexContextWindowErrorText(cleanText string) string {
	if strings.TrimSpace(cleanText) == "" {
		return ""
	}

	for _, line := range strings.Split(cleanText, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), codexContextWindowErrorNeedle) {
			return trimmed
		}
	}

	return ""
}

func (s *SessionManagerService) publishCodexPTYState(sessionID, state string) {
	evt := events.NewPTYStateEventWithSession(state, false, "", sessionID)
	evt.SetAgentType(sessionManagerAgentCodex)
	s.manager.PublishEvent(evt)
}

func (s *SessionManagerService) emitCodexPermissionResolved(ctx context.Context, sessionID, input string) {
	clientID, _ := ctx.Value(handler.ClientIDKey).(string)
	evt := events.NewPTYPermissionResolvedEvent(sessionID, "", clientID, input)
	evt.SetAgentType(sessionManagerAgentCodex)
	s.manager.PublishEvent(evt)
}

func (s *SessionManagerService) listCodexHistory(workspaceID string, limit int) ([]session.HistoryInfo, error) {
	ws, err := s.manager.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	entries, err := codex.GetGlobalIndexCache().GetSessionsForProject(ws.Definition.Path)
	if err != nil {
		if errors.Is(err, codex.ErrProjectNotFound) {
			return []session.HistoryInfo{}, nil
		}
		return nil, err
	}

	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}

	history := make([]session.HistoryInfo, 0, len(entries))
	for _, entry := range entries {
		summary := strings.TrimSpace(entry.Summary)
		if summary == "" {
			summary = strings.TrimSpace(entry.FirstPrompt)
		}
		if summary == "" {
			summary = "Session " + entry.SessionID
		}

		lastUpdated := entry.Modified
		if lastUpdated.IsZero() {
			lastUpdated = entry.Created
		}

		history = append(history, session.HistoryInfo{
			SessionID:    entry.SessionID,
			Summary:      summary,
			MessageCount: entry.MessageCount,
			LastUpdated:  lastUpdated,
			Branch:       entry.GitBranch,
		})
	}

	return history, nil
}

func (s *SessionManagerService) resolveCodexSessionForWorkspace(workspaceID, sessionID string) (*codex.SessionIndexEntry, error) {
	ws, err := s.manager.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	entry, err := codex.GetGlobalIndexCache().FindSessionByID(sessionID)
	if err != nil || entry == nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if strings.TrimSpace(entry.ProjectPath) == "" {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	if !codexWorkspacePathsMatch(ws.Definition.Path, entry.ProjectPath) {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	return entry, nil
}

func (s *SessionManagerService) getRuntimeSessionMessages(ctx context.Context, agentType, sessionID string, limit, offset int, order string) (interface{}, *message.Error) {
	if s.sessionService == nil {
		return nil, message.NewError(message.AgentNotConfigured, "session service is not configured")
	}

	rawParams, err := json.Marshal(GetSessionMessagesParams{
		SessionID: sessionID,
		AgentType: agentType,
		Limit:     limit,
		Offset:    offset,
		Order:     order,
	})
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to encode runtime messages params: "+err.Error())
	}

	return s.sessionService.GetSessionMessages(ctx, rawParams)
}

func (s *SessionManagerService) watchRuntimeSession(ctx context.Context, agentType, sessionID string) *message.Error {
	if s.sessionService == nil {
		return message.NewError(message.AgentNotConfigured, "session service is not configured")
	}

	rawParams, err := json.Marshal(WatchSessionParams{
		SessionID: sessionID,
		AgentType: agentType,
	})
	if err != nil {
		return message.NewError(message.InternalError, "failed to encode runtime watch params: "+err.Error())
	}

	_, rpcErr := s.sessionService.WatchSession(ctx, rawParams)
	return rpcErr
}

func (s *SessionManagerService) unwatchRuntimeSession(ctx context.Context, agentType string) *message.Error {
	if s.sessionService == nil {
		return message.NewError(message.AgentNotConfigured, "session service is not configured")
	}

	var rawParams json.RawMessage
	if strings.TrimSpace(agentType) != "" {
		encoded, err := json.Marshal(struct {
			AgentType string `json:"agent_type,omitempty"`
		}{
			AgentType: agentType,
		})
		if err != nil {
			return message.NewError(message.InternalError, "failed to encode runtime unwatch params: "+err.Error())
		}
		rawParams = encoded
	}

	_, rpcErr := s.sessionService.UnwatchSession(ctx, rawParams)
	return rpcErr
}

func (s *SessionManagerService) deleteCodexWorkspaceSession(workspaceID, sessionID string) error {
	entry, err := s.resolveCodexSessionForWorkspace(workspaceID, sessionID)
	if err != nil {
		return err
	}
	if entry.FullPath == "" {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := os.Remove(entry.FullPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("session not found: %s", sessionID)
		}
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	_ = codex.GetGlobalIndexCache().InvalidateByPath(entry.FullPath)
	return nil
}

func (s *SessionManagerService) snapshotCodexSessionIDs(workspacePath string) map[string]struct{} {
	out := make(map[string]struct{})
	_ = codex.GetGlobalIndexCache().Refresh()
	entries, err := codex.GetGlobalIndexCache().GetSessionsForProject(workspacePath)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		out[entry.SessionID] = struct{}{}
	}
	return out
}

func (s *SessionManagerService) resolveNewCodexSessionID(workspacePath string, before map[string]struct{}, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_ = codex.GetGlobalIndexCache().Refresh()
		entries, err := codex.GetGlobalIndexCache().GetSessionsForProject(workspacePath)
		if err == nil && len(entries) > 0 {
			for _, entry := range entries {
				if _, exists := before[entry.SessionID]; !exists {
					return entry.SessionID, nil
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("timed out waiting for Codex session id")
}

func (s *SessionManagerService) startCodexSessionIDWatcher(ctx context.Context, workspaceID, workspacePath, temporaryID string, before map[string]struct{}) {
	if workspacePath == "" || temporaryID == "" {
		return
	}

	s.codexMu.Lock()
	if _, exists := s.codexSessionWatchers[temporaryID]; exists {
		s.codexMu.Unlock()
		return
	}
	watchCtx, cancel := context.WithCancel(ctx)
	s.codexSessionWatchers[temporaryID] = cancel
	s.codexMu.Unlock()

	go func() {
		defer s.stopCodexSessionIDWatcher(temporaryID)

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				if s.getCodexSession(temporaryID) == nil {
					return
				}

				_ = codex.GetGlobalIndexCache().Refresh()
				entries, err := codex.GetGlobalIndexCache().GetSessionsForProject(workspacePath)
				if err != nil && !errors.Is(err, codex.ErrProjectNotFound) {
					log.Debug().
						Err(err).
						Str("workspace_id", workspaceID).
						Str("workspace_path", workspacePath).
						Msg("codex session watcher refresh failed")
					continue
				}

				for _, entry := range entries {
					if _, exists := before[entry.SessionID]; exists {
						continue
					}
					if entry.SessionID == "" {
						continue
					}

					s.remapCodexSessionID(temporaryID, entry.SessionID)
					evt := events.NewSessionIDResolvedEvent(temporaryID, entry.SessionID, workspaceID, entry.FullPath)
					evt.SetAgentType(sessionManagerAgentCodex)
					s.manager.PublishEvent(evt)
					return
				}
			}
		}
	}()
}

func (s *SessionManagerService) stopCodexSessionIDWatcher(sessionID string) {
	if sessionID == "" {
		return
	}

	s.codexMu.Lock()
	cancel, exists := s.codexSessionWatchers[sessionID]
	if exists {
		delete(s.codexSessionWatchers, sessionID)
	}
	s.codexMu.Unlock()

	if exists {
		cancel()
	}
}

func (s *SessionManagerService) remapCodexSessionID(oldSessionID, newSessionID string) {
	if oldSessionID == "" || newSessionID == "" || oldSessionID == newSessionID {
		return
	}

	s.codexMu.Lock()
	defer s.codexMu.Unlock()

	codexSession := s.codexSessions[oldSessionID]
	if codexSession == nil {
		return
	}

	delete(s.codexSessions, oldSessionID)
	codexSession.SetSessionID(newSessionID)
	s.codexSessions[newSessionID] = codexSession

	if lastLine, ok := s.codexLastPTYLogLine[oldSessionID]; ok {
		if _, exists := s.codexLastPTYLogLine[newSessionID]; !exists {
			s.codexLastPTYLogLine[newSessionID] = lastLine
		}
	}
	delete(s.codexLastPTYLogLine, oldSessionID)
}

func (s *SessionManagerService) getCodexSession(sessionID string) *codexPTYSession {
	s.codexMu.Lock()
	defer s.codexMu.Unlock()
	return s.codexSessions[sessionID]
}

func (s *SessionManagerService) getCodexSessionForWorkspace(workspaceID string) *codexPTYSession {
	s.codexMu.Lock()
	defer s.codexMu.Unlock()
	for _, session := range s.codexSessions {
		if session.workspaceID == workspaceID {
			return session
		}
	}
	return nil
}

func codexRuntimeError(method string, err error) *message.Error {
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(strings.ToLower(err.Error()), "executable file not found") {
		return message.NewErrorWithData(
			message.AgentNotConfigured,
			"Codex CLI is not installed or not available on PATH",
			map[string]string{
				"agent_type": sessionManagerAgentCodex,
				"method":     method,
			},
		)
	}
	return message.NewError(message.InternalError, err.Error())
}
