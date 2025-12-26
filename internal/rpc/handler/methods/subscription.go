// Package methods provides JSON-RPC method implementations.
package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// FilteredSubscriberProvider provides access to filtered subscribers by client ID.
type FilteredSubscriberProvider interface {
	GetFilteredSubscriber(clientID string) *hub.FilteredSubscriber
}

// GitWatcherManager provides the ability to start/stop git watchers for workspaces.
type GitWatcherManager interface {
	StartGitWatcher(workspaceID string) error
	StopGitWatcher(workspaceID string)
}

// SubscriptionService handles workspace subscription RPC methods.
type SubscriptionService struct {
	provider          FilteredSubscriberProvider
	gitWatcherManager GitWatcherManager
}

// NewSubscriptionService creates a new subscription service.
func NewSubscriptionService() *SubscriptionService {
	return &SubscriptionService{}
}

// SetProvider sets the filtered subscriber provider.
// This is called after the server is created to avoid circular dependencies.
func (s *SubscriptionService) SetProvider(provider FilteredSubscriberProvider) {
	s.provider = provider
}

// SetGitWatcherManager sets the git watcher manager (session manager).
// This is called to enable git_status_changed events on workspace subscription.
func (s *SubscriptionService) SetGitWatcherManager(manager GitWatcherManager) {
	s.gitWatcherManager = manager
}

// RegisterMethods registers all subscription methods with the handler.
func (s *SubscriptionService) RegisterMethods(registry *handler.Registry) {
	registry.RegisterWithMeta("workspace/subscribe", s.Subscribe, handler.MethodMeta{
		Summary:     "Subscribe to workspace events",
		Description: "Subscribe to receive events for a specific workspace. By default, clients receive all events. After calling this method, only events for subscribed workspaces (and global events) will be forwarded.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/unsubscribe", s.Unsubscribe, handler.MethodMeta{
		Summary:     "Unsubscribe from workspace events",
		Description: "Stop receiving events for a specific workspace.",
		Params: []handler.OpenRPCParam{
			{Name: "workspace_id", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/subscriptions", s.Subscriptions, handler.MethodMeta{
		Summary:     "List workspace subscriptions",
		Description: "Returns the list of workspaces this client is subscribed to. Empty list means all events are received.",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "subscriptions",
			Schema: map[string]interface{}{"type": "object"},
		},
	})

	registry.RegisterWithMeta("workspace/subscribeAll", s.SubscribeAll, handler.MethodMeta{
		Summary:     "Subscribe to all workspace events",
		Description: "Clear workspace filters and receive events from all workspaces (default behavior).",
		Params:      []handler.OpenRPCParam{},
		Result: &handler.OpenRPCResult{
			Name:   "result",
			Schema: map[string]interface{}{"type": "object"},
		},
	})
}

// Subscribe adds a workspace to the client's subscription filter.
func (s *SubscriptionService) Subscribe(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.NewError(message.InternalError, "subscription provider not configured")
	}

	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Get client ID from context
	clientID, ok := ctx.Value(handler.ClientIDKey).(string)
	if !ok || clientID == "" {
		return nil, message.NewError(message.InternalError, "client ID not found in context")
	}

	filtered := s.provider.GetFilteredSubscriber(clientID)
	if filtered == nil {
		return nil, message.NewError(message.InternalError, "client not found")
	}

	filtered.SubscribeWorkspace(p.WorkspaceID)

	// Start git watcher for this workspace to emit git_status_changed events
	if s.gitWatcherManager != nil {
		if err := s.gitWatcherManager.StartGitWatcher(p.WorkspaceID); err != nil {
			// Log but don't fail the subscription - git watcher is optional
			// The subscription still succeeds, just without git events
		}
	}

	return map[string]interface{}{
		"success":      true,
		"workspace_id": p.WorkspaceID,
		"subscribed":   filtered.GetSubscribedWorkspaces(),
	}, nil
}

// Unsubscribe removes a workspace from the client's subscription filter.
func (s *SubscriptionService) Unsubscribe(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.NewError(message.InternalError, "subscription provider not configured")
	}

	var p struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.NewError(message.InvalidParams, "failed to parse params: "+err.Error())
	}

	if p.WorkspaceID == "" {
		return nil, message.NewError(message.InvalidParams, "workspace_id is required")
	}

	// Get client ID from context
	clientID, ok := ctx.Value(handler.ClientIDKey).(string)
	if !ok || clientID == "" {
		return nil, message.NewError(message.InternalError, "client ID not found in context")
	}

	filtered := s.provider.GetFilteredSubscriber(clientID)
	if filtered == nil {
		return nil, message.NewError(message.InternalError, "client not found")
	}

	filtered.UnsubscribeWorkspace(p.WorkspaceID)

	// Stop git watcher for this workspace (uses reference counting)
	if s.gitWatcherManager != nil {
		s.gitWatcherManager.StopGitWatcher(p.WorkspaceID)
	}

	return map[string]interface{}{
		"success":      true,
		"workspace_id": p.WorkspaceID,
		"subscribed":   filtered.GetSubscribedWorkspaces(),
	}, nil
}

// Subscriptions returns the client's current workspace subscriptions.
func (s *SubscriptionService) Subscriptions(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.NewError(message.InternalError, "subscription provider not configured")
	}

	// Get client ID from context
	clientID, ok := ctx.Value(handler.ClientIDKey).(string)
	if !ok || clientID == "" {
		return nil, message.NewError(message.InternalError, "client ID not found in context")
	}

	filtered := s.provider.GetFilteredSubscriber(clientID)
	if filtered == nil {
		return nil, message.NewError(message.InternalError, "client not found")
	}

	subscribed := filtered.GetSubscribedWorkspaces()
	return map[string]interface{}{
		"workspaces":   subscribed,
		"is_filtering": filtered.IsFiltering(),
		"count":        len(subscribed),
	}, nil
}

// SubscribeAll clears the workspace filter and receives all events.
func (s *SubscriptionService) SubscribeAll(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.NewError(message.InternalError, "subscription provider not configured")
	}

	// Get client ID from context
	clientID, ok := ctx.Value(handler.ClientIDKey).(string)
	if !ok || clientID == "" {
		return nil, message.NewError(message.InternalError, "client ID not found in context")
	}

	filtered := s.provider.GetFilteredSubscriber(clientID)
	if filtered == nil {
		return nil, message.NewError(message.InternalError, "client not found")
	}

	filtered.SubscribeAll()

	return map[string]interface{}{
		"success":      true,
		"is_filtering": false,
	}, nil
}
