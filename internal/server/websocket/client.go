// Package websocket provides WebSocket server functionality for real-time
// bidirectional communication between cdev and connected clients
// (mobile apps, desktop apps, or other services).
//
// Architecture:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                      WebSocket Hub                          │
//	│  (Manages all connected clients, broadcasts events)         │
//	└─────────────────────────────┬───────────────────────────────┘
//	                              │
//	          ┌───────────────────┼───────────────────┐
//	          │                   │                   │
//	          ▼                   ▼                   ▼
//	    ┌──────────┐        ┌──────────┐        ┌──────────┐
//	    │ Client 1 │        │ Client 2 │        │ Client N │
//	    │ (iOS)    │        │ (Desktop)│        │ (...)    │
//	    └──────────┘        └──────────┘        └──────────┘
//
// Each Client manages:
//   - A goroutine for reading incoming messages (readPump)
//   - A goroutine for writing outgoing messages (writePump)
//   - Automatic ping/pong for connection health monitoring
//   - Graceful shutdown handling
//
// Message Flow:
//   - Incoming: WebSocket → readPump → CommandHandler → Process
//   - Outgoing: Event Hub → Client.Send() → writePump → WebSocket
//
// Thread Safety:
//   - Send() is safe to call from any goroutine
//   - Close() is safe to call multiple times
//   - Internal state is protected by mutex
package websocket

import (
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Client represents a WebSocket client connection.
//
// A Client wraps a WebSocket connection and provides:
//   - Concurrent-safe message sending via buffered channel
//   - Automatic connection health monitoring via ping/pong
//   - Graceful shutdown with proper close frame handling
//   - Command handling for incoming messages
//
// Lifecycle:
//  1. Create with NewClient()
//  2. Start read/write pumps with Start()
//  3. Send messages with Send()
//  4. Close with Close() or wait for connection to close
//
// Example:
//
//	client := NewClient(conn, handleCommand, onClientClose)
//	client.Start()
//	// ... later ...
//	client.Send([]byte(`{"event":"status","payload":{}}`))
type Client struct {
	id             string
	conn           *websocket.Conn
	send           chan []byte
	done           chan struct{}
	commandHandler CommandHandler
	onClose        func(id string)

	mu     sync.Mutex
	closed bool
}

// NewClient creates a new WebSocket client.
func NewClient(conn *websocket.Conn, commandHandler CommandHandler, onClose func(id string)) *Client {
	return &Client{
		id:             uuid.New().String(),
		conn:           conn,
		send:           make(chan []byte, sendBufferSize),
		done:           make(chan struct{}),
		commandHandler: commandHandler,
		onClose:        onClose,
	}
}

// ID returns the client's unique identifier.
func (c *Client) ID() string {
	return c.id
}

// Start starts the client's read and write pumps.
func (c *Client) Start() {
	go c.writePump()
	go c.readPump()
}

// Send queues a message to be sent to the client.
func (c *Client) Send(message []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	select {
	case c.send <- message:
	default:
		// Channel full, client is too slow
		log.Warn().Str("client_id", c.id).Msg("client send channel full, dropping message")
	}
}

// Close closes the client connection.
func (c *Client) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()

	close(c.done)
}

// readPump pumps messages from the WebSocket connection to the command handler.
func (c *Client) readPump() {
	defer func() {
		c.Close()
		_ = c.conn.Close()
		if c.onClose != nil {
			c.onClose(c.id)
		}
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warn().Err(err).Str("client_id", c.id).Msg("websocket read error")
			}
			return
		}

		// Handle command
		if c.commandHandler != nil {
			c.commandHandler(c.id, message)
		}
	}
}

// writePump pumps messages from the send channel to the WebSocket connection.
// Each message is sent as a separate WebSocket frame to avoid JSON corruption.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		// Send close frame with deadline to prevent blocking on laggy connections
		_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
		_ = c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			// Graceful shutdown requested - defer will handle close frame
			return

		case message, ok := <-c.send:
			if !ok {
				// Send channel closed - defer will handle close frame
				return
			}

			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))

			// Send each message as a separate WebSocket frame
			// This prevents JSON corruption from batching multiple objects
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Debug().Err(err).Str("client_id", c.id).Msg("write error")
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Debug().Err(err).Str("client_id", c.id).Msg("ping error")
				return
			}
		}
	}
}
