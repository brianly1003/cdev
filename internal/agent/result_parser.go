package agent

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/brianly1003/cdev/internal/domain/task"
)

// TaskResultFile is the expected structure of .cdev/task-result.json.
// Written by skills at the end of execution.
type TaskResultFile struct {
	Status          string            `json:"status"`           // "success", "failed", "stuck"
	Summary         string            `json:"summary"`
	TestsPassed     bool              `json:"tests_passed"`
	BuildPassed     bool              `json:"build_passed"`
	FilesChanged    []task.FileChange `json:"files_changed"`
	RoundsCompleted int               `json:"rounds_completed"`
	PRUrl           string            `json:"pr_url,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// ParseResultFromGitDiff extracts file changes from git diff output in a worktree.
func ParseResultFromGitDiff(worktreePath string) ([]task.FileChange, string, error) {
	// Get diff stat
	cmd := exec.Command("git", "diff", "--stat", "HEAD")
	cmd.Dir = worktreePath
	statOutput, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("git diff --stat failed: %w", err)
	}

	// Get full diff
	cmd = exec.Command("git", "diff", "HEAD")
	cmd.Dir = worktreePath
	diffOutput, err := cmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("git diff failed: %w", err)
	}

	files := parseGitDiffStat(string(statOutput))
	return files, string(diffOutput), nil
}

// parseGitDiffStat parses `git diff --stat` output into file changes.
func parseGitDiffStat(stat string) []task.FileChange {
	var files []task.FileChange
	// Match lines like: " path/to/file.go | 42 ++++----"
	re := regexp.MustCompile(`^\s*(.+?)\s*\|\s*(\d+)\s*([+-]*)`)

	for _, line := range strings.Split(stat, "\n") {
		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		path := strings.TrimSpace(matches[1])
		total, _ := strconv.Atoi(matches[2])
		changes := matches[3]

		added := strings.Count(changes, "+")
		removed := strings.Count(changes, "-")

		// Approximate: if we only have the total
		if added == 0 && removed == 0 && total > 0 {
			added = total / 2
			removed = total - added
		}

		files = append(files, task.FileChange{
			Path:         path,
			LinesAdded:   added,
			LinesRemoved: removed,
		})
	}

	return files
}

// ParseResultFromSessionOutput attempts to extract a result from raw session output text.
// This is a fallback when .cdev/task-result.json doesn't exist.
func ParseResultFromSessionOutput(output string) *task.Result {
	result := &task.Result{
		VerdictStatus: "unknown",
	}

	// Check for test results
	if strings.Contains(output, "Test Run Successful") || strings.Contains(output, "Passed!") {
		result.TestsPassed = true
	}
	if strings.Contains(output, "Build succeeded") {
		result.BuildPassed = true
	}

	// Check for PR URL
	prURLRegex := regexp.MustCompile(`https://github\.com/[^\s]+/pull/\d+`)
	if match := prURLRegex.FindString(output); match != "" {
		result.PRUrl = match
	}

	// Determine verdict
	if result.TestsPassed && result.BuildPassed {
		result.VerdictStatus = "converged"
		result.VerdictSummary = "Tests and build passed"
	} else if strings.Contains(output, "stuck") || strings.Contains(output, "max rounds") {
		result.VerdictStatus = "stuck"
		result.VerdictSummary = "Agent got stuck during execution"
	}

	return result
}

// MapResultFileToResult converts a TaskResultFile to a task.Result.
func MapResultFileToResult(rf *TaskResultFile) *task.Result {
	status := "converged"
	switch rf.Status {
	case "failed":
		status = "failed"
	case "stuck":
		status = "stuck"
	}

	return &task.Result{
		VerdictStatus:   status,
		VerdictSummary:  rf.Summary,
		TestsPassed:     rf.TestsPassed,
		BuildPassed:     rf.BuildPassed,
		FilesChanged:    rf.FilesChanged,
		RoundsCompleted: rf.RoundsCompleted,
		PRUrl:           rf.PRUrl,
	}
}

// SerializeResult creates the .cdev/task-result.json content.
func SerializeResult(r *task.Result) ([]byte, error) {
	rf := TaskResultFile{
		Status:          r.VerdictStatus,
		Summary:         r.VerdictSummary,
		TestsPassed:     r.TestsPassed,
		BuildPassed:     r.BuildPassed,
		FilesChanged:    r.FilesChanged,
		RoundsCompleted: r.RoundsCompleted,
		PRUrl:           r.PRUrl,
	}
	return json.MarshalIndent(rf, "", "  ")
}
