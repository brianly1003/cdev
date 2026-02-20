// Package claude implements the Claude CLI process manager.
package claude

import (
	"regexp"
	"strconv"
	"strings"
)

// ANSIParser handles terminal escape code processing.
// It can strip ANSI codes for clean text display or extract formatting info.
type ANSIParser struct {
	// Pattern to match ANSI escape codes
	ansiPattern *regexp.Regexp
	// Pattern for common control sequences
	controlPattern *regexp.Regexp
	// Cursor forward pattern (CSI n C) - used to preserve spacing
	cursorForwardPattern *regexp.Regexp
}

// NewANSIParser creates a new ANSI parser.
func NewANSIParser() *ANSIParser {
	return &ANSIParser{
		// Matches ANSI escape sequences:
		// - CSI sequences: ESC[ followed by parameters and command
		// - OSC sequences: ESC] followed by text and BEL or ST
		// - Other escape sequences
		ansiPattern: regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[PX^_][^\x1b]*\x1b\\|\x1b[\(\)][AB012]|\x1b[>=]`),

		// Control characters that should be stripped
		controlPattern: regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1a\x1c-\x1f\x7f]`),
		// Cursor forward (CSI n C) to preserve spacing in logs
		// Example: ESC[4C means move cursor forward 4 cells
		cursorForwardPattern: regexp.MustCompile(`\x1b\[(\d*)C`),
	}
}

// StripCodes removes all ANSI escape codes and control characters from text.
func (p *ANSIParser) StripCodes(text string) string {
	// Replace cursor-forward sequences with spaces before stripping
	if p.cursorForwardPattern != nil {
		text = p.cursorForwardPattern.ReplaceAllStringFunc(text, func(match string) string {
			sub := p.cursorForwardPattern.FindStringSubmatch(match)
			count := 1
			if len(sub) > 1 && sub[1] != "" {
				if n, err := strconv.Atoi(sub[1]); err == nil && n > 0 {
					// Clamp to avoid pathological allocations
					if n > 200 {
						n = 200
					}
					count = n
				}
			}
			return strings.Repeat(" ", count)
		})
	}
	// Remove ANSI escape sequences
	result := p.ansiPattern.ReplaceAllString(text, "")
	// Remove control characters (except newline, carriage return, tab)
	result = p.controlPattern.ReplaceAllString(result, "")
	return result
}

// ParsedLine contains both raw and clean versions of a terminal line.
type ParsedLine struct {
	RawText   string `json:"raw_text"`   // Original text with ANSI codes
	CleanText string `json:"clean_text"` // Text with ANSI codes stripped
}

// ParseLine extracts clean text from a raw terminal line.
func (p *ANSIParser) ParseLine(raw string) *ParsedLine {
	clean := p.StripCodes(raw)
	// Trim trailing whitespace but preserve leading (for indentation)
	clean = strings.TrimRight(clean, " \t\r\n")
	return &ParsedLine{
		RawText:   raw,
		CleanText: clean,
	}
}

// IsEmptyLine returns true if the line contains only whitespace or control codes.
func (p *ANSIParser) IsEmptyLine(line string) bool {
	clean := p.StripCodes(line)
	return strings.TrimSpace(clean) == ""
}

// ContainsText checks if the line contains the given text (after stripping ANSI codes).
func (p *ANSIParser) ContainsText(line, text string) bool {
	clean := p.StripCodes(line)
	return strings.Contains(strings.ToLower(clean), strings.ToLower(text))
}
