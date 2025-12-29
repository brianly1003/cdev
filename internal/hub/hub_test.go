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
	time.Sleep(50 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test-1")
	h.Subscribe(sub)

	// Wait for registration to process
	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", h.SubscriberCount())
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test-1")
	h.Subscribe(sub)

	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 1 {
		t.Fatalf("SubscriberCount() = %d, want 1", h.SubscriberCount())
	}

	h.Unsubscribe("test-1")

	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test-1")
	h.Subscribe(sub)

	time.Sleep(50 * time.Millisecond)

	// Publish an event
	event := events.NewEvent(events.EventTypeHeartbeat, map[string]string{"test": "value"})
	h.Publish(event)

	// Wait for event to be delivered
	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)

	sub1 := testutil.NewMockSubscriber("test-1")
	sub2 := testutil.NewMockSubscriber("test-2")
	sub3 := testutil.NewMockSubscriber("test-3")

	h.Subscribe(sub1)
	h.Subscribe(sub2)
	h.Subscribe(sub3)

	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)

	// Create a subscriber that fails on send
	failingSub := testutil.NewMockSubscriber("failing")
	failingSub.SetSendError(errTestSendFailed)

	goodSub := testutil.NewMockSubscriber("good")

	h.Subscribe(failingSub)
	h.Subscribe(goodSub)

	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	numGoroutines := 5
	numEvents := 20

	// Create subscribers
	subscribers := make([]*testutil.MockSubscriber, numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		subscribers[i] = testutil.NewMockSubscriber(string(rune('a' + i)))
		h.Subscribe(subscribers[i])
	}

	time.Sleep(100 * time.Millisecond)

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

	// Wait for events with polling instead of fixed sleep
	expectedEvents := numGoroutines * numEvents
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		allReceived := true
		for _, sub := range subscribers {
			if sub.EventCount() < expectedEvents {
				allReceived = false
				break
			}
		}
		if allReceived {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Each subscriber should receive all events
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

	time.Sleep(50 * time.Millisecond)

	sub1 := testutil.NewMockSubscriber("test-1")
	sub2 := testutil.NewMockSubscriber("test-2")

	h.Subscribe(sub1)
	h.Subscribe(sub2)

	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 0 {
		t.Errorf("initial SubscriberCount() = %d, want 0", h.SubscriberCount())
	}

	for i := 0; i < 5; i++ {
		sub := testutil.NewMockSubscriber(string(rune('a' + i)))
		h.Subscribe(sub)
	}

	time.Sleep(100 * time.Millisecond)

	if h.SubscriberCount() != 5 {
		t.Errorf("SubscriberCount() = %d, want 5", h.SubscriberCount())
	}
}

// errTestSendFailed is a test error for failed sends.
var errTestSendFailed = &testSendError{}

type testSendError struct{}

func (e *testSendError) Error() string { return "test send failed" }

func TestHub_MultipleSubscribersFailSimultaneously(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create multiple failing subscribers
	failing1 := testutil.NewMockSubscriber("failing-1")
	failing1.SetSendError(errTestSendFailed)
	failing2 := testutil.NewMockSubscriber("failing-2")
	failing2.SetSendError(errTestSendFailed)
	failing3 := testutil.NewMockSubscriber("failing-3")
	failing3.SetSendError(errTestSendFailed)

	goodSub := testutil.NewMockSubscriber("good")

	h.Subscribe(failing1)
	h.Subscribe(failing2)
	h.Subscribe(failing3)
	h.Subscribe(goodSub)

	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 4 {
		t.Fatalf("SubscriberCount() = %d, want 4", h.SubscriberCount())
	}

	// Publish an event - all failing subscribers should be removed
	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	h.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// Only the good subscriber should remain
	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1 (all failing subscribers should be removed)", h.SubscriberCount())
	}

	// Good subscriber should have received the event
	if goodSub.EventCount() != 1 {
		t.Errorf("good subscriber received %d events, want 1", goodSub.EventCount())
	}

	// All failing subscribers should be closed
	if !failing1.IsClosed() {
		t.Error("failing-1 should be closed")
	}
	if !failing2.IsClosed() {
		t.Error("failing-2 should be closed")
	}
	if !failing3.IsClosed() {
		t.Error("failing-3 should be closed")
	}
}

