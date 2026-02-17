package permission

import (
	"path/filepath"
	"regexp"
	"strings"
)

var mcpToolNamePattern = regexp.MustCompile(`^mcp__([A-Za-z0-9._-]+)__([A-Za-z0-9._-]+)$`)

// GeneratePattern generates a pattern from a tool name and input.
// Patterns are used to match future similar permission requests.
//
// Examples:
//   - Bash(rm:*) for any rm command
//   - Write(*.py) for writing Python files
//   - Edit(/path/to/*.json) for editing JSON files in a directory
func GeneratePattern(toolName string, toolInput map[string]interface{}) string {
	switch toolName {
	case "Bash":
		return generateBashPattern(toolInput)
	case "Write":
		return generateFilePattern("Write", toolInput, "file_path")
	case "Edit":
		return generateFilePattern("Edit", toolInput, "file_path")
	case "Read":
		return generateFilePattern("Read", toolInput, "file_path")
	default:
		// Generic pattern for unknown tools
		return toolName + "(*)"
	}
}

// generateBashPattern generates a pattern for Bash commands.
func generateBashPattern(toolInput map[string]interface{}) string {
	cmd, ok := toolInput["command"].(string)
	if !ok || cmd == "" {
		return "Bash(*)"
	}

	// Extract the base command (first word)
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return "Bash(*)"
	}

	baseCmd := parts[0]

	// Handle common commands with wildcard patterns
	switch baseCmd {
	case "rm", "mkdir", "touch", "chmod", "chown":
		// File operation commands - pattern on the command
		return "Bash(" + baseCmd + ":*)"
	case "git":
		// Git commands - include subcommand
		if len(parts) >= 2 {
			return "Bash(git " + parts[1] + ":*)"
		}
		return "Bash(git:*)"
	case "npm", "yarn", "pnpm":
		// Package manager commands - include subcommand
		if len(parts) >= 2 {
			return "Bash(" + baseCmd + " " + parts[1] + ":*)"
		}
		return "Bash(" + baseCmd + ":*)"
	case "go":
		// Go commands - include subcommand
		if len(parts) >= 2 {
			return "Bash(go " + parts[1] + ":*)"
		}
		return "Bash(go:*)"
	case "python", "python3", "node", "ruby":
		// Script execution - generic pattern
		return "Bash(" + baseCmd + ":*)"
	default:
		// Default: just the base command with wildcard
		return "Bash(" + baseCmd + ":*)"
	}
}

// generateFilePattern generates a pattern for file operations.
func generateFilePattern(toolName string, toolInput map[string]interface{}, pathKey string) string {
	path, ok := toolInput[pathKey].(string)
	if !ok || path == "" {
		return toolName + "(*)"
	}

	// Get the file extension
	ext := filepath.Ext(path)
	if ext != "" {
		return toolName + "(*" + ext + ")"
	}

	// No extension - use directory pattern
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		return toolName + "(" + dir + "/*)"
	}

	return toolName + "(*)"
}

// MatchPattern checks if a permission request matches a stored pattern.
func MatchPattern(pattern, toolName string, toolInput map[string]interface{}) bool {
	// Parse the pattern
	if !strings.HasPrefix(pattern, toolName+"(") || !strings.HasSuffix(pattern, ")") {
		return false
	}

	// Extract pattern content
	patternContent := pattern[len(toolName)+1 : len(pattern)-1]
	if patternContent == "*" {
		return true // Matches everything for this tool
	}

	switch toolName {
	case "Bash":
		return matchBashPattern(patternContent, toolInput)
	case "Write", "Edit", "Read":
		return matchFilePattern(patternContent, toolInput)
	default:
		return patternContent == "*"
	}
}

// matchBashPattern matches a Bash command against a pattern.
func matchBashPattern(patternContent string, toolInput map[string]interface{}) bool {
	cmd, ok := toolInput["command"].(string)
	if !ok || cmd == "" {
		return false
	}

	// Handle patterns like "rm:*" or "git add:*"
	if strings.HasSuffix(patternContent, ":*") {
		prefix := strings.TrimSuffix(patternContent, ":*")
		cmdParts := strings.Fields(cmd)

		// Check if command starts with the pattern prefix
		prefixParts := strings.Fields(prefix)
		if len(cmdParts) < len(prefixParts) {
			return false
		}

		for i, p := range prefixParts {
			if cmdParts[i] != p {
				return false
			}
		}
		return true
	}

	// Exact match
	return cmd == patternContent
}

