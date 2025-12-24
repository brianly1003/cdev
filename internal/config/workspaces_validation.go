package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateWorkspaces validates the workspaces configuration
func ValidateWorkspaces(cfg *WorkspacesConfig) error {
	if err := validateWorkspaceManager(&cfg.Manager); err != nil {
		return err
	}

	if err := validateWorkspaceDefinitions(cfg.Workspaces, &cfg.Manager); err != nil {
		return err
	}

	return nil
}

func validateWorkspaceManager(cfg *WorkspaceManagerConfig) error {
	// Port validation
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("manager.port must be between 1 and 65535")
	}

	// Port range validation
	if cfg.PortRangeStart < 1 || cfg.PortRangeStart > 65535 {
		return fmt.Errorf("manager.port_range_start must be between 1 and 65535")
	}
	if cfg.PortRangeEnd < 1 || cfg.PortRangeEnd > 65535 {
		return fmt.Errorf("manager.port_range_end must be between 1 and 65535")
	}
	if cfg.PortRangeStart >= cfg.PortRangeEnd {
		return fmt.Errorf("manager.port_range_start must be less than port_range_end")
	}

	// Manager port must not overlap with workspace range
	if cfg.Port >= cfg.PortRangeStart && cfg.Port <= cfg.PortRangeEnd {
		return fmt.Errorf("manager.port (%d) must not be in workspace port range [%d-%d]",
			cfg.Port, cfg.PortRangeStart, cfg.PortRangeEnd)
	}

	// Resource limits
	if cfg.MaxConcurrentWorkspaces < 1 {
		return fmt.Errorf("manager.max_concurrent_workspaces must be at least 1")
	}
	if cfg.MaxConcurrentWorkspaces > 100 {
		return fmt.Errorf("manager.max_concurrent_workspaces cannot exceed 100")
	}

	portRange := cfg.PortRangeEnd - cfg.PortRangeStart + 1
	if portRange < cfg.MaxConcurrentWorkspaces {
		return fmt.Errorf("port range (%d ports) must accommodate max_concurrent_workspaces (%d)",
			portRange, cfg.MaxConcurrentWorkspaces)
	}

	if cfg.AutoStopIdleMinutes < 0 {
		return fmt.Errorf("manager.auto_stop_idle_minutes cannot be negative")
	}

	if cfg.MaxRestartAttempts < 0 {
		return fmt.Errorf("manager.max_restart_attempts cannot be negative")
	}

	if cfg.RestartBackoffSeconds < 0 {
		return fmt.Errorf("manager.restart_backoff_seconds cannot be negative")
	}

	return nil
}

func validateWorkspaceDefinitions(workspaces []WorkspaceDefinition, managerCfg *WorkspaceManagerConfig) error {
	if len(workspaces) > 100 {
		return fmt.Errorf("cannot have more than 100 workspaces")
	}

	ids := make(map[string]bool)
	names := make(map[string]bool)
	ports := make(map[int]bool)
	paths := make(map[string]bool)

	for i, ws := range workspaces {
		// ID validation
		if ws.ID == "" {
			return fmt.Errorf("workspace %d: id cannot be empty", i)
		}
		if ids[ws.ID] {
			return fmt.Errorf("workspace %d: duplicate id: %s", i, ws.ID)
		}
		ids[ws.ID] = true

		// Name validation
		if ws.Name == "" {
			return fmt.Errorf("workspace %d: name cannot be empty", i)
		}
		if names[ws.Name] {
			return fmt.Errorf("workspace %d: duplicate name: %s", i, ws.Name)
		}
		names[ws.Name] = true

		// Path validation
		if ws.Path == "" {
			return fmt.Errorf("workspace %d: path cannot be empty", i)
		}

		info, err := os.Stat(ws.Path)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("workspace %d: path does not exist: %s", i, ws.Path)
			}
			return fmt.Errorf("workspace %d: error accessing path: %w", i, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("workspace %d: path is not a directory: %s", i, ws.Path)
		}

		// Check for path conflicts (no parent/child relationships)
		for existingPath := range paths {
			if isParentOrChild(ws.Path, existingPath) {
				return fmt.Errorf("workspace %d: path %s conflicts with existing workspace path %s (cannot be parent or child)",
					i, ws.Path, existingPath)
			}
		}
		paths[ws.Path] = true

		// Port validation
		if ws.Port < 0 || ws.Port > 65535 {
			return fmt.Errorf("workspace %d: port must be between 0 and 65535", i)
		}
		if ws.Port != 0 {
			if ws.Port < managerCfg.PortRangeStart || ws.Port > managerCfg.PortRangeEnd {
				return fmt.Errorf("workspace %d: port %d outside allowed range [%d-%d]",
					i, ws.Port, managerCfg.PortRangeStart, managerCfg.PortRangeEnd)
			}
			if ws.Port == managerCfg.Port {
				return fmt.Errorf("workspace %d: port %d conflicts with manager port", i, ws.Port)
			}
			if ports[ws.Port] {
				return fmt.Errorf("workspace %d: duplicate port: %d", i, ws.Port)
			}
			ports[ws.Port] = true
		}

		// Config file validation (if specified)
		if ws.ConfigFile != "" {
			if _, err := os.Stat(ws.ConfigFile); err != nil {
				return fmt.Errorf("workspace %d: config file does not exist: %s", i, ws.ConfigFile)
			}
		}
	}

	return nil
}

// isParentOrChild checks if path1 is a parent or child of path2
func isParentOrChild(path1, path2 string) bool {
	path1Clean := filepath.Clean(path1)
	path2Clean := filepath.Clean(path2)

	// Check if path1 is parent of path2
	if strings.HasPrefix(path2Clean+string(filepath.Separator), path1Clean+string(filepath.Separator)) {
		return true
	}

	// Check if path2 is parent of path1
	if strings.HasPrefix(path1Clean+string(filepath.Separator), path2Clean+string(filepath.Separator)) {
		return true
	}

	return false
}
