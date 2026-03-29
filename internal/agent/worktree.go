package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/rs/zerolog/log"
)

// createWorktree creates an isolated git worktree for the task.
// Returns (worktreePath, branchName, error).
func (s *Spawner) createWorktree(t *task.AgentTask) (string, string, error) {
	if s.workspaceLookup == nil {
		return "", "", fmt.Errorf("workspace lookup is not configured")
	}

	ws, err := s.workspaceLookup.GetWorkspace(t.WorkspaceID)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve workspace %s: %w", t.WorkspaceID, err)
	}
	repoPath := ws.Definition.Path

	// Generate branch name
	branchName := generateBranchName(t)

	// Match Claude CLI's native worktree layout: <repo>/.claude/worktrees/<name>
	worktreeBase := filepath.Join(repoPath, ".claude", "worktrees")
	if err := os.MkdirAll(worktreeBase, 0755); err != nil {
		return "", "", fmt.Errorf("failed to create worktree base dir: %w", err)
	}

	worktreePath := filepath.Join(worktreeBase, t.ID[:8]+"-"+sanitizeName(t.Title))

	// Create the worktree — detached for read-only tasks (plan_case)
	var cmd *exec.Cmd
	if t.IsPlanCase() {
		cmd = exec.Command("git", "worktree", "add", "--detach", worktreePath)
		branchName = "(detached)"
	} else {
		cmd = exec.Command("git", "worktree", "add", "-b", branchName, worktreePath)
	}
	cmd.Dir = repoPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}

	log.Info().
		Str("task_id", t.ID).
		Str("repo_path", repoPath).
		Str("worktree", worktreePath).
		Str("branch", branchName).
		Msg("created git worktree")

	return worktreePath, branchName, nil
}

// cleanupWorktree removes a git worktree.
func (s *Spawner) cleanupWorktree(worktreePath string) {
	if worktreePath == "" {
		return
	}

	// Remove the worktree
	cmd := exec.Command("git", "worktree", "remove", "--force", worktreePath)
	if repoPath, ok := resolveClaudeWorktreeRepoPath(worktreePath); ok {
		cmd.Dir = repoPath
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Warn().
			Str("path", worktreePath).
			Str("output", string(output)).
			Err(err).
			Msg("failed to remove worktree")
		// Fallback: try to remove the directory
		if removeErr := os.RemoveAll(worktreePath); removeErr != nil {
			log.Error().
				Str("path", worktreePath).
				Err(removeErr).
				Msg("failed to remove worktree directory fallback")
		}
	}
}

func resolveClaudeWorktreeRepoPath(worktreePath string) (string, bool) {
	worktreePath = filepath.Clean(worktreePath)
	marker := filepath.Join(".claude", "worktrees")
	suffix := string(filepath.Separator) + marker + string(filepath.Separator)

	idx := strings.LastIndex(worktreePath, suffix)
	if idx == -1 {
		return "", false
	}

	return worktreePath[:idx], true
}

// generateBranchName creates a descriptive branch name from the task.
func generateBranchName(t *task.AgentTask) string {
	prefix := "agent"
	switch t.TaskType {
	case task.TaskTypeFixIssue:
		prefix = "fix"
	case task.TaskTypeFixReplay:
		prefix = "replay-fix"
	case task.TaskTypeImplementCR:
		prefix = "cr"
	case task.TaskTypeRefactor:
		prefix = "refactor"
	case task.TaskTypeAddTest:
		prefix = "test"
	case task.TaskTypePlanCase:
		prefix = "plan"
	}

	timestamp := time.Now().Format("0102-1504")
	name := sanitizeName(t.Title)
	if len(name) > 30 {
		name = name[:30]
	}

	return fmt.Sprintf("agent/%s/%s-%s", prefix, name, timestamp)
}

// sanitizeName converts a title to a valid git branch name component.
func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, name)

	// Collapse multiple dashes
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return strings.Trim(name, "-")
}
