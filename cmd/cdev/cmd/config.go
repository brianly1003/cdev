// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	configInitLocal bool
	configInitForce bool
)

// configCmd displays or manages configuration.
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Display and manage configuration",
	Long: `Display and manage cdev configuration.

Without subcommands, shows the current effective configuration.

Examples:
  cdev config              # Show current config
  cdev config init         # Create config file with defaults
  cdev config path         # Show config file location
  cdev config get <key>    # Get a config value
  cdev config set <key> <value>  # Set a config value`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
			os.Exit(1)
		}
		printConfig(cfg)
	},
}

// configInitCmd creates a config file with defaults.
var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a config file with default settings",
	Long: `Create a config file with default settings and documentation.

By default, creates ~/.cdev/config.yaml.
Use --local to create ./config.yaml in the current directory.

Examples:
  cdev config init          # Create ~/.cdev/config.yaml
  cdev config init --local  # Create ./config.yaml
  cdev config init --force  # Overwrite existing file`,
	RunE: runConfigInit,
}

// configPathCmd shows config file location.
var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Show config file location",
	Long: `Show where the config file is located and whether it exists.

Examples:
  cdev config path`,
	Run: runConfigPath,
}

// configGetCmd gets a config value.
var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Long: `Get a configuration value by key.

Keys use dot notation to access nested values.

Examples:
  cdev config get server.http_port
  cdev config get logging.level
  cdev config get claude.command`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

// configSetCmd sets a config value.
var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Long: `Set a configuration value by key.

Creates the config file if it doesn't exist.
Keys use dot notation to access nested values.

Examples:
  cdev config set server.http_port 9000
  cdev config set logging.level debug
  cdev config set claude.skip_permissions true`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

func init() {
	// Add subcommands to config
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configPathCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)

	// Flags for init
	configInitCmd.Flags().BoolVar(&configInitLocal, "local", false, "create config in current directory instead of ~/.cdev/")
	configInitCmd.Flags().BoolVar(&configInitForce, "force", false, "overwrite existing config file")
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	var configPath string

	if configInitLocal {
		configPath = "config.yaml"
	} else {
		configDir, err := config.EnsureConfigDir()
		if err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		configPath = filepath.Join(configDir, "config.yaml")
	}

	// Check if file exists
	if _, err := os.Stat(configPath); err == nil {
		if !configInitForce {
			return fmt.Errorf("config file already exists: %s\nUse --force to overwrite", configPath)
		}
	}

	// Write default config with comments
	if err := writeDefaultConfig(configPath); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Created %s\n", configPath)
	fmt.Println("Edit this file to customize cdev behavior.")
	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting config dir: %v\n", err)
		os.Exit(1)
	}

	configPath := filepath.Join(configDir, "config.yaml")

	// Check various locations
	locations := []string{
		"./config.yaml",
		configPath,
		"/etc/cdev/config.yaml",
	}

	fmt.Println("Config search paths (in order):")
	for i, loc := range locations {
		exists := "not found"
		if _, err := os.Stat(loc); err == nil {
			exists = "exists"
		}
		fmt.Printf("  %d. %s (%s)\n", i+1, loc, exists)
	}

	fmt.Printf("\nConfig directory: %s\n", configDir)
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	key := args[0]

	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	value, err := getConfigValue(cfg, key)
	if err != nil {
		return err
	}

	fmt.Println(value)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	key := args[0]
	value := args[1]

	// Determine config path
	configDir, err := config.EnsureConfigDir()
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	configPath := filepath.Join(configDir, "config.yaml")

	// Load existing config or create new one
	var data map[string]interface{}

	if content, err := os.ReadFile(configPath); err == nil {
		if err := yaml.Unmarshal(content, &data); err != nil {
			return fmt.Errorf("failed to parse existing config: %w", err)
		}
	}

	if data == nil {
		data = make(map[string]interface{})
	}

	// Set the value
	if err := setNestedValue(data, key, value); err != nil {
		return err
	}

	// Write back
	content, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	if err := os.WriteFile(configPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Set %s = %s in %s\n", key, value, configPath)
	return nil
}

