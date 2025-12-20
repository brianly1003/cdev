// Package websocket implements the WebSocket server for real-time events.
package websocket

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 15 * time.Second

	// Time allowed to read the next pong message from the peer.
	// Increased from 60s to 90s for better mobile network tolerance.
	pongWait = 90 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10 // 81 seconds

	// Maximum message size allowed from peer.
	maxMessageSize = 512 * 1024 // 512KB

	// Send buffer size per client.
	// Increased from 256 to 1024 for handling burst events (e.g., rapid Claude output).
	sendBufferSize = 1024

	// Application-level heartbeat interval.
	// Sent as a JSON event (not WebSocket ping) for client-side monitoring.
	heartbeatInterval = 30 * time.Second
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for now (POC)
		// In production, validate origin
		return true
	},
}

// CommandHandler is a function that handles incoming commands.
type CommandHandler func(clientID string, message []byte)

// StatusProvider provides status information for heartbeat events.
type StatusProvider interface {
	GetClaudeStatus() string
	GetUptimeSeconds() int64
}

// Server is the WebSocket server.
type Server struct {
	addr           string
	server         *http.Server
	commandHandler CommandHandler
	hub            ports.EventHub
	statusProvider StatusProvider

	mu      sync.RWMutex
	clients map[string]*Client

	// Heartbeat management
	heartbeatDone chan struct{}
	heartbeatSeq  int64
	startTime     time.Time
}

// NewServer creates a new WebSocket server.
func NewServer(host string, port int, commandHandler CommandHandler, hub ports.EventHub) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)
	s := &Server{
		addr:           addr,
		commandHandler: commandHandler,
		hub:            hub,
		clients:        make(map[string]*Client),
		heartbeatDone:  make(chan struct{}),
		startTime:      time.Now(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWebSocket)

	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
		// Note: Do NOT set ReadTimeout/WriteTimeout for WebSocket server
		// These timeouts apply to the underlying HTTP connection and can
		// cause premature disconnection of long-lived WebSocket connections.
		// The gorilla/websocket library handles its own deadlines via
		// SetReadDeadline/SetWriteDeadline in the read/write pumps.
	}

	return s
}

// SetStatusProvider sets the status provider for heartbeat events.
func (s *Server) SetStatusProvider(provider StatusProvider) {
	s.statusProvider = provider
}

// Start starts the WebSocket server.
func (s *Server) Start() error {
	log.Info().Str("addr", s.addr).Msg("WebSocket server starting")

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("WebSocket server error")
		}
	}()

	// Start heartbeat broadcaster
	go s.heartbeatLoop()

	return nil
}

// Stop gracefully stops the WebSocket server.
func (s *Server) Stop(ctx context.Context) error {
	log.Info().Msg("WebSocket server stopping")

	// Stop heartbeat
	close(s.heartbeatDone)

	// Close all client connections
	s.mu.Lock()
	for _, client := range s.clients {
		client.Close()
	}
	s.clients = make(map[string]*Client)
	s.mu.Unlock()

	return s.server.Shutdown(ctx)
}

// handleWebSocket handles WebSocket upgrade requests.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to upgrade connection")
		return
	}

	// Create client with a wrapper that unsubscribes from hub on close
	client := NewClient(conn, s.commandHandler, func(id string) {
		// Unsubscribe from hub
		if s.hub != nil {
			s.hub.Unsubscribe(id)
		}
		s.removeClient(id)
	})

	// Register client
	s.mu.Lock()
	s.clients[client.ID()] = client
	s.mu.Unlock()

	// Subscribe client to event hub
	if s.hub != nil {
		subscriber := NewClientSubscriber(client)
		s.hub.Subscribe(subscriber)
	}

	log.Info().
		Str("client_id", client.ID()).
		Str("remote_addr", conn.RemoteAddr().String()).
		Msg("client connected")

	// Start client goroutines
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
func (s *Server) Broadcast(message []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, client := range s.clients {
		client.Send(message)
	}
}

// ClientCount returns the number of connected clients.
func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

// GetClient returns a client by ID.
func (s *Server) GetClient(id string) *Client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[id]
}

// heartbeatLoop broadcasts periodic heartbeat events to all connected clients.
// This provides application-level connection monitoring beyond WebSocket ping/pong.
func (s *Server) heartbeatLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	log.Debug().Dur("interval", heartbeatInterval).Msg("heartbeat loop started")

	for {
		select {
		case <-s.heartbeatDone:
			log.Debug().Msg("heartbeat loop stopped")
			return

		case <-ticker.C:
			s.broadcastHeartbeat()
		}
	}
}

// broadcastHeartbeat sends a heartbeat event to all connected clients.
func (s *Server) broadcastHeartbeat() {
	s.mu.RLock()
	clientCount := len(s.clients)
	s.mu.RUnlock()

	// Don't send heartbeats if no clients connected
	if clientCount == 0 {
		return
	}

	// Get status from provider if available
	claudeStatus := "unknown"
	uptimeSeconds := int64(time.Since(s.startTime).Seconds())

	if s.statusProvider != nil {
		claudeStatus = s.statusProvider.GetClaudeStatus()
		uptimeSeconds = s.statusProvider.GetUptimeSeconds()
	}

	// Create and send heartbeat event
	seq := atomic.AddInt64(&s.heartbeatSeq, 1)
	heartbeat := events.NewHeartbeatEvent(seq, claudeStatus, uptimeSeconds)

	data, err := heartbeat.ToJSON()
	if err != nil {
		log.Warn().Err(err).Msg("failed to serialize heartbeat")
		return
	}

	s.Broadcast(data)
	log.Trace().Int64("seq", seq).Int("clients", clientCount).Msg("heartbeat sent")
}
