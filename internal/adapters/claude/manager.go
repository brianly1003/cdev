// Package claude implements the Claude CLI process manager.
package claude

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/sync"
	"github.com/creack/pty"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// SessionMode defines how to handle conversation sessions.
type SessionMode string

const (
	// SessionModeNew starts a new conversation (default).
	SessionModeNew SessionMode = "new"
	// SessionModeContinue continues a conversation by session ID.
	// Requires session_id parameter.
	SessionModeContinue SessionMode = "continue"
)

// Manager implements the ClaudeManager port interface.
type Manager struct {
	command         string
	args            []string
	timeout         time.Duration
	hub             ports.EventHub
	logDir          string
	workDir         string
	skipPermissions bool
	rotationConfig  *config.LogRotationConfig

	// Workspace/Session context for multi-workspace support
	workspaceID string
	sessionID   string

	mu            sync.RWMutex
	state         events.ClaudeState
	currentPrompt string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	cancel        context.CancelFunc
	pid           int
	logFile       io.WriteCloser // Changed from *os.File to support lumberjack rotation

	// PTY mode for true interactive terminal support
	ptmx      *os.File   // PTY master file descriptor
	usePTY    bool       // Whether currently using PTY mode
	ptyParser *PTYParser // Parser for PTY output

	// PTY state tracking
	ptyState          PTYState             // Current PTY interaction state
	ptyPromptType     PermissionType       // Type of permission being requested
	lastPTYPermission *PTYPermissionPrompt // Last detected permission prompt (for reconnect)

	// Session tracking
	claudeSessionID string // Session ID from Claude CLI output

	// Interactive state tracking
	waitingForInput  bool
	pendingToolUseID string
	pendingToolName  string

	// Callback when PTY completes (for emitting stop_reason)
	onPTYComplete func(sessionID string)

	// Spinner tracking for deduplication and debouncing
	lastSpinnerText   string    // Last emitted spinner message to avoid duplicates
	lastSpinnerSymbol string    // Last emitted spinner symbol for animation
	lastSpinnerTime   time.Time // Time of last spinner event for debouncing
	lastLogLine       string    // Last log line to filter duplicates
}

// NewManager creates a new Claude CLI manager (legacy constructor for backward compatibility).
func NewManager(command string, args []string, timeoutMinutes int, hub ports.EventHub, skipPermissions bool, rotationConfig *config.LogRotationConfig) *Manager {
	return &Manager{
		command:         command,
		args:            args,
		timeout:         time.Duration(timeoutMinutes) * time.Minute,
		hub:             hub,
		state:           events.ClaudeStateIdle,
		logDir:          "", // Empty means no file logging
		workDir:         "", // Empty means use current directory
		skipPermissions: skipPermissions,
		rotationConfig:  rotationConfig,
	}
}

// NewManagerWithContext creates a new Claude CLI manager with workspace/session context.
// This is the preferred constructor for multi-workspace support.
func NewManagerWithContext(hub ports.EventHub, command string, args []string, timeoutMinutes int, skipPermissions bool, workDir, workspaceID, sessionID string, rotationConfig *config.LogRotationConfig) *Manager {
	m := &Manager{
		command:         command,
		args:            args,
		timeout:         time.Duration(timeoutMinutes) * time.Minute,
		hub:             hub,
		state:           events.ClaudeStateIdle,
		workDir:         workDir,
		skipPermissions: skipPermissions,
		workspaceID:     workspaceID,
		sessionID:       sessionID,
		rotationConfig:  rotationConfig,
	}

	// Setup log directory in workspace
	if workDir != "" {
		logDir := filepath.Join(workDir, ".cdev", "logs")
		if err := os.MkdirAll(logDir, 0755); err == nil {
			m.logDir = logDir
		}
	}

	return m
}

// WorkspaceID returns the workspace ID for this manager.
func (m *Manager) WorkspaceID() string {
	return m.workspaceID
}

// SessionID returns the session ID for this manager.
func (m *Manager) SessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionID
}

// SetSessionID updates the session ID for this manager.
// This is called when the real session ID is detected from Claude's session file.
// Thread-safe: uses mutex to protect concurrent access.
func (m *Manager) SetSessionID(newID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionID = newID
}

// publishEvent publishes an event with workspace/session context.
// This ensures all events from this manager include proper context for multi-device support.
func (m *Manager) publishEvent(event *events.BaseEvent) {
	if m.hub == nil {
		return
	}
	// Set context on the event before publishing
	event.SetContext(m.workspaceID, m.sessionID)
	event.SetAgentType("claude")
	m.hub.Publish(event)
}

// SetLogDir enables file logging to the specified directory.
func (m *Manager) SetLogDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logDir = dir
}

// SetWorkDir sets the working directory for Claude CLI.
func (m *Manager) SetWorkDir(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workDir = dir
}

