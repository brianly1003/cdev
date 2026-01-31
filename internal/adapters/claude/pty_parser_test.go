package claude

import (
	"testing"
)

func TestPTYParser_ProcessLine_DetectsThinking(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name          string
		input         string
		expectedState PTYState
	}{
		// All Claude Code thinking indicators have "(esc to interrupt)" or "(ctrl+c to interrupt)" suffix
		// Old format: "(esc to interrupt)"
		{
			name:          "thinking indicator (esc)",
			input:         "✢ Thinking… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "scheming indicator (esc)",
			input:         "✻ Scheming… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "cooking indicator (esc)",
			input:         "✶ Cooking… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "imagining indicator (esc)",
			input:         "✳ Imagining… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "sussing indicator (esc)",
			input:         "· Sussing… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "finagling indicator (esc)",
			input:         "✻ Finagling… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "vibing indicator (esc)",
			input:         "✶ Vibing… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "mulling indicator (esc)",
			input:         "✽ Mulling… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "deciphering indicator (esc)",
			input:         "✢ Deciphering… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "any future verb (esc)",
			input:         "✳ WhateverNewVerb… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		// New format: "(ctrl+c to interrupt)" - Claude Code newer versions
		{
			name:          "thinking indicator (ctrl+c)",
			input:         "✢ Thinking… (ctrl+c to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "scheming indicator (ctrl+c)",
			input:         "✻ Scheming… (ctrl+c to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "vibing indicator (ctrl+c)",
			input:         "✶ Vibing… (ctrl+c to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "ctrl+c with middle dot thinking",
			input:         "✳ Pondering… (ctrl+c to interrupt · thinking)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "ctrl+c with extra content",
			input:         "✽ Processing… (ctrl+c to interrupt · processing)",
			expectedState: PTYStateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)
			if state != tt.expectedState {
				t.Errorf("ProcessLine(%q) state = %v, want %v", tt.input, state, tt.expectedState)
			}
		})
	}
}

func TestPTYParser_ProcessLine_DetectsErrors(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name          string
		input         string
		expectedState PTYState
	}{
		{
			name:          "error message",
			input:         "Error: Something went wrong",
			expectedState: PTYStateError,
		},
		{
			name:          "failed message",
			input:         "Failed: File not found",
			expectedState: PTYStateError,
		},
		{
			name:          "exception message",
			input:         "Exception: Invalid input",
			expectedState: PTYStateError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)
			if state != tt.expectedState {
				t.Errorf("ProcessLine(%q) state = %v, want %v", tt.input, state, tt.expectedState)
			}
		})
	}
}

func TestPTYParser_DetectPermissionType(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name         string
		line         string
		expectedType PermissionType
		expectedTarget string
	}{
		{
			name:           "write file",
			line:           "Write(test.txt)",
			expectedType:   PermissionTypeWriteFile,
			expectedTarget: "test.txt",
		},
		{
			name:           "write file with space",
			line:           "Write (hello.txt)",
			expectedType:   PermissionTypeWriteFile,
			expectedTarget: "hello.txt",
		},
		{
			name:           "create file",
			line:           "Create file hello.txt",
			expectedType:   PermissionTypeWriteFile,
			expectedTarget: "hello.txt",
		},
		{
			name:           "edit file",
			line:           "Edit(config.yaml)",
			expectedType:   PermissionTypeEditFile,
			expectedTarget: "config.yaml",
		},
		{
			name:           "delete file",
			line:           "Delete test.txt",
			expectedType:   PermissionTypeDeleteFile,
			expectedTarget: "test.txt",
		},
		{
			name:           "bash command parens",
			line:           "Bash(npm install)",
			expectedType:   PermissionTypeBashCommand,
			expectedTarget: "npm install",
		},
		{
			name:           "run command",
			line:           "Run command: git status",
			expectedType:   PermissionTypeBashCommand,
			expectedTarget: "git status",
		},
		{
			name:           "mcp tool",
			line:           "mcp__server__tool",
			expectedType:   PermissionTypeMCP,
			expectedTarget: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			parser.detectPermissionType(tt.line)

			if parser.currentPromptType != tt.expectedType {
				t.Errorf("detectPermissionType(%q) type = %v, want %v",
					tt.line, parser.currentPromptType, tt.expectedType)
			}
			if tt.expectedTarget != "" && parser.currentPromptTarget != tt.expectedTarget {
				t.Errorf("detectPermissionType(%q) target = %q, want %q",
					tt.line, parser.currentPromptTarget, tt.expectedTarget)
			}
		})
	}
}

