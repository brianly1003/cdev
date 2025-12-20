package events

import (
	"encoding/json"
	"testing"
)

func TestNewSessionStartEvent(t *testing.T) {
	event := NewSessionStartEvent("sess-123", "/path/to/repo", "myrepo", "1.0.0")

	if event.Type() != EventTypeSessionStart {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeSessionStart)
	}

	payload, ok := event.Payload.(SessionStartPayload)
	if !ok {
		t.Fatal("Payload is not SessionStartPayload")
	}

	if payload.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want sess-123", payload.SessionID)
	}
	if payload.RepoPath != "/path/to/repo" {
		t.Errorf("RepoPath = %q, want /path/to/repo", payload.RepoPath)
	}
	if payload.RepoName != "myrepo" {
		t.Errorf("RepoName = %q, want myrepo", payload.RepoName)
	}
	if payload.AgentVersion != "1.0.0" {
		t.Errorf("AgentVersion = %q, want 1.0.0", payload.AgentVersion)
	}
}

func TestNewSessionEndEvent(t *testing.T) {
	event := NewSessionEndEvent("sess-123", "user requested")

	if event.Type() != EventTypeSessionEnd {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeSessionEnd)
	}

	payload, ok := event.Payload.(SessionEndPayload)
	if !ok {
		t.Fatal("Payload is not SessionEndPayload")
	}

	if payload.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want sess-123", payload.SessionID)
	}
	if payload.Reason != "user requested" {
		t.Errorf("Reason = %q, want 'user requested'", payload.Reason)
	}
}

func TestNewStatusResponseEvent(t *testing.T) {
	requestID := "req-status-1"
	payload := StatusResponsePayload{
		ClaudeState:      "idle",
		ConnectedClients: 3,
		RepoPath:         "/repo",
		RepoName:         "test-repo",
		UptimeSeconds:    3600,
		AgentVersion:     "2.0.0",
		WatcherEnabled:   true,
		GitEnabled:       true,
	}

	event := NewStatusResponseEvent(payload, requestID)

	if event.Type() != EventTypeStatusResponse {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeStatusResponse)
	}
	if event.RequestID != requestID {
		t.Errorf("RequestID = %q, want %q", event.RequestID, requestID)
	}

	p, ok := event.Payload.(StatusResponsePayload)
	if !ok {
		t.Fatal("Payload is not StatusResponsePayload")
	}

	if p.ConnectedClients != 3 {
		t.Errorf("ConnectedClients = %d, want 3", p.ConnectedClients)
	}
	if p.UptimeSeconds != 3600 {
		t.Errorf("UptimeSeconds = %d, want 3600", p.UptimeSeconds)
	}
}

func TestNewErrorEvent(t *testing.T) {
	details := map[string]interface{}{"field": "prompt", "max": 10000}
	event := NewErrorEvent("VALIDATION_ERROR", "prompt too long", "req-1", details)

	if event.Type() != EventTypeError {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeError)
	}

	payload, ok := event.Payload.(ErrorPayload)
	if !ok {
		t.Fatal("Payload is not ErrorPayload")
	}

	if payload.Code != "VALIDATION_ERROR" {
		t.Errorf("Code = %q, want VALIDATION_ERROR", payload.Code)
	}
	if payload.Message != "prompt too long" {
		t.Errorf("Message = %q, want 'prompt too long'", payload.Message)
	}
	if payload.Details["field"] != "prompt" {
		t.Errorf("Details[field] = %v, want prompt", payload.Details["field"])
	}
}

