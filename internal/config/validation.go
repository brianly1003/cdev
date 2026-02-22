package config

import (
	"fmt"
	"net"
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

	if err := validateSecurity(&cfg.Security, &cfg.Server); err != nil {
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

func validateSecurity(cfg *SecurityConfig, serverCfg *ServerConfig) error {
	if cfg == nil || serverCfg == nil {
		return nil
	}

	if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			return fmt.Errorf("security.tls_cert_file and security.tls_key_file must both be set")
		}

		if err := validateExistingFile(cfg.TLSCertFile, "security.tls_cert_file"); err != nil {
			return err
		}
		if err := validateExistingFile(cfg.TLSKeyFile, "security.tls_key_file"); err != nil {
			return err
		}
	}

	if cfg.RequireSecureTransport && !cfg.BindLocalhostOnly {
		if cfg.TLSCertFile == "" || cfg.TLSKeyFile == "" {
			if serverCfg.ExternalURL == "" {
				return fmt.Errorf("security.require_secure_transport is true and security.bind_localhost_only is false; configure security.tls_cert_file and security.tls_key_file, or set server.external_url to an https URL")
			}

			if err := validateExternalURL(serverCfg.ExternalURL, "server.external_url", []string{"https"}); err != nil {
				return err
			}
		}
	}

	if err := validateTrustedProxies(cfg.TrustedProxies); err != nil {
		return err
	}

	return nil
}

func validateExistingFile(path, fieldName string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist: %s", fieldName, path)
		}
		return fmt.Errorf("unable to access %s (%s): %w", fieldName, path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%s must be a file, not a directory: %s", fieldName, path)
	}

	return nil
}

func validateTrustedProxies(trustedProxies []string) error {
	for _, proxy := range trustedProxies {
		trimmed := strings.TrimSpace(proxy)
		if trimmed == "" {
			return fmt.Errorf("security.trusted_proxies contains an empty value")
		}

		if net.ParseIP(trimmed) != nil {
			continue
		}

		if _, _, err := net.ParseCIDR(trimmed); err != nil {
			return fmt.Errorf("security.trusted_proxies has invalid CIDR/IP value: %s", trimmed)
		}
	}

	return nil
}

func validateServer(cfg *ServerConfig) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535")
	}
	if cfg.Host == "" {
		return fmt.Errorf("server.host cannot be empty")
	}

	// Validate external URL if provided
	if cfg.ExternalURL != "" {
		if err := validateExternalURL(cfg.ExternalURL, "server.external_url", []string{"http", "https"}); err != nil {
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
	// repository.path is optional - workspaces are managed via workspace/add API
	if cfg.Path == "" {
		return nil
	}

	// Only validate if path is explicitly configured
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
