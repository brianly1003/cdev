package watcher

import (
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
)

// DebouncedEvent represents a debounced file change event.
type DebouncedEvent struct {
	Path       string
	ChangeType events.FileChangeType
	Timer      *time.Timer
}

// Debouncer coalesces rapid file system events.
type Debouncer struct {
	window   time.Duration
	callback func(path string, changeType events.FileChangeType)

	mu      sync.Mutex
	pending map[string]*DebouncedEvent
	stopped bool
}

// NewDebouncer creates a new debouncer with the given window and callback.
func NewDebouncer(window time.Duration, callback func(path string, changeType events.FileChangeType)) *Debouncer {
	return &Debouncer{
		window:   window,
		callback: callback,
		pending:  make(map[string]*DebouncedEvent),
	}
}

// Add queues an event for debouncing.
func (d *Debouncer) Add(path string, changeType events.FileChangeType) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.stopped {
		return
	}

	// Check if there's already a pending event for this path
	if existing, ok := d.pending[path]; ok {
		// Stop existing timer
		existing.Timer.Stop()

		// Update change type (prioritize certain changes)
		existing.ChangeType = mergeChangeTypes(existing.ChangeType, changeType)

		// Reset timer
		existing.Timer = time.AfterFunc(d.window, func() {
			d.fire(path)
		})
	} else {
		// Create new pending event
		d.pending[path] = &DebouncedEvent{
			Path:       path,
			ChangeType: changeType,
			Timer: time.AfterFunc(d.window, func() {
				d.fire(path)
			}),
		}
	}
}

// fire executes the callback for a path.
func (d *Debouncer) fire(path string) {
	d.mu.Lock()
	event, ok := d.pending[path]
	if !ok {
		d.mu.Unlock()
		return
	}
	delete(d.pending, path)
	stopped := d.stopped
	d.mu.Unlock()

	if !stopped && d.callback != nil {
		d.callback(event.Path, event.ChangeType)
	}
}

// Stop stops all pending timers.
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.stopped = true
	for _, event := range d.pending {
		event.Timer.Stop()
	}
	d.pending = make(map[string]*DebouncedEvent)
}

// mergeChangeTypes combines two change types, preferring the more significant one.
func mergeChangeTypes(existing, new events.FileChangeType) events.FileChangeType {
	// Delete takes precedence
	if new == events.FileChangeDeleted {
		return events.FileChangeDeleted
	}
	// Create takes precedence over modify
	if existing == events.FileChangeCreated {
		return events.FileChangeCreated
	}
	// Otherwise use the new type
	return new
}