func TestNewHeartbeatEvent(t *testing.T) {
	event := NewHeartbeatEvent(42, "running", 7200)

	if event.Type() != EventTypeHeartbeat {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeHeartbeat)
	}

	payload, ok := event.Payload.(HeartbeatPayload)
	if !ok {
		t.Fatal("Payload is not HeartbeatPayload")
	}

	if payload.Sequence != 42 {
		t.Errorf("Sequence = %d, want 42", payload.Sequence)
	}
	if payload.ClaudeStatus != "running" {
		t.Errorf("ClaudeStatus = %q, want running", payload.ClaudeStatus)
	}
	if payload.Uptime != 7200 {
		t.Errorf("Uptime = %d, want 7200", payload.Uptime)
	}
	if payload.ServerTime == "" {
		t.Error("ServerTime should not be empty")
	}
}

func TestSessionStartPayload_JSON(t *testing.T) {
	event := NewSessionStartEvent("s-1", "/repo", "myrepo", "v1.0")

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			SessionID    string `json:"session_id"`
			RepoPath     string `json:"repo_path"`
			RepoName     string `json:"repo_name"`
			AgentVersion string `json:"agent_version"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeSessionStart) {
		t.Errorf("event = %q, want session_start", parsed.Event)
	}
	if parsed.Payload.SessionID != "s-1" {
		t.Errorf("session_id = %q, want s-1", parsed.Payload.SessionID)
	}
}

func TestErrorPayload_JSON(t *testing.T) {
	event := NewErrorEvent("INTERNAL_ERROR", "something went wrong", "req-x", nil)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event     string `json:"event"`
		RequestID string `json:"request_id"`
		Payload   struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeError) {
		t.Errorf("event = %q, want error", parsed.Event)
	}
	if parsed.RequestID != "req-x" {
		t.Errorf("request_id = %q, want req-x", parsed.RequestID)
	}
	if parsed.Payload.Code != "INTERNAL_ERROR" {
		t.Errorf("payload.code = %q, want INTERNAL_ERROR", parsed.Payload.Code)
	}
}

func TestHeartbeatPayload_JSON(t *testing.T) {
	event := NewHeartbeatEvent(100, "idle", 3600)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			ServerTime   string `json:"server_time"`
			Sequence     int64  `json:"sequence"`
			ClaudeStatus string `json:"claude_status"`
			Uptime       int64  `json:"uptime_seconds"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeHeartbeat) {
		t.Errorf("event = %q, want heartbeat", parsed.Event)
	}
	if parsed.Payload.Sequence != 100 {
		t.Errorf("sequence = %d, want 100", parsed.Payload.Sequence)
	}
	if parsed.Payload.ServerTime == "" {
		t.Error("server_time should not be empty")
	}
}

func TestStatusResponsePayload_JSON(t *testing.T) {
	payload := StatusResponsePayload{
		ClaudeState:      "running",
		ConnectedClients: 2,
		RepoPath:         "/home/user/project",
		RepoName:         "project",
		UptimeSeconds:    1800,
		AgentVersion:     "1.5.0",
		WatcherEnabled:   true,
		GitEnabled:       true,
	}

	event := NewStatusResponseEvent(payload, "req-status")

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event     string `json:"event"`
		RequestID string `json:"request_id"`
		Payload   struct {
			ClaudeState      string `json:"claude_state"`
			ConnectedClients int    `json:"connected_clients"`
			RepoPath         string `json:"repo_path"`
			RepoName         string `json:"repo_name"`
			UptimeSeconds    int64  `json:"uptime_seconds"`
			AgentVersion     string `json:"agent_version"`
			WatcherEnabled   bool   `json:"watcher_enabled"`
			GitEnabled       bool   `json:"git_enabled"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Payload.ClaudeState != "running" {
		t.Errorf("claude_state = %q, want running", parsed.Payload.ClaudeState)
	}
	if parsed.Payload.ConnectedClients != 2 {
		t.Errorf("connected_clients = %d, want 2", parsed.Payload.ConnectedClients)
	}
	if !parsed.Payload.WatcherEnabled {
		t.Error("watcher_enabled should be true")
	}
	if !parsed.Payload.GitEnabled {
		t.Error("git_enabled should be true")
	}
}
