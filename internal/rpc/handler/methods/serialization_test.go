package methods

import (
	"encoding/json"
	"testing"
	"time"
)

// TestAgentRunParamsJSON tests snake_case serialization for AgentRunParams.
func TestAgentRunParamsJSON(t *testing.T) {
	params := AgentRunParams{
		Prompt:    "test prompt",
		Mode:      "continue",
		SessionID: "session-123",
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	if !containsField(jsonStr, "session_id") {
		t.Errorf("expected 'session_id' field, got: %s", jsonStr)
	}
	if containsField(jsonStr, "sessionId") {
		t.Errorf("unexpected camelCase 'sessionId' found: %s", jsonStr)
	}

	// Verify round-trip
	var decoded AgentRunParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.SessionID != params.SessionID {
		t.Errorf("SessionID mismatch: got %s, want %s", decoded.SessionID, params.SessionID)
	}

	// Verify parsing with snake_case input (what iOS sends)
	input := `{"prompt":"hello","mode":"continue","session_id":"abc-123"}`
	var parsed AgentRunParams
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("unmarshal from snake_case input error: %v", err)
	}
	if parsed.SessionID != "abc-123" {
		t.Errorf("SessionID from snake_case input mismatch: got %s, want abc-123", parsed.SessionID)
	}
}

// TestAgentRunResultJSON tests snake_case serialization for AgentRunResult.
func TestAgentRunResultJSON(t *testing.T) {
	result := AgentRunResult{
		Status:    "started",
		PID:       12345,
		SessionID: "session-456",
		Mode:      "new",
		AgentType: "claude",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{"session_id", "agent_type"}
	unexpectedFields := []string{"sessionId", "agentType"}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}
}

// TestAgentRespondParamsJSON tests snake_case serialization for AgentRespondParams.
func TestAgentRespondParamsJSON(t *testing.T) {
	// Test with IsError = true to ensure field is included (it has omitempty)
	params := AgentRespondParams{
		ToolUseID: "tool-123",
		Response:  "approved",
		IsError:   true,
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{"tool_use_id", "is_error"}
	unexpectedFields := []string{"toolUseId", "isError"}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}

	// Verify omitempty works (IsError=false should be omitted)
	paramsNoError := AgentRespondParams{
		ToolUseID: "tool-456",
		Response:  "ok",
		IsError:   false,
	}
	dataNoError, _ := json.Marshal(paramsNoError)
	if containsField(string(dataNoError), "is_error") {
		t.Errorf("is_error should be omitted when false, got: %s", string(dataNoError))
	}

	// Verify parsing from iOS-style snake_case input
	input := `{"tool_use_id":"xyz-789","response":"denied","is_error":true}`
	var parsed AgentRespondParams
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed.ToolUseID != "xyz-789" {
		t.Errorf("ToolUseID mismatch: got %s, want xyz-789", parsed.ToolUseID)
	}
	if !parsed.IsError {
		t.Errorf("IsError should be true")
	}
}

// TestAgentStatusResultJSON tests snake_case serialization for AgentStatusResult.
func TestAgentStatusResultJSON(t *testing.T) {
	result := AgentStatusResult{
		State:      "running",
		PID:        99999,
		SessionID:  "session-789",
		AgentType:  "gemini",
		WaitingFor: "permission",
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{"session_id", "agent_type", "waiting_for"}
	unexpectedFields := []string{"sessionId", "agentType", "waitingFor"}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}
}

// TestSessionInfoJSON tests snake_case serialization for SessionInfo.
func TestSessionInfoJSON(t *testing.T) {
	now := time.Now()
	info := SessionInfo{
		SessionID:    "session-abc",
		AgentType:    "claude",
		Summary:      "Test session",
		MessageCount: 42,
		StartTime:    now,
		LastUpdated:  now.Add(time.Hour),
		Branch:       "main",
		ProjectPath:  "/Users/test/project",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{
		"session_id", "agent_type", "message_count",
		"start_time", "last_updated", "project_path",
	}
	unexpectedFields := []string{
		"sessionId", "agentType", "messageCount",
		"startTime", "lastUpdated", "projectPath",
	}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}
}

// TestSessionMessageJSON tests snake_case serialization for SessionMessage.
func TestSessionMessageJSON(t *testing.T) {
	msg := SessionMessage{
		ID:                  1,
		SessionID:           "session-xyz",
		Type:                "assistant",
		UUID:                "uuid-123",
		Timestamp:           "2025-12-20T10:00:00Z",
		GitBranch:           "main",
		Message:             json.RawMessage(`{"role":"assistant","content":"Hello"}`),
		IsContextCompaction: true,
		IsMeta:              true,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{"session_id", "git_branch", "is_context_compaction", "is_meta"}
	unexpectedFields := []string{"sessionId", "gitBranch", "isContextCompaction", "isMeta"}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}

	// Test omitempty - IsMeta=false should not appear in output
	msgNoMeta := SessionMessage{
		ID:        2,
		SessionID: "session-abc",
		Type:      "user",
		Message:   json.RawMessage(`{"role":"user","content":"Hi"}`),
		IsMeta:    false,
	}
	dataNoMeta, _ := json.Marshal(msgNoMeta)
	if containsField(string(dataNoMeta), "is_meta") {
		t.Errorf("is_meta should be omitted when false, got: %s", string(dataNoMeta))
	}
}

