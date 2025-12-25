// Package app provides adapters that wrap existing components to implement RPC method interfaces.
package app

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
)

// StatusProvider interface implementation for App
// This allows App to be used with methods.StatusService

// ClaudeState returns the current Claude state.
func (a *App) ClaudeState() string {
	if a.claudeManager == nil {
		return "idle"
	}
	return string(a.claudeManager.State())
}

// ConnectedClients returns the number of connected clients.
func (a *App) ConnectedClients() int {
	if a.unifiedServer == nil {
		return 0
	}
	return a.unifiedServer.ClientCount()
}

// RepoPath returns the repository path.
func (a *App) RepoPath() string {
	return a.cfg.Repository.Path
}

// Version returns the server version.
func (a *App) Version() string {
	return a.version
}

// WatcherEnabled returns whether the file watcher is enabled.
func (a *App) WatcherEnabled() bool {
	return a.cfg.Watcher.Enabled && a.fileWatcher != nil
}

// GitEnabled returns whether git integration is enabled.
func (a *App) GitEnabled() bool {
	return a.cfg.Git.Enabled && a.gitTracker != nil
}

// ClaudeAgentAdapter wraps claude.Manager to implement methods.AgentManager.
type ClaudeAgentAdapter struct {
	manager *claude.Manager
}

// NewClaudeAgentAdapter creates a new Claude agent adapter.
func NewClaudeAgentAdapter(manager *claude.Manager) *ClaudeAgentAdapter {
	return &ClaudeAgentAdapter{manager: manager}
}

// StartWithSession starts the agent with a prompt and session configuration.
func (a *ClaudeAgentAdapter) StartWithSession(ctx context.Context, prompt string, mode methods.SessionMode, sessionID string) error {
	if a.manager == nil {
		return nil
	}
	return a.manager.StartWithSession(ctx, prompt, claude.SessionMode(mode), sessionID)
}

// Stop stops the running agent process.
func (a *ClaudeAgentAdapter) Stop(ctx context.Context) error {
	if a.manager == nil {
		return nil
	}
	return a.manager.Stop(ctx)
}

// SendResponse sends a response to an interactive prompt.
func (a *ClaudeAgentAdapter) SendResponse(toolUseID, response string, isError bool) error {
	if a.manager == nil {
		return nil
	}
	return a.manager.SendResponse(toolUseID, response, isError)
}

// State returns the current agent state.
func (a *ClaudeAgentAdapter) State() methods.AgentState {
	if a.manager == nil {
		return methods.AgentStateIdle
	}
	return methods.AgentState(a.manager.State())
}

// PID returns the process ID of the running agent.
func (a *ClaudeAgentAdapter) PID() int {
	if a.manager == nil {
		return 0
	}
	return a.manager.PID()
}

// SessionID returns the current session ID.
func (a *ClaudeAgentAdapter) SessionID() string {
	if a.manager == nil {
		return ""
	}
	return a.manager.ClaudeSessionID()
}

// AgentType returns the type of agent.
func (a *ClaudeAgentAdapter) AgentType() string {
	return "claude"
}

// GitProviderAdapter wraps git.Tracker to implement methods.GitProvider.
type GitProviderAdapter struct {
	tracker *git.Tracker
}

// NewGitProviderAdapter creates a new Git provider adapter.
func NewGitProviderAdapter(tracker *git.Tracker) *GitProviderAdapter {
	return &GitProviderAdapter{tracker: tracker}
}

// Status returns the git status matching HTTP API format.
func (a *GitProviderAdapter) Status(ctx context.Context) (methods.GitStatusInfo, error) {
	if a.tracker == nil {
		return methods.GitStatusInfo{}, nil
	}

	// Use GetEnhancedStatus for comprehensive info including branch
	enhanced, err := a.tracker.GetEnhancedStatus(ctx)
	if err != nil {
		return methods.GitStatusInfo{}, err
	}

	// Convert FileEntry arrays to GitFileStatus arrays
	staged := make([]methods.GitFileStatus, len(enhanced.Staged))
	for i, f := range enhanced.Staged {
		staged[i] = methods.GitFileStatus{Path: f.Path, Status: f.Status}
	}

	unstaged := make([]methods.GitFileStatus, len(enhanced.Unstaged))
	for i, f := range enhanced.Unstaged {
		unstaged[i] = methods.GitFileStatus{Path: f.Path, Status: f.Status}
	}

	untracked := make([]methods.GitFileStatus, len(enhanced.Untracked))
	for i, f := range enhanced.Untracked {
		untracked[i] = methods.GitFileStatus{Path: f.Path, Status: f.Status}
	}

	conflicted := make([]methods.GitFileStatus, len(enhanced.Conflicted))
	for i, f := range enhanced.Conflicted {
		conflicted[i] = methods.GitFileStatus{Path: f.Path, Status: f.Status}
	}

	return methods.GitStatusInfo{
		Branch:     enhanced.Branch,
		Upstream:   enhanced.Upstream,
		Ahead:      enhanced.Ahead,
		Behind:     enhanced.Behind,
		Staged:     staged,
		Unstaged:   unstaged,
		Untracked:  untracked,
		Conflicted: conflicted,
		RepoName:   enhanced.RepoName,
		RepoRoot:   enhanced.RepoRoot,
	}, nil
}

