// Package unified provides a dual-protocol WebSocket server that supports
// both the legacy command format and JSON-RPC 2.0.
package unified

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brianly1003/cdev/internal/domain/commands"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/rpc"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/rpc/transport"
	"github.com/brianly1003/cdev/internal/server/common"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (configure for production)
	},
}

// Protocol represents the detected protocol type.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolLegacy           // {"command": "...", "payload": {...}}
	ProtocolJSONRPC          // {"jsonrpc": "2.0", "method": "...", ...}
)

// LegacyCommandHandler handles legacy commands.
type LegacyCommandHandler func(clientID string, cmd *commands.Command)

// Server is a unified WebSocket server supporting dual protocols.
type Server struct {
	addr string

	// RPC components
	dispatcher *handler.Dispatcher
	rpcServer  *rpc.Server

	// Legacy handler
	legacyHandler LegacyCommandHandler

	// Event hub for broadcasting
	hub ports.EventHub

	// Status provider for heartbeats
	statusProvider common.StatusProvider

	// HTTP server
	httpServer *http.Server

	// Client management
	mu      sync.RWMutex
	clients map[string]*UnifiedClient

	// Heartbeat
	heartbeatDone chan struct{}
	heartbeatSeq  int64
	startTime     time.Time
}

// NewServer creates a new unified server.
func NewServer(host string, port int, dispatcher *handler.Dispatcher, hub ports.EventHub) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	s := &Server{
		addr:          addr,
		dispatcher:    dispatcher,
		hub:           hub,
		clients:       make(map[string]*UnifiedClient),
		heartbeatDone: make(chan struct{}),
		startTime:     time.Now(),
	}

	// Create RPC server
	s.rpcServer = rpc.NewServer(dispatcher, hub)

	return s
}

// SetLegacyHandler sets the handler for legacy commands.
func (s *Server) SetLegacyHandler(h LegacyCommandHandler) {
	s.legacyHandler = h
}

// SetStatusProvider sets the status provider for heartbeats.
func (s *Server) SetStatusProvider(provider common.StatusProvider) {
	s.statusProvider = provider
}

// Start starts the unified server with its own HTTP server.
// For port consolidation, use StartBackgroundTasks() with the HTTP server's SetWebSocketHandler() instead.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleWebSocket)

	s.httpServer = &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	log.Info().Str("addr", s.addr).Msg("Unified WebSocket server starting")

	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Unified server error")
		}
	}()

	// Start heartbeat broadcaster
	go s.heartbeatLoop()

	return nil
}

// StartBackgroundTasks starts background tasks (heartbeat) without starting the HTTP server.
// Use this when integrating with an existing HTTP server via SetWebSocketHandler().
func (s *Server) StartBackgroundTasks() {
	go s.heartbeatLoop()
	log.Info().Msg("Unified server background tasks started")
}

// Stop gracefully stops the server.
func (s *Server) Stop(ctx context.Context) error {
	log.Info().Msg("Unified server stopping")

	close(s.heartbeatDone)

	s.mu.Lock()
	for _, client := range s.clients {
		client.Close()
	}
	s.clients = make(map[string]*UnifiedClient)
	s.mu.Unlock()

	// Only shutdown HTTP server if it was started
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// HandleWebSocket handles WebSocket upgrade requests.
// This can be used as a handler in HTTP servers for WebSocket upgrades.
func (s *Server) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Debug().
		Str("remote_addr", r.RemoteAddr).
		Str("path", r.URL.Path).
		Str("upgrade", r.Header.Get("Upgrade")).
		Str("connection", r.Header.Get("Connection")).
		Str("sec_websocket_key", r.Header.Get("Sec-WebSocket-Key")).
		Msg("processing WebSocket upgrade")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().
			Err(err).
			Str("remote_addr", r.RemoteAddr).
			Str("upgrade_header", r.Header.Get("Upgrade")).
			Str("connection_header", r.Header.Get("Connection")).
			Msg("failed to upgrade connection to WebSocket")
		return
	}

	client := NewUnifiedClient(conn, s.dispatcher, s.legacyHandler, func(id string) {
		if s.hub != nil {
			s.hub.Unsubscribe(id)
		}
		s.removeClient(id)
	})

	s.mu.Lock()
	s.clients[client.ID()] = client
	s.mu.Unlock()

	// Subscribe to events
	if s.hub != nil {
		s.hub.Subscribe(client)
	}

	log.Info().
		Str("client_id", client.ID()).
		Str("remote_addr", conn.RemoteAddr().String()).
		Msg("client connected (unified)")

	client.Start()
}

// removeClient removes a client from the server.
func (s *Server) removeClient(id string) {
	s.mu.Lock()
	delete(s.clients, id)
	s.mu.Unlock()
	log.Info().Str("client_id", id).Msg("client disconnected")
}

