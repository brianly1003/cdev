// Package agent provides the plan executor for read-only
// plan_case tasks. These tasks spawn Claude in a detached worktree,
// enforce a bounded timeout, and fire callbacks to LazyAdmin on completion.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/rs/zerolog/log"
)

// executePlanCase runs a plan_case task:
// 1. Create detached worktree (read-only, no branch)
// 2. Write .cdev/case-context.json + .cdev/task.json
// 3. Spawn Claude with /lazy:plan-case prompt
// 4. On timeout → stop the session, preserve the worktree, fire planning_timeout callback
// 5. On completion → fire Analyzed callback with output YAMLs
// 6. Clean up the worktree on successful completion
func (s *Spawner) executePlanCase(ctx context.Context, t *task.AgentTask, cancel context.CancelFunc) {
	defer func() {
		s.mu.Lock()
		delete(s.activeTasks, t.ID)
		s.mu.Unlock()
		cancel()
	}()

	logger := log.With().Str("task_id", t.ID).Str("trigger", "plan_case").Logger()
	logger.Info().Msg("starting plan case task")

	// 1. Transition to running
	if err := t.Transition(task.StatusRunning); err != nil {
		logger.Error().Err(err).Msg("failed to transition to running")
		return
	}
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist task state after transition to running")
	}
	s.emitEvent(events.EventTypeTaskStarted, t)

	// 2. Create detached worktree (IsPlanCase() triggers --detach in createWorktree)
	worktreePath, _, err := s.createWorktree(t)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create worktree")
		s.failTask(t, "Failed to create worktree: "+err.Error())
		return
	}
	t.WorktreePath = worktreePath
	t.AddTimelineEvent("worktree", fmt.Sprintf("Detached worktree created: %s", worktreePath), "system")
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist task state after worktree creation")
	}

	cleanupWorktree := true
	defer func() {
		if !cleanupWorktree {
			logger.Warn().Str("worktree", worktreePath).Msg("preserving plan case worktree after timeout")
			return
		}
		s.cleanupWorktree(worktreePath)
	}()

	// 3. Write .cdev/task.json
	if err := s.writeTaskContext(t); err != nil {
		logger.Warn().Err(err).Msg("failed to write .cdev/task.json (non-fatal)")
	}

	// 4. Write .cdev/case-context.json
	if err := writeCaseContext(worktreePath, t.CaseContext); err != nil {
		logger.Error().Err(err).Msg("failed to write case-context.json")
		s.failTask(t, "Failed to write case context: "+err.Error())
		return
	}

	// 5. Build plan prompt and spawn agent
	prompt := buildPlanCasePrompt(t)
	logger.Info().Int("prompt_len", len(prompt)).Msg("built plan case prompt")

	sessionID, err := s.sessionStarter.StartSessionWithPrompt(ctx, t.WorkspaceID, prompt, "claude", t.WorktreePath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to start agent session")
		s.failTask(t, "Failed to start agent session: "+err.Error())
		return
	}

	t.SessionID = sessionID
	t.AddTimelineEvent("session_started", fmt.Sprintf("Plan case session started: %s", sessionID), "system")
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist task state after session start")
	}

	// 6. Wait for completion or timeout
	finalState, waitErr := s.sessionStarter.WaitForCompletion(ctx, sessionID)
	logger.Info().Str("final_state", finalState).Err(waitErr).Msg("plan case session completed")
	s.persistResolvedSessionID(t)

	// 7. Handle timeout
	if waitErr != nil && ctx.Err() == context.DeadlineExceeded {
		timeoutMins := 30
		if t.Policy != nil && t.Policy.MaxDurationMins > 0 {
			timeoutMins = t.Policy.MaxDurationMins
		}

		if stopper, ok := s.sessionStarter.(sessionStopper); ok && strings.TrimSpace(t.SessionID) != "" {
			if err := stopper.StopSession(t.SessionID); err != nil {
				logger.Warn().Err(err).Str("session_id", t.SessionID).Msg("failed to stop timed out plan case session")
			}
		}

		cleanupWorktree = false
		t.AddTimelineEvent("worktree_retained",
			fmt.Sprintf("Timed out plan case worktree preserved for inspection: %s", worktreePath),
			"system")
		if err := s.store.Update(t); err != nil {
			logger.Error().Err(err).Msg("failed to persist task state after timeout")
		}

		logger.Warn().Int("timeout_minutes", timeoutMins).Msg("plan case timed out")
		s.failTask(t, fmt.Sprintf("Planning timed out after %d minutes", timeoutMins))
		fireCallback(t, "PlanningTimeout", nil)
		return
	}

	// 8. Handle completion — read output task YAML contents before worktree cleanup
	taskYAMLContents := readOutputTaskYAMLs(worktreePath)
	t.AddTimelineEvent("plan_complete",
		fmt.Sprintf("Plan case finished: state=%s, output_yamls=%d", finalState, len(taskYAMLContents)),
		"system")

	if finalState == "error" || finalState == "stopped" {
		s.failTask(t, fmt.Sprintf("Plan case session ended with state: %s", finalState))
		fireCallback(t, "PlanningTimeout", nil)
		return
	}

	// Transition to completed (plan cases skip approval)
	if err := t.Transition(task.StatusValidating); err != nil {
		logger.Error().Err(err).Msg("failed to transition to validating")
		s.failTask(t, "Failed to transition to validating: "+err.Error())
		return
	}
	if err := t.Transition(task.StatusCompleted); err != nil {
		logger.Error().Err(err).Msg("failed to transition to completed")
		s.failTask(t, "Failed to transition to completed: "+err.Error())
		return
	}
	t.Result = &task.Result{
		VerdictStatus:  "converged",
		VerdictSummary: fmt.Sprintf("Plan case completed with %d output task YAMLs", len(taskYAMLContents)),
	}
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist task state after completion")
	}
	s.emitEvent(events.EventTypeTaskCompleted, t)

	// 9. Fire Analyzed callback to LazyAdmin with YAML contents
	fireCallback(t, "Analyzed", taskYAMLContents)
}

