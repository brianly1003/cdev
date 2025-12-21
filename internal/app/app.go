// Package app orchestrates all components of cdev.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/repository"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/adapters/watcher"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/commands"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/pairing"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
	httpserver "github.com/brianly1003/cdev/internal/server/http"
	"github.com/brianly1003/cdev/internal/server/unified"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// App is the main application struct that orchestrates all components.
type App struct {
	cfg     *config.Config
	version string

	// Core components
	hub             *hub.Hub
	claudeManager   *claude.Manager
	fileWatcher     *watcher.Watcher
	gitTracker      *git.Tracker
	sessionCache    *sessioncache.Cache
	messageCache    *sessioncache.MessageCache
	sessionStreamer *sessioncache.SessionStreamer
	repoIndexer     *repository.SQLiteIndexer
	httpServer      *httpserver.Server
	unifiedServer   *unified.Server
	rpcDispatcher   *handler.Dispatcher
	qrGenerator     *pairing.QRGenerator

	// Session info
	sessionID string
	startTime time.Time

	// Lifecycle
	mu      sync.RWMutex
	running bool
}

// New creates a new App instance.
func New(cfg *config.Config, version string) (*App, error) {
	sessionID := uuid.New().String()

	app := &App{
		cfg:       cfg,
		version:   version,
		hub:       hub.New(),
		sessionID: sessionID,
	}

	return app, nil
}

