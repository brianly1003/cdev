package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseConversationLine_MessageAndTools(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantRole string
		wantType string
	}{
		{
			name:     "user message",
			line:     `{"timestamp":"2026-01-31T12:00:02Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
			wantRole: "user",
			wantType: "text",
		},
		{
			name:     "assistant message",
			line:     `{"timestamp":"2026-01-31T12:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}`,
			wantRole: "assistant",
			wantType: "text",
		},
		{
			name:     "function call",
			line:     `{"timestamp":"2026-01-31T12:00:04Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"ls -la\"}","call_id":"call_1"}}`,
			wantRole: "assistant",
			wantType: "tool_use",
		},
		{
			name:     "function call view_image normalizes dot cdev path",
			line:     `{"timestamp":"2026-01-31T12:00:04Z","type":"response_item","payload":{"type":"function_call","name":"view_image","arguments":"{\"path\":\"/Users/brianly/Projects/cdev/.cdev/images/img_6ab0243f-1a6.jpg\"}","call_id":"call_img_1"}}`,
			wantRole: "assistant",
			wantType: "tool_use",
		},
		{
			name:     "function call output",
			line:     `{"timestamp":"2026-01-31T12:00:05Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Process exited with code 1\\nOutput:\\nnope"}}`,
			wantRole: "assistant",
			wantType: "tool_result",
		},
		{
			name:     "custom tool call (string input)",
			line:     `{"timestamp":"2026-01-31T12:00:06Z","type":"response_item","payload":{"type":"custom_tool_call","status":"completed","call_id":"call_2","name":"apply_patch","input":"*** Begin Patch\\n*** Add File: A.txt\\n+hi\\n*** End Patch\\n"}}`,
			wantRole: "assistant",
			wantType: "tool_use",
		},
		{
			name:     "custom tool call output (json string wrapper)",
			line:     `{"timestamp":"2026-01-31T12:00:07Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"call_2","output":"{\"output\":\"Success. Updated the following files:\\nA A.txt\\n\",\"metadata\":{\"exit_code\":0}}"}}`,
			wantRole: "assistant",
			wantType: "tool_result",
		},
		{
			name:     "reasoning summary",
			line:     `{"timestamp":"2026-01-31T12:00:08Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"Plan"}],"encrypted_content":"abc"}}`,
			wantRole: "assistant",
			wantType: "thinking",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, err := ParseConversationLine(tt.line)
			if err != nil {
				t.Fatalf("ParseConversationLine error: %v", err)
			}
			if item == nil {
				t.Fatalf("ParseConversationLine returned nil item")
			}
			if item.Role != tt.wantRole {
				t.Fatalf("Role = %q, want %q", item.Role, tt.wantRole)
			}
			if len(item.Content) == 0 {
				t.Fatalf("Content empty")
			}
			if item.Content[0].Type != tt.wantType {
				t.Fatalf("Content[0].Type = %q, want %q", item.Content[0].Type, tt.wantType)
			}

			// Spot checks for tool-specific fields.
			switch tt.wantType {
			case "tool_use":
				if item.Content[0].ToolName == "" || item.Content[0].ToolID == "" {
					t.Fatalf("tool_use missing tool_name/tool_id: %+v", item.Content[0])
				}
				if item.Content[0].ToolName == "exec_command" {
					if _, ok := item.Content[0].ToolInput["command"].(string); !ok {
						t.Fatalf("exec_command tool_use missing normalized command field: %+v", item.Content[0].ToolInput)
					}
				}
				if item.Content[0].ToolName == "view_image" {
					got, _ := item.Content[0].ToolInput["path"].(string)
					want := ".cdev/images/img_6ab0243f-1a6.jpg"
					if got != want {
						t.Fatalf("view_image tool_use path=%q, want %q", got, want)
					}
				}
			case "tool_result":
				if item.Content[0].ToolUseID == "" || item.Content[0].Content == "" {
					t.Fatalf("tool_result missing tool_use_id/content: %+v", item.Content[0])
				}
			}
		})
	}
}

