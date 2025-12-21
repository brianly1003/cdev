package handler

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/rs/zerolog/log"
)

// Dispatcher routes JSON-RPC requests to registered handlers.
type Dispatcher struct {
	registry *Registry
}

// NewDispatcher creates a new dispatcher with the given registry.
func NewDispatcher(registry *Registry) *Dispatcher {
	return &Dispatcher{registry: registry}
}

// Dispatch handles a JSON-RPC request and returns a response.
// Returns nil for notifications (requests without ID).
func (d *Dispatcher) Dispatch(ctx context.Context, req *message.Request) *message.Response {
	log.Debug().
		Str("method", req.Method).
		Str("id", req.ID.String()).
		Bool("notification", req.IsNotification()).
		Msg("dispatching request")

	handler := d.registry.Get(req.Method)
	if handler == nil {
		log.Warn().Str("method", req.Method).Msg("method not found")

		// For notifications, don't return error response
		if req.IsNotification() {
			return nil
		}

		return message.NewErrorResponse(req.ID, message.ErrMethodNotFound(req.Method))
	}

	// Execute handler
	result, rpcErr := handler(ctx, req.Params)

	// For notifications, don't return any response
	if req.IsNotification() {
		if rpcErr != nil {
			log.Warn().
				Str("method", req.Method).
				Int("code", rpcErr.Code).
				Str("error", rpcErr.Message).
				Msg("notification handler error (not sent to client)")
		}
		return nil
	}

	// Build response
	if rpcErr != nil {
		log.Debug().
			Str("method", req.Method).
			Int("code", rpcErr.Code).
			Str("error", rpcErr.Message).
			Msg("request failed")

		return message.NewErrorResponse(req.ID, rpcErr)
	}

	resp, err := message.NewSuccessResponse(req.ID, result)
	if err != nil {
		log.Error().
			Str("method", req.Method).
			Err(err).
			Msg("failed to marshal response")

		return message.NewErrorResponse(req.ID, message.ErrInternalError("failed to marshal response"))
	}

	log.Debug().
		Str("method", req.Method).
		Str("id", req.ID.String()).
		Msg("request completed")

	return resp
}

// DispatchBytes parses and dispatches a JSON-RPC request from bytes.
// Returns the response bytes, or nil for notifications.
func (d *Dispatcher) DispatchBytes(ctx context.Context, data []byte) ([]byte, error) {
	// Parse request
	req, err := message.ParseRequest(data)
	if err != nil {
		log.Debug().Err(err).Msg("failed to parse request")

		// Return parse error response
		resp := message.NewErrorResponse(nil, message.ErrParseError(err.Error()))
		return json.Marshal(resp)
	}

	// Dispatch
	resp := d.Dispatch(ctx, req)
	if resp == nil {
		return nil, nil
	}

	// Marshal response
	return json.Marshal(resp)
}

// BatchDispatch handles a batch of JSON-RPC requests.
// Returns a slice of responses (some may be nil for notifications).
func (d *Dispatcher) BatchDispatch(ctx context.Context, requests []*message.Request) []*message.Response {
	responses := make([]*message.Response, 0, len(requests))

	for _, req := range requests {
		resp := d.Dispatch(ctx, req)
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	return responses
}

// HandleMessage handles an incoming message, determining if it's a single
// request or a batch, and returns the appropriate response(s).
func (d *Dispatcher) HandleMessage(ctx context.Context, data []byte) ([]byte, error) {
	// Check if it's a batch (starts with '[')
	if len(data) > 0 && data[0] == '[' {
		return d.handleBatch(ctx, data)
	}

	return d.DispatchBytes(ctx, data)
}

// handleBatch handles a batch request.
func (d *Dispatcher) handleBatch(ctx context.Context, data []byte) ([]byte, error) {
	var rawRequests []json.RawMessage
	if err := json.Unmarshal(data, &rawRequests); err != nil {
		resp := message.NewErrorResponse(nil, message.ErrParseError("Invalid batch request"))
		return json.Marshal(resp)
	}

	if len(rawRequests) == 0 {
		resp := message.NewErrorResponse(nil, message.ErrInvalidRequest("Empty batch"))
		return json.Marshal(resp)
	}

	// Parse and dispatch each request
	responses := make([]*message.Response, 0, len(rawRequests))

	for _, rawReq := range rawRequests {
		req, err := message.ParseRequest(rawReq)
		if err != nil {
			// Invalid request in batch - add error response
			responses = append(responses, message.NewErrorResponse(nil, message.ErrParseError(err.Error())))
			continue
		}

		resp := d.Dispatch(ctx, req)
		if resp != nil {
			responses = append(responses, resp)
		}
	}

	// If all requests were notifications, return nothing
	if len(responses) == 0 {
		return nil, nil
	}

	return json.Marshal(responses)
}

// Registry returns the underlying registry.
func (d *Dispatcher) Registry() *Registry {
	return d.registry
}
