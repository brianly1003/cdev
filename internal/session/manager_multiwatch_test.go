package session

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/testutil"
	"github.com/brianly1003/cdev/internal/workspace"
)

func TestWatchWorkspaceSession_AllowsConcurrentSessionsPerClient(t *testing.T) {
	workspaceID := "workspace-multi-watch"
	workspacePath := t.TempDir()
	manager := newSessionTestManager(t, workspaceID, workspacePath)

	const (
		sessionA = "a-session"
		sessionB = "b-session"
		clientID = "client-a"
	)

	createClaudeSessionFile(t, workspacePath, sessionA)
	createClaudeSessionFile(t, workspacePath, sessionB)

	if _, err := manager.WatchWorkspaceSession(clientID, workspaceID, sessionA); err != nil {
		t.Fatalf("watch sessionA failed: %v", err)
	}
	if _, err := manager.WatchWorkspaceSession(clientID, workspaceID, sessionB); err != nil {
		t.Fatalf("watch sessionB failed: %v", err)
	}

	keyA := watchedSessionKey{WorkspaceID: workspaceID, SessionID: sessionA}
	keyB := watchedSessionKey{WorkspaceID: workspaceID, SessionID: sessionB}

	manager.streamerMu.Lock()
	defer manager.streamerMu.Unlock()

	if _, ok := manager.streamerSessions[keyA]; !ok {
		t.Fatalf("expected streamer for %s to exist", sessionA)
	}
	if _, ok := manager.streamerSessions[keyB]; !ok {
		t.Fatalf("expected streamer for %s to exist", sessionB)
	}
	if len(manager.streamerClientSessions[clientID]) != 2 {
		t.Fatalf("client watched sessions = %d, want 2", len(manager.streamerClientSessions[clientID]))
	}
}

func TestUnwatchWorkspaceSession_RemovesOnlyOneSessionPerCall(t *testing.T) {
	workspaceID := "workspace-unwatch-isolation"
	workspacePath := t.TempDir()
	manager := newSessionTestManager(t, workspaceID, workspacePath)

	const (
		sessionA = "a-session"
		sessionB = "b-session"
		clientA  = "client-a"
		clientB  = "client-b"
	)

	createClaudeSessionFile(t, workspacePath, sessionA)
	createClaudeSessionFile(t, workspacePath, sessionB)

	if _, err := manager.WatchWorkspaceSession(clientA, workspaceID, sessionA); err != nil {
		t.Fatalf("watch sessionA by clientA failed: %v", err)
	}
	if _, err := manager.WatchWorkspaceSession(clientA, workspaceID, sessionB); err != nil {
		t.Fatalf("watch sessionB by clientA failed: %v", err)
	}
	if _, err := manager.WatchWorkspaceSession(clientB, workspaceID, sessionB); err != nil {
		t.Fatalf("watch sessionB by clientB failed: %v", err)
	}

	keyA := watchedSessionKey{WorkspaceID: workspaceID, SessionID: sessionA}
	keyB := watchedSessionKey{WorkspaceID: workspaceID, SessionID: sessionB}

	info1 := manager.UnwatchWorkspaceSession(clientA, sessionA)
	if info1.WorkspaceID != workspaceID || info1.SessionID != sessionA {
		t.Fatalf("first unwatch removed %s/%s, want %s/%s", info1.WorkspaceID, info1.SessionID, workspaceID, sessionA)
	}
	if info1.Watching {
		t.Fatalf("first unwatch should report Watching=false because sessionA has no remaining watchers")
	}

	manager.streamerMu.Lock()
	if _, ok := manager.streamerSessions[keyA]; ok {
		manager.streamerMu.Unlock()
		t.Fatalf("expected sessionA streamer to be removed")
	}
	streamB, ok := manager.streamerSessions[keyB]
	if !ok {
		manager.streamerMu.Unlock()
		t.Fatalf("expected sessionB streamer to remain")
	}
	if len(streamB.watchers) != 2 {
		manager.streamerMu.Unlock()
		t.Fatalf("sessionB watchers after first unwatch = %d, want 2", len(streamB.watchers))
	}
	manager.streamerMu.Unlock()

	info2 := manager.UnwatchWorkspaceSession(clientA, sessionB)
	if info2.WorkspaceID != workspaceID || info2.SessionID != sessionB {
		t.Fatalf("second unwatch removed %s/%s, want %s/%s", info2.WorkspaceID, info2.SessionID, workspaceID, sessionB)
	}
	if !info2.Watching {
		t.Fatalf("second unwatch should report Watching=true because clientB still watches sessionB")
	}

	manager.streamerMu.Lock()
	streamB, ok = manager.streamerSessions[keyB]
	if !ok {
		manager.streamerMu.Unlock()
		t.Fatalf("expected sessionB streamer to remain after clientA unwatch")
	}
	if len(streamB.watchers) != 1 || !streamB.watchers[clientB] {
		manager.streamerMu.Unlock()
		t.Fatalf("expected only clientB to remain watching sessionB")
	}
	manager.streamerMu.Unlock()

	info3 := manager.UnwatchWorkspaceSession(clientB, sessionB)
	if info3.WorkspaceID != workspaceID || info3.SessionID != sessionB {
		t.Fatalf("third unwatch removed %s/%s, want %s/%s", info3.WorkspaceID, info3.SessionID, workspaceID, sessionB)
	}
	if info3.Watching {
		t.Fatalf("third unwatch should report Watching=false after last watcher leaves")
	}

	manager.streamerMu.Lock()
	if len(manager.streamerSessions) != 0 {
		manager.streamerMu.Unlock()
		t.Fatalf("remaining streamers = %d, want 0", len(manager.streamerSessions))
	}
	manager.streamerMu.Unlock()
}

