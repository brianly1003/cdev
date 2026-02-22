package security

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ParseTrustedProxies parses CIDR and IP entries into concrete CIDR ranges.
func ParseTrustedProxies(trustedProxies []string) ([]*net.IPNet, error) {
	parsed := make([]*net.IPNet, 0, len(trustedProxies))

	for _, proxy := range trustedProxies {
		trimmed := strings.TrimSpace(proxy)
		if trimmed == "" {
			continue
		}

		// Single IP
		if ip := net.ParseIP(trimmed); ip != nil {
			if ip4 := ip.To4(); ip4 != nil {
				ip = ip4
			}
			parsed = append(parsed, &net.IPNet{
				IP:   ip,
				Mask: net.CIDRMask(len(ip)*8, len(ip)*8),
			})
			continue
		}

		// CIDR range
		_, cidr, err := net.ParseCIDR(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy %q: %w", trimmed, err)
		}
		parsed = append(parsed, cidr)
	}

	return parsed, nil
}

// IsTrustedProxy reports whether remoteAddr belongs to one of trusted CIDRs.
func IsTrustedProxy(remoteAddr string, trustedProxies []*net.IPNet) bool {
	if len(trustedProxies) == 0 {
		return false
	}

	ip := parseIPFromAddress(remoteAddr)
	if ip == nil {
		return false
	}

	for _, trusted := range trustedProxies {
		if trusted != nil && trusted.Contains(ip) {
			return true
		}
	}

	return false
}

// RequestClientIP resolves the client IP from the request.
// Forwarded headers are used only when the remote address is trusted.
func RequestClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	if r == nil {
		return ""
	}

	remoteAddr := r.RemoteAddr
	trusted := IsTrustedProxy(remoteAddr, trustedProxies)

	if trusted {
		if xff := firstForwardedValue(r.Header.Get("X-Forwarded-For")); xff != "" {
			if ip := parseIPFromAddress(xff); ip != nil {
				return ip.String()
			}
		}
		if xRealIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); xRealIP != "" {
			if ip := parseIPFromAddress(xRealIP); ip != nil {
				return ip.String()
			}
		}
	}

	if ip := parseIPFromAddress(remoteAddr); ip != nil {
		return ip.String()
	}

	return ""
}

// RequestBaseURL resolves scheme+host from an HTTP request for service discovery.
// Forwarded headers are used only when the remote address is trusted.
func RequestBaseURL(r *http.Request, trustedProxies []*net.IPNet) (string, bool) {
	if r == nil {
		return "", false
	}

	trusted := IsTrustedProxy(r.RemoteAddr, trustedProxies)

	host := strings.TrimSpace(r.Host)
	if trusted {
		if forwardedHost := firstForwardedValue(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			host = forwardedHost
		}
	}

	if host == "" {
		return "", false
	}

	proto := ""
	if trusted {
		proto = strings.ToLower(strings.TrimSpace(firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))))
		if proto == "" {
			proto = strings.ToLower(strings.TrimSpace(firstForwardedValue(r.Header.Get("X-Forwarded-Scheme"))))
		}
	}

	if proto != "http" && proto != "https" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}

	if forwardedPort := firstForwardedValue(r.Header.Get("X-Forwarded-Port")); trusted && forwardedPort != "" {
		if !strings.Contains(host, ":") {
			if (proto == "http" && forwardedPort != "80") || (proto == "https" && forwardedPort != "443") {
				host = host + ":" + forwardedPort
			}
		}
	}

	base := proto + "://" + host
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", false
	}

	return base, true
}

// WebSocketURL creates a websocket URL from an HTTP URL.
func WebSocketURL(httpURL string) string {
	base := strings.TrimRight(strings.TrimSpace(httpURL), "/")
	if base == "" {
		return "/ws"
	}

	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return base + "/ws"
	}

	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	}

	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/ws"

	return parsed.String()
}

func firstForwardedValue(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func parseIPFromAddress(address string) net.IP {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return nil
	}

	host, _, err := net.SplitHostPort(trimmed)
	if err == nil {
		return net.ParseIP(host)
	}

	return net.ParseIP(strings.Trim(trimmed, "[]"))
}
