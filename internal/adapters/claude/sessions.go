// Package claude implements the Claude CLI process manager.
package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// SessionInfo represents information about a stored Claude session.
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	Summary      string    `json:"summary"`
	MessageCount int       `json:"message_count"`
	LastUpdated  time.Time `json:"last_updated"`
	Branch       string    `json:"branch,omitempty"`
}

// sessionMessage represents a message in a session file.
type sessionMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	GitBranch string `json:"gitBranch"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"` // Can be string or array
	} `json:"message"`
	Timestamp string `json:"timestamp"`
}

// extractContent extracts text content from the message.
// Claude's content can be a string or an array of content blocks.
func (m *sessionMessage) extractContent() string {
	if m.Message.Content == nil {
		return ""
	}

	// Try string first
	var contentStr string
	if err := json.Unmarshal(m.Message.Content, &contentStr); err == nil {
		return contentStr
	}

	// Try array of content blocks
	var contentBlocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Message.Content, &contentBlocks); err == nil {
		var texts []string
		for _, block := range contentBlocks {
			if block.Type == "text" && block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

// isUserTextMessage returns true if the user message contains actual user text.
// Excludes:
// - tool_result messages (API responses)
// - System messages starting with "Caveat:", "<command-name>", "<local-command-stdout>"
func (m *sessionMessage) isUserTextMessage() bool {
	if m.Message.Content == nil {
		return false
	}

	// If content is a string, check if it's a real user message (not system injection)
	var contentStr string
	if err := json.Unmarshal(m.Message.Content, &contentStr); err == nil {
		if contentStr == "" {
			return false
		}
		// Exclude system messages
		if strings.HasPrefix(contentStr, "Caveat:") ||
			strings.HasPrefix(contentStr, "<command-name>") ||
			strings.HasPrefix(contentStr, "<local-command-stdout>") ||
			strings.HasPrefix(contentStr, "<local-command-stderr>") {
			return false
		}
		return true
	}

	// If content is an array, check if it has text type (not just tool_result)
	var contentBlocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(m.Message.Content, &contentBlocks); err == nil {
		for _, block := range contentBlocks {
			if block.Type == "text" {
				return true
			}
		}
	}

	return false
}

// hasTextOrThinkingContent returns true if the assistant message contains "text" or "thinking" content.
// This excludes messages that only have "tool_use" blocks.
func (m *sessionMessage) hasTextOrThinkingContent() bool {
	if m.Message.Content == nil {
		return false
	}

	var contentBlocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(m.Message.Content, &contentBlocks); err == nil {
		for _, block := range contentBlocks {
			if block.Type == "text" || block.Type == "thinking" {
				return true
			}
		}
	}

	return false
}

// uuidPattern matches UUID format session IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// ListSessions returns all available sessions for a repository.
func ListSessions(repoPath string) ([]SessionInfo, error) {
	start := time.Now()

	// Encode repo path to Claude's storage format
	sessionsDir := getSessionsDir(repoPath)

	log.Debug().Str("sessions_dir", sessionsDir).Msg("listing sessions")

	// Check if directory exists
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		log.Debug().Msg("sessions directory does not exist")
		return []SessionInfo{}, nil
	}

	// Read directory
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo
	fileCount := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only process UUID-pattern .jsonl files (skip agent-* files)
		if !uuidPattern.MatchString(entry.Name()) {
			continue
		}

		fileCount++
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		filePath := filepath.Join(sessionsDir, entry.Name())

		info, err := parseSessionFile(filePath, sessionID)
		if err != nil {
			log.Debug().Err(err).Str("file", entry.Name()).Msg("failed to parse session file")
			continue
		}

		// Skip empty sessions
		if info.MessageCount == 0 {
			continue
		}

		sessions = append(sessions, info)
	}

	// Sort by last updated (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})

	elapsed := time.Since(start)
	log.Debug().
		Int("files_scanned", fileCount).
		Int("sessions_found", len(sessions)).
		Dur("elapsed_ms", elapsed).
		Msg("listed sessions")

	return sessions, nil
}

// getSessionsDir returns the Claude sessions directory for a repo path.
func getSessionsDir(repoPath string) string {
	// Claude stores sessions in ~/.claude/projects/[encoded-path]/
	// The path is encoded by replacing "/" with "-"
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	// Clean the path (remove trailing slashes)
	repoPath = filepath.Clean(repoPath)

	// Encode the repo path: /Users/foo/bar -> -Users-foo-bar
	encodedPath := strings.ReplaceAll(repoPath, "/", "-")

	return filepath.Join(homeDir, ".claude", "projects", encodedPath)
}

