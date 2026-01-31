// Package app orchestrates all components of cdev.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/claude"
	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/adapters/repository"
	"github.com/brianly1003/cdev/internal/adapters/sessioncache"
	"github.com/brianly1003/cdev/internal/adapters/watcher"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/hooks"
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/pairing"
	"github.com/brianly1003/cdev/internal/permission"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
	"github.com/brianly1003/cdev/internal/security"
	httpserver "github.com/brianly1003/cdev/internal/server/http"
	httpMiddleware "github.com/brianly1003/cdev/internal/server/http/middleware"
	"github.com/brianly1003/cdev/internal/server/unified"
	"github.com/brianly1003/cdev/internal/services/imagestorage"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/brianly1003/cdev/internal/terminal"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/fsnotify/fsnotify"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// App is the main application struct that orchestrates all components.
type App struct {
	cfg     *config.Config
	version string

	// Core components
	hub                 *hub.Hub
	claudeManager       *claude.Manager
	fileWatcher         *watcher.Watcher
	gitTracker          *git.Tracker
	sessionCache        *sessioncache.Cache
	messageCache        *sessioncache.MessageCache
	sessionStreamer     *sessioncache.SessionStreamer
	repoIndexer         *repository.SQLiteIndexer
	imageStorageManager *imagestorage.Manager
	httpServer          *httpserver.Server
	unifiedServer       *unified.Server
	rpcDispatcher       *handler.Dispatcher
	qrGenerator         *pairing.QRGenerator
	tokenManager        *security.TokenManager
	authRegistry        *security.AuthRegistry

	// Multi-workspace support
	sessionManager         *session.Manager
	workspaceConfigManager *workspace.ConfigManager
	gitTrackerManager      *workspace.GitTrackerManager

	// Permission hook bridge
	permissionManager *permission.MemoryManager

	// Claude Code hooks for external session capture
	hooksManager *hooks.Manager

	// Security
	rateLimiter *httpMiddleware.RateLimiter

	// Terminal mode support (headless=false)
	terminalRunner *terminal.Runner

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

	// Create terminal runner if not in headless mode
	if !cfg.Server.Headless {
		app.terminalRunner = terminal.NewRunner(
			cfg.Repository.Path,
			cfg.Claude.Command,
			cfg.Claude.Args,
		)
		log.Info().Msg("terminal mode enabled - Claude will run in current terminal")
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

	// Initialize Claude Manager (always needed for multi-workspace support)
	a.claudeManager = claude.NewManager(
		a.cfg.Claude.Command,
		a.cfg.Claude.Args,
		a.cfg.Claude.TimeoutMinutes,
		a.hub,
		a.cfg.Claude.SkipPermissions,
		&a.cfg.Logging.Rotation,
	)

	// Legacy single-repo mode: Initialize repo-dependent components only if repository.path is configured
	// New multi-workspace mode uses workspace/add API and doesn't require repository.path
	if a.cfg.Repository.Path != "" {
		// Set Claude working directory to repository path
		a.claudeManager.SetWorkDir(a.cfg.Repository.Path)

		// Create .cdev/logs directory for Claude session logs
		cdevLogsDir := filepath.Join(a.cfg.Repository.Path, ".cdev", "logs")
		if err := os.MkdirAll(cdevLogsDir, 0755); err != nil {
			log.Warn().Err(err).Msg("failed to create .cdev/logs directory")
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

		log.Info().Str("repo", a.cfg.Repository.Path).Msg("legacy single-repo mode initialized")
	} else {
		log.Info().Msg("no repository.path configured, using multi-workspace mode only")
	}

	// Initialize Multi-Workspace Support
	// Workspaces are managed dynamically via workspace/add API and persisted to workspaces.yaml
	workspacesPath := config.DefaultWorkspacesPath()
	workspacesCfg, err := config.LoadWorkspaces(workspacesPath)
	if err != nil {
		log.Warn().Err(err).Str("workspaces_path", workspacesPath).Msg("failed to load workspaces config, starting with empty workspace list")
		workspacesCfg = &config.WorkspacesConfig{
			Workspaces: []config.WorkspaceDefinition{},
		}
	}
	a.workspaceConfigManager = workspace.NewConfigManager(workspacesCfg, workspacesPath)
	log.Info().
		Str("workspaces_path", workspacesPath).
		Int("workspaces", len(a.workspaceConfigManager.ListWorkspaces())).
		Msg("workspace manager initialized")

	authRegistryPath := config.DefaultAuthRegistryPath()
	authRegistry, err := security.LoadAuthRegistry(authRegistryPath)
	if err != nil {
		log.Warn().Err(err).Str("auth_registry_path", authRegistryPath).Msg("failed to load auth registry")
	} else {
		a.authRegistry = authRegistry
		log.Info().Str("auth_registry_path", authRegistryPath).Msg("auth registry initialized")
	}

	// Initialize Image Storage Manager for per-workspace image uploads
	pathResolver := workspace.NewImageStoragePathResolver(a.workspaceConfigManager)
	a.imageStorageManager = imagestorage.NewManager(pathResolver)
	log.Info().Msg("image storage manager initialized (per-workspace)")

	// Initialize Claude Code Hooks Manager for external session capture
	// This allows cdev to receive real-time events from Claude running in VS Code, Cursor, or terminal
	a.hooksManager = hooks.NewManager(a.cfg.Server.Port)
	if !a.hooksManager.IsInstalled() {
		log.Info().Msg("Claude Code hooks not installed - installing for external session capture...")
		if err := a.hooksManager.Install(); err != nil {
			log.Warn().Err(err).Msg("failed to install Claude Code hooks - external session capture will not work")
		} else {
			log.Info().Msg("Claude Code hooks installed - external Claude sessions will now be captured")
		}
	} else {
		log.Debug().Str("status", a.hooksManager.Status()).Msg("Claude Code hooks already installed")
	}

	// Initialize GitTrackerManager for cached git operations
	// This provides lazy-init, cached git trackers for all workspaces
	gitTrackerConfig := workspace.GitTrackerManagerConfig{
		GitCommand:          a.cfg.Git.Command,
		HealthCheckInterval: 5 * time.Minute,
		OperationTimeout:    30 * time.Second,
		Logger:              a.newSlogLogger(),
	}
	a.gitTrackerManager = workspace.NewGitTrackerManager(gitTrackerConfig)

	// Connect workspace config manager to git tracker manager
	a.workspaceConfigManager.SetGitTrackerManager(a.gitTrackerManager)
	log.Info().Int("workspaces", len(a.workspaceConfigManager.ListWorkspaces())).Msg("git tracker manager initialized")

	// Session manager orchestrates Claude sessions across workspaces
	a.sessionManager = session.NewManager(a.hub, a.cfg, a.newSlogLogger())

	// Connect session manager to git tracker manager
	a.sessionManager.SetGitTrackerManager(a.gitTrackerManager)

	// Register persisted workspaces with the session manager
	registered := 0
	for _, ws := range a.workspaceConfigManager.ListWorkspaces() {
		a.sessionManager.RegisterWorkspace(ws)
		registered++
	}
	log.Info().Int("workspaces_registered", registered).Msg("session manager workspace registration complete")

	// Enable LIVE session support for the repository (only if path configured)
	if a.cfg.Repository.Path != "" {
		a.sessionManager.SetLiveSessionSupport(a.cfg.Repository.Path)
	}

	if err := a.sessionManager.Start(); err != nil {
		log.Warn().Err(err).Msg("failed to start session manager")
	}

	if a.authRegistry != nil {
		a.pruneAuthRegistry("startup")
		a.startAuthRegistryCleanup(ctx)
	}

	// Workspaces are loaded from config and updated via workspace/add API
	log.Info().Msg("multi-workspace support initialized (workspace/add persists updates)")

	// Initialize Permission Memory Manager (for hook bridge "Allow for Session" functionality)
	if a.cfg.Permissions.SessionMemory.Enabled {
		permConfig := permission.SessionMemoryConfig{
			Enabled:     true,
			TTL:         time.Duration(a.cfg.Permissions.SessionMemory.TTLSeconds) * time.Second,
			MaxPatterns: a.cfg.Permissions.SessionMemory.MaxPatterns,
		}
		a.permissionManager = permission.NewMemoryManager(permConfig, log.Logger)
		a.permissionManager.StartCleanup(ctx)
		log.Info().Msg("permission hook bridge enabled")
	}

	// Initialize Repository Indexer (only if repository.path is configured)
	if a.cfg.Indexer.Enabled && a.cfg.Repository.Path != "" {
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
	} else if a.cfg.Indexer.Enabled && a.cfg.Repository.Path == "" {
		log.Info().Msg("repository indexer skipped - no repository.path configured")
	} else {
		log.Info().Msg("repository indexer disabled by config")
	}

	// Initialize QR Generator
	repoName := "cdev"
	if a.cfg.Repository.Path != "" {
		repoName = filepath.Base(a.cfg.Repository.Path)
		if a.gitTracker != nil && a.gitTracker.IsGitRepo() {
			repoName = a.gitTracker.GetRepoName()
		}
	}
	// Create QR generator with unified port
	a.qrGenerator = pairing.NewQRGenerator(
		a.cfg.Server.Host,
		a.cfg.Server.Port,
		a.sessionID,
		repoName,
	)

	// Set external URL if configured (for VS Code port forwarding, tunnels, etc.)
	if a.cfg.Server.ExternalURL != "" {
		a.qrGenerator.SetExternalURL(a.cfg.Server.ExternalURL)
		log.Info().
			Str("external_url", a.cfg.Server.ExternalURL).
			Msg("using external URL for QR code")
	}

	// Generate pairing token for QR code if auth is required (before printing QR)
	if a.cfg.Security.RequireAuth {
		tokenManager, err := security.NewTokenManager(a.cfg.Security.TokenExpirySecs)
		if err != nil {
			log.Warn().Err(err).Msg("failed to create token manager for QR code")
		} else {
			a.tokenManager = tokenManager // Store for later reuse
			token, expiresAt, err := tokenManager.GeneratePairingToken()
			if err != nil {
				log.Warn().Err(err).Msg("failed to generate pairing token for QR code")
			} else {
				a.qrGenerator.SetToken(token)
				log.Info().
					Time("expires_at", expiresAt).
					Msg("pairing token generated for QR code")
			}
		}
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
		gitService := methods.NewGitService(NewGitProviderAdapter(a.gitTracker), a.cfg.Limits.MaxDiffSizeKB)
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

	// Workspace config service (workspace/list, workspace/add, etc.)
	workspaceConfigService := methods.NewWorkspaceConfigService(a.sessionManager, a.workspaceConfigManager, a.hub, a.authRegistry)
	workspaceConfigService.RegisterMethods(rpcRegistry)

	// Session manager service (session/start, session/stop, session/send, etc.)
	sessionManagerService := methods.NewSessionManagerService(a.sessionManager)
	sessionManagerService.RegisterMethods(rpcRegistry)

	// Repository service (repository/search, repository/files/list, etc.)
	if a.repoIndexer != nil {
		repositoryService := methods.NewRepositoryService(a.repoIndexer)
		repositoryService.RegisterMethods(rpcRegistry)
	}

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
			Index:   a.repoIndexer != nil,
			Search:  a.repoIndexer != nil,
			List:    a.repoIndexer != nil,
			Tree:    a.repoIndexer != nil,
			Stats:   a.repoIndexer != nil,
			Rebuild: a.repoIndexer != nil,
		},
		Notifications:   []string{"agent_log", "agent_state", "file_changed", "git_status"},
		SupportedAgents: []string{"claude"},
	}
	lifecycleService := methods.NewLifecycleService(a.version, caps)
	lifecycleService.RegisterMethods(rpcRegistry)

	// Subscription service (for workspace event filtering)
	subscriptionService := methods.NewSubscriptionService()
	subscriptionService.RegisterMethods(rpcRegistry)

	// Client service (for multi-device session awareness)
	// Provider will be set after unified server is created
	clientService := methods.NewClientService(nil)
	clientService.RegisterMethods(rpcRegistry)

	// Permission service (for hook bridge)
	if a.permissionManager != nil {
		permissionService := methods.NewPermissionService(
			a.permissionManager,
			a.hub,
			methods.NewWorkspaceIDResolver(),
		)
		permissionService.RegisterMethods(rpcRegistry)
	}

	a.rpcDispatcher = handler.NewDispatcher(rpcRegistry)

	// Create unified server for dual-protocol WebSocket support
	// For port consolidation, we use the HTTP server's port
	a.unifiedServer = unified.NewServer(
		a.cfg.Server.Host,
		a.cfg.Server.Port,
		a.rpcDispatcher,
		a.hub,
	)
	// Set status provider for heartbeats
	a.unifiedServer.SetStatusProvider(a)
	// Set subscription provider (unified server manages filtered subscribers)
	subscriptionService.SetProvider(a.unifiedServer)
	// Set git watcher manager (session manager starts/stops git watchers on subscribe/unsubscribe)
	subscriptionService.SetGitWatcherManager(a.sessionManager)
	// Set disconnect handler for cleanup when clients disconnect (git watchers, session streamers)
	a.unifiedServer.SetDisconnectHandler(a.sessionManager)
	// Set client focus provider (unified server tracks multi-device session awareness)
	clientFocusAdapter := NewClientFocusAdapter(a.unifiedServer)
	clientService.SetProvider(clientFocusAdapter)
	// Set viewer provider for workspace/list to include session viewers
	workspaceConfigService.SetViewerProvider(a.unifiedServer)
	// Set focus provider for session manager so workspace/session/watch updates viewers
	sessionManagerService.SetFocusProvider(clientFocusAdapter)
	// Start background tasks (heartbeat)
	a.unifiedServer.StartBackgroundTasks()

	// Start HTTP server with unified WebSocket handler
	a.httpServer = httpserver.New(
		a.cfg.Server.Host,
		a.cfg.Server.Port,
		a.getStatus,
		a.claudeManager,
		a.gitTracker,
		a.sessionCache,
		a.messageCache,
		a.hub,
		a.cfg.Limits.MaxFileSizeKB,
		a.cfg.Limits.MaxDiffSizeKB,
		a.cfg.Repository.Path,
	)
	// Set repository indexer for file search APIs
	if a.repoIndexer != nil {
		a.httpServer.SetRepositoryIndexer(a.repoIndexer)
	}
	// Set image storage manager for iOS image uploads (multi-workspace)
	if a.imageStorageManager != nil {
		a.httpServer.SetImageStorageManager(a.imageStorageManager)
	}

	// Set up pairing handler for mobile app connection
	// Reuse tokenManager if already created for QR code, otherwise create new one
	if a.tokenManager == nil {
		tokenManager, err := security.NewTokenManager(a.cfg.Security.TokenExpirySecs)
		if err != nil {
			log.Warn().Err(err).Msg("failed to create token manager, pairing will work without auth")
		}
		a.tokenManager = tokenManager
	}

	originChecker := security.NewOriginChecker(a.cfg.Security.AllowedOrigins, a.cfg.Security.BindLocalhostOnly)

	// Configure security on unified server
	a.unifiedServer.SetSecurity(a.tokenManager, originChecker, a.cfg.Security.RequireAuth)

	// Configure security on HTTP server (CORS origin checking)
	a.httpServer.SetOriginChecker(originChecker)
	// Configure HTTP auth (bearer tokens)
	a.httpServer.SetAuth(a.tokenManager, a.cfg.Security.RequireAuth)

	// Configure rate limiting if enabled
	if a.cfg.Security.RateLimit.Enabled {
		rateLimiter := httpMiddleware.NewRateLimiter(
			httpMiddleware.WithMaxRequests(a.cfg.Security.RateLimit.RequestsPerMinute),
			httpMiddleware.WithWindow(time.Minute),
		)
		a.httpServer.SetRateLimiter(rateLimiter)
		a.rateLimiter = rateLimiter
		log.Info().Int("requests_per_minute", a.cfg.Security.RateLimit.RequestsPerMinute).Msg("rate limiting enabled")
	}

	// Create pairing handler with function to get current pairing info
	pairingHandler := httpserver.NewPairingHandler(
		a.tokenManager,
		a.cfg.Security.RequireAuth,
		func() *pairing.PairingInfo {
			if a.qrGenerator != nil {
				return a.qrGenerator.GetPairingInfo()
			}
			return nil
		},
		a.cfg.Server.ExternalURL == "",
	)
	a.httpServer.SetPairingHandler(pairingHandler)

	// Create auth handler for token exchange and refresh
	if a.tokenManager != nil {
		authHandler := httpserver.NewAuthHandler(a.tokenManager, a.authRegistry, a.cleanupOrphanedWorkspaces)
		a.httpServer.SetAuthHandler(authHandler)
	}

	// Create debug handler for pprof and runtime info (when enabled)
	if a.cfg.Debug.Enabled {
		debugHandler := httpserver.NewDebugHandler(a.cfg.Debug.PprofEnabled)
		a.httpServer.SetDebugHandler(debugHandler)
		log.Info().Bool("pprof", a.cfg.Debug.PprofEnabled).Msg("debug endpoints enabled")
	}

	// Set up hooks handler for receiving Claude Code hook events
	// This enables real-time event capture from external Claude sessions (VS Code, Cursor, terminal)
	// The permission manager enables mobile permission approval for PreToolUse hooks
	hooksHandler := httpserver.NewHooksHandler(a.hub)
	if a.permissionManager != nil {
		hooksHandler.SetPermissionManager(a.permissionManager)
	}
	a.httpServer.SetHooksHandler(hooksHandler)

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

	// NOTE: Git state watcher is NOT started here at app startup.
	// It is started per-workspace when workspace/subscribe is called.
	// This prevents watching directories when no workspaces are active.

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

	// Remove Claude Code hooks on shutdown (clean exit)
	if a.hooksManager != nil && a.hooksManager.IsInstalled() {
		log.Info().Msg("removing Claude Code hooks...")
		if err := a.hooksManager.Uninstall(); err != nil {
			log.Warn().Err(err).Msg("failed to remove Claude Code hooks")
		} else {
			log.Info().Msg("Claude Code hooks removed")
		}
	}

	// Publish session end event
	a.hub.Publish(events.NewSessionEndEvent(a.sessionID, "shutdown"))

	// Give events time to be delivered
	time.Sleep(100 * time.Millisecond)

	// Stop file watcher
	if a.fileWatcher != nil {
		_ = a.fileWatcher.Stop()
	}

	// Stop session cache
	if a.sessionCache != nil {
		_ = a.sessionCache.Stop()
	}

	// Stop message cache
	if a.messageCache != nil {
		_ = a.messageCache.Close()
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
		_ = a.claudeManager.Stop(ctx)
		cancel()
	}

	// Stop session manager (stops all active sessions)
	if a.sessionManager != nil {
		if err := a.sessionManager.Stop(); err != nil {
			log.Error().Err(err).Msg("error stopping session manager")
		}
	}

	// Stop git tracker manager (cleans up cached trackers)
	if a.gitTrackerManager != nil {
		a.gitTrackerManager.Stop()
	}

	// Stop unified server
	if a.unifiedServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.unifiedServer.Stop(shutdownCtx)
		cancel()
	}

	// Stop HTTP server
	if a.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = a.httpServer.Stop(shutdownCtx)
		cancel()
	}

	// Stop rate limiter (cleanup goroutine)
	if a.rateLimiter != nil {
		a.rateLimiter.Close()
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
	diffEvent := a.gitTracker.GetDiffForEvent(ctx, wrapper.Payload.Path, a.cfg.Limits.MaxDiffSizeKB)
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

	// Skip files in directories that should not be indexed (use config values)
	for _, skipDir := range a.cfg.Indexer.SkipDirectories {
		if strings.HasPrefix(wrapper.Payload.Path, skipDir+"/") {
			return
		}
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

	// Determine which URLs to display (external URL takes precedence)
	wsURL := fmt.Sprintf("ws://%s:%d/ws", a.cfg.Server.Host, a.cfg.Server.Port)
	httpURL := fmt.Sprintf("http://%s:%d", a.cfg.Server.Host, a.cfg.Server.Port)
	usingExternal := false

	if a.cfg.Server.ExternalURL != "" {
		httpURL = strings.TrimRight(a.cfg.Server.ExternalURL, "/")
		// Derive WebSocket URL from HTTP URL
		wsURL = httpURL
		if strings.HasPrefix(wsURL, "https://") {
			wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
		} else if strings.HasPrefix(wsURL, "http://") {
			wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
		}
		wsURL = wsURL + "/ws"
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

// IsTerminalMode returns whether the app is running in terminal mode.
func (a *App) IsTerminalMode() bool {
	return a.terminalRunner != nil
}

// GetTerminalRunner returns the terminal runner (nil if in headless mode).
func (a *App) GetTerminalRunner() *terminal.Runner {
	return a.terminalRunner
}

// RunTerminalSession runs Claude in terminal mode (blocking).
// This takes over the current terminal and runs Claude interactively.
// Output is sent to both the local terminal and WebSocket clients.
func (a *App) RunTerminalSession(ctx context.Context, prompt string) error {
	if a.terminalRunner == nil {
		return fmt.Errorf("not in terminal mode")
	}

	// Set output writer to broadcast to WebSocket clients
	a.terminalRunner.SetOutputWriter(&wsOutputWriter{app: a})

	log.Info().
		Str("prompt", truncateString(prompt, 50)).
		Msg("starting terminal session")

	// Run Claude (blocks until completion)
	return a.terminalRunner.Run(ctx, prompt)
}

// SendTerminalInput sends input to the terminal session from WebSocket.
func (a *App) SendTerminalInput(data []byte) error {
	if a.terminalRunner == nil {
		return fmt.Errorf("not in terminal mode")
	}
	return a.terminalRunner.SendInput(data)
}

// wsOutputWriter writes PTY output to WebSocket clients.
type wsOutputWriter struct {
	app *App
}

func (w *wsOutputWriter) Write(p []byte) (n int, err error) {
	// Broadcast PTY output to all WebSocket clients as pty_output event
	if w.app.hub != nil {
		text := string(p)
		// Send same text as both clean and raw (terminal mode sends raw PTY output)
		event := events.NewPTYOutputEvent(text, text, "running")
		w.app.hub.Publish(event)
	}
	return len(p), nil
}

// watchGitState watches ONLY the .git directory for state changes and emits git_status_changed events.
// This is designed to be lightweight and not conflict with IDEs (VS Code, IntelliJ) or tools (SourceTree).
//
// We intentionally DO NOT watch the working directory because:
// 1. IDEs already watch working directory files - adding another watcher causes contention
// 2. Working directory changes don't affect git state until staged (git add)
// 3. The existing file_changed events already notify about working directory changes
//
// This covers:
// - Staging/unstaging: .git/index changes (git add, git reset)
// - Commits: .git/HEAD, .git/refs/heads/<branch> changes
// - Branch switches: .git/HEAD changes (git checkout, git switch)
// - Pull/Fetch: .git/FETCH_HEAD, .git/refs/remotes/* changes
// - Merges/Rebases: .git/ORIG_HEAD, .git/MERGE_HEAD changes
//
//nolint:unused
func (a *App) watchGitState(ctx context.Context) {
	repoPath := a.cfg.Repository.Path
	gitDir := filepath.Join(repoPath, ".git")

	// Check if .git directory exists
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		log.Debug().Msg("No .git directory, skipping git state watcher")
		return
	}

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create git state watcher")
		return
	}
	defer func() { _ = watcher.Close() }()

	// Watch .git directory for index, HEAD, FETCH_HEAD, ORIG_HEAD, MERGE_HEAD
	if err := watcher.Add(gitDir); err != nil {
		log.Warn().Err(err).Msg("Failed to watch .git directory")
		return
	}

	// Watch .git/refs/heads for branch commits
	refsHeads := filepath.Join(gitDir, "refs", "heads")
	if _, err := os.Stat(refsHeads); err == nil {
		if err := watcher.Add(refsHeads); err != nil {
			log.Debug().Err(err).Msg("Failed to watch refs/heads")
		}
	}

	// Watch .git/refs/remotes for pull/fetch updates
	refsRemotes := filepath.Join(gitDir, "refs", "remotes")
	if _, err := os.Stat(refsRemotes); err == nil {
		if err := watcher.Add(refsRemotes); err != nil {
			log.Debug().Err(err).Msg("Failed to watch refs/remotes")
		}
		// Also watch subdirectories (e.g., refs/remotes/origin)
		entries, _ := os.ReadDir(refsRemotes)
		for _, entry := range entries {
			if entry.IsDir() {
				remotePath := filepath.Join(refsRemotes, entry.Name())
				if err := watcher.Add(remotePath); err != nil {
					log.Debug().Str("remote", entry.Name()).Err(err).Msg("Failed to watch remote")
				}
			}
		}
	}

	log.Info().
		Str("git_dir", gitDir).
		Msg("Started git state watcher (watching .git only - IDE/SourceTree safe)")

	// Wait for startup activity to settle before processing events
	// This prevents initial burst of events during application startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Second):
		// Drain any pending events from startup
		for {
			select {
			case <-watcher.Events:
			default:
				goto startWatching
			}
		}
	}

startWatching:
	log.Debug().Msg("Git state watcher now active")

	// Files in .git that trigger git_status_changed events
	gitTriggerFiles := map[string]bool{
		"index":       true, // Staging/unstaging
		"HEAD":        true, // Commits, branch switches
		"FETCH_HEAD":  true, // Fetch/pull
		"ORIG_HEAD":   true, // Merges, rebases
		"MERGE_HEAD":  true, // Merge in progress
		"REBASE_HEAD": true, // Rebase in progress
	}

	// Throttle + Debounce: emit at most once per minInterval, with debounce for settling
	const debounceDelay = 500 * time.Millisecond
	const minInterval = 1 * time.Second // Minimum time between emits
	var debounceTimer *time.Timer
	var debounceTimerMu sync.Mutex
	var lastEmit time.Time

	for {
		select {
		case <-ctx.Done():
			log.Debug().Msg("Git state watcher stopped")
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Only trigger for specific .git files that indicate state changes
			fileName := filepath.Base(event.Name)
			relPath, _ := filepath.Rel(gitDir, event.Name)

			// Debug log all events to diagnose issues
			log.Debug().
				Str("file", fileName).
				Str("path", relPath).
				Str("op", event.Op.String()).
				Msg("Git watcher event received")

			shouldTrigger := false

			// Check for specific trigger files in .git root
			// Also check for index.lock as Git uses atomic rename
			if gitTriggerFiles[fileName] || fileName == "index.lock" {
				shouldTrigger = true
			}

			// Also trigger for any file in refs/heads or refs/remotes
			if strings.HasPrefix(relPath, "refs/heads") || strings.HasPrefix(relPath, "refs/remotes") {
				shouldTrigger = true
			}

			if !shouldTrigger {
				log.Debug().Str("file", fileName).Msg("Git event ignored (not a trigger file)")
				continue
			}

			log.Info().Str("file", fileName).Str("op", event.Op.String()).Msg("Git event detected, scheduling status update")

			// Throttle + debounce: only schedule emit if not within minInterval of last emit
			debounceTimerMu.Lock()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				debounceTimerMu.Lock()
				// Check if we're within minInterval of last emit
				if time.Since(lastEmit) < minInterval {
					debounceTimerMu.Unlock()
					return
				}
				lastEmit = time.Now()
				debounceTimerMu.Unlock()
				a.emitGitStatusChanged(ctx)
			})
			debounceTimerMu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("Git state watcher error")
		}
	}
}

