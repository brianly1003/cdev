// Package http implements the HTTP API server for cdev.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/repository"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/brianly1003/cdev/internal/server/http/middleware"
	"github.com/brianly1003/cdev/internal/services/imagestorage"
	"github.com/rs/zerolog/log"
	httpSwagger "github.com/swaggo/http-swagger"
)

// WebSocketHandler is a function that handles WebSocket connections.
type WebSocketHandler func(http.ResponseWriter, *http.Request)

// Server is the HTTP API server.
type Server struct {
	server              *http.Server
	mux                 *http.ServeMux
	addr                string
	statusFn            func() map[string]interface{}
	claudeManager       *claude.Manager
	gitTracker          *git.Tracker
	sessionCache        *sessioncache.Cache
	messageCache        *sessioncache.MessageCache
	eventHub            ports.EventHub
	repoIndexer         *repository.SQLiteIndexer
	maxFileSizeKB       int
	maxDiffSizeKB       int
	repoPath            string
	wsHandler           WebSocketHandler
	rpcRegistry         *handler.Registry
	imageHandler        *ImageHandler
	imageStorageManager *imagestorage.Manager
	originChecker       *security.OriginChecker
	rateLimiter         *middleware.RateLimiter
	tokenManager        *security.TokenManager
	requireAuth         bool
	authAllowlist       []string
}

// New creates a new HTTP server.
func New(host string, port int, statusFn func() map[string]interface{}, claudeManager *claude.Manager, gitTracker *git.Tracker, sessionCache *sessioncache.Cache, messageCache *sessioncache.MessageCache, eventHub ports.EventHub, maxFileSizeKB int, maxDiffSizeKB int, repoPath string) *Server {
	addr := fmt.Sprintf("%s:%d", host, port)

	s := &Server{
		addr:          addr,
		statusFn:      statusFn,
		claudeManager: claudeManager,
		gitTracker:    gitTracker,
		sessionCache:  sessionCache,
		messageCache:  messageCache,
		eventHub:      eventHub,
		maxFileSizeKB: maxFileSizeKB,
		maxDiffSizeKB: maxDiffSizeKB,
		repoPath:      repoPath,
		mux:           http.NewServeMux(),
		authAllowlist: defaultAuthAllowlist(),
	}

	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/claude/sessions", s.handleClaudeSessions)
	s.mux.HandleFunc("/api/claude/sessions/messages", s.handleClaudeSessionMessages)
	s.mux.HandleFunc("/api/claude/sessions/elements", s.handleClaudeSessionElements)
	s.mux.HandleFunc("/api/claude/run", s.handleClaudeRun)
	s.mux.HandleFunc("/api/claude/stop", s.handleClaudeStop)
	s.mux.HandleFunc("/api/claude/respond", s.handleClaudeRespond)
	s.mux.HandleFunc("/api/file", s.handleGetFile)
	s.mux.HandleFunc("/api/files/list", s.handleFilesList)
	s.mux.HandleFunc("/api/git/status", s.handleGitStatus)
	s.mux.HandleFunc("/api/git/diff", s.handleGitDiff)
	s.mux.HandleFunc("/api/git/stage", s.handleGitStage)
	s.mux.HandleFunc("/api/git/unstage", s.handleGitUnstage)
	s.mux.HandleFunc("/api/git/discard", s.handleGitDiscard)
	s.mux.HandleFunc("/api/git/commit", s.handleGitCommit)
	s.mux.HandleFunc("/api/git/push", s.handleGitPush)
	s.mux.HandleFunc("/api/git/pull", s.handleGitPull)
	s.mux.HandleFunc("/api/git/branches", s.handleGitBranches)
	s.mux.HandleFunc("/api/git/checkout", s.handleGitCheckout)

	// Repository indexer endpoints
	s.mux.HandleFunc("/api/repository/index/status", s.handleRepositoryIndexStatus)
	s.mux.HandleFunc("/api/repository/index/rebuild", s.handleRepositoryRebuild)
	s.mux.HandleFunc("/api/repository/search", s.handleRepositorySearch)
	s.mux.HandleFunc("/api/repository/files/list", s.handleRepositoryFilesList)
	s.mux.HandleFunc("/api/repository/files/tree", s.handleRepositoryTree)
	s.mux.HandleFunc("/api/repository/stats", s.handleRepositoryStats)

	// Swagger UI endpoint (REST API docs)
	s.mux.Handle("/swagger/", httpSwagger.Handler(
		httpSwagger.DeepLinking(true),
		httpSwagger.DocExpansion("none"),
		httpSwagger.DomID("swagger-ui"),
	))

	// OpenRPC endpoint (JSON-RPC API docs)
	s.mux.HandleFunc("/api/rpc/discover", s.handleOpenRPCDiscover)

	return s
}

// SetImageStorageManager sets the image storage manager for multi-workspace support.
// The manager creates storage per-workspace on demand.
// Must be called before Start() to enable image upload functionality.
func (s *Server) SetImageStorageManager(manager *imagestorage.Manager) {
	if manager == nil {
		log.Warn().Msg("image storage manager is nil, image upload will not be available")
		return
	}
	s.imageStorageManager = manager
	s.imageHandler = NewImageHandler(manager)

	// Register image routes with workspace_id support
	s.mux.HandleFunc("/api/images", s.imageHandler.HandleImages)
	s.mux.HandleFunc("/api/images/validate", s.imageHandler.ValidateImagePath)
	s.mux.HandleFunc("/api/images/stats", s.imageHandler.HandleImageStats)
	s.mux.HandleFunc("/api/images/all", s.imageHandler.HandleClearImages)

	log.Info().Msg("image upload routes registered: /api/images?workspace_id=xxx")
}

