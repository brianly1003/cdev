// Package sessioncache provides session caching and element parsing.
package sessioncache

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ElementType represents the type of UI element.
type ElementType string

const (
	ElementTypeUserInput     ElementType = "user_input"
	ElementTypeAssistantText ElementType = "assistant_text"
	ElementTypeToolCall      ElementType = "tool_call"
	ElementTypeToolResult    ElementType = "tool_result"
	ElementTypeDiff          ElementType = "diff"
	ElementTypeThinking      ElementType = "thinking"
	ElementTypeInterrupted   ElementType = "interrupted"
)

// ToolStatus represents the status of a tool call.
type ToolStatus string

const (
	ToolStatusRunning     ToolStatus = "running"
	ToolStatusCompleted   ToolStatus = "completed"
	ToolStatusError       ToolStatus = "error"
	ToolStatusInterrupted ToolStatus = "interrupted"
)

// DiffLineType represents the type of diff line.
type DiffLineType string

const (
	DiffLineContext DiffLineType = "context"
	DiffLineAdded   DiffLineType = "added"
	DiffLineRemoved DiffLineType = "removed"
)

// Element represents a UI element parsed from session data.
// @Description UI element with type-specific content
type Element struct {
	ID        string          `json:"id" example:"elem_001"`
	Type      ElementType     `json:"type" example:"assistant_text"`
	Timestamp string          `json:"timestamp" example:"2025-12-19T10:30:00Z"`
	Content   json.RawMessage `json:"content" swaggertype:"object"`
}

// UserInputContent represents user input element content.
type UserInputContent struct {
	Text string `json:"text"`
}

// AssistantTextContent represents assistant text element content.
type AssistantTextContent struct {
	Text string `json:"text"`
}

// ToolCallContent represents tool call element content.
type ToolCallContent struct {
	Tool       string                 `json:"tool"`
	ToolID     string                 `json:"tool_id,omitempty"`
	Display    string                 `json:"display"`
	Params     map[string]interface{} `json:"params"`
	Status     ToolStatus             `json:"status"`
	DurationMS int64                  `json:"duration_ms,omitempty"`
}

// ToolResultContent represents tool result element content.
type ToolResultContent struct {
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
	IsError     bool   `json:"is_error"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Summary     string `json:"summary"`
	FullContent string `json:"full_content"`
	LineCount   int    `json:"line_count"`
	Expandable  bool   `json:"expandable"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// DiffContent represents diff element content.
type DiffContent struct {
	ToolCallID string      `json:"tool_call_id"`
	FilePath   string      `json:"file_path"`
	Summary    DiffSummary `json:"summary"`
	Hunks      []DiffHunk  `json:"hunks"`
}

// DiffSummary represents a summary of diff changes.
type DiffSummary struct {
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Display string `json:"display"`
}

// DiffHunk represents a diff hunk.
type DiffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"old_start"`
	OldCount int        `json:"old_count"`
	NewStart int        `json:"new_start"`
	NewCount int        `json:"new_count"`
	Lines    []DiffLine `json:"lines"`
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type    DiffLineType `json:"type"`
	OldLine *int         `json:"old_line,omitempty"`
	NewLine *int         `json:"new_line,omitempty"`
	Content string       `json:"content"`
}

// ThinkingContent represents thinking element content.
type ThinkingContent struct {
	Text      string `json:"text"`
	Collapsed bool   `json:"collapsed"`
}

// InterruptedContent represents interrupted element content.
type InterruptedContent struct {
	ToolCallID string `json:"tool_call_id"`
	Message    string `json:"message"`
}

// ElementsPagination represents pagination info for elements.
type ElementsPagination struct {
	Total         int    `json:"total"`
	Returned      int    `json:"returned"`
	HasMoreBefore bool   `json:"has_more_before"`
	HasMoreAfter  bool   `json:"has_more_after"`
	OldestID      string `json:"oldest_id,omitempty"`
	NewestID      string `json:"newest_id,omitempty"`
}

// ElementsResponse represents the response for elements API.
type ElementsResponse struct {
	SessionID  string             `json:"session_id"`
	Elements   []Element          `json:"elements"`
	Pagination ElementsPagination `json:"pagination"`
}

// Regex patterns for parsing
var (
	functionCallsPattern = regexp.MustCompile(`<function_calls>\s*<invoke name="(\w+)">(.*?)</invoke>\s*</function_calls>`)
	parameterPattern     = regexp.MustCompile(`<parameter name="([^"]+)">([^<]*)</parameter>`)
	responsePattern      = regexp.MustCompile(`<response>\s*(.*?)\s*</response>`)
)

