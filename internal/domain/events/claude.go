package events

import "time"

// ClaudeState represents the current state of Claude CLI.
type ClaudeState string

const (
	ClaudeStateIdle    ClaudeState = "idle"
	ClaudeStateRunning ClaudeState = "running"
	ClaudeStateWaiting ClaudeState = "waiting"
	ClaudeStateError   ClaudeState = "error"
	ClaudeStateStopped ClaudeState = "stopped"
)

// StreamType represents the output stream type.
type StreamType string

const (
	StreamStdout StreamType = "stdout"
	StreamStderr StreamType = "stderr"
)

// ClaudeLogPayload is the payload for claude_log events.
// Enhanced to include parsed content for rich UI rendering.
type ClaudeLogPayload struct {
	Line   string     `json:"line"`
	Stream StreamType `json:"stream"`
	// Parsed is the structured content extracted from Claude CLI stream-json output.
	// Only populated for valid JSON lines from stdout.
	Parsed *ParsedClaudeMessage `json:"parsed,omitempty"`
}

// ParsedClaudeMessage represents a parsed message from Claude CLI stream-json output.
type ParsedClaudeMessage struct {
	// Type is the message type: "assistant", "user", "result", "system", "init"
	Type string `json:"type"`
	// SessionID is the Claude session ID (present in most messages)
	SessionID string `json:"session_id,omitempty"`
	// Content is the array of content blocks in the message
	Content []ParsedContentBlock `json:"content,omitempty"`
	// StopReason indicates why Claude stopped (e.g., "end_turn", "tool_use")
	// Empty string means still generating
	StopReason string `json:"stop_reason,omitempty"`
	// CostUSD is the cost in USD for result messages
	CostUSD float64 `json:"cost_usd,omitempty"`
	// DurationMS is the duration in milliseconds for result messages
	DurationMS int64 `json:"duration_ms,omitempty"`
	// OutputTokens is the number of tokens generated so far
	// Use this to show "↓ 9.2k tokens" in UI
	OutputTokens int `json:"output_tokens,omitempty"`
	// IsThinking indicates Claude is in thinking/ideating mode
	// True when content contains <thinking> tags or type="thinking" blocks
	IsThinking bool `json:"is_thinking,omitempty"`
	// IsContextCompaction is true when this is an auto-generated message
	// created by Claude Code when the context window was maxed out.
	// Content starts with "This session is being continued from a previous conversation"
	IsContextCompaction bool `json:"is_context_compaction,omitempty"`
}

// ParsedContentBlock represents a single content block from Claude's output.
type ParsedContentBlock struct {
	// Type is the content type: "text", "thinking", "tool_use", "tool_result"
	Type string `json:"type"`
	// Text is the text content (for "text" and "thinking" types)
	Text string `json:"text,omitempty"`
	// ToolName is the tool being called (for "tool_use" type)
	ToolName string `json:"tool_name,omitempty"`
	// ToolID is the unique ID for this tool call (for "tool_use" type)
	ToolID string `json:"tool_id,omitempty"`
	// ToolInput is the raw JSON input for the tool (for "tool_use" type)
	ToolInput string `json:"tool_input,omitempty"`
}

// ClaudeStatusPayload is the payload for claude_status events.
type ClaudeStatusPayload struct {
	State     ClaudeState `json:"state"`
	Prompt    string      `json:"prompt,omitempty"`
	PID       int         `json:"pid,omitempty"`
	StartedAt *time.Time  `json:"started_at,omitempty"`
	Error     string      `json:"error,omitempty"`
	ExitCode  *int        `json:"exit_code,omitempty"`
}

// NewClaudeLogEvent creates a new claude_log event.
func NewClaudeLogEvent(line string, stream StreamType) *BaseEvent {
	return NewEvent(EventTypeClaudeLog, ClaudeLogPayload{
		Line:   line,
		Stream: stream,
	})
}

// NewClaudeLogEventWithParsed creates a new claude_log event with parsed content.
func NewClaudeLogEventWithParsed(line string, stream StreamType, parsed *ParsedClaudeMessage) *BaseEvent {
	return NewEvent(EventTypeClaudeLog, ClaudeLogPayload{
		Line:   line,
		Stream: stream,
		Parsed: parsed,
	})
}

// NewClaudeStatusEvent creates a new claude_status event.
func NewClaudeStatusEvent(state ClaudeState, prompt string, pid int) *BaseEvent {
	now := time.Now().UTC()
	payload := ClaudeStatusPayload{
		State:  state,
		Prompt: prompt,
	}
	if state == ClaudeStateRunning {
		payload.PID = pid
		payload.StartedAt = &now
	}
	return NewEvent(EventTypeClaudeStatus, payload)
}

// NewClaudeErrorEvent creates a new claude_status event with error state.
func NewClaudeErrorEvent(err string, exitCode int) *BaseEvent {
	return NewEvent(EventTypeClaudeStatus, ClaudeStatusPayload{
		State:    ClaudeStateError,
		Error:    err,
		ExitCode: &exitCode,
	})
}

