package claude

import (
	"testing"
)

func TestANSIParser_StripCodes(t *testing.T) {
	parser := NewANSIParser()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "Hello, World!",
			expected: "Hello, World!",
		},
		{
			name:     "simple color code",
			input:    "\x1b[32mGreen text\x1b[0m",
			expected: "Green text",
		},
		{
			name:     "bold text",
			input:    "\x1b[1mBold text\x1b[0m",
			expected: "Bold text",
		},
		{
			name:     "multiple colors",
			input:    "\x1b[31mRed\x1b[0m \x1b[32mGreen\x1b[0m \x1b[34mBlue\x1b[0m",
			expected: "Red Green Blue",
		},
		{
			name:     "256 color code",
			input:    "\x1b[38;5;196mBright red\x1b[0m",
			expected: "Bright red",
		},
		{
			name:     "true color RGB",
			input:    "\x1b[38;2;255;100;50mRGB color\x1b[0m",
			expected: "RGB color",
		},
		{
			name:     "cursor movement",
			input:    "\x1b[2K\x1b[1GClear line",
			expected: "Clear line",
		},
		{
			name:     "OSC sequence (title)",
			input:    "\x1b]0;Terminal Title\x07Some text",
			expected: "Some text",
		},
		{
			name:     "thinking indicator",
			input:    "\x1b[36m✢ Thinking...\x1b[0m",
			expected: "✢ Thinking...",
		},
		{
			name:     "permission prompt with colors",
			input:    "\x1b[1;33mWrite(test.txt)\x1b[0m Allow?",
			expected: "Write(test.txt) Allow?",
		},
		{
			name:     "option with unicode arrow",
			input:    "\x1b[32m❯\x1b[0m 1. Yes",
			expected: "❯ 1. Yes",
		},
		{
			name:     "control characters",
			input:    "Line with\x00null\x1fand control\x7fchars",
			expected: "Line withnulland controlchars",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only ANSI codes",
			input:    "\x1b[32m\x1b[0m",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.StripCodes(tt.input)
			if result != tt.expected {
				t.Errorf("StripCodes() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestANSIParser_ParseLine(t *testing.T) {
	parser := NewANSIParser()

	tests := []struct {
		name          string
		input         string
		expectedClean string
	}{
		{
			name:          "simple line",
			input:         "Hello, World!",
			expectedClean: "Hello, World!",
		},
		{
			name:          "colored line with trailing whitespace",
			input:         "\x1b[32mGreen text\x1b[0m   \n",
			expectedClean: "Green text",
		},
		{
			name:          "preserves leading indentation",
			input:         "    Indented text",
			expectedClean: "    Indented text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.ParseLine(tt.input)
			if result.RawText != tt.input {
				t.Errorf("ParseLine().RawText = %q, want %q", result.RawText, tt.input)
			}
			if result.CleanText != tt.expectedClean {
				t.Errorf("ParseLine().CleanText = %q, want %q", result.CleanText, tt.expectedClean)
			}
		})
	}
}

func TestANSIParser_IsEmptyLine(t *testing.T) {
	parser := NewANSIParser()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: true,
		},
		{
			name:     "whitespace only",
			input:    "   \t   ",
			expected: true,
		},
		{
			name:     "ANSI codes only",
			input:    "\x1b[32m\x1b[0m",
			expected: true,
		},
		{
			name:     "content with ANSI",
			input:    "\x1b[32mText\x1b[0m",
			expected: false,
		},
		{
			name:     "content with whitespace",
			input:    "  Text  ",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.IsEmptyLine(tt.input)
			if result != tt.expected {
				t.Errorf("IsEmptyLine(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestANSIParser_ContainsText(t *testing.T) {
	parser := NewANSIParser()

	tests := []struct {
		name     string
		line     string
		text     string
		expected bool
	}{
		{
			name:     "plain text match",
			line:     "Hello, World!",
			text:     "world",
			expected: true,
		},
		{
			name:     "ANSI colored text match",
			line:     "\x1b[32mHello, World!\x1b[0m",
			text:     "hello",
			expected: true,
		},
		{
			name:     "no match",
			line:     "Hello, World!",
			text:     "foo",
			expected: false,
		},
		{
			name:     "case insensitive",
			line:     "HELLO",
			text:     "hello",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.ContainsText(tt.line, tt.text)
			if result != tt.expected {
				t.Errorf("ContainsText(%q, %q) = %v, want %v", tt.line, tt.text, result, tt.expected)
			}
		})
	}
}
