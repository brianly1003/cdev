// Package methods provides JSON-RPC method implementations.
package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/brianly1003/cdev/internal/workspace"
)

// SessionViewerProvider provides session viewer information.
type SessionViewerProvider interface {
	// GetSessionViewers returns a map of session ID to list of client IDs viewing that session.
	GetSessionViewers(workspaceID string) map[string][]string
}

// WorkspaceConfigService handles workspace configuration CRUD operations.
// This is the simplified version that only manages workspace configs, not processes.
type WorkspaceConfigService struct {
	sessionManager *session.Manager
	configManager  *workspace.ConfigManager
	viewerProvider SessionViewerProvider
	hub            ports.EventHub
	authRegistry   *security.AuthRegistry
}

// NewWorkspaceConfigService creates a new workspace config service.
func NewWorkspaceConfigService(sessionManager *session.Manager, configManager *workspace.ConfigManager, hub ports.EventHub, authRegistry *security.AuthRegistry) *WorkspaceConfigService {
	return &WorkspaceConfigService{
		sessionManager: sessionManager,
		configManager:  configManager,
		hub:            hub,
		authRegistry:   authRegistry,
	}
}

// SetViewerProvider sets the session viewer provider.
// This allows the provider to be set after service initialization.
func (s *WorkspaceConfigService) SetViewerProvider(provider SessionViewerProvider) {
	s.viewerProvider = provider
}

