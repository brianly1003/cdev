// Package http provides HTTP handlers for cdev.
package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/permission"
	"github.com/rs/zerolog/log"
)

// PermissionManager interface for checking/storing permission decisions.
// This allows the hooks handler to use the existing permission memory system.
type PermissionManager interface {
	CheckMemory(sessionID, toolName string, toolInput map[string]interface{}) *permission.StoredDecision
	StoreDecision(sessionID, workspaceID, pattern string, decision permission.Decision)
	AddPendingRequest(req *permission.Request)
	GetAndRemovePendingRequest(toolUseID string) *permission.Request
}

// HooksHandler handles incoming Claude hook events.
type HooksHandler struct {
	hub               *hub.Hub
	permissionManager PermissionManager
	permissionTimeout time.Duration
}

// NewHooksHandler creates a new HooksHandler.
func NewHooksHandler(h *hub.Hub) *HooksHandler {
	return &HooksHandler{
		hub:               h,
		permissionTimeout: 60 * time.Second, // Default timeout
	}
}

// SetPermissionManager sets the permission manager for handling PreToolUse requests.
func (h *HooksHandler) SetPermissionManager(pm PermissionManager) {
	h.permissionManager = pm
}

// SetPermissionTimeout sets the timeout for waiting for mobile responses.
func (h *HooksHandler) SetPermissionTimeout(timeout time.Duration) {
	h.permissionTimeout = timeout
}