// SetPairingHandler sets up pairing endpoints for mobile app connection.
// Must be called before Start() to enable pairing functionality.
func (s *Server) SetPairingHandler(handler *PairingHandler) {
	if handler == nil {
		log.Warn().Msg("pairing handler is nil, pairing routes will not be available")
		return
	}

	// Register pairing routes
	s.mux.HandleFunc("/pair", handler.HandlePairPage)
	s.mux.HandleFunc("/api/pair/info", handler.HandlePairInfo)
	s.mux.HandleFunc("/api/pair/qr", handler.HandlePairQR)
	s.mux.HandleFunc("/api/pair/refresh", handler.HandlePairRefresh)

	log.Info().Msg("pairing routes registered: /pair, /api/pair/info, /api/pair/qr, /api/pair/refresh")
}

// SetAuthHandler sets up authentication endpoints for token exchange and refresh.
// Must be called before Start() to enable token refresh functionality.
func (s *Server) SetAuthHandler(handler *AuthHandler) {
	if handler == nil {
		log.Warn().Msg("auth handler is nil, auth routes will not be available")
		return
	}

	// Register auth routes (no authentication required - they handle their own validation)
	s.mux.HandleFunc("/api/auth/exchange", handler.HandleExchange)
	s.mux.HandleFunc("/api/auth/refresh", handler.HandleRefresh)
	s.mux.HandleFunc("/api/auth/revoke", handler.HandleRevoke)

	log.Info().Msg("auth routes registered: /api/auth/exchange, /api/auth/refresh, /api/auth/revoke")
}

// SetDebugHandler sets up debug and profiling endpoints.
// Must be called before Start() to enable debug functionality.
// Debug endpoints are automatically protected by localhost-only binding (default).
func (s *Server) SetDebugHandler(handler *DebugHandler) {
	if handler == nil {
		log.Warn().Msg("debug handler is nil, debug routes will not be available")
		return
	}

	// Register debug routes
	handler.Register(s.mux)
}

// SetHooksHandler sets up Claude hooks endpoints for receiving events from external Claude sessions.
// This enables real-time event capture from Claude running in VS Code, Cursor, or terminal.
func (s *Server) SetHooksHandler(handler *HooksHandler) {
	if handler == nil {
		log.Warn().Msg("hooks handler is nil, Claude hook routes will not be available")
		return
	}

	// Register hooks routes - accepts events from Claude's hook system
	s.mux.HandleFunc("/api/hooks/", handler.HandleHook)

	log.Info().Msg("Claude hooks routes registered: /api/hooks/{hookType}")
}

// requestLoggingMiddleware logs all incoming requests for debugging.
func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Str("host", r.Host).
			Str("user_agent", r.UserAgent()).
			Msg("incoming request")

		next.ServeHTTP(w, r)

		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Dur("duration", time.Since(start)).
			Msg("request completed")
	})
}

// timeoutMiddleware wraps handlers with a timeout to prevent hanging requests.
func timeoutMiddleware(timeout time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip timeout for certain paths that need longer processing
		// (e.g., swagger UI, health checks, WebSocket upgrades, debug/pprof endpoints,
		// permission requests that block waiting for mobile response)
		if r.URL.Path == "/health" || r.URL.Path == "/ws" ||
			strings.HasPrefix(r.URL.Path, "/swagger/") ||
			strings.HasPrefix(r.URL.Path, "/debug/") ||
			r.URL.Path == "/api/hooks/permission-request" {
			next.ServeHTTP(w, r)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		// Create a channel to track handler completion
		done := make(chan struct{})

		// Use a response wrapper to prevent writing after timeout
		tw := &timeoutResponseWriter{ResponseWriter: w, done: done}

		go func() {
			next.ServeHTTP(tw, r.WithContext(ctx))
			close(done)
		}()

		select {
		case <-done:
			// Handler completed normally
		case <-ctx.Done():
			// Timeout reached
			tw.mu.Lock()
			if !tw.written {
				tw.written = true
				tw.mu.Unlock()
				log.Warn().
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Dur("timeout", timeout).
					Msg("request timed out")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusGatewayTimeout)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "Request timed out",
				})
			} else {
				tw.mu.Unlock()
			}
		}
	})
}

// timeoutResponseWriter wraps http.ResponseWriter to track if response was written.
type timeoutResponseWriter struct {
	http.ResponseWriter
	mu      sync.Mutex
	written bool
	done    chan struct{}
}

func (tw *timeoutResponseWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.written {
		return
	}
	tw.written = true
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *timeoutResponseWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	if !tw.written {
		tw.written = true
	}
	tw.mu.Unlock()
	return tw.ResponseWriter.Write(b)
}

// SetOriginChecker sets the origin checker for CORS validation.
func (s *Server) SetOriginChecker(checker *security.OriginChecker) {
	s.originChecker = checker
}

// SetRateLimiter sets the rate limiter for request throttling.
func (s *Server) SetRateLimiter(limiter *middleware.RateLimiter) {
	s.rateLimiter = limiter
}

// SetAuth configures HTTP authentication.
func (s *Server) SetAuth(tokenManager *security.TokenManager, requireAuth bool) {
	s.tokenManager = tokenManager
	s.requireAuth = requireAuth
}

// SetAuthAllowlist overrides the default unauthenticated path allowlist.
func (s *Server) SetAuthAllowlist(allowlist []string) {
	s.authAllowlist = allowlist
}

func defaultAuthAllowlist() []string {
	return []string{
		"/health",
		"/pair",
		"/api/pair/",
		"/api/auth/exchange",
		"/api/auth/refresh",
	}
}

func (s *Server) isAuthExempt(path string) bool {
	for _, allowed := range s.authAllowlist {
		if strings.HasPrefix(path, allowed) {
			return true
		}
	}
	return false
}