// Start starts the application and blocks until context is cancelled.
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return fmt.Errorf("application is already running")
	}
	a.running = true
	a.startTime = time.Now()
	a.mu.Unlock()

	// Start event hub
	if err := a.hub.Start(); err != nil {
		return fmt.Errorf("failed to start event hub: %w", err)
	}

	// Add log subscriber for debugging
	logSub := hub.NewLogSubscriber("internal-logger", func(event events.Event) {
		log.Trace().
			Str("event_type", string(event.Type())).
			Time("timestamp", event.Timestamp()).
			Msg("event broadcast")
	})
	a.hub.Subscribe(logSub)

	// Initialize Claude Manager
	a.claudeManager = claude.NewManager(
		a.cfg.Claude.Command,
		a.cfg.Claude.Args,
		a.cfg.Claude.TimeoutMinutes,
		a.hub,
		a.cfg.Claude.SkipPermissions,
	)
	// Set Claude working directory to repository path
	a.claudeManager.SetWorkDir(a.cfg.Repository.Path)

	// Create .cdev directory structure
	cdevDir := filepath.Join(a.cfg.Repository.Path, ".cdev")
	cdevLogsDir := filepath.Join(cdevDir, "logs")
	cdevImagesDir := filepath.Join(cdevDir, "images")
	if err := os.MkdirAll(cdevLogsDir, 0755); err != nil {
		log.Warn().Err(err).Msg("failed to create .cdev/logs directory")
	}
	if err := os.MkdirAll(cdevImagesDir, 0755); err != nil {
		log.Warn().Err(err).Msg("failed to create .cdev/images directory")
	}

	// Enable Claude output logging to .cdev/logs directory
	a.claudeManager.SetLogDir(cdevLogsDir)

	// Initialize Git Tracker
	a.gitTracker = git.NewTracker(
		a.cfg.Repository.Path,
		a.cfg.Git.Command,
		a.hub,
	)

	// Initialize File Watcher
	if a.cfg.Watcher.Enabled {
		a.fileWatcher = watcher.NewWatcher(
			a.cfg.Repository.Path,
			a.hub,
			a.cfg.Watcher.DebounceMS,
			a.cfg.Watcher.IgnorePatterns,
		)

		// Subscribe to file change events to generate git diffs
		if a.cfg.Git.Enabled && a.cfg.Git.DiffOnChange {
			fileChangeSub := hub.NewLogSubscriber("git-diff-generator", func(event events.Event) {
				if event.Type() == events.EventTypeFileChanged {
					go a.handleFileChangeForGitDiff(ctx, event)
				}
			})
			a.hub.Subscribe(fileChangeSub)
		}

		// Subscribe to file change events for repository indexer incremental updates
		repoIndexSub := hub.NewLogSubscriber("repo-index-updater", func(event events.Event) {
			if event.Type() == events.EventTypeFileChanged && a.repoIndexer != nil {
				go a.handleFileChangeForRepoIndex(ctx, event)
			}
		})
		a.hub.Subscribe(repoIndexSub)
	}

	// Initialize Session Cache for fast session listing
	sessionCache, err := sessioncache.New(a.cfg.Repository.Path)
	if err != nil {
		log.Warn().Err(err).Msg("failed to create session cache, falling back to direct file access")
	} else {
		a.sessionCache = sessionCache
		if err := a.sessionCache.Start(); err != nil {
			log.Warn().Err(err).Msg("failed to start session cache")
		}
	}

	// Initialize Message Cache for fast paginated message retrieval
	sessionsDir := claude.GetSessionsDir(a.cfg.Repository.Path)
	messageCache, err := sessioncache.NewMessageCache(sessionsDir)
	if err != nil {
		log.Warn().Err(err).Msg("failed to create message cache, falling back to direct file access")
	} else {
		a.messageCache = messageCache
		log.Info().Msg("message cache initialized for fast pagination")
	}

	// Initialize Session Streamer for real-time session watching
	// This allows iOS to receive messages when Claude Code runs directly on laptop
	a.sessionStreamer = sessioncache.NewSessionStreamer(sessionsDir, a.hub)
	log.Info().Msg("session streamer initialized for real-time watching")

	// Initialize Repository Indexer
	if a.cfg.Indexer.Enabled {
		repoIndexer, err := repository.NewIndexer(a.cfg.Repository.Path, a.cfg.Indexer.SkipDirectories)
		if err != nil {
			log.Warn().Err(err).Msg("failed to create repository indexer")
		} else {
			a.repoIndexer = repoIndexer
			if err := a.repoIndexer.Start(ctx); err != nil {
				log.Warn().Err(err).Msg("failed to start repository indexer")
			} else {
				log.Info().Int("skip_dirs", len(a.cfg.Indexer.SkipDirectories)).Msg("repository indexer started")
			}
		}
	} else {
		log.Info().Msg("repository indexer disabled by config")
	}

	// Initialize QR Generator
	repoName := filepath.Base(a.cfg.Repository.Path)
	if a.gitTracker.IsGitRepo() {
		repoName = a.gitTracker.GetRepoName()
	}
	// Use HTTPPort for both since WebSocket is now consolidated at /ws endpoint
	a.qrGenerator = pairing.NewQRGenerator(
		a.cfg.Server.Host,
		a.cfg.Server.HTTPPort, // Same port for WebSocket (/ws endpoint)
		a.cfg.Server.HTTPPort,
		a.sessionID,
		repoName,
	)

	// Set external URLs if configured (for VS Code port forwarding, tunnels, etc.)
	if a.cfg.Server.ExternalWSURL != "" || a.cfg.Server.ExternalHTTPURL != "" {
		a.qrGenerator.SetExternalURLs(a.cfg.Server.ExternalWSURL, a.cfg.Server.ExternalHTTPURL)
		log.Info().
			Str("external_ws_url", a.cfg.Server.ExternalWSURL).
			Str("external_http_url", a.cfg.Server.ExternalHTTPURL).
			Msg("using external URLs for QR code")
	}

	// Log startup info
	log.Info().
		Str("session_id", a.sessionID).
		Str("repo_path", a.cfg.Repository.Path).
		Str("repo_name", repoName).
		Msg("session started")

	// Print connection info
	a.printConnectionInfo()

	// Create RPC registry and dispatcher for JSON-RPC methods
	rpcRegistry := handler.NewRegistry()

	// Register RPC method services
	// Status service
	statusService := methods.NewStatusService(a)
	statusService.RegisterMethods(rpcRegistry)

	// Agent service (using Claude manager wrapped as AgentManager)
	agentService := methods.NewAgentService(NewClaudeAgentAdapter(a.claudeManager))
	agentService.RegisterMethods(rpcRegistry)

	// Git service
	if a.gitTracker != nil {
		gitService := methods.NewGitService(NewGitProviderAdapter(a.gitTracker))
		gitService.RegisterMethods(rpcRegistry)
	}

	// File service
	fileService := methods.NewFileService(NewFileProviderAdapter(a.gitTracker), a.cfg.Limits.MaxFileSizeKB)
	fileService.RegisterMethods(rpcRegistry)

	// Session service (pass streamer for real-time watching via RPC)
	sessionService := methods.NewSessionService(a.sessionStreamer)
	if a.sessionCache != nil && a.messageCache != nil {
		sessionService.RegisterProvider(NewClaudeSessionAdapter(a.sessionCache, a.messageCache, a.cfg.Repository.Path))
	}
	sessionService.RegisterMethods(rpcRegistry)

	// Lifecycle service with capabilities
	caps := methods.ServerCapabilities{
		Agent: &methods.AgentCapabilities{
			Run:          a.claudeManager != nil,
			Stop:         a.claudeManager != nil,
			Respond:      a.claudeManager != nil,
			Sessions:     a.sessionCache != nil,
			SessionWatch: a.sessionCache != nil,
		},
		Git: &methods.GitCapabilities{
			Status:   a.gitTracker != nil,
			Diff:     a.gitTracker != nil,
			Stage:    a.gitTracker != nil,
			Unstage:  a.gitTracker != nil,
			Commit:   a.gitTracker != nil,
			Push:     a.gitTracker != nil,
			Pull:     a.gitTracker != nil,
			Branches: a.gitTracker != nil,
			Checkout: a.gitTracker != nil,
		},
		File: &methods.FileCapabilities{
			Get:  a.gitTracker != nil,
			List: a.repoIndexer != nil,
		},
		Repository: &methods.RepositoryCapabilities{
			Index:  a.repoIndexer != nil,
			Search: a.repoIndexer != nil,
			Tree:   a.repoIndexer != nil,
		},
		Notifications:   []string{"agent_log", "agent_state", "file_changed", "git_status"},
		SupportedAgents: []string{"claude"},
	}
	lifecycleService := methods.NewLifecycleService(a.version, caps)
	lifecycleService.RegisterMethods(rpcRegistry)

	a.rpcDispatcher = handler.NewDispatcher(rpcRegistry)

	// Create unified server for dual-protocol WebSocket support
	// For port consolidation, we use the HTTP server's port
	a.unifiedServer = unified.NewServer(
		a.cfg.Server.Host,
		a.cfg.Server.HTTPPort,
		a.rpcDispatcher,
		a.hub,
	)
	// Set legacy handler for backward compatibility with existing clients
	a.unifiedServer.SetLegacyHandler(a.handleLegacyCommand)
	// Set status provider for heartbeats
	a.unifiedServer.SetStatusProvider(a)
	// Start background tasks (heartbeat)
	a.unifiedServer.StartBackgroundTasks()

	// Start HTTP server with unified WebSocket handler
	a.httpServer = httpserver.New(
		a.cfg.Server.Host,
		a.cfg.Server.HTTPPort,
		a.getStatus,
		a.claudeManager,
		a.gitTracker,
		a.sessionCache,
		a.messageCache,
		a.hub,
		a.cfg.Limits.MaxFileSizeKB,
		a.cfg.Repository.Path,
	)
	// Set repository indexer for file search APIs
	if a.repoIndexer != nil {
		a.httpServer.SetRepositoryIndexer(a.repoIndexer)
	}
	// Set RPC registry for dynamic OpenRPC spec generation
	a.httpServer.SetRPCRegistry(rpcRegistry)
	// Set WebSocket handler for port consolidation
	a.httpServer.SetWebSocketHandler(a.unifiedServer.HandleWebSocket)
	if err := a.httpServer.Start(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Start file watcher
	if a.fileWatcher != nil {
		if err := a.fileWatcher.Start(ctx); err != nil {
			log.Warn().Err(err).Msg("failed to start file watcher")
		}
	}

	// Publish session start event
	a.hub.Publish(events.NewSessionStartEvent(
		a.sessionID,
		a.cfg.Repository.Path,
		repoName,
		a.version,
	))

	// Wait for context cancellation
	<-ctx.Done()

	// Graceful shutdown
	return a.shutdown()
}

