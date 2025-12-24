package workspace

import (
	"os/exec"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/config"
)

// WorkspaceStatus represents the current state of a workspace
type WorkspaceStatus string

const (
	StatusStopped  WorkspaceStatus = "stopped"
	StatusStarting WorkspaceStatus = "starting"
	StatusRunning  WorkspaceStatus = "running"
	StatusStopping WorkspaceStatus = "stopping"
	StatusError    WorkspaceStatus = "error"
)

// Workspace represents a running or configured workspace instance
type Workspace struct {
	// Configuration from workspaces.yaml
	Definition config.WorkspaceDefinition

	// Runtime state
	Status       WorkspaceStatus
	PID          int
	ProcessCmd   *exec.Cmd
	LastActive   time.Time
	ErrorMessage string
	RestartCount int

	// Synchronization
	mu sync.RWMutex
}

// NewWorkspace creates a new workspace instance from a definition
func NewWorkspace(def config.WorkspaceDefinition) *Workspace {
	return &Workspace{
		Definition:   def,
		Status:       StatusStopped,
		LastActive:   time.Now().UTC(),
		RestartCount: 0,
	}
}

// IsRunning returns true if the workspace is currently running
func (w *Workspace) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Status == StatusRunning || w.Status == StatusStarting
}

// IsStopped returns true if the workspace is stopped
func (w *Workspace) IsStopped() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Status == StatusStopped
}

// UpdateLastActive updates the last active timestamp
func (w *Workspace) UpdateLastActive() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.LastActive = time.Now().UTC()
}

// SetStatus updates the workspace status
func (w *Workspace) SetStatus(status WorkspaceStatus) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Status = status
}

// SetError sets the workspace to error state with a message
func (w *Workspace) SetError(message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Status = StatusError
	w.ErrorMessage = message
}

// GetStatus returns the current status
func (w *Workspace) GetStatus() WorkspaceStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.Status
}

// GetPID returns the process ID
func (w *Workspace) GetPID() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.PID
}

// SetPID sets the process ID
func (w *Workspace) SetPID(pid int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.PID = pid
}

// IncrementRestartCount increments and returns the restart counter
func (w *Workspace) IncrementRestartCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.RestartCount++
	return w.RestartCount
}

// ResetRestartCount resets the restart counter to 0
func (w *Workspace) ResetRestartCount() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.RestartCount = 0
}

// GetRestartCount returns the current restart count
func (w *Workspace) GetRestartCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.RestartCount
}

// GetIdleTime returns how long the workspace has been idle
func (w *Workspace) GetIdleTime() time.Duration {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return time.Since(w.LastActive)
}

// WorkspaceInfo contains information about a workspace for API responses
type WorkspaceInfo struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Path         string          `json:"path"`
	Port         int             `json:"port"`
	Status       WorkspaceStatus `json:"status"`
	AutoStart    bool            `json:"auto_start"`
	PID          int             `json:"pid,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	RestartCount int             `json:"restart_count"`
	LastActive   time.Time       `json:"last_active"`
	CreatedAt    time.Time       `json:"created_at"`
	LastAccessed time.Time       `json:"last_accessed"`
}

// ToInfo converts a Workspace to WorkspaceInfo for API responses
func (w *Workspace) ToInfo() WorkspaceInfo {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return WorkspaceInfo{
		ID:           w.Definition.ID,
		Name:         w.Definition.Name,
		Path:         w.Definition.Path,
		Port:         w.Definition.Port,
		Status:       w.Status,
		AutoStart:    w.Definition.AutoStart,
		PID:          w.PID,
		ErrorMessage: w.ErrorMessage,
		RestartCount: w.RestartCount,
		LastActive:   w.LastActive,
		CreatedAt:    w.Definition.CreatedAt,
		LastAccessed: w.Definition.LastAccessed,
	}
}
