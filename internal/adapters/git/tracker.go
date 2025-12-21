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
		log.Warn().Str("path", t.repoPath).Msg("not a git repository")
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
				fmt.Sscanf(parts[0], "%d", &ahead)
				fmt.Sscanf(parts[1], "%d", &behind)
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
		fmt.Sscanf(parts[0], "%d", &additions)
		fmt.Sscanf(parts[1], "%d", &deletions)
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
	Success bool   `json:"success"`
	Branch  string `json:"branch,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Checkout switches branches or creates a new branch.
func (t *Tracker) Checkout(ctx context.Context, branch string, create bool) (*CheckoutResult, error) {
	if !t.isRepo {
		return nil, domain.ErrNotGitRepo
	}

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

	log.Info().Str("branch", branch).Bool("created", create).Msg("checkout branch")

	return &CheckoutResult{
		Success: true,
		Branch:  branch,
		Message: fmt.Sprintf("%s branch '%s'", action, branch),
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
