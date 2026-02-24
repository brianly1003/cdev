// Package app provides adapters that wrap existing components to implement RPC method interfaces.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/codex"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
	"github.com/brianly1003/cdev/internal/workspace"
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
func (a *ClaudeAgentAdapter) StartWithSession(ctx context.Context, prompt string, mode methods.SessionMode, sessionID string, permissionMode string) error {
	if a.manager == nil {
		return nil
	}
	return a.manager.StartWithSession(ctx, prompt, claude.SessionMode(mode), sessionID, permissionMode)
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

// SendPTYInput sends input to the PTY (for interactive responses).
func (a *ClaudeAgentAdapter) SendPTYInput(input string) error {
	if a.manager == nil {
		return nil
	}
	return a.manager.SendPTYInput(input)
}

// IsPTYMode returns true if running in PTY mode.
func (a *ClaudeAgentAdapter) IsPTYMode() bool {
	if a.manager == nil {
		return false
	}
	return a.manager.IsPTYMode()
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
// The projectPath parameter is ignored for Claude since it already operates on a single project.
func (a *ClaudeSessionAdapter) ListSessions(ctx context.Context, projectPath string) ([]methods.SessionInfo, error) {
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

// CodexSessionAdapter implements methods.SessionProvider for Codex CLI sessions.
// It uses the IndexCache for efficient project-based session discovery with rich metadata.
type CodexSessionAdapter struct {
	repoPath   string
	indexCache *codex.IndexCache
}

// NewCodexSessionAdapter creates a new Codex session adapter.
func NewCodexSessionAdapter(repoPath string) *CodexSessionAdapter {
	return &CodexSessionAdapter{
		repoPath:   repoPath,
		indexCache: codex.GetGlobalIndexCache(),
	}
}

// AgentType returns the agent type.
func (a *CodexSessionAdapter) AgentType() string {
	return "codex"
}

// ListSessions returns available Codex CLI sessions.
// If projectPath is provided, filters sessions to that project path.
// If projectPath is empty, returns ALL sessions from all projects (sorted by modification time).
// Note: Unlike Claude sessions which are stored per-project, Codex sessions are stored globally
// in ~/.codex/ so we can discover sessions from all projects regardless of repoPath.
func (a *CodexSessionAdapter) ListSessions(ctx context.Context, projectPath string) ([]methods.SessionInfo, error) {
	// If client specifies a project path, filter by that path
	if projectPath != "" {
		entries, err := a.indexCache.GetSessionsForProject(projectPath)
		if err != nil {
			// Fallback to direct parsing if index cache fails
			sessions, err := codex.ListSessionsForWorkspace(projectPath)
			if err != nil {
				return nil, err
			}
			result := make([]methods.SessionInfo, len(sessions))
			for i, s := range sessions {
				result[i] = methods.SessionInfo{
					SessionID:    s.SessionID,
					AgentType:    "codex",
					Summary:      s.Summary,
					MessageCount: s.MessageCount,
					StartTime:    s.LastUpdated,
					LastUpdated:  s.LastUpdated,
					ProjectPath:  s.WorkspacePath,
				}
			}
			return result, nil
		}

		result := make([]methods.SessionInfo, len(entries))
		for i, e := range entries {
			result[i] = convertIndexEntryToSessionInfo(e)
		}
		return result, nil
	}

	// No project path specified - return ALL sessions from all projects
	// This is the expected behavior for Codex since sessions are stored globally
	entries, err := a.indexCache.GetAllSessions()
	if err != nil {
		return nil, err
	}
	result := make([]methods.SessionInfo, len(entries))
	for i, e := range entries {
		result[i] = convertIndexEntryToSessionInfo(e)
	}
	return result, nil
}

// GetSession returns detailed session info.
// Uses the index cache for richer metadata.
func (a *CodexSessionAdapter) GetSession(ctx context.Context, sessionID string) (*methods.SessionInfo, error) {
	// Try index cache first for richer metadata
	entry, err := a.indexCache.FindSessionByID(sessionID)
	if err == nil && entry != nil {
		info := convertIndexEntryToSessionInfo(*entry)
		return &info, nil
	}

	// Fallback to direct parsing only when repository.path is configured.
	// In multi-workspace mode repository.path is empty by design.
	if strings.TrimSpace(a.repoPath) == "" {
		return nil, nil
	}

	// Fallback to direct parsing
	info, _, err := codex.FindSessionByID(a.repoPath, sessionID)
	if err != nil || info == nil {
		return nil, err
	}

	return &methods.SessionInfo{
		SessionID:    info.SessionID,
		AgentType:    "codex",
		Summary:      info.Summary,
		MessageCount: info.MessageCount,
		StartTime:    info.LastUpdated,
		LastUpdated:  info.LastUpdated,
		ProjectPath:  info.WorkspacePath,
	}, nil
}

// convertIndexEntryToSessionInfo converts a codex.SessionIndexEntry to methods.SessionInfo.
func convertIndexEntryToSessionInfo(e codex.SessionIndexEntry) methods.SessionInfo {
	return methods.SessionInfo{
		SessionID:     e.SessionID,
		AgentType:     "codex",
		Summary:       e.Summary,
		FirstPrompt:   e.FirstPrompt,
		MessageCount:  e.MessageCount,
		StartTime:     e.Created,
		LastUpdated:   e.Modified,
		Branch:        e.GitBranch,
		GitCommit:     e.GitCommit,
		GitRepo:       e.GitRepo,
		ProjectPath:   e.ProjectPath,
		ModelProvider: e.ModelProvider,
		Model:         e.Model,
		CLIVersion:    e.CLIVersion,
		FileSize:      e.FileSize,
		FilePath:      e.FullPath,
	}
}

// GetSessionMessages returns messages for a Codex session.
func (a *CodexSessionAdapter) GetSessionMessages(ctx context.Context, sessionID string, limit, offset int, order string) ([]methods.SessionMessage, int, error) {
	// Find the session file via the global index (Codex sessions are not repo-scoped).
	entry, err := a.indexCache.FindSessionByID(sessionID)
	if err != nil || entry == nil || entry.FullPath == "" {
		return nil, 0, fmt.Errorf("session not found")
	}

	items, err := codex.ReadConversationItems(entry.FullPath)
	if err != nil {
		return nil, 0, err
	}

	// Convert conversation items to synthetic SessionMessages that match the Claude-style
	// message.content block format (text/thinking/tool_use/tool_result).
	all := make([]methods.SessionMessage, 0, len(items))

	type exploredState struct {
		entries   []string
		timestamp string
		lastLine  int
	}
	var explored exploredState
	exploredSeq := 0

	appendMessage := func(role string, blocks []events.ClaudeMessageContent, timestamp string, line int, suffix string, isContextCompaction bool) {
		raw, err := formatCodexMessageJSON(role, blocks)
		if err != nil || raw == nil {
			return
		}

		uuid := fmt.Sprintf("%s:%06d", sessionID, line)
		if suffix != "" {
			uuid = uuid + ":" + suffix
		}

		all = append(all, methods.SessionMessage{
			UUID:                uuid,
			SessionID:           sessionID,
			Type:                role,
			Timestamp:           timestamp,
			Message:             raw,
			IsContextCompaction: isContextCompaction,
		})
	}

	flushExplored := func(fallbackTS string) {
		if len(explored.entries) == 0 {
			return
		}
		exploredSeq++
		text := formatCodexExploredText(explored.entries)
		ts := explored.timestamp
		if ts == "" {
			ts = fallbackTS
		}
		appendMessage(
			"assistant",
			[]events.ClaudeMessageContent{{Type: "text", Text: text}},
			ts,
			explored.lastLine,
			fmt.Sprintf("explored-%03d", exploredSeq),
			false,
		)
		explored = exploredState{}
	}

	for _, it := range dedupCodexThinking(items) {
		if shouldFlushCodexExploredBeforeItem(it) {
			flushExplored(it.Timestamp)
		}

		for _, summary := range collectCodexToolSummaries(it.Content) {
			if strings.TrimSpace(summary) == "" {
				continue
			}
			explored.entries = append(explored.entries, summary)
			explored.timestamp = it.Timestamp
			explored.lastLine = it.Line
		}

		appendMessage(it.Role, it.Content, it.Timestamp, it.Line, "", it.IsContextCompaction)
	}
	flushExplored("")

	for i := range all {
		all[i].ID = int64(i + 1)
	}

	total := len(all)
	if total == 0 {
		return []methods.SessionMessage{}, 0, nil
	}

	if order != "asc" && order != "desc" {
		order = "asc"
	}
	if order == "desc" {
		reversed := make([]methods.SessionMessage, len(all))
		for i := range all {
			reversed[i] = all[len(all)-1-i]
		}
		all = reversed
	}

	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(all) {
		return []methods.SessionMessage{}, total, nil
	}

	end := offset + limit
	if end > len(all) {
		end = len(all)
	}

	return all[offset:end], total, nil
}

// GetSessionElements returns pre-parsed UI elements for Codex sessions.
func (a *CodexSessionAdapter) GetSessionElements(ctx context.Context, sessionID string, limit int, beforeID, afterID string) ([]methods.SessionElement, int, error) {
	entry, err := a.indexCache.FindSessionByID(sessionID)
	if err != nil || entry == nil || entry.FullPath == "" {
		return nil, 0, fmt.Errorf("session not found")
	}

	items, err := codex.ReadConversationItems(entry.FullPath)
	if err != nil {
		return nil, 0, err
	}
	items = dedupCodexThinking(items)

	// Track tool name by call_id so tool_result elements can include tool_name.
	toolNames := make(map[string]string)
	pendingExplored := make([]string, 0, 4)
	pendingExploredTS := ""
	pendingExploredLine := 0
	exploredSeq := 0

	var elements []methods.SessionElement
	flushExplored := func(fallbackTS string) {
		if len(pendingExplored) == 0 {
			return
		}
		exploredSeq++
		ts := pendingExploredTS
		if ts == "" {
			ts = fallbackTS
		}
		elements = append(elements, methods.SessionElement{
			ID:        fmt.Sprintf("%s:%06d:explored:%03d", sessionID, pendingExploredLine, exploredSeq),
			Type:      "assistant_text",
			Timestamp: ts,
			Content:   mustJSON(sessioncache.AssistantTextContent{Text: formatCodexExploredText(pendingExplored)}),
		})
		pendingExplored = pendingExplored[:0]
		pendingExploredTS = ""
		pendingExploredLine = 0
	}

	for _, it := range items {
		if shouldFlushCodexExploredBeforeItem(it) {
			flushExplored(it.Timestamp)
		}

		if it.IsContextCompaction {
			summary := strings.TrimSpace(joinCodexText(it.Content))
			if summary == "" {
				summary = "Conversation compacted to continue this session."
			}
			elements = append(elements, methods.SessionElement{
				ID:        fmt.Sprintf("%s:%06d:context", sessionID, it.Line),
				Type:      string(sessioncache.ElementTypeContextCompaction),
				Timestamp: it.Timestamp,
				Content:   mustJSON(sessioncache.ContextCompactionContent{Summary: summary}),
			})
			continue
		}

		if it.IsTurnAborted {
			message := strings.TrimSpace(joinCodexText(it.Content))
			if message == "" {
				message = "The previous turn was interrupted."
			}
			elements = append(elements, methods.SessionElement{
				ID:        fmt.Sprintf("%s:%06d:interrupted", sessionID, it.Line),
				Type:      string(sessioncache.ElementTypeInterrupted),
				Timestamp: it.Timestamp,
				Content: mustJSON(sessioncache.InterruptedContent{
					Message: message,
				}),
			})
			continue
		}

		// Most items are single-type (text/thinking/tool_use/tool_result). For "message"
		// items, we can join all text blocks into a single element.
		if it.Role == "user" {
			text := joinCodexText(it.Content)
			if strings.TrimSpace(text) == "" {
				continue
			}
			elements = append(elements, methods.SessionElement{
				ID:        fmt.Sprintf("%s:%06d", sessionID, it.Line),
				Type:      "user_input",
				Timestamp: it.Timestamp,
				Content:   mustJSON(sessioncache.UserInputContent{Text: text}),
			})
			continue
		}

		// Assistant side: preserve block ordering, but coalesce adjacent text blocks.
		var pendingText []string
		flushText := func(idx int) {
			if len(pendingText) == 0 {
				return
			}
			text := strings.TrimSpace(strings.Join(pendingText, "\n"))
			pendingText = nil
			if text == "" {
				return
			}
			elements = append(elements, methods.SessionElement{
				ID:        fmt.Sprintf("%s:%06d:%d", sessionID, it.Line, idx),
				Type:      "assistant_text",
				Timestamp: it.Timestamp,
				Content:   mustJSON(sessioncache.AssistantTextContent{Text: text}),
			})
		}

		elemIdx := 0
		for _, block := range it.Content {
			switch block.Type {
			case "text":
				if strings.TrimSpace(block.Text) != "" {
					pendingText = append(pendingText, block.Text)
				}
			case "thinking":
				flushText(elemIdx)
				elemIdx++
				if strings.TrimSpace(block.Text) == "" {
					continue
				}
				elements = append(elements, methods.SessionElement{
					ID:        fmt.Sprintf("%s:%06d:%d", sessionID, it.Line, elemIdx),
					Type:      "thinking",
					Timestamp: it.Timestamp,
					Content:   mustJSON(sessioncache.ThinkingContent{Text: block.Text, Collapsed: true}),
				})
			case "tool_use":
				flushText(elemIdx)
				elemIdx++
				if block.ToolID != "" {
					toolNames[block.ToolID] = block.ToolName
				}
				if summary := summarizeCodexToolForExplored(block.ToolName, block.ToolInput); strings.TrimSpace(summary) != "" {
					pendingExplored = append(pendingExplored, summary)
					pendingExploredTS = it.Timestamp
					pendingExploredLine = it.Line
				}
				display := formatCodexToolDisplay(block.ToolName, block.ToolInput)
				elements = append(elements, methods.SessionElement{
					ID:        fmt.Sprintf("%s:%06d:%d", sessionID, it.Line, elemIdx),
					Type:      "tool_call",
					Timestamp: it.Timestamp,
					Content: mustJSON(sessioncache.ToolCallContent{
						Tool:    block.ToolName,
						ToolID:  block.ToolID,
						Display: display,
						Params:  block.ToolInput,
						Status:  sessioncache.ToolStatusCompleted,
					}),
				})
			case "tool_result":
				flushText(elemIdx)
				elemIdx++
				full := block.Content
				summary := summarizeToolOutput(full)
				toolName := toolNames[block.ToolUseID]
				lineCount := 0
				if strings.TrimSpace(full) != "" {
					lineCount = strings.Count(full, "\n") + 1
				}
				expandable := lineCount > 12 || len(full) > 400

				elements = append(elements, methods.SessionElement{
					ID:        fmt.Sprintf("%s:%06d:%d", sessionID, it.Line, elemIdx),
					Type:      "tool_result",
					Timestamp: it.Timestamp,
					Content: mustJSON(sessioncache.ToolResultContent{
						ToolCallID:  block.ToolUseID,
						ToolName:    toolName,
						IsError:     block.IsError,
						Summary:     summary,
						FullContent: full,
						LineCount:   lineCount,
						Expandable:  expandable,
					}),
				})
			}
		}
		flushText(elemIdx)
	}
	flushExplored("")

	total := len(elements)
	if total == 0 {
		return []methods.SessionElement{}, 0, nil
	}

	// Apply pagination similar to Claude elements.
	startIdx := 0
	endIdx := len(elements)
	if afterID != "" {
		for i, e := range elements {
			if e.ID == afterID {
				startIdx = i + 1
				break
			}
		}
	}
	if beforeID != "" {
		for i, e := range elements {
			if e.ID == beforeID {
				endIdx = i
				break
			}
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if endIdx > startIdx+limit {
		endIdx = startIdx + limit
	}
	if startIdx >= len(elements) {
		return []methods.SessionElement{}, total, nil
	}
	if endIdx > len(elements) {
		endIdx = len(elements)
	}

	return elements[startIdx:endIdx], total, nil
}

func dedupCodexThinking(items []codex.ConversationItem) []codex.ConversationItem {
	// Best-effort de-dupe for identical thinking blocks (agent_reasoning vs reasoning.summary).
	out := make([]codex.ConversationItem, 0, len(items))

	var lastText string
	var lastTS time.Time

	for _, it := range items {
		if it.Role == "assistant" && len(it.Content) == 1 && it.Content[0].Type == "thinking" {
			text := strings.TrimSpace(it.Content[0].Text)
			if text == "" {
				continue
			}
			ts, ok := parseRFC3339(it.Timestamp)
			if ok && lastText == text && !lastTS.IsZero() {
				delta := ts.Sub(lastTS)
				if delta < 0 {
					delta = -delta
				}
				if delta <= 2*time.Second {
					continue
				}
			}
			if ok {
				lastTS = ts
			}
			lastText = text
		}
		out = append(out, it)
	}

	return out
}

func parseRFC3339(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func joinCodexText(blocks []events.ClaudeMessageContent) string {
	var parts []string
	for _, b := range blocks {
		if b.Type != "text" {
			continue
		}
		if strings.TrimSpace(b.Text) == "" {
			continue
		}
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n")
}

func shouldFlushCodexExploredBeforeItem(it codex.ConversationItem) bool {
	if it.Role != "assistant" {
		return true
	}
	for _, block := range it.Content {
		if block.Type != "tool_use" && block.Type != "tool_result" {
			return true
		}
	}
	return false
}

func collectCodexToolSummaries(blocks []events.ClaudeMessageContent) []string {
	var out []string
	for _, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}
		if summary := summarizeCodexToolForExplored(block.ToolName, block.ToolInput); strings.TrimSpace(summary) != "" {
			out = append(out, summary)
		}
	}
	return out
}

func formatCodexExploredText(entries []string) string {
	if len(entries) == 0 {
		return ""
	}

	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "**Explored**")
	for i, entry := range entries {
		prefix := "├ "
		if i == len(entries)-1 {
			prefix = "└ "
		}
		lines = append(lines, prefix+entry)
	}
	return strings.Join(lines, "\n")
}

func summarizeCodexToolForExplored(toolName string, params map[string]interface{}) string {
	switch toolName {
	case "exec_command":
		if cmd, ok := params["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			return codex.SummarizeExecCommandForExplored(cmd)
		}
		if cmd, ok := params["cmd"].(string); ok && strings.TrimSpace(cmd) != "" {
			return codex.SummarizeExecCommandForExplored(cmd)
		}
	case "apply_patch":
		// apply_patch already renders as a dedicated Updated/Added tool row with diff preview.
		// Skip synthetic "Explored" summary to avoid duplicate split presentation.
		return ""
	case "view_image":
		// view_image already appears as its own tool row.
		return ""
	}

	display := strings.TrimSpace(formatCodexToolDisplay(toolName, params))
	if display == "" {
		return "Run tool"
	}
	return display
}

func formatCodexToolDisplay(toolName string, params map[string]interface{}) string {
	switch toolName {
	case "exec_command":
		cmd := ""
		if raw, ok := params["command"].(string); ok {
			cmd = raw
		}
		if strings.TrimSpace(cmd) == "" {
			if raw, ok := params["cmd"].(string); ok {
				cmd = raw
			}
		}
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			return "Ran command"
		}
		if len(cmd) > 140 {
			cmd = cmd[:137] + "..."
		}
		return "Ran " + cmd
	case "apply_patch":
		if in, ok := params["input"].(string); ok {
			if summary := summarizeApplyPatch(in); summary != "" {
				return summary
			}
		}
		return "Applied patch"
	case "view_image":
		path := ""
		if raw, ok := params["path"].(string); ok {
			path = compactDotCdevDisplayPath(raw)
		}
		if strings.TrimSpace(path) == "" {
			return "view_image"
		}
		return fmt.Sprintf("view_image(path: %s)", path)
	}
	if toolName != "" {
		return toolName
	}
	return "tool"
}

func compactDotCdevDisplayPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "/.cdev/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func summarizeApplyPatch(input string) string {
	action := "Applied"
	file := ""
	added := 0
	removed := 0

	for _, line := range strings.Split(input, "\n") {
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			action = "Added"
			file = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Update File: "):
			action = "Updated"
			file = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File: "))
		case strings.HasPrefix(line, "*** Delete File: "):
			action = "Deleted"
			file = strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File: "))
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			added++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			removed++
		}
	}

	if file == "" {
		return ""
	}
	if action == "Deleted" {
		return action + " " + file
	}
	return fmt.Sprintf("%s %s (+%d -%d)", action, file, added, removed)
}

func summarizeToolOutput(full string) string {
	full = strings.TrimSpace(full)
	if full == "" {
		return ""
	}
	line := full
	if nl := strings.IndexByte(full, '\n'); nl >= 0 {
		line = full[:nl]
	}
	line = strings.TrimSpace(line)
	if len(line) > 160 {
		return line[:157] + "..."
	}
	return line
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func formatCodexMessageJSON(role string, blocks []events.ClaudeMessageContent) (json.RawMessage, error) {
	if role != "user" && role != "assistant" {
		return nil, nil
	}
	if len(blocks) == 0 {
		return nil, nil
	}

	content := make([]map[string]interface{}, 0, len(blocks))
	for _, b := range blocks {
		switch b.Type {
		case "text", "thinking":
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			content = append(content, map[string]interface{}{
				"type": b.Type,
				"text": b.Text,
			})
		case "tool_use":
			if b.ToolName == "" {
				continue
			}
			content = append(content, map[string]interface{}{
				"type":  "tool_use",
				"id":    b.ToolID,
				"name":  b.ToolName,
				"input": b.ToolInput,
			})
		case "tool_result":
			if strings.TrimSpace(b.Content) == "" {
				continue
			}
			content = append(content, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": b.ToolUseID,
				"content":     b.Content,
				"is_error":    b.IsError,
			})
		}
	}
	if len(content) == 0 {
		return nil, nil
	}

	msg := map[string]interface{}{
		"role":    role,
		"content": content,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// DeleteSession deletes a specific Codex session.
func (a *CodexSessionAdapter) DeleteSession(ctx context.Context, sessionID string) error {
	if a.repoPath == "" {
		return nil
	}
	return codex.DeleteSession(a.repoPath, sessionID)
}

// DeleteAllSessions deletes all Codex sessions for the repo.
func (a *CodexSessionAdapter) DeleteAllSessions(ctx context.Context) (int, error) {
	if a.repoPath == "" {
		return 0, nil
	}
	return codex.DeleteAllSessions(a.repoPath)
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

// WorkspaceConfigProvider provides workspace configuration access.
type WorkspaceConfigProvider interface {
	GetWorkspace(id string) (interface{ GetPath() string }, error)
}

// WorkspacePathResolverAdapter implements methods.WorkspacePathResolver.
type WorkspacePathResolverAdapter struct {
	configManager *workspace.ConfigManager
}

// NewWorkspacePathResolverAdapter creates a new workspace path resolver adapter.
func NewWorkspacePathResolverAdapter(configManager *workspace.ConfigManager) *WorkspacePathResolverAdapter {
	return &WorkspacePathResolverAdapter{configManager: configManager}
}

// GetWorkspacePath resolves workspace ID to its path.
func (a *WorkspacePathResolverAdapter) GetWorkspacePath(workspaceID string) (string, error) {
	if a.configManager == nil {
		return "", nil
	}
	ws, err := a.configManager.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	return ws.Definition.Path, nil
}
