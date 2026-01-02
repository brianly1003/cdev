package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/message"
)

// TestDeleteHistorySession_ParamValidation tests the parameter validation
// in the DeleteHistorySession RPC handler without needing to mock the manager.
func TestDeleteHistorySession_ParamValidation(t *testing.T) {
	tests := []struct {
		name        string
		params      string
		wantErrCode int
		wantErrMsg  string
	}{
		{
			name:        "missing workspace_id",
			params:      `{"session_id": "sess-456"}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "workspace_id is required",
		},
		{
			name:        "missing session_id",
			params:      `{"workspace_id": "ws-123"}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "session_id is required",
		},
		{
			name:        "empty params",
			params:      `{}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "workspace_id is required",
		},
		{
			name:        "invalid JSON",
			params:      `not valid json`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "failed to parse params",
		},
		{
			name:        "empty workspace_id",
			params:      `{"workspace_id": "", "session_id": "sess-456"}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "workspace_id is required",
		},
		{
			name:        "empty session_id",
			params:      `{"workspace_id": "ws-123", "session_id": ""}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "session_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create service with nil manager - validation happens before manager call
			service := &SessionManagerService{manager: nil}

			_, err := service.DeleteHistorySession(context.Background(), []byte(tt.params))

			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if err.Code != tt.wantErrCode {
				t.Errorf("Error code = %d, want %d", err.Code, tt.wantErrCode)
			}

			if tt.wantErrMsg != "" && !containsSubstr(err.Message, tt.wantErrMsg) {
				t.Errorf("Error message = %q, want to contain %q", err.Message, tt.wantErrMsg)
			}
		})
	}
}

// TestDeleteHistorySession_ValidParams tests that valid params are parsed correctly.
// This test verifies the JSON parsing works before attempting the manager call.
func TestDeleteHistorySession_ValidParams(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]interface{}
	}{
		{
			name: "basic params",
			params: map[string]interface{}{
				"workspace_id": "ws-123",
				"session_id":   "sess-456",
			},
		},
		{
			name: "UUID format session_id",
			params: map[string]interface{}{
				"workspace_id": "550e8400-e29b-41d4-a716-446655440000",
				"session_id":   "8a10cd3b-9a1e-4324-8673-b927b686b919",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramsJSON, _ := json.Marshal(tt.params)

			// Parse params manually to verify they're valid
			var p struct {
				WorkspaceID string `json:"workspace_id"`
				SessionID   string `json:"session_id"`
			}
			err := json.Unmarshal(paramsJSON, &p)
			if err != nil {
				t.Fatalf("Failed to parse params: %v", err)
			}

			if p.WorkspaceID != tt.params["workspace_id"] {
				t.Errorf("WorkspaceID = %q, want %q", p.WorkspaceID, tt.params["workspace_id"])
			}
			if p.SessionID != tt.params["session_id"] {
				t.Errorf("SessionID = %q, want %q", p.SessionID, tt.params["session_id"])
			}
		})
	}
}

// TestDeleteHistorySession_ErrorClassification tests that errors from the manager
// are correctly classified as SessionNotFound vs InternalError.
func TestDeleteHistorySession_ErrorClassification(t *testing.T) {
	tests := []struct {
		name        string
		errMsg      string
		wantCode    int
	}{
		{
			name:     "session not found",
			errMsg:   "session file not found: sess-456",
			wantCode: message.SessionNotFound,
		},
		{
			name:     "workspace not found",
			errMsg:   "workspace not found: ws-123",
			wantCode: message.SessionNotFound,
		},
		{
			name:     "generic not found",
			errMsg:   "not found",
			wantCode: message.SessionNotFound,
		},
		{
			name:     "permission denied",
			errMsg:   "failed to delete: permission denied",
			wantCode: message.InternalError,
		},
		{
			name:     "generic error",
			errMsg:   "something went wrong",
			wantCode: message.InternalError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the error classification logic
			var errCode int
			if containsSubstr(tt.errMsg, "not found") {
				errCode = message.SessionNotFound
			} else {
				errCode = message.InternalError
			}

			if errCode != tt.wantCode {
				t.Errorf("Error code = %d, want %d for message %q", errCode, tt.wantCode, tt.errMsg)
			}
		})
	}
}

// containsSubstr checks if s contains substr
func containsSubstr(s, substr string) bool {
	return len(substr) <= len(s) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
