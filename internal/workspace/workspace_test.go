package workspace

import (
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
)

// --- Workspace Tests ---

func TestNewWorkspace(t *testing.T) {
	def := config.WorkspaceDefinition{
		ID:       "test-workspace",
		Name:     "Test Workspace",
		Path:     "/tmp/test",
		Port:     8080,
		AutoStart: true,
	}

	ws := NewWorkspace(def)

	if ws == nil {
		t.Fatal("NewWorkspace returned nil")
	}
	if ws.Definition.ID != def.ID {
		t.Errorf("Definition.ID = %s, want %s", ws.Definition.ID, def.ID)
	}
	if ws.Status != StatusStopped {
		t.Errorf("Status = %v, want %v", ws.Status, StatusStopped)
	}
	if ws.RestartCount != 0 {
		t.Errorf("RestartCount = %d, want 0", ws.RestartCount)
	}
	if ws.LastActive.IsZero() {
		t.Error("LastActive should be set")
	}
}

func TestWorkspace_StatusTransitions(t *testing.T) {
	tests := []struct {
		name     string
		status   WorkspaceStatus
		expected WorkspaceStatus
	}{
		{"stopped", StatusStopped, StatusStopped},
		{"starting", StatusStarting, StatusStarting},
		{"running", StatusRunning, StatusRunning},
		{"stopping", StatusStopping, StatusStopping},
		{"error", StatusError, StatusError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})
			ws.SetStatus(tt.status)

			if ws.GetStatus() != tt.expected {
				t.Errorf("GetStatus() = %v, want %v", ws.GetStatus(), tt.expected)
			}
		})
	}
}

func TestWorkspace_SetStatus_ThreadSafe(t *testing.T) {
	ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})

	var wg sync.WaitGroup
	numGoroutines := 50
	statuses := []WorkspaceStatus{StatusStopped, StatusStarting, StatusRunning, StatusStopping, StatusError}

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			status := statuses[id%len(statuses)]
			ws.SetStatus(status)
			_ = ws.GetStatus()
		}(i)
	}

	wg.Wait()
	// No panic means thread-safe
}

func TestWorkspace_SetPID(t *testing.T) {
	ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})

	if ws.GetPID() != 0 {
		t.Errorf("initial PID = %d, want 0", ws.GetPID())
	}

	ws.SetPID(12345)

	if ws.GetPID() != 12345 {
		t.Errorf("GetPID() = %d, want 12345", ws.GetPID())
	}
}

func TestWorkspace_IsRunning(t *testing.T) {
	tests := []struct {
		name     string
		status   WorkspaceStatus
		expected bool
	}{
		{"stopped is not running", StatusStopped, false},
		{"starting is running", StatusStarting, true},
		{"running is running", StatusRunning, true},
		{"stopping is not running", StatusStopping, false},
		{"error is not running", StatusError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})
			ws.SetStatus(tt.status)

			if ws.IsRunning() != tt.expected {
				t.Errorf("IsRunning() = %v, want %v", ws.IsRunning(), tt.expected)
			}
		})
	}
}

func TestWorkspace_IsStopped(t *testing.T) {
	tests := []struct {
		name     string
		status   WorkspaceStatus
		expected bool
	}{
		{"stopped", StatusStopped, true},
		{"starting", StatusStarting, false},
		{"running", StatusRunning, false},
		{"stopping", StatusStopping, false},
		{"error", StatusError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})
			ws.SetStatus(tt.status)

			if ws.IsStopped() != tt.expected {
				t.Errorf("IsStopped() = %v, want %v", ws.IsStopped(), tt.expected)
			}
		})
	}
}

func TestWorkspace_RestartCount(t *testing.T) {
	ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})

	if ws.GetRestartCount() != 0 {
		t.Errorf("initial RestartCount = %d, want 0", ws.GetRestartCount())
	}

	// Increment multiple times
	for i := 1; i <= 5; i++ {
		count := ws.IncrementRestartCount()
		if count != i {
			t.Errorf("IncrementRestartCount() returned %d, want %d", count, i)
		}
	}

	if ws.GetRestartCount() != 5 {
		t.Errorf("GetRestartCount() = %d, want 5", ws.GetRestartCount())
	}

	// Reset
	ws.ResetRestartCount()
	if ws.GetRestartCount() != 0 {
		t.Errorf("after reset RestartCount = %d, want 0", ws.GetRestartCount())
	}
}