// Broadcast sends a message to all connected clients.
func (s *Server) Broadcast(data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, client := range s.clients {
		client.SendRaw(data)
	}
}

// ClientCount returns the number of connected clients.
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// GetClient returns a client by ID for sending direct messages.
func (s *Server) GetClient(id string) *UnifiedClient {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[id]
}

// heartbeatLoop broadcasts periodic heartbeat events.
func (s *Server) heartbeatLoop() {
	ticker := time.NewTicker(common.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.heartbeatDone:
			return
		case <-ticker.C:
			s.broadcastHeartbeat()
		}
	}
}

// broadcastHeartbeat sends a heartbeat to all clients.
func (s *Server) broadcastHeartbeat() {
	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()

	if clientCount == 0 {
		return
	}

	agentStatus := "unknown"
	uptimeSeconds := int64(time.Since(s.startTime).Seconds())

	if s.statusProvider != nil {
		agentStatus = s.statusProvider.GetAgentStatus()
		uptimeSeconds = s.statusProvider.GetUptimeSeconds()
	}

	seq := atomic.AddInt64(&s.heartbeatSeq, 1)

	// Send legacy format heartbeat
	heartbeat := events.NewHeartbeatEvent(seq, agentStatus, uptimeSeconds)
	data, err := heartbeat.ToJSON()
	if err != nil {
		return
	}

	s.Broadcast(data)
}

// UnifiedClient represents a client that can speak either protocol.
type UnifiedClient struct {
	id            string
	conn          *websocket.Conn
	send          chan []byte
	done          chan struct{}
	dispatcher    *handler.Dispatcher
	legacyHandler LegacyCommandHandler
	onClose       func(id string)

	mu       sync.Mutex
	closed   bool
	protocol Protocol // Detected after first message
}

// NewUnifiedClient creates a new unified client.
func NewUnifiedClient(
	conn *websocket.Conn,
	dispatcher *handler.Dispatcher,
	legacyHandler LegacyCommandHandler,
	onClose func(id string),
) *UnifiedClient {
	return &UnifiedClient{
		id:            transport.GenerateID(),
		conn:          conn,
		send:          make(chan []byte, common.SendBufferSize),
		done:          make(chan struct{}),
		dispatcher:    dispatcher,
		legacyHandler: legacyHandler,
		onClose:       onClose,
		protocol:      ProtocolUnknown,
	}
}

// ID returns the client ID.
func (c *UnifiedClient) ID() string {
	return c.id
}

// Start starts the client's read and write loops.
func (c *UnifiedClient) Start() {
	go c.writePump()
	go c.readPump()
}

// SendRaw queues raw bytes to be sent.
func (c *UnifiedClient) SendRaw(data []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	select {
	case c.send <- data:
	default:
		log.Warn().Str("client_id", c.id).Msg("send buffer full")
	}
}

// Close closes the client connection.
func (c *UnifiedClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	close(c.done)

	// Close the websocket connection to unblock readPump
	if c.conn != nil {
		c.conn.Close()
	}

	return nil
}

// Done returns a channel that's closed when the client is done.
func (c *UnifiedClient) Done() <-chan struct{} {
	return c.done
}

// Send implements ports.Subscriber - converts events to appropriate format.
func (c *UnifiedClient) Send(event events.Event) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client closed")
	}
	protocol := c.protocol
	c.mu.Unlock()

	// Get event JSON
	data, err := event.ToJSON()
	if err != nil {
		return err
	}

	// If client is using JSON-RPC, convert to notification format
	if protocol == ProtocolJSONRPC {
		return c.sendJSONRPCNotification(event)
	}

	// Legacy format or unknown - send as-is
	c.SendRaw(data)
	return nil
}

// sendJSONRPCNotification sends an event as a JSON-RPC notification.
func (c *UnifiedClient) sendJSONRPCNotification(event events.Event) error {
	method := "event/" + string(event.Type())

	// Extract payload from event
	data, err := event.ToJSON()
	if err != nil {
		return err
	}

	var eventData struct {
		Payload interface{} `json:"payload"`
	}
	if err := json.Unmarshal(data, &eventData); err != nil {
		return err
	}

	notification, err := message.NewNotification(method, eventData.Payload)
	if err != nil {
		return err
	}

	notifData, err := json.Marshal(notification)
	if err != nil {
		return err
	}

	c.SendRaw(notifData)
	return nil
}

// readPump reads messages from the WebSocket connection.
func (c *UnifiedClient) readPump() {
	defer func() {
		c.Close()
		c.conn.Close()
		if c.onClose != nil {
			c.onClose(c.id)
		}
	}()

	c.conn.SetReadLimit(common.MaxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(common.PongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(common.PongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Warn().Err(err).Str("client_id", c.id).Msg("websocket read error")
			}
			return
		}

		c.handleMessage(data)
	}
}