// ParseSessionToElements converts session messages to UI elements.
func ParseSessionToElements(messages []json.RawMessage, sessionID string) ([]Element, error) {
	var elements []Element
	elementCounter := 0

	for _, msgRaw := range messages {
		var msg struct {
			Type      string `json:"type"`
			UUID      string `json:"uuid"`
			Timestamp string `json:"timestamp"`
			Message   struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}

		if err := json.Unmarshal(msgRaw, &msg); err != nil {
			continue
		}

		timestamp := msg.Timestamp
		if timestamp == "" {
			timestamp = time.Now().UTC().Format(time.RFC3339)
		}

		switch msg.Type {
		case "user":
			userElements := parseUserMessage(msg.Message.Content, msg.UUID, timestamp, &elementCounter)
			elements = append(elements, userElements...)

		case "assistant":
			assistantElements := parseAssistantMessage(msg.Message.Content, msg.UUID, timestamp, &elementCounter)
			elements = append(elements, assistantElements...)
		}
	}

	return elements, nil
}

// parseUserMessage parses a user message into elements.
func parseUserMessage(content json.RawMessage, uuid, timestamp string, counter *int) []Element {
	var elements []Element

	// Try as string first
	var textContent string
	if err := json.Unmarshal(content, &textContent); err == nil && textContent != "" {
		*counter++
		elem := createUserInputElement(fmt.Sprintf("elem_%05d", *counter), timestamp, textContent)
		elements = append(elements, elem)
		return elements
	}

	// Try as array of content blocks
	var contentBlocks []struct {
		Type       string `json:"type"`
		Text       string `json:"text,omitempty"`
		Content    string `json:"content,omitempty"`
		ToolUseID  string `json:"tool_use_id,omitempty"`
		IsError    bool   `json:"is_error,omitempty"`
	}

	if err := json.Unmarshal(content, &contentBlocks); err == nil {
		for _, block := range contentBlocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					*counter++
					elem := createUserInputElement(fmt.Sprintf("elem_%05d", *counter), timestamp, block.Text)
					elements = append(elements, elem)
				}
			case "tool_result":
				*counter++
				elem := createToolResultElement(
					fmt.Sprintf("elem_%05d", *counter),
					timestamp,
					block.ToolUseID,
					"",
					block.IsError,
					block.Content,
				)
				elements = append(elements, elem)
			}
		}
	}

	return elements
}

// parseAssistantMessage parses an assistant message into elements.
func parseAssistantMessage(content json.RawMessage, uuid, timestamp string, counter *int) []Element {
	var elements []Element

	var contentBlocks []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	}

	if err := json.Unmarshal(content, &contentBlocks); err != nil {
		return elements
	}

	for _, block := range contentBlocks {
		switch block.Type {
		case "text":
			// Parse embedded function calls from text
			textElements := parseTextWithFunctionCalls(block.Text, timestamp, counter)
			elements = append(elements, textElements...)

		case "thinking":
			*counter++
			elem := createThinkingElement(fmt.Sprintf("elem_%05d", *counter), timestamp, block.Text)
			elements = append(elements, elem)

		case "tool_use":
			*counter++
			elem := createToolCallElement(
				fmt.Sprintf("elem_%05d", *counter),
				timestamp,
				block.Name,
				block.ID,
				block.Input,
			)
			elements = append(elements, elem)
		}
	}

	return elements
}

