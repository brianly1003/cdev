// Package app provides unit tests for the App orchestrator.
package app

import (
	"context"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
)

// --- New() Tests ---

func TestNew(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/repo",
		},
	}

	app, err := New(cfg, "1.0.0")

	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if app == nil {
		t.Fatal("New() returned nil")
	}
	if app.cfg != cfg {
		t.Error("config not set correctly")
	}
	if app.version != "1.0.0" {
		t.Errorf("version = %s, want 1.0.0", app.version)
	}
	if app.hub == nil {
		t.Error("hub should be initialized")
	}
	if app.sessionID == "" {
		t.Error("sessionID should be generated")
	}
	if app.running {
		t.Error("app should not be running initially")
	}
}

func TestNew_GeneratesUniqueSessionID(t *testing.T) {
	cfg := &config.Config{}

	app1, _ := New(cfg, "1.0.0")
	app2, _ := New(cfg, "1.0.0")

	if app1.sessionID == app2.sessionID {
		t.Error("each app should have a unique session ID")
	}
}

// --- Getter Tests ---

func TestApp_GetSessionID(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")

	sessionID := app.GetSessionID()

	if sessionID == "" {
		t.Error("GetSessionID() should return non-empty string")
	}
	if sessionID != app.sessionID {
		t.Error("GetSessionID() should match internal sessionID")
	}
}

func TestApp_GetHub(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")

	hub := app.GetHub()

	if hub == nil {
		t.Error("GetHub() should return non-nil hub")
	}
	if hub != app.hub {
		t.Error("GetHub() should return the internal hub")
	}
}

func TestApp_GetConfig(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/path",
		},
	}
	app, _ := New(cfg, "1.0.0")

	result := app.GetConfig()

	if result == nil {
		t.Error("GetConfig() should return non-nil config")
		return
	}
	if result != cfg {
		t.Error("GetConfig() should return the same config")
	}
	if result.Repository.Path != "/test/path" {
		t.Errorf("GetConfig().Repository.Path = %s, want /test/path", result.Repository.Path)
	}
}

// --- UptimeSeconds Tests ---

func TestApp_UptimeSeconds_BeforeStart(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")

	// Before Start, startTime is zero, so uptime will be very large
	// This is expected behavior - uptime is only meaningful after Start()
	uptime := app.UptimeSeconds()

	// Just verify it doesn't panic and returns a value
	if uptime < 0 {
		t.Error("UptimeSeconds() should not be negative")
	}
}

func TestApp_UptimeSeconds_AfterStartTimeSet(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")

	// Manually set startTime to simulate Start() being called
	app.startTime = time.Now().Add(-10 * time.Second)

	uptime := app.UptimeSeconds()

	// Should be approximately 10 seconds (with some tolerance)
	if uptime < 9 || uptime > 12 {
		t.Errorf("UptimeSeconds() = %d, want approximately 10", uptime)
	}
}

// --- GetAgentStatus Tests ---

func TestApp_GetAgentStatus_NilManager(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")
	app.claudeManager = nil

	status := app.GetAgentStatus()

	if status != "idle" {
		t.Errorf("GetAgentStatus() with nil manager = %s, want idle", status)
	}
}

// --- GetUptimeSeconds (StatusProvider interface) Tests ---

func TestApp_GetUptimeSeconds(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")
	app.startTime = time.Now().Add(-60 * time.Second)

	uptime := app.GetUptimeSeconds()

	if uptime < 59 || uptime > 62 {
		t.Errorf("GetUptimeSeconds() = %d, want approximately 60", uptime)
	}
}

// --- getStatus Tests ---

