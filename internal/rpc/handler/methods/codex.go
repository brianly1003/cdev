package methods

import (
	"context"
	"errors"
)

// CodexManager interface for OpenAI Codex CLI operations.
// This interface is designed to wrap a future Codex CLI adapter.
//
// Codex CLI supports:
// - Code completion
// - Interactive coding sessions
// - Multi-file operations
// - OpenAI API integration
type CodexManager interface {
	// Start starts Codex with the given prompt.
	Start(ctx context.Context, prompt string) error

	// Stop stops the running Codex process.
	Stop(ctx context.Context) error

	// SendInput sends input to the Codex process.
	SendInput(input string) error

	// Approve approves a pending action.
	Approve() error

	// Deny denies a pending action.
	Deny() error

	// IsRunning returns true if Codex is running.
	IsRunning() bool

	// IsWaiting returns true if waiting for user input.
	IsWaiting() bool

	// PID returns the process ID.
	PID() int

	// SessionID returns the current session ID.
	SessionID() string
}

// CodexAdapter wraps a Codex CLI manager to implement AgentManager.
type CodexAdapter struct {
	manager CodexManager
}

// NewCodexAdapter creates a new Codex adapter.
func NewCodexAdapter(manager CodexManager) *CodexAdapter {
	return &CodexAdapter{manager: manager}
}

// StartWithSession implements AgentManager.
func (a *CodexAdapter) StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string, permissionMode string) error {
	if a.manager == nil {
		return errors.New("codex manager not configured")
	}
	// Codex doesn't support permission modes like Claude
	if permissionMode == "interactive" {
		return errors.New("codex does not support interactive (PTY) mode")
	}
	return a.manager.Start(ctx, prompt)
}

// Stop implements AgentManager.
func (a *CodexAdapter) Stop(ctx context.Context) error {
	if a.manager == nil {
		return errors.New("codex manager not configured")
	}
	return a.manager.Stop(ctx)
}

// SendResponse implements AgentManager.
// Codex has approval/denial semantics similar to Claude.
func (a *CodexAdapter) SendResponse(toolUseID, response string, isError bool) error {
	if a.manager == nil {
		return errors.New("codex manager not configured")
	}
	// Handle approval/denial if applicable
	if response == "approve" || response == "yes" {
		return a.manager.Approve()
	}
	if response == "deny" || response == "no" {
		return a.manager.Deny()
	}
	return a.manager.SendInput(response)
}

// State implements AgentManager.
func (a *CodexAdapter) State() AgentState {
	if a.manager == nil {
		return AgentStateIdle
	}
	if a.manager.IsWaiting() {
		return AgentStateWaiting
	}
	if a.manager.IsRunning() {
		return AgentStateRunning
	}
	return AgentStateIdle
}

// PID implements AgentManager.
func (a *CodexAdapter) PID() int {
	if a.manager == nil {
		return 0
	}
	return a.manager.PID()
}

// SessionID implements AgentManager.
func (a *CodexAdapter) SessionID() string {
	if a.manager == nil {
		return ""
	}
	return a.manager.SessionID()
}

// AgentType implements AgentManager.
func (a *CodexAdapter) AgentType() string {
	return "codex"
}

// SendPTYInput implements AgentManager.
// Codex doesn't support PTY mode, so this always returns an error.
func (a *CodexAdapter) SendPTYInput(input string) error {
	return errors.New("codex does not support PTY mode")
}

// IsPTYMode implements AgentManager.
// Codex doesn't support PTY mode, so this always returns false.
func (a *CodexAdapter) IsPTYMode() bool {
	return false
}

// Ensure CodexAdapter implements AgentManager
var _ AgentManager = (*CodexAdapter)(nil)
