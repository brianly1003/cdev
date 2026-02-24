package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brianly1003/cdev/internal/adapters/codex"
)

func TestCodexSessionAdapter_GetSessionMessages_SetsStableUUID(t *testing.T) {
	codexHome := t.TempDir()
	sessionsRoot := filepath.Join(codexHome, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-session-codex"
	sessionPath := filepath.Join(sessionsRoot, "rollout-test.jsonl")

	ts := "2026-02-15T00:00:00.000Z"
	lines := []map[string]interface{}{
		{
			"timestamp": ts,
			"type":      "session_meta",
			"payload": map[string]interface{}{
				"id":             sessionID,
				"cwd":            "/tmp/project",
				"model_provider": "openai",
				"cli_version":    "0.0.0",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": "Hello",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":      "function_call",
				"name":      "exec_command",
				"arguments": `{"cmd":"ls -la"}`,
				"call_id":   "call-1",
			},
		},
	}

	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		b, _ := json.Marshal(line)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	_ = f.Close()

	adapter := NewCodexSessionAdapter("/repo")
	adapter.indexCache = codex.NewIndexCache(codexHome)

	msgs, total, err := adapter.GetSessionMessages(context.Background(), sessionID, 50, 0, "asc")
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}
	if total != 3 {
		t.Fatalf("total=%d, want 3", total)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs)=%d, want 3", len(msgs))
	}

	// Codex JSONL can emit multiple assistant items with the same timestamp. iOS relies on UUID
	// for stable `Identifiable` keys and will collide if UUID is empty.
	seen := make(map[string]bool)
	foundExplored := false
	for _, m := range msgs {
		if m.UUID == "" {
			t.Fatalf("message UUID is empty (id=%d type=%q ts=%q)", m.ID, m.Type, m.Timestamp)
		}
		if seen[m.UUID] {
			t.Fatalf("duplicate message UUID: %q", m.UUID)
		}
		seen[m.UUID] = true

		var raw struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"content"`
		}
		if err := json.Unmarshal(m.Message, &raw); err == nil {
			for _, block := range raw.Content {
				if block.Type == "text" && strings.Contains(block.Text, "**Explored**") {
					foundExplored = true
				}
			}
		}
	}

	if !foundExplored {
		t.Fatalf("expected synthetic explored summary message in codex history")
	}
}

func TestCodexSessionAdapter_GetSession_WorksWithoutRepositoryPath(t *testing.T) {
	codexHome := t.TempDir()
	sessionsRoot := filepath.Join(codexHome, "sessions", "2026", "02", "16")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "019c6572-6e3f-78a1-9d70-7af2973de2a4"
	sessionPath := filepath.Join(sessionsRoot, "rollout-test.jsonl")

	ts := "2026-02-16T09:14:35.985Z"
	lines := []map[string]interface{}{
		{
			"timestamp": ts,
			"type":      "session_meta",
			"payload": map[string]interface{}{
				"id":             sessionID,
				"cwd":            "/Users/brianly/Projects/AIQA",
				"model_provider": "openai",
				"model":          "gpt-5.3-codex",
				"cli_version":    "0.101.0",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": "hello",
			},
		},
	}

	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		b, _ := json.Marshal(line)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	_ = f.Close()

	adapter := NewCodexSessionAdapter("")
	adapter.indexCache = codex.NewIndexCache(codexHome)

	info, err := adapter.GetSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetSession error: %v", err)
	}
	if info == nil {
		t.Fatal("GetSession returned nil info, want session")
	}
	if info.SessionID != sessionID {
		t.Fatalf("session_id=%s, want %s", info.SessionID, sessionID)
	}
	if info.AgentType != "codex" {
		t.Fatalf("agent_type=%s, want codex", info.AgentType)
	}
}

func TestCodexSessionAdapter_GetSessionElements_IncludesExploredSummary(t *testing.T) {
	codexHome := t.TempDir()
	sessionsRoot := filepath.Join(codexHome, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-elements-codex"
	sessionPath := filepath.Join(sessionsRoot, "rollout-elements.jsonl")

	ts := "2026-02-15T00:00:00.000Z"
	lines := []map[string]interface{}{
		{
			"timestamp": ts,
			"type":      "session_meta",
			"payload": map[string]interface{}{
				"id":             sessionID,
				"cwd":            "/tmp/project",
				"model_provider": "openai",
				"cli_version":    "0.0.0",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": "Scanning repo",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":      "function_call",
				"name":      "exec_command",
				"arguments": `{"cmd":"find . -name .git"}`,
				"call_id":   "call-1",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":    "message",
				"role":    "assistant",
				"content": "Done",
			},
		},
	}

	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		b, _ := json.Marshal(line)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	_ = f.Close()

	adapter := NewCodexSessionAdapter("/repo")
	adapter.indexCache = codex.NewIndexCache(codexHome)

	elements, total, err := adapter.GetSessionElements(context.Background(), sessionID, 50, "", "")
	if err != nil {
		t.Fatalf("GetSessionElements error: %v", err)
	}
	if total == 0 || len(elements) == 0 {
		t.Fatalf("expected elements, got total=%d len=%d", total, len(elements))
	}

	foundExplored := false
	for _, elem := range elements {
		if elem.Type != "assistant_text" {
			continue
		}
		contentBytes, err := json.Marshal(elem.Content)
		if err != nil {
			continue
		}
		var content struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(contentBytes, &content); err != nil {
			continue
		}
		if strings.Contains(content.Text, "**Explored**") {
			foundExplored = true
			break
		}
	}

	if !foundExplored {
		t.Fatalf("expected assistant_text explored summary element")
	}
}

func TestCodexSessionAdapter_GetSessionMessages_IncludesContextCompacted(t *testing.T) {
	codexHome := t.TempDir()
	sessionsRoot := filepath.Join(codexHome, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-context-compacted-codex"
	sessionPath := filepath.Join(sessionsRoot, "rollout-context.jsonl")

	ts := "2026-02-17T15:05:38.531Z"
	lines := []map[string]interface{}{
		{
			"timestamp": ts,
			"type":      "session_meta",
			"payload": map[string]interface{}{
				"id":             sessionID,
				"cwd":            "/tmp/project",
				"model_provider": "openai",
				"cli_version":    "0.0.0",
			},
		},
		{
			"timestamp": ts,
			"type":      "event_msg",
			"payload": map[string]interface{}{
				"type": "context_compacted",
			},
		},
	}

	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		b, _ := json.Marshal(line)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	_ = f.Close()

	adapter := NewCodexSessionAdapter("/repo")
	adapter.indexCache = codex.NewIndexCache(codexHome)

	msgs, total, err := adapter.GetSessionMessages(context.Background(), sessionID, 50, 0, "asc")
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}
	if total != 1 || len(msgs) != 1 {
		t.Fatalf("expected one message, got total=%d len=%d", total, len(msgs))
	}

	msg := msgs[0]
	if !msg.IsContextCompaction {
		t.Fatalf("expected is_context_compaction=true")
	}
	if msg.Type != "user" {
		t.Fatalf("type=%q, want user", msg.Type)
	}

	var raw struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(msg.Message, &raw); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if raw.Role != "user" {
		t.Fatalf("message.role=%q, want user", raw.Role)
	}
	if len(raw.Content) != 1 || raw.Content[0].Type != "text" || strings.TrimSpace(raw.Content[0].Text) == "" {
		t.Fatalf("unexpected message content: %+v", raw.Content)
	}
}

func TestCodexSessionAdapter_GetSessionElements_IncludesContextCompacted(t *testing.T) {
	codexHome := t.TempDir()
	sessionsRoot := filepath.Join(codexHome, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-context-compacted-elements-codex"
	sessionPath := filepath.Join(sessionsRoot, "rollout-context-elements.jsonl")

	ts := "2026-02-17T15:05:38.531Z"
	lines := []map[string]interface{}{
		{
			"timestamp": ts,
			"type":      "session_meta",
			"payload": map[string]interface{}{
				"id":             sessionID,
				"cwd":            "/tmp/project",
				"model_provider": "openai",
				"cli_version":    "0.0.0",
			},
		},
		{
			"timestamp": ts,
			"type":      "event_msg",
			"payload": map[string]interface{}{
				"type": "context_compacted",
			},
		},
	}

	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		b, _ := json.Marshal(line)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	_ = f.Close()

	adapter := NewCodexSessionAdapter("/repo")
	adapter.indexCache = codex.NewIndexCache(codexHome)

	elements, total, err := adapter.GetSessionElements(context.Background(), sessionID, 50, "", "")
	if err != nil {
		t.Fatalf("GetSessionElements error: %v", err)
	}
	if total != 1 || len(elements) != 1 {
		t.Fatalf("expected one element, got total=%d len=%d", total, len(elements))
	}

	elem := elements[0]
	if elem.Type != "context_compaction" {
		t.Fatalf("type=%q, want context_compaction", elem.Type)
	}
	contentBytes, err := json.Marshal(elem.Content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	var content struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(contentBytes, &content); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if strings.TrimSpace(content.Summary) == "" {
		t.Fatal("expected non-empty summary")
	}
}

func TestCodexSessionAdapter_TurnAbortedFormatsAsInterrupted(t *testing.T) {
	codexHome := t.TempDir()
	sessionsRoot := filepath.Join(codexHome, "sessions")
	if err := os.MkdirAll(sessionsRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-turn-aborted-codex"
	sessionPath := filepath.Join(sessionsRoot, "rollout-turn-aborted.jsonl")

	ts := "2026-02-17T15:05:38.531Z"
	lines := []map[string]interface{}{
		{
			"timestamp": ts,
			"type":      "session_meta",
			"payload": map[string]interface{}{
				"id":             sessionID,
				"cwd":            "/tmp/project",
				"model_provider": "openai",
				"cli_version":    "0.0.0",
			},
		},
		{
			"timestamp": ts,
			"type":      "response_item",
			"payload": map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": "<turn_aborted>\nThe user interrupted the previous turn on purpose.\n</turn_aborted>",
			},
		},
	}

	f, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		b, _ := json.Marshal(line)
		if _, err := f.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	_ = f.Close()

	adapter := NewCodexSessionAdapter("/repo")
	adapter.indexCache = codex.NewIndexCache(codexHome)

	msgs, total, err := adapter.GetSessionMessages(context.Background(), sessionID, 50, 0, "asc")
	if err != nil {
		t.Fatalf("GetSessionMessages error: %v", err)
	}
	if total != 1 || len(msgs) != 1 {
		t.Fatalf("expected one message, got total=%d len=%d", total, len(msgs))
	}

	var msgBody struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(msgs[0].Message, &msgBody); err != nil {
		t.Fatalf("unmarshal message: %v", err)
	}
	if len(msgBody.Content) != 1 {
		t.Fatalf("expected one content block, got %+v", msgBody.Content)
	}
	if strings.Contains(msgBody.Content[0].Text, "<turn_aborted>") {
		t.Fatalf("expected normalized interruption text, got %q", msgBody.Content[0].Text)
	}

	elements, total, err := adapter.GetSessionElements(context.Background(), sessionID, 50, "", "")
	if err != nil {
		t.Fatalf("GetSessionElements error: %v", err)
	}
	if total != 1 || len(elements) != 1 {
		t.Fatalf("expected one element, got total=%d len=%d", total, len(elements))
	}
	if elements[0].Type != "interrupted" {
		t.Fatalf("type=%q, want interrupted", elements[0].Type)
	}

	contentBytes, err := json.Marshal(elements[0].Content)
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	var elemContent struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(contentBytes, &elemContent); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	if strings.TrimSpace(elemContent.Message) == "" {
		t.Fatal("expected non-empty interrupted message")
	}
	if strings.Contains(elemContent.Message, "<turn_aborted>") {
		t.Fatalf("unexpected raw tag in interrupted message: %q", elemContent.Message)
	}
}

func TestFormatCodexToolDisplay_ExecCommand(t *testing.T) {
	display := formatCodexToolDisplay("exec_command", map[string]interface{}{
		"cmd": "ls -la",
	})
	if display != "Ran ls -la" {
		t.Fatalf("display=%q, want %q", display, "Ran ls -la")
	}

	display = formatCodexToolDisplay("exec_command", map[string]interface{}{
		"command": "pwd",
	})
	if display != "Ran pwd" {
		t.Fatalf("display=%q, want %q", display, "Ran pwd")
	}
}

func TestFormatCodexToolDisplay_ViewImage_CompactsDotCdevPath(t *testing.T) {
	display := formatCodexToolDisplay("view_image", map[string]interface{}{
		"path": "/Users/brianly/Projects/cdev/.cdev/images/img_6ab0243f-1a6.jpg",
	})
	want := "view_image(path: .cdev/images/img_6ab0243f-1a6.jpg)"
	if display != want {
		t.Fatalf("display=%q, want %q", display, want)
	}
}

func TestSummarizeCodexToolForExplored_SkipsViewImage(t *testing.T) {
	summary := summarizeCodexToolForExplored("view_image", map[string]interface{}{
		"path": ".cdev/images/img_6ab0243f-1a6.jpg",
	})
	if summary != "" {
		t.Fatalf("summary=%q, want empty", summary)
	}
}

func TestSummarizeCodexToolForExplored_SkipsApplyPatch(t *testing.T) {
	summary := summarizeCodexToolForExplored("apply_patch", map[string]interface{}{
		"input": "*** Begin Patch\n*** Update File: a.txt\n+hi\n*** End Patch\n",
	})
	if summary != "" {
		t.Fatalf("summary=%q, want empty", summary)
	}
}

func TestSummarizeCodexToolForExplored_FormatsReadAndSearch(t *testing.T) {
	readSummary := summarizeCodexToolForExplored("exec_command", map[string]interface{}{
		"cmd": "sed -n '1170,1275p' /Users/brianly/Projects/cdev/internal/app/adapters.go",
	})
	if readSummary != "Read adapters.go" {
		t.Fatalf("readSummary=%q, want %q", readSummary, "Read adapters.go")
	}

	searchSummary := summarizeCodexToolForExplored("exec_command", map[string]interface{}{
		"cmd": "rg -n \"claude_message|tool_use\" internal -S",
	})
	if searchSummary != "Search claude_message|tool_use in internal" {
		t.Fatalf("searchSummary=%q, want %q", searchSummary, "Search claude_message|tool_use in internal")
	}
}

func TestSummarizeCodexToolForExplored_SkipsExecutionCommands(t *testing.T) {
	summary := summarizeCodexToolForExplored("exec_command", map[string]interface{}{
		"cmd": "python3 - <<'PY'\nprint('hello')\nPY",
	})
	if summary != "" {
		t.Fatalf("summary=%q, want empty", summary)
	}
}

func TestSummarizeApplyPatch(t *testing.T) {
	patch := strings.Join([]string{
		"*** Begin Patch",
		"*** Add File: add_2_numbers.py",
		"+def add_2_numbers():",
		"+    return [7, 0, 8]",
		"*** End Patch",
	}, "\n")

	got := summarizeApplyPatch(patch)
	want := "Added add_2_numbers.py (+2 -0)"
	if got != want {
		t.Fatalf("summarizeApplyPatch=%q, want %q", got, want)
	}
}