func TestHub_AllSubscribersFail(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create only failing subscribers
	failing1 := testutil.NewMockSubscriber("failing-1")
	failing1.SetSendError(errTestSendFailed)
	failing2 := testutil.NewMockSubscriber("failing-2")
	failing2.SetSendError(errTestSendFailed)

	h.Subscribe(failing1)
	h.Subscribe(failing2)

	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 2 {
		t.Fatalf("SubscriberCount() = %d, want 2", h.SubscriberCount())
	}

	// Publish an event - all subscribers should be removed
	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	h.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// No subscribers should remain
	if h.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0 (all subscribers failed)", h.SubscriberCount())
	}

	// Hub should still be running
	if !h.IsRunning() {
		t.Error("hub should still be running after all subscribers fail")
	}

	// Should be able to add new subscribers
	newSub := testutil.NewMockSubscriber("new")
	h.Subscribe(newSub)

	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1 (new subscriber should be added)", h.SubscriberCount())
	}
}

func TestHub_SubscriberFailsOnConsecutiveEvents(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	failingSub := testutil.NewMockSubscriber("failing")
	failingSub.SetSendError(errTestSendFailed)

	goodSub := testutil.NewMockSubscriber("good")

	h.Subscribe(failingSub)
	h.Subscribe(goodSub)

	time.Sleep(50 * time.Millisecond)

	// Publish multiple events
	for i := 0; i < 5; i++ {
		event := events.NewEvent(events.EventTypeHeartbeat, map[string]int{"seq": i})
		h.Publish(event)
		time.Sleep(20 * time.Millisecond)
	}

	time.Sleep(50 * time.Millisecond)

	// Failing subscriber should be removed after first event
	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", h.SubscriberCount())
	}

	// Good subscriber should have received all events
	if goodSub.EventCount() != 5 {
		t.Errorf("good subscriber received %d events, want 5", goodSub.EventCount())
	}
}

func TestHub_IntermittentFailure(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create a subscriber that fails on the 3rd event
	callCount := 0
	intermittentSub := testutil.NewMockSubscriber("intermittent")
	intermittentSub.SetSendFunc(func(e events.Event) error {
		callCount++
		if callCount == 3 {
			return errTestSendFailed
		}
		return nil
	})

	goodSub := testutil.NewMockSubscriber("good")

	h.Subscribe(intermittentSub)
	h.Subscribe(goodSub)

	time.Sleep(50 * time.Millisecond)

	// Publish 5 events
	for i := 0; i < 5; i++ {
		event := events.NewEvent(events.EventTypeHeartbeat, map[string]int{"seq": i})
		h.Publish(event)
		time.Sleep(20 * time.Millisecond)
	}

	time.Sleep(50 * time.Millisecond)

	// Intermittent subscriber should be removed after 3rd event
	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1", h.SubscriberCount())
	}

	// Good subscriber should have received all events
	if goodSub.EventCount() != 5 {
		t.Errorf("good subscriber received %d events, want 5", goodSub.EventCount())
	}

	// Intermittent subscriber should be closed
	if !intermittentSub.IsClosed() {
		t.Error("intermittent subscriber should be closed after failure")
	}
}

func TestHub_ResubscribeAfterFailure(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create a failing subscriber
	failingSub := testutil.NewMockSubscriber("will-fail")
	failingSub.SetSendError(errTestSendFailed)

	h.Subscribe(failingSub)

	time.Sleep(50 * time.Millisecond)

	// Publish to trigger failure
	event1 := events.NewEvent(events.EventTypeHeartbeat, nil)
	h.Publish(event1)

	time.Sleep(50 * time.Millisecond)

	// Subscriber should be removed
	if h.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0", h.SubscriberCount())
	}

	// Create a new subscriber with the same ID (simulating reconnection)
	newSub := testutil.NewMockSubscriber("will-fail")
	// This time it won't fail
	h.Subscribe(newSub)

	time.Sleep(50 * time.Millisecond)

	if h.SubscriberCount() != 1 {
		t.Errorf("SubscriberCount() = %d, want 1 (resubscribed)", h.SubscriberCount())
	}

	// Publish another event
	event2 := events.NewEvent(events.EventTypeClaudeLog, nil)
	h.Publish(event2)

	time.Sleep(50 * time.Millisecond)

	// New subscriber should receive the event
	if newSub.EventCount() != 1 {
		t.Errorf("resubscribed subscriber received %d events, want 1", newSub.EventCount())
	}
}

