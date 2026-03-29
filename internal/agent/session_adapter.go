// Package agent provides the SessionStarterAdapter that bridges the Spawner's
// SessionStarter interface to the session.Manager implementation.
package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/session"
	"github.com/rs/zerolog/log"
)

// SessionStarterAdapter bridges the Spawner's SessionStarter interface to session.Manager.
// It starts a managed session, sends prompts, and monitors completion.
type SessionStarterAdapter struct {
	manager *session.Manager
}

// NewSessionStarterAdapter creates a new adapter wrapping the session manager.
func NewSessionStarterAdapter(manager *session.Manager) *SessionStarterAdapter {
	return &SessionStarterAdapter{manager: manager}
}

// ResolveSessionID returns the canonical Claude session ID after any temporary
// cdev session ID has been remapped to the real transcript UUID.
func (a *SessionStarterAdapter) ResolveSessionID(sessionID string) string {
	if a == nil || a.manager == nil {
		return sessionID
	}
	resolved := a.manager.ResolveSessionID(sessionID)
	if resolved == "" {
		return sessionID
	}
	return resolved
}

// StopSession stops a managed or live Claude session.
func (a *SessionStarterAdapter) StopSession(sessionID string) error {
	if a == nil || a.manager == nil {
		return fmt.Errorf("session manager is not configured")
	}
	return a.manager.StopSession(sessionID)
}

// StartSessionWithPrompt starts a new session and sends the prompt.
// workDir overrides the working directory (e.g., worktree path). Empty string uses workspace default.
// It returns the session ID once the session is started and the prompt is submitted.
// The prompt runs asynchronously — this method does not wait for completion.
func (a *SessionStarterAdapter) StartSessionWithPrompt(ctx context.Context, workspaceID string, prompt string, agentType string, workDir string) (string, error) {
	logger := log.With().
		Str("workspace_id", workspaceID).
		Str("agent_type", agentType).
		Str("project_path", workDir).
		Logger()

	// Start a fresh session in the specified working directory (worktree path)
	sess, err := a.manager.StartNewSessionInDir(workspaceID, workDir)
	if err != nil {
		return "", fmt.Errorf("failed to start session: %w", err)
	}

	sessionID := sess.GetID()
	logger.Info().Str("session_id", sessionID).Msg("agent session started")

	// Watch for the real Claude session file and remap temporary session IDs.
	// Use worktree path if provided, otherwise fall back to workspace path.
	repoPath := workDir
	ws, err := a.manager.GetWorkspace(workspaceID)
	if err != nil {
		_ = a.manager.StopSession(sessionID)
		return "", fmt.Errorf("failed to get workspace for session setup: %w", err)
	}
	if repoPath == "" {
		repoPath = ws.Definition.Path
	}
	if launchDir, launchArgs, ok := resolveClaudeNativeWorktreeLaunch(ws.Definition.Path, workDir); ok {
		if cm := sess.ClaudeManager(); cm != nil {
			cm.SetWorkDir(launchDir)
			cm.SetLaunchArgs(launchArgs)
			logger = logger.With().Str("launch_dir", launchDir).Strs("launch_args", launchArgs).Logger()
		}
	}
	go a.manager.WatchForNewSessionFile(ctx, workspaceID, sessionID, repoPath)

	// Small delay for session initialization
	time.Sleep(200 * time.Millisecond)

	// Use the documented bypassPermissions mode for autonomous agent execution.
	// When launchArgs already include --dangerously-skip-permissions, the Claude manager
	// suppresses a redundant --permission-mode bypassPermissions flag.
	permissionMode := "bypassPermissions"
	if err := a.manager.SendPrompt(sessionID, prompt, "new", permissionMode, false); err != nil {
		// Try to clean up the session on failure
		_ = a.manager.StopSession(sessionID)
		return "", fmt.Errorf("failed to send prompt: %w", err)
	}

	logger.Info().Str("session_id", sessionID).Msg("agent prompt sent")
	return sessionID, nil
}

func resolveClaudeNativeWorktreeLaunch(workspaceRoot, projectPath string) (string, []string, bool) {
	workspaceRoot = filepath.Clean(workspaceRoot)
	projectPath = filepath.Clean(projectPath)
	if workspaceRoot == "" || projectPath == "" {
		return "", nil, false
	}

	worktreesRoot := filepath.Join(workspaceRoot, ".claude", "worktrees")
	relPath, err := filepath.Rel(worktreesRoot, projectPath)
	if err != nil || relPath == "." || relPath == "" {
		return "", nil, false
	}

	if strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || relPath == ".." {
		return "", nil, false
	}
	if strings.Contains(relPath, string(filepath.Separator)) {
		return "", nil, false
	}

	return workspaceRoot, []string{"-w", relPath, "--dangerously-skip-permissions"}, true
}

// WaitForCompletion polls the session until the Claude process finishes.
// Returns the final Claude state: "idle" (success), "error", "stopped" (cancelled).
// Polls every 3 seconds. Returns immediately if session is no longer found.
func (a *SessionStarterAdapter) WaitForCompletion(ctx context.Context, sessionID string) (string, error) {
	baseLogger := log.With().Str("requested_session_id", sessionID).Logger()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "timeout", ctx.Err()
		case <-ticker.C:
			sess, err := a.manager.GetSession(sessionID)
			if err != nil {
				// Session no longer exists — it was removed after completion
				baseLogger.Info().Msg("session no longer found, assuming completed")
				return "idle", nil
			}

			logger := baseLogger.With().Str("session_id", sess.GetID()).Logger()

			// Check session-level status
			status := sess.GetStatus()
			if status == session.StatusStopped || status == session.StatusError {
				finalState := "idle"
				if status == session.StatusError {
					finalState = "error"
				}
				logger.Info().Str("status", string(status)).Msg("session stopped")
				return finalState, nil
			}

			// Check Claude process status
			cm := sess.ClaudeManager()
			if cm == nil {
				logger.Info().Msg("no Claude manager, assuming completed")
				return "idle", nil
			}

			if !cm.IsRunning() {
				state := cm.State()
				finalState := string(state) // "idle", "error", "stopped"
				logger.Info().Str("claude_state", finalState).Msg("Claude process finished")
				return finalState, nil
			}

			// Still running, continue polling
			logger.Debug().Msg("session still running, waiting...")
		}
	}
}
