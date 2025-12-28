package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	limiter := NewRateLimiter()
	defer limiter.Close()

	if limiter.maxRequests != DefaultMaxRequests {
		t.Errorf("expected maxRequests %d, got %d", DefaultMaxRequests, limiter.maxRequests)
	}
	if limiter.window != DefaultWindow {
		t.Errorf("expected window %v, got %v", DefaultWindow, limiter.window)
	}
}

func TestNewRateLimiter_WithOptions(t *testing.T) {
	limiter := NewRateLimiter(
		WithMaxRequests(5),
		WithWindow(30*time.Second),
	)
	defer limiter.Close()

	if limiter.maxRequests != 5 {
		t.Errorf("expected maxRequests 5, got %d", limiter.maxRequests)
	}
	if limiter.window != 30*time.Second {
		t.Errorf("expected window 30s, got %v", limiter.window)
	}
}

func TestWithMaxRequests_InvalidValue(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(0))
	defer limiter.Close()

	// Should use default when invalid value provided
	if limiter.maxRequests != DefaultMaxRequests {
		t.Errorf("expected default maxRequests, got %d", limiter.maxRequests)
	}
}

func TestWithWindow_InvalidValue(t *testing.T) {
	limiter := NewRateLimiter(WithWindow(-1))
	defer limiter.Close()

	// Should use default when invalid value provided
	if limiter.window != DefaultWindow {
		t.Errorf("expected default window, got %v", limiter.window)
	}
}

func TestAllow_SingleKey(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(3), WithWindow(time.Minute))
	defer limiter.Close()

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !limiter.Allow("key1") {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if limiter.Allow("key1") {
		t.Error("4th request should be denied")
	}
}

func TestAllow_MultipleKeys(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(2), WithWindow(time.Minute))
	defer limiter.Close()

	// Each key should have independent limits
	if !limiter.Allow("key1") {
		t.Error("first request for key1 should be allowed")
	}
	if !limiter.Allow("key2") {
		t.Error("first request for key2 should be allowed")
	}
	if !limiter.Allow("key1") {
		t.Error("second request for key1 should be allowed")
	}
	if !limiter.Allow("key2") {
		t.Error("second request for key2 should be allowed")
	}

	// Both should be at limit now
	if limiter.Allow("key1") {
		t.Error("third request for key1 should be denied")
	}
	if limiter.Allow("key2") {
		t.Error("third request for key2 should be denied")
	}
}

func TestAllow_WindowExpiry(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(2), WithWindow(100*time.Millisecond))
	defer limiter.Close()

	// Use up the limit
	limiter.Allow("key1")
	limiter.Allow("key1")

	if limiter.Allow("key1") {
		t.Error("should be rate limited")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !limiter.Allow("key1") {
		t.Error("should be allowed after window expires")
	}
}

func TestRemaining(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(5), WithWindow(time.Minute))
	defer limiter.Close()

	// Initially should have full remaining
	if remaining := limiter.Remaining("key1"); remaining != 5 {
		t.Errorf("expected 5 remaining, got %d", remaining)
	}

	// After one request
	limiter.Allow("key1")
	if remaining := limiter.Remaining("key1"); remaining != 4 {
		t.Errorf("expected 4 remaining, got %d", remaining)
	}

	// Use up the rest
	limiter.Allow("key1")
	limiter.Allow("key1")
	limiter.Allow("key1")
	limiter.Allow("key1")

	if remaining := limiter.Remaining("key1"); remaining != 0 {
		t.Errorf("expected 0 remaining, got %d", remaining)
	}
}

func TestReset(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(2), WithWindow(time.Minute))
	defer limiter.Close()

	// Use up the limit
	limiter.Allow("key1")
	limiter.Allow("key1")

	if limiter.Allow("key1") {
		t.Error("should be rate limited before reset")
	}

	// Reset
	limiter.Reset("key1")

	// Should be allowed again
	if !limiter.Allow("key1") {
		t.Error("should be allowed after reset")
	}
}

func TestResetAll(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(1), WithWindow(time.Minute))
	defer limiter.Close()

	// Use up limits for multiple keys
	limiter.Allow("key1")
	limiter.Allow("key2")

	// Reset all
	limiter.ResetAll()

	// Both should be allowed again
	if !limiter.Allow("key1") {
		t.Error("key1 should be allowed after ResetAll")
	}
	if !limiter.Allow("key2") {
		t.Error("key2 should be allowed after ResetAll")
	}
}

func TestConcurrency(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(100), WithWindow(time.Minute))
	defer limiter.Close()

	var wg sync.WaitGroup
	allowed := make(chan bool, 1000)

	// Concurrent requests from multiple goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 15; j++ {
				allowed <- limiter.Allow("concurrent-key")
			}
		}()
	}

	wg.Wait()
	close(allowed)

	// Count allowed requests
	allowedCount := 0
	for a := range allowed {
		if a {
			allowedCount++
		}
	}

	// Should have exactly 100 allowed
	if allowedCount != 100 {
		t.Errorf("expected exactly 100 allowed requests, got %d", allowedCount)
	}
}

