package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// SessionMode represents the mode for agent sessions.
type SessionMode string

const (
	SessionModeNew      SessionMode = "new"
	SessionModeContinue SessionMode = "continue"
)

// AgentState represents the current state of an AI agent.
type AgentState string

const (
	AgentStateIdle     AgentState = "idle"
	AgentStateRunning  AgentState = "running"
	AgentStateWaiting  AgentState = "waiting"
	AgentStateStopping AgentState = "stopping"
)

// AgentManager defines the interface for managing AI CLI agents.
// This interface is CLI-agnostic and can be implemented by:
// - Claude Code adapter
// - Gemini CLI adapter
// - Codex CLI adapter
// - Any other AI coding assistant CLI
type AgentManager interface {
	// StartWithSession starts the agent with a prompt and session configuration.
	StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string) error

	// Stop stops the running agent process.
	Stop(ctx context.Context) error

	// SendResponse sends a response to an interactive prompt.
	SendResponse(toolUseID, response string, isError bool) error

	// State returns the current agent state.
	State() AgentState

	// PID returns the process ID of the running agent (0 if not running).
	PID() int

	// SessionID returns the current session ID.
	SessionID() string

	// AgentType returns the type of agent (e.g., "claude", "gemini", "codex").
	AgentType() string
}

// AgentService provides agent-related RPC methods.
// This is CLI-agnostic and works with any AI agent that implements AgentManager.
type AgentService struct {
	manager AgentManager
}

// NewAgentService creates a new agent service.
func NewAgentService(manager AgentManager) *AgentService {
	return &AgentService{manager: manager}
}

// RegisterMethods registers all agent methods with the registry.
func (s *AgentService) RegisterMethods(r *handler.Registry) {
	// agent/run
	r.RegisterWithMeta("agent/run", s.Run, handler.MethodMeta{
		Summary:     "Start an AI agent with a prompt",
		Description: "Starts the specified AI agent (Claude, Gemini, or Codex) with the given prompt. Can start a new session or continue an existing one.",
		Params: []handler.OpenRPCParam{
			{Name: "prompt", Description: "The prompt to send to the agent", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "mode", Description: "Session mode: 'new' for new session, 'continue' to continue existing", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"new", "continue"}, "default": "new"}},
			{Name: "session_id", Description: "Session ID to continue (required if mode is 'continue')", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "AgentRunResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/AgentRunResult"}},
		Errors: []string{"AgentAlreadyRunning", "AgentNotConfigured"},
	})

	// agent/stop
	r.RegisterWithMeta("agent/stop", s.Stop, handler.MethodMeta{
		Summary:     "Stop the running agent",
		Description: "Stops the currently running AI agent gracefully.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "AgentStopResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/AgentStopResult"}},
		Errors:      []string{"AgentNotRunning"},
	})

	// agent/respond
	r.RegisterWithMeta("agent/respond", s.Respond, handler.MethodMeta{
		Summary:     "Respond to agent tool use request",
		Description: "Sends a response to an agent's tool use request (e.g., permission approval).",
		Params: []handler.OpenRPCParam{
			{Name: "tool_use_id", Description: "The tool use ID from the agent's request", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "response", Description: "The response content", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "is_error", Description: "Whether this is an error response", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{Name: "AgentRespondResult", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"status": map[string]interface{}{"type": "string"}, "tool_use_id": map[string]interface{}{"type": "string"}}}},
		Errors: []string{"AgentNotRunning"},
	})

	// agent/status
	r.RegisterWithMeta("agent/status", s.Status, handler.MethodMeta{
		Summary:     "Get agent status",
		Description: "Returns the current status of the AI agent.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "AgentStatusResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/AgentStatusResult"}},
	})
}

// AgentRunParams for agent/run method.
type AgentRunParams struct {
	// Prompt is the prompt to send to the agent.
	Prompt string `json:"prompt"`

	// Mode is the session mode: "new" or "continue".
	// Default is "new".
	Mode string `json:"mode,omitempty"`

	// SessionID is required when mode is "continue".
	SessionID string `json:"session_id,omitempty"`
}