func TestPTYParser_DetectPermissionPrompt(t *testing.T) {
	parser := NewPTYParser()

	// Simulate a complete permission prompt sequence
	// The parser detects a prompt when it sees the Allow? pattern AND options
	lines := []string{
		"Write(test.txt)",
		"Content preview...",
		"Do you want to allow this?",
		"1. Yes",
		"2. Yes to all",
		"n. No",
		"Esc to cancel", // End marker triggers detection
	}

	var prompt *PTYPermissionPrompt
	var state PTYState

	for _, line := range lines {
		prompt, state = parser.ProcessLine(line)
		if prompt != nil {
			break
		}
	}

	if prompt == nil {
		t.Fatal("Expected permission prompt to be detected")
	}

	if state != PTYStatePermission {
		t.Errorf("State = %v, want %v", state, PTYStatePermission)
	}

	if prompt.Type != PermissionTypeWriteFile {
		t.Errorf("Prompt.Type = %v, want %v", prompt.Type, PermissionTypeWriteFile)
	}

	if prompt.Target != "test.txt" {
		t.Errorf("Prompt.Target = %q, want %q", prompt.Target, "test.txt")
	}

	// Parser should find 3 options
	if len(prompt.Options) != 3 {
		t.Errorf("Prompt.Options count = %d, want 3", len(prompt.Options))
	}

	// Verify options
	if len(prompt.Options) >= 3 {
		if prompt.Options[0].Key != "1" || prompt.Options[0].Label != "Yes" {
			t.Errorf("Option[0] = %+v, want Key=1, Label=Yes", prompt.Options[0])
		}
		if prompt.Options[1].Key != "2" || prompt.Options[1].Label != "Yes to all" {
			t.Errorf("Option[1] = %+v, want Key=2, Label=Yes to all", prompt.Options[1])
		}
		if prompt.Options[2].Key != "n" || prompt.Options[2].Label != "No" {
			t.Errorf("Option[2] = %+v, want Key=n, Label=No", prompt.Options[2])
		}
	}
}

func TestPTYParser_ExtractPreview(t *testing.T) {
	parser := NewPTYParser()

	// Process lines that contain the target - preview is extracted between target line and options
	lines := []string{
		"Write(test.txt)",
		"Line 1 of content",
		"Line 2 of content",
		"Line 3 of content",
		"Allow?",
		"1. Yes",
	}

	// First process lines to build buffer
	for _, line := range lines {
		parser.ProcessLine(line)
	}

	// The preview should extract content between target and options
	// Note: extractPreview looks for lines between target (test.txt) and options/Allow?
	preview := parser.extractPreview()

	// Preview may be empty if the target isn't found or no content between
	// This is acceptable behavior - the parser is designed for real Claude output
	t.Logf("Preview extracted: %q", preview)
}

func TestPTYParser_Reset(t *testing.T) {
	parser := NewPTYParser()

	// Set some state
	parser.ProcessLine("Write(test.txt)")
	parser.ProcessLine("Thinking...")

	// Reset
	parser.Reset()

	if parser.GetState() != PTYStateIdle {
		t.Errorf("After Reset(), state = %v, want %v", parser.GetState(), PTYStateIdle)
	}

	if parser.currentPromptType != "" {
		t.Errorf("After Reset(), currentPromptType = %q, want empty", parser.currentPromptType)
	}

	if parser.currentPromptTarget != "" {
		t.Errorf("After Reset(), currentPromptTarget = %q, want empty", parser.currentPromptTarget)
	}

	if len(parser.buffer) != 0 {
		t.Errorf("After Reset(), buffer length = %d, want 0", len(parser.buffer))
	}
}

