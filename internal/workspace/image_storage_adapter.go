// Package workspace provides workspace configuration management.
package workspace

import (
	"fmt"
)

// ImageStoragePathResolver adapts ConfigManager to implement imagestorage.WorkspacePathResolver.
type ImageStoragePathResolver struct {
	configManager *ConfigManager
}

// NewImageStoragePathResolver creates a new resolver that uses the ConfigManager.
func NewImageStoragePathResolver(cm *ConfigManager) *ImageStoragePathResolver {
	return &ImageStoragePathResolver{configManager: cm}
}

// GetWorkspacePath returns the absolute path for a workspace ID.
func (r *ImageStoragePathResolver) GetWorkspacePath(workspaceID string) (string, error) {
	ws, err := r.configManager.GetWorkspace(workspaceID)
	if err != nil {
		return "", fmt.Errorf("workspace not found: %s", workspaceID)
	}
	return ws.Definition.Path, nil
}
