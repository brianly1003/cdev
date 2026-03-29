package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/brianly1003/cdev/internal/workspace"
)

type stubWorkspaceLookup struct {
	workspaces map[string]*workspace.Workspace
}

func (s *stubWorkspaceLookup) GetWorkspace(workspaceID string) (*workspace.Workspace, error) {
	ws, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, fmt.Errorf("workspace %s not found", workspaceID)
	}
	return ws, nil
}

func TestCreateWorktreeUsesClaudeWorktreesDirectory(t *testing.T) {
	repoPath := initGitRepo(t)
	workspaceID := "ws-lazy"

	ws := workspace.NewWorkspace(config.WorkspaceDefinition{
		ID:           workspaceID,
		Name:         "Lazy",
		Path:         repoPath,
		CreatedAt:    time.Now().UTC(),
		LastAccessed: time.Now().UTC(),
	})

	spawner := &Spawner{
		workspaceLookup: &stubWorkspaceLookup{
			workspaces: map[string]*workspace.Workspace{
				workspaceID: ws,
			},
		},
	}

	agentTask := task.NewTask(workspaceID, task.TaskTypeFixIssue, "Feature Auth", "Implement auth flow")

	worktreePath, branchName, err := spawner.createWorktree(agentTask)
	if err != nil {
		t.Fatalf("createWorktree returned error: %v", err)
	}

	wantParent := filepath.Join(repoPath, ".claude", "worktrees")
	if gotParent := filepath.Dir(worktreePath); gotParent != wantParent {
		t.Fatalf("worktree parent = %q, want %q", gotParent, wantParent)
	}
	if branchName == "" || branchName == "(detached)" {
		t.Fatalf("branchName = %q, want a created branch name", branchName)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("expected worktree path to exist: %v", err)
	}

	spawner.cleanupWorktree(worktreePath)

	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("expected worktree path to be removed, got err=%v", err)
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	repoPath := t.TempDir()
	runGit(t, repoPath, "init")
	runGit(t, repoPath, "config", "user.name", "cdev-test")
	runGit(t, repoPath, "config", "user.email", "cdev-test@example.com")

	readmePath := filepath.Join(repoPath, "README.md")
	if err := os.WriteFile(readmePath, []byte("hello\n"), 0644); err != nil {
		t.Fatalf("failed to write seed file: %v", err)
	}

	runGit(t, repoPath, "add", "README.md")
	runGit(t, repoPath, "commit", "-m", "init")

	return repoPath
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
