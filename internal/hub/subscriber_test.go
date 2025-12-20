package hub

import (
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain"
	"github.com/brianly1003/cdev/internal/domain/events"
)

func TestNewChannelSubscriber(t *testing.T) {
	sub := NewChannelSubscriber("test-1", 10)

	if sub == nil {
		t.Fatal("NewChannelSubscriber() returned nil")
	}
	if sub.ID() != "test-1" {
		t.Errorf("ID() = %q, want test-1", sub.ID())
	}
	if sub.closed {
		t.Error("subscriber should not be closed initially")
	}
	if sub.send == nil {
		t.Error("send channel should not be nil")
	}
	if sub.done == nil {
		t.Error("done channel should not be nil")
	}
}

func TestChannelSubscriber_ID(t *testing.T) {
	tests := []struct {
		id string
	}{
		{"subscriber-1"},
		{"ws-client-abc"},
		{""},
		{"a-very-long-subscriber-id-12345"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			sub := NewChannelSubscriber(tt.id, 1)
			if sub.ID() != tt.id {
				t.Errorf("ID() = %q, want %q", sub.ID(), tt.id)
			}
		})
	}
}

func TestChannelSubscriber_Send(t *testing.T) {
	sub := NewChannelSubscriber("test", 10)

	event := events.NewEvent(events.EventTypeHeartbeat, nil)
	err := sub.Send(event)

	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	// Verify event was received
	select {
	case received := <-sub.Events():
		if received.Type() != events.EventTypeHeartbeat {
			t.Errorf("received event type = %v, want %v", received.Type(), events.EventTypeHeartbeat)
		}
	default:
		t.Error("expected event in channel")
	}
}

func TestChannelSubscriber_Send_Multiple(t *testing.T) {
	sub := NewChannelSubscriber("test", 100)

	for i := 0; i < 50; i++ {
		event := events.NewEvent(events.EventTypeClaudeLog, map[string]int{"seq": i})
		if err := sub.Send(event); err != nil {
			t.Errorf("Send() error on event %d: %v", i, err)
		}
	}

	// Verify all events were received
	count := 0
	for count < 50 {
		select {
		case <-sub.Events():
			count++
		default:
			t.Errorf("expected 50 events, got %d", count)
			return
		}
	}
}

func TestChannelSubscriber_Send_BufferFull(t *testing.T) {
	sub := NewChannelSubscriber("test", 2)

	// Fill the buffer
	sub.Send(events.NewEvent(events.EventTypeHeartbeat, nil))
	sub.Send(events.NewEvent(events.EventTypeHeartbeat, nil))

	// Next send should fail (buffer full)
	err := sub.Send(events.NewEvent(events.EventTypeHeartbeat, nil))
	if err != domain.ErrSubscriberClosed {
		t.Errorf("Send() error = %v, want ErrSubscriberClosed", err)
	}
}

func TestChannelSubscriber_Send_AfterClose(t *testing.T) {
	sub := NewChannelSubscriber("test", 10)
	sub.Close()

	err := sub.Send(events.NewEvent(events.EventTypeHeartbeat, nil))
	if err != domain.ErrSubscriberClosed {
		t.Errorf("Send() after close error = %v, want ErrSubscriberClosed", err)
	}
}

func TestChannelSubscriber_Close(t *testing.T) {
	sub := NewChannelSubscriber("test", 10)

	// First close should succeed
	err := sub.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
	if !sub.closed {
		t.Error("subscriber should be closed")
	}

	// Second close should be idempotent
	err = sub.Close()
	if err != nil {
		t.Errorf("second Close() error = %v, want nil", err)
	}
}

func TestChannelSubscriber_Done(t *testing.T) {
	sub := NewChannelSubscriber("test", 10)

	done := sub.Done()
	if done == nil {
		t.Fatal("Done() returned nil")
	}

	// Done channel should not be closed initially
	select {
	case <-done:
		t.Error("Done channel should not be closed initially")
	default:
		// expected
	}

	// Close subscriber
	sub.Close()

	// Done channel should be closed now
	select {
	case <-done:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Close()")
	}
}

func TestChannelSubscriber_Events(t *testing.T) {
	sub := NewChannelSubscriber("test", 10)

	eventsChan := sub.Events()
	if eventsChan == nil {
		t.Fatal("Events() returned nil")
	}

	// Send an event and verify it's on the Events channel
	event := events.NewEvent(events.EventTypeFileChanged, nil)
	sub.Send(event)

	select {
	case received := <-eventsChan:
		if received == nil {
			t.Error("received nil event")
		}
	default:
		t.Error("expected event on Events channel")
	}
}

