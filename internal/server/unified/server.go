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
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/rpc"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/rpc/transport"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/brianly1003/cdev/internal/server/common"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

// Default buffer sizes for WebSocket upgrader
const (
	wsReadBufferSize  = 1024
	wsWriteBufferSize = 1024
)

// Protocol represents the detected protocol type.
type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolLegacy           // {"command": "...", "payload": {...}}
	ProtocolJSONRPC          // {"jsonrpc": "2.0", "method": "...", ...}
)

// LegacyCommandHandler handles legacy commands.
type LegacyCommandHandler func(clientID string, cmd *commands.Command)

// SessionFocus represents which session a client is currently focused on.
type SessionFocus struct {
	ClientID    string
	WorkspaceID string
	SessionID   string
	FocusedAt   time.Time
}

// ClientDisconnectHandler handles cleanup when a client disconnects.
// Called with the client ID and list of workspace IDs they were subscribed to.
type ClientDisconnectHandler interface {
	OnClientDisconnect(clientID string, subscribedWorkspaces []string)
}

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
	mu              sync.RWMutex
	clients         map[string]*UnifiedClient
	filteredClients map[string]*hub.FilteredSubscriber // Workspace-filtered subscribers

	// Session focus tracking (multi-device awareness)
	sessionFocusMu sync.RWMutex
	sessionFocus   map[string]*SessionFocus // keyed by client_id

	// Disconnect handler for cleanup (git watchers, session streamers, etc.)
	disconnectHandler ClientDisconnectHandler

	// Heartbeat
	heartbeatDone chan struct{}
	heartbeatSeq  int64
	startTime     time.Time

	// Security
	tokenManager  *security.TokenManager
	originChecker *security.OriginChecker
	requireAuth   bool
}

// NewServer creates a new unified server.
func NewServer(host string, port int, dispatcher *handler.Dispatcher, eventHub ports.EventHub) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	s := &Server{
		addr:            addr,
		dispatcher:      dispatcher,
		hub:             eventHub,
		clients:         make(map[string]*UnifiedClient),
		filteredClients: make(map[string]*hub.FilteredSubscriber),
		sessionFocus:    make(map[string]*SessionFocus),
		heartbeatDone:   make(chan struct{}),
		startTime:       time.Now(),
	}

	// Create RPC server
	s.rpcServer = rpc.NewServer(dispatcher, eventHub)

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

// SetDisconnectHandler sets the handler for client disconnect cleanup.
func (s *Server) SetDisconnectHandler(handler ClientDisconnectHandler) {
	s.disconnectHandler = handler
}

// SetSecurity configures security settings for the server.
func (s *Server) SetSecurity(tokenManager *security.TokenManager, originChecker *security.OriginChecker, requireAuth bool) {
	s.tokenManager = tokenManager
	s.originChecker = originChecker
	s.requireAuth = requireAuth
}

// TokenManager returns the server's token manager (for pairing endpoints).
func (s *Server) TokenManager() *security.TokenManager {
	return s.tokenManager
}