// handleMessage detects protocol and routes appropriately.
func (c *UnifiedClient) handleMessage(data []byte) {
	// Detect protocol from message
	protocol := detectProtocol(data)

	// Set client protocol on first message
	c.mu.Lock()
	if c.protocol == ProtocolUnknown {
		c.protocol = protocol
		log.Debug().
			Str("client_id", c.id).
			Str("protocol", protocolName(protocol)).
			Msg("protocol detected")
	}
	c.mu.Unlock()

	switch protocol {
	case ProtocolJSONRPC:
		c.handleJSONRPC(data)
	case ProtocolLegacy:
		c.handleLegacy(data)
	default:
		log.Warn().Str("client_id", c.id).Msg("unknown protocol")
	}
}

// handleJSONRPC handles JSON-RPC messages.
func (c *UnifiedClient) handleJSONRPC(data []byte) {
	if c.dispatcher == nil {
		log.Warn().Str("client_id", c.id).Msg("no dispatcher for JSON-RPC")
		return
	}

	ctx := context.Background()
	response, err := c.dispatcher.HandleMessage(ctx, data)
	if err != nil {
		log.Warn().Err(err).Str("client_id", c.id).Msg("JSON-RPC dispatch error")
		return
	}

	// Don't send empty responses (notifications)
	if len(response) == 0 {
		return
	}

	c.SendRaw(response)
}

// handleLegacy handles legacy command messages.
// DEPRECATED: Legacy command format is deprecated. Use JSON-RPC 2.0 instead.
// Legacy support will be removed in version 3.0.
func (c *UnifiedClient) handleLegacy(data []byte) {
	if c.legacyHandler == nil {
		log.Warn().Str("client_id", c.id).Msg("no handler for legacy commands")
		return
	}

	cmd, err := commands.ParseCommand(data)
	if err != nil {
		log.Warn().Err(err).Str("client_id", c.id).Msg("failed to parse legacy command")
		return
	}

	// Log deprecation warning
	log.Warn().
		Str("client_id", c.id).
		Str("command", string(cmd.Command)).
		Msg("DEPRECATED: Legacy command format is deprecated. Please migrate to JSON-RPC 2.0. Legacy support will be removed in version 3.0.")

	// Send deprecation warning to client
	c.sendDeprecationWarning(string(cmd.Command))

	c.legacyHandler(c.id, cmd)
}

// sendDeprecationWarning sends a deprecation warning event to the client.
func (c *UnifiedClient) sendDeprecationWarning(command string) {
	warning := map[string]interface{}{
		"event":     "deprecation_warning",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"payload": map[string]interface{}{
			"message":       "Legacy command format is deprecated. Please migrate to JSON-RPC 2.0.",
			"command":       command,
			"documentation": "See /api/rpc/discover for the JSON-RPC 2.0 API specification.",
			"removal":       "Legacy support will be removed in version 3.0.",
			"migration": map[string]string{
				"run_claude":        "agent/run",
				"stop_claude":       "agent/stop",
				"respond_to_claude": "agent/respond",
				"get_status":        "status/get",
				"get_file":          "file/get",
				"watch_session":     "session/watch",
				"unwatch_session":   "session/unwatch",
			},
		},
	}

	data, err := json.Marshal(warning)
	if err != nil {
		return
	}

	c.SendRaw(data)
}

// writePump sends messages to the WebSocket connection.
func (c *UnifiedClient) writePump() {
	ticker := time.NewTicker(common.PingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait))
		c.conn.WriteMessage(websocket.CloseMessage, []byte{})
		c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			return

		case data, ok := <-c.send:
			if !ok {
				return
			}

			c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Debug().Err(err).Str("client_id", c.id).Msg("write error")
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// detectProtocol detects the protocol from a message.
func detectProtocol(data []byte) Protocol {
	if len(data) == 0 {
		return ProtocolUnknown
	}

	// Quick check for JSON-RPC
	if message.IsJSONRPC(data) {
		return ProtocolJSONRPC
	}

	// Check for legacy command format
	var msg struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(data, &msg); err == nil && msg.Command != "" {
		return ProtocolLegacy
	}

	return ProtocolUnknown
}

// protocolName returns a human-readable protocol name.
func protocolName(p Protocol) string {
	switch p {
	case ProtocolJSONRPC:
		return "json-rpc"
	case ProtocolLegacy:
		return "legacy"
	default:
		return "unknown"
	}
}

// Ensure UnifiedClient implements ports.Subscriber
var _ ports.Subscriber = (*UnifiedClient)(nil)