// SetOnPTYComplete sets a callback that's called when PTY streaming finishes.
// Used by session manager to emit claude_message with stop_reason.
func (m *Manager) SetOnPTYComplete(callback func(sessionID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPTYComplete = callback
}

// Start spawns Claude CLI with the given prompt (new session).
func (m *Manager) Start(ctx context.Context, prompt string) error {
	return m.StartWithSession(ctx, prompt, SessionModeNew, "", "")
}

// StartWithSession spawns Claude CLI with session control.
// permissionMode controls how Claude handles permissions:
// - "" or "default": Use skipPermissions config setting
// - "acceptEdits": Auto-accept file edits
// - "bypassPermissions": Skip all permission checks
// - "plan": Plan mode only
func (m *Manager) StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string, permissionMode string) error {
	// Handle interactive (PTY) mode - use PTY for true terminal-like permission prompts
	if permissionMode == "interactive" {
		return m.StartWithPTY(ctx, prompt, mode, sessionID)
	}

	m.mu.Lock()
	if m.state == events.ClaudeStateRunning {
		m.mu.Unlock()
		return domain.ErrClaudeAlreadyRunning
	}

	if prompt == "" {
		m.mu.Unlock()
		return domain.ErrInvalidPrompt
	}

	// Check for bash-only mode prefix - execute locally instead of calling Claude
	if strings.HasPrefix(prompt, "!") {
		// Strip the "!" prefix to get the actual bash command
		command := strings.TrimPrefix(prompt, "!")
		command = strings.TrimSpace(command)

		log.Info().
			Str("command", truncatePrompt(command, 50)).
			Str("mode", string(mode)).
			Str("session_id", sessionID).
			Msg("detected bash-only mode, executing locally")
		m.mu.Unlock() // Release lock before executing bash

		// Execute bash command locally and log in Claude Code format
		return m.executeBashLocally(ctx, command, mode, sessionID)
	}

	// Create cancellable context with timeout
	runCtx, cancel := context.WithTimeout(ctx, m.timeout)
	m.cancel = cancel

	// Build command arguments
	cmdArgs := make([]string, len(m.args))
	copy(cmdArgs, m.args)

	// Add session mode flags
	switch mode {
	case SessionModeContinue:
		// Continue requires session_id, uses --resume flag
		if sessionID == "" {
			m.mu.Unlock()
			cancel()
			return fmt.Errorf("session_id is required for continue mode")
		}
		cmdArgs = append(cmdArgs, "--resume", sessionID)
		// SessionModeNew: no flags needed, starts fresh conversation
	}

	// Handle permission mode
	// Priority: explicit permissionMode parameter > skipPermissions config
	if permissionMode != "" && permissionMode != "default" {
		// Use explicit --permission-mode flag for acceptEdits, bypassPermissions, plan
		cmdArgs = append(cmdArgs, "--permission-mode", permissionMode)
	} else if m.skipPermissions {
		// Fallback to config-based skip permissions
		cmdArgs = append(cmdArgs, "--dangerously-skip-permissions")
	}
	// Note: If neither is set, Claude will prompt for permissions interactively.
	// Since stdin is closed, this may cause Claude to hang or fail on permission prompts.
	// Consider using "acceptEdits" or "bypassPermissions" for non-interactive use.

	// Add the prompt as the last argument (always)
	cmdArgs = append(cmdArgs, prompt)

	// Debug: Log the full command being executed
	log.Debug().
		Str("command", m.command).
		Strs("args", cmdArgs).
		Str("work_dir", m.workDir).
		Bool("skip_permissions", m.skipPermissions).
		Msg("spawning claude process")

	// Create command
	m.cmd = exec.CommandContext(runCtx, m.command, cmdArgs...)

	// Set working directory
	if m.workDir != "" {
		m.cmd.Dir = m.workDir
	}

	// Setup process for platform-specific handling
	m.setupProcess(m.cmd)

	// Get stdin pipe for interactive input (needed for permission responses)
	stdin, err := m.cmd.StdinPipe()
	if err != nil {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}
	m.stdin = stdin

	// Get stdout and stderr pipes
	stdout, err := m.cmd.StdoutPipe()
	if err != nil {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := m.cmd.StderrPipe()
	if err != nil {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start the process
	if err := m.cmd.Start(); err != nil {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to start claude: %w", err)
	}

	// Always close stdin after starting Claude.
	// Claude CLI waits for stdin EOF when stdin is a pipe before processing.
	// For permission responses, we'll need a different approach (TBD).
	_ = stdin.Close()
	m.stdin = nil
	log.Debug().Msg("closed stdin to trigger EOF")

	m.state = events.ClaudeStateRunning
	m.currentPrompt = prompt
	m.pid = m.cmd.Process.Pid

	// Open log file if logging is enabled
	if m.logDir != "" {
		if err := os.MkdirAll(m.logDir, 0755); err != nil {
			log.Warn().Err(err).Msg("failed to create log directory")
		} else {
			logPath := filepath.Join(m.logDir, fmt.Sprintf("claude_%d.jsonl", m.pid))
			// Use lumberjack for log rotation if configured
			if m.rotationConfig != nil && m.rotationConfig.Enabled {
				m.logFile = &lumberjack.Logger{
					Filename:   logPath,
					MaxSize:    m.rotationConfig.MaxSizeMB,
					MaxBackups: m.rotationConfig.MaxBackups,
					MaxAge:     m.rotationConfig.MaxAgeDays,
					Compress:   m.rotationConfig.Compress,
				}
				log.Info().
					Str("path", logPath).
					Int("max_mb", m.rotationConfig.MaxSizeMB).
					Int("max_backups", m.rotationConfig.MaxBackups).
					Msg("claude log file created with rotation")
			} else {
				f, err := os.Create(logPath)
				if err != nil {
					log.Warn().Err(err).Msg("failed to create log file")
				} else {
					m.logFile = f
					log.Info().Str("path", logPath).Msg("claude log file created")
				}
			}
		}
	}
	m.mu.Unlock()

	log.Info().
		Str("prompt", truncatePrompt(prompt, 50)).
		Int("pid", m.pid).
		Msg("claude started")

	// Publish status event
	m.publishEvent(events.NewClaudeStatusEvent(events.ClaudeStateRunning, prompt, m.pid))

	// Stream output in goroutines
	// Session ID will be captured from first message and broadcast via event
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		m.streamOutput(stdout, events.StreamStdout)
	}()

	go func() {
		defer wg.Done()
		m.streamOutput(stderr, events.StreamStderr)
	}()

	// Wait for process to complete in a goroutine
	go func() {
		// Wait for output streaming to complete
		wg.Wait()

		// Wait for process
		err := m.cmd.Wait()

		m.mu.Lock()
		defer m.mu.Unlock()

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		// Check if context was cancelled (stopped by user)
		if runCtx.Err() == context.Canceled {
			m.state = events.ClaudeStateStopped
			m.publishEvent(events.NewClaudeStoppedEvent(exitCode))
			log.Info().Int("exit_code", exitCode).Msg("claude stopped by user")
		} else if runCtx.Err() == context.DeadlineExceeded {
			m.state = events.ClaudeStateError
			m.publishEvent(events.NewClaudeErrorEvent("timeout exceeded", exitCode))
			log.Warn().Msg("claude timed out")
		} else if exitCode != 0 {
			m.state = events.ClaudeStateError
			m.publishEvent(events.NewClaudeErrorEvent(fmt.Sprintf("exit code %d", exitCode), exitCode))
			log.Warn().Int("exit_code", exitCode).Msg("claude exited with error")
		} else {
			m.state = events.ClaudeStateIdle
			m.publishEvent(events.NewClaudeIdleEvent())
			log.Info().Msg("claude completed successfully")
		}

		// Close log file
		if m.logFile != nil {
			_ = m.logFile.Close()
			m.logFile = nil
		}

		// Close stdin
		if m.stdin != nil {
			_ = m.stdin.Close()
			m.stdin = nil
		}

		m.cmd = nil
		m.cancel = nil
		m.currentPrompt = ""
		m.pid = 0
		m.waitingForInput = false
		m.pendingToolUseID = ""
		m.pendingToolName = ""
		m.claudeSessionID = ""

		// Close PTY if used
		if m.ptmx != nil {
			_ = m.ptmx.Close()
			m.ptmx = nil
		}
		m.usePTY = false
	}()

	return nil
}

// StartWithPTY spawns Claude CLI with a pseudo-terminal for true interactive support.
// This allows permission prompts to work interactively from remote clients.
// The PTY makes Claude think it's running in a real terminal.
// If prompt is empty, Claude starts in interactive mode waiting for user input.
func (m *Manager) StartWithPTY(ctx context.Context, prompt string, mode SessionMode, sessionID string) error {
	m.mu.Lock()
	if m.state == events.ClaudeStateRunning {
		m.mu.Unlock()
		return domain.ErrClaudeAlreadyRunning
	}

	// Create cancellable context with timeout
	runCtx, cancel := context.WithTimeout(ctx, m.timeout)
	m.cancel = cancel

	// Build command arguments for PTY mode
	// For true terminal-like behavior, we run Claude WITHOUT -p flag
	// This gives us the full interactive UI with permission prompts
	var cmdArgs []string

	// Add session mode flags (resume)
	switch mode {
	case SessionModeContinue:
		if sessionID == "" {
			m.mu.Unlock()
			cancel()
			return fmt.Errorf("session_id is required for continue mode")
		}
		cmdArgs = append(cmdArgs, "--resume", sessionID)
	}

	// NOTE: We do NOT add the prompt as a CLI argument!
	// The prompt will be sent via PTY input after Claude starts

	log.Debug().
		Str("command", m.command).
		Strs("args", cmdArgs).
		Str("work_dir", m.workDir).
		Bool("pty_mode", true).
		Msg("spawning claude process with PTY")

	// Create command
	m.cmd = exec.CommandContext(runCtx, m.command, cmdArgs...)

	// Set working directory
	if m.workDir != "" {
		m.cmd.Dir = m.workDir
	}

	// Set up environment for PTY mode
	m.cmd.Env = append(os.Environ(),
		"TERM=xterm-256color", // Tell Claude it's in a capable terminal
		"COLORTERM=truecolor",
		"COLUMNS=120",
		"LINES=40",
	)

	// Start with PTY - this creates a pseudo-terminal
	ptmx, err := pty.Start(m.cmd)
	if err != nil {
		m.mu.Unlock()
		cancel()
		return fmt.Errorf("failed to start claude with PTY: %w", err)
	}

	// Set terminal size
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		log.Warn().Err(err).Msg("failed to set PTY size")
	}

	m.ptmx = ptmx
	m.usePTY = true
	m.state = events.ClaudeStateRunning
	m.currentPrompt = prompt
	m.pid = m.cmd.Process.Pid

	// Open log file if logging is enabled
	if m.logDir != "" {
		if err := os.MkdirAll(m.logDir, 0755); err != nil {
			log.Warn().Err(err).Msg("failed to create log directory")
		} else {
			logPath := filepath.Join(m.logDir, fmt.Sprintf("claude_%d.jsonl", m.pid))
			// Use lumberjack for log rotation if configured
			if m.rotationConfig != nil && m.rotationConfig.Enabled {
				m.logFile = &lumberjack.Logger{
					Filename:   logPath,
					MaxSize:    m.rotationConfig.MaxSizeMB,
					MaxBackups: m.rotationConfig.MaxBackups,
					MaxAge:     m.rotationConfig.MaxAgeDays,
					Compress:   m.rotationConfig.Compress,
				}
				log.Info().
					Str("path", logPath).
					Int("max_mb", m.rotationConfig.MaxSizeMB).
					Int("max_backups", m.rotationConfig.MaxBackups).
					Msg("claude log file created with rotation (PTY mode)")
			} else {
				f, err := os.Create(logPath)
				if err != nil {
					log.Warn().Err(err).Msg("failed to create log file")
				} else {
					m.logFile = f
					log.Info().Str("path", logPath).Msg("claude log file created (PTY mode)")
				}
			}
		}
	}
	m.mu.Unlock()

	log.Info().
		Str("prompt", truncatePrompt(prompt, 50)).
		Int("pid", m.pid).
		Bool("pty_mode", true).
		Msg("claude started with PTY")

	// Publish status event
	m.publishEvent(events.NewClaudeStatusEvent(events.ClaudeStateRunning, prompt, m.pid))

	// Stream PTY output in a goroutine
	go func() {
		m.streamPTYOutput(ptmx)
	}()

	// Send the initial prompt after Claude UI initializes
	go func() {
		// Wait for Claude to initialize (show the UI)
		// Claude needs ~3-4 seconds to fully initialize its TUI
		time.Sleep(4 * time.Second)

		// Send the prompt text followed by Enter (carriage return for TTY)
		if prompt != "" {
			log.Info().Str("prompt", truncatePrompt(prompt, 50)).Msg("sending initial prompt to PTY")
			// First send the prompt text
			_, _ = ptmx.Write([]byte(prompt))

			// Wait a moment for Claude's TUI to process the input
			time.Sleep(200 * time.Millisecond)

			// Then send Enter (carriage return) to submit the prompt
			log.Debug().Msg("sending Enter key to submit prompt")
			_, _ = ptmx.Write([]byte("\r"))
		}
	}()

	// Wait for process to complete in a goroutine
	go func() {
		err := m.cmd.Wait()

		m.mu.Lock()
		defer m.mu.Unlock()

		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		// Check if context was cancelled
		if runCtx.Err() == context.Canceled {
			m.state = events.ClaudeStateStopped
			m.publishEvent(events.NewClaudeStoppedEvent(exitCode))
			log.Info().Int("exit_code", exitCode).Msg("claude stopped by user (PTY mode)")
		} else if runCtx.Err() == context.DeadlineExceeded {
			m.state = events.ClaudeStateError
			m.publishEvent(events.NewClaudeErrorEvent("timeout exceeded", exitCode))
			log.Warn().Msg("claude timed out (PTY mode)")
		} else if exitCode != 0 {
			m.state = events.ClaudeStateError
			m.publishEvent(events.NewClaudeErrorEvent(fmt.Sprintf("exit code %d", exitCode), exitCode))
			log.Warn().Int("exit_code", exitCode).Msg("claude exited with error (PTY mode)")
		} else {
			m.state = events.ClaudeStateIdle
			m.publishEvent(events.NewClaudeIdleEvent())
			log.Info().Msg("claude completed successfully (PTY mode)")
		}

		// Cleanup
		if m.logFile != nil {
			_ = m.logFile.Close()
			m.logFile = nil
		}
		if m.ptmx != nil {
			_ = m.ptmx.Close()
			m.ptmx = nil
		}

		m.cmd = nil
		m.cancel = nil
		m.currentPrompt = ""
		m.pid = 0
		m.waitingForInput = false
		m.pendingToolUseID = ""
		m.pendingToolName = ""
		m.claudeSessionID = ""
		m.usePTY = false
	}()

	return nil
}

