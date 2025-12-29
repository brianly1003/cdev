// Package workspace provides workspace management including git tracking.
package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/config"
)

// TrackerState represents the health state of a git tracker.
type TrackerState string

const (
	TrackerStateHealthy     TrackerState = "healthy"
	TrackerStateUnhealthy   TrackerState = "unhealthy"
	TrackerStateUnavailable TrackerState = "unavailable"
	TrackerStateNotGit      TrackerState = "not_git"
)

// TrackerInfo contains information about a cached git tracker.
type TrackerInfo struct {
	WorkspaceID string       `json:"workspace_id"`
	Path        string       `json:"path"`
	State       TrackerState `json:"state"`
	IsGitRepo   bool         `json:"is_git_repo"`
	RepoName    string       `json:"repo_name,omitempty"`
	LastChecked time.Time    `json:"last_checked"`
	LastError   string       `json:"last_error,omitempty"`
}

// cachedTracker holds a git tracker with metadata.
type cachedTracker struct {
	tracker     *git.Tracker
	info        TrackerInfo
	mu          sync.Mutex // Per-tracker mutex for concurrent git operations
	lastUsed    time.Time
	initError   error
	initialized bool
}

// GitTrackerManager manages git trackers for all workspaces.
// It provides:
// - Lazy initialization of trackers (on first use)
// - Caching of trackers by workspace ID
// - Thread-safe access with RWMutex
// - Health monitoring and auto-recovery
// - Graceful degradation for non-git repos
type GitTrackerManager struct {
	trackers   map[string]*cachedTracker // keyed by workspace ID
	gitCommand string
	logger     *slog.Logger

	// Health monitoring
	healthCheckInterval time.Duration
	operationTimeout    time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// GitTrackerManagerConfig holds configuration for GitTrackerManager.
type GitTrackerManagerConfig struct {
	GitCommand          string
	HealthCheckInterval time.Duration
	OperationTimeout    time.Duration
	Logger              *slog.Logger
}

// DefaultGitTrackerManagerConfig returns default configuration.
func DefaultGitTrackerManagerConfig() GitTrackerManagerConfig {
	return GitTrackerManagerConfig{
		GitCommand:          "git",
		HealthCheckInterval: 5 * time.Minute,
		OperationTimeout:    30 * time.Second,
		Logger:              slog.Default(),
	}
}

// NewGitTrackerManager creates a new git tracker manager.
func NewGitTrackerManager(cfg GitTrackerManagerConfig) *GitTrackerManager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &GitTrackerManager{
		trackers:            make(map[string]*cachedTracker),
		gitCommand:          cfg.GitCommand,
		logger:              cfg.Logger,
		healthCheckInterval: cfg.HealthCheckInterval,
		operationTimeout:    cfg.OperationTimeout,
		ctx:                 ctx,
		cancel:              cancel,
	}

	// Start health monitor
	go m.healthMonitor()

	return m
}

// RegisterWorkspace registers a workspace for git tracking.
// This validates the path and prepares for lazy tracker initialization.
// Returns error if path is invalid, but allows non-git repos.
func (m *GitTrackerManager) RegisterWorkspace(ws *config.WorkspaceDefinition) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Validate path exists
	info, err := os.Stat(ws.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", ws.Path)
		}
		return fmt.Errorf("failed to access path: %w", err)
	}

	// Validate it's a directory
	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", ws.Path)
	}

	// Check if already registered
	if existing, ok := m.trackers[ws.ID]; ok {
		// Update path if changed
		if existing.info.Path != ws.Path {
			m.logger.Info("Workspace path changed, re-registering",
				"workspace_id", ws.ID,
				"old_path", existing.info.Path,
				"new_path", ws.Path,
			)
			delete(m.trackers, ws.ID)
		} else {
			return nil // Already registered with same path
		}
	}

	// Create cached tracker entry (lazy init - tracker created on first use)
	ct := &cachedTracker{
		info: TrackerInfo{
			WorkspaceID: ws.ID,
			Path:        ws.Path,
			State:       TrackerStateUnavailable, // Will be updated on first use
			LastChecked: time.Now().UTC(),
		},
		lastUsed:    time.Now().UTC(),
		initialized: false,
	}

	m.trackers[ws.ID] = ct

	m.logger.Info("Registered workspace for git tracking",
		"workspace_id", ws.ID,
		"path", ws.Path,
	)

	return nil
}

// UnregisterWorkspace removes a workspace from git tracking.
func (m *GitTrackerManager) UnregisterWorkspace(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ct, ok := m.trackers[workspaceID]; ok {
		m.logger.Info("Unregistered workspace from git tracking",
			"workspace_id", workspaceID,
			"path", ct.info.Path,
		)
		delete(m.trackers, workspaceID)
	}
}

