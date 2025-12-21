// Package transport provides transport layer abstractions for RPC communication.
package transport

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// Common transport errors.
var (
	ErrTransportClosed = errors.New("transport is closed")
	ErrWriteTimeout    = errors.New("write timeout")
	ErrReadTimeout     = errors.New("read timeout")
)

// Transport represents a bidirectional communication channel.
// It abstracts the underlying transport mechanism (WebSocket, stdio, etc.)
// to provide a uniform interface for JSON-RPC communication.
type Transport interface {
	// ID returns a unique identifier for this transport instance.
	// For WebSocket, this is typically a UUID generated per connection.
	// For stdio, this is typically "stdio".
	ID() string

	// Read reads the next message from the transport.
	// It blocks until a message is available or the context is cancelled.
	// Returns io.EOF when transport is closed cleanly.
	Read(ctx context.Context) ([]byte, error)

	// Write sends a message through the transport.
	// It blocks until the message is sent or the context is cancelled.
	Write(ctx context.Context, data []byte) error

	// Close closes the transport.
	// After Close is called, Read and Write will return errors.
	// Close is safe to call multiple times.
	Close() error

	// Done returns a channel that's closed when the transport is closed.
	// This can be used to detect transport closure from another goroutine.
	Done() <-chan struct{}
}

// TransportInfo contains metadata about a transport connection.
type TransportInfo struct {
	// Type is the transport type: "websocket", "stdio", "tcp", etc.
	Type string

	// RemoteAddr is the remote address for network transports.
	// Empty for stdio transport.
	RemoteAddr string

	// LocalAddr is the local address for network transports.
	// Empty for stdio transport.
	LocalAddr string
}

// TransportHandler is called when a new transport connection is established.
type TransportHandler func(Transport)

// Server represents a transport server that accepts connections.
type Server interface {
	// Start begins accepting connections.
	// For each new connection, handler is called in a new goroutine.
	// Start blocks until ctx is cancelled or an error occurs.
	Start(ctx context.Context, handler TransportHandler) error

	// Stop gracefully stops the server.
	// It waits for existing connections to complete up to the context deadline.
	Stop(ctx context.Context) error

	// Addr returns the address the server is listening on.
	// For WebSocket, this is the HTTP address (e.g., "localhost:8766").
	// For stdio, this returns "stdio".
	Addr() string
}

// Listener is a simplified interface for accepting transport connections.
// Unlike Server, it doesn't manage the lifecycle of a network listener.
type Listener interface {
	// Accept waits for and returns the next transport connection.
	Accept(ctx context.Context) (Transport, error)

	// Close closes the listener.
	Close() error
}

// GenerateID generates a unique transport/client ID.
func GenerateID() string {
	return uuid.New().String()
}