// parseTextWithFunctionCalls extracts function calls and plain text from a text block.
func parseTextWithFunctionCalls(text string, timestamp string, counter *int) []Element {
	var elements []Element

	// Find all function calls and responses
	lastEnd := 0

	// Find function_calls blocks
	matches := functionCallsPattern.FindAllStringSubmatchIndex(text, -1)
	responses := responsePattern.FindAllStringSubmatchIndex(text, -1)

	responseMap := make(map[int][]int) // Map function call end to response match
	for _, resp := range responses {
		// Find the closest function call before this response
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i][1] < resp[0] {
				responseMap[matches[i][1]] = resp
				break
			}
		}
	}

	for i, match := range matches {
		// Extract text before this function call
		if match[0] > lastEnd {
			plainText := strings.TrimSpace(text[lastEnd:match[0]])
			if plainText != "" {
				*counter++
				elem := createAssistantTextElement(fmt.Sprintf("elem_%05d", *counter), timestamp, plainText)
				elements = append(elements, elem)
			}
		}

		// Extract function call
		toolName := text[match[2]:match[3]]
		paramsStr := text[match[4]:match[5]]

		// Parse parameters
		params := make(map[string]interface{})
		paramMatches := parameterPattern.FindAllStringSubmatch(paramsStr, -1)
		for _, pm := range paramMatches {
			if len(pm) >= 3 {
				params[pm[1]] = pm[2]
			}
		}

		*counter++
		toolCallID := fmt.Sprintf("elem_%05d", *counter)
		elem := createToolCallElementFromParams(toolCallID, timestamp, toolName, params)
		elements = append(elements, elem)

		// Check for response
		lastEnd = match[1]
		if resp, ok := responseMap[match[1]]; ok {
			respContent := text[resp[2]:resp[3]]

			*counter++
			// Check if this is a diff (Update/Edit tool)
			if toolName == "Update" || toolName == "Edit" {
				diffElem := createDiffElementFromResponse(
					fmt.Sprintf("elem_%05d", *counter),
					timestamp,
					toolCallID,
					params,
					respContent,
				)
				elements = append(elements, diffElem)
			} else {
				resultElem := createToolResultFromResponse(
					fmt.Sprintf("elem_%05d", *counter),
					timestamp,
					toolCallID,
					toolName,
					respContent,
				)
				elements = append(elements, resultElem)
			}
			lastEnd = resp[1]

			// Skip to after response for next iteration
			if i < len(matches)-1 && lastEnd > matches[i+1][0] {
				continue
			}
		}
	}

	// Extract remaining text after last function call
	if lastEnd < len(text) {
		plainText := strings.TrimSpace(text[lastEnd:])
		if plainText != "" {
			*counter++
			elem := createAssistantTextElement(fmt.Sprintf("elem_%05d", *counter), timestamp, plainText)
			elements = append(elements, elem)
		}
	}

	// If no function calls found, treat entire text as assistant text
	if len(matches) == 0 && strings.TrimSpace(text) != "" {
		*counter++
		elem := createAssistantTextElement(fmt.Sprintf("elem_%05d", *counter), timestamp, strings.TrimSpace(text))
		elements = append(elements, elem)
	}

	return elements
}

// Helper functions to create elements

