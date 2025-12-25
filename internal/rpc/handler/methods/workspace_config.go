// Package methods provides JSON-RPC method implementations.
package methods

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
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
}

// NewWorkspaceConfigService creates a new workspace config service.
func NewWorkspaceConfigService(sessionManager *session.Manager, configManager *workspace.ConfigManager) *WorkspaceConfigService {
	return &WorkspaceConfigService{
		sessionManager: sessionManager,
		configManager:  configManager,
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
		Description: "Returns a list of all configured workspaces.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "workspaces",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/get", s.Get, handler.MethodMeta{
		Summary:     "Get workspace details",
		Description: "Returns detailed information about a specific workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "workspace",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/add", s.Add, handler.MethodMeta{
		Summary:     "Add a new workspace",
		Description: "Registers a new workspace (git repository) configuration.",
		Params: []handler.OpenRPCParam{
			{Name: "name", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "path", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "auto_start", Required: false, Schema: map[string]interface{}{"type": "boolean"}},
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
			{Name: "id", Required: true, Schema: map[string]interface{}{"type": "string"}},
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
func (s *WorkspaceConfigService) List(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	workspaces := s.configManager.ListWorkspaces()

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

		// Get session viewers for this workspace (if provider is available)
		var sessionViewers map[string][]string
		if s.viewerProvider != nil {
			sessionViewers = s.viewerProvider.GetSessionViewers(ws.Definition.ID)
		}

		// Track running session IDs to avoid duplicates
		runningSessionIDs := make(map[string]bool)

		// Get running sessions for this workspace
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
			// Skip if this session is already running
			if runningSessionIDs[hist.SessionID] {
				continue
			}
			sessInfo := map[string]interface{}{
				"id":            hist.SessionID,
				"workspace_id":  ws.Definition.ID,
				"status":        "historical",
				"summary":       hist.Summary,
				"message_count": hist.MessageCount,
				"last_updated":  hist.LastUpdated,
			}
			// Add viewers for this session
			if sessionViewers != nil {
				if viewers, ok := sessionViewers[hist.SessionID]; ok {
					sessInfo["viewers"] = viewers
				}
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

// Get returns details of a specific workspace.
func (s *WorkspaceConfigService) Get(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		ID string `json:"id"`
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

	info := map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	}

	// Get all sessions for this workspace
	sessions := s.sessionManager.GetSessionsForWorkspace(ws.Definition.ID)
	sessionInfos := make([]*session.Info, 0, len(sessions))
	activeCount := 0
	for _, sess := range sessions {
		sessionInfos = append(sessionInfos, sess.ToInfo())
		if sess.GetStatus() == session.StatusRunning {
			activeCount++
		}
	}
	info["sessions"] = sessionInfos
	info["active_session_count"] = activeCount
	info["has_active_session"] = activeCount > 0

	// Include the activated session ID (set via workspace/session/activate)
	activeSessionID := s.sessionManager.GetActiveSession(ws.Definition.ID)
	info["active_session_id"] = activeSessionID

	return info, nil
}

// Add registers a new workspace.
func (s *WorkspaceConfigService) Add(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		AutoStart bool   `json:"auto_start"`
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

	ws, err := s.configManager.AddWorkspace(p.Name, p.Path, p.AutoStart)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Register with session manager
	s.sessionManager.RegisterWorkspace(ws)

	return map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	}, nil
}

// Remove unregisters a workspace.
func (s *WorkspaceConfigService) Remove(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.ID == "" {
		return nil, message.NewError(message.InvalidParams, "id is required")
	}

	// Check for active sessions
	activeCount := s.sessionManager.CountActiveSessionsForWorkspace(p.ID)
	if activeCount > 0 {
		return nil, message.NewError(message.InternalError, fmt.Sprintf("cannot remove workspace with %d active session(s)", activeCount))
	}

	// Unregister from session manager
	if err := s.sessionManager.UnregisterWorkspace(p.ID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	// Remove from config
	if err := s.configManager.RemoveWorkspace(p.ID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success": true,
		"message": "Workspace removed",
	}, nil
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
	json.Unmarshal(params, &p)

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

	// Get active session ID
	activeSessionID := s.sessionManager.GetActiveSession(p.WorkspaceID)

	return map[string]interface{}{
		// Workspace info
		"workspace_id":         ws.Definition.ID,
		"workspace_name":       ws.Definition.Name,
		"path":                 ws.Definition.Path,
		"auto_start":           ws.Definition.AutoStart,
		"created_at":           ws.Definition.CreatedAt,

		// Session info
		"sessions":             sessionInfos,
		"active_session_count": activeCount,
		"has_active_session":   activeCount > 0,
		"active_session_id":    activeSessionID,

		// Git tracker info
		"git_tracker_state":    gitTrackerState,
		"git_repo_name":        gitRepoName,
		"is_git_repo":          isGitRepo,
		"git_last_error":       gitLastError,

		// Watch status
		"is_being_watched":     isBeingWatched,
		"watched_session_id":   func() string {
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
