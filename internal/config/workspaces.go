package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// WorkspaceManagerConfig holds workspace manager configuration.
type WorkspaceManagerConfig struct {
	Enabled                 bool   `mapstructure:"enabled" yaml:"enabled"`
	Port                    int    `mapstructure:"port" yaml:"port"`
	Host                    string `mapstructure:"host" yaml:"host"`
	AutoStartWorkspaces     bool   `mapstructure:"auto_start_workspaces" yaml:"auto_start_workspaces"`
	LogLevel                string `mapstructure:"log_level" yaml:"log_level"`
	MaxConcurrentWorkspaces int    `mapstructure:"max_concurrent_workspaces" yaml:"max_concurrent_workspaces"`
	AutoStopIdleMinutes     int    `mapstructure:"auto_stop_idle_minutes" yaml:"auto_stop_idle_minutes"`
	PortRangeStart          int    `mapstructure:"port_range_start" yaml:"port_range_start"`
	PortRangeEnd            int    `mapstructure:"port_range_end" yaml:"port_range_end"`
	RestartOnCrash          bool   `mapstructure:"restart_on_crash" yaml:"restart_on_crash"`
	MaxRestartAttempts      int    `mapstructure:"max_restart_attempts" yaml:"max_restart_attempts"`
	RestartBackoffSeconds   int    `mapstructure:"restart_backoff_seconds" yaml:"restart_backoff_seconds"`
}

// WorkspaceDefinition represents a configured workspace.
type WorkspaceDefinition struct {
	ID           string    `mapstructure:"id" yaml:"id"`
	Name         string    `mapstructure:"name" yaml:"name"`
	Path         string    `mapstructure:"path" yaml:"path"`
	Port         int       `mapstructure:"port" yaml:"port"`
	AutoStart    bool      `mapstructure:"auto_start" yaml:"auto_start"`
	ConfigFile   string    `mapstructure:"config_file,omitempty" yaml:"config_file,omitempty"`
	CreatedAt    time.Time `mapstructure:"created_at" yaml:"created_at"`
	LastAccessed time.Time `mapstructure:"last_accessed" yaml:"last_accessed"`
}

// WorkspaceDefaults holds default settings for all workspaces.
type WorkspaceDefaults struct {
	Watcher WatcherConfig `mapstructure:"watcher" yaml:"watcher"`
	Git     GitConfig     `mapstructure:"git" yaml:"git"`
	Logging LoggingConfig `mapstructure:"logging" yaml:"logging"`
	Limits  LimitsConfig  `mapstructure:"limits" yaml:"limits"`
}

// WorkspacesConfig represents the complete workspaces.yaml file.
type WorkspacesConfig struct {
	Manager    WorkspaceManagerConfig `mapstructure:"manager" yaml:"manager"`
	Workspaces []WorkspaceDefinition  `mapstructure:"workspaces" yaml:"workspaces"`
	Defaults   WorkspaceDefaults      `mapstructure:"defaults" yaml:"defaults"`
}

// DefaultWorkspacesPath returns the default path for workspaces.yaml.
func DefaultWorkspacesPath() string {
	configDir, err := GetConfigDir()
	if err != nil {
		// Fallback to home directory
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cdev", "workspaces.yaml")
	}
	return filepath.Join(configDir, "workspaces.yaml")
}

