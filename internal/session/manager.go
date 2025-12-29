package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/live"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/adapters/watcher"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/sync"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Manager orchestrates multiple Claude sessions across workspaces.
type Manager struct {
	sessions                map[string]*Session             // keyed by session ID
	workspaces              map[string]*workspace.Workspace // keyed by workspace ID
	activeSessions          map[string]string               // workspace ID -> active session ID
	activeSessionWorkspaces map[string]string               // session ID -> workspace ID (reverse mapping)
	gitTrackerManager       *workspace.GitTrackerManager
	hub                     ports.EventHub
	cfg                     *config.Config
	logger                  *slog.Logger

	// Configuration
	idleTimeout time.Duration

	// Session streaming for live message updates
	streamer             *sessioncache.SessionStreamer
	streamerWorkspaceID  string          // Workspace ID of currently watched session
	streamerSessionID    string          // Session ID currently being watched
	streamerWatchers     map[string]bool // Client IDs currently watching (for proper cleanup)
	streamerMu           sync.Mutex

	// LIVE session support (Claude running in user's terminal)
	liveInjector *live.Injector // Shared injector (platform-specific keystroke injection)

	// Git watchers per workspace (started on workspace/subscribe)
	gitWatchers      map[string]context.CancelFunc // workspace ID -> cancel function
	gitWatcherCounts map[string]int                // workspace ID -> subscriber count (reference counting)
	gitWatchersMu    sync.Mutex

	// Session file watchers for detecting real session IDs (one per workspace)
	// Used when starting a new session in a workspace with no existing sessions
	sessionFileWatchers   map[string]context.CancelFunc // workspaceID -> cancel function
	sessionFileWatchersMu sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
}

// NewManager creates a new session manager.
func NewManager(hub ports.EventHub, cfg *config.Config, logger *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	// Default idle timeout: 30 minutes
	idleTimeout := 30 * time.Minute
	// TODO: Read from config when available

	return &Manager{
		sessions:                make(map[string]*Session),
		workspaces:              make(map[string]*workspace.Workspace),
		activeSessions:          make(map[string]string),
		activeSessionWorkspaces: make(map[string]string),
		gitWatchers:             make(map[string]context.CancelFunc),
		gitWatcherCounts:        make(map[string]int),
		streamerWatchers:        make(map[string]bool),
		sessionFileWatchers:     make(map[string]context.CancelFunc),
		hub:                     hub,
		cfg:                     cfg,
		logger:                  logger,
		idleTimeout:             idleTimeout,
		ctx:                     ctx,
		cancel:                  cancel,
	}
}

// SetGitTrackerManager sets the shared git tracker manager.
func (m *Manager) SetGitTrackerManager(gtm *workspace.GitTrackerManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gitTrackerManager = gtm
}

// SetLiveSessionSupport enables LIVE session support.
// This allows sending messages to Claude instances running in the user's terminal.
// The detector is created dynamically per workspace, but the injector is shared.
func (m *Manager) SetLiveSessionSupport(workspacePath string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.liveInjector = live.NewInjector()
	log.Info().Msg("LIVE session support enabled")
}

// Start starts the session manager and idle monitor.
func (m *Manager) Start() error {
	m.logger.Info("Starting session manager")

	// Start idle session monitor
	go m.idleMonitor()

	return nil
}

// Stop stops all sessions and the manager.
func (m *Manager) Stop() error {
	m.logger.Info("Stopping session manager")

	// Close the session streamer first (has its own goroutine)
	m.streamerMu.Lock()
	if m.streamer != nil {
		m.streamer.Close()
		m.streamer = nil
		m.logger.Debug("Session streamer closed")
	}
	m.streamerMu.Unlock()

	// Cancel all session file watchers (prevents goroutine leaks)
	m.sessionFileWatchersMu.Lock()
	for workspaceID, cancel := range m.sessionFileWatchers {
		cancel()
		m.logger.Debug("Cancelled session file watcher", "workspace_id", workspaceID)
	}
	m.sessionFileWatchers = make(map[string]context.CancelFunc)
	m.sessionFileWatchersMu.Unlock()

	// Cancel the manager context
	m.cancel()

	// Try to acquire lock with timeout to prevent hanging
	lockAcquired := make(chan struct{})
	go func() {
		m.mu.Lock()
		close(lockAcquired)
	}()

	select {
	case <-lockAcquired:
		// Got the lock, proceed with cleanup
		defer m.mu.Unlock()
	case <-time.After(5 * time.Second):
		m.logger.Warn("Timeout waiting for session manager lock, forcing shutdown")
		return fmt.Errorf("timeout waiting for lock")
	}

	// Create a fresh context for stopping sessions (since m.ctx is cancelled)
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()

	// Stop all active sessions
	for _, session := range m.sessions {
		if session.GetStatus() == StatusRunning || session.GetStatus() == StatusStarting {
			_ = m.stopSessionInternalWithContext(stopCtx, session)
		}
	}

	m.logger.Info("Session manager stopped")
	return nil
}

// RegisterWorkspace registers a workspace with the manager.
func (m *Manager) RegisterWorkspace(ws *workspace.Workspace) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaces[ws.Definition.ID] = ws
	m.logger.Debug("Registered workspace", "id", ws.Definition.ID, "name", ws.Definition.Name)
}

// UnregisterWorkspace removes a workspace from the manager.
func (m *Manager) UnregisterWorkspace(workspaceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if there's an active session for this workspace
	for _, session := range m.sessions {
		if session.WorkspaceID == workspaceID && session.GetStatus() == StatusRunning {
			return fmt.Errorf("cannot unregister workspace with active session")
		}
	}

	delete(m.workspaces, workspaceID)
	return nil
}

// GetWorkspace returns a workspace by ID.
func (m *Manager) GetWorkspace(workspaceID string) (*workspace.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}
	return ws, nil
}

// StartGitWatcher starts watching git state changes for a workspace.
// This is called when a client subscribes to a workspace (workspace/subscribe).
// The watcher emits git_status_changed events when staging/commits/branch changes occur.
// Uses reference counting - multiple clients can subscribe, watcher starts on first subscriber.
// Returns nil if git is disabled.
func (m *Manager) StartGitWatcher(workspaceID string) error {
	if !m.cfg.Git.Enabled {
		m.logger.Debug("Git watcher not started (git disabled)", "workspace_id", workspaceID)
		return nil
	}

	m.gitWatchersMu.Lock()
	defer m.gitWatchersMu.Unlock()

	// Increment subscriber count
	m.gitWatcherCounts[workspaceID]++

	// Check if watcher already running for this workspace
	if _, exists := m.gitWatchers[workspaceID]; exists {
		m.logger.Debug("Git watcher already running, added subscriber",
			"workspace_id", workspaceID,
			"subscriber_count", m.gitWatcherCounts[workspaceID],
		)
		return nil
	}

	// Get workspace
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		m.gitWatcherCounts[workspaceID]-- // Rollback count on error
		return err
	}

	// Create git tracker for this workspace
	gitTracker := git.NewTracker(ws.Definition.Path, m.cfg.Git.Command, m.hub)
	if !gitTracker.IsGitRepo() {
		m.logger.Debug("Not a git repository, skipping git watcher", "workspace_id", workspaceID)
		m.gitWatcherCounts[workspaceID]-- // Rollback count
		return nil
	}

	// Create cancellable context for this watcher
	watchCtx, cancel := context.WithCancel(m.ctx)
	m.gitWatchers[workspaceID] = cancel

	// Start git watcher in background
	go m.watchGitIndex(watchCtx, ws.Definition.Path, workspaceID, gitTracker)

	m.logger.Info("Started git watcher for workspace",
		"workspace_id", workspaceID,
		"path", ws.Definition.Path,
		"subscriber_count", m.gitWatcherCounts[workspaceID],
	)

	return nil
}

// StopGitWatcher decrements the subscriber count for a workspace's git watcher.
// The watcher is only stopped when the last subscriber unsubscribes.
// This is called when a client unsubscribes from a workspace (workspace/unsubscribe).
func (m *Manager) StopGitWatcher(workspaceID string) {
	m.gitWatchersMu.Lock()
	defer m.gitWatchersMu.Unlock()

	// Decrement subscriber count
	if m.gitWatcherCounts[workspaceID] > 0 {
		m.gitWatcherCounts[workspaceID]--
	}

	// If there are still subscribers, keep the watcher running
	if m.gitWatcherCounts[workspaceID] > 0 {
		m.logger.Debug("Removed git watcher subscriber (others still watching)",
			"workspace_id", workspaceID,
			"remaining_subscribers", m.gitWatcherCounts[workspaceID],
		)
		return
	}

	// Last subscriber - stop the watcher
	if cancel, exists := m.gitWatchers[workspaceID]; exists {
		cancel()
		delete(m.gitWatchers, workspaceID)
		delete(m.gitWatcherCounts, workspaceID)
		m.logger.Info("Stopped git watcher for workspace (last subscriber left)",
			"workspace_id", workspaceID,
		)
	}
}

// OnClientDisconnect handles cleanup when a client disconnects.
// This is called by the unified server when a WebSocket connection is closed.
// It decrements git watcher counts and session streamer counts for the disconnected client.
func (m *Manager) OnClientDisconnect(clientID string, subscribedWorkspaces []string) {
	m.logger.Info("Client disconnected, cleaning up",
		"client_id", clientID,
		"subscribed_workspaces", len(subscribedWorkspaces),
	)

	// Decrement git watcher counts for each subscribed workspace
	for _, workspaceID := range subscribedWorkspaces {
		m.StopGitWatcher(workspaceID)
	}

	// Remove client from session streamer watchers (only if they were watching)
	m.streamerMu.Lock()
	if m.streamerWatchers[clientID] {
		delete(m.streamerWatchers, clientID)
		if len(m.streamerWatchers) == 0 && m.streamer != nil {
			// Last watcher - close the streamer
			m.streamer.Close()
			m.streamer = nil
			prevWorkspaceID := m.streamerWorkspaceID
			prevSessionID := m.streamerSessionID
			m.streamerWorkspaceID = ""
			m.streamerSessionID = ""
			m.logger.Info("Stopped session streamer (last watcher disconnected)",
				"workspace_id", prevWorkspaceID,
				"session_id", prevSessionID,
			)
		} else if len(m.streamerWatchers) > 0 {
			m.logger.Debug("Removed client from session streamer watchers (others still watching)",
				"client_id", clientID,
				"remaining_watchers", len(m.streamerWatchers),
			)
		}
	}
	m.streamerMu.Unlock()
}