// shutdown performs graceful shutdown of all components.
func (a *App) shutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return nil
	}
	a.running = false

	log.Info().Msg("shutting down...")

	// Publish session end event
	a.hub.Publish(events.NewSessionEndEvent(a.sessionID, "shutdown"))

	// Give events time to be delivered
	time.Sleep(100 * time.Millisecond)

	// Stop file watcher
	if a.fileWatcher != nil {
		a.fileWatcher.Stop()
	}

	// Stop session cache
	if a.sessionCache != nil {
		a.sessionCache.Stop()
	}

	// Stop message cache
	if a.messageCache != nil {
		a.messageCache.Close()
	}

	// Stop session streamer
	if a.sessionStreamer != nil {
		a.sessionStreamer.Close()
	}

	// Stop repository indexer
	if a.repoIndexer != nil {
		if err := a.repoIndexer.Stop(); err != nil {
			log.Error().Err(err).Msg("error stopping repository indexer")
		}
	}

	// Stop Claude if running
	if a.claudeManager != nil && a.claudeManager.IsRunning() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.claudeManager.Stop(ctx)
		cancel()
	}

	// Stop unified server
	if a.unifiedServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.unifiedServer.Stop(shutdownCtx)
		cancel()
	}

	// Stop HTTP server
	if a.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		a.httpServer.Stop(shutdownCtx)
		cancel()
	}

	// Stop hub
	if err := a.hub.Stop(); err != nil {
		log.Error().Err(err).Msg("error stopping event hub")
	}

	return nil
}