func TestWorkspace_IdleTime(t *testing.T) {
	ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})

	// Idle time should be very small initially
	initialIdle := ws.GetIdleTime()
	if initialIdle < 0 {
		t.Error("idle time should not be negative")
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Idle time should have increased
	laterIdle := ws.GetIdleTime()
	if laterIdle < initialIdle {
		t.Error("idle time should increase over time")
	}

	// Update last active
	ws.UpdateLastActive()

	// Idle time should reset to near zero
	resetIdle := ws.GetIdleTime()
	if resetIdle > 10*time.Millisecond {
		t.Errorf("after UpdateLastActive(), idle time = %v, expected near zero", resetIdle)
	}
}

func TestWorkspace_SetError(t *testing.T) {
	ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})

	ws.SetError("connection failed")

	if ws.GetStatus() != StatusError {
		t.Errorf("Status = %v, want %v", ws.GetStatus(), StatusError)
	}

	ws.mu.RLock()
	msg := ws.ErrorMessage
	ws.mu.RUnlock()

	if msg != "connection failed" {
		t.Errorf("ErrorMessage = %s, want 'connection failed'", msg)
	}
}

func TestWorkspace_ToInfo(t *testing.T) {
	now := time.Now().UTC()
	def := config.WorkspaceDefinition{
		ID:           "test-ws",
		Name:         "Test Workspace",
		Path:         "/tmp/test",
		Port:         8080,
		AutoStart:    true,
		CreatedAt:    now.Add(-time.Hour),
		LastAccessed: now,
	}

	ws := NewWorkspace(def)
	ws.SetStatus(StatusRunning)
	ws.SetPID(12345)
	ws.IncrementRestartCount()
	ws.IncrementRestartCount()

	info := ws.ToInfo()

	if info.ID != "test-ws" {
		t.Errorf("ID = %s, want 'test-ws'", info.ID)
	}
	if info.Name != "Test Workspace" {
		t.Errorf("Name = %s, want 'Test Workspace'", info.Name)
	}
	if info.Path != "/tmp/test" {
		t.Errorf("Path = %s, want '/tmp/test'", info.Path)
	}
	if info.Port != 8080 {
		t.Errorf("Port = %d, want 8080", info.Port)
	}
	if info.Status != StatusRunning {
		t.Errorf("Status = %v, want %v", info.Status, StatusRunning)
	}
	if !info.AutoStart {
		t.Error("AutoStart = false, want true")
	}
	if info.PID != 12345 {
		t.Errorf("PID = %d, want 12345", info.PID)
	}
	if info.RestartCount != 2 {
		t.Errorf("RestartCount = %d, want 2", info.RestartCount)
	}
}

func TestWorkspace_ToInfo_ThreadSafe(t *testing.T) {
	ws := NewWorkspace(config.WorkspaceDefinition{ID: "test"})

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = ws.ToInfo()
		}()
		go func(id int) {
			defer wg.Done()
			ws.SetStatus(WorkspaceStatus(string([]rune{rune('a' + id%5)})))
		}(i)
	}

	wg.Wait()
	// No panic means thread-safe
}

// --- Status Constants Tests ---

func TestWorkspaceStatus_Constants(t *testing.T) {
	if StatusStopped != "stopped" {
		t.Errorf("StatusStopped = %s, want 'stopped'", StatusStopped)
	}
	if StatusStarting != "starting" {
		t.Errorf("StatusStarting = %s, want 'starting'", StatusStarting)
	}
	if StatusRunning != "running" {
		t.Errorf("StatusRunning = %s, want 'running'", StatusRunning)
	}
	if StatusStopping != "stopping" {
		t.Errorf("StatusStopping = %s, want 'stopping'", StatusStopping)
	}
	if StatusError != "error" {
		t.Errorf("StatusError = %s, want 'error'", StatusError)
	}
}
