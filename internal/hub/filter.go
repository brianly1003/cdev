// Package hub implements the central event hub for cdev.
package hub

import (
	"sync"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
)

// FilteredSubscriber wraps a subscriber and filters events by workspace ID.
// Events without a workspace ID (global events) are always forwarded.
// If no workspaces are subscribed, all events are forwarded (backward compatible).
type FilteredSubscriber struct {
	inner      ports.Subscriber
	workspaces map[string]bool // Set of workspace IDs to receive events for
	mu         sync.RWMutex
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

// shouldForward determines if an event should be forwarded to the subscriber.
func (f *FilteredSubscriber) shouldForward(event events.Event) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// If no filter set, forward all events (backward compatible)
	if len(f.workspaces) == 0 {
		return true
	}

	// Global events (no workspace ID) are always forwarded
	workspaceID := event.GetWorkspaceID()
	if workspaceID == "" {
		return true
	}

	// Check if event's workspace is in our filter
	return f.workspaces[workspaceID]
}
