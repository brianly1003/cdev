// Package rpc provides a JSON-RPC 2.0 server implementation.
package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/rpc/transport"
	"github.com/rs/zerolog/log"
)

// Server handles JSON-RPC communication over a transport.
type Server struct {
	dispatcher *handler.Dispatcher
	hub        ports.EventHub

	// clients tracks active client connections
	clients   map[string]*Client
	clientsMu sync.RWMutex

	// done signals server shutdown
	done chan struct{}
}

// NewServer creates a new RPC server.
func NewServer(dispatcher *handler.Dispatcher, hub ports.EventHub) *Server {
	return &Server{
		dispatcher: dispatcher,
		hub:        hub,
		clients:    make(map[string]*Client),
		done:       make(chan struct{}),
	}
}

// ServeTransport handles a single transport connection.
// This method blocks until the transport is closed or the server is stopped.
func (s *Server) ServeTransport(ctx context.Context, t transport.Transport) error {
	client := NewClient(t, s.dispatcher)

	// Register client
	s.clientsMu.Lock()
	s.clients[t.ID()] = client
	s.clientsMu.Unlock()

	// Subscribe to events using adapter
	if s.hub != nil {
		adapter := NewEventAdapter(client)
		s.hub.Subscribe(adapter)
	}

	log.Debug().
		Str("client_id", t.ID()).
		Msg("RPC client connected")

	// Handle requests
	err := client.Serve(ctx)

	// Cleanup
	s.clientsMu.Lock()
	delete(s.clients, t.ID())
	s.clientsMu.Unlock()

	if s.hub != nil {
		s.hub.Unsubscribe(t.ID())
	}

	log.Debug().
		Str("client_id", t.ID()).
		Err(err).
		Msg("RPC client disconnected")

	return err
}

// Stop gracefully stops the server.
func (s *Server) Stop() error {
	close(s.done)

	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()

	for _, client := range s.clients {
		client.Close()
	}
	s.clients = make(map[string]*Client)

	return nil
}

// ClientCount returns the number of connected clients.
func (s *Server) ClientCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

// Broadcast sends a notification to all connected clients.
func (s *Server) Broadcast(method string, params interface{}) error {
	notification, err := message.NewNotification(method, params)
	if err != nil {
		return err
	}
	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}

	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, client := range s.clients {
		if err := client.Send(data); err != nil {
			log.Warn().
				Str("client_id", client.ID()).
				Err(err).
				Msg("failed to send notification")
		}
	}

	return nil
}

// Client represents a connected RPC client.
type Client struct {
	transport  transport.Transport
	dispatcher *handler.Dispatcher

	// send is a buffered channel for outgoing messages
	send chan []byte

	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

// NewClient creates a new RPC client.
func NewClient(t transport.Transport, dispatcher *handler.Dispatcher) *Client {
	return &Client{
		transport:  t,
		dispatcher: dispatcher,
		send:       make(chan []byte, 256),
		done:       make(chan struct{}),
	}
}

// ID returns the client's unique identifier.
func (c *Client) ID() string {
	return c.transport.ID()
}

// Serve handles the client's message loop.
// This method blocks until the client disconnects.
func (c *Client) Serve(ctx context.Context) error {
	// Start writer goroutine
	go c.writeLoop(ctx)

	// Read loop (main goroutine)
	return c.readLoop(ctx)
}

// readLoop reads and processes incoming messages.
func (c *Client) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.done:
			return nil
		case <-c.transport.Done():
			return transport.ErrTransportClosed
		default:
			data, err := c.transport.Read(ctx)
			if err != nil {
				if errors.Is(err, transport.ErrTransportClosed) {
					return nil
				}
				return err
			}

			// Process request and send response
			go c.handleRequest(ctx, data)
		}
	}
}

// handleRequest processes a single request.
func (c *Client) handleRequest(ctx context.Context, data []byte) {
	response, err := c.dispatcher.HandleMessage(ctx, data)
	if err != nil {
		log.Warn().
			Str("client_id", c.ID()).
			Err(err).
			Msg("failed to handle message")
		return
	}

	// Don't send empty responses (notifications don't have responses)
	if len(response) == 0 {
		return
	}

	if err := c.Send(response); err != nil {
		log.Warn().
			Str("client_id", c.ID()).
			Err(err).
			Msg("failed to send response")
	}
}

// writeLoop sends messages to the transport.
func (c *Client) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case data := <-c.send:
			if err := c.transport.Write(ctx, data); err != nil {
				log.Warn().
					Str("client_id", c.ID()).
					Err(err).
					Msg("write error")
				return
			}
		}
	}
}

// Send queues a message for sending.
func (c *Client) Send(data []byte) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("client closed")
	}
	c.mu.Unlock()

	select {
	case c.send <- data:
		return nil
	default:
		return errors.New("send buffer full")
	}
}

// SendNotification sends a JSON-RPC notification.
func (c *Client) SendNotification(method string, params interface{}) error {
	notification, err := message.NewNotification(method, params)
	if err != nil {
		return err
	}
	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	return c.Send(data)
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()

	return c.transport.Close()
}

// Done returns a channel that's closed when the client is done.
func (c *Client) Done() <-chan struct{} {
	return c.done
}

// --- Subscriber interface implementation ---
// Client implements ports.Subscriber to receive events from EventHub.

// Send implements ports.Subscriber.Send.
// Converts events to JSON-RPC notifications.
func (c *Client) SendEvent(event events.Event) error {
	// Convert event to JSON-RPC notification
	method := "event/" + string(event.Type())

	// Extract payload from event
	var params interface{}
	data, err := event.ToJSON()
	if err != nil {
		return err
	}

	// Parse the event JSON to extract payload
	var eventData struct {
		Event     string      `json:"event"`
		Timestamp string      `json:"timestamp"`
		Payload   interface{} `json:"payload"`
	}
	if err := json.Unmarshal(data, &eventData); err != nil {
		return err
	}
	params = eventData.Payload

	return c.SendNotification(method, params)
}

// EventAdapter wraps a Client to implement ports.Subscriber.
type EventAdapter struct {
	client *Client
}

// NewEventAdapter creates a new event adapter.
func NewEventAdapter(client *Client) *EventAdapter {
	return &EventAdapter{client: client}
}

// ID implements ports.Subscriber.
func (a *EventAdapter) ID() string {
	return a.client.ID()
}

// Send implements ports.Subscriber.
func (a *EventAdapter) Send(event events.Event) error {
	return a.client.SendEvent(event)
}

// Close implements ports.Subscriber.
func (a *EventAdapter) Close() error {
	return a.client.Close()
}

// Done implements ports.Subscriber.
func (a *EventAdapter) Done() <-chan struct{} {
	return a.client.Done()
}

// Ensure EventAdapter implements ports.Subscriber
var _ ports.Subscriber = (*EventAdapter)(nil)