// GetAgentStatus returns the current agent status for heartbeats.
// Implements common.StatusProvider.
func (a *App) GetAgentStatus() string {
	if a.claudeManager == nil {
		return string(events.ClaudeStateIdle)
	}
	return string(a.claudeManager.State())
}

// GetUptimeSeconds returns the server uptime in seconds.
// Implements common.StatusProvider.
func (a *App) GetUptimeSeconds() int64 {
	return a.UptimeSeconds()
}

// handleLegacyCommand handles incoming legacy WebSocket commands.
// This is used by the unified server for backward compatibility.
func (a *App) handleLegacyCommand(clientID string, cmd *commands.Command) {
	log.Debug().
		Str("client_id", clientID).
		Str("command", string(cmd.Command)).
		Msg("received legacy command")

	ctx := context.Background()

	switch cmd.Command {
	case commands.CommandRunClaude:
		payload, err := cmd.ParseRunClaudePayload()
		if err != nil {
			a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "Invalid run_claude payload", cmd.RequestID)
			return
		}

		// Determine session mode
		mode := claude.SessionModeNew
		switch payload.Mode {
		case "continue":
			mode = claude.SessionModeContinue
			if payload.SessionID == "" {
				a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "session_id is required when mode is 'continue'", cmd.RequestID)
				return
			}
		case "", "new":
			mode = claude.SessionModeNew
		default:
			a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "Invalid mode. Must be 'new' or 'continue'", cmd.RequestID)
			return
		}

		if err := a.claudeManager.StartWithSession(ctx, payload.Prompt, mode, payload.SessionID); err != nil {
			a.sendErrorToClient(clientID, "CLAUDE_ERROR", err.Error(), cmd.RequestID)
			return
		}

	case commands.CommandStopClaude:
		if err := a.claudeManager.Stop(ctx); err != nil {
			a.sendErrorToClient(clientID, "CLAUDE_ERROR", err.Error(), cmd.RequestID)
			return
		}

	case commands.CommandRespondToClaude:
		payload, err := cmd.ParseRespondToClaudePayload()
		if err != nil {
			a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "Invalid respond_to_claude payload", cmd.RequestID)
			return
		}
		if err := a.claudeManager.SendResponse(payload.ToolUseID, payload.Response, payload.IsError); err != nil {
			a.sendErrorToClient(clientID, "CLAUDE_ERROR", err.Error(), cmd.RequestID)
			return
		}

	case commands.CommandGetStatus:
		event := events.NewStatusResponseEvent(events.StatusResponsePayload{
			ClaudeState:      a.claudeManager.State(),
			ConnectedClients: a.unifiedServer.ClientCount(),
			RepoPath:         a.cfg.Repository.Path,
			RepoName:         filepath.Base(a.cfg.Repository.Path),
			UptimeSeconds:    a.UptimeSeconds(),
			AgentVersion:     a.version,
			WatcherEnabled:   a.cfg.Watcher.Enabled,
			GitEnabled:       a.cfg.Git.Enabled,
		}, cmd.RequestID)
		a.sendEventToClient(clientID, event)
		return

	case commands.CommandGetFile:
		payload, err := cmd.ParseGetFilePayload()
		if err != nil {
			a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "Invalid get_file payload", cmd.RequestID)
			return
		}
		content, truncated, err := a.gitTracker.GetFileContent(ctx, payload.Path, a.cfg.Limits.MaxFileSizeKB)
		if err != nil {
			a.sendEventToClient(clientID, events.NewFileContentErrorEvent(payload.Path, err.Error(), cmd.RequestID))
			return
		}
		a.sendEventToClient(clientID, events.NewFileContentEvent(payload.Path, content, int64(len(content)), truncated, cmd.RequestID))
		return

	case commands.CommandWatchSession:
		payload, err := cmd.ParseWatchSessionPayload()
		if err != nil {
			a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "Invalid watch_session payload", cmd.RequestID)
			return
		}
		if payload.SessionID == "" {
			a.sendErrorToClient(clientID, "INVALID_PAYLOAD", "session_id is required", cmd.RequestID)
			return
		}
		if err := a.sessionStreamer.WatchSession(payload.SessionID); err != nil {
			a.sendErrorToClient(clientID, "WATCH_FAILED", err.Error(), cmd.RequestID)
			return
		}
		// Send confirmation
		a.sendEventToClient(clientID, events.NewEvent("session_watch_started", map[string]interface{}{
			"session_id": payload.SessionID,
			"watching":   true,
		}))
		log.Info().Str("session_id", payload.SessionID).Str("client_id", clientID).Msg("client started watching session")
		return

	case commands.CommandUnwatchSession:
		watchedSession := a.sessionStreamer.GetWatchedSession()
		a.sessionStreamer.UnwatchSession()
		// Send confirmation
		a.sendEventToClient(clientID, events.NewEvent("session_watch_stopped", map[string]interface{}{
			"session_id": watchedSession,
			"watching":   false,
			"reason":     "client_request",
		}))
		log.Info().Str("session_id", watchedSession).Str("client_id", clientID).Msg("client stopped watching session")
		return

	default:
		a.sendErrorToClient(clientID, "UNKNOWN_COMMAND", fmt.Sprintf("Unknown command: %s", cmd.Command), cmd.RequestID)
	}
}

