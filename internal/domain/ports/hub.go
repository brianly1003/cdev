package ports

import (
	"github.com/brianly1003/cdev/internal/domain/events"
)

// Subscriber represents an event subscriber.
type Subscriber interface {
	// ID returns a unique identifier for this subscriber.
	ID() string

	// Send sends an event to this subscriber.
	// Returns error if the subscriber is closed or the send fails.
	Send(event events.Event) error

	// Close closes the subscriber.
	Close() error

	// Done returns a channel that's closed when the subscriber is done.
	Done() <-chan struct{}
}

// EventHub defines the contract for event distribution.
type EventHub interface {
	// Start begins the event hub.
	Start() error

	// Stop gracefully stops the hub.
	Stop() error

	// Publish sends an event to all subscribers.
	Publish(event events.Event)

	// Subscribe adds a new subscriber.
	Subscribe(sub Subscriber)

	// Unsubscribe removes a subscriber by ID.
	Unsubscribe(id string)

	// SubscriberCount returns the number of active subscribers.
	SubscriberCount() int
}