func extractBearerToken(authHeader string) string {
	const bearerPrefix = "Bearer "
	if len(authHeader) > len(bearerPrefix) && strings.HasPrefix(authHeader, bearerPrefix) {
		return strings.TrimSpace(authHeader[len(bearerPrefix):])
	}
	return ""
}

// authMiddleware enforces bearer token authentication for HTTP endpoints.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAuth || s.tokenManager == nil {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodOptions || s.isAuthExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r.Header.Get("Authorization"))
		if token == "" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		payload, err := s.tokenManager.ValidateToken(token)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if payload.Type != security.TokenTypeSession && payload.Type != security.TokenTypeAccess {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isLocalhostOrigin checks if an origin is from localhost.
func isLocalhostOrigin(origin string) bool {
	return strings.Contains(origin, "localhost") ||
		strings.Contains(origin, "127.0.0.1") ||
		strings.Contains(origin, "::1")
}

// corsMiddleware adds CORS headers with configurable origin checking.
// Security: Uses OriginChecker instead of wildcard "*" for Access-Control-Allow-Origin.
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Log incoming request
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote", r.RemoteAddr).
			Str("origin", r.Header.Get("Origin")).
			Msg("HTTP request received")

		origin := r.Header.Get("Origin")

		// Validate origin using OriginChecker if configured
		if origin != "" {
			if s.originChecker != nil {
				if !s.originChecker.CheckOrigin(r) {
					log.Warn().
						Str("origin", origin).
						Str("remote", r.RemoteAddr).
						Msg("CORS request rejected - origin not allowed")
					http.Error(w, "Origin not allowed", http.StatusForbidden)
					return
				}
				// Set the specific origin that was validated (not wildcard)
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else {
				// No checker configured - only allow localhost origins
				if isLocalhostOrigin(origin) {
					w.Header().Set("Access-Control-Allow-Origin", origin)
				} else {
					log.Warn().
						Str("origin", origin).
						Str("remote", r.RemoteAddr).
						Msg("CORS request rejected - no origin checker and not localhost")
					http.Error(w, "Origin not allowed", http.StatusForbidden)
					return
				}
			}
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)

		// Log request completion
		log.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Dur("elapsed", time.Since(start)).
			Msg("HTTP request completed")
	})
}

// Start starts the HTTP server.
// SetWebSocketHandler sets the handler for WebSocket connections.
// Must be called before Start().
func (s *Server) SetWebSocketHandler(handler WebSocketHandler) {
	s.wsHandler = handler
}

func (s *Server) Start() error {
	// Add WebSocket handler if set (must be done before creating http.Server)
	if s.wsHandler != nil {
		s.mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			log.Info().
				Str("remote_addr", r.RemoteAddr).
				Str("path", r.URL.Path).
				Str("host", r.Host).
				Str("user_agent", r.UserAgent()).
				Str("origin", r.Header.Get("Origin")).
				Msg("WebSocket upgrade request received at /ws")
			s.wsHandler(w, r)
		})
		log.Debug().Msg("WebSocket handler registered at /ws")
	}

	// Build middleware chain from inside out:
	// request -> logging -> rate limit (optional) -> timeout -> cors -> auth -> mux
	var handler http.Handler = s.mux
	handler = s.authMiddleware(handler)
	handler = s.corsMiddleware(handler)
	handler = timeoutMiddleware(10*time.Second, handler)

	// Add rate limiting if configured
	if s.rateLimiter != nil {
		handler = middleware.RateLimitMiddleware(s.rateLimiter, middleware.IPKeyExtractor)(handler)
		log.Info().Msg("Rate limiting enabled for HTTP server")
	}

	handler = requestLoggingMiddleware(handler)

	// Create the http.Server with middleware chain
	s.server = &http.Server{
		Addr:    s.addr,
		Handler: handler,
	}

	log.Info().Str("addr", s.addr).Msg("HTTP server starting")

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return nil
}

// Stop gracefully stops the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	log.Info().Msg("HTTP server stopping")

	// Close image handler
	if s.imageHandler != nil {
		s.imageHandler.Close()
	}

	// Close image storage manager (closes all per-workspace storages)
	if s.imageStorageManager != nil {
		s.imageStorageManager.Close()
	}

	return s.server.Shutdown(ctx)
}

// handleHealth handles GET /health
//
//	@Summary		Health check
//	@Description	Returns the health status of the agent
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	HealthResponse
//	@Router			/health [get]
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	response := map[string]interface{}{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	}

	writeJSON(w, http.StatusOK, response)
}

// handleStatus handles GET /api/status
//
//	@Summary		Get agent status
//	@Description	Returns the current status of the cdev including Claude state, connected clients, and repository info
//	@Tags			status
//	@Produce		json
//	@Success		200	{object}	StatusResponse
//	@Router			/api/status [get]
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.statusFn()
	writeJSON(w, http.StatusOK, status)
}

// handleClaudeSessions handles GET/DELETE /api/claude/sessions
//
//	@Summary		List or delete Claude sessions
//	@Description	GET: Returns a list of available Claude sessions. DELETE: Deletes session(s). Use ?session_id=xxx to delete specific session, or no param to delete all.
//	@Tags			claude
//	@Produce		json
//	@Param			session_id	query		string	false	"Session ID (for DELETE only - if omitted, deletes all sessions)"
//	@Success		200			{object}	SessionsResponse
//	@Failure		404			{object}	ErrorResponse	"Session not found"
//	@Failure		500			{object}	ErrorResponse	"Failed to list/delete sessions"
//	@Router			/api/claude/sessions [get]
//	@Router			/api/claude/sessions [delete]
func (s *Server) handleClaudeSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListSessions(w, r)
	case http.MethodDelete:
		s.handleDeleteSessions(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleListSessions handles GET /api/claude/sessions
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	// Parse pagination parameters
	limit := parseIntParam(r, "limit", 20)  // Default 20 per page
	offset := parseIntParam(r, "offset", 0) // Default start from 0

	// Clamp limit to reasonable bounds
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var sessions any
	var total int
	var err error

	// Use session cache if available (fast path with pagination)
	if s.sessionCache != nil {
		sessionList, t, e := s.sessionCache.ListSessionsPaginated(limit, offset)
		sessions = sessionList
		total = t
		err = e
	} else {
		// Fallback to direct file read (no pagination in fallback)
		sessionList, e := claude.ListSessions(s.repoPath)
		sessions = sessionList
		if sessionList != nil {
			total = len(sessionList)
		}
		err = e
	}

	if err != nil {
		log.Error().Err(err).Msg("failed to list sessions")
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to list sessions: " + err.Error(),
		})
		return
	}

	// Get current session ID if Claude is running
	currentSessionID := ""
	if s.claudeManager != nil {
		currentSessionID = s.claudeManager.ClaudeSessionID()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
		"current":  currentSessionID,
		"total":    total,
		"limit":    limit,
		"offset":   offset,
	})
}

