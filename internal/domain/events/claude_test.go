package events

import (
	"encoding/json"
	"testing"
)

func TestClaudeState_Values(t *testing.T) {
	tests := []struct {
		state    ClaudeState
		expected string
	}{
		{ClaudeStateIdle, "idle"},
		{ClaudeStateRunning, "running"},
		{ClaudeStateWaiting, "waiting"},
		{ClaudeStateError, "error"},
		{ClaudeStateStopped, "stopped"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("ClaudeState = %s, want %s", tt.state, tt.expected)
			}
		})
	}
}

func TestStreamType_Values(t *testing.T) {
	if string(StreamStdout) != "stdout" {
		t.Errorf("StreamStdout = %s, want stdout", StreamStdout)
	}
	if string(StreamStderr) != "stderr" {
		t.Errorf("StreamStderr = %s, want stderr", StreamStderr)
	}
}

func TestNewClaudeLogEvent(t *testing.T) {
	event := NewClaudeLogEvent("test output line", StreamStdout)

	if event.Type() != EventTypeClaudeLog {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeLog)
	}

	payload, ok := event.Payload.(ClaudeLogPayload)
	if !ok {
		t.Fatal("Payload is not ClaudeLogPayload")
	}

	if payload.Line != "test output line" {
		t.Errorf("Line = %q, want 'test output line'", payload.Line)
	}
	if payload.Stream != StreamStdout {
		t.Errorf("Stream = %v, want %v", payload.Stream, StreamStdout)
	}
	if payload.Parsed != nil {
		t.Error("Parsed should be nil")
	}
}

func TestNewClaudeLogEvent_Stderr(t *testing.T) {
	event := NewClaudeLogEvent("error message", StreamStderr)

	payload := event.Payload.(ClaudeLogPayload)
	if payload.Stream != StreamStderr {
		t.Errorf("Stream = %v, want %v", payload.Stream, StreamStderr)
	}
}

func TestNewClaudeLogEventWithParsed(t *testing.T) {
	parsed := &ParsedClaudeMessage{
		Type:      "assistant",
		SessionID: "sess-123",
		Content: []ParsedContentBlock{
			{Type: "text", Text: "Hello world"},
		},
		StopReason:   "end_turn",
		OutputTokens: 100,
	}

	event := NewClaudeLogEventWithParsed("raw line", StreamStdout, parsed)

	if event.Type() != EventTypeClaudeLog {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeLog)
	}

	payload := event.Payload.(ClaudeLogPayload)
	if payload.Parsed == nil {
		t.Fatal("Parsed should not be nil")
	}
	if payload.Parsed.Type != "assistant" {
		t.Errorf("Parsed.Type = %q, want assistant", payload.Parsed.Type)
	}
	if payload.Parsed.SessionID != "sess-123" {
		t.Errorf("Parsed.SessionID = %q, want sess-123", payload.Parsed.SessionID)
	}
	if len(payload.Parsed.Content) != 1 {
		t.Errorf("Parsed.Content length = %d, want 1", len(payload.Parsed.Content))
	}
	if payload.Parsed.OutputTokens != 100 {
		t.Errorf("Parsed.OutputTokens = %d, want 100", payload.Parsed.OutputTokens)
	}
}

func TestNewClaudeStatusEvent_Running(t *testing.T) {
	event := NewClaudeStatusEvent(ClaudeStateRunning, "test prompt", 12345)

	if event.Type() != EventTypeClaudeStatus {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeStatus)
	}

	payload, ok := event.Payload.(ClaudeStatusPayload)
	if !ok {
		t.Fatal("Payload is not ClaudeStatusPayload")
	}

	if payload.State != ClaudeStateRunning {
		t.Errorf("State = %v, want %v", payload.State, ClaudeStateRunning)
	}
	if payload.Prompt != "test prompt" {
		t.Errorf("Prompt = %q, want 'test prompt'", payload.Prompt)
	}
	if payload.PID != 12345 {
		t.Errorf("PID = %d, want 12345", payload.PID)
	}
	if payload.StartedAt == nil {
		t.Error("StartedAt should not be nil for running state")
	}
}

