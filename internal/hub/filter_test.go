package hub

import (
	"testing"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/testutil"
)

// --- Workspace-scoped permission filtering ---

func TestFilteredSubscriber_WorkspaceFocus_TwoSessionsSameWorkspace(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	// Permission from session-abc in ws-1 → passes
	e1 := events.NewEvent(events.EventTypePTYPermission, nil)
	e1.SessionID = "session-abc"
	e1.WorkspaceID = "ws-1"

	// Permission from session-xyz in ws-1 → also passes (same workspace)
	e2 := events.NewEvent(events.EventTypePTYPermission, nil)
	e2.SessionID = "session-xyz"
	e2.WorkspaceID = "ws-1"

	_ = fs.Send(e1)
	_ = fs.Send(e2)
	if inner.EventCount() != 2 {
		t.Errorf("expected 2 events forwarded (both sessions in focused workspace), got %d", inner.EventCount())
	}
}

func TestFilteredSubscriber_WorkspaceFocus_DifferentWorkspaceBlocked(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	// Permission from ws-2 → blocked
	event := events.NewEvent(events.EventTypePTYPermission, nil)
	event.SessionID = "session-xyz"
	event.WorkspaceID = "ws-2"

	_ = fs.Send(event)
	if inner.EventCount() != 0 {
		t.Errorf("expected 0 events forwarded (different workspace blocked), got %d", inner.EventCount())
	}
}

func TestFilteredSubscriber_WorkspaceFocus_PermissionResolvedFiltered(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	// pty_permission_resolved from different workspace → blocked
	event := events.NewEventWithContext(events.EventTypePTYPermissionResolved, nil, "ws-2", "session-xyz")
	_ = fs.Send(event)
	if inner.EventCount() != 0 {
		t.Errorf("expected 0 events forwarded (resolved from different workspace blocked), got %d", inner.EventCount())
	}

	// pty_permission_resolved from same workspace → passes
	event2 := events.NewEventWithContext(events.EventTypePTYPermissionResolved, nil, "ws-1", "session-xyz")
	_ = fs.Send(event2)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event forwarded (resolved from same workspace passes), got %d", inner.EventCount())
	}
}

// --- No focus: backward compatible pass-all ---

func TestFilteredSubscriber_NoFocus_PassesAllPermissions(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)

	e1 := events.NewEvent(events.EventTypePTYPermission, nil)
	e1.SessionID = "session-abc"
	e1.WorkspaceID = "ws-1"
	e2 := events.NewEvent(events.EventTypePTYPermission, nil)
	e2.SessionID = "session-xyz"
	e2.WorkspaceID = "ws-2"

	_ = fs.Send(e1)
	_ = fs.Send(e2)
	if inner.EventCount() != 2 {
		t.Errorf("expected 2 events forwarded (no focus), got %d", inner.EventCount())
	}
}

// --- Non-permission events are never workspace-filtered ---

func TestFilteredSubscriber_WorkspaceFocus_NonPermissionEventsNotFiltered(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	event := events.NewEvent(events.EventTypeClaudeLog, nil)
	event.SessionID = "session-xyz"
	event.WorkspaceID = "ws-2" // different workspace, but not a permission event

	_ = fs.Send(event)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event forwarded (non-permission not filtered), got %d", inner.EventCount())
	}
}

// --- Empty WorkspaceID on permission event passes through ---

func TestFilteredSubscriber_WorkspaceFocus_EmptyWorkspacePassesThrough(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	event := events.NewEvent(events.EventTypePTYPermission, nil)
	// event.WorkspaceID is "" by default

	_ = fs.Send(event)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event forwarded (empty workspace passes), got %d", inner.EventCount())
	}
}

// --- ClearSessionFocus reverts to pass-all ---

func TestFilteredSubscriber_ClearSessionFocus(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	blocked := events.NewEvent(events.EventTypePTYPermission, nil)
	blocked.SessionID = "session-xyz"
	blocked.WorkspaceID = "ws-2"
	_ = fs.Send(blocked)
	if inner.EventCount() != 0 {
		t.Fatal("expected blocked before clear")
	}

	fs.ClearSessionFocus()

	passed := events.NewEvent(events.EventTypePTYPermission, nil)
	passed.SessionID = "session-xyz"
	passed.WorkspaceID = "ws-2"
	_ = fs.Send(passed)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event forwarded after ClearSessionFocus, got %d", inner.EventCount())
	}
}

