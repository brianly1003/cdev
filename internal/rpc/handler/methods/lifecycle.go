// Package methods provides JSON-RPC method implementations.
package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// ServerCapabilities describes what the server can do.
type ServerCapabilities struct {
	// Agent operations (generic for any AI CLI)
	Agent *AgentCapabilities `json:"agent,omitempty"`

	// Git operations
	Git *GitCapabilities `json:"git,omitempty"`

	// File operations
	File *FileCapabilities `json:"file,omitempty"`

	// Repository indexing
	Repository *RepositoryCapabilities `json:"repository,omitempty"`

	// Notifications the server can send
	Notifications []string `json:"notifications,omitempty"`

	// SupportedAgents lists the configured agent types (e.g., "claude", "gemini", "codex")
	SupportedAgents []string `json:"supportedAgents,omitempty"`
}

// AgentCapabilities describes AI agent-related capabilities.
// This is CLI-agnostic and applies to Claude, Gemini, Codex, etc.
type AgentCapabilities struct {
	Run          bool `json:"run"`
	Stop         bool `json:"stop"`
	Respond      bool `json:"respond"`
	Sessions     bool `json:"sessions"`
	SessionWatch bool `json:"sessionWatch"`
}

// ClaudeCapabilities is an alias for backward compatibility.
// Deprecated: Use AgentCapabilities instead.
type ClaudeCapabilities = AgentCapabilities

// GitCapabilities describes Git-related capabilities.
type GitCapabilities struct {
	Status   bool `json:"status"`
	Diff     bool `json:"diff"`
	Stage    bool `json:"stage"`
	Unstage  bool `json:"unstage"`
	Discard  bool `json:"discard"`
	Commit   bool `json:"commit"`
	Push     bool `json:"push"`
	Pull     bool `json:"pull"`
	Branches bool `json:"branches"`
	Checkout bool `json:"checkout"`
}

// FileCapabilities describes file-related capabilities.
type FileCapabilities struct {
	Get         bool  `json:"get"`
	List        bool  `json:"list"`
	MaxFileSize int64 `json:"maxFileSize,omitempty"`
}

// RepositoryCapabilities describes repository indexer capabilities.
type RepositoryCapabilities struct {
	Index  bool `json:"index"`
	Search bool `json:"search"`
	Tree   bool `json:"tree"`
}

// LifecycleService handles initialization and shutdown.
type LifecycleService struct {
	version      string
	capabilities ServerCapabilities
	initialized  bool
	shutdownCh   chan struct{}
}

// NewLifecycleService creates a new lifecycle service.
func NewLifecycleService(version string, caps ServerCapabilities) *LifecycleService {
	return &LifecycleService{
		version:      version,
		capabilities: caps,
		shutdownCh:   make(chan struct{}),
	}
}

// RegisterMethods registers lifecycle methods with the registry.
func (s *LifecycleService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("initialize", s.Initialize, handler.MethodMeta{
		Summary:     "Initialize connection",
		Description: "Initialize the connection and negotiate capabilities. Must be called first before using other methods.",
		Params: []handler.OpenRPCParam{
			{Name: "protocolVersion", Description: "Protocol version the client supports", Required: false, Schema: map[string]interface{}{"type": "string"}},
			{Name: "clientInfo", Description: "Client information", Required: false, Schema: map[string]interface{}{"$ref": "#/components/schemas/ClientInfo"}},
			{Name: "capabilities", Description: "Client capabilities", Required: false, Schema: map[string]interface{}{"type": "object"}},
		},
		Result: &handler.OpenRPCResult{Name: "InitializeResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/InitializeResult"}},
	})

	r.RegisterWithMeta("initialized", s.Initialized, handler.MethodMeta{
		Summary:     "Confirm initialization",
		Description: "Confirm that the client has processed the initialize response. This is a notification (no response expected).",
		Params:      []handler.OpenRPCParam{},
	})

	r.RegisterWithMeta("shutdown", s.Shutdown, handler.MethodMeta{
		Summary:     "Shutdown server",
		Description: "Request the server to shut down gracefully.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "ShutdownResult", Schema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"status": map[string]interface{}{"type": "string"}}}},
	})
}