// AgentRunResult for agent/run method.
type AgentRunResult struct {
	// Status is "started" on success.
	Status string `json:"status"`

	// PID is the process ID of the agent CLI.
	PID int `json:"pid"`

	// SessionID is the agent session ID.
	SessionID string `json:"session_id,omitempty"`

	// Mode is the session mode used.
	Mode string `json:"mode"`

	// AgentType is the type of agent (e.g., "claude", "gemini", "codex").
	AgentType string `json:"agent_type"`
}

// Run starts the agent with the given prompt.
func (s *AgentService) Run(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.ErrInternalError("Agent manager not available")
	}

	var p AgentRunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.Prompt == "" {
		return nil, message.ErrInvalidParams("prompt is required")
	}

	// Validate session mode
	mode := SessionModeNew
	switch p.Mode {
	case "continue":
		mode = SessionModeContinue
		if p.SessionID == "" {
			return nil, message.ErrInvalidParams("sessionId is required when mode is 'continue'")
		}
	case "", "new":
		mode = SessionModeNew
	default:
		return nil, message.ErrInvalidParams("invalid mode: must be 'new' or 'continue'")
	}

	// Start agent (use background context so process continues even if request is cancelled)
	if err := s.manager.StartWithSession(context.Background(), p.Prompt, mode, p.SessionID); err != nil {
		return nil, message.NewError(message.AgentAlreadyRunning, err.Error())
	}

	return AgentRunResult{
		Status:    "started",
		PID:       s.manager.PID(),
		SessionID: s.manager.SessionID(),
		Mode:      string(mode),
		AgentType: s.manager.AgentType(),
	}, nil
}

// AgentStopResult for agent/stop method.
type AgentStopResult struct {
	Status string `json:"status"`
}

// Stop stops the running agent process.
func (s *AgentService) Stop(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.ErrInternalError("Agent manager not available")
	}

	if err := s.manager.Stop(ctx); err != nil {
		return nil, message.NewError(message.AgentNotRunning, err.Error())
	}

	return AgentStopResult{Status: "stopped"}, nil
}

// AgentRespondParams for agent/respond method.
type AgentRespondParams struct {
	// ToolUseID is the ID of the tool use to respond to.
	ToolUseID string `json:"tool_use_id"`

	// Response is the response content.
	Response string `json:"response"`

	// IsError indicates if the response is an error.
	IsError bool `json:"is_error,omitempty"`
}

// AgentRespondResult for agent/respond method.
type AgentRespondResult struct {
	Status    string `json:"status"`
	ToolUseID string `json:"tool_use_id"`
}

// Respond sends a response to the agent's interactive prompt.
func (s *AgentService) Respond(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.ErrInternalError("Agent manager not available")
	}

	var p AgentRespondParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.ToolUseID == "" {
		return nil, message.ErrInvalidParams("toolUseId is required")
	}

	if err := s.manager.SendResponse(p.ToolUseID, p.Response, p.IsError); err != nil {
		return nil, message.NewError(message.AgentError, err.Error())
	}

	return AgentRespondResult{
		Status:    "sent",
		ToolUseID: p.ToolUseID,
	}, nil
}

// AgentStatusResult for agent/status method.
type AgentStatusResult struct {
	// State is the current agent state.
	State string `json:"state"`

	// PID is the process ID if running.
	PID int `json:"pid,omitempty"`

	// SessionID is the current session ID.
	SessionID string `json:"session_id,omitempty"`

	// AgentType is the type of agent (e.g., "claude", "gemini", "codex").
	AgentType string `json:"agent_type"`

	// WaitingFor describes what the agent is waiting for (if any).
	WaitingFor string `json:"waiting_for,omitempty"`
}

// Status returns the current agent status.
func (s *AgentService) Status(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.ErrInternalError("Agent manager not available")
	}

	return AgentStatusResult{
		State:     string(s.manager.State()),
		PID:       s.manager.PID(),
		SessionID: s.manager.SessionID(),
		AgentType: s.manager.AgentType(),
	}, nil
}