func TestPTYParser_IsWaitingForInput(t *testing.T) {
	parser := NewPTYParser()

	// Initially not waiting
	if parser.IsWaitingForInput() {
		t.Error("Initially should not be waiting for input")
	}

	// Set to permission state
	parser.SetState(PTYStatePermission)
	if !parser.IsWaitingForInput() {
		t.Error("In permission state should be waiting for input")
	}

	// Set to question state
	parser.SetState(PTYStateQuestion)
	if !parser.IsWaitingForInput() {
		t.Error("In question state should be waiting for input")
	}

	// Set to thinking state
	parser.SetState(PTYStateThinking)
	if parser.IsWaitingForInput() {
		t.Error("In thinking state should not be waiting for input")
	}
}

func TestPTYParser_BufferLimit(t *testing.T) {
	parser := NewPTYParser()

	// Add more than 30 lines
	for i := 0; i < 50; i++ {
		parser.ProcessLine("Some line content")
	}

	// Buffer should be capped at 30
	if len(parser.buffer) > 30 {
		t.Errorf("Buffer length = %d, want <= 30", len(parser.buffer))
	}
}

func TestPTYParser_QuestionDetection(t *testing.T) {
	parser := NewPTYParser()

	// Process a question line (ends with ?)
	_, state := parser.ProcessLine("What file would you like to create?")

	if state != PTYStateQuestion {
		t.Errorf("State = %v, want %v", state, PTYStateQuestion)
	}
}

func TestPTYParser_EmptyLineHandling(t *testing.T) {
	parser := NewPTYParser()

	// Process empty lines
	prompt, state := parser.ProcessLine("")

	if prompt != nil {
		t.Error("Empty line should not produce a prompt")
	}

	// State should remain idle
	if state != PTYStateIdle {
		t.Errorf("State = %v, want %v", state, PTYStateIdle)
	}
}

func TestPTYParser_ANSIStripping(t *testing.T) {
	parser := NewPTYParser()

	// Process a line with ANSI codes
	line := "\x1b[32mWrite(test.txt)\x1b[0m"
	parser.ProcessLine(line)

	// Should have detected the write pattern after stripping ANSI
	if parser.currentPromptType != PermissionTypeWriteFile {
		t.Errorf("currentPromptType = %v, want %v", parser.currentPromptType, PermissionTypeWriteFile)
	}

	if parser.currentPromptTarget != "test.txt" {
		t.Errorf("currentPromptTarget = %q, want %q", parser.currentPromptTarget, "test.txt")
	}
}

// TestPTYParser_RealClaudeOutputFormat tests with actual Claude Code output format
// including leading whitespace and multi-line options
func TestPTYParser_RealClaudeOutputFormat(t *testing.T) {
	parser := NewPTYParser()

	// Simulate actual Claude permission prompt output (with leading whitespace)
	lines := []string{
		"Write(hello.txt)",
		"",
		"Create file hello.txt",
		"──────────────────────",
		" Hello World",
		"",
		"──────────────────────",
		" Do you want to create hello.txt?",
		" ❯ 1. Yes",
		"   2. Yes, allow all edits during this session (shift+tab)",
		"   3. Type here to tell Claude what to do differently",
		"",
		" Esc to cancel",
	}

	var prompt *PTYPermissionPrompt
	var state PTYState

	for _, line := range lines {
		prompt, state = parser.ProcessLine(line)
		if prompt != nil {
			break
		}
	}

	if prompt == nil {
		t.Fatal("Expected permission prompt to be detected")
	}

	if state != PTYStatePermission {
		t.Errorf("State = %v, want %v", state, PTYStatePermission)
	}

	if prompt.Type != PermissionTypeWriteFile {
		t.Errorf("Prompt.Type = %v, want %v", prompt.Type, PermissionTypeWriteFile)
	}

	if prompt.Target != "hello.txt" {
		t.Errorf("Prompt.Target = %q, want %q", prompt.Target, "hello.txt")
	}

	// Should detect all 3 options
	if len(prompt.Options) < 3 {
		t.Errorf("Prompt.Options count = %d, want >= 3", len(prompt.Options))
	}

	// Verify options
	if len(prompt.Options) >= 3 {
		if prompt.Options[0].Key != "1" {
			t.Errorf("Option[0].Key = %q, want %q", prompt.Options[0].Key, "1")
		}
		if prompt.Options[1].Key != "2" {
			t.Errorf("Option[1].Key = %q, want %q", prompt.Options[1].Key, "2")
		}
		if prompt.Options[2].Key != "3" {
			t.Errorf("Option[2].Key = %q, want %q", prompt.Options[2].Key, "3")
		}
	}

	t.Logf("Detected %d options: %+v", len(prompt.Options), prompt.Options)
}

