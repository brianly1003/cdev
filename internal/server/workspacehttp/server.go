// Package workspacehttp provides the HTTP/WebSocket server for multi-workspace management.
package workspacehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/rpc"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/transport"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (configure for production)
	},
}

// Server is the multi-workspace HTTP/WebSocket server.
// This is the new simplified architecture that manages sessions in-process.
type Server struct {
	sessionManager *session.Manager
	configManager  *workspace.ConfigManager
	dispatcher     *handler.Dispatcher
	rpcServer      *rpc.Server
	hub            ports.EventHub
	logger         *slog.Logger

	addr       string
	httpServer *http.Server

	// Active connections counter
	mu          sync.RWMutex
	connections int
}

// NewServer creates a new multi-workspace HTTP server.
func NewServer(
	host string,
	port int,
	sessionManager *session.Manager,
	configManager *workspace.ConfigManager,
	dispatcher *handler.Dispatcher,
	hub ports.EventHub,
	logger *slog.Logger,
) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)

	// Create RPC server with event hub for broadcasting events
	rpcServer := rpc.NewServer(dispatcher, hub)

	return &Server{
		sessionManager: sessionManager,
		configManager:  configManager,
		dispatcher:     dispatcher,
		rpcServer:      rpcServer,
		hub:            hub,
		logger:         logger,
		addr:           addr,
		connections:    0,
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	router := mux.NewRouter()

	// Health check
	router.HandleFunc("/health", s.handleHealth).Methods("GET")

	// REST API endpoints for convenience (JSON-RPC is primary interface)
	api := router.PathPrefix("/api").Subrouter()

	// Workspace CRUD
	api.HandleFunc("/workspaces", s.handleListWorkspaces).Methods("GET")
	api.HandleFunc("/workspaces", s.handleAddWorkspace).Methods("POST")
	api.HandleFunc("/workspaces/{id}", s.handleGetWorkspace).Methods("GET")
	api.HandleFunc("/workspaces/{id}", s.handleUpdateWorkspace).Methods("PUT")
	api.HandleFunc("/workspaces/{id}", s.handleRemoveWorkspace).Methods("DELETE")
	api.HandleFunc("/workspaces/discover", s.handleDiscoverWorkspaces).Methods("POST")

	// Session management
	api.HandleFunc("/sessions", s.handleListSessions).Methods("GET")
	api.HandleFunc("/sessions/{id}", s.handleGetSession).Methods("GET")
	api.HandleFunc("/workspaces/{workspace_id}/sessions", s.handleStartSession).Methods("POST")
	api.HandleFunc("/sessions/{id}/stop", s.handleStopSession).Methods("POST")
	api.HandleFunc("/sessions/{id}/send", s.handleSendPrompt).Methods("POST")
	api.HandleFunc("/sessions/{id}/respond", s.handleRespond).Methods("POST")

	// Git operations (workspace-scoped)
	api.HandleFunc("/workspaces/{workspace_id}/git/status", s.handleGitStatus).Methods("GET")
	api.HandleFunc("/workspaces/{workspace_id}/git/diff", s.handleGitDiff).Methods("GET")

	// WebSocket endpoint for JSON-RPC 2.0 (primary/recommended interface)
	router.HandleFunc("/ws", s.handleWebSocket)

	// Apply CORS middleware
	handler := corsMiddleware(router)

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.logger.Info("Starting multi-workspace HTTP server", "addr", s.addr)

	// Start server in goroutine
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP server error", "error", err)
		}
	}()

	return nil
}

// Stop stops the HTTP server.
func (s *Server) Stop() error {
	s.logger.Info("Stopping multi-workspace HTTP server")

	// Stop RPC server (closes all clients)
	if err := s.rpcServer.Stop(); err != nil {
		s.logger.Error("Error stopping RPC server", "error", err)
	}

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctx)
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"service":   "cdev-multi-workspace",
		"timestamp": time.Now().Unix(),
	})
}

// handleListWorkspaces handles GET /api/workspaces
func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces := s.configManager.ListWorkspaces()

	// Enrich with session info
	result := make([]map[string]interface{}, 0, len(workspaces))
	for _, ws := range workspaces {
		info := map[string]interface{}{
			"id":         ws.Definition.ID,
			"name":       ws.Definition.Name,
			"path":       ws.Definition.Path,
			"auto_start": ws.Definition.AutoStart,
			"created_at": ws.Definition.CreatedAt,
		}

		// Get all sessions for this workspace
		sessions := s.sessionManager.GetSessionsForWorkspace(ws.Definition.ID)
		sessionInfos := make([]*session.Info, 0, len(sessions))
		activeCount := 0
		for _, sess := range sessions {
			sessionInfos = append(sessionInfos, sess.ToInfo())
			if sess.GetStatus() == session.StatusRunning {
				activeCount++
			}
		}
		info["sessions"] = sessionInfos
		info["active_session_count"] = activeCount
		info["has_active_session"] = activeCount > 0

		result = append(result, info)
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"workspaces": result,
	})
}

// handleGetWorkspace handles GET /api/workspaces/{id}
func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	ws, err := s.configManager.GetWorkspace(id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	info := map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	}

	// Get all sessions for this workspace
	sessions := s.sessionManager.GetSessionsForWorkspace(ws.Definition.ID)
	sessionInfos := make([]*session.Info, 0, len(sessions))
	activeCount := 0
	for _, sess := range sessions {
		sessionInfos = append(sessionInfos, sess.ToInfo())
		if sess.GetStatus() == session.StatusRunning {
			activeCount++
		}
	}
	info["sessions"] = sessionInfos
	info["active_session_count"] = activeCount
	info["has_active_session"] = activeCount > 0

	s.respondJSON(w, http.StatusOK, info)
}

