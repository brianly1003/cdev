// Package git implements the Git CLI wrapper for tracking repository changes.
package git

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/domain"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/rs/zerolog/log"
)

// Tracker implements the GitTracker port interface.
type Tracker struct {
	repoPath string
	command  string
	hub      ports.EventHub

	mu       sync.RWMutex
	repoRoot string
	repoName string
	isRepo   bool
}

// NewTracker creates a new Git tracker.
func NewTracker(repoPath, command string, hub ports.EventHub) *Tracker {
	t := &Tracker{
		repoPath: repoPath,
		command:  command,
		hub:      hub,
	}

	// Check if this is a git repo and get root
	t.detectRepo()

	return t
}

// detectRepo checks if the path is a git repository.
func (t *Tracker) detectRepo() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get repo root
	cmd := exec.CommandContext(ctx, t.command, "rev-parse", "--show-toplevel")
	cmd.Dir = t.repoPath
	output, err := cmd.Output()
	if err != nil {
		t.isRepo = false
		// Get stderr for more details
		if exitErr, ok := err.(*exec.ExitError); ok {
			log.Warn().
				Str("path", t.repoPath).
				Str("command", t.command).
				Str("stderr", string(exitErr.Stderr)).
				Err(err).
				Msg("not a git repository")
		} else {
			log.Warn().
				Str("path", t.repoPath).
				Str("command", t.command).
				Err(err).
				Msg("not a git repository")
		}
		return
	}

	t.repoRoot = strings.TrimSpace(string(output))
	t.repoName = filepath.Base(t.repoRoot)
	t.isRepo = true

	log.Info().
		Str("root", t.repoRoot).
		Str("name", t.repoName).
		Msg("git repository detected")
}

// Status returns the current git status.
func (t *Tracker) Status(ctx context.Context) ([]ports.GitFileStatus, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "status", "--porcelain", "-uall")
	cmd.Dir = t.repoRoot

	output, err := cmd.Output()
	if err != nil {
		return nil, domain.NewGitError("status", err)
	}

	return t.parseStatus(string(output)), nil
}

// parseStatus parses git status porcelain output.
func (t *Tracker) parseStatus(output string) []ports.GitFileStatus {
	// Initialize to empty slice (not nil) so JSON marshals to [] not null
	files := make([]ports.GitFileStatus, 0)

	// Only trim trailing newline, NOT leading spaces (they are significant in porcelain format)
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for _, line := range lines {
		if len(line) < 4 {
			continue
		}

		// Format: XY PATH (where XY is 2 chars, then space(s), then path)
		// X = staged status, Y = unstaged status
		staged := line[0]
		unstaged := line[1]
		// Skip the status chars and trim any leading spaces to get the path
		path := strings.TrimLeft(line[2:], " ")

		// Handle renamed files (format: XY old -> new)
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}

		files = append(files, ports.GitFileStatus{
			Path:        path,
			Status:      string([]byte{staged, unstaged}),
			IsStaged:    staged != ' ' && staged != '?',
			IsUntracked: staged == '?' && unstaged == '?',
		})
	}

	return files
}

// Diff returns the diff for a specific file.
func (t *Tracker) Diff(ctx context.Context, path string) (string, error) {
	if !t.isRepo {
		return "", domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "diff", "--", path)
	cmd.Dir = t.repoRoot

	output, err := cmd.Output()
	if err != nil {
		// Empty diff is not an error
		if len(output) == 0 {
			return "", nil
		}
		return "", domain.NewGitError("diff", err)
	}

	return string(output), nil
}

// DiffStaged returns the staged diff for a specific file.
func (t *Tracker) DiffStaged(ctx context.Context, path string) (string, error) {
	if !t.isRepo {
		return "", domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "diff", "--cached", "--", path)
	cmd.Dir = t.repoRoot

	output, err := cmd.Output()
	if err != nil {
		if len(output) == 0 {
			return "", nil
		}
		return "", domain.NewGitError("diff --cached", err)
	}

	return string(output), nil
}

// DiffNewFile generates a diff-like output for untracked/new files.
// This reads the file content and formats it as if it were a git diff with all additions.
func (t *Tracker) DiffNewFile(ctx context.Context, path string) (string, error) {
	if !t.isRepo {
		return "", domain.ErrNotGitRepo
	}

	fullPath := filepath.Join(t.repoRoot, path)

	// Check if file exists
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("file not found: %s", path)
	}

	// Skip directories
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", path)
	}

	// Skip large files (> 1MB)
	if info.Size() > 1024*1024 {
		return fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1 @@\n+[File too large to display: %d bytes]\n", path, path, path, info.Size()), nil
	}

	// Read file content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Check if binary
	if isBinaryContent(content) {
		return fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\nBinary file %s\n", path, path, path), nil
	}

	// Generate diff-like output
	lines := strings.Split(string(content), "\n")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", path, path))
	sb.WriteString("new file mode 100644\n")
	sb.WriteString("--- /dev/null\n")
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", path))
	sb.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
	for _, line := range lines {
		sb.WriteString("+")
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

// isBinaryContent checks if content appears to be binary (contains null bytes).
func isBinaryContent(content []byte) bool {
	// Check first 8KB for null bytes
	checkLen := len(content)
	if checkLen > 8192 {
		checkLen = 8192
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return true
		}
	}
	return false
}

// DiffAll returns diffs for all changed files.
func (t *Tracker) DiffAll(ctx context.Context) (map[string]string, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	// Get list of changed files
	status, err := t.Status(ctx)
	if err != nil {
		return nil, err
	}

	diffs := make(map[string]string)

	for _, file := range status {
		if file.IsUntracked {
			// For untracked files, we can't get a diff
			continue
		}

		diff, err := t.Diff(ctx, file.Path)
		if err != nil {
			log.Warn().Err(err).Str("file", file.Path).Msg("failed to get diff")
			continue
		}

		if diff != "" {
			diffs[file.Path] = diff
		}

		// Also check staged diff
		stagedDiff, err := t.DiffStaged(ctx, file.Path)
		if err == nil && stagedDiff != "" && stagedDiff != diff {
			diffs[file.Path+" (staged)"] = stagedDiff
		}
	}

	return diffs, nil
}

// IsGitRepo checks if the configured path is a git repository.
func (t *Tracker) IsGitRepo() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isRepo
}

// GetRepoRoot returns the root path of the git repository.
func (t *Tracker) GetRepoRoot() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.repoRoot
}

// GetRepoName returns the name of the repository.
func (t *Tracker) GetRepoName() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.repoName
}

// GetDiffForEvent generates a git diff event for a file change.
func (t *Tracker) GetDiffForEvent(ctx context.Context, path string) *events.BaseEvent {
	if !t.isRepo {
		return nil
	}

	// Get both unstaged and staged diffs
	unstagedDiff, _ := t.Diff(ctx, path)
	stagedDiff, _ := t.DiffStaged(ctx, path)

	// Use unstaged diff if available, otherwise staged
	diff := unstagedDiff
	isStaged := false
	if diff == "" && stagedDiff != "" {
		diff = stagedDiff
		isStaged = true
	}

	if diff == "" {
		return nil
	}

	// Count additions and deletions
	additions, deletions := countDiffLines(diff)

	// Check if it's a new file
	isNewFile := strings.Contains(diff, "new file mode")

	return events.NewGitDiffEvent(path, diff, additions, deletions, isStaged, isNewFile)
}