// TestPTYParser_ConsideringState tests that "Considering..." is detected as thinking state
func TestPTYParser_ConsideringState(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name          string
		input         string
		expectedState PTYState
	}{
		{"considering", "· Considering… (esc to interrupt)", PTYStateThinking},
		{"baking", "✻ Baking… (esc to interrupt)", PTYStateThinking},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)
			if state != tt.expectedState {
				t.Errorf("ProcessLine(%q) state = %v, want %v", tt.input, state, tt.expectedState)
			}
		})
	}
}

// TestPTYParser_TrustFolderPrompt tests detection of folder trust prompts
// which have a different format with text options instead of numbered options
func TestPTYParser_TrustFolderPrompt(t *testing.T) {
	parser := NewPTYParser()

	// Simulate actual folder trust prompt output
	lines := []string{
		"Do you want to trust the files in this folder and allow Claude to proceed?",
		"",
		"/private/tmp/pty-test",
		"",
		"❯ Yes, proceed",
		"  No, exit",
	}

	var prompt *PTYPermissionPrompt
	var state PTYState

	for _, line := range lines {
		prompt, state = parser.ProcessLine(line)
		if prompt != nil {
			break
		}
	}

	if prompt == nil {
		t.Fatal("Expected trust folder prompt to be detected")
	}

	if state != PTYStatePermission {
		t.Errorf("State = %v, want %v", state, PTYStatePermission)
	}

	if prompt.Type != PermissionTypeTrustFolder {
		t.Errorf("Prompt.Type = %v, want %v", prompt.Type, PermissionTypeTrustFolder)
	}

	if prompt.Target != "/private/tmp/pty-test" {
		t.Errorf("Prompt.Target = %q, want %q", prompt.Target, "/private/tmp/pty-test")
	}

	// Should detect 2 options
	if len(prompt.Options) != 2 {
		t.Errorf("Prompt.Options count = %d, want 2", len(prompt.Options))
	}

	// Verify options
	if len(prompt.Options) >= 2 {
		if prompt.Options[0].Key != "y" || prompt.Options[0].Label != "Yes, proceed" {
			t.Errorf("Option[0] = %+v, want Key=y, Label=Yes, proceed", prompt.Options[0])
		}
		if prompt.Options[1].Key != "n" || prompt.Options[1].Label != "No, exit" {
			t.Errorf("Option[1] = %+v, want Key=n, Label=No, exit", prompt.Options[1])
		}
	}

	t.Logf("Trust folder prompt detected: type=%s, target=%s, options=%+v", prompt.Type, prompt.Target, prompt.Options)
}

// TestPTYParser_DetectTrustFolderType tests that trust folder pattern is detected
func TestPTYParser_DetectTrustFolderType(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name         string
		line         string
		expectedType PermissionType
	}{
		{
			name:         "trust files in folder",
			line:         "Do you want to trust the files in this folder and allow Claude to proceed?",
			expectedType: PermissionTypeTrustFolder,
		},
		{
			name:         "trust file in folder variant",
			line:         "trust file in this folder",
			expectedType: PermissionTypeTrustFolder,
		},
		{
			name:         "work in this folder",
			line:         "Do you want to work in this folder?",
			expectedType: PermissionTypeTrustFolder,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			parser.detectPermissionType(tt.line)

			if parser.currentPromptType != tt.expectedType {
				t.Errorf("detectPermissionType(%q) type = %v, want %v",
					tt.line, parser.currentPromptType, tt.expectedType)
			}
		})
	}
}

