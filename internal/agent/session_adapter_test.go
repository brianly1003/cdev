package agent

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveClaudeNativeWorktreeLaunch(t *testing.T) {
	workspaceRoot := filepath.Clean("/Users/brianly/Projects/Lazy")
	projectPath := filepath.Join(workspaceRoot, ".claude", "worktrees", "feature-auth")

	launchDir, launchArgs, ok := resolveClaudeNativeWorktreeLaunch(workspaceRoot, projectPath)
	if !ok {
		t.Fatal("expected Claude native worktree launch to be detected")
	}
	if launchDir != workspaceRoot {
		t.Fatalf("launchDir = %q, want %q", launchDir, workspaceRoot)
	}

	wantArgs := []string{"-w", "feature-auth", "--dangerously-skip-permissions"}
	if !reflect.DeepEqual(launchArgs, wantArgs) {
		t.Fatalf("launchArgs = %v, want %v", launchArgs, wantArgs)
	}
}

func TestResolveClaudeNativeWorktreeLaunchRejectsNonClaudeWorktreePaths(t *testing.T) {
	workspaceRoot := filepath.Clean("/Users/brianly/Projects/Lazy")

	cases := []string{
		"/tmp/cdev/worktrees/feature-auth",
		filepath.Join(workspaceRoot, ".claude", "worktrees", "feature-auth", "nested"),
		filepath.Join(workspaceRoot, "feature-auth"),
	}

	for _, projectPath := range cases {
		t.Run(projectPath, func(t *testing.T) {
			if _, _, ok := resolveClaudeNativeWorktreeLaunch(workspaceRoot, projectPath); ok {
				t.Fatalf("expected %q to be rejected", projectPath)
			}
		})
	}
}

func TestResolveClaudeWorktreeRepoPath(t *testing.T) {
	worktreePath := filepath.Join("/Users/brianly/Projects/Lazy", ".claude", "worktrees", "feature-auth")

	repoPath, ok := resolveClaudeWorktreeRepoPath(worktreePath)
	if !ok {
		t.Fatal("expected repo path resolution to succeed")
	}
	if repoPath != filepath.Clean("/Users/brianly/Projects/Lazy") {
		t.Fatalf("repoPath = %q, want %q", repoPath, filepath.Clean("/Users/brianly/Projects/Lazy"))
	}
}

func TestResolveClaudeWorktreeRepoPathRejectsNonClaudePaths(t *testing.T) {
	if _, ok := resolveClaudeWorktreeRepoPath("/tmp/cdev/worktrees/feature-auth"); ok {
		t.Fatal("expected non-Claude worktree path to be rejected")
	}
}
