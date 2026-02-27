package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/security"
	"github.com/brianly1003/cdev/internal/server/http/middleware"
)

func TestNew(t *testing.T) {
	statusFn := func() map[string]interface{} {
		return map[string]interface{}{"status": "ok"}
	}

	server := New("localhost", 16180, statusFn, nil, nil, nil, nil, nil, 100, 100, "/tmp/testrepo")

	if server.addr != "localhost:16180" {
		t.Errorf("expected addr localhost:16180, got %s", server.addr)
	}
	if server.maxFileSizeKB != 100 {
		t.Errorf("expected maxFileSizeKB 100, got %d", server.maxFileSizeKB)
	}
	if server.repoPath != "/tmp/testrepo" {
		t.Errorf("expected repoPath /tmp/testrepo, got %s", server.repoPath)
	}
}

func TestServer_HandleHealth(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	server.handleHealth(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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

	server := New("localhost", 16180, statusFn, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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

// --- Auth Middleware Tests ---

func TestAuthMiddleware_RejectsMissingToken(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetAuth(newTestTokenManager(t), true)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := server.authMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_AllowsValidToken(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	tokenManager := newTestTokenManager(t)
	server.SetAuth(tokenManager, true)

	token, _, err := tokenManager.GenerateAccessToken()
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := server.authMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_RejectsPairingToken(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	tokenManager := newTestTokenManager(t)
	server.SetAuth(tokenManager, true)

	token, _, err := tokenManager.GeneratePairingToken()
	if err != nil {
		t.Fatalf("failed to generate pairing token: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := server.authMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestAuthMiddleware_AllowsAllowlistedPaths(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetAuth(newTestTokenManager(t), true)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := server.authMiddleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestRootRedirectMiddleware_RedirectsHome(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rootRedirectMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if nextCalled {
		t.Error("expected middleware to handle root redirect without calling next handler")
	}
	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected status 302, got %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/pair" {
		t.Errorf("expected redirect location /pair, got %q", location)
	}
}

func TestRootRedirectMiddleware_PassesThroughNonRoot(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})

	wrapped := rootRedirectMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if !nextCalled {
		t.Error("expected non-root request to pass through to next handler")
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", resp.StatusCode)
	}
}

func TestRootRedirectMiddleware_AuthEnabledRedirectsBeforeAuth(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetAuth(newTestTokenManager(t), true)

	// Mirror Start() middleware order around auth and root redirect.
	var h http.Handler = server.mux
	h = server.authMiddleware(h)
	h = rootRedirectMiddleware(h)
	h = server.corsMiddleware(h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("expected status 302 at root with auth enabled, got %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/pair" {
		t.Errorf("expected redirect location /pair, got %q", location)
	}
}

func TestRootRedirectMiddleware_PreservesQueryString(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := rootRedirectMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/?token=test123", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected status 302, got %d", resp.StatusCode)
	}
	if location := resp.Header.Get("Location"); location != "/pair?token=test123" {
		t.Fatalf("expected redirect location /pair?token=test123, got %q", location)
	}
}

func TestPairAccessTokenMiddleware_RequiresTokenForPairRoutes(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetPairAccessToken("secret-token")

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := server.pairAccessTokenMiddleware(next)

	tests := []struct {
		name       string
		url        string
		headerName string
		headerVal  string
		wantStatus int
		wantNext   bool
	}{
		{
			name:       "missing token rejected",
			url:        "/pair",
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		{
			name:       "wrong token rejected",
			url:        "/pair?token=wrong",
			wantStatus: http.StatusUnauthorized,
			wantNext:   false,
		},
		{
			name:       "query token redirects to strip token from URL",
			url:        "/pair?token=secret-token",
			wantStatus: http.StatusFound,
			wantNext:   false,
		},
		{
			name:       "header token accepted",
			url:        "/pair",
			headerName: "X-Cdev-Token",
			headerVal:  "secret-token",
			wantStatus: http.StatusNoContent,
			wantNext:   true,
		},
		{
			name:       "non-pair route unaffected",
			url:        "/api/status",
			wantStatus: http.StatusNoContent,
			wantNext:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled = false
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			if tt.headerName != "" {
				req.Header.Set(tt.headerName, tt.headerVal)
			}
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, req)

			resp := w.Result()
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if nextCalled != tt.wantNext {
				t.Fatalf("nextCalled = %v, want %v", nextCalled, tt.wantNext)
			}
		})
	}
}

func TestPairAccessTokenMiddleware_QueryRedirectStripsToken(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetPairAccessToken("secret-token")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := server.pairAccessTokenMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/pair?token=secret-token&other=keep", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	location := resp.Header.Get("Location")
	if strings.Contains(location, "token=") {
		t.Fatalf("redirect URL still contains token param: %s", location)
	}
	if !strings.Contains(location, "other=keep") {
		t.Fatalf("redirect URL lost non-token params: %s", location)
	}
}

func TestPairAccessTokenMiddleware_CookieIsHashed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetPairAccessToken("secret-token")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := server.pairAccessTokenMiddleware(next)

	// Use header token (no redirect) to inspect cookie value.
	req := httptest.NewRequest(http.MethodGet, "/pair", nil)
	req.Header.Set("X-Cdev-Token", "secret-token")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	var pairCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "cdev_pair_token" {
			pairCookie = c
			break
		}
	}
	if pairCookie == nil {
		t.Fatal("expected cdev_pair_token cookie to be set")
	}
	// Cookie must NOT contain the raw token.
	if pairCookie.Value == "secret-token" {
		t.Fatal("cookie contains raw token — expected HMAC hash")
	}
	// Cookie must have MaxAge set.
	if pairCookie.MaxAge != 86400 {
		t.Fatalf("cookie MaxAge = %d, want 86400", pairCookie.MaxAge)
	}
}