// --- GetFocusedSessionID lifecycle ---

func TestFilteredSubscriber_GetFocusedSessionID(t *testing.T) {
	fs := NewFilteredSubscriber(testutil.NewMockSubscriber("client-1"))

	// Initially no focus
	id, hasFocus := fs.GetFocusedSessionID()
	if hasFocus {
		t.Error("expected hasFocus=false initially")
	}
	if id != "" {
		t.Errorf("expected empty session ID, got %q", id)
	}

	// Set focus
	fs.SetSessionFocus("ws-1", "session-abc")
	id, hasFocus = fs.GetFocusedSessionID()
	if !hasFocus {
		t.Error("expected hasFocus=true after SetSessionFocus")
	}
	if id != "session-abc" {
		t.Errorf("expected session-abc, got %q", id)
	}

	// Clear focus
	fs.ClearSessionFocus()
	id, hasFocus = fs.GetFocusedSessionID()
	if hasFocus {
		t.Error("expected hasFocus=false after ClearSessionFocus")
	}
	if id != "" {
		t.Errorf("expected empty session ID after clear, got %q", id)
	}
}

// --- Workspace subscription + workspace focus work together ---

func TestFilteredSubscriber_WorkspaceSubscriptionAndFocusTogether(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SubscribeWorkspace("ws-1")
	fs.SetSessionFocus("ws-1", "session-abc")

	// Workspace match, different session in same workspace → passes (workspace-scoped)
	e1 := events.NewEventWithContext(events.EventTypePTYPermission, nil, "ws-1", "session-other")
	_ = fs.Send(e1)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event: same workspace, different session should pass; got %d", inner.EventCount())
	}

	// Different workspace → blocked by workspace subscription filter
	e2 := events.NewEventWithContext(events.EventTypePTYPermission, nil, "ws-other", "session-abc")
	_ = fs.Send(e2)
	if inner.EventCount() != 1 {
		t.Error("expected blocked: different workspace not in subscription")
	}

	// Same workspace, same session → passes
	e3 := events.NewEventWithContext(events.EventTypePTYPermission, nil, "ws-1", "session-abc")
	_ = fs.Send(e3)
	if inner.EventCount() != 2 {
		t.Errorf("expected 2 events forwarded (both match), got %d", inner.EventCount())
	}
}

func TestFilteredSubscriber_WorkspaceFilterWithNoSessionFilter(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SubscribeWorkspace("ws-1")

	event := events.NewEventWithContext(events.EventTypePTYPermission, nil, "ws-1", "session-xyz")
	_ = fs.Send(event)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event forwarded (workspace match, no focus filter), got %d", inner.EventCount())
	}
}

// --- ClearSessionFocusIfWorkspace ---

func TestFilteredSubscriber_ClearSessionFocusIfWorkspace_Matching(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-abc")

	// Unsubscribing ws-1 should clear focus since session-abc belongs to ws-1
	cleared := fs.ClearSessionFocusIfWorkspace("ws-1")
	if !cleared {
		t.Error("expected ClearSessionFocusIfWorkspace to return true for matching workspace")
	}

	_, hasFocus := fs.GetFocusedSessionID()
	if hasFocus {
		t.Error("expected hasFocus=false after clearing matching workspace")
	}

	// Now permission from any workspace should pass through (focus cleared → pass-all)
	event := events.NewEvent(events.EventTypePTYPermission, nil)
	event.SessionID = "session-xyz"
	event.WorkspaceID = "ws-2"
	_ = fs.Send(event)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event forwarded after workspace clear, got %d", inner.EventCount())
	}
}

func TestFilteredSubscriber_ClearSessionFocusIfWorkspace_NonMatching(t *testing.T) {
	fs := NewFilteredSubscriber(testutil.NewMockSubscriber("client-1"))
	fs.SetSessionFocus("ws-1", "session-abc")

	// Unsubscribing ws-2 should NOT clear focus (session is in ws-1)
	cleared := fs.ClearSessionFocusIfWorkspace("ws-2")
	if cleared {
		t.Error("expected ClearSessionFocusIfWorkspace to return false for non-matching workspace")
	}

	id, hasFocus := fs.GetFocusedSessionID()
	if !hasFocus || id != "session-abc" {
		t.Error("focus should be preserved after clearing non-matching workspace")
	}
}

