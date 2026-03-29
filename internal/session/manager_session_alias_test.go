package session

import "testing"

func TestUpdateSessionID_AllowsLookupByTemporaryAndRealID(t *testing.T) {
	workspaceID := "workspace-session-alias"
	workspacePath := t.TempDir()
	manager := newSessionTestManager(t, workspaceID, workspacePath)

	sess, err := manager.StartNewSession(workspaceID)
	if err != nil {
		t.Fatalf("StartNewSession failed: %v", err)
	}

	temporaryID := sess.GetID()
	realID := "550e8400-e29b-41d4-a716-446655440000"

	manager.updateSessionID(workspaceID, temporaryID, realID)

	byTemp, err := manager.GetSession(temporaryID)
	if err != nil {
		t.Fatalf("GetSession(temporaryID) failed: %v", err)
	}
	byReal, err := manager.GetSession(realID)
	if err != nil {
		t.Fatalf("GetSession(realID) failed: %v", err)
	}

	if byTemp != byReal {
		t.Fatalf("temporary and real lookups should resolve to the same session instance")
	}
	if byTemp.GetID() != realID {
		t.Fatalf("session ID = %s, want %s", byTemp.GetID(), realID)
	}
	if got := manager.GetActiveSession(workspaceID); got != realID {
		t.Fatalf("active session = %s, want %s", got, realID)
	}
}

func TestStopSession_WorksWithTemporaryAliasAfterResolution(t *testing.T) {
	workspaceID := "workspace-session-stop-alias"
	workspacePath := t.TempDir()
	manager := newSessionTestManager(t, workspaceID, workspacePath)

	sess, err := manager.StartNewSession(workspaceID)
	if err != nil {
		t.Fatalf("StartNewSession failed: %v", err)
	}

	temporaryID := sess.GetID()
	realID := "550e8400-e29b-41d4-a716-446655440001"

	manager.updateSessionID(workspaceID, temporaryID, realID)

	if err := manager.StopSession(temporaryID); err != nil {
		t.Fatalf("StopSession(temporaryID) failed: %v", err)
	}

	stopped, err := manager.GetSession(realID)
	if err != nil {
		t.Fatalf("GetSession(realID) after stop failed: %v", err)
	}
	if status := stopped.GetStatus(); status != StatusStopped {
		t.Fatalf("session status = %s, want %s", status, StatusStopped)
	}
}

func TestWatchWorkspaceSession_ResolvesTemporaryAliasToRealID(t *testing.T) {
	workspaceID := "workspace-session-watch-alias"
	workspacePath := t.TempDir()
	manager := newSessionTestManager(t, workspaceID, workspacePath)

	sess, err := manager.StartNewSession(workspaceID)
	if err != nil {
		t.Fatalf("StartNewSession failed: %v", err)
	}

	temporaryID := sess.GetID()
	realID := "550e8400-e29b-41d4-a716-446655440002"

	manager.updateSessionID(workspaceID, temporaryID, realID)
	createClaudeSessionFile(t, workspacePath, realID)

	info, err := manager.WatchWorkspaceSession("client-a", workspaceID, temporaryID)
	if err != nil {
		t.Fatalf("WatchWorkspaceSession with temporary ID failed: %v", err)
	}

	if info.SessionID != realID {
		t.Fatalf("watched session ID = %s, want %s", info.SessionID, realID)
	}
}
