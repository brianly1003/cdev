// Package hub implements the central event hub for cdev.
package hub

import (
	"sync"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/rs/zerolog/log"
)

// Hub is the central event dispatcher that fans out events to all subscribers.
type Hub struct {
	// subscribers holds all active subscribers
	subscribers map[string]ports.Subscriber

	// broadcast channel receives events to be broadcast
	broadcast chan events.Event

	// register channel receives new subscribers
	register chan ports.Subscriber

	// unregister channel receives subscriber IDs to remove
	unregister chan string

	// mu protects subscribers map
	mu sync.RWMutex

	// done signals when the hub should stop
	done chan struct{}

	// running indicates if the hub is running
	running bool
}

// New creates a new Hub.
func New() *Hub {
	return &Hub{
		subscribers: make(map[string]ports.Subscriber),
		broadcast:   make(chan events.Event, 256),
		register:    make(chan ports.Subscriber),
		unregister:  make(chan string),
		done:        make(chan struct{}),
	}
}

// Start begins the hub's main loop.
func (h *Hub) Start() error {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return nil
	}
	h.running = true
	h.mu.Unlock()

	log.Debug().Msg("event hub started")

	go h.run()
	return nil
}

// Stop gracefully stops the hub.
func (h *Hub) Stop() error {
	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return nil
	}
	h.running = false
	h.mu.Unlock()

	close(h.done)

	// Close all subscribers
	h.mu.Lock()
	for _, sub := range h.subscribers {
		_ = sub.Close()
	}
	h.subscribers = make(map[string]ports.Subscriber)
	h.mu.Unlock()

	log.Debug().Msg("event hub stopped")
	return nil
}

// run is the main event loop.
func (h *Hub) run() {
	for {
		select {
		case <-h.done:
			return

		case sub := <-h.register:
			h.mu.Lock()
			h.subscribers[sub.ID()] = sub
			h.mu.Unlock()
			log.Debug().Str("subscriber_id", sub.ID()).Msg("subscriber registered")

		case id := <-h.unregister:
			h.mu.Lock()
			if sub, ok := h.subscribers[id]; ok {
				_ = sub.Close()
				delete(h.subscribers, id)
			}
			h.mu.Unlock()
			log.Debug().Str("subscriber_id", id).Msg("subscriber unregistered")

		case event := <-h.broadcast:
			h.mu.RLock()
			for id, sub := range h.subscribers {
				if err := sub.Send(event); err != nil {
					log.Warn().
						Str("subscriber_id", id).
						Err(err).
						Msg("failed to send event to subscriber")
					// Queue unregister (don't block)
					go func(subID string) {
						select {
						case h.unregister <- subID:
						default:
						}
					}(id)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// Publish sends an event to all subscribers.
func (h *Hub) Publish(event events.Event) {
	select {
	case h.broadcast <- event:
		log.Trace().
			Str("event_type", string(event.Type())).
			Msg("event published")
	default:
		log.Warn().
			Str("event_type", string(event.Type())).
			Msg("event dropped: broadcast channel full")
	}
}

// Subscribe adds a new subscriber.
func (h *Hub) Subscribe(sub ports.Subscriber) {
	select {
	case h.register <- sub:
	case <-h.done:
	}
}

// Unsubscribe removes a subscriber by ID.
func (h *Hub) Unsubscribe(id string) {
	select {
	case h.unregister <- id:
	case <-h.done:
	}
}

// SubscriberCount returns the number of active subscribers.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// IsRunning returns true if the hub is running.
func (h *Hub) IsRunning() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.running
}