// findWorkspaceForSession searches all workspaces to find which one contains the given session.
// This is used for LIVE sessions that were discovered but not explicitly activated.
func (m *Manager) findWorkspaceForSession(sessionID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for workspaceID, ws := range m.workspaces {
		// Check if this workspace has the session in its session files
		exists, _ := m.sessionFileExistsForWorkspace(ws, sessionID)
		if exists {
			log.Debug().
				Str("session_id", sessionID).
				Str("workspace_id", workspaceID).
				Msg("found workspace for session")
			return workspaceID
		}
	}
	return ""
}

// sessionFileExistsForWorkspace checks if a session file exists for a workspace.
// This is a standalone check that does file I/O only.
func (m *Manager) sessionFileExistsForWorkspace(ws *workspace.Workspace, sessionID string) (bool, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}

	// Convert workspace path to Claude's project path format
	projectPath := strings.ReplaceAll(ws.Definition.Path, "/", "-")
	if !strings.HasPrefix(projectPath, "-") {
		projectPath = "-" + projectPath
	}

	sessionFile := filepath.Join(homeDir, ".claude", "projects", projectPath, sessionID+".jsonl")
	_, err = os.Stat(sessionFile)
	return err == nil, nil
}

// SessionFileExists checks if a session file exists in .claude/projects for the workspace.
// This validates against the source of truth for Claude Code sessions.
func (m *Manager) SessionFileExists(workspaceID string, sessionID string) (bool, error) {
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return false, err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false, fmt.Errorf("failed to get home directory: %w", err)
	}

	// Convert workspace path to Claude's project path format
	// /Users/brian/Projects/cdev-ios -> -Users-brian-Projects-cdev-ios
	projectPath := strings.ReplaceAll(ws.Definition.Path, "/", "-")
	if !strings.HasPrefix(projectPath, "-") {
		projectPath = "-" + projectPath
	}

	sessionFile := filepath.Join(homeDir, ".claude", "projects", projectPath, sessionID+".jsonl")
	_, err = os.Stat(sessionFile)
	exists := err == nil

	log.Debug().
		Str("workspace_id", workspaceID).
		Str("session_id", sessionID).
		Str("session_file", sessionFile).
		Bool("exists", exists).
		Msg("checking session file in .claude/projects")

	return exists, nil
}

// GetLatestSessionID returns the most recently modified session ID from .claude/projects.
// This is the source of truth for Claude Code sessions.
// Only returns main sessions (UUID-formatted files with user interaction), not agent sub-sessions.
func (m *Manager) GetLatestSessionID(workspaceID string) (string, error) {
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// Convert workspace path to Claude's project path format
	projectPath := strings.ReplaceAll(ws.Definition.Path, "/", "-")
	if !strings.HasPrefix(projectPath, "-") {
		projectPath = "-" + projectPath
	}

	sessionsDir := filepath.Join(homeDir, ".claude", "projects", projectPath)

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No sessions directory yet
		}
		return "", fmt.Errorf("failed to read sessions directory: %w", err)
	}

	// Collect valid main sessions (UUID format with user interaction)
	type sessionCandidate struct {
		sessionID string
		modTime   int64
	}
	var candidates []sessionCandidate

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		// Skip agent sub-sessions (agent-*.jsonl)
		if strings.HasPrefix(entry.Name(), "agent-") {
			continue
		}

		// Get session ID (filename without .jsonl)
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")

		// Validate UUID format (main sessions have UUID filenames)
		if _, err := uuid.Parse(sessionID); err != nil {
			continue // Not a valid UUID, skip
		}

		// Check if file contains "role":"user" (actual user session)
		sessionPath := filepath.Join(sessionsDir, entry.Name())
		if !m.sessionHasUserRole(sessionPath) {
			log.Debug().
				Str("session_id", sessionID).
				Msg("skipping session without user interaction")
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		candidates = append(candidates, sessionCandidate{
			sessionID: sessionID,
			modTime:   info.ModTime().UnixNano(),
		})
	}

	if len(candidates) == 0 {
		return "", nil // No valid session files found
	}

	// Find the most recently modified valid session
	var latestSession string
	var latestModTime int64
	for _, c := range candidates {
		if c.modTime > latestModTime {
			latestModTime = c.modTime
			latestSession = c.sessionID
		}
	}

	log.Info().
		Str("workspace_id", workspaceID).
		Str("session_id", latestSession).
		Str("sessions_dir", sessionsDir).
		Int("candidates_count", len(candidates)).
		Msg("found latest session from .claude/projects")

	return latestSession, nil
}

// sessionHasUserRole checks if a session file contains "role":"user" entries.
// This indicates actual user interaction (not just summaries or system metadata).
func (m *Manager) sessionHasUserRole(sessionPath string) bool {
	file, err := os.Open(sessionPath)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	// Read first 32KB to check for user role (avoid reading entire large files)
	buf := make([]byte, 32*1024)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return false
	}

	// Check for "role":"user" pattern
	content := string(buf[:n])
	return strings.Contains(content, `"role":"user"`)
}

// ListWorkspaces returns all registered workspaces.
func (m *Manager) ListWorkspaces() []*workspace.Workspace {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*workspace.Workspace, 0, len(m.workspaces))
	for _, ws := range m.workspaces {
		result = append(result, ws)
	}
	return result
}

// StartSession starts a new Claude session for a workspace.
// It automatically uses the most recent historical session ID from ~/.claude/projects/
// so that session/send with mode "continue" will properly resume the conversation.
func (m *Manager) StartSession(workspaceID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check workspace exists
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Get the most recent historical session ID for this workspace
	// This allows session/send with mode "continue" to resume the conversation
	sessionID := m.getMostRecentHistoricalSessionID(ws.Definition.Path)
	if sessionID == "" {
		// No historical session found, generate a new UUID
		sessionID = uuid.New().String()
		m.logger.Info("No historical session found, using new session ID",
			"session_id", sessionID,
			"workspace_id", workspaceID,
		)
	} else {
		m.logger.Info("Using most recent historical session ID",
			"session_id", sessionID,
			"workspace_id", workspaceID,
		)
	}

	session := NewSession(sessionID, workspaceID)
	session.SetStatus(StatusStarting)

	// Create Claude manager for this session
	claudeManager := claude.NewManagerWithContext(
		m.hub,
		m.cfg.Claude.Command,
		m.cfg.Claude.Args,
		m.cfg.Claude.TimeoutMinutes,
		m.cfg.Claude.SkipPermissions,
		ws.Definition.Path,
		workspaceID,
		sessionID,
	)
	session.SetClaudeManager(claudeManager)

	// Create git tracker for this workspace
	gitTracker := git.NewTracker(ws.Definition.Path, m.cfg.Git.Command, m.hub)
	session.SetGitTracker(gitTracker)

	// Optionally create file watcher (lazy init based on config)
	if m.cfg.Watcher.Enabled {
		fileWatcher := watcher.NewWatcher(
			ws.Definition.Path,
			m.hub,
			m.cfg.Watcher.DebounceMS,
			m.cfg.Watcher.IgnorePatterns,
		)
		session.SetFileWatcher(fileWatcher)

		// Start file watcher
		if err := fileWatcher.Start(m.ctx); err != nil {
			m.logger.Warn("Failed to start file watcher", "error", err, "workspace_id", workspaceID)
		}
	}

	// NOTE: Git index watcher is now started on workspace/subscribe, not session/start.
	// This allows git_status_changed events to be emitted as soon as a client subscribes
	// to a workspace, rather than waiting for a session to start.

	// Store session
	m.sessions[sessionID] = session
	session.SetStatus(StatusRunning)

	// Auto-activate this session for the workspace
	m.activeSessions[workspaceID] = sessionID

	m.logger.Info("Started session",
		"session_id", sessionID,
		"workspace_id", workspaceID,
		"workspace_name", ws.Definition.Name,
		"path", ws.Definition.Path,
	)

	return session, nil
}

// WatchForNewSessionFile watches the .claude/projects directory for new session files.
// When a new .jsonl file is created (after trust folder is accepted), it emits
// a session_id_resolved event with the real session ID from Claude.
//
// Thread-safety: Only one watcher per workspace is allowed. If a watcher already exists
// for this workspace, this call is a no-op. The watcher is automatically cleaned up
// on completion, timeout, or cancellation.
func (m *Manager) WatchForNewSessionFile(ctx context.Context, workspaceID, temporaryID, repoPath string) {
	// Prevent multiple watchers for the same workspace (race condition fix)
	m.sessionFileWatchersMu.Lock()
	if _, exists := m.sessionFileWatchers[workspaceID]; exists {
		m.sessionFileWatchersMu.Unlock()
		m.logger.Debug("session file watcher already exists for workspace",
			"workspace_id", workspaceID,
		)
		return
	}

	// Create cancellable context for this watcher
	watchCtx, watchCancel := context.WithCancel(ctx)
	m.sessionFileWatchers[workspaceID] = watchCancel
	m.sessionFileWatchersMu.Unlock()

	sessionsDir := getSessionsDir(repoPath)
	parentDir := filepath.Dir(sessionsDir)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		m.logger.Error("failed to create session file watcher", "error", err)
		m.cleanupSessionFileWatcher(workspaceID)
		return
	}

	go func() {
		defer func() {
			_ = watcher.Close()
			m.cleanupSessionFileWatcher(workspaceID)
		}()

		// Track if we've found the session
		found := false
		dirExists := false

		// Check if sessions dir already exists
		if _, err := os.Stat(sessionsDir); err == nil {
			dirExists = true
			if err := watcher.Add(sessionsDir); err != nil {
				m.logger.Error("failed to watch sessions dir", "error", err, "path", sessionsDir)
				return
			}
		} else {
			// Watch parent for directory creation
			if err := watcher.Add(parentDir); err != nil {
				m.logger.Error("failed to watch parent dir", "error", err, "path", parentDir)
				return
			}
		}

		m.logger.Info("watching for new session file",
			"workspace_id", workspaceID,
			"temporary_id", temporaryID,
			"sessions_dir", sessionsDir,
		)

		// No timeout - watcher runs until:
		// 1. Session file found (success)
		// 2. Claude exits (detected via onPTYComplete -> FailSessionIDResolution)
		// 3. Session stopped or manager shutdown (cancellation)
		// This avoids race conditions with arbitrary timeouts

		for !found {
			select {
			case <-watchCtx.Done():
				// Watcher was cancelled - don't emit event here
				// The caller (FailSessionIDResolution or CancelSessionFileWatcher)
				// will emit the appropriate event if needed
				m.logger.Debug("session file watcher cancelled", "workspace_id", workspaceID)
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				// If sessions dir was just created, start watching it
				if !dirExists && event.Op&fsnotify.Create != 0 && event.Name == sessionsDir {
					dirExists = true
					if err := watcher.Add(sessionsDir); err != nil {
						m.logger.Error("failed to watch sessions dir", "error", err, "path", sessionsDir)
					}
					m.logger.Debug("sessions directory created, now watching", "path", sessionsDir)

					// RACE CONDITION FIX: Scan for any .jsonl files that might have been
					// created between the directory creation and when we started watching.
					// fsnotify won't emit events for files that existed before watching started.
					if sessionID := m.scanForExistingSessionFile(sessionsDir, temporaryID); sessionID != "" {
						m.logger.Info("detected existing session file after directory creation",
							"workspace_id", workspaceID,
							"temporary_id", temporaryID,
							"real_id", sessionID,
						)
						m.updateSessionID(workspaceID, temporaryID, sessionID)
						m.hub.Publish(events.NewSessionIDResolvedEvent(
							temporaryID,
							sessionID,
							workspaceID,
							filepath.Join(sessionsDir, sessionID+".jsonl"),
						))
						found = true
					}
					continue
				}

				// Check for new .jsonl files
				if event.Op&fsnotify.Create != 0 && strings.HasSuffix(event.Name, ".jsonl") {
					filename := filepath.Base(event.Name)

					// Skip agent sub-sessions (agent-*.jsonl)
					if strings.HasPrefix(filename, "agent-") {
						m.logger.Debug("skipping agent sub-session file", "file", filename)
						continue
					}

					realSessionID := strings.TrimSuffix(filename, ".jsonl")

					// Skip if this is the same as our temporary ID (unlikely but possible)
					if realSessionID == temporaryID {
						continue
					}

					// Validate UUID format to ensure this is a real session file
					if _, uuidErr := uuid.Parse(realSessionID); uuidErr != nil {
						m.logger.Debug("skipping non-UUID session file", "file", filename)
						continue
					}

					m.logger.Info("detected new session file",
						"workspace_id", workspaceID,
						"temporary_id", temporaryID,
						"real_id", realSessionID,
						"file", event.Name,
					)

					// Update internal state to use real session ID
					m.updateSessionID(workspaceID, temporaryID, realSessionID)

					// Emit the session_id_resolved event
					m.hub.Publish(events.NewSessionIDResolvedEvent(
						temporaryID,
						realSessionID,
						workspaceID,
						event.Name,
					))

					found = true
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				m.logger.Error("session file watcher error", "error", err)
			}
		}
	}()
}