func TestPairAccessTokenMiddleware_ReferrerPolicySet(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetPairAccessToken("secret-token")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := server.pairAccessTokenMiddleware(next)

	// Even rejected requests on pairing routes should get the header.
	req := httptest.NewRequest(http.MethodGet, "/pair", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	rp := resp.Header.Get("Referrer-Policy")
	if rp != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want %q", rp, "no-referrer")
	}
}

func TestPairAccessTokenMiddleware_AllowsCookieAfterInitialToken(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetPairAccessToken("secret-token")

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	wrapped := server.pairAccessTokenMiddleware(next)

	// First request with query token redirects and sets cookie.
	firstReq := httptest.NewRequest(http.MethodGet, "/pair?token=secret-token", nil)
	firstW := httptest.NewRecorder()
	wrapped.ServeHTTP(firstW, firstReq)
	firstResp := firstW.Result()
	defer func() { _ = firstResp.Body.Close() }()
	if firstResp.StatusCode != http.StatusFound {
		t.Fatalf("first request status = %d, want %d", firstResp.StatusCode, http.StatusFound)
	}
	cookies := firstResp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected pairing token cookie to be set")
	}

	var pairCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "cdev_pair_token" {
			pairCookie = c
			break
		}
	}
	if pairCookie == nil {
		t.Fatal("expected cdev_pair_token cookie to be set")
	}

	// Second request without query/header should pass via hashed cookie.
	secondReq := httptest.NewRequest(http.MethodGet, "/pair", nil)
	secondReq.AddCookie(pairCookie)
	secondW := httptest.NewRecorder()
	wrapped.ServeHTTP(secondW, secondReq)

	secondResp := secondW.Result()
	defer func() { _ = secondResp.Body.Close() }()
	if secondResp.StatusCode != http.StatusNoContent {
		t.Fatalf("second request status = %d, want %d", secondResp.StatusCode, http.StatusNoContent)
	}
}

