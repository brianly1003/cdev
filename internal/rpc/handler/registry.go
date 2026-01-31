// Package handler provides JSON-RPC request handling infrastructure.
package handler

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/brianly1003/cdev/internal/rpc/message"
)

// ContextKey is a type for context keys to avoid collisions.
type ContextKey string

const (
	// ClientIDKey is the context key for the client ID.
	ClientIDKey ContextKey = "client_id"
	// AuthPayloadKey is the context key for the auth token payload.
	AuthPayloadKey ContextKey = "auth_payload"
)

// HandlerFunc is the signature for RPC method handlers.
// It receives the context and raw params, and returns either a result or an error.
// If the result is nil and error is nil, an empty successful response is sent.
type HandlerFunc func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error)

// MiddlewareFunc is a function that wraps a HandlerFunc.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// Registry holds registered RPC methods and provides lookup functionality.
type Registry struct {
	mu         sync.RWMutex
	handlers   map[string]HandlerFunc
	meta       map[string]MethodMeta
	middleware []MiddlewareFunc
}

// NewRegistry creates a new method registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]HandlerFunc),
		meta:     make(map[string]MethodMeta),
	}
}

// Register registers a handler for a method.
// If a handler is already registered for the method, it will be replaced.
func (r *Registry) Register(method string, handler HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = handler
}

// RegisterWithMeta registers a handler with OpenRPC metadata.
func (r *Registry) RegisterWithMeta(method string, handler HandlerFunc, meta MethodMeta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[method] = handler
	r.meta[method] = meta
}

// GetMeta returns the metadata for a method.
// If no metadata is registered, returns a default MethodMeta with just the method name.
func (r *Registry) GetMeta(method string) MethodMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if meta, ok := r.meta[method]; ok {
		return meta
	}
	return MethodMeta{Summary: method}
}

// RegisterAll registers multiple handlers at once.
func (r *Registry) RegisterAll(handlers map[string]HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for method, handler := range handlers {
		r.handlers[method] = handler
	}
}

// Use adds middleware to the registry.
// Middleware is applied in the order it is added.
func (r *Registry) Use(mw MiddlewareFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middleware = append(r.middleware, mw)
}

// Get returns the handler for a method.
// It applies all registered middleware to the handler.
// Returns nil if the method is not registered.
func (r *Registry) Get(method string) HandlerFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, ok := r.handlers[method]
	if !ok {
		return nil
	}

	// Apply middleware in reverse order (last added = innermost)
	for i := len(r.middleware) - 1; i >= 0; i-- {
		handler = r.middleware[i](handler)
	}

	return handler
}

// Has returns true if a handler is registered for the method.
func (r *Registry) Has(method string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.handlers[method]
	return ok
}

// Methods returns a list of all registered methods.
func (r *Registry) Methods() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	methods := make([]string, 0, len(r.handlers))
	for method := range r.handlers {
		methods = append(methods, method)
	}
	return methods
}

// Unregister removes a handler for a method.
func (r *Registry) Unregister(method string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.handlers, method)
}

// Clear removes all registered handlers and middleware.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers = make(map[string]HandlerFunc)
	r.middleware = nil
}

// MethodService is an interface for services that register multiple methods.
type MethodService interface {
	// RegisterMethods registers all methods provided by this service.
	RegisterMethods(r *Registry)
}

// RegisterService registers all methods from a MethodService.
func (r *Registry) RegisterService(svc MethodService) {
	svc.RegisterMethods(r)
}
