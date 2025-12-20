# Technical Review: cdev Security & Performance Analysis

**Document Type:** Technical Review & Roadmap
**Version:** 1.0.0
**Date:** December 2025
**Status:** Active

---

## Executive Summary

cdev is a well-architected Go daemon implementing hexagonal architecture (ports & adapters) for remote Claude Code CLI management. The codebase demonstrates solid Go practices with clear separation of concerns. However, as a POC transitioning toward production readiness, several security and performance improvements are required.

### Overall Assessment

| Category | Grade | Notes |
|----------|-------|-------|
| Architecture | A | Excellent hexagonal design, clean interfaces |
| Code Quality | B+ | Well-structured, readable, follows Go conventions |
| Security | C- | Multiple critical issues for production use |
| Performance | B | Acceptable for POC, optimization opportunities exist |
| Testing | D | No test coverage found |
| Documentation | A | Comprehensive docs, good API reference |
| Production Readiness | D+ | Requires security hardening before production |

---

## 1. Security Analysis

### 1.1 Critical Issues (Must Fix)

#### ISSUE-SEC-001: CORS Allow-All Configuration

**Severity:** CRITICAL
**Location:** `internal/server/http/server.go`, `internal/server/websocket/server.go`

**Current Implementation:**
```go
// HTTP
w.Header().Set("Access-Control-Allow-Origin", "*")

// WebSocket
CheckOrigin: func(r *http.Request) bool {
    return true  // Allow all origins
}
```

**Risk:** Enables Cross-Site Request Forgery (CSRF) attacks. Any malicious website can make requests to the agent if running on localhost.

**Recommendation:**
```go
// HTTP CORS middleware
func corsMiddleware(allowedOrigins map[string]bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")
            if allowedOrigins[origin] || origin == "" {
                w.Header().Set("Access-Control-Allow-Origin", origin)
            }
            next.ServeHTTP(w, r)
        })
    }
}

// WebSocket
CheckOrigin: func(r *http.Request) bool {
    origin := r.Header.Get("Origin")
    return allowedOrigins[origin] || origin == ""
}
```

**Config Addition:**
```yaml
security:
  allowed_origins:
    - "http://localhost:3000"
    - "http://127.0.0.1:3000"
```

---

#### ISSUE-SEC-002: No Authentication

**Severity:** CRITICAL
**Location:** All endpoints

**Current State:** No authentication mechanism exists. Any client can:
- Start/stop Claude processes
- Access file contents
- View git diffs
- Respond to permission requests

**Recommendation:** Implement token-based authentication:

```go
// Token middleware
func authMiddleware(validTokens map[string]bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("X-Auth-Token")
            if token == "" {
                token = r.URL.Query().Get("token")
            }
            if !validTokens[token] {
                http.Error(w, "Unauthorized", http.StatusUnauthorized)
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

**Implementation Options:**
1. **Simple Token:** Generate random token on startup, display in terminal/QR code
2. **JWT:** For multi-session support with token expiry
3. **mTLS:** For high-security environments

---

#### ISSUE-SEC-003: File Reading Uses External Command

**Severity:** HIGH
**Location:** `internal/adapters/git/tracker.go:297`

**Current Implementation:**
```go
cmd := exec.CommandContext(ctx, "cat", fullPath)
```

**Issues:**
1. `cat` command not available on Windows
2. Unnecessary external process spawn
3. Potential for edge-case path handling issues

**Recommendation:**
```go
func (t *Tracker) GetFileContent(ctx context.Context, path string, maxSizeKB int) (string, bool, error) {
    fullPath := filepath.Join(t.repoRoot, path)

    // Validate path is within repo (improved validation)
    absPath, err := filepath.Abs(fullPath)
    if err != nil {
        return "", false, err
    }

    absRoot, err := filepath.Abs(t.repoRoot)
    if err != nil {
        return "", false, err
    }

    // Ensure trailing separator for proper prefix matching
    if !strings.HasSuffix(absRoot, string(filepath.Separator)) {
        absRoot += string(filepath.Separator)
    }

    if !strings.HasPrefix(absPath, absRoot) {
        return "", false, domain.ErrPathOutsideRepo
    }

    // Use os.ReadFile instead of cat
    content, err := os.ReadFile(fullPath)
    if err != nil {
        if os.IsNotExist(err) {
            return "", false, domain.ErrFileNotFound
        }
        return "", false, fmt.Errorf("failed to read file: %w", err)
    }

    // Check truncation
    maxSize := maxSizeKB * 1024
    truncated := len(content) > maxSize
    if truncated {
        content = content[:maxSize]
    }

    return string(content), truncated, nil
}
```

---

### 1.2 High Severity Issues

#### ISSUE-SEC-004: Fragile Path Validation

**Severity:** HIGH
**Location:** `internal/adapters/git/tracker.go:282-293`

**Current Implementation:**
```go
if !strings.HasPrefix(absPath, absRoot) {
    return "", false, domain.ErrPathOutsideRepo
}
```

**Issue:** String prefix matching can be bypassed:
- `/repo` matches `/repository/` incorrectly
- Case sensitivity issues on Windows
- No symlink resolution

**Recommendation:**
```go
func validatePathInRepo(repoRoot, requestedPath string) (string, error) {
    absRoot, err := filepath.Abs(repoRoot)
    if err != nil {
        return "", err
    }

    fullPath := filepath.Join(absRoot, requestedPath)
    fullPath = filepath.Clean(fullPath)

    // Resolve symlinks
    realPath, err := filepath.EvalSymlinks(fullPath)
    if err != nil && !os.IsNotExist(err) {
        return "", err
    }
    if realPath != "" {
        fullPath = realPath
    }

    realRoot, err := filepath.EvalSymlinks(absRoot)
    if err != nil {
        return "", err
    }

    // Use filepath.Rel for proper validation
    rel, err := filepath.Rel(realRoot, fullPath)
    if err != nil {
        return "", domain.ErrPathOutsideRepo
    }

    // Check for directory traversal
    if strings.HasPrefix(rel, "..") {
        return "", domain.ErrPathOutsideRepo
    }

    return fullPath, nil
}
```

---

#### ISSUE-SEC-005: No Rate Limiting

**Severity:** HIGH
**Location:** All endpoints

**Risk:** Denial of Service through:
- Rapid API requests
- WebSocket message flooding
- Multiple Claude process start attempts

**Recommendation:**
```go
import "golang.org/x/time/rate"