func TestParseConversationLine_IgnoresAgentReasoningEvent(t *testing.T) {
	line := `{"timestamp":"2026-01-31T12:00:09Z","type":"event_msg","payload":{"type":"agent_reasoning","text":"Thinking..."}}`
	item, err := ParseConversationLine(line)
	if err != nil {
		t.Fatalf("ParseConversationLine error: %v", err)
	}
	if item != nil {
		t.Fatalf("expected nil item for agent_reasoning event, got: %+v", item)
	}
}

func TestParseConversationLine_ContextCompactedEvent(t *testing.T) {
	line := `{"timestamp":"2026-01-31T12:00:10Z","type":"event_msg","payload":{"type":"context_compacted"}}`
	item, err := ParseConversationLine(line)
	if err != nil {
		t.Fatalf("ParseConversationLine error: %v", err)
	}
	if item == nil {
		t.Fatal("expected non-nil item")
	}
	if !item.IsContextCompaction {
		t.Fatal("expected IsContextCompaction=true")
	}
	if item.Role != "user" {
		t.Fatalf("role=%q, want user", item.Role)
	}
	if len(item.Content) != 1 || item.Content[0].Type != "text" {
		t.Fatalf("unexpected content: %+v", item.Content)
	}
	if item.Content[0].Text == "" {
		t.Fatal("expected non-empty context compaction text")
	}
}

func TestParseConversationLine_IgnoresDeveloperMessages(t *testing.T) {
	line := `{"timestamp":"2026-01-31T12:00:00Z","type":"response_item","payload":{"type":"message","role":"developer","content":[{"type":"output_text","text":"secret"}]}}`
	item, err := ParseConversationLine(line)
	if err != nil {
		t.Fatalf("ParseConversationLine error: %v", err)
	}
	if item != nil {
		t.Fatalf("expected nil item for developer role, got: %+v", item)
	}
}

func TestParseConversationLine_IgnoresCodexBootstrapUserMessages(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{
			name: "agents instructions",
			line: `{"timestamp":"2026-02-17T14:53:32.719Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for /Users/brianly/Projects/AIQA\n\n<INSTRUCTIONS>\n# Repository Guidelines\n\n## Project Structure & Module Organization\n\n- web/\n</INSTRUCTIONS>"}]}}`,
		},
		{
			name: "environment context",
			line: `{"timestamp":"2026-02-17T14:53:32.719Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"<environment_context>\n  <cwd>/Users/brianly/Projects/AIQA</cwd>\n  <shell>zsh</shell>\n</environment_context>"}]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item, err := ParseConversationLine(tt.line)
			if err != nil {
				t.Fatalf("ParseConversationLine error: %v", err)
			}
			if item != nil {
				t.Fatalf("expected nil item, got: %+v", item)
			}
		})
	}
}

func TestReadConversationItems_SetsLineNumbers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout-test.jsonl")

	lines := []string{
		`{"timestamp":"2026-01-31T12:00:00Z","type":"session_meta","payload":{"id":"sess-1"}}`,
		`{"timestamp":"2026-01-31T12:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}`,
		`{"timestamp":"2026-01-31T12:00:02Z","type":"event_msg","payload":{"type":"token_count"}}`,
		`{"timestamp":"2026-01-31T12:00:03Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"pwd\"}","call_id":"call_1"}}`,
	}

	content := ""
	for i, l := range lines {
		content += l
		if i < len(lines)-1 {
			content += "\n"
		}
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	items, err := ReadConversationItems(path)
	if err != nil {
		t.Fatalf("ReadConversationItems: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	// The first relevant item is on line 2, the second on line 4.
	if items[0].Line != 2 {
		t.Fatalf("items[0].Line = %d, want 2", items[0].Line)
	}
	if items[1].Line != 4 {
		t.Fatalf("items[1].Line = %d, want 4", items[1].Line)
	}
}
