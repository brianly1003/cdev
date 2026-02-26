// Package hub implements the central event hub for cdev.
package hub

import (
	"sync"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
)

// FilteredSubscriber wraps a subscriber and filters events by workspace ID
// and optionally by focused workspace for permission events.
// Events without a workspace ID (global events) are always forwarded.
// If no workspaces are subscribed, all events are forwarded (backward compatible).
// Permission filtering only applies to pty_permission and pty_permission_resolved events.
// When a client has a focused workspace, permission events from any session in that
// workspace are forwarded (enabling multi-session badge indicators on iOS).
// Clients that never call SetSessionFocus receive all permission events (backward compatible).
type FilteredSubscriber struct {
	inner      ports.Subscriber
	workspaces map[string]bool // Set of workspace IDs to receive events for

	// Session focus filtering (for permission events only)
	focusedWorkspaceID string // Workspace that the focused session belongs to
	focusedSessionID   string // Session ID the client is currently focused on
	hasFocus           bool   // Whether the client has set a session focus

	mu sync.RWMutex
}

// NewFilteredSubscriber creates a new filtered subscriber wrapping the given subscriber.
func NewFilteredSubscriber(inner ports.Subscriber) *FilteredSubscriber {
	return &FilteredSubscriber{
		inner:      inner,
		workspaces: make(map[string]bool),
	}
}

// ID returns the subscriber's unique identifier.
func (f *FilteredSubscriber) ID() string {
	return f.inner.ID()
}

// Send sends an event to the subscriber if it passes the filter.
func (f *FilteredSubscriber) Send(event events.Event) error {
	if !f.shouldForward(event) {
		return nil // Silently skip events that don't match filter
	}
	return f.inner.Send(event)
}

// Close closes the subscriber.
func (f *FilteredSubscriber) Close() error {
	return f.inner.Close()
}

// Done returns a channel that's closed when the subscriber is done.
func (f *FilteredSubscriber) Done() <-chan struct{} {
	return f.inner.Done()
}

// SubscribeWorkspace adds a workspace to the filter.
// Events for this workspace will be forwarded to the subscriber.
func (f *FilteredSubscriber) SubscribeWorkspace(workspaceID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspaces[workspaceID] = true
}

// UnsubscribeWorkspace removes a workspace from the filter.
func (f *FilteredSubscriber) UnsubscribeWorkspace(workspaceID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.workspaces, workspaceID)
}

// SubscribeAll clears the filter, forwarding all events (default behavior).
func (f *FilteredSubscriber) SubscribeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspaces = make(map[string]bool)
}

// GetSubscribedWorkspaces returns the list of subscribed workspace IDs.
func (f *FilteredSubscriber) GetSubscribedWorkspaces() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	result := make([]string, 0, len(f.workspaces))
	for id := range f.workspaces {
		result = append(result, id)
	}
	return result
}

// IsFiltering returns true if the subscriber is filtering by workspace.
func (f *FilteredSubscriber) IsFiltering() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.workspaces) > 0
}

// SetSessionFocus sets the session and workspace the client is focused on.
// Only pty_permission and pty_permission_resolved events are session-filtered.
func (f *FilteredSubscriber) SetSessionFocus(workspaceID, sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focusedWorkspaceID = workspaceID
	f.focusedSessionID = sessionID
	f.hasFocus = true
}

// ClearSessionFocus removes session focus, reverting to pass-all behavior for permission events.
func (f *FilteredSubscriber) ClearSessionFocus() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focusedWorkspaceID = ""
	f.focusedSessionID = ""
	f.hasFocus = false
}

// ClearSessionFocusIfWorkspace clears session focus only if the focused session
// belongs to the given workspace. Returns true if focus was cleared.
// Guards against empty workspaceID to prevent accidental clearing.
func (f *FilteredSubscriber) ClearSessionFocusIfWorkspace(workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hasFocus && f.focusedWorkspaceID == workspaceID {
		f.focusedWorkspaceID = ""
		f.focusedSessionID = ""
		f.hasFocus = false
		return true
	}
	return false
}

// GetFocusedSessionID returns the focused session ID and whether focus is set.
func (f *FilteredSubscriber) GetFocusedSessionID() (string, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.focusedSessionID, f.hasFocus
}

// isPermissionEvent returns true for event types that should be filtered by workspace focus.
func isPermissionEvent(eventType events.EventType) bool {
	return eventType == events.EventTypePTYPermission || eventType == events.EventTypePTYPermissionResolved
}

// shouldForward determines if an event should be forwarded to the subscriber.
func (f *FilteredSubscriber) shouldForward(event events.Event) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Workspace filtering
	if len(f.workspaces) > 0 {
		workspaceID := event.GetWorkspaceID()
		// Non-global events must match a subscribed workspace
		if workspaceID != "" && !f.workspaces[workspaceID] {
			return false
		}
	}

	// Workspace-scoped permission filtering: when a client has a focused workspace,
	// permission events from any session in that workspace pass through.
	if f.hasFocus && isPermissionEvent(event.Type()) {
		workspaceID := event.GetWorkspaceID()
		// Events with a workspace ID must match the focused workspace
		if workspaceID != "" && workspaceID != f.focusedWorkspaceID {
			return false
		}
	}

	return true
}