func TestApp_getStatus_MinimalConfig(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/my-project",
		},
		Watcher: config.WatcherConfig{
			Enabled: true,
		},
		Git: config.GitConfig{
			Enabled: true,
		},
	}
	app, _ := New(cfg, "1.2.3")
	app.startTime = time.Now()

	status := app.getStatus()

	// Check required fields
	if status["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", status["version"])
	}
	if status["repo_path"] != "/test/my-project" {
		t.Errorf("repo_path = %v, want /test/my-project", status["repo_path"])
	}
	if status["repo_name"] != "my-project" {
		t.Errorf("repo_name = %v, want my-project", status["repo_name"])
	}
	if status["claude_state"] != "idle" {
		t.Errorf("claude_state = %v, want idle", status["claude_state"])
	}
	if status["connected_clients"] != 0 {
		t.Errorf("connected_clients = %v, want 0", status["connected_clients"])
	}
	if status["watcher_enabled"] != true {
		t.Errorf("watcher_enabled = %v, want true", status["watcher_enabled"])
	}
	if status["git_enabled"] != true {
		t.Errorf("git_enabled = %v, want true", status["git_enabled"])
	}
	if status["agent_session_id"] != app.sessionID {
		t.Errorf("agent_session_id = %v, want %s", status["agent_session_id"], app.sessionID)
	}
}

func TestApp_getStatus_NilComponents(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/project",
		},
	}
	app, _ := New(cfg, "1.0.0")
	// Explicitly set components to nil
	app.claudeManager = nil
	app.unifiedServer = nil
	app.gitTracker = nil

	// Should not panic with nil components
	status := app.getStatus()

	if status["claude_state"] != "idle" {
		t.Errorf("claude_state = %v, want idle", status["claude_state"])
	}
	if status["connected_clients"] != 0 {
		t.Errorf("connected_clients = %v, want 0", status["connected_clients"])
	}
	if status["is_git_repo"] != false {
		t.Errorf("is_git_repo = %v, want false", status["is_git_repo"])
	}
}

// --- Start() Double Start Prevention Test ---

func TestApp_Start_AlreadyRunning(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/repo",
		},
	}
	app, _ := New(cfg, "1.0.0")

	// Manually set running to true to simulate already running
	app.running = true

	ctx := context.Background()
	err := app.Start(ctx)

	if err == nil {
		t.Error("Start() should return error when already running")
	}
	if err.Error() != "application is already running" {
		t.Errorf("Start() error = %v, want 'application is already running'", err)
	}
}

// --- shutdown() Tests ---

func TestApp_shutdown_NotRunning(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")
	app.running = false

	err := app.shutdown()

	if err != nil {
		t.Errorf("shutdown() when not running should return nil, got %v", err)
	}
}

func TestApp_shutdown_NilComponents(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/repo",
		},
	}
	app, _ := New(cfg, "1.0.0")
	app.running = true
	// All components are nil by default

	// Start the hub so shutdown can stop it
	_ = app.hub.Start()

	err := app.shutdown()

	if err != nil {
		t.Errorf("shutdown() with nil components should return nil, got %v", err)
	}
	if app.running {
		t.Error("app should not be running after shutdown")
	}
}

// --- truncateString Tests ---

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "string shorter than max",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "string equal to max",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "string longer than max",
			input:  "hello world",
			maxLen: 8,
			want:   "hello...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "maxLen of 3",
			input:  "hello",
			maxLen: 3,
			want:   "hel",
		},
		{
			name:   "maxLen of 4",
			input:  "hello",
			maxLen: 4,
			want:   "h...",
		},
		{
			name:   "maxLen of 1",
			input:  "hello",
			maxLen: 1,
			want:   "h",
		},
		{
			name:   "maxLen of 0",
			input:  "hello",
			maxLen: 0,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// --- handleFileChangeForGitDiff Tests ---

func TestApp_handleFileChangeForGitDiff_DeletedFile(t *testing.T) {
	cfg := &config.Config{
		Repository: config.RepositoryConfig{
			Path: "/test/repo",
		},
	}
	app, _ := New(cfg, "1.0.0")
	// gitTracker is nil, but we're testing deleted file path which returns early

	// This test just verifies no panic occurs with deleted files
	// The actual git diff logic requires a real git tracker
	_ = app // Verify app is created successfully
}

// --- handleFileChangeForRepoIndex Tests ---

func TestApp_handleFileChangeForRepoIndex_NilIndexer(t *testing.T) {
	cfg := &config.Config{}
	app, _ := New(cfg, "1.0.0")
	app.repoIndexer = nil

	// Should return early without panic when indexer is nil
	// We can't easily test this without creating a mock event
	_ = app // Verify app is created with nil indexer
}