// cleanupSessionFileWatcher removes the watcher tracking for a workspace.
func (m *Manager) cleanupSessionFileWatcher(workspaceID string) {
	m.sessionFileWatchersMu.Lock()
	defer m.sessionFileWatchersMu.Unlock()
	delete(m.sessionFileWatchers, workspaceID)
}

// CancelSessionFileWatcher cancels an active session file watcher for a workspace.
// Called when a session is stopped or workspace is removed.
// Does NOT emit any event - use FailSessionIDResolution if you need to emit an event.
func (m *Manager) CancelSessionFileWatcher(workspaceID string) {
	m.sessionFileWatchersMu.Lock()
	defer m.sessionFileWatchersMu.Unlock()
	if cancel, exists := m.sessionFileWatchers[workspaceID]; exists {
		cancel()
		delete(m.sessionFileWatchers, workspaceID)
		m.logger.Debug("cancelled session file watcher", "workspace_id", workspaceID)
	}
}

// FailSessionIDResolution cancels the session file watcher and emits a session_id_failed event.
// Called when Claude exits without creating a session (e.g., user declined trust folder).
// Parameters:
//   - workspaceID: The workspace ID
//   - temporaryID: The temporary session ID that was waiting for resolution
//   - reason: "trust_declined", "claude_exited", or "error"
//   - message: Optional human-readable message
func (m *Manager) FailSessionIDResolution(workspaceID, temporaryID, reason, message string) {
	m.sessionFileWatchersMu.Lock()
	cancel, exists := m.sessionFileWatchers[workspaceID]
	if exists {
		cancel()
		delete(m.sessionFileWatchers, workspaceID)
	}
	m.sessionFileWatchersMu.Unlock()

	if exists {
		m.logger.Info("session ID resolution failed",
			"workspace_id", workspaceID,
			"temporary_id", temporaryID,
			"reason", reason,
		)
		m.hub.Publish(events.NewSessionIDFailedEvent(
			temporaryID,
			workspaceID,
			reason,
			message,
		))
	}
}

// HasActiveSessionFileWatcher returns true if there's an active watcher for the workspace.
func (m *Manager) HasActiveSessionFileWatcher(workspaceID string) bool {
	m.sessionFileWatchersMu.Lock()
	defer m.sessionFileWatchersMu.Unlock()
	_, exists := m.sessionFileWatchers[workspaceID]
	return exists
}

// PublishEvent publishes an event to the hub.
// Used by RPC handlers to emit events.
func (m *Manager) PublishEvent(event *events.BaseEvent) {
	m.hub.Publish(event)
}

// scanForExistingSessionFile scans a directory for existing session .jsonl files.
// Returns the session ID if found, empty string otherwise.
// Used to handle race condition where file is created before watcher starts.
func (m *Manager) scanForExistingSessionFile(sessionsDir, temporaryID string) string {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		m.logger.Debug("failed to scan sessions dir", "error", err, "path", sessionsDir)
		return ""
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()

		// Skip non-.jsonl files
		if !strings.HasSuffix(filename, ".jsonl") {
			continue
		}

		// Skip agent sub-sessions
		if strings.HasPrefix(filename, "agent-") {
			continue
		}

		sessionID := strings.TrimSuffix(filename, ".jsonl")

		// Skip if same as temporary ID
		if sessionID == temporaryID {
			continue
		}

		// Validate UUID format
		if _, err := uuid.Parse(sessionID); err != nil {
			continue
		}

		// Found a valid session file
		return sessionID
	}

	return ""
}

// startSessionWithID starts a managed session with a specific session ID.
// This is used when resuming a historical session where we know the exact session ID.
// Note: This is an internal helper - external callers should use StartSession.
func (m *Manager) startSessionWithID(workspaceID, sessionID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check workspace exists
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Check if session already exists
	if existing, ok := m.sessions[sessionID]; ok {
		// If workspace_id changed (e.g., workspace was re-added with new ID), update it
		if existing.WorkspaceID != workspaceID {
			m.logger.Info("Updating session workspace ID",
				"session_id", sessionID,
				"old_workspace_id", existing.WorkspaceID,
				"new_workspace_id", workspaceID,
			)
			existing.WorkspaceID = workspaceID
			// Also update the reverse mapping
			delete(m.activeSessionWorkspaces, sessionID)
			m.activeSessionWorkspaces[sessionID] = workspaceID
			m.activeSessions[workspaceID] = sessionID
		}
		return existing, nil
	}

	session := NewSession(sessionID, workspaceID)
	session.SetStatus(StatusStarting)

	// Create Claude manager for this session
	claudeManager := claude.NewManagerWithContext(
		m.hub,
		m.cfg.Claude.Command,
		m.cfg.Claude.Args,
		m.cfg.Claude.TimeoutMinutes,
		m.cfg.Claude.SkipPermissions,
		ws.Definition.Path,
		workspaceID,
		sessionID,
	)
	session.SetClaudeManager(claudeManager)

	// Create git tracker for this workspace
	gitTracker := git.NewTracker(ws.Definition.Path, m.cfg.Git.Command, m.hub)
	session.SetGitTracker(gitTracker)

	// Create file watcher (same as StartSession)
	if m.cfg.Watcher.Enabled {
		fileWatcher := watcher.NewWatcher(
			ws.Definition.Path,
			m.hub,
			m.cfg.Watcher.DebounceMS,
			m.cfg.Watcher.IgnorePatterns,
		)
		session.SetFileWatcher(fileWatcher)

		// Start file watcher
		if err := fileWatcher.Start(m.ctx); err != nil {
			m.logger.Warn("Failed to start file watcher", "error", err, "workspace_id", workspaceID)
		}
	}

	// Store session
	m.sessions[sessionID] = session
	session.SetStatus(StatusRunning)

	// Auto-activate this session for the workspace
	m.activeSessions[workspaceID] = sessionID
	m.activeSessionWorkspaces[sessionID] = workspaceID

	m.logger.Info("Started session with specific ID",
		"session_id", sessionID,
		"workspace_id", workspaceID,
		"workspace_name", ws.Definition.Name,
		"path", ws.Definition.Path,
	)

	return session, nil
}

// getMostRecentHistoricalSessionID returns the most recent Claude session ID
// from the historical sessions stored in ~/.claude/projects/<encoded-path>.
// Returns empty string if no historical session is found.
// Note: This method does NOT acquire the manager lock - caller must handle locking.
func (m *Manager) getMostRecentHistoricalSessionID(workspacePath string) string {
	// Create session cache for this workspace path
	cache, err := sessioncache.New(workspacePath)
	if err != nil {
		m.logger.Debug("Failed to create session cache for historical lookup",
			"path", workspacePath,
			"error", err,
		)
		return ""
	}
	defer func() { _ = cache.Stop() }()

	// Force sync to ensure we have fresh data from disk
	if err := cache.ForceSync(); err != nil {
		m.logger.Debug("Failed to sync session cache",
			"path", workspacePath,
			"error", err,
		)
	}

	// Get session list (already sorted by last_updated, most recent first)
	sessions, err := cache.ListSessions()
	if err != nil {
		m.logger.Debug("Failed to list sessions from cache",
			"path", workspacePath,
			"error", err,
		)
		return ""
	}

	if len(sessions) == 0 {
		return ""
	}

	// Return the most recent session ID
	return sessions[0].SessionID
}

