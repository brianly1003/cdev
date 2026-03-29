package http

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/taskstore"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// TaskSpawner is the minimal interface for spawning agent tasks.
type TaskSpawner interface {
	SpawnTask(ctx context.Context, taskID string) error
}

// TaskHandler handles agent task HTTP endpoints.
type TaskHandler struct {
	store             *taskstore.Store
	webhookSecret     string
	eventHub          interface{ Publish(events.Event) }
	spawner           TaskSpawner
	workspaceResolver WorkspaceResolver
}

// NewTaskHandler creates a new TaskHandler.
func NewTaskHandler(store *taskstore.Store, webhookSecret string, eventHub interface{ Publish(events.Event) }) *TaskHandler {
	return &TaskHandler{
		store:         store,
		webhookSecret: webhookSecret,
		eventHub:      eventHub,
	}
}

// SetSpawner sets the task spawner for auto-spawning tasks on webhook creation.
func (h *TaskHandler) SetSpawner(spawner TaskSpawner) {
	h.spawner = spawner
}

// SetWorkspaceResolver sets the workspace resolver for mapping names/paths to workspace IDs.
func (h *TaskHandler) SetWorkspaceResolver(resolver WorkspaceResolver) {
	h.workspaceResolver = resolver
}

// RegisterRoutes registers task-related HTTP routes on the given mux.
func (h *TaskHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/tasks/webhook", h.handleWebhook)
	mux.HandleFunc("/api/tasks", h.handleTasks)
	mux.HandleFunc("/api/tasks/", h.handleTaskByID)
	mux.HandleFunc("/api/tasks/stats", h.handleTaskStats)
}

// --- Webhook Endpoint ---

// WebhookPayload is the expected payload from LazyAdmin CdevWebhookService.
type WebhookPayload struct {
	Trigger     WebhookTrigger  `json:"trigger"`
	WorkspaceID string          `json:"workspace_id"`
	TaskType    string          `json:"task_type"`
	Title       string          `json:"title"`
	Description string          `json:"description,omitempty"`
	Severity    string          `json:"severity,omitempty"`
	Labels      []string        `json:"labels,omitempty"`
	CaseID      *int            `json:"case_id,omitempty"`
	TaskYAML    string          `json:"task_yaml,omitempty"`
	Anchors     *task.Anchors   `json:"anchors,omitempty"`
	Constraints *WebhookConstraints `json:"constraints,omitempty"`
	Origin      *WebhookOrigin      `json:"origin,omitempty"`
	CaseContext json.RawMessage `json:"case_context,omitempty"`
	Prompt      string          `json:"prompt,omitempty"`
}

// WebhookOrigin carries callback credentials so the agent can report status back to the source system.
type WebhookOrigin struct {
	System string `json:"system"`              // "lazyadmin", "github", "manual"
	TaskID int    `json:"task_id,omitempty"`    // External task ID in the origin system
	CaseID *int   `json:"case_id,omitempty"`    // External case ID if applicable
	URL    string `json:"url,omitempty"`        // Base URL for status callbacks
	APIKey string `json:"api_key,omitempty"`    // API key for callbacks
}

// WebhookTrigger describes what initiated the webhook.
type WebhookTrigger struct {
	Type      string `json:"type"`       // "task_created", "regression"
	Ref       string `json:"ref"`        // External reference ID
	Hash      string `json:"hash"`       // Content hash for idempotency
	Timestamp string `json:"timestamp"`
}

// WebhookConstraints maps from LazyAdmin's constraint format.
type WebhookConstraints struct {
	MaxFilesChanged int      `json:"max_files_changed"`
	MustPassTests   bool     `json:"must_pass_tests"`
	Autonomy        string   `json:"autonomy"`
	RequireApproval []string `json:"require_approval"`
}