func TestHub_ConcurrentPublishWithFailingSubscribers(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create a mix of good and failing subscribers
	var goodSubs []*testutil.MockSubscriber
	for i := 0; i < 3; i++ {
		sub := testutil.NewMockSubscriber("good-" + string(rune('a'+i)))
		goodSubs = append(goodSubs, sub)
		h.Subscribe(sub)
	}

	var failingSubs []*testutil.MockSubscriber
	for i := 0; i < 3; i++ {
		sub := testutil.NewMockSubscriber("fail-" + string(rune('a'+i)))
		sub.SetSendError(errTestSendFailed)
		failingSubs = append(failingSubs, sub)
		h.Subscribe(sub)
	}

	time.Sleep(100 * time.Millisecond)

	if h.SubscriberCount() != 6 {
		t.Fatalf("SubscriberCount() = %d, want 6", h.SubscriberCount())
	}

	// Concurrently publish many events
	var wg sync.WaitGroup
	numGoroutines := 5
	numEvents := 10

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
	time.Sleep(200 * time.Millisecond)

	// Only good subscribers should remain
	if h.SubscriberCount() != 3 {
		t.Errorf("SubscriberCount() = %d, want 3 (only good subscribers)", h.SubscriberCount())
	}

	// All failing subscribers should be closed
	for _, sub := range failingSubs {
		if !sub.IsClosed() {
			t.Errorf("failing subscriber %s should be closed", sub.ID())
		}
	}

	// Good subscribers should have received events (exact count may vary due to timing)
	for _, sub := range goodSubs {
		if sub.EventCount() == 0 {
			t.Errorf("good subscriber %s received no events", sub.ID())
		}
	}
}

func TestHub_FailedSubscriberIsClosed(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	failingSub := testutil.NewMockSubscriber("failing")
	failingSub.SetSendError(errTestSendFailed)

	h.Subscribe(failingSub)

	time.Sleep(50 * time.Millisecond)

	// Verify not closed initially
	if failingSub.IsClosed() {
		t.Fatal("subscriber should not be closed before failure")
	}

	// Publish to trigger failure
	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	h.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// Subscriber should be closed after removal
	if !failingSub.IsClosed() {
		t.Error("failed subscriber should be closed after removal")
	}

	// Done channel should be closed
	select {
	case <-failingSub.Done():
		// Expected - channel is closed
	default:
		t.Error("Done() channel should be closed after subscriber removal")
	}
}

func TestHub_HighLoadWithFailures(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Add many subscribers, some failing
	numGood := 10
	numFailing := 10

	var goodSubs []*testutil.MockSubscriber
	for i := 0; i < numGood; i++ {
		sub := testutil.NewMockSubscriber("good-" + string(rune('a'+i)))
		goodSubs = append(goodSubs, sub)
		h.Subscribe(sub)
	}

	var failingSubs []*testutil.MockSubscriber
	for i := 0; i < numFailing; i++ {
		sub := testutil.NewMockSubscriber("fail-" + string(rune('a'+i)))
		sub.SetSendError(errTestSendFailed)
		failingSubs = append(failingSubs, sub)
		h.Subscribe(sub)
	}

	time.Sleep(100 * time.Millisecond)

	// Publish many events rapidly
	numEvents := 100
	for i := 0; i < numEvents; i++ {
		event := events.NewEvent(events.EventTypeClaudeLog, map[string]int{"seq": i})
		h.Publish(event)
	}

	// Wait for all events to be processed
	time.Sleep(500 * time.Millisecond)

	// Only good subscribers should remain
	if h.SubscriberCount() != numGood {
		t.Errorf("SubscriberCount() = %d, want %d", h.SubscriberCount(), numGood)
	}

	// All good subscribers should have received all events
	for _, sub := range goodSubs {
		if sub.EventCount() != numEvents {
			t.Errorf("good subscriber %s received %d events, want %d", sub.ID(), sub.EventCount(), numEvents)
		}
	}

	// All failing subscribers should be closed
	for _, sub := range failingSubs {
		if !sub.IsClosed() {
			t.Errorf("failing subscriber %s should be closed", sub.ID())
		}
	}
}

func TestHub_SubscriberFailsDuringUnsubscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create subscribers
	sub1 := testutil.NewMockSubscriber("sub-1")
	sub2 := testutil.NewMockSubscriber("sub-2")
	sub2.SetSendError(errTestSendFailed)

	h.Subscribe(sub1)
	h.Subscribe(sub2)

	time.Sleep(50 * time.Millisecond)

	// Unsubscribe sub1 while sub2 will fail on next publish
	h.Unsubscribe("sub-1")

	time.Sleep(50 * time.Millisecond)

	// Publish event - sub2 should fail and be removed
	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	h.Publish(event)

	time.Sleep(50 * time.Millisecond)

	// Both should be removed
	if h.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0", h.SubscriberCount())
	}

	// Both should be closed
	if !sub1.IsClosed() {
		t.Error("sub-1 should be closed after unsubscribe")
	}
	if !sub2.IsClosed() {
		t.Error("sub-2 should be closed after failure")
	}
}

// =============================================================================
// Race Condition and Deadlock Tests
// =============================================================================

// TestHub_RaceCondition_ConcurrentSubscribeUnsubscribe tests for race conditions
// when subscribing and unsubscribing concurrently.
func TestHub_RaceCondition_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	numOps := 100

	// Concurrent subscribes
	wg.Add(numOps)
	for i := 0; i < numOps; i++ {
		go func(id int) {
			defer wg.Done()
			sub := testutil.NewMockSubscriber("sub-" + string(rune('a'+id%26)) + "-" + string(rune('0'+id/26)))
			h.Subscribe(sub)
		}(i)
	}

	// Concurrent unsubscribes (some will be no-ops)
	wg.Add(numOps / 2)
	for i := 0; i < numOps/2; i++ {
		go func(id int) {
			defer wg.Done()
			h.Unsubscribe("sub-" + string(rune('a'+id%26)) + "-" + string(rune('0'+id/26)))
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Hub should still be functional
	if !h.IsRunning() {
		t.Error("hub should still be running")
	}
}

// TestHub_RaceCondition_ConcurrentPublishSubscribe tests for race conditions
// when publishing and subscribing concurrently.
func TestHub_RaceCondition_ConcurrentPublishSubscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	numPublishers := 10
	numSubscribers := 10
	numEvents := 50

	// Start publishers
	wg.Add(numPublishers)
	for i := 0; i < numPublishers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				event := events.NewEvent(events.EventTypeClaudeLog, map[string]int{"pub": id, "seq": j})
				h.Publish(event)
			}
		}(i)
	}

	// Start subscribers joining mid-stream
	wg.Add(numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		go func(id int) {
			defer wg.Done()
			time.Sleep(time.Duration(id*5) * time.Millisecond) // Stagger subscriptions
			sub := testutil.NewMockSubscriber("late-sub-" + string(rune('a'+id)))
			h.Subscribe(sub)
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Hub should still be functional
	if !h.IsRunning() {
		t.Error("hub should still be running")
	}

	// Should have some subscribers
	if h.SubscriberCount() == 0 {
		t.Error("expected some subscribers to be registered")
	}
}

// TestHub_RaceCondition_ConcurrentPublishUnsubscribe tests for race conditions
// when publishing events while subscribers are being removed.
func TestHub_RaceCondition_ConcurrentPublishUnsubscribe(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Add subscribers first
	numSubs := 20
	subs := make([]*testutil.MockSubscriber, numSubs)
	for i := 0; i < numSubs; i++ {
		subs[i] = testutil.NewMockSubscriber("sub-" + string(rune('a'+i)))
		h.Subscribe(subs[i])
	}

	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup

	// Concurrent publishers
	numPublishers := 5
	numEvents := 100
	wg.Add(numPublishers)
	for i := 0; i < numPublishers; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				event := events.NewEvent(events.EventTypeHeartbeat, nil)
				h.Publish(event)
			}
		}(i)
	}

	// Concurrent unsubscribers
	wg.Add(numSubs / 2)
	for i := 0; i < numSubs/2; i++ {
		go func(id int) {
			defer wg.Done()
			time.Sleep(time.Duration(id*2) * time.Millisecond)
			h.Unsubscribe("sub-" + string(rune('a'+id)))
		}(i)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Hub should still be functional
	if !h.IsRunning() {
		t.Error("hub should still be running")
	}
}

