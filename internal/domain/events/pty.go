// Package events defines domain events for the cdev system.
package events

// PTY-specific event types
const (
	EventTypePTYOutput             EventType = "pty_output"
	EventTypePTYPermission         EventType = "pty_permission"
	EventTypePTYPermissionResolved EventType = "pty_permission_resolved" // Permission was responded to by a device
	EventTypePTYState              EventType = "pty_state"
	EventTypePTYSpinner            EventType = "pty_spinner"
)

// PTYOutputPayload represents processed terminal output.
type PTYOutputPayload struct {
	CleanText string `json:"clean_text"` // ANSI codes stripped
	RawText   string `json:"raw_text"`   // Original with ANSI codes
	State     string `json:"state"`      // "idle", "thinking", "permission", etc.
	SessionID string `json:"session_id,omitempty"`
}

// PTYPermissionPayload represents a permission prompt from PTY mode.
type PTYPermissionPayload struct {
	Type        string            `json:"type"`        // "write_file", "edit_file", "bash_command"
	Target      string            `json:"target"`      // filename or command
	Description string            `json:"description"` // human-readable description
	Preview     string            `json:"preview"`     // content preview
	Options     []PTYPromptOption `json:"options"`     // available choices
	SessionID   string            `json:"session_id,omitempty"`
}

// PTYPromptOption represents a choice in a permission prompt.
type PTYPromptOption struct {
	Key         string `json:"key"`                   // "1", "2", "n"
	Label       string `json:"label"`                 // "Yes", "Yes to all", "No"
	Description string `json:"description,omitempty"` // optional extra info
	Selected    bool   `json:"selected"`              // true if this option has the cursor (❯)
}

// PTYStatePayload represents PTY state change.
type PTYStatePayload struct {
	State           string `json:"state"`
	WaitingForInput bool   `json:"waiting_for_input"`
	PromptType      string `json:"prompt_type,omitempty"` // "write_file", "bash_command", etc.
	SessionID       string `json:"session_id,omitempty"`
}

// PTYSpinnerPayload represents spinner animation state (e.g., "✶ Vibing…").
type PTYSpinnerPayload struct {
	Text      string `json:"text"`                 // Full spinner text (e.g., "✶ Vibing…")
	Symbol    string `json:"symbol"`               // Just the spinner symbol (e.g., "✶")
	Message   string `json:"message"`              // Just the message (e.g., "Vibing…")
	SessionID string `json:"session_id,omitempty"`
}

// NewPTYOutputEvent creates a new PTY output event.
func NewPTYOutputEvent(cleanText, rawText, state string) *BaseEvent {
	return NewEvent(EventTypePTYOutput, PTYOutputPayload{
		CleanText: cleanText,
		RawText:   rawText,
		State:     state,
	})
}

// NewPTYOutputEventWithSession creates a new PTY output event with session ID.
func NewPTYOutputEventWithSession(cleanText, rawText, state, sessionID string) *BaseEvent {
	return NewEvent(EventTypePTYOutput, PTYOutputPayload{
		CleanText: cleanText,
		RawText:   rawText,
		State:     state,
		SessionID: sessionID,
	})
}

// NewPTYPermissionEvent creates a new PTY permission event.
func NewPTYPermissionEvent(permType, target, description, preview string, options []PTYPromptOption) *BaseEvent {
	return NewEvent(EventTypePTYPermission, PTYPermissionPayload{
		Type:        permType,
		Target:      target,
		Description: description,
		Preview:     preview,
		Options:     options,
	})
}

// NewPTYPermissionEventWithSession creates a new PTY permission event with session ID.
func NewPTYPermissionEventWithSession(permType, target, description, preview, sessionID string, options []PTYPromptOption) *BaseEvent {
	return NewEvent(EventTypePTYPermission, PTYPermissionPayload{
		Type:        permType,
		Target:      target,
		Description: description,
		Preview:     preview,
		Options:     options,
		SessionID:   sessionID,
	})
}

// NewPTYStateEvent creates a new PTY state event.
func NewPTYStateEvent(state string, waitingForInput bool, promptType string) *BaseEvent {
	return NewEvent(EventTypePTYState, PTYStatePayload{
		State:           state,
		WaitingForInput: waitingForInput,
		PromptType:      promptType,
	})
}

// NewPTYStateEventWithSession creates a new PTY state event with session ID.
func NewPTYStateEventWithSession(state string, waitingForInput bool, promptType, sessionID string) *BaseEvent {
	return NewEvent(EventTypePTYState, PTYStatePayload{
		State:           state,
		WaitingForInput: waitingForInput,
		PromptType:      promptType,
		SessionID:       sessionID,
	})
}

// NewPTYSpinnerEventWithSession creates a new PTY spinner event with session ID.
func NewPTYSpinnerEventWithSession(text, symbol, message, sessionID string) *BaseEvent {
	return NewEvent(EventTypePTYSpinner, PTYSpinnerPayload{
		Text:      text,
		Symbol:    symbol,
		Message:   message,
		SessionID: sessionID,
	})
}

// PTYPermissionResolvedPayload represents that a permission prompt was responded to.
// This is broadcast to all devices so they can dismiss their permission popups.
type PTYPermissionResolvedPayload struct {
	SessionID   string `json:"session_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	ResolvedBy  string `json:"resolved_by,omitempty"` // Client ID that resolved the permission
	Input       string `json:"input,omitempty"`       // The input that was sent (e.g., "1", "enter")
}

// NewPTYPermissionResolvedEvent creates a new pty_permission_resolved event.
func NewPTYPermissionResolvedEvent(sessionID, workspaceID, resolvedBy, input string) *BaseEvent {
	return NewEventWithContext(EventTypePTYPermissionResolved, PTYPermissionResolvedPayload{
		SessionID:   sessionID,
		WorkspaceID: workspaceID,
		ResolvedBy:  resolvedBy,
		Input:       input,
	}, workspaceID, sessionID)
}
