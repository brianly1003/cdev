package methods

import (
	"context"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/domain/events"
)

// ClaudeAdapter wraps the Claude CLI manager to implement AgentManager.
type ClaudeAdapter struct {
	manager *claude.Manager
}

// NewClaudeAdapter creates a new Claude adapter.
func NewClaudeAdapter(manager *claude.Manager) *ClaudeAdapter {
	return &ClaudeAdapter{manager: manager}
}

// StartWithSession implements AgentManager.
func (a *ClaudeAdapter) StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string, permissionMode string) error {
	// Convert SessionMode to Claude's session mode
	var claudeMode claude.SessionMode
	switch mode {
	case SessionModeContinue:
		claudeMode = claude.SessionModeContinue
	default:
		claudeMode = claude.SessionModeNew
	}
	return a.manager.StartWithSession(ctx, prompt, claudeMode, sessionID, permissionMode)
}

// Stop implements AgentManager.
func (a *ClaudeAdapter) Stop(ctx context.Context) error {
	return a.manager.Stop(ctx)
}

// SendResponse implements AgentManager.
func (a *ClaudeAdapter) SendResponse(toolUseID, response string, isError bool) error {
	return a.manager.SendResponse(toolUseID, response, isError)
}

// State implements AgentManager.
func (a *ClaudeAdapter) State() AgentState {
	state := a.manager.State()
	switch state {
	case events.ClaudeStateRunning:
		return AgentStateRunning
	case events.ClaudeStateWaiting:
		return AgentStateWaiting
	case events.ClaudeStateStopped, events.ClaudeStateError:
		return AgentStateIdle
	default:
		return AgentStateIdle
	}
}

// PID implements AgentManager.
func (a *ClaudeAdapter) PID() int {
	return a.manager.PID()
}

// SessionID implements AgentManager.
func (a *ClaudeAdapter) SessionID() string {
	return a.manager.ClaudeSessionID()
}

// AgentType implements AgentManager.
func (a *ClaudeAdapter) AgentType() string {
	return "claude"
}

// SendPTYInput implements AgentManager.
func (a *ClaudeAdapter) SendPTYInput(input string) error {
	return a.manager.SendPTYInput(input)
}

// IsPTYMode implements AgentManager.
func (a *ClaudeAdapter) IsPTYMode() bool {
	return a.manager.IsPTYMode()
}

// Ensure ClaudeAdapter implements AgentManager
var _ AgentManager = (*ClaudeAdapter)(nil)
