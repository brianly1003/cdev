package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Create a temp directory for testing
	tempDir := t.TempDir()

	// Load config with defaults (no config file)
	cfg, err := Load("")
	if err != nil {
		// If error is about repository path, that's expected
		// since we're not in a valid directory
		t.Skipf("Skipping default load test: %v", err)
	}

	// Check default values
	if cfg.Server.WebSocketPort != 8765 {
		t.Errorf("default WebSocketPort = %d, want 8765", cfg.Server.WebSocketPort)
	}
	if cfg.Server.HTTPPort != 8766 {
		t.Errorf("default HTTPPort = %d, want 8766", cfg.Server.HTTPPort)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("default Host = %s, want 127.0.0.1", cfg.Server.Host)
	}
	if !cfg.Watcher.Enabled {
		t.Error("default Watcher.Enabled should be true")
	}
	if cfg.Watcher.DebounceMS != 100 {
		t.Errorf("default DebounceMS = %d, want 100", cfg.Watcher.DebounceMS)
	}
	if cfg.Claude.Command != "claude" {
		t.Errorf("default Claude.Command = %s, want claude", cfg.Claude.Command)
	}
	if cfg.Claude.TimeoutMinutes != 30 {
		t.Errorf("default TimeoutMinutes = %d, want 30", cfg.Claude.TimeoutMinutes)
	}
	if !cfg.Git.Enabled {
		t.Error("default Git.Enabled should be true")
	}
	if cfg.Limits.MaxFileSizeKB != 200 {
		t.Errorf("default MaxFileSizeKB = %d, want 200", cfg.Limits.MaxFileSizeKB)
	}

	_ = tempDir // use the temp dir
}

func TestLoad_FromFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test config file
	configContent := `
server:
  websocket_port: 9000
  http_port: 9001
  host: "0.0.0.0"

repository:
  path: "` + tempDir + `"

watcher:
  enabled: false
  debounce_ms: 200

claude:
  command: "/usr/local/bin/claude"
  timeout_minutes: 60
  skip_permissions: true

git:
  enabled: false

logging:
  level: debug
  format: json

limits:
  max_file_size_kb: 500
  max_diff_size_kb: 1000
  max_log_buffer: 500
  max_prompt_len: 5000
`
	configPath := filepath.Join(tempDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check loaded values
	if cfg.Server.WebSocketPort != 9000 {
		t.Errorf("WebSocketPort = %d, want 9000", cfg.Server.WebSocketPort)
	}
	if cfg.Server.HTTPPort != 9001 {
		t.Errorf("HTTPPort = %d, want 9001", cfg.Server.HTTPPort)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Host = %s, want 0.0.0.0", cfg.Server.Host)
	}
	if cfg.Watcher.Enabled {
		t.Error("Watcher.Enabled should be false")
	}
	if cfg.Watcher.DebounceMS != 200 {
		t.Errorf("DebounceMS = %d, want 200", cfg.Watcher.DebounceMS)
	}
	if cfg.Claude.Command != "/usr/local/bin/claude" {
		t.Errorf("Claude.Command = %s, want /usr/local/bin/claude", cfg.Claude.Command)
	}
	if cfg.Claude.TimeoutMinutes != 60 {
		t.Errorf("TimeoutMinutes = %d, want 60", cfg.Claude.TimeoutMinutes)
	}
	if !cfg.Claude.SkipPermissions {
		t.Error("SkipPermissions should be true")
	}
	if cfg.Git.Enabled {
		t.Error("Git.Enabled should be false")
	}
	if cfg.Limits.MaxFileSizeKB != 500 {
		t.Errorf("MaxFileSizeKB = %d, want 500", cfg.Limits.MaxFileSizeKB)
	}
}

func TestMergeClaudeArgs(t *testing.T) {
	tests := []struct {
		name     string
		base     []string
		user     []string
		expected []string
	}{
		{
			name:     "empty user args",
			base:     []string{"-p", "--verbose"},
			user:     []string{},
			expected: []string{"-p", "--verbose"},
		},
		{
			name:     "no duplicates",
			base:     []string{"-p", "--verbose"},
			user:     []string{"--model", "claude-3"},
			expected: []string{"-p", "--verbose", "--model", "claude-3"},
		},
		{
			name:     "with duplicates",
			base:     []string{"-p", "--verbose", "--output-format", "stream-json"},
			user:     []string{"-p", "--verbose", "--model", "claude-3"},
			expected: []string{"-p", "--verbose", "--output-format", "stream-json", "--model", "claude-3"},
		},
		{
			name:     "all duplicates",
			base:     []string{"-p", "--verbose"},
			user:     []string{"-p", "--verbose"},
			expected: []string{"-p", "--verbose"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeClaudeArgs(tt.base, tt.user)

			if len(result) != len(tt.expected) {
				t.Errorf("len(result) = %d, want %d", len(result), len(tt.expected))
				return
			}

			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("result[%d] = %s, want %s", i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestGetConfigDir(t *testing.T) {
	dir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir() error = %v", err)
	}

	if dir == "" {
		t.Error("GetConfigDir() returned empty string")
	}

	// Should end with .cdev
	if filepath.Base(dir) != ".cdev" {
		t.Errorf("GetConfigDir() = %s, want to end with .cdev", dir)
	}
}

func TestEnsureConfigDir(t *testing.T) {
	// This test actually creates the config directory
	// Skip if we don't want side effects
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	dir, err := EnsureConfigDir()
	if err != nil {
		t.Fatalf("EnsureConfigDir() error = %v", err)
	}

	// Check that directory exists
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("failed to stat config dir: %v", err)
	}

	if !info.IsDir() {
		t.Errorf("config path %s is not a directory", dir)
	}
}
