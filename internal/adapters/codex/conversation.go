// Package codex provides helpers for parsing Codex CLI session logs into a
// normalized conversation stream that matches cdev's existing Claude-oriented UI
// primitives (text/thinking/tool_use/tool_result).
package codex

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/brianly1003/cdev/internal/adapters/jsonl"
	"github.com/brianly1003/cdev/internal/domain/events"
)

// ConversationItem is a normalized, UI-ready item derived from Codex CLI JSONL logs.
// It is intentionally shaped to be convertible into:
// - `claude_message` events (for live UI streaming)
// - `session/messages` synthetic message payloads
// - `session/elements` for rich timeline rendering
type ConversationItem struct {
	// Line is the 1-based line number in the JSONL file (stable for append-only logs).
	Line int

	// Timestamp is the entry timestamp (RFC3339/RFC3339Nano string).
	Timestamp string

	// Role is "user" or "assistant".
	Role string

	// IsContextCompaction marks synthetic compaction boundary/summary messages.
	IsContextCompaction bool

	// IsTurnAborted marks synthetic interruption boundary messages.
	IsTurnAborted bool

	// Content are Claude-like content blocks (text/thinking/tool_use/tool_result).
	Content []events.ClaudeMessageContent
}

type conversationEntry struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// ParseConversationLine parses a single Codex CLI JSONL line into a normalized conversation item.
// Returns (nil, nil) for lines that aren't relevant for UI rendering.
func ParseConversationLine(line string) (*ConversationItem, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, nil
	}

	var entry conversationEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, err
	}

	switch entry.Type {
	case "response_item":
		return parseResponseItem(entry.Timestamp, entry.Payload)
	case "event_msg":
		return parseEventMsg(entry.Timestamp, entry.Payload)
	default:
		return nil, nil
	}
}

// ReadConversationItems reads a Codex session JSONL file and returns all normalized conversation items.
func ReadConversationItems(path string) ([]ConversationItem, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	reader := jsonl.NewReader(file, 0)

	var items []ConversationItem
	lineNo := 0

	for {
		line, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		lineNo++

		if line.TooLong || len(line.Data) == 0 {
			continue
		}

		item, err := ParseConversationLine(string(line.Data))
		if err != nil || item == nil {
			continue
		}
		item.Line = lineNo
		items = append(items, *item)
	}

	return items, nil
}