// countDiffLines counts additions and deletions in a diff.
func countDiffLines(diff string) (additions, deletions int) {
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case '+':
			if !strings.HasPrefix(line, "+++") {
				additions++
			}
		case '-':
			if !strings.HasPrefix(line, "---") {
				deletions++
			}
		}
	}
	return
}

// GetFileContent returns the content of a file from the repository.
func (t *Tracker) GetFileContent(ctx context.Context, path string, maxSizeKB int) (string, bool, error) {
	fullPath := filepath.Join(t.repoRoot, path)

	// Validate path is within repo
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", false, err
	}

	absRoot, err := filepath.Abs(t.repoRoot)
	if err != nil {
		return "", false, err
	}

	if !strings.HasPrefix(absPath, absRoot) {
		return "", false, domain.ErrPathOutsideRepo
	}

	// Read file
	cmd := exec.CommandContext(ctx, "cat", fullPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", false, fmt.Errorf("failed to read file: %s", stderr.String())
	}

	content := stdout.String()

	// Check if truncation needed
	maxSize := maxSizeKB * 1024
	truncated := false
	if len(content) > maxSize {
		content = content[:maxSize]
		truncated = true
	}

	return content, truncated, nil
}

// EnhancedStatus represents enhanced git status with staging information.
type EnhancedStatus struct {
	Branch     string       `json:"branch"`
	Upstream   string       `json:"upstream,omitempty"`
	Ahead      int          `json:"ahead"`
	Behind     int          `json:"behind"`
	Staged     []FileEntry  `json:"staged"`
	Unstaged   []FileEntry  `json:"unstaged"`
	Untracked  []FileEntry  `json:"untracked"`
	Conflicted []FileEntry  `json:"conflicted"`
	RepoName   string       `json:"repo_name"`
	RepoRoot   string       `json:"repo_root"`
}

// FileEntry represents a file in git status with diff stats.
type FileEntry struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions,omitempty"`
	Deletions int    `json:"deletions,omitempty"`
}

// GetEnhancedStatus returns comprehensive git status including staging info.
func (t *Tracker) GetEnhancedStatus(ctx context.Context) (*EnhancedStatus, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	status := &EnhancedStatus{
		Staged:     make([]FileEntry, 0),
		Unstaged:   make([]FileEntry, 0),
		Untracked:  make([]FileEntry, 0),
		Conflicted: make([]FileEntry, 0),
		RepoName:   t.repoName,
		RepoRoot:   t.repoRoot,
	}

	// Get branch info
	branch, upstream, ahead, behind := t.getBranchInfo(ctx)
	status.Branch = branch
	status.Upstream = upstream
	status.Ahead = ahead
	status.Behind = behind

	// Get file status
	cmd := exec.CommandContext(ctx, t.command, "status", "--porcelain", "-uall")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, domain.NewGitError("status", err)
	}

	// Only trim trailing newline, NOT leading spaces (they are significant in porcelain format)
	lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
	for _, line := range lines {
		if len(line) < 3 {
			continue
		}

		staged := line[0]
		unstaged := line[1]
		path := strings.TrimLeft(line[2:], " ")

		// Handle renamed files
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}

		// Categorize files
		if staged == 'U' || unstaged == 'U' || (staged == 'A' && unstaged == 'A') || (staged == 'D' && unstaged == 'D') {
			// Conflicted
			status.Conflicted = append(status.Conflicted, FileEntry{
				Path:   path,
				Status: "!",
			})
		} else if staged == '?' && unstaged == '?' {
			// Untracked
			status.Untracked = append(status.Untracked, FileEntry{
				Path:   path,
				Status: "?",
			})
		} else {
			// Check staged changes
			if staged != ' ' && staged != '?' {
				entry := FileEntry{
					Path:   path,
					Status: string(staged),
				}
				// Get diff stats for staged
				additions, deletions := t.getDiffStats(ctx, path, true)
				entry.Additions = additions
				entry.Deletions = deletions
				status.Staged = append(status.Staged, entry)
			}
			// Check unstaged changes
			if unstaged != ' ' && unstaged != '?' {
				entry := FileEntry{
					Path:   path,
					Status: string(unstaged),
				}
				// Get diff stats for unstaged
				additions, deletions := t.getDiffStats(ctx, path, false)
				entry.Additions = additions
				entry.Deletions = deletions
				status.Unstaged = append(status.Unstaged, entry)
			}
		}
	}

	return status, nil
}

// getBranchInfo returns current branch, upstream, ahead/behind counts.
func (t *Tracker) getBranchInfo(ctx context.Context) (branch, upstream string, ahead, behind int) {
	// Get current branch
	cmd := exec.CommandContext(ctx, t.command, "branch", "--show-current")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err == nil {
		branch = strings.TrimSpace(string(output))
	}

	// Get upstream and ahead/behind
	cmd = exec.CommandContext(ctx, t.command, "rev-parse", "--abbrev-ref", "@{upstream}")
	cmd.Dir = t.repoRoot
	output, err = cmd.Output()
	if err == nil {
		upstream = strings.TrimSpace(string(output))

		// Get ahead/behind counts
		cmd = exec.CommandContext(ctx, t.command, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
		cmd.Dir = t.repoRoot
		output, err = cmd.Output()
		if err == nil {
			parts := strings.Fields(strings.TrimSpace(string(output)))
			if len(parts) == 2 {
				_, _ = fmt.Sscanf(parts[0], "%d", &ahead)
				_, _ = fmt.Sscanf(parts[1], "%d", &behind)
			}
		}
	}

	return
}

// getDiffStats returns additions and deletions for a file.
func (t *Tracker) getDiffStats(ctx context.Context, path string, staged bool) (additions, deletions int) {
	args := []string{"diff", "--numstat"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", path)

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return 0, 0
	}

	line := strings.TrimSpace(string(output))
	parts := strings.Fields(line)
	if len(parts) >= 2 {
		_, _ = fmt.Sscanf(parts[0], "%d", &additions)
		_, _ = fmt.Sscanf(parts[1], "%d", &deletions)
	}

	return
}

// Stage stages files for commit.
func (t *Tracker) Stage(ctx context.Context, paths []string) error {
	if !t.isRepo {
		return domain.ErrNotGitRepo
	}

	args := append([]string{"add", "--"}, paths...)
	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %s", stderr.String())
	}

	log.Info().Strs("paths", paths).Msg("staged files")
	return nil
}