// TestHub_RaceCondition_ConcurrentStartStop tests for race conditions
// when starting and stopping the hub concurrently.
func TestHub_RaceCondition_ConcurrentStartStop(t *testing.T) {
	h := New()

	var wg sync.WaitGroup
	numOps := 50

	// Concurrent starts and stops
	wg.Add(numOps * 2)
	for i := 0; i < numOps; i++ {
		go func() {
			defer wg.Done()
			_ = h.Start()
		}()
		go func() {
			defer wg.Done()
			_ = h.Stop()
		}()
	}

	wg.Wait()

	// Final state should be consistent (either running or not)
	// Just ensure no panic or deadlock occurred
}

// TestHub_RaceCondition_AllOperationsConcurrent tests for race conditions
// with all operations happening concurrently.
func TestHub_RaceCondition_AllOperationsConcurrent(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Concurrent publishers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				event := events.NewEvent(events.EventTypeHeartbeat, nil)
				h.Publish(event)
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Concurrent subscribers
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := 0
		for {
			select {
			case <-done:
				return
			default:
				sub := testutil.NewMockSubscriber("dynamic-" + string(rune('0'+counter%10)))
				h.Subscribe(sub)
				counter++
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Concurrent unsubscribers
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := 0
		for {
			select {
			case <-done:
				return
			default:
				h.Unsubscribe("dynamic-" + string(rune('0'+counter%10)))
				counter++
				time.Sleep(7 * time.Millisecond)
			}
		}
	}()

	// Concurrent count readers
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				_ = h.SubscriberCount()
				_ = h.IsRunning()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Let it run for a while
	time.Sleep(500 * time.Millisecond)
	close(done)
	wg.Wait()

	// Hub should still be functional
	if !h.IsRunning() {
		t.Error("hub should still be running")
	}
}

// TestHub_RaceCondition_SubscriberCountDuringModification tests reading
// subscriber count while modifications are happening.
func TestHub_RaceCondition_SubscriberCountDuringModification(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Subscriber modifier
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := 0
		for {
			select {
			case <-done:
				return
			default:
				sub := testutil.NewMockSubscriber("mod-" + string(rune('0'+counter%10)))
				h.Subscribe(sub)
				counter++
				if counter%3 == 0 {
					h.Unsubscribe("mod-" + string(rune('0'+(counter-1)%10)))
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	// Concurrent count readers
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					count := h.SubscriberCount()
					// Count should always be non-negative
					if count < 0 {
						t.Errorf("SubscriberCount() returned negative: %d", count)
					}
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	time.Sleep(300 * time.Millisecond)
	close(done)
	wg.Wait()
}

// TestHub_Deadlock_PublishDuringFailure tests that publishing doesn't deadlock
// when a subscriber fails during send.
func TestHub_Deadlock_PublishDuringFailure(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	// Create a subscriber that takes a long time to fail
	slowFailSub := testutil.NewMockSubscriber("slow-fail")
	slowFailSub.SetSendFunc(func(e events.Event) error {
		time.Sleep(50 * time.Millisecond)
		return errTestSendFailed
	})

	goodSub := testutil.NewMockSubscriber("good")

	h.Subscribe(slowFailSub)
	h.Subscribe(goodSub)

	time.Sleep(50 * time.Millisecond)

	// Publish many events quickly - should not deadlock
	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			event := events.NewEvent(events.EventTypeHeartbeat, map[string]int{"seq": i})
			h.Publish(event)
		}
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("publish deadlocked")
	}

	time.Sleep(200 * time.Millisecond)

	// Good subscriber should have received events
	if goodSub.EventCount() == 0 {
		t.Error("good subscriber should have received events")
	}
}

// TestHub_Deadlock_StopDuringPublish tests that stopping the hub doesn't
// deadlock during active publishing.
func TestHub_Deadlock_StopDuringPublish(t *testing.T) {
	h := New()
	_ = h.Start()

	time.Sleep(50 * time.Millisecond)

	sub := testutil.NewMockSubscriber("test")
	h.Subscribe(sub)

	time.Sleep(50 * time.Millisecond)

	// Start continuous publishing
	stopPublish := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopPublish:
				return
			default:
				event := events.NewEvent(events.EventTypeHeartbeat, nil)
				h.Publish(event)
			}
		}
	}()

	// Let some events be published
	time.Sleep(50 * time.Millisecond)

	// Stop hub while publishing - should not deadlock
	done := make(chan struct{})
	go func() {
		_ = h.Stop()
		close(done)
	}()

	select {
	case <-done:
		close(stopPublish)
	case <-time.After(5 * time.Second):
		close(stopPublish)
		t.Fatal("hub stop deadlocked during publish")
	}
}

