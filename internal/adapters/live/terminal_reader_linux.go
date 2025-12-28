//go:build linux

package live

import "fmt"

// supportedTerminals returns the list of supported terminals on Linux.
// Terminal content reading is not currently supported on Linux.
func supportedTerminals() []string {
	return []string{} // No supported terminals on Linux yet
}

// getTerminalContent reads terminal content on Linux.
// This feature is not currently supported on Linux.
func getTerminalContent(terminalApp string) (string, error) {
	return "", fmt.Errorf("terminal content reading is not supported on Linux")
}