// emitGitStatusChanged emits a git_status_changed event.
//
//nolint:unused
func (a *App) emitGitStatusChanged(ctx context.Context) {
	if a.gitTracker == nil {
		log.Debug().Msg("Git tracker is nil, skipping emit")
		return
	}

	log.Debug().Msg("Fetching git status for event...")

	// Use GetEnhancedStatus which provides all the info we need
	status, err := a.gitTracker.GetEnhancedStatus(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("Failed to get git status for event")
		return
	}

	log.Debug().
		Str("branch", status.Branch).
		Int("staged", len(status.Staged)).
		Int("unstaged", len(status.Unstaged)).
		Msg("Git status fetched")

	// Collect all changed file paths
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
	for _, f := range status.Conflicted {
		changedFiles = append(changedFiles, f.Path)
	}

	payload := events.GitStatusChangedPayload{
		Branch:         status.Branch,
		StagedCount:    len(status.Staged),
		UnstagedCount:  len(status.Unstaged),
		UntrackedCount: len(status.Untracked),
		ChangedFiles:   changedFiles,
	}

	event := events.NewEvent(events.EventTypeGitStatusChanged, payload)
	a.hub.Publish(event)

	log.Info().
		Str("branch", status.Branch).
		Int("staged", len(status.Staged)).
		Int("unstaged", len(status.Unstaged)).
		Int("untracked", len(status.Untracked)).
		Msg("Emitted git_status_changed")
}

// configLevelToSlog converts the config logging level string to slog.Level.
// This ensures slog-based components respect the same log level as zerolog.
func configLevelToSlog(level string) slog.Level {
	switch strings.ToLower(level) {
	case "trace", "debug":
		return slog.LevelDebug
	case "info", "":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error", "err", "fatal", "panic":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// newSlogLogger creates a slog.Logger that respects the app's logging configuration.
// It uses stderr for consistency with zerolog output.
func (a *App) newSlogLogger() *slog.Logger {
	level := configLevelToSlog(a.cfg.Logging.Level)
	opts := &slog.HandlerOptions{Level: level}

	// Use text handler for console format, JSON handler otherwise
	var handler slog.Handler
	if a.cfg.Logging.Format == "console" {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}
