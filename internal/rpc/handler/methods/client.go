package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// ClientFocusProvider provides client focus management operations.
type ClientFocusProvider interface {
	// SetSessionFocus updates the session focus for a client.
	// Returns focus change result with other viewers info and error.
	SetSessionFocus(clientID, workspaceID, sessionID string) (interface{}, error)
}

// ClientService provides client-related RPC methods.
type ClientService struct {
	provider ClientFocusProvider
}

// NewClientService creates a new client service.
func NewClientService(provider ClientFocusProvider) *ClientService {
	return &ClientService{
		provider: provider,
	}
}

// SetProvider sets the focus provider for the service.
// This allows the provider to be set after service initialization.
func (s *ClientService) SetProvider(provider ClientFocusProvider) {
	s.provider = provider
}

// RegisterMethods registers all client methods with the registry.
func (s *ClientService) RegisterMethods(r *handler.Registry) {
	r.RegisterWithMeta("client/session/focus", s.SessionFocus, handler.MethodMeta{
		Summary:     "Set session focus for current client",
		Description: "Notifies the server which session this client is currently viewing. The server will broadcast session_joined events to other clients viewing the same session.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Description: "Workspace ID", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "session_id", Description: "Session ID", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "FocusChangeResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/FocusChangeResult"}},
		Errors: []string{},
	})
}

// SessionFocusParams for client/session/focus method.
type SessionFocusParams struct {
	// WorkspaceID is the workspace ID.
	WorkspaceID string `json:"workspace_id"`

	// SessionID is the session ID being focused on.
	SessionID string `json:"session_id"`
}

// SessionFocus updates the session focus for the current client.
// This notifies the server which session the client is currently viewing,
// enabling multi-device awareness and notifications when other devices join the same session.
func (s *ClientService) SessionFocus(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrInternalError("focus provider not available")
	}

	// Parse params
	var p SessionFocusParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	// Validate
	if p.WorkspaceID == "" {
		return nil, message.ErrInvalidParams("workspace_id is required")
	}
	if p.SessionID == "" {
		return nil, message.ErrInvalidParams("session_id is required")
	}

	// Extract client ID from context
	clientID, ok := ctx.Value(handler.ClientIDKey).(string)
	if !ok || clientID == "" {
		return nil, message.ErrInternalError("client ID not found in context")
	}

	// Update focus
	result, err := s.provider.SetSessionFocus(clientID, p.WorkspaceID, p.SessionID)
	if err != nil {
		return nil, message.ErrInternalError(err.Error())
	}

	return result, nil
}