func TestUnwatchWorkspaceSession_UnknownSessionIDKeepsExistingWatch(t *testing.T) {
	workspaceID := "workspace-unwatch-unknown"
	workspacePath := t.TempDir()
	manager := newSessionTestManager(t, workspaceID, workspacePath)

	const (
		sessionA = "a-session"
		clientA  = "client-a"
	)

	createClaudeSessionFile(t, workspacePath, sessionA)

	if _, err := manager.WatchWorkspaceSession(clientA, workspaceID, sessionA); err != nil {
		t.Fatalf("watch sessionA by clientA failed: %v", err)
	}

	info := manager.UnwatchWorkspaceSession(clientA, "missing-session")
	if info.SessionID != "missing-session" {
		t.Fatalf("unwatch info session_id = %q, want %q", info.SessionID, "missing-session")
	}
	if !info.Watching {
		t.Fatalf("expected Watching=true when target session is not watched")
	}

	keyA := watchedSessionKey{WorkspaceID: workspaceID, SessionID: sessionA}
	manager.streamerMu.Lock()
	if _, ok := manager.streamerSessions[keyA]; !ok {
		manager.streamerMu.Unlock()
		t.Fatalf("expected sessionA streamer to remain after unknown-session unwatch")
	}
	if watched := manager.streamerClientSessions[clientA]; len(watched) != 1 {
		manager.streamerMu.Unlock()
		t.Fatalf("client watched session count = %d, want 1", len(watched))
	}
	manager.streamerMu.Unlock()
}

func newSessionTestManager(t *testing.T, workspaceID, workspacePath string) *Manager {
	t.Helper()

	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	workspacesPath := filepath.Join(t.TempDir(), "workspaces.yaml")
	def := config.WorkspaceDefinition{
		ID:        workspaceID,
		Name:      "Test Workspace",
		Path:      workspacePath,
		CreatedAt: time.Now().UTC(),
	}

	cfg := &config.WorkspacesConfig{
		Workspaces: []config.WorkspaceDefinition{def},
	}
	if err := config.SaveWorkspaces(workspacesPath, cfg); err != nil {
		t.Fatalf("failed to save workspaces config: %v", err)
	}

	cfgMgr := workspace.NewConfigManager(cfg, workspacesPath)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := NewManager(testutil.NewMockEventHub(), newSessionTestConfig(), logger)

	for _, ws := range cfgMgr.ListWorkspaces() {
		manager.RegisterWorkspace(ws)
	}

	t.Cleanup(func() {
		_ = manager.Stop()
	})

	return manager
}

func createClaudeSessionFile(t *testing.T, workspacePath, sessionID string) {
	t.Helper()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to resolve home directory: %v", err)
	}

	encodedPath := strings.ReplaceAll(filepath.Clean(workspacePath), "/", "-")
	sessionsDir := filepath.Join(homeDir, ".claude", "projects", encodedPath)
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("failed to create sessions dir: %v", err)
	}

	sessionPath := filepath.Join(sessionsDir, sessionID+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(""), 0o644); err != nil {
		t.Fatalf("failed to create session file: %v", err)
	}
}

func newSessionTestConfig() *config.Config {
	return &config.Config{
		Claude: config.ClaudeConfig{
			Command: "claude",
		},
		Git: config.GitConfig{
			Enabled: false,
			Command: "git",
		},
		Watcher: config.WatcherConfig{
			Enabled: false,
		},
	}
}
