package permission

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHookInput_JSONUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    HookInput
		wantErr bool
	}{
		{
			name: "valid Bash command",
			json: `{
				"session_id": "test-session",
				"transcript_path": "/tmp/transcript.json",
				"cwd": "/home/user/project",
				"permission_mode": "interactive",
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": "rm -rf /tmp/test"},
				"tool_use_id": "tool-123"
			}`,
			want: HookInput{
				SessionID:      "test-session",
				TranscriptPath: "/tmp/transcript.json",
				Cwd:            "/home/user/project",
				PermissionMode: "interactive",
				HookEventName:  "PreToolUse",
				ToolName:       "Bash",
				ToolInput:      map[string]interface{}{"command": "rm -rf /tmp/test"},
				ToolUseID:      "tool-123",
			},
			wantErr: false,
		},
		{
			name: "valid Write command",
			json: `{
				"session_id": "session-456",
				"tool_name": "Write",
				"tool_input": {"file_path": "/tmp/test.txt", "content": "hello"},
				"tool_use_id": "tool-456"
			}`,
			want: HookInput{
				SessionID: "session-456",
				ToolName:  "Write",
				ToolInput: map[string]interface{}{"file_path": "/tmp/test.txt", "content": "hello"},
				ToolUseID: "tool-456",
			},
			wantErr: false,
		},
		{
			name: "minimal required fields",
			json: `{
				"tool_name": "Read",
				"tool_use_id": "tool-789"
			}`,
			want: HookInput{
				ToolName:  "Read",
				ToolUseID: "tool-789",
			},
			wantErr: false,
		},
		{
			name:    "invalid JSON",
			json:    `{not valid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got HookInput
			err := json.Unmarshal([]byte(tt.json), &got)

			if (err != nil) != tt.wantErr {
				t.Errorf("json.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if got.SessionID != tt.want.SessionID {
					t.Errorf("SessionID = %v, want %v", got.SessionID, tt.want.SessionID)
				}
				if got.ToolName != tt.want.ToolName {
					t.Errorf("ToolName = %v, want %v", got.ToolName, tt.want.ToolName)
				}
				if got.ToolUseID != tt.want.ToolUseID {
					t.Errorf("ToolUseID = %v, want %v", got.ToolUseID, tt.want.ToolUseID)
				}
			}
		})
	}
}

func TestHookOutput_JSONMarshal(t *testing.T) {
	tests := []struct {
		name     string
		output   HookOutput
		contains []string
	}{
		{
			name: "allow decision",
			output: HookOutput{
				HookSpecificOutput: HookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "allow",
					PermissionDecisionReason: "Approved via cdev mobile app",
				},
			},
			contains: []string{
				`"hookEventName":"PreToolUse"`,
				`"permissionDecision":"allow"`,
				`"permissionDecisionReason":"Approved via cdev mobile app"`,
			},
		},
		{
			name: "deny decision",
			output: HookOutput{
				HookSpecificOutput: HookSpecificOutput{
					HookEventName:            "PreToolUse",
					PermissionDecision:       "deny",
					PermissionDecisionReason: "Denied via cdev mobile app",
				},
			},
			contains: []string{
				`"permissionDecision":"deny"`,
			},
		},
		{
			name: "with updated input",
			output: HookOutput{
				HookSpecificOutput: HookSpecificOutput{
					HookEventName:      "PreToolUse",
					PermissionDecision: "allow",
					UpdatedInput:       map[string]interface{}{"command": "ls -la"},
				},
			},
			contains: []string{
				`"updatedInput"`,
				`"command":"ls -la"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.output)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			jsonStr := string(data)
			for _, substr := range tt.contains {
				if !containsString(jsonStr, substr) {
					t.Errorf("Marshal output missing %q, got: %s", substr, jsonStr)
				}
			}
		})
	}
}