func getConfigValue(cfg *config.Config, key string) (interface{}, error) {
	parts := strings.Split(key, ".")

	switch parts[0] {
	case "server":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "port":
			return cfg.Server.Port, nil
		case "host":
			return cfg.Server.Host, nil
		case "external_url":
			return cfg.Server.ExternalURL, nil
		}
	case "logging":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "level":
			return cfg.Logging.Level, nil
		case "format":
			return cfg.Logging.Format, nil
		}
	case "claude":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "command":
			return cfg.Claude.Command, nil
		case "timeout_minutes":
			return cfg.Claude.TimeoutMinutes, nil
		case "skip_permissions":
			return cfg.Claude.SkipPermissions, nil
		}
	case "git":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "enabled":
			return cfg.Git.Enabled, nil
		case "command":
			return cfg.Git.Command, nil
		case "diff_on_change":
			return cfg.Git.DiffOnChange, nil
		}
	case "watcher":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "enabled":
			return cfg.Watcher.Enabled, nil
		case "debounce_ms":
			return cfg.Watcher.DebounceMS, nil
		}
	case "indexer":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "enabled":
			return cfg.Indexer.Enabled, nil
		}
	case "repository":
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid key: %s", key)
		}
		switch parts[1] {
		case "path":
			return cfg.Repository.Path, nil
		}
	}

	return nil, fmt.Errorf("unknown config key: %s", key)
}

func setNestedValue(data map[string]interface{}, key string, value string) error {
	parts := strings.Split(key, ".")

	// Navigate to the parent
	current := data
	for i := 0; i < len(parts)-1; i++ {
		if _, ok := current[parts[i]]; !ok {
			current[parts[i]] = make(map[string]interface{})
		}
		if nested, ok := current[parts[i]].(map[string]interface{}); ok {
			current = nested
		} else {
			return fmt.Errorf("cannot set nested value: %s is not a map", parts[i])
		}
	}

	// Convert value to appropriate type based on key
	finalKey := parts[len(parts)-1]
	current[finalKey] = parseValue(key, value)

	return nil
}

func parseValue(key string, value string) interface{} {
	// Boolean values
	if value == "true" {
		return true
	}
	if value == "false" {
		return false
	}

	// Integer values for known int fields
	intKeys := []string{"port", "debounce_ms", "timeout_minutes",
		"max_file_size_kb", "max_diff_size_kb", "max_log_buffer", "max_prompt_len",
		"token_expiry_secs"}
	for _, k := range intKeys {
		if strings.HasSuffix(key, k) {
			var i int
			if _, err := fmt.Sscanf(value, "%d", &i); err == nil {
				return i
			}
		}
	}

	// Default to string
	return value
}

func writeDefaultConfig(path string) error {
	content := `# cdev Configuration
# Documentation: https://github.com/brianly1003/cdev
# Copy this file to ~/.cdev/config.yaml and modify as needed

# Server settings
server:
  # Unified port for HTTP API and WebSocket connections
  port: 8766

  # Bind address (use 0.0.0.0 to allow external connections)
  host: "127.0.0.1"

  # External URL for VS Code port forwarding / tunnels
  # When set, the QR code will contain this URL instead of localhost
  # external_url: "https://your-tunnel.devtunnels.ms"

# Logging settings
logging:
  # Log level: debug, info, warn, error
  level: "info"

  # Log format: console (human-readable) or json
  format: "console"

# Claude CLI settings
claude:
  # Path to claude CLI executable (or just "claude" if in PATH)
  command: "claude"

  # Set to true to skip permission prompts (--dangerously-skip-permissions)
  skip_permissions: false

  # Timeout for Claude operations in minutes
  timeout_minutes: 30

# Git integration
git:
  # Enable git status and diff tracking
  enabled: true

  # Path to git CLI executable
  command: "git"

  # Auto-generate diff when files change
  diff_on_change: true

# File watcher
watcher:
  # Enable file system watching
  enabled: true

  # Debounce rapid changes (milliseconds)
  debounce_ms: 100

  # Patterns to ignore (supports glob syntax)
  ignore_patterns:
    - ".git"
    - "node_modules"
    - ".venv"
    - "venv"
    - "__pycache__"
    - "*.pyc"
    - ".DS_Store"
    - "dist"
    - "build"
    - ".next"
    - ".nuxt"

# Repository indexer (for fast file search)
indexer:
  # Enable repository indexing
  enabled: true

  # Directories to skip during indexing
  skip_directories:
    - ".git"
    - "node_modules"
    - "vendor"
    - "__pycache__"
    - ".venv"
    - "venv"
    - "dist"
    - "build"

# Resource limits
limits:
  max_file_size_kb: 200
  max_diff_size_kb: 500
  max_log_buffer: 1000
  max_prompt_len: 10000

# Mobile pairing settings
pairing:
  token_expiry_secs: 3600
  show_qr_in_terminal: true
`

	return os.WriteFile(path, []byte(content), 0644)
}