func TestFilteredSubscriber_ClearSessionFocusIfWorkspace_EmptyWorkspace(t *testing.T) {
	fs := NewFilteredSubscriber(testutil.NewMockSubscriber("client-1"))
	fs.SetSessionFocus("ws-1", "session-abc")

	// Empty workspace ID should be a no-op (guard against accidental clear)
	cleared := fs.ClearSessionFocusIfWorkspace("")
	if cleared {
		t.Error("expected ClearSessionFocusIfWorkspace(\"\") to return false")
	}

	id, hasFocus := fs.GetFocusedSessionID()
	if !hasFocus || id != "session-abc" {
		t.Error("focus should be preserved after clearing with empty workspace")
	}
}

func TestFilteredSubscriber_ClearSessionFocusIfWorkspace_NoFocusSet(t *testing.T) {
	fs := NewFilteredSubscriber(testutil.NewMockSubscriber("client-1"))

	// No focus set — should be no-op
	cleared := fs.ClearSessionFocusIfWorkspace("ws-1")
	if cleared {
		t.Error("expected false when no focus set")
	}
}

// --- SetSessionFocus overwrites previous focus ---

func TestFilteredSubscriber_SetSessionFocus_Overwrite(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)
	fs.SetSessionFocus("ws-1", "session-old")

	// Switch focus to new session in a different workspace
	fs.SetSessionFocus("ws-2", "session-new")

	// Permission from old workspace should now be blocked
	e1 := events.NewEvent(events.EventTypePTYPermission, nil)
	e1.SessionID = "session-old"
	e1.WorkspaceID = "ws-1"
	_ = fs.Send(e1)
	if inner.EventCount() != 0 {
		t.Error("old workspace should be blocked after focus switch")
	}

	// Permission from new workspace should pass
	e2 := events.NewEvent(events.EventTypePTYPermission, nil)
	e2.SessionID = "session-new"
	e2.WorkspaceID = "ws-2"
	_ = fs.Send(e2)
	if inner.EventCount() != 1 {
		t.Error("new workspace should pass after focus switch")
	}

	// ClearSessionFocusIfWorkspace on old workspace should no-op
	cleared := fs.ClearSessionFocusIfWorkspace("ws-1")
	if cleared {
		t.Error("old workspace should not match after focus switch to ws-2")
	}

	// ClearSessionFocusIfWorkspace on new workspace should clear
	cleared = fs.ClearSessionFocusIfWorkspace("ws-2")
	if !cleared {
		t.Error("new workspace should match after focus switch")
	}
}

// --- Workspace unsubscribe + session focus interaction (end-to-end) ---

func TestFilteredSubscriber_WorkspaceUnsubscribeClearsFocus(t *testing.T) {
	inner := testutil.NewMockSubscriber("client-1")
	fs := NewFilteredSubscriber(inner)

	// Simulate: subscribe to ws-1, focus on a session, then switch to ws-2
	fs.SubscribeWorkspace("ws-1")
	fs.SetSessionFocus("ws-1", "session-abc")

	// Add ws-2
	fs.SubscribeWorkspace("ws-2")

	// Unsubscribe ws-1 — should clear focus (session was in ws-1)
	fs.UnsubscribeWorkspace("ws-1")
	fs.ClearSessionFocusIfWorkspace("ws-1")

	// Permission event from ws-2 should now pass (focus cleared → pass-all)
	event := events.NewEvent(events.EventTypePTYPermission, nil)
	event.SessionID = "session-in-ws2"
	event.WorkspaceID = "ws-2"
	_ = fs.Send(event)
	if inner.EventCount() != 1 {
		t.Errorf("expected 1 event after workspace switch, got %d", inner.EventCount())
	}
}

// --- isPermissionEvent ---

func TestIsPermissionEvent(t *testing.T) {
	tests := []struct {
		eventType events.EventType
		expected  bool
	}{
		{events.EventTypePTYPermission, true},
		{events.EventTypePTYPermissionResolved, true},
		{events.EventTypeClaudeLog, false},
		{events.EventTypeHeartbeat, false},
		{events.EventTypeSessionStart, false},
		{events.EventTypeFileChanged, false},
		{events.EventTypePTYOutput, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.eventType), func(t *testing.T) {
			if got := isPermissionEvent(tt.eventType); got != tt.expected {
				t.Errorf("isPermissionEvent(%s) = %v, want %v", tt.eventType, got, tt.expected)
			}
		})
	}
}