func TestChannelSubscriber_Concurrent(t *testing.T) {
	sub := NewChannelSubscriber("test", 1000)
	var wg sync.WaitGroup

	// Concurrent senders
	numSenders := 10
	eventsPerSender := 100

	wg.Add(numSenders)
	for i := 0; i < numSenders; i++ {
		go func(senderID int) {
			defer wg.Done()
			for j := 0; j < eventsPerSender; j++ {
				event := events.NewEvent(events.EventTypeClaudeLog, map[string]int{"sender": senderID, "seq": j})
				sub.Send(event)
			}
		}(i)
	}

	wg.Wait()

	// Drain events
	count := 0
	for {
		select {
		case <-sub.Events():
			count++
		default:
			goto done
		}
	}
done:

	expected := numSenders * eventsPerSender
	if count != expected {
		t.Errorf("received %d events, want %d", count, expected)
	}
}

// LogSubscriber tests

func TestNewLogSubscriber(t *testing.T) {
	logFn := func(e events.Event) {}
	sub := NewLogSubscriber("log-1", logFn)

	if sub == nil {
		t.Fatal("NewLogSubscriber() returned nil")
	}
	if sub.ID() != "log-1" {
		t.Errorf("ID() = %q, want log-1", sub.ID())
	}
	if sub.closed {
		t.Error("subscriber should not be closed initially")
	}
	if sub.logFn == nil {
		t.Error("logFn should not be nil")
	}
}

func TestLogSubscriber_ID(t *testing.T) {
	sub := NewLogSubscriber("logger-xyz", nil)
	if sub.ID() != "logger-xyz" {
		t.Errorf("ID() = %q, want logger-xyz", sub.ID())
	}
}

func TestLogSubscriber_Send(t *testing.T) {
	var received events.Event
	logFn := func(e events.Event) {
		received = e
	}

	sub := NewLogSubscriber("log", logFn)
	event := events.NewEvent(events.EventTypeGitDiff, map[string]string{"file": "test.go"})

	err := sub.Send(event)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}

	if received == nil {
		t.Fatal("logFn was not called")
	}
	if received.Type() != events.EventTypeGitDiff {
		t.Errorf("received event type = %v, want %v", received.Type(), events.EventTypeGitDiff)
	}
}

func TestLogSubscriber_Send_NilLogFn(t *testing.T) {
	sub := NewLogSubscriber("log", nil)
	event := events.NewEvent(events.EventTypeHeartbeat, nil)

	// Should not panic even with nil logFn
	err := sub.Send(event)
	if err != nil {
		t.Errorf("Send() error = %v, want nil", err)
	}
}

func TestLogSubscriber_Send_AfterClose(t *testing.T) {
	sub := NewLogSubscriber("log", func(e events.Event) {})
	sub.Close()

	err := sub.Send(events.NewEvent(events.EventTypeHeartbeat, nil))
	if err != domain.ErrSubscriberClosed {
		t.Errorf("Send() after close error = %v, want ErrSubscriberClosed", err)
	}
}

func TestLogSubscriber_Close(t *testing.T) {
	sub := NewLogSubscriber("log", nil)

	// First close should succeed
	err := sub.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
	if !sub.closed {
		t.Error("subscriber should be closed")
	}

	// Second close should be idempotent
	err = sub.Close()
	if err != nil {
		t.Errorf("second Close() error = %v, want nil", err)
	}
}

func TestLogSubscriber_Done(t *testing.T) {
	sub := NewLogSubscriber("log", nil)

	done := sub.Done()
	if done == nil {
		t.Fatal("Done() returned nil")
	}

	// Done channel should not be closed initially
	select {
	case <-done:
		t.Error("Done channel should not be closed initially")
	default:
		// expected
	}

	// Close subscriber
	sub.Close()

	// Done channel should be closed now
	select {
	case <-done:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Error("Done channel should be closed after Close()")
	}
}

func TestLogSubscriber_Send_Multiple(t *testing.T) {
	var count int
	var mu sync.Mutex
	logFn := func(e events.Event) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	sub := NewLogSubscriber("log", logFn)

	for i := 0; i < 100; i++ {
		sub.Send(events.NewEvent(events.EventTypeClaudeLog, nil))
	}

	if count != 100 {
		t.Errorf("logFn called %d times, want 100", count)
	}
}

// Benchmark tests

func BenchmarkChannelSubscriber_Send(b *testing.B) {
	sub := NewChannelSubscriber("bench", b.N+100)
	event := events.NewEvent(events.EventTypeHeartbeat, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sub.Send(event)
	}
}

func BenchmarkLogSubscriber_Send(b *testing.B) {
	sub := NewLogSubscriber("bench", func(e events.Event) {})
	event := events.NewEvent(events.EventTypeHeartbeat, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sub.Send(event)
	}
}
