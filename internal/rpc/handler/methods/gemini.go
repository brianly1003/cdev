package methods

import (
	"context"
	"errors"
)

// GeminiManager interface for Gemini CLI operations.
// This interface is designed to wrap a future Gemini CLI adapter.
//
// Gemini CLI (https://github.com/google/gemini-cli) supports:
// - Interactive prompts
// - Code generation and editing
// - Multi-turn conversations
// - Various Gemini models
type GeminiManager interface {
	// Start starts Gemini with the given prompt.
	Start(ctx context.Context, prompt string) error

	// Stop stops the running Gemini process.
	Stop(ctx context.Context) error

	// SendInput sends input to the Gemini process.
	SendInput(input string) error

	// IsRunning returns true if Gemini is running.
	IsRunning() bool

	// PID returns the process ID.
	PID() int

	// ConversationID returns the current conversation ID.
	ConversationID() string
}

// GeminiAdapter wraps a Gemini CLI manager to implement AgentManager.
type GeminiAdapter struct {
	manager GeminiManager
}

// NewGeminiAdapter creates a new Gemini adapter.
func NewGeminiAdapter(manager GeminiManager) *GeminiAdapter {
	return &GeminiAdapter{manager: manager}
}

// StartWithSession implements AgentManager.
func (a *GeminiAdapter) StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string) error {
	if a.manager == nil {
		return errors.New("gemini manager not configured")
	}
	// Gemini CLI doesn't have explicit session modes like Claude
	// The conversation ID is used for continuity
	return a.manager.Start(ctx, prompt)
}

// Stop implements AgentManager.
func (a *GeminiAdapter) Stop(ctx context.Context) error {
	if a.manager == nil {
		return errors.New("gemini manager not configured")
	}
	return a.manager.Stop(ctx)
}

// SendResponse implements AgentManager.
// Gemini uses a simpler input model without tool IDs.
func (a *GeminiAdapter) SendResponse(toolUseID, response string, isError bool) error {
	if a.manager == nil {
		return errors.New("gemini manager not configured")
	}
	return a.manager.SendInput(response)
}

// State implements AgentManager.
func (a *GeminiAdapter) State() AgentState {
	if a.manager == nil {
		return AgentStateIdle
	}
	if a.manager.IsRunning() {
		return AgentStateRunning
	}
	return AgentStateIdle
}

// PID implements AgentManager.
func (a *GeminiAdapter) PID() int {
	if a.manager == nil {
		return 0
	}
	return a.manager.PID()
}

// SessionID implements AgentManager.
func (a *GeminiAdapter) SessionID() string {
	if a.manager == nil {
		return ""
	}
	return a.manager.ConversationID()
}

// AgentType implements AgentManager.
func (a *GeminiAdapter) AgentType() string {
	return "gemini"
}

// Ensure GeminiAdapter implements AgentManager
var _ AgentManager = (*GeminiAdapter)(nil)
