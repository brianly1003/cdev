package methods

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/session"
)

type sessionRuntimeDispatch struct {
	start   func(ctx context.Context, workspaceID, sessionID string) (interface{}, *message.Error)
	stop    func(ctx context.Context, sessionID string) (interface{}, *message.Error)
	send    func(ctx context.Context, workspaceID, sessionID, prompt, mode, permissionMode string, yoloMode bool) (interface{}, *message.Error)
	input   func(ctx context.Context, sessionID, input, key string) (interface{}, *message.Error)
	respond func(ctx context.Context, sessionID, responseType, response string) (interface{}, *message.Error)
}

func (s *SessionManagerService) ensureRuntimeDispatch() {
	if s.runtimeDispatch != nil {
		return
	}

	s.runtimeDispatch = map[string]sessionRuntimeDispatch{
		sessionManagerAgentClaude: {
			start:   s.startClaudeSession,
			stop:    s.stopClaudeSession,
			send:    s.sendClaudePrompt,
			input:   s.inputClaudeSession,
			respond: s.respondClaudeSessionRPC,
		},
		sessionManagerAgentCodex: {
			start:   s.startCodexSession,
			stop:    s.stopCodexSessionRPC,
			send:    s.sendCodexPromptWithPermissionMode,
			input:   s.inputCodexSession,
			respond: s.respondCodexSessionRPC,
		},
	}
}

func (s *SessionManagerService) supportedRuntimeAgents() []string {
	s.ensureRuntimeDispatch()

	agents := make([]string, 0, len(s.runtimeDispatch))
	for agentType := range s.runtimeDispatch {
		agents = append(agents, agentType)
	}
	sort.Strings(agents)
	return agents
}

func (s *SessionManagerService) resolveRuntimeDispatch(rawAgentType string) (string, sessionRuntimeDispatch, *message.Error) {
	s.ensureRuntimeDispatch()

	agentType := strings.ToLower(strings.TrimSpace(rawAgentType))
	if agentType == "" {
		agentType = sessionManagerAgentClaude
	}

	dispatch, ok := s.runtimeDispatch[agentType]
	if ok {
		return agentType, dispatch, nil
	}

	allowed := strings.Join(s.supportedRuntimeAgents(), ", ")
	return "", sessionRuntimeDispatch{}, message.NewError(message.InvalidParams, "agent_type must be one of: "+allowed)
}

func (s *SessionManagerService) resolveRuntimeDispatchForWorkspaceSession(rawAgentType string, workspaceID string, sessionID string) (string, sessionRuntimeDispatch, *message.Error) {
	_ = workspaceID
	_ = sessionID
	s.ensureRuntimeDispatch()
	return s.resolveRuntimeDispatch(rawAgentType)
}

