// Package workspace provides workspace configuration management.
package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// ConfigManager handles workspace configuration CRUD operations.
// This is the simplified version that only manages configs, not processes.
type ConfigManager struct {
	workspaces        map[string]*Workspace // keyed by workspace ID
	configPath        string
	gitTrackerManager *GitTrackerManager
	repoDiscovery     *RepoDiscovery
	mu                sync.RWMutex
}

// NewConfigManager creates a new workspace config manager.
func NewConfigManager(cfg *config.WorkspacesConfig, configPath string) *ConfigManager {
	m := &ConfigManager{
		workspaces:    make(map[string]*Workspace),
		configPath:    configPath,
		repoDiscovery: NewRepoDiscovery(DefaultDiscoveryConfig()),
	}

	// Load workspaces from config
	for _, def := range cfg.Workspaces {
		ws := NewWorkspace(def)
		m.workspaces[def.ID] = ws
	}

	// Update discovery engine with configured paths
	m.updateDiscoveryConfiguredPaths()

	return m
}

// updateDiscoveryConfiguredPaths updates the discovery engine with currently configured paths.
func (m *ConfigManager) updateDiscoveryConfiguredPaths() {
	configuredPaths := make(map[string]bool)
	for _, ws := range m.workspaces {
		configuredPaths[ws.Definition.Path] = true
	}
	m.repoDiscovery.SetConfiguredPaths(configuredPaths)
}

// SetGitTrackerManager sets the git tracker manager and registers all existing workspaces.
func (m *ConfigManager) SetGitTrackerManager(gtm *GitTrackerManager) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gitTrackerManager = gtm

	// Register all existing workspaces with git tracker manager
	for _, ws := range m.workspaces {
		// Log but don't fail - workspace may have invalid path
		// The error will be detected when git operations are attempted
		_ = gtm.RegisterWorkspace(&ws.Definition)
	}
}

// GetGitTrackerManager returns the git tracker manager.
func (m *ConfigManager) GetGitTrackerManager() *GitTrackerManager {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.gitTrackerManager
}

// ListWorkspaces returns all configured workspaces.
func (m *ConfigManager) ListWorkspaces() []*Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		result = append(result, ws)
	}
	return result
}

// GetWorkspace returns a workspace by ID.
func (m *ConfigManager) GetWorkspace(id string) (*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ws, ok := m.workspaces[id]
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", id)
	}
	return ws, nil
}

// AddWorkspace adds a new workspace configuration.
// This is a convenience wrapper around AddWorkspaceWithOptions with createIfMissing=false.
func (m *ConfigManager) AddWorkspace(name, path string, autoStart bool) (*Workspace, error) {
	return m.AddWorkspaceWithOptions(name, path, autoStart, false)
}

// AddWorkspaceWithOptions adds a new workspace configuration with additional options.
// If createIfMissing is true and the path doesn't exist, it will be created.
// This supports the iOS app flow where users can create new project folders.
func (m *ConfigManager) AddWorkspaceWithOptions(name, path string, autoStart bool, createIfMissing bool) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Resolve absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// Verify path exists and is a directory, or create it if missing
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			if createIfMissing {
				// Create the directory
				if err := os.MkdirAll(absPath, 0755); err != nil {
					return nil, fmt.Errorf("failed to create directory: %w", err)
				}
			} else {
				return nil, fmt.Errorf("path does not exist: %s", absPath)
			}
		} else {
			return nil, fmt.Errorf("failed to access path: %w", err)
		}
	} else if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", absPath)
	}

	// Check for duplicate path
	for _, ws := range m.workspaces {
		if ws.Definition.Path == absPath {
			return nil, fmt.Errorf("workspace already exists for path: %s", absPath)
		}
	}

	// Create workspace definition
	def := config.WorkspaceDefinition{
		ID:        uuid.New().String(),
		Name:      name,
		Path:      absPath,
		AutoStart: autoStart,
		CreatedAt: time.Now().UTC(),
	}

	ws := NewWorkspace(def)
	m.workspaces[def.ID] = ws

	// Register with git tracker manager (if available)
	if m.gitTrackerManager != nil {
		// Log but don't fail - git tracking is optional
		// Non-git repos are still valid workspaces
		_ = m.gitTrackerManager.RegisterWorkspace(&def)
	}

	// Save config
	if err := m.saveConfig(); err != nil {
		// Rollback
		delete(m.workspaces, def.ID)
		if m.gitTrackerManager != nil {
			m.gitTrackerManager.UnregisterWorkspace(def.ID)
		}
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	// Update configured paths map so enrichWithConfiguredStatus() marks this repo as configured
	// Note: We don't invalidate the cache - isConfigured is applied dynamically, not stored in cache
	m.updateDiscoveryConfiguredPaths()

	return ws, nil
}