// parseSessionFile reads a session file and extracts info.
// Message counting matches Claude CLI logic:
// - User messages: only count if content is text (not tool_result or system messages)
// - Assistant messages: count if contains "text" OR "thinking" content (not tool_use-only)
func parseSessionFile(filePath string, sessionID string) (SessionInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return SessionInfo{}, err
	}
	defer file.Close()

	info := SessionInfo{
		SessionID: sessionID,
	}

	// Get file modification time
	stat, err := file.Stat()
	if err == nil {
		info.LastUpdated = stat.ModTime()
	}

	scanner := bufio.NewScanner(file)
	// Increase buffer for large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	messageCount := 0
	foundSummary := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg sessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Count messages matching Claude CLI logic
		if msg.Type == "user" && msg.isUserTextMessage() {
			messageCount++
		} else if msg.Type == "assistant" && msg.hasTextOrThinkingContent() {
			// Count assistant messages with "text" or "thinking" content (not tool_use-only)
			messageCount++
		}

		// Get summary from first user message
		content := msg.extractContent()
		if !foundSummary && msg.Type == "user" && content != "" {
			info.Summary = truncateSummary(content, 100)
			info.Branch = msg.GitBranch
			foundSummary = true
		}
	}

	info.MessageCount = messageCount

	return info, scanner.Err()
}

// truncateSummary truncates a string for display.
func truncateSummary(s string, maxLen int) string {
	// Remove newlines
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// GetSessionsDir exports the sessions directory path for a repo.
func GetSessionsDir(repoPath string) string {
	return getSessionsDir(repoPath)
}

// sessionMessageRaw is used to read from JSONL files (camelCase field names).
type sessionMessageRaw struct {
	Type      string          `json:"type"`
	UUID      string          `json:"uuid,omitempty"`
	SessionID string          `json:"sessionId,omitempty"` // camelCase in JSONL files
	Timestamp string          `json:"timestamp,omitempty"`
	GitBranch string          `json:"gitBranch,omitempty"` // camelCase in JSONL files
	Message   json.RawMessage `json:"message"`
}

// SessionMessage represents a single message in a session for API response.
// Uses snake_case for consistency with WebSocket events.
type SessionMessage struct {
	Type      string          `json:"type"`
	UUID      string          `json:"uuid,omitempty"`
	SessionID string          `json:"session_id,omitempty"` // snake_case for API response
	Timestamp string          `json:"timestamp,omitempty"`
	GitBranch string          `json:"git_branch,omitempty"` // snake_case for API response
	Message   json.RawMessage `json:"message"`
}

// GetSessionMessages returns all messages for a specific session.
// Returns raw JSON messages to preserve token usage and other metadata.
func GetSessionMessages(repoPath, sessionID string) ([]SessionMessage, error) {
	sessionsDir := getSessionsDir(repoPath)
	filePath := filepath.Join(sessionsDir, sessionID+".jsonl")

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var messages []SessionMessage

	scanner := bufio.NewScanner(file)
	// Increase buffer for large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse into a map first to check type and extract fields
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			log.Debug().Err(err).Msg("failed to parse session message")
			continue
		}

		// Get the type
		var msgType string
		if typeRaw, ok := raw["type"]; ok {
			json.Unmarshal(typeRaw, &msgType)
		}

		// Only include user and assistant messages (skip summary, file-history-snapshot, etc.)
		if msgType == "user" || msgType == "assistant" {
			// Read with camelCase struct, convert to snake_case for API response
			var rawMsg sessionMessageRaw
			if err := json.Unmarshal([]byte(line), &rawMsg); err != nil {
				log.Debug().Err(err).Msg("failed to parse session message into struct")
				continue
			}
			// Convert to API response format (snake_case)
			msg := SessionMessage{
				Type:      rawMsg.Type,
				UUID:      rawMsg.UUID,
				SessionID: rawMsg.SessionID,
				Timestamp: rawMsg.Timestamp,
				GitBranch: rawMsg.GitBranch,
				Message:   rawMsg.Message,
			}
			messages = append(messages, msg)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return messages, nil
}

// DeleteSession deletes a specific session file.
func DeleteSession(repoPath, sessionID string) error {
	sessionsDir := getSessionsDir(repoPath)
	filePath := filepath.Join(sessionsDir, sessionID+".jsonl")

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	log.Info().Str("session_id", sessionID).Msg("deleted session")
	return nil
}

// DeleteAllSessions deletes all session files for a repository.
func DeleteAllSessions(repoPath string) (int, error) {
	sessionsDir := getSessionsDir(repoPath)

	// Check if directory exists
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return 0, nil // No sessions to delete
	}

	// Read directory
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	deleted := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// Only delete UUID-pattern .jsonl files (skip agent-* files)
		if !uuidPattern.MatchString(entry.Name()) {
			continue
		}

		filePath := filepath.Join(sessionsDir, entry.Name())
		if err := os.Remove(filePath); err != nil {
			log.Warn().Err(err).Str("file", entry.Name()).Msg("failed to delete session file")
			continue
		}
		deleted++
	}

	log.Info().Int("deleted", deleted).Msg("deleted all sessions")
	return deleted, nil
}
