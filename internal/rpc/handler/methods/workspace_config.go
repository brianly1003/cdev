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

// WorkspaceConfigService handles workspace configuration CRUD operations.
// This is the simplified version that only manages workspace configs, not processes.
type WorkspaceConfigService struct {
	sessionManager *session.Manager
	configManager  *workspace.ConfigManager
}

// NewWorkspaceConfigService creates a new workspace config service.
func NewWorkspaceConfigService(sessionManager *session.Manager, configManager *workspace.ConfigManager) *WorkspaceConfigService {
	return &WorkspaceConfigService{
		sessionManager: sessionManager,
		configManager:  configManager,
	}
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
}

// List returns all configured workspaces.
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
func (s *WorkspaceConfigService) Discover(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p struct {
		Paths []string `json:"paths"`
	}
	// Params are optional
	json.Unmarshal(params, &p)

	repos, err := s.configManager.DiscoverRepositories(p.Paths)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"repositories": repos,
		"count":        len(repos),
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
		"workspace_id":         ws.Definition.ID,
		"workspace_name":       ws.Definition.Name,
		"path":                 ws.Definition.Path,
		"auto_start":           ws.Definition.AutoStart,
		"created_at":           ws.Definition.CreatedAt,

		// Session info
		"sessions":             sessionInfos,
		"active_session_count": activeCount,
		"has_active_session":   activeCount > 0,

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
