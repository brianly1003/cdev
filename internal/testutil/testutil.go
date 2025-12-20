// Package testutil provides shared test utilities and mocks for cdev tests.
package testutil

import (
	"sync"
	"testing"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
)

// MockSubscriber implements ports.Subscriber for testing.
type MockSubscriber struct {
	id       string
	events   []events.Event
	mu       sync.Mutex
	closed   bool
	sendErr  error
	sendFunc func(events.Event) error
	done     chan struct{}
}

// NewMockSubscriber creates a new mock subscriber.
func NewMockSubscriber(id string) *MockSubscriber {
	return &MockSubscriber{
		id:     id,
		events: make([]events.Event, 0),
		done:   make(chan struct{}),
	}
}

// ID returns the subscriber ID.
func (m *MockSubscriber) ID() string {
	return m.id
}

// Send records the event and returns any configured error.
func (m *MockSubscriber) Send(e events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendFunc != nil {
		return m.sendFunc(e)
	}

	if m.sendErr != nil {
		return m.sendErr
	}

	m.events = append(m.events, e)
	return nil
}

// Close marks the subscriber as closed.
func (m *MockSubscriber) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.closed = true
		close(m.done)
	}
	return nil
}

// Done returns a channel that's closed when the subscriber is done.
func (m *MockSubscriber) Done() <-chan struct{} {
	return m.done
}

// Events returns all received events.
func (m *MockSubscriber) Events() []events.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]events.Event, len(m.events))
	copy(result, m.events)
	return result
}

// EventCount returns the number of received events.
func (m *MockSubscriber) EventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

// IsClosed returns whether the subscriber was closed.
func (m *MockSubscriber) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// SetSendError configures an error to return on Send.
func (m *MockSubscriber) SetSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendErr = err
}

// SetSendFunc sets a custom function for Send behavior.
func (m *MockSubscriber) SetSendFunc(fn func(events.Event) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendFunc = fn
}

// ClearEvents removes all recorded events.
func (m *MockSubscriber) ClearEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = m.events[:0]
}

// Ensure MockSubscriber implements ports.Subscriber.
var _ ports.Subscriber = (*MockSubscriber)(nil)

// MockEventHub implements ports.EventHub for testing.
type MockEventHub struct {
	events      []events.Event
	subscribers []ports.Subscriber
	mu          sync.Mutex
	started     bool
	stopped     bool
}

// NewMockEventHub creates a new mock event hub.
func NewMockEventHub() *MockEventHub {
	return &MockEventHub{
		events:      make([]events.Event, 0),
		subscribers: make([]ports.Subscriber, 0),
	}
}

// Start marks the hub as started.
func (m *MockEventHub) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

// Stop marks the hub as stopped.
func (m *MockEventHub) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	return nil
}

// Publish records the event.
func (m *MockEventHub) Publish(e events.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

// Subscribe records the subscriber.
func (m *MockEventHub) Subscribe(sub ports.Subscriber) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers = append(m.subscribers, sub)
}

// Unsubscribe removes a subscriber by ID.
func (m *MockEventHub) Unsubscribe(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, sub := range m.subscribers {
		if sub.ID() == id {
			m.subscribers = append(m.subscribers[:i], m.subscribers[i+1:]...)
			return
		}
	}
}

// SubscriberCount returns the number of subscribers.
func (m *MockEventHub) SubscriberCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.subscribers)
}

// IsRunning returns true if the hub was started and not stopped.
func (m *MockEventHub) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started && !m.stopped
}

// PublishedEvents returns all published events.
func (m *MockEventHub) PublishedEvents() []events.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]events.Event, len(m.events))
	copy(result, m.events)
	return result
}

// Ensure MockEventHub implements ports.EventHub.
var _ ports.EventHub = (*MockEventHub)(nil)

// AssertEqual is a simple equality assertion helper.
func AssertEqual(t *testing.T, expected, actual interface{}, msg string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// AssertTrue asserts that a condition is true.
func AssertTrue(t *testing.T, condition bool, msg string) {
	t.Helper()
	if !condition {
		t.Errorf("%s: expected true, got false", msg)
	}
}

// AssertFalse asserts that a condition is false.
func AssertFalse(t *testing.T, condition bool, msg string) {
	t.Helper()
	if condition {
		t.Errorf("%s: expected false, got true", msg)
	}
}

// AssertNil asserts that a value is nil.
func AssertNil(t *testing.T, value interface{}, msg string) {
	t.Helper()
	if value != nil {
		t.Errorf("%s: expected nil, got %v", msg, value)
	}
}

// AssertNotNil asserts that a value is not nil.
func AssertNotNil(t *testing.T, value interface{}, msg string) {
	t.Helper()
	if value == nil {
		t.Errorf("%s: expected non-nil value", msg)
	}
}

// AssertNoError asserts that an error is nil.
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", msg, err)
	}
}

// AssertError asserts that an error is not nil.
func AssertError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error, got nil", msg)
	}
}

// AssertContains checks if a string contains a substring.
func AssertContains(t *testing.T, s, substr, msg string) {
	t.Helper()
	if len(substr) == 0 {
		return
	}
	if len(s) < len(substr) {
		t.Errorf("%s: string %q does not contain %q", msg, s, substr)
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("%s: string %q does not contain %q", msg, s, substr)
}