func TestNewClaudeStatusEvent_Idle(t *testing.T) {
	event := NewClaudeStatusEvent(ClaudeStateIdle, "", 0)

	payload := event.Payload.(ClaudeStatusPayload)

	if payload.State != ClaudeStateIdle {
		t.Errorf("State = %v, want %v", payload.State, ClaudeStateIdle)
	}
	if payload.PID != 0 {
		t.Errorf("PID = %d, want 0", payload.PID)
	}
	if payload.StartedAt != nil {
		t.Error("StartedAt should be nil for idle state")
	}
}

func TestNewClaudeErrorEvent(t *testing.T) {
	event := NewClaudeErrorEvent("process crashed", 1)

	if event.Type() != EventTypeClaudeStatus {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeStatus)
	}

	payload := event.Payload.(ClaudeStatusPayload)

	if payload.State != ClaudeStateError {
		t.Errorf("State = %v, want %v", payload.State, ClaudeStateError)
	}
	if payload.Error != "process crashed" {
		t.Errorf("Error = %q, want 'process crashed'", payload.Error)
	}
	if payload.ExitCode == nil {
		t.Fatal("ExitCode should not be nil")
	}
	if *payload.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", *payload.ExitCode)
	}
}

func TestNewClaudeStoppedEvent(t *testing.T) {
	event := NewClaudeStoppedEvent(0)

	payload := event.Payload.(ClaudeStatusPayload)

	if payload.State != ClaudeStateStopped {
		t.Errorf("State = %v, want %v", payload.State, ClaudeStateStopped)
	}
	if payload.ExitCode == nil {
		t.Fatal("ExitCode should not be nil")
	}
	if *payload.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", *payload.ExitCode)
	}
}

func TestNewClaudeIdleEvent(t *testing.T) {
	event := NewClaudeIdleEvent()

	if event.Type() != EventTypeClaudeStatus {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeStatus)
	}

	payload := event.Payload.(ClaudeStatusPayload)

	if payload.State != ClaudeStateIdle {
		t.Errorf("State = %v, want %v", payload.State, ClaudeStateIdle)
	}
}

func TestNewClaudeWaitingEvent(t *testing.T) {
	event := NewClaudeWaitingEvent("tool-123", "Bash", `{"command": "ls -la"}`)

	if event.Type() != EventTypeClaudeWaiting {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeWaiting)
	}

	payload, ok := event.Payload.(ClaudeWaitingPayload)
	if !ok {
		t.Fatal("Payload is not ClaudeWaitingPayload")
	}

	if payload.ToolUseID != "tool-123" {
		t.Errorf("ToolUseID = %q, want tool-123", payload.ToolUseID)
	}
	if payload.ToolName != "Bash" {
		t.Errorf("ToolName = %q, want Bash", payload.ToolName)
	}
	if payload.Input != `{"command": "ls -la"}` {
		t.Errorf("Input = %q, want JSON command", payload.Input)
	}
}

func TestNewClaudePermissionEvent(t *testing.T) {
	event := NewClaudePermissionEvent(
		"tool-456",
		"Write",
		`{"path": "/etc/hosts"}`,
		"Write to system file",
	)

	if event.Type() != EventTypeClaudePermission {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudePermission)
	}

	payload, ok := event.Payload.(ClaudePermissionPayload)
	if !ok {
		t.Fatal("Payload is not ClaudePermissionPayload")
	}

	if payload.ToolUseID != "tool-456" {
		t.Errorf("ToolUseID = %q, want tool-456", payload.ToolUseID)
	}
	if payload.ToolName != "Write" {
		t.Errorf("ToolName = %q, want Write", payload.ToolName)
	}
	if payload.Description != "Write to system file" {
		t.Errorf("Description = %q, want 'Write to system file'", payload.Description)
	}
}

func TestNewClaudeSessionInfoEvent(t *testing.T) {
	event := NewClaudeSessionInfoEvent("sess-abc", "claude-3-opus", "1.0.0")

	if event.Type() != EventTypeClaudeSessionInfo {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeSessionInfo)
	}

	payload, ok := event.Payload.(ClaudeSessionInfoPayload)
	if !ok {
		t.Fatal("Payload is not ClaudeSessionInfoPayload")
	}

	if payload.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q, want sess-abc", payload.SessionID)
	}
	if payload.Model != "claude-3-opus" {
		t.Errorf("Model = %q, want claude-3-opus", payload.Model)
	}
	if payload.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", payload.Version)
	}
}

