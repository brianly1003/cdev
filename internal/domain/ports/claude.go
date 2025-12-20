// Package ports defines the interfaces (ports) for the hexagonal architecture.
package ports

import (
	"context"

	"github.com/brianly1003/cdev/internal/domain/events"
)

// ClaudeManager defines the contract for managing Claude CLI.
type ClaudeManager interface {
	// Start spawns Claude CLI with the given prompt.
	Start(ctx context.Context, prompt string) error

	// Stop gracefully terminates the running Claude process.
	Stop(ctx context.Context) error

	// Kill forcefully terminates the Claude process.
	Kill() error

	// State returns the current state.
	State() events.ClaudeState

	// IsRunning returns true if Claude is currently running.
	IsRunning() bool

	// CurrentPrompt returns the current prompt if running.
	CurrentPrompt() string

	// PID returns the process ID if running.
	PID() int
}
