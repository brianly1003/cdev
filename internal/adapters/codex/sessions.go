// Package codex provides Codex CLI session parsing and discovery helpers.
package codex

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/jsonl"
)

// SessionInfo describes a Codex CLI session file.
type SessionInfo struct {
	SessionID     string    `json:"session_id"`
	Summary       string    `json:"summary"`
	MessageCount  int       `json:"message_count"`
	LastUpdated   time.Time `json:"last_updated"`
	WorkspacePath string    `json:"workspace_path"`
}

// Message is a normalized Codex session message.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"`
}

type logEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

type turnContextPayload struct {
	Cwd string `json:"cwd"`
}

type responseItemPayload struct {
	Type             string          `json:"type"`
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	EncryptedContent string          `json:"encrypted_content"`
	Summary          json.RawMessage `json:"summary"`
}

type eventMsgPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// DefaultCodexHome resolves CODEX_HOME (or ~/.codex by default).
func DefaultCodexHome() string {
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".codex"
	}
	return filepath.Join(home, ".codex")
}

// SessionsDir returns the root sessions directory for Codex CLI.
func SessionsDir(codexHome string) string {
	return filepath.Join(codexHome, "sessions")
}

// ListSessionsForWorkspace returns sessions whose cwd is under the workspace path.
func ListSessionsForWorkspace(workspacePath string) ([]SessionInfo, error) {
	root := SessionsDir(DefaultCodexHome())
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SessionInfo{}, nil
		}
		return nil, err
	}

	absWorkspace, err := filepath.Abs(workspacePath)
	if err != nil {
		return nil, err
	}

	var sessions []SessionInfo

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}

		info, _, err := ParseSessionFile(path)
		if err != nil {
			return nil
		}
		if info.MessageCount == 0 || info.WorkspacePath == "" {
			return nil
		}

		absCwd, err := filepath.Abs(info.WorkspacePath)
		if err != nil {
			return nil
		}
		if !isWithinPath(absWorkspace, absCwd) {
			return nil
		}

		sessions = append(sessions, info)
		return nil
	})

	if err != nil {
		return nil, err
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdated.After(sessions[j].LastUpdated)
	})

	return sessions, nil
}

// ParseSessionFile parses a Codex JSONL session file.
func ParseSessionFile(path string) (SessionInfo, []Message, error) {
	file, err := os.Open(path)
	if err != nil {
		return SessionInfo{}, nil, err
	}
	defer func() { _ = file.Close() }()

	info := SessionInfo{}
	var messages []Message

	var lastMessage string
	var lastUpdated time.Time

	reader := jsonl.NewReader(file, 0)

	for {
		line, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return SessionInfo{}, nil, err
		}
		if line.TooLong {
			continue
		}
		if len(line.Data) == 0 {
			continue
		}

		lineStr := strings.TrimSpace(string(line.Data))
		if lineStr == "" {
			continue
		}

		var entry logEntry
		if err := json.Unmarshal([]byte(lineStr), &entry); err != nil {
			continue
		}

		if ts, ok := parseTimestamp(entry.Timestamp); ok {
			if ts.After(lastUpdated) {
				lastUpdated = ts
			}
		}

		switch entry.Type {
		case "session_meta":
			var payload sessionMetaPayload
			if err := json.Unmarshal(entry.Payload, &payload); err == nil {
				if payload.ID != "" {
					info.SessionID = payload.ID
				}
				if payload.Cwd != "" && info.WorkspacePath == "" {
					info.WorkspacePath = payload.Cwd
				}
			}
		case "turn_context":
			var payload turnContextPayload
			if err := json.Unmarshal(entry.Payload, &payload); err == nil {
				if payload.Cwd != "" && info.WorkspacePath == "" {
					info.WorkspacePath = payload.Cwd
				}
			}
		case "response_item":
			var payload responseItemPayload
			if err := json.Unmarshal(entry.Payload, &payload); err != nil {
				continue
			}
			if payload.Type == "message" {
				content := extractContent(payload.Content)
				if strings.TrimSpace(content) == "" {
					continue
				}
				role := payload.Role
				msg := Message{
					Role:      role,
					Content:   content,
					Timestamp: lastUpdated,
					Source:    "response_item",
				}
				messages = append(messages, msg)
				info.MessageCount++
				if role == "assistant" {
					lastMessage = content
				}
			}
		case "event_msg":
			var payload eventMsgPayload
			if err := json.Unmarshal(entry.Payload, &payload); err != nil {
				continue
			}
			if payload.Type == "agent_message" && payload.Message != "" {
				lastMessage = payload.Message
			}
		}
	}

	info.Summary = lastMessage
	info.LastUpdated = lastUpdated

	return info, messages, nil
}

func extractContent(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, block := range blocks {
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}

	return ""
}

func parseTimestamp(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, ts)
	if err == nil {
		return parsed, true
	}
	parsed, err = time.Parse(time.RFC3339, ts)
	if err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

func isWithinPath(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
