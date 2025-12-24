// Package workspace provides workspace configuration management.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/google/uuid"
)

// ConfigManager handles workspace configuration CRUD operations.
// This is the simplified version that only manages configs, not processes.
type ConfigManager struct {
	workspaces        map[string]*Workspace // keyed by workspace ID
	configPath        string
	gitTrackerManager *GitTrackerManager
	mu                sync.RWMutex
}

// NewConfigManager creates a new workspace config manager.
func NewConfigManager(cfg *config.WorkspacesConfig, configPath string) *ConfigManager {
	m := &ConfigManager{
		workspaces: make(map[string]*Workspace),
		configPath: configPath,
	}

	// Load workspaces from config
	for _, def := range cfg.Workspaces {
		ws := NewWorkspace(def)
		m.workspaces[def.ID] = ws
	}

	return m
}

// SetGitTrackerManager sets the git tracker manager and registers all existing workspaces.
func (m *ConfigManager) SetGitTrackerManager(gtm *GitTrackerManager) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gitTrackerManager = gtm

	// Register all existing workspaces with git tracker manager
	for _, ws := range m.workspaces {
		if err := gtm.RegisterWorkspace(&ws.Definition); err != nil {
			// Log but don't fail - workspace may have invalid path
			// The error will be detected when git operations are attempted
		}
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
func (m *ConfigManager) AddWorkspace(name, path string, autoStart bool) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Resolve absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	// Verify path exists and is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("path does not exist: %s", absPath)
		}
		return nil, fmt.Errorf("failed to access path: %w", err)
	}
	if !info.IsDir() {
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
		if err := m.gitTrackerManager.RegisterWorkspace(&def); err != nil {
			// Log but don't fail - git tracking is optional
			// Non-git repos are still valid workspaces
		}
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
			m.gitTrackerManager.RegisterWorkspace(&ws.Definition)
		}
		return fmt.Errorf("failed to save config: %w", err)
	}

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

// DiscoverRepositories scans directories for git repositories.
func (m *ConfigManager) DiscoverRepositories(searchPaths []string) ([]DiscoveredRepo, error) {
	// Use default search paths if none provided
	if len(searchPaths) == 0 {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		searchPaths = []string{
			filepath.Join(homeDir, "Projects"),
			filepath.Join(homeDir, "Code"),
			filepath.Join(homeDir, "Desktop"),
			filepath.Join(homeDir, "Documents"),
		}
	}

	discovered := make([]DiscoveredRepo, 0)
	seen := make(map[string]bool)

	// Get list of configured workspace paths for filtering
	m.mu.RLock()
	configuredPaths := make(map[string]bool)
	for _, ws := range m.workspaces {
		configuredPaths[ws.Definition.Path] = true
	}
	m.mu.RUnlock()

	for _, searchPath := range searchPaths {
		// Skip if path doesn't exist
		if _, err := os.Stat(searchPath); err != nil {
			continue
		}

		// Walk directory tree
		filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip errors
			}

			// Check if this is a .git directory
			if info.IsDir() && info.Name() == ".git" {
				repoPath := filepath.Dir(path)

				// Skip if already seen
				if seen[repoPath] {
					return filepath.SkipDir
				}

				// Mark as seen
				seen[repoPath] = true

				// Get repository info
				repo := getRepoInfo(repoPath)
				repo.IsConfigured = configuredPaths[repoPath]
				discovered = append(discovered, repo)

				// Don't descend into this repository
				return filepath.SkipDir
			}

			// Skip common directories
			if info.IsDir() {
				name := info.Name()
				if name == "node_modules" || name == ".cache" || name == "vendor" || name == ".venv" {
					return filepath.SkipDir
				}
			}

			return nil
		})
	}

	return discovered, nil
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

	for _, ws := range m.workspaces {
		cfg.Workspaces = append(cfg.Workspaces, ws.Definition)
	}

	return config.SaveWorkspaces(m.configPath, cfg)
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