// StopSession stops a running session.
// For managed sessions (started via cdev), this stops the Claude process.
// For LIVE sessions (watched but not started by cdev), this unwatches and clears active status.
func (m *Manager) StopSession(sessionID string) error {
	// First check if it's a managed session (started by cdev)
	m.mu.Lock()
	session, ok := m.sessions[sessionID]
	if ok {
		// It's a managed session - stop it while holding the lock
		err := m.stopSessionInternal(session)
		m.mu.Unlock()
		return err
	}

	// Not a managed session - check if it's a LIVE/watched session
	// Find the workspace that has this session as active
	var workspaceID string
	for wsID, activeID := range m.activeSessions {
		if activeID == sessionID {
			workspaceID = wsID
			break
		}
	}
	m.mu.Unlock() // Release m.mu before acquiring streamerMu to prevent deadlock

	if workspaceID == "" {
		// Check if we're currently streaming this session
		m.streamerMu.Lock()
		if m.streamerSessionID == sessionID {
			workspaceID = m.streamerWorkspaceID
		}
		m.streamerMu.Unlock()
	}

	if workspaceID == "" {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	// Stop watching/streaming the LIVE session
	m.stopWatchingLiveSession(workspaceID, sessionID)

	return nil
}

// stopWatchingLiveSession stops watching a LIVE session and clears its active status.
// IMPORTANT: Acquires locks in order: streamerMu first, then m.mu (same as WatchWorkspaceSession)
func (m *Manager) stopWatchingLiveSession(workspaceID, sessionID string) {
	// Stop the streamer if it's watching this session
	// Acquire streamerMu FIRST to match lock order in WatchWorkspaceSession
	m.streamerMu.Lock()
	if m.streamerSessionID == sessionID {
		if m.streamer != nil {
			m.streamer.UnwatchSession()
		}
		m.streamerSessionID = ""
		m.streamerWorkspaceID = ""
		m.streamerWatchers = make(map[string]bool)
	}
	m.streamerMu.Unlock()

	// Clear active session mapping (acquire m.mu AFTER streamerMu is released)
	m.mu.Lock()
	if m.activeSessions[workspaceID] == sessionID {
		delete(m.activeSessions, workspaceID)
	}
	delete(m.activeSessionWorkspaces, sessionID)
	m.mu.Unlock()

	// Emit session_stopped event for multi-device sync
	if m.hub != nil {
		m.hub.Publish(events.NewSessionStoppedEvent(workspaceID, sessionID))
	}

	m.logger.Info("Stopped watching LIVE session", "session_id", sessionID, "workspace_id", workspaceID)
}

// stopSessionInternal stops a session (must hold lock).
func (m *Manager) stopSessionInternal(session *Session) error {
	return m.stopSessionInternalWithContext(m.ctx, session)
}

// stopSessionInternalWithContext stops a session with a specific context (must hold lock).
func (m *Manager) stopSessionInternalWithContext(ctx context.Context, session *Session) error {
	if session.GetStatus() != StatusRunning && session.GetStatus() != StatusStarting {
		return nil // Already stopped
	}

	session.SetStatus(StatusStopping)

	// Cancel session file watcher if active (prevents memory leak)
	// Note: CancelSessionFileWatcher uses its own mutex, safe to call while holding m.mu
	m.CancelSessionFileWatcher(session.WorkspaceID)

	// Stop Claude manager with the provided context
	if cm := session.ClaudeManager(); cm != nil {
		if err := cm.Stop(ctx); err != nil {
			m.logger.Warn("Error stopping Claude manager", "error", err, "session_id", session.ID)
		}
	}

	// Stop file watcher
	if fw := session.FileWatcher(); fw != nil {
		_ = fw.Stop()
	}

	session.SetStatus(StatusStopped)

	// Clear active session mapping for this workspace
	// This ensures workspace/list doesn't show the stopped session as "running"
	if m.activeSessions[session.WorkspaceID] == session.ID {
		delete(m.activeSessions, session.WorkspaceID)
	}
	delete(m.activeSessionWorkspaces, session.ID)

	// Emit session_stopped event for multi-device sync
	// All connected clients will receive this notification
	if m.hub != nil {
		m.hub.Publish(events.NewSessionStoppedEvent(session.WorkspaceID, session.ID))
	}

	m.logger.Info("Stopped session", "session_id", session.ID, "workspace_id", session.WorkspaceID)

	return nil
}

// GetSession returns a session by ID.
func (m *Manager) GetSession(sessionID string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	return session, nil
}

// GetSessionForWorkspace returns the first active session for a workspace (if any).
// Used for operations that need any session (e.g., git operations).
func (m *Manager) GetSessionForWorkspace(workspaceID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, session := range m.sessions {
		if session.WorkspaceID == workspaceID && session.GetStatus() == StatusRunning {
			return session
		}
	}
	return nil
}

// GetSessionsForWorkspace returns all sessions for a workspace.
func (m *Manager) GetSessionsForWorkspace(workspaceID string) []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Session, 0)
	for _, session := range m.sessions {
		if session.WorkspaceID == workspaceID {
			result = append(result, session)
		}
	}
	return result
}

// ActivateSession sets the active session for a workspace.
// This allows iOS clients to switch which session they are viewing/interacting with.
// Multiple clients can have different active sessions for the same workspace.
func (m *Manager) ActivateSession(workspaceID, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Verify workspace exists
	if _, ok := m.workspaces[workspaceID]; !ok {
		return fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Store the active session for this workspace
	// Note: This is a simple implementation that tracks one active session per workspace.
	// In the future, this could be extended to track per-client active sessions.
	m.activeSessions[workspaceID] = sessionID

	// Store reverse mapping: session ID -> workspace ID (for LIVE session lookup)
	m.activeSessionWorkspaces[sessionID] = workspaceID

	m.logger.Info("Activated session",
		"workspace_id", workspaceID,
		"session_id", sessionID,
	)

	return nil
}

// GetActiveSession returns the active session ID for a workspace.
// Returns empty string if no session is explicitly activated.
func (m *Manager) GetActiveSession(workspaceID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.activeSessions[workspaceID]
}

// updateSessionID updates internal state when the real session ID is detected.
// This is called when Claude creates a new session file with its own UUID.
// It updates all internal mappings so workspace/list and other APIs return the real ID.
//
// Thread-safety: This function is idempotent - calling it multiple times with the same
// arguments is safe and will only update state on the first call.
//
// Lock ordering: Manager.mu is acquired first, then Session.mu via SetSessionID().
// This is consistent with other methods to avoid deadlocks.
func (m *Manager) updateSessionID(workspaceID, temporaryID, realID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Idempotency check: if session already exists with realID, skip
	if _, exists := m.sessions[realID]; exists {
		m.logger.Debug("Session ID already updated (idempotent skip)",
			"workspace_id", workspaceID,
			"real_id", realID,
		)
		return
	}

	// Check if session with temporaryID exists
	session, ok := m.sessions[temporaryID]
	if !ok {
		m.logger.Warn("Session not found for ID update",
			"workspace_id", workspaceID,
			"temporary_id", temporaryID,
			"real_id", realID,
		)
		return
	}

	// Update active session mapping
	if m.activeSessions[workspaceID] == temporaryID {
		m.activeSessions[workspaceID] = realID
	}

	// Update reverse mapping (session ID -> workspace ID)
	delete(m.activeSessionWorkspaces, temporaryID)
	m.activeSessionWorkspaces[realID] = workspaceID

	// Update session's internal ID (thread-safe via SetSessionID)
	// This also updates ClaudeManager's sessionID for PTY events
	session.SetSessionID(realID)

	// Re-key in sessions map
	delete(m.sessions, temporaryID)
	m.sessions[realID] = session

	m.logger.Info("Updated session ID mapping",
		"workspace_id", workspaceID,
		"temporary_id", temporaryID,
		"real_id", realID,
	)
}

// CountActiveSessionsForWorkspace returns the count of active sessions for a workspace.
func (m *Manager) CountActiveSessionsForWorkspace(workspaceID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, session := range m.sessions {
		if session.WorkspaceID == workspaceID && session.GetStatus() == StatusRunning {
			count++
		}
	}
	return count
}

// ListSessions returns all sessions, optionally filtered by workspace.
func (m *Manager) ListSessions(workspaceID string) []*Info {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Info, 0)
	for _, session := range m.sessions {
		if workspaceID == "" || session.WorkspaceID == workspaceID {
			result = append(result, session.ToInfo())
		}
	}
	return result
}

