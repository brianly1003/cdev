package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
)

func TestConfigManager_ResolveWorkspaceByWorktreePath(t *testing.T) {
	rootPath, worktreePath := createGitRepoWithWorktree(t)

	cfg := &config.WorkspacesConfig{
		Workspaces: []config.WorkspaceDefinition{
			{
				ID:        "workspace-root",
				Name:      "Root Repo",
				Path:      rootPath,
				CreatedAt: time.Now().UTC(),
			},
		},
	}

	manager := NewConfigManager(cfg, filepath.Join(t.TempDir(), "workspaces.yaml"))

	resolved, err := manager.ResolveWorkspace(worktreePath)
	if err != nil {
		t.Fatalf("ResolveWorkspace(worktreePath) failed: %v", err)
	}
	if resolved.Definition.ID != "workspace-root" {
		t.Fatalf("resolved workspace ID = %s, want workspace-root", resolved.Definition.ID)
	}
}

func createGitRepoWithWorktree(t *testing.T) (string, string) {
	t.Helper()

	baseDir, err := os.MkdirTemp("/tmp", "cdev-worktree-config-*")
	if err != nil {
		t.Fatalf("failed to create base dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(baseDir) })

	rootPath := filepath.Join(baseDir, "repo")
	if err := os.MkdirAll(rootPath, 0o755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	runGit(t, rootPath, "init")
	if err := os.WriteFile(filepath.Join(rootPath, "README.md"), []byte("root\n"), 0o644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	runGit(t, rootPath, "add", "README.md")
	runGit(t, rootPath, "-c", "user.name=Test User", "-c", "user.email=test@example.com", "commit", "-m", "init")

	worktreePath := filepath.Join(baseDir, "repo-worktree")
	runGit(t, rootPath, "worktree", "add", "-b", "feature/worktree-test", worktreePath)

	return rootPath, worktreePath
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}
