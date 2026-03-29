package session

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/gitutil"
	"github.com/brianly1003/cdev/internal/testutil"
	"github.com/brianly1003/cdev/internal/workspace"
)

type staticHistoricalPathResolver struct {
	paths map[string]string
}

func (s staticHistoricalPathResolver) ResolveHistoricalSessionProjectPath(workspaceID, sessionID string) (string, bool, error) {
	path, ok := s.paths[workspaceID+"|"+sessionID]
	return path, ok, nil
}

func TestManager_WorktreeHistoryAndMessagesResolveAcrossRepoFamily(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	rootPath, worktreePath := createSessionGitRepoWithWorktree(t)
	rootPath = gitutil.NormalizePath(rootPath)
	worktreePath = gitutil.NormalizePath(worktreePath)
	manager := newSessionTestManagerForPath(t, "workspace-worktree-history", rootPath)

	rootSessionID := "550e8400-e29b-41d4-a716-446655440010"
	worktreeSessionID := "550e8400-e29b-41d4-a716-446655440011"
	content := `{"type":"user","message":{"role":"user","content":"Hello from worktree"}}
{"type":"assistant","message":{"role":"assistant","content":"Hi there"}}`

	createClaudeSessionFileWithContent(t, rootPath, rootSessionID, content)
	createClaudeSessionFileWithContent(t, worktreePath, worktreeSessionID, content)

	history, err := manager.ListHistory("workspace-worktree-history", 0)
	if err != nil {
		t.Fatalf("ListHistory failed: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history count = %d, want 2", len(history))
	}

	projectBySessionID := make(map[string]string, len(history))
	for _, item := range history {
		projectBySessionID[item.SessionID] = item.ProjectPath
	}

	if got := projectBySessionID[rootSessionID]; got != rootPath {
		t.Fatalf("root session project_path = %q, want %q", got, rootPath)
	}
	if got := projectBySessionID[worktreeSessionID]; got != worktreePath {
		t.Fatalf("worktree session project_path = %q, want %q", got, worktreePath)
	}

	messages, err := manager.GetSessionMessages("workspace-worktree-history", worktreeSessionID, 10, 0, "asc")
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if messages.SessionID != worktreeSessionID {
		t.Fatalf("messages session_id = %q, want %q", messages.SessionID, worktreeSessionID)
	}
	if len(messages.Messages) != 2 {
		t.Fatalf("messages count = %d, want 2", len(messages.Messages))
	}

	info, err := manager.WatchWorkspaceSession("client-worktree", "workspace-worktree-history", worktreeSessionID)
	if err != nil {
		t.Fatalf("WatchWorkspaceSession failed: %v", err)
	}
	if info.SessionID != worktreeSessionID {
		t.Fatalf("watch session_id = %q, want %q", info.SessionID, worktreeSessionID)
	}
}

func TestManager_GetSessionMessagesFallsBackToHistoricalPathResolver(t *testing.T) {
	homeDir, err := os.MkdirTemp("/tmp", "cdev-session-home-*")
	if err != nil {
		t.Fatalf("failed to create home dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(homeDir) })
	t.Setenv("HOME", homeDir)

	rootBaseDir, err := os.MkdirTemp("/tmp", "cdev-session-root-*")
	if err != nil {
		t.Fatalf("failed to create root dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(rootBaseDir) })

	rootPath := gitutil.NormalizePath(filepath.Join(rootBaseDir, "repo"))
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	manager := newSessionTestManagerForPath(t, "workspace-worktree-fallback", rootPath)

	deletedWorktreePath := gitutil.NormalizePath(filepath.Join(rootPath, ".claude", "worktrees", "deleted-task"))
	sessionID := "66cfa725-4112-4a44-945b-368c600c08b2"
	content := `{"type":"user","message":{"role":"user","content":"Hello from deleted worktree"}}`

	createClaudeSessionFileWithContent(t, deletedWorktreePath, sessionID, content)
	manager.SetHistoricalSessionProjectPathResolver(staticHistoricalPathResolver{
		paths: map[string]string{
			"workspace-worktree-fallback|" + sessionID: deletedWorktreePath,
		},
	})

	messages, err := manager.GetSessionMessages("workspace-worktree-fallback", sessionID, 10, 0, "asc")
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if messages.SessionID != sessionID {
		t.Fatalf("messages session_id = %q, want %q", messages.SessionID, sessionID)
	}
	if len(messages.Messages) != 1 {
		t.Fatalf("messages count = %d, want 1", len(messages.Messages))
	}

	info, err := manager.WatchWorkspaceSession("client-fallback", "workspace-worktree-fallback", sessionID)
	if err != nil {
		t.Fatalf("WatchWorkspaceSession failed: %v", err)
	}
	if info.SessionID != sessionID {
		t.Fatalf("watch session_id = %q, want %q", info.SessionID, sessionID)
	}
}

func newSessionTestManagerForPath(t *testing.T, workspaceID, workspacePath string) *Manager {
	t.Helper()

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

func createClaudeSessionFileWithContent(t *testing.T, projectPath, sessionID, content string) {
	t.Helper()

	sessionsDir := getSessionsDir(gitutil.NormalizePath(projectPath))
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("failed to create sessions dir: %v", err)
	}

	sessionPath := filepath.Join(sessionsDir, sessionID+".jsonl")
	if err := os.WriteFile(sessionPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to create session file: %v", err)
	}
}

func createSessionGitRepoWithWorktree(t *testing.T) (string, string) {
	t.Helper()

	baseDir, err := os.MkdirTemp("/tmp", "cdev-session-worktree-*")
	if err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(baseDir) })

	rootPath := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	runSessionGit(t, rootPath, "init")
	if err := os.WriteFile(filepath.Join(rootPath, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runSessionGit(t, rootPath, "add", "README.md")
	runSessionGit(t, rootPath, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")

	worktreePath := filepath.Join(baseDir, "repo-worktree")
	runSessionGit(t, rootPath, "worktree", "add", "-b", "feature/worktree-history", worktreePath)

	return rootPath, worktreePath
}

func runSessionGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
