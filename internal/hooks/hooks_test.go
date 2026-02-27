package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHooksManager_PreservesOtherHooks(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	homeDir := tmpDir
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Create .claude directory
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create claude dir: %v", err)
	}

	// Create initial settings with existing hooks (simulating user's own hooks)
	initialSettings := map[string]interface{}{
		"enabledPlugins": map[string]interface{}{
			"some-plugin": true,
		},
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "my-custom-hook.sh",
						},
					},
				},
			},
			"SessionStart": []interface{}{
				map[string]interface{}{
					"matcher": "*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "notify-session-start.sh",
						},
					},
				},
			},
		},
		"someOtherSetting": "value",
	}

	initialData, _ := json.MarshalIndent(initialSettings, "", "  ")
	if err := os.WriteFile(settingsPath, initialData, 0644); err != nil {
		t.Fatalf("failed to write initial settings: %v", err)
	}

	// Create manager with custom paths
	manager := &Manager{
		homeDir:        homeDir,
		cdevPort:       16180,
		claudeSettings: settingsPath,
	}

	// Install hooks
	if err := manager.Install(); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Read settings after install
	afterInstall, err := manager.readClaudeSettings()
	if err != nil {
		t.Fatalf("failed to read settings after install: %v", err)
	}

	// Verify other settings are preserved
	if afterInstall["enabledPlugins"] == nil {
		t.Error("enabledPlugins was removed after install")
	}
	if afterInstall["someOtherSetting"] != "value" {
		t.Error("someOtherSetting was modified after install")
	}

	// Verify hooks section exists and has expected structure
	hooks, ok := afterInstall["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks section is not a map after install")
	}

	// Verify PreToolUse has both user's hook and cdev's hook
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		t.Fatal("PreToolUse is not an array")
	}

	// Count cdev hooks and user hooks in PreToolUse
	cdevHookCount := 0
	userHookCount := 0
	for _, h := range preToolUse {
		hookMap, ok := h.(map[string]interface{})
		if !ok {
			continue
		}
		if _, isCdev := hookMap["_cdev_managed"]; isCdev {
			cdevHookCount++
		} else {
			userHookCount++
		}
	}

	if cdevHookCount != 1 {
		t.Errorf("expected 1 cdev hook in PreToolUse, got %d", cdevHookCount)
	}
	if userHookCount != 1 {
		t.Errorf("expected 1 user hook in PreToolUse, got %d", userHookCount)
	}

	// Uninstall hooks
	if err := manager.Uninstall(); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	// Read settings after uninstall
	afterUninstall, err := manager.readClaudeSettings()
	if err != nil {
		t.Fatalf("failed to read settings after uninstall: %v", err)
	}

	// Verify other settings are still preserved
	if afterUninstall["enabledPlugins"] == nil {
		t.Error("enabledPlugins was removed after uninstall")
	}
	if afterUninstall["someOtherSetting"] != "value" {
		t.Error("someOtherSetting was modified after uninstall")
	}

	// Verify user's hooks are still there
	hooksAfter, ok := afterUninstall["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("hooks section was completely removed, should still have user's hooks")
	}

	// Verify PreToolUse still has user's hook
	preToolUseAfter, ok := hooksAfter["PreToolUse"].([]interface{})
	if !ok {
		t.Fatal("PreToolUse was removed after uninstall")
	}

	if len(preToolUseAfter) != 1 {
		t.Errorf("expected 1 hook in PreToolUse after uninstall, got %d", len(preToolUseAfter))
	}

	// Verify it's the user's hook, not cdev's
	userHook, ok := preToolUseAfter[0].(map[string]interface{})
	if !ok {
		t.Fatal("PreToolUse hook is not a map")
	}
	if _, isCdev := userHook["_cdev_managed"]; isCdev {
		t.Error("cdev hook was not removed from PreToolUse")
	}

	// Verify SessionStart still has user's hook
	sessionStartAfter, ok := hooksAfter["SessionStart"].([]interface{})
	if !ok {
		t.Fatal("SessionStart was removed after uninstall")
	}

	if len(sessionStartAfter) != 1 {
		t.Errorf("expected 1 hook in SessionStart after uninstall, got %d", len(sessionStartAfter))
	}

	t.Log("All user hooks and settings preserved correctly!")
}

func TestHooksManager_RemovesEmptyHooksSection(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	homeDir := tmpDir
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Create .claude directory
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create claude dir: %v", err)
	}

	// Create initial settings WITHOUT any hooks (only cdev will add hooks)
	initialSettings := map[string]interface{}{
		"enabledPlugins": map[string]interface{}{
			"some-plugin": true,
		},
	}

	initialData, _ := json.MarshalIndent(initialSettings, "", "  ")
	if err := os.WriteFile(settingsPath, initialData, 0644); err != nil {
		t.Fatalf("failed to write initial settings: %v", err)
	}

	// Create manager
	manager := &Manager{
		homeDir:        homeDir,
		cdevPort:       16180,
		claudeSettings: settingsPath,
	}

	// Install hooks
	if err := manager.Install(); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify hooks were added
	afterInstall, _ := manager.readClaudeSettings()
	if afterInstall["hooks"] == nil {
		t.Fatal("hooks section was not created")
	}

	// Uninstall hooks
	if err := manager.Uninstall(); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	// Read settings after uninstall
	afterUninstall, err := manager.readClaudeSettings()
	if err != nil {
		t.Fatalf("failed to read settings after uninstall: %v", err)
	}

	// Verify hooks section is completely removed (since it was empty)
	if afterUninstall["hooks"] != nil {
		t.Error("empty hooks section should have been removed after uninstall")
	}

	// Verify other settings are preserved
	if afterUninstall["enabledPlugins"] == nil {
		t.Error("enabledPlugins was removed")
	}

	t.Log("Empty hooks section correctly removed!")
}

