//go:build darwin

package live

import (
	"fmt"
	"os/exec"
)

// supportedTerminals returns the list of supported terminals on macOS.
func supportedTerminals() []string {
	return []string{"Terminal", "iTerm2"}
}

// getTerminalContent reads terminal content using AppleScript on macOS.
func getTerminalContent(terminalApp string) (string, error) {
	var script string

	switch terminalApp {
	case "Terminal":
		script = `
			tell application "Terminal"
				if (count of windows) > 0 then
					return contents of selected tab of front window
				else
					return ""
				end if
			end tell
		`
	case "iTerm2", "iTerm":
		script = `
			tell application "iTerm2"
				if (count of windows) > 0 then
					tell current session of current window
						return contents
					end tell
				else
					return ""
				end if
			end tell
		`
	case "Warp":
		return "", fmt.Errorf("Warp does not support content reading via AppleScript")
	case "Code", "Cursor":
		return "", fmt.Errorf("%s terminal is not accessible via AppleScript, use IDE extension instead", terminalApp)
	default:
		// Try generic approach for unknown terminals
		script = fmt.Sprintf(`
			tell application "%s"
				if (count of windows) > 0 then
					return contents of front window
				else
					return ""
				end if
			end tell
		`, terminalApp)
	}

	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("AppleScript failed: %w (output: %s)", err, string(output))
	}

	return string(output), nil
}
