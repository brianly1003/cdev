// Package hooks manages Claude Code hooks integration for cdev.
// It handles installing, uninstalling, and checking hook status
// to capture real-time events from Claude running in any terminal/IDE.
package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

const (
	// CdevHooksDir is the directory for cdev hook scripts
	CdevHooksDir = ".cdev/hooks"
	// ForwardScriptName is the name of the hook forwarder script (fire-and-forget)
	ForwardScriptName = "forward.sh"
	// PermissionScriptName is the name of the blocking permission hook script
	PermissionScriptName = "permission.sh"
	// CdevMarker is used to identify cdev-managed hooks in settings.json
	CdevMarker = "_cdev_managed"
	// InstalledMarkerFile indicates hooks have been installed
	InstalledMarkerFile = ".cdev/hooks/.installed"
	// DefaultPermissionTimeout is how long to wait for mobile response (seconds)
	DefaultPermissionTimeout = 60
)

// Manager handles Claude Code hooks installation and management.
type Manager struct {
	homeDir       string
	cdevPort      int
	claudeSettings string
}

// NewManager creates a new hooks manager.
func NewManager(port int) *Manager {
	homeDir, _ := os.UserHomeDir()
	return &Manager{
		homeDir:        homeDir,
		cdevPort:       port,
		claudeSettings: filepath.Join(homeDir, ".claude", "settings.json"),
	}
}

// IsInstalled checks if cdev hooks are already installed.
func (m *Manager) IsInstalled() bool {
	markerPath := filepath.Join(m.homeDir, InstalledMarkerFile)
	_, err := os.Stat(markerPath)
	return err == nil
}

// Install installs cdev hooks for Claude Code.
// This creates the hook scripts and adds hooks to Claude's settings.json.
// - forward.sh: Fire-and-forget script for notifications (SessionStart, PostToolUse)
// - permission.sh: Blocking script for PreToolUse that enables mobile permission approval
func (m *Manager) Install() error {
	log.Info().Msg("Installing cdev hooks for Claude Code...")

	// 1. Create hooks directory
	hooksDir := filepath.Join(m.homeDir, CdevHooksDir)
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	// 2. Create forward.sh script (fire-and-forget for notifications)
	if err := m.createForwardScript(hooksDir); err != nil {
		return fmt.Errorf("failed to create forward script: %w", err)
	}

	// 3. Create permission.sh script (blocking for PreToolUse)
	if err := m.createPermissionScript(hooksDir); err != nil {
		return fmt.Errorf("failed to create permission script: %w", err)
	}

	// 4. Add hooks to Claude settings.json
	if err := m.addHooksToSettings(); err != nil {
		return fmt.Errorf("failed to add hooks to settings: %w", err)
	}

	// 5. Create installed marker
	markerPath := filepath.Join(m.homeDir, InstalledMarkerFile)
	if err := os.WriteFile(markerPath, []byte("installed"), 0644); err != nil {
		log.Warn().Err(err).Msg("failed to create installed marker")
	}

	log.Info().Msg("cdev hooks installed successfully")
	return nil
}

// Uninstall removes cdev hooks from Claude Code.
func (m *Manager) Uninstall() error {
	log.Info().Msg("Uninstalling cdev hooks...")

	// 1. Remove hooks from Claude settings.json
	if err := m.removeHooksFromSettings(); err != nil {
		log.Warn().Err(err).Msg("failed to remove hooks from settings")
	}

	// 2. Remove hooks directory
	hooksDir := filepath.Join(m.homeDir, CdevHooksDir)
	if err := os.RemoveAll(hooksDir); err != nil {
		log.Warn().Err(err).Msg("failed to remove hooks directory")
	}

	log.Info().Msg("cdev hooks uninstalled successfully")
	return nil
}

