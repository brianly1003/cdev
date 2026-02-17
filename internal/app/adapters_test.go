// Package app provides unit tests for the RPC adapters.
package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
)

// --- StatusProvider Interface Tests (on App) ---

func TestApp_ClaudeState_NilManager(t *testing.T) {
	app := &App{
		claudeManager: nil,
	}

	state := app.ClaudeState()
	if state != "idle" {
		t.Errorf("expected idle for nil manager, got %s", state)
	}
}

func TestApp_ConnectedClients_NilServer(t *testing.T) {
	app := &App{
		unifiedServer: nil,
	}

	count := app.ConnectedClients()
	if count != 0 {
		t.Errorf("expected 0 for nil server, got %d", count)
	}
}

func TestApp_RepoPath(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/repo/path",
		},
	}
	app := &App{
		cfg: cfg,
	}

	path := app.RepoPath()
	if path != "/test/repo/path" {
		t.Errorf("expected /test/repo/path, got %s", path)
	}
}

func TestApp_Version(t *testing.T) {
	app := &App{
		version: "1.2.3",
	}

	version := app.Version()
	if version != "1.2.3" {
		t.Errorf("expected 1.2.3, got %s", version)
	}
}

func TestApp_WatcherEnabled_NilWatcher(t *testing.T) {
	cfg := &config.Config{
		Watcher: config.WatcherConfig{
			Enabled: true,
		},
	}
	app := &App{
		cfg:         cfg,
		fileWatcher: nil,
	}

	enabled := app.WatcherEnabled()
	if enabled {
		t.Error("expected false when fileWatcher is nil")
	}
}

func TestApp_WatcherEnabled_DisabledInConfig(t *testing.T) {
	cfg := &config.Config{
		Watcher: config.WatcherConfig{
			Enabled: false,
		},
	}
	app := &App{
		cfg: cfg,
	}

	enabled := app.WatcherEnabled()
	if enabled {
		t.Error("expected false when disabled in config")
	}
}

func TestApp_GitEnabled_NilTracker(t *testing.T) {
	cfg := &config.Config{
		Git: config.GitConfig{
			Enabled: true,
		},
	}
	app := &App{
		cfg:        cfg,
		gitTracker: nil,
	}

	enabled := app.GitEnabled()
	if enabled {
		t.Error("expected false when gitTracker is nil")
	}
}

func TestApp_GitEnabled_DisabledInConfig(t *testing.T) {
	cfg := &config.Config{
		Git: config.GitConfig{
			Enabled: false,
		},
	}
	app := &App{
		cfg: cfg,
	}

	enabled := app.GitEnabled()
	if enabled {
		t.Error("expected false when disabled in config")
	}
}

// --- ClaudeAgentAdapter Tests ---

func TestClaudeAgentAdapter_NilManager(t *testing.T) {
	adapter := NewClaudeAgentAdapter(nil)

	// All methods should handle nil manager gracefully

	ctx := context.Background()

	err := adapter.StartWithSession(ctx, "test", methods.SessionModeNew, "", "")
	if err != nil {
		t.Errorf("StartWithSession with nil manager should return nil, got %v", err)
	}

	err = adapter.Stop(ctx)
	if err != nil {
		t.Errorf("Stop with nil manager should return nil, got %v", err)
	}

	err = adapter.SendResponse("tool-id", "response", false)
	if err != nil {
		t.Errorf("SendResponse with nil manager should return nil, got %v", err)
	}

	state := adapter.State()
	if state != methods.AgentStateIdle {
		t.Errorf("State with nil manager should return idle, got %v", state)
	}

	pid := adapter.PID()
	if pid != 0 {
		t.Errorf("PID with nil manager should return 0, got %d", pid)
	}

	sessionID := adapter.SessionID()
	if sessionID != "" {
		t.Errorf("SessionID with nil manager should return empty, got %s", sessionID)
	}

	agentType := adapter.AgentType()
	if agentType != "claude" {
		t.Errorf("AgentType should return claude, got %s", agentType)
	}
}

// --- GitProviderAdapter Tests ---

func TestGitProviderAdapter_NilTracker(t *testing.T) {
	adapter := NewGitProviderAdapter(nil)
	ctx := context.Background()

	// All methods should handle nil tracker gracefully

	status, err := adapter.Status(ctx)
	if err != nil {
		t.Errorf("Status with nil tracker should return nil error, got %v", err)
	}
	if status.Branch != "" {
		t.Errorf("Status with nil tracker should return empty status, got branch %s", status.Branch)
	}

	diff, isStaged, isNew, err := adapter.Diff(ctx, "file.go")
	if err != nil {
		t.Errorf("Diff with nil tracker should return nil error, got %v", err)
	}
	if diff != "" || isStaged || isNew {
		t.Error("Diff with nil tracker should return empty values")
	}

	err = adapter.Stage(ctx, []string{"file.go"})
	if err != nil {
		t.Errorf("Stage with nil tracker should return nil, got %v", err)
	}

	err = adapter.Unstage(ctx, []string{"file.go"})
	if err != nil {
		t.Errorf("Unstage with nil tracker should return nil, got %v", err)
	}

	err = adapter.Discard(ctx, []string{"file.go"})
	if err != nil {
		t.Errorf("Discard with nil tracker should return nil, got %v", err)
	}

	commitResult, err := adapter.Commit(ctx, "test message", false)
	if err != nil {
		t.Errorf("Commit with nil tracker should return nil error, got %v", err)
	}
	if commitResult == nil || commitResult.Success {
		t.Errorf("Commit with nil tracker should return unsuccessful result")
	}

	pushResult, err := adapter.Push(ctx)
	if err != nil {
		t.Errorf("Push with nil tracker should return nil error, got %v", err)
	}
	if pushResult == nil || pushResult.Success {
		t.Errorf("Push with nil tracker should return unsuccessful result")
	}

	pullResult, err := adapter.Pull(ctx)
	if err != nil {
		t.Errorf("Pull with nil tracker should return nil error, got %v", err)
	}
	if pullResult == nil || pullResult.Success {
		t.Errorf("Pull with nil tracker should return unsuccessful result")
	}

	branchesResult, err := adapter.Branches(ctx)
	if err != nil {
		t.Errorf("Branches with nil tracker should return nil error, got %v", err)
	}
	if branchesResult == nil || len(branchesResult.Branches) != 0 {
		t.Errorf("Branches with nil tracker should return empty branches slice")
	}

	checkoutResult, err := adapter.Checkout(ctx, "main")
	if err != nil {
		t.Errorf("Checkout with nil tracker should return nil error, got %v", err)
	}
	if checkoutResult == nil || checkoutResult.Success {
		t.Errorf("Checkout with nil tracker should return unsuccessful result")
	}
}

