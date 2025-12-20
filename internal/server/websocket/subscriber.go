package websocket

import (
	"github.com/brianly1003/cdev/internal/domain"
	"github.com/brianly1003/cdev/internal/domain/events"
)

// ClientSubscriber wraps a WebSocket client as an EventHub subscriber.
type ClientSubscriber struct {
	client *Client
}

// NewClientSubscriber creates a subscriber from a WebSocket client.
func NewClientSubscriber(client *Client) *ClientSubscriber {
	return &ClientSubscriber{client: client}
}

// ID returns the subscriber's unique identifier.
func (s *ClientSubscriber) ID() string {
	return s.client.ID()
}

// Send sends an event to the subscriber.
func (s *ClientSubscriber) Send(event events.Event) error {
	s.client.mu.Lock()
	closed := s.client.closed
	s.client.mu.Unlock()

	if closed {
		return domain.ErrSubscriberClosed
	}

	data, err := event.ToJSON()
	if err != nil {
		return err
	}

	s.client.Send(data)
	return nil
}

// Close closes the subscriber.
func (s *ClientSubscriber) Close() error {
	s.client.Close()
	return nil
}

// Done returns a channel that's closed when the subscriber is done.
func (s *ClientSubscriber) Done() <-chan struct{} {
	return s.client.done
}