func (s *SessionManagerService) startClaudeSession(ctx context.Context, workspaceID, sessionID string) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewError(message.AgentNotConfigured, "claude session manager is not configured")
	}

	// If session_id is provided, validate against .claude/projects
	if sessionID != "" {
		exists, err := s.manager.SessionFileExists(workspaceID, sessionID)
		if err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}

		if exists {
			// Session exists in .claude/projects - activate it for LIVE attachment
			_ = s.manager.ActivateSession(workspaceID, sessionID)
			return map[string]interface{}{
				"session_id":   sessionID,
				"workspace_id": workspaceID,
				"source":       "live",
				"status":       "attached",
				"agent_type":   sessionManagerAgentClaude,
				"message":      "Session found in .claude/projects - ready for LIVE interaction",
			}, nil
		}

		// Session doesn't exist - return empty to let user select from history
		return map[string]interface{}{
			"session_id":   "",
			"workspace_id": workspaceID,
			"source":       "",
			"status":       "not_found",
			"agent_type":   sessionManagerAgentClaude,
			"message":      "Session not found in .claude/projects. Use workspace/session/history to select a valid session.",
		}, nil
	}

	// No session_id provided - get the latest session from .claude/projects
	latestSessionID, err := s.manager.GetLatestSessionID(workspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	if latestSessionID != "" {
		// Activate the latest session for LIVE attachment
		_ = s.manager.ActivateSession(workspaceID, latestSessionID)
		return map[string]interface{}{
			"session_id":   latestSessionID,
			"workspace_id": workspaceID,
			"source":       "live",
			"status":       "attached",
			"agent_type":   sessionManagerAgentClaude,
			"message":      "Latest session found in .claude/projects - ready for LIVE interaction",
		}, nil
	}

	// Check if there's already an active managed session for this workspace.
	// This handles the case where iOS app was closed and reopened while Claude
	// was still waiting for trust folder approval.
	if activeSessionID := s.manager.GetActiveSession(workspaceID); activeSessionID != "" {
		if existingSession, err := s.manager.GetSession(activeSessionID); err == nil {
			status := existingSession.GetStatus()
			if status == session.StatusRunning || status == session.StatusStarting {
				// Check if there's a pending permission (e.g., trust folder prompt)
				result := map[string]interface{}{
					"session_id":             activeSessionID,
					"workspace_id":           workspaceID,
					"source":                 "managed",
					"status":                 "existing",
					"agent_type":             sessionManagerAgentClaude,
					"message":                "Returning existing active session (Claude still running)",
					"has_pending_permission": false,
				}

				// If there's a pending permission, re-emit the pty_permission event
				// so the reconnecting client can show the permission dialog.
				if cm := existingSession.ClaudeManager(); cm != nil {
					if pendingPerm := cm.GetPendingPTYPermission(); pendingPerm != nil {
						result["has_pending_permission"] = true
						result["pending_permission_type"] = string(pendingPerm.Type)
						result["pending_permission_target"] = pendingPerm.Target

						// Re-emit the pty_permission event for the reconnecting client.
						options := make([]events.PTYPromptOption, len(pendingPerm.Options))
						for i, opt := range pendingPerm.Options {
							options[i] = events.PTYPromptOption{
								Key:         opt.Key,
								Label:       opt.Label,
								Description: opt.Description,
								Selected:    opt.Selected,
							}
						}
						evt := events.NewPTYPermissionEventWithSession(
							string(pendingPerm.Type),
							pendingPerm.Target,
							pendingPerm.Description,
							pendingPerm.Preview,
							activeSessionID,
							options,
						)
						evt.SetAgentType(sessionManagerAgentClaude)
						s.manager.PublishEvent(evt)
					}
				}

				return result, nil
			}
		}
	}

	// No sessions found in .claude/projects - start a new managed session.
	// This starts Claude in interactive mode (PTY) waiting for user input.
	newSession, err := s.manager.StartSession(workspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to start session: "+err.Error())
	}

	// Get workspace for path
	ws, err := s.manager.GetWorkspace(workspaceID)
	if err != nil {
		return nil, message.NewError(message.InternalError, "failed to get workspace: "+err.Error())
	}

	// Always watch for new session file creation for PTY sessions.
	// Claude creates a NEW session file with a NEW UUID every time it starts.
	// The temporary ID generated by cdev is internal only - we need to detect
	// the real session ID from Claude and emit session_id_resolved event
	// so iOS can switch to watching the real session file.
	go s.manager.WatchForNewSessionFile(ctx, workspaceID, newSession.ID, ws.Definition.Path)

	// Start Claude in interactive PTY mode (no initial prompt).
	claudeManager := newSession.ClaudeManager()
	if claudeManager != nil {
		// Set up callback to detect when Claude exits without creating a session.
		// This handles the case where user declines trust folder (clicks "No").
		temporaryID := newSession.ID
		workspace := workspaceID
		claudeManager.SetOnPTYComplete(func(sid string) {
			// Check if there's still an active watcher - if so, Claude exited
			// without creating a session file (user likely declined trust).
			if s.manager.HasActiveSessionFileWatcher(workspace) {
				s.manager.FailSessionIDResolution(
					workspace,
					temporaryID,
					"trust_declined",
					"Claude exited without creating a session. User may have declined trust folder.",
				)
			}
		})

		go func() {
			// Start Claude with empty prompt for interactive mode.
			if err := claudeManager.StartWithPTY(ctx, "", "new", newSession.ID, false); err != nil {
				// Log error but don't fail - session is still created.
				// The user can send a prompt via session/send.
				fmt.Printf("Warning: failed to start Claude in interactive mode: %v\n", err)
			}
		}()
	}

	return map[string]interface{}{
		"session_id":   newSession.ID,
		"workspace_id": workspaceID,
		"source":       "managed",
		"status":       "started",
		"agent_type":   sessionManagerAgentClaude,
		"message":      "New Claude session started in interactive mode",
	}, nil
}

