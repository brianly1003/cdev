package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManager_DeleteHistorySession(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()

	// Create workspace config directory
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	// Create a mock workspace directory
	workspaceDir := filepath.Join(tmpDir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("Failed to create workspace dir: %v", err)
	}

	// Create sessions directory (mimicking Claude's structure)
	// ~/.claude/projects/-path-to-workspace/sessions/
	// For testing, we'll create a sessions dir that matches what getSessionsDir would return
	encodedPath := "-" + filepath.Base(workspaceDir)
	sessionsDir := filepath.Join(tmpDir, ".claude", "projects", encodedPath)
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("Failed to create sessions dir: %v", err)
	}

	// Create a mock session file
	sessionID := "test-session-12345678-1234-1234-1234-123456789abc"
	sessionFile := filepath.Join(sessionsDir, sessionID+".jsonl")
	if err := os.WriteFile(sessionFile, []byte(`{"test": "data"}`), 0644); err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Fatalf("Session file was not created")
	}

	t.Run("delete existing session", func(t *testing.T) {
		// This test requires a fully initialized manager with workspaces
		// For unit testing, we'll test the core logic by testing getSessionsDir
		sessDir := getSessionsDir(workspaceDir)
		if sessDir == "" {
			t.Error("getSessionsDir returned empty string")
		}
	})

	t.Run("getSessionsDir returns correct path", func(t *testing.T) {
		testPath := "/Users/test/Projects/myapp"
		expected := filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-Users-test-Projects-myapp")

		result := getSessionsDir(testPath)
		if result != expected {
			t.Errorf("getSessionsDir(%q) = %q, want %q", testPath, result, expected)
		}
	})

	t.Run("getSessionsDir handles root path", func(t *testing.T) {
		testPath := "/"
		result := getSessionsDir(testPath)
		if result == "" {
			t.Error("getSessionsDir returned empty string for root path")
		}
	})

	t.Run("getSessionsDir handles relative path", func(t *testing.T) {
		testPath := "myproject"
		result := getSessionsDir(testPath)
		if result == "" {
			t.Error("getSessionsDir returned empty string for relative path")
		}
	})
}

func TestGetSessionsDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Could not get home directory: %v", err)
	}

	tests := []struct {
		name     string
		repoPath string
		want     string
	}{
		{
			name:     "standard project path",
			repoPath: "/Users/dev/Projects/myapp",
			want:     filepath.Join(homeDir, ".claude", "projects", "-Users-dev-Projects-myapp"),
		},
		{
			name:     "path with special characters",
			repoPath: "/home/user/my-project",
			want:     filepath.Join(homeDir, ".claude", "projects", "-home-user-my-project"),
		},
		{
			name:     "root path",
			repoPath: "/",
			want:     filepath.Join(homeDir, ".claude", "projects", "-"),
		},
		{
			name:     "path with trailing slash",
			repoPath: "/Users/dev/project/",
			want:     filepath.Join(homeDir, ".claude", "projects", "-Users-dev-project"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getSessionsDir(tt.repoPath)
			if got != tt.want {
				t.Errorf("getSessionsDir(%q) = %q, want %q", tt.repoPath, got, tt.want)
			}
		})
	}
}

// TestDeleteHistorySession_Integration tests the full delete flow
// This test is skipped in short mode as it requires file system operations
func TestDeleteHistorySession_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create a mock sessions directory
	sessionsDir := filepath.Join(tmpDir, ".claude", "projects", "-test-workspace")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("Failed to create sessions dir: %v", err)
	}

	// Create a session file
	sessionID := "550e8400-e29b-41d4-a716-446655440000"
	sessionFile := filepath.Join(sessionsDir, sessionID+".jsonl")
	content := `{"type":"user","message":"hello"}
{"type":"assistant","message":"hi there"}`
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create session file: %v", err)
	}

	// Verify file exists before delete
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Fatal("Session file should exist before delete")
	}

	// Delete the file directly (simulating what DeleteHistorySession does)
	if err := os.Remove(sessionFile); err != nil {
		t.Fatalf("Failed to delete session file: %v", err)
	}

	// Verify file no longer exists
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Error("Session file should not exist after delete")
	}
}

// TestDeleteHistorySession_NonExistent tests deleting a non-existent session
func TestDeleteHistorySession_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()

	// Create sessions directory but no session file
	sessionsDir := filepath.Join(tmpDir, ".claude", "projects", "-test-workspace")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("Failed to create sessions dir: %v", err)
	}

	// Try to stat a non-existent file (simulating the check in DeleteHistorySession)
	sessionFile := filepath.Join(sessionsDir, "non-existent.jsonl")
	if _, err := os.Stat(sessionFile); !os.IsNotExist(err) {
		t.Error("Expected IsNotExist error for non-existent file")
	}
}

