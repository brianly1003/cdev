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
	Server      ServerConfig      `mapstructure:"server"`
	Repository  RepositoryConfig  `mapstructure:"repository"`
	Watcher     WatcherConfig     `mapstructure:"watcher"`
	Claude      ClaudeConfig      `mapstructure:"claude"`
	Git         GitConfig         `mapstructure:"git"`
	Logging     LoggingConfig     `mapstructure:"logging"`
	Limits      LimitsConfig      `mapstructure:"limits"`
	Pairing     PairingConfig     `mapstructure:"pairing"`
	Indexer     IndexerConfig     `mapstructure:"indexer"`
	Security    SecurityConfig    `mapstructure:"security"`
	Permissions PermissionsConfig `mapstructure:"permissions"`
	Debug       DebugConfig       `mapstructure:"debug"`
}

// ServerConfig holds server-related configuration.
type ServerConfig struct {
	Port        int    `mapstructure:"port"`         // Unified port for HTTP and WebSocket (default: 8766)
	Host        string `mapstructure:"host"`         // Bind address (default: 127.0.0.1)
	ExternalURL string `mapstructure:"external_url"` // Optional: public URL for tunnels (e.g., https://tunnel.devtunnels.ms)
	Headless    bool   `mapstructure:"headless"`     // If true, run as background daemon; if false (default), run in terminal mode
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
	Level    string            `mapstructure:"level"`
	Format   string            `mapstructure:"format"`
	Rotation LogRotationConfig `mapstructure:"rotation"`
}

// LogRotationConfig holds log rotation configuration.
type LogRotationConfig struct {
	Enabled    bool `mapstructure:"enabled"`      // Enable log rotation for Claude JSONL logs
	MaxSizeMB  int  `mapstructure:"max_size_mb"`  // Max size in MB before rotation
	MaxBackups int  `mapstructure:"max_backups"`  // Number of old logs to keep
	MaxAgeDays int  `mapstructure:"max_age_days"` // Days to keep old logs (0 = no limit)
	Compress   bool `mapstructure:"compress"`     // Compress rotated logs with gzip
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

// SecurityConfig holds security-related configuration.
type SecurityConfig struct {
	RequireAuth            bool            `mapstructure:"require_auth"`             // If true, require token for WebSocket connections
	RequirePairingApproval bool            `mapstructure:"require_pairing_approval"` // If true, pairing exchange requires local approve/reject
	TokenExpirySecs        int             `mapstructure:"token_expiry_secs"`        // Pairing token expiry in seconds
	AllowedOrigins         []string        `mapstructure:"allowed_origins"`          // Allowed origins for CORS/WebSocket (empty = localhost only)
	BindLocalhostOnly      bool            `mapstructure:"bind_localhost_only"`      // If true, only bind to localhost
	RequireSecureTransport bool            `mapstructure:"require_secure_transport"` // If true, require TLS/WSS for non-localhost use
	TLSCertFile            string          `mapstructure:"tls_cert_file"`            // Optional TLS certificate file path
	TLSKeyFile             string          `mapstructure:"tls_key_file"`             // Optional TLS key file path
	TrustedProxies         []string        `mapstructure:"trusted_proxies"`          // Optional trusted reverse-proxy CIDRs
	RateLimit              RateLimitConfig `mapstructure:"rate_limit"`               // Rate limiting configuration
}

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	Enabled           bool `mapstructure:"enabled"`             // Enable rate limiting
	RequestsPerMinute int  `mapstructure:"requests_per_minute"` // Max requests per minute per IP
}

// PermissionsConfig holds permission hook bridge configuration.
type PermissionsConfig struct {
	SessionMemory SessionMemoryConfig `mapstructure:"session_memory"`
}

// SessionMemoryConfig holds session memory configuration for the permission system.
type SessionMemoryConfig struct {
	Enabled     bool `mapstructure:"enabled"`      // Enable session memory for "Allow for Session" functionality
	TTLSeconds  int  `mapstructure:"ttl_seconds"`  // Idle timeout for session memory in seconds (default: 3600)
	MaxPatterns int  `mapstructure:"max_patterns"` // Max patterns per session (default: 100)
}

