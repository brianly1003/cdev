// Package agent orchestrates autonomous task execution by spawning AI coding
// agents in isolated git worktrees and extracting structured results.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/taskstore"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/rs/zerolog/log"
)

// SessionStarter is the interface for spawning and monitoring Claude Code sessions.
// This decouples the spawner from the session manager implementation.
type SessionStarter interface {
	// StartSessionWithPrompt spawns a Claude Code session with a prompt in the given workspace.
	// workDir overrides the working directory (e.g., worktree path). Empty string uses workspace default.
	// Returns the session ID.
	StartSessionWithPrompt(ctx context.Context, workspaceID string, prompt string, agentType string, workDir string) (string, error)

	// WaitForCompletion blocks until the Claude session finishes (process exits).
	// Returns the final Claude state ("idle" = success, "error", "stopped" = cancelled)
	// and any error if the wait itself failed (e.g., context cancelled).
	WaitForCompletion(ctx context.Context, sessionID string) (finalState string, err error)
}

type sessionIDResolver interface {
	ResolveSessionID(sessionID string) string
}

type sessionStopper interface {
	StopSession(sessionID string) error
}

// WorkspaceLookup resolves a logical workspace ID to the configured repo root.
type WorkspaceLookup interface {
	GetWorkspace(workspaceID string) (*workspace.Workspace, error)
}

// Spawner orchestrates task execution: task → worktree → agent session → result.
type Spawner struct {
	store           *taskstore.Store
	sessionStarter  SessionStarter
	workspaceLookup WorkspaceLookup
	eventHub        interface{ Publish(events.Event) }
	mu              sync.Mutex
	activeTasks     map[string]context.CancelFunc // taskID → cancel
}

// NewSpawner creates a new task spawner.
func NewSpawner(store *taskstore.Store, sessionStarter SessionStarter, workspaceLookup WorkspaceLookup, eventHub interface{ Publish(events.Event) }) *Spawner {
	return &Spawner{
		store:           store,
		sessionStarter:  sessionStarter,
		workspaceLookup: workspaceLookup,
		eventHub:        eventHub,
		activeTasks:     make(map[string]context.CancelFunc),
	}
}

// SpawnTask starts executing a task asynchronously.
// It transitions the task to running, creates a worktree, spawns the agent,
// and monitors execution until completion or timeout.
func (s *Spawner) SpawnTask(ctx context.Context, taskID string) error {
	s.mu.Lock()
	if _, exists := s.activeTasks[taskID]; exists {
		s.mu.Unlock()
		return fmt.Errorf("task %s is already running", taskID)
	}

	t, err := s.store.GetByID(taskID)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("task not found: %w", err)
	}

	// Create cancellable context with timeout
	timeout := 30 * time.Minute
	if t.Policy != nil && t.Policy.MaxDurationMins > 0 {
		timeout = time.Duration(t.Policy.MaxDurationMins) * time.Minute
	}
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	s.activeTasks[taskID] = cancel
	s.mu.Unlock()

	// Route to plan_case-specific executor if applicable
	if t.IsPlanCase() {
		go s.executePlanCase(taskCtx, t, cancel)
		return nil
	}

	// Run in background
	go s.executeTask(taskCtx, t, cancel)
	return nil
}

// CancelTask cancels a running task.
func (s *Spawner) CancelTask(taskID string) error {
	s.mu.Lock()
	cancel, exists := s.activeTasks[taskID]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("task %s is not running", taskID)
	}

	cancel()
	return nil
}

// ActiveTaskCount returns the number of currently running tasks.
func (s *Spawner) ActiveTaskCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.activeTasks)
}