// SendPrompt sends a prompt to a session's Claude instance.
// Supports both managed sessions (started by cdev) and LIVE sessions (user's terminal).
// permissionMode controls how Claude handles permissions:
// - "default": Standard permission prompts (may hang if stdin is closed)
// - "acceptEdits": Auto-accept file edits
// - "bypassPermissions": Skip all permission checks
// - "plan": Plan mode only
// - "interactive": Use PTY mode for true interactive permission handling
func (m *Manager) SendPrompt(sessionID string, prompt string, mode string, permissionMode string) error {
	// First try to get from managed sessions
	session, err := m.GetSession(sessionID)

	// If not found in managed sessions, check for LIVE sessions or historical sessions
	if err != nil {
		m.mu.RLock()
		injector := m.liveInjector
		workspaceID := m.activeSessionWorkspaces[sessionID]
		m.mu.RUnlock()

		// If workspaceID not found in activeSessionWorkspaces, search all workspaces
		// This handles LIVE sessions and historical sessions that weren't explicitly activated
		if workspaceID == "" {
			workspaceID = m.findWorkspaceForSession(sessionID)
		}

		if workspaceID != "" {
			// Get workspace for context
			ws, wsErr := m.GetWorkspace(workspaceID)
			if wsErr == nil {
				// If permission_mode is "interactive", skip LIVE detection and spawn PTY
				// User explicitly wants PTY mode with pty_output/pty_permission events
				if permissionMode == "interactive" {
					log.Info().
						Str("session_id", sessionID).
						Str("workspace_id", workspaceID).
						Str("prompt", truncateString(prompt, 50)).
						Msg("interactive mode requested - spawning PTY session instead of LIVE injection")

					// Start a new managed session with the specific session ID
					newSession, startErr := m.startSessionWithID(workspaceID, sessionID)
					if startErr != nil {
						return fmt.Errorf("failed to start PTY session: %w", startErr)
					}

					// Use the new managed session
					session = newSession
					// Fall through to normal session handling below (will hit PTY code path)
				} else {
					// Not interactive mode - try LIVE session first, then fall back to managed session
					if injector != nil {
						detector := live.NewDetector(ws.Definition.Path)
						liveSession := detector.GetLiveSession(sessionID)
						if liveSession != nil {
							log.Info().
								Str("session_id", sessionID).
								Str("tty", liveSession.TTY).
								Int("pid", liveSession.PID).
								Str("terminal_app", liveSession.TerminalApp).
								Str("prompt", truncateString(prompt, 50)).
								Msg("sending prompt to LIVE session via keystroke injection")

							// Auto-activate the session for future calls
							m.mu.Lock()
							m.activeSessionWorkspaces[sessionID] = workspaceID
							m.activeSessions[workspaceID] = sessionID
							m.mu.Unlock()

							// For LIVE sessions, inject prompt with Enter to the specific terminal app
							if err := injector.SendWithEnterToApp(prompt, liveSession.TerminalApp); err != nil {
								return fmt.Errorf("failed to inject prompt to LIVE session: %w", err)
							}
							return nil
						}
					}

					// No LIVE session found - auto-start a managed session
					log.Info().
						Str("session_id", sessionID).
						Str("workspace_id", workspaceID).
						Str("prompt", truncateString(prompt, 50)).
						Msg("auto-starting managed session to resume historical session")

					// Start a new managed session with the specific session ID
					newSession, startErr := m.startSessionWithID(workspaceID, sessionID)
					if startErr != nil {
						return fmt.Errorf("failed to auto-start session for historical session %s: %w", sessionID, startErr)
					}

					// Update session reference and continue with the new managed session
					session = newSession
					// Fall through to normal session handling below
				}
			}
		}

		// If we still don't have a session, return original error
		if session == nil {
			return err
		}
	}

	// Handle interactive mode (PTY) - check before status check since we can restart stopped sessions
	if permissionMode == "interactive" {
		// Handle "!" prefix as bash command mode
		// When prompt starts with "!", execute it as a bash command directly
		if strings.HasPrefix(prompt, "!") {
			bashCmd := strings.TrimPrefix(prompt, "!")
			bashCmd = strings.TrimSpace(bashCmd)

			if bashCmd == "" {
				return fmt.Errorf("empty bash command after '!'")
			}

			log.Info().
				Str("session_id", sessionID).
				Str("bash_cmd", bashCmd).
				Msg("executing bash command (! prefix)")

			// Get workspace path for command execution
			ws, wsErr := m.GetWorkspace(session.WorkspaceID)
			if wsErr != nil {
				return fmt.Errorf("failed to get workspace for bash command: %w", wsErr)
			}

			// Execute bash command and emit output as pty_output event
			return m.executeBashCommand(session.WorkspaceID, sessionID, ws.Definition.Path, bashCmd)
		}

		// If session exists but is stopped, restart it for interactive mode
		if session.GetStatus() != StatusRunning {
			log.Info().
				Str("session_id", sessionID).
				Str("current_status", string(session.GetStatus())).
				Msg("session exists but not running - restarting for interactive mode")

			// Set status back to running
			session.SetStatus(StatusRunning)
		}

		cm := session.ClaudeManager()
		if cm == nil {
			return fmt.Errorf("no Claude manager for session: %s", sessionID)
		}

		session.UpdateLastActive()
		// Check if Claude is already running in PTY mode
		// If yes, send the prompt via PTY input instead of starting a new process
		if cm.IsPTYMode() && cm.IsRunning() {
			log.Info().
				Str("session_id", sessionID).
				Str("prompt", truncateString(prompt, 50)).
				Msg("sending prompt to existing PTY session")

			// Type the prompt text
			if err := cm.SendPTYInput(prompt); err != nil {
				return fmt.Errorf("failed to send prompt to PTY: %w", err)
			}

			// Small delay for TUI to process the input
			time.Sleep(100 * time.Millisecond)

			// Press Enter to submit
			if err := cm.SendPTYInput("\r"); err != nil {
				return fmt.Errorf("failed to send Enter to PTY: %w", err)
			}

			return nil
		}

		// Claude not running in PTY mode, start new process
		claudeMode := claude.SessionModeNew
		claudeSessionID := ""
		if mode == "continue" {
			claudeMode = claude.SessionModeContinue
			claudeSessionID = sessionID
		}

		// Determine session ID for watching JSONL
		watchSessionID := session.ID
		if claudeSessionID != "" {
			watchSessionID = claudeSessionID
		}

		// Start the PTY session
		if err := cm.StartWithPTY(m.ctx, prompt, claudeMode, claudeSessionID); err != nil {
			return err
		}

		// Start watching the JSONL session file for claude_message events
		// This allows cdev-ios to receive structured messages in addition to pty_output events
		// watchSessionID already set above for the callback
		// Use internal client ID for server-initiated watches (won't be removed by client disconnect)
		if _, err := m.WatchWorkspaceSession("internal:pty-session", session.WorkspaceID, watchSessionID); err != nil {
			// Don't fail the whole operation if watch fails - PTY will still work
			// claude_message events just won't be emitted
			log.Warn().
				Str("session_id", watchSessionID).
				Str("workspace_id", session.WorkspaceID).
				Err(err).
				Msg("failed to start JSONL watch for PTY session (claude_message events may not be emitted)")
		} else {
			log.Info().
				Str("session_id", watchSessionID).
				Str("workspace_id", session.WorkspaceID).
				Msg("started JSONL watch for PTY session - will emit claude_message events")
		}

		return nil
	}

	// Non-interactive mode - check session status
	if session.GetStatus() != StatusRunning {
		return fmt.Errorf("session not running: %s (use session/start with resume_session_id to resume historical sessions)", sessionID)
	}

	cm := session.ClaudeManager()
	if cm == nil {
		return fmt.Errorf("no Claude manager for session: %s", sessionID)
	}

	session.UpdateLastActive()

	// Determine session mode and call appropriate method
	if mode == "continue" {
		// Use the cdev session ID which now equals the historical Claude session ID
		// (set in StartSession from getMostRecentHistoricalSessionID)
		return cm.StartWithSession(m.ctx, prompt, claude.SessionModeContinue, sessionID, permissionMode)
	}

	return cm.StartWithSession(m.ctx, prompt, claude.SessionModeNew, "", permissionMode)
}

// SendKey sends a special key (like arrow keys, enter, escape) to a session.
// For LIVE sessions, uses platform-specific key code injection instead of text keystroke.
func (m *Manager) SendKey(sessionID string, key string) error {
	// First try to get from managed sessions
	session, err := m.GetSession(sessionID)

	// If not found in managed sessions, check for LIVE sessions
	if err != nil {
		m.mu.RLock()
		injector := m.liveInjector
		workspaceID := m.activeSessionWorkspaces[sessionID]
		m.mu.RUnlock()

		// If workspaceID not found in activeSessionWorkspaces, search all workspaces
		if workspaceID == "" {
			workspaceID = m.findWorkspaceForSession(sessionID)
		}

		if injector != nil && workspaceID != "" {
			// Get workspace path for the detector
			ws, wsErr := m.GetWorkspace(workspaceID)
			if wsErr == nil {
				// Create detector for this workspace
				detector := live.NewDetector(ws.Definition.Path)
				liveSession := detector.GetLiveSession(sessionID)
				if liveSession != nil {
					log.Info().
						Str("session_id", sessionID).
						Str("tty", liveSession.TTY).
						Int("pid", liveSession.PID).
						Str("terminal_app", liveSession.TerminalApp).
						Str("key", key).
						Msg("sending key to LIVE session via key code injection")

					// Auto-activate the session for future calls
					m.mu.Lock()
					m.activeSessionWorkspaces[sessionID] = workspaceID
					m.activeSessions[workspaceID] = sessionID
					m.mu.Unlock()

					// For LIVE sessions, use SendKeyToApp which uses key codes
					if err := injector.SendKeyToApp(key, liveSession.TerminalApp); err != nil {
						return fmt.Errorf("failed to inject key to LIVE session: %w", err)
					}
					return nil
				}
			}
		}
		return err // Return original error if no LIVE session found
	}

	// For managed sessions, convert key to escape sequence and send via PTY
	if session.GetStatus() != StatusRunning {
		return fmt.Errorf("session not running: %s", sessionID)
	}

	cm := session.ClaudeManager()
	if cm == nil {
		return fmt.Errorf("no Claude manager for session: %s", sessionID)
	}

	if !cm.IsPTYMode() {
		return fmt.Errorf("session not in interactive mode (PTY): %s", sessionID)
	}

	// Convert key name to escape sequence for PTY
	var input string
	switch key {
	case "enter", "return":
		input = "\r"
	case "escape", "esc":
		input = "\x1b"
	case "up":
		input = "\x1b[A"
	case "down":
		input = "\x1b[B"
	case "right":
		input = "\x1b[C"
	case "left":
		input = "\x1b[D"
	case "tab":
		input = "\t"
	case "backspace":
		input = "\x7f"
	case "space":
		input = " "
	default:
		return fmt.Errorf("unknown key: %s", key)
	}

	session.UpdateLastActive()
	return cm.SendPTYInput(input)
}

// SendInput sends input to a session's Claude PTY (for interactive responses).
// Supports both managed sessions (started by cdev) and LIVE sessions (user's terminal).
// This is used to respond to permission prompts when running in interactive mode.
func (m *Manager) SendInput(sessionID string, input string) error {
	// First try to get from managed sessions
	session, err := m.GetSession(sessionID)

	// If not found in managed sessions, check for LIVE sessions
	if err != nil {
		m.mu.RLock()
		injector := m.liveInjector
		workspaceID := m.activeSessionWorkspaces[sessionID]
		m.mu.RUnlock()

		// If workspaceID not found in activeSessionWorkspaces, search all workspaces
		if workspaceID == "" {
			workspaceID = m.findWorkspaceForSession(sessionID)
		}

		if injector != nil && workspaceID != "" {
			// Get workspace path for the detector
			ws, wsErr := m.GetWorkspace(workspaceID)
			if wsErr == nil {
				// Create detector for this workspace
				detector := live.NewDetector(ws.Definition.Path)
				liveSession := detector.GetLiveSession(sessionID)
				if liveSession != nil {
					log.Info().
						Str("session_id", sessionID).
						Str("tty", liveSession.TTY).
						Int("pid", liveSession.PID).
						Str("terminal_app", liveSession.TerminalApp).
						Str("input", truncateString(input, 20)).
						Msg("sending input to LIVE session via keystroke injection")

					// Auto-activate the session for future calls
					m.mu.Lock()
					m.activeSessionWorkspaces[sessionID] = workspaceID
					m.activeSessions[workspaceID] = sessionID
					m.mu.Unlock()

					// For LIVE sessions, inject input to the specific terminal app
					if err := injector.SendToApp(input, liveSession.TerminalApp); err != nil {
						return fmt.Errorf("failed to inject input to LIVE session: %w", err)
					}
					return nil
				}
			}
		}
		return err // Return original error if no LIVE session found
	}

	if session.GetStatus() != StatusRunning {
		return fmt.Errorf("session not running: %s", sessionID)
	}

	cm := session.ClaudeManager()
	if cm == nil {
		return fmt.Errorf("no Claude manager for session: %s", sessionID)
	}

	if !cm.IsPTYMode() {
		return fmt.Errorf("session not in interactive mode (PTY): %s", sessionID)
	}

	session.UpdateLastActive()
	return cm.SendPTYInput(input)
}

