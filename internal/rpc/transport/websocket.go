package transport

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const (
	// Default timeouts for WebSocket operations.
	DefaultWriteTimeout = 15 * time.Second
	DefaultReadTimeout  = 90 * time.Second

	// Default maximum message size (512KB).
	DefaultMaxMessageSize = 512 * 1024

	// Ping interval for keepalive
	DefaultPingInterval = 30 * time.Second
	DefaultPongTimeout  = 60 * time.Second
)

// WebSocketTransport implements Transport over a WebSocket connection.
type WebSocketTransport struct {
	id   string
	conn *websocket.Conn

	writeTimeout time.Duration
	readTimeout  time.Duration

	done   chan struct{}
	mu     sync.Mutex
	closed bool
}

// WebSocketOption configures a WebSocketTransport.
type WebSocketOption func(*WebSocketTransport)

// WithWriteTimeout sets the write timeout for the WebSocket transport.
func WithWriteTimeout(d time.Duration) WebSocketOption {
	return func(t *WebSocketTransport) {
		t.writeTimeout = d
	}
}

// WithReadTimeout sets the read timeout for the WebSocket transport.
func WithReadTimeout(d time.Duration) WebSocketOption {
	return func(t *WebSocketTransport) {
		t.readTimeout = d
	}
}

// WithTransportID sets a custom ID for the transport.
func WithTransportID(id string) WebSocketOption {
	return func(t *WebSocketTransport) {
		t.id = id
	}
}

// NewWebSocketTransport creates a new WebSocket transport.
func NewWebSocketTransport(conn *websocket.Conn, opts ...WebSocketOption) *WebSocketTransport {
	t := &WebSocketTransport{
		id:           uuid.New().String(),
		conn:         conn,
		writeTimeout: DefaultWriteTimeout,
		readTimeout:  DefaultReadTimeout,
		done:         make(chan struct{}),
	}

	for _, opt := range opts {
		opt(t)
	}

	// Set max message size
	conn.SetReadLimit(DefaultMaxMessageSize)

	// Setup ping/pong handlers for keepalive
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(DefaultPongTimeout))
		return nil
	})

	// Start ping ticker
	go t.pingLoop()

	return t
}

// pingLoop sends periodic pings to keep the connection alive.
func (t *WebSocketTransport) pingLoop() {
	ticker := time.NewTicker(DefaultPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			t.mu.Lock()
			if t.closed {
				t.mu.Unlock()
				return
			}
			_ = t.conn.SetWriteDeadline(time.Now().Add(DefaultWriteTimeout))
			if err := t.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				t.mu.Unlock()
				return
			}
			t.mu.Unlock()
		}
	}
}

// ID returns the unique identifier for this transport.
func (t *WebSocketTransport) ID() string {
	return t.id
}

// Read reads the next message from the WebSocket connection.
func (t *WebSocketTransport) Read(ctx context.Context) ([]byte, error) {
	// Check if already closed
	select {
	case <-t.done:
		return nil, ErrTransportClosed
	default:
	}

	// Set read deadline based on context or default timeout
	deadline := time.Now().Add(t.readTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = t.conn.SetReadDeadline(deadline)

	// Read message
	messageType, message, err := t.conn.ReadMessage()
	if err != nil {
		// Check for clean close
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil, io.EOF
		}
		return nil, err
	}

	// Only accept text messages for JSON-RPC
	if messageType != websocket.TextMessage {
		return nil, io.EOF
	}

	return message, nil
}

// Write sends a message through the WebSocket connection.
func (t *WebSocketTransport) Write(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return ErrTransportClosed
	}

	// Set write deadline based on context or default timeout
	deadline := time.Now().Add(t.writeTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = t.conn.SetWriteDeadline(deadline)

	return t.conn.WriteMessage(websocket.TextMessage, data)
}

// Close closes the WebSocket connection.
func (t *WebSocketTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	close(t.done)

	// Send close frame before closing
	_ = t.conn.SetWriteDeadline(time.Now().Add(time.Second))
	_ = t.conn.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	return t.conn.Close()
}

// Done returns a channel that's closed when the transport is closed.
func (t *WebSocketTransport) Done() <-chan struct{} {
	return t.done
}

// Info returns metadata about the WebSocket transport.
func (t *WebSocketTransport) Info() TransportInfo {
	return TransportInfo{
		Type:       "websocket",
		RemoteAddr: t.conn.RemoteAddr().String(),
		LocalAddr:  t.conn.LocalAddr().String(),
	}
}

// Conn returns the underlying WebSocket connection.
// Use with caution as direct access bypasses transport abstractions.
func (t *WebSocketTransport) Conn() *websocket.Conn {
	return t.conn
}
