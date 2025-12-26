// Package config handles configuration management for cdev.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the application.
type Config struct {
	Server     ServerConfig     `mapstructure:"server"`
	Repository RepositoryConfig `mapstructure:"repository"`
	Watcher    WatcherConfig    `mapstructure:"watcher"`
	Claude     ClaudeConfig     `mapstructure:"claude"`
	Git        GitConfig        `mapstructure:"git"`
	Logging    LoggingConfig    `mapstructure:"logging"`
	Limits     LimitsConfig     `mapstructure:"limits"`
	Pairing    PairingConfig    `mapstructure:"pairing"`
	Indexer    IndexerConfig    `mapstructure:"indexer"`
}

// ServerConfig holds server-related configuration.
type ServerConfig struct {
	WebSocketPort   int    `mapstructure:"websocket_port"`
	HTTPPort        int    `mapstructure:"http_port"`
	Host            string `mapstructure:"host"`
	ExternalWSURL   string `mapstructure:"external_ws_url"`   // Optional: public URL for WebSocket (e.g., wss://tunnel.devtunnels.ms)
	ExternalHTTPURL string `mapstructure:"external_http_url"` // Optional: public URL for HTTP API (e.g., https://tunnel.devtunnels.ms)
	Headless        bool   `mapstructure:"headless"`          // If true, run as background daemon; if false (default), run in terminal mode
}

// RepositoryConfig holds repository-related configuration.
type RepositoryConfig struct {
	Path string `mapstructure:"path"`
}

// WatcherConfig holds file watcher configuration.
type WatcherConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	DebounceMS     int      `mapstructure:"debounce_ms"`
	IgnorePatterns []string `mapstructure:"ignore_patterns"`
}

// ClaudeConfig holds Claude CLI configuration.
type ClaudeConfig struct {
	Command         string   `mapstructure:"command"`
	Args            []string `mapstructure:"args"`
	TimeoutMinutes  int      `mapstructure:"timeout_minutes"`
	SkipPermissions bool     `mapstructure:"skip_permissions"`
}

// GitConfig holds Git configuration.
type GitConfig struct {
	Enabled      bool   `mapstructure:"enabled"`
	Command      string `mapstructure:"command"`
	DiffOnChange bool   `mapstructure:"diff_on_change"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// LimitsConfig holds various limits.
type LimitsConfig struct {
	MaxFileSizeKB int `mapstructure:"max_file_size_kb"`
	MaxDiffSizeKB int `mapstructure:"max_diff_size_kb"`
	MaxLogBuffer  int `mapstructure:"max_log_buffer"`
	MaxPromptLen  int `mapstructure:"max_prompt_len"`
}

// PairingConfig holds pairing/QR code configuration.
type PairingConfig struct {
	TokenExpirySecs  int  `mapstructure:"token_expiry_secs"`
	ShowQRInTerminal bool `mapstructure:"show_qr_in_terminal"`
}

// IndexerConfig holds repository indexer configuration.
type IndexerConfig struct {
	Enabled         bool     `mapstructure:"enabled"`
	SkipDirectories []string `mapstructure:"skip_directories"`
}

// Load loads configuration from files and environment.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set config file if provided
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		// Default search paths
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.cdev")
		v.AddConfigPath("/etc/cdev")
	}

	// Environment variable prefix
	v.SetEnvPrefix("CDOT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Set defaults
	setDefaults(v)

	// Read config file (optional - not an error if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	// Post-process configuration
	if err := postProcess(&cfg); err != nil {
		return nil, err
	}

	// Validate configuration
	if err := Validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// setDefaults sets default configuration values.
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.websocket_port", 8765)
	v.SetDefault("server.http_port", 8766)
	v.SetDefault("server.host", "127.0.0.1")

	// Repository defaults
	v.SetDefault("repository.path", "")

	// Watcher defaults - uses centralized patterns from defaults.go
	v.SetDefault("watcher.enabled", true)
	v.SetDefault("watcher.debounce_ms", 100)
	v.SetDefault("watcher.ignore_patterns", DefaultWatcherIgnorePatterns)

	// Claude defaults
	// Note: Don't use --input-format stream-json when passing prompt as CLI argument
	// stream-json input expects JSON on stdin, not command-line prompts
	v.SetDefault("claude.command", "claude")
	v.SetDefault("claude.args", []string{"-p", "--verbose", "--output-format", "stream-json"})
	v.SetDefault("claude.timeout_minutes", 30)
	v.SetDefault("claude.skip_permissions", false)

	// Git defaults
	v.SetDefault("git.enabled", true)
	v.SetDefault("git.command", "git")
	v.SetDefault("git.diff_on_change", true)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "console")

	// Limits defaults
	v.SetDefault("limits.max_file_size_kb", 200)
	v.SetDefault("limits.max_diff_size_kb", 500)
	v.SetDefault("limits.max_log_buffer", 1000)
	v.SetDefault("limits.max_prompt_len", 10000)

	// Pairing defaults
	v.SetDefault("pairing.token_expiry_secs", 3600)
	v.SetDefault("pairing.show_qr_in_terminal", true)

	// Indexer defaults - uses centralized patterns from defaults.go
	v.SetDefault("indexer.enabled", true)
	v.SetDefault("indexer.skip_directories", DefaultSkipDirectories)
}

// postProcess applies post-processing to configuration.
func postProcess(cfg *Config) error {
	// If repository path is empty, use current directory
	if cfg.Repository.Path == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		cfg.Repository.Path = cwd
	}

	// Resolve to absolute path
	absPath, err := filepath.Abs(cfg.Repository.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve repository path: %w", err)
	}
	cfg.Repository.Path = absPath

	// Ensure required Claude args are always present
	// Base args needed for proper operation: -p, --verbose, --output-format stream-json
	baseArgs := []string{"-p", "--verbose", "--output-format", "stream-json"}
	cfg.Claude.Args = mergeClaudeArgs(baseArgs, cfg.Claude.Args)

	return nil
}

// mergeClaudeArgs merges base args with user-provided args.
// User args are appended after base args (no duplicates).
func mergeClaudeArgs(baseArgs, userArgs []string) []string {
	// Create a set of base args for deduplication
	baseSet := make(map[string]bool)
	for _, arg := range baseArgs {
		baseSet[arg] = true
	}

	// Start with base args
	result := make([]string, len(baseArgs))
	copy(result, baseArgs)

	// Append user args that aren't duplicates of base args
	for _, arg := range userArgs {
		if !baseSet[arg] {
			result = append(result, arg)
		}
	}

	return result
}

// GetConfigDir returns the user config directory for cdev.
func GetConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".cdev"), nil
}

// EnsureConfigDir ensures the config directory exists.
func EnsureConfigDir() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}