// RequireAuth returns whether authentication is required.
func (s *Server) RequireAuth() bool {
	return s.requireAuth
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
		_ = client.Close()
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

	// Create upgrader with security checks
	wsUpgrader := websocket.Upgrader{
		ReadBufferSize:  wsReadBufferSize,
		WriteBufferSize: wsWriteBufferSize,
		CheckOrigin:     s.checkOrigin,
	}

	// Validate token if authentication is required
	if s.requireAuth {
		if err := s.validateTokenFromRequest(r); err != nil {
			log.Warn().
				Err(err).
				Str("remote_addr", r.RemoteAddr).
				Msg("WebSocket authentication failed")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
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

	// Wrap in FilteredSubscriber for workspace filtering support
	filtered := hub.NewFilteredSubscriber(client)

	s.mu.Lock()
	s.clients[client.ID()] = client
	s.filteredClients[client.ID()] = filtered
	s.mu.Unlock()

	// Subscribe the filtered subscriber to events
	if s.hub != nil {
		s.hub.Subscribe(filtered)
	}

	log.Info().
		Str("client_id", client.ID()).
		Str("remote_addr", conn.RemoteAddr().String()).
		Msg("client connected (unified)")

	client.Start()
}

// removeClient removes a client from the server.
func (s *Server) removeClient(id string) {
	// Get subscribed workspaces BEFORE removing the filtered subscriber
	var subscribedWorkspaces []string
	s.mu.Lock()
	if filtered, ok := s.filteredClients[id]; ok {
		subscribedWorkspaces = filtered.GetSubscribedWorkspaces()
	}
	delete(s.clients, id)
	delete(s.filteredClients, id)
	s.mu.Unlock()

	// Clear session focus and notify other viewers
	s.clearClientFocus(id)

	// Call disconnect handler for cleanup (git watchers, session streamers, etc.)
	if s.disconnectHandler != nil && len(subscribedWorkspaces) > 0 {
		s.disconnectHandler.OnClientDisconnect(id, subscribedWorkspaces)
	}

	log.Info().
		Str("client_id", id).
		Int("subscribed_workspaces", len(subscribedWorkspaces)).
		Msg("client disconnected")
}

// GetFilteredSubscriber returns the filtered subscriber for a client ID.
// Returns nil if the client doesn't exist.
func (s *Server) GetFilteredSubscriber(clientID string) *hub.FilteredSubscriber {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.filteredClients[clientID]
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

// FocusChangeResult is the result of a focus change operation.
type FocusChangeResult struct {
	WorkspaceID  string   `json:"workspace_id"`
	SessionID    string   `json:"session_id"`
	OtherViewers []string `json:"other_viewers"`
	ViewerCount  int      `json:"viewer_count"`
	Success      bool     `json:"success"`
}

// SetSessionFocus updates the session focus for a client and broadcasts join/leave events.
func (s *Server) SetSessionFocus(clientID, workspaceID, sessionID string) (interface{}, error) {
	s.sessionFocusMu.Lock()

	// Get previous focus for this client
	oldFocus := s.sessionFocus[clientID]

	// Update to new focus
	newFocus := &SessionFocus{
		ClientID:    clientID,
		WorkspaceID: workspaceID,
		SessionID:   sessionID,
		FocusedAt:   time.Now(),
	}
	s.sessionFocus[clientID] = newFocus

	// Find other clients viewing the SAME session
	var otherViewers []string
	for cID, focus := range s.sessionFocus {
		if cID != clientID &&
			focus.WorkspaceID == workspaceID &&
			focus.SessionID == sessionID {
			otherViewers = append(otherViewers, cID)
		}
	}

	s.sessionFocusMu.Unlock()

	// Emit session_joined event to other viewers if any exist
	if len(otherViewers) > 0 && s.hub != nil {
		event := events.NewSessionJoinedEvent(clientID, workspaceID, sessionID, otherViewers)
		s.hub.Publish(event)
	}

	// If client had previous focus on a different session, emit session_left
	if oldFocus != nil &&
		(oldFocus.WorkspaceID != workspaceID || oldFocus.SessionID != sessionID) {
		s.emitSessionLeft(clientID, oldFocus.WorkspaceID, oldFocus.SessionID)
	}

	return &FocusChangeResult{
		WorkspaceID:  workspaceID,
		SessionID:    sessionID,
		OtherViewers: otherViewers,
		ViewerCount:  len(otherViewers) + 1,
		Success:      true,
	}, nil
}

// emitSessionLeft broadcasts that a client left a session.
func (s *Server) emitSessionLeft(leavingClientID, workspaceID, sessionID string) {
	s.sessionFocusMu.RLock()

	// Find remaining viewers of the OLD session
	var remainingViewers []string
	for cID, focus := range s.sessionFocus {
		if cID != leavingClientID &&
			focus.WorkspaceID == workspaceID &&
			focus.SessionID == sessionID {
			remainingViewers = append(remainingViewers, cID)
		}
	}

	s.sessionFocusMu.RUnlock()

	// Only broadcast if there are viewers left to notify
	if len(remainingViewers) > 0 && s.hub != nil {
		event := events.NewSessionLeftEvent(leavingClientID, workspaceID, sessionID, remainingViewers)
		s.hub.Publish(event)
	}
}

// clearClientFocus removes a client's focus and notifies other viewers.
// Called when a client disconnects.
func (s *Server) clearClientFocus(clientID string) {
	s.sessionFocusMu.Lock()
	focus, ok := s.sessionFocus[clientID]
	delete(s.sessionFocus, clientID)
	s.sessionFocusMu.Unlock()

	// Emit session_left if client was viewing a session
	if ok && focus != nil {
		s.emitSessionLeft(clientID, focus.WorkspaceID, focus.SessionID)
	}
}

// GetSessionViewers returns a map of session ID to list of client IDs viewing that session.
// Optionally filter by workspace ID.
func (s *Server) GetSessionViewers(workspaceID string) map[string][]string {
	s.sessionFocusMu.RLock()
	defer s.sessionFocusMu.RUnlock()

	result := make(map[string][]string)
	for clientID, focus := range s.sessionFocus {
		// Filter by workspace if specified
		if workspaceID != "" && focus.WorkspaceID != workspaceID {
			continue
		}
		result[focus.SessionID] = append(result[focus.SessionID], clientID)
	}
	return result
}

// GetAllSessionFocus returns all session focus info.
// Returns a map of client ID to their focus info.
func (s *Server) GetAllSessionFocus() map[string]*SessionFocus {
	s.sessionFocusMu.RLock()
	defer s.sessionFocusMu.RUnlock()

	result := make(map[string]*SessionFocus, len(s.sessionFocus))
	for clientID, focus := range s.sessionFocus {
		result[clientID] = &SessionFocus{
			ClientID:    focus.ClientID,
			WorkspaceID: focus.WorkspaceID,
			SessionID:   focus.SessionID,
			FocusedAt:   focus.FocusedAt,
		}
	}
	return result
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

// checkOrigin validates the origin header for WebSocket connections.
func (s *Server) checkOrigin(r *http.Request) bool {
	// If origin checker is configured, use it
	if s.originChecker != nil {
		return s.originChecker.CheckOrigin(r)
	}

	// Default: allow localhost origins only
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true // Same-origin request
	}

	// Default localhost check
	return isLocalhostOrigin(origin)
}

// isLocalhostOrigin checks if an origin is from localhost.
func isLocalhostOrigin(origin string) bool {
	// Common localhost origins
	localhostOrigins := []string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"http://[::1]",
		"https://[::1]",
	}

	for _, allowed := range localhostOrigins {
		if origin == allowed || (len(origin) > len(allowed) && origin[:len(allowed)+1] == allowed+":") {
			return true
		}
	}
	return false
}

// validateTokenFromRequest extracts and validates a token from the request.
// Token can be in Authorization header (Bearer token) or query param (?token=xxx).
func (s *Server) validateTokenFromRequest(r *http.Request) error {
	if s.tokenManager == nil {
		return fmt.Errorf("token manager not configured")
	}

	token := ""

	// Check Authorization header first
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		const bearerPrefix = "Bearer "
		if len(authHeader) > len(bearerPrefix) && authHeader[:len(bearerPrefix)] == bearerPrefix {
			token = authHeader[len(bearerPrefix):]
		}
	}

	// Fall back to query parameter
	if token == "" {
		token = r.URL.Query().Get("token")
	}

	if token == "" {
		return fmt.Errorf("no token provided")
	}

	_, err := s.tokenManager.ValidateToken(token)
	return err
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
		_ = c.conn.Close()
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
		_ = c.Close()
		_ = c.conn.Close()
		if c.onClose != nil {
			c.onClose(c.id)
		}
	}()

	c.conn.SetReadLimit(common.MaxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(common.PongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(common.PongWait))
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
		// Log the actual message for debugging
		msgPreview := string(data)
		if len(msgPreview) > 100 {
			msgPreview = msgPreview[:100] + "..."
		}
		log.Warn().
			Str("client_id", c.id).
			Str("message", msgPreview).
			Int("length", len(data)).
			Msg("unknown protocol")
	}
}

// handleJSONRPC handles JSON-RPC messages.
func (c *UnifiedClient) handleJSONRPC(data []byte) {
	if c.dispatcher == nil {
		log.Warn().Str("client_id", c.id).Msg("no dispatcher for JSON-RPC")
		return
	}

	// Add client ID to context for methods that need it
	ctx := context.WithValue(context.Background(), handler.ClientIDKey, c.id)
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
		_ = c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait))
		_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
		_ = c.conn.Close()
	}()

	for {
		select {
		case <-c.done:
			return

		case data, ok := <-c.send:
			if !ok {
				return
			}

			_ = c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Debug().Err(err).Str("client_id", c.id).Msg("write error")
				return
			}

		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(common.WriteWait))
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