// createForwardScript creates the fire-and-forget hook forwarder script.
// Used for: SessionStart, Notification, PostToolUse (non-blocking notifications)
func (m *Manager) createForwardScript(hooksDir string) error {
	script := fmt.Sprintf(`#!/bin/bash
# cdev hook forwarder - forwards Claude events to cdev server (fire-and-forget)
# Used for: SessionStart, Notification, PostToolUse
# This script only forwards if cdev is running (silent fail otherwise)

HOOK_TYPE="$1"
CDEV_PORT=%d

# Read stdin (hook data from Claude)
INPUT=$(cat)

# Only forward if cdev is running
if curl -s --connect-timeout 0.5 "http://127.0.0.1:${CDEV_PORT}/health" > /dev/null 2>&1; then
    echo "$INPUT" | curl -s -X POST \
        -H "Content-Type: application/json" \
        "http://127.0.0.1:${CDEV_PORT}/api/hooks/${HOOK_TYPE}" \
        -d @- > /dev/null 2>&1
fi

# Always exit successfully so Claude doesn't error
exit 0
`, m.cdevPort)

	scriptPath := filepath.Join(hooksDir, ForwardScriptName)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	log.Debug().Str("path", scriptPath).Msg("created forward script")
	return nil
}

// createPermissionScript creates the blocking permission hook script.
// Used for: PreToolUse - enables mobile permission approval for external Claude sessions.
//
// Flow:
// 1. Claude calls this hook before executing a tool
// 2. Script calls cdev's permission/request RPC (blocking)
// 3. cdev checks pattern memory for stored "Allow for Session" decisions
// 4. If no match, forwards to iOS and waits for response (with timeout)
// 5. Returns decision to Claude: allow, deny, or ask (fallback to desktop)
func (m *Manager) createPermissionScript(hooksDir string) error {
	script := fmt.Sprintf(`#!/bin/bash
# cdev permission hook - enables mobile permission approval for external Claude sessions
# Used for: PreToolUse (blocking - waits for mobile response)
#
# Returns to Claude:
#   - {"hookSpecificOutput":{"permissionDecision":"allow"}} - allow the tool
#   - {"hookSpecificOutput":{"permissionDecision":"deny"}} - deny the tool
#   - {"hookSpecificOutput":{"permissionDecision":"ask"}} - fallback to desktop prompt
#   - (empty/error) - fallback to desktop prompt

CDEV_PORT=%d
TIMEOUT=%d

# Read stdin (hook data from Claude)
INPUT=$(cat)

# Check if cdev is running
if ! curl -s --connect-timeout 0.5 "http://127.0.0.1:${CDEV_PORT}/health" > /dev/null 2>&1; then
    # cdev not running - let Claude handle it (desktop prompt)
    exit 0
fi

# Call cdev's permission request endpoint (blocking)
# This will:
# 1. Check pattern memory for stored decisions
# 2. If no match, forward to iOS and wait for response
# 3. Return the decision
RESPONSE=$(echo "$INPUT" | curl -s -X POST \
    -H "Content-Type: application/json" \
    --max-time ${TIMEOUT} \
    "http://127.0.0.1:${CDEV_PORT}/api/hooks/permission-request" \
    -d @- 2>/dev/null)

# Check if we got a valid response
if [ -z "$RESPONSE" ]; then
    # No response (timeout or error) - let Claude handle it
    exit 0
fi

# Extract decision from response
# Response format: {"decision":"allow|deny","scope":"once|session",...}
DECISION=$(echo "$RESPONSE" | grep -o '"decision":"[^"]*"' | cut -d'"' -f4)

if [ -z "$DECISION" ]; then
    # Could not parse decision - let Claude handle it
    exit 0
fi

# Return decision to Claude in the expected format
if [ "$DECISION" = "allow" ]; then
    echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","permissionDecisionReason":"Approved by cdev"}}'
elif [ "$DECISION" = "deny" ]; then
    echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Denied by cdev"}}'
else
    # Unknown decision - let Claude handle it
    echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask"}}'
fi

exit 0
`, m.cdevPort, DefaultPermissionTimeout)

	scriptPath := filepath.Join(hooksDir, PermissionScriptName)
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return err
	}

	log.Debug().Str("path", scriptPath).Msg("created permission script")
	return nil
}

