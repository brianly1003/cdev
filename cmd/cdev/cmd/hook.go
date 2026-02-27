// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/hooks"
	"github.com/brianly1003/cdev/internal/permission"
	"github.com/gorilla/websocket"
	"github.com/spf13/cobra"
)

var (
	hookServerURL string
	hookTimeout   int
)

// hookCmd is the parent command for hook subcommands.
var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Handle Claude Code hook events",
	Long: `Handle hook events from Claude Code.

These commands are designed to be called by Claude Code's hook system.
They read JSON from stdin and output JSON to stdout.

Configure in ~/.claude/settings.json:
  {
    "hooks": {
      "PreToolUse": [{
        "matcher": "*",
        "hooks": [{
          "type": "command",
          "command": "cdev hook permission-request",
          "timeout": 300
        }]
      }]
    }
  }

For more details, see: https://code.claude.com/docs/en/hooks`,
}

// hookInstallCmd installs Claude Code hooks for external session capture.
var hookInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Claude Code hooks for external session capture",
	Long: `Install hooks into Claude Code's settings to capture events from external sessions.

This command:
1. Creates a forwarder script in ~/.cdev/hooks/forward.sh
2. Adds hooks to ~/.claude/settings.json for SessionStart, Notification, PreToolUse, PostToolUse
3. Creates an installed marker at ~/.cdev/hooks/.installed

Once installed, cdev will receive real-time events from Claude running in VS Code, Cursor,
or any terminal - not just sessions started by cdev.

The hooks are designed to fail silently when cdev is not running, so they won't interfere
with normal Claude Code operation.

Examples:
  # Install hooks:
  cdev hook install

  # Check installation status:
  cdev hook status`,
	RunE: runHookInstall,
}

// hookUninstallCmd removes Claude Code hooks.
var hookUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove Claude Code hooks",
	Long: `Remove cdev hooks from Claude Code's settings.

This command:
1. Removes cdev hooks from ~/.claude/settings.json
2. Deletes ~/.cdev/hooks/ directory

Examples:
  cdev hook uninstall`,
	RunE: runHookUninstall,
}

// hookStatusCmd shows Claude Code hooks installation status.
var hookStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show Claude Code hooks installation status",
	Long: `Check whether Claude Code hooks are installed and working.

Examples:
  cdev hook status`,
	RunE: runHookStatus,
}

// hookPermissionRequestCmd handles permission request hooks.
var hookPermissionRequestCmd = &cobra.Command{
	Use:   "permission-request",
	Short: "Handle a permission request from Claude Code",
	Long: `Handle a PreToolUse hook event from Claude Code.

This command:
1. Reads the permission request JSON from stdin
2. Checks session memory for matching patterns
3. If no match, forwards the request to the cdev server
4. Waits for a response from the mobile app
5. Returns the decision JSON to stdout

The cdev server must be running for this command to work.
Use --server to specify a custom server URL (default: ws://127.0.0.1:16180).

Examples:
  # Called by Claude Code hook system:
  echo '{"tool_name":"Bash",...}' | cdev hook permission-request

  # With custom server:
  cdev hook permission-request --server ws://localhost:16180`,
	RunE: runHookPermissionRequest,
}

var hookPort int

func init() {
	// Add hook command to root
	rootCmd.AddCommand(hookCmd)

	// Add subcommands to hook
	hookCmd.AddCommand(hookInstallCmd)
	hookCmd.AddCommand(hookUninstallCmd)
	hookCmd.AddCommand(hookStatusCmd)
	hookCmd.AddCommand(hookPermissionRequestCmd)

	// Flags for install
	hookInstallCmd.Flags().IntVar(&hookPort, "port", 16180, "cdev server port for hook forwarding")

	// Flags for permission-request
	hookPermissionRequestCmd.Flags().StringVar(&hookServerURL, "server", "ws://127.0.0.1:16180", "cdev server URL (WebSocket)")
	hookPermissionRequestCmd.Flags().IntVar(&hookTimeout, "timeout", 300, "timeout in seconds waiting for response")
}

// runHookPermissionRequest handles the permission-request subcommand.
func runHookPermissionRequest(cmd *cobra.Command, args []string) error {
	// Read JSON from stdin
	input, err := readHookInput()
	if err != nil {
		return outputDenyError(fmt.Sprintf("Failed to read input: %v", err))
	}

	// Validate required fields
	if input.ToolName == "" {
		return outputDenyError("Missing tool_name in input")
	}
	if input.ToolUseID == "" {
		return outputDenyError("Missing tool_use_id in input")
	}

	// Send request to cdev server and wait for response
	response, err := sendPermissionRequest(input)
	if err != nil {
		// On error, default to asking user to allow manually
		return outputDenyError(fmt.Sprintf("Server error: %v", err))
	}

	// Output the response to Claude
	return outputHookResponse(response)
}

// readHookInput reads and parses the JSON input from stdin.
func readHookInput() (*permission.HookInput, error) {
	reader := bufio.NewReader(os.Stdin)
	var input bytes.Buffer

	// Read all input
	_, err := io.Copy(&input, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read stdin: %w", err)
	}

	// Parse JSON
	var hookInput permission.HookInput
	if err := json.Unmarshal(input.Bytes(), &hookInput); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	return &hookInput, nil
}