// matchFilePattern matches a file path against a pattern.
func matchFilePattern(patternContent string, toolInput map[string]interface{}) bool {
	path, ok := toolInput["file_path"].(string)
	if !ok || path == "" {
		return false
	}

	// Handle extension patterns like "*.py"
	if strings.HasPrefix(patternContent, "*") {
		ext := strings.TrimPrefix(patternContent, "*")
		return strings.HasSuffix(path, ext)
	}

	// Handle directory patterns like "/path/to/*"
	if strings.HasSuffix(patternContent, "/*") {
		dir := strings.TrimSuffix(patternContent, "/*")
		return strings.HasPrefix(filepath.Dir(path), dir)
	}

	// Glob pattern matching
	matched, _ := filepath.Match(patternContent, path)
	return matched
}

// GenerateReadableDescription generates a human-readable description of a permission request.
func GenerateReadableDescription(toolName string, toolInput map[string]interface{}) string {
	if serverName, toolID, isMCP := parseMCPToolName(toolName); isMCP {
		if target := extractMCPTarget(toolInput); target != "" {
			return "Use MCP " + serverName + "/" + toolID + ": " + target
		}
		return "Use MCP tool: " + serverName + "/" + toolID
	}

	switch toolName {
	case "Bash":
		if cmd, ok := toolInput["command"].(string); ok {
			desc, _ := toolInput["description"].(string)
			if desc != "" {
				return desc
			}
			// Truncate long commands
			if len(cmd) > 80 {
				return cmd[:77] + "..."
			}
			return cmd
		}
		return "Execute shell command"

	case "Write":
		if path, ok := toolInput["file_path"].(string); ok {
			return "Write to " + filepath.Base(path)
		}
		return "Write file"

	case "Edit":
		if path, ok := toolInput["file_path"].(string); ok {
			return "Edit " + filepath.Base(path)
		}
		return "Edit file"

	case "Read":
		if path, ok := toolInput["file_path"].(string); ok {
			return "Read " + filepath.Base(path)
		}
		return "Read file"

	default:
		return "Use " + toolName + " tool"
	}
}

// GeneratePermissionType maps tool names to permission event types.
func GeneratePermissionType(toolName string) string {
	if _, _, isMCP := parseMCPToolName(toolName); isMCP {
		return "mcp_tool"
	}

	switch toolName {
	case "Bash":
		return "bash_command"
	case "Write":
		return "write_file"
	case "Edit":
		return "edit_file"
	case "Read":
		return "read_file"
	default:
		return "tool_use"
	}
}

// ExtractTarget extracts the target (file path or command) from tool input.
func ExtractTarget(toolName string, toolInput map[string]interface{}) string {
	if serverName, toolID, isMCP := parseMCPToolName(toolName); isMCP {
		if target := extractMCPTarget(toolInput); target != "" {
			return target
		}
		return serverName + "/" + toolID
	}

	switch toolName {
	case "Bash":
		if cmd, ok := toolInput["command"].(string); ok {
			return cmd
		}
	case "Write", "Edit", "Read":
		if path, ok := toolInput["file_path"].(string); ok {
			return path
		}
	}
	return ""
}

// ExtractPreview extracts a preview (content snippet) from tool input.
func ExtractPreview(toolName string, toolInput map[string]interface{}) string {
	switch toolName {
	case "Write":
		if content, ok := toolInput["content"].(string); ok {
			// Return first few lines
			lines := strings.Split(content, "\n")
			preview := strings.Join(lines[:min(5, len(lines))], "\n")
			if len(lines) > 5 {
				preview += "\n..."
			}
			if len(preview) > 500 {
				preview = preview[:497] + "..."
			}
			return preview
		}
	case "Edit":
		if newStr, ok := toolInput["new_string"].(string); ok {
			if len(newStr) > 200 {
				return newStr[:197] + "..."
			}
			return newStr
		}
	}
	return ""
}

// SanitizeCommand sanitizes a command for display, removing sensitive info.
func SanitizeCommand(cmd string) string {
	// List of patterns to redact
	sensitivePatterns := []string{
		`(?i)(password|passwd|pwd)=\S+`,
		`(?i)(secret|token|key|api_key|apikey)=\S+`,
		`(?i)(authorization|auth):\s*\S+`,
	}

	result := cmd
	for _, pattern := range sensitivePatterns {
		re := regexp.MustCompile(pattern)
		result = re.ReplaceAllString(result, "[REDACTED]")
	}

	return result
}

func parseMCPToolName(toolName string) (serverName string, toolID string, ok bool) {
	matches := mcpToolNamePattern.FindStringSubmatch(strings.TrimSpace(toolName))
	if len(matches) != 3 {
		return "", "", false
	}
	return matches[1], matches[2], true
}

func extractMCPTarget(toolInput map[string]interface{}) string {
	if len(toolInput) == 0 {
		return ""
	}

	for _, key := range []string{"url", "path", "file_path", "selector", "command", "query"} {
		if raw, ok := toolInput[key]; ok {
			if text, ok := raw.(string); ok && strings.TrimSpace(text) != "" {
				return text
			}
		}
	}

	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
