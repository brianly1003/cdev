package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewDebugHandler(t *testing.T) {
	h := NewDebugHandler(true)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if !h.pprofEnabled {
		t.Error("expected pprof to be enabled")
	}

	h2 := NewDebugHandler(false)
	if h2.pprofEnabled {
		t.Error("expected pprof to be disabled")
	}
}

func TestDebugHandler_RuntimeInfo(t *testing.T) {
	h := NewDebugHandler(true)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/runtime", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify expected fields
	requiredFields := []string{"go_version", "go_os", "go_arch", "num_cpu", "num_goroutine", "uptime_seconds", "memory"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}

	// Verify memory sub-fields
	memory, ok := result["memory"].(map[string]interface{})
	if !ok {
		t.Fatal("memory field is not a map")
	}

	memFields := []string{"alloc_mb", "heap_alloc_mb", "num_gc"}
	for _, field := range memFields {
		if _, ok := memory[field]; !ok {
			t.Errorf("missing memory field: %s", field)
		}
	}
}

func TestDebugHandler_RuntimeInfo_MethodNotAllowed(t *testing.T) {
	h := NewDebugHandler(true)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/debug/runtime", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestDebugHandler_Index(t *testing.T) {
	h := NewDebugHandler(true)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "cdev Debug") {
		t.Error("expected debug index page title")
	}
	if !strings.Contains(body, "/debug/runtime") {
		t.Error("expected runtime link in index")
	}
	if !strings.Contains(body, "/debug/pprof/") {
		t.Error("expected pprof link in index when enabled")
	}
}

func TestDebugHandler_Index_PprofDisabled(t *testing.T) {
	h := NewDebugHandler(false)
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodGet, "/debug/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "pprof is disabled") {
		t.Error("expected pprof disabled message")
	}
}

func TestDebugHandler_Index_NotFound(t *testing.T) {
	h := NewDebugHandler(true)
	mux := http.NewServeMux()
	h.Register(mux)

	// /debug (without trailing slash) should 404
	req := httptest.NewRequest(http.MethodGet, "/debug/nonexistent", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestDebugHandler_Pprof_WhenEnabled(t *testing.T) {
	h := NewDebugHandler(true)
	mux := http.NewServeMux()
	h.Register(mux)

	// Test pprof index
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for pprof index, got %d", rec.Code)
	}

	// Test pprof cmdline
	req = httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for pprof cmdline, got %d", rec.Code)
	}

	// Test pprof symbol (POST allowed)
	req = httptest.NewRequest(http.MethodGet, "/debug/pprof/symbol", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for pprof symbol, got %d", rec.Code)
	}
}

func TestDebugHandler_Pprof_WhenDisabled(t *testing.T) {
	h := NewDebugHandler(false)
	mux := http.NewServeMux()
	h.Register(mux)

	// pprof index should 404 when disabled
	req := httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for pprof when disabled, got %d", rec.Code)
	}
}

func TestSetDebugHandler_Nil(t *testing.T) {
	s := &Server{
		mux: http.NewServeMux(),
	}

	// Should not panic with nil handler
	s.SetDebugHandler(nil)
}

func TestSetDebugHandler(t *testing.T) {
	s := &Server{
		mux: http.NewServeMux(),
	}

	h := NewDebugHandler(true)
	s.SetDebugHandler(h)

	// Verify routes are registered by making a request
	req := httptest.NewRequest(http.MethodGet, "/debug/runtime", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected debug routes to be registered, got status %d", rec.Code)
	}
}