func (s *Spawner) executeTask(ctx context.Context, t *task.AgentTask, cancel context.CancelFunc) {
	defer func() {
		s.mu.Lock()
		delete(s.activeTasks, t.ID)
		s.mu.Unlock()
		cancel()
	}()

	logger := log.With().Str("task_id", t.ID).Str("title", t.Title).Logger()
	logger.Info().Msg("starting task execution")

	// 1. Transition to running
	if err := t.Transition(task.StatusRunning); err != nil {
		logger.Error().Err(err).Msg("failed to transition to running")
		return
	}
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist running state")
	}
	s.emitEvent(events.EventTypeTaskStarted, t)

	// 2. Create git worktree
	worktreePath, branchName, err := s.createWorktree(t)
	if err != nil {
		logger.Error().Err(err).Msg("failed to create worktree")
		s.failTask(t, "Failed to create worktree: "+err.Error())
		return
	}
	t.WorktreePath = worktreePath
	t.BranchName = branchName
	t.AddTimelineEvent("worktree", fmt.Sprintf("Worktree created: %s (branch: %s)", worktreePath, branchName), "system")
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist worktree state")
	}

	// 3. Write .cdev/task.json for Claude to discover callback context
	if err := s.writeTaskContext(t); err != nil {
		logger.Warn().Err(err).Msg("failed to write .cdev/task.json (non-fatal)")
	}

	// 4. Build prompt from task
	prompt := s.buildPrompt(t)
	logger.Info().Int("prompt_len", len(prompt)).Msg("built agent prompt")

	// 5. Spawn agent session
	agentType := "claude"
	if t.Policy != nil && t.Policy.AgentType != "" {
		agentType = t.Policy.AgentType
	}

	sessionID, err := s.sessionStarter.StartSessionWithPrompt(ctx, t.WorkspaceID, prompt, agentType, t.WorktreePath)
	if err != nil {
		logger.Error().Err(err).Msg("failed to start agent session")
		s.failTask(t, "Failed to start agent session: "+err.Error())
		s.cleanupWorktree(worktreePath)
		return
	}

	t.SessionID = sessionID
	t.AddTimelineEvent("session_started", fmt.Sprintf("Agent session started: %s", sessionID), "system")
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist session started state")
	}

	logger.Info().Str("session_id", sessionID).Msg("agent session started")

	// 6. Wait for session completion (polls Claude process state)
	finalState, waitErr := s.sessionStarter.WaitForCompletion(ctx, sessionID)
	logger.Info().Str("final_state", finalState).Err(waitErr).Msg("agent session completed")
	s.persistResolvedSessionID(t)

	if waitErr != nil && ctx.Err() == context.DeadlineExceeded {
		logger.Warn().Msg("task timed out")
		s.failTask(t, fmt.Sprintf("Task timed out after %d minutes", t.Policy.MaxDurationMins))
		return
	}

	t.AddTimelineEvent("session_completed", fmt.Sprintf("Agent session finished: state=%s", finalState), "system")
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist session completed state")
	}

	// 7. Extract result from worktree
	result := s.extractAndBuildResult(worktreePath, finalState)
	t.Result = result

	// 8. Transition based on result
	if finalState == "error" || finalState == "stopped" {
		s.failTask(t, fmt.Sprintf("Agent session ended with state: %s", finalState))
		return
	}

	if err := t.Transition(task.StatusValidating); err != nil {
		logger.Error().Err(err).Msg("failed to transition to validating")
		return
	}
	t.AddTimelineEvent("validating", "Validating agent results", "system")
	if err := s.store.Update(t); err != nil {
		logger.Error().Err(err).Msg("failed to persist validating state")
	}

	// Auto-transition to awaiting_approval if result looks good
	if result.TestsPassed && result.BuildPassed {
		if err := t.Transition(task.StatusAwaitingApproval); err == nil {
			t.AddTimelineEvent("awaiting_approval", "Tests and build passed, awaiting human approval", "system")
			if err := s.store.Update(t); err != nil {
				logger.Error().Err(err).Msg("failed to persist awaiting_approval state")
			}
			s.emitEvent(events.EventTypeTaskCompleted, t)
		}
	} else {
		// Tests or build failed — needs human review
		if err := t.Transition(task.StatusAwaitingApproval); err == nil {
			t.AddTimelineEvent("awaiting_approval", fmt.Sprintf("Agent finished (tests=%v, build=%v), awaiting human review", result.TestsPassed, result.BuildPassed), "system")
			if err := s.store.Update(t); err != nil {
				logger.Error().Err(err).Msg("failed to persist awaiting_approval state")
			}
			s.emitEvent(events.EventTypeTaskProgress, t)
		}
	}
}