func (s *SessionManagerService) stopClaudeSession(ctx context.Context, sessionID string) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewError(message.AgentNotConfigured, "claude session manager is not configured")
	}

	if err := s.manager.StopSession(sessionID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success":    true,
		"message":    "Session stopped",
		"agent_type": sessionManagerAgentClaude,
	}, nil
}

func (s *SessionManagerService) sendClaudePrompt(ctx context.Context, workspaceID, sessionID, prompt, mode, permissionMode string, yoloMode bool) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewError(message.AgentNotConfigured, "claude session manager is not configured")
	}

	// Auto-create session if session_id is empty but workspace_id is provided.
	if sessionID == "" {
		if workspaceID == "" {
			return nil, message.NewError(message.InvalidParams, "either session_id or workspace_id is required")
		}

		// Auto-create a new session for this workspace.
		newSession, err := s.manager.StartSession(workspaceID)
		if err != nil {
			return nil, message.NewError(message.InternalError, "failed to auto-create session: "+err.Error())
		}

		sessionID = newSession.ID

		// Get workspace for path (needed for session file watcher).
		ws, err := s.manager.GetWorkspace(workspaceID)
		if err != nil {
			return nil, message.NewError(message.InternalError, "failed to get workspace: "+err.Error())
		}

		// Always watch for new session file creation for PTY sessions.
		// Claude creates a NEW session file with a NEW UUID every time.
		go s.manager.WatchForNewSessionFile(ctx, workspaceID, newSession.ID, ws.Definition.Path)

		// Start Claude with the prompt immediately (don't wait for user input).
		claudeManager := newSession.ClaudeManager()
		if claudeManager != nil {
			// Set up callback to detect when Claude exits without creating a session.
			temporaryID := newSession.ID
			workspace := workspaceID
			claudeManager.SetOnPTYComplete(func(sid string) {
				if s.manager.HasActiveSessionFileWatcher(workspace) {
					s.manager.FailSessionIDResolution(
						workspace,
						temporaryID,
						"trust_declined",
						"Claude exited without creating a session. User may have declined trust folder.",
					)
				}
			})

			// Start Claude with PTY and the prompt.
			go func() {
				if err := claudeManager.StartWithPTY(ctx, prompt, "new", newSession.ID, yoloMode); err != nil {
					// Log error but session was created.
					fmt.Printf("Warning: failed to start Claude with prompt: %v\n", err)
				}
			}()
		}

		return map[string]interface{}{
			"status":       "sent",
			"session_id":   sessionID,
			"workspace_id": workspaceID,
			"auto_created": true,
			"agent_type":   sessionManagerAgentClaude,
			"message":      "New session created and prompt sent",
		}, nil
	}

	// Session ID provided - send to existing session.
	if err := s.manager.SendPrompt(sessionID, prompt, mode, permissionMode, yoloMode); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"status":     "sent",
		"session_id": sessionID,
		"agent_type": sessionManagerAgentClaude,
	}, nil
}

func (s *SessionManagerService) inputClaudeSession(ctx context.Context, sessionID, input, key string) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewError(message.AgentNotConfigured, "claude session manager is not configured")
	}

	// If a special key is provided, use SendKey (handles key codes for LIVE sessions).
	if key != "" {
		// Validate key name.
		validKeys := map[string]bool{
			"enter": true, "return": true, "escape": true, "esc": true,
			"up": true, "down": true, "left": true, "right": true,
			"tab": true, "backspace": true, "delete": true,
			"home": true, "end": true, "pageup": true, "pagedown": true, "space": true,
		}
		if !validKeys[key] {
			return nil, message.NewError(message.InvalidParams, "unknown key: "+key+". Valid keys: enter, escape, up, down, left, right, tab, backspace, delete, home, end, pageup, pagedown, space")
		}

		if err := s.manager.SendKey(sessionID, key); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}

		// Emit pty_permission_resolved event so other devices can dismiss their permission popups.
		clientID, _ := ctx.Value(handler.ClientIDKey).(string)
		s.manager.EmitPermissionResolved(sessionID, clientID, key)

		return map[string]interface{}{
			"status":     "sent",
			"key":        key,
			"agent_type": sessionManagerAgentClaude,
		}, nil
	}

	// If text input is provided, use SendInput.
	if input != "" {
		if err := s.manager.SendInput(sessionID, input); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}

		// Emit pty_permission_resolved event so other devices can dismiss their permission popups.
		clientID, _ := ctx.Value(handler.ClientIDKey).(string)
		s.manager.EmitPermissionResolved(sessionID, clientID, input)

		return map[string]interface{}{
			"status":     "sent",
			"agent_type": sessionManagerAgentClaude,
		}, nil
	}

	return nil, message.NewError(message.InvalidParams, "either 'input' or 'key' is required")
}

