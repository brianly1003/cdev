// Package http provides OpenRPC discovery endpoint for JSON-RPC API documentation.
package http

import (
	_ "embed"
	"fmt"
	"net/http"
	"net"
	"strconv"
	"strings"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/security"
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

	// Generate dynamic spec from registry if available
	if s.rpcRegistry != nil {
		baseURL, ok := security.RequestBaseURL(r, s.trustedProxies)
		if !ok {
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			host, port := openrpcHostPort(s.addr)
			baseURL = fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(host, port))
		}
		spec := s.rpcRegistry.GenerateOpenRPC(
			handler.OpenRPCInfo{
				Title:       "cdev Unified API",
				Description: "JSON-RPC 2.0 API for cdev mobile AI coding agent",
				Version:     "1.0.0",
			},
			security.WebSocketURL(baseURL),
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

func openrpcHostPort(addr string) (host string, port string) {
	host = "localhost"
	port = "8766"

	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return
	}

	listenHost, listenPort, err := net.SplitHostPort(trimmed)
	if err == nil {
		if listenHost != "" {
			if strings.EqualFold(listenHost, "0.0.0.0") || strings.EqualFold(listenHost, "::") {
				host = "localhost"
			} else {
				host = strings.Trim(listenHost, "[]")
			}
		}
		if p := strings.TrimSpace(listenPort); p != "" {
			port = p
		}
		return
	}

	idx := strings.LastIndex(trimmed, ":")
	if idx <= 0 {
		return
	}

	possiblePort := strings.TrimSpace(trimmed[idx+1:])
	if possiblePort != "" && isDigitsOnly(possiblePort) {
		potentialHost := strings.TrimSpace(trimmed[:idx])
		if potentialHost == "" || strings.EqualFold(potentialHost, "0.0.0.0") || strings.EqualFold(potentialHost, "::") {
			potentialHost = "localhost"
		}
		port = possiblePort
		host = strings.Trim(potentialHost, "[]")
	}

	return
}

func isDigitsOnly(value string) bool {
	if value == "" {
		return false
	}

	_, err := strconv.Atoi(value)
	return err == nil
}