// RegisterMethods registers all workspace config methods with the handler.
func (s *WorkspaceConfigService) RegisterMethods(registry *handler.Registry) {
	registry.RegisterWithMeta("workspace/list", s.List, handler.MethodMeta{
		Summary:     "List all workspaces",
		Description: "Returns a list of all configured workspaces. Optionally includes git status for each workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "include_git", Required: false, Schema: map[string]interface{}{"type": "boolean", "description": "Include git status for each workspace (default: false)"}},
			{Name: "git_limit", Required: false, Schema: map[string]interface{}{"type": "integer", "description": "Limit git status fetching to first N workspaces (0 = all)"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "workspaces",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/get", s.Get, handler.MethodMeta{
		Summary:     "Get workspace details",
		Description: "Returns detailed information about a specific workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "workspace",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/add", s.Add, handler.MethodMeta{
		Summary:     "Add a new workspace",
		Description: "Registers a new workspace configuration. Can be any folder, not just git repositories. Set create_if_missing to true to create the directory if it doesn't exist.",
		Params: []handler.OpenRPCParam{
			{Name: "name", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "path", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "auto_start", Required: false, Schema: map[string]interface{}{"type": "boolean"}},
			{Name: "create_if_missing", Required: false, Schema: map[string]interface{}{"type": "boolean", "description": "Create the directory if it doesn't exist"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "workspace",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/remove", s.Remove, handler.MethodMeta{
		Summary:     "Remove a workspace",
		Description: "Unregisters a workspace configuration.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/update", s.Update, handler.MethodMeta{
		Summary:     "Update workspace settings",
		Description: "Updates workspace configuration settings.",
		Params: []handler.OpenRPCParam{
			{Name: "id", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "name", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "auto_start", Required: false, Schema: map[string]interface{}{"type": "boolean"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "workspace",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/discover", s.Discover, handler.MethodMeta{
		Summary:     "Discover git repositories",
		Description: "Scans directories to find git repositories that can be added as workspaces.",
		Params: []handler.OpenRPCParam{
			{Name: "paths", Required: false, Schema: map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "repositories",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/status", s.Status, handler.MethodMeta{
		Summary:     "Get workspace status",
		Description: "Returns detailed status for a specific workspace including git tracker state, active sessions, and watch status.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "status",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/cache/invalidate", s.InvalidateCache, handler.MethodMeta{
		Summary:     "Invalidate discovery cache",
		Description: "Clears the cached repository discovery results. The next workspace/discover call will perform a fresh scan.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
}

// List returns all configured workspaces.
// Includes both running sessions and historical sessions from ~/.claude/projects/
// Optional params:
//   - include_git: bool - include git status for each workspace (default: false)
//   - git_limit: int - limit git status fetching to first N workspaces (0 = all)
func (s *WorkspaceConfigService) List(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	// Parse optional params
	var p struct {
		IncludeGit bool `json:"include_git"`
		GitLimit   int  `json:"git_limit"`
	}
	// Params are optional, ignore parse errors
	_ = json.Unmarshal(params, &p)

	workspaces := s.configManager.ListWorkspaces()

	// Fetch git status in parallel if requested
	var gitStatuses map[string]*git.Status
	if p.IncludeGit {
		gitStatuses = s.fetchGitStatusParallel(ctx, workspaces, p.GitLimit)
	}

	// Enrich with session status
	result := make([]map[string]interface{}, 0, len(workspaces))
	for _, ws := range workspaces {
		info := map[string]interface{}{
			"id":         ws.Definition.ID,
			"name":       ws.Definition.Name,
			"path":       ws.Definition.Path,
			"auto_start": ws.Definition.AutoStart,
			"created_at": ws.Definition.CreatedAt,
		}

		// Add git info if available
		if gitStatuses != nil {
			s.addGitInfoToWorkspace(info, ws.Definition.ID, gitStatuses)
		}

		// Get session viewers for this workspace (if provider is available)
		var sessionViewers map[string][]string
		if s.viewerProvider != nil {
			sessionViewers = s.viewerProvider.GetSessionViewers(ws.Definition.ID)
		}

		// Track running session IDs to avoid duplicates
		runningSessionIDs := make(map[string]bool)

		// Get the active (LIVE attached) session for this workspace
		activeSessionID := s.sessionManager.GetActiveSession(ws.Definition.ID)

		// Get running sessions for this workspace (managed by cdev)
		runningSessions := s.sessionManager.GetSessionsForWorkspace(ws.Definition.ID)
		allSessions := make([]map[string]interface{}, 0)
		runningCount := 0

		for _, sess := range runningSessions {
			// Only include sessions that are actually running
			if sess.GetStatus() != session.StatusRunning {
				continue
			}
			runningSessionIDs[sess.ID] = true
			runningCount++
			sessInfo := map[string]interface{}{
				"id":           sess.ID,
				"workspace_id": sess.WorkspaceID,
				"status":       "running",
				"started_at":   sess.StartedAt,
				"last_active":  sess.LastActive,
			}
			// Add viewers for this session
			if sessionViewers != nil {
				if viewers, ok := sessionViewers[sess.ID]; ok {
					sessInfo["viewers"] = viewers
				}
			}
			allSessions = append(allSessions, sessInfo)
		}

		// Get historical sessions from ~/.claude/projects/
		historicalSessions, _ := s.sessionManager.ListHistory(ws.Definition.ID, 50)
		for _, hist := range historicalSessions {
			// Skip if this session is already running (managed by cdev)
			if runningSessionIDs[hist.SessionID] {
				continue
			}

			// Check if this session has viewers
			var viewers []string
			hasViewers := false
			if sessionViewers != nil {
				if v, ok := sessionViewers[hist.SessionID]; ok && len(v) > 0 {
					viewers = v
					hasViewers = true
				}
			}

			// Determine status:
			// - "running" if this is the active LIVE session
			// - "running" if there are viewers watching the session
			// - "historical" otherwise
			status := "historical"
			if hist.SessionID == activeSessionID || hasViewers {
				status = "running"
				runningCount++ // Count as active
			}

			sessInfo := map[string]interface{}{
				"id":            hist.SessionID,
				"workspace_id":  ws.Definition.ID,
				"status":        status,
				"summary":       hist.Summary,
				"message_count": hist.MessageCount,
				"last_updated":  hist.LastUpdated,
			}
			// Add viewers for this session
			if len(viewers) > 0 {
				sessInfo["viewers"] = viewers
			}
			allSessions = append(allSessions, sessInfo)
		}

		info["sessions"] = allSessions
		info["active_session_count"] = runningCount
		info["has_active_session"] = runningCount > 0

		result = append(result, info)
	}

	return map[string]interface{}{
		"workspaces": result,
	}, nil
}

// fetchGitStatusParallel fetches git status for workspaces in parallel.
// Uses a semaphore to limit concurrent git operations.
func (s *WorkspaceConfigService) fetchGitStatusParallel(ctx context.Context, workspaces []*workspace.Workspace, limit int) map[string]*git.Status {
	// Determine how many workspaces to fetch git status for
	count := len(workspaces)
	if limit > 0 && limit < count {
		count = limit
	}

	if count == 0 {
		return nil
	}

	// Result map with mutex for thread-safe access
	statuses := make(map[string]*git.Status)
	var mu sync.Mutex

	// Use semaphore to limit concurrent git operations (max 10)
	const maxConcurrent = 10
	sem := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		ws := workspaces[i]
		wg.Add(1)

		go func(wsID string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			// Fetch git status
			if status, err := s.sessionManager.GitGetStatus(wsID); err == nil && status != nil {
				mu.Lock()
				statuses[wsID] = status
				mu.Unlock()
			}
		}(ws.Definition.ID)
	}

	wg.Wait()
	return statuses
}

// addGitInfoToWorkspace adds git status info to a workspace info map.
func (s *WorkspaceConfigService) addGitInfoToWorkspace(info map[string]interface{}, wsID string, gitStatuses map[string]*git.Status) {
	// Initialize defaults
	info["is_git_repo"] = false
	info["git_state"] = "no_git"

	gitInfo := map[string]interface{}{
		"initialized": false,
		"has_remotes": false,
		"branch":      nil,
		"state":       "no_git",
	}

	// Check if we have git status for this workspace
	if status, ok := gitStatuses[wsID]; ok && status != nil {
		gitInfo["initialized"] = status.IsGitRepo
		gitInfo["branch"] = status.Branch
		gitInfo["has_remotes"] = len(status.Remotes) > 0
		gitInfo["ahead"] = status.Ahead
		gitInfo["behind"] = status.Behind
		gitInfo["staged_count"] = len(status.Staged)
		gitInfo["unstaged_count"] = len(status.Unstaged)
		gitInfo["untracked_count"] = len(status.Untracked)
		gitInfo["has_conflicts"] = status.HasConflicts
		gitInfo["state"] = status.State

		// Set top-level git state fields
		info["is_git_repo"] = status.IsGitRepo
		info["git_state"] = status.State
	}

	info["git"] = gitInfo
}

// Get returns details of a specific workspace.
func (s *WorkspaceConfigService) Get(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	ws, err := s.configManager.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	info := map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	}

	// Get session viewers for this workspace (if provider is available)
	var sessionViewers map[string][]string
	if s.viewerProvider != nil {
		sessionViewers = s.viewerProvider.GetSessionViewers(ws.Definition.ID)
	}

	// Track running session IDs to avoid duplicates
	runningSessionIDs := make(map[string]bool)

	// Get the active (LIVE attached) session for this workspace
	activeSessionID := s.sessionManager.GetActiveSession(ws.Definition.ID)

	// Get running sessions for this workspace (managed by cdev)
	runningSessions := s.sessionManager.GetSessionsForWorkspace(ws.Definition.ID)
	allSessions := make([]map[string]interface{}, 0)
	runningCount := 0

	for _, sess := range runningSessions {
		// Only include sessions that are actually running
		if sess.GetStatus() != session.StatusRunning {
			continue
		}
		runningSessionIDs[sess.ID] = true
		runningCount++
		sessInfo := map[string]interface{}{
			"id":           sess.ID,
			"workspace_id": sess.WorkspaceID,
			"status":       "running",
			"started_at":   sess.StartedAt,
			"last_active":  sess.LastActive,
		}
		// Add viewers for this session
		if sessionViewers != nil {
			if viewers, ok := sessionViewers[sess.ID]; ok {
				sessInfo["viewers"] = viewers
			}
		}
		allSessions = append(allSessions, sessInfo)
	}

	// Get historical sessions from ~/.claude/projects/
	historicalSessions, _ := s.sessionManager.ListHistory(ws.Definition.ID, 50)
	for _, hist := range historicalSessions {
		// Skip if this session is already running (managed by cdev)
		if runningSessionIDs[hist.SessionID] {
			continue
		}

		// Check if this session has viewers
		var viewers []string
		hasViewers := false
		if sessionViewers != nil {
			if v, ok := sessionViewers[hist.SessionID]; ok && len(v) > 0 {
				viewers = v
				hasViewers = true
			}
		}

		// Determine status:
		// - "running" if this is the active LIVE session
		// - "running" if there are viewers watching the session
		// - "historical" otherwise
		status := "historical"
		if hist.SessionID == activeSessionID || hasViewers {
			status = "running"
			runningCount++
		}

		sessInfo := map[string]interface{}{
			"id":            hist.SessionID,
			"workspace_id":  ws.Definition.ID,
			"status":        status,
			"summary":       hist.Summary,
			"message_count": hist.MessageCount,
			"last_updated":  hist.LastUpdated,
		}
		// Add viewers for this session
		if len(viewers) > 0 {
			sessInfo["viewers"] = viewers
		}
		allSessions = append(allSessions, sessInfo)
	}

	info["sessions"] = allSessions
	info["active_session_count"] = runningCount
	info["has_active_session"] = runningCount > 0

	return info, nil
}

// Add registers a new workspace.
func (s *WorkspaceConfigService) Add(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		Name            string `json:"name"`
		Path            string `json:"path"`
		AutoStart       bool   `json:"auto_start"`
		CreateIfMissing bool   `json:"create_if_missing"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.Name == "" {
		return nil, message.NewError(message.InvalidParams, "name is required")
	}
	if p.Path == "" {
		return nil, message.NewError(message.InvalidParams, "path is required")
	}

	ws, err := s.configManager.AddWorkspaceWithOptions(p.Name, p.Path, p.AutoStart, p.CreateIfMissing)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Register with session manager
	s.sessionManager.RegisterWorkspace(ws)

	if err := s.bindWorkspaceToDevice(ctx, ws.Definition.ID); err != nil {
		_ = s.sessionManager.UnregisterWorkspace(ws.Definition.ID)
		_ = s.configManager.RemoveWorkspace(ws.Definition.ID)
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Build response with git state info
	response := map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
		"sessions":   []interface{}{}, // Empty sessions array for new workspace
	}

	// Initialize git state fields (defaults for non-git folders)
	response["is_git_repo"] = false
	response["git_state"] = "no_git"

	// Check git state and add to response
	gitInfo := map[string]interface{}{
		"initialized": false,
		"has_remotes": false,
		"branch":      nil,
	}

	// Try to get git status for this workspace
	if status, err := s.sessionManager.GitGetStatus(ws.Definition.ID); err == nil && status != nil {
		gitInfo["initialized"] = status.IsGitRepo
		gitInfo["branch"] = status.Branch
		gitInfo["has_remotes"] = len(status.Remotes) > 0
		gitInfo["ahead"] = status.Ahead
		gitInfo["behind"] = status.Behind
		gitInfo["staged_count"] = len(status.Staged)
		gitInfo["unstaged_count"] = len(status.Unstaged)
		gitInfo["untracked_count"] = len(status.Untracked)
		gitInfo["state"] = status.State

		// Set top-level git state fields
		response["is_git_repo"] = status.IsGitRepo
		response["git_state"] = status.State
	}

	response["git"] = gitInfo
	return response, nil
}

// Remove unregisters a workspace.
func (s *WorkspaceConfigService) Remove(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Get workspace details before removal (for the event)
	ws, err := s.configManager.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Check for active sessions
	activeCount := s.sessionManager.CountActiveSessionsForWorkspace(p.WorkspaceID)
	if activeCount > 0 {
		return nil, message.NewError(message.InternalError, fmt.Sprintf("cannot remove workspace with %d active session(s)", activeCount))
	}

	// Unregister from session manager
	if err := s.sessionManager.UnregisterWorkspace(p.WorkspaceID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Remove from config
	if err := s.configManager.RemoveWorkspace(p.WorkspaceID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	if s.authRegistry != nil {
		if err := s.authRegistry.UnbindWorkspace(p.WorkspaceID); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}
	}

	// Broadcast workspace_removed event to all clients
	if s.hub != nil {
		event := events.NewWorkspaceRemovedEvent(ws.Definition.ID, ws.Definition.Name, ws.Definition.Path)
		s.hub.Publish(event)
	}

	return map[string]interface{}{
		"success": true,
		"message": "Workspace removed",
	}, nil
}

func (s *WorkspaceConfigService) bindWorkspaceToDevice(ctx context.Context, workspaceID string) error {
	if s.authRegistry == nil {
		return nil
	}

	payload, _ := ctx.Value(handler.AuthPayloadKey).(*security.TokenPayload)
	if payload == nil {
		return nil
	}
	if payload.DeviceID == "" {
		return fmt.Errorf("token missing device_id; re-pair required")
	}

	return s.authRegistry.BindWorkspace(payload.DeviceID, workspaceID)
}

// Update updates workspace settings.
func (s *WorkspaceConfigService) Update(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		ID        string  `json:"id"`
		Name      *string `json:"name"`
		AutoStart *bool   `json:"auto_start"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.ID == "" {
		return nil, message.NewError(message.InvalidParams, "id is required")
	}

	ws, err := s.configManager.GetWorkspace(p.ID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Update fields if provided
	if p.Name != nil {
		ws.Definition.Name = *p.Name
	}
	if p.AutoStart != nil {
		ws.Definition.AutoStart = *p.AutoStart
	}

	if err := s.configManager.UpdateWorkspace(ws); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	}, nil
}

// Discover scans for git repositories.
// Uses cache-first strategy: returns cached results immediately if available,
// triggers background refresh if cache is stale.
func (s *WorkspaceConfigService) Discover(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		Paths []string `json:"paths"`
		Fresh bool     `json:"fresh"` // Force fresh scan, ignore cache
	}
	// Params are optional
	_ = json.Unmarshal(params, &p)

	var result *workspace.DiscoveryResult
	var err error

	if p.Fresh {
		result, err = s.configManager.DiscoverRepositoriesFresh(p.Paths)
	} else {
		result, err = s.configManager.DiscoverRepositoriesWithResult(p.Paths)
	}

	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"repositories":        result.Repositories,
		"count":               result.Count,
		"cached":              result.Cached,
		"cache_age_seconds":   result.CacheAgeSeconds,
		"refresh_in_progress": result.RefreshInProgress,
		"elapsed_ms":          result.ElapsedMs,
		"scanned_paths":       result.ScannedPaths,
		"skipped_paths":       result.SkippedPaths,
	}, nil
}

// Status returns detailed status for a specific workspace.
// Response format matches status/get but scoped to a workspace.
func (s *WorkspaceConfigService) Status(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Get workspace
	ws, err := s.configManager.GetWorkspace(p.WorkspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Get active sessions
	sessions := s.sessionManager.GetSessionsForWorkspace(p.WorkspaceID)
	sessionInfos := make([]*session.Info, 0, len(sessions))
	activeCount := 0
	for _, sess := range sessions {
		sessionInfos = append(sessionInfos, sess.ToInfo())
		if sess.GetStatus() == session.StatusRunning {
			activeCount++
		}
	}

	// Get git tracker info
	var gitTrackerState string
	var gitRepoName string
	var isGitRepo bool
	var gitLastError string

	gtm := s.configManager.GetGitTrackerManager()
	if gtm != nil {
		trackerInfo, err := gtm.GetTrackerInfo(p.WorkspaceID)
		if err == nil && trackerInfo != nil {
			gitTrackerState = string(trackerInfo.State)
			gitRepoName = trackerInfo.RepoName
			isGitRepo = trackerInfo.IsGitRepo
			gitLastError = trackerInfo.LastError
		}
	}

	// Get watch status from session manager
	watchInfo := s.sessionManager.GetWatchedSession()
	isBeingWatched := watchInfo != nil && watchInfo.Watching && watchInfo.WorkspaceID == p.WorkspaceID

	return map[string]interface{}{
		// Workspace info
		"workspace_id":   ws.Definition.ID,
		"workspace_name": ws.Definition.Name,
		"path":           ws.Definition.Path,
		"auto_start":     ws.Definition.AutoStart,
		"created_at":     ws.Definition.CreatedAt,

		// Session info
		"sessions":             sessionInfos,
		"active_session_count": activeCount,
		"has_active_session":   activeCount > 0,

		// Git tracker info
		"git_tracker_state": gitTrackerState,
		"git_repo_name":     gitRepoName,
		"is_git_repo":       isGitRepo,
		"git_last_error":    gitLastError,

		// Watch status
		"is_being_watched": isBeingWatched,
		"watched_session_id": func() string {
			if isBeingWatched {
				return watchInfo.SessionID
			}
			return ""
		}(),
	}, nil
}

// InvalidateCache clears the discovery cache.
// This forces the next workspace/discover call to perform a fresh scan.
func (s *WorkspaceConfigService) InvalidateCache(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if err := s.configManager.InvalidateDiscoveryCache(); err != nil {
		// Cache file might not exist, which is fine
		return map[string]interface{}{
			"success": true,
			"message": "Cache cleared (or was already empty)",
		}, nil
	}

	return map[string]interface{}{
		"success": true,
		"message": "Discovery cache invalidated",
	}, nil
}

// WorkspaceInfo represents workspace information for API responses.
type WorkspaceInfo struct {
	ID               string        `json:"id"`
	Name             string        `json:"name"`
	Path             string        `json:"path"`
	AutoStart        bool          `json:"auto_start"`
	CreatedAt        string        `json:"created_at"`
	HasActiveSession bool          `json:"has_active_session"`
	Session          *session.Info `json:"session,omitempty"`
}
