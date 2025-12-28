//go:build windows

package live

import "fmt"

// supportedTerminals returns the list of supported terminals on Windows.
// Terminal content reading is not currently supported on Windows.
func supportedTerminals() []string {
	return []string{} // No supported terminals on Windows yet
}

// getTerminalContent reads terminal content on Windows.
// This feature is not currently supported on Windows.
func getTerminalContent(terminalApp string) (string, error) {
	return "", fmt.Errorf("terminal content reading is not supported on Windows")
}