// Unstage removes files from staging area.
func (t *Tracker) Unstage(ctx context.Context, paths []string) error {
	if !t.isRepo {
		return domain.ErrNotGitRepo
	}

	// Check if HEAD exists (first commit scenario)
	checkCmd := exec.CommandContext(ctx, t.command, "rev-parse", "HEAD")
	checkCmd.Dir = t.repoRoot
	headExists := checkCmd.Run() == nil

	var cmd *exec.Cmd
	var stderr bytes.Buffer

	if !headExists {
		// No HEAD (initial commit) - use git rm --cached
		args := append([]string{"rm", "--cached", "-r", "--"}, paths...)
		cmd = exec.CommandContext(ctx, t.command, args...)
	} else {
		// HEAD exists - git reset works for both new and modified files
		args := append([]string{"reset", "HEAD", "--"}, paths...)
		cmd = exec.CommandContext(ctx, t.command, args...)
	}

	cmd.Dir = t.repoRoot
	cmd.Stderr = &stderr

	log.Debug().
		Strs("paths", paths).
		Bool("head_exists", headExists).
		Str("command", cmd.String()).
		Msg("running unstage command")

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		log.Error().
			Err(err).
			Str("stderr", errMsg).
			Strs("paths", paths).
			Msg("unstage command failed")
		if headExists {
			return fmt.Errorf("git reset failed: %s", errMsg)
		}
		return fmt.Errorf("git rm --cached failed: %s", errMsg)
	}

	log.Info().Strs("paths", paths).Msg("unstaged files")
	return nil
}

// Discard discards uncommitted changes (restore files to last commit).
// Handles tracked files (checkout), staged files (unstage + checkout), and untracked files (delete).
func (t *Tracker) Discard(ctx context.Context, paths []string) error {
	if !t.isRepo {
		return domain.ErrNotGitRepo
	}

	// Get the status of all files to determine how to handle each
	status, err := t.Status(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	// Build a map of file statuses
	fileStatus := make(map[string]string)
	for _, f := range status {
		fileStatus[f.Path] = f.Status
	}

	var trackedFiles []string
	var untrackedFiles []string
	var stagedFiles []string

	for _, path := range paths {
		st, exists := fileStatus[path]
		if !exists {
			// File not in status - might be a wrong path or already clean
			continue
		}

		// Check the two-character status code
		// First char = index (staged), Second char = worktree
		// '?' = untracked, ' ' = unmodified, 'M' = modified, 'A' = added, 'D' = deleted
		if len(st) >= 2 {
			indexStatus := st[0]
			worktreeStatus := st[1]

			if st == "??" {
				// Untracked file - need to delete
				untrackedFiles = append(untrackedFiles, path)
			} else if indexStatus != ' ' && indexStatus != '?' {
				// Has staged changes - need to unstage first
				stagedFiles = append(stagedFiles, path)
				if worktreeStatus != ' ' {
					// Also has unstaged changes
					trackedFiles = append(trackedFiles, path)
				}
			} else if worktreeStatus != ' ' {
				// Only unstaged changes
				trackedFiles = append(trackedFiles, path)
			}
		} else if st == "?" {
			// Simple untracked marker
			untrackedFiles = append(untrackedFiles, path)
		}
	}

	var errors []string

	// 1. Unstage staged files first
	if len(stagedFiles) > 0 {
		args := append([]string{"reset", "HEAD", "--"}, stagedFiles...)
		cmd := exec.CommandContext(ctx, t.command, args...)
		cmd.Dir = t.repoRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errors = append(errors, fmt.Sprintf("unstage failed: %s", stderr.String()))
		}
	}

	// 2. Checkout tracked files with modifications
	allTracked := append(trackedFiles, stagedFiles...)
	if len(allTracked) > 0 {
		args := append([]string{"checkout", "--"}, allTracked...)
		cmd := exec.CommandContext(ctx, t.command, args...)
		cmd.Dir = t.repoRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errors = append(errors, fmt.Sprintf("checkout failed: %s", stderr.String()))
		}
	}

	// 3. Delete untracked files
	for _, path := range untrackedFiles {
		fullPath := filepath.Join(t.repoRoot, path)
		if err := os.Remove(fullPath); err != nil {
			errors = append(errors, fmt.Sprintf("delete %s failed: %v", path, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("discard errors: %s", strings.Join(errors, "; "))
	}

	log.Info().Strs("paths", paths).Msg("discarded changes")
	return nil
}

// CommitResult represents the result of a commit operation.
type CommitResult struct {
	Success        bool   `json:"success"`
	SHA            string `json:"sha,omitempty"`
	Message        string `json:"message,omitempty"`
	FilesCommitted int    `json:"files_committed,omitempty"`
	Pushed         bool   `json:"pushed,omitempty"`
	Error          string `json:"error,omitempty"`
}

// Commit commits staged changes.
func (t *Tracker) Commit(ctx context.Context, message string, push bool) (*CommitResult, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	// Check if there are staged changes
	cmd := exec.CommandContext(ctx, t.command, "diff", "--cached", "--name-only")
	cmd.Dir = t.repoRoot
	output, _ := cmd.Output()
	stagedFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(stagedFiles) == 0 || (len(stagedFiles) == 1 && stagedFiles[0] == "") {
		return &CommitResult{
			Success: false,
			Error:   "Nothing to commit (no staged changes)",
		}, nil
	}

	// Commit
	cmd = exec.CommandContext(ctx, t.command, "commit", "-m", message)
	cmd.Dir = t.repoRoot

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &CommitResult{
			Success: false,
			Error:   fmt.Sprintf("Commit failed: %s", stderr.String()),
		}, nil
	}

	// Get the commit SHA
	cmd = exec.CommandContext(ctx, t.command, "rev-parse", "--short", "HEAD")
	cmd.Dir = t.repoRoot
	shaOutput, _ := cmd.Output()
	sha := strings.TrimSpace(string(shaOutput))

	result := &CommitResult{
		Success:        true,
		SHA:            sha,
		FilesCommitted: len(stagedFiles),
		Message:        fmt.Sprintf("Committed: %s", truncateMessage(message, 50)),
	}

	log.Info().Str("sha", sha).Int("files", len(stagedFiles)).Msg("committed changes")

	// Push if requested
	if push {
		pushResult, err := t.Push(ctx, false, false, "", "")
		if err != nil || !pushResult.Success {
			result.Message = fmt.Sprintf("Committed but push failed: %s", pushResult.Error)
		} else {
			result.Pushed = true
			_, upstream, _, _ := t.getBranchInfo(ctx)
			if upstream == "" {
				upstream = "origin"
			}
			result.Message = fmt.Sprintf("Committed and pushed to %s", upstream)
		}
	}

	return result, nil
}

// truncateMessage truncates a commit message for display.
func truncateMessage(s string, maxLen int) string {
	// Take first line only
	if idx := strings.Index(s, "\n"); idx > 0 {
		s = s[:idx]
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// PushResult represents the result of a push operation.
type PushResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message,omitempty"`
	CommitsPushed int    `json:"commits_pushed,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Push pushes commits to remote.
func (t *Tracker) Push(ctx context.Context, force bool, setUpstream bool, remote, branch string) (*PushResult, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	if setUpstream {
		args = append(args, "-u")
		if remote != "" {
			args = append(args, remote)
		}
		if branch != "" {
			args = append(args, branch)
		}
	}

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if strings.Contains(errMsg, "rejected") {
			return &PushResult{
				Success: false,
				Error:   "Push rejected: Updates were rejected because the remote contains work that you do not have locally",
			}, nil
		}
		return &PushResult{
			Success: false,
			Error:   fmt.Sprintf("Push failed: %s", errMsg),
		}, nil
	}

	_, upstream, ahead, _ := t.getBranchInfo(ctx)
	if upstream == "" {
		upstream = "origin"
	}

	log.Info().Str("upstream", upstream).Msg("pushed to remote")

	return &PushResult{
		Success:       true,
		Message:       fmt.Sprintf("Pushed to %s", upstream),
		CommitsPushed: ahead,
	}, nil
}

// PullResult represents the result of a pull operation.
type PullResult struct {
	Success         bool     `json:"success"`
	Message         string   `json:"message,omitempty"`
	CommitsPulled   int      `json:"commits_pulled,omitempty"`
	FilesChanged    int      `json:"files_changed,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// Pull pulls changes from remote.
func (t *Tracker) Pull(ctx context.Context, rebase bool) (*PullResult, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	args := []string{"pull"}
	if rebase {
		args = append(args, "--rebase")
	}

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	outputStr := stdout.String() + stderr.String()

	// Check for conflicts
	if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "Merge conflict") {
		// Get conflicted files
		conflicted := t.getConflictedFiles(ctx)
		return &PullResult{
			Success:         false,
			Error:           "Merge conflict",
			ConflictedFiles: conflicted,
		}, nil
	}

	if err != nil {
		return &PullResult{
			Success: false,
			Error:   fmt.Sprintf("Pull failed: %s", stderr.String()),
		}, nil
	}

	_, upstream, _, _ := t.getBranchInfo(ctx)
	if upstream == "" {
		upstream = "origin"
	}

	log.Info().Str("upstream", upstream).Msg("pulled from remote")

	return &PullResult{
		Success: true,
		Message: fmt.Sprintf("Pulled from %s", upstream),
	}, nil
}

// getConflictedFiles returns list of files with merge conflicts.
func (t *Tracker) getConflictedFiles(ctx context.Context) []string {
	cmd := exec.CommandContext(ctx, t.command, "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(files) == 1 && files[0] == "" {
		return nil
	}
	return files
}

// BranchInfo represents information about a git branch.
type BranchInfo struct {
	Name      string `json:"name"`
	IsCurrent bool   `json:"is_current"`
	IsRemote  bool   `json:"is_remote"`
	Upstream  string `json:"upstream,omitempty"`
	Ahead     int    `json:"ahead,omitempty"`
	Behind    int    `json:"behind,omitempty"`
}

// BranchesResult represents the result of listing branches.
type BranchesResult struct {
	Current  string       `json:"current"`
	Upstream string       `json:"upstream,omitempty"`
	Ahead    int          `json:"ahead"`
	Behind   int          `json:"behind"`
	Branches []BranchInfo `json:"branches"`
}

// ListBranches lists all branches with info.
func (t *Tracker) ListBranches(ctx context.Context) (*BranchesResult, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	result := &BranchesResult{
		Branches: make([]BranchInfo, 0),
	}

	// Get current branch info
	current, upstream, ahead, behind := t.getBranchInfo(ctx)
	result.Current = current
	result.Upstream = upstream
	result.Ahead = ahead
	result.Behind = behind

	// List local branches
	cmd := exec.CommandContext(ctx, t.command, "branch", "--format=%(refname:short)")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if name == "" {
				continue
			}
			branch := BranchInfo{
				Name:      name,
				IsCurrent: name == current,
				IsRemote:  false,
			}
			if name == current {
				branch.Upstream = upstream
				branch.Ahead = ahead
				branch.Behind = behind
			}
			result.Branches = append(result.Branches, branch)
		}
	}

	// List remote branches
	cmd = exec.CommandContext(ctx, t.command, "branch", "-r", "--format=%(refname:short)")
	cmd.Dir = t.repoRoot
	output, err = cmd.Output()
	if err == nil {
		for _, name := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if name == "" || strings.Contains(name, "HEAD") {
				continue
			}
			result.Branches = append(result.Branches, BranchInfo{
				Name:     name,
				IsRemote: true,
			})
		}
	}

	return result, nil
}