// addHooksToSettings adds cdev hooks to Claude's settings.json.
// Hook types:
// - SessionStart: Fire-and-forget notification when Claude session starts
// - Notification: Fire-and-forget notification for permission prompts (info only)
// - PreToolUse: BLOCKING - enables mobile permission approval via permission.sh
// - PostToolUse: Fire-and-forget notification when tool completes
func (m *Manager) addHooksToSettings() error {
	// Read existing settings
	settings, err := m.readClaudeSettings()
	if err != nil {
		// Create new settings if file doesn't exist
		settings = make(map[string]interface{})
	}

	// Get or create hooks section
	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		hooks = make(map[string]interface{})
	}

	// Script paths
	forwardScript := filepath.Join(m.homeDir, CdevHooksDir, ForwardScriptName)
	permissionScript := filepath.Join(m.homeDir, CdevHooksDir, PermissionScriptName)

	// Define cdev hooks
	// PreToolUse uses the blocking permission script for mobile approval
	// Other hooks use fire-and-forget forward script for notifications
	cdevHooks := map[string][]map[string]interface{}{
		"SessionStart": {{
			"matcher":  "*",
			CdevMarker: true,
			"hooks": []map[string]interface{}{{
				"type":    "command",
				"command": fmt.Sprintf("%s session", forwardScript),
			}},
		}},
		"Notification": {{
			"matcher":  "permission_prompt",
			CdevMarker: true,
			"hooks": []map[string]interface{}{{
				"type":    "command",
				"command": fmt.Sprintf("%s notification", forwardScript),
			}},
		}},
		"PreToolUse": {{
			"matcher":  "*",
			CdevMarker: true,
			"hooks": []map[string]interface{}{{
				"type":    "command",
				// Use blocking permission script - enables mobile approval
				"command": permissionScript,
			}},
		}},
		"PostToolUse": {{
			"matcher":  "*",
			CdevMarker: true,
			"hooks": []map[string]interface{}{{
				"type":    "command",
				"command": fmt.Sprintf("%s tool-end", forwardScript),
			}},
		}},
	}

	// Merge cdev hooks with existing hooks
	for hookType, cdevHookList := range cdevHooks {
		existing, ok := hooks[hookType].([]interface{})
		if !ok {
			existing = []interface{}{}
		}

		// Remove any existing cdev hooks first
		filtered := m.filterOutCdevHooks(existing)

		// Add new cdev hooks
		for _, h := range cdevHookList {
			filtered = append(filtered, h)
		}

		hooks[hookType] = filtered
	}

	settings["hooks"] = hooks

	// Write back
	return m.writeClaudeSettings(settings)
}

// removeHooksFromSettings removes cdev hooks from Claude's settings.json.
// Only removes hooks with _cdev_managed marker or .cdev/hooks/ in command path.
// Preserves all other hooks and settings.
func (m *Manager) removeHooksFromSettings() error {
	settings, err := m.readClaudeSettings()
	if err != nil {
		if os.IsNotExist(err) {
			// No settings file, nothing to remove
			return nil
		}
		return fmt.Errorf("failed to read settings: %w", err)
	}

	// Check if hooks section exists
	hooksRaw, exists := settings["hooks"]
	if !exists {
		return nil // No hooks section, nothing to remove
	}

	hooks, ok := hooksRaw.(map[string]interface{})
	if !ok {
		// hooks exists but is not a map (unusual but possible)
		// Don't touch it to avoid breaking things
		log.Warn().Msg("hooks section exists but is not a map, skipping removal")
		return nil
	}

	// Track if we made any changes
	modified := false

	// Remove cdev hooks from each hook type
	for hookType, hookList := range hooks {
		list, ok := hookList.([]interface{})
		if !ok {
			// Not an array, skip this hook type
			continue
		}

		originalLen := len(list)
		filtered := m.filterOutCdevHooks(list)

		if len(filtered) != originalLen {
			modified = true
		}

		if len(filtered) == 0 {
			delete(hooks, hookType)
		} else {
			hooks[hookType] = filtered
		}
	}

	// Only write if we made changes
	if !modified {
		log.Debug().Msg("no cdev hooks found to remove")
		return nil
	}

	// Remove hooks section if empty
	if len(hooks) == 0 {
		delete(settings, "hooks")
	} else {
		settings["hooks"] = hooks
	}

	return m.writeClaudeSettings(settings)
}