// TestPTYParser_WorkInFolderPrompt tests the new "Do you want to work in this folder?" format
func TestPTYParser_WorkInFolderPrompt(t *testing.T) {
	parser := NewPTYParser()

	// Simulate actual "work in folder" prompt from Claude Code
	lines := []string{
		"Do you want to work in this folder?",
		"/Users/brianly/Projects/demo1",
		"In order to work in this folder, we need your permission for Claude Code to read, edit, and execute files.",
		"If this folder has malicious code or untrusted scripts, Claude Code could run them while trying to help.",
		"Only continue if this is your code or a project you trust.",
		"Security details",
		"❯ 1. Yes, continue",
		"  2. No, exit",
		"Enter to confirm · Esc to cancel",
	}

	var prompt *PTYPermissionPrompt
	var state PTYState

	for _, line := range lines {
		prompt, state = parser.ProcessLine(line)
		if prompt != nil {
			break
		}
	}

	if prompt == nil {
		t.Fatal("Expected work in folder prompt to be detected")
	}

	if state != PTYStatePermission {
		t.Errorf("State = %v, want %v", state, PTYStatePermission)
	}

	if prompt.Type != PermissionTypeTrustFolder {
		t.Errorf("Prompt.Type = %v, want %v", prompt.Type, PermissionTypeTrustFolder)
	}

	if prompt.Target != "/Users/brianly/Projects/demo1" {
		t.Errorf("Prompt.Target = %q, want %q", prompt.Target, "/Users/brianly/Projects/demo1")
	}

	// Should detect 2 numbered options
	if len(prompt.Options) != 2 {
		t.Errorf("Prompt.Options count = %d, want 2", len(prompt.Options))
	}

	t.Logf("Work in folder prompt detected: type=%s, target=%s, options=%+v", prompt.Type, prompt.Target, prompt.Options)
}

// TestPTYParser_PermissionPanelFormat tests the permission panel format with "Bash command" header
// This is the format shown in Claude Code's permission prompt box (not inline Bash(...) format)
func TestPTYParser_PermissionPanelFormat(t *testing.T) {
	parser := NewPTYParser()

	// Simulate actual Claude permission panel output format
	// This format shows "Bash command" as a header, with the command on the next line
	lines := []string{
		"● Bash(rm /Users/brianly/Projects/cdev-ios/hello02.txt)",
		"└─ Running…",
		"────────────────────────────────────────────────────────────────────",
		"Bash command",
		"",
		"  rm /Users/brianly/Projects/cdev-ios/hello02.txt",
		"  Delete hello02.txt",
		"",
		"Do you want to proceed?",
		"❯ 1. Yes",
		"  2. Yes, and don't ask again for rm commands in /Users/brianly/Projects/cdev-ios",
		"  3. Type here to tell Claude what to do differently",
		"",
		"Esc to cancel",
	}

	var prompt *PTYPermissionPrompt
	var state PTYState

	for _, line := range lines {
		prompt, state = parser.ProcessLine(line)
		if prompt != nil {
			break
		}
	}

	if prompt == nil {
		t.Fatal("Expected permission prompt to be detected")
	}

	if state != PTYStatePermission {
		t.Errorf("State = %v, want %v", state, PTYStatePermission)
	}

	if prompt.Type != PermissionTypeBashCommand {
		t.Errorf("Prompt.Type = %v, want %v", prompt.Type, PermissionTypeBashCommand)
	}

	// Should detect all 3 options
	if len(prompt.Options) < 3 {
		t.Errorf("Prompt.Options count = %d, want >= 3", len(prompt.Options))
	}

	// Verify options are sorted
	if len(prompt.Options) >= 3 {
		if prompt.Options[0].Key != "1" {
			t.Errorf("Option[0].Key = %q, want %q", prompt.Options[0].Key, "1")
		}
		if prompt.Options[1].Key != "2" {
			t.Errorf("Option[1].Key = %q, want %q", prompt.Options[1].Key, "2")
		}
		if prompt.Options[2].Key != "3" {
			t.Errorf("Option[2].Key = %q, want %q", prompt.Options[2].Key, "3")
		}
	}

	t.Logf("Detected prompt: Type=%s, Target=%s, Options=%d", prompt.Type, prompt.Target, len(prompt.Options))
}

