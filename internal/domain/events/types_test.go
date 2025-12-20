package events

import (
	"encoding/json"
	"testing"
	"time"
)

func TestBaseEvent_Type(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
	}{
		{"claude_log", EventTypeClaudeLog},
		{"claude_message", EventTypeClaudeMessage},
		{"claude_status", EventTypeClaudeStatus},
		{"file_changed", EventTypeFileChanged},
		{"git_diff", EventTypeGitDiff},
		{"session_start", EventTypeSessionStart},
		{"heartbeat", EventTypeHeartbeat},
		{"error", EventTypeError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := NewEvent(tt.eventType, nil)

			if event.Type() != tt.eventType {
				t.Errorf("Type() = %v, want %v", event.Type(), tt.eventType)
			}
		})
	}
}

func TestBaseEvent_Timestamp(t *testing.T) {
	before := time.Now().UTC()
	event := NewEvent(EventTypeHeartbeat, nil)
	after := time.Now().UTC()

	ts := event.Timestamp()

	if ts.Before(before) {
		t.Errorf("Timestamp() = %v, should be >= %v", ts, before)
	}
	if ts.After(after) {
		t.Errorf("Timestamp() = %v, should be <= %v", ts, after)
	}
}

func TestBaseEvent_ToJSON(t *testing.T) {
	payload := map[string]string{"key": "value"}
	event := NewEvent(EventTypeClaudeLog, payload)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// Parse the JSON to verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	// Check event type
	if parsed["event"] != string(EventTypeClaudeLog) {
		t.Errorf("JSON event = %v, want %v", parsed["event"], EventTypeClaudeLog)
	}

	// Check timestamp exists
	if _, ok := parsed["timestamp"]; !ok {
		t.Error("JSON should contain timestamp field")
	}

	// Check payload
	payloadMap, ok := parsed["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON payload should be a map")
	}
	if payloadMap["key"] != "value" {
		t.Errorf("JSON payload.key = %v, want value", payloadMap["key"])
	}
}

func TestNewEvent(t *testing.T) {
	event := NewEvent(EventTypeFileChanged, map[string]string{"path": "/test"})

	if event == nil {
		t.Fatal("NewEvent() returned nil")
	}
	if event.EventType != EventTypeFileChanged {
		t.Errorf("EventType = %v, want %v", event.EventType, EventTypeFileChanged)
	}
	if event.Payload == nil {
		t.Error("Payload should not be nil")
	}
	if event.RequestID != "" {
		t.Errorf("RequestID = %q, want empty string", event.RequestID)
	}
}

func TestNewEventWithRequestID(t *testing.T) {
	requestID := "req-123"
	event := NewEventWithRequestID(EventTypeStatusResponse, nil, requestID)

	if event == nil {
		t.Fatal("NewEventWithRequestID() returned nil")
	}
	if event.RequestID != requestID {
		t.Errorf("RequestID = %q, want %q", event.RequestID, requestID)
	}
}

func TestEventTypes_Constants(t *testing.T) {
	// Verify all event types are unique
	types := []EventType{
		EventTypeClaudeLog,
		EventTypeClaudeMessage,
		EventTypeClaudeStatus,
		EventTypeClaudeWaiting,
		EventTypeClaudePermission,
		EventTypeClaudeSessionInfo,
		EventTypeFileChanged,
		EventTypeGitDiff,
		EventTypeGitStatusChanged,
		EventTypeGitOperationComplete,
		EventTypeSessionStart,
		EventTypeSessionEnd,
		EventTypeStatusResponse,
		EventTypeFileContent,
		EventTypeError,
		EventTypeHeartbeat,
	}

	seen := make(map[EventType]bool)
	for _, t := range types {
		if seen[t] {
			panic("duplicate event type: " + string(t))
		}
		seen[t] = true
	}
}

func TestGitStatusChangedPayload_JSON(t *testing.T) {
	payload := GitStatusChangedPayload{
		Branch:         "main",
		Ahead:          2,
		Behind:         1,
		StagedCount:    3,
		UnstagedCount:  5,
		UntrackedCount: 2,
		HasConflicts:   false,
		ChangedFiles:   []string{"file1.go", "file2.go"},
	}

	event := NewEvent(EventTypeGitStatusChanged, payload)
	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// Parse and verify
	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			Branch         string   `json:"branch"`
			Ahead          int      `json:"ahead"`
			Behind         int      `json:"behind"`
			StagedCount    int      `json:"staged_count"`
			UnstagedCount  int      `json:"unstaged_count"`
			UntrackedCount int      `json:"untracked_count"`
			HasConflicts   bool     `json:"has_conflicts"`
			ChangedFiles   []string `json:"changed_files"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Payload.Branch != "main" {
		t.Errorf("branch = %v, want main", parsed.Payload.Branch)
	}
	if parsed.Payload.Ahead != 2 {
		t.Errorf("ahead = %v, want 2", parsed.Payload.Ahead)
	}
	if len(parsed.Payload.ChangedFiles) != 2 {
		t.Errorf("changed_files length = %v, want 2", len(parsed.Payload.ChangedFiles))
	}
}

func TestGitOperationCompletedPayload_JSON(t *testing.T) {
	payload := GitOperationCompletedPayload{
		Operation:     "commit",
		Success:       true,
		Message:       "Committed successfully",
		SHA:           "abc1234",
		FilesAffected: 5,
	}

	event := NewEvent(EventTypeGitOperationComplete, payload)
	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// Parse and verify key fields
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	payloadMap := parsed["payload"].(map[string]interface{})
	if payloadMap["operation"] != "commit" {
		t.Errorf("operation = %v, want commit", payloadMap["operation"])
	}
	if payloadMap["success"] != true {
		t.Errorf("success = %v, want true", payloadMap["success"])
	}
	if payloadMap["sha"] != "abc1234" {
		t.Errorf("sha = %v, want abc1234", payloadMap["sha"])
	}
}

// Benchmark tests
func BenchmarkNewEvent(b *testing.B) {
	payload := map[string]string{"key": "value"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewEvent(EventTypeClaudeLog, payload)
	}
}

func BenchmarkEvent_ToJSON(b *testing.B) {
	event := NewEvent(EventTypeClaudeLog, map[string]string{"key": "value"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		event.ToJSON()
	}
}
