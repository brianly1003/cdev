// Package common provides shared types and utilities for server implementations.
package common

import (
	"errors"
	"sync"

	"github.com/rs/zerolog/log"
)

// ErrBufferFull is returned when the send buffer is full.
var ErrBufferFull = errors.New("send buffer full")

// ErrClosed is returned when operations are attempted on a closed resource.
var ErrClosed = errors.New("closed")

// SendBuffer provides a thread-safe buffered channel for sending messages.
// It's designed for high-throughput scenarios like WebSocket message delivery.
type SendBuffer struct {
	id   string
	ch   chan []byte
	done chan struct{}

	mu     sync.Mutex
	closed bool
}

// NewSendBuffer creates a new send buffer with the specified capacity.
func NewSendBuffer(id string, capacity int) *SendBuffer {
	return &SendBuffer{
		id:   id,
		ch:   make(chan []byte, capacity),
		done: make(chan struct{}),
	}
}

// Send queues data for sending. Returns ErrBufferFull if the buffer is full,
// or ErrClosed if the buffer has been closed.
func (b *SendBuffer) Send(data []byte) error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrClosed
	}
	b.mu.Unlock()

	select {
	case b.ch <- data:
		return nil
	default:
		log.Warn().Str("id", b.id).Msg("send buffer full, dropping message")
		return ErrBufferFull
	}
}

// Channel returns the underlying channel for reading.
func (b *SendBuffer) Channel() <-chan []byte {
	return b.ch
}

// Close closes the send buffer.
func (b *SendBuffer) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	close(b.done)
}

// Done returns a channel that's closed when the buffer is closed.
func (b *SendBuffer) Done() <-chan struct{} {
	return b.done
}

// IsClosed returns true if the buffer is closed.
func (b *SendBuffer) IsClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