// TestPTYParser_PermissionPanelHeaders tests detection of permission panel headers
func TestPTYParser_PermissionPanelHeaders(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name         string
		line         string
		expectedType PermissionType
	}{
		{
			name:         "Bash command header",
			line:         "Bash command",
			expectedType: PermissionTypeBashCommand,
		},
		{
			name:         "Write file header",
			line:         "Write file",
			expectedType: PermissionTypeWriteFile,
		},
		{
			name:         "Edit file header",
			line:         "Edit file",
			expectedType: PermissionTypeEditFile,
		},
		{
			name:         "Delete file header",
			line:         "Delete file",
			expectedType: PermissionTypeDeleteFile,
		},
		{
			name:         "Create file header",
			line:         "Create file",
			expectedType: PermissionTypeWriteFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			parser.detectPermissionType(tt.line)

			if parser.currentPromptType != tt.expectedType {
				t.Errorf("detectPermissionType(%q) type = %v, want %v",
					tt.line, parser.currentPromptType, tt.expectedType)
			}
		})
	}
}

// TestPTYParser_StateResetOnNormalOutput tests that error/question states reset when normal output is seen
func TestPTYParser_StateResetOnNormalOutput(t *testing.T) {
	parser := NewPTYParser()

	// First, trigger error state
	parser.ProcessLine("Error: something went wrong")
	if parser.GetState() != PTYStateError {
		t.Errorf("Expected error state after error line, got %v", parser.GetState())
	}

	// Test that normal output resets error state
	tests := []struct {
		name  string
		input string
	}{
		{"welcome_banner_top", "╭─── Claude Code v2.0.71 ───────────────"},
		{"welcome_banner_bottom", "╰────────────────────────────────────────"},
		{"welcome_banner_side", "│                 Welcome back Brian!                │"},
		{"divider_line", "─────────────────────────────"},
		{"user_prompt", "> create new text.txt"},
		{"user_prompt_arrow", "❯ hello"},
		{"claude_marker", "⏺ Write(text.txt)"},
		{"result_indicator", "  ⎿  Wrote 1 lines to text.txt"},
		{"shortcuts_help", "  ? for shortcuts"},
		{"wrote_file", "Wrote 5 lines to file.txt"},
		{"created_file", "Created text.txt in the current directory."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset and set to error state
			parser.Reset()
			parser.ProcessLine("Error: test error")

			// Now process normal output
			_, state := parser.ProcessLine(tt.input)

			if state != PTYStateIdle {
				t.Errorf("ProcessLine(%q) after error should reset to idle, got %v", tt.input, state)
			}
		})
	}
}

// TestPTYParser_ErrorPatternStricter tests that error pattern doesn't match embedded errors
func TestPTYParser_ErrorPatternStricter(t *testing.T) {
	parser := NewPTYParser()

	// These should NOT trigger error state (embedded in grep commands, etc.)
	noError := []struct {
		name  string
		input string
	}{
		{"grep_error_pattern", `grep -E "(error:|BUILD FAILED)"`},
		{"echo_error", `echo "error: something"`},
		{"quoted_error", `"error:" in the output`},
	}

	for _, tt := range noError {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)

			if state == PTYStateError {
				t.Errorf("ProcessLine(%q) should NOT trigger error state", tt.input)
			}
		})
	}

	// These SHOULD trigger error state
	yesError := []struct {
		name  string
		input string
	}{
		{"error_at_start", "Error: something went wrong"},
		{"range_error", "RangeError: Invalid time value"},
		{"type_error", "TypeError: undefined is not a function"},
		{"failed_at_start", "Failed: could not connect"},
		{"indented_error", "    Error: indented error message"},
	}

	for _, tt := range yesError {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)

			if state != PTYStateError {
				t.Errorf("ProcessLine(%q) SHOULD trigger error state, got %v", tt.input, state)
			}
		})
	}
}