// LoadWorkspaces loads the workspaces configuration from workspaces.yaml
func LoadWorkspaces(configPath string) (*WorkspacesConfig, error) {
	v := viper.New()

	// Determine config file path
	if configPath == "" {
		configDir, err := GetConfigDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get config directory: %w", err)
		}
		configPath = filepath.Join(configDir, "workspaces.yaml")
	}

	v.SetConfigFile(configPath)

	// Set defaults
	setWorkspaceDefaults(v)

	// Read config file (create if not exists)
	if err := v.ReadInConfig(); err != nil {
		// Check if file doesn't exist (ConfigFileNotFoundError or os.IsNotExist)
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Create default config
			cfg := createDefaultWorkspacesConfig()
			if err := SaveWorkspaces(configPath, cfg); err != nil {
				return nil, fmt.Errorf("failed to create default workspaces config: %w", err)
			}
			return cfg, nil
		}
		// Also handle os.PathError (directory doesn't exist)
		if os.IsNotExist(err) {
			// Create default config
			cfg := createDefaultWorkspacesConfig()
			if err := SaveWorkspaces(configPath, cfg); err != nil {
				return nil, fmt.Errorf("failed to create default workspaces config: %w", err)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("error reading workspaces config: %w", err)
	}

	var cfg WorkspacesConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("error parsing workspaces config: %w", err)
	}

	// Post-process
	sanitizeWorkspaceManagerConfig(&cfg.Manager)
	if err := postProcessWorkspaces(&cfg); err != nil {
		return nil, err
	}

	// Validate
	if err := ValidateWorkspaces(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// sanitizeWorkspaceManagerConfig fills invalid manager values with defaults.
// This prevents invalid zero values from breaking workspace loading.
func sanitizeWorkspaceManagerConfig(cfg *WorkspaceManagerConfig) {
	defaults := createDefaultWorkspacesConfig().Manager

	if cfg.Port < 1 || cfg.Port > 65535 {
		cfg.Port = defaults.Port
	}
	if cfg.PortRangeStart < 1 || cfg.PortRangeStart > 65535 {
		cfg.PortRangeStart = defaults.PortRangeStart
	}
	if cfg.PortRangeEnd < 1 || cfg.PortRangeEnd > 65535 {
		cfg.PortRangeEnd = defaults.PortRangeEnd
	}
	if cfg.PortRangeStart >= cfg.PortRangeEnd {
		cfg.PortRangeStart = defaults.PortRangeStart
		cfg.PortRangeEnd = defaults.PortRangeEnd
	}
	if cfg.Port >= cfg.PortRangeStart && cfg.Port <= cfg.PortRangeEnd {
		cfg.Port = defaults.Port
		// Ensure ranges still valid after fixing port
		if cfg.Port >= cfg.PortRangeStart && cfg.Port <= cfg.PortRangeEnd {
			cfg.PortRangeStart = defaults.PortRangeStart
			cfg.PortRangeEnd = defaults.PortRangeEnd
		}
	}
	if cfg.MaxConcurrentWorkspaces < 1 || cfg.MaxConcurrentWorkspaces > 100 {
		cfg.MaxConcurrentWorkspaces = defaults.MaxConcurrentWorkspaces
	}
	if cfg.AutoStopIdleMinutes < 0 {
		cfg.AutoStopIdleMinutes = defaults.AutoStopIdleMinutes
	}
	if cfg.MaxRestartAttempts < 0 {
		cfg.MaxRestartAttempts = defaults.MaxRestartAttempts
	}
	if cfg.RestartBackoffSeconds < 0 {
		cfg.RestartBackoffSeconds = defaults.RestartBackoffSeconds
	}
	if cfg.PortRangeEnd-cfg.PortRangeStart+1 < cfg.MaxConcurrentWorkspaces {
		cfg.MaxConcurrentWorkspaces = defaults.MaxConcurrentWorkspaces
	}
	if cfg.Host == "" {
		cfg.Host = defaults.Host
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = defaults.LogLevel
	}
}

// SaveWorkspaces saves the workspaces configuration
func SaveWorkspaces(configPath string, cfg *WorkspacesConfig) error {
	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// createDefaultWorkspacesConfig creates a minimal default config
func createDefaultWorkspacesConfig() *WorkspacesConfig {
	return &WorkspacesConfig{
		Manager: WorkspaceManagerConfig{
			Enabled:                 true,
			Port:                    8765,
			Host:                    "127.0.0.1",
			AutoStartWorkspaces:     false,
			LogLevel:                "info",
			MaxConcurrentWorkspaces: 5,
			AutoStopIdleMinutes:     30,
			PortRangeStart:          8766,
			PortRangeEnd:            8799,
			RestartOnCrash:          true,
			MaxRestartAttempts:      3,
			RestartBackoffSeconds:   5,
		},
		Workspaces: []WorkspaceDefinition{},
		Defaults: WorkspaceDefaults{
			Watcher: WatcherConfig{
				Enabled:        true,
				DebounceMS:     100,
				IgnorePatterns: DefaultWatcherIgnorePatterns,
			},
			Git: GitConfig{
				Enabled:      true,
				Command:      "git",
				DiffOnChange: true,
			},
			Logging: LoggingConfig{
				Level:  "info",
				Format: "console",
			},
			Limits: LimitsConfig{
				MaxFileSizeKB: 200,
				MaxDiffSizeKB: 500,
				MaxLogBuffer:  1000,
				MaxPromptLen:  10000,
			},
		},
	}
}

// DefaultWorkspacesConfig returns a copy of the default workspaces configuration.
func DefaultWorkspacesConfig() *WorkspacesConfig {
	return createDefaultWorkspacesConfig()
}

// setWorkspaceDefaults sets default values for workspace config
func setWorkspaceDefaults(v *viper.Viper) {
	// Manager defaults
	v.SetDefault("manager.enabled", true)
	v.SetDefault("manager.port", 8765)
	v.SetDefault("manager.host", "127.0.0.1")
	v.SetDefault("manager.auto_start_workspaces", false)
	v.SetDefault("manager.log_level", "info")
	v.SetDefault("manager.max_concurrent_workspaces", 5)
	v.SetDefault("manager.auto_stop_idle_minutes", 30)
	v.SetDefault("manager.port_range_start", 8766)
	v.SetDefault("manager.port_range_end", 8799)
	v.SetDefault("manager.restart_on_crash", true)
	v.SetDefault("manager.max_restart_attempts", 3)
	v.SetDefault("manager.restart_backoff_seconds", 5)

	// Defaults for all workspaces - uses centralized patterns from defaults.go
	v.SetDefault("defaults.watcher.enabled", true)
	v.SetDefault("defaults.watcher.debounce_ms", 100)
	v.SetDefault("defaults.watcher.ignore_patterns", DefaultWatcherIgnorePatterns)
	v.SetDefault("defaults.git.enabled", true)
	v.SetDefault("defaults.git.command", "git")
	v.SetDefault("defaults.git.diff_on_change", true)
	v.SetDefault("defaults.logging.level", "info")
	v.SetDefault("defaults.logging.format", "console")
	v.SetDefault("defaults.limits.max_file_size_kb", 200)
	v.SetDefault("defaults.limits.max_diff_size_kb", 500)
	v.SetDefault("defaults.limits.max_log_buffer", 1000)
	v.SetDefault("defaults.limits.max_prompt_len", 10000)
}

// postProcessWorkspaces applies post-processing to workspaces config
func postProcessWorkspaces(cfg *WorkspacesConfig) error {
	// Generate IDs for workspaces without them
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == "" {
			cfg.Workspaces[i].ID = "ws-" + uuid.New().String()[:8]
		}

		// Resolve absolute paths
		absPath, err := filepath.Abs(cfg.Workspaces[i].Path)
		if err != nil {
			return fmt.Errorf("failed to resolve workspace path: %w", err)
		}
		cfg.Workspaces[i].Path = absPath

		// Set created_at if not set
		if cfg.Workspaces[i].CreatedAt.IsZero() {
			cfg.Workspaces[i].CreatedAt = time.Now()
		}

		// Set last_accessed if not set
		if cfg.Workspaces[i].LastAccessed.IsZero() {
			cfg.Workspaces[i].LastAccessed = time.Now()
		}
	}

	return nil
}