func parseResponseItem(timestamp string, raw json.RawMessage) (*ConversationItem, error) {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}

	switch header.Type {
	case "message":
		var msg struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			return nil, err
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			return nil, nil // ignore developer/system messages
		}
		contentBlocks := extractTextContentBlocks(msg.Content)
		if len(contentBlocks) == 0 {
			return nil, nil
		}
		isTurnAborted := false
		if msg.Role == "user" && shouldHideCodexUserMessage(contentBlocks) {
			return nil, nil
		}
		if msg.Role == "user" {
			contentBlocks, isTurnAborted = normalizeCodexUserMessage(contentBlocks)
		}
		return &ConversationItem{
			Timestamp:     timestamp,
			Role:          msg.Role,
			IsTurnAborted: isTurnAborted,
			Content:       contentBlocks,
		}, nil

	case "function_call":
		var call struct {
			Type      string `json:"type"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			CallID    string `json:"call_id"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		if call.Name == "" || call.CallID == "" {
			return nil, nil
		}
		input := normalizeCodexToolInput(call.Name, parseToolArguments(call.Arguments))
		return &ConversationItem{
			Timestamp: timestamp,
			Role:      "assistant",
			Content: []events.ClaudeMessageContent{
				{
					Type:      "tool_use",
					ToolName:  call.Name,
					ToolID:    call.CallID,
					ToolInput: input,
				},
			},
		}, nil

	case "function_call_output":
		var out struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		if out.CallID == "" || strings.TrimSpace(out.Output) == "" {
			return nil, nil
		}
		return &ConversationItem{
			Timestamp: timestamp,
			Role:      "assistant",
			Content: []events.ClaudeMessageContent{
				{
					Type:      "tool_result",
					ToolUseID: out.CallID,
					Content:   out.Output,
					IsError:   detectToolOutputError(out.Output, 0),
				},
			},
		}, nil

	case "custom_tool_call":
		var call struct {
			Type   string          `json:"type"`
			Status string          `json:"status"`
			CallID string          `json:"call_id"`
			Name   string          `json:"name"`
			Input  json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		if call.Name == "" || call.CallID == "" {
			return nil, nil
		}
		input := normalizeCodexToolInput(call.Name, parseToolInput(call.Input))
		return &ConversationItem{
			Timestamp: timestamp,
			Role:      "assistant",
			Content: []events.ClaudeMessageContent{
				{
					Type:      "tool_use",
					ToolName:  call.Name,
					ToolID:    call.CallID,
					ToolInput: input,
				},
			},
		}, nil

	case "custom_tool_call_output":
		var out struct {
			Type   string `json:"type"`
			CallID string `json:"call_id"`
			Output string `json:"output"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return nil, err
		}
		if out.CallID == "" || strings.TrimSpace(out.Output) == "" {
			return nil, nil
		}

		normalizedOutput, exitCode := normalizeToolOutput(out.Output)
		return &ConversationItem{
			Timestamp: timestamp,
			Role:      "assistant",
			Content: []events.ClaudeMessageContent{
				{
					Type:      "tool_result",
					ToolUseID: out.CallID,
					Content:   normalizedOutput,
					IsError:   detectToolOutputError(normalizedOutput, exitCode),
				},
			},
		}, nil

	case "reasoning":
		// Codex records an opaque encrypted_content plus a readable summary. The summary
		// is sufficient for UI purposes.
		var r struct {
			Type    string `json:"type"`
			Summary []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"summary"`
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}

		text := extractReasoningSummary(r.Summary)
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}

		return &ConversationItem{
			Timestamp: timestamp,
			Role:      "assistant",
			Content: []events.ClaudeMessageContent{
				{Type: "thinking", Text: text},
			},
		}, nil

	default:
		return nil, nil
	}
}

func parseEventMsg(timestamp string, raw json.RawMessage) (*ConversationItem, error) {
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}

	switch header.Type {
	case "agent_reasoning":
		// Ignore raw `event_msg.agent_reasoning` lines so mobile clients only receive
		// normalized assistant content from `response_item` records.
		return nil, nil
	case "turn_aborted":
		// Codex also emits a matching response_item user message that includes the
		// interruption text body. We normalize that message and skip this event to
		// avoid duplicate timeline rows.
		return nil, nil
	case "context_compacted":
		// Codex emits context compaction as a standalone event with no text body.
		// Emit a synthetic summary message so mobile can render a compaction marker.
		const fallbackSummary = "Conversation compacted to continue this session."
		var compact struct {
			Type        string `json:"type"`
			UserSummary string `json:"user_summary"`
			Summary     string `json:"summary"`
			Message     string `json:"message"`
		}
		if err := json.Unmarshal(raw, &compact); err != nil {
			return nil, err
		}
		summary := strings.TrimSpace(compact.UserSummary)
		if summary == "" {
			summary = strings.TrimSpace(compact.Summary)
		}
		if summary == "" {
			summary = strings.TrimSpace(compact.Message)
		}
		if summary == "" {
			summary = fallbackSummary
		}
		return &ConversationItem{
			Timestamp:           timestamp,
			Role:                "user",
			IsContextCompaction: true,
			Content: []events.ClaudeMessageContent{
				{Type: "text", Text: summary},
			},
		}, nil
	default:
		return nil, nil
	}
}

func extractTextContentBlocks(raw json.RawMessage) []events.ClaudeMessageContent {
	if raw == nil {
		return nil
	}

	// Content can be a simple string or an array of blocks ({type,text,...}).
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if strings.TrimSpace(asString) == "" {
			return nil
		}
		return []events.ClaudeMessageContent{{Type: "text", Text: asString}}
	}

	var blocks []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil
	}

	var out []events.ClaudeMessageContent
	for _, b := range blocks {
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		out = append(out, events.ClaudeMessageContent{Type: "text", Text: b.Text})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseToolArguments(arguments string) map[string]interface{} {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return nil
	}

	var m map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &m); err == nil && len(m) > 0 {
		return m
	}

	// Fallback: keep raw text.
	return map[string]interface{}{"arguments": arguments}
}