func TestServer_HandleStatus_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	w := httptest.NewRecorder()

	server.handleStatus(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGetFile_MissingPath(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/file", nil)
	w := httptest.NewRecorder()

	server.handleGetFile(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(body, &result)

	if result["error"] == nil {
		t.Error("expected error field in response")
	}
}

func TestServer_HandleGetFile_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/file", nil)
	w := httptest.NewRecorder()

	server.handleGetFile(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGetFile_NoGitTracker(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/file?path=test.txt", nil)
	w := httptest.NewRecorder()

	server.handleGetFile(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test files
	_ = os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	_ = os.WriteFile(filepath.Join(tmpDir, "file2.go"), []byte("content2"), 0644)
	_ = os.Mkdir(filepath.Join(tmpDir, "subdir"), 0755)

	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create subdirectory with files
	subDir := filepath.Join(tmpDir, "subdir")
	_ = os.Mkdir(subDir, 0755)
	_ = os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested content"), 0644)

	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=subdir", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var result DirectoryListingResponse
	_ = json.Unmarshal(body, &result)

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
	defer func() { _ = os.RemoveAll(tmpDir) }()

	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, tmpDir)

	// Attempt path traversal
	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=../../../etc", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for path traversal, got %d", resp.StatusCode)
	}
}

func TestServer_HandleFilesList_SymlinkTraversal(t *testing.T) {
	// Skip on Windows where symlinks may require elevated privileges
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink test on Windows")
	}

	tmpDir, err := os.MkdirTemp("", "cdev-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	outsideDir := t.TempDir()

	symlinkPath := filepath.Join(tmpDir, "outside")
	if err := os.Symlink(outsideDir, symlinkPath); err != nil {
		t.Skip("Cannot create symlink:", err)
	}

	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=outside", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for symlink traversal, got %d", resp.StatusCode)
	}
}

func TestServer_HandleFilesList_NotADirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cdev-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	_ = os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("content"), 0644)

	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/files/list?path=file.txt", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400 for file path, got %d", resp.StatusCode)
	}
}