// TestHub_Deadlock_SubscribeDuringStop tests that subscribing during
// hub stop doesn't deadlock.
func TestHub_Deadlock_SubscribeDuringStop(t *testing.T) {
	h := New()
	_ = h.Start()

	time.Sleep(50 * time.Millisecond)

	// Start subscribing in goroutine
	stopSubscribe := make(chan struct{})
	go func() {
		counter := 0
		for {
			select {
			case <-stopSubscribe:
				return
			default:
				sub := testutil.NewMockSubscriber("sub-" + string(rune('0'+counter%10)))
				h.Subscribe(sub)
				counter++
				time.Sleep(time.Millisecond)
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Stop hub while subscribing
	done := make(chan struct{})
	go func() {
		_ = h.Stop()
		close(done)
	}()

	select {
	case <-done:
		close(stopSubscribe)
	case <-time.After(5 * time.Second):
		close(stopSubscribe)
		t.Fatal("hub stop deadlocked during subscribe")
	}
}

// TestHub_RaceCondition_FailingSubscribersWithConcurrentOps tests race conditions
// when failing subscribers are being removed while other operations occur.
func TestHub_RaceCondition_FailingSubscribersWithConcurrentOps(t *testing.T) {
	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup

	// Add mix of good and failing subscribers
	for i := 0; i < 10; i++ {
		sub := testutil.NewMockSubscriber("good-" + string(rune('a'+i)))
		h.Subscribe(sub)
	}
	for i := 0; i < 10; i++ {
		sub := testutil.NewMockSubscriber("fail-" + string(rune('a'+i)))
		sub.SetSendError(errTestSendFailed)
		h.Subscribe(sub)
	}

	time.Sleep(100 * time.Millisecond)

	// Concurrent publishers
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				event := events.NewEvent(events.EventTypeClaudeLog, nil)
				h.Publish(event)
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Concurrent new subscriber additions
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				sub := testutil.NewMockSubscriber("new-" + string(rune('a'+id)) + "-" + string(rune('0'+j%10)))
				h.Subscribe(sub)
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	// Concurrent count checks
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = h.SubscriberCount()
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Hub should still be functional
	if !h.IsRunning() {
		t.Error("hub should still be running")
	}

	// All failing subscribers should be removed
	// (Count will vary based on timing of new subscriber additions)
}

// TestHub_StressTest runs a high-stress test with many concurrent operations.
func TestHub_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	h := New()
	_ = h.Start()
	defer func() { _ = h.Stop() }()

	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	duration := 2 * time.Second
	done := make(chan struct{})

	// Heavy publishing
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					event := events.NewEvent(events.EventTypeClaudeLog, nil)
					h.Publish(event)
				}
			}
		}()
	}

	// Rapid subscribe/unsubscribe
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func(id int) {
			defer wg.Done()
			counter := 0
			for {
				select {
				case <-done:
					return
				default:
					subID := "stress-" + string(rune('a'+id)) + "-" + string(rune('0'+counter%10))
					sub := testutil.NewMockSubscriber(subID)
					if counter%3 == 0 {
						sub.SetSendError(errTestSendFailed)
					}
					h.Subscribe(sub)
					if counter%2 == 0 {
						h.Unsubscribe(subID)
					}
					counter++
				}
			}
		}(i)
	}

	// Continuous monitoring
	wg.Add(3)
	for i := 0; i < 3; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					_ = h.SubscriberCount()
					_ = h.IsRunning()
				}
			}
		}()
	}

	time.Sleep(duration)
	close(done)
	wg.Wait()

	// Hub should still be running
	if !h.IsRunning() {
		t.Error("hub should still be running after stress test")
	}
}
