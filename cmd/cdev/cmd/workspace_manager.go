package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
	"github.com/brianly1003/cdev/internal/server/workspacehttp"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/lmittmann/tint"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	managerConfigFile string
)

// workspaceManagerCmd is the parent command for workspace manager operations
var workspaceManagerCmd = &cobra.Command{
	Use:   "workspace-manager",
	Short: "Manage the multi-workspace service",
	Long: `Control the multi-workspace service that manages Claude sessions across repositories.

The workspace manager runs on a single port (default 8766) and manages all workspaces
in-process. Each workspace can have one active Claude session at a time.`,
}

// workspaceManagerStartCmd starts the workspace manager service
var workspaceManagerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the multi-workspace service",
	Long: `Start the multi-workspace service on port 8766 (default).

The workspace manager will:
- Load workspace configuration from ~/.cdev/workspaces.yaml
- Start an HTTP/WebSocket server on port 8766
- Manage Claude sessions for each workspace in-process
- Auto-stop idle sessions after 30 minutes (configurable)

Example:
  cdev workspace-manager start
  cdev workspace-manager start --config /path/to/workspaces.yaml`,
	RunE: runWorkspaceManagerStart,
}

// workspaceManagerStopCmd stops the workspace manager service
var workspaceManagerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the multi-workspace service",
	Long:  `Stop the running workspace manager service and all active sessions.`,
	RunE:  runWorkspaceManagerStop,
}

// workspaceManagerStatusCmd shows workspace manager status
var workspaceManagerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show workspace manager status",
	Long:  `Display the current status of the workspace manager service.`,
	RunE:  runWorkspaceManagerStatus,
}

func init() {
	// Add subcommands
	workspaceManagerCmd.AddCommand(workspaceManagerStartCmd)
	workspaceManagerCmd.AddCommand(workspaceManagerStopCmd)
	workspaceManagerCmd.AddCommand(workspaceManagerStatusCmd)

	// Flags for 'workspace-manager start'
	workspaceManagerStartCmd.Flags().StringVar(&managerConfigFile, "config", "", "workspace manager config file (default: ~/.cdev/workspaces.yaml)")
}

func runWorkspaceManagerStart(cmd *cobra.Command, args []string) error {
	fmt.Println("Starting multi-workspace service...")

	// 1. Load workspaces config
	var workspaceCfg *config.WorkspacesConfig
	var err error

	if managerConfigFile != "" {
		workspaceCfg, err = config.LoadWorkspaces(managerConfigFile)
	} else {
		workspaceCfg, err = config.LoadWorkspaces("")
	}

	if err != nil {
		return fmt.Errorf("failed to load workspaces config: %w", err)
	}

	// 2. Load main config for Claude, git settings
	mainCfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup logger with pretty console output
	logLevel := slog.LevelInfo
	zerologLevel := zerolog.InfoLevel
	if verbose || workspaceCfg.Manager.LogLevel == "debug" {
		logLevel = slog.LevelDebug
		zerologLevel = zerolog.DebugLevel
	}

	// Configure zerolog global logger for rpc package
	zerolog.SetGlobalLevel(zerologLevel)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Configure slog logger for workspace manager
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      logLevel,
		TimeFormat: time.Kitchen,
	}))

	logger.Info("Loaded workspace configuration",
		"workspaces", len(workspaceCfg.Workspaces),
		"port", workspaceCfg.Manager.Port,
	)

	// 3. Create event hub for event broadcasting
	eventHub := hub.New()
	if err := eventHub.Start(); err != nil {
		return fmt.Errorf("failed to start event hub: %w", err)
	}

	// 4. Create workspace config manager (CRUD operations)
	configPath := managerConfigFile
	if configPath == "" {
		configPath = config.DefaultWorkspacesPath()
	}
	configManager := workspace.NewConfigManager(workspaceCfg, configPath)

	// 5. Create session manager (manages Claude sessions)
	sessionManager := session.NewManager(eventHub, mainCfg, logger)
	if err := sessionManager.Start(); err != nil {
		return fmt.Errorf("failed to start session manager: %w", err)
	}

	// Register existing workspaces with session manager
	for _, ws := range configManager.ListWorkspaces() {
		sessionManager.RegisterWorkspace(ws)
	}

	// 6. Create JSON-RPC registry and dispatcher
	registry := handler.NewRegistry()
	dispatcher := handler.NewDispatcher(registry)

	// 7. Register workspace config service (CRUD)
	workspaceConfigService := methods.NewWorkspaceConfigService(sessionManager, configManager)
	workspaceConfigService.RegisterMethods(registry)

	// 8. Register session manager service (lifecycle)
	sessionManagerService := methods.NewSessionManagerService(sessionManager)
	sessionManagerService.RegisterMethods(registry)

	logger.Info("Registered JSON-RPC methods",
		"count", len(registry.Methods()),
	)

	// 9. Create and start HTTP server
	server := workspacehttp.NewServer(
		workspaceCfg.Manager.Host,
		workspaceCfg.Manager.Port,
		sessionManager,
		configManager,
		dispatcher,
		eventHub,
		logger,
	)

	if err := server.Start(); err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	fmt.Printf("\n✓ Multi-workspace service running on %s:%d\n", workspaceCfg.Manager.Host, workspaceCfg.Manager.Port)
	fmt.Printf("  - HTTP API: http://%s:%d/api/workspaces\n", workspaceCfg.Manager.Host, workspaceCfg.Manager.Port)
	fmt.Printf("  - WebSocket: ws://%s:%d/ws\n", workspaceCfg.Manager.Host, workspaceCfg.Manager.Port)
	fmt.Printf("  - Health: http://%s:%d/health\n\n", workspaceCfg.Manager.Host, workspaceCfg.Manager.Port)

	if len(workspaceCfg.Workspaces) > 0 {
		fmt.Printf("Configured workspaces: %d\n", len(workspaceCfg.Workspaces))
		for _, ws := range workspaceCfg.Workspaces {
			fmt.Printf("  - %s (%s)\n", ws.Name, ws.Path)
		}
		fmt.Println()
	} else {
		fmt.Println("No workspaces configured yet. Add one with:")
		fmt.Println("  cdev workspace add <path>")
		fmt.Println()
	}

	fmt.Println("Press Ctrl+C to stop...")

	// 10. Setup signal handling (SIGINT/SIGTERM)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 11. Block until shutdown signal
	<-sigChan

	fmt.Println("\nShutting down multi-workspace service...")

	// 12. Gracefully stop all components
	// Stop session manager (stops all sessions)
	if err := sessionManager.Stop(); err != nil {
		logger.Error("Error stopping session manager", "error", err)
	}

	// Stop HTTP server
	if err := server.Stop(); err != nil {
		logger.Error("Error stopping HTTP server", "error", err)
	}

	// Stop event hub
	if err := eventHub.Stop(); err != nil {
		logger.Error("Error stopping event hub", "error", err)
	}

	fmt.Println("Multi-workspace service stopped")
	return nil
}

func runWorkspaceManagerStop(cmd *cobra.Command, args []string) error {
	// TODO: Implement remote stop via API
	return fmt.Errorf("remote stop not yet implemented - use Ctrl+C to stop the running service")
}

func runWorkspaceManagerStatus(cmd *cobra.Command, args []string) error {
	// TODO: Implement remote status check via API
	return fmt.Errorf("remote status not yet implemented - check http://localhost:8766/health")
}