// RestoreBackup restores the backup settings file if it exists.
// Call this if something goes wrong after modification.
func (m *Manager) RestoreBackup() error {
	backupPath := m.claudeSettings + ".cdev-backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup file found at %s", backupPath)
	}

	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	// Validate backup is valid JSON
	var validation map[string]interface{}
	if err := json.Unmarshal(backupData, &validation); err != nil {
		return fmt.Errorf("backup file contains invalid JSON: %w", err)
	}

	if err := os.WriteFile(m.claudeSettings, backupData, 0644); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	log.Info().Str("backup", backupPath).Msg("restored settings from backup")
	return nil
}

// filterOutCdevHooks removes hooks that have the cdev marker.
func (m *Manager) filterOutCdevHooks(hooks []interface{}) []interface{} {
	var filtered []interface{}
	for _, h := range hooks {
		if hookMap, ok := h.(map[string]interface{}); ok {
			if _, isCdev := hookMap[CdevMarker]; !isCdev {
				// Also check if command contains cdev hooks path
				if !m.isCdevHook(hookMap) {
					filtered = append(filtered, h)
				}
			}
		} else {
			filtered = append(filtered, h)
		}
	}
	return filtered
}

// isCdevHook checks if a hook is a cdev hook by examining its command.
func (m *Manager) isCdevHook(hook map[string]interface{}) bool {
	// Check nested hooks array
	if hooksList, ok := hook["hooks"].([]interface{}); ok {
		for _, h := range hooksList {
			if hMap, ok := h.(map[string]interface{}); ok {
				if cmd, ok := hMap["command"].(string); ok {
					if strings.Contains(cmd, ".cdev/hooks/") {
						return true
					}
				}
			}
		}
	}
	return false
}

// readClaudeSettings reads Claude's settings.json.
func (m *Manager) readClaudeSettings() (map[string]interface{}, error) {
	data, err := os.ReadFile(m.claudeSettings)
	if err != nil {
		return nil, err
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// writeClaudeSettings writes Claude's settings.json safely.
// Uses atomic write (temp file + rename) to prevent corruption.
// Creates a backup before modification.
func (m *Manager) writeClaudeSettings(settings map[string]interface{}) error {
	// Ensure .claude directory exists
	claudeDir := filepath.Dir(m.claudeSettings)
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("failed to create claude directory: %w", err)
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Validate: ensure we can parse what we're about to write
	var validation map[string]interface{}
	if err := json.Unmarshal(data, &validation); err != nil {
		return fmt.Errorf("validation failed - generated invalid JSON: %w", err)
	}

	// Create backup of existing file (if it exists)
	if _, err := os.Stat(m.claudeSettings); err == nil {
		backupPath := m.claudeSettings + ".cdev-backup"
		existingData, err := os.ReadFile(m.claudeSettings)
		if err == nil {
			if err := os.WriteFile(backupPath, existingData, 0644); err != nil {
				log.Warn().Err(err).Str("backup", backupPath).Msg("failed to create backup")
			} else {
				log.Debug().Str("backup", backupPath).Msg("created settings backup")
			}
		}
	}

	// Atomic write: write to temp file, then rename
	tempPath := m.claudeSettings + ".tmp"
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Rename temp file to actual settings file (atomic on most filesystems)
	if err := os.Rename(tempPath, m.claudeSettings); err != nil {
		// Clean up temp file on failure
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	log.Debug().Str("path", m.claudeSettings).Msg("updated Claude settings")
	return nil
}

// Status returns the current hook installation status.
func (m *Manager) Status() string {
	if !m.IsInstalled() {
		return "not installed"
	}

	// Check if forward script exists
	scriptPath := filepath.Join(m.homeDir, CdevHooksDir, ForwardScriptName)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return "partially installed (missing forward script)"
	}

	// Check if hooks are in settings
	settings, err := m.readClaudeSettings()
	if err != nil {
		return "partially installed (cannot read settings)"
	}

	hooks, ok := settings["hooks"].(map[string]interface{})
	if !ok {
		return "partially installed (no hooks in settings)"
	}

	// Check for SessionStart hook
	if _, ok := hooks["SessionStart"]; !ok {
		return "partially installed (missing SessionStart hook)"
	}

	return "installed"
}