// parseIntParam parses an integer query parameter with a default value.
func parseIntParam(r *http.Request, name string, defaultVal int) int {
	valStr := r.URL.Query().Get(name)
	if valStr == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(valStr)
	if err != nil {
		return defaultVal
	}
	return val
}

// handleDeleteSessions handles DELETE /api/claude/sessions
func (s *Server) handleDeleteSessions(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")

	if sessionID != "" {
		// Delete specific session
		if err := claude.DeleteSession(s.repoPath, sessionID); err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("failed to delete session")
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":      err.Error(),
				"session_id": sessionID,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message":    "Session deleted",
			"session_id": sessionID,
		})
	} else {
		// Delete all sessions
		deleted, err := claude.DeleteAllSessions(s.repoPath)
		if err != nil {
			log.Error().Err(err).Msg("failed to delete all sessions")
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "Failed to delete sessions: " + err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"message": "All sessions deleted",
			"deleted": deleted,
		})
	}
}

// handleClaudeSessionMessages handles GET /api/claude/sessions/messages?session_id=...
//
//	@Summary		Get session messages (paginated)
//	@Description	Returns messages for a specific Claude session with pagination support.
//	@Description	For sessions with many messages (3000+), use pagination to avoid large payloads.
//	@Tags			claude
//	@Produce		json
//	@Param			session_id	query		string	true	"Session ID"
//	@Param			limit		query		int		false	"Max messages to return (default 50, max 500)"
//	@Param			offset		query		int		false	"Starting position (default 0)"
//	@Param			order		query		string	false	"Sort order: 'asc' (oldest first) or 'desc' (newest first, default)"
//	@Success		200			{object}	SessionMessagesResponse
//	@Failure		400			{object}	ErrorResponse	"Missing session_id"
//	@Failure		404			{object}	ErrorResponse	"Session not found"
//	@Router			/api/claude/sessions/messages [get]
func (s *Server) handleClaudeSessionMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "session_id query parameter is required",
		})
		return
	}

	// Parse pagination parameters
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	order := r.URL.Query().Get("order")
	if order != "asc" && order != "desc" {
		order = "asc" // Default to chronological (oldest first) for chat history
	}

	// Use message cache if available for better performance
	if s.messageCache != nil {
		page, err := s.messageCache.GetMessages(sessionID, limit, offset, order)
		if err != nil {
			log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get session messages from cache")
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error":      err.Error(),
				"session_id": sessionID,
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"session_id":    sessionID,
			"messages":      page.Messages,
			"total":         page.Total,
			"limit":         page.Limit,
			"offset":        page.Offset,
			"has_more":      page.HasMore,
			"cache_hit":     page.CacheHit,
			"query_time_ms": page.QueryTimeMS,
		})
		return
	}

	// Fallback to direct file read (no pagination, for backward compatibility)
	messages, err := claude.GetSessionMessages(s.repoPath, sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get session messages")
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error":      err.Error(),
			"session_id": sessionID,
		})
		return
	}

	// Apply basic pagination to fallback
	total := len(messages)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
		"messages":   messages[offset:end],
		"total":      total,
		"limit":      limit,
		"offset":     offset,
		"has_more":   end < total,
	})
}

// handleClaudeSessionElements handles GET /api/claude/sessions/elements
//
//	@Summary		Get session UI elements
//	@Description	Returns pre-parsed UI elements for a session, ready for rendering in mobile apps.
//	@Tags			claude
//	@Accept			json
//	@Produce		json
//	@Param			session_id	query		string	true	"Session UUID"
//	@Param			limit		query		int		false	"Number of elements to return (default 50, max 100)"
//	@Param			before		query		string	false	"Return elements before this ID (for pagination)"
//	@Param			after		query		string	false	"Return elements after this ID (for catch-up)"
//	@Success		200			{object}	sessioncache.ElementsResponse
//	@Failure		400			{object}	ErrorResponse	"Missing session_id"
//	@Failure		404			{object}	ErrorResponse	"Session not found"
//	@Router			/api/claude/sessions/elements [get]
func (s *Server) handleClaudeSessionElements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "session_id query parameter is required",
		})
		return
	}

	// Parse pagination params
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	beforeID := r.URL.Query().Get("before")
	afterID := r.URL.Query().Get("after")

	// Get raw messages from session
	messages, err := claude.GetSessionMessages(s.repoPath, sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("failed to get session messages")
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error":      err.Error(),
			"session_id": sessionID,
		})
		return
	}

	// Convert messages to raw JSON for parsing
	var rawMessages []json.RawMessage
	for _, msg := range messages {
		msgJSON, err := json.Marshal(msg)
		if err != nil {
			continue
		}
		rawMessages = append(rawMessages, msgJSON)
	}

	// Parse messages into elements
	elements, err := sessioncache.ParseSessionToElements(rawMessages, sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("failed to parse session elements")
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to parse session elements",
		})
		return
	}

	// Apply pagination
	totalElements := len(elements)
	startIdx := 0
	endIdx := totalElements

	if beforeID != "" {
		// Find element with beforeID and return elements before it
		for i, elem := range elements {
			if elem.ID == beforeID {
				endIdx = i
				startIdx = max(0, endIdx-limit)
				break
			}
		}
	} else if afterID != "" {
		// Find element with afterID and return elements after it
		for i, elem := range elements {
			if elem.ID == afterID {
				startIdx = i + 1
				endIdx = min(totalElements, startIdx+limit)
				break
			}
		}
	} else {
		// Default: return last N elements
		startIdx = max(0, totalElements-limit)
	}

	paginatedElements := elements[startIdx:endIdx]

	// Build pagination info
	pagination := sessioncache.ElementsPagination{
		Total:         totalElements,
		Returned:      len(paginatedElements),
		HasMoreBefore: startIdx > 0,
		HasMoreAfter:  endIdx < totalElements,
	}

	if len(paginatedElements) > 0 {
		pagination.OldestID = paginatedElements[0].ID
		pagination.NewestID = paginatedElements[len(paginatedElements)-1].ID
	}

	response := sessioncache.ElementsResponse{
		SessionID:  sessionID,
		Elements:   paginatedElements,
		Pagination: pagination,
	}

	writeJSON(w, http.StatusOK, response)
}

