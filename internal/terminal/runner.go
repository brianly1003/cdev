//go:build !windows

// Package terminal provides terminal mode support for cdev.
// Terminal mode spawns Claude in the current terminal with PTY,
// allowing both local and remote (mobile) interaction.
package terminal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/rs/zerolog/log"
	"golang.org/x/term"
)

// Runner manages terminal mode for Claude sessions.
// It connects Claude's PTY to the current terminal while also
// streaming output to WebSocket clients.
type Runner struct {
	workDir      string
	claudeCmd    string
	claudeArgs   []string
	outputWriter io.Writer // Additional writer for output (WebSocket)
	inputChan    chan []byte // Channel for remote input (WebSocket)

	ptmx         *os.File
	cmd          *exec.Cmd
	running      bool
	mu           sync.Mutex

	// Terminal state
	oldState     *term.State
}

// NewRunner creates a new terminal mode runner.
func NewRunner(workDir, claudeCmd string, claudeArgs []string) *Runner {
	return &Runner{
		workDir:    workDir,
		claudeCmd:  claudeCmd,
		claudeArgs: claudeArgs,
		inputChan:  make(chan []byte, 100),
	}
}

// SetOutputWriter sets an additional writer for output (e.g., WebSocket).
// Output will be written to both stdout and this writer.
func (r *Runner) SetOutputWriter(w io.Writer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputWriter = w
}

// SendInput sends input to the PTY from a remote source (e.g., WebSocket).
func (r *Runner) SendInput(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running || r.ptmx == nil {
		return fmt.Errorf("not running")
	}

	select {
	case r.inputChan <- data:
		return nil
	default:
		return fmt.Errorf("input channel full")
	}
}

// Run starts Claude in terminal mode and blocks until completion.
// This connects Claude's PTY to the current terminal.
func (r *Runner) Run(ctx context.Context, prompt string) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("already running")
	}
	r.running = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	}()

	// Build command
	args := append(r.claudeArgs, prompt)
	r.cmd = exec.CommandContext(ctx, r.claudeCmd, args...)
	r.cmd.Dir = r.workDir
	r.cmd.Env = os.Environ()

	// Start with PTY
	ptmx, err := pty.Start(r.cmd)
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	r.ptmx = ptmx
	defer func() {
		_ = r.ptmx.Close()
		r.ptmx = nil
	}()

	// Set terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Warn().Err(err).Msg("failed to set terminal to raw mode")
	} else {
		r.oldState = oldState
		defer func() { _ = term.Restore(int(os.Stdin.Fd()), oldState) }()
	}

	// Handle window size changes
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			r.handleResize()
		}
	}()
	defer signal.Stop(ch)

	// Initial resize
	r.handleResize()

	// Create done channel for goroutine coordination
	done := make(chan struct{})

	// Copy PTY output to stdout (and WebSocket if configured)
	go func() {
		r.mu.Lock()
		outputWriter := r.outputWriter
		r.mu.Unlock()

		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Debug().Err(err).Msg("pty read error")
				}
				break
			}
			if n > 0 {
				// Write to local stdout
				_, _ = os.Stdout.Write(buf[:n])

				// Write to WebSocket if configured
				if outputWriter != nil {
					_, _ = outputWriter.Write(buf[:n])
				}
			}
		}
		close(done)
	}()

	// Copy local stdin to PTY
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Debug().Err(err).Msg("stdin read error")
				}
				return
			}
			if n > 0 {
				r.mu.Lock()
				if r.ptmx != nil {
					_, _ = r.ptmx.Write(buf[:n])
				}
				r.mu.Unlock()
			}
		}
	}()

	// Handle remote input (from WebSocket)
	go func() {
		for {
			select {
			case data := <-r.inputChan:
				r.mu.Lock()
				if r.ptmx != nil {
					_, _ = r.ptmx.Write(data)
				}
				r.mu.Unlock()
			case <-done:
				return
			}
		}
	}()

	// Wait for command to complete or context cancellation
	select {
	case <-done:
		// PTY closed, wait for command
		return r.cmd.Wait()
	case <-ctx.Done():
		// Context cancelled, kill the process
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		return ctx.Err()
	}
}

// handleResize updates the PTY size to match the terminal.
func (r *Runner) handleResize() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.ptmx == nil {
		return
	}

	size, err := pty.GetsizeFull(os.Stdin)
	if err != nil {
		log.Debug().Err(err).Msg("failed to get terminal size")
		return
	}

	if err := pty.Setsize(r.ptmx, size); err != nil {
		log.Debug().Err(err).Msg("failed to set pty size")
	}
}

// Stop terminates the running Claude session.
func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running || r.cmd == nil || r.cmd.Process == nil {
		return nil
	}

	// Restore terminal state first
	if r.oldState != nil {
		_ = term.Restore(int(os.Stdin.Fd()), r.oldState)
		r.oldState = nil
	}

	// Send SIGTERM, then SIGKILL if needed
	_ = r.cmd.Process.Signal(syscall.SIGTERM)

	return nil
}

// IsRunning returns whether a session is currently running.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