// --- FileProviderAdapter Tests ---

func TestFileProviderAdapter_NilTracker(t *testing.T) {
	adapter := NewFileProviderAdapter(nil)
	ctx := context.Background()

	content, truncated, err := adapter.GetFileContent(ctx, "file.go", 100)
	if err != nil {
		t.Errorf("GetFileContent with nil tracker should return nil error, got %v", err)
	}
	if content != "" {
		t.Errorf("GetFileContent with nil tracker should return empty content, got %s", content)
	}
	if truncated {
		t.Error("GetFileContent with nil tracker should return truncated=false")
	}
}

// --- ClaudeSessionAdapter Tests ---

func TestClaudeSessionAdapter_NilCache(t *testing.T) {
	adapter := NewClaudeSessionAdapter(nil, nil, "/test/repo")
	ctx := context.Background()

	agentType := adapter.AgentType()
	if agentType != "claude" {
		t.Errorf("AgentType should return claude, got %s", agentType)
	}

	sessions, err := adapter.ListSessions(ctx, "")
	if err != nil {
		t.Errorf("ListSessions with nil cache should return nil error, got %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("ListSessions with nil cache should return empty slice, got %d", len(sessions))
	}

	session, err := adapter.GetSession(ctx, "test-session")
	if err != nil {
		t.Errorf("GetSession with nil cache should return nil error, got %v", err)
	}
	if session != nil {
		t.Error("GetSession with nil cache should return nil session")
	}
}

func TestClaudeSessionAdapter_NilMessageCache(t *testing.T) {
	adapter := NewClaudeSessionAdapter(nil, nil, "/test/repo")
	ctx := context.Background()

	messages, total, err := adapter.GetSessionMessages(ctx, "test-session", 10, 0, "asc")
	if err != nil {
		t.Errorf("GetSessionMessages with nil messageCache should return nil error, got %v", err)
	}
	if len(messages) != 0 {
		t.Errorf("GetSessionMessages with nil messageCache should return empty slice, got %d", len(messages))
	}
	if total != 0 {
		t.Errorf("GetSessionMessages with nil messageCache should return total 0, got %d", total)
	}
}

func TestClaudeSessionAdapter_GetSessionMessages_Integration(t *testing.T) {
	// Create temp sessions dir
	sessionsDir, err := os.MkdirTemp("", "cdev-sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(sessionsDir) }()

	// Create a session file
	sessionID := "test-session-1"
	filePath := filepath.Join(sessionsDir, sessionID+".jsonl")

	// Create sample messages
	messages := []map[string]interface{}{
		{
			"type":      "user",
			"uuid":      "uuid-1",
			"timestamp": time.Now().Format(time.RFC3339),
			"message": map[string]interface{}{
				"content": "Hello",
			},
		},
		{
			"type":      "assistant",
			"uuid":      "uuid-2",
			"timestamp": time.Now().Add(time.Second).Format(time.RFC3339),
			"message": map[string]interface{}{
				"content": "Hi there",
			},
		},
	}

	f, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range messages {
		data, _ := json.Marshal(msg)
		_, _ = f.Write(data)
		_, _ = f.WriteString("\n")
	}
	_ = f.Close()

	// Initialize MessageCache
	msgCache, err := sessioncache.NewMessageCache(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}

	// Initialize Adapter
	adapter := NewClaudeSessionAdapter(nil, msgCache, "/repo")
	ctx := context.Background()

	// Test GetSessionMessages
	result, total, err := adapter.GetSessionMessages(ctx, sessionID, 10, 0, "asc")
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}

	if total != 2 {
		t.Errorf("Expected total 2, got %d", total)
	}
	if len(result) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(result))
	}

	// Verify raw message format matching HTTP API
	if len(result) > 0 {
		if result[0].Type != "user" {
			t.Errorf("Expected first message type 'user', got '%s'", result[0].Type)
		}
		if result[0].UUID != "uuid-1" {
			t.Errorf("Expected first message UUID 'uuid-1', got '%s'", result[0].UUID)
		}
		// Verify Message contains raw content
		if result[0].Message == nil {
			t.Error("Expected first message Message field to be non-nil")
		}
	}
	if len(result) > 1 {
		if result[1].Type != "assistant" {
			t.Errorf("Expected second message type 'assistant', got '%s'", result[1].Type)
		}
		if result[1].UUID != "uuid-2" {
			t.Errorf("Expected second message UUID 'uuid-2', got '%s'", result[1].UUID)
		}
	}
}

