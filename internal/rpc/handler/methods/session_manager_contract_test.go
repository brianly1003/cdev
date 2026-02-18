package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

func TestSessionManagerProtocolContract_MethodMetadata(t *testing.T) {
	service := NewSessionManagerService(nil)
	registry := handler.NewRegistry()
	service.RegisterMethods(registry)

	startMeta := registry.GetMeta("session/start")
	if startMeta.Summary == "" {
		t.Fatal("session/start summary should not be empty")
	}
	assertParamContract(t, startMeta, "workspace_id", true)
	assertParamContract(t, startMeta, "session_id", false)
	startAgent := assertParamContract(t, startMeta, "agent_type", false)
	assertSchemaDefault(t, startAgent, "claude")
	assertSchemaEnumContains(t, startAgent, "claude", "codex")

	sendMeta := registry.GetMeta("session/send")
	if sendMeta.Summary == "" {
		t.Fatal("session/send summary should not be empty")
	}
	assertParamContract(t, sendMeta, "prompt", true)
	assertParamContract(t, sendMeta, "session_id", false)
	assertParamContract(t, sendMeta, "workspace_id", false)
	sendMode := assertParamContract(t, sendMeta, "mode", false)
	assertSchemaDefault(t, sendMode, "new")
	assertSchemaEnumContains(t, sendMode, "new", "continue")
	sendPermission := assertParamContract(t, sendMeta, "permission_mode", false)
	assertSchemaDefault(t, sendPermission, "default")
	assertSchemaEnumContains(t, sendPermission, "default", "acceptEdits", "bypassPermissions", "plan", "interactive")
	sendAgent := assertParamContract(t, sendMeta, "agent_type", false)
	assertSchemaDefault(t, sendAgent, "claude")
	assertSchemaEnumContains(t, sendAgent, "claude", "codex")

	watchMeta := registry.GetMeta("workspace/session/watch")
	if watchMeta.Summary == "" {
		t.Fatal("workspace/session/watch summary should not be empty")
	}
	assertParamContract(t, watchMeta, "workspace_id", true)
	assertParamContract(t, watchMeta, "session_id", true)
	watchAgent := assertParamContract(t, watchMeta, "agent_type", false)
	assertSchemaDefault(t, watchAgent, "claude")
	assertSchemaEnumContains(t, watchAgent, "claude", "codex")
}

