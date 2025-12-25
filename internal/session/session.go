// Package session provides multi-workspace session management for Claude CLI instances.
package session

import (
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/watcher"
)

// Status represents the current state of a session.
type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusRunning  Status = "running"
	StatusStopping Status = "stopping"
	StatusError    Status = "error"
)

// Session represents an active Claude CLI instance for a workspace.
type Session struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Status      Status    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	LastActive  time.Time `json:"last_active"`
	Error       string    `json:"error,omitempty"`

	// Runtime components (not serialized)
	claudeManager *claude.Manager
	fileWatcher   *watcher.Watcher
	gitTracker    *git.Tracker

	mu sync.RWMutex
}

// NewSession creates a new session for a workspace.
func NewSession(id, workspaceID string) *Session {
	now := time.Now().UTC()
	return &Session{
		ID:          id,
		WorkspaceID: workspaceID,
		Status:      StatusStopped,
		StartedAt:   now,
		LastActive:  now,
	}
}

// GetStatus returns the current session status.
func (s *Session) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// SetStatus updates the session status.
func (s *Session) SetStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	s.LastActive = time.Now().UTC()
}

// SetError sets an error status with message.
func (s *Session) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusError
	if err != nil {
		s.Error = err.Error()
	}
	s.LastActive = time.Now().UTC()
}

// UpdateLastActive updates the last active timestamp.
func (s *Session) UpdateLastActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActive = time.Now().UTC()
}

// GetLastActive returns the last active timestamp.
func (s *Session) GetLastActive() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.LastActive
}

// ClaudeManager returns the Claude manager for this session.
func (s *Session) ClaudeManager() *claude.Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.claudeManager
}

// SetClaudeManager sets the Claude manager for this session.
func (s *Session) SetClaudeManager(m *claude.Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claudeManager = m
}

// FileWatcher returns the file watcher for this session.
func (s *Session) FileWatcher() *watcher.Watcher {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fileWatcher
}

// SetFileWatcher sets the file watcher for this session.
func (s *Session) SetFileWatcher(w *watcher.Watcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileWatcher = w
}

// GitTracker returns the git tracker for this session.
func (s *Session) GitTracker() *git.Tracker {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.gitTracker
}

// SetGitTracker sets the git tracker for this session.
func (s *Session) SetGitTracker(t *git.Tracker) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gitTracker = t
}

// ToInfo returns a serializable session info.
func (s *Session) ToInfo() *Info {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &Info{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Status:      s.Status,
		StartedAt:   s.StartedAt,
		LastActive:  s.LastActive,
		Error:       s.Error,
	}
}

// Info is a serializable representation of a session.
type Info struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Status      Status    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	LastActive  time.Time `json:"last_active"`
	Error       string    `json:"error,omitempty"`
	// Viewers is the list of client IDs currently viewing this session.
	// Populated by workspace/list when session focus info is available.
	Viewers []string `json:"viewers,omitempty"`
}

// RuntimeState contains the current runtime state of a session for reconnection sync.
type RuntimeState struct {
	// Session info
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Status      Status    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	LastActive  time.Time `json:"last_active"`
	Error       string    `json:"error,omitempty"`

	// Claude state
	ClaudeState       string `json:"claude_state"`        // idle, running, error, waiting
	ClaudeSessionID   string `json:"claude_session_id"`   // For conversation continuity
	IsRunning         bool   `json:"is_running"`          // Is Claude process running
	WaitingForInput   bool   `json:"waiting_for_input"`   // Is Claude waiting for user input
	PendingToolUseID  string `json:"pending_tool_use_id,omitempty"`
	PendingToolName   string `json:"pending_tool_name,omitempty"`
}

// ToRuntimeState returns the current runtime state for reconnection sync.
func (s *Session) ToRuntimeState() *RuntimeState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state := &RuntimeState{
		ID:          s.ID,
		WorkspaceID: s.WorkspaceID,
		Status:      s.Status,
		StartedAt:   s.StartedAt,
		LastActive:  s.LastActive,
		Error:       s.Error,
	}

	// Get Claude manager state if available
	if s.claudeManager != nil {
		state.ClaudeState = string(s.claudeManager.State())
		state.ClaudeSessionID = s.claudeManager.ClaudeSessionID()
		state.IsRunning = s.claudeManager.IsRunning()
		state.WaitingForInput = s.claudeManager.IsWaitingForInput()

		toolID, toolName := s.claudeManager.GetPendingToolUse()
		state.PendingToolUseID = toolID
		state.PendingToolName = toolName
	}

	return state
}