// GetTracker returns a git tracker for a workspace.
// Initializes the tracker lazily on first call.
// Returns nil tracker and no error for non-git repos (graceful degradation).
func (m *GitTrackerManager) GetTracker(workspaceID string) (*git.Tracker, error) {
	m.mu.RLock()
	ct, ok := m.trackers[workspaceID]
	m.mu.RUnlock()

	if !ok {
		m.logger.Debug("GetTracker: workspace not registered",
			"workspace_id", workspaceID,
		)
		return nil, fmt.Errorf("workspace not registered: %s", workspaceID)
	}

	// Lock this specific tracker for initialization
	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Lazy initialization
	if !ct.initialized {
		m.logger.Debug("GetTracker: performing lazy initialization",
			"workspace_id", workspaceID,
		)
		m.initializeTracker(ct)
	}

	// Update last used time
	ct.lastUsed = time.Now().UTC()

	// Check if initialization failed
	if ct.initError != nil {
		m.logger.Debug("GetTracker: returning init error",
			"workspace_id", workspaceID,
			"error", ct.initError,
		)
		return nil, ct.initError
	}

	// Return nil for non-git repos (not an error)
	if !ct.info.IsGitRepo {
		m.logger.Debug("GetTracker: workspace is not a git repo",
			"workspace_id", workspaceID,
			"state", ct.info.State,
		)
		return nil, nil
	}

	m.logger.Debug("GetTracker: returning tracker",
		"workspace_id", workspaceID,
		"repo_name", ct.info.RepoName,
	)
	return ct.tracker, nil
}

// GetTrackerInfo returns information about a workspace's git tracker.
func (m *GitTrackerManager) GetTrackerInfo(workspaceID string) (*TrackerInfo, error) {
	m.mu.RLock()
	ct, ok := m.trackers[workspaceID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("workspace not registered: %s", workspaceID)
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Lazy initialization if needed
	if !ct.initialized {
		m.initializeTracker(ct)
	}

	// Return a copy to avoid race conditions
	info := ct.info
	return &info, nil
}

// GetAllTrackerInfo returns information about all registered trackers.
func (m *GitTrackerManager) GetAllTrackerInfo() []TrackerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]TrackerInfo, 0, len(m.trackers))
	for _, ct := range m.trackers {
		ct.mu.Lock()
		info := ct.info
		ct.mu.Unlock()
		result = append(result, info)
	}
	return result
}

// RefreshTracker re-initializes a tracker, useful after repo changes.
func (m *GitTrackerManager) RefreshTracker(workspaceID string) error {
	m.mu.RLock()
	ct, ok := m.trackers[workspaceID]
	m.mu.RUnlock()

	if !ok {
		m.logger.Warn("RefreshTracker: workspace not registered",
			"workspace_id", workspaceID,
		)
		return fmt.Errorf("workspace not registered: %s", workspaceID)
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	m.logger.Info("RefreshTracker: refreshing tracker",
		"workspace_id", workspaceID,
		"path", ct.info.Path,
		"previous_state", ct.info.State,
		"previous_is_git_repo", ct.info.IsGitRepo,
	)

	// Force re-initialization
	ct.initialized = false
	ct.initError = nil
	ct.tracker = nil

	m.initializeTracker(ct)

	m.logger.Info("RefreshTracker: completed",
		"workspace_id", workspaceID,
		"new_state", ct.info.State,
		"new_is_git_repo", ct.info.IsGitRepo,
		"init_error", ct.initError,
	)

	if ct.initError != nil {
		return ct.initError
	}

	return nil
}

// initializeTracker initializes a cached tracker (must hold ct.mu lock).
func (m *GitTrackerManager) initializeTracker(ct *cachedTracker) {
	ct.initialized = true
	ct.info.LastChecked = time.Now().UTC()

	// Check if path still exists
	info, err := os.Stat(ct.info.Path)
	if err != nil {
		if os.IsNotExist(err) {
			ct.initError = fmt.Errorf("workspace path no longer exists: %s", ct.info.Path)
			ct.info.State = TrackerStateUnavailable
			ct.info.LastError = ct.initError.Error()
			m.logger.Warn("Workspace path no longer exists",
				"workspace_id", ct.info.WorkspaceID,
				"path", ct.info.Path,
			)
			return
		}
		ct.initError = fmt.Errorf("failed to access workspace path: %w", err)
		ct.info.State = TrackerStateUnhealthy
		ct.info.LastError = ct.initError.Error()
		return
	}

	if !info.IsDir() {
		ct.initError = fmt.Errorf("workspace path is not a directory: %s", ct.info.Path)
		ct.info.State = TrackerStateUnhealthy
		ct.info.LastError = ct.initError.Error()
		return
	}

	// Create git tracker
	tracker := git.NewTracker(ct.info.Path, m.gitCommand, nil)

	// Check if it's a git repo
	if !tracker.IsGitRepo() {
		ct.info.IsGitRepo = false
		ct.info.State = TrackerStateNotGit
		ct.info.LastError = ""
		ct.tracker = nil
		m.logger.Info("Workspace is not a git repository (after init check)",
			"workspace_id", ct.info.WorkspaceID,
			"path", ct.info.Path,
			"git_command", m.gitCommand,
		)
		return
	}

	// Successfully initialized
	ct.tracker = tracker
	ct.info.IsGitRepo = true
	ct.info.RepoName = tracker.GetRepoName()
	ct.info.State = TrackerStateHealthy
	ct.info.LastError = ""
	ct.initError = nil

	m.logger.Info("Initialized git tracker successfully",
		"workspace_id", ct.info.WorkspaceID,
		"path", ct.info.Path,
		"repo_name", ct.info.RepoName,
		"state", ct.info.State,
	)
}

// healthMonitor periodically checks the health of all trackers.
func (m *GitTrackerManager) healthMonitor() {
	if m.healthCheckInterval <= 0 {
		return
	}

	ticker := time.NewTicker(m.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkAllTrackers()
		}
	}
}

