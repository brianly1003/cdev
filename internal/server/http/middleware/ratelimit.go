// Package middleware provides HTTP middleware components for the cdev server.
package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter configuration constants.
const (
	DefaultMaxRequests = 10              // Max requests per window
	DefaultWindow      = 1 * time.Minute // Time window for rate limiting
	DefaultCleanup     = 5 * time.Minute // Cleanup interval for stale buckets
)

// RateLimiter implements a sliding window rate limiter with per-key limiting.
type RateLimiter struct {
	maxRequests int
	window      time.Duration
	buckets     map[string]*bucket
	mu          sync.RWMutex
	cleanupDone chan struct{}
}

// bucket tracks request timestamps for a single key.
type bucket struct {
	timestamps []time.Time
	lastAccess time.Time
}

// RateLimiterOption is a functional option for configuring RateLimiter.
type RateLimiterOption func(*RateLimiter)

// WithMaxRequests sets the maximum number of requests per window.
func WithMaxRequests(n int) RateLimiterOption {
	return func(r *RateLimiter) {
		if n > 0 {
			r.maxRequests = n
		}
	}
}

// WithWindow sets the time window for rate limiting.
func WithWindow(d time.Duration) RateLimiterOption {
	return func(r *RateLimiter) {
		if d > 0 {
			r.window = d
		}
	}
}

// NewRateLimiter creates a new RateLimiter with the given options.
func NewRateLimiter(opts ...RateLimiterOption) *RateLimiter {
	r := &RateLimiter{
		maxRequests: DefaultMaxRequests,
		window:      DefaultWindow,
		buckets:     make(map[string]*bucket),
		cleanupDone: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(r)
	}

	// Start cleanup goroutine
	go r.cleanupLoop()

	return r
}

// Allow checks if a request from the given key is allowed.
// Returns true if allowed, false if rate limited.
func (r *RateLimiter) Allow(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)

	b, exists := r.buckets[key]
	if !exists {
		b = &bucket{
			timestamps: make([]time.Time, 0, r.maxRequests),
		}
		r.buckets[key] = b
	}

	// Filter out old timestamps
	valid := make([]time.Time, 0, len(b.timestamps))
	for _, ts := range b.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	b.timestamps = valid
	b.lastAccess = now

	// Check if we can accept this request
	if len(b.timestamps) >= r.maxRequests {
		return false
	}

	// Add current timestamp
	b.timestamps = append(b.timestamps, now)
	return true
}

// Remaining returns the number of remaining requests for a key.
func (r *RateLimiter) Remaining(key string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	b, exists := r.buckets[key]
	if !exists {
		return r.maxRequests
	}

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Count valid timestamps
	count := 0
	for _, ts := range b.timestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	remaining := r.maxRequests - count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Reset clears the rate limit for a key.
func (r *RateLimiter) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.buckets, key)
}

// ResetAll clears all rate limits.
func (r *RateLimiter) ResetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buckets = make(map[string]*bucket)
}

// Close stops the cleanup goroutine.
func (r *RateLimiter) Close() {
	close(r.cleanupDone)
}

// cleanupLoop periodically removes stale buckets.
func (r *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(DefaultCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-r.cleanupDone:
			return
		case <-ticker.C:
			r.cleanup()
		}
	}
}

// cleanup removes buckets that haven't been accessed recently.
func (r *RateLimiter) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-r.window * 2)

	for key, b := range r.buckets {
		if b.lastAccess.Before(cutoff) {
			delete(r.buckets, key)
		}
	}
}

// KeyExtractor is a function that extracts a rate limit key from a request.
type KeyExtractor func(*http.Request) string

// TrustProxy controls whether to trust X-Forwarded-For headers.
// Set to true only when behind a trusted reverse proxy.
var TrustProxy = false

// IPKeyExtractor extracts the client IP address as the rate limit key.
// WARNING: X-Forwarded-For headers are only trusted when TrustProxy is true.
// Without a trusted proxy, these headers can be spoofed to bypass rate limiting.
func IPKeyExtractor(r *http.Request) string {
	// Only trust proxy headers if explicitly configured
	if TrustProxy {
		// X-Forwarded-For can contain multiple IPs: "client, proxy1, proxy2"
		// The first IP is the original client
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Parse first IP from comma-separated list
			if idx := indexByte(xff, ','); idx > 0 {
				return trimSpace(xff[:idx])
			}
			return trimSpace(xff)
		}
		// Check X-Real-IP header
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return trimSpace(xri)
		}
	}
	// Use RemoteAddr as the authoritative source
	// Strip port if present (e.g., "192.168.1.1:12345" -> "192.168.1.1")
	addr := r.RemoteAddr
	if idx := lastIndexByte(addr, ':'); idx > 0 {
		// Check if it's IPv6 (has brackets)
		if addr[0] == '[' {
			if bracketIdx := indexByte(addr, ']'); bracketIdx > 0 {
				return addr[1:bracketIdx]
			}
		}
		return addr[:idx]
	}
	return addr
}

// indexByte returns the index of the first occurrence of c in s, or -1.
func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// lastIndexByte returns the index of the last occurrence of c in s, or -1.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// trimSpace trims leading and trailing whitespace.
func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// HeaderKeyExtractor creates a key extractor that uses a specific header value.
func HeaderKeyExtractor(header string) KeyExtractor {
	return func(r *http.Request) string {
		if val := r.Header.Get(header); val != "" {
			return val
		}
		return IPKeyExtractor(r)
	}
}

// RateLimitMiddleware returns an HTTP middleware that applies rate limiting.
func RateLimitMiddleware(limiter *RateLimiter, keyExtractor KeyExtractor) func(http.Handler) http.Handler {
	if keyExtractor == nil {
		keyExtractor = IPKeyExtractor
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyExtractor(r)

			if !limiter.Allow(key) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"rate limit exceeded","message":"Too many requests. Please wait before trying again."}`))
				return
			}

			// Add rate limit headers
			remaining := limiter.Remaining(key)
			w.Header().Set("X-RateLimit-Limit", formatInt(limiter.maxRequests))
			w.Header().Set("X-RateLimit-Remaining", formatInt(remaining))

			next.ServeHTTP(w, r)
		})
	}
}

// formatInt converts an int to a string.
func formatInt(n int) string {
	return strconv.Itoa(n)
}