// Diff returns the diff for a file.
func (a *GitProviderAdapter) Diff(ctx context.Context, path string) (string, bool, bool, error) {
	if a.tracker == nil {
		return "", false, false, nil
	}

	// Try unstaged diff first
	diff, err := a.tracker.Diff(ctx, path)
	if err == nil && diff != "" {
		return diff, false, false, nil
	}

	// Try staged diff
	diff, err = a.tracker.DiffStaged(ctx, path)
	if err == nil && diff != "" {
		return diff, true, false, nil
	}

	// Try new file diff
	diff, err = a.tracker.DiffNewFile(ctx, path)
	if err == nil && diff != "" {
		return diff, false, true, nil
	}

	return "", false, false, err
}

// Stage stages files.
func (a *GitProviderAdapter) Stage(ctx context.Context, paths []string) error {
	if a.tracker == nil {
		return nil
	}
	return a.tracker.Stage(ctx, paths)
}

// Unstage unstages files.
func (a *GitProviderAdapter) Unstage(ctx context.Context, paths []string) error {
	if a.tracker == nil {
		return nil
	}
	return a.tracker.Unstage(ctx, paths)
}

// Discard discards changes to files.
func (a *GitProviderAdapter) Discard(ctx context.Context, paths []string) error {
	if a.tracker == nil {
		return nil
	}
	return a.tracker.Discard(ctx, paths)
}

// Commit creates a commit and optionally pushes.
func (a *GitProviderAdapter) Commit(ctx context.Context, message string, push bool) (*methods.CommitResult, error) {
	if a.tracker == nil {
		return &methods.CommitResult{Success: false, Error: "git tracker not available"}, nil
	}
	result, err := a.tracker.Commit(ctx, message, push)
	if err != nil {
		return nil, err
	}
	return &methods.CommitResult{
		Success:        result.Success,
		SHA:            result.SHA,
		Message:        result.Message,
		FilesCommitted: result.FilesCommitted,
		Pushed:         result.Pushed,
		Error:          result.Error,
	}, nil
}

// Push pushes to remote.
func (a *GitProviderAdapter) Push(ctx context.Context) (*methods.PushResult, error) {
	if a.tracker == nil {
		return &methods.PushResult{Success: false, Error: "git tracker not available"}, nil
	}
	result, err := a.tracker.Push(ctx, false, false, "", "")
	if err != nil {
		return nil, err
	}
	return &methods.PushResult{
		Success:       result.Success,
		Message:       result.Message,
		CommitsPushed: result.CommitsPushed,
		Error:         result.Error,
	}, nil
}

// Pull pulls from remote.
func (a *GitProviderAdapter) Pull(ctx context.Context) (*methods.PullResult, error) {
	if a.tracker == nil {
		return &methods.PullResult{Success: false, Error: "git tracker not available"}, nil
	}
	result, err := a.tracker.Pull(ctx, false)
	if err != nil {
		return nil, err
	}
	return &methods.PullResult{
		Success:         result.Success,
		Message:         result.Message,
		CommitsPulled:   result.CommitsPulled,
		FilesChanged:    result.FilesChanged,
		ConflictedFiles: result.ConflictedFiles,
		Error:           result.Error,
	}, nil
}

// Branches returns the list of branches with full info.
func (a *GitProviderAdapter) Branches(ctx context.Context) (*methods.BranchesResult, error) {
	if a.tracker == nil {
		return &methods.BranchesResult{Branches: []methods.BranchInfo{}}, nil
	}
	branchesResult, err := a.tracker.ListBranches(ctx)
	if err != nil {
		return nil, err
	}
	branches := make([]methods.BranchInfo, len(branchesResult.Branches))
	for i, b := range branchesResult.Branches {
		branches[i] = methods.BranchInfo{
			Name:    b.Name,
			Current: b.IsCurrent,
		}
	}
	return &methods.BranchesResult{
		Current:  branchesResult.Current,
		Upstream: branchesResult.Upstream,
		Ahead:    branchesResult.Ahead,
		Behind:   branchesResult.Behind,
		Branches: branches,
	}, nil
}