// CheckoutResult represents the result of a checkout operation.
type CheckoutResult struct {
	Success    bool   `json:"success"`
	Branch     string `json:"branch,omitempty"`
	FromBranch string `json:"from_branch,omitempty"` // Previous branch (for git_branch_changed event)
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Checkout switches branches or creates a new branch.
func (t *Tracker) Checkout(ctx context.Context, branch string, create bool) (*CheckoutResult, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

	// Get current branch before checkout (for git_branch_changed event)
	fromBranch, _, _, _ := t.getBranchInfo(ctx)

	args := []string{"checkout"}
	if create {
		args = append(args, "-b")
	}
	args = append(args, branch)

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := stderr.String()
		if strings.Contains(errMsg, "Your local changes") || strings.Contains(errMsg, "uncommitted changes") {
			return &CheckoutResult{
				Success: false,
				Error:   "Cannot switch branches: You have unstaged changes",
			}, nil
		}
		if strings.Contains(errMsg, "already exists") {
			return &CheckoutResult{
				Success: false,
				Error:   fmt.Sprintf("Branch '%s' already exists", branch),
			}, nil
		}
		return &CheckoutResult{
			Success: false,
			Error:   fmt.Sprintf("Checkout failed: %s", errMsg),
		}, nil
	}

	action := "Switched to"
	if create {
		action = "Created and switched to"
	}

	log.Info().Str("branch", branch).Str("from", fromBranch).Bool("created", create).Msg("checkout branch")

	return &CheckoutResult{
		Success:    true,
		Branch:     branch,
		FromBranch: fromBranch,
		Message:    fmt.Sprintf("%s branch '%s'", action, branch),
	}, nil
}

// DirectoryEntry represents a file or directory entry.
type DirectoryEntry struct {
	Name          string  `json:"name"`
	Type          string  `json:"type"` // "file" or "directory"
	Size          *int64  `json:"size,omitempty"`
	Modified      *string `json:"modified,omitempty"`
	ChildrenCount *int    `json:"children_count,omitempty"`
}

