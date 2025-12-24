package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/brianly1003/cdev/internal/workspace"
)

// WorkspaceClient provides JSON-RPC methods for workspace operations.
type WorkspaceClient struct {
	client *Client
}

// NewWorkspaceClient creates a new workspace client connected via JSON-RPC.
func NewWorkspaceClient(url string) (*WorkspaceClient, error) {
	client, err := NewClient(url)
	if err != nil {
		return nil, err
	}
	return &WorkspaceClient{client: client}, nil
}

// List returns all workspaces.
func (wc *WorkspaceClient) List(ctx context.Context) ([]workspace.WorkspaceInfo, error) {
	resp, err := wc.client.Call(ctx, "workspace/list", nil)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result struct {
		Workspaces []workspace.WorkspaceInfo `json:"workspaces"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Workspaces, nil
}

// Get returns a specific workspace by ID.
func (wc *WorkspaceClient) Get(ctx context.Context, id string) (*workspace.WorkspaceInfo, error) {
	params := map[string]string{"id": id}
	resp, err := wc.client.Call(ctx, "workspace/get", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var info workspace.WorkspaceInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// Add creates a new workspace.
func (wc *WorkspaceClient) Add(ctx context.Context, name, path string, autoStart bool, port int) (*workspace.WorkspaceInfo, error) {
	params := map[string]interface{}{
		"name":       name,
		"path":       path,
		"auto_start": autoStart,
		"port":       port,
	}

	resp, err := wc.client.Call(ctx, "workspace/add", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var info workspace.WorkspaceInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// Remove deletes a workspace.
func (wc *WorkspaceClient) Remove(ctx context.Context, id string) error {
	params := map[string]string{"id": id}
	resp, err := wc.client.Call(ctx, "workspace/remove", params)
	if err != nil {
		return err
	}

	if resp.Error != nil {
		return fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	return nil
}

// Start starts a workspace.
func (wc *WorkspaceClient) Start(ctx context.Context, id string) (*workspace.WorkspaceInfo, error) {
	params := map[string]string{"id": id}
	resp, err := wc.client.Call(ctx, "workspace/start", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var info workspace.WorkspaceInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// Stop stops a workspace.
func (wc *WorkspaceClient) Stop(ctx context.Context, id string) (*workspace.WorkspaceInfo, error) {
	params := map[string]string{"id": id}
	resp, err := wc.client.Call(ctx, "workspace/stop", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var info workspace.WorkspaceInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// Restart restarts a workspace.
func (wc *WorkspaceClient) Restart(ctx context.Context, id string) (*workspace.WorkspaceInfo, error) {
	params := map[string]string{"id": id}
	resp, err := wc.client.Call(ctx, "workspace/restart", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var info workspace.WorkspaceInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &info, nil
}

// Discover scans for Git repositories.
func (wc *WorkspaceClient) Discover(ctx context.Context, paths []string) ([]workspace.DiscoveredRepo, error) {
	params := map[string]interface{}{
		"paths": paths,
	}

	resp, err := wc.client.Call(ctx, "workspace/discover", params)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("RPC error: %s", resp.Error.Message)
	}

	var result struct {
		Repositories []workspace.DiscoveredRepo `json:"repositories"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Repositories, nil
}

// Close closes the underlying connection.
func (wc *WorkspaceClient) Close() error {
	return wc.client.Close()
}

// WithTimeout is a helper to create a context with timeout.
func WithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