// handleWebhook handles POST /api/tasks/webhook — webhook ingestion with HMAC validation.
func (h *TaskHandler) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read body"})
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Validate HMAC signature if secret is configured
	if h.webhookSecret != "" {
		signature := r.Header.Get("X-Webhook-Signature")
		if signature == "" {
			log.Warn().Msg("webhook: missing X-Webhook-Signature header")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing signature"})
			return
		}

		if !h.validateHMAC(body, signature) {
			log.Warn().Msg("webhook: invalid HMAC signature")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
			return
		}
	}

	// Parse payload
	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	// Handle plan_case trigger: override task_type and require case_context
	isInvestigation := payload.Trigger.Type == "plan_case"
	if isInvestigation {
		payload.TaskType = string(task.TaskTypePlanCase)
		if len(payload.CaseContext) == 0 || !json.Valid(payload.CaseContext) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "case_context is required and must be valid JSON for plan_case trigger"})
			return
		}
	}

	// Validate required fields
	if payload.Title == "" || payload.TaskType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and task_type are required"})
		return
	}

	// Resolve workspace: accepts ID, name (case-insensitive), or path
	workspaceID := payload.WorkspaceID
	if workspaceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspace_id is required"})
		return
	}
	if h.workspaceResolver != nil {
		resolved, err := h.workspaceResolver.ResolveWorkspaceID(workspaceID)
		if err != nil {
			log.Warn().Str("workspace_id", workspaceID).Err(err).Msg("webhook: workspace not found")
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("workspace not found: %s (provide a valid workspace ID, name, or path)", workspaceID),
			})
			return
		}
		workspaceID = resolved
	}

	// Create task
	t := task.NewTask(workspaceID, task.TaskType(payload.TaskType), payload.Title, payload.Description)
	t.CaseID = payload.CaseID
	t.TaskYAML = payload.TaskYAML
	t.Anchors = payload.Anchors
	t.Prompt = payload.Prompt
	t.CreatedBy = "webhook"

	if payload.Severity != "" {
		t.Severity = task.Severity(payload.Severity)
	}
	if payload.Labels != nil {
		t.Labels = payload.Labels
	}

	// Map trigger
	t.Trigger = &task.Trigger{
		Type:      payload.Trigger.Type,
		Source:    "lazyadmin",
		Ref:       payload.Trigger.Ref,
		Hash:      payload.Trigger.Hash,
		Timestamp: time.Now().UTC(),
	}

	// Map origin for callback credentials
	if payload.Origin != nil {
		t.Origin = &task.Origin{
			System: payload.Origin.System,
			TaskID: payload.Origin.TaskID,
			CaseID: payload.Origin.CaseID,
			URL:    payload.Origin.URL,
			APIKey: payload.Origin.APIKey,
		}
	}

	// Store case_context for plan_case triggers
	if isInvestigation {
		t.CaseContext = payload.CaseContext
		// Extract case_id from case_context and populate origin
		caseID := extractCaseIDFromContext(payload.CaseContext)
		if caseID != nil {
			t.CaseID = caseID
			if t.Origin != nil {
				t.Origin.CaseID = caseID
			}
		}
	}

	// Map constraints to policy — plan_case uses hardcoded policy (not overridable)
	if isInvestigation {
		t.Policy = task.PlanCasePolicy()
	} else if payload.Constraints != nil {
		t.Policy = &task.Policy{
			MaxFilesChanged: payload.Constraints.MaxFilesChanged,
			MustPassTests:   payload.Constraints.MustPassTests,
			MustPassBuild:   true,
			Autonomy:        payload.Constraints.Autonomy,
			RequireApproval: payload.Constraints.RequireApproval,
			MaxRounds:       3,
			MaxDurationMins: 30,
			AgentType:       "claude",
		}
	} else {
		t.Policy = task.DefaultPolicy()
	}

	t.AddTimelineEvent("created", "Task created from webhook", "webhook")

	// Persist
	if err := h.store.Create(t); err != nil {
		log.Error().Err(err).Str("title", t.Title).Msg("webhook: failed to create task")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create task"})
		return
	}

	log.Info().Str("task_id", t.ID).Str("title", t.Title).Str("type", string(t.TaskType)).Msg("webhook: task created")

	// Emit event
	if h.eventHub != nil {
		h.eventHub.Publish(events.NewTaskEvent(events.EventTypeTaskCreated, workspaceID, events.TaskEventPayload{
			TaskID:   t.ID,
			TaskType: string(t.TaskType),
			Title:    t.Title,
			Status:   string(t.Status),
			Severity: string(t.Severity),
		}))
	}

	// Auto-spawn if spawner is configured and policy allows autonomous execution
	autoSpawned := false
	if h.spawner != nil && shouldAutoSpawn(t) {
		if err := h.spawner.SpawnTask(context.Background(), t.ID); err != nil {
			log.Warn().Err(err).Str("task_id", t.ID).Msg("webhook: auto-spawn failed (task created but not started)")
		} else {
			autoSpawned = true
			log.Info().Str("task_id", t.ID).Msg("webhook: task auto-spawned")
		}
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":           t.ID,
		"status":       string(t.Status),
		"auto_spawned": autoSpawned,
	})
}