// Checkout checks out a branch.
func (a *GitProviderAdapter) Checkout(ctx context.Context, branch string) (*methods.CheckoutResult, error) {
	if a.tracker == nil {
		return &methods.CheckoutResult{Success: false, Error: "git tracker not available"}, nil
	}
	result, err := a.tracker.Checkout(ctx, branch, false)
	if err != nil {
		return nil, err
	}
	return &methods.CheckoutResult{
		Success: result.Success,
		Branch:  result.Branch,
		Message: result.Message,
		Error:   result.Error,
	}, nil
}

// FileProviderAdapter wraps git.Tracker to implement methods.FileContentProvider.
type FileProviderAdapter struct {
	tracker *git.Tracker
}

// NewFileProviderAdapter creates a new file provider adapter.
func NewFileProviderAdapter(tracker *git.Tracker) *FileProviderAdapter {
	return &FileProviderAdapter{tracker: tracker}
}

// GetFileContent returns the content of a file.
func (a *FileProviderAdapter) GetFileContent(ctx context.Context, path string, maxSizeKB int) (string, bool, error) {
	if a.tracker == nil {
		return "", false, nil
	}
	content, truncated, err := a.tracker.GetFileContent(ctx, path, maxSizeKB)
	return content, truncated, err
}

// ListDirectory returns entries in a directory.
func (a *FileProviderAdapter) ListDirectory(ctx context.Context, path string) ([]methods.FileEntry, error) {
	if a.tracker == nil {
		return nil, nil
	}
	entries, err := a.tracker.ListDirectory(ctx, path)
	if err != nil {
		return nil, err
	}
	result := make([]methods.FileEntry, len(entries))
	for i, e := range entries {
		entry := methods.FileEntry{
			Name: e.Name,
			Type: e.Type,
		}
		if e.Size != nil {
			entry.Size = e.Size
		}
		if e.Modified != nil {
			entry.Modified = e.Modified
		}
		if e.ChildrenCount != nil {
			entry.ChildrenCount = e.ChildrenCount
		}
		result[i] = entry
	}
	return result, nil
}

// ClaudeSessionAdapter wraps sessioncache.Cache and MessageCache to implement methods.SessionProvider.
type ClaudeSessionAdapter struct {
	cache        *sessioncache.Cache
	messageCache *sessioncache.MessageCache
	repoPath     string
}

// NewClaudeSessionAdapter creates a new Claude session adapter.
func NewClaudeSessionAdapter(cache *sessioncache.Cache, messageCache *sessioncache.MessageCache, repoPath string) *ClaudeSessionAdapter {
	return &ClaudeSessionAdapter{cache: cache, messageCache: messageCache, repoPath: repoPath}
}

// AgentType returns the agent type.
func (a *ClaudeSessionAdapter) AgentType() string {
	return "claude"
}

// ListSessions returns available sessions.
func (a *ClaudeSessionAdapter) ListSessions(ctx context.Context) ([]methods.SessionInfo, error) {
	if a.cache == nil {
		return nil, nil
	}
	sessions, err := a.cache.ListSessions()
	if err != nil {
		return nil, err
	}
	result := make([]methods.SessionInfo, len(sessions))
	for i, s := range sessions {
		result[i] = methods.SessionInfo{
			SessionID:    s.SessionID,
			AgentType:    "claude",
			Summary:      s.Summary,
			MessageCount: s.MessageCount,
			StartTime:    s.LastUpdated, // Use LastUpdated since StartTime isn't available
			LastUpdated:  s.LastUpdated,
			Branch:       s.Branch,
			ProjectPath:  a.repoPath,
		}
	}
	return result, nil
}

// GetSession returns detailed session info.
func (a *ClaudeSessionAdapter) GetSession(ctx context.Context, sessionID string) (*methods.SessionInfo, error) {
	if a.cache == nil {
		return nil, nil
	}
	// Get session from list (there's no GetSession method on Cache)
	sessions, err := a.cache.ListSessions()
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		if s.SessionID == sessionID {
			return &methods.SessionInfo{
				SessionID:    s.SessionID,
				AgentType:    "claude",
				Summary:      s.Summary,
				MessageCount: s.MessageCount,
				StartTime:    s.LastUpdated,
				LastUpdated:  s.LastUpdated,
				Branch:       s.Branch,
				ProjectPath:  a.repoPath,
			}, nil
		}
	}
	return nil, nil // Not found
}