// handleClaudeRun handles POST /api/claude/run
//
//	@Summary		Start Claude CLI
//	@Description	Spawns a Claude CLI process with the given prompt. Supports session modes: "new" (default) or "continue" (requires session_id).
//	@Tags			claude
//	@Accept			json
//	@Produce		json
//	@Param			request	body		RunClaudeRequest	true	"Prompt and session options"
//	@Success		200		{object}	RunClaudeResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request"
//	@Failure		409		{object}	ErrorResponse	"Claude already running"
//	@Failure		503		{object}	ErrorResponse	"Claude manager not available"
//	@Router			/api/claude/run [post]
func (s *Server) handleClaudeRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Prompt    string `json:"prompt"`
		Mode      string `json:"mode,omitempty"`
		SessionID string `json:"session_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Prompt is required",
		})
		return
	}

	if s.claudeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Claude manager not available",
		})
		return
	}

	// Determine session mode
	var mode claude.SessionMode
	switch req.Mode {
	case "continue":
		mode = claude.SessionModeContinue
		if req.SessionID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "session_id is required when mode is 'continue'",
			})
			return
		}
	case "", "new":
		mode = claude.SessionModeNew
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid mode. Must be 'new' or 'continue'",
		})
		return
	}

	// Start Claude - use Background context since Claude should continue running
	// even if the HTTP request is cancelled
	// Note: HTTP API doesn't support permission_mode, use empty string for default behavior
	if err := s.claudeManager.StartWithSession(context.Background(), req.Prompt, mode, req.SessionID, ""); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": err.Error(),
		})
		return
	}

	log.Info().
		Str("prompt", truncateString(req.Prompt, 50)).
		Str("mode", string(mode)).
		Int("pid", s.claudeManager.PID()).
		Msg("Claude started via HTTP")

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "started",
		"prompt":     req.Prompt,
		"pid":        s.claudeManager.PID(),
		"mode":       string(mode),
		"session_id": s.claudeManager.ClaudeSessionID(),
	})
}

// handleClaudeStop handles POST /api/claude/stop
//
//	@Summary		Stop Claude CLI
//	@Description	Gracefully stops the running Claude CLI process
//	@Tags			claude
//	@Produce		json
//	@Success		200	{object}	StopClaudeResponse
//	@Failure		409	{object}	ErrorResponse	"Claude not running"
//	@Failure		503	{object}	ErrorResponse	"Claude manager not available"
//	@Router			/api/claude/stop [post]
func (s *Server) handleClaudeStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.claudeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Claude manager not available",
		})
		return
	}

	if err := s.claudeManager.Stop(r.Context()); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": err.Error(),
		})
		return
	}

	log.Info().Msg("Claude stopped via HTTP")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status": "stopped",
	})
}

// handleClaudeRespond handles POST /api/claude/respond
//
//	@Summary		Respond to Claude
//	@Description	Send a response to Claude's interactive prompt or permission request
//	@Tags			claude
//	@Accept			json
//	@Produce		json
//	@Param			request	body		RespondToClaudeRequest	true	"Response to send to Claude"
//	@Success		200		{object}	RespondToClaudeResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request"
//	@Failure		409		{object}	ErrorResponse	"Claude not waiting for input"
//	@Failure		503		{object}	ErrorResponse	"Claude manager not available"
//	@Router			/api/claude/respond [post]
func (s *Server) handleClaudeRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ToolUseID string `json:"tool_use_id"`
		Response  string `json:"response"`
		IsError   bool   `json:"is_error"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if req.ToolUseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "tool_use_id is required",
		})
		return
	}

	if s.claudeManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Claude manager not available",
		})
		return
	}

	if err := s.claudeManager.SendResponse(req.ToolUseID, req.Response, req.IsError); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": err.Error(),
		})
		return
	}

	log.Info().
		Str("tool_use_id", req.ToolUseID).
		Str("response", truncateString(req.Response, 50)).
		Msg("Response sent to Claude via HTTP")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "sent",
		"tool_use_id": req.ToolUseID,
	})
}

