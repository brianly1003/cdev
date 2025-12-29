package security

import (
	"net/http"
	"net/url"
	"strings"
)

// OriginChecker validates WebSocket and CORS origins.
type OriginChecker struct {
	allowedOrigins    []string
	bindLocalhostOnly bool
}

// NewOriginChecker creates a new origin checker.
func NewOriginChecker(allowedOrigins []string, bindLocalhostOnly bool) *OriginChecker {
	return &OriginChecker{
		allowedOrigins:    allowedOrigins,
		bindLocalhostOnly: bindLocalhostOnly,
	}
}

// CheckOrigin validates the origin header in a request.
// Returns true if the origin is allowed.
func (oc *OriginChecker) CheckOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")

	// No origin header means same-origin request (browser doesn't send Origin for same-origin)
	if origin == "" {
		return true
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// Extract host without port
	originHost := parsedOrigin.Hostname()

	// If binding to localhost only, allow localhost origins
	if oc.bindLocalhostOnly {
		if isLocalhost(originHost) {
			return true
		}
	}

	// Check against allowed origins list
	for _, allowed := range oc.allowedOrigins {
		if matchOrigin(origin, allowed) {
			return true
		}
	}

	// If no allowed origins configured and binding localhost only,
	// only localhost origins are allowed (already checked above)
	if len(oc.allowedOrigins) == 0 && oc.bindLocalhostOnly {
		return false
	}

	// If no allowed origins configured and not binding localhost only,
	// allow all origins (development mode)
	if len(oc.allowedOrigins) == 0 {
		return true
	}

	return false
}

// isLocalhost checks if a host is localhost.
func isLocalhost(host string) bool {
	return host == "localhost" ||
		host == "127.0.0.1" ||
		host == "::1" ||
		strings.HasSuffix(host, ".localhost")
}

// matchOrigin checks if an origin matches an allowed pattern.
// Supports exact match and wildcard subdomain matching (*.example.com).
func matchOrigin(origin, allowed string) bool {
	// Exact match
	if origin == allowed {
		return true
	}

	// Parse both for comparison
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}

	// Handle wildcard subdomain matching (*.example.com)
	if strings.HasPrefix(allowed, "*.") {
		domain := allowed[2:] // Remove "*."

		// Check if origin's host ends with the domain
		originHost := parsedOrigin.Hostname()
		if strings.HasSuffix(originHost, domain) || originHost == domain[1:] {
			return true
		}
	}

	return false
}

// CheckOriginFunc returns a function suitable for websocket.Upgrader.CheckOrigin.
func (oc *OriginChecker) CheckOriginFunc() func(r *http.Request) bool {
	return oc.CheckOrigin
}
