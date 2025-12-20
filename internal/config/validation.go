package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Validate validates the configuration.
func Validate(cfg *Config) error {
	// Validate server config
	if err := validateServer(&cfg.Server); err != nil {
		return err
	}

	// Validate repository config
	if err := validateRepository(&cfg.Repository); err != nil {
		return err
	}

	// Validate watcher config
	if err := validateWatcher(&cfg.Watcher); err != nil {
		return err
	}

	// Validate claude config
	if err := validateClaude(&cfg.Claude); err != nil {
		return err
	}

	// Validate limits config
	if err := validateLimits(&cfg.Limits); err != nil {
		return err
	}

	return nil
}

func validateServer(cfg *ServerConfig) error {
	if cfg.WebSocketPort < 1 || cfg.WebSocketPort > 65535 {
		return fmt.Errorf("server.websocket_port must be between 1 and 65535")
	}
	if cfg.HTTPPort < 1 || cfg.HTTPPort > 65535 {
		return fmt.Errorf("server.http_port must be between 1 and 65535")
	}
	if cfg.WebSocketPort == cfg.HTTPPort {
		return fmt.Errorf("server.websocket_port and server.http_port must be different")
	}
	if cfg.Host == "" {
		return fmt.Errorf("server.host cannot be empty")
	}

	// Validate external WebSocket URL if provided
	if cfg.ExternalWSURL != "" {
		if err := validateExternalURL(cfg.ExternalWSURL, "server.external_ws_url", []string{"ws", "wss"}); err != nil {
			return err
		}
	}

	// Validate external HTTP URL if provided
	if cfg.ExternalHTTPURL != "" {
		if err := validateExternalURL(cfg.ExternalHTTPURL, "server.external_http_url", []string{"http", "https"}); err != nil {
			return err
		}
	}

	return nil
}

// validateExternalURL validates that a URL is well-formed and uses an allowed scheme.
func validateExternalURL(rawURL, fieldName string, allowedSchemes []string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s is not a valid URL: %w", fieldName, err)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%s must include a host", fieldName)
	}

	schemeValid := false
	for _, scheme := range allowedSchemes {
		if strings.EqualFold(parsed.Scheme, scheme) {
			schemeValid = true
			break
		}
	}
	if !schemeValid {
		return fmt.Errorf("%s must use one of these schemes: %s", fieldName, strings.Join(allowedSchemes, ", "))
	}

	return nil
}

func validateRepository(cfg *RepositoryConfig) error {
	if cfg.Path == "" {
		return fmt.Errorf("repository.path cannot be empty")
	}

	// Check if path exists
	info, err := os.Stat(cfg.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("repository.path does not exist: %s", cfg.Path)
		}
		return fmt.Errorf("error accessing repository.path: %w", err)
	}

	// Check if path is a directory
	if !info.IsDir() {
		return fmt.Errorf("repository.path is not a directory: %s", cfg.Path)
	}

	return nil
}

func validateWatcher(cfg *WatcherConfig) error {
	if cfg.DebounceMS < 0 {
		return fmt.Errorf("watcher.debounce_ms cannot be negative")
	}
	if cfg.DebounceMS > 10000 {
		return fmt.Errorf("watcher.debounce_ms cannot exceed 10000ms")
	}
	return nil
}

func validateClaude(cfg *ClaudeConfig) error {
	if cfg.Command == "" {
		return fmt.Errorf("claude.command cannot be empty")
	}
	if cfg.TimeoutMinutes < 1 {
		return fmt.Errorf("claude.timeout_minutes must be at least 1")
	}
	if cfg.TimeoutMinutes > 120 {
		return fmt.Errorf("claude.timeout_minutes cannot exceed 120")
	}
	return nil
}

func validateLimits(cfg *LimitsConfig) error {
	if cfg.MaxFileSizeKB < 1 {
		return fmt.Errorf("limits.max_file_size_kb must be at least 1")
	}
	if cfg.MaxFileSizeKB > 10240 { // 10MB max
		return fmt.Errorf("limits.max_file_size_kb cannot exceed 10240 (10MB)")
	}
	if cfg.MaxDiffSizeKB < 1 {
		return fmt.Errorf("limits.max_diff_size_kb must be at least 1")
	}
	if cfg.MaxLogBuffer < 100 {
		return fmt.Errorf("limits.max_log_buffer must be at least 100")
	}
	if cfg.MaxPromptLen < 100 {
		return fmt.Errorf("limits.max_prompt_len must be at least 100")
	}
	return nil
}
