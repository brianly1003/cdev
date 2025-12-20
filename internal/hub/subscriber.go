package hub

import (
	"github.com/brianly1003/cdev/internal/domain"
	"github.com/brianly1003/cdev/internal/domain/events"
)

// ChannelSubscriber is a subscriber that sends events to a channel.
type ChannelSubscriber struct {
	id      string
	send    chan events.Event
	done    chan struct{}
	closed  bool
}

// NewChannelSubscriber creates a new channel-based subscriber.
func NewChannelSubscriber(id string, bufferSize int) *ChannelSubscriber {
	return &ChannelSubscriber{
		id:   id,
		send: make(chan events.Event, bufferSize),
		done: make(chan struct{}),
	}
}

// ID returns the subscriber's unique identifier.
func (s *ChannelSubscriber) ID() string {
	return s.id
}

// Send sends an event to the subscriber.
func (s *ChannelSubscriber) Send(event events.Event) error {
	if s.closed {
		return domain.ErrSubscriberClosed
	}

	select {
	case s.send <- event:
		return nil
	default:
		// Channel full, subscriber is too slow
		return domain.ErrSubscriberClosed
	}
}

// Close closes the subscriber.
func (s *ChannelSubscriber) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	close(s.send)
	return nil
}

// Done returns a channel that's closed when the subscriber is done.
func (s *ChannelSubscriber) Done() <-chan struct{} {
	return s.done
}

// Events returns the channel to receive events from.
func (s *ChannelSubscriber) Events() <-chan events.Event {
	return s.send
}

// LogSubscriber is a subscriber that logs events (useful for debugging).
type LogSubscriber struct {
	id     string
	done   chan struct{}
	closed bool
	logFn  func(event events.Event)
}

// NewLogSubscriber creates a new log subscriber.
func NewLogSubscriber(id string, logFn func(event events.Event)) *LogSubscriber {
	return &LogSubscriber{
		id:    id,
		done:  make(chan struct{}),
		logFn: logFn,
	}
}

// ID returns the subscriber's unique identifier.
func (s *LogSubscriber) ID() string {
	return s.id
}

// Send logs the event.
func (s *LogSubscriber) Send(event events.Event) error {
	if s.closed {
		return domain.ErrSubscriberClosed
	}
	if s.logFn != nil {
		s.logFn(event)
	}
	return nil
}

// Close closes the subscriber.
func (s *LogSubscriber) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	return nil
}

// Done returns a channel that's closed when the subscriber is done.
func (s *LogSubscriber) Done() <-chan struct{} {
	return s.done
}