// handleGetFile handles GET /api/file?path=...
//
//	@Summary		Get file content
//	@Description	Returns the content of a file in the repository
//	@Tags			file
//	@Produce		json
//	@Param			path	query		string	true	"Relative path to the file"
//	@Success		200		{object}	FileContentResponse
//	@Failure		400		{object}	ErrorResponse	"Missing path parameter"
//	@Failure		404		{object}	ErrorResponse	"File not found"
//	@Failure		503		{object}	ErrorResponse	"Git tracker not available"
//	@Router			/api/file [get]
func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "path query parameter is required",
		})
		return
	}

	if s.gitTracker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Git tracker not available",
		})
		return
	}

	content, truncated, err := s.gitTracker.GetFileContent(r.Context(), path, s.maxFileSizeKB)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": err.Error(),
			"path":  path,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"path":      path,
		"content":   content,
		"encoding":  "utf-8",
		"truncated": truncated,
		"size":      len(content),
	})
}

// handleFilesList handles GET /api/files/list
//
//	@Summary		List directory contents
//	@Description	Returns the contents of a directory in the repository. Supports browsing the file tree.
//	@Tags			file
//	@Produce		json
//	@Param			path	query		string	false	"Relative path from repo root (empty for root directory)"
//	@Success		200		{object}	DirectoryListingResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid path format"
//	@Failure		403		{object}	ErrorResponse	"Path outside repository"
//	@Failure		404		{object}	ErrorResponse	"Directory not found"
//	@Router			/api/files/list [get]
func (s *Server) handleFilesList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get the relative path from query parameter
	relPath := r.URL.Query().Get("path")

	// Build absolute path
	absPath := s.repoPath
	if relPath != "" {
		absPath = filepath.Join(s.repoPath, relPath)
	}

	// Security: Validate path is within repository root (prevent path traversal)
	cleanAbsPath, err := filepath.Abs(absPath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid path format",
			"path":  relPath,
		})
		return
	}

	cleanRepoPath, _ := filepath.Abs(s.repoPath)
	rel, err := filepath.Rel(cleanRepoPath, cleanAbsPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Path outside repository",
			"path":  relPath,
		})
		return
	}

	realRepoPath := cleanRepoPath
	if resolvedRoot, err := filepath.EvalSymlinks(cleanRepoPath); err == nil {
		realRepoPath = resolvedRoot
	}

	realPath, err := filepath.EvalSymlinks(cleanAbsPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "Directory not found",
				"path":  relPath,
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid path format",
			"path":  relPath,
		})
		return
	}

	relResolved, err := filepath.Rel(realRepoPath, realPath)
	if err != nil || strings.HasPrefix(relResolved, "..") {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "Path outside repository",
			"path":  relPath,
		})
		return
	}

	// Check if directory exists
	info, err := os.Stat(realPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Directory not found",
			"path":  relPath,
		})
		return
	}

	if !info.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Path is not a directory",
			"path":  relPath,
		})
		return
	}

	// Read directory entries
	dirEntries, err := os.ReadDir(realPath)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to read directory: " + err.Error(),
			"path":  relPath,
		})
		return
	}

	// Build response entries
	entries := make([]FileEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		name := entry.Name()

		// Skip hidden files and common ignored directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		if name == "node_modules" || name == "__pycache__" || name == "vendor" {
			continue
		}

		fe := FileEntry{
			Name: name,
		}

		if entry.Type()&os.ModeSymlink != 0 {
			fe.Type = "file"
			if info, err := os.Lstat(filepath.Join(realPath, name)); err == nil {
				size := info.Size()
				fe.Size = &size
				mod := info.ModTime().Format(time.RFC3339)
				fe.Modified = &mod
			}
		} else if entry.IsDir() {
			fe.Type = "directory"
			// Count children (non-hidden)
			childPath := filepath.Join(realPath, name)
			if childEntries, err := os.ReadDir(childPath); err == nil {
				count := 0
				for _, ce := range childEntries {
					if !strings.HasPrefix(ce.Name(), ".") {
						count++
					}
				}
				fe.ChildrenCount = &count
			}
		} else {
			fe.Type = "file"
			if info, err := entry.Info(); err == nil {
				size := info.Size()
				fe.Size = &size
				mod := info.ModTime().Format(time.RFC3339)
				fe.Modified = &mod
			}
		}

		entries = append(entries, fe)
	}

	// Sort: directories first, then alphabetically
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Type != entries[j].Type {
			return entries[i].Type == "directory"
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	response := DirectoryListingResponse{
		Path:       relPath,
		Entries:    entries,
		TotalCount: len(entries),
	}

	writeJSON(w, http.StatusOK, response)
}

// handleGitStatus handles GET /api/git/status
//
//	@Summary		Get enhanced git status
//	@Description	Returns the git status with staging info, branch tracking, and diff stats
//	@Tags			git
//	@Produce		json
//	@Success		200	{object}	GitEnhancedStatusResponse
//	@Failure		404	{object}	ErrorResponse	"Not a git repository"
//	@Failure		500	{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/status [get]
func (s *Server) handleGitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	status, err := s.gitTracker.GetEnhancedStatus(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// handleGitDiff handles GET /api/git/diff?path=...
//
//	@Summary		Get git diff
//	@Description	Returns the git diff for a specific file or all files if no path is provided
//	@Tags			git
//	@Produce		json
//	@Param			path	query		string	false	"Relative path to the file (optional, returns all diffs if not provided)"
//	@Success		200		{object}	GitDiffResponse		"Single file diff (when path provided)"
//	@Success		200		{object}	GitDiffAllResponse	"All diffs (when path not provided)"
//	@Failure		404		{object}	ErrorResponse		"Not a git repository"
//	@Failure		500		{object}	ErrorResponse		"Git command failed"
//	@Router			/api/git/diff [get]
func (s *Server) handleGitDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	path := r.URL.Query().Get("path")

	if path == "" {
		// Get all diffs
		diffs, err := s.gitTracker.DiffAll(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": err.Error(),
			})
			return
		}
		truncatedPaths := []string{}
		for filePath, diff := range diffs {
			capped, truncated := git.TruncateDiff(diff, s.maxDiffSizeKB)
			diffs[filePath] = capped
			if truncated {
				truncatedPaths = append(truncatedPaths, filePath)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"diffs":           diffs,
			"truncated_paths": truncatedPaths,
		})
		return
	}

	// Get diff for specific file - check unstaged, staged, and new files
	diff, err := s.gitTracker.Diff(r.Context(), path)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
			"path":  path,
		})
		return
	}

	isStaged := false
	isNew := false

	// If no unstaged diff, check for staged diff
	if diff == "" {
		stagedDiff, err := s.gitTracker.DiffStaged(r.Context(), path)
		if err == nil && stagedDiff != "" {
			diff = stagedDiff
			isStaged = true
		}
	}

	// If still no diff, check if it's a new/untracked file
	if diff == "" {
		newFileDiff, err := s.gitTracker.DiffNewFile(r.Context(), path)
		if err == nil && newFileDiff != "" {
			diff = newFileDiff
			isNew = true
		}
	}

	capped, truncated := git.TruncateDiff(diff, s.maxDiffSizeKB)
	writeJSON(w, http.StatusOK, map[string]any{
		"path":         path,
		"diff":         capped,
		"is_staged":    isStaged,
		"is_new":       isNew,
		"is_truncated": truncated,
	})
}

