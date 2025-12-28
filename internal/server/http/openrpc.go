// Package http provides OpenRPC discovery endpoint for JSON-RPC API documentation.
package http

import (
	_ "embed"
	"net/http"

	"github.com/brianly1003/cdev/internal/rpc/handler"
)

//go:embed openrpc.json
var fallbackOpenRPCSpec []byte

// SetRPCRegistry sets the RPC registry for dynamic OpenRPC generation.
func (s *Server) SetRPCRegistry(registry *handler.Registry) {
	s.rpcRegistry = registry
}

// handleOpenRPCDiscover serves the OpenRPC specification.
// If an RPC registry is set, generates the spec dynamically.
// Otherwise, falls back to the embedded static spec.
//
//	@Summary		Get OpenRPC specification
//	@Description	Returns the OpenRPC specification for the JSON-RPC WebSocket API
//	@Tags			Documentation
//	@Produce		json
//	@Success		200	{object}	object	"OpenRPC specification"
//	@Router			/api/rpc/discover [get]
func (s *Server) handleOpenRPCDiscover(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Generate dynamic spec from registry if available
	if s.rpcRegistry != nil {
		spec := s.rpcRegistry.GenerateOpenRPC(
			handler.OpenRPCInfo{
				Title:       "cdev Unified API",
				Description: "JSON-RPC 2.0 API for cdev mobile AI coding agent",
				Version:     "1.0.0",
			},
			"ws://localhost:8766/ws",
		)
		data, err := spec.ToJSON()
		if err == nil {
			_, _ = w.Write(data)
			return
		}
	}

	// Fall back to static spec
	_, _ = w.Write(fallbackOpenRPCSpec)
}
