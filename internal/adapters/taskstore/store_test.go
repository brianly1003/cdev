package taskstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/brianly1003/cdev/internal/domain/task"
)

func TestReplaceSessionIDUpdatesPersistedTask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	agentTask := task.NewTask("ws-1", task.TaskTypePlanCase, "Case 1", "Investigate")
	agentTask.SessionID = "temp-session-id"
	if err := store.Create(agentTask); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	updated, err := store.ReplaceSessionID("temp-session-id", "real-session-id")
	if err != nil {
		t.Fatalf("ReplaceSessionID() failed: %v", err)
	}
	if updated != 1 {
		t.Fatalf("ReplaceSessionID() updated %d rows, want 1", updated)
	}

	persisted, err := store.GetByID(agentTask.ID)
	if err != nil {
		t.Fatalf("GetByID() failed: %v", err)
	}
	if persisted.SessionID != "real-session-id" {
		t.Fatalf("persisted SessionID = %q, want %q", persisted.SessionID, "real-session-id")
	}
}

func TestRepairSessionIDsRecoversRealClaudeTranscriptID(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	worktreePath := filepath.Join("/Users/brianly/Projects/Lazy", ".claude", "worktrees", "task-123")
	realSessionID := "66cfa725-4112-4a44-945b-368c600c08b2"

	sessionsDir := claudeSessionsDirForProjectPath(worktreePath)
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("MkdirAll(%q) failed: %v", sessionsDir, err)
	}
	sessionFile := filepath.Join(sessionsDir, realSessionID+".jsonl")
	if err := os.WriteFile(sessionFile, []byte(`{"sessionId":"`+realSessionID+`"}`+"\n"), 0644); err != nil {
		t.Fatalf("WriteFile(%q) failed: %v", sessionFile, err)
	}

	agentTask := task.NewTask("ws-1", task.TaskTypePlanCase, "Case 5", "Investigate")
	agentTask.SessionID = "cf67b01b-d48c-48e8-8921-34875d5b7d7d"
	agentTask.WorktreePath = worktreePath
	if err := store.Create(agentTask); err != nil {
		t.Fatalf("Create() failed: %v", err)
	}

	repaired, err := store.RepairSessionIDs()
	if err != nil {
		t.Fatalf("RepairSessionIDs() failed: %v", err)
	}
	if repaired != 1 {
		t.Fatalf("RepairSessionIDs() repaired %d rows, want 1", repaired)
	}

	persisted, err := store.GetByID(agentTask.ID)
	if err != nil {
		t.Fatalf("GetByID() failed: %v", err)
	}
	if persisted.SessionID != realSessionID {
		t.Fatalf("persisted SessionID = %q, want %q", persisted.SessionID, realSessionID)
	}
}