// TestListSessionsParamsJSON tests snake_case serialization for ListSessionsParams.
func TestListSessionsParamsJSON(t *testing.T) {
	params := ListSessionsParams{
		AgentType: "claude",
		Limit:     50,
	}

	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !containsField(jsonStr, "agent_type") {
		t.Errorf("expected 'agent_type' field, got: %s", jsonStr)
	}
	if containsField(jsonStr, "agentType") {
		t.Errorf("unexpected camelCase 'agentType' found: %s", jsonStr)
	}

	// Verify parsing from iOS-style input
	input := `{"agent_type":"gemini","limit":100}`
	var parsed ListSessionsParams
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if parsed.AgentType != "gemini" {
		t.Errorf("AgentType mismatch: got %s, want gemini", parsed.AgentType)
	}
}

// TestGetSessionMessagesResultJSON tests snake_case serialization for GetSessionMessagesResult.
func TestGetSessionMessagesResultJSON(t *testing.T) {
	result := GetSessionMessagesResult{
		Messages: []SessionMessage{},
		Total:    100,
		Limit:    50,
		Offset:   0,
		HasMore:  true,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !containsField(jsonStr, "has_more") {
		t.Errorf("expected 'has_more' field, got: %s", jsonStr)
	}
	if containsField(jsonStr, "hasMore") {
		t.Errorf("unexpected camelCase 'hasMore' found: %s", jsonStr)
	}
}

// TestGitStatusInfoJSON tests snake_case serialization for GitStatusInfo.
func TestGitStatusInfoJSON(t *testing.T) {
	status := GitStatusInfo{
		Branch:   "main",
		Upstream: "origin/main",
		Ahead:    2,
		Behind:   1,
		Staged:   []GitFileStatus{{Path: "file1.go", Status: "M"}},
		Unstaged: []GitFileStatus{{Path: "file2.go", Status: "M"}},
		Untracked: []GitFileStatus{
			{Path: "file3.go", Status: "?"},
			{Path: "file4.go", Status: "?"},
		},
		Conflicted: []GitFileStatus{},
		RepoName:   "cdev",
		RepoRoot:   "/Users/test/cdev",
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{
		"repo_name", "repo_root",
	}
	unexpectedFields := []string{
		"repoName", "repoRoot",
	}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}
}

// TestDiffResultJSON tests snake_case serialization for DiffResult.
func TestDiffResultJSON(t *testing.T) {
	result := DiffResult{
		Path:     "main.go",
		Diff:     "--- a/main.go\n+++ b/main.go",
		IsStaged: true,
		IsNew:    false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{"is_staged", "is_new"}
	unexpectedFields := []string{"isStaged", "isNew"}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}
}

// TestStageResultJSON tests snake_case serialization for StageResult.
func TestStageResultJSON(t *testing.T) {
	result := StageResult{
		Success: true,
		Staged:  []string{"file1.go", "file2.go"},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !containsField(jsonStr, "success") {
		t.Errorf("expected 'success' field, got: %s", jsonStr)
	}
	if !containsField(jsonStr, "staged") {
		t.Errorf("expected 'staged' field, got: %s", jsonStr)
	}
}

// TestCommitResultJSON tests snake_case serialization for CommitResult.
func TestCommitResultJSON(t *testing.T) {
	result := CommitResult{
		Success:        true,
		SHA:            "abc123",
		Message:        "test commit",
		FilesCommitted: 3,
		Pushed:         false,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !containsField(jsonStr, "files_committed") {
		t.Errorf("expected 'files_committed' field, got: %s", jsonStr)
	}
	if containsField(jsonStr, "filesCommitted") {
		t.Errorf("unexpected camelCase 'filesCommitted' found: %s", jsonStr)
	}
}

// TestGetStatusResultJSON tests snake_case serialization for GetStatusResult.
func TestGetStatusResultJSON(t *testing.T) {
	result := GetStatusResult{
		ClaudeState:      "running",
		ConnectedClients: 3,
		RepoPath:         "/Users/test/project",
		RepoName:         "project",
		UptimeSeconds:    3600,
		AgentVersion:     "1.0.0",
		WatcherEnabled:   true,
		GitEnabled:       true,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	// Verify snake_case field names
	expectedFields := []string{
		"claude_state", "connected_clients", "repo_path", "repo_name",
		"uptime_seconds", "agent_version", "watcher_enabled", "git_enabled",
	}
	unexpectedFields := []string{
		"claudeState", "connectedClients", "repoPath", "repoName",
		"uptimeSeconds", "agentVersion", "watcherEnabled", "gitEnabled",
	}

	for _, field := range expectedFields {
		if !containsField(jsonStr, field) {
			t.Errorf("expected '%s' field, got: %s", field, jsonStr)
		}
	}
	for _, field := range unexpectedFields {
		if containsField(jsonStr, field) {
			t.Errorf("unexpected camelCase '%s' found: %s", field, jsonStr)
		}
	}
}

// TestHealthResultJSON tests snake_case serialization for HealthResult.
func TestHealthResultJSON(t *testing.T) {
	result := HealthResult{
		Status:        "ok",
		UptimeSeconds: 7200,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)

	if !containsField(jsonStr, "uptime_seconds") {
		t.Errorf("expected 'uptime_seconds' field, got: %s", jsonStr)
	}
	if containsField(jsonStr, "uptimeSeconds") {
		t.Errorf("unexpected camelCase 'uptimeSeconds' found: %s", jsonStr)
	}
}

// containsField checks if a JSON string contains a specific field name.
func containsField(jsonStr, fieldName string) bool {
	// Look for "field_name": or "field_name":
	pattern := `"` + fieldName + `"`
	return len(jsonStr) > 0 && contains(jsonStr, pattern)
}

// contains is a simple string contains check.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