// DebugConfig holds debug and profiling configuration.
type DebugConfig struct {
	Enabled      bool `mapstructure:"enabled"`       // Enable debug endpoints (default: false)
	PprofEnabled bool `mapstructure:"pprof_enabled"` // Enable /debug/pprof/* endpoints (default: true when debug enabled)
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
	// NOTE: Keep this aligned with docs (CDEV_* env overrides).
	v.SetEnvPrefix("CDEV")
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

	// Backward compatibility: older docs/configs used server.http_port.
	// Map it into server.port unless server.port was explicitly set.
	applyLegacyKeyMappings(v)

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

func applyLegacyKeyMappings(v *viper.Viper) {
	// Treat server.http_port as an alias for server.port.
	// Precedence:
	// - `server.port` env override always wins (CDEV_SERVER_PORT)
	// - legacy `server.http_port` env override wins next (CDEV_SERVER_HTTP_PORT)
	// - then config file keys (server.port, then server.http_port)
	portEnv := isEnvSet("CDEV_SERVER_PORT")
	httpPortEnv := isEnvSet("CDEV_SERVER_HTTP_PORT")
	portCfg := v.InConfig("server.port")
	httpPortCfg := v.InConfig("server.http_port")

	if portEnv {
		return
	}
	if httpPortEnv {
		v.Set("server.port", v.Get("server.http_port"))
		return
	}
	if portCfg {
		return
	}
	if httpPortCfg {
		v.Set("server.port", v.Get("server.http_port"))
	}
}

func isEnvSet(key string) bool {
	_, ok := os.LookupEnv(key)
	return ok
}

// setDefaults sets default configuration values.
func setDefaults(v *viper.Viper) {
	// Server defaults - unified port for HTTP and WebSocket
	v.SetDefault("server.port", 8766)
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
	v.SetDefault("logging.rotation.enabled", true)
	v.SetDefault("logging.rotation.max_size_mb", 50)
	v.SetDefault("logging.rotation.max_backups", 5)
	v.SetDefault("logging.rotation.max_age_days", 30)
	v.SetDefault("logging.rotation.compress", true)

	// Limits defaults
	v.SetDefault("limits.max_file_size_kb", 200)
	v.SetDefault("limits.max_diff_size_kb", 500)
	v.SetDefault("limits.max_log_buffer", 1000)
	v.SetDefault("limits.max_prompt_len", 10000)

	// Pairing defaults
	v.SetDefault("pairing.token_expiry_secs", 3600)
	v.SetDefault("pairing.show_qr_in_terminal", false)

	// Indexer defaults - uses centralized patterns from defaults.go
	v.SetDefault("indexer.enabled", true)
	v.SetDefault("indexer.skip_directories", DefaultSkipDirectories)

	// Security defaults
	v.SetDefault("security.require_auth", true) // Auth required by default
	v.SetDefault("security.require_pairing_approval", true)
	v.SetDefault("security.token_expiry_secs", 3600) // 1 hour
	v.SetDefault("security.allowed_origins", []string{})
	v.SetDefault("security.bind_localhost_only", true) // Localhost only by default
	v.SetDefault("security.require_secure_transport", true)
	v.SetDefault("security.tls_cert_file", "")
	v.SetDefault("security.tls_key_file", "")
	v.SetDefault("security.trusted_proxies", []string{})
	v.SetDefault("security.rate_limit.enabled", false) // Disabled by default for local dev
	v.SetDefault("security.rate_limit.requests_per_minute", 100)

	// Permissions defaults (hook bridge)
	v.SetDefault("permissions.session_memory.enabled", true)
	v.SetDefault("permissions.session_memory.ttl_seconds", 3600) // 1 hour idle timeout
	v.SetDefault("permissions.session_memory.max_patterns", 100)

	// Debug defaults - disabled by default for security
	v.SetDefault("debug.enabled", false)
	v.SetDefault("debug.pprof_enabled", false) // Must be explicitly enabled
}

// postProcess applies post-processing to configuration.
func postProcess(cfg *Config) error {
	if cfg.Repository.Path != "" {
		// Resolve to absolute path only if explicitly configured
		absPath, err := filepath.Abs(cfg.Repository.Path)
		if err != nil {
			return fmt.Errorf("failed to resolve repository path: %w", err)
		}
		cfg.Repository.Path = absPath
	}

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