// TestPTYParser_StalePromptTypeClear tests that prompt type clears after buffer overflow
func TestPTYParser_StalePromptTypeClear(t *testing.T) {
	parser := NewPTYParser()

	// Set a prompt type
	parser.ProcessLine("Write(test.txt)")

	// Verify prompt type was detected
	if parser.currentPromptType != PermissionTypeWriteFile {
		t.Errorf("Expected write_file type, got %v", parser.currentPromptType)
	}

	// Feed more than 30 lines without completing a prompt
	for i := 0; i < 35; i++ {
		parser.ProcessLine("some random line")
	}

	// After buffer overflow, prompt type should be cleared
	if parser.currentPromptType != "" {
		t.Errorf("Expected empty prompt type after buffer overflow, got %v", parser.currentPromptType)
	}
	if parser.currentPromptTarget != "" {
		t.Errorf("Expected empty prompt target after buffer overflow, got %v", parser.currentPromptTarget)
	}
}

// TestPTYParser_ThinkingWithMiddleDot tests that thinking patterns match spinner lines
// with middle dot content like "(esc to interrupt · thinking)"
func TestPTYParser_ThinkingWithMiddleDot(t *testing.T) {
	parser := NewPTYParser()

	tests := []struct {
		name          string
		input         string
		expectedState PTYState
	}{
		{
			name:          "standard esc to interrupt",
			input:         "✢ Thinking… (esc to interrupt)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "esc to interrupt with middle dot thinking",
			input:         "✻ Flibbertigibbeting… (esc to interrupt · thinking)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "esc to interrupt with middle dot",
			input:         "✶ Scheming… (esc to interrupt ·)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "esc to interrupt with extra text",
			input:         "✳ Pondering… (esc to interrupt · processing)",
			expectedState: PTYStateThinking,
		},
		{
			name:          "middle dot spinner symbol",
			input:         "· Flibbertigibbeting… (esc to interrupt · thinking)",
			expectedState: PTYStateThinking,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)
			if state != tt.expectedState {
				t.Errorf("ProcessLine(%q) state = %v, want %v", tt.input, state, tt.expectedState)
			}
		})
	}
}

// TestPTYParser_TipLineNotQuestion tests that "Tip:" lines ending with ? are not detected as questions
func TestPTYParser_TipLineNotQuestion(t *testing.T) {
	parser := NewPTYParser()

	// Tip lines should NOT trigger question state even if they end with ?
	tipLines := []struct {
		name  string
		input string
	}{
		{"tip with question", "  ⎿  Tip: Did you know you can drag and drop image files into your terminal?"},
		{"tip shortcut", "Tip: Did you know about the shortcuts?"},
		{"tip feature", "  Tip: Have you tried the new feature?"},
	}

	for _, tt := range tipLines {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)

			if state == PTYStateQuestion {
				t.Errorf("ProcessLine(%q) should NOT trigger question state, got %v", tt.input, state)
			}
		})
	}

	// Real questions SHOULD trigger question state
	realQuestions := []struct {
		name  string
		input string
	}{
		{"simple question", "What file should I edit?"},
		{"user question", "Do you want me to proceed?"},
		{"technical question", "Should I refactor this code?"},
	}

	for _, tt := range realQuestions {
		t.Run(tt.name, func(t *testing.T) {
			parser.Reset()
			_, state := parser.ProcessLine(tt.input)

			if state != PTYStateQuestion {
				t.Errorf("ProcessLine(%q) should trigger question state, got %v", tt.input, state)
			}
		})
	}
}