func TestHooksManager_BackupCreated(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	homeDir := tmpDir
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")
	backupPath := settingsPath + ".cdev-backup"

	// Create .claude directory
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create claude dir: %v", err)
	}

	// Create initial settings
	initialSettings := map[string]interface{}{
		"originalSetting": "originalValue",
	}

	initialData, _ := json.MarshalIndent(initialSettings, "", "  ")
	if err := os.WriteFile(settingsPath, initialData, 0644); err != nil {
		t.Fatalf("failed to write initial settings: %v", err)
	}

	// Create manager
	manager := &Manager{
		homeDir:        homeDir,
		cdevPort:       16180,
		claudeSettings: settingsPath,
	}

	// Install hooks (this should create a backup)
	if err := manager.Install(); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Verify backup was created
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatal("backup file was not created")
	}

	// Verify backup contains original content
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}

	var backupSettings map[string]interface{}
	if err := json.Unmarshal(backupData, &backupSettings); err != nil {
		t.Fatalf("backup is not valid JSON: %v", err)
	}

	if backupSettings["originalSetting"] != "originalValue" {
		t.Error("backup does not contain original settings")
	}

	// Backup should NOT have hooks (that's the original state)
	if backupSettings["hooks"] != nil {
		t.Error("backup should not have hooks (original state)")
	}

	t.Log("Backup created correctly!")
}

func TestHooksManager_ValidJSON(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	homeDir := tmpDir
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Create .claude directory
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create claude dir: %v", err)
	}

	// Create complex initial settings
	initialSettings := map[string]interface{}{
		"enabledPlugins": map[string]interface{}{
			"plugin-a": true,
			"plugin-b": false,
		},
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "echo 'bash hook'",
						},
					},
				},
			},
		},
		"nested": map[string]interface{}{
			"deep": map[string]interface{}{
				"value": 123,
			},
		},
		"array": []interface{}{"a", "b", "c"},
	}

	initialData, _ := json.MarshalIndent(initialSettings, "", "  ")
	if err := os.WriteFile(settingsPath, initialData, 0644); err != nil {
		t.Fatalf("failed to write initial settings: %v", err)
	}

	// Create manager
	manager := &Manager{
		homeDir:        homeDir,
		cdevPort:       16180,
		claudeSettings: settingsPath,
	}

	// Install hooks
	if err := manager.Install(); err != nil {
		t.Fatalf("Install failed: %v", err)
	}

	// Read and validate JSON after install
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}

	var afterInstall map[string]interface{}
	if err := json.Unmarshal(data, &afterInstall); err != nil {
		t.Fatalf("settings.json is not valid JSON after install: %v", err)
	}

	// Uninstall hooks
	if err := manager.Uninstall(); err != nil {
		t.Fatalf("Uninstall failed: %v", err)
	}

	// Read and validate JSON after uninstall
	data, err = os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}

	var afterUninstall map[string]interface{}
	if err := json.Unmarshal(data, &afterUninstall); err != nil {
		t.Fatalf("settings.json is not valid JSON after uninstall: %v", err)
	}

	// Verify nested structures are preserved
	nested, ok := afterUninstall["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("nested structure was corrupted")
	}
	deep, ok := nested["deep"].(map[string]interface{})
	if !ok {
		t.Fatal("nested.deep structure was corrupted")
	}
	if deep["value"].(float64) != 123 {
		t.Error("nested.deep.value was corrupted")
	}

	// Verify array is preserved
	arr, ok := afterUninstall["array"].([]interface{})
	if !ok || len(arr) != 3 {
		t.Error("array was corrupted")
	}

	t.Log("JSON syntax maintained correctly!")
}

func TestHooksManager_IdempotentInstall(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	homeDir := tmpDir
	claudeDir := filepath.Join(tmpDir, ".claude")
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Create .claude directory
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("failed to create claude dir: %v", err)
	}

	// Create initial settings
	initialSettings := map[string]interface{}{
		"test": true,
	}

	initialData, _ := json.MarshalIndent(initialSettings, "", "  ")
	if err := os.WriteFile(settingsPath, initialData, 0644); err != nil {
		t.Fatalf("failed to write initial settings: %v", err)
	}

	// Create manager
	manager := &Manager{
		homeDir:        homeDir,
		cdevPort:       16180,
		claudeSettings: settingsPath,
	}

	// Install hooks multiple times
	for i := 0; i < 3; i++ {
		if err := manager.Install(); err != nil {
			t.Fatalf("Install %d failed: %v", i+1, err)
		}
	}

	// Read settings
	settings, err := manager.readClaudeSettings()
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}

	// Count cdev hooks in SessionStart - should be exactly 1
	hooks := settings["hooks"].(map[string]interface{})
	sessionStart := hooks["SessionStart"].([]interface{})

	cdevCount := 0
	for _, h := range sessionStart {
		hookMap := h.(map[string]interface{})
		if _, isCdev := hookMap["_cdev_managed"]; isCdev {
			cdevCount++
		}
	}

	if cdevCount != 1 {
		t.Errorf("expected 1 cdev hook after multiple installs, got %d", cdevCount)
	}

	t.Log("Idempotent install works correctly!")
}