func parseToolInput(raw json.RawMessage) map[string]interface{} {
	if raw == nil {
		return nil
	}

	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		// Fallback to raw string.
		return map[string]interface{}{"input": string(raw)}
	}

	switch t := v.(type) {
	case map[string]interface{}:
		return t
	case string:
		// Large tool inputs (like apply_patch patches) are strings.
		if strings.TrimSpace(t) == "" {
			return nil
		}
		return map[string]interface{}{"input": t}
	default:
		return map[string]interface{}{"input": t}
	}
}

// normalizeCodexToolInput adds common aliases so Codex tool payloads map cleanly
// to existing Claude-oriented renderers in cdev/cdev-ios.
func normalizeCodexToolInput(toolName string, input map[string]interface{}) map[string]interface{} {
	if len(input) == 0 {
		return input
	}

	switch toolName {
	case "exec_command":
		// Codex uses "cmd", while existing UI renderers prioritize "command".
		if cmd, ok := input["cmd"].(string); ok && strings.TrimSpace(cmd) != "" {
			if _, exists := input["command"]; !exists {
				input["command"] = cmd
			}
		}
	case "view_image":
		// Keep image paths concise in UI when they point to workspace-local .cdev storage.
		if path, ok := input["path"].(string); ok && strings.TrimSpace(path) != "" {
			input["path"] = compactDotCdevPath(path)
		}
	}

	return input
}

func compactDotCdevPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "/.cdev/"); idx >= 0 {
		// Trim absolute workspace prefix: /abs/workspace/.cdev/... -> .cdev/...
		return path[idx+1:]
	}
	return path
}

func extractReasoningSummary(summary []struct {
	Type string `json:"type"`
	Text string `json:"text"`
}) string {
	if len(summary) == 0 {
		return ""
	}
	var parts []string
	for _, s := range summary {
		if s.Type != "summary_text" {
			continue
		}
		if strings.TrimSpace(s.Text) == "" {
			continue
		}
		parts = append(parts, s.Text)
	}
	return strings.Join(parts, "\n")
}

