package config

import (
	"os"
	"strings"
	"testing"
)

func TestValidateServer(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ServerConfig
		wantErr string
	}{
		{
			name: "valid config",
			cfg: ServerConfig{
				WebSocketPort: 8765,
				HTTPPort:      8766,
				Host:          "127.0.0.1",
			},
			wantErr: "",
		},
		{
			name: "port too low",
			cfg: ServerConfig{
				WebSocketPort: 0,
				HTTPPort:      8766,
				Host:          "127.0.0.1",
			},
			wantErr: "websocket_port must be between 1 and 65535",
		},
		{
			name: "port too high",
			cfg: ServerConfig{
				WebSocketPort: 8765,
				HTTPPort:      70000,
				Host:          "127.0.0.1",
			},
			wantErr: "http_port must be between 1 and 65535",
		},
		{
			name: "same ports",
			cfg: ServerConfig{
				WebSocketPort: 8765,
				HTTPPort:      8765,
				Host:          "127.0.0.1",
			},
			wantErr: "must be different",
		},
		{
			name: "empty host",
			cfg: ServerConfig{
				WebSocketPort: 8765,
				HTTPPort:      8766,
				Host:          "",
			},
			wantErr: "host cannot be empty",
		},
		{
			name: "valid external ws url",
			cfg: ServerConfig{
				WebSocketPort: 8765,
				HTTPPort:      8766,
				Host:          "127.0.0.1",
				ExternalWSURL: "wss://example.com/ws",
			},
			wantErr: "",
		},
		{
			name: "invalid external ws url scheme",
			cfg: ServerConfig{
				WebSocketPort: 8765,
				HTTPPort:      8766,
				Host:          "127.0.0.1",
				ExternalWSURL: "http://example.com/ws",
			},
			wantErr: "must use one of these schemes: ws, wss",
		},
		{
			name: "valid external http url",
			cfg: ServerConfig{
				WebSocketPort:   8765,
				HTTPPort:        8766,
				Host:            "127.0.0.1",
				ExternalHTTPURL: "https://example.com/api",
			},
			wantErr: "",
		},
		{
			name: "invalid external http url scheme",
			cfg: ServerConfig{
				WebSocketPort:   8765,
				HTTPPort:        8766,
				Host:            "127.0.0.1",
				ExternalHTTPURL: "wss://example.com/api",
			},
			wantErr: "must use one of these schemes: http, https",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateServer(&tt.cfg)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateServer() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateServer() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validateServer() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateRepository(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name    string
		cfg     RepositoryConfig
		wantErr string
	}{
		{
			name: "valid directory",
			cfg: RepositoryConfig{
				Path: tempDir,
			},
			wantErr: "",
		},
		{
			name: "empty path (valid - workspaces via API)",
			cfg: RepositoryConfig{
				Path: "",
			},
			wantErr: "", // Empty path is valid - workspaces are managed via workspace/add API
		},
		{
			name: "non-existent path",
			cfg: RepositoryConfig{
				Path: "/nonexistent/path/12345",
			},
			wantErr: "does not exist",
		},
	}

	// Create a file for the "not a directory" test
	tempFile := tempDir + "/testfile"
	if err := os.WriteFile(tempFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	tests = append(tests, struct {
		name    string
		cfg     RepositoryConfig
		wantErr string
	}{
		name: "path is file not directory",
		cfg: RepositoryConfig{
			Path: tempFile,
		},
		wantErr: "is not a directory",
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRepository(&tt.cfg)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateRepository() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateRepository() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validateRepository() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateWatcher(t *testing.T) {
	tests := []struct {
		name    string
		cfg     WatcherConfig
		wantErr string
	}{
		{
			name: "valid config",
			cfg: WatcherConfig{
				Enabled:    true,
				DebounceMS: 100,
			},
			wantErr: "",
		},
		{
			name: "zero debounce (valid)",
			cfg: WatcherConfig{
				Enabled:    true,
				DebounceMS: 0,
			},
			wantErr: "",
		},
		{
			name: "negative debounce",
			cfg: WatcherConfig{
				Enabled:    true,
				DebounceMS: -1,
			},
			wantErr: "cannot be negative",
		},
		{
			name: "debounce too high",
			cfg: WatcherConfig{
				Enabled:    true,
				DebounceMS: 15000,
			},
			wantErr: "cannot exceed 10000ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWatcher(&tt.cfg)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateWatcher() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateWatcher() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validateWatcher() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateClaude(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ClaudeConfig
		wantErr string
	}{
		{
			name: "valid config",
			cfg: ClaudeConfig{
				Command:        "claude",
				TimeoutMinutes: 30,
			},
			wantErr: "",
		},
		{
			name: "empty command",
			cfg: ClaudeConfig{
				Command:        "",
				TimeoutMinutes: 30,
			},
			wantErr: "command cannot be empty",
		},
		{
			name: "timeout too low",
			cfg: ClaudeConfig{
				Command:        "claude",
				TimeoutMinutes: 0,
			},
			wantErr: "must be at least 1",
		},
		{
			name: "timeout too high",
			cfg: ClaudeConfig{
				Command:        "claude",
				TimeoutMinutes: 200,
			},
			wantErr: "cannot exceed 120",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClaude(&tt.cfg)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateClaude() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateClaude() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validateClaude() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateLimits(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LimitsConfig
		wantErr string
	}{
		{
			name: "valid config",
			cfg: LimitsConfig{
				MaxFileSizeKB: 200,
				MaxDiffSizeKB: 500,
				MaxLogBuffer:  1000,
				MaxPromptLen:  10000,
			},
			wantErr: "",
		},
		{
			name: "file size too small",
			cfg: LimitsConfig{
				MaxFileSizeKB: 0,
				MaxDiffSizeKB: 500,
				MaxLogBuffer:  1000,
				MaxPromptLen:  10000,
			},
			wantErr: "max_file_size_kb must be at least 1",
		},
		{
			name: "file size too large",
			cfg: LimitsConfig{
				MaxFileSizeKB: 20000,
				MaxDiffSizeKB: 500,
				MaxLogBuffer:  1000,
				MaxPromptLen:  10000,
			},
			wantErr: "max_file_size_kb cannot exceed 10240",
		},
		{
			name: "diff size too small",
			cfg: LimitsConfig{
				MaxFileSizeKB: 200,
				MaxDiffSizeKB: 0,
				MaxLogBuffer:  1000,
				MaxPromptLen:  10000,
			},
			wantErr: "max_diff_size_kb must be at least 1",
		},
		{
			name: "log buffer too small",
			cfg: LimitsConfig{
				MaxFileSizeKB: 200,
				MaxDiffSizeKB: 500,
				MaxLogBuffer:  50,
				MaxPromptLen:  10000,
			},
			wantErr: "max_log_buffer must be at least 100",
		},
		{
			name: "prompt len too small",
			cfg: LimitsConfig{
				MaxFileSizeKB: 200,
				MaxDiffSizeKB: 500,
				MaxLogBuffer:  1000,
				MaxPromptLen:  50,
			},
			wantErr: "max_prompt_len must be at least 100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLimits(&tt.cfg)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateLimits() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateLimits() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validateLimits() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateExternalURL(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		fieldName      string
		allowedSchemes []string
		wantErr        string
	}{
		{
			name:           "valid https url",
			url:            "https://example.com/api",
			fieldName:      "test_url",
			allowedSchemes: []string{"http", "https"},
			wantErr:        "",
		},
		{
			name:           "valid wss url",
			url:            "wss://example.com/ws",
			fieldName:      "test_url",
			allowedSchemes: []string{"ws", "wss"},
			wantErr:        "",
		},
		{
			name:           "invalid scheme",
			url:            "ftp://example.com",
			fieldName:      "test_url",
			allowedSchemes: []string{"http", "https"},
			wantErr:        "must use one of these schemes",
		},
		{
			name:           "missing host",
			url:            "https://",
			fieldName:      "test_url",
			allowedSchemes: []string{"http", "https"},
			wantErr:        "must include a host",
		},
		{
			name:           "case insensitive scheme",
			url:            "HTTPS://example.com",
			fieldName:      "test_url",
			allowedSchemes: []string{"http", "https"},
			wantErr:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExternalURL(tt.url, tt.fieldName, tt.allowedSchemes)

			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("validateExternalURL() error = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("validateExternalURL() error = nil, want error containing %q", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("validateExternalURL() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidate_FullConfig(t *testing.T) {
	tempDir := t.TempDir()

	cfg := &Config{
		Server: ServerConfig{
			WebSocketPort: 8765,
			HTTPPort:      8766,
			Host:          "127.0.0.1",
		},
		Repository: RepositoryConfig{
			Path: tempDir,
		},
		Watcher: WatcherConfig{
			Enabled:    true,
			DebounceMS: 100,
		},
		Claude: ClaudeConfig{
			Command:        "claude",
			TimeoutMinutes: 30,
		},
		Limits: LimitsConfig{
			MaxFileSizeKB: 200,
			MaxDiffSizeKB: 500,
			MaxLogBuffer:  1000,
			MaxPromptLen:  10000,
		},
	}

	if err := Validate(cfg); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}
