// Package methods provides JSON-RPC method implementations.
package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/permission"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/rs/zerolog/log"
)

// PermissionManager defines the interface for managing permission requests.
type PermissionManager interface {
	// CheckMemory checks session memory for a matching pattern.
	CheckMemory(sessionID, toolName string, toolInput map[string]interface{}) *permission.StoredDecision

	// StoreDecision stores a permission decision in session memory.
	StoreDecision(sessionID, workspaceID, pattern string, decision permission.Decision)

	// AddPendingRequest adds a request waiting for mobile response.
	AddPendingRequest(req *permission.Request)

	// GetPendingRequest retrieves a pending request.
	GetPendingRequest(toolUseID string) *permission.Request

	// RemovePendingRequest removes a pending request.
	RemovePendingRequest(toolUseID string)

	// RespondToRequest sends a response to a pending request.
	RespondToRequest(toolUseID string, response *permission.Response) bool

	// GetAndRemovePendingRequest atomically gets and removes a pending request.
	GetAndRemovePendingRequest(toolUseID string) *permission.Request

	// ListPendingRequests returns all pending permission requests.
	ListPendingRequests() []*permission.Request

	// GetSessionStats returns statistics about session memory.
	GetSessionStats() map[string]interface{}
}

// EventPublisher defines the interface for publishing events.
type EventPublisher interface {
	Publish(event events.Event)
	// SubscriberCount returns the number of active subscribers.
	SubscriberCount() int
}

// WorkspaceResolver resolves workspace ID from a path.
type WorkspaceResolver interface {
	ResolveWorkspaceID(path string) (string, error)
}

// PermissionService provides permission-related RPC methods.
type PermissionService struct {
	manager           PermissionManager
	publisher         EventPublisher
	workspaceResolver WorkspaceResolver
	timeout           time.Duration
}

// NewPermissionService creates a new permission service.
func NewPermissionService(manager PermissionManager, publisher EventPublisher, resolver WorkspaceResolver) *PermissionService {
	return &PermissionService{
		manager:           manager,
		publisher:         publisher,
		workspaceResolver: resolver,
		timeout:           5 * time.Minute, // Default timeout for mobile response
	}
}

// SetTimeout sets the timeout for waiting for mobile responses.
func (s *PermissionService) SetTimeout(timeout time.Duration) {
	s.timeout = timeout
}

