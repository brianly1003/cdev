package watcher

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
)

type testEventHub struct {
	mu     sync.Mutex
	events []events.Event
}

func (h *testEventHub) Start() error {
	return nil
}

func (h *testEventHub) Stop() error {
	return nil
}

func (h *testEventHub) Publish(event events.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *testEventHub) Subscribe(sub ports.Subscriber) {
}

func (h *testEventHub) Unsubscribe(id string) {
}

func (h *testEventHub) SubscriberCount() int {
	return 0
}

func (h *testEventHub) requireSingleEvent(t *testing.T) *events.BaseEvent {
	t.Helper()

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.events) != 1 {
		t.Fatalf("event count = %d, want 1", len(h.events))
	}

	base, ok := h.events[0].(*events.BaseEvent)
	if !ok {
		t.Fatalf("event type = %T, want *events.BaseEvent", h.events[0])
	}

	return base
}

func TestHandleDebouncedEventSetsWorkspaceContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	hub := &testEventHub{}
	w := NewWatcherWithWorkspace(root, hub, 10, nil, "ws-123")

	w.handleDebouncedEvent("file.txt", events.FileChangeCreated)

	event := hub.requireSingleEvent(t)
	if event.GetWorkspaceID() != "ws-123" {
		t.Fatalf("workspace_id = %q, want %q", event.GetWorkspaceID(), "ws-123")
	}
}

func TestHandleDebouncedRenameSetsWorkspaceContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "new.txt"), []byte("content"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	hub := &testEventHub{}
	w := NewWatcherWithWorkspace(root, hub, 10, nil, "ws-rename")

	w.pendingRenamesMu.Lock()
	w.pendingRenames["."] = pendingRename{
		oldPath:   "old.txt",
		timestamp: time.Now(),
	}
	w.pendingRenamesMu.Unlock()

	w.handleDebouncedEvent("new.txt", events.FileChangeCreated)

	event := hub.requireSingleEvent(t)
	if event.GetWorkspaceID() != "ws-rename" {
		t.Fatalf("workspace_id = %q, want %q", event.GetWorkspaceID(), "ws-rename")
	}

	payload, ok := event.Payload.(events.FileChangedPayload)
	if !ok {
		t.Fatalf("payload type = %T, want events.FileChangedPayload", event.Payload)
	}
	if payload.Change != events.FileChangeRenamed {
		t.Fatalf("change = %q, want %q", payload.Change, events.FileChangeRenamed)
	}
	if payload.OldPath != "old.txt" || payload.Path != "new.txt" {
		t.Fatalf("rename payload = old:%q new:%q, want old:%q new:%q", payload.OldPath, payload.Path, "old.txt", "new.txt")
	}
}

func TestHandleDebouncedEventWithoutWorkspaceContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	hub := &testEventHub{}
	w := NewWatcher(root, hub, 10, nil)

	w.handleDebouncedEvent("plain.txt", events.FileChangeCreated)

	event := hub.requireSingleEvent(t)
	if event.GetWorkspaceID() != "" {
		t.Fatalf("workspace_id = %q, want empty", event.GetWorkspaceID())
	}
}
