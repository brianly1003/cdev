package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSessionFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "rollout-test.jsonl")

	lines := []string{
		`{"timestamp":"2026-01-31T12:00:00Z","type":"session_meta","payload":{"id":"sess-123","timestamp":"2026-01-31T12:00:00Z","cwd":"/repo"}}`,
		`{"timestamp":"2026-01-31T12:00:01Z","type":"turn_context","payload":{"cwd":"/repo"}}`,
		`{"timestamp":"2026-01-31T12:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
		`{"timestamp":"2026-01-31T12:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`,
		`{"timestamp":"2026-01-31T12:00:04Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"abc"}}`,
		`{"timestamp":"2026-01-31T12:00:05Z","type":"event_msg","payload":{"type":"agent_message","message":"hello"}}`,
	}

	if err := os.WriteFile(filePath, []byte(joinLines(lines)), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	info, messages, err := ParseSessionFile(filePath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if info.SessionID != "sess-123" {
		t.Fatalf("session_id = %s, want sess-123", info.SessionID)
	}
	if info.WorkspacePath != "/repo" {
		t.Fatalf("workspace_path = %s, want /repo", info.WorkspacePath)
	}
	if info.MessageCount != 2 {
		t.Fatalf("message_count = %d, want 2", info.MessageCount)
	}
	if info.Summary != "hello" {
		t.Fatalf("summary = %q, want %q", info.Summary, "hello")
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("roles = %s/%s, want user/assistant", messages[0].Role, messages[1].Role)
	}
	if messages[0].SessionID != "sess-123" {
		t.Fatalf("message session_id = %s, want sess-123", messages[0].SessionID)
	}

	wantTime, _ := time.Parse(time.RFC3339, "2026-01-31T12:00:05Z")
	if !info.LastUpdated.Equal(wantTime) {
		t.Fatalf("last_updated = %s, want %s", info.LastUpdated, wantTime)
	}
}

func TestIsWithinPath(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "repo")
	if !isWithinPath(base, filepath.Join(base, "subdir")) {
		t.Fatalf("expected subdir to be within base")
	}
	if isWithinPath(base, filepath.Join(string(filepath.Separator), "other")) {
		t.Fatalf("expected /other to be outside base")
	}
}

func joinLines(lines []string) string {
	out := ""
	for i, line := range lines {
		out += line
		if i < len(lines)-1 {
			out += "\n"
		}
	}
	return out
}
