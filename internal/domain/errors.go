// Package domain contains domain errors used throughout the application.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors for common error conditions.
var (
	ErrClaudeAlreadyRunning = errors.New("claude is already running")
	ErrClaudeNotRunning     = errors.New("claude is not running")
	ErrInvalidPrompt        = errors.New("invalid prompt: prompt cannot be empty")
	ErrPromptTooLong        = errors.New("prompt exceeds maximum length")
	ErrRepoNotFound         = errors.New("repository not found")
	ErrNotGitRepo           = errors.New("not a git repository")
	ErrPathOutsideRepo      = errors.New("path is outside repository")
	ErrFileTooLarge         = errors.New("file exceeds size limit")
	ErrFileNotFound         = errors.New("file not found")
	ErrInvalidCommand       = errors.New("invalid command")
	ErrInvalidPayload       = errors.New("invalid payload")
	ErrHubNotRunning        = errors.New("event hub is not running")
	ErrSubscriberClosed     = errors.New("subscriber is closed")
)

// Error codes for client responses.
const (
	ErrCodeClaudeAlreadyRunning = "CLAUDE_ALREADY_RUNNING"
	ErrCodeClaudeNotRunning     = "CLAUDE_NOT_RUNNING"
	ErrCodeInvalidCommand       = "INVALID_COMMAND"
	ErrCodeInvalidPayload       = "INVALID_PAYLOAD"
	ErrCodePathOutsideRepo      = "PATH_OUTSIDE_REPO"
	ErrCodeFileNotFound         = "FILE_NOT_FOUND"
	ErrCodeFileTooLarge         = "FILE_TOO_LARGE"
	ErrCodeGitError             = "GIT_ERROR"
	ErrCodeInternalError        = "INTERNAL_ERROR"
)

// ClaudeError represents an error from Claude CLI operations.
type ClaudeError struct {
	Op       string // Operation that failed
	Err      error  // Underlying error
	ExitCode int    // Exit code if process exited
}

func (e *ClaudeError) Error() string {
	if e.ExitCode != 0 {
		return fmt.Sprintf("claude %s: exit code %d: %v", e.Op, e.ExitCode, e.Err)
	}
	return fmt.Sprintf("claude %s: %v", e.Op, e.Err)
}

func (e *ClaudeError) Unwrap() error {
	return e.Err
}

// NewClaudeError creates a new ClaudeError.
func NewClaudeError(op string, err error, exitCode int) *ClaudeError {
	return &ClaudeError{
		Op:       op,
		Err:      err,
		ExitCode: exitCode,
	}
}

// GitError represents an error from Git operations.
type GitError struct {
	Op  string // Operation that failed
	Err error  // Underlying error
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %v", e.Op, e.Err)
}

func (e *GitError) Unwrap() error {
	return e.Err
}

// NewGitError creates a new GitError.
func NewGitError(op string, err error) *GitError {
	return &GitError{
		Op:  op,
		Err: err,
	}
}

// ValidationError represents a validation error.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s: %s", e.Field, e.Message)
}

// NewValidationError creates a new ValidationError.
func NewValidationError(field, message string) *ValidationError {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}
