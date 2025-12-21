// Package common provides shared types and utilities for server implementations.
package common

import "time"

// WebSocket timing constants.
// These are tuned for mobile network tolerance.
const (
	// WriteWait is time allowed to write a message to the peer.
	WriteWait = 15 * time.Second

	// PongWait is time allowed to read the next pong message from the peer.
	// Increased from 60s to 90s for better mobile network tolerance.
	PongWait = 90 * time.Second

	// PingPeriod is the interval for sending pings. Must be less than PongWait.
	PingPeriod = (PongWait * 9) / 10 // 81 seconds

	// MaxMessageSize is the maximum message size allowed from peer.
	MaxMessageSize = 512 * 1024 // 512KB

	// SendBufferSize is the send buffer size per client.
	// Increased from 256 to 1024 for handling burst events.
	SendBufferSize = 1024

	// HeartbeatInterval is the application-level heartbeat interval.
	HeartbeatInterval = 30 * time.Second
)