// IsInitialized returns true if the initialize handshake is complete.
func (s *LifecycleService) IsInitialized() bool {
	return s.initialized
}

// ShutdownChannel returns a channel that's closed when shutdown is requested.
func (s *LifecycleService) ShutdownChannel() <-chan struct{} {
	return s.shutdownCh
}

// InitializeParams from the client.
type InitializeParams struct {
	// ProtocolVersion is the protocol version the client supports.
	ProtocolVersion string `json:"protocolVersion"`

	// ClientInfo describes the client.
	ClientInfo *ClientInfo `json:"clientInfo,omitempty"`

	// Capabilities describes what the client can handle.
	Capabilities *ClientCapabilities `json:"capabilities,omitempty"`

	// RootPath is the root path of the workspace.
	RootPath string `json:"rootPath,omitempty"`
}

// ClientInfo describes the client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ClientCapabilities describes what the client can handle.
type ClientCapabilities struct {
	// Notifications the client can handle.
	Notifications []string `json:"notifications,omitempty"`
}

// InitializeResult is returned to the client.
type InitializeResult struct {
	// ProtocolVersion is the protocol version the server uses.
	ProtocolVersion string `json:"protocolVersion"`

	// ServerInfo describes the server.
	ServerInfo ServerInfo `json:"serverInfo"`

	// Capabilities describes what the server can do.
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerInfo describes the server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Initialize handles the initialize request.
// This must be the first request sent by the client.
func (s *LifecycleService) Initialize(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	var p InitializeParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.ErrInvalidParams("invalid initialize params: " + err.Error())
		}
	}

	// Log client info
	if p.ClientInfo != nil {
		// We could validate client version here if needed
	}

	return InitializeResult{
		ProtocolVersion: "1.0",
		ServerInfo: ServerInfo{
			Name:    "cdev",
			Version: s.version,
		},
		Capabilities: s.capabilities,
	}, nil
}

// Initialized is a notification from the client that initialization is complete.
// After this, the client can start sending other requests.
func (s *LifecycleService) Initialized(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	s.initialized = true
	return nil, nil // Notification - no response
}

// ShutdownResult is returned from shutdown.
type ShutdownResult struct {
	Success bool `json:"success"`
}

// Shutdown handles the shutdown request.
// After this, the client should send an 'exit' notification.
func (s *LifecycleService) Shutdown(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	// Signal shutdown
	select {
	case <-s.shutdownCh:
		// Already closed
	default:
		close(s.shutdownCh)
	}

	return ShutdownResult{Success: true}, nil
}

// DefaultCapabilities returns default server capabilities.
func DefaultCapabilities() ServerCapabilities {
	return ServerCapabilities{
		Agent: &AgentCapabilities{
			Run:          true,
			Stop:         true,
			Respond:      true,
			Sessions:     true,
			SessionWatch: true,
		},
		Git: &GitCapabilities{
			Status:   true,
			Diff:     true,
			Stage:    true,
			Unstage:  true,
			Discard:  true,
			Commit:   true,
			Push:     true,
			Pull:     true,
			Branches: true,
			Checkout: true,
		},
		File: &FileCapabilities{
			Get:         true,
			List:        true,
			MaxFileSize: 10 * 1024 * 1024, // 10MB
		},
		Repository: &RepositoryCapabilities{
			Index:  true,
			Search: true,
			Tree:   true,
		},
		Notifications: []string{
			// Agent events (generic for any AI CLI)
			"event/agent_log",
			"event/agent_status",
			"event/agent_message",
			"event/agent_waiting",
			"event/agent_permission",
			// File events
			"event/file_changed",
			// Git events
			"event/git_status_changed",
			"event/git_diff",
			// Connection events
			"event/heartbeat",
			// Session events
			"event/session_start",
			"event/session_end",
		},
		SupportedAgents: []string{"claude"}, // Default to Claude, expandable
	}
}

// DefaultCapabilitiesWithAgents returns capabilities with specific agents.
func DefaultCapabilitiesWithAgents(agents []string) ServerCapabilities {
	caps := DefaultCapabilities()
	if len(agents) > 0 {
		caps.SupportedAgents = agents
	}
	return caps
}