func TestIPKeyExtractor(t *testing.T) {
	// Test default behavior: TrustProxy = false
	// Headers are ignored, RemoteAddr is used (port stripped)
	t.Run("Default_IgnoresHeaders", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = false
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4")
		req.RemoteAddr = "5.6.7.8:1234"

		result := IPKeyExtractor(req)
		if result != "5.6.7.8" {
			t.Errorf("expected '5.6.7.8', got %q", result)
		}
	})

	t.Run("Default_StripsPort", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = false
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.1:12345"

		result := IPKeyExtractor(req)
		if result != "192.168.1.1" {
			t.Errorf("expected '192.168.1.1', got %q", result)
		}
	})

	t.Run("Default_IPv6", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = false
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "[::1]:12345"

		result := IPKeyExtractor(req)
		if result != "::1" {
			t.Errorf("expected '::1', got %q", result)
		}
	})

	// Test TrustProxy = true behavior
	t.Run("TrustProxy_XForwardedFor", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = true
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4")
		req.RemoteAddr = "5.6.7.8:1234"

		result := IPKeyExtractor(req)
		if result != "1.2.3.4" {
			t.Errorf("expected '1.2.3.4', got %q", result)
		}
	})

	t.Run("TrustProxy_XForwardedFor_Multiple", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = true
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1, 192.168.1.1")
		req.RemoteAddr = "5.6.7.8:1234"

		result := IPKeyExtractor(req)
		if result != "1.2.3.4" {
			t.Errorf("expected '1.2.3.4' (first IP), got %q", result)
		}
	})

	t.Run("TrustProxy_XRealIP", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = true
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Real-IP", "9.10.11.12")
		req.RemoteAddr = "5.6.7.8:1234"

		result := IPKeyExtractor(req)
		if result != "9.10.11.12" {
			t.Errorf("expected '9.10.11.12', got %q", result)
		}
	})

	t.Run("TrustProxy_XForwardedFor_Priority", func(t *testing.T) {
		oldTrust := TrustProxy
		TrustProxy = true
		defer func() { TrustProxy = oldTrust }()

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Forwarded-For", "1.2.3.4")
		req.Header.Set("X-Real-IP", "9.10.11.12")
		req.RemoteAddr = "5.6.7.8:1234"

		result := IPKeyExtractor(req)
		if result != "1.2.3.4" {
			t.Errorf("expected '1.2.3.4' (X-Forwarded-For priority), got %q", result)
		}
	})
}

func TestHeaderKeyExtractor(t *testing.T) {
	extractor := HeaderKeyExtractor("X-API-Key")

	// With header
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-API-Key", "my-api-key")
	if result := extractor(req); result != "my-api-key" {
		t.Errorf("expected 'my-api-key', got %q", result)
	}

	// Without header - falls back to IP (port stripped)
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	if result := extractor(req); result != "1.2.3.4" {
		t.Errorf("expected '1.2.3.4', got %q", result)
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(2), WithWindow(time.Minute))
	defer limiter.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	middleware := RateLimitMiddleware(limiter, nil)
	wrappedHandler := middleware(handler)

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("request %d should return 200, got %d", i+1, rec.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("3rd request should return 429, got %d", rec.Code)
	}

	if rec.Header().Get("Retry-After") == "" {
		t.Error("should have Retry-After header")
	}
}

func TestRateLimitMiddleware_CustomExtractor(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(1), WithWindow(time.Minute))
	defer limiter.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Use custom header extractor
	extractor := HeaderKeyExtractor("X-User-ID")
	middleware := RateLimitMiddleware(limiter, extractor)
	wrappedHandler := middleware(handler)

	// Request with user1
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "user1")
	rec := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first request for user1 should succeed, got %d", rec.Code)
	}

	// Second request with user1 should fail
	rec = httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request for user1 should fail, got %d", rec.Code)
	}

	// Request with user2 should succeed
	req.Header.Set("X-User-ID", "user2")
	rec = httptest.NewRecorder()
	wrappedHandler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first request for user2 should succeed, got %d", rec.Code)
	}
}

func TestCleanup(t *testing.T) {
	limiter := NewRateLimiter(WithMaxRequests(1), WithWindow(50*time.Millisecond))
	defer limiter.Close()

	// Add some buckets
	limiter.Allow("key1")
	limiter.Allow("key2")
	limiter.Allow("key3")

	// Wait for entries to become stale
	time.Sleep(150 * time.Millisecond)

	// Trigger cleanup
	limiter.cleanup()

	// Check buckets are cleaned up
	limiter.mu.RLock()
	bucketCount := len(limiter.buckets)
	limiter.mu.RUnlock()

	if bucketCount != 0 {
		t.Errorf("expected 0 buckets after cleanup, got %d", bucketCount)
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{100, "100"},
		{-1, "-1"},
	}

	for _, tt := range tests {
		result := formatInt(tt.input)
		if result != tt.expected {
			t.Errorf("formatInt(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