// RegisterMethods registers all permission methods with the registry.
func (s *PermissionService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("permission/request", s.Request, handler.MethodMeta{
		Summary:     "Request permission decision from mobile app",
		Description: "Called by the hook CLI to request a permission decision. Checks session memory first, then forwards to mobile app if no match.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Claude session ID"}},
			{Name: "tool_name", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Tool name (Bash, Write, Edit, etc.)"}},
			{Name: "tool_input", Required: true, Schema: map[string]interface{}{"type": "object", "description": "Tool input parameters"}},
			{Name: "tool_use_id", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Claude's tool use ID"}},
			{Name: "cwd", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Current working directory"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "PermissionResponse",
			Schema: map[string]interface{}{"$ref": "#/components/schemas/PermissionResponse"},
		},
	})

	r.RegisterWithMeta("permission/respond", s.Respond, handler.MethodMeta{
		Summary:     "Respond to a permission request",
		Description: "Called by the mobile app to respond to a pending permission request.",
		Params: []handler.OpenRPCParam{
			{Name: "tool_use_id", Required: true, Schema: map[string]interface{}{"type": "string", "description": "Tool use ID from the permission request"}},
			{Name: "decision", Required: true, Schema: map[string]interface{}{"type": "string", "enum": []string{"allow", "deny"}, "description": "Allow or deny the request"}},
			{Name: "scope", Required: false, Schema: map[string]interface{}{"type": "string", "enum": []string{"once", "session"}, "default": "once", "description": "Scope of the decision"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "RespondResult",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	r.RegisterWithMeta("permission/stats", s.Stats, handler.MethodMeta{
		Summary:     "Get permission system statistics",
		Description: "Returns statistics about the permission memory system.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "StatsResult",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	r.RegisterWithMeta("permission/pending", s.Pending, handler.MethodMeta{
		Summary:     "Get all pending permission requests",
		Description: "Returns all pending permission requests. Call this on reconnect to catch any missed permissions.",
		Params: []handler.OpenRPCParam{
			{Name: "session_id", Required: false, Schema: map[string]interface{}{"type": "string", "description": "Optional: filter by session ID"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "PendingResult",
			Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"requests": map[string]interface{}{"type": "array"}}},
		},
	})
}

// RequestParams for permission/request method.
type RequestParams struct {
	SessionID string                 `json:"session_id"`
	ToolName  string                 `json:"tool_name"`
	ToolInput map[string]interface{} `json:"tool_input"`
	ToolUseID string                 `json:"tool_use_id"`
	Cwd       string                 `json:"cwd"`
}

// Request handles a permission request from the hook CLI.
// This method blocks until a response is received from the mobile app or timeout.
func (s *PermissionService) Request(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	log.Debug().Msg("permission/request called")

	if s.manager == nil {
		return nil, message.ErrInternalError("Permission manager not available")
	}

	var p RequestParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams(fmt.Sprintf("Invalid params: %v", err))
	}

	// Validate required fields
	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("session_id is required")
	}
	if p.ToolName == "" {
		return nil, message.ErrInvalidParams("tool_name is required")
	}
	if p.ToolUseID == "" {
		return nil, message.ErrInvalidParams("tool_use_id is required")
	}

	// Check session memory for matching pattern
	if stored := s.manager.CheckMemory(p.SessionID, p.ToolName, p.ToolInput); stored != nil {
		// Found a matching pattern in session memory
		return &permission.Response{
			Decision: stored.Decision,
			Scope:    permission.ScopeSession,
			Pattern:  stored.Pattern,
		}, nil
	}

	// Resolve workspace ID from cwd
	workspaceID := ""
	if s.workspaceResolver != nil && p.Cwd != "" {
		if id, err := s.workspaceResolver.ResolveWorkspaceID(p.Cwd); err == nil {
			workspaceID = id
		}
	}

	// Create pending request with buffered channel
	req := &permission.Request{
		ID:           p.ToolUseID,
		SessionID:    p.SessionID,
		WorkspaceID:  workspaceID,
		ToolName:     p.ToolName,
		ToolInput:    p.ToolInput,
		ToolUseID:    p.ToolUseID,
		CreatedAt:    time.Now(),
		ResponseChan: make(chan *permission.Response, 1),
	}

	// Add to pending requests
	s.manager.AddPendingRequest(req)

	// Publish event to mobile app
	if s.publisher != nil {
		// Check if any mobile clients are connected
		// Subscriber count should be > 1 (the hook command itself is a subscriber)
		subscriberCount := s.publisher.SubscriberCount()
		if subscriberCount <= 1 {
			// No mobile clients connected - return deny (user cannot approve)
			s.manager.RemovePendingRequest(p.ToolUseID)
			log.Warn().
				Int("subscriber_count", subscriberCount).
				Msg("No mobile clients connected - returning deny")
			return &permission.Response{
				Decision: permission.DecisionDeny,
				Message:  "no_clients",
			}, nil
		}

		event := s.createPermissionEvent(req)
		log.Info().
			Str("tool_use_id", req.ToolUseID).
			Str("tool_name", req.ToolName).
			Str("session_id", req.SessionID).
			Str("workspace_id", req.WorkspaceID).
			Str("event_type", string(event.Type())).
			Int("subscriber_count", subscriberCount).
			Msg("Publishing pty_permission event to mobile app")
		s.publisher.Publish(event)
	} else {
		log.Warn().Msg("Publisher is nil - cannot send pty_permission event")
	}

	// Wait for response with timeout
	// Note: We don't use defer RemovePendingRequest here because
	// RespondToRequest already removes the request atomically
	select {
	case response := <-req.ResponseChan:
		return response, nil
	case <-time.After(s.timeout):
		// Timeout - return deny (user did not respond in time)
		s.manager.RemovePendingRequest(p.ToolUseID)
		log.Warn().
			Str("tool_use_id", p.ToolUseID).
			Dur("timeout", s.timeout).
			Msg("Permission request timed out - returning deny")
		return &permission.Response{
			Decision: permission.DecisionDeny,
			Message:  "timeout",
		}, nil
	case <-ctx.Done():
		// Context cancelled - return deny
		s.manager.RemovePendingRequest(p.ToolUseID)
		log.Warn().
			Str("tool_use_id", p.ToolUseID).
			Msg("Permission request cancelled - returning deny")
		return &permission.Response{
			Decision: permission.DecisionDeny,
			Message:  "cancelled",
		}, nil
	}
}

// createPermissionEvent creates a pty_permission event for the mobile app.
func (s *PermissionService) createPermissionEvent(req *permission.Request) *events.BaseEvent {
	// Generate human-readable description
	description := permission.GenerateReadableDescription(req.ToolName, req.ToolInput)
	target := permission.ExtractTarget(req.ToolName, req.ToolInput)
	preview := permission.ExtractPreview(req.ToolName, req.ToolInput)
	permType := permission.GeneratePermissionType(req.ToolName)

	// Create options for the mobile app
	options := []events.PTYPromptOption{
		{Key: "allow_once", Label: "Allow Once", Description: "Allow this one request"},
		{Key: "allow_session", Label: "Allow for Session", Description: "Allow similar requests for this session"},
		{Key: "deny", Label: "Deny", Description: "Deny this request"},
	}

	payload := events.PTYPermissionPayload{
		ToolUseID:   req.ToolUseID, // Include tool_use_id for mobile app to respond
		Type:        permType,
		Target:      target,
		Description: description,
		Preview:     preview,
		Options:     options,
		SessionID:   req.SessionID,
		WorkspaceID: req.WorkspaceID,
	}

	// Create event with context.
	// Both WorkspaceID and SessionID are set so that:
	// 1. Workspace subscription filtering works (client subscribed to ws-1 only gets ws-1 permissions)
	// 2. Workspace-scoped permission filtering works (all sessions in the focused workspace pass through)
	// The iOS app uses session_id in the payload to route to the correct session tab.
	event := events.NewEvent(events.EventTypePTYPermission, payload)
	event.SessionID = req.SessionID
	event.WorkspaceID = req.WorkspaceID
	event.SetAgentType("claude") // Permission hooks are Claude-specific

	return event
}

// RespondParams for permission/respond method.
type RespondParams struct {
	ToolUseID string `json:"tool_use_id"`
	Decision  string `json:"decision"` // "allow" or "deny"
	Scope     string `json:"scope"`    // "once" or "session"
}

// Respond handles a response from the mobile app.
func (s *PermissionService) Respond(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.ErrInternalError("Permission manager not available")
	}

	var p RespondParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams(fmt.Sprintf("Invalid params: %v", err))
	}

	// Validate required fields
	if p.ToolUseID == "" {
		return nil, message.ErrInvalidParams("tool_use_id is required")
	}
	if p.Decision == "" {
		return nil, message.ErrInvalidParams("decision is required")
	}
	if p.Decision != "allow" && p.Decision != "deny" {
		return nil, message.ErrInvalidParams("decision must be 'allow' or 'deny'")
	}

	// Default scope to once
	scope := permission.ScopeOnce
	if p.Scope == "session" {
		scope = permission.ScopeSession
	}

	// Atomically get and remove the pending request
	// This prevents race conditions where the request times out between
	// GetPendingRequest and RespondToRequest
	req := s.manager.GetAndRemovePendingRequest(p.ToolUseID)
	if req == nil {
		return nil, message.ErrInternalError("Request not found or already responded")
	}

	// Generate pattern for session scope
	pattern := ""
	if scope == permission.ScopeSession {
		pattern = permission.GeneratePattern(req.ToolName, req.ToolInput)
	}

	response := &permission.Response{
		Decision: permission.Decision(p.Decision),
		Scope:    scope,
		Pattern:  pattern,
	}

	// Store decision in session memory if scope is session
	if scope == permission.ScopeSession && pattern != "" {
		s.manager.StoreDecision(req.SessionID, req.WorkspaceID, pattern, response.Decision)
	}

	// Send response through the channel (non-blocking)
	select {
	case req.ResponseChan <- response:
		// Success
	default:
		// Channel is full - shouldn't happen with buffer size 1
		// but handle gracefully
	}

	// Publish resolved event to dismiss popups on other devices
	if s.publisher != nil {
		resolved := events.NewPTYPermissionResolvedEvent(
			req.SessionID,
			req.WorkspaceID,
			"", // clientID - not tracked currently
			string(response.Decision),
		)
		s.publisher.Publish(resolved)
	}

	return map[string]interface{}{
		"success": true,
	}, nil
}

// Stats returns permission system statistics.
func (s *PermissionService) Stats(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.ErrInternalError("Permission manager not available")
	}

	return s.manager.GetSessionStats(), nil
}

