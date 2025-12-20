package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	statusFn := func() map[string]interface{} {
		return map[string]interface{}{"status": "ok"}
	}

	server := New("localhost", 8766, statusFn, nil, nil, nil, nil, nil, 100, "/tmp/testrepo")

	if server.addr != "localhost:8766" {
		t.Errorf("expected addr localhost:8766, got %s", server.addr)
	}
	if server.maxFileSizeKB != 100 {
		t.Errorf("expected maxFileSizeKB 100, got %d", server.maxFileSizeKB)
	}
	if server.repoPath != "/tmp/testrepo" {
		t.Errorf("expected repoPath /tmp/testrepo, got %s", server.repoPath)
	}
}

func TestServer_HandleHealth(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status ok, got %v", result["status"])
	}
	if result["time"] == nil {
		t.Error("expected time field in response")
	}
}

func TestServer_HandleHealth_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleStatus(t *testing.T) {
	statusFn := func() map[string]interface{} {
		return map[string]interface{}{
			"session_id":        "test-session",
			"version":           "1.0.0",
			"uptime_seconds":    3600,
			"connected_clients": 2,
		}
	}

	server := New("localhost", 8766, statusFn, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result["session_id"] != "test-session" {
		t.Errorf("expected session_id test-session, got %v", result["session_id"])
	}
	if result["version"] != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %v", result["version"])
	}
}

func TestServer_HandleStatus_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGetFile_MissingPath(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/file", nil)
	w := httptest.NewRecorder()

	server.handleGetFile(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)

	if result["error"] == nil {
		t.Error("expected error field in response")
	}
}

func TestServer_HandleGetFile_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/file", nil)
	w := httptest.NewRecorder()

	server.handleGetFile(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGetFile_NoGitTracker(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/file?path=test.txt", nil)
	w := httptest.NewRecorder()

	server.handleGetFile(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}
}

func TestServer_HandleFilesList(t *testing.T) {
	// Create a temp directory with some files
	tmpDir, err := os.MkdirTemp("", "cdev-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.go"), []byte("content2"), 0644)
	os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result DirectoryListingResponse
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if result.TotalCount != 3 {
		t.Errorf("expected 3 entries, got %d", result.TotalCount)
	}

	// First entry should be directory (sorted first)
	if result.Entries[0].Type != "directory" {
		t.Errorf("expected first entry to be directory, got %s", result.Entries[0].Type)
	}
}

func TestServer_HandleFilesList_SubPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cdev-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create subdirectory with files
	subDir := filepath.Join(tmpDir, "subdir")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested content"), 0644)

	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=subdir", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result DirectoryListingResponse
	json.Unmarshal(body, &result)

	if result.Path != "subdir" {
		t.Errorf("expected path subdir, got %s", result.Path)
	}
	if result.TotalCount != 1 {
		t.Errorf("expected 1 entry, got %d", result.TotalCount)
	}
}

func TestServer_HandleFilesList_PathTraversal(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cdev-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, tmpDir)

	// Attempt path traversal
	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=../../../etc", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for path traversal, got %d", resp.StatusCode)
	}
}

func TestServer_HandleFilesList_NotADirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cdev-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)

	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=file.txt", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for file path, got %d", resp.StatusCode)
	}
}

func TestServer_HandleFilesList_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/files/list", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/run", nil)
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_InvalidJSON(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/run", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_EmptyPrompt(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/run", strings.NewReader(`{"prompt":""}`))
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_NoManager(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/run", strings.NewReader(`{"prompt":"test"}`))
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeStop_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/stop", nil)
	w := httptest.NewRecorder()

	server.handleClaudeStop(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeStop_NoManager(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/stop", nil)
	w := httptest.NewRecorder()

	server.handleClaudeStop(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRespond_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/respond", nil)
	w := httptest.NewRecorder()

	server.handleClaudeRespond(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRespond_InvalidJSON(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/respond", strings.NewReader("invalid"))
	w := httptest.NewRecorder()

	server.handleClaudeRespond(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRespond_MissingToolUseID(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/respond", strings.NewReader(`{"response":"yes"}`))
	w := httptest.NewRecorder()

	server.handleClaudeRespond(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeSessions_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPut, "/api/claude/sessions", nil)
	w := httptest.NewRecorder()

	server.handleClaudeSessions(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeSessionMessages_MissingSessionID(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/sessions/messages", nil)
	w := httptest.NewRecorder()

	server.handleClaudeSessionMessages(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGitStatus_NoTracker(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/git/status", nil)
	w := httptest.NewRecorder()

	server.handleGitStatus(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGitDiff_NoTracker(t *testing.T) {
	server := New("localhost", 8766, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/git/diff", nil)
	w := httptest.NewRecorder()

	server.handleGitDiff(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestServer_StartStop(t *testing.T) {
	server := New("127.0.0.1", 0, nil, nil, nil, nil, nil, nil, 100, "/tmp")

	err := server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		param      string
		defaultVal int
		expected   int
	}{
		{"valid value", "limit=10", "limit", 20, 10},
		{"missing param", "", "limit", 20, 20},
		{"invalid value", "limit=abc", "limit", 20, 20},
		{"negative value", "limit=-5", "limit", 20, -5},
		{"zero value", "limit=0", "limit", 20, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tt.query, nil)
			result := parseIntParam(req, tt.param, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 4, "abcd"},   // len == maxLen, no truncation
		{"abcde", 4, "a..."},  // len > maxLen: 1 char + "..." = 4
		{"abcdef", 5, "ab..."}, // len > maxLen: 2 chars + "..." = 5
		{"ab", 5, "ab"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := truncateString(tt.input, tt.maxLen)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestCorsMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Error("expected CORS header Access-Control-Allow-Origin: *")
	}
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
}

func TestCorsMiddleware_Options(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("should not see this"))
	})

	wrapped := corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for OPTIONS, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		t.Error("OPTIONS request should return empty body")
	}
}
