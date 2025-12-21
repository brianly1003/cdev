// Package common provides shared types and utilities for server implementations.
package common

// StatusProvider provides status information for heartbeat events.
// This interface is implemented by the application to provide runtime status.
type StatusProvider interface {
	// GetAgentStatus returns the current agent status (e.g., "idle", "running").
	GetAgentStatus() string

	// GetUptimeSeconds returns the server uptime in seconds.
	GetUptimeSeconds() int64
}

// Sender is an interface for sending raw bytes.
type Sender interface {
	// SendRaw sends raw bytes to the client.
	SendRaw(data []byte) error
}

// Closer is an interface for closable resources.
type Closer interface {
	// Close closes the resource.
	Close() error

	// Done returns a channel that's closed when the resource is closed.
	Done() <-chan struct{}
}

// Client combines common client capabilities.
type Client interface {
	// ID returns the unique client identifier.
	ID() string

	Sender
	Closer
}