// EmitPermissionResolved broadcasts a pty_permission_resolved event to all devices.
// This is called when one device responds to a permission prompt, so other devices
// can dismiss their permission popup.
func (m *Manager) EmitPermissionResolved(sessionID, clientID, input string) {
	// Get workspace ID for the session
	m.mu.RLock()
	workspaceID := m.activeSessionWorkspaces[sessionID]
	m.mu.RUnlock()

	// If not in activeSessionWorkspaces, try to find it
	if workspaceID == "" {
		workspaceID = m.findWorkspaceForSession(sessionID)
	}

	// Emit the event
	evt := events.NewPTYPermissionResolvedEvent(sessionID, workspaceID, clientID, input)
	m.hub.Publish(evt)

	m.logger.Info("emitted pty_permission_resolved event",
		"session_id", sessionID,
		"workspace_id", workspaceID,
		"resolved_by", clientID,
		"input", input,
	)
}

// RespondToPermission responds to a permission request in a session.
func (m *Manager) RespondToPermission(sessionID string, allow bool) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}

	if session.GetStatus() != StatusRunning {
		return fmt.Errorf("session not running: %s", sessionID)
	}

	cm := session.ClaudeManager()
	if cm == nil {
		return fmt.Errorf("no Claude manager for session: %s", sessionID)
	}

	session.UpdateLastActive()

	// Get pending tool use info
	toolUseID, _ := cm.GetPendingToolUse()
	if toolUseID == "" {
		return fmt.Errorf("no pending permission request")
	}

	if allow {
		return cm.SendResponse(toolUseID, "yes", false)
	}
	return cm.SendResponse(toolUseID, "no", false)
}

// RespondToQuestion responds to an interactive question in a session.
func (m *Manager) RespondToQuestion(sessionID string, response string) error {
	session, err := m.GetSession(sessionID)
	if err != nil {
		return err
	}

	if session.GetStatus() != StatusRunning {
		return fmt.Errorf("session not running: %s", sessionID)
	}

	cm := session.ClaudeManager()
	if cm == nil {
		return fmt.Errorf("no Claude manager for session: %s", sessionID)
	}

	session.UpdateLastActive()

	// Get pending tool use info for question response
	toolUseID, _ := cm.GetPendingToolUse()
	if toolUseID == "" {
		return fmt.Errorf("no pending question")
	}

	return cm.SendResponse(toolUseID, response, false)
}

// idleMonitor periodically checks for idle sessions and stops them.
func (m *Manager) idleMonitor() {
	if m.idleTimeout <= 0 {
		return // Idle timeout disabled
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkIdleSessions()
		}
	}
}

// checkIdleSessions checks for and stops idle sessions.
func (m *Manager) checkIdleSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for _, session := range m.sessions {
		if session.GetStatus() == StatusRunning {
			if now.Sub(session.GetLastActive()) > m.idleTimeout {
				m.logger.Info("Stopping idle session",
					"session_id", session.ID,
					"workspace_id", session.WorkspaceID,
					"idle_duration", now.Sub(session.GetLastActive()),
				)
				_ = m.stopSessionInternal(session)
			}
		}
	}
}

// getGitTracker returns a git tracker for a workspace.
// Uses the shared GitTrackerManager if available (cached), otherwise creates a temporary tracker.
func (m *Manager) getGitTracker(workspaceID string) (*git.Tracker, error) {
	// Try shared GitTrackerManager first (cached, efficient)
	if m.gitTrackerManager != nil {
		tracker, err := m.gitTrackerManager.GetTracker(workspaceID)
		if err != nil {
			return nil, err
		}
		if tracker != nil {
			return tracker, nil
		}
		// tracker is nil means not a git repo - return error
		return nil, fmt.Errorf("workspace is not a git repository: %s", workspaceID)
	}

	// Fallback: create temporary tracker (for backward compatibility)
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	tracker := git.NewTracker(ws.Definition.Path, m.cfg.Git.Command, nil)
	if !tracker.IsGitRepo() {
		return nil, fmt.Errorf("workspace is not a git repository: %s", workspaceID)
	}
	return tracker, nil
}

// GetGitStatus returns git status for a workspace.
func (m *Manager) GetGitStatus(workspaceID string) (interface{}, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Status(m.ctx)
}

// GetGitDiff returns git diff for a workspace.
// If staged is true, returns staged changes; otherwise returns unstaged changes.
func (m *Manager) GetGitDiff(workspaceID string, staged bool) (string, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return "", err
	}
	if staged {
		return tracker.DiffStaged(m.ctx, "")
	}
	return tracker.Diff(m.ctx, "")
}

// GetGitDiffWithMeta returns git diff with metadata (isStaged, isNew).
// This matches the git/diff response format.
func (m *Manager) GetGitDiffWithMeta(workspaceID string, path string) (diff string, isStaged bool, isNew bool, err error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return "", false, false, err
	}

	// Try unstaged diff first
	diff, err = tracker.Diff(m.ctx, path)
	if err == nil && diff != "" {
		return diff, false, false, nil
	}

	// Try staged diff
	diff, err = tracker.DiffStaged(m.ctx, path)
	if err == nil && diff != "" {
		return diff, true, false, nil
	}

	// Try new file diff
	diff, err = tracker.DiffNewFile(m.ctx, path)
	if err == nil && diff != "" {
		return diff, false, true, nil
	}

	return "", false, false, err
}

// GetGitEnhancedStatus returns enhanced git status for a workspace.
func (m *Manager) GetGitEnhancedStatus(workspaceID string) (*git.EnhancedStatus, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.GetEnhancedStatus(m.ctx)
}

// GitStage stages files for a workspace.
func (m *Manager) GitStage(workspaceID string, paths []string) error {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return err
	}
	return tracker.Stage(m.ctx, paths)
}

// GitUnstage unstages files for a workspace.
func (m *Manager) GitUnstage(workspaceID string, paths []string) error {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return err
	}
	return tracker.Unstage(m.ctx, paths)
}

// GitDiscard discards changes for a workspace.
func (m *Manager) GitDiscard(workspaceID string, paths []string) error {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return err
	}
	return tracker.Discard(m.ctx, paths)
}

// GitCommit commits staged changes for a workspace.
func (m *Manager) GitCommit(workspaceID string, message string, push bool) (*git.CommitResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Commit(m.ctx, message, push)
}

// GitPush pushes commits to remote for a workspace.
func (m *Manager) GitPush(workspaceID string, force bool, setUpstream bool, remote, branch string) (*git.PushResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Push(m.ctx, force, setUpstream, remote, branch)
}

// GitPull pulls changes from remote for a workspace.
func (m *Manager) GitPull(workspaceID string, rebase bool) (*git.PullResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Pull(m.ctx, rebase)
}

// GitBranches lists branches for a workspace.
func (m *Manager) GitBranches(workspaceID string) (*git.BranchesResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.ListBranches(m.ctx)
}

// GitCheckout checks out a branch for a workspace.
func (m *Manager) GitCheckout(workspaceID string, branch string, create bool) (*git.CheckoutResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	result, err := tracker.Checkout(m.ctx, branch, create)
	if err != nil {
		return nil, err
	}

	// Emit git_branch_changed event if branch actually changed
	if result.FromBranch != "" && result.FromBranch != result.Branch {
		event := events.NewGitBranchChangedEvent(workspaceID, result.FromBranch, result.Branch, "")
		m.hub.Publish(event)
	}

	return result, nil
}

// GitDeleteBranch deletes a branch for a workspace.
func (m *Manager) GitDeleteBranch(workspaceID string, branch string, force bool) (*git.DeleteBranchResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.DeleteBranch(m.ctx, branch, force)
}

// GitFetch fetches from a remote for a workspace.
func (m *Manager) GitFetch(workspaceID string, remote string, prune bool) (*git.FetchResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Fetch(m.ctx, remote, prune)
}

// GitLog returns the commit log for a workspace.
func (m *Manager) GitLog(workspaceID string, limit int, skip int, branch string, path string, graph bool) (*git.LogResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Log(m.ctx, limit, skip, branch, path, graph)
}

// GitStash creates a stash for a workspace.
func (m *Manager) GitStash(workspaceID string, message string, includeUntracked bool) (*git.StashResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Stash(m.ctx, message, includeUntracked)
}

// GitStashList lists stashes for a workspace.
func (m *Manager) GitStashList(workspaceID string) (*git.StashListResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.StashList(m.ctx)
}

// GitStashApply applies a stash for a workspace.
func (m *Manager) GitStashApply(workspaceID string, index int) (*git.StashResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.StashApply(m.ctx, index)
}

// GitStashPop pops a stash for a workspace.
func (m *Manager) GitStashPop(workspaceID string, index int) (*git.StashResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.StashPop(m.ctx, index)
}

// GitStashDrop drops a stash for a workspace.
func (m *Manager) GitStashDrop(workspaceID string, index int) (*git.StashResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.StashDrop(m.ctx, index)
}

// GitMerge merges a branch for a workspace.
func (m *Manager) GitMerge(workspaceID string, branch string, noFastForward bool, message string) (*git.MergeResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.Merge(m.ctx, branch, noFastForward, message)
}

// GitMergeAbort aborts a merge for a workspace.
func (m *Manager) GitMergeAbort(workspaceID string) (*git.MergeResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.MergeAbort(m.ctx)
}

// GitInit initializes a git repository for a workspace.
// This method works on non-git directories to initialize them.
func (m *Manager) GitInit(workspaceID string, initialBranch string, initialCommit bool, commitMessage string) (*git.InitResult, error) {
	// Get workspace path - don't use getGitTracker as it rejects non-git repos
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	// Create a tracker for the directory (works even if not a git repo yet)
	tracker := git.NewTracker(ws.Definition.Path, m.cfg.Git.Command, nil)

	// Initialize the git repository
	result, err := tracker.Init(m.ctx, initialBranch, initialCommit, commitMessage)
	if err != nil {
		return nil, err
	}

	// Refresh the tracker in GitTrackerManager to update cached state
	// Do this even on failure (e.g., "already a git repo") to sync the state
	if m.gitTrackerManager != nil {
		if err := m.gitTrackerManager.RefreshTracker(workspaceID); err != nil {
			m.logger.Warn("Failed to refresh git tracker after init",
				"workspace_id", workspaceID,
				"error", err,
			)
		}

		// Emit git_status_changed to update clients
		m.emitGitStatusChanged(m.ctx, workspaceID, tracker)
	}

	return result, nil
}

