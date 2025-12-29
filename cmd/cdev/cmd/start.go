package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/brianly1003/cdev/internal/app"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	repoPath    string
	port        int
	externalURL string // Single URL that auto-derives WS and HTTP URLs
	headless    bool
)

// startCmd represents the start command.
var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the cdev server",
	Long: `Start the cdev server to begin monitoring your repository
and accepting connections from mobile devices.

Two modes are available:

Terminal Mode (default, --headless=false):
  - Runs in the current terminal with full PTY support
  - When mobile sends commands, Claude spawns in THIS terminal
  - You can interact with Claude locally AND via mobile
  - Permission prompts are visible and can be answered from either side

Headless Mode (--headless=true):
  - Runs as a background daemon
  - Claude runs as subprocess without terminal UI
  - Best for server deployments or background automation

Example:
  cdev start                           # Terminal mode (default)
  cdev start --headless                # Headless/daemon mode
  cdev start --repo /path/to/project
  cdev start --port 8766               # Custom port

VS Code Port Forwarding:
  When using VS Code port forwarding, just copy the forwarded URL and pass it:

  cdev start --external-url https://your-tunnel.devtunnels.ms

  This auto-derives both HTTP and WebSocket URLs from the single URL.`,
	RunE: runStart,
}

func init() {
	startCmd.Flags().StringVar(&repoPath, "repo", "", "path to repository (default: current directory)")
	startCmd.Flags().IntVar(&port, "port", 0, "server port for HTTP and WebSocket (default: 8766)")
	startCmd.Flags().StringVar(&externalURL, "external-url", "", "external URL for tunnels - auto-derives WS and HTTP URLs (e.g., https://tunnel.devtunnels.ms)")
	startCmd.Flags().BoolVar(&headless, "headless", false, "run in headless mode (no terminal UI, daemon mode)")
}

func runStart(cmd *cobra.Command, args []string) error {
	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override config with flags
	if repoPath != "" {
		cfg.Repository.Path = repoPath
	}
	if port != 0 {
		cfg.Server.Port = port
	}
	if externalURL != "" {
		cfg.Server.ExternalURL = externalURL
	}
	cfg.Server.Headless = headless

	// Re-validate after overrides
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Setup logging
	setupLogging(cfg)

	mode := "terminal"
	if cfg.Server.Headless {
		mode = "headless"
	}

	log.Info().
		Str("version", version).
		Str("mode", mode).
		Str("repo", cfg.Repository.Path).
		Int("port", cfg.Server.Port).
		Msg("starting cdev")

	// Create application
	application, err := app.New(cfg, version)
	if err != nil {
		return fmt.Errorf("failed to create application: %w", err)
	}

	// Setup signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("received shutdown signal")
		cancel()
	}()

	// Start the application
	if err := application.Start(ctx); err != nil {
		return fmt.Errorf("application error: %w", err)
	}

	log.Info().Msg("cdev stopped")
	return nil
}

func loadConfig() (*config.Config, error) {
	return config.Load(cfgFile)
}

func setupLogging(cfg *config.Config) {
	// Set log level
	level, err := zerolog.ParseLevel(cfg.Logging.Level)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	// Set output format
	if cfg.Logging.Format == "console" || verbose {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// Add verbose logging if flag is set
	if verbose {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
}

func printConfig(cfg *config.Config) {
	fmt.Println("Current Configuration:")
	fmt.Println("----------------------")
	fmt.Printf("Repository Path: %s\n", cfg.Repository.Path)
	fmt.Printf("Port:            %d\n", cfg.Server.Port)
	fmt.Printf("Host:            %s\n", cfg.Server.Host)
	fmt.Printf("Watcher Enabled: %t\n", cfg.Watcher.Enabled)
	fmt.Printf("Git Enabled:     %t\n", cfg.Git.Enabled)
	fmt.Printf("Log Level:       %s\n", cfg.Logging.Level)
	fmt.Printf("Log Format:      %s\n", cfg.Logging.Format)
}

// deriveExternalURLs derives HTTP and WebSocket URLs from a single base URL.
// Input: https://abc123x4-8766.asse.devtunnels.ms/ (or http://)
// Output: httpURL = https://abc123x4-8766.asse.devtunnels.ms
//
//	wsURL   = wss://abc123x4-8766.asse.devtunnels.ms/ws
func deriveExternalURLs(baseURL string) (httpURL, wsURL string) {
	// Trim trailing slashes
	baseURL = strings.TrimRight(baseURL, "/")

	// HTTP URL is the base URL as-is
	httpURL = baseURL

	// WebSocket URL: convert scheme and append /ws
	wsURL = baseURL
	if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	} else if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	}
	wsURL = wsURL + "/ws"

	return httpURL, wsURL
}