// NewClaudeStoppedEvent creates a new claude_status event with stopped state.
func NewClaudeStoppedEvent(exitCode int) *BaseEvent {
	return NewEvent(EventTypeClaudeStatus, ClaudeStatusPayload{
		State:    ClaudeStateStopped,
		ExitCode: &exitCode,
	})
}

// NewClaudeIdleEvent creates a new claude_status event with idle state.
func NewClaudeIdleEvent() *BaseEvent {
	return NewEvent(EventTypeClaudeStatus, ClaudeStatusPayload{
		State: ClaudeStateIdle,
	})
}

// ClaudeWaitingPayload is the payload for claude_waiting events.
type ClaudeWaitingPayload struct {
	ToolUseID string `json:"tool_use_id"`
	ToolName  string `json:"tool_name"`
	Input     string `json:"input"`
}

// NewClaudeWaitingEvent creates a new claude_waiting event.
func NewClaudeWaitingEvent(toolUseID, toolName, input string) *BaseEvent {
	return NewEvent(EventTypeClaudeWaiting, ClaudeWaitingPayload{
		ToolUseID: toolUseID,
		ToolName:  toolName,
		Input:     input,
	})
}

// ClaudePermissionPayload is the payload for claude_permission events.
type ClaudePermissionPayload struct {
	ToolUseID   string `json:"tool_use_id"`
	ToolName    string `json:"tool_name"`
	Input       string `json:"input"`
	Description string `json:"description"`
}

// NewClaudePermissionEvent creates a new claude_permission event.
func NewClaudePermissionEvent(toolUseID, toolName, input, description string) *BaseEvent {
	return NewEvent(EventTypeClaudePermission, ClaudePermissionPayload{
		ToolUseID:   toolUseID,
		ToolName:    toolName,
		Input:       input,
		Description: description,
	})
}

// ClaudeSessionInfoPayload is the payload for claude_session_info events.
type ClaudeSessionInfoPayload struct {
	SessionID string `json:"session_id"`
	Model     string `json:"model,omitempty"`
	Version   string `json:"version,omitempty"`
}

// NewClaudeSessionInfoEvent creates a new claude_session_info event.
func NewClaudeSessionInfoEvent(sessionID, model, version string) *BaseEvent {
	return NewEvent(EventTypeClaudeSessionInfo, ClaudeSessionInfoPayload{
		SessionID: sessionID,
		Model:     model,
		Version:   version,
	})
}

// ClaudeMessagePayload is the payload for claude_message events.
// This provides real-time structured messages for rich UI rendering.
type ClaudeMessagePayload struct {
	// SessionID is the Claude session ID
	SessionID string `json:"session_id"`
	// Type is the message type: "assistant", "user", "result"
	Type string `json:"type"`
	// Role is the message role: "assistant" or "user"
	Role string `json:"role,omitempty"`
	// Content contains the message content blocks
	Content []ClaudeMessageContent `json:"content,omitempty"`
	// Model is the model used (for assistant messages)
	Model string `json:"model,omitempty"`
	// StopReason indicates why the message ended
	StopReason string `json:"stop_reason,omitempty"`
	// IsContextCompaction is true when this is an auto-generated message
	// created by Claude Code when the context window was maxed out and
	// the conversation was compacted/summarized.
	// iOS should display this differently (e.g., as a system notice).
	IsContextCompaction bool `json:"is_context_compaction,omitempty"`
}

// ClaudeMessageContent represents a content block in a claude_message.
type ClaudeMessageContent struct {
	// Type is the content type: "text", "thinking", "tool_use", "tool_result"
	Type string `json:"type"`
	// Text is the text content (for "text" and "thinking" types)
	Text string `json:"text,omitempty"`
	// ToolName is the tool being called (for "tool_use" type)
	ToolName string `json:"tool_name,omitempty"`
	// ToolID is the unique ID for this tool call (for "tool_use" type)
	ToolID string `json:"tool_id,omitempty"`
	// ToolInput is the parsed tool input (for "tool_use" type)
	ToolInput map[string]interface{} `json:"tool_input,omitempty"`
	// ToolUseID references the tool call (for "tool_result" type)
	ToolUseID string `json:"tool_use_id,omitempty"`
	// Content is the result content (for "tool_result" type)
	Content string `json:"content,omitempty"`
	// IsError indicates if the tool result is an error
	IsError bool `json:"is_error,omitempty"`
}

// NewClaudeMessageEvent creates a new claude_message event.
func NewClaudeMessageEvent(sessionID, msgType, role string, content []ClaudeMessageContent) *BaseEvent {
	return NewEvent(EventTypeClaudeMessage, ClaudeMessagePayload{
		SessionID: sessionID,
		Type:      msgType,
		Role:      role,
		Content:   content,
	})
}

// NewClaudeMessageEventFull creates a new claude_message event with all fields.
func NewClaudeMessageEventFull(payload ClaudeMessagePayload) *BaseEvent {
	return NewEvent(EventTypeClaudeMessage, payload)
}