// checkAllTrackers checks health of all initialized trackers.
func (m *GitTrackerManager) checkAllTrackers() {
	m.mu.RLock()
	workspaceIDs := make([]string, 0, len(m.trackers))
	for id := range m.trackers {
		workspaceIDs = append(workspaceIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range workspaceIDs {
		m.checkTrackerHealth(id)
	}
}

// checkTrackerHealth checks and updates the health of a single tracker.
func (m *GitTrackerManager) checkTrackerHealth(workspaceID string) {
	m.mu.RLock()
	ct, ok := m.trackers[workspaceID]
	m.mu.RUnlock()

	if !ok {
		return
	}

	ct.mu.Lock()
	defer ct.mu.Unlock()

	// Skip if never initialized
	if !ct.initialized {
		return
	}

	// Check if path still exists
	_, err := os.Stat(ct.info.Path)
	if err != nil {
		oldState := ct.info.State
		if os.IsNotExist(err) {
			ct.info.State = TrackerStateUnavailable
			ct.info.LastError = "path no longer exists"
		} else {
			ct.info.State = TrackerStateUnhealthy
			ct.info.LastError = err.Error()
		}
		ct.info.LastChecked = time.Now().UTC()

		if oldState != ct.info.State {
			m.logger.Warn("Workspace health changed",
				"workspace_id", workspaceID,
				"old_state", oldState,
				"new_state", ct.info.State,
				"error", ct.info.LastError,
			)
		}
		return
	}

	// For git repos, verify git is still accessible
	if ct.info.IsGitRepo && ct.tracker != nil {
		ctx, cancel := context.WithTimeout(m.ctx, m.operationTimeout)
		_, err := ct.tracker.Status(ctx)
		cancel()

		oldState := ct.info.State
		if err != nil {
			ct.info.State = TrackerStateUnhealthy
			ct.info.LastError = err.Error()
		} else {
			ct.info.State = TrackerStateHealthy
			ct.info.LastError = ""
		}
		ct.info.LastChecked = time.Now().UTC()

		if oldState != ct.info.State {
			m.logger.Info("Workspace health changed",
				"workspace_id", workspaceID,
				"old_state", oldState,
				"new_state", ct.info.State,
			)
		}
	}
}

// Stop stops the git tracker manager and cleans up resources.
func (m *GitTrackerManager) Stop() {
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Stopping git tracker manager", "tracker_count", len(m.trackers))
	m.trackers = make(map[string]*cachedTracker)
}

// Stats returns statistics about the tracker manager.
func (m *GitTrackerManager) Stats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var healthy, unhealthy, unavailable, notGit, uninitialized int

	for _, ct := range m.trackers {
		ct.mu.Lock()
		if !ct.initialized {
			uninitialized++
		} else {
			switch ct.info.State {
			case TrackerStateHealthy:
				healthy++
			case TrackerStateUnhealthy:
				unhealthy++
			case TrackerStateUnavailable:
				unavailable++
			case TrackerStateNotGit:
				notGit++
			}
		}
		ct.mu.Unlock()
	}

	return map[string]interface{}{
		"total":         len(m.trackers),
		"healthy":       healthy,
		"unhealthy":     unhealthy,
		"unavailable":   unavailable,
		"not_git":       notGit,
		"uninitialized": uninitialized,
	}
}
