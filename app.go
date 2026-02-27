package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/skip2/go-qrcode"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/brianly1003/cdev/internal/app"
	"github.com/brianly1003/cdev/internal/config"
)

// Repository represents a managed repository
type Repository struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	DisplayName string `json:"display_name"`
	GitBranch   string `json:"git_branch,omitempty"`
	GitRemote   string `json:"git_remote,omitempty"`
	IsActive    bool   `json:"is_active"`
	LastActive  string `json:"last_active,omitempty"`
}

// ConnectionStatus represents the current connection state
type ConnectionStatus struct {
	ServerRunning    bool   `json:"server_running"`
	ServerPort       int    `json:"server_port"`
	ServerAddress    string `json:"server_address"`
	ConnectedClients int    `json:"connected_clients"`
	ClaudeState      string `json:"claude_state"`
	ActiveRepo       string `json:"active_repo"`
	SessionID        string `json:"session_id,omitempty"`
}

// DesktopApp wraps the Portal Core for desktop GUI
type DesktopApp struct {
	ctx          context.Context
	portalCore   *app.App
	config       *config.Config
	repositories []Repository
	mu           sync.RWMutex
	cancelFunc   context.CancelFunc
}

// NewDesktopApp creates a new desktop application
func NewDesktopApp() *DesktopApp {
	return &DesktopApp{
		repositories: make([]Repository, 0),
	}
}

// startup is called when the app starts
func (d *DesktopApp) startup(ctx context.Context) {
	d.ctx = ctx

	// Load configuration
	cfg, err := config.Load("")
	if err != nil {
		log.Warn().Err(err).Msg("Failed to load config, using defaults")
		// Create default config manually
		cfg = &config.Config{
			Server: config.ServerConfig{
				Port: 16180,
				Host: "0.0.0.0",
			},
			Repository: config.RepositoryConfig{
				Path: "",
			},
			Claude: config.ClaudeConfig{
				Command:         "claude",
				SkipPermissions: false,
			},
		}
	}
	d.config = cfg

	// Add initial repository from config if set
	if cfg.Repository.Path != "" {
		repo := Repository{
			ID:          "repo_1",
			Path:        cfg.Repository.Path,
			DisplayName: filepath.Base(cfg.Repository.Path),
			IsActive:    true,
		}
		d.repositories = append(d.repositories, repo)
	}

	// Initialize Portal Core
	portalCore, err := app.New(cfg, "1.0.0")
	if err != nil {
		log.Error().Err(err).Msg("Failed to create Portal Core")
		runtime.EventsEmit(d.ctx, "portal_error", err.Error())
		return
	}
	d.portalCore = portalCore

	// Start Portal Core in background
	coreCtx, cancel := context.WithCancel(context.Background())
	d.cancelFunc = cancel

	go func() {
		if err := d.portalCore.Start(coreCtx); err != nil {
			log.Error().Err(err).Msg("Portal Core error")
			runtime.EventsEmit(d.ctx, "portal_error", err.Error())
		}
	}()

	log.Info().Msg("Desktop app started with Portal Core")
}

// shutdown is called when the app is closing
func (d *DesktopApp) shutdown(ctx context.Context) {
	log.Info().Msg("Shutting down desktop app")
	if d.cancelFunc != nil {
		d.cancelFunc()
	}
	// Portal Core will stop when context is cancelled
}

// domReady is called when the DOM is ready
func (d *DesktopApp) domReady(ctx context.Context) {
	log.Info().Msg("DOM ready")
}

// --- Repository Management ---

// GetRepositories returns all managed repositories
func (d *DesktopApp) GetRepositories() []Repository {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.repositories
}

// AddRepository adds a new repository
func (d *DesktopApp) AddRepository(path string, displayName string) (*Repository, error) {
	// Validate path exists and is a directory
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory")
	}

	// Check if it's a git repository
	gitPath := filepath.Join(path, ".git")
	if _, err := os.Stat(gitPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("not a git repository")
	}

	// Generate display name if not provided
	if displayName == "" {
		displayName = filepath.Base(path)
	}

	d.mu.Lock()
	repo := Repository{
		ID:          fmt.Sprintf("repo_%d", len(d.repositories)+1),
		Path:        path,
		DisplayName: displayName,
		IsActive:    len(d.repositories) == 0, // First repo is active
	}
	d.repositories = append(d.repositories, repo)
	d.mu.Unlock()

	// If this is the first/active repo, update config
	if repo.IsActive {
		d.setActiveRepository(repo.ID)
	}

	runtime.EventsEmit(d.ctx, "repository_added", repo)
	return &repo, nil
}

// RemoveRepository removes a repository
func (d *DesktopApp) RemoveRepository(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, repo := range d.repositories {
		if repo.ID == id {
			d.repositories = append(d.repositories[:i], d.repositories[i+1:]...)
			runtime.EventsEmit(d.ctx, "repository_removed", id)
			return nil
		}
	}
	return fmt.Errorf("repository not found")
}

