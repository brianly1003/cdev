// Package claude implements the Claude CLI process manager.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
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
	logFile       *os.File

	// Session tracking
	claudeSessionID string // Session ID from Claude CLI output

	// Interactive state tracking
	waitingForInput  bool
	pendingToolUseID string
	pendingToolName  string
}

// NewManager creates a new Claude CLI manager (legacy constructor for backward compatibility).
func NewManager(command string, args []string, timeoutMinutes int, hub ports.EventHub, skipPermissions bool) *Manager {
	return &Manager{
		command:         command,
		args:            args,
		timeout:         time.Duration(timeoutMinutes) * time.Minute,
		hub:             hub,
		state:           events.ClaudeStateIdle,
		logDir:          "", // Empty means no file logging
		workDir:         "", // Empty means use current directory
		skipPermissions: skipPermissions,
	}
}

// NewManagerWithContext creates a new Claude CLI manager with workspace/session context.
// This is the preferred constructor for multi-workspace support.
func NewManagerWithContext(hub ports.EventHub, command string, args []string, timeoutMinutes int, skipPermissions bool, workDir, workspaceID, sessionID string) *Manager {
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
	return m.sessionID
}

// publishEvent publishes an event with workspace/session context.
// This ensures all events from this manager include proper context for multi-device support.
func (m *Manager) publishEvent(event *events.BaseEvent) {
	if m.hub == nil {
		return
	}
	// Set context on the event before publishing
	event.SetContext(m.workspaceID, m.sessionID)
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

// Start spawns Claude CLI with the given prompt (new session).
func (m *Manager) Start(ctx context.Context, prompt string) error {
	return m.StartWithSession(ctx, prompt, SessionModeNew, "")
}

// StartWithSession spawns Claude CLI with session control.
func (m *Manager) StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string) error {
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
			return fmt.Errorf("session_id is required for continue mode")
		}
		cmdArgs = append(cmdArgs, "--resume", sessionID)
		// SessionModeNew: no flags needed, starts fresh conversation
	}

	// Add skip permissions flag if enabled
	if m.skipPermissions {
		cmdArgs = append(cmdArgs, "--dangerously-skip-permissions")
	}

	// Add the prompt as the last argument
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

	// If skip permissions is enabled, we won't need stdin for responses
	// Claude CLI waits for stdin EOF when stdin is a pipe, so close it
	// to prevent blocking
	closeStdinAfterStart := m.skipPermissions

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

	// Close stdin if not needed (in skip permissions mode)
	// Claude CLI waits for stdin EOF when stdin is a pipe
	if closeStdinAfterStart {
		stdin.Close()
		m.stdin = nil
		log.Debug().Msg("closed stdin (skip permissions mode)")
	}

	m.state = events.ClaudeStateRunning
	m.currentPrompt = prompt
	m.pid = m.cmd.Process.Pid

	// Open log file if logging is enabled
	if m.logDir != "" {
		if err := os.MkdirAll(m.logDir, 0755); err != nil {
			log.Warn().Err(err).Msg("failed to create log directory")
		} else {
			logPath := filepath.Join(m.logDir, fmt.Sprintf("claude_%d.jsonl", m.pid))
			f, err := os.Create(logPath)
			if err != nil {
				log.Warn().Err(err).Msg("failed to create log file")
			} else {
				m.logFile = f
				log.Info().Str("path", logPath).Msg("claude log file created")
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
			m.logFile.Close()
			m.logFile = nil
		}

		// Close stdin
		if m.stdin != nil {
			m.stdin.Close()
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
	}()

	return nil
}

// Stop gracefully terminates the running Claude process.
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != events.ClaudeStateRunning {
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
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

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
				m.logFile.WriteString(line + "\n")
			} else {
				// Wrap stderr in JSON for consistent parsing
				stderrEntry := map[string]interface{}{
					"_type":      "stderr",
					"_stream":    "stderr",
					"_content":   line,
					"_timestamp": time.Now().UTC().Format(time.RFC3339),
				}
				if stderrJSON, err := json.Marshal(stderrEntry); err == nil {
					m.logFile.WriteString(string(stderrJSON) + "\n")
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
			m.logFile.WriteString(string(stdinJSON) + "\n")
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

				if permissionTools[content.Name] {
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

// claudeStreamMessage represents a message from Claude CLI stream-json output.
// This is the full structure for parsing.
type claudeStreamMessage struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`    // "compact_boundary" for context compaction marker
	UserType  string `json:"userType,omitempty"`   // "external" for auto-generated messages (context compaction)
	SessionID string `json:"session_id,omitempty"` // snake_case in stream-json output
	Content   string `json:"content,omitempty"`    // For system messages (e.g., "Conversation compacted")
	Message   struct {
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
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

	m.mu.Lock()

	// Set up working directory
	workDir := m.workDir
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			m.mu.Unlock()
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Construct Claude Code session file path
	// Transform: /Users/brianly/Projects/cdev -> -Users-brianly-Projects-cdev
	projectDirName := strings.TrimPrefix(workDir, "/")
	projectDirName = strings.ReplaceAll(projectDirName, "/", "-")
	projectDirName = "-" + projectDirName

	// Claude Code session file path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	claudeProjectsDir := filepath.Join(homeDir, ".claude", "projects", projectDirName)
	sessionFilePath := filepath.Join(claudeProjectsDir, fmt.Sprintf("%s.jsonl", sessionID))

	// Ensure the Claude projects directory exists
	if err := os.MkdirAll(claudeProjectsDir, 0700); err != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to create Claude projects directory: %w", err)
	}

	// Open or create the session file for appending
	logFile, err := os.OpenFile(sessionFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("failed to open Claude session file: %w", err)
	}
	defer logFile.Close()

	log.Info().
		Str("session_file", sessionFilePath).
		Str("session_id", sessionID).
		Msg("appending to Claude Code session file")

	m.mu.Unlock()

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
	err = cmd.Run()
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

	// Get git branch for logging
	gitBranch := "main" // Default
	branchCmd := exec.Command("git", "branch", "--show-current")
	branchCmd.Dir = workDir
	if branchOutput, err := branchCmd.Output(); err == nil {
		gitBranch = strings.TrimSpace(string(branchOutput))
	}

	// Generate UUIDs for message chain
	caveatUUID := generateUUID()
	inputUUID := generateUUID()
	outputUUID := generateUUID()

	// Get parent UUID (use sessionID as parent for now)
	parentUUID := sessionID

	// Current timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)

	// Helper function to marshal JSON without HTML escaping
	marshalWithoutEscape := func(v interface{}) ([]byte, error) {
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(v); err != nil {
			return nil, err
		}
		// Encoder.Encode adds a newline, so trim it
		return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
	}

	// Write 3 JSONL messages in Claude Code format
	// Message 1: Caveat
	caveatMsg := map[string]interface{}{
		"parentUuid":  parentUUID,
		"isSidechain": false,
		"userType":    "external",
		"cwd":         workDir,
		"sessionId":   sessionID,
		"version":     "2.0.71", // Claude Code version
		"gitBranch":   gitBranch,
		"type":        "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": "Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to.",
		},
		"isMeta":    true,
		"uuid":      caveatUUID,
		"timestamp": timestamp,
	}
	if data, err := marshalWithoutEscape(caveatMsg); err == nil {
		logFile.WriteString(string(data) + "\n")
	}

	// Message 2: Bash Input
	inputMsg := map[string]interface{}{
		"parentUuid":  caveatUUID,
		"isSidechain": false,
		"userType":    "external",
		"cwd":         workDir,
		"sessionId":   sessionID,
		"version":     "2.0.71",
		"gitBranch":   gitBranch,
		"type":        "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("<bash-input>%s</bash-input>", command),
		},
		"uuid":      inputUUID,
		"timestamp": timestamp,
	}
	if data, err := marshalWithoutEscape(inputMsg); err == nil {
		logFile.WriteString(string(data) + "\n")
	}

	// Message 3: Bash Output
	outputMsg := map[string]interface{}{
		"parentUuid":  inputUUID,
		"isSidechain": false,
		"userType":    "external",
		"cwd":         workDir,
		"sessionId":   sessionID,
		"version":     "2.0.71",
		"gitBranch":   gitBranch,
		"type":        "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": fmt.Sprintf("<bash-stdout>%s</bash-stdout><bash-stderr>%s</bash-stderr>", stdout.String(), stderr.String()),
		},
		"uuid":      outputUUID,
		"timestamp": timestamp,
	}
	if data, err := marshalWithoutEscape(outputMsg); err == nil {
		logFile.WriteString(string(data) + "\n")
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