// streamPTYOutput reads from the PTY and publishes events.
// PTY output is raw terminal data with ANSI codes that we parse for mobile display.
func (m *Manager) streamPTYOutput(ptmx *os.File) {
	log.Debug().Msg("PTY output streaming started")

	reader := bufio.NewReader(ptmx)
	parser := NewPTYParser()

	// Store parser reference for state queries
	m.mu.Lock()
	m.ptyParser = parser
	m.mu.Unlock()

	lineCount := 0
	var lastState PTYState

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				log.Debug().Err(err).Msg("PTY read error")
			}
			break
		}

		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r") // Remove CRLF from PTY

		m.processPTYLine(line, parser, &lastState, &lineCount)
	}

	m.finishPTYStreaming(parser, lineCount)
}

// processPTYLine handles a single line of PTY output.
func (m *Manager) processPTYLine(line string, parser *PTYParser, lastState *PTYState, lineCount *int) {
	*lineCount++

	// Parse line with PTY parser (strips ANSI, detects prompts)
	parsedLine := parser.ansi.ParseLine(line)

	// Log session_id detection attempts
	cleanText := parsedLine.CleanText
	if strings.Contains(cleanText, "session") || strings.Contains(line, "session") {
		log.Info().
			Str("raw", truncatePrompt(line, 200)).
			Str("clean", truncatePrompt(cleanText, 200)).
			Msg("PTY: possible session_id in output")
	}
	// Also check for UUID patterns (8-4-4-4-12 format)
	if matched, _ := regexp.MatchString(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`, strings.ToLower(cleanText)); matched {
		log.Info().
			Str("clean", truncatePrompt(cleanText, 200)).
			Msg("PTY: UUID pattern detected in output")
	}
	permissionPrompt, ptyState := parser.ProcessLine(line)

	// Update manager state
	m.mu.Lock()
	m.ptyState = ptyState
	if permissionPrompt != nil {
		m.waitingForInput = true
		m.ptyPromptType = permissionPrompt.Type
	}
	m.mu.Unlock()

	// Emit pty_permission event when a prompt is detected
	if permissionPrompt != nil {
		// Check if any mobile clients are connected before waiting for response
		// Subscriber count > 1 means at least one mobile client (besides internal subscribers)
		subscriberCount := 0
		if m.hub != nil {
			subscriberCount = m.hub.SubscriberCount()
		}

		if subscriberCount <= 1 {
			// No mobile clients connected - auto-deny by sending escape
			log.Warn().
				Int("subscriber_count", subscriberCount).
				Str("type", string(permissionPrompt.Type)).
				Str("target", permissionPrompt.Target).
				Msg("No mobile clients connected - auto-denying PTY permission")

			// Send escape to cancel the permission prompt
			// Don't store the permission since we're auto-denying
			go func() {
				// Small delay to let the prompt fully render
				time.Sleep(100 * time.Millisecond)
				if err := m.SendPTYInput("\x1b"); err != nil {
					log.Warn().Err(err).Msg("Failed to send escape for auto-deny")
				}
			}()
			return
		}

		// Store the permission for reconnect scenarios
		m.mu.Lock()
		m.lastPTYPermission = permissionPrompt
		m.mu.Unlock()

		// Convert options to event format
		options := make([]events.PTYPromptOption, len(permissionPrompt.Options))
		for i, opt := range permissionPrompt.Options {
			options[i] = events.PTYPromptOption{
				Key:         opt.Key,
				Label:       opt.Label,
				Description: opt.Description,
				Selected:    opt.Selected,
			}
		}

		m.publishEvent(events.NewPTYPermissionEventWithSession(
			string(permissionPrompt.Type),
			permissionPrompt.Target,
			permissionPrompt.Description,
			permissionPrompt.Preview,
			m.sessionID,
			options,
		))

		log.Info().
			Str("type", string(permissionPrompt.Type)).
			Str("target", permissionPrompt.Target).
			Int("options", len(permissionPrompt.Options)).
			Int("subscriber_count", subscriberCount).
			Msg("PTY permission prompt detected - waiting for mobile response")
	}

	// Emit pty_state event only when state becomes idle (reduces noise)
	// Other state changes (thinking, permission, question) are too frequent
	if ptyState != *lastState {
		if ptyState == PTYStateIdle {
			m.publishEvent(events.NewPTYStateEventWithSession(
				string(ptyState),
				parser.IsWaitingForInput(),
				string(m.ptyPromptType),
				m.sessionID,
			))
		}
		*lastState = ptyState
	}

	// Detect spinner patterns and emit debounced pty_spinner events
	if symbol, message, hasSymbol, hasMessage := detectSpinnerParts(cleanText); hasSymbol || hasMessage {
		m.mu.Lock()
		now := time.Now()

		// Prefer newly detected parts, fall back to last emitted ones
		if !hasMessage {
			message = m.lastSpinnerText
		}
		if message == "" {
			// Ignore symbol-only updates until we have a full message
			m.mu.Unlock()
		} else {
			if !hasSymbol {
				symbol = m.lastSpinnerSymbol
			}

			// Debounce: emit if message changed, symbol changed, or 150ms passed
			shouldEmit := message != m.lastSpinnerText ||
				(symbol != "" && symbol != m.lastSpinnerSymbol) ||
				now.Sub(m.lastSpinnerTime) > 150*time.Millisecond
			if shouldEmit {
				m.lastSpinnerText = message
				if symbol != "" {
					m.lastSpinnerSymbol = symbol
				}
				m.lastSpinnerTime = now
				sessionID := m.sessionID
				m.mu.Unlock()

				text := message
				if symbol != "" {
					text = symbol + " " + message
				}

				log.Debug().
					Str("text", text).
					Str("symbol", symbol).
					Str("message", message).
					Str("session_id", sessionID).
					Msg("emitting pty_spinner event")

				m.publishEvent(events.NewPTYSpinnerEventWithSession(
					text,    // Full text like "✶ Vibing…"
					symbol,  // Just "✶"
					message, // Just "Vibing…"
					sessionID,
				))
			} else {
				log.Debug().
					Str("symbol", symbol).
					Str("message", message).
					Msg("pty_spinner debounced (same message within 150ms)")
				m.mu.Unlock()
			}
		}
	}

	// Write to log file if enabled
	m.mu.RLock()
	if m.logFile != nil {
		_, _ = m.logFile.Write([]byte(line + "\n"))
	}
	m.mu.RUnlock()

	// Log PTY output, but filter duplicate lines to reduce noise
	m.mu.Lock()
	isDuplicate := cleanText == m.lastLogLine
	m.lastLogLine = cleanText
	m.mu.Unlock()

	if !isDuplicate && cleanText != "" {
		log.Debug().
			Str("clean", truncatePrompt(cleanText, 80)).
			Str("state", string(ptyState)).
			Msg("PTY output")
	}
}

// finishPTYStreaming handles cleanup when PTY streaming ends.
func (m *Manager) finishPTYStreaming(parser *PTYParser, lineCount int) {
	// Clean up parser reference
	m.mu.Lock()
	m.ptyParser = nil
	sessionID := m.sessionID
	m.mu.Unlock()

	// Emit final pty_state event with idle state so cdev-ios knows Claude finished
	m.publishEvent(events.NewPTYStateEventWithSession(
		string(PTYStateIdle),
		false, // not waiting for input
		"",    // no prompt type
		sessionID,
	))

	// Call completion callback if set (to emit claude_message with stop_reason)
	// This is only called when PTY streaming actually ends (Claude exits)
	m.mu.RLock()
	callback := m.onPTYComplete
	m.mu.RUnlock()
	if callback != nil {
		callback(sessionID)
	}

	log.Debug().Int("lines_read", lineCount).Msg("PTY output streaming finished")
	log.Info().Str("session_id", sessionID).Msg("emitted pty_state idle - Claude PTY session finished")
}

// SendPTYInput sends input to the PTY (for interactive responses).
// This allows remote clients to respond to permission prompts.
// For regular text input, a carriage return is automatically appended.
// For special keys (escape sequences, control characters), the input is sent as-is.
func (m *Manager) SendPTYInput(input string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != events.ClaudeStateRunning {
		return domain.ErrClaudeNotRunning
	}

	if !m.usePTY || m.ptmx == nil {
		return fmt.Errorf("not running in PTY mode")
	}

	// Check if input is a key name (e.g., "enter", "escape") and convert to escape sequence
	// This allows cdev-ios to send input: "enter" as a convenience
	keyNameToSequence := map[string]string{
		"enter": "\r", "return": "\r",
		"escape": "\x1b", "esc": "\x1b",
		"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
		"tab": "\t", "backspace": "\x7f", "space": " ",
	}
	if seq, ok := keyNameToSequence[strings.ToLower(input)]; ok {
		input = seq
	}

	// Determine if this is a special key/control sequence that should be sent as-is
	isSpecialKey := false
	if len(input) > 0 {
		firstByte := input[0]
		// Control characters (0x00-0x1F) and escape sequences (\x1b...) are special
		if firstByte < 0x20 || firstByte == 0x7f {
			isSpecialKey = true
		}
	}

	// Auto-append carriage return only for regular text input
	if !isSpecialKey && !strings.HasSuffix(input, "\r") && !strings.HasSuffix(input, "\n") {
		input = input + "\r"
	}

	// Write input to PTY (simulates keyboard input)
	_, err := m.ptmx.Write([]byte(input))
	if err != nil {
		return fmt.Errorf("failed to write to PTY: %w", err)
	}

	// Reset waiting state after user responds
	// Note: we already hold m.mu from the function entry, no need to lock again
	m.waitingForInput = false
	m.ptyPromptType = ""
	m.lastPTYPermission = nil // Clear pending permission

	// Reset parser state if available
	if m.ptyParser != nil {
		m.ptyParser.SetState(PTYStateIdle)
	}

	log.Info().
		Str("input", truncatePrompt(input, 50)).
		Bool("special_key", isSpecialKey).
		Msg("sent input to claude PTY")

	return nil
}

// IsPTYMode returns true if running in PTY mode.
func (m *Manager) IsPTYMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.usePTY
}

// GetPendingPTYPermission returns the pending PTY permission prompt if any.
// Used when client reconnects to re-show the permission dialog.
func (m *Manager) GetPendingPTYPermission() *PTYPermissionPrompt {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastPTYPermission
}

// HasPendingPermission returns true if there's a pending permission request.
func (m *Manager) HasPendingPermission() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastPTYPermission != nil
}

// Stop gracefully terminates the running Claude process.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	sessionID := m.sessionID
	wasPTY := m.usePTY
	m.mu.Unlock()

	m.mu.Lock()
	if m.state != events.ClaudeStateRunning {
		m.mu.Unlock()
		return domain.ErrClaudeNotRunning
	}

	if m.cancel != nil {
		m.cancel()
	}

	// Try graceful termination first
	if m.cmd != nil && m.cmd.Process != nil {
		if err := m.terminateProcess(m.cmd); err != nil {
			log.Warn().Err(err).Msg("graceful termination failed, will force kill")
		}
	}
	m.mu.Unlock()

	// Emit pty_state idle when stopped (backup in case streamPTYOutput doesn't emit it)
	if wasPTY && sessionID != "" {
		m.publishEvent(events.NewPTYStateEventWithSession(
			string(PTYStateIdle),
			false,
			"",
			sessionID,
		))
		log.Info().Str("session_id", sessionID).Msg("emitted pty_state idle on Stop()")
	}

	return nil
}

// Kill forcefully terminates the Claude process.
func (m *Manager) Kill() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil && m.cmd.Process != nil {
		return m.killProcess(m.cmd)
	}
	return nil
}

// State returns the current state.
func (m *Manager) State() events.ClaudeState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IsRunning returns true if Claude is currently running.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state == events.ClaudeStateRunning
}

// CurrentPrompt returns the current prompt if running.
func (m *Manager) CurrentPrompt() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentPrompt
}

// PID returns the process ID if running.
func (m *Manager) PID() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pid
}

// streamOutput reads from the pipe and publishes log events.
func (m *Manager) streamOutput(pipe io.ReadCloser, stream events.StreamType) {
	log.Debug().Str("stream", string(stream)).Msg("streamOutput goroutine started")

	scanner := bufio.NewScanner(pipe)
	// Increase buffer size for long lines (10MB to handle base64 images)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)

	lineCount := 0
	for scanner.Scan() {
		lineCount++
		line := scanner.Text()

		// Try to parse JSON for stdout to provide structured data to clients
		var parsed *events.ParsedClaudeMessage
		if stream == events.StreamStdout {
			parsed = m.parseClaudeJSON(line)
		}

		// Publish event with optional parsed data
		if parsed != nil {
			m.publishEvent(events.NewClaudeLogEventWithParsed(line, stream, parsed))

			// Also emit claude_message event for structured UI rendering
			if msgPayload := m.convertToClaudeMessagePayload(parsed); msgPayload != nil {
				log.Debug().
					Str("type", msgPayload.Type).
					Str("session_id", msgPayload.SessionID).
					Int("content_blocks", len(msgPayload.Content)).
					Msg("emitting claude_message event")
				m.publishEvent(events.NewClaudeMessageEventFull(*msgPayload))
			}
		} else {
			m.publishEvent(events.NewClaudeLogEvent(line, stream))
		}

		// Parse stdout for interactive tool use detection
		if stream == events.StreamStdout {
			m.parseAndDetectToolUse(line)
		}

		// Write to log file if enabled (raw JSONL format)
		m.mu.RLock()
		if m.logFile != nil {
			if stream == events.StreamStdout {
				// Write raw JSON line as-is for stdout (Claude's stream-json output)
				_, _ = m.logFile.Write([]byte(line + "\n"))
			} else {
				// Wrap stderr in JSON for consistent parsing
				stderrEntry := map[string]interface{}{
					"_type":      "stderr",
					"_stream":    "stderr",
					"_content":   line,
					"_timestamp": time.Now().UTC().Format(time.RFC3339),
				}
				if stderrJSON, err := json.Marshal(stderrEntry); err == nil {
					_, _ = m.logFile.Write([]byte(string(stderrJSON) + "\n"))
				}
			}
		}
		m.mu.RUnlock()

		if stream == events.StreamStdout {
			log.Debug().Str("line", truncatePrompt(line, 100)).Msg("claude stdout")
		} else {
			log.Debug().Str("line", truncatePrompt(line, 100)).Msg("claude stderr")
		}
	}

	if err := scanner.Err(); err != nil {
		log.Warn().Err(err).Str("stream", string(stream)).Msg("error reading claude output")
	}

	log.Debug().
		Str("stream", string(stream)).
		Int("lines_read", lineCount).
		Msg("streamOutput goroutine finished")
}

// SendResponse sends a user response to Claude's stdin for interactive prompts.
func (m *Manager) SendResponse(toolUseID string, response string, isError bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != events.ClaudeStateRunning {
		return domain.ErrClaudeNotRunning
	}

	if m.stdin == nil {
		return fmt.Errorf("stdin pipe not available")
	}

	if !m.waitingForInput {
		return fmt.Errorf("claude is not waiting for input")
	}

	// Build the response in Claude's expected format
	toolResult := map[string]interface{}{
		"type": "user",
		"content": []map[string]interface{}{
			{
				"type":        "tool_result",
				"tool_use_id": toolUseID,
				"content":     response,
				"is_error":    isError,
			},
		},
	}

	data, err := json.Marshal(toolResult)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// Write to stdin with newline
	if _, err := m.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	log.Info().
		Str("tool_use_id", toolUseID).
		Str("response", truncatePrompt(response, 50)).
		Msg("sent response to claude")

	// Write to log file (JSON format for consistent parsing)
	if m.logFile != nil {
		stdinEntry := map[string]interface{}{
			"_type":      "stdin",
			"_stream":    "stdin",
			"_content":   json.RawMessage(data),
			"_timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		if stdinJSON, err := json.Marshal(stdinEntry); err == nil {
			_, _ = m.logFile.Write([]byte(string(stdinJSON) + "\n"))
		}
	}

	m.waitingForInput = false
	m.pendingToolUseID = ""
	m.pendingToolName = ""

	return nil
}

// IsWaitingForInput returns true if Claude is waiting for user input.
func (m *Manager) IsWaitingForInput() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.waitingForInput
}

// GetPendingToolUse returns the pending tool use info if waiting for input.
func (m *Manager) GetPendingToolUse() (toolUseID, toolName string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.pendingToolUseID, m.pendingToolName
}

// ClaudeSessionID returns the current Claude CLI session ID.
func (m *Manager) ClaudeSessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.claudeSessionID
}

// truncatePrompt truncates a string for logging.
func truncatePrompt(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// spinnerSymbols are the characters used in Claude's thinking animation
var spinnerSymbols = []string{"✳", "✶", "✻", "✽", "✢", "·"}

// detectSpinnerParts extracts spinner symbol/message parts from a line.
// It handles carriage-return updates where symbol and message arrive separately.
// Returns symbol/message along with flags indicating what was found.
func detectSpinnerParts(line string) (symbol, message string, hasSymbol, hasMessage bool) {
	segments := strings.Split(line, "\r")
	for _, segment := range segments {
		seg := strings.TrimSpace(segment)
		if seg == "" {
			continue
		}
		// Skip non-spinner UI markers (not part of spinner animation)
		if strings.HasPrefix(seg, "⎿") || strings.HasPrefix(seg, "⏺") {
			continue
		}

		// Check for spinner symbol at the start
		for _, sym := range spinnerSymbols {
			if strings.HasPrefix(seg, sym) {
				hasSymbol = true
				symbol = sym
				msg := strings.TrimSpace(strings.TrimPrefix(seg, sym))
				if looksLikeSpinnerMessage(msg) {
					hasMessage = true
					message = msg
				}
				goto nextSegment
			}
		}

		// Message-only line (newer Claude output formats)
		if looksLikeSpinnerMessage(seg) {
			hasMessage = true
			message = seg
		}

	nextSegment:
	}

	return symbol, message, hasSymbol, hasMessage
}

func looksLikeSpinnerMessage(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if !hasLikelySpinnerPrefix(text) {
		return false
	}
	if !(strings.Contains(text, "…") || strings.Contains(text, "...")) {
		return false
	}
	if strings.HasSuffix(text, "…") || strings.HasSuffix(text, "...") {
		return true
	}
	if strings.Contains(text, "(esc to interrupt") || strings.Contains(text, "(ctrl+c to interrupt") {
		return true
	}
	// Avoid prompt fragments and overly long lines
	if strings.Contains(text, "❯") {
		return false
	}
	if len([]rune(text)) > 80 {
		return false
	}
	return true
}

func hasLikelySpinnerPrefix(text string) bool {
	runes := []rune(text)
	if len(runes) < 4 {
		return false
	}
	first := runes[0]
	if unicode.IsLetter(first) && !unicode.IsUpper(first) {
		return false
	}
	return true
}

// claudeMessage represents a parsed Claude output message.
type claudeMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"` // snake_case in stream-json output
	Message   struct {
		Content []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
			Text  string          `json:"text,omitempty"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	} `json:"message"`
}

// parseAndDetectToolUse parses Claude output and detects if it's waiting for input.
func (m *Manager) parseAndDetectToolUse(line string) {
	var msg claudeMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return // Not valid JSON or not a message we care about
	}

	// Capture session ID from the first message that contains it
	if msg.SessionID != "" {
		m.mu.Lock()
		if m.claudeSessionID == "" {
			m.claudeSessionID = msg.SessionID
			log.Info().Str("claude_session_id", msg.SessionID).Msg("captured Claude session ID")

			// Broadcast session info event to WebSocket clients
			m.mu.Unlock()
			m.publishEvent(events.NewClaudeSessionInfoEvent(msg.SessionID, "", ""))
		} else {
			m.mu.Unlock()
		}
	}

	// Check for assistant message with tool_use that requires input/permission
	if msg.Type == "assistant" && msg.Message.StopReason == "tool_use" {
		for _, content := range msg.Message.Content {
			if content.Type == "tool_use" {
				// Tools that require user input (questions)
				interactiveTools := map[string]bool{
					"AskUserQuestion": true,
				}

				// Tools that require permission approval
				permissionTools := map[string]bool{
					"Write":        true,
					"Edit":         true,
					"Bash":         true,
					"Read":         true,
					"Glob":         true,
					"Grep":         true,
					"WebFetch":     true,
					"WebSearch":    true,
					"NotebookEdit": true,
					"TodoWrite":    true,
				}

				if interactiveTools[content.Name] {
					m.mu.Lock()
					m.waitingForInput = true
					m.pendingToolUseID = content.ID
					m.pendingToolName = content.Name
					m.mu.Unlock()

					log.Info().
						Str("tool_name", content.Name).
						Str("tool_use_id", content.ID).
						Msg("claude waiting for user input")

					// Publish event to notify clients
					m.publishEvent(events.NewClaudeWaitingEvent(content.ID, content.Name, string(content.Input)))
					return
				}

				if permissionTools[content.Name] || isMCPToolName(content.Name) {
					m.mu.Lock()
					m.waitingForInput = true
					m.pendingToolUseID = content.ID
					m.pendingToolName = content.Name
					m.mu.Unlock()

					// Generate a human-readable description
					description := generateToolDescription(content.Name, content.Input)

					log.Info().
						Str("tool_name", content.Name).
						Str("tool_use_id", content.ID).
						Msg("claude requesting permission")

					// Publish permission event to notify clients
					m.publishEvent(events.NewClaudePermissionEvent(content.ID, content.Name, string(content.Input), description))
					return
				}
			}
		}
	}

	// Check for tool result received
	if msg.Type == "user" {
		// This indicates Claude received a tool result
		// Reset waiting state as Claude will continue or ask again
		m.mu.Lock()
		if m.waitingForInput {
			m.waitingForInput = false
		}
		m.mu.Unlock()
	}
}

// generateToolDescription creates a human-readable description for a tool use.
func generateToolDescription(toolName string, input json.RawMessage) string {
	var params map[string]interface{}
	if err := json.Unmarshal(input, &params); err != nil {
		return fmt.Sprintf("Use %s tool", toolName)
	}

	if isMCPToolName(toolName) {
		if target, ok := params["url"].(string); ok && target != "" {
			return fmt.Sprintf("Use MCP tool %s on %s", toolName, target)
		}
		if target, ok := params["path"].(string); ok && target != "" {
			return fmt.Sprintf("Use MCP tool %s on %s", toolName, target)
		}
		if target, ok := params["selector"].(string); ok && target != "" {
			return fmt.Sprintf("Use MCP tool %s with selector %s", toolName, target)
		}
		return fmt.Sprintf("Use MCP tool: %s", toolName)
	}

	switch toolName {
	case "Write":
		if path, ok := params["file_path"].(string); ok {
			return fmt.Sprintf("Write to file: %s", path)
		}
	case "Edit":
		if path, ok := params["file_path"].(string); ok {
			return fmt.Sprintf("Edit file: %s", path)
		}
	case "Read":
		if path, ok := params["file_path"].(string); ok {
			return fmt.Sprintf("Read file: %s", path)
		}
	case "Bash":
		if cmd, ok := params["command"].(string); ok {
			if len(cmd) > 100 {
				cmd = cmd[:100] + "..."
			}
			return fmt.Sprintf("Run command: %s", cmd)
		}
	case "Glob":
		if pattern, ok := params["pattern"].(string); ok {
			return fmt.Sprintf("Search files: %s", pattern)
		}
	case "Grep":
		if pattern, ok := params["pattern"].(string); ok {
			return fmt.Sprintf("Search content: %s", pattern)
		}
	case "WebFetch":
		if url, ok := params["url"].(string); ok {
			return fmt.Sprintf("Fetch URL: %s", url)
		}
	case "WebSearch":
		if query, ok := params["query"].(string); ok {
			return fmt.Sprintf("Web search: %s", query)
		}
	}

	return fmt.Sprintf("Use %s tool", toolName)
}

func isMCPToolName(toolName string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(toolName)), "mcp__")
}

// claudeStreamMessage represents a message from Claude CLI stream-json output.
// This is the full structure for parsing.
type claudeStreamMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`   // "compact_boundary" for context compaction marker
	UserType  string `json:"userType,omitempty"`  // "external" for auto-generated messages (context compaction)
	SessionID string `json:"sessionId,omitempty"` // camelCase in JSONL files
	Content   string `json:"content,omitempty"`   // For system messages (e.g., "Conversation compacted")
	Timestamp string `json:"timestamp,omitempty"` // ISO 8601 timestamp from Claude CLI
	Message   struct {
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
		Model      string          `json:"model,omitempty"` // Model used (e.g., "claude-opus-4-5-20251101")
		Usage      struct {
			OutputTokens int `json:"output_tokens"`
			InputTokens  int `json:"input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	// Result fields (for type="result")
	CostUSD     float64 `json:"costUsd,omitempty"`
	DurationMS  int64   `json:"durationMs,omitempty"`
	DurationAPI int64   `json:"duration_api_ms,omitempty"`
	// Init fields (for type="init")
	Model   string `json:"model,omitempty"`
	Version string `json:"version,omitempty"`
}

// contextCompactionPrefix is the prefix for context compaction messages.
const contextCompactionPrefix = "This session is being continued from a previous conversation"

// convertToClaudeMessagePayload converts a ParsedClaudeMessage to ClaudeMessagePayload.
// Returns nil for message types that shouldn't be emitted as claude_message events.
func (m *Manager) convertToClaudeMessagePayload(parsed *events.ParsedClaudeMessage) *events.ClaudeMessagePayload {
	if parsed == nil {
		return nil
	}

	// Only convert assistant, user, and result messages
	switch parsed.Type {
	case "assistant", "user", "result":
		// Continue processing
	default:
		return nil
	}

	payload := &events.ClaudeMessagePayload{
		SessionID:           parsed.SessionID,
		Type:                parsed.Type,
		StopReason:          parsed.StopReason,
		IsContextCompaction: parsed.IsContextCompaction,
		Timestamp:           parsed.Timestamp,
		Model:               parsed.Model,
	}

	// Set role based on type
	switch parsed.Type {
	case "assistant":
		payload.Role = "assistant"
	case "user":
		payload.Role = "user"
	}

	// Convert content blocks
	for _, block := range parsed.Content {
		content := events.ClaudeMessageContent{
			Type: block.Type,
		}

		switch block.Type {
		case "text", "thinking":
			content.Text = block.Text
		case "tool_use":
			content.ToolName = block.ToolName
			content.ToolID = block.ToolID
			// Parse tool input JSON into map
			if block.ToolInput != "" {
				var inputMap map[string]interface{}
				if err := json.Unmarshal([]byte(block.ToolInput), &inputMap); err == nil {
					normalizeClaudeToolInput(content.ToolName, inputMap)
					content.ToolInput = inputMap
				}
			}
		case "tool_result":
			content.ToolUseID = block.ToolID
			content.Content = block.Text
		}

		payload.Content = append(payload.Content, content)
	}

	return payload
}

func normalizeClaudeToolInput(toolName string, input map[string]interface{}) {
	if len(input) == 0 {
		return
	}

	switch toolName {
	case "view_image":
		if path, ok := input["path"].(string); ok {
			if compact := compactDotCdevPath(path); compact != "" {
				input["path"] = compact
			}
		}
	}
}

func compactDotCdevPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "/.cdev/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// parseClaudeJSON parses a line of Claude CLI stream-json output and returns structured data.
func (m *Manager) parseClaudeJSON(line string) *events.ParsedClaudeMessage {
	var msg claudeStreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return nil // Not valid JSON
	}

	// Skip empty type (invalid message)
	if msg.Type == "" {
		return nil
	}

	parsed := &events.ParsedClaudeMessage{
		Type:         msg.Type,
		SessionID:    msg.SessionID,
		StopReason:   msg.Message.StopReason,
		CostUSD:      msg.CostUSD,
		DurationMS:   msg.DurationMS,
		OutputTokens: msg.Message.Usage.OutputTokens,
		Timestamp:    msg.Timestamp,
		Model:        msg.Message.Model,
	}

	// Detect context compaction boundary (system message)
	// Example: {"type":"system","subtype":"compact_boundary","content":"Conversation compacted"}
	if msg.Type == "system" && msg.Subtype == "compact_boundary" {
		parsed.IsContextCompaction = true
	}

	// Parse content blocks if present
	if msg.Message.Content != nil {
		var contentBlocks []struct {
			Type  string          `json:"type"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Text  string          `json:"text,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		}
		if err := json.Unmarshal(msg.Message.Content, &contentBlocks); err == nil {
			for _, block := range contentBlocks {
				parsedBlock := events.ParsedContentBlock{
					Type: block.Type,
				}
				switch block.Type {
				case "text":
					parsedBlock.Text = block.Text
					// Check for <thinking> tags in text content
					if strings.Contains(block.Text, "<thinking>") {
						parsed.IsThinking = true
					}
				case "thinking":
					parsedBlock.Text = block.Text
					// Content type "thinking" means extended thinking mode
					parsed.IsThinking = true
				case "tool_use":
					parsedBlock.ToolName = block.Name
					parsedBlock.ToolID = block.ID
					if block.Input != nil {
						parsedBlock.ToolInput = string(block.Input)
					}
				case "tool_result":
					// Tool results have content that could be text
					if block.Text != "" {
						parsedBlock.Text = block.Text
					}
				}
				parsed.Content = append(parsed.Content, parsedBlock)
			}
		}
	}

	// Detect context compaction continuation message (user message with auto-generated summary)
	// Example: {"type":"user","userType":"external","message":{"content":"This session is being continued..."}}
	if msg.Type == "user" && msg.UserType == "external" {
		for _, block := range parsed.Content {
			if block.Type == "text" && strings.HasPrefix(block.Text, contextCompactionPrefix) {
				parsed.IsContextCompaction = true
				break
			}
		}
	}

	return parsed
}

// executeBashLocally executes a bash command locally without calling Claude AI.
// It logs the execution in Claude Code's format for session compatibility.
func (m *Manager) executeBashLocally(ctx context.Context, command string, mode SessionMode, sessionID string) error {
	// Generate session ID if not provided (for "new" mode)
	if sessionID == "" {
		sessionID = generateUUID()
		log.Info().Str("generated_session_id", sessionID).Msg("generated new session ID for bash execution")
	}

	// Set up working directory
	workDir := m.workDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	log.Info().
		Str("command", truncatePrompt(command, 50)).
		Str("work_dir", workDir).
		Msg("executing bash command locally")

	// Execute bash command
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = workDir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	duration := time.Since(startTime)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	log.Info().
		Int("exit_code", exitCode).
		Dur("duration", duration).
		Msg("bash command completed")

	// Append to JSONL session file using the shared helper (correct Claude Code format)
	if appendErr := AppendBashToSession(workDir, sessionID, command, stdout.String(), stderr.String()); appendErr != nil {
		log.Warn().Err(appendErr).Str("session_id", sessionID).Msg("failed to append bash command to session file")
	}

	// Emit claude_message events for bash input and output
	log.Info().
		Str("session_id", sessionID).
		Str("command", truncatePrompt(command, 50)).
		Msg("emitting bash input claude_message event")

	// Event 1: Bash input
	inputPayload := events.ClaudeMessagePayload{
		SessionID: sessionID,
		Type:      "user",
		Role:      "user",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Content: []events.ClaudeMessageContent{
			{
				Type: "text",
				Text: fmt.Sprintf("<bash-input>%s</bash-input>", command),
			},
		},
	}
	m.publishEvent(events.NewClaudeMessageEventFull(inputPayload))
	log.Debug().Msg("bash input event published to hub")

	// Event 2: Bash output
	log.Info().
		Str("session_id", sessionID).
		Int("stdout_len", stdout.Len()).
		Int("stderr_len", stderr.Len()).
		Msg("emitting bash output claude_message event")

	outputPayload := events.ClaudeMessagePayload{
		SessionID: sessionID,
		Type:      "user",
		Role:      "user",
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Content: []events.ClaudeMessageContent{
			{
				Type: "text",
				Text: fmt.Sprintf("<bash-stdout>%s</bash-stdout><bash-stderr>%s</bash-stderr>", stdout.String(), stderr.String()),
			},
		},
	}
	m.publishEvent(events.NewClaudeMessageEventFull(outputPayload))
	log.Debug().Msg("bash output event published to hub")

	// Emit completion event
	m.publishEvent(events.NewClaudeIdleEvent())
	log.Info().Msg("bash execution completed, events emitted")

	if exitCode != 0 {
		return fmt.Errorf("bash command exited with code %d", exitCode)
	}

	return nil
}

// generateUUID generates a UUID v4 string.
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback to timestamp-based UUID
		now := time.Now().UnixNano()
		return fmt.Sprintf("%x-%x-%x-%x-%x",
			now&0xFFFFFFFF,
			now>>32&0xFFFF,
			now>>48&0xFFFF,
			now>>56&0xFFFF,
			now>>60&0xFFFFFFFFFFFF)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant bits
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