type RateLimiter struct {
    visitors map[string]*rate.Limiter
    mu       sync.RWMutex
    r        rate.Limit
    b        int
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
    return &RateLimiter{
        visitors: make(map[string]*rate.Limiter),
        r:        rate.Limit(rps),
        b:        burst,
    }
}

func (rl *RateLimiter) GetLimiter(ip string) *rate.Limiter {
    rl.mu.Lock()
    defer rl.mu.Unlock()

    limiter, exists := rl.visitors[ip]
    if !exists {
        limiter = rate.NewLimiter(rl.r, rl.b)
        rl.visitors[ip] = limiter
    }
    return limiter
}
```

---

#### ISSUE-SEC-006: No Log Rotation

**Severity:** HIGH
**Location:** `internal/adapters/claude/manager.go` (file logging)

**Current State:** Claude output logs to `.cdev/logs/claude_<pid>.jsonl` without:
- Size limits
- Rotation
- Cleanup of old logs

**Risk:** Disk exhaustion from long-running or malicious Claude output.

**Recommendation:**
```go
import "gopkg.in/natefinch/lumberjack.v2"

type RotatingLogger struct {
    *lumberjack.Logger
}

func NewRotatingLogger(filename string) *RotatingLogger {
    return &RotatingLogger{
        Logger: &lumberjack.Logger{
            Filename:   filename,
            MaxSize:    50,  // MB
            MaxBackups: 3,
            MaxAge:     7,   // days
            Compress:   true,
        },
    }
}
```

---

### 1.3 Medium Severity Issues

#### ISSUE-SEC-007: Insufficient Input Validation

**Severity:** MEDIUM
**Location:** Various handlers

**Current State:** Limited validation on:
- Prompt length (10000 chars max, but could be DoS vector)
- Session ID format (regex exists but not always applied early)
- File paths (validated but fragile)

**Recommendation:** Add JSON schema validation:
```go
type RunClaudeRequest struct {
    Prompt    string `json:"prompt" validate:"required,min=1,max=5000"`
    Mode      string `json:"mode" validate:"omitempty,oneof=new continue resume"`
    SessionID string `json:"session_id" validate:"omitempty,uuid4"`
}