// ListDirectory returns entries in a directory.
func (t *Tracker) ListDirectory(ctx context.Context, path string) ([]DirectoryEntry, error) {
	fullPath := t.repoRoot
	if path != "" {
		fullPath = filepath.Join(t.repoRoot, path)
	}

	// Validate path is within repo
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return nil, err
	}

	absRoot, err := filepath.Abs(t.repoRoot)
	if err != nil {
		return nil, err
	}

	if !strings.HasPrefix(absPath, absRoot) {
		return nil, domain.ErrPathOutsideRepo
	}

	// Read directory
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, err
	}

	result := make([]DirectoryEntry, 0, len(entries))
	for _, entry := range entries {
		// Skip hidden files and common ignored directories
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Name() == "node_modules" || entry.Name() == "__pycache__" || entry.Name() == "vendor" {
			continue
		}

		dirEntry := DirectoryEntry{
			Name: entry.Name(),
		}

		if entry.IsDir() {
			dirEntry.Type = "directory"
			// Count children
			subPath := filepath.Join(absPath, entry.Name())
			if subEntries, err := os.ReadDir(subPath); err == nil {
				count := len(subEntries)
				dirEntry.ChildrenCount = &count
			}
		} else {
			dirEntry.Type = "file"
			if info, err := entry.Info(); err == nil {
				size := info.Size()
				dirEntry.Size = &size
				modTime := info.ModTime().Format(time.RFC3339)
				dirEntry.Modified = &modTime
			}
		}

		result = append(result, dirEntry)
	}

	return result, nil
}

// --- Branch Delete ---

// DeleteBranchResult represents the result of a branch delete operation.
type DeleteBranchResult struct {
	Success bool   `json:"success"`
	Branch  string `json:"branch"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// DeleteBranch deletes a local branch.
func (t *Tracker) DeleteBranch(ctx context.Context, branch string, force bool) (*DeleteBranchResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	currentBranch, _, _, _ := t.getBranchInfo(ctx)
	if branch == currentBranch {
		return &DeleteBranchResult{
			Success: false,
			Branch:  branch,
			Error:   "cannot delete the currently checked out branch",
		}, nil
	}

	flag := "-d"
	if force {
		flag = "-D"
	}

	cmd := exec.CommandContext(ctx, t.command, "branch", flag, branch)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &DeleteBranchResult{
			Success: false,
			Branch:  branch,
			Error:   strings.TrimSpace(string(output)),
		}, nil
	}

	return &DeleteBranchResult{
		Success: true,
		Branch:  branch,
		Message: fmt.Sprintf("Deleted branch %s", branch),
	}, nil
}

// --- Fetch ---

// FetchResult represents the result of a fetch operation.
type FetchResult struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message,omitempty"`
	Error          string   `json:"error,omitempty"`
	UpdatedRefs    []string `json:"updated_refs,omitempty"`
	NewBranches    []string `json:"new_branches,omitempty"`
	PrunedBranches []string `json:"pruned_branches,omitempty"`
}

// Fetch fetches from remote without merging.
func (t *Tracker) Fetch(ctx context.Context, remote string, prune bool) (*FetchResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	if remote == "" {
		remote = "origin"
	}

	args := []string{"fetch", remote}
	if prune {
		args = append(args, "--prune")
	}
	args = append(args, "--verbose")

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		return &FetchResult{
			Success: false,
			Error:   strings.TrimSpace(outputStr),
		}, nil
	}

	result := &FetchResult{
		Success: true,
		Message: "Fetch completed",
	}

	lines := strings.Split(outputStr, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "->") {
			result.UpdatedRefs = append(result.UpdatedRefs, line)
		}
		if strings.HasPrefix(line, "[new branch]") {
			result.NewBranches = append(result.NewBranches, line)
		}
		if strings.HasPrefix(line, "[deleted]") || strings.Contains(line, "- [deleted]") {
			result.PrunedBranches = append(result.PrunedBranches, line)
		}
	}

	return result, nil
}

// --- Log (Commit History) ---

// GraphLineType represents the type of line in a git graph.
type GraphLineType string

const (
	GraphLineStraight    GraphLineType = "straight"
	GraphLineMergeLeft   GraphLineType = "merge_left"
	GraphLineMergeRight  GraphLineType = "merge_right"
	GraphLineBranchLeft  GraphLineType = "branch_left"
	GraphLineBranchRight GraphLineType = "branch_right"
	GraphLinePass        GraphLineType = "pass"
)

// GraphLine represents a line segment in the git graph.
type GraphLine struct {
	FromColumn int           `json:"from_column"`
	ToColumn   int           `json:"to_column"`
	Type       GraphLineType `json:"type"`
}

// GraphPosition represents the position of a commit in the git graph.
type GraphPosition struct {
	Column int         `json:"column"`
	Lines  []GraphLine `json:"lines"`
}

// LogEntry represents a single commit in the log.
type LogEntry struct {
	SHA           string         `json:"sha"`
	ShortSHA      string         `json:"short_sha"`
	Author        string         `json:"author"`
	AuthorEmail   string         `json:"author_email"`
	Date          string         `json:"date"`
	RelativeDate  string         `json:"relative_date"`
	Subject       string         `json:"subject"`
	Body          string         `json:"body,omitempty"`
	ParentSHAs    []string       `json:"parent_shas,omitempty"`
	IsMerge       bool           `json:"is_merge"`
	GraphPosition *GraphPosition `json:"graph_position,omitempty"`
}

// LogResult represents the result of a log operation.
type LogResult struct {
	Commits    []LogEntry `json:"commits"`
	TotalCount int        `json:"total_count"`
	HasMore    bool       `json:"has_more"`
	MaxColumns int        `json:"max_columns,omitempty"`
}

// Log returns commit history.
func (t *Tracker) Log(ctx context.Context, limit int, skip int, branch string, path string, graph bool) (*LogResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	format := "%H|%h|%an|%ae|%aI|%ar|%s|%P"
	args := []string{"log", fmt.Sprintf("--format=%s", format), fmt.Sprintf("-n%d", limit+1)}
	if skip > 0 {
		args = append(args, fmt.Sprintf("--skip=%d", skip))
	}
	if graph && branch == "" {
		args = append(args, "--all")
	} else if branch != "" {
		args = append(args, branch)
	}
	if path != "" {
		args = append(args, "--", path)
	}

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	commits := make([]LogEntry, 0, len(lines))

	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 8)
		if len(parts) < 7 {
			continue
		}

		entry := LogEntry{
			SHA:          parts[0],
			ShortSHA:     parts[1],
			Author:       parts[2],
			AuthorEmail:  parts[3],
			Date:         parts[4],
			RelativeDate: parts[5],
			Subject:      parts[6],
		}

		if len(parts) > 7 && parts[7] != "" {
			entry.ParentSHAs = strings.Split(parts[7], " ")
			entry.IsMerge = len(entry.ParentSHAs) > 1
		}

		commits = append(commits, entry)
	}

	hasMore := len(commits) > limit
	if hasMore {
		commits = commits[:limit]
	}

	maxColumns := 0
	if graph && len(commits) > 0 {
		maxColumns = computeGraphLayout(commits)
	}

	return &LogResult{
		Commits:    commits,
		TotalCount: len(commits),
		HasMore:    hasMore,
		MaxColumns: maxColumns,
	}, nil
}

