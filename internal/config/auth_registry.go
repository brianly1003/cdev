package config

import (
	"os"
	"path/filepath"
)

// DefaultAuthRegistryPath returns the default path for auth registry storage.
func DefaultAuthRegistryPath() string {
	configDir, err := GetConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".cdev", "auth_registry.json")
	}
	return filepath.Join(configDir, "auth_registry.json")
}