func validateRequest(req interface{}) error {
    validate := validator.New()
    return validate.Struct(req)
}
```

---

#### ISSUE-SEC-008: No TLS/HTTPS

**Severity:** MEDIUM (localhost-only mitigates)
**Location:** `internal/server/http/server.go`

**Current State:** Plain HTTP only. While localhost binding provides some protection, TLS would enable:
- Secure remote access
- Certificate-based authentication
- Encrypted communication

**Recommendation for Production:**
```go
func (s *Server) StartTLS(certFile, keyFile string) error {
    return s.server.ListenAndServeTLS(certFile, keyFile)
}
```

---

### 1.4 Security Recommendations Summary

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| P0 | CORS Configuration | Low | High |
| P0 | Authentication | Medium | Critical |
| P0 | File Reading (cat → os.ReadFile) | Low | High |
| P1 | Path Validation | Medium | High |
| P1 | Rate Limiting | Medium | High |
| P1 | Log Rotation | Low | Medium |
| P2 | Input Validation | Medium | Medium |
| P2 | TLS Support | Medium | Medium |

---

## 2. Performance Analysis

### 2.1 Current Performance Characteristics

| Metric | Target | Current | Status |
|--------|--------|---------|--------|
| Diff latency | < 1 second | ~100-500ms | ✅ Good |
| Log streaming | Real-time | ~10ms | ✅ Good |
| Agent idle CPU | < 1% | < 1% | ✅ Good |
| Agent memory | < 100MB | ~20-50MB | ✅ Good |
| WebSocket latency | < 50ms | ~5-20ms | ✅ Good |

### 2.2 Identified Bottlenecks

#### PERF-001: Git Status Per File Change

**Location:** `internal/app/app.go` (file change handler)

**Current Behavior:** Every file change triggers:
1. `git status` command
2. `git diff` for the changed file

**Impact:** In rapid file change scenarios (e.g., Claude writing multiple files), this causes process spawn overhead.

**Recommendation:** Implement caching with TTL:
```go
type GitStatusCache struct {
    status    []ports.GitFileStatus
    timestamp time.Time
    ttl       time.Duration
    mu        sync.RWMutex
}

func (c *GitStatusCache) Get(ctx context.Context, tracker *Tracker) ([]ports.GitFileStatus, error) {
    c.mu.RLock()
    if time.Since(c.timestamp) < c.ttl {
        status := c.status
        c.mu.RUnlock()
        return status, nil
    }
    c.mu.RUnlock()

    c.mu.Lock()
    defer c.mu.Unlock()

    // Double-check after acquiring write lock
    if time.Since(c.timestamp) < c.ttl {
        return c.status, nil
    }

    status, err := tracker.Status(ctx)
    if err != nil {
        return nil, err
    }

    c.status = status
    c.timestamp = time.Now()
    return status, nil
}
```

**Configuration:**
```yaml
performance:
  git_status_cache_ttl_ms: 2000  # 2 seconds
```

---

#### PERF-002: Event Hub Sequential Dispatch

**Location:** `internal/hub/hub.go`

**Current Behavior:** Events dispatched sequentially to subscribers. If one subscriber is slow, it blocks others.

**Recommendation:** Add timeout to Send():
```go
func (h *Hub) broadcast(event events.Event) {
    h.mu.RLock()
    defer h.mu.RUnlock()

    var wg sync.WaitGroup
    for _, sub := range h.subscribers {
        wg.Add(1)
        go func(s ports.Subscriber) {
            defer wg.Done()

            // Use timeout channel
            done := make(chan struct{})
            go func() {
                if err := s.Send(event); err != nil {
                    log.Warn().Err(err).Str("subscriber", s.ID()).Msg("send failed")
                    h.unregisterCh <- s.ID()
                }
                close(done)
            }()

            select {
            case <-done:
                // Success
            case <-time.After(5 * time.Second):
                log.Warn().Str("subscriber", s.ID()).Msg("send timeout")
                h.unregisterCh <- s.ID()
            }
        }(sub)
    }
    wg.Wait()
}
```

---

#### PERF-003: No Connection Pooling for Git Operations

**Location:** `internal/adapters/git/tracker.go`

**Current Behavior:** Each git command spawns a new process.

**Impact:** Process creation overhead (~1-5ms per command).

**Recommendation for High-Load Scenarios:** Consider using `go-git` for read operations:
```go
import "github.com/go-git/go-git/v5"

