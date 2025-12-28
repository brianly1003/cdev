//go:build windows

// Package terminal provides terminal mode support for cdev.
// Terminal mode is not currently supported on Windows.
package terminal

import (
	"context"
	"fmt"
	"io"
)

// Runner manages terminal mode for Claude sessions.
// This is a stub for Windows which does not support PTY.
type Runner struct {
	workDir    string
	claudeCmd  string
	claudeArgs []string
}

// NewRunner creates a new terminal mode runner.
func NewRunner(workDir, claudeCmd string, claudeArgs []string) *Runner {
	return &Runner{
		workDir:    workDir,
		claudeCmd:  claudeCmd,
		claudeArgs: claudeArgs,
	}
}

// SetOutputWriter sets an additional writer for output.
// Not supported on Windows.
func (r *Runner) SetOutputWriter(w io.Writer) {
	// No-op on Windows
}

// SendInput sends input to the PTY from a remote source.
// Not supported on Windows.
func (r *Runner) SendInput(data []byte) error {
	return fmt.Errorf("terminal mode is not supported on Windows")
}

// Run starts Claude in terminal mode.
// Not supported on Windows.
func (r *Runner) Run(ctx context.Context, prompt string) error {
	return fmt.Errorf("terminal mode is not supported on Windows")
}

// Stop terminates the running Claude session.
// Not supported on Windows.
func (r *Runner) Stop() error {
	return nil
}

// IsRunning returns whether a session is currently running.
func (r *Runner) IsRunning() bool {
	return false
}
