package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/brianly1003/cdev/internal/rpc/message"
)

// Client is a JSON-RPC 2.0 client for WebSocket communication.
type Client struct {
	conn      *websocket.Conn
	mu        sync.Mutex
	nextID    int64
	pending   map[int64]chan *message.Response
	pendingMu sync.RWMutex
	closeCh   chan struct{}
}

// NewClient creates a new JSON-RPC client connected to the given WebSocket URL.
func NewClient(url string) (*Client, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", url, err)
	}

	c := &Client{
		conn:    conn,
		nextID:  1,
		pending: make(map[int64]chan *message.Response),
		closeCh: make(chan struct{}),
	}

	// Start reading responses in background
	go c.readLoop()

	return c, nil
}

// Call makes a JSON-RPC call and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params interface{}) (*message.Response, error) {
	// Generate request ID
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	// Create request using message.NewRequest
	req, err := message.NewRequest(message.NumberID(id), method, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Create response channel
	respCh := make(chan *message.Response, 1)
	c.pendingMu.Lock()
	c.pending[id] = respCh
	c.pendingMu.Unlock()

	// Send request
	c.mu.Lock()
	err = c.conn.WriteJSON(req)
	c.mu.Unlock()
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Wait for response
	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.closeCh:
		return nil, fmt.Errorf("connection closed")
	}
}

// readLoop reads messages from the WebSocket connection.
func (c *Client) readLoop() {
	defer close(c.closeCh)

	for {
		var resp message.Response
		err := c.conn.ReadJSON(&resp)
		if err != nil {
			// Connection closed or error
			c.pendingMu.Lock()
			for _, ch := range c.pending {
				close(ch)
			}
			c.pending = make(map[int64]chan *message.Response)
			c.pendingMu.Unlock()
			return
		}

		// Route response to waiting caller
		if resp.ID != nil {
			// Extract int64 from ID (we always use NumberID)
			idInt := idToInt64(resp.ID)
			if idInt >= 0 {
				c.pendingMu.RLock()
				ch, ok := c.pending[idInt]
				c.pendingMu.RUnlock()

				if ok {
					ch <- &resp
					c.pendingMu.Lock()
					delete(c.pending, idInt)
					c.pendingMu.Unlock()
				}
			}
		}
		// Ignore notifications (no ID)
	}
}

// idToInt64 extracts the int64 value from an ID.
// Returns -1 if the ID is not a number.
func idToInt64(id *message.ID) int64 {
	if id == nil {
		return -1
	}
	// Parse the ID string representation
	var n int64
	_, err := fmt.Sscanf(id.String(), "%d", &n)
	if err != nil {
		return -1
	}
	return n
}

// Close closes the WebSocket connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}