func TestResponse_JSONRoundTrip(t *testing.T) {
	original := &Response{
		Decision:     DecisionAllow,
		Scope:        ScopeSession,
		Pattern:      "Bash(rm:*)",
		UpdatedInput: map[string]interface{}{"command": "rm file.txt"},
		Message:      "Approved by user",
		Interrupt:    false,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Decision != original.Decision {
		t.Errorf("Decision = %v, want %v", decoded.Decision, original.Decision)
	}
	if decoded.Scope != original.Scope {
		t.Errorf("Scope = %v, want %v", decoded.Scope, original.Scope)
	}
	if decoded.Pattern != original.Pattern {
		t.Errorf("Pattern = %v, want %v", decoded.Pattern, original.Pattern)
	}
	if decoded.Message != original.Message {
		t.Errorf("Message = %v, want %v", decoded.Message, original.Message)
	}
}

func TestDecisionConstants(t *testing.T) {
	if DecisionAllow != "allow" {
		t.Errorf("DecisionAllow = %v, want 'allow'", DecisionAllow)
	}
	if DecisionDeny != "deny" {
		t.Errorf("DecisionDeny = %v, want 'deny'", DecisionDeny)
	}
}

func TestScopeConstants(t *testing.T) {
	if ScopeOnce != "once" {
		t.Errorf("ScopeOnce = %v, want 'once'", ScopeOnce)
	}
	if ScopeSession != "session" {
		t.Errorf("ScopeSession = %v, want 'session'", ScopeSession)
	}
	if ScopePath != "path" {
		t.Errorf("ScopePath = %v, want 'path'", ScopePath)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.SessionMemory.Enabled {
		t.Error("SessionMemory.Enabled should be true by default")
	}
	if cfg.SessionMemory.TTL != time.Hour {
		t.Errorf("SessionMemory.TTL = %v, want 1 hour", cfg.SessionMemory.TTL)
	}
	if cfg.SessionMemory.MaxPatterns != 100 {
		t.Errorf("SessionMemory.MaxPatterns = %v, want 100", cfg.SessionMemory.MaxPatterns)
	}
}

func TestStoredDecision_JSONRoundTrip(t *testing.T) {
	original := StoredDecision{
		Pattern:    "Bash(git:*)",
		Decision:   DecisionAllow,
		CreatedAt:  time.Now().Truncate(time.Second),
		UsageCount: 5,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded StoredDecision
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Pattern != original.Pattern {
		t.Errorf("Pattern = %v, want %v", decoded.Pattern, original.Pattern)
	}
	if decoded.Decision != original.Decision {
		t.Errorf("Decision = %v, want %v", decoded.Decision, original.Decision)
	}
	if decoded.UsageCount != original.UsageCount {
		t.Errorf("UsageCount = %v, want %v", decoded.UsageCount, original.UsageCount)
	}
}

func TestPermissionEvent_JSONMarshal(t *testing.T) {
	event := PermissionEvent{
		ToolUseID:   "tool-123",
		Type:        "bash_command",
		Target:      "rm -rf /tmp/test",
		Description: "Delete files in /tmp/test",
		Preview:     "rm -rf /tmp/test",
		Options: []PermissionOption{
			{Key: "allow_once", Label: "Allow Once", Description: "Allow this one request"},
			{Key: "deny", Label: "Deny", Description: "Deny this request"},
		},
		SessionID:   "session-456",
		WorkspaceID: "workspace-789",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	jsonStr := string(data)
	expected := []string{
		`"tool_use_id":"tool-123"`,
		`"type":"bash_command"`,
		`"target":"rm -rf /tmp/test"`,
		`"options":[`,
		`"key":"allow_once"`,
	}

	for _, substr := range expected {
		if !containsString(jsonStr, substr) {
			t.Errorf("Marshal output missing %q, got: %s", substr, jsonStr)
		}
	}
}

func TestRequest_ResponseChannel(t *testing.T) {
	now := time.Now()
	req := &Request{
		ID:           "req-1",
		SessionID:    "session-1",
		WorkspaceID:  "workspace-1",
		ToolName:     "Bash",
		ToolInput:    map[string]interface{}{"command": "ls"},
		ToolUseID:    "tool-1",
		CreatedAt:    now,
		ResponseChan: make(chan *Response, 1),
	}

	// Verify fields are set correctly
	if req.ID != "req-1" {
		t.Errorf("ID = %v, want req-1", req.ID)
	}
	if req.SessionID != "session-1" {
		t.Errorf("SessionID = %v, want session-1", req.SessionID)
	}
	if req.WorkspaceID != "workspace-1" {
		t.Errorf("WorkspaceID = %v, want workspace-1", req.WorkspaceID)
	}
	if req.ToolName != "Bash" {
		t.Errorf("ToolName = %v, want Bash", req.ToolName)
	}
	if req.ToolUseID != "tool-1" {
		t.Errorf("ToolUseID = %v, want tool-1", req.ToolUseID)
	}
	if req.CreatedAt != now {
		t.Errorf("CreatedAt = %v, want %v", req.CreatedAt, now)
	}
	if cmd, ok := req.ToolInput["command"].(string); !ok || cmd != "ls" {
		t.Errorf("ToolInput[command] = %v, want ls", req.ToolInput["command"])
	}

	// Send a response
	response := &Response{
		Decision: DecisionAllow,
		Scope:    ScopeOnce,
	}
	req.ResponseChan <- response

	// Receive the response
	received := <-req.ResponseChan
	if received.Decision != DecisionAllow {
		t.Errorf("Decision = %v, want %v", received.Decision, DecisionAllow)
	}
}

// Helper function
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