func (s *Spawner) failTask(t *task.AgentTask, reason string) {
	s.persistResolvedSessionID(t)
	if err := t.Transition(task.StatusFailed); err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("failed to transition to failed state")
		return
	}
	t.Result = &task.Result{
		VerdictStatus:  "failed",
		VerdictSummary: reason,
	}
	t.AddTimelineEvent("failed", reason, "system")
	if err := s.store.Update(t); err != nil {
		log.Error().Err(err).Str("task_id", t.ID).Msg("failed to persist failed state")
	}
	s.emitEvent(events.EventTypeTaskFailed, t)
}

func (s *Spawner) emitEvent(eventType events.EventType, t *task.AgentTask) {
	if s.eventHub == nil {
		return
	}
	s.eventHub.Publish(events.NewTaskEvent(eventType, t.WorkspaceID, events.TaskEventPayload{
		TaskID:    t.ID,
		TaskType:  string(t.TaskType),
		Title:     t.Title,
		Status:    string(t.Status),
		SessionID: t.SessionID,
	}))
}

func (s *Spawner) persistResolvedSessionID(t *task.AgentTask) {
	if t == nil || strings.TrimSpace(t.SessionID) == "" {
		return
	}

	resolver, ok := s.sessionStarter.(sessionIDResolver)
	if !ok {
		return
	}

	resolvedID := strings.TrimSpace(resolver.ResolveSessionID(t.SessionID))
	if resolvedID == "" || resolvedID == t.SessionID {
		return
	}

	previousID := t.SessionID
	t.SessionID = resolvedID
	t.AddTimelineEvent("session_id_resolved", fmt.Sprintf("Resolved session ID: %s", resolvedID), "system")
	if err := s.store.Update(t); err != nil {
		log.Warn().
			Err(err).
			Str("task_id", t.ID).
			Str("temporary_session_id", previousID).
			Str("real_session_id", resolvedID).
			Msg("failed to persist resolved session ID for task")
		return
	}

	log.Info().
		Str("task_id", t.ID).
		Str("temporary_session_id", previousID).
		Str("real_session_id", resolvedID).
		Msg("persisted resolved session ID for task")
}

// taskContextFile is the worktree-scoped context file that Claude reads for callback credentials.
// See cdev_agent_protocol.md §7 for the full schema.
type taskContextFile struct {
	Version      int                `json:"version"`
	CdevTaskID   string             `json:"cdev_task_id"`
	Origin       *taskContextOrigin `json:"origin,omitempty"`
	WorkspaceID  string             `json:"workspace_id"`
	TaskYAMLPath string             `json:"task_yaml_path,omitempty"`
	Branch       string             `json:"branch"`
	AutonomyMode string             `json:"autonomy_mode"`
	DispatchedAt string             `json:"dispatched_at"`
}

type taskContextOrigin struct {
	System string `json:"system"`
	TaskID int    `json:"task_id,omitempty"`
	CaseID *int   `json:"case_id,omitempty"`
	URL    string `json:"url,omitempty"`
	APIKey string `json:"api_key,omitempty"`
}

// writeTaskContext writes .cdev/task.json to the worktree so Claude can discover callback context.
func (s *Spawner) writeTaskContext(t *task.AgentTask) error {
	if t.WorktreePath == "" {
		return fmt.Errorf("worktree path not set")
	}

	autonomy := "supervised"
	if t.Policy != nil && t.Policy.Autonomy != "" {
		autonomy = t.Policy.Autonomy
	}

	ctx := &taskContextFile{
		Version:      1,
		CdevTaskID:   t.ID,
		WorkspaceID:  t.WorkspaceID,
		Branch:       t.BranchName,
		AutonomyMode: autonomy,
		DispatchedAt: time.Now().UTC().Format(time.RFC3339),
	}

	// Map origin from task
	if t.Origin != nil {
		ctx.Origin = &taskContextOrigin{
			System: t.Origin.System,
			TaskID: t.Origin.TaskID,
			CaseID: t.Origin.CaseID,
			URL:    t.Origin.URL,
			APIKey: t.Origin.APIKey,
		}
	}

	// Create .cdev directory
	cdevDir := filepath.Join(t.WorktreePath, ".cdev")
	if err := os.MkdirAll(cdevDir, 0755); err != nil {
		return fmt.Errorf("failed to create .cdev directory: %w", err)
	}

	// Marshal and write
	data, err := json.MarshalIndent(ctx, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task context: %w", err)
	}

	taskJSONPath := filepath.Join(cdevDir, "task.json")
	if err := os.WriteFile(taskJSONPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write task.json: %w", err)
	}

	log.Info().Str("path", taskJSONPath).Msg("wrote .cdev/task.json")
	return nil
}