// handleGitStage handles POST /api/git/stage
//
//	@Summary		Stage files
//	@Description	Stage files for commit
//	@Tags			git
//	@Accept			json
//	@Produce		json
//	@Param			request	body		GitStageRequest	true	"Files to stage"
//	@Success		200		{object}	GitStageResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request"
//	@Failure		404		{object}	ErrorResponse	"Not a git repository"
//	@Failure		500		{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/stage [post]
func (s *Server) handleGitStage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if len(req.Paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "paths is required",
		})
		return
	}

	if err := s.gitTracker.Stage(r.Context(), req.Paths); err != nil {
		s.publishGitOperationCompleted("stage", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	s.publishGitOperationCompleted("stage", true, "Files staged", "", map[string]interface{}{
		"files_affected": len(req.Paths),
	})
	go s.publishGitStatusChanged(context.Background())

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"staged":  req.Paths,
	})
}

// handleGitUnstage handles POST /api/git/unstage
//
//	@Summary		Unstage files
//	@Description	Unstage files from the staging area
//	@Tags			git
//	@Accept			json
//	@Produce		json
//	@Param			request	body		GitUnstageRequest	true	"Files to unstage"
//	@Success		200		{object}	GitUnstageResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request"
//	@Failure		404		{object}	ErrorResponse	"Not a git repository"
//	@Failure		500		{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/unstage [post]
func (s *Server) handleGitUnstage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if len(req.Paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "paths is required",
		})
		return
	}

	if err := s.gitTracker.Unstage(r.Context(), req.Paths); err != nil {
		s.publishGitOperationCompleted("unstage", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	s.publishGitOperationCompleted("unstage", true, "Files unstaged", "", map[string]interface{}{
		"files_affected": len(req.Paths),
	})
	go s.publishGitStatusChanged(context.Background())

	writeJSON(w, http.StatusOK, map[string]any{
		"success":  true,
		"unstaged": req.Paths,
	})
}

// handleGitDiscard handles POST /api/git/discard
//
//	@Summary		Discard changes
//	@Description	Discard unstaged changes to files
//	@Tags			git
//	@Accept			json
//	@Produce		json
//	@Param			request	body		GitDiscardRequest	true	"Files to discard"
//	@Success		200		{object}	GitDiscardResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request"
//	@Failure		404		{object}	ErrorResponse	"Not a git repository"
//	@Failure		500		{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/discard [post]
func (s *Server) handleGitDiscard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	var req struct {
		Paths []string `json:"paths"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if len(req.Paths) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "paths is required",
		})
		return
	}

	if err := s.gitTracker.Discard(r.Context(), req.Paths); err != nil {
		s.publishGitOperationCompleted("discard", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	s.publishGitOperationCompleted("discard", true, "Changes discarded", "", map[string]interface{}{
		"files_affected": len(req.Paths),
	})
	go s.publishGitStatusChanged(context.Background())

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"discarded": req.Paths,
	})
}

// handleGitCommit handles POST /api/git/commit
//
//	@Summary		Commit staged changes
//	@Description	Create a commit with staged changes, optionally push
//	@Tags			git
//	@Accept			json
//	@Produce		json
//	@Param			request	body		GitCommitRequest	true	"Commit message and options"
//	@Success		200		{object}	GitCommitResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request or nothing staged"
//	@Failure		404		{object}	ErrorResponse	"Not a git repository"
//	@Failure		500		{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/commit [post]
func (s *Server) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	var req struct {
		Message string `json:"message"`
		Push    bool   `json:"push"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if req.Message == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "message is required",
		})
		return
	}

	result, err := s.gitTracker.Commit(r.Context(), req.Message, req.Push)
	if err != nil {
		s.publishGitOperationCompleted("commit", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if result.Success {
		s.publishGitOperationCompleted("commit", true, result.Message, "", map[string]interface{}{
			"sha":            result.SHA,
			"files_affected": result.FilesCommitted,
		})
		go s.publishGitStatusChanged(context.Background())
	} else {
		s.publishGitOperationCompleted("commit", false, "", result.Error, nil)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGitPush handles POST /api/git/push
//
//	@Summary		Push commits
//	@Description	Push local commits to remote
//	@Tags			git
//	@Produce		json
//	@Success		200	{object}	GitPushResponse
//	@Failure		404	{object}	ErrorResponse	"Not a git repository"
//	@Failure		500	{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/push [post]
func (s *Server) handleGitPush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	// Simple push with defaults: no force, no set-upstream, use default remote/branch
	result, err := s.gitTracker.Push(r.Context(), false, false, "", "")
	if err != nil {
		s.publishGitOperationCompleted("push", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if result.Success {
		s.publishGitOperationCompleted("push", true, result.Message, "", map[string]interface{}{
			"commits_pushed": result.CommitsPushed,
		})
		go s.publishGitStatusChanged(context.Background())
	} else {
		s.publishGitOperationCompleted("push", false, "", result.Error, nil)
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGitPull handles POST /api/git/pull
//
//	@Summary		Pull commits
//	@Description	Pull remote commits to local
//	@Tags			git
//	@Produce		json
//	@Success		200	{object}	GitPullResponse
//	@Failure		404	{object}	ErrorResponse	"Not a git repository"
//	@Failure		500	{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/pull [post]
func (s *Server) handleGitPull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	// Simple pull with merge (not rebase)
	result, err := s.gitTracker.Pull(r.Context(), false)
	if err != nil {
		s.publishGitOperationCompleted("pull", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if result.Success {
		s.publishGitOperationCompleted("pull", true, result.Message, "", map[string]interface{}{
			"commits_pulled":   result.CommitsPulled,
			"files_affected":   result.FilesChanged,
			"conflicted_files": result.ConflictedFiles,
		})
		go s.publishGitStatusChanged(context.Background())
	} else {
		s.publishGitOperationCompleted("pull", false, "", result.Error, map[string]interface{}{
			"conflicted_files": result.ConflictedFiles,
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGitBranches handles GET /api/git/branches
//
//	@Summary		List branches
//	@Description	List all local and remote branches
//	@Tags			git
//	@Produce		json
//	@Success		200	{object}	GitBranchesResponse
//	@Failure		404	{object}	ErrorResponse	"Not a git repository"
//	@Failure		500	{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/branches [get]
func (s *Server) handleGitBranches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	result, err := s.gitTracker.ListBranches(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleGitCheckout handles POST /api/git/checkout
//
//	@Summary		Checkout branch
//	@Description	Switch to a different branch or create a new one
//	@Tags			git
//	@Accept			json
//	@Produce		json
//	@Param			request	body		GitCheckoutRequest	true	"Branch to checkout"
//	@Success		200		{object}	GitCheckoutResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid request"
//	@Failure		404		{object}	ErrorResponse	"Not a git repository"
//	@Failure		500		{object}	ErrorResponse	"Git command failed"
//	@Router			/api/git/checkout [post]
func (s *Server) handleGitCheckout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.gitTracker == nil || !s.gitTracker.IsGitRepo() {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "Not a git repository",
		})
		return
	}

	var req struct {
		Branch string `json:"branch"`
		Create bool   `json:"create"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid JSON",
		})
		return
	}

	if req.Branch == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "branch is required",
		})
		return
	}

	result, err := s.gitTracker.Checkout(r.Context(), req.Branch, req.Create)
	if err != nil {
		s.publishGitOperationCompleted("checkout", false, "", err.Error(), nil)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if result.Success {
		s.publishGitOperationCompleted("checkout", true, result.Message, "", map[string]interface{}{
			"branch": result.Branch,
		})
		go s.publishGitStatusChanged(context.Background())
	} else {
		s.publishGitOperationCompleted("checkout", false, "", result.Error, nil)
	}

	writeJSON(w, http.StatusOK, result)
}

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)

	// Flush to ensure response is sent immediately
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// truncateString truncates a string for logging.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// publishGitStatusChanged publishes a git_status_changed event after operations.
func (s *Server) publishGitStatusChanged(ctx context.Context) {
	if s.eventHub == nil || s.gitTracker == nil {
		return
	}

	status, err := s.gitTracker.GetEnhancedStatus(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("failed to get git status for event")
		return
	}

	// Collect changed file paths
	var changedFiles []string
	for _, f := range status.Staged {
		changedFiles = append(changedFiles, f.Path)
	}
	for _, f := range status.Unstaged {
		changedFiles = append(changedFiles, f.Path)
	}
	for _, f := range status.Untracked {
		changedFiles = append(changedFiles, f.Path)
	}

	payload := events.GitStatusChangedPayload{
		Branch:         status.Branch,
		Ahead:          status.Ahead,
		Behind:         status.Behind,
		StagedCount:    len(status.Staged),
		UnstagedCount:  len(status.Unstaged),
		UntrackedCount: len(status.Untracked),
		HasConflicts:   len(status.Conflicted) > 0,
		ChangedFiles:   changedFiles,
	}

	s.eventHub.Publish(events.NewEvent(events.EventTypeGitStatusChanged, payload))
}

// publishGitOperationCompleted publishes a git_operation_completed event.
func (s *Server) publishGitOperationCompleted(operation string, success bool, message string, errMsg string, extra map[string]interface{}) {
	if s.eventHub == nil {
		return
	}

	payload := events.GitOperationCompletedPayload{
		Operation: operation,
		Success:   success,
		Message:   message,
		Error:     errMsg,
	}

	// Add operation-specific fields
	if extra != nil {
		if sha, ok := extra["sha"].(string); ok {
			payload.SHA = sha
		}
		if branch, ok := extra["branch"].(string); ok {
			payload.Branch = branch
		}
		if filesAffected, ok := extra["files_affected"].(int); ok {
			payload.FilesAffected = filesAffected
		}
		if commitsPushed, ok := extra["commits_pushed"].(int); ok {
			payload.CommitsPushed = commitsPushed
		}
		if commitsPulled, ok := extra["commits_pulled"].(int); ok {
			payload.CommitsPulled = commitsPulled
		}
		if conflictedFiles, ok := extra["conflicted_files"].([]string); ok {
			payload.ConflictedFiles = conflictedFiles
		}
	}

	s.eventHub.Publish(events.NewEvent(events.EventTypeGitOperationComplete, payload))
}
