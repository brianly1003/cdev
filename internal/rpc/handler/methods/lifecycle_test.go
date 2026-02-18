package methods

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/rpc/handler"
)

func TestDefaultRuntimeRegistryWithAgents(t *testing.T) {
	registry := DefaultRuntimeRegistryWithAgents([]string{"codex", "claude", "codex", "  "})
	if registry == nil {
		t.Fatal("expected runtime registry, got nil")
	}

	if registry.SchemaVersion != "1.0" {
		t.Fatalf("schemaVersion = %q, want 1.0", registry.SchemaVersion)
	}

	if registry.DefaultRuntime != "claude" {
		t.Fatalf("defaultRuntime = %q, want claude", registry.DefaultRuntime)
	}

	if registry.Routing.AgentTypeField != "agent_type" {
		t.Fatalf("routing.agentTypeField = %q, want agent_type", registry.Routing.AgentTypeField)
	}

	if len(registry.Routing.RequiredOnMethods) == 0 {
		t.Fatal("expected requiredOnMethods to be populated")
	}

	if len(registry.Runtimes) != 2 {
		t.Fatalf("runtimes len = %d, want 2", len(registry.Runtimes))
	}

	if _, err := time.Parse(time.RFC3339, registry.GeneratedAt); err != nil {
		t.Fatalf("generatedAt is not RFC3339: %v", err)
	}

	runtimeByID := make(map[string]RuntimeDescriptor, len(registry.Runtimes))
	for _, runtime := range registry.Runtimes {
		runtimeByID[runtime.ID] = runtime
	}

	claude, ok := runtimeByID["claude"]
	if !ok {
		t.Fatal("claude runtime missing from registry")
	}
	if !claude.RequiresWorkspaceActivationOnResume {
		t.Fatal("claude requiresWorkspaceActivationOnResume should be true")
	}
	if !claude.RequiresSessionResolutionOnNewSession {
		t.Fatal("claude requiresSessionResolutionOnNewSession should be true")
	}
	if claude.Methods.Send != "session/send" {
		t.Fatalf("claude methods.send = %q, want session/send", claude.Methods.Send)
	}

	codex, ok := runtimeByID["codex"]
	if !ok {
		t.Fatal("codex runtime missing from registry")
	}
	if codex.RequiresWorkspaceActivationOnResume {
		t.Fatal("codex requiresWorkspaceActivationOnResume should be false")
	}
	if codex.RequiresSessionResolutionOnNewSession {
		t.Fatal("codex requiresSessionResolutionOnNewSession should be false")
	}
	if codex.Methods.Watch != "workspace/session/watch" {
		t.Fatalf("codex methods.watch = %q, want workspace/session/watch", codex.Methods.Watch)
	}
}

func TestLifecycleServiceInitializeIncludesRuntimeRegistry(t *testing.T) {
	caps := ServerCapabilities{
		Agent: &AgentCapabilities{
			Run:          true,
			Stop:         true,
			Respond:      true,
			Sessions:     true,
			SessionWatch: true,
		},
		SupportedAgents: []string{"claude", "codex"},
	}

	service := NewLifecycleService("test-version", caps)
	ctx := context.WithValue(context.Background(), handler.ClientIDKey, "client-123")

	result, rpcErr := service.Initialize(ctx, json.RawMessage(`{}`))
	if rpcErr != nil {
		t.Fatalf("Initialize() error = %v", rpcErr)
	}

	initResult, ok := result.(InitializeResult)
	if !ok {
		t.Fatalf("Initialize() type = %T, want InitializeResult", result)
	}

	if initResult.ClientID != "client-123" {
		t.Fatalf("clientId = %q, want client-123", initResult.ClientID)
	}

	if initResult.Capabilities.RuntimeRegistry == nil {
		t.Fatal("capabilities.runtimeRegistry should be populated")
	}

	registry := initResult.Capabilities.RuntimeRegistry
	if registry.DefaultRuntime != "claude" {
		t.Fatalf("runtimeRegistry.defaultRuntime = %q, want claude", registry.DefaultRuntime)
	}

	if registry.Routing.DefaultAgentType != registry.DefaultRuntime {
		t.Fatalf("routing.defaultAgentType = %q, want %q", registry.Routing.DefaultAgentType, registry.DefaultRuntime)
	}

	if len(registry.Runtimes) != len(initResult.Capabilities.SupportedAgents) {
		t.Fatalf("runtime count = %d, supportedAgents count = %d", len(registry.Runtimes), len(initResult.Capabilities.SupportedAgents))
	}
}
