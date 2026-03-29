package agent

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/taskstore"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/brianly1003/cdev/internal/workspace"
)

type mockSessionStarter struct {
	resolved             map[string]string
	startSessionFn       func(ctx context.Context, workspaceID string, prompt string, agentType string, workDir string) (string, error)
	waitForCompletionFn  func(ctx context.Context, sessionID string) (string, error)
	stopSessionFn        func(sessionID string) error
}

func (m *mockSessionStarter) StartSessionWithPrompt(ctx context.Context, workspaceID string, prompt string, agentType string, workDir string) (string, error) {
	if m.startSessionFn != nil {
		return m.startSessionFn(ctx, workspaceID, prompt, agentType, workDir)
	}
	return "", nil
}

func (m *mockSessionStarter) WaitForCompletion(ctx context.Context, sessionID string) (string, error) {
	if m.waitForCompletionFn != nil {
		return m.waitForCompletionFn(ctx, sessionID)
	}
	return "idle", nil
}

func (m *mockSessionStarter) ResolveSessionID(sessionID string) string {
	if resolved, ok := m.resolved[sessionID]; ok {
		return resolved
	}
	return sessionID
}

func (m *mockSessionStarter) StopSession(sessionID string) error {
	if m.stopSessionFn != nil {
		return m.stopSessionFn(sessionID)
	}
	return nil
}

func TestPersistResolvedSessionIDUpdatesTaskStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := taskstore.NewStore()
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	agentTask := task.NewTask("ws-1", task.TaskTypePlanCase, "Case 5", "Plan")
	agentTask.SessionID = "temporary-session-id"
	if err := store.Create(agentTask); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	spawner := &Spawner{
		store:          store,
		sessionStarter: &mockSessionStarter{resolved: map[string]string{"temporary-session-id": "real-session-id"}},
	}

	spawner.persistResolvedSessionID(agentTask)

	if agentTask.SessionID != "real-session-id" {
		t.Fatalf("task SessionID = %q, want %q", agentTask.SessionID, "real-session-id")
	}

	persisted, err := store.GetByID(agentTask.ID)
	if err != nil {
		t.Fatalf("GetByID() failed: %v", err)
	}
	if persisted.SessionID != "real-session-id" {
		t.Fatalf("persisted SessionID = %q, want %q", persisted.SessionID, "real-session-id")
	}
}

func TestExecutePlanCaseTimeoutPreservesWorktreeAndStopsSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := taskstore.NewStore()
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	repoPath := initGitRepo(t)
	workspaceID := "ws-plan-case"
	ws := workspace.NewWorkspace(config.WorkspaceDefinition{
		ID:           workspaceID,
		Name:         "Lazy",
		Path:         repoPath,
		CreatedAt:    time.Now().UTC(),
		LastAccessed: time.Now().UTC(),
	})

	var stoppedSessionID string
	starter := &mockSessionStarter{
		startSessionFn: func(ctx context.Context, workspaceID string, prompt string, agentType string, workDir string) (string, error) {
			return "timeout-session-id", nil
		},
		waitForCompletionFn: func(ctx context.Context, sessionID string) (string, error) {
			<-ctx.Done()
			return "timeout", ctx.Err()
		},
		stopSessionFn: func(sessionID string) error {
			stoppedSessionID = sessionID
			return nil
		},
	}

	spawner := &Spawner{
		store:          store,
		sessionStarter: starter,
		workspaceLookup: &stubWorkspaceLookup{
			workspaces: map[string]*workspace.Workspace{
				workspaceID: ws,
			},
		},
	}

	agentTask := task.NewTask(workspaceID, task.TaskTypePlanCase, "Case 5", "Plan case timeout preservation")
	agentTask.Trigger = &task.Trigger{Type: "plan_case"}
	agentTask.Policy = &task.Policy{MaxDurationMins: 1}
	agentTask.CaseContext = json.RawMessage(`{"case_id":5,"title":"AccountVerifyPhone"}`)
	if err := store.Create(agentTask); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	spawner.executePlanCase(ctx, agentTask, cancel)
	defer spawner.cleanupWorktree(agentTask.WorktreePath)

	if stoppedSessionID != "timeout-session-id" {
		t.Fatalf("stoppedSessionID = %q, want %q", stoppedSessionID, "timeout-session-id")
	}

	if _, err := os.Stat(agentTask.WorktreePath); err != nil {
		t.Fatalf("expected timed out plan case worktree to be preserved: %v", err)
	}

	persisted, err := store.GetByID(agentTask.ID)
	if err != nil {
		t.Fatalf("GetByID() failed: %v", err)
	}
	if persisted.Status != task.StatusFailed {
		t.Fatalf("persisted status = %s, want %s", persisted.Status, task.StatusFailed)
	}
	if persisted.Result == nil || persisted.Result.VerdictSummary != "Planning timed out after 1 minutes" {
		t.Fatalf("unexpected timeout verdict: %#v", persisted.Result)
	}
}