// PendingParams for permission/pending method.
type PendingParams struct {
	SessionID string `json:"session_id,omitempty"`
}

// PendingRequestInfo is the response format for pending permission requests.
type PendingRequestInfo struct {
	ToolUseID   string                 `json:"tool_use_id"`
	SessionID   string                 `json:"session_id"`
	WorkspaceID string                 `json:"workspace_id"`
	ToolName    string                 `json:"tool_name"`
	ToolInput   map[string]interface{} `json:"tool_input"`
	CreatedAt   string                 `json:"created_at"`
}

// Pending returns all pending permission requests.
// Call this on reconnect to catch any missed permissions.
func (s *PermissionService) Pending(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	log.Debug().Msg("permission/pending called")

	if s.manager == nil {
		return nil, message.ErrInternalError("Permission manager not available")
	}

	var p PendingParams
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p) // Optional params, ignore errors
	}

	// Get all pending requests
	requests := s.manager.ListPendingRequests()

	// Convert to response format, optionally filtering by session_id
	result := make([]PendingRequestInfo, 0, len(requests))
	for _, req := range requests {
		// Filter by session_id if provided
		if p.SessionID != "" && req.SessionID != p.SessionID {
			continue
		}

		result = append(result, PendingRequestInfo{
			ToolUseID:   req.ToolUseID,
			SessionID:   req.SessionID,
			WorkspaceID: req.WorkspaceID,
			ToolName:    req.ToolName,
			ToolInput:   req.ToolInput,
			CreatedAt:   req.CreatedAt.Format(time.RFC3339),
		})
	}

	log.Info().
		Int("count", len(result)).
		Str("filter_session_id", p.SessionID).
		Msg("Returning pending permission requests")

	return map[string]interface{}{
		"requests": result,
		"count":    len(result),
	}, nil
}

// WorkspaceIDResolver is a simple implementation that extracts workspace ID from path.
type WorkspaceIDResolver struct {
	// workspaceCache maps paths to workspace IDs (optional caching)
}

// NewWorkspaceIDResolver creates a new workspace ID resolver.
func NewWorkspaceIDResolver() *WorkspaceIDResolver {
	return &WorkspaceIDResolver{}
}

// ResolveWorkspaceID resolves a workspace ID from a path.
// For now, it uses the directory name as the workspace ID.
func (r *WorkspaceIDResolver) ResolveWorkspaceID(path string) (string, error) {
	// Use the base directory name
	return filepath.Base(path), nil
}
