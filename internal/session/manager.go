package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/adapters/watcher"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/google/uuid"
)

// Manager orchestrates multiple Claude sessions across workspaces.
type Manager struct {
	sessions          map[string]*Session             // keyed by session ID
	workspaces        map[string]*workspace.Workspace // keyed by workspace ID
	gitTrackerManager *workspace.GitTrackerManager
	hub               ports.EventHub
	cfg               *config.Config
	logger            *slog.Logger

	// Configuration
	idleTimeout time.Duration

	// Session streaming for live message updates
	streamer              *sessioncache.SessionStreamer
	streamerWorkspaceID   string // Workspace ID of currently watched session
	streamerSessionID     string // Session ID currently being watched
	streamerMu            sync.Mutex

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
		sessions:    make(map[string]*Session),
		workspaces:  make(map[string]*workspace.Workspace),
		hub:         hub,
		cfg:         cfg,
		logger:      logger,
		idleTimeout: idleTimeout,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetGitTrackerManager sets the shared git tracker manager.
func (m *Manager) SetGitTrackerManager(gtm *workspace.GitTrackerManager) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gitTrackerManager = gtm
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
	m.cancel()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop all active sessions
	for _, session := range m.sessions {
		if session.GetStatus() == StatusRunning {
			m.stopSessionInternal(session)
		}
	}

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
// Multiple sessions can run concurrently for the same workspace.
func (m *Manager) StartSession(workspaceID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check workspace exists
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, fmt.Errorf("workspace not found: %s", workspaceID)
	}

	// Create new session
	sessionID := uuid.New().String()
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

	// Store session
	m.sessions[sessionID] = session
	session.SetStatus(StatusRunning)

	m.logger.Info("Started session",
		"session_id", sessionID,
		"workspace_id", workspaceID,
		"workspace_name", ws.Definition.Name,
		"path", ws.Definition.Path,
	)

	return session, nil
}

// StopSession stops a running session.
func (m *Manager) StopSession(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	return m.stopSessionInternal(session)
}

// stopSessionInternal stops a session (must hold lock).
func (m *Manager) stopSessionInternal(session *Session) error {
	if session.GetStatus() != StatusRunning && session.GetStatus() != StatusStarting {
		return nil // Already stopped
	}

	session.SetStatus(StatusStopping)

	// Stop Claude manager
	if cm := session.ClaudeManager(); cm != nil {
		if err := cm.Stop(m.ctx); err != nil {
			m.logger.Warn("Error stopping Claude manager", "error", err, "session_id", session.ID)
		}
	}

	// Stop file watcher
	if fw := session.FileWatcher(); fw != nil {
		fw.Stop()
	}

	session.SetStatus(StatusStopped)

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
func (m *Manager) SendPrompt(sessionID string, prompt string, mode string) error {
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

	// Determine session mode and call appropriate method
	if mode == "continue" {
		claudeSessionID := cm.ClaudeSessionID()
		return cm.StartWithSession(m.ctx, prompt, claude.SessionModeContinue, claudeSessionID)
	}

	return cm.Start(m.ctx, prompt)
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
				m.stopSessionInternal(session)
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
	return tracker.Checkout(m.ctx, branch, create)
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
	defer cache.Stop()

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
	defer messageCache.Close()

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
// Only one session can be watched at a time. Calling this while already watching
// a session will stop the previous watch and start a new one.
func (m *Manager) WatchWorkspaceSession(workspaceID, sessionID string) (*WatchInfo, error) {
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

	// Stop existing streamer if any
	if m.streamer != nil {
		m.streamer.Close()
		m.streamer = nil
		m.logger.Debug("Stopped previous session watch",
			"workspace_id", m.streamerWorkspaceID,
			"session_id", m.streamerSessionID,
		)
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

	m.logger.Info("Started watching session for live updates",
		"workspace_id", workspaceID,
		"session_id", sessionID,
		"sessions_dir", sessionsDir,
	)

	return &WatchInfo{
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		Watching:    true,
	}, nil
}

// UnwatchWorkspaceSession stops watching the current session.
func (m *Manager) UnwatchWorkspaceSession() *WatchInfo {
	m.streamerMu.Lock()
	defer m.streamerMu.Unlock()

	if m.streamer == nil {
		return &WatchInfo{
			Watching: false,
		}
	}

	prevWorkspaceID := m.streamerWorkspaceID
	prevSessionID := m.streamerSessionID

	m.streamer.Close()
	m.streamer = nil
	m.streamerWorkspaceID = ""
	m.streamerSessionID = ""

	m.logger.Info("Stopped watching session",
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