// RemoveWorkspace removes a workspace configuration.
func (m *ConfigManager) RemoveWorkspace(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, ok := m.workspaces[id]
	if !ok {
		return fmt.Errorf("workspace not found: %s", id)
	}

	// Unregister from git tracker manager
	if m.gitTrackerManager != nil {
		m.gitTrackerManager.UnregisterWorkspace(id)
	}

	delete(m.workspaces, id)

	// Save config
	if err := m.saveConfig(); err != nil {
		// Rollback
		m.workspaces[id] = ws
		if m.gitTrackerManager != nil {
			_ = m.gitTrackerManager.RegisterWorkspace(&ws.Definition)
		}
		return fmt.Errorf("failed to save config: %w", err)
	}

	// Update configured paths map so enrichWithConfiguredStatus() no longer marks this repo as configured
	m.updateDiscoveryConfiguredPaths()

	return nil
}

// UpdateWorkspace updates a workspace configuration.
func (m *ConfigManager) UpdateWorkspace(ws *Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.workspaces[ws.Definition.ID]; !ok {
		return fmt.Errorf("workspace not found: %s", ws.Definition.ID)
	}

	m.workspaces[ws.Definition.ID] = ws

	// Save config
	if err := m.saveConfig(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

// DiscoverRepositories scans directories for git repositories using the enterprise discovery engine.
// Returns cached results immediately if available, with background refresh for stale cache.
func (m *ConfigManager) DiscoverRepositories(searchPaths []string) ([]DiscoveredRepo, error) {
	// Update configured paths before discovery
	m.mu.RLock()
	m.updateDiscoveryConfiguredPaths()
	m.mu.RUnlock()

	// Use the new discovery engine
	result, err := m.repoDiscovery.Discover(context.Background(), searchPaths)
	if err != nil {
		return nil, err
	}

	return result.Repositories, nil
}

// DiscoverRepositoriesWithResult returns the full discovery result including cache metadata.
func (m *ConfigManager) DiscoverRepositoriesWithResult(searchPaths []string) (*DiscoveryResult, error) {
	// Update configured paths before discovery
	m.mu.RLock()
	m.updateDiscoveryConfiguredPaths()
	m.mu.RUnlock()

	return m.repoDiscovery.Discover(context.Background(), searchPaths)
}

// DiscoverRepositoriesFresh forces a fresh scan, ignoring cache.
func (m *ConfigManager) DiscoverRepositoriesFresh(searchPaths []string) (*DiscoveryResult, error) {
	// Update configured paths before discovery
	m.mu.RLock()
	m.updateDiscoveryConfiguredPaths()
	m.mu.RUnlock()

	return m.repoDiscovery.DiscoverFresh(context.Background(), searchPaths)
}

// InvalidateDiscoveryCache clears the discovery cache.
func (m *ConfigManager) InvalidateDiscoveryCache() error {
	return m.repoDiscovery.InvalidateCache()
}

// saveConfig saves the current workspace configuration to file.
func (m *ConfigManager) saveConfig() error {
	if m.configPath == "" {
		return nil // No config file specified
	}

	// Build config
	cfg := &config.WorkspacesConfig{
		Workspaces: make([]config.WorkspaceDefinition, 0, len(m.workspaces)),
	}

	// Preserve manager/defaults from existing config (or fall back to defaults)
	existingCfg, err := config.LoadWorkspaces(m.configPath)
	if err == nil {
		cfg.Manager = existingCfg.Manager
		cfg.Defaults = existingCfg.Defaults
	} else {
		defaults := config.DefaultWorkspacesConfig()
		cfg.Manager = defaults.Manager
		cfg.Defaults = defaults.Defaults
	}

	for _, ws := range m.workspaces {
		cfg.Workspaces = append(cfg.Workspaces, ws.Definition)
	}

	if err := config.SaveWorkspaces(m.configPath, cfg); err != nil {
		log.Warn().Err(err).Str("workspaces_path", m.configPath).Msg("failed to save workspaces config")
		return err
	}
	log.Info().
		Str("workspaces_path", m.configPath).
		Int("workspaces_saved", len(cfg.Workspaces)).
		Msg("workspaces config saved")
	return nil
}

// GetAllWorkspaceIDs returns all workspace IDs.
func (m *ConfigManager) GetAllWorkspaceIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.workspaces))
	for id := range m.workspaces {
		ids = append(ids, id)
	}
	return ids
}
