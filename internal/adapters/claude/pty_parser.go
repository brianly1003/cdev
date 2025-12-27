// Package claude implements the Claude CLI process manager.
package claude

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/rs/zerolog/log"
)

// debugLog logs debug messages for PTY parser tracing
func debugLog(format string, args ...interface{}) {
	log.Debug().Msg(fmt.Sprintf(format, args...))
}

// PTYState represents the current state of PTY interaction.
type PTYState string

const (
	PTYStateIdle       PTYState = "idle"
	PTYStateThinking   PTYState = "thinking"
	PTYStatePermission PTYState = "permission"
	PTYStateQuestion   PTYState = "question"
	PTYStateError      PTYState = "error"
)

// PermissionType identifies what Claude wants to do.
type PermissionType string

const (
	PermissionTypeWriteFile   PermissionType = "write_file"
	PermissionTypeEditFile    PermissionType = "edit_file"
	PermissionTypeDeleteFile  PermissionType = "delete_file"
	PermissionTypeBashCommand PermissionType = "bash_command"
	PermissionTypeMCP         PermissionType = "mcp_tool"
	PermissionTypeTrustFolder PermissionType = "trust_folder"
	PermissionTypeUnknown     PermissionType = "unknown"
)

// PTYPermissionPrompt represents a detected permission request.
type PTYPermissionPrompt struct {
	Type        PermissionType `json:"type"`
	Target      string         `json:"target"`      // filename or command
	Description string         `json:"description"` // full description
	Preview     string         `json:"preview"`     // file content preview or command
	Options     []PromptOption `json:"options"`
}

// PromptOption represents a choice in a permission prompt.
type PromptOption struct {
	Key         string `json:"key"`                   // "1", "2", "n", etc.
	Label       string `json:"label"`                 // "Yes", "Yes to all", "No"
	Description string `json:"description,omitempty"` // optional extra info
	Selected    bool   `json:"selected"`              // true if this option has the cursor (❯)
}

// PTYParser detects and parses interactive prompts from terminal output.
type PTYParser struct {
	ansi   *ANSIParser
	state  PTYState
	buffer []string // Accumulates lines for multi-line prompt detection

	// Accumulated content for preview extraction
	previewBuffer strings.Builder

	// Detection patterns
	writeFilePattern       *regexp.Regexp
	createFilePattern      *regexp.Regexp
	editFilePattern        *regexp.Regexp
	deleteFilePattern      *regexp.Regexp
	bashCommandPattern     *regexp.Regexp
	mcpToolPattern         *regexp.Regexp
	trustFolderPattern     *regexp.Regexp // Pattern for folder trust prompt
	allowPattern           *regexp.Regexp
	optionPattern          *regexp.Regexp
	textOptionPattern      *regexp.Regexp // Pattern for non-numbered options (Yes, proceed / No, exit)
	promptEndPattern       *regexp.Regexp // Pattern to detect end of permission prompt
	thinkingPatterns       []*regexp.Regexp
	errorPattern           *regexp.Regexp
	questionPattern        *regexp.Regexp
	permissionPanelPattern *regexp.Regexp // Pattern for permission panel headers

	// Current detected prompt info
	currentPromptType   PermissionType
	currentPromptTarget string
}