// For frequently accessed operations
func (t *Tracker) StatusFast(ctx context.Context) ([]ports.GitFileStatus, error) {
    repo, err := git.PlainOpen(t.repoRoot)
    if err != nil {
        return nil, err
    }

    worktree, err := repo.Worktree()
    if err != nil {
        return nil, err
    }

    status, err := worktree.Status()
    if err != nil {
        return nil, err
    }

    // Convert to our format...
}
```

**Trade-off:** Adds ~2MB to binary size, introduces more dependencies.

---

### 2.3 Performance Recommendations Summary

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| P1 | Git Status Caching | Low | Medium |
| P2 | Event Dispatch Timeout | Medium | Low |
| P3 | Connection Pooling | High | Low |
| P3 | Event Batching | Medium | Low |

---

## 3. Testing Gap Analysis

### 3.1 Current State

**Test Coverage:** 0% (no test files found)

### 3.2 Critical Test Needs

| Package | Priority | Test Types Needed |
|---------|----------|-------------------|
| `internal/adapters/git/` | P0 | Path validation, diff parsing |
| `internal/hub/` | P0 | Pub-sub, concurrent access |
| `internal/adapters/claude/` | P1 | State machine, stream parsing |
| `internal/server/websocket/` | P1 | Connection handling, message routing |
| `internal/server/http/` | P1 | Request validation, error responses |
| `internal/config/` | P2 | Config loading, validation |

### 3.3 Recommended Test Structure

```
internal/
├── adapters/
│   ├── git/
│   │   ├── tracker.go
│   │   └── tracker_test.go      # Path validation, parsing
│   ├── claude/
│   │   ├── manager.go
│   │   └── manager_test.go      # State machine, mock process
│   └── watcher/
│       ├── watcher.go
│       └── watcher_test.go      # Pattern matching
├── hub/
│   ├── hub.go
│   └── hub_test.go              # Concurrent pub-sub
└── server/
    ├── http/
    │   ├── handlers.go
    │   └── handlers_test.go     # HTTP request/response
    └── websocket/
        ├── client.go
        └── client_test.go       # WebSocket messages

test/
├── integration/
│   ├── e2e_test.go             # Full system tests
│   └── claude_integration_test.go
└── fixtures/
    ├── repos/                   # Sample git repos
    └── claude_output/           # Sample Claude output
```

---

## 4. Architecture Improvements

### 4.1 Recommended Enhancements

#### Observability Stack

```go
// Add metrics endpoint
import "github.com/prometheus/client_golang/prometheus"