func TestNewClaudeMessageEvent(t *testing.T) {
	content := []ClaudeMessageContent{
		{Type: "text", Text: "Hello!"},
		{Type: "tool_use", ToolName: "Bash", ToolID: "t1", ToolInput: map[string]interface{}{"cmd": "ls"}},
	}

	event := NewClaudeMessageEvent("sess-xyz", "assistant", "assistant", content)

	if event.Type() != EventTypeClaudeMessage {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeMessage)
	}

	payload, ok := event.Payload.(ClaudeMessagePayload)
	if !ok {
		t.Fatal("Payload is not ClaudeMessagePayload")
	}

	if payload.SessionID != "sess-xyz" {
		t.Errorf("SessionID = %q, want sess-xyz", payload.SessionID)
	}
	if payload.Type != "assistant" {
		t.Errorf("Type = %q, want assistant", payload.Type)
	}
	if payload.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", payload.Role)
	}
	if len(payload.Content) != 2 {
		t.Errorf("Content length = %d, want 2", len(payload.Content))
	}
}

func TestNewClaudeMessageEventFull(t *testing.T) {
	payload := ClaudeMessagePayload{
		SessionID:           "sess-full",
		Type:                "assistant",
		Role:                "assistant",
		Model:               "claude-3-opus",
		StopReason:          "end_turn",
		IsContextCompaction: true,
		Content: []ClaudeMessageContent{
			{Type: "text", Text: "Compacted context"},
		},
	}

	event := NewClaudeMessageEventFull(payload)

	if event.Type() != EventTypeClaudeMessage {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeClaudeMessage)
	}

	p := event.Payload.(ClaudeMessagePayload)
	if !p.IsContextCompaction {
		t.Error("IsContextCompaction should be true")
	}
	if p.Model != "claude-3-opus" {
		t.Errorf("Model = %q, want claude-3-opus", p.Model)
	}
	if p.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want end_turn", p.StopReason)
	}
}

func TestClaudeLogPayload_JSON(t *testing.T) {
	event := NewClaudeLogEvent("output line", StreamStdout)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			Line   string `json:"line"`
			Stream string `json:"stream"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeClaudeLog) {
		t.Errorf("event = %q, want claude_log", parsed.Event)
	}
	if parsed.Payload.Line != "output line" {
		t.Errorf("line = %q, want 'output line'", parsed.Payload.Line)
	}
	if parsed.Payload.Stream != "stdout" {
		t.Errorf("stream = %q, want stdout", parsed.Payload.Stream)
	}
}

func TestClaudeStatusPayload_JSON(t *testing.T) {
	event := NewClaudeStatusEvent(ClaudeStateRunning, "test prompt", 9999)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			State     string `json:"state"`
			Prompt    string `json:"prompt"`
			PID       int    `json:"pid"`
			StartedAt string `json:"started_at"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Payload.State != "running" {
		t.Errorf("state = %q, want running", parsed.Payload.State)
	}
	if parsed.Payload.PID != 9999 {
		t.Errorf("pid = %d, want 9999", parsed.Payload.PID)
	}
	if parsed.Payload.StartedAt == "" {
		t.Error("started_at should not be empty for running state")
	}
}

func TestClaudeWaitingPayload_JSON(t *testing.T) {
	event := NewClaudeWaitingEvent("t1", "Read", `{"path": "/file.txt"}`)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			ToolUseID string `json:"tool_use_id"`
			ToolName  string `json:"tool_name"`
			Input     string `json:"input"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeClaudeWaiting) {
		t.Errorf("event = %q, want claude_waiting", parsed.Event)
	}
	if parsed.Payload.ToolUseID != "t1" {
		t.Errorf("tool_use_id = %q, want t1", parsed.Payload.ToolUseID)
	}
}

func TestClaudePermissionPayload_JSON(t *testing.T) {
	event := NewClaudePermissionEvent("t2", "Bash", `{"cmd": "rm -rf /"}`, "Dangerous command")

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			ToolUseID   string `json:"tool_use_id"`
			ToolName    string `json:"tool_name"`
			Input       string `json:"input"`
			Description string `json:"description"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeClaudePermission) {
		t.Errorf("event = %q, want claude_permission", parsed.Event)
	}
	if parsed.Payload.Description != "Dangerous command" {
		t.Errorf("description = %q, want 'Dangerous command'", parsed.Payload.Description)
	}
}