// computeGraphLayout computes the graph positions for commits.
func computeGraphLayout(commits []LogEntry) int {
	if len(commits) == 0 {
		return 0
	}

	lanes := make([]string, 0, 8)

	findLane := func(sha string) int {
		for i, s := range lanes {
			if s == sha {
				return i
			}
		}
		return -1
	}

	findEmptyLane := func() int {
		for i, s := range lanes {
			if s == "" {
				return i
			}
		}
		lanes = append(lanes, "")
		return len(lanes) - 1
	}

	maxCols := 0

	for i := range commits {
		commit := &commits[i]
		var lines []GraphLine

		column := findLane(commit.SHA)
		if column == -1 {
			column = findEmptyLane()
			lanes[column] = commit.SHA
		}

		if column+1 > maxCols {
			maxCols = column + 1
		}

		if len(commit.ParentSHAs) == 0 {
			lanes[column] = ""
		} else {
			firstParent := true
			for _, parentSHA := range commit.ParentSHAs {
				parentCol := findLane(parentSHA)

				if parentCol == -1 {
					if firstParent {
						lanes[column] = parentSHA
						parentCol = column
					} else {
						parentCol = findEmptyLane()
						lanes[parentCol] = parentSHA
						if parentCol+1 > maxCols {
							maxCols = parentCol + 1
						}
					}
				}

				var lineType GraphLineType
				if parentCol == column {
					lineType = GraphLineStraight
				} else if parentCol < column {
					lineType = GraphLineMergeLeft
				} else {
					lineType = GraphLineMergeRight
				}

				lines = append(lines, GraphLine{
					FromColumn: parentCol,
					ToColumn:   column,
					Type:       lineType,
				})

				firstParent = false
			}
		}

		for col, sha := range lanes {
			if sha != "" && col != column {
				isParent := false
				for _, psha := range commit.ParentSHAs {
					if sha == psha {
						isParent = true
						break
					}
				}
				if !isParent {
					lines = append(lines, GraphLine{
						FromColumn: col,
						ToColumn:   col,
						Type:       GraphLinePass,
					})
				}
			}
		}

		commit.GraphPosition = &GraphPosition{
			Column: column,
			Lines:  lines,
		}
	}

	return maxCols
}

// --- Stash Operations ---

// StashEntry represents a single stash entry.
type StashEntry struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Branch  string `json:"branch"`
	Message string `json:"message"`
}

// StashResult represents the result of a stash operation.
type StashResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Name    string `json:"name,omitempty"`
}

// StashListResult represents the result of listing stashes.
type StashListResult struct {
	Stashes []StashEntry `json:"stashes"`
	Count   int          `json:"count"`
}

// Stash creates a new stash.
func (t *Tracker) Stash(ctx context.Context, message string, includeUntracked bool) (*StashResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "-u")
	}
	if message != "" {
		args = append(args, "-m", message)
	}

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return &StashResult{Success: false, Error: outputStr}, nil
	}

	if strings.Contains(outputStr, "No local changes to save") {
		return &StashResult{Success: false, Message: "No local changes to stash"}, nil
	}

	return &StashResult{Success: true, Message: outputStr, Name: "stash@{0}"}, nil
}

// StashList returns all stash entries.
func (t *Tracker) StashList(ctx context.Context) (*StashListResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "stash", "list", "--format=%gd|%gs")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return &StashListResult{Stashes: []StashEntry{}, Count: 0}, nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	stashes := make([]StashEntry, 0, len(lines))

	for i, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "|", 2)
		entry := StashEntry{Index: i, Name: parts[0]}

		if len(parts) > 1 {
			msg := parts[1]
			if idx := strings.Index(msg, ": "); idx > 0 {
				branchPart := msg[:idx]
				entry.Message = msg[idx+2:]
				if strings.HasPrefix(branchPart, "WIP on ") {
					entry.Branch = branchPart[7:]
				} else if strings.HasPrefix(branchPart, "On ") {
					entry.Branch = branchPart[3:]
				} else {
					entry.Branch = branchPart
				}
			} else {
				entry.Message = msg
			}
		}

		stashes = append(stashes, entry)
	}

	return &StashListResult{Stashes: stashes, Count: len(stashes)}, nil
}

// StashApply applies a stash without removing it.
func (t *Tracker) StashApply(ctx context.Context, index int) (*StashResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	stashRef := fmt.Sprintf("stash@{%d}", index)
	cmd := exec.CommandContext(ctx, t.command, "stash", "apply", stashRef)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &StashResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	return &StashResult{Success: true, Message: fmt.Sprintf("Applied %s", stashRef)}, nil
}

// StashPop applies and removes a stash.
func (t *Tracker) StashPop(ctx context.Context, index int) (*StashResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	stashRef := fmt.Sprintf("stash@{%d}", index)
	cmd := exec.CommandContext(ctx, t.command, "stash", "pop", stashRef)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &StashResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	return &StashResult{Success: true, Message: fmt.Sprintf("Applied and dropped %s", stashRef)}, nil
}

// StashDrop removes a stash.
func (t *Tracker) StashDrop(ctx context.Context, index int) (*StashResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	stashRef := fmt.Sprintf("stash@{%d}", index)
	cmd := exec.CommandContext(ctx, t.command, "stash", "drop", stashRef)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &StashResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	return &StashResult{Success: true, Message: fmt.Sprintf("Dropped %s", stashRef)}, nil
}

// --- Merge Operations ---