// SwitchRepository switches to a different repository
func (d *DesktopApp) SwitchRepository(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	found := false
	for i := range d.repositories {
		if d.repositories[i].ID == id {
			d.repositories[i].IsActive = true
			found = true
		} else {
			d.repositories[i].IsActive = false
		}
	}

	if !found {
		return fmt.Errorf("repository not found")
	}

	d.setActiveRepository(id)
	runtime.EventsEmit(d.ctx, "repository_switched", id)
	return nil
}

func (d *DesktopApp) setActiveRepository(id string) {
	for _, repo := range d.repositories {
		if repo.ID == id {
			d.config.Repository.Path = repo.Path
			// TODO: Restart Portal Core with new config
			break
		}
	}
}

// --- Connection & Status ---

// GetConnectionStatus returns current connection status
func (d *DesktopApp) GetConnectionStatus() ConnectionStatus {
	status := ConnectionStatus{
		ServerRunning:    true,
		ServerPort:       d.config.Server.Port,
		ServerAddress:    d.getServerAddress(),
		ConnectedClients: 0, // TODO: Get from WebSocket hub
		ClaudeState:      "idle",
		ActiveRepo:       d.config.Repository.Path,
	}

	// Get active repo name
	for _, repo := range d.repositories {
		if repo.IsActive {
			status.ActiveRepo = repo.DisplayName
			break
		}
	}

	return status
}

// GetQRCodeData returns QR code as base64 data URL
func (d *DesktopApp) GetQRCodeData() (string, error) {
	// Build connection URL
	addr := d.getServerAddress()
	wsURL := fmt.Sprintf("ws://%s:%d/ws", addr, d.config.Server.Port)
	httpURL := fmt.Sprintf("http://%s:%d", addr, d.config.Server.Port)

	// Get repo name
	repoName := "unknown"
	for _, repo := range d.repositories {
		if repo.IsActive {
			repoName = repo.DisplayName
			break
		}
	}
	if repoName == "unknown" && d.config.Repository.Path != "" {
		repoName = filepath.Base(d.config.Repository.Path)
	}

	// QR payload
	payload := fmt.Sprintf(`{"ws":"%s","http":"%s","repo":"%s"}`,
		wsURL, httpURL, repoName)

	// Generate QR code
	qr, err := qrcode.New(payload, qrcode.Medium)
	if err != nil {
		return "", fmt.Errorf("failed to generate QR code: %w", err)
	}

	// Convert to PNG bytes
	png, err := qr.PNG(256)
	if err != nil {
		return "", fmt.Errorf("failed to encode QR code: %w", err)
	}

	// Return as data URL
	b64 := base64.StdEncoding.EncodeToString(png)
	return fmt.Sprintf("data:image/png;base64,%s", b64), nil
}

// GetConnectionURLs returns the connection URLs
func (d *DesktopApp) GetConnectionURLs() map[string]string {
	addr := d.getServerAddress()
	return map[string]string{
		"websocket": fmt.Sprintf("ws://%s:%d/ws", addr, d.config.Server.Port),
		"http":      fmt.Sprintf("http://%s:%d", addr, d.config.Server.Port),
	}
}

func (d *DesktopApp) getServerAddress() string {
	if d.config.Server.Host == "0.0.0.0" || d.config.Server.Host == "" {
		return getLocalIP()
	}
	return d.config.Server.Host
}

// --- Settings ---

// GetConfig returns current configuration as a map for frontend
func (d *DesktopApp) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"http_port":        d.config.Server.Port,
		"host":             d.config.Server.Host,
		"claude_command":   d.config.Claude.Command,
		"skip_permissions": d.config.Claude.SkipPermissions,
		"repository_path":  d.config.Repository.Path,
	}
}

// UpdateConfig updates configuration
func (d *DesktopApp) UpdateConfig(key string, value interface{}) error {
	switch key {
	case "http_port":
		if v, ok := value.(float64); ok {
			d.config.Server.Port = int(v)
		}
	case "host":
		if v, ok := value.(string); ok {
			d.config.Server.Host = v
		}
	case "claude_command":
		if v, ok := value.(string); ok {
			d.config.Claude.Command = v
		}
	case "skip_permissions":
		if v, ok := value.(bool); ok {
			d.config.Claude.SkipPermissions = v
		}
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}

	runtime.EventsEmit(d.ctx, "config_updated", d.GetConfig())
	return nil
}

// --- File Dialogs ---

// OpenDirectoryDialog opens a native directory picker
func (d *DesktopApp) OpenDirectoryDialog() (string, error) {
	return runtime.OpenDirectoryDialog(d.ctx, runtime.OpenDialogOptions{
		Title: "Select Repository",
	})
}

// --- Utility Functions ---

// getLocalIP returns the local IP address
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "localhost"
}