// writeCaseContext writes .cdev/case-context.json inside the worktree.
func writeCaseContext(worktreePath string, caseContext json.RawMessage) error {
	if len(caseContext) == 0 {
		return fmt.Errorf("empty case context")
	}

	cdevDir := filepath.Join(worktreePath, ".cdev")
	if err := os.MkdirAll(cdevDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cdev directory: %w", err)
	}

	// Pretty-print the JSON for readability
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, caseContext, "", "  "); err != nil {
		// Fallback to raw if formatting fails
		formatted.Reset()
		formatted.Write(caseContext)
	}

	path := filepath.Join(cdevDir, "case-context.json")
	if err := os.WriteFile(path, formatted.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write case-context.json: %w", err)
	}

	log.Info().Str("path", path).Msg("wrote .cdev/case-context.json")
	return nil
}

// buildPlanCasePrompt constructs the prompt for plan_case tasks.
// The prompt MUST be provided by the dispatcher (LazyAdmin) via the webhook payload.
// cdev is a generic platform — it does not hardcode project-specific prompts.
func buildPlanCasePrompt(t *task.AgentTask) string {
	prompt := t.Prompt
	if prompt == "" {
		prompt = "No prompt provided by dispatcher. Please check the webhook payload."
	}

	if t.Description != "" {
		return prompt + fmt.Sprintf("\n\n## Context\n\n%s\n", t.Description)
	}
	return prompt
}

// readOutputTaskYAMLs reads task YAML file contents from the worktree output directory.
// Returns file contents (not paths) so they survive worktree cleanup.
func readOutputTaskYAMLs(worktreePath string) []string {
	outputDir := filepath.Join(worktreePath, ".cdev", "output-tasks")
	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return nil
	}

	var contents []string
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml")) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outputDir, e.Name()))
		if err != nil {
			log.Warn().Err(err).Str("file", e.Name()).Msg("failed to read output task YAML")
			continue
		}
		contents = append(contents, string(data))
	}
	return contents
}