// sendPermissionRequest sends the permission request to the cdev server via WebSocket.
func sendPermissionRequest(input *permission.HookInput) (*permission.Response, error) {
	// Ensure WebSocket URL
	wsURL := hookServerURL
	if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	} else if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	}
	if !strings.HasSuffix(wsURL, "/ws") {
		wsURL = strings.TrimSuffix(wsURL, "/") + "/ws"
	}

	// Connect to WebSocket
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(hookTimeout)*time.Second)
	defer cancel()

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	// Create the RPC request
	rpcRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "permission/request",
		"params": map[string]interface{}{
			"session_id":  input.SessionID,
			"tool_name":   input.ToolName,
			"tool_input":  input.ToolInput,
			"tool_use_id": input.ToolUseID,
			"cwd":         input.Cwd,
		},
		"id": 1,
	}

	// Send request
	if err := conn.WriteJSON(rpcRequest); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(time.Duration(hookTimeout) * time.Second)); err != nil {
		return nil, fmt.Errorf("failed to set deadline: %w", err)
	}

	// Wait for response
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		// Parse RPC response
		var rpcResponse struct {
			JSONRPC string               `json:"jsonrpc"`
			ID      interface{}          `json:"id"`
			Result  *permission.Response `json:"result"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			// Also handle event messages (notifications)
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}

		if err := json.Unmarshal(message, &rpcResponse); err != nil {
			// Skip non-JSON messages
			continue
		}

		// Skip notifications/events (no id)
		if rpcResponse.ID == nil && rpcResponse.Method != "" {
			continue
		}

		// Check if this is our response (id = 1)
		if rpcResponse.ID != nil {
			idFloat, ok := rpcResponse.ID.(float64)
			if !ok || idFloat != 1 {
				continue // Not our response
			}

			if rpcResponse.Error != nil {
				return nil, fmt.Errorf("server error: %s", rpcResponse.Error.Message)
			}

			if rpcResponse.Result == nil {
				return nil, fmt.Errorf("empty response from server")
			}

			return rpcResponse.Result, nil
		}
	}
}

// outputHookResponse outputs the hook response JSON to stdout.
func outputHookResponse(response *permission.Response) error {
	reason := response.Message
	if reason == "" {
		if response.Decision == permission.DecisionAllow {
			reason = "Approved via cdev mobile app"
		} else {
			reason = "Denied via cdev mobile app"
		}
	}

	hookOutput := permission.HookOutput{
		HookSpecificOutput: permission.HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       string(response.Decision),
			PermissionDecisionReason: reason,
			UpdatedInput:             response.UpdatedInput,
		},
	}

	encoder := json.NewEncoder(os.Stdout)
	return encoder.Encode(hookOutput)
}

// outputDenyError outputs a deny response with an error message.
func outputDenyError(message string) error {
	hookOutput := permission.HookOutput{
		HookSpecificOutput: permission.HookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: message,
		},
	}

	encoder := json.NewEncoder(os.Stdout)
	if err := encoder.Encode(hookOutput); err != nil {
		return fmt.Errorf("failed to output error: %w", err)
	}
	return nil
}

// runHookInstall installs Claude Code hooks for external session capture.
func runHookInstall(cmd *cobra.Command, args []string) error {
	manager := hooks.NewManager(hookPort)

	if manager.IsInstalled() {
		fmt.Println("Claude Code hooks are already installed.")
		fmt.Printf("Status: %s\n", manager.Status())
		return nil
	}

	fmt.Println("Installing Claude Code hooks...")
	if err := manager.Install(); err != nil {
		return fmt.Errorf("failed to install hooks: %w", err)
	}

	fmt.Println("✓ Claude Code hooks installed successfully!")
	fmt.Println()
	fmt.Println("What this enables:")
	fmt.Println("  • Real-time event capture from Claude running in VS Code, Cursor, or terminal")
	fmt.Println("  • Session discovery when Claude starts (even outside cdev)")
	fmt.Println("  • Permission prompt notifications")
	fmt.Println("  • Tool execution tracking")
	fmt.Println()
	fmt.Println("The hooks will only forward events when cdev is running.")
	fmt.Println("To remove hooks: cdev hook uninstall")

	return nil
}

// runHookUninstall removes Claude Code hooks.
func runHookUninstall(cmd *cobra.Command, args []string) error {
	manager := hooks.NewManager(hookPort)

	if !manager.IsInstalled() {
		fmt.Println("Claude Code hooks are not installed.")
		return nil
	}

	fmt.Println("Removing Claude Code hooks...")
	if err := manager.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall hooks: %w", err)
	}

	fmt.Println("✓ Claude Code hooks removed successfully!")
	fmt.Println()
	fmt.Println("External Claude sessions will no longer be captured.")
	fmt.Println("To reinstall: cdev hook install")

	return nil
}

// runHookStatus shows the Claude Code hooks installation status.
func runHookStatus(cmd *cobra.Command, args []string) error {
	manager := hooks.NewManager(hookPort)

	status := manager.Status()
	installed := manager.IsInstalled()

	fmt.Println("Claude Code Hooks Status")
	fmt.Println("========================")
	fmt.Printf("Installed: %v\n", installed)
	fmt.Printf("Status:    %s\n", status)

	if installed {
		fmt.Println()
		fmt.Println("Hooks are forwarding events to: http://127.0.0.1:" + fmt.Sprint(hookPort) + "/api/hooks/")
		fmt.Println()
		fmt.Println("Active hooks:")
		fmt.Println("  • SessionStart  → /api/hooks/session")
		fmt.Println("  • Notification  → /api/hooks/permission (permission_prompt)")
		fmt.Println("  • PreToolUse    → /api/hooks/tool-start")
		fmt.Println("  • PostToolUse   → /api/hooks/tool-end")
	}

	return nil
}