// normalizeToolOutput handles cases where Codex embeds a JSON string in output.
// Returns the cleaned output plus an exit code if present (0 if unknown).
func normalizeToolOutput(output string) (string, int) {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return "", 0
	}

	// Many custom tool outputs are JSON strings like:
	// {"output":"Success...","metadata":{"exit_code":0,...}}
	var wrapped struct {
		Output   string `json:"output"`
		Metadata struct {
			ExitCode int `json:"exit_code"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(trimmed), &wrapped) == nil && strings.TrimSpace(wrapped.Output) != "" {
		return wrapped.Output, wrapped.Metadata.ExitCode
	}

	return output, 0
}

func detectToolOutputError(output string, exitCode int) bool {
	if exitCode != 0 {
		return true
	}

	// Best-effort heuristic for exec_command outputs.
	// We see strings like: "Process exited with code 1" or "Exit code: 1".
	if code, ok := parseExitCode(output, "process exited with code"); ok {
		return code != 0
	}
	if code, ok := parseExitCode(output, "exit code:"); ok {
		return code != 0
	}
	return false
}

func parseExitCode(output, marker string) (int, bool) {
	lower := strings.ToLower(output)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return 0, false
	}
	rest := output[idx+len(marker):]
	rest = strings.TrimLeft(rest, " \t=:")

	n := 0
	i := 0
	for i < len(rest) {
		c := rest[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		i++
	}
	if i == 0 {
		return 0, false
	}
	return n, true
}

func shouldHideCodexUserMessage(blocks []events.ClaudeMessageContent) bool {
	if len(blocks) == 0 {
		return false
	}

	for _, block := range blocks {
		if block.Type != "text" {
			continue
		}
		if !shouldHideCodexBootstrapText(block.Text) {
			return false
		}
	}

	return true
}

func shouldHideCodexBootstrapText(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	// Codex often injects repository bootstrap instructions as the first user item.
	// This is operational context, not user chat content.
	if strings.HasPrefix(trimmed, "# AGENTS.md instructions for ") &&
		strings.Contains(trimmed, "<INSTRUCTIONS>") {
		return true
	}

	// Codex also injects shell/cwd context as a dedicated user message.
	if strings.HasPrefix(trimmed, "<environment_context>") &&
		strings.Contains(trimmed, "</environment_context>") &&
		strings.Contains(trimmed, "<cwd>") {
		return true
	}

	return false
}

func normalizeCodexUserMessage(blocks []events.ClaudeMessageContent) ([]events.ClaudeMessageContent, bool) {
	if len(blocks) == 0 {
		return blocks, false
	}

	isTurnAborted := false
	out := make([]events.ClaudeMessageContent, 0, len(blocks))
	for _, block := range blocks {
		if block.Type != "text" {
			out = append(out, block)
			continue
		}
		if message, ok := extractTurnAbortedMessage(block.Text); ok {
			block.Text = message
			isTurnAborted = true
		}
		if message, ok := extractUserShellCommandMessage(block.Text); ok {
			block.Text = message
		}
		out = append(out, block)
	}
	return out, isTurnAborted
}

func extractTurnAbortedMessage(text string) (string, bool) {
	const (
		openTag                    = "<turn_aborted>"
		closeTag                   = "</turn_aborted>"
		fallbackInterruptedMessage = "The previous turn was interrupted."
	)

	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, openTag) || !strings.HasSuffix(trimmed, closeTag) {
		return "", false
	}

	body := strings.TrimPrefix(trimmed, openTag)
	body = strings.TrimSuffix(body, closeTag)
	body = strings.TrimSpace(body)
	if body == "" {
		body = fallbackInterruptedMessage
	}
	return body, true
}

func extractUserShellCommandMessage(text string) (string, bool) {
	body, ok := extractTaggedBody(text, "user_shell_command")
	if !ok {
		return "", false
	}

	command, _ := extractTaggedBody(body, "command")
	result, _ := extractTaggedBody(body, "result")
	command = strings.TrimSpace(command)
	result = strings.TrimSpace(result)

	if command == "" && result == "" {
		return "", false
	}

	lines := []string{"You ran a shell command"}
	if command != "" {
		lines[0] = "You ran " + command
	}

	resultLines := summarizeUserShellCommandResult(result)
	if len(resultLines) > 0 {
		lines = append(lines, "  └ "+resultLines[0])
		for _, line := range resultLines[1:] {
			lines = append(lines, "    "+line)
		}
	}

	return strings.Join(lines, "\n"), true
}

func summarizeUserShellCommandResult(result string) []string {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil
	}

	trimmedLines := splitNonEmptyTrimmedLines(result)
	if len(trimmedLines) == 0 {
		return nil
	}

	outputLine := -1
	exitLine := ""
	for i, line := range trimmedLines {
		lower := strings.ToLower(line)
		if lower == "output:" {
			outputLine = i
			continue
		}
		if strings.HasPrefix(lower, "exit code:") && exitLine == "" {
			exitLine = line
		}
	}

	if outputLine >= 0 && outputLine+1 < len(trimmedLines) {
		outputLines := trimmedLines[outputLine+1:]
		if exitCode, ok := parseExitCode(result, "exit code:"); ok && exitCode != 0 {
			if exitLine == "" {
				exitLine = fmt.Sprintf("Exit code: %d", exitCode)
			}
			return append([]string{exitLine}, outputLines...)
		}
		return outputLines
	}

	return trimmedLines
}

func extractTaggedBody(text, tag string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", false
	}

	openTag := "<" + tag + ">"
	closeTag := "</" + tag + ">"
	start := strings.Index(trimmed, openTag)
	if start < 0 {
		return "", false
	}
	start += len(openTag)
	end := strings.Index(trimmed[start:], closeTag)
	if end < 0 {
		return "", false
	}
	body := trimmed[start : start+end]
	return strings.TrimSpace(body), true
}

func splitNonEmptyTrimmedLines(text string) []string {
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out
}