func (s *SessionManagerService) respondClaudeSessionRPC(ctx context.Context, sessionID, responseType, response string) (interface{}, *message.Error) {
	if s.manager == nil {
		return nil, message.NewError(message.AgentNotConfigured, "claude session manager is not configured")
	}

	var responseErr error
	switch responseType {
	case "permission":
		allow := response == "yes" || response == "true" || response == "allow"
		responseErr = s.manager.RespondToPermission(sessionID, allow)
	case "question":
		responseErr = s.manager.RespondToQuestion(sessionID, response)
	default:
		return nil, message.NewError(message.InvalidParams, "type must be 'permission' or 'question'")
	}

	if responseErr != nil {
		return nil, message.NewError(message.InternalError, responseErr.Error())
	}

	return map[string]interface{}{
		"status":     "responded",
		"agent_type": sessionManagerAgentClaude,
	}, nil
}

func (s *SessionManagerService) stopCodexSessionRPC(ctx context.Context, sessionID string) (interface{}, *message.Error) {
	if err := s.stopCodexSession(sessionID); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}

	return map[string]interface{}{
		"success":    true,
		"message":    "Session stopped",
		"agent_type": sessionManagerAgentCodex,
	}, nil
}

func (s *SessionManagerService) inputCodexSession(ctx context.Context, sessionID, input, key string) (interface{}, *message.Error) {
	if key != "" {
		if err := s.sendCodexInput(sessionID, key); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}
		s.emitCodexPermissionResolved(ctx, sessionID, key)
		return map[string]interface{}{
			"status":     "sent",
			"key":        key,
			"agent_type": sessionManagerAgentCodex,
		}, nil
	}

	if input != "" {
		if err := s.sendCodexInput(sessionID, input); err != nil {
			return nil, message.NewError(message.InternalError, err.Error())
		}
		s.emitCodexPermissionResolved(ctx, sessionID, input)
		return map[string]interface{}{
			"status":     "sent",
			"agent_type": sessionManagerAgentCodex,
		}, nil
	}

	return nil, message.NewError(message.InvalidParams, "either 'input' or 'key' is required")
}

func (s *SessionManagerService) respondCodexSessionRPC(ctx context.Context, sessionID, responseType, response string) (interface{}, *message.Error) {
	if err := s.respondCodexSession(sessionID, responseType, response); err != nil {
		return nil, message.NewError(message.InternalError, err.Error())
	}
	s.emitCodexPermissionResolved(ctx, sessionID, response)
	return map[string]interface{}{
		"status":     "responded",
		"agent_type": sessionManagerAgentCodex,
	}, nil
}

func (s *SessionManagerService) sendCodexPromptWithPermissionMode(ctx context.Context, workspaceID, sessionID, prompt, mode, permissionMode string, yoloMode bool) (interface{}, *message.Error) {
	// Keep this runtime-agnostic: permission mode can request bypass semantics,
	// and yolo_mode is the explicit runtime-independent bypass signal.
	enableBypass := yoloMode || permissionMode == "bypassPermissions"
	return s.sendCodexPrompt(ctx, workspaceID, sessionID, prompt, mode, enableBypass)
}

func validatePermissionMode(permissionMode string) *message.Error {
	if permissionMode == "" {
		return nil
	}

	validModes := map[string]bool{
		"default": true, "acceptEdits": true, "bypassPermissions": true, "plan": true, "interactive": true,
	}
	if validModes[permissionMode] {
		return nil
	}

	return message.NewError(message.InvalidParams, "permission_mode must be one of: default, acceptEdits, bypassPermissions, plan, interactive")
}