// buildPrompt constructs a Universal Task Prompt (UTP) from the task definition.
// UTP is agent-agnostic: the same prompt works with Claude Code, Codex Desktop,
// Gemini, or any future AI coding agent that can read files and follow instructions.
func (s *Spawner) buildPrompt(t *task.AgentTask) string {
	// If task YAML is provided, build UTP with explicit skill file reference
	if t.TaskYAML != "" {
		return s.buildUTPFromYAML(t)
	}
	// Build UTP from structured task fields
	return s.buildUTPFromFields(t)
}

// buildUTPFromYAML generates a Universal Task Prompt when the task carries raw YAML.
func (s *Spawner) buildUTPFromYAML(t *task.AgentTask) string {
	var sb strings.Builder

	sb.WriteString("Read CLAUDE.md for project conventions.\n\n")

	// Try to resolve skill file from TaskType (which may be set alongside TaskYAML)
	if skillFile := resolveSkillFile(t.TaskType); skillFile != "" {
		sb.WriteString(fmt.Sprintf("Execute the portable skill defined in %s\n", skillFile))
		sb.WriteString("Read the full skill file and follow its workflow exactly.\n\n")
	} else {
		sb.WriteString("Follow the appropriate skill workflow from docs/skills/.\n\n")
	}

	sb.WriteString(fmt.Sprintf("## Task Definition\n\n```yaml\n%s\n```\n\n", t.TaskYAML))
	sb.WriteString("After completion, write structured results to .cdev/task-result.json\n")

	return sb.String()
}

// buildUTPFromFields generates a Universal Task Prompt from the task's structured fields.
func (s *Spawner) buildUTPFromFields(t *task.AgentTask) string {
	var sb strings.Builder

	sb.WriteString("Read CLAUDE.md for project conventions.\n\n")

	// Resolve skill file — every task type maps to a portable skill doc
	skillFile := resolveSkillFile(t.TaskType)
	if skillFile != "" {
		sb.WriteString(fmt.Sprintf("Execute the portable skill defined in %s\n", skillFile))
		sb.WriteString("Read the full skill file and follow its workflow exactly.\n\n")
	}

	// Build inline task definition YAML
	sb.WriteString("## Task Definition\n\n```yaml\n")
	sb.WriteString(fmt.Sprintf("type: %s\n", t.TaskType))
	sb.WriteString(fmt.Sprintf("title: \"%s\"\n", t.Title))
	if t.Description != "" {
		sb.WriteString("description: |\n")
		for _, line := range strings.Split(t.Description, "\n") {
			sb.WriteString(fmt.Sprintf("  %s\n", line))
		}
	}
	sb.WriteString(fmt.Sprintf("severity: %s\n", t.Severity))

	// Anchors section
	if t.Anchors != nil && (len(t.Anchors.Files) > 0 || len(t.Anchors.Methods) > 0) {
		sb.WriteString("anchors:\n")
		if len(t.Anchors.Files) > 0 {
			sb.WriteString("  files:\n")
			for _, f := range t.Anchors.Files {
				sb.WriteString(fmt.Sprintf("    - %s\n", f))
			}
		}
		if len(t.Anchors.Methods) > 0 {
			sb.WriteString("  methods:\n")
			for _, m := range t.Anchors.Methods {
				sb.WriteString(fmt.Sprintf("    - %s\n", m))
			}
		}
	}

	// Constraints from policy
	sb.WriteString("constraints:\n")
	maxFiles := 10
	maxRounds := 3
	mustTests := true
	mustBuild := true
	if t.Policy != nil {
		if t.Policy.MaxFilesChanged > 0 {
			maxFiles = t.Policy.MaxFilesChanged
		}
		if t.Policy.MaxRounds > 0 {
			maxRounds = t.Policy.MaxRounds
		}
		mustTests = t.Policy.MustPassTests
		mustBuild = t.Policy.MustPassBuild
	}
	sb.WriteString(fmt.Sprintf("  max_files_changed: %d\n", maxFiles))
	sb.WriteString(fmt.Sprintf("  must_pass_tests: %v\n", mustTests))
	sb.WriteString(fmt.Sprintf("  must_pass_build: %v\n", mustBuild))
	sb.WriteString(fmt.Sprintf("  max_rounds: %d\n", maxRounds))
	sb.WriteString("```\n\n")

	// Input mapping for skills that expect description_or_file
	if t.TaskType == task.TaskTypeFixIssue || t.TaskType == task.TaskTypeImplementCR || t.TaskType == task.TaskTypeFixReplay {
		sb.WriteString("## Input Mapping\n")
		sb.WriteString("- description_or_file: (the description above)\n\n")
	}

	sb.WriteString("After completion, write structured results to .cdev/task-result.json\n")

	return sb.String()
}