// GitRemoteAdd adds a remote to a workspace.
func (m *Manager) GitRemoteAdd(workspaceID string, name string, url string, fetch bool) (*git.RemoteAddResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	result, err := tracker.RemoteAdd(m.ctx, name, url, fetch)
	if err == nil && result.Success {
		// Emit git_status_changed to update clients (state changes from no_remote to no_push)
		m.emitGitStatusChanged(m.ctx, workspaceID, tracker)
	}
	return result, err
}

// GitRemoteList lists remotes for a workspace.
func (m *Manager) GitRemoteList(workspaceID string) (*git.RemoteListResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.RemoteList(m.ctx)
}

// GitRemoteRemove removes a remote from a workspace.
func (m *Manager) GitRemoteRemove(workspaceID string, name string) (*git.RemoteRemoveResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	result, err := tracker.RemoteRemove(m.ctx, name)
	if err == nil && result.Success {
		// Emit git_status_changed to update clients (state may change if last remote removed)
		m.emitGitStatusChanged(m.ctx, workspaceID, tracker)
	}
	return result, err
}

// GitSetUpstream sets the upstream for a branch.
func (m *Manager) GitSetUpstream(workspaceID string, branch string, upstream string) (*git.SetUpstreamResult, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	result, err := tracker.SetUpstream(m.ctx, branch, upstream)
	if err == nil && result.Success {
		// Emit git_status_changed to update clients (state changes from no_push to synced/diverged)
		m.emitGitStatusChanged(m.ctx, workspaceID, tracker)
	}
	return result, err
}

// GitGetStatus returns the comprehensive git status for a workspace.
func (m *Manager) GitGetStatus(workspaceID string) (*git.Status, error) {
	tracker, err := m.getGitTracker(workspaceID)
	if err != nil {
		return nil, err
	}
	return tracker.GetStatus(m.ctx)
}

// HistoryInfo represents historical session information from the session cache.
type HistoryInfo struct {
	SessionID    string    `json:"session_id"`
	Summary      string    `json:"summary"`
	MessageCount int       `json:"message_count"`
	LastUpdated  time.Time `json:"last_updated"`
	Branch       string    `json:"branch,omitempty"`
}

// ListHistory returns historical Claude sessions for a workspace.
// This reads from the Claude session cache at ~/.claude/projects/<encoded-path>
func (m *Manager) ListHistory(workspaceID string, limit int) ([]HistoryInfo, error) {
	// Get workspace path
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	// Create session cache for this workspace path
	cache, err := sessioncache.New(ws.Definition.Path)
	if err != nil {
		m.logger.Warn("Failed to create session cache",
			"workspace_id", workspaceID,
			"path", ws.Definition.Path,
			"error", err,
		)
		return []HistoryInfo{}, nil // Return empty list on error
	}
	defer func() { _ = cache.Stop() }()

	// Force sync to ensure we have fresh data from disk
	if err := cache.ForceSync(); err != nil {
		m.logger.Warn("Failed to sync session cache",
			"workspace_id", workspaceID,
			"error", err,
		)
	}

	// Get session list
	sessions, err := cache.ListSessions()
	if err != nil {
		m.logger.Warn("Failed to list sessions from cache",
			"workspace_id", workspaceID,
			"error", err,
		)
		return []HistoryInfo{}, nil
	}

	// Apply limit
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	// Convert to HistoryInfo
	result := make([]HistoryInfo, len(sessions))
	for i, s := range sessions {
		result[i] = HistoryInfo{
			SessionID:    s.SessionID,
			Summary:      s.Summary,
			MessageCount: s.MessageCount,
			LastUpdated:  s.LastUpdated,
			Branch:       s.Branch,
		}
	}

	return result, nil
}