func TestClaudeMessagePayload_JSON(t *testing.T) {
	content := []ClaudeMessageContent{
		{Type: "text", Text: "Hello"},
		{Type: "tool_result", ToolUseID: "t1", Content: "result", IsError: false},
	}
	event := NewClaudeMessageEvent("s1", "assistant", "assistant", content)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			SessionID string `json:"session_id"`
			Type      string `json:"type"`
			Role      string `json:"role"`
			Content   []struct {
				Type      string `json:"type"`
				Text      string `json:"text,omitempty"`
				ToolUseID string `json:"tool_use_id,omitempty"`
				Content   string `json:"content,omitempty"`
				IsError   bool   `json:"is_error,omitempty"`
			} `json:"content"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeClaudeMessage) {
		t.Errorf("event = %q, want claude_message", parsed.Event)
	}
	if len(parsed.Payload.Content) != 2 {
		t.Errorf("content length = %d, want 2", len(parsed.Payload.Content))
	}
	if parsed.Payload.Content[0].Type != "text" {
		t.Errorf("content[0].type = %q, want text", parsed.Payload.Content[0].Type)
	}
	if parsed.Payload.Content[1].Type != "tool_result" {
		t.Errorf("content[1].type = %q, want tool_result", parsed.Payload.Content[1].Type)
	}
}

func TestParsedClaudeMessage_ContextCompaction(t *testing.T) {
	parsed := &ParsedClaudeMessage{
		Type:                "assistant",
		SessionID:           "s1",
		IsContextCompaction: true,
		Content: []ParsedContentBlock{
			{Type: "text", Text: "This session is being continued..."},
		},
	}

	event := NewClaudeLogEventWithParsed("raw", StreamStdout, parsed)
	payload := event.Payload.(ClaudeLogPayload)

	if !payload.Parsed.IsContextCompaction {
		t.Error("IsContextCompaction should be true")
	}
}

func TestParsedContentBlock_ToolUse(t *testing.T) {
	block := ParsedContentBlock{
		Type:      "tool_use",
		ToolName:  "Bash",
		ToolID:    "tool-abc",
		ToolInput: `{"command": "echo hello"}`,
	}

	if block.Type != "tool_use" {
		t.Errorf("Type = %q, want tool_use", block.Type)
	}
	if block.ToolName != "Bash" {
		t.Errorf("ToolName = %q, want Bash", block.ToolName)
	}
	if block.ToolID != "tool-abc" {
		t.Errorf("ToolID = %q, want tool-abc", block.ToolID)
	}
}

func TestClaudeMessageContent_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		content ClaudeMessageContent
	}{
		{
			name:    "text content",
			content: ClaudeMessageContent{Type: "text", Text: "hello"},
		},
		{
			name:    "thinking content",
			content: ClaudeMessageContent{Type: "thinking", Text: "pondering..."},
		},
		{
			name: "tool_use content",
			content: ClaudeMessageContent{
				Type:      "tool_use",
				ToolName:  "Read",
				ToolID:    "t1",
				ToolInput: map[string]interface{}{"path": "/file"},
			},
		},
		{
			name: "tool_result content",
			content: ClaudeMessageContent{
				Type:      "tool_result",
				ToolUseID: "t1",
				Content:   "file contents",
				IsError:   false,
			},
		},
		{
			name: "tool_result error",
			content: ClaudeMessageContent{
				Type:      "tool_result",
				ToolUseID: "t2",
				Content:   "file not found",
				IsError:   true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify content can be serialized
			event := NewClaudeMessageEvent("s1", "assistant", "assistant", []ClaudeMessageContent{tt.content})
			_, err := event.ToJSON()
			if err != nil {
				t.Errorf("ToJSON() error = %v", err)
			}
		})
	}
}