// shouldAutoSpawn determines whether a task should be auto-spawned based on its policy.
// Tasks with autonomy mode "manual" require explicit start via the REST API.
// All other modes (supervised, full-auto-bounded) are auto-spawned.
func shouldAutoSpawn(t *task.AgentTask) bool {
	if t.Policy == nil {
		return true // Default policy allows auto-spawn
	}
	return t.Policy.Autonomy != "manual"
}

// validateHMAC validates the HMAC-SHA256 signature.
func (h *TaskHandler) validateHMAC(body []byte, signature string) bool {
	// Signature format: "sha256=<hex>"
	sig := strings.TrimPrefix(signature, "sha256=")

	mac := hmac.New(sha256.New, []byte(h.webhookSecret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))

	return subtle.ConstantTimeCompare([]byte(sig), []byte(expectedMAC)) == 1
}

// --- REST API Endpoints ---

// handleTasks handles GET /api/tasks — list tasks with filtering.
func (h *TaskHandler) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	filter := taskstore.QueryFilter{
		Status:      q.Get("status"),
		TaskType:    q.Get("task_type"),
		WorkspaceID: q.Get("workspace_id"),
	}

	if limit := q.Get("limit"); limit != "" {
		_, _ = fmt.Sscanf(limit, "%d", &filter.Limit)
	}
	if offset := q.Get("offset"); offset != "" {
		_, _ = fmt.Sscanf(offset, "%d", &filter.Offset)
	}

	tasks, err := h.store.List(filter)
	if err != nil {
		log.Error().Err(err).Msg("failed to list tasks")
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tasks"})
		return
	}

	if tasks == nil {
		tasks = []*task.AgentTask{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// handleTaskByID routes /api/tasks/{id} and /api/tasks/{id}/{action}.
func (h *TaskHandler) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/tasks/{id} or /api/tasks/{id}/approve etc.
	path := strings.TrimPrefix(r.URL.Path, "/api/tasks/")
	if path == "" || path == "webhook" || path == "stats" {
		return // handled by other routes
	}

	parts := strings.SplitN(path, "/", 2)
	taskID := parts[0]

	if len(parts) == 1 {
		// /api/tasks/{id}
		h.handleTaskDetail(w, r, taskID)
		return
	}

	// /api/tasks/{id}/{action}
	action := parts[1]
	switch action {
	case "approve":
		h.handleTaskApprove(w, r, taskID)
	case "reject":
		h.handleTaskReject(w, r, taskID)
	case "cancel":
		h.handleTaskCancel(w, r, taskID)
	case "spawn":
		h.handleTaskSpawn(w, r, taskID)
	case "revisions":
		h.handleTaskRevisions(w, r, taskID)
	case "status":
		h.handleTaskStatusUpdate(w, r, taskID)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

// handleTaskDetail handles GET /api/tasks/{id}.
func (h *TaskHandler) handleTaskDetail(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t, err := h.store.GetByID(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	// Include revisions
	revisions, _ := h.store.GetRevisions(taskID)
	if revisions == nil {
		revisions = []task.Revision{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"task":      t,
		"revisions": revisions,
	})
}

// handleTaskApprove handles POST /api/tasks/{id}/approve.
func (h *TaskHandler) handleTaskApprove(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t, err := h.store.GetByID(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	if err := t.Transition(task.StatusCompleted); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	t.AddTimelineEvent("approved", "Task approved", "user")

	if err := h.store.Update(t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update task"})
		return
	}

	if h.eventHub != nil {
		h.eventHub.Publish(events.NewTaskEvent(events.EventTypeTaskApproved, t.WorkspaceID, events.TaskEventPayload{
			TaskID:   t.ID,
			TaskType: string(t.TaskType),
			Title:    t.Title,
			Status:   string(t.Status),
		}))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": t.ID, "status": string(t.Status)})
}

// handleTaskReject handles POST /api/tasks/{id}/reject.
func (h *TaskHandler) handleTaskReject(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t, err := h.store.GetByID(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	// Parse optional feedback
	var body struct {
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	// Rejected tasks go back to running for revision
	if err := t.Transition(task.StatusRunning); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	msg := "Task rejected"
	if body.Feedback != "" {
		msg = fmt.Sprintf("Task rejected: %s", body.Feedback)
	}
	t.AddTimelineEvent("rejected", msg, "user")

	// Create revision record if feedback provided
	if body.Feedback != "" {
		revisions, _ := h.store.GetRevisions(taskID)
		revision := &task.Revision{
			ID:         uuid.New().String(),
			TaskID:     taskID,
			RevisionNo: len(revisions) + 1,
			Feedback:   body.Feedback,
			CreatedBy:  "user",
			CreatedAt:  time.Now().UTC(),
		}
		if err := h.store.AddRevision(revision); err != nil {
			log.Warn().Err(err).Str("task_id", taskID).Msg("failed to add revision on reject")
		}
	}

	if err := h.store.Update(t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update task"})
		return
	}

	if h.eventHub != nil {
		h.eventHub.Publish(events.NewTaskEvent(events.EventTypeTaskRejected, t.WorkspaceID, events.TaskEventPayload{
			TaskID:   t.ID,
			TaskType: string(t.TaskType),
			Title:    t.Title,
			Status:   string(t.Status),
			Message:  body.Feedback,
		}))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": t.ID, "status": string(t.Status)})
}

// handleTaskCancel handles POST /api/tasks/{id}/cancel.
func (h *TaskHandler) handleTaskCancel(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	t, err := h.store.GetByID(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	if err := t.Transition(task.StatusFailed); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	t.AddTimelineEvent("cancelled", "Task cancelled by user", "user")

	if err := h.store.Update(t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update task"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": t.ID, "status": string(t.Status)})
}

// handleTaskSpawn handles POST /api/tasks/{id}/spawn — manually trigger task execution.
func (h *TaskHandler) handleTaskSpawn(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.spawner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "task spawner not configured"})
		return
	}

	// Verify task exists and is in a spawnable state
	t, err := h.store.GetByID(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	if t.Status != task.StatusPending && t.Status != task.StatusFailed && t.Status != task.StatusStuck {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("task cannot be spawned in status: %s (must be pending, failed, or stuck)", t.Status),
		})
		return
	}

	if err := h.spawner.SpawnTask(context.Background(), taskID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to spawn task: " + err.Error()})
		return
	}

	log.Info().Str("task_id", taskID).Msg("task manually spawned via API")
	writeJSON(w, http.StatusOK, map[string]interface{}{"id": taskID, "status": "spawned"})
}

// handleTaskRevisions handles GET/POST /api/tasks/{id}/revisions.
func (h *TaskHandler) handleTaskRevisions(w http.ResponseWriter, r *http.Request, taskID string) {
	switch r.Method {
	case http.MethodGet:
		revisions, err := h.store.GetRevisions(taskID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get revisions"})
			return
		}
		if revisions == nil {
			revisions = []task.Revision{}
		}
		writeJSON(w, http.StatusOK, revisions)

	case http.MethodPost:
		var body struct {
			Feedback      string `json:"feedback"`
			ResultSummary string `json:"result_summary"`
			CreatedBy     string `json:"created_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Feedback == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feedback is required"})
			return
		}

		existing, _ := h.store.GetRevisions(taskID)
		revision := &task.Revision{
			ID:            uuid.New().String(),
			TaskID:        taskID,
			RevisionNo:    len(existing) + 1,
			Feedback:      body.Feedback,
			ResultSummary: body.ResultSummary,
			CreatedBy:     body.CreatedBy,
			CreatedAt:     time.Now().UTC(),
		}

		if err := h.store.AddRevision(revision); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add revision"})
			return
		}

		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"id":          revision.ID,
			"revision_no": revision.RevisionNo,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleTaskStatusUpdate handles PUT /api/tasks/{id}/status.
func (h *TaskHandler) handleTaskStatusUpdate(w http.ResponseWriter, r *http.Request, taskID string) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Status         string            `json:"status"`
		VerdictStatus  string            `json:"verdict_status,omitempty"`
		VerdictSummary string            `json:"verdict_summary,omitempty"`
		DiffContent    string            `json:"diff_content,omitempty"`
		FilesChanged   []task.FileChange `json:"files_changed,omitempty"`
		RoundsCompleted int             `json:"rounds_completed,omitempty"`
		SessionID      string            `json:"session_id,omitempty"`
		BranchName     string            `json:"branch_name,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if !task.IsValidStatus(body.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status"})
		return
	}

	t, err := h.store.GetByID(taskID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	newStatus := task.Status(body.Status)
	if err := t.Transition(newStatus); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Update result fields
	if body.VerdictStatus != "" || body.VerdictSummary != "" || body.DiffContent != "" || body.FilesChanged != nil {
		if t.Result == nil {
			t.Result = &task.Result{}
		}
		if body.VerdictStatus != "" {
			t.Result.VerdictStatus = body.VerdictStatus
		}
		if body.VerdictSummary != "" {
			t.Result.VerdictSummary = body.VerdictSummary
		}
		if body.DiffContent != "" {
			t.Result.DiffContent = body.DiffContent
		}
		if body.FilesChanged != nil {
			t.Result.FilesChanged = body.FilesChanged
		}
		if body.RoundsCompleted > 0 {
			t.Result.RoundsCompleted = body.RoundsCompleted
		}
	}
	if body.SessionID != "" {
		t.SessionID = body.SessionID
	}
	if body.BranchName != "" {
		t.BranchName = body.BranchName
	}

	if err := h.store.Update(t); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update task"})
		return
	}

	// Emit appropriate event
	if h.eventHub != nil {
		eventType := events.EventTypeTaskProgress
		switch newStatus {
		case task.StatusRunning:
			eventType = events.EventTypeTaskStarted
		case task.StatusCompleted:
			eventType = events.EventTypeTaskCompleted
		case task.StatusFailed:
			eventType = events.EventTypeTaskFailed
		}
		h.eventHub.Publish(events.NewTaskEvent(eventType, t.WorkspaceID, events.TaskEventPayload{
			TaskID:    t.ID,
			TaskType:  string(t.TaskType),
			Title:     t.Title,
			Status:    string(t.Status),
			SessionID: t.SessionID,
		}))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"id": t.ID, "status": string(t.Status)})
}

// handleTaskStats handles GET /api/tasks/stats.
func (h *TaskHandler) handleTaskStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	counts, err := h.store.CountByStatus()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get stats"})
		return
	}

	writeJSON(w, http.StatusOK, counts)
}

// extractCaseIDFromContext extracts case_id from a case_context JSON blob.
func extractCaseIDFromContext(ctx json.RawMessage) *int {
	var parsed struct {
		CaseID *int `json:"case_id"`
	}
	if err := json.Unmarshal(ctx, &parsed); err != nil {
		return nil
	}
	return parsed.CaseID
}