func TestSessionManagerProtocolContract_SessionStart(t *testing.T) {
	service := NewSessionManagerService(nil)

	t.Run("defaults to claude when agent_type is omitted", func(t *testing.T) {
		_, rpcErr := service.Start(context.Background(), []byte(`{"workspace_id":"ws-1"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.AgentNotConfigured {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.AgentNotConfigured)
		}
		if !containsSubstr(rpcErr.Message, "claude session manager is not configured") {
			t.Fatalf("error message = %q, want claude manager not configured", rpcErr.Message)
		}
	})

	t.Run("codex manager-not-configured error includes method and agent_type", func(t *testing.T) {
		_, rpcErr := service.Start(context.Background(), []byte(`{"workspace_id":"ws-1","agent_type":"codex"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.AgentNotConfigured {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.AgentNotConfigured)
		}

		data := decodeErrorData(t, rpcErr)
		if got := data["method"]; got != "session/start" {
			t.Fatalf("error data method = %q, want %q", got, "session/start")
		}
		if got := data["agent_type"]; got != sessionManagerAgentCodex {
			t.Fatalf("error data agent_type = %q, want %q", got, sessionManagerAgentCodex)
		}
	})
}

func TestSessionManagerProtocolContract_SessionSend(t *testing.T) {
	service := NewSessionManagerService(nil)

	t.Run("invalid permission_mode returns invalid params", func(t *testing.T) {
		_, rpcErr := service.Send(context.Background(), []byte(`{"workspace_id":"ws-1","prompt":"hello","permission_mode":"invalid"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.InvalidParams {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.InvalidParams)
		}
		if !containsSubstr(rpcErr.Message, "permission_mode must be one of") {
			t.Fatalf("error message = %q, want permission_mode validation", rpcErr.Message)
		}
	})

	t.Run("defaults to claude when agent_type is omitted", func(t *testing.T) {
		_, rpcErr := service.Send(context.Background(), []byte(`{"workspace_id":"ws-1","prompt":"hello"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.AgentNotConfigured {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.AgentNotConfigured)
		}
		if !containsSubstr(rpcErr.Message, "claude session manager is not configured") {
			t.Fatalf("error message = %q, want claude manager not configured", rpcErr.Message)
		}
	})

	t.Run("codex manager-not-configured error includes method and agent_type", func(t *testing.T) {
		_, rpcErr := service.Send(context.Background(), []byte(`{"workspace_id":"ws-1","prompt":"hello","agent_type":"codex"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.AgentNotConfigured {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.AgentNotConfigured)
		}

		data := decodeErrorData(t, rpcErr)
		if got := data["method"]; got != "session/send" {
			t.Fatalf("error data method = %q, want %q", got, "session/send")
		}
		if got := data["agent_type"]; got != sessionManagerAgentCodex {
			t.Fatalf("error data agent_type = %q, want %q", got, sessionManagerAgentCodex)
		}
	})
}

func TestSessionManagerProtocolContract_WorkspaceSessionWatch(t *testing.T) {
	service := NewSessionManagerService(nil)

	t.Run("workspace_id is required", func(t *testing.T) {
		_, rpcErr := service.WatchSession(context.Background(), []byte(`{"session_id":"sess-1"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.InvalidParams {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.InvalidParams)
		}
		if rpcErr.Message != "workspace_id is required" {
			t.Fatalf("error message = %q, want %q", rpcErr.Message, "workspace_id is required")
		}
	})

	t.Run("session_id is required", func(t *testing.T) {
		_, rpcErr := service.WatchSession(context.Background(), []byte(`{"workspace_id":"ws-1"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.InvalidParams {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.InvalidParams)
		}
		if rpcErr.Message != "session_id is required" {
			t.Fatalf("error message = %q, want %q", rpcErr.Message, "session_id is required")
		}
	})

	t.Run("invalid agent_type is rejected", func(t *testing.T) {
		_, rpcErr := service.WatchSession(context.Background(), []byte(`{"workspace_id":"ws-1","session_id":"sess-1","agent_type":"gemini"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.InvalidParams {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.InvalidParams)
		}
		if !containsSubstr(rpcErr.Message, "agent_type must be one of: claude, codex") {
			t.Fatalf("error message = %q, want runtime enum validation", rpcErr.Message)
		}
	})

	t.Run("manager-not-configured includes method in error data", func(t *testing.T) {
		_, rpcErr := service.WatchSession(context.Background(), []byte(`{"workspace_id":"ws-1","session_id":"sess-1"}`))
		if rpcErr == nil {
			t.Fatal("expected error, got nil")
		}
		if rpcErr.Code != message.AgentNotConfigured {
			t.Fatalf("error code = %d, want %d", rpcErr.Code, message.AgentNotConfigured)
		}

		data := decodeErrorData(t, rpcErr)
		if got := data["method"]; got != "workspace/session/watch" {
			t.Fatalf("error data method = %q, want %q", got, "workspace/session/watch")
		}
	})
}

func assertParamContract(t *testing.T, meta handler.MethodMeta, name string, required bool) handler.OpenRPCParam {
	t.Helper()
	param, ok := findParam(meta, name)
	if !ok {
		t.Fatalf("param %q not found", name)
	}
	if param.Required != required {
		t.Fatalf("param %q required = %v, want %v", name, param.Required, required)
	}
	return param
}

func assertSchemaDefault(t *testing.T, param handler.OpenRPCParam, want string) {
	t.Helper()
	raw, ok := param.Schema["default"]
	if !ok {
		t.Fatalf("param %q missing schema.default", param.Name)
	}
	got, ok := raw.(string)
	if !ok {
		t.Fatalf("param %q schema.default type = %T, want string", param.Name, raw)
	}
	if got != want {
		t.Fatalf("param %q schema.default = %q, want %q", param.Name, got, want)
	}
}

func assertSchemaEnumContains(t *testing.T, param handler.OpenRPCParam, wants ...string) {
	t.Helper()
	raw, ok := param.Schema["enum"]
	if !ok {
		t.Fatalf("param %q missing schema.enum", param.Name)
	}

	values := make(map[string]struct{})
	switch enum := raw.(type) {
	case []string:
		for _, value := range enum {
			values[value] = struct{}{}
		}
	case []interface{}:
		for _, value := range enum {
			str, ok := value.(string)
			if ok {
				values[str] = struct{}{}
			}
		}
	default:
		t.Fatalf("param %q schema.enum type = %T, want []string or []interface{}", param.Name, raw)
	}

	for _, want := range wants {
		if _, ok := values[want]; !ok {
			t.Fatalf("param %q schema.enum missing %q", param.Name, want)
		}
	}
}

func findParam(meta handler.MethodMeta, name string) (handler.OpenRPCParam, bool) {
	for _, param := range meta.Params {
		if param.Name == name {
			return param, true
		}
	}
	return handler.OpenRPCParam{}, false
}

func decodeErrorData(t *testing.T, rpcErr *message.Error) map[string]string {
	t.Helper()
	if len(rpcErr.Data) == 0 {
		t.Fatalf("error data is empty")
	}

	var data map[string]string
	if err := json.Unmarshal(rpcErr.Data, &data); err != nil {
		t.Fatalf("failed to decode error data: %v", err)
	}
	return data
}

