package hub

import (
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/testutil"
)

func TestHub_New(t *testing.T) {
	h := New()

	if h == nil {
		t.Fatal("New() returned nil")
	}
	if h.subscribers == nil {
		t.Error("subscribers map is nil")
	}
	if h.broadcast == nil {
		t.Error("broadcast channel is nil")
	}
	if h.register == nil {
		t.Error("register channel is nil")
	}
	if h.unregister == nil {
		t.Error("unregister channel is nil")
	}
	if h.done == nil {
		t.Error("done channel is nil")
	}
	if h.running {
		t.Error("hub should not be running initially")
	}
}

func TestHub_StartStop(t *testing.T) {
	h := New()

	// Start the hub
	if err := h.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !h.IsRunning() {
		t.Error("hub should be running after Start()")
	}

	// Starting again should be a no-op
	if err := h.Start(); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}

	// Stop the hub
	if err := h.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if h.IsRunning() {
		t.Error("hub should not be running after Stop()")
	}

	// Stopping again should be a no-op
	if err := h.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestHub_Subscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	// Give hub time to start
	time.Sleep(10 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test-1")
	h.Subscribe(sub)

	// Wait for registration to process
	time.Sleep(10 * time.Millisecond)

	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", h.SubscriberCount())
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(10 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test-1")
	h.Subscribe(sub)

	time.Sleep(10 * time.Millisecond)

	if h.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1", h.SubscriberCount())
	}

	h.Unsubscribe("test-1")

	time.Sleep(10 * time.Millisecond)

	if h.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() after unsubscribe = %d, want 0", h.SubscriberCount())
	}

	if !sub.IsClosed() {
		t.Error("subscriber should be closed after unsubscribe")
	}
}

func TestHub_Publish(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(10 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test-1")
	h.Subscribe(sub)

	time.Sleep(10 * time.Millisecond)

	// Publish an event
	event := events.NewEvent(events.EventTypeHeartbeat, map[string]string{"test": "value"})
	h.Publish(event)

	// Wait for event to be delivered
	time.Sleep(20 * time.Millisecond)

	if sub.EventCount() != 1 {
		t.Errorf("subscriber received %d events, want 1", sub.EventCount())
	}

	received := sub.Events()[0]
	if received.Type() != events.EventTypeHeartbeat {
		t.Errorf("received event type = %v, want %v", received.Type(), events.EventTypeHeartbeat)
	}
}

func TestHub_PublishToMultipleSubscribers(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(10 * time.Millisecond)

	sub1 := testutil.NewMockSubscriber("test-1")
	sub2 := testutil.NewMockSubscriber("test-2")
	sub3 := testutil.NewMockSubscriber("test-3")

	h.Subscribe(sub1)
	h.Subscribe(sub2)
	h.Subscribe(sub3)

	time.Sleep(10 * time.Millisecond)

	if h.SubscriberCount() != 3 {
		t.Fatalf("SubscriberCount() = %d, want 3", h.SubscriberCount())
	}

	// Publish multiple events
	for i := 0; i < 5; i++ {
		event := events.NewEvent(events.EventTypeClaudeLog, map[string]int{"count": i})
		h.Publish(event)
	}

	time.Sleep(50 * time.Millisecond)

	// All subscribers should receive all events
	for _, sub := range []*testutil.MockSubscriber{sub1, sub2, sub3} {
		if sub.EventCount() != 5 {
			t.Errorf("subscriber %s received %d events, want 5", sub.ID(), sub.EventCount())
		}
	}
}

func TestHub_FailedSendRemovesSubscriber(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(10 * time.Millisecond)

	// Create a subscriber that fails on send
	failingSub := testutil.NewMockSubscriber("failing")
	failingSub.SetSendError(errTestSendFailed)

	goodSub := testutil.NewMockSubscriber("good")

	h.Subscribe(failingSub)
	h.Subscribe(goodSub)

	time.Sleep(10 * time.Millisecond)

	// Publish an event
	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	h.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// Failing subscriber should be removed
	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1 (failing subscriber should be removed)", h.SubscriberCount())
	}

	// Good subscriber should have received the event
	if goodSub.EventCount() != 1 {
		t.Errorf("good subscriber received %d events, want 1", goodSub.EventCount())
	}
}

func TestHub_ConcurrentOperations(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(10 * time.Millisecond)

	var wg sync.WaitGroup
	numGoroutines := 10
	numEvents := 100

	// Create subscribers
	subscribers := make([]*testutil.MockSubscriber, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		subscribers[i] = testutil.NewMockSubscriber(string(rune('a' + i)))
		h.Subscribe(subscribers[i])
	}

	time.Sleep(20 * time.Millisecond)

	// Concurrently publish events
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				event := events.NewEvent(events.EventTypeClaudeLog, map[string]int{"id": id, "seq": j})
				h.Publish(event)
			}
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	// Each subscriber should receive all events
	expectedEvents := numGoroutines * numEvents
	for _, sub := range subscribers {
		count := sub.EventCount()
		if count != expectedEvents {
			t.Errorf("subscriber %s received %d events, want %d", sub.ID(), count, expectedEvents)
		}
	}
}

func TestHub_StopClosesAllSubscribers(t *testing.T) {
	h := New()
	_ = h.Start()

	time.Sleep(10 * time.Millisecond)

	sub1 := testutil.NewMockSubscriber("test-1")
	sub2 := testutil.NewMockSubscriber("test-2")

	h.Subscribe(sub1)
	h.Subscribe(sub2)

	time.Sleep(10 * time.Millisecond)

	_ = h.Stop()

	// All subscribers should be closed
	if !sub1.IsClosed() {
		t.Error("subscriber 1 should be closed after hub stop")
	}
	if !sub2.IsClosed() {
		t.Error("subscriber 2 should be closed after hub stop")
	}
}

func TestHub_SubscriberCount(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(10 * time.Millisecond)

	if h.SubscriberCount() != 0 {
		t.Errorf("initial SubscriberCount() = %d, want 0", h.SubscriberCount())
	}

	for i := 0; i < 5; i++ {
		sub := testutil.NewMockSubscriber(string(rune('a' + i)))
		h.Subscribe(sub)
	}

	time.Sleep(20 * time.Millisecond)

	if h.SubscriberCount() != 5 {
		t.Errorf("SubscriberCount() = %d, want 5", h.SubscriberCount())
	}
}

// errTestSendFailed is a test error for failed sends.
var errTestSendFailed = &testSendError{}

type testSendError struct{}

func (e *testSendError) Error() string { return "test send failed" }