func TestServer_HandleFilesList_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/files/list", nil)
	w := httptest.NewRecorder()

	server.handleFilesList(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/run", nil)
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_InvalidJSON(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/run", strings.NewReader("invalid json"))
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_EmptyPrompt(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/run", strings.NewReader(`{"prompt":""}`))
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRun_NoManager(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/run", strings.NewReader(`{"prompt":"test"}`))
	w := httptest.NewRecorder()

	server.handleClaudeRun(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeStop_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/stop", nil)
	w := httptest.NewRecorder()

	server.handleClaudeStop(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeStop_NoManager(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/stop", nil)
	w := httptest.NewRecorder()

	server.handleClaudeStop(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRespond_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/respond", nil)
	w := httptest.NewRecorder()

	server.handleClaudeRespond(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRespond_InvalidJSON(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/respond", strings.NewReader("invalid"))
	w := httptest.NewRecorder()

	server.handleClaudeRespond(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeRespond_MissingToolUseID(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPost, "/api/claude/respond", strings.NewReader(`{"response":"yes"}`))
	w := httptest.NewRecorder()

	server.handleClaudeRespond(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeSessions_MethodNotAllowed(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodPut, "/api/claude/sessions", nil)
	w := httptest.NewRecorder()

	server.handleClaudeSessions(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

func TestServer_HandleClaudeSessionMessages_MissingSessionID(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/claude/sessions/messages", nil)
	w := httptest.NewRecorder()

	server.handleClaudeSessionMessages(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGitStatus_NoTracker(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/git/status", nil)
	w := httptest.NewRecorder()

	server.handleGitStatus(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestServer_HandleGitDiff_NoTracker(t *testing.T) {
	server := New("localhost", 16180, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

	req := httptest.NewRequest(http.MethodGet, "/api/git/diff", nil)
	w := httptest.NewRecorder()

	server.handleGitDiff(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestServer_StartStop(t *testing.T) {
	server := New("127.0.0.1", 0, nil, nil, nil, nil, nil, nil, 100, 100, "/tmp")

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
		{"abcd", 4, "abcd"},    // len == maxLen, no truncation
		{"abcde", 4, "a..."},   // len > maxLen: 1 char + "..." = 4
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

	// Create a server to access the corsMiddleware method
	server := &Server{}
	wrapped := server.corsMiddleware(handler)

	// Request without Origin header (same-origin request)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// No origin header means same-origin request, so no CORS headers needed
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
}

func TestCorsMiddleware_Options(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should not see this"))
	})

	server := &Server{}
	wrapped := server.corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for OPTIONS, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) > 0 {
		t.Error("OPTIONS request should return empty body")
	}
}

// --- CORS Security Tests ---

func TestCorsMiddleware_WithOriginChecker_AllowedOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create server with origin checker allowing specific origins
	server := &Server{}
	server.SetOriginChecker(security.NewOriginChecker(
		[]string{"https://example.com"},
		true, // bindLocalhostOnly
	))

	wrapped := server.corsMiddleware(handler)

	// Test allowed origin
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	// Check that the specific origin is returned (not wildcard)
	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin != "https://example.com" {
		t.Errorf("expected Access-Control-Allow-Origin: https://example.com, got %q", allowOrigin)
	}

	// Check credentials header is set
	allowCreds := resp.Header.Get("Access-Control-Allow-Credentials")
	if allowCreds != "true" {
		t.Errorf("expected Access-Control-Allow-Credentials: true, got %q", allowCreds)
	}
}

func TestCorsMiddleware_WithOriginChecker_RejectedOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should not see this"))
	})

	// Create server with origin checker allowing only specific origins
	server := &Server{}
	server.SetOriginChecker(security.NewOriginChecker(
		[]string{"https://example.com"},
		false, // NOT bindLocalhostOnly - this ensures non-localhost is rejected
	))

	wrapped := server.corsMiddleware(handler)

	// Test rejected origin
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for unauthorized origin, got %d", resp.StatusCode)
	}
}

func TestCorsMiddleware_LocalhostAllowed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create server with origin checker - localhost should be allowed when bindLocalhostOnly=true
	server := &Server{}
	server.SetOriginChecker(security.NewOriginChecker(
		[]string{}, // Empty allowed origins
		true,       // bindLocalhostOnly
	))

	wrapped := server.corsMiddleware(handler)

	localhostOrigins := []string{
		"http://localhost:3000",
		"http://localhost:8080",
		"http://127.0.0.1:3000",
	}

	for _, origin := range localhostOrigins {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()

			wrapped.ServeHTTP(w, req)

			resp := w.Result()
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected status 200 for localhost origin %s, got %d", origin, resp.StatusCode)
			}
		})
	}
}

func TestCorsMiddleware_NoOriginChecker_LocalhostOnly(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Server without origin checker - should fallback to localhost-only
	server := &Server{}
	wrapped := server.corsMiddleware(handler)

	// Localhost should be allowed
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for localhost, got %d", resp.StatusCode)
	}

	// Non-localhost should be rejected when no origin checker
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Origin", "https://external.com")
	w2 := httptest.NewRecorder()

	wrapped.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("expected status 403 for non-localhost without checker, got %d", resp2.StatusCode)
	}
}

func TestCorsMiddleware_WildcardSubdomain(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Create server with wildcard subdomain pattern
	server := &Server{}
	server.SetOriginChecker(security.NewOriginChecker(
		[]string{"*.devtunnels.ms"},
		false,
	))

	wrapped := server.corsMiddleware(handler)

	// Test allowed wildcard subdomain
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://my-tunnel.devtunnels.ms")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for wildcard subdomain, got %d", resp.StatusCode)
	}
}

func TestCorsMiddleware_OptionsWithOrigin(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for OPTIONS")
	})

	server := &Server{}
	server.SetOriginChecker(security.NewOriginChecker(
		[]string{"https://example.com"},
		true,
	))

	wrapped := server.corsMiddleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/api/status", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 for preflight, got %d", resp.StatusCode)
	}

	// Should have CORS headers
	if resp.Header.Get("Access-Control-Allow-Methods") == "" {
		t.Error("expected Access-Control-Allow-Methods header")
	}
	if resp.Header.Get("Access-Control-Allow-Headers") == "" {
		t.Error("expected Access-Control-Allow-Headers header")
	}
}

// --- Rate Limiting Tests ---

func TestRateLimiter_AllowRequests(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(5),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	key := "test-client"

	// First 5 requests should be allowed
	for i := 0; i < 5; i++ {
		if !limiter.Allow(key) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th request should be rate limited
	if limiter.Allow(key) {
		t.Error("6th request should be rate limited")
	}
}

func TestRateLimiter_Remaining(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(10),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	key := "test-client"

	// Initially should have 10 remaining
	if remaining := limiter.Remaining(key); remaining != 10 {
		t.Errorf("expected 10 remaining, got %d", remaining)
	}

	// After 3 requests, should have 7 remaining
	for i := 0; i < 3; i++ {
		limiter.Allow(key)
	}

	if remaining := limiter.Remaining(key); remaining != 7 {
		t.Errorf("expected 7 remaining, got %d", remaining)
	}
}

func TestRateLimiter_Reset(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(3),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	key := "test-client"

	// Use up all requests
	for i := 0; i < 3; i++ {
		limiter.Allow(key)
	}

	// Should be rate limited
	if limiter.Allow(key) {
		t.Error("should be rate limited after 3 requests")
	}

	// Reset the key
	limiter.Reset(key)

	// Should be allowed again
	if !limiter.Allow(key) {
		t.Error("should be allowed after reset")
	}
}

func TestRateLimiter_ResetAll(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(2),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	keys := []string{"client1", "client2", "client3"}

	// Use up all requests for each key
	for _, key := range keys {
		for i := 0; i < 2; i++ {
			limiter.Allow(key)
		}
	}

	// All should be rate limited
	for _, key := range keys {
		if limiter.Allow(key) {
			t.Errorf("key %s should be rate limited", key)
		}
	}

	// Reset all
	limiter.ResetAll()

	// All should be allowed
	for _, key := range keys {
		if !limiter.Allow(key) {
			t.Errorf("key %s should be allowed after reset all", key)
		}
	}
}

func TestRateLimitMiddleware_ReturnsHeaders(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(10),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.RateLimitMiddleware(limiter, middleware.IPKeyExtractor)(handler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.100:12345"
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	// Check rate limit headers
	limitHeader := resp.Header.Get("X-RateLimit-Limit")
	if limitHeader != "10" {
		t.Errorf("expected X-RateLimit-Limit: 10, got %q", limitHeader)
	}

	remainingHeader := resp.Header.Get("X-RateLimit-Remaining")
	if remainingHeader != "9" {
		t.Errorf("expected X-RateLimit-Remaining: 9, got %q", remainingHeader)
	}
}

func TestRateLimitMiddleware_Returns429WhenExceeded(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(2),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.RateLimitMiddleware(limiter, middleware.IPKeyExtractor)(handler)

	// Make 3 requests - third should be rate limited
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.100:12345"
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		resp := w.Result()

		if i < 2 {
			if resp.StatusCode != http.StatusOK {
				t.Errorf("request %d: expected status 200, got %d", i+1, resp.StatusCode)
			}
		} else {
			if resp.StatusCode != http.StatusTooManyRequests {
				t.Errorf("request %d: expected status 429, got %d", i+1, resp.StatusCode)
			}

			// Check Retry-After header
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter == "" {
				t.Error("expected Retry-After header when rate limited")
			}

			// Check error response body
			body, _ := io.ReadAll(resp.Body)
			if !strings.Contains(string(body), "rate limit exceeded") {
				t.Errorf("expected rate limit error message, got %s", body)
			}
		}

		_ = resp.Body.Close()
	}
}

func TestRateLimitMiddleware_DifferentIPsIndependent(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(2),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.RateLimitMiddleware(limiter, middleware.IPKeyExtractor)(handler)

	// Each IP should have independent rate limits
	ips := []string{"192.168.1.1:12345", "192.168.1.2:12345", "192.168.1.3:12345"}

	for _, ip := range ips {
		// Each IP gets 2 requests
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = ip
			w := httptest.NewRecorder()

			wrapped.ServeHTTP(w, req)

			resp := w.Result()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("IP %s request %d: expected 200, got %d", ip, i+1, resp.StatusCode)
			}
			_ = resp.Body.Close()
		}

		// 3rd request from same IP should be rate limited
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()

		wrapped.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Errorf("IP %s 3rd request: expected 429, got %d", ip, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestIPKeyExtractor(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"ipv4 with port", "192.168.1.100:12345", "192.168.1.100"},
		{"ipv4 without port", "192.168.1.100", "192.168.1.100"},
		{"ipv6 with port", "[::1]:12345", "::1"},
		{"localhost", "127.0.0.1:8080", "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			key := middleware.IPKeyExtractor(req)
			if key != tt.expected {
				t.Errorf("expected key %q, got %q", tt.expected, key)
			}
		})
	}
}