// callbackPayload is the JSON body sent to LazyAdmin status callbacks.
type callbackPayload struct {
	Status    string   `json:"status"`
	TaskYAMLs []string `json:"task_yamls,omitempty"` // YAML file contents
}

// fireCallback sends a status update to the LazyAdmin origin URL.
// PUT {origin.url}/api/ai-cases/{case_id}/status
func fireCallback(t *task.AgentTask, status string, taskYAMLContents []string) {
	if t.Origin == nil || t.Origin.URL == "" || t.Origin.CaseID == nil {
		log.Debug().Str("task_id", t.ID).Msg("no origin callback configured, skipping")
		return
	}

	// Validate callback URL to prevent SSRF
	callbackURL := fmt.Sprintf("%s/api/ai-cases/%d/status", strings.TrimRight(t.Origin.URL, "/"), *t.Origin.CaseID)
	if err := validateCallbackURLForOrigin(t.Origin.System, callbackURL); err != nil {
		log.Error().Err(err).Str("url", callbackURL).Str("task_id", t.ID).Msg("callback URL rejected")
		return
	}

	payload := callbackPayload{
		Status:    status,
		TaskYAMLs: taskYAMLContents,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("failed to marshal callback payload")
		return
	}

	req, err := http.NewRequest(http.MethodPut, callbackURL, bytes.NewReader(body))
	if err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("failed to create callback request")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if t.Origin.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.Origin.APIKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().Err(err).Str("url", callbackURL).Str("task_id", t.ID).Msg("callback request failed")
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	log.Info().
		Str("task_id", t.ID).
		Str("status", status).
		Int("http_status", resp.StatusCode).
		Str("url", callbackURL).
		Msg("fired plan case callback")
}

// validateCallbackURL rejects URLs targeting internal/private networks to prevent SSRF.
// Set CDEV_ALLOW_LOCAL_CALLBACKS=1 (or legacy alias CDEV_ALLOW_LOCAL_CALLBACK=1)
// to allow localhost callbacks during development.
func validateCallbackURL(rawURL string) error {
	return validateCallbackURLForOrigin("", rawURL)
}

// validateCallbackURLForOrigin applies the same SSRF protections as validateCallbackURL,
// but allows loopback callbacks for local LazyAdmin development without requiring env vars.
func validateCallbackURLForOrigin(originSystem, rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	// Must be HTTPS (or HTTP for local dev)
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("unsupported scheme: %s", parsed.Scheme)
	}

	// In dev mode, allow localhost/private network callbacks.
	if allowLocalCallbacksEnabled() {
		return nil
	}

	host := strings.ToLower(parsed.Hostname())

	// Block localhost and loopback
	if isLoopbackHost(host) {
		if strings.EqualFold(originSystem, "lazyadmin") {
			return nil
		}
		return fmt.Errorf("callback to %s is not allowed (set CDEV_ALLOW_LOCAL_CALLBACKS=1 or CDEV_ALLOW_LOCAL_CALLBACK=1 for dev)", host)
	}

	// Block common cloud metadata endpoints
	if host == "169.254.169.254" || host == "metadata.google.internal" {
		return fmt.Errorf("callback to metadata endpoint %s is not allowed", host)
	}

	// Block private IP ranges (10.x, 172.16-31.x, 192.168.x)
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
		return fmt.Errorf("callback to private IP %s is not allowed", host)
	}
	if strings.HasPrefix(host, "172.") {
		// 172.16.0.0 - 172.31.255.255
		parts := strings.SplitN(host, ".", 3)
		if len(parts) >= 2 {
			var second int
			if _, err := fmt.Sscanf(parts[1], "%d", &second); err == nil && second >= 16 && second <= 31 {
				return fmt.Errorf("callback to private IP %s is not allowed", host)
			}
		}
	}

	return nil
}

func allowLocalCallbacksEnabled() bool {
	return os.Getenv("CDEV_ALLOW_LOCAL_CALLBACKS") == "1" || os.Getenv("CDEV_ALLOW_LOCAL_CALLBACK") == "1"
}

func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1", "[::1]":
		return true
	default:
		return false
	}
}
