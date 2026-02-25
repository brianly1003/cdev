// Package security provides authentication and authorization for cdev.
package security

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const authRegistryVersion = 1

// DeviceSession tracks the latest issued tokens for a device.
type DeviceSession struct {
	DeviceID         string    `json:"device_id"`
	RefreshNonce     string    `json:"refresh_nonce"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	AccessNonce      string    `json:"access_nonce"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// AuthRegistry tracks device sessions and workspace bindings.
// It persists refresh/access token nonces so tokens can be revoked on logout.
type AuthRegistry struct {
	mu sync.Mutex

	path string `json:"-"`

	Version    int                             `json:"version"`
	Devices    map[string]*DeviceSession       `json:"devices"`
	Workspaces map[string]map[string]time.Time `json:"workspaces"` // workspaceID -> deviceID -> boundAt
}

// LoadAuthRegistry loads the auth registry from disk or returns an empty registry.
func LoadAuthRegistry(path string) (*AuthRegistry, error) {
	registry := &AuthRegistry{
		path:       path,
		Version:    authRegistryVersion,
		Devices:    make(map[string]*DeviceSession),
		Workspaces: make(map[string]map[string]time.Time),
	}

	if path == "" {
		return registry, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return registry, nil
		}
		return registry, err
	}

	var stored AuthRegistry
	if err := json.Unmarshal(data, &stored); err != nil {
		return registry, err
	}

	if stored.Version != authRegistryVersion {
		// Ignore incompatible versions but keep empty registry.
		return registry, nil
	}

	registry.Version = stored.Version
	if stored.Devices != nil {
		registry.Devices = stored.Devices
	}
	if stored.Workspaces != nil {
		registry.Workspaces = stored.Workspaces
	}

	return registry, nil
}

// RegisterDevice registers or updates a device session with its latest tokens.
func (r *AuthRegistry) RegisterDevice(deviceID, refreshNonce string, refreshExpiry time.Time, accessNonce string, accessExpiry time.Time) error {
	if deviceID == "" {
		return fmt.Errorf("device_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.Devices[deviceID] = &DeviceSession{
		DeviceID:         deviceID,
		RefreshNonce:     refreshNonce,
		RefreshExpiresAt: refreshExpiry,
		AccessNonce:      accessNonce,
		AccessExpiresAt:  accessExpiry,
		UpdatedAt:        time.Now().UTC(),
	}

	return r.saveLocked()
}

// BindWorkspace associates a workspace with a device.
func (r *AuthRegistry) BindWorkspace(deviceID, workspaceID string) error {
	if deviceID == "" {
		return fmt.Errorf("device_id is required")
	}
	if workspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.Devices[deviceID]; !ok {
		return fmt.Errorf("device not registered: %s", deviceID)
	}

	if _, ok := r.Workspaces[workspaceID]; !ok {
		r.Workspaces[workspaceID] = make(map[string]time.Time)
	}
	r.Workspaces[workspaceID][deviceID] = time.Now().UTC()

	return r.saveLocked()
}

// UnbindWorkspace removes all device bindings for a workspace.
func (r *AuthRegistry) UnbindWorkspace(workspaceID string) error {
	if workspaceID == "" {
		return fmt.Errorf("workspace_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.Workspaces, workspaceID)
	return r.saveLocked()
}

// RemoveDevice removes a device session and returns any orphaned workspaces.
func (r *AuthRegistry) RemoveDevice(deviceID string) ([]string, error) {
	if deviceID == "" {
		return nil, fmt.Errorf("device_id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.Devices, deviceID)
	orphaned := r.pruneDeviceBindingsLocked(deviceID)

	if err := r.saveLocked(); err != nil {
		return orphaned, err
	}
	return orphaned, nil
}

// PruneExpired removes devices with expired refresh tokens and returns orphaned workspaces.
func (r *AuthRegistry) PruneExpired(now time.Time) ([]string, []string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	expiredDevices := make([]string, 0)
	orphaned := make([]string, 0)

	for deviceID, session := range r.Devices {
		if session == nil || session.RefreshExpiresAt.IsZero() || !session.RefreshExpiresAt.After(now) {
			expiredDevices = append(expiredDevices, deviceID)
		}
	}

	for _, deviceID := range expiredDevices {
		delete(r.Devices, deviceID)
		orphaned = append(orphaned, r.pruneDeviceBindingsLocked(deviceID)...)
	}

	if len(expiredDevices) == 0 {
		return nil, nil, nil
	}

	if err := r.saveLocked(); err != nil {
		return expiredDevices, orphaned, err
	}

	return expiredDevices, orphaned, nil
}

// IsRefreshNonceValid reports whether nonce is the currently registered
// refresh nonce for any device. Returns false for unknown or removed devices.
func (r *AuthRegistry) IsRefreshNonceValid(deviceID, nonce string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.Devices[deviceID]
	if !ok || session == nil {
		return false
	}
	return session.RefreshNonce == nonce
}

// GetDevice returns the device session, if any.
func (r *AuthRegistry) GetDevice(deviceID string) (*DeviceSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	session, ok := r.Devices[deviceID]
	return session, ok
}

func (r *AuthRegistry) pruneDeviceBindingsLocked(deviceID string) []string {
	orphaned := make([]string, 0)
	for workspaceID, devices := range r.Workspaces {
		if devices == nil {
			delete(r.Workspaces, workspaceID)
			orphaned = append(orphaned, workspaceID)
			continue
		}

		delete(devices, deviceID)
		if len(devices) == 0 {
			delete(r.Workspaces, workspaceID)
			orphaned = append(orphaned, workspaceID)
		}
	}
	return orphaned
}

func (r *AuthRegistry) saveLocked() error {
	if r.path == "" {
		return nil
	}

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(r.path, data, 0600)
}
