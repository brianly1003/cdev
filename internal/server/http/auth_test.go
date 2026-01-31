package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/brianly1003/cdev/internal/security"
)

func TestAuthHandler_HandleRevoke_RemovesDeviceAndReturnsOrphaned(t *testing.T) {
	tempDir := t.TempDir()
	secretPath := filepath.Join(tempDir, "token_secret.json")
	registryPath := filepath.Join(tempDir, "auth_registry.json")

	tm, err := security.NewTokenManagerWithPath(3600, secretPath)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}

	registry, err := security.LoadAuthRegistry(registryPath)
	if err != nil {
		t.Fatalf("failed to load auth registry: %v", err)
	}

	deviceID := "device-1"
	pair, err := tm.GenerateTokenPairWithDeviceID(deviceID)
	if err != nil {
		t.Fatalf("failed to generate token pair: %v", err)
	}

	if err := registry.RegisterDevice(deviceID, pair.RefreshNonce, pair.RefreshTokenExpiry, pair.AccessNonce, pair.AccessTokenExpiry); err != nil {
		t.Fatalf("failed to register device: %v", err)
	}

	workspaceID := "workspace-1"
	if err := registry.BindWorkspace(deviceID, workspaceID); err != nil {
		t.Fatalf("failed to bind workspace: %v", err)
	}

	var orphanedFromCallback []string
	handler := NewAuthHandler(tm, registry, func(ids []string, reason string) {
		orphanedFromCallback = ids
	})

	body, err := json.Marshal(TokenRevokeRequest{RefreshToken: pair.RefreshToken})
	if err != nil {
		t.Fatalf("failed to encode request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/revoke", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.HandleRevoke(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	var resp struct {
		Success            bool     `json:"success"`
		OrphanedWorkspaces []string `json:"orphaned_workspaces"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected success response")
	}
	if len(resp.OrphanedWorkspaces) != 1 || resp.OrphanedWorkspaces[0] != workspaceID {
		t.Fatalf("unexpected orphaned workspaces: %#v", resp.OrphanedWorkspaces)
	}
	if len(orphanedFromCallback) != 1 || orphanedFromCallback[0] != workspaceID {
		t.Fatalf("expected orphaned callback for %s, got %#v", workspaceID, orphanedFromCallback)
	}

	if _, ok := registry.GetDevice(deviceID); ok {
		t.Fatalf("expected device to be removed from registry")
	}
}