// SessionMessage represents a message from a Claude session.
// This struct matches the format in methods.SessionMessage for consistency.
type SessionMessage struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Type      string          `json:"type"`
	UUID      string          `json:"uuid,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	GitBranch string          `json:"git_branch,omitempty"`
	Message   json.RawMessage `json:"message"`

	// IsContextCompaction is true when this is an auto-generated message
	// created by Claude Code when the context window was maxed out.
	IsContextCompaction bool `json:"is_context_compaction,omitempty"`

	// IsMeta is true for system-generated metadata messages (e.g., command caveats).
	IsMeta bool `json:"is_meta,omitempty"`
}

// SessionMessagesResult represents the result of GetSessionMessages.
type SessionMessagesResult struct {
	SessionID   string           `json:"session_id"`
	Messages    []SessionMessage `json:"messages"`
	Total       int              `json:"total"`
	Limit       int              `json:"limit"`
	Offset      int              `json:"offset"`
	HasMore     bool             `json:"has_more"`
	QueryTimeMs float64          `json:"query_time_ms"`
}

// GetSessionMessages returns messages from a historical Claude session.
// This reads from the Claude session files at ~/.claude/projects/<encoded-path>
func (m *Manager) GetSessionMessages(workspaceID, sessionID string, limit, offset int, order string) (*SessionMessagesResult, error) {
	startTime := time.Now()

	// Get workspace path
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	// Get sessions directory for this workspace
	sessionsDir := getSessionsDir(ws.Definition.Path)

	// Create message cache for this workspace
	messageCache, err := sessioncache.NewMessageCache(sessionsDir)
	if err != nil {
		m.logger.Warn("Failed to create message cache",
			"workspace_id", workspaceID,
			"sessions_dir", sessionsDir,
			"error", err,
		)
		return nil, fmt.Errorf("failed to create message cache: %w", err)
	}
	defer func() { _ = messageCache.Close() }()

	// Get messages
	page, err := messageCache.GetMessages(sessionID, limit, offset, order)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}

	// Convert to SessionMessage (matching methods.SessionMessage format)
	messages := make([]SessionMessage, len(page.Messages))
	for i, msg := range page.Messages {
		messages[i] = SessionMessage{
			ID:                  msg.ID,
			SessionID:           msg.SessionID,
			Type:                msg.Type,
			UUID:                msg.UUID,
			Timestamp:           msg.Timestamp,
			GitBranch:           msg.GitBranch,
			Message:             msg.Message,
			IsContextCompaction: msg.IsContextCompaction,
			IsMeta:              msg.IsMeta,
		}
	}

	queryTimeMs := float64(time.Since(startTime).Microseconds()) / 1000.0

	return &SessionMessagesResult{
		SessionID:   sessionID,
		Messages:    messages,
		Total:       page.Total,
		Limit:       page.Limit,
		Offset:      page.Offset,
		HasMore:     page.HasMore,
		QueryTimeMs: queryTimeMs,
	}, nil
}

// getSessionsDir returns the Claude sessions directory for a repo path.
// Maps /Users/brianly/Projects/cdev -> ~/.claude/projects/-Users-brianly-Projects-cdev
func getSessionsDir(repoPath string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	repoPath = filepath.Clean(repoPath)
	encodedPath := strings.ReplaceAll(repoPath, "/", "-")

	return filepath.Join(homeDir, ".claude", "projects", encodedPath)
}

// WatchInfo contains information about the currently watched session.
type WatchInfo struct {
	WorkspaceID string `json:"workspace_id"`
	SessionID   string `json:"session_id"`
	Watching    bool   `json:"watching"`
}

// WatchWorkspaceSession starts watching a session file for live message updates.
// This is used by iOS to receive real-time claude_message events when new messages
// are added to the session file.
//
// Multiple clients can watch the same session. The streamer uses client tracking
// to keep watching until the last client stops watching.
// The clientID parameter identifies the client making this request.
func (m *Manager) WatchWorkspaceSession(clientID, workspaceID, sessionID string) (*WatchInfo, error) {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	// Get workspace path
	ws, err := m.GetWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	// Get sessions directory for this workspace
	sessionsDir := getSessionsDir(ws.Definition.Path)

	// Verify session file exists
	sessionPath := filepath.Join(sessionsDir, sessionID+".jsonl")
	if _, err := os.Stat(sessionPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		return nil, fmt.Errorf("failed to access session file: %w", err)
	}

	// Check if already watching the same session
	if m.streamer != nil && m.streamerWorkspaceID == workspaceID && m.streamerSessionID == sessionID {
		// Same session - just add this client to watchers
		m.streamerWatchers[clientID] = true
		m.logger.Info("Added watcher to existing session watch",
			"client_id", clientID,
			"workspace_id", workspaceID,
			"session_id", sessionID,
			"watcher_count", len(m.streamerWatchers),
		)
		return &WatchInfo{
			WorkspaceID: workspaceID,
			SessionID:   sessionID,
			Watching:    true,
		}, nil
	}

	// Switching to a different session - close existing streamer
	if m.streamer != nil {
		m.streamer.Close()
		m.streamer = nil
		m.logger.Debug("Stopped previous session watch (switching sessions)",
			"workspace_id", m.streamerWorkspaceID,
			"session_id", m.streamerSessionID,
			"prev_watcher_count", len(m.streamerWatchers),
		)
		// Clear all watchers since we're switching sessions
		m.streamerWatchers = make(map[string]bool)
	}

	// Create new streamer for this workspace's sessions directory
	m.streamer = sessioncache.NewSessionStreamer(sessionsDir, m.hub)

	// Start watching the session
	if err := m.streamer.WatchSession(sessionID); err != nil {
		m.streamer = nil
		return nil, fmt.Errorf("failed to start session watch: %w", err)
	}

	m.streamerWorkspaceID = workspaceID
	m.streamerSessionID = sessionID
	m.streamerWatchers[clientID] = true // First watcher

	// Auto-activate the watched session (uses separate mutex, no deadlock)
	m.mu.Lock()
	m.activeSessions[workspaceID] = sessionID
	m.mu.Unlock()

	m.logger.Info("Started watching session for live updates",
		"client_id", clientID,
		"workspace_id", workspaceID,
		"session_id", sessionID,
		"sessions_dir", sessionsDir,
		"watcher_count", len(m.streamerWatchers),
	)

	return &WatchInfo{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Watching:    true,
	}, nil
}

// UnwatchWorkspaceSession stops watching the current session for one client.
// The streamer continues running until all clients have stopped watching.
// The clientID parameter identifies the client making this request.
func (m *Manager) UnwatchWorkspaceSession(clientID string) *WatchInfo {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil {
		return &WatchInfo{
			Watching: false,
		}
	}

	prevWorkspaceID := m.streamerWorkspaceID
	prevSessionID := m.streamerSessionID

	// Remove this client from watchers (if they were watching)
	if !m.streamerWatchers[clientID] {
		// Client wasn't watching - nothing to do
		return &WatchInfo{
			WorkspaceID: prevWorkspaceID,
			SessionID:   prevSessionID,
			Watching:    len(m.streamerWatchers) > 0,
		}
	}
	delete(m.streamerWatchers, clientID)

	if len(m.streamerWatchers) > 0 {
		// Other clients still watching - keep the streamer running
		m.logger.Info("Removed watcher from session (others still watching)",
			"client_id", clientID,
			"workspace_id", prevWorkspaceID,
			"session_id", prevSessionID,
			"remaining_watchers", len(m.streamerWatchers),
		)
		return &WatchInfo{
			WorkspaceID: prevWorkspaceID,
			SessionID:   prevSessionID,
			Watching:    true, // Streamer still active
		}
	}

	// Last watcher - close the streamer
	m.streamer.Close()
	m.streamer = nil
	m.streamerWorkspaceID = ""
	m.streamerSessionID = ""

	m.logger.Info("Stopped watching session (last watcher left)",
		"client_id", clientID,
		"workspace_id", prevWorkspaceID,
		"session_id", prevSessionID,
	)

	return &WatchInfo{
		WorkspaceID: prevWorkspaceID,
		SessionID:   prevSessionID,
		Watching:    false,
	}
}

// GetWatchedSession returns information about the currently watched session.
func (m *Manager) GetWatchedSession() *WatchInfo {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil {
		return &WatchInfo{
			Watching: false,
		}
	}

	return &WatchInfo{
		WorkspaceID: m.streamerWorkspaceID,
		SessionID:   m.streamerSessionID,
		Watching:    true,
	}
}

// truncateString truncates a string to the specified length with ellipsis.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// watchGitState watches ONLY the .git directory for state changes and emits git_status_changed events.
// This is designed to be lightweight and not conflict with IDEs (VS Code, IntelliJ) or tools (SourceTree).
//
// We intentionally DO NOT watch the working directory because:
// 1. IDEs already watch working directory files - adding another watcher causes contention
// 2. Working directory changes don't affect git state until staged (git add)
// 3. The existing file_changed events already notify about working directory changes
//
// This covers:
// - Staging/unstaging: .git/index changes (git add, git reset)
// - Commits: .git/HEAD, .git/refs/heads/<branch> changes
// - Branch switches: .git/HEAD changes (git checkout, git switch)
// - Pull/Fetch: .git/FETCH_HEAD, .git/refs/remotes/* changes
// - Merges/Rebases: .git/ORIG_HEAD, .git/MERGE_HEAD changes
func (m *Manager) watchGitIndex(ctx context.Context, repoPath, workspaceID string, tracker *git.Tracker) {
	gitDir := filepath.Join(repoPath, ".git")

	// Check if .git directory exists
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		m.logger.Debug("No .git directory, skipping git state watcher", "workspace_id", workspaceID)
		return
	}

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		m.logger.Warn("Failed to create git state watcher", "error", err, "workspace_id", workspaceID)
		return
	}
	defer func() { _ = watcher.Close() }()

	// Watch .git directory for index, HEAD, FETCH_HEAD, ORIG_HEAD, MERGE_HEAD
	if err := watcher.Add(gitDir); err != nil {
		m.logger.Warn("Failed to watch .git directory", "error", err, "workspace_id", workspaceID)
		return
	}

	// Watch .git/refs/heads for branch commits
	refsHeads := filepath.Join(gitDir, "refs", "heads")
	if _, err := os.Stat(refsHeads); err == nil {
		if err := watcher.Add(refsHeads); err != nil {
			m.logger.Debug("Failed to watch refs/heads", "error", err, "workspace_id", workspaceID)
		}
	}

	// Watch .git/refs/remotes for pull/fetch updates
	refsRemotes := filepath.Join(gitDir, "refs", "remotes")
	if _, err := os.Stat(refsRemotes); err == nil {
		if err := watcher.Add(refsRemotes); err != nil {
			m.logger.Debug("Failed to watch refs/remotes", "error", err, "workspace_id", workspaceID)
		}
		// Also watch subdirectories (e.g., refs/remotes/origin)
		entries, _ := os.ReadDir(refsRemotes)
		for _, entry := range entries {
			if entry.IsDir() {
				remotePath := filepath.Join(refsRemotes, entry.Name())
				if err := watcher.Add(remotePath); err != nil {
					m.logger.Debug("Failed to watch remote", "remote", entry.Name(), "error", err)
				}
			}
		}
	}

	m.logger.Info("Started git state watcher (watching .git only - IDE/SourceTree safe)",
		"workspace_id", workspaceID,
		"git_dir", gitDir,
	)

	// Wait for startup activity to settle before processing events
	// This prevents initial burst of events during application startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
		// Drain any pending events from startup
		for {
			select {
			case <-watcher.Events:
			default:
				goto startWatching
			}
		}
	}

startWatching:
	m.logger.Debug("Git state watcher now active", "workspace_id", workspaceID)

	// Files in .git that trigger git_status_changed events
	gitTriggerFiles := map[string]bool{
		"index":       true, // Staging/unstaging
		"HEAD":        true, // Commits, branch switches
		"FETCH_HEAD":  true, // Fetch/pull
		"ORIG_HEAD":   true, // Merges, rebases
		"MERGE_HEAD":  true, // Merge in progress
		"REBASE_HEAD": true, // Rebase in progress
	}

	// Throttle + Debounce: emit at most once per minInterval, with debounce for settling
	const debounceDelay = 500 * time.Millisecond
	const minInterval = 1 * time.Second // Minimum time between emits
	var debounceTimer *time.Timer
	var debounceTimerMu sync.Mutex
	var lastEmit time.Time

	for {
		select {
		case <-ctx.Done():
			m.logger.Debug("Git state watcher stopped", "workspace_id", workspaceID)
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Only trigger for specific .git files that indicate state changes
			fileName := filepath.Base(event.Name)
			relPath, _ := filepath.Rel(gitDir, event.Name)

			// Debug log all events to diagnose issues
			m.logger.Debug("Git watcher event received",
				"file", fileName,
				"path", relPath,
				"op", event.Op.String(),
				"workspace_id", workspaceID,
			)

			shouldTrigger := false

			// Check for specific trigger files in .git root
			// Also check for index.lock as Git uses atomic rename
			if gitTriggerFiles[fileName] || fileName == "index.lock" {
				shouldTrigger = true
			}

			// Also trigger for any file in refs/heads or refs/remotes
			if strings.HasPrefix(relPath, "refs/heads") || strings.HasPrefix(relPath, "refs/remotes") {
				shouldTrigger = true
			}

			if !shouldTrigger {
				m.logger.Debug("Git event ignored (not a trigger file)", "file", fileName, "workspace_id", workspaceID)
				continue
			}

			m.logger.Info("Git event detected, scheduling status update",
				"file", fileName,
				"op", event.Op.String(),
				"workspace_id", workspaceID,
			)

			// Throttle + debounce: only schedule emit if not within minInterval of last emit
			debounceTimerMu.Lock()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				debounceTimerMu.Lock()
				// Check if we're within minInterval of last emit
				if time.Since(lastEmit) < minInterval {
					debounceTimerMu.Unlock()
					return
				}
				lastEmit = time.Now()
				debounceTimerMu.Unlock()
				m.emitGitStatusChanged(ctx, workspaceID, tracker)
			})
			debounceTimerMu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			m.logger.Warn("Git state watcher error", "error", err, "workspace_id", workspaceID)
		}
	}
}

// emitGitStatusChanged emits a git_status_changed event for a workspace.
func (m *Manager) emitGitStatusChanged(ctx context.Context, workspaceID string, tracker *git.Tracker) {
	// Use GetEnhancedStatus which provides all the info we need
	status, err := tracker.GetEnhancedStatus(ctx)
	if err != nil {
		m.logger.Debug("Failed to get git status for event", "error", err, "workspace_id", workspaceID)
		return
	}

	// Collect all changed file paths
	var changedFiles []string
	for _, f := range status.Staged {
		changedFiles = append(changedFiles, f.Path)
	}
	for _, f := range status.Unstaged {
		changedFiles = append(changedFiles, f.Path)
	}
	for _, f := range status.Untracked {
		changedFiles = append(changedFiles, f.Path)
	}
	for _, f := range status.Conflicted {
		changedFiles = append(changedFiles, f.Path)
	}

	payload := events.GitStatusChangedPayload{
		Branch:         status.Branch,
		StagedCount:    len(status.Staged),
		UnstagedCount:  len(status.Unstaged),
		UntrackedCount: len(status.Untracked),
		ChangedFiles:   changedFiles,
	}

	event := events.NewEvent(events.EventTypeGitStatusChanged, payload)
	event.SetContext(workspaceID, "")

	m.hub.Publish(event)
	m.logger.Info("Emitted git_status_changed",
		"workspace_id", workspaceID,
		"branch", status.Branch,
		"staged", len(status.Staged),
		"unstaged", len(status.Unstaged),
		"untracked", len(status.Untracked),
	)
}

// executeBashCommand executes a bash command and writes output to the session JSONL file.
// This is used for "!" prefix commands in interactive mode.
// It uses claude.AppendBashToSession to write in the correct Claude Code JSONL format.
func (m *Manager) executeBashCommand(workspaceID, sessionID, workDir, cmd string) error {
	// Execute command using bash
	bashCmd := exec.Command("bash", "-c", cmd)
	bashCmd.Dir = workDir

	// Capture stdout and stderr separately for correct JSONL format
	var stdout, stderr strings.Builder
	bashCmd.Stdout = &stdout
	bashCmd.Stderr = &stderr

	err := bashCmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	// Append to JSONL session file using the shared helper (correct Claude Code format)
	if appendErr := claude.AppendBashToSession(workDir, sessionID, cmd, stdout.String(), stderr.String()); appendErr != nil {
		log.Warn().Err(appendErr).Str("session_id", sessionID).Msg("failed to append bash command to session file")
	}

	// Emit pty_state event with idle state
	stateEvent := events.NewEvent(events.EventTypePTYState, events.PTYStatePayload{
		SessionID:       sessionID,
		State:           "idle",
		WaitingForInput: false,
	})
	stateEvent.SetContext(workspaceID, sessionID)
	m.hub.Publish(stateEvent)

	log.Info().
		Str("session_id", sessionID).
		Str("workspace_id", workspaceID).
		Str("cmd", cmd).
		Int("exit_code", exitCode).
		Msg("bash command executed")

	return nil
}
