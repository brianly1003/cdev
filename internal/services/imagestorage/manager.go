// Package imagestorage provides image storage management for cdev.
package imagestorage

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// WorkspacePathResolver is an interface for looking up workspace paths by ID.
// This decouples the ImageStorageManager from the workspace package.
type WorkspacePathResolver interface {
	// GetWorkspacePath returns the absolute path for a workspace ID.
	// Returns an error if the workspace is not found.
	GetWorkspacePath(workspaceID string) (string, error)
}

// Manager manages per-workspace image storage instances.
// It creates storage on demand when a workspace is first accessed.
type Manager struct {
	resolver WorkspacePathResolver
	storages map[string]*Storage // workspace_id -> storage
	mu       sync.RWMutex
}

// NewManager creates a new ImageStorageManager.
func NewManager(resolver WorkspacePathResolver) *Manager {
	return &Manager{
		resolver: resolver,
		storages: make(map[string]*Storage),
	}
}

// GetStorage returns the image storage for a workspace.
// Creates the storage on demand if it doesn't exist.
func (m *Manager) GetStorage(workspaceID string) (*Storage, error) {
	// Fast path: check if storage already exists
	m.mu.RLock()
	storage, exists := m.storages[workspaceID]
	m.mu.RUnlock()

	if exists {
		return storage, nil
	}

	// Slow path: create new storage
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if storage, exists = m.storages[workspaceID]; exists {
		return storage, nil
	}

	// Look up workspace path
	path, err := m.resolver.GetWorkspacePath(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("workspace not found: %w", err)
	}

	// Create storage for this workspace
	storage, err = New(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create image storage for workspace %s: %w", workspaceID, err)
	}

	m.storages[workspaceID] = storage
	log.Info().
		Str("workspace_id", workspaceID).
		Str("path", path).
		Msg("created image storage for workspace")

	return storage, nil
}

// CloseWorkspace closes and removes the storage for a specific workspace.
// Use this when a workspace is removed.
func (m *Manager) CloseWorkspace(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if storage, exists := m.storages[workspaceID]; exists {
		storage.Close()
		delete(m.storages, workspaceID)
		log.Info().Str("workspace_id", workspaceID).Msg("closed image storage for workspace")
	}
}

// Close closes all storage instances.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, storage := range m.storages {
		storage.Close()
		log.Debug().Str("workspace_id", id).Msg("closed image storage")
	}
	m.storages = make(map[string]*Storage)
}

// ListWorkspaces returns the IDs of workspaces that have active storage.
func (m *Manager) ListWorkspaces() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.storages))
	for id := range m.storages {
		ids = append(ids, id)
	}
	return ids
}