// NewPTYParser creates a new PTY parser.
func NewPTYParser() *PTYParser {
	return &PTYParser{
		ansi:  NewANSIParser(),
		state: PTYStateIdle,

		// Pattern: "Write(hello.txt)" or "Write (hello.txt)"
		writeFilePattern: regexp.MustCompile(`(?i)Write\s*\(([^\)]+)\)`),

		// Pattern: "Create file hello.txt" or "Create hello.txt"
		createFilePattern: regexp.MustCompile(`(?i)Create\s+(?:file\s+)?([^\s\n]+\.[a-zA-Z0-9]+)`),

		// Pattern: "Edit(hello.txt)" or "Edit file hello.txt"
		// Requires word boundary or file extension to avoid matching "edits" in other text
		editFilePattern: regexp.MustCompile(`(?i)\bEdit\s*\(([^\)]+)\)|Edit\s+(?:file\s+)?(\S+\.\w+)`),

		// Pattern: "Delete hello.txt" or "Remove file"
		deleteFilePattern: regexp.MustCompile(`(?i)(?:Delete|Remove)\s+(?:file\s+)?([^\s\n]+)`),

		// Pattern: "Bash(cmd)" or "Run command: cmd" - must start at line beginning to avoid false positives
		bashCommandPattern: regexp.MustCompile(`(?i)(?:^Bash\s*\(([^\)]+)\)|^Run(?:ning)?\s+(?:command)?:?\s*(.+)|^Bash\s+command\s*$)`),

		// Pattern: "mcp__server__tool" or "MCP tool"
		mcpToolPattern: regexp.MustCompile(`(?i)(?:mcp__\w+__\w+|MCP\s+tool)`),

		// Pattern: "trust the files in this folder" or "work in this folder" for folder trust prompts
		trustFolderPattern: regexp.MustCompile(`(?i)(?:trust\s+(?:the\s+)?files?\s+in\s+this\s+folder|Do you want to work in this folder)`),

		// Pattern: "Allow?" or "Do you want to" or numbered options
		allowPattern: regexp.MustCompile(`(?i)(Do you want to|Allow\?|Enter your choice)`),

		// Pattern: "1. Yes" or "❯ 1. Yes" or "n. No" - handles leading whitespace
		// Uses (?m) for multiline matching so ^ matches start of each line in buffer
		// Captures: [1]=cursor (❯ or >), [2]=key (1-9 or y/n), [3]=label
		optionPattern: regexp.MustCompile(`(?m)^\s*([❯>])?\s*([1-9]|[yn])\.\s+(.+)`),

		// Pattern for non-numbered options like "Yes, proceed" / "No, exit" in trust folder prompts
		// Matches: "❯ Yes, proceed" or "No, exit" with optional leading whitespace
		// Captures: [1]=cursor (❯ or >), [2]=label
		textOptionPattern: regexp.MustCompile(`(?m)^\s*([❯>])?\s*(Yes,?\s+proceed|No,?\s+exit)`),

		// Pattern to detect end of permission prompt (signals we can extract all options)
		// Matches "Esc to cancel" or similar closing lines
		promptEndPattern: regexp.MustCompile(`(?i)^\s*(?:Esc(?:ape)?\s+to\s+cancel|press\s+\d+|enter\s+to\s+confirm)`),

		// Thinking patterns - matches Claude's various "processing" indicators
		thinkingPatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)(?:Thinking|Scheming|Cooking|Analyzing|Processing|Considering|Baking)…?\.{0,3}`),
			regexp.MustCompile(`(?i)(?:✢|✽|✻|✶|✳|·)\s*(?:Thinking|Scheming|Cooking|Considering|Baking)`),
		},

		// Error pattern
		errorPattern: regexp.MustCompile(`(?i)(?:Error|Failed|Exception|Cannot|Unable to):\s*(.+)`),

		// Question pattern (ends with ?)
		questionPattern: regexp.MustCompile(`\?\s*$`),

		// Permission panel headers (these appear in the permission prompt box at bottom)
		// Matches: "Bash command", "Write file", "Edit file", "Read file", etc.
		permissionPanelPattern: regexp.MustCompile(`(?i)^(Bash|Write|Edit|Read|Delete|Create)\s+(command|file)\s*$`),
	}
}

// ProcessLine processes a single line of PTY output.
// Returns a permission prompt if one is detected, nil otherwise.
func (p *PTYParser) ProcessLine(rawLine string) (*PTYPermissionPrompt, PTYState) {
	cleanLine := p.ansi.StripCodes(rawLine)
	cleanLine = strings.TrimSpace(cleanLine)

	// Skip empty lines
	if cleanLine == "" {
		return nil, p.state
	}

	// Check for thinking state
	for _, pattern := range p.thinkingPatterns {
		if pattern.MatchString(cleanLine) {
			p.state = PTYStateThinking
			p.buffer = nil // Clear buffer for next round
			return nil, p.state
		}
	}

	// Check for error state
	if p.errorPattern.MatchString(cleanLine) {
		p.state = PTYStateError
		return nil, p.state
	}

	// Detect permission type from line
	p.detectPermissionType(cleanLine)

	// Add to buffer for multi-line detection
	p.buffer = append(p.buffer, cleanLine)

	// Keep buffer at reasonable size
	if len(p.buffer) > 30 {
		p.buffer = p.buffer[len(p.buffer)-30:]
	}

	// Only try to detect permission prompt if NOT already in permission state
	// This prevents re-detection when user navigates options with up/down arrows
	if p.state != PTYStatePermission {
		prompt := p.detectPermissionFromBuffer()
		if prompt != nil {
			p.state = PTYStatePermission
			p.buffer = nil // Clear buffer after detection
			return prompt, p.state
		}
	}

	// Check if this looks like a question (ends with ?)
	if p.questionPattern.MatchString(cleanLine) && !p.allowPattern.MatchString(cleanLine) {
		p.state = PTYStateQuestion
	}

	return nil, p.state
}

// detectPermissionType extracts permission type from a line.
func (p *PTYParser) detectPermissionType(line string) {
	// Check for permission panel headers first (these are standalone headers like "Bash command", "Edit file")
	if matches := p.permissionPanelPattern.FindStringSubmatch(line); len(matches) > 2 {
		toolType := strings.ToLower(matches[1])
		switch toolType {
		case "bash":
			p.currentPromptType = PermissionTypeBashCommand
		case "write", "create":
			p.currentPromptType = PermissionTypeWriteFile
		case "edit":
			p.currentPromptType = PermissionTypeEditFile
		case "delete":
			p.currentPromptType = PermissionTypeDeleteFile
		case "read":
			p.currentPromptType = PermissionTypeUnknown // Read doesn't usually need permission
		}
		p.currentPromptTarget = ""
		return
	}

	// Extract folder path for trust folder prompts (paths start with /)
	if p.currentPromptType == PermissionTypeTrustFolder && p.currentPromptTarget == "" && strings.HasPrefix(line, "/") {
		p.currentPromptTarget = strings.TrimSpace(line)
		return
	}

	// Don't override existing permission type
	if p.currentPromptType != "" {
		return
	}

	// Check trust folder first (before patterns that might false-positive)
	if p.trustFolderPattern.MatchString(line) {
		p.currentPromptType = PermissionTypeTrustFolder
		return
	}

	if matches := p.writeFilePattern.FindStringSubmatch(line); len(matches) > 1 {
		p.currentPromptType = PermissionTypeWriteFile
		p.currentPromptTarget = strings.TrimSpace(matches[1])
		return
	}
	if matches := p.createFilePattern.FindStringSubmatch(line); len(matches) > 1 {
		p.currentPromptType = PermissionTypeWriteFile
		p.currentPromptTarget = strings.TrimSpace(matches[1])
		return
	}

	if matches := p.editFilePattern.FindStringSubmatch(line); len(matches) > 1 {
		p.currentPromptType = PermissionTypeEditFile
		if matches[1] != "" {
			p.currentPromptTarget = strings.TrimSpace(matches[1])
		} else if len(matches) > 2 && matches[2] != "" {
			p.currentPromptTarget = strings.TrimSpace(matches[2])
		}
		return
	}

	if matches := p.deleteFilePattern.FindStringSubmatch(line); len(matches) > 1 {
		p.currentPromptType = PermissionTypeDeleteFile
		p.currentPromptTarget = strings.TrimSpace(matches[1])
		return
	}

	if matches := p.bashCommandPattern.FindStringSubmatch(line); len(matches) > 1 {
		if matches[1] != "" {
			p.currentPromptType = PermissionTypeBashCommand
			p.currentPromptTarget = strings.TrimSpace(matches[1])
			return
		} else if len(matches) > 2 && matches[2] != "" {
			p.currentPromptType = PermissionTypeBashCommand
			p.currentPromptTarget = strings.TrimSpace(matches[2])
			return
		}
	}

	if p.mcpToolPattern.MatchString(line) {
		p.currentPromptType = PermissionTypeMCP
		return
	}
}

// detectPermissionFromBuffer analyzes the buffer to detect a complete permission prompt.
func (p *PTYParser) detectPermissionFromBuffer() *PTYPermissionPrompt {
	if len(p.buffer) < 2 {
		return nil
	}

	bufferText := strings.Join(p.buffer, "\n")

	hasAllowPattern := p.allowPattern.MatchString(bufferText)
	hasNumberedOptions := p.hasOptions(bufferText)
	hasTextOptions := p.hasTextOptions(bufferText)
	hasAnyOptions := hasNumberedOptions || hasTextOptions

	if !hasAllowPattern && !hasAnyOptions {
		return nil
	}

	hasPromptEnd := p.promptEndPattern.MatchString(bufferText)
	numberedOptionCount := len(p.optionPattern.FindAllString(bufferText, -1))
	textOptionCount := len(p.textOptionPattern.FindAllString(bufferText, -1))

	if hasAllowPattern || hasAnyOptions {
		debugLog("PTY parser: allowPattern=%v, numberedOpts=%d, textOpts=%d, hasPromptEnd=%v, bufferLen=%d, type=%s, target=%s",
			hasAllowPattern, numberedOptionCount, textOptionCount, hasPromptEnd, len(p.buffer), p.currentPromptType, p.currentPromptTarget)
	}

	isTrustFolder := p.currentPromptType == PermissionTypeTrustFolder || p.trustFolderPattern.MatchString(bufferText)

	// Trigger conditions:
	// - End marker (Esc to cancel), OR
	// - Trust folder: 2+ options (text or numbered), OR
	// - Regular: 3+ numbered options AND allow pattern
	if !hasPromptEnd {
		if isTrustFolder {
			if textOptionCount < 2 && numberedOptionCount < 2 {
				return nil
			}
		} else {
			if numberedOptionCount < 3 || !hasAllowPattern {
				return nil
			}
		}
	}

	prompt := &PTYPermissionPrompt{
		Type:    p.currentPromptType,
		Target:  p.currentPromptTarget,
		Options: []PromptOption{},
	}

	// Clear target if it contains code-like patterns
	if prompt.Target != "" && strings.ContainsAny(prompt.Target, "{}[]()=|&;") {
		prompt.Target = ""
	}

	p.currentPromptType = ""
	p.currentPromptTarget = ""

	if isTrustFolder && prompt.Type == "" {
		prompt.Type = PermissionTypeTrustFolder
	}
	if prompt.Type == "" {
		prompt.Type = PermissionTypeUnknown
	}

	// Generate description based on type
	switch prompt.Type {
	case PermissionTypeWriteFile:
		prompt.Description = "Claude wants to create a file"
	case PermissionTypeEditFile:
		prompt.Description = "Claude wants to edit a file"
	case PermissionTypeDeleteFile:
		prompt.Description = "Claude wants to delete a file"
	case PermissionTypeBashCommand:
		prompt.Description = "Claude wants to run a command"
	case PermissionTypeMCP:
		prompt.Description = "Claude wants to use an MCP tool"
	case PermissionTypeTrustFolder:
		prompt.Description = "Claude needs to trust this folder"
	default:
		prompt.Description = "Claude needs your permission"
	}

	// Extract preview content (lines between target and options)
	prompt.Preview = p.extractPreview()

	// Extract numbered options from entire buffer text (not line-by-line)
	// This handles screen redraws where options may not be on separate lines
	// Pattern captures: [1]=cursor (❯ or >), [2]=key (1-9 or y/n), [3]=label
	seenOptions := make(map[string]bool) // Deduplicate options by key
	allMatches := p.optionPattern.FindAllStringSubmatch(bufferText, -1)
	for _, matches := range allMatches {
		if len(matches) > 3 {
			cursor := strings.TrimSpace(matches[1])
			key := strings.TrimSpace(matches[2])
			label := strings.TrimSpace(matches[3])
			// Skip duplicates - but update Selected if this match has cursor
			if seenOptions[key] {
				// Update existing option's Selected status if cursor is found
				if cursor == "❯" || cursor == ">" {
					for i := range prompt.Options {
						if prompt.Options[i].Key == key {
							prompt.Options[i].Selected = true
							break
						}
					}
				}
				continue
			}
			seenOptions[key] = true
			opt := PromptOption{
				Key:      key,
				Label:    label,
				Selected: cursor == "❯" || cursor == ">",
			}
			// Add description for special options
			if opt.Key == "2" && strings.Contains(strings.ToLower(opt.Label), "all") {
				opt.Description = "Allow all similar actions this session"
			}
			prompt.Options = append(prompt.Options, opt)
		}
	}

	// Extract text-based options (for trust folder prompts)
	// Pattern captures: [1]=cursor (❯ or >), [2]=label
	if hasTextOptions {
		textMatches := p.textOptionPattern.FindAllStringSubmatch(bufferText, -1)
		for _, matches := range textMatches {
			if len(matches) > 2 {
				cursor := strings.TrimSpace(matches[1])
				optLabel := strings.TrimSpace(matches[2])
				// Determine key based on option text
				var key string
				if strings.HasPrefix(strings.ToLower(optLabel), "yes") {
					key = "y"
				} else if strings.HasPrefix(strings.ToLower(optLabel), "no") {
					key = "n"
				} else {
					key = "1" // Default fallback
				}
				// Skip duplicates - but update Selected if this match has cursor
				if seenOptions[key] {
					if cursor == "❯" || cursor == ">" {
						for i := range prompt.Options {
							if prompt.Options[i].Key == key {
								prompt.Options[i].Selected = true
								break
							}
						}
					}
					continue
				}
				seenOptions[key] = true
				opt := PromptOption{
					Key:      key,
					Label:    optLabel,
					Selected: cursor == "❯" || cursor == ">",
				}
				prompt.Options = append(prompt.Options, opt)
			}
		}
	}

	// Only return if we found at least one option
	if len(prompt.Options) == 0 {
		return nil
	}

	// Sort options by key (1, 2, 3, ... then y before n for text options)
	sort.Slice(prompt.Options, func(i, j int) bool {
		ki, kj := prompt.Options[i].Key, prompt.Options[j].Key
		// Numbered options (1-9) come before letter options (y, n)
		iIsNum := ki >= "1" && ki <= "9"
		jIsNum := kj >= "1" && kj <= "9"
		if iIsNum && !jIsNum {
			return true
		}
		if !iIsNum && jIsNum {
			return false
		}
		// For numbered options, sort numerically
		if iIsNum && jIsNum {
			return ki < kj
		}
		// For letter options, "y" (yes) comes before "n" (no) for better UX
		if ki == "y" && kj == "n" {
			return true
		}
		if ki == "n" && kj == "y" {
			return false
		}
		return ki < kj
	})

	return prompt
}

// hasOptions checks if the buffer contains numbered options.
func (p *PTYParser) hasOptions(text string) bool {
	return p.optionPattern.MatchString(text)
}

// hasTextOptions checks if the buffer contains text-based options (Yes, proceed / No, exit).
func (p *PTYParser) hasTextOptions(text string) bool {
	return p.textOptionPattern.MatchString(text)
}

// extractPreview extracts content preview from buffer.
func (p *PTYParser) extractPreview() string {
	var previewLines []string
	inPreview := false

	for _, line := range p.buffer {
		// Start preview after seeing the target
		if strings.Contains(line, p.currentPromptTarget) {
			inPreview = true
			continue
		}

		// End preview when we hit options or Allow?
		if inPreview {
			if p.optionPattern.MatchString(line) || p.allowPattern.MatchString(line) {
				break
			}
			// Skip separator lines
			if strings.Trim(line, "─╌-=") == "" {
				continue
			}
			previewLines = append(previewLines, line)
		}
	}

	// Limit preview size
	if len(previewLines) > 20 {
		previewLines = previewLines[:20]
		previewLines = append(previewLines, "...")
	}

	return strings.Join(previewLines, "\n")
}

// Reset clears the parser state.
func (p *PTYParser) Reset() {
	p.state = PTYStateIdle
	p.buffer = nil
	p.currentPromptType = ""
	p.currentPromptTarget = ""
	p.previewBuffer.Reset()
}

// GetState returns the current PTY state.
func (p *PTYParser) GetState() PTYState {
	return p.state
}

// SetState sets the PTY state.
func (p *PTYParser) SetState(state PTYState) {
	p.state = state
}

// IsWaitingForInput returns true if in a state that requires user input.
func (p *PTYParser) IsWaitingForInput() bool {
	return p.state == PTYStatePermission || p.state == PTYStateQuestion
}
