package testutil

import (
	"errors"
	"testing"

	"github.com/brianly1003/cdev/internal/domain/events"
)

// --- MockSubscriber Tests ---

func TestNewMockSubscriber(t *testing.T) {
	sub := NewMockSubscriber("test-sub")

	if sub.ID() != "test-sub" {
		t.Errorf("expected ID test-sub, got %s", sub.ID())
	}
	if sub.EventCount() != 0 {
		t.Errorf("expected 0 events, got %d", sub.EventCount())
	}
	if sub.IsClosed() {
		t.Error("expected subscriber to not be closed initially")
	}
}

func TestMockSubscriber_Send(t *testing.T) {
	sub := NewMockSubscriber("test-sub")

	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	err := sub.Send(event)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if sub.EventCount() != 1 {
		t.Errorf("expected 1 event, got %d", sub.EventCount())
	}

	evts := sub.Events()
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0].Type() != events.EventTypeHeartbeat {
		t.Errorf("expected heartbeat event, got %s", evts[0].Type())
	}
}

func TestMockSubscriber_SendWithError(t *testing.T) {
	sub := NewMockSubscriber("test-sub")
	expectedErr := errors.New("send failed")
	sub.SetSendError(expectedErr)

	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	err := sub.Send(event)

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
	// Event should not be recorded when error occurs
	if sub.EventCount() != 0 {
		t.Errorf("expected 0 events when error, got %d", sub.EventCount())
	}
}

func TestMockSubscriber_SendWithCustomFunc(t *testing.T) {
	sub := NewMockSubscriber("test-sub")

	callCount := 0
	sub.SetSendFunc(func(e events.Event) error {
		callCount++
		return nil
	})

	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	sub.Send(event)
	sub.Send(event)

	if callCount != 2 {
		t.Errorf("expected sendFunc called 2 times, got %d", callCount)
	}
}

func TestMockSubscriber_Close(t *testing.T) {
	sub := NewMockSubscriber("test-sub")

	err := sub.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !sub.IsClosed() {
		t.Error("expected subscriber to be closed")
	}

	// Second close should be safe
	err = sub.Close()
	if err != nil {
		t.Errorf("unexpected error on second close: %v", err)
	}
}

func TestMockSubscriber_Done(t *testing.T) {
	sub := NewMockSubscriber("test-sub")

	// Done channel should be open initially
	select {
	case <-sub.Done():
		t.Error("Done channel should not be closed initially")
	default:
		// Expected
	}

	sub.Close()

	// Done channel should be closed after Close()
	select {
	case <-sub.Done():
		// Expected
	default:
		t.Error("Done channel should be closed after Close()")
	}
}

func TestMockSubscriber_ClearEvents(t *testing.T) {
	sub := NewMockSubscriber("test-sub")

	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	sub.Send(event)
	sub.Send(event)
	sub.Send(event)

	if sub.EventCount() != 3 {
		t.Fatalf("expected 3 events, got %d", sub.EventCount())
	}

	sub.ClearEvents()

	if sub.EventCount() != 0 {
		t.Errorf("expected 0 events after clear, got %d", sub.EventCount())
	}
}

// --- MockEventHub Tests ---

func TestNewMockEventHub(t *testing.T) {
	hub := NewMockEventHub()

	if hub.IsRunning() {
		t.Error("hub should not be running initially")
	}
	if hub.SubscriberCount() != 0 {
		t.Errorf("expected 0 subscribers, got %d", hub.SubscriberCount())
	}
}

func TestMockEventHub_StartStop(t *testing.T) {
	hub := NewMockEventHub()

	err := hub.Start()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !hub.IsRunning() {
		t.Error("hub should be running after Start()")
	}

	err = hub.Stop()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if hub.IsRunning() {
		t.Error("hub should not be running after Stop()")
	}
}

func TestMockEventHub_Publish(t *testing.T) {
	hub := NewMockEventHub()

	event1 := events.NewEvent(events.EventTypeHeartbeat, nil)
	event2 := events.NewEvent(events.EventTypeClaudeLog, "test log")

	hub.Publish(event1)
	hub.Publish(event2)

	evts := hub.PublishedEvents()
	if len(evts) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evts))
	}
	if evts[0].Type() != events.EventTypeHeartbeat {
		t.Errorf("expected heartbeat, got %s", evts[0].Type())
	}
	if evts[1].Type() != events.EventTypeClaudeLog {
		t.Errorf("expected claude_log, got %s", evts[1].Type())
	}
}

func TestMockEventHub_Subscribe(t *testing.T) {
	hub := NewMockEventHub()
	sub := NewMockSubscriber("sub-1")

	hub.Subscribe(sub)

	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber, got %d", hub.SubscriberCount())
	}
}

func TestMockEventHub_Unsubscribe(t *testing.T) {
	hub := NewMockEventHub()
	sub1 := NewMockSubscriber("sub-1")
	sub2 := NewMockSubscriber("sub-2")

	hub.Subscribe(sub1)
	hub.Subscribe(sub2)

	if hub.SubscriberCount() != 2 {
		t.Fatalf("expected 2 subscribers, got %d", hub.SubscriberCount())
	}

	hub.Unsubscribe("sub-1")

	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber after unsubscribe, got %d", hub.SubscriberCount())
	}

	// Unsubscribe non-existent should be safe
	hub.Unsubscribe("non-existent")
	if hub.SubscriberCount() != 1 {
		t.Errorf("expected 1 subscriber after unsubscribe non-existent, got %d", hub.SubscriberCount())
	}
}

// --- Assertion Helper Tests ---

func TestAssertEqual(t *testing.T) {
	mockT := &testing.T{}
	AssertEqual(mockT, 5, 5, "should be equal")
	if mockT.Failed() {
		t.Error("AssertEqual should pass for equal values")
	}
}

func TestAssertTrue(t *testing.T) {
	mockT := &testing.T{}
	AssertTrue(mockT, true, "should be true")
	if mockT.Failed() {
		t.Error("AssertTrue should pass for true condition")
	}
}

func TestAssertFalse(t *testing.T) {
	mockT := &testing.T{}
	AssertFalse(mockT, false, "should be false")
	if mockT.Failed() {
		t.Error("AssertFalse should pass for false condition")
	}
}

func TestAssertNil(t *testing.T) {
	mockT := &testing.T{}
	AssertNil(mockT, nil, "should be nil")
	if mockT.Failed() {
		t.Error("AssertNil should pass for nil value")
	}
}

func TestAssertNotNil(t *testing.T) {
	mockT := &testing.T{}
	AssertNotNil(mockT, "not nil", "should not be nil")
	if mockT.Failed() {
		t.Error("AssertNotNil should pass for non-nil value")
	}
}

func TestAssertNoError(t *testing.T) {
	mockT := &testing.T{}
	AssertNoError(mockT, nil, "should have no error")
	if mockT.Failed() {
		t.Error("AssertNoError should pass for nil error")
	}
}

func TestAssertError(t *testing.T) {
	mockT := &testing.T{}
	AssertError(mockT, errors.New("test error"), "should have error")
	if mockT.Failed() {
		t.Error("AssertError should pass for non-nil error")
	}
}

func TestAssertContains(t *testing.T) {
	mockT := &testing.T{}
	AssertContains(mockT, "hello world", "world", "should contain substring")
	if mockT.Failed() {
		t.Error("AssertContains should pass when substring is found")
	}

	// Empty substring should always pass
	mockT2 := &testing.T{}
	AssertContains(mockT2, "any string", "", "empty substring")
	if mockT2.Failed() {
		t.Error("AssertContains should pass for empty substring")
	}
}