// MergeResult represents the result of a merge operation.
type MergeResult struct {
	Success         bool     `json:"success"`
	Message         string   `json:"message,omitempty"`
	Error           string   `json:"error,omitempty"`
	CommitSHA       string   `json:"commit_sha,omitempty"`
	HasConflicts    bool     `json:"has_conflicts"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
}

// Merge merges a branch into the current branch.
func (t *Tracker) Merge(ctx context.Context, branch string, noFastForward bool, message string) (*MergeResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	args := []string{"merge", branch}
	if noFastForward {
		args = append(args, "--no-ff")
	}
	if message != "" {
		args = append(args, "-m", message)
	}

	cmd := exec.CommandContext(ctx, t.command, args...)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "Automatic merge failed") {
			conflictedFiles := t.getConflictedFiles(ctx)
			return &MergeResult{
				Success:         false,
				HasConflicts:    true,
				ConflictedFiles: conflictedFiles,
				Error:           "Merge conflicts detected. Resolve conflicts and commit.",
			}, nil
		}
		return &MergeResult{Success: false, Error: outputStr}, nil
	}

	sha := ""
	shaCmd := exec.CommandContext(ctx, t.command, "rev-parse", "HEAD")
	shaCmd.Dir = t.repoRoot
	if shaOutput, err := shaCmd.Output(); err == nil {
		sha = strings.TrimSpace(string(shaOutput))
	}

	return &MergeResult{Success: true, Message: fmt.Sprintf("Merged %s", branch), CommitSHA: sha}, nil
}

// MergeAbort aborts an in-progress merge.
func (t *Tracker) MergeAbort(ctx context.Context) (*MergeResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "merge", "--abort")
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()

	if err != nil {
		return &MergeResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	return &MergeResult{Success: true, Message: "Merge aborted"}, nil
}

// --- Git Init ---

// InitResult represents the result of a git init operation.
type InitResult struct {
	Success        bool   `json:"success"`
	Message        string `json:"message,omitempty"`
	Error          string `json:"error,omitempty"`
	Branch         string `json:"branch,omitempty"`
	CommitSHA      string `json:"commit_sha,omitempty"`
	FilesCommitted int    `json:"files_committed,omitempty"`
}

// Init initializes a git repository.
func (t *Tracker) Init(ctx context.Context, initialBranch string, initialCommit bool, commitMessage string) (*InitResult, error) {
	if t.IsGitRepo() {
		return &InitResult{Success: false, Error: "Directory is already a git repository"}, nil
	}

	if initialBranch == "" {
		initialBranch = "main"
	}

	cmd := exec.CommandContext(ctx, t.command, "init", "-b", initialBranch)
	cmd.Dir = t.repoPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &InitResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	t.mu.Lock()
	t.isRepo = true
	t.repoRoot = t.repoPath
	t.repoName = filepath.Base(t.repoPath)
	t.mu.Unlock()

	result := &InitResult{Success: true, Branch: initialBranch, Message: "Initialized empty Git repository"}

	if initialCommit {
		stageCmd := exec.CommandContext(ctx, t.command, "add", "-A")
		stageCmd.Dir = t.repoPath
		_, _ = stageCmd.CombinedOutput()

		statusCmd := exec.CommandContext(ctx, t.command, "diff", "--cached", "--name-only")
		statusCmd.Dir = t.repoPath
		statusOutput, _ := statusCmd.Output()
		stagedFiles := strings.Split(strings.TrimSpace(string(statusOutput)), "\n")
		fileCount := 0
		for _, f := range stagedFiles {
			if f != "" {
				fileCount++
			}
		}

		if fileCount > 0 {
			if commitMessage == "" {
				commitMessage = "Initial commit"
			}

			commitCmd := exec.CommandContext(ctx, t.command, "commit", "-m", commitMessage)
			commitCmd.Dir = t.repoPath
			if _, err := commitCmd.CombinedOutput(); err == nil {
				result.FilesCommitted = fileCount
				result.Message = fmt.Sprintf("Initialized and created initial commit with %d files", fileCount)

				shaCmd := exec.CommandContext(ctx, t.command, "rev-parse", "HEAD")
				shaCmd.Dir = t.repoPath
				if shaOutput, err := shaCmd.Output(); err == nil {
					result.CommitSHA = strings.TrimSpace(string(shaOutput))
				}
			}
		}
	}

	return result, nil
}

// --- Remote Operations ---

// RemoteInfo represents information about a git remote.
type RemoteInfo struct {
	Name             string   `json:"name"`
	FetchURL         string   `json:"fetch_url"`
	PushURL          string   `json:"push_url"`
	Provider         string   `json:"provider,omitempty"`
	TrackingBranches []string `json:"tracking_branches,omitempty"`
}

// RemoteAddResult represents the result of adding a remote.
type RemoteAddResult struct {
	Success         bool        `json:"success"`
	Message         string      `json:"message,omitempty"`
	Error           string      `json:"error,omitempty"`
	Remote          *RemoteInfo `json:"remote,omitempty"`
	FetchedBranches []string    `json:"fetched_branches,omitempty"`
}

// RemoteListResult represents the result of listing remotes.
type RemoteListResult struct {
	Remotes []RemoteInfo `json:"remotes"`
}

// RemoteRemoveResult represents the result of removing a remote.
type RemoteRemoveResult struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

func detectProvider(url string) string {
	url = strings.ToLower(url)
	if strings.Contains(url, "github.com") {
		return "github"
	}
	if strings.Contains(url, "gitlab.com") {
		return "gitlab"
	}
	if strings.Contains(url, "bitbucket.org") {
		return "bitbucket"
	}
	return "custom"
}

// RemoteAdd adds a new remote.
func (t *Tracker) RemoteAdd(ctx context.Context, name string, url string, fetch bool) (*RemoteAddResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	if name == "" {
		name = "origin"
	}

	checkCmd := exec.CommandContext(ctx, t.command, "remote", "get-url", name)
	checkCmd.Dir = t.repoRoot
	if _, err := checkCmd.Output(); err == nil {
		return &RemoteAddResult{Success: false, Error: fmt.Sprintf("Remote '%s' already exists", name)}, nil
	}

	cmd := exec.CommandContext(ctx, t.command, "remote", "add", name, url)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &RemoteAddResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	result := &RemoteAddResult{
		Success: true,
		Message: fmt.Sprintf("Added remote '%s'", name),
		Remote:  &RemoteInfo{Name: name, FetchURL: url, PushURL: url, Provider: detectProvider(url)},
	}

	if fetch {
		fetchCmd := exec.CommandContext(ctx, t.command, "fetch", name)
		fetchCmd.Dir = t.repoRoot
		_, _ = fetchCmd.CombinedOutput()

		branchCmd := exec.CommandContext(ctx, t.command, "branch", "-r", "--list", fmt.Sprintf("%s/*", name))
		branchCmd.Dir = t.repoRoot
		if branchOutput, err := branchCmd.Output(); err == nil {
			branches := strings.Split(strings.TrimSpace(string(branchOutput)), "\n")
			for _, b := range branches {
				b = strings.TrimSpace(b)
				if b != "" {
					if strings.HasPrefix(b, name+"/") {
						b = strings.TrimPrefix(b, name+"/")
					}
					result.FetchedBranches = append(result.FetchedBranches, b)
				}
			}
		}
	}

	return result, nil
}

// RemoteList returns all configured remotes.
func (t *Tracker) RemoteList(ctx context.Context) (*RemoteListResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "remote")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return &RemoteListResult{Remotes: []RemoteInfo{}}, nil
	}

	remoteNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	remotes := make([]RemoteInfo, 0, len(remoteNames))

	for _, name := range remoteNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		remote := RemoteInfo{Name: name}

		fetchCmd := exec.CommandContext(ctx, t.command, "remote", "get-url", name)
		fetchCmd.Dir = t.repoRoot
		if fetchOutput, err := fetchCmd.Output(); err == nil {
			remote.FetchURL = strings.TrimSpace(string(fetchOutput))
			remote.Provider = detectProvider(remote.FetchURL)
		}

		pushCmd := exec.CommandContext(ctx, t.command, "remote", "get-url", "--push", name)
		pushCmd.Dir = t.repoRoot
		if pushOutput, err := pushCmd.Output(); err == nil {
			remote.PushURL = strings.TrimSpace(string(pushOutput))
		}

		branchCmd := exec.CommandContext(ctx, t.command, "branch", "-r", "--list", fmt.Sprintf("%s/*", name))
		branchCmd.Dir = t.repoRoot
		if branchOutput, err := branchCmd.Output(); err == nil {
			branches := strings.Split(strings.TrimSpace(string(branchOutput)), "\n")
			for _, b := range branches {
				b = strings.TrimSpace(b)
				if b != "" {
					if strings.HasPrefix(b, name+"/") {
						b = strings.TrimPrefix(b, name+"/")
					}
					remote.TrackingBranches = append(remote.TrackingBranches, b)
				}
			}
		}

		remotes = append(remotes, remote)
	}

	return &RemoteListResult{Remotes: remotes}, nil
}

// RemoteRemove removes a remote.
func (t *Tracker) RemoteRemove(ctx context.Context, name string) (*RemoteRemoveResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	cmd := exec.CommandContext(ctx, t.command, "remote", "remove", name)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &RemoteRemoveResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	return &RemoteRemoveResult{Success: true, Message: fmt.Sprintf("Removed remote '%s'", name)}, nil
}

// --- Set Upstream ---

// SetUpstreamResult represents the result of setting upstream.
type SetUpstreamResult struct {
	Success  bool   `json:"success"`
	Message  string `json:"message,omitempty"`
	Error    string `json:"error,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Upstream string `json:"upstream,omitempty"`
}

