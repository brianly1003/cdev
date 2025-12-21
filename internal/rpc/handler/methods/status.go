package methods

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// StatusProvider provides status information.
type StatusProvider interface {
	// ClaudeState returns the current Claude state.
	ClaudeState() string

	// ConnectedClients returns the number of connected clients.
	ConnectedClients() int

	// RepoPath returns the repository path.
	RepoPath() string

	// UptimeSeconds returns the uptime in seconds.
	UptimeSeconds() int64

	// Version returns the server version.
	Version() string

	// WatcherEnabled returns whether the file watcher is enabled.
	WatcherEnabled() bool

	// GitEnabled returns whether git integration is enabled.
	GitEnabled() bool
}

// StatusService provides status-related RPC methods.
type StatusService struct {
	provider  StatusProvider
	startTime time.Time
}

// NewStatusService creates a new status service.
func NewStatusService(provider StatusProvider) *StatusService {
	return &StatusService{
		provider:  provider,
		startTime: time.Now(),
	}
}

// RegisterMethods registers all status methods with the registry.
func (s *StatusService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("status/get", s.GetStatus, handler.MethodMeta{
		Summary:     "Get server status",
		Description: "Returns the current status of the cdev server including agent state, connected clients, and configuration.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "StatusResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/StatusResult"}},
	})

	r.RegisterWithMeta("status/health", s.Health, handler.MethodMeta{
		Summary:     "Health check",
		Description: "Returns a simple health check response for monitoring.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "HealthResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/HealthResult"}},
	})
}

// GetStatusResult for status/get method.
type GetStatusResult struct {
	// ClaudeState is the current Claude state.
	ClaudeState string `json:"claudeState"`

	// ConnectedClients is the number of connected clients.
	ConnectedClients int `json:"connectedClients"`

	// RepoPath is the repository path.
	RepoPath string `json:"repoPath"`

	// RepoName is the repository name (basename of path).
	RepoName string `json:"repoName"`

	// UptimeSeconds is the server uptime in seconds.
	UptimeSeconds int64 `json:"uptimeSeconds"`

	// AgentVersion is the server version.
	AgentVersion string `json:"agentVersion"`

	// WatcherEnabled indicates if the file watcher is enabled.
	WatcherEnabled bool `json:"watcherEnabled"`

	// GitEnabled indicates if git integration is enabled.
	GitEnabled bool `json:"gitEnabled"`
}

// GetStatus returns the current server status.
// This consolidates logic from:
// - app.go:421-433 (WebSocket handler)
// - http/server.go:264-272 (HTTP handler)
func (s *StatusService) GetStatus(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrInternalError("Status provider not available")
	}

	repoPath := s.provider.RepoPath()

	return GetStatusResult{
		ClaudeState:      s.provider.ClaudeState(),
		ConnectedClients: s.provider.ConnectedClients(),
		RepoPath:         repoPath,
		RepoName:         filepath.Base(repoPath),
		UptimeSeconds:    s.provider.UptimeSeconds(),
		AgentVersion:     s.provider.Version(),
		WatcherEnabled:   s.provider.WatcherEnabled(),
		GitEnabled:       s.provider.GitEnabled(),
	}, nil
}

// HealthResult for status/health method.
type HealthResult struct {
	Status        string `json:"status"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

// Health returns a simple health check response.
func (s *StatusService) Health(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	uptime := int64(0)
	if s.provider != nil {
		uptime = s.provider.UptimeSeconds()
	}

	return HealthResult{
		Status:        "ok",
		UptimeSeconds: uptime,
	}, nil
}
