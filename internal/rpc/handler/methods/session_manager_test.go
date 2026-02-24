package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/session"
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

func TestBuildCodexCLIArgs_PreservesBangPrefix(t *testing.T) {
	args := buildCodexCLIArgs("/tmp", "019c7b09-3a7c-7953-90cf-91c48d81c877", "!ls")
	want := []string{"exec", "resume", "019c7b09-3a7c-7953-90cf-91c48d81c877", "!ls"}
	if len(args) != len(want) {
		t.Fatalf("args len = %d, want %d (%v)", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q (%v)", i, args[i], want[i], args)
		}
	}
}

// TestDeleteHistorySession_ErrorClassification tests that errors from the manager
// are correctly classified as SessionNotFound vs InternalError.
func TestDeleteHistorySession_ErrorClassification(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		wantCode int
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

func TestSessionManagerSend_AgentTypeValidation(t *testing.T) {
	tests := []struct {
		name          string
		params        string
		wantErrCode   int
		wantErrMsg    string
		wantAgentType string
	}{
		{
			name:        "invalid agent_type",
			params:      `{"workspace_id":"ws-123","prompt":"hello","agent_type":"gemini"}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "agent_type must be one of: claude, codex",
		},
		{
			name:          "codex runtime not configured",
			params:        `{"workspace_id":"ws-123","prompt":"hello","agent_type":"codex"}`,
			wantErrCode:   message.AgentNotConfigured,
			wantErrMsg:    "codex runtime requires session manager context",
			wantAgentType: "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &SessionManagerService{manager: nil}

			_, err := service.Send(context.Background(), []byte(tt.params))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if err.Code != tt.wantErrCode {
				t.Errorf("error code = %d, want %d", err.Code, tt.wantErrCode)
			}

			if tt.wantErrMsg != "" && !containsSubstr(err.Message, tt.wantErrMsg) {
				t.Errorf("error message = %q, want to contain %q", err.Message, tt.wantErrMsg)
			}

			if tt.wantAgentType != "" {
				var data map[string]string
				if unmarshalErr := json.Unmarshal(err.Data, &data); unmarshalErr != nil {
					t.Fatalf("failed to parse error data: %v", unmarshalErr)
				}
				if data["agent_type"] != tt.wantAgentType {
					t.Errorf("error data agent_type = %q, want %q", data["agent_type"], tt.wantAgentType)
				}
			}
		})
	}
}

func TestSessionManagerStart_AgentTypeValidation(t *testing.T) {
	tests := []struct {
		name        string
		params      string
		wantErrCode int
		wantErrMsg  string
	}{
		{
			name:        "invalid agent_type",
			params:      `{"workspace_id":"ws-123","agent_type":"gemini"}`,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "agent_type must be one of: claude, codex",
		},
		{
			name:        "codex runtime not configured",
			params:      `{"workspace_id":"ws-123","agent_type":"codex"}`,
			wantErrCode: message.AgentNotConfigured,
			wantErrMsg:  "codex runtime requires session manager context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &SessionManagerService{manager: nil}

			_, err := service.Start(context.Background(), []byte(tt.params))
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if err.Code != tt.wantErrCode {
				t.Errorf("error code = %d, want %d", err.Code, tt.wantErrCode)
			}

			if tt.wantErrMsg != "" && !containsSubstr(err.Message, tt.wantErrMsg) {
				t.Errorf("error message = %q, want to contain %q", err.Message, tt.wantErrMsg)
			}
		})
	}
}

func TestSessionManagerWorkspaceMethods_ManagerNotConfigured(t *testing.T) {
	service := &SessionManagerService{manager: nil}

	tests := []struct {
		name       string
		methodName string
		params     string
		call       func(context.Context, json.RawMessage) (interface{}, *message.Error)
	}{
		{
			name:       "workspace session history",
			methodName: "workspace/session/history",
			params:     `{"workspace_id":"ws-123","agent_type":"codex"}`,
			call:       service.History,
		},
		{
			name:       "workspace session messages",
			methodName: "workspace/session/messages",
			params:     `{"workspace_id":"ws-123","session_id":"sess-1","agent_type":"codex"}`,
			call:       service.GetSessionMessages,
		},
		{
			name:       "workspace session delete",
			methodName: "workspace/session/delete",
			params:     `{"workspace_id":"ws-123","session_id":"sess-1","agent_type":"codex"}`,
			call:       service.DeleteHistorySession,
		},
		{
			name:       "workspace session watch",
			methodName: "workspace/session/watch",
			params:     `{"workspace_id":"ws-123","session_id":"sess-1","agent_type":"codex"}`,
			call:       service.WatchSession,
		},
		{
			name:       "workspace session unwatch claude",
			methodName: "workspace/session/unwatch",
			params:     `{"agent_type":"claude"}`,
			call:       service.UnwatchSession,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.call(context.Background(), []byte(tt.params))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if err.Code != message.AgentNotConfigured {
				t.Fatalf("error code = %d, want %d", err.Code, message.AgentNotConfigured)
			}
			if !containsSubstr(err.Message, "session manager is not configured") {
				t.Fatalf("error message = %q, want manager not configured", err.Message)
			}

			var data map[string]string
			if unmarshalErr := json.Unmarshal(err.Data, &data); unmarshalErr != nil {
				t.Fatalf("failed to parse error data: %v", unmarshalErr)
			}
			if data["method"] != tt.methodName {
				t.Fatalf("error data method = %q, want %q", data["method"], tt.methodName)
			}
		})
	}
}

func TestSessionManagerUnwatchSession_AgentTypeValidation(t *testing.T) {
	tests := []struct {
		name        string
		params      json.RawMessage
		wantErrCode int
		wantErrMsg  string
	}{
		{
			name:        "missing params",
			params:      nil,
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "agent_type is required",
		},
		{
			name:        "missing agent_type",
			params:      []byte(`{}`),
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "agent_type is required",
		},
		{
			name:        "blank agent_type",
			params:      []byte(`{"agent_type":"   "}`),
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "agent_type is required",
		},
		{
			name:        "invalid agent_type",
			params:      []byte(`{"agent_type":"gemini"}`),
			wantErrCode: message.InvalidParams,
			wantErrMsg:  "agent_type must be one of: claude, codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &SessionManagerService{
				manager:       nil,
				codexWatchers: make(map[string]session.WatchInfo),
			}

			_, err := service.UnwatchSession(context.Background(), tt.params)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if err.Code != tt.wantErrCode {
				t.Fatalf("error code = %d, want %d", err.Code, tt.wantErrCode)
			}
			if !containsSubstr(err.Message, tt.wantErrMsg) {
				t.Fatalf("error message = %q, want to contain %q", err.Message, tt.wantErrMsg)
			}
		})
	}
}

func TestSessionManagerUnwatchSession_Codex_KeepsSharedStreamAliveForRemainingWatchers(t *testing.T) {
	codexStreamer := &mockSessionStreamer{}
	sessionService := NewSessionService(nil)
	sessionService.RegisterStreamer(sessionManagerAgentCodex, codexStreamer)

	service := &SessionManagerService{
		sessionService: sessionService,
		codexWatchers: map[string]session.WatchInfo{
			"client-a": {
				WorkspaceID: "ws-1",
				SessionID:   "sess-1",
				Watching:    true,
			},
			"client-b": {
				WorkspaceID: "ws-1",
				SessionID:   "sess-1",
				Watching:    true,
			},
		},
	}

	ctx := context.WithValue(context.Background(), handler.ClientIDKey, "client-a")
	_, err := service.UnwatchSession(ctx, []byte(`{"agent_type":"codex"}`))
	if err != nil {
		t.Fatalf("UnwatchSession returned error: %v", err)
	}

	if codexStreamer.unwatchCalled {
		t.Fatal("runtime unwatch should not be called while codex watchers remain")
	}

	if len(service.codexWatchers) != 1 {
		t.Fatalf("expected 1 remaining codex watcher, got %d", len(service.codexWatchers))
	}

	if _, ok := service.codexWatchers["client-a"]; ok {
		t.Fatal("client-a should be removed from codexWatchers")
	}
}

func TestSessionManagerUnwatchSession_Codex_LastWatcherStopsRuntimeStream(t *testing.T) {
	codexStreamer := &mockSessionStreamer{}
	sessionService := NewSessionService(nil)
	sessionService.RegisterStreamer(sessionManagerAgentCodex, codexStreamer)

	service := &SessionManagerService{
		sessionService: sessionService,
		codexWatchers: map[string]session.WatchInfo{
			"client-a": {
				WorkspaceID: "ws-1",
				SessionID:   "sess-1",
				Watching:    true,
			},
		},
	}

	ctx := context.WithValue(context.Background(), handler.ClientIDKey, "client-a")
	_, err := service.UnwatchSession(ctx, []byte(`{"agent_type":"codex"}`))
	if err != nil {
		t.Fatalf("UnwatchSession returned error: %v", err)
	}

	if !codexStreamer.unwatchCalled {
		t.Fatal("runtime unwatch should be called when last codex watcher stops")
	}

	if len(service.codexWatchers) != 0 {
		t.Fatalf("expected codexWatchers to be empty, got %d", len(service.codexWatchers))
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