// SetUpstream sets the upstream tracking branch.
func (t *Tracker) SetUpstream(ctx context.Context, branch string, upstream string) (*SetUpstreamResult, error) {
	if !t.IsGitRepo() {
		return nil, domain.ErrNotGitRepo
	}

	if branch == "" {
		currentBranch, _, _, _ := t.getBranchInfo(ctx)
		branch = currentBranch
	}

	cmd := exec.CommandContext(ctx, t.command, "branch", "--set-upstream-to="+upstream, branch)
	cmd.Dir = t.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &SetUpstreamResult{Success: false, Error: strings.TrimSpace(string(output))}, nil
	}

	return &SetUpstreamResult{
		Success:  true,
		Branch:   branch,
		Upstream: upstream,
		Message:  fmt.Sprintf("Branch '%s' set up to track '%s'", branch, upstream),
	}, nil
}

// --- Enhanced Status ---

// WorkspaceGitState represents the git state of a workspace.
type WorkspaceGitState string

const (
	GitStateNoGit       WorkspaceGitState = "no_git"
	GitStateInitialized WorkspaceGitState = "git_init"
	GitStateNoRemote    WorkspaceGitState = "no_remote"
	GitStateNoPush      WorkspaceGitState = "no_push"
	GitStateSynced      WorkspaceGitState = "synced"
	GitStateDiverged    WorkspaceGitState = "diverged"
	GitStateConflict    WorkspaceGitState = "conflict"
)

// Status represents the full git status of a workspace.
type Status struct {
	IsGitRepo    bool              `json:"is_git_repo"`
	HasCommits   bool              `json:"has_commits"`
	State        WorkspaceGitState `json:"state"`
	Branch       string            `json:"branch,omitempty"`
	Upstream     string            `json:"upstream,omitempty"`
	Ahead        int               `json:"ahead"`
	Behind       int               `json:"behind"`
	Staged       []FileEntry       `json:"staged"`
	Unstaged     []FileEntry       `json:"unstaged"`
	Untracked    []FileEntry       `json:"untracked"`
	Conflicted   []FileEntry       `json:"conflicted"`
	HasConflicts bool              `json:"has_conflicts"`
	Remotes      []RemoteInfo      `json:"remotes,omitempty"`
	RepoName     string            `json:"repo_name,omitempty"`
	RepoRoot     string            `json:"repo_root,omitempty"`
}

// GetStatus returns the full git status including state machine.
func (t *Tracker) GetStatus(ctx context.Context) (*Status, error) {
	status := &Status{
		Staged:     make([]FileEntry, 0),
		Unstaged:   make([]FileEntry, 0),
		Untracked:  make([]FileEntry, 0),
		Conflicted: make([]FileEntry, 0),
		Remotes:    make([]RemoteInfo, 0),
	}

	if !t.IsGitRepo() {
		status.IsGitRepo = false
		status.State = GitStateNoGit
		return status, nil
	}
	status.IsGitRepo = true
	status.RepoName = t.repoName
	status.RepoRoot = t.repoRoot

	headCmd := exec.CommandContext(ctx, t.command, "rev-parse", "HEAD")
	headCmd.Dir = t.repoRoot
	if _, err := headCmd.Output(); err != nil {
		status.HasCommits = false
		status.State = GitStateInitialized
		branchCmd := exec.CommandContext(ctx, t.command, "branch", "--show-current")
		branchCmd.Dir = t.repoRoot
		if branchOutput, err := branchCmd.Output(); err == nil {
			status.Branch = strings.TrimSpace(string(branchOutput))
		}
		return status, nil
	}
	status.HasCommits = true

	branch, upstream, ahead, behind := t.getBranchInfo(ctx)
	status.Branch = branch
	status.Upstream = upstream
	status.Ahead = ahead
	status.Behind = behind

	cmd := exec.CommandContext(ctx, t.command, "status", "--porcelain", "-uall")
	cmd.Dir = t.repoRoot
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSuffix(string(output), "\n"), "\n")
		for _, line := range lines {
			if len(line) < 3 {
				continue
			}

			staged := line[0]
			unstaged := line[1]
			path := strings.TrimLeft(line[2:], " ")

			if strings.Contains(path, " -> ") {
				parts := strings.Split(path, " -> ")
				path = parts[len(parts)-1]
			}

			if staged == 'U' || unstaged == 'U' || (staged == 'A' && unstaged == 'A') || (staged == 'D' && unstaged == 'D') {
				status.Conflicted = append(status.Conflicted, FileEntry{Path: path, Status: "!"})
				status.HasConflicts = true
			} else if staged == '?' && unstaged == '?' {
				status.Untracked = append(status.Untracked, FileEntry{Path: path, Status: "?"})
			} else {
				if staged != ' ' && staged != '?' {
					entry := FileEntry{Path: path, Status: string(staged)}
					additions, deletions := t.getDiffStats(ctx, path, true)
					entry.Additions = additions
					entry.Deletions = deletions
					status.Staged = append(status.Staged, entry)
				}
				if unstaged != ' ' && unstaged != '?' {
					entry := FileEntry{Path: path, Status: string(unstaged)}
					additions, deletions := t.getDiffStats(ctx, path, false)
					entry.Additions = additions
					entry.Deletions = deletions
					status.Unstaged = append(status.Unstaged, entry)
				}
			}
		}
	}

	remoteResult, _ := t.RemoteList(ctx)
	if remoteResult != nil {
		status.Remotes = remoteResult.Remotes
	}

	if len(status.Remotes) == 0 {
		status.State = GitStateNoRemote
	} else if status.Upstream == "" {
		status.State = GitStateNoPush
	} else if status.HasConflicts {
		status.State = GitStateConflict
	} else if status.Ahead > 0 || status.Behind > 0 {
		status.State = GitStateDiverged
	} else {
		status.State = GitStateSynced
	}

	return status, nil
}
