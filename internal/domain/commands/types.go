// Package commands defines all command types used in cdev.
package commands

import "encoding/json"

// CommandType represents the type of command.
type CommandType string

const (
	CommandRunClaude       CommandType = "run_claude"
	CommandStopClaude      CommandType = "stop_claude"
	CommandRespondToClaude CommandType = "respond_to_claude"
	CommandGetStatus       CommandType = "get_status"
	CommandGetFile         CommandType = "get_file"
	CommandWatchSession    CommandType = "watch_session"
	CommandUnwatchSession  CommandType = "unwatch_session"
)

// Command represents a command received from a client.
type Command struct {
	Command   CommandType     `json:"command"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// RunClaudePayload is the payload for run_claude command.
type RunClaudePayload struct {
	Prompt    string `json:"prompt"`
	Mode      string `json:"mode,omitempty"`       // "new" or "continue"
	SessionID string `json:"session_id,omitempty"` // Required when mode is "continue"
}

// GetFilePayload is the payload for get_file command.
type GetFilePayload struct {
	Path string `json:"path"`
}

// RespondToClaudePayload is the payload for respond_to_claude command.
type RespondToClaudePayload struct {
	ToolUseID string `json:"tool_use_id"`
	Response  string `json:"response"`
	IsError   bool   `json:"is_error,omitempty"`
}

// WatchSessionPayload is the payload for watch_session command.
// Used to start watching a session file for real-time updates.
type WatchSessionPayload struct {
	SessionID string `json:"session_id"`
}

// ParseCommand parses a JSON message into a Command.
func ParseCommand(data []byte) (*Command, error) {
	var cmd Command
	if err := json.Unmarshal(data, &cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}

// ParseRunClaudePayload parses the payload for run_claude command.
func (c *Command) ParseRunClaudePayload() (*RunClaudePayload, error) {
	var payload RunClaudePayload
	if err := json.Unmarshal(c.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseGetFilePayload parses the payload for get_file command.
func (c *Command) ParseGetFilePayload() (*GetFilePayload, error) {
	var payload GetFilePayload
	if err := json.Unmarshal(c.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseRespondToClaudePayload parses the payload for respond_to_claude command.
func (c *Command) ParseRespondToClaudePayload() (*RespondToClaudePayload, error) {
	var payload RespondToClaudePayload
	if err := json.Unmarshal(c.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ParseWatchSessionPayload parses the payload for watch_session command.
func (c *Command) ParseWatchSessionPayload() (*WatchSessionPayload, error) {
	var payload WatchSessionPayload
	if err := json.Unmarshal(c.Payload, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}