func TestServer_WithRateLimiter(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(3),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	statusFn := func() map[string]interface{} {
		return map[string]interface{}{"status": "ok"}
	}

	server := New("127.0.0.1", 0, statusFn, nil, nil, nil, nil, nil, 100, 100, "/tmp")
	server.SetRateLimiter(limiter)

	err := server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Stop(ctx)
	}()

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Server should have rate limiter set
	if server.rateLimiter == nil {
		t.Error("expected rate limiter to be set")
	}
}

func TestIsLocalhostOrigin(t *testing.T) {
	tests := []struct {
		origin   string
		expected bool
	}{
		{"http://localhost:3000", true},
		{"http://localhost", true},
		{"http://127.0.0.1:8080", true},
		{"http://127.0.0.1", true},
		{"http://[::1]:3000", true},
		{"http://example.com", false},
		{"https://evil.localhost.com", false},
		{"http://notlocalhost:3000", false},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			result := isLocalhostOrigin(tt.origin)
			if result != tt.expected {
				t.Errorf("isLocalhostOrigin(%q) = %v, want %v", tt.origin, result, tt.expected)
			}
		})
	}
}

// --- Combined CORS + Rate Limiting Integration Test ---

func TestServer_CorsAndRateLimitingIntegration(t *testing.T) {
	limiter := middleware.NewRateLimiter(
		middleware.WithMaxRequests(5),
		middleware.WithWindow(time.Minute),
	)
	defer limiter.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := &Server{}
	server.SetOriginChecker(security.NewOriginChecker(
		[]string{"https://allowed.com"},
		true,
	))
	server.SetRateLimiter(limiter)

	// Build middleware chain like Start() does
	h := server.corsMiddleware(handler)
	h = middleware.RateLimitMiddleware(limiter, middleware.IPKeyExtractor)(h)

	// Test 1: Allowed origin with rate limiting
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", "https://allowed.com")
		req.RemoteAddr = "192.168.1.1:12345"
		w := httptest.NewRecorder()

		h.ServeHTTP(w, req)

		resp := w.Result()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// Request 6 should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://allowed.com")
	req.RemoteAddr = "192.168.1.1:12345"
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("6th request: expected 429, got %d", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Test 2: Disallowed origin should be rejected before rate limiting
	limiter.ResetAll()
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.Header.Set("Origin", "https://evil.com")
	req2.RemoteAddr = "192.168.1.2:12345"
	w2 := httptest.NewRecorder()

	h.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	if resp2.StatusCode != http.StatusForbidden {
		t.Errorf("disallowed origin: expected 403, got %d", resp2.StatusCode)
	}
	_ = resp2.Body.Close()
}

func newTestTokenManager(t *testing.T) *security.TokenManager {
	t.Helper()
	secretPath := filepath.Join(t.TempDir(), "token_secret.json")
	manager, err := security.NewTokenManagerWithPath(300, secretPath)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	return manager
}