// handleAddWorkspace handles POST /api/workspaces
func (s *Server) handleAddWorkspace(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string `json:"name"`
		Path      string `json:"path"`
		AutoStart bool   `json:"auto_start"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ws, err := s.configManager.AddWorkspace(req.Name, req.Path, req.AutoStart)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Register workspace with session manager
	s.sessionManager.RegisterWorkspace(ws)

	s.respondJSON(w, http.StatusCreated, map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	})
}

// handleUpdateWorkspace handles PUT /api/workspaces/{id}
func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Name      *string `json:"name"`
		AutoStart *bool   `json:"auto_start"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	ws, err := s.configManager.GetWorkspace(id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	// Update fields if provided
	if req.Name != nil {
		ws.Definition.Name = *req.Name
	}
	if req.AutoStart != nil {
		ws.Definition.AutoStart = *req.AutoStart
	}

	if err := s.configManager.UpdateWorkspace(ws); err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"id":         ws.Definition.ID,
		"name":       ws.Definition.Name,
		"path":       ws.Definition.Path,
		"auto_start": ws.Definition.AutoStart,
		"created_at": ws.Definition.CreatedAt,
	})
}

// handleRemoveWorkspace handles DELETE /api/workspaces/{id}
func (s *Server) handleRemoveWorkspace(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	// Check for active sessions
	activeCount := s.sessionManager.CountActiveSessionsForWorkspace(id)
	if activeCount > 0 {
		s.respondError(w, http.StatusBadRequest, fmt.Sprintf("Cannot remove workspace with %d active session(s)", activeCount))
		return
	}

	// Unregister from session manager
	if err := s.sessionManager.UnregisterWorkspace(id); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Remove from config
	if err := s.configManager.RemoveWorkspace(id); err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Workspace %s removed", id),
	})
}

// handleDiscoverWorkspaces handles POST /api/workspaces/discover
func (s *Server) handleDiscoverWorkspaces(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}

	// Parse request (optional)
	_ = json.NewDecoder(r.Body).Decode(&req)

	repos, err := s.configManager.DiscoverRepositories(req.Paths)
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"repositories": repos,
		"count":        len(repos),
	})
}

// handleListSessions handles GET /api/sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.URL.Query().Get("workspace_id")
	sessions := s.sessionManager.ListSessions(workspaceID)
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
	})
}

// handleGetSession handles GET /api/sessions/{id}
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	sess, err := s.sessionManager.GetSession(id)
	if err != nil {
		s.respondError(w, http.StatusNotFound, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, sess.ToInfo())
}

// handleStartSession handles POST /api/workspaces/{workspace_id}/sessions
func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workspaceID := vars["workspace_id"]

	sess, err := s.sessionManager.StartSession(workspaceID)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusCreated, sess.ToInfo())
}

// handleStopSession handles POST /api/sessions/{id}/stop
func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	if err := s.sessionManager.StopSession(id); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Session stopped",
	})
}

// handleSendPrompt handles POST /api/sessions/{id}/send
func (s *Server) handleSendPrompt(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Prompt string `json:"prompt"`
		Mode   string `json:"mode"` // "new" or "continue"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Prompt == "" {
		s.respondError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Note: HTTP API doesn't support permission_mode, use empty string for default behavior
	if err := s.sessionManager.SendPrompt(id, req.Prompt, req.Mode, ""); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "sent",
	})
}

// handleRespond handles POST /api/sessions/{id}/respond
func (s *Server) handleRespond(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	id := vars["id"]

	var req struct {
		Type     string `json:"type"`     // "permission" or "question"
		Response string `json:"response"` // "yes"/"no" for permission, or text for question
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var err error
	switch req.Type {
	case "permission":
		allow := req.Response == "yes" || req.Response == "true" || req.Response == "allow"
		err = s.sessionManager.RespondToPermission(id, allow)
	case "question":
		err = s.sessionManager.RespondToQuestion(id, req.Response)
	default:
		s.respondError(w, http.StatusBadRequest, "type must be 'permission' or 'question'")
		return
	}

	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": "responded",
	})
}

// handleGitStatus handles GET /api/workspaces/{workspace_id}/git/status
func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workspaceID := vars["workspace_id"]

	status, err := s.sessionManager.GetGitStatus(workspaceID)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"status": status,
	})
}

// handleGitDiff handles GET /api/workspaces/{workspace_id}/git/diff
func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	workspaceID := vars["workspace_id"]
	staged := r.URL.Query().Get("staged") == "true"

	diff, err := s.sessionManager.GetGitDiff(workspaceID, staged)
	if err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"diff": diff,
	})
}

// handleWebSocket handles WebSocket connections for JSON-RPC
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade connection
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade WebSocket", "error", err)
		return
	}

	// Create WebSocket transport
	wsTransport := transport.NewWebSocketTransport(conn)

	// Track connection
	s.mu.Lock()
	s.connections++
	connID := s.connections
	s.mu.Unlock()

	s.logger.Info("WebSocket client connected",
		"client_id", wsTransport.ID(),
		"connection", connID,
	)

	// Serve RPC over this transport (blocking)
	ctx := context.Background()
	err = s.rpcServer.ServeTransport(ctx, wsTransport)

	s.logger.Info("WebSocket client disconnected",
		"client_id", wsTransport.ID(),
		"error", err,
	)
}

// respondJSON sends a JSON response
func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("Failed to encode JSON response", "error", err)
	}
}

// respondError sends an error response
func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	s.respondJSON(w, status, map[string]interface{}{
		"error": message,
	})
}

// corsMiddleware adds CORS headers
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		// Allow local development origins
		if strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