// GetSessionMessages returns messages for a session.
// Returns raw CachedMessage format matching the HTTP API.
// Order can be "asc" or "desc".
func (a *ClaudeSessionAdapter) GetSessionMessages(ctx context.Context, sessionID string, limit, offset int, order string) ([]methods.SessionMessage, int, error) {
	if a.messageCache == nil {
		return nil, 0, nil
	}

	page, err := a.messageCache.GetMessages(sessionID, limit, offset, order)
	if err != nil {
		return nil, 0, err
	}

	result := make([]methods.SessionMessage, len(page.Messages))
	for i, m := range page.Messages {
		result[i] = methods.SessionMessage{
			ID:                  m.ID,
			SessionID:           m.SessionID,
			Type:                m.Type,
			UUID:                m.UUID,
			Timestamp:           m.Timestamp,
			GitBranch:           m.GitBranch,
			Message:             m.Message,
			IsContextCompaction: m.IsContextCompaction,
			IsMeta:              m.IsMeta,
		}
	}

	return result, page.Total, nil
}

// GetSessionElements returns pre-parsed UI elements for a session.
func (a *ClaudeSessionAdapter) GetSessionElements(ctx context.Context, sessionID string, limit int, beforeID, afterID string) ([]methods.SessionElement, int, error) {
	if a.messageCache == nil {
		return nil, 0, nil
	}

	// Get all messages for the session
	page, err := a.messageCache.GetMessages(sessionID, 500, 0, "asc")
	if err != nil {
		return nil, 0, err
	}

	// Convert CachedMessage to json.RawMessage with proper structure for parsing
	rawMessages := make([]json.RawMessage, len(page.Messages))
	for i, m := range page.Messages {
		// Create structured message with type and timestamp
		wrapper := map[string]interface{}{
			"type":      m.Type,
			"uuid":      m.UUID,
			"timestamp": m.Timestamp,
			"message":   json.RawMessage(m.Message),
		}
		if data, err := json.Marshal(wrapper); err == nil {
			rawMessages[i] = data
		}
	}

	// Parse messages into elements using sessioncache.ParseSessionToElements
	elements, err := sessioncache.ParseSessionToElements(rawMessages, sessionID)
	if err != nil {
		return nil, 0, err
	}

	// Convert to methods.SessionElement
	result := make([]methods.SessionElement, len(elements))
	for i, e := range elements {
		result[i] = methods.SessionElement{
			ID:        e.ID,
			Type:      string(e.Type),
			Timestamp: e.Timestamp,
			Content:   e.Content,
		}
	}

	total := len(result)

	// Handle pagination
	startIdx := 0
	endIdx := len(result)

	// If after is specified, find the element and start from there
	if afterID != "" {
		for i, e := range result {
			if e.ID == afterID {
				startIdx = i + 1
				break
			}
		}
	}

	// If before is specified, find the element and end there
	if beforeID != "" {
		for i, e := range result {
			if e.ID == beforeID {
				endIdx = i
				break
			}
		}
	}

	// Apply limit
	if endIdx > startIdx+limit {
		endIdx = startIdx + limit
	}

	if startIdx >= len(result) {
		return nil, total, nil
	}
	if endIdx > len(result) {
		endIdx = len(result)
	}

	return result[startIdx:endIdx], total, nil
}

// DeleteSession deletes a specific session.
func (a *ClaudeSessionAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	if a.cache == nil {
		return nil
	}
	return a.cache.DeleteSession(sessionID)
}

// DeleteAllSessions deletes all sessions.
func (a *ClaudeSessionAdapter) DeleteAllSessions(ctx context.Context) (int, error) {
	if a.cache == nil {
		return 0, nil
	}
	return a.cache.DeleteAllSessions()
}

// ClientFocusServer interface for server operations.
// Note: The actual implementation returns *unified.FocusChangeResult, but we use interface{}
// to avoid circular dependencies.
type ClientFocusServer interface {
	SetSessionFocus(clientID, workspaceID, sessionID string) (interface{}, error)
}

// ClientFocusAdapter wraps the unified server to implement methods.ClientFocusProvider.
type ClientFocusAdapter struct {
	server ClientFocusServer
}

// NewClientFocusAdapter creates a new client focus adapter.
func NewClientFocusAdapter(server ClientFocusServer) *ClientFocusAdapter {
	return &ClientFocusAdapter{server: server}
}

// SetSessionFocus updates the session focus for a client.
func (a *ClientFocusAdapter) SetSessionFocus(clientID, workspaceID, sessionID string) (interface{}, error) {
	if a.server == nil {
		return nil, nil
	}
	return a.server.SetSessionFocus(clientID, workspaceID, sessionID)
}
