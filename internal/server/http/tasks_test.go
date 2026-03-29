package http

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brianly1003/cdev/internal/adapters/taskstore"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/task"
)

// mockEventHub implements the Publish interface for testing.
type mockEventHub struct {
	events []events.Event
}

func (m *mockEventHub) Publish(e events.Event) {
	m.events = append(m.events, e)
}

// mockSpawner implements TaskSpawner for testing.
type mockSpawner struct {
	spawnedTaskIDs []string
	shouldFail     bool
}

func (m *mockSpawner) SpawnTask(_ context.Context, taskID string) error {
	if m.shouldFail {
		return errSpawnFailed
	}
	m.spawnedTaskIDs = append(m.spawnedTaskIDs, taskID)
	return nil
}

var errSpawnFailed = errorString("spawn failed")

type errorString string

func (e errorString) Error() string { return string(e) }

func setupTestHandler(t *testing.T) (*TaskHandler, *taskstore.Store, *mockEventHub, *mockSpawner) {
	t.Helper()

	store, err := taskstore.NewStore()
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	hub := &mockEventHub{}
	spawner := &mockSpawner{}

	handler := NewTaskHandler(store, "test-secret", hub)
	handler.SetSpawner(spawner)

	return handler, store, hub, spawner
}

func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhook_CreatesTask(t *testing.T) {
	handler, store, hub, _ := setupTestHandler(t)

	payload := WebhookPayload{
		Trigger: WebhookTrigger{
			Type:      "task_created",
			Ref:       "123",
			Timestamp: "2026-03-04T00:00:00Z",
		},
		WorkspaceID: "test-workspace",
		TaskType:    "fix-issue",
		Title:       "Fix broken login flow",
		Description: "The login flow fails for new members",
		Severity:    "high",
		Labels:      []string{"bug", "login"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(body, "test-secret"))

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	// Assert: 201 Created
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Assert: response contains task ID
	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	taskID, ok := resp["id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("expected non-empty task ID in response, got: %v", resp)
	}

	// Assert: task exists in store
	createdTask, err := store.GetByID(taskID)
	if err != nil {
		t.Fatalf("task not found in store: %v", err)
	}

	if createdTask.Title != "Fix broken login flow" {
		t.Errorf("expected title 'Fix broken login flow', got '%s'", createdTask.Title)
	}
	if createdTask.TaskType != task.TaskType("fix-issue") {
		t.Errorf("expected task type 'fix-issue', got '%s'", createdTask.TaskType)
	}
	if createdTask.Severity != task.Severity("high") {
		t.Errorf("expected severity 'high', got '%s'", createdTask.Severity)
	}
	if createdTask.WorkspaceID != "test-workspace" {
		t.Errorf("expected workspace 'test-workspace', got '%s'", createdTask.WorkspaceID)
	}
	if createdTask.Status != task.StatusPending {
		t.Errorf("expected status 'pending', got '%s'", createdTask.Status)
	}

	// Assert: event was emitted
	if len(hub.events) == 0 {
		t.Fatal("expected at least one event emitted")
	}
	if hub.events[0].Type() != events.EventTypeTaskCreated {
		t.Errorf("expected event type 'task_created', got '%s'", hub.events[0].Type())
	}
}

func TestWebhook_AutoSpawn_DefaultPolicy(t *testing.T) {
	handler, _, _, spawner := setupTestHandler(t)

	// Default policy (no constraints) should auto-spawn
	payload := WebhookPayload{
		Trigger:     WebhookTrigger{Type: "task_created", Ref: "456"},
		WorkspaceID: "test-workspace",
		TaskType:    "fix-issue",
		Title:       "Auto-spawn test",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(body, "test-secret"))

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Assert: auto_spawned is true
	if autoSpawned, ok := resp["auto_spawned"].(bool); !ok || !autoSpawned {
		t.Errorf("expected auto_spawned=true, got %v", resp["auto_spawned"])
	}

	// Assert: spawner was called
	if len(spawner.spawnedTaskIDs) != 1 {
		t.Fatalf("expected 1 spawned task, got %d", len(spawner.spawnedTaskIDs))
	}
}

func TestWebhook_NoAutoSpawn_ManualPolicy(t *testing.T) {
	handler, _, _, spawner := setupTestHandler(t)

	// Manual autonomy mode should NOT auto-spawn
	payload := WebhookPayload{
		Trigger:     WebhookTrigger{Type: "task_created", Ref: "789"},
		WorkspaceID: "test-workspace",
		TaskType:    "fix-issue",
		Title:       "Manual task",
		Constraints: &WebhookConstraints{
			Autonomy: "manual",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(body, "test-secret"))

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Assert: auto_spawned is false
	if autoSpawned, ok := resp["auto_spawned"].(bool); !ok || autoSpawned {
		t.Errorf("expected auto_spawned=false, got %v", resp["auto_spawned"])
	}

	// Assert: spawner was NOT called
	if len(spawner.spawnedTaskIDs) != 0 {
		t.Errorf("expected 0 spawned tasks, got %d", len(spawner.spawnedTaskIDs))
	}
}

func TestWebhook_InvalidSignature_Rejected(t *testing.T) {
	handler, _, _, _ := setupTestHandler(t)

	payload := WebhookPayload{
		Trigger:  WebhookTrigger{Type: "task_created", Ref: "bad"},
		TaskType: "fix-issue",
		Title:    "Bad signature",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", "sha256=invalid_signature")

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rr.Code)
	}
}

func TestWebhook_MissingTitle_BadRequest(t *testing.T) {
	handler, _, _, _ := setupTestHandler(t)

	payload := WebhookPayload{
		Trigger:  WebhookTrigger{Type: "task_created"},
		TaskType: "fix-issue",
		// Title intentionally omitted
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(body, "test-secret"))

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestTaskApprove_TransitionsToCompleted(t *testing.T) {
	handler, store, hub, _ := setupTestHandler(t)

	// Create a task in awaiting_approval state
	tk := task.NewTask("ws", "fix-issue", "Test", "desc")
	_ = tk.Transition(task.StatusRunning)
	_ = tk.Transition(task.StatusValidating)
	_ = tk.Transition(task.StatusAwaitingApproval)
	_ = store.Create(tk)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+tk.ID+"/approve", nil)
	rr := httptest.NewRecorder()
	handler.handleTaskApprove(rr, req, tk.ID)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify task is completed
	updated, err := store.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.Status != task.StatusCompleted {
		t.Errorf("expected completed, got %s", updated.Status)
	}

	// Verify approved event was emitted
	found := false
	for _, e := range hub.events {
		if e.Type() == events.EventTypeTaskApproved {
			found = true
		}
	}
	if !found {
		t.Error("expected task_approved event")
	}
}

func TestTaskReject_TransitionsToRunningWithRevision(t *testing.T) {
	handler, store, _, _ := setupTestHandler(t)

	// Create a task in awaiting_approval state
	tk := task.NewTask("ws", "fix-issue", "Test", "desc")
	_ = tk.Transition(task.StatusRunning)
	_ = tk.Transition(task.StatusValidating)
	_ = tk.Transition(task.StatusAwaitingApproval)
	_ = store.Create(tk)

	feedback := `{"feedback": "Please also add tests"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+tk.ID+"/reject", bytes.NewBufferString(feedback))
	rr := httptest.NewRecorder()
	handler.handleTaskReject(rr, req, tk.ID)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify task is back to running
	updated, err := store.GetByID(tk.ID)
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if updated.Status != task.StatusRunning {
		t.Errorf("expected running, got %s", updated.Status)
	}

	// Verify revision was created
	revisions, err := store.GetRevisions(tk.ID)
	if err != nil {
		t.Fatalf("failed to get revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("expected 1 revision, got %d", len(revisions))
	}
	if revisions[0].Feedback != "Please also add tests" {
		t.Errorf("expected feedback 'Please also add tests', got '%s'", revisions[0].Feedback)
	}
}

func TestTaskSpawn_ManualEndpoint(t *testing.T) {
	handler, store, _, spawner := setupTestHandler(t)

	// Create a pending task
	tk := task.NewTask("ws", "fix-issue", "Manual spawn test", "desc")
	_ = store.Create(tk)

	req := httptest.NewRequest(http.MethodPost, "/api/tasks/"+tk.ID+"/spawn", nil)
	rr := httptest.NewRecorder()
	handler.handleTaskSpawn(rr, req, tk.ID)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if len(spawner.spawnedTaskIDs) != 1 {
		t.Fatalf("expected 1 spawned task, got %d", len(spawner.spawnedTaskIDs))
	}
	if spawner.spawnedTaskIDs[0] != tk.ID {
		t.Errorf("expected spawned task ID %s, got %s", tk.ID, spawner.spawnedTaskIDs[0])
	}
}

func TestWebhook_PlanCase_EnforcesPolicy(t *testing.T) {
	handler, store, _, spawner := setupTestHandler(t)

	caseCtx := json.RawMessage(`{"case_id": 42, "title": "Login timeout", "description": "Users report 500 on /api/login"}`)
	payload := WebhookPayload{
		Trigger: WebhookTrigger{
			Type:      "plan_case",
			Ref:       "case-42",
			Timestamp: "2026-03-11T00:00:00Z",
		},
		WorkspaceID: "test-workspace",
		TaskType:    "fix-issue", // should be overridden to plan-case
		Title:       "Plan login timeout case",
		CaseContext: caseCtx,
		Origin: &WebhookOrigin{
			System: "lazyadmin",
			CaseID: intPtr(42),
			URL:    "https://lazy.example.com",
			APIKey: "test-key",
		},
		// Caller tries to set constraints — should be ignored
		Constraints: &WebhookConstraints{
			MaxFilesChanged: 50,
			Autonomy:        "full-auto-bounded",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(body, "test-secret"))

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	taskID := resp["id"].(string)

	// Verify task in store
	created, err := store.GetByID(taskID)
	if err != nil {
		t.Fatalf("task not found: %v", err)
	}

	// Task type should be overridden to plan-case
	if created.TaskType != task.TaskTypePlanCase {
		t.Errorf("expected task type plan-case, got %s", created.TaskType)
	}

	// Policy should be hardcoded plan case policy
	if created.Policy == nil {
		t.Fatal("expected non-nil policy")
	}
	if created.Policy.MaxFilesChanged != 0 {
		t.Errorf("expected max_files_changed=0 (read-only), got %d", created.Policy.MaxFilesChanged)
	}
	if created.Policy.MaxDurationMins != 30 {
		t.Errorf("expected max_duration_mins=30, got %d", created.Policy.MaxDurationMins)
	}
	if created.Policy.Autonomy != "bounded-auto" {
		t.Errorf("expected autonomy=bounded-auto, got %s", created.Policy.Autonomy)
	}
	if len(created.Policy.RequireApproval) != 0 {
		t.Errorf("expected empty require_approval, got %v", created.Policy.RequireApproval)
	}

	// Case context should be stored
	if len(created.CaseContext) == 0 {
		t.Error("expected case_context to be stored on task")
	}

	// Case ID should be extracted from case_context
	if created.CaseID == nil || *created.CaseID != 42 {
		t.Errorf("expected case_id=42, got %v", created.CaseID)
	}

	// Auto-spawn should have been called (bounded-auto)
	if len(spawner.spawnedTaskIDs) != 1 {
		t.Fatalf("expected 1 spawned task, got %d", len(spawner.spawnedTaskIDs))
	}
}

func TestWebhook_PlanCase_RequiresCaseContext(t *testing.T) {
	handler, _, _, _ := setupTestHandler(t)

	// plan_case without case_context should fail
	payload := WebhookPayload{
		Trigger: WebhookTrigger{
			Type: "plan_case",
			Ref:  "case-99",
		},
		WorkspaceID: "test-workspace",
		Title:       "Missing context test",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/tasks/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", signPayload(body, "test-secret"))

	rr := httptest.NewRecorder()
	handler.handleWebhook(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing case_context, got %d: %s", rr.Code, rr.Body.String())
	}
}

func intPtr(i int) *int { return &i }

func TestShouldAutoSpawn(t *testing.T) {
	tests := []struct {
		name     string
		policy   *task.Policy
		expected bool
	}{
		{"nil policy = auto-spawn", nil, true},
		{"empty autonomy = auto-spawn", &task.Policy{Autonomy: ""}, true},
		{"supervised = auto-spawn", &task.Policy{Autonomy: "supervised"}, true},
		{"full-auto-bounded = auto-spawn", &task.Policy{Autonomy: "full-auto-bounded"}, true},
		{"bounded-auto = auto-spawn", &task.Policy{Autonomy: "bounded-auto"}, true},
		{"manual = no auto-spawn", &task.Policy{Autonomy: "manual"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tk := task.NewTask("ws", "fix-issue", "test", "desc")
			tk.Policy = tt.policy
			result := shouldAutoSpawn(tk)
			if result != tt.expected {
				t.Errorf("shouldAutoSpawn() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestTaskStats(t *testing.T) {
	handler, store, _, _ := setupTestHandler(t)

	// Record baseline counts (shared DB may have residual data)
	baseReq := httptest.NewRequest(http.MethodGet, "/api/tasks/stats", nil)
	baseRR := httptest.NewRecorder()
	handler.handleTaskStats(baseRR, baseReq)
	var baseline map[string]int
	if err := json.Unmarshal(baseRR.Body.Bytes(), &baseline); err != nil {
		t.Fatalf("failed to unmarshal baseline: %v", err)
	}

	// Create tasks in various states
	t1 := task.NewTask("stats-ws", "fix-issue", "Stats Task 1", "")
	_ = store.Create(t1)

	t2 := task.NewTask("stats-ws", "fix-issue", "Stats Task 2", "")
	_ = t2.Transition(task.StatusRunning)
	_ = store.Create(t2)

	t3 := task.NewTask("stats-ws", "fix-issue", "Stats Task 3", "")
	_ = t3.Transition(task.StatusRunning)
	_ = t3.Transition(task.StatusValidating)
	_ = t3.Transition(task.StatusAwaitingApproval)
	_ = t3.Transition(task.StatusCompleted)
	_ = store.Create(t3)

	req := httptest.NewRequest(http.MethodGet, "/api/tasks/stats", nil)
	rr := httptest.NewRecorder()
	handler.handleTaskStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var stats map[string]int
	if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
		t.Fatalf("failed to unmarshal stats: %v", err)
	}

	// Check relative increase from baseline
	pendingInc := stats["pending"] - baseline["pending"]
	runningInc := stats["running"] - baseline["running"]
	completedInc := stats["completed"] - baseline["completed"]

	if pendingInc < 1 {
		t.Errorf("expected at least 1 new pending task, got increase of %d", pendingInc)
	}
	if runningInc < 1 {
		t.Errorf("expected at least 1 new running task, got increase of %d", runningInc)
	}
	if completedInc < 1 {
		t.Errorf("expected at least 1 new completed task, got increase of %d", completedInc)
	}
}