var (
    eventsPublished = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "cdev_events_published_total",
            Help: "Total events published",
        },
        []string{"type"},
    )

    claudeProcessDuration = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "cdev_claude_duration_seconds",
            Help:    "Claude process duration",
            Buckets: prometheus.ExponentialBuckets(1, 2, 10),
        },
    )
)
```

#### Health Check Enhancement

```go
type HealthStatus struct {
    Status     string            `json:"status"`
    Time       time.Time         `json:"time"`
    Components map[string]string `json:"components"`
    Version    string            `json:"version"`
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
    status := HealthStatus{
        Status:  "ok",
        Time:    time.Now(),
        Version: version.Version,
        Components: map[string]string{
            "claude":    s.claudeManager.State().String(),
            "watcher":   "running",
            "git":       boolToStatus(s.gitTracker.IsGitRepo()),
            "websocket": fmt.Sprintf("%d clients", s.wsHub.ClientCount()),
        },
    }
    json.NewEncoder(w).Encode(status)
}
```

---

## 5. Roadmap & Backlog

### Phase 1: Security Hardening (1-2 weeks)

| ID | Task | Priority | Effort | Status |
|----|------|----------|--------|--------|
| SEC-001 | Fix CORS configuration | P0 | 2h | Pending |
| SEC-002 | Implement token authentication | P0 | 8h | Pending |
| SEC-003 | Replace `cat` with `os.ReadFile` | P0 | 1h | Pending |
| SEC-004 | Improve path validation | P1 | 4h | Pending |
| SEC-005 | Add rate limiting | P1 | 4h | Pending |
| SEC-006 | Implement log rotation | P1 | 2h | Pending |

**Deliverables:**
- [ ] Secure CORS with configurable allowed origins
- [ ] Token-based authentication with QR code display
- [ ] Cross-platform file reading
- [ ] Robust path traversal prevention
- [ ] Rate limiting on all endpoints
- [ ] Log rotation with size limits

---

### Phase 2: Testing Foundation (2-3 weeks)

| ID | Task | Priority | Effort | Status |
|----|------|----------|--------|--------|
| TEST-001 | Path validation tests | P0 | 4h | Pending |
| TEST-002 | Hub pub-sub tests | P0 | 4h | Pending |
| TEST-003 | Claude manager tests | P1 | 8h | Pending |
| TEST-004 | HTTP handler tests | P1 | 6h | Pending |
| TEST-005 | WebSocket tests | P1 | 6h | Pending |
| TEST-006 | Integration test framework | P2 | 8h | Pending |
| TEST-007 | CI/CD pipeline | P2 | 4h | Pending |

**Deliverables:**
- [ ] >60% unit test coverage
- [ ] Integration test suite
- [ ] GitHub Actions CI pipeline
- [ ] Test fixtures and mocks

---

### Phase 3: Performance Optimization (1-2 weeks)

| ID | Task | Priority | Effort | Status |
|----|------|----------|--------|--------|
| PERF-001 | Git status caching | P1 | 4h | Pending |
| PERF-002 | Event dispatch timeout | P2 | 4h | Pending |
| PERF-003 | Add pprof endpoints | P2 | 2h | Pending |
| PERF-004 | Memory profiling | P2 | 4h | Pending |

**Deliverables:**
- [ ] Git operation caching
- [ ] Non-blocking event dispatch
- [ ] Performance profiling endpoints
- [ ] Baseline performance benchmarks

---

### Phase 4: Production Features (2-4 weeks)

| ID | Task | Priority | Effort | Status |
|----|------|----------|--------|--------|
| PROD-001 | TLS/HTTPS support | P1 | 8h | Pending |
| PROD-002 | Prometheus metrics | P2 | 6h | Pending |
| PROD-003 | Structured error codes | P2 | 4h | Pending |
| PROD-004 | Graceful shutdown improvements | P2 | 4h | Pending |
| PROD-005 | systemd/launchd service files | P3 | 4h | Pending |
| PROD-006 | Docker container | P3 | 4h | Pending |

**Deliverables:**
- [ ] TLS encryption support
- [ ] Prometheus metrics endpoint
- [ ] Standardized error responses
- [ ] Production deployment guides
- [ ] Container images

---

### Phase 5: Future Enhancements (Backlog)

| ID | Task | Priority | Effort | Status |
|----|------|----------|--------|--------|
| FUTURE-001 | Cloud relay integration | P3 | 40h | Backlog |
| FUTURE-002 | Multi-session support | P3 | 24h | Backlog |
| FUTURE-003 | iOS app development | P3 | 80h+ | Backlog |
| FUTURE-004 | Real-time collaboration | P4 | 40h | Backlog |
| FUTURE-005 | Plugin system | P4 | 24h | Backlog |

---

## 6. Implementation Checklist

### Immediate Actions (This Sprint)

- [ ] **SEC-001:** Fix CORS - restrict to localhost origins
- [ ] **SEC-003:** Replace `cat` command with `os.ReadFile()`
- [ ] **SEC-004:** Improve path validation with `filepath.Rel()`
- [ ] **TEST-001:** Add path validation tests

### Short Term (Next 2 Sprints)

- [ ] **SEC-002:** Implement authentication tokens
- [ ] **SEC-005:** Add rate limiting middleware
- [ ] **SEC-006:** Add log rotation
- [ ] **TEST-002:** Add hub tests
- [ ] **TEST-004:** Add HTTP handler tests

### Medium Term (This Quarter)

- [ ] **PERF-001:** Git status caching
- [ ] **PROD-001:** TLS support
- [ ] **PROD-002:** Prometheus metrics
- [ ] **TEST-006:** Integration test framework
- [ ] **TEST-007:** CI/CD pipeline

---

## 7. Risk Assessment

| Risk | Probability | Impact | Mitigation |
|------|-------------|--------|------------|
| CSRF attack via CORS | High | High | Fix CORS immediately |
| Unauthorized access | High | Critical | Implement auth |
| Path traversal | Low | High | Improve validation |
| Resource exhaustion | Medium | Medium | Add rate limiting, log rotation |
| Data loss | Low | Low | Events are transient, acceptable |

---

## 8. Conclusion

cdev has a solid architectural foundation with excellent separation of concerns. The hexagonal architecture enables easy testing and future extensions. However, **critical security issues must be addressed before any production use**:

1. **CORS must be restricted** to prevent CSRF attacks
2. **Authentication must be implemented** to prevent unauthorized access
3. **File reading must use native Go** for cross-platform compatibility
4. **Path validation must be hardened** against traversal attacks

The recommended approach is to:
1. Complete Phase 1 (Security Hardening) before any external deployment
2. Establish testing infrastructure in Phase 2
3. Optimize performance as needed in Phase 3
4. Add production features for deployment readiness in Phase 4

With these improvements, cdev will be well-positioned for production use and future feature development.

---

*Document Version: 1.0.0*
*Generated: December 2025*