func createUserInputElement(id, timestamp, text string) Element {
	content := UserInputContent{Text: text}
	contentJSON, _ := json.Marshal(content)
	return Element{
		ID:        id,
		Type:      ElementTypeUserInput,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createAssistantTextElement(id, timestamp, text string) Element {
	content := AssistantTextContent{Text: text}
	contentJSON, _ := json.Marshal(content)
	return Element{
		ID:        id,
		Type:      ElementTypeAssistantText,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createThinkingElement(id, timestamp, text string) Element {
	content := ThinkingContent{Text: text, Collapsed: true}
	contentJSON, _ := json.Marshal(content)
	return Element{
		ID:        id,
		Type:      ElementTypeThinking,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createToolCallElement(id, timestamp, toolName, toolID string, input json.RawMessage) Element {
	var params map[string]interface{}
	json.Unmarshal(input, &params)

	display := formatToolDisplay(toolName, params)

	content := ToolCallContent{
		Tool:    toolName,
		ToolID:  toolID,
		Display: display,
		Params:  params,
		Status:  ToolStatusCompleted,
	}
	contentJSON, _ := json.Marshal(content)
	return Element{
		ID:        id,
		Type:      ElementTypeToolCall,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createToolCallElementFromParams(id, timestamp, toolName string, params map[string]interface{}) Element {
	display := formatToolDisplay(toolName, params)

	content := ToolCallContent{
		Tool:    toolName,
		Display: display,
		Params:  params,
		Status:  ToolStatusCompleted,
	}
	contentJSON, _ := json.Marshal(content)
	return Element{
		ID:        id,
		Type:      ElementTypeToolCall,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createToolResultElement(id, timestamp, toolCallID, toolName string, isError bool, content string) Element {
	lines := strings.Split(content, "\n")
	summary := content
	if len(lines) > 0 {
		summary = lines[0]
		if len(summary) > 80 {
			summary = summary[:80] + "..."
		}
	}

	resultContent := ToolResultContent{
		ToolCallID:  toolCallID,
		ToolName:    toolName,
		IsError:     isError,
		Summary:     summary,
		FullContent: content,
		LineCount:   len(lines),
		Expandable:  len(lines) > 1,
	}
	contentJSON, _ := json.Marshal(resultContent)
	return Element{
		ID:        id,
		Type:      ElementTypeToolResult,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createToolResultFromResponse(id, timestamp, toolCallID, toolName, respContent string) Element {
	// Parse JSON response
	var respData struct {
		DurationMS int64       `json:"duration_ms"`
		Result     interface{} `json:"result"`
		Error      string      `json:"error,omitempty"`
	}

	isError := false
	var fullContent string

	if err := json.Unmarshal([]byte(respContent), &respData); err == nil {
		if respData.Error != "" {
			isError = true
			fullContent = respData.Error
		} else {
			switch v := respData.Result.(type) {
			case string:
				fullContent = v
			case []interface{}:
				// Array of strings (e.g., file list)
				var parts []string
				for _, item := range v {
					if s, ok := item.(string); ok {
						parts = append(parts, s)
					}
				}
				fullContent = strings.Join(parts, "\n")
			default:
				resultJSON, _ := json.MarshalIndent(respData.Result, "", "  ")
				fullContent = string(resultJSON)
			}
		}
	} else {
		fullContent = respContent
	}

	lines := strings.Split(fullContent, "\n")
	summary := fullContent
	if len(lines) > 0 {
		summary = lines[0]
		if len(summary) > 80 {
			summary = summary[:80] + "..."
		}
	}

	// Check for error patterns
	if strings.HasPrefix(summary, "Error:") || strings.HasPrefix(summary, "Exit code") {
		isError = true
	}

	resultContent := ToolResultContent{
		ToolCallID:  toolCallID,
		ToolName:    toolName,
		IsError:     isError,
		Summary:     summary,
		FullContent: fullContent,
		LineCount:   len(lines),
		Expandable:  len(lines) > 1,
	}
	contentJSON, _ := json.Marshal(resultContent)
	return Element{
		ID:        id,
		Type:      ElementTypeToolResult,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func createDiffElementFromResponse(id, timestamp, toolCallID string, params map[string]interface{}, respContent string) Element {
	filePath := ""
	if fp, ok := params["file_path"].(string); ok {
		filePath = fp
	}

	// Parse response to extract diff info
	var respData struct {
		DurationMS int64  `json:"duration_ms"`
		Result     string `json:"result"`
	}
	json.Unmarshal([]byte(respContent), &respData)

	// Parse diff from result or generate summary
	added, removed := countDiffLines(respData.Result)

	summary := DiffSummary{
		Added:   added,
		Removed: removed,
		Display: formatDiffSummary(added, removed),
	}

	hunks := parseDiffHunks(respData.Result)

	diffContent := DiffContent{
		ToolCallID: toolCallID,
		FilePath:   filePath,
		Summary:    summary,
		Hunks:      hunks,
	}
	contentJSON, _ := json.Marshal(diffContent)
	return Element{
		ID:        id,
		Type:      ElementTypeDiff,
		Timestamp: timestamp,
		Content:   contentJSON,
	}
}

func formatToolDisplay(toolName string, params map[string]interface{}) string {
	var display string

	switch toolName {
	case "Bash":
		if cmd, ok := params["command"].(string); ok {
			if len(cmd) > 80 {
				cmd = cmd[:80] + "..."
			}
			display = fmt.Sprintf("%s(%s)", toolName, cmd)
		} else {
			display = toolName + "()"
		}
	case "Read", "Write", "Update", "Edit":
		if path, ok := params["file_path"].(string); ok {
			display = fmt.Sprintf("%s(%s)", toolName, path)
		} else {
			display = toolName + "()"
		}
	case "Glob", "Search":
		if pattern, ok := params["pattern"].(string); ok {
			display = fmt.Sprintf("%s(pattern: \"%s\")", toolName, pattern)
		} else {
			display = toolName + "()"
		}
	case "Grep":
		if pattern, ok := params["pattern"].(string); ok {
			display = fmt.Sprintf("%s(pattern: \"%s\")", toolName, pattern)
		} else {
			display = toolName + "()"
		}
	default:
		display = toolName + "()"
	}

	return display
}

func formatDiffSummary(added, removed int) string {
	if added > 0 && removed > 0 {
		return fmt.Sprintf("Added %d lines, removed %d lines", added, removed)
	} else if added > 0 {
		return fmt.Sprintf("Added %d lines", added)
	} else if removed > 0 {
		return fmt.Sprintf("Removed %d lines", removed)
	}
	return "No changes"
}

func countDiffLines(diff string) (added, removed int) {
	lines := strings.Split(diff, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			removed++
		}
	}
	return
}

func parseDiffHunks(diff string) []DiffHunk {
	var hunks []DiffHunk

	// Simple diff parsing - can be enhanced
	lines := strings.Split(diff, "\n")
	var currentHunk *DiffHunk
	oldLine, newLine := 1, 1

	hunkHeaderPattern := regexp.MustCompile(`^@@\s*-(\d+)(?:,(\d+))?\s*\+(\d+)(?:,(\d+))?\s*@@`)

	for _, line := range lines {
		if matches := hunkHeaderPattern.FindStringSubmatch(line); matches != nil {
			if currentHunk != nil {
				hunks = append(hunks, *currentHunk)
			}
			currentHunk = &DiffHunk{
				Header: line,
				Lines:  []DiffLine{},
			}
			fmt.Sscanf(matches[1], "%d", &currentHunk.OldStart)
			fmt.Sscanf(matches[3], "%d", &currentHunk.NewStart)
			oldLine = currentHunk.OldStart
			newLine = currentHunk.NewStart
			continue
		}

		if currentHunk == nil {
			continue
		}

		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			nl := newLine
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:    DiffLineAdded,
				NewLine: &nl,
				Content: strings.TrimPrefix(line, "+"),
			})
			newLine++
		} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
			ol := oldLine
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:    DiffLineRemoved,
				OldLine: &ol,
				Content: strings.TrimPrefix(line, "-"),
			})
			oldLine++
		} else if strings.HasPrefix(line, " ") || line == "" {
			ol := oldLine
			nl := newLine
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:    DiffLineContext,
				OldLine: &ol,
				NewLine: &nl,
				Content: strings.TrimPrefix(line, " "),
			})
			oldLine++
			newLine++
		}
	}

	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks
}