// ClaudeHookPayload represents the data sent by Claude hooks.
// Claude sends different structures for different hook types, so we capture
// both specific fields and the raw payload for flexibility.
type ClaudeHookPayload struct {
	// Common fields (all hook types)
	SessionID      string `json:"session_id,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	HookEventName  string `json:"hook_event_name,omitempty"` // PreToolUse, PostToolUse, Notification, SessionStart
	PermissionMode string `json:"permission_mode,omitempty"` // default, etc.

	// Tool use fields (PreToolUse/PostToolUse)
	ToolName   string      `json:"tool_name,omitempty"`
	ToolInput  interface{} `json:"tool_input,omitempty"`
	ToolResult interface{} `json:"tool_result,omitempty"`
	ToolUseID  string      `json:"tool_use_id,omitempty"`

	// Notification fields (permission_prompt)
	Message          string `json:"message,omitempty"`           // "Claude needs your permission to use Bash"
	NotificationType string `json:"notification_type,omitempty"` // "permission_prompt"

	// Raw payload for any fields we didn't explicitly map
	Raw map[string]interface{} `json:"-"`
}

// HandleHook handles POST /api/hooks/:hookType
//
//	@Summary		Receive Claude hook event
//	@Description	Receives events from Claude Code hooks (SessionStart, Notification, PreToolUse, PostToolUse)
//	@Tags			hooks
//	@Accept			json
//	@Produce		json
//	@Param			hookType	path		string				true	"Hook type (session, permission, tool-start, tool-end)"
//	@Param			body		body		ClaudeHookPayload	true	"Hook payload from Claude"
//	@Success		200			{object}	map[string]bool
//	@Router			/api/hooks/{hookType} [post]
func (h *HooksHandler) HandleHook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract hook type from path: /api/hooks/{hookType}
	path := strings.TrimPrefix(r.URL.Path, "/api/hooks/")
	hookType := strings.TrimSuffix(path, "/")

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024)) // 1MB limit
	if err != nil {
		log.Warn().Err(err).Msg("failed to read hook body")
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Parse payload
	var payload ClaudeHookPayload
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Debug().Err(err).Str("body", string(body)).Msg("failed to parse hook payload")
			// Continue anyway - some hooks may send non-JSON data
		}
		// Also parse into Raw map for debugging and flexible field access
		if err := json.Unmarshal(body, &payload.Raw); err != nil {
			log.Debug().Err(err).Msg("failed to parse raw hook payload")
		}
	}

	// Log raw body for debugging hook payloads
	log.Debug().
		Str("hook_type", hookType).
		Str("raw_body", string(body)).
		Msg("hook raw payload")

	log.Info().
		Str("hook_type", hookType).
		Str("session_id", payload.SessionID).
		Str("cwd", payload.Cwd).
		Str("tool_name", payload.ToolName).
		Str("message", payload.Message).
		Msg("received Claude hook event")

	// Publish event based on hook type
	switch hookType {
	case "session":
		h.handleSessionStart(payload)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case "notification", "permission":
		// Both "notification" and "permission" are handled the same way
		// "permission" is the old endpoint name, "notification" is the new one
		h.handleNotification(payload)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case "permission-request":
		// Blocking permission request - waits for mobile response
		h.handlePermissionRequest(w, payload)
		// Response written by handlePermissionRequest
		return

	case "tool-start":
		h.handleToolStart(payload)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	case "tool-end":
		h.handleToolEnd(payload)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

	default:
		log.Warn().Str("hook_type", hookType).Msg("unknown hook type")
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func (h *HooksHandler) handleSessionStart(payload ClaudeHookPayload) {
	data := map[string]interface{}{
		"session_id":      payload.SessionID,
		"cwd":             payload.Cwd,
		"transcript_path": payload.TranscriptPath,
		"permission_mode": payload.PermissionMode,
		"source":          "external", // Claude running outside of cdev
	}

	// Include raw payload for any fields we didn't explicitly map
	if payload.Raw != nil {
		for k, v := range payload.Raw {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	event := events.NewEvent(events.EventTypeClaudeHookSession, data)
	h.hub.Publish(event)
}

func (h *HooksHandler) handleNotification(payload ClaudeHookPayload) {
	// Claude Notification hook sends: message, notification_type, cwd, etc.
	// Example message: "Claude needs your permission to use Bash"
	// This is fire-and-forget - just notifies iOS that a permission prompt appeared
	data := map[string]interface{}{
		"session_id":        payload.SessionID,
		"cwd":               payload.Cwd,
		"message":           payload.Message,
		"notification_type": payload.NotificationType,
		"transcript_path":   payload.TranscriptPath,
	}

	// Include raw payload for any fields we didn't explicitly map
	if payload.Raw != nil {
		for k, v := range payload.Raw {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	event := events.NewEvent(events.EventTypeClaudeHookPermission, data)
	h.hub.Publish(event)
}

// handlePermissionRequest handles blocking permission requests from PreToolUse hooks.
// This is the core of the Permission Hook Bridge - it enables mobile permission approval.
//
// Flow:
// 1. Check pattern memory for stored "Allow for Session" decisions
// 2. If match found, return immediately with stored decision
// 3. If no match, forward to iOS via pty_permission event
// 4. Wait for iOS response via permission/respond RPC (with timeout)
// 5. Return decision to Claude hook script
func (h *HooksHandler) handlePermissionRequest(w http.ResponseWriter, payload ClaudeHookPayload) {
	log.Info().
		Str("session_id", payload.SessionID).
		Str("tool_name", payload.ToolName).
		Str("tool_use_id", payload.ToolUseID).
		Msg("handling permission request from PreToolUse hook")

	// If permission manager is not configured, fallback to "ask" (desktop prompt)
	if h.permissionManager == nil {
		log.Warn().Msg("permission manager not configured - returning 'ask'")
		writeJSON(w, http.StatusOK, map[string]string{
			"decision": "ask",
			"message":  "permission_manager_not_configured",
		})
		return
	}

	// Parse tool_input into map for pattern matching
	toolInput := make(map[string]interface{})
	if payload.ToolInput != nil {
		if inputMap, ok := payload.ToolInput.(map[string]interface{}); ok {
			toolInput = inputMap
		}
	}

	// 1. Check pattern memory for stored decisions (fast path)
	if stored := h.permissionManager.CheckMemory(payload.SessionID, payload.ToolName, toolInput); stored != nil {
		log.Info().
			Str("session_id", payload.SessionID).
			Str("pattern", stored.Pattern).
			Str("decision", string(stored.Decision)).
			Msg("found matching pattern in memory - returning stored decision")

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"decision": string(stored.Decision),
			"scope":    "session",
			"pattern":  stored.Pattern,
		})
		return
	}

	// 2. No stored decision - need to ask mobile
	// Create pending request with response channel
	req := &permission.Request{
		ID:           payload.ToolUseID,
		SessionID:    payload.SessionID,
		WorkspaceID:  "", // Will be resolved from cwd if needed
		ToolName:     payload.ToolName,
		ToolInput:    toolInput,
		ToolUseID:    payload.ToolUseID,
		CreatedAt:    time.Now(),
		ResponseChan: make(chan *permission.Response, 1),
	}

	h.permissionManager.AddPendingRequest(req)

	// 3. Publish pty_permission event to iOS
	permEvent := h.createPermissionEvent(req, payload)
	h.hub.Publish(permEvent)

	log.Info().
		Str("tool_use_id", payload.ToolUseID).
		Str("tool_name", payload.ToolName).
		Dur("timeout", h.permissionTimeout).
		Msg("waiting for mobile response")

	// 4. Wait for response with timeout
	ctx, cancel := context.WithTimeout(context.Background(), h.permissionTimeout)
	defer cancel()

	select {
	case response := <-req.ResponseChan:
		// Got response from iOS
		log.Info().
			Str("tool_use_id", payload.ToolUseID).
			Str("decision", string(response.Decision)).
			Str("scope", string(response.Scope)).
			Msg("received permission response from mobile")

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"decision": string(response.Decision),
			"scope":    string(response.Scope),
			"pattern":  response.Pattern,
		})

	case <-ctx.Done():
		// Timeout - return "ask" to let Claude prompt on desktop
		h.permissionManager.GetAndRemovePendingRequest(payload.ToolUseID)
		log.Warn().
			Str("tool_use_id", payload.ToolUseID).
			Dur("timeout", h.permissionTimeout).
			Msg("permission request timed out - returning 'ask'")

		writeJSON(w, http.StatusOK, map[string]string{
			"decision": "ask",
			"message":  "timeout",
		})
	}
}

// createPermissionEvent creates a pty_permission event to send to iOS.
func (h *HooksHandler) createPermissionEvent(req *permission.Request, payload ClaudeHookPayload) *events.BaseEvent {
	// Generate human-readable description
	description := permission.GenerateReadableDescription(req.ToolName, req.ToolInput)
	target := permission.ExtractTarget(req.ToolName, req.ToolInput)
	preview := permission.ExtractPreview(req.ToolName, req.ToolInput)
	permType := permission.GeneratePermissionType(req.ToolName)

	// Create options for iOS
	options := []events.PTYPromptOption{
		{Key: "allow_once", Label: "Allow Once", Description: "Allow this one request"},
		{Key: "allow_session", Label: "Allow for Session", Description: "Allow similar requests for this session"},
		{Key: "deny", Label: "Deny", Description: "Deny this request"},
	}

	eventPayload := events.PTYPermissionPayload{
		ToolUseID:   req.ToolUseID,
		Type:        permType,
		Target:      target,
		Description: description,
		Preview:     preview,
		Options:     options,
		SessionID:   req.SessionID,
		WorkspaceID: req.WorkspaceID,
	}

	event := events.NewEvent(events.EventTypePTYPermission, eventPayload)
	event.SessionID = req.SessionID

	return event
}

func (h *HooksHandler) handleToolStart(payload ClaudeHookPayload) {
	data := map[string]interface{}{
		"session_id":      payload.SessionID,
		"cwd":             payload.Cwd,
		"tool_name":       payload.ToolName,
		"tool_input":      payload.ToolInput,
		"tool_use_id":     payload.ToolUseID,
		"transcript_path": payload.TranscriptPath,
		"permission_mode": payload.PermissionMode,
	}

	// Include raw payload for any fields we didn't explicitly map
	if payload.Raw != nil {
		for k, v := range payload.Raw {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	event := events.NewEvent(events.EventTypeClaudeHookToolStart, data)
	h.hub.Publish(event)
}

func (h *HooksHandler) handleToolEnd(payload ClaudeHookPayload) {
	data := map[string]interface{}{
		"session_id":      payload.SessionID,
		"cwd":             payload.Cwd,
		"tool_name":       payload.ToolName,
		"tool_result":     payload.ToolResult,
		"tool_use_id":     payload.ToolUseID,
		"transcript_path": payload.TranscriptPath,
	}

	// Include raw payload for any fields we didn't explicitly map
	if payload.Raw != nil {
		for k, v := range payload.Raw {
			if _, exists := data[k]; !exists {
				data[k] = v
			}
		}
	}

	event := events.NewEvent(events.EventTypeClaudeHookToolEnd, data)
	h.hub.Publish(event)
}