// sendErrorToClient sends an error event to a specific client.
func (a *App) sendErrorToClient(clientID, code, message, requestID string) {
	event := events.NewErrorEvent(code, message, requestID, nil)
	a.sendEventToClient(clientID, event)
}

// sendEventToClient sends an event to a specific client.
func (a *App) sendEventToClient(clientID string, event events.Event) {
	if a.unifiedServer == nil {
		return
	}
	client := a.unifiedServer.GetClient(clientID)
	if client == nil {
		return
	}
	data, err := event.ToJSON()
	if err != nil {
		return
	}
	client.SendRaw(data)
}

// handleFileChangeForGitDiff generates a git diff when a file changes.
func (a *App) handleFileChangeForGitDiff(ctx context.Context, event events.Event) {
	data, err := event.ToJSON()
	if err != nil {
		return
	}

	var wrapper struct {
		Payload struct {
			Path   string `json:"path"`
			Change string `json:"change"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return
	}

	// Don't generate diff for deleted files
	if wrapper.Payload.Change == "deleted" {
		return
	}

	// Wait a bit for file to be fully written
	time.Sleep(50 * time.Millisecond)

	// Generate git diff
	diffEvent := a.gitTracker.GetDiffForEvent(ctx, wrapper.Payload.Path)
	if diffEvent != nil {
		a.hub.Publish(diffEvent)
	}
}

// handleFileChangeForRepoIndex updates the repository index when a file changes.
func (a *App) handleFileChangeForRepoIndex(ctx context.Context, event events.Event) {
	if a.repoIndexer == nil {
		return
	}

	data, err := event.ToJSON()
	if err != nil {
		return
	}

	var wrapper struct {
		Payload struct {
			Path    string `json:"path"`
			Change  string `json:"change"`
			OldPath string `json:"old_path,omitempty"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return
	}

	log.Debug().
		Str("path", wrapper.Payload.Path).
		Str("change", wrapper.Payload.Change).
		Str("old_path", wrapper.Payload.OldPath).
		Msg("repo indexer received file change event")

	// Wait a bit for file to be fully written
	time.Sleep(50 * time.Millisecond)

	switch wrapper.Payload.Change {
	case "deleted":
		// Remove file from index
		if err := a.repoIndexer.RemoveFile(ctx, wrapper.Payload.Path); err != nil {
			log.Warn().Err(err).Str("path", wrapper.Payload.Path).Msg("failed to remove file from index")
		} else {
			log.Info().Str("path", wrapper.Payload.Path).Msg("removed file from index")
		}

	case "renamed":
		// Rename event now includes both old_path and path (new path)
		// 1. Remove old path from index
		// 2. Index the new path
		if wrapper.Payload.OldPath != "" {
			if err := a.repoIndexer.RemoveFile(ctx, wrapper.Payload.OldPath); err != nil {
				log.Warn().Err(err).Str("old_path", wrapper.Payload.OldPath).Msg("failed to remove old file from index")
			}
		}
		if err := a.repoIndexer.IndexFile(ctx, wrapper.Payload.Path); err != nil {
			log.Warn().Err(err).Str("path", wrapper.Payload.Path).Msg("failed to index renamed file")
		} else {
			log.Info().
				Str("old_path", wrapper.Payload.OldPath).
				Str("new_path", wrapper.Payload.Path).
				Msg("indexed renamed file")
		}

	case "created", "modified":
		// Index the file (also handles rename detection via inode as backup)
		if err := a.repoIndexer.IndexFile(ctx, wrapper.Payload.Path); err != nil {
			log.Warn().Err(err).Str("path", wrapper.Payload.Path).Msg("failed to index file")
		} else {
			log.Debug().Str("path", wrapper.Payload.Path).Str("change", wrapper.Payload.Change).Msg("indexed file")
		}

	default:
		log.Warn().Str("path", wrapper.Payload.Path).Str("change", wrapper.Payload.Change).Msg("unknown file change type")
	}
}

// getStatus returns the current status for API responses.
func (a *App) getStatus() map[string]interface{} {
	claudeState := "idle"
	if a.claudeManager != nil {
		claudeState = string(a.claudeManager.State())
	}

	wsClients := 0
	if a.unifiedServer != nil {
		wsClients = a.unifiedServer.ClientCount()
	}

	// Get current Claude session ID (for continue operations)
	claudeSessionID := ""
	if a.claudeManager != nil {
		claudeSessionID = a.claudeManager.ClaudeSessionID()
	}

	return map[string]interface{}{
		"session_id":        claudeSessionID, // Claude session ID (for continue) - empty if no active session
		"agent_session_id":  a.sessionID,     // Agent instance ID (generated on startup)
		"version":           a.version,
		"repo_path":         a.cfg.Repository.Path,
		"repo_name":         filepath.Base(a.cfg.Repository.Path),
		"uptime_seconds":    a.UptimeSeconds(),
		"claude_state":      claudeState,
		"connected_clients": wsClients,
		"watcher_enabled":   a.cfg.Watcher.Enabled,
		"git_enabled":       a.cfg.Git.Enabled,
		"is_git_repo":       a.gitTracker != nil && a.gitTracker.IsGitRepo(),
	}
}

// printConnectionInfo prints connection information to the console.
func (a *App) printConnectionInfo() {
	repoName := filepath.Base(a.cfg.Repository.Path)

	// Determine which URLs to display (external URLs take precedence)
	// With port consolidation, WebSocket is served at /ws on the HTTP port
	wsURL := fmt.Sprintf("ws://%s:%d/ws", a.cfg.Server.Host, a.cfg.Server.HTTPPort)
	httpURL := fmt.Sprintf("http://%s:%d", a.cfg.Server.Host, a.cfg.Server.HTTPPort)
	usingExternal := false

	if a.cfg.Server.ExternalWSURL != "" {
		wsURL = a.cfg.Server.ExternalWSURL
		usingExternal = true
	}
	if a.cfg.Server.ExternalHTTPURL != "" {
		httpURL = a.cfg.Server.ExternalHTTPURL
		usingExternal = true
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                     cdev ready                             ║")
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  Session ID: %-46s ║\n", a.sessionID[:8]+"...")
	fmt.Printf("║  Repository: %-46s ║\n", truncateString(repoName, 46))
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  API:        %-46s ║\n", truncateString(httpURL, 46))
	fmt.Printf("║  WebSocket:  %-46s ║\n", truncateString(wsURL, 46))
	if usingExternal {
		fmt.Println("║  (using external URLs for port forwarding)                 ║")
	}
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Scan QR code with cdev mobile app to connect              ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Display QR code
	if a.cfg.Pairing.ShowQRInTerminal && a.qrGenerator != nil {
		a.qrGenerator.PrintToTerminal()
	}
}

// GetSessionID returns the current session ID.
func (a *App) GetSessionID() string {
	return a.sessionID
}

// GetHub returns the event hub.
func (a *App) GetHub() *hub.Hub {
	return a.hub
}

// GetConfig returns the configuration.
func (a *App) GetConfig() *config.Config {
	return a.cfg
}

// UptimeSeconds returns how long the app has been running.
func (a *App) UptimeSeconds() int64 {
	return int64(time.Since(a.startTime).Seconds())
}

// truncateString truncates a string to maxLen characters.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