// resolveSkillFile maps a TaskType to its portable skill document path.
func resolveSkillFile(taskType task.TaskType) string {
	switch taskType {
	case task.TaskTypeFixIssue:
		return "docs/skills/fix-issue.md"
	case task.TaskTypeFixReplay:
		return "docs/skills/analyze-replay.md"
	case task.TaskTypeImplementCR:
		return "docs/skills/implement-change-request.md"
	case task.TaskTypeAutoFix:
		return "docs/skills/auto-fix.md"
	case task.TaskTypeAddTest:
		return "docs/skills/fix-issue.md"
	case task.TaskTypeRefactor:
		return "docs/skills/fix-issue.md"
	case task.TaskTypePlanCase:
		return "docs/skills/plan-case.md"
	default:
		return ""
	}
}

// extractAndBuildResult tries multiple strategies to build a task result:
// 1. Structured result file (.cdev/task-result.json) — written by skills
// 2. Git diff analysis — file changes from the worktree
// 3. Fallback — minimal result based on final Claude state
func (s *Spawner) extractAndBuildResult(worktreePath string, finalState string) *task.Result {
	logger := log.With().Str("worktree", worktreePath).Logger()

	// Strategy 1: Read structured result file
	resultPath := filepath.Join(worktreePath, ".cdev", "task-result.json")
	if data, err := os.ReadFile(resultPath); err == nil {
		var rf TaskResultFile
		if err := json.Unmarshal(data, &rf); err == nil {
			result := MapResultFileToResult(&rf)
			logger.Info().Str("status", result.VerdictStatus).Msg("extracted structured result from task-result.json")

			// Enrich with git diff if files_changed is empty
			if len(result.FilesChanged) == 0 {
				if files, diffContent, err := ParseResultFromGitDiff(worktreePath); err == nil {
					result.FilesChanged = files
					result.DiffContent = diffContent
				}
			}
			return result
		}
		logger.Warn().Err(err).Msg("failed to parse task-result.json")
	}

	// Strategy 2: Build result from git diff
	files, diffContent, err := ParseResultFromGitDiff(worktreePath)
	if err == nil && len(files) > 0 {
		logger.Info().Int("files_changed", len(files)).Msg("built result from git diff")
		return &task.Result{
			VerdictStatus:  verdictFromState(finalState),
			VerdictSummary: fmt.Sprintf("Agent completed with %d files changed", len(files)),
			FilesChanged:   files,
			DiffContent:    diffContent,
		}
	}

	// Strategy 3: Minimal fallback result
	logger.Info().Msg("no structured result or git changes found, using fallback")
	return &task.Result{
		VerdictStatus:  verdictFromState(finalState),
		VerdictSummary: fmt.Sprintf("Agent session completed with state: %s", finalState),
	}
}

// verdictFromState maps Claude's final state to a task verdict.
func verdictFromState(state string) string {
	switch state {
	case "idle":
		return "converged"
	case "error":
		return "failed"
	case "stopped":
		return "cancelled"
	default:
		return "unknown"
	}
}
