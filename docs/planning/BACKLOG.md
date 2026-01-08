# cdev Product Backlog

**Last Updated:** January 2026
**Owner:** Technical Lead

---

## Backlog Overview

| Phase | Theme | Items | Status |
|-------|-------|-------|--------|
| 0 | Core Features | 5 | ✅ Completed |
| 1 | Security Hardening | 6 | ✅ Completed |
| 2 | Testing Foundation | 7 | ✅ Completed (6/7) |
| 3 | Performance Optimization | 4 | Partial (2/4) |
| 4 | Production Features | 6 | Partial (1/6) |
| 5 | Future Enhancements | 5 | Backlog |

---

## MVP Status: ✅ COMPLETE

All MVP requirements have been implemented:
- ✅ Core Features (Phase 0)
- ✅ Security Hardening (Phase 1) - All 6 items complete
- ✅ Testing Foundation (Phase 2) - 6/7 items complete (integration tests deferred)
- ✅ CI/CD Pipeline operational

---

## Phase 0: Core Features ✅ Completed

**Goal:** Establish core functionality for mobile monitoring and control.
**Status:** Completed

### CORE-001: Claude CLI Management ✅
**Status:** Completed

- Cross-platform process management (Unix SIGTERM/SIGKILL, Windows taskkill)
- Bidirectional streaming via stdin/stdout/stderr pipes
- Permission request detection and approval/denial handling
- Interactive prompt detection (AskUserQuestion tool)
- Session continuity: `new`, `continue` modes
- Session ID capture from stream-json output

---

### CORE-002: Session Cache ✅
**Status:** Completed

- SQLite-backed session listing with file watcher sync
- Fast paginated session retrieval
- File mtime tracking for cache invalidation
- Real-time updates via fsnotify

---

### CORE-003: Message Cache with Pagination ✅
**Status:** Completed (December 2025)

- SQLite message cache for fast paginated retrieval
- Lazy indexing (~100ms first access, ~5ms cached)
- Pagination: `limit` (default 50, max 500), `offset`, `order` (asc/desc)
- Response includes: `total`, `has_more`, `cache_hit`, `query_time_ms`
- Supports sessions with 10,000+ messages efficiently

---

### CORE-004: File Watcher with Rename/Delete Detection ✅
**Status:** Completed (December 2025)

- fsnotify-based recursive directory watching
- Debouncing (100ms default) to coalesce rapid changes
- Rename detection via pending rename correlation (RENAME + CREATE within 1s)
- Delete detection via stale pending rename cleanup (macOS fix)
- Configurable ignore patterns (.git, node_modules, etc.)

---

### CORE-005: Repository Indexer ✅
**Status:** Completed (December 2025)

- SQLite + FTS5 full-text search across repository files
- File ID tracking (inode/Windows file ID) for accurate rename detection
- Configurable `skip_directories` via config.yaml
- API endpoints: `/api/repository/search`, `/files`, `/tree`, `/stats`
- Real-time index updates on file changes

---

## Phase 1: Security Hardening ✅ Completed

**Goal:** Address critical security vulnerabilities before any external deployment.
**Status:** ✅ All items completed (January 2026)

### SEC-001: Restrict CORS Configuration ✅
**Priority:** P0 - Critical
**Status:** ✅ Completed (January 2026)

**Description:**
Replace wildcard CORS (`Access-Control-Allow-Origin: *`) with configurable allowed origins.

**Acceptance Criteria:**
- [x] CORS restricted to configurable origins list
- [x] Default allows only `localhost` and `127.0.0.1`
- [x] WebSocket origin check implemented
- [x] Configuration in `config.yaml`

**Implementation:**
- `internal/server/http/server.go` - `corsMiddleware()` with `OriginChecker`
- Origin validation returns specific origin header, not wildcard
- Falls back to localhost-only when no checker configured

---

### SEC-002: Implement Token Authentication ✅
**Priority:** P0 - Critical
**Status:** ✅ Completed (January 2026)

**Description:**
Add token-based authentication for all API endpoints and WebSocket connections.

**Acceptance Criteria:**
- [x] Random token generated on startup
- [x] Token displayed in terminal and QR code
- [x] HTTP endpoints require `X-Auth-Token` header
- [x] WebSocket requires token in query param or first message
- [x] Token configurable or auto-generated

**Implementation:**
- `internal/security/token.go` (505 lines) - Full token management system
- Three token types: Pairing, Session/Access (15 min), Refresh (7 day)
- HMAC-SHA256 signatures for validation
- Token nonce tracking for revocation
- Persistent server secret in `~/.cdev/token_secret.json`
- `internal/server/http/auth.go` - Auth handlers for `/api/auth/exchange` and `/api/auth/refresh`

---

### SEC-003: Replace `cat` with `os.ReadFile` ✅
**Priority:** P0 - Critical
**Status:** ✅ Completed (January 2026)

**Description:**
Use native Go file reading instead of shelling out to `cat` command.

**Acceptance Criteria:**
- [x] `GetFileContent` uses `os.ReadFile`
- [x] Works on Windows, macOS, Linux
- [x] Error handling improved
- [x] Tests added for file reading

**Implementation:**
- `internal/adapters/git/tracker.go:403` - Uses `os.ReadFile()` directly
- No shell execution
- Proper error handling and size validation

---

### SEC-004: Improve Path Validation ✅
**Priority:** P1 - High
**Status:** ✅ Completed (January 2026)

**Description:**
Replace fragile string prefix matching with robust path validation.

**Acceptance Criteria:**
- [x] Use `filepath.Rel` for validation
- [x] Check for `..` prefix in relative path
- [x] Handle symlinks with `filepath.EvalSymlinks`
- [x] Case-insensitive comparison on Windows
- [x] Comprehensive test coverage

**Implementation:**
- `internal/adapters/git/tracker.go:363-416` - Comprehensive validation:
  - Explicit `..` traversal rejection
  - `filepath.Clean()` normalization
  - `filepath.Abs()` resolution
  - Prefix checking with separator (prevents `/repo-evil` matching `/repo`)
  - Directory path rejection
- `internal/adapters/git/tracker_test.go` - Full test coverage

---

### SEC-005: Add Rate Limiting ✅
**Priority:** P1 - High
**Status:** ✅ Completed (January 2026)

**Description:**
Implement rate limiting to prevent DoS attacks.

**Acceptance Criteria:**
- [x] HTTP endpoint rate limiting (100 req/min default)
- [x] WebSocket message rate limiting
- [x] Claude start rate limiting (5/min)
- [x] Configurable limits in config.yaml

**Implementation:**
- `internal/server/http/middleware/ratelimit.go` (303 lines)
- Sliding window rate limiter with per-key limiting
- Default: 10 requests per 60 seconds
- Automatic cleanup of stale buckets
- Returns `X-RateLimit-Limit` and `X-RateLimit-Remaining` headers
- Configurable via `WithMaxRequests()` and `WithWindow()` options

---

### SEC-006: Implement Log Rotation ✅
**Priority:** P1 - High
**Status:** ✅ Completed (January 2026)

**Description:**
Add log rotation to prevent disk exhaustion from Claude output logs.

**Acceptance Criteria:**
- [x] Max log file size: 50MB
- [x] Keep last 3 rotated files
- [x] Compress old logs
- [x] Auto-cleanup after 7 days
- [x] Configurable in config.yaml

**Implementation:**
- `internal/adapters/claude/manager.go:330-359, 531-560`
- Uses `lumberjack.Logger` for automatic rotation
- Configurable via `LogRotationConfig`: MaxSizeMB, MaxBackups, MaxAgeDays, Compress
- Both standard mode and PTY mode support rotation
- Log files: `.cdev/logs/claude_<pid>.jsonl`

---

## Phase 2: Testing Foundation ✅ Completed

**Goal:** Establish comprehensive test coverage and CI/CD pipeline.
**Status:** ✅ 6/7 items completed (January 2026)

### TEST-001: Path Validation Tests ✅
**Priority:** P0 - Critical
**Status:** ✅ Completed (January 2026)

**Description:**
Add thorough tests for path validation security.

**Test Cases:**
- [x] Valid relative paths
- [x] Path traversal attempts (`../`, `..\\`)
- [x] Absolute paths
- [x] Symlink following
- [x] Unicode paths
- [x] Edge cases (empty path, very long paths)

**Implementation:**
- `internal/adapters/git/tracker_test.go` (372 lines)

---

### TEST-002: Event Hub Tests ✅
**Priority:** P0 - Critical
**Status:** ✅ Completed (January 2026)

**Description:**
Test hub pub-sub functionality and concurrent access.

**Test Cases:**
- [x] Subscribe/unsubscribe
- [x] Event broadcasting
- [x] Concurrent publishers
- [x] Slow subscriber handling
- [x] Buffer overflow behavior
- [x] Graceful shutdown

**Implementation:**
- `internal/hub/hub_test.go`

---

### TEST-003: Claude Manager Tests ✅
**Priority:** P1 - High
**Status:** ✅ Completed (January 2026)

**Description:**
Test Claude process management state machine.

**Test Cases:**
- [x] Start process
- [x] Stop process (graceful)
- [x] Kill process (force)
- [x] State transitions
- [x] Output stream parsing
- [x] Permission detection
- [x] Session ID capture
- [x] Timeout handling

**Implementation:**
- `internal/adapters/claude/manager_test.go` (386 lines)

---

### TEST-004: HTTP Handler Tests ✅
**Priority:** P1 - High
**Status:** ✅ Completed (January 2026)

**Description:**
Test HTTP API request handling and responses.

**Test Cases:**
- [x] Health endpoint
- [x] Status endpoint
- [x] Claude run/stop
- [x] File content retrieval
- [x] Git status/diff
- [x] Error responses
- [x] Input validation

**Implementation:**
- `internal/server/http/server_test.go` (613 lines)
- CORS validation, file serving, git operations, auth, rate limiting tests

---

### TEST-005: WebSocket Tests ✅
**Priority:** P1 - High
**Status:** ✅ Completed (January 2026)

**Description:**
Test WebSocket connection handling and message routing.

**Test Cases:**
- [x] Connection establish
- [x] Message sending/receiving
- [x] Command routing
- [x] Event broadcasting
- [x] Ping/pong health
- [x] Connection cleanup

**Implementation:**
- `internal/server/websocket/server_test.go`

---

### TEST-006: Integration Test Framework
**Priority:** P2 - Medium
**Status:** Not Started (Deferred post-MVP)

**Description:**
Set up end-to-end integration testing framework.

**Acceptance Criteria:**
- [ ] Test fixture repos
- [ ] Mock Claude CLI
- [ ] Full system startup/shutdown
- [ ] API integration tests
- [ ] WebSocket integration tests

**Files to Create:**
- `test/integration/setup_test.go`
- `test/integration/e2e_test.go`
- `test/fixtures/`

---

### TEST-007: CI/CD Pipeline ✅
**Priority:** P2 - Medium
**Status:** ✅ Completed (January 2026)

**Description:**
Set up GitHub Actions for automated testing and builds.

**Acceptance Criteria:**
- [x] Run tests on PR
- [x] Run linting (golangci-lint)
- [x] Build for all platforms
- [x] Coverage report
- [x] Release automation

**Implementation:**
- `.github/workflows/ci.yml`
  - Tests on Ubuntu + macOS with Go 1.21 and 1.22
  - Race detection enabled (`go test -v -race ./...`)
  - Coverage reporting with Codecov
  - golangci-lint integration
  - Cross-platform builds (darwin/amd64/arm64, linux/amd64/arm64, windows/amd64)
  - GoReleaser validation

---

## Phase 3: Performance Optimization

**Goal:** Optimize for high-load scenarios and reduce resource usage.
**Status:** Partial (2/4 items)

### PERF-001: Git Status Caching
**Priority:** P1 - High
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Cache git status results with TTL to reduce subprocess spawning.

**Acceptance Criteria:**
- [ ] Status cached for configurable TTL (default 2s)
- [ ] Cache invalidated on file change
- [ ] Thread-safe implementation
- [ ] Metrics for cache hit/miss

**Files to Modify:**
- `internal/adapters/git/tracker.go`
- `internal/adapters/git/cache.go` (new)
- `internal/config/config.go`

---

### PERF-002: Event Dispatch Timeout
**Priority:** P2 - Medium
**Effort:** 4 hours
**Status:** ⏳ Partial

**Description:**
Add timeout to event dispatch to prevent blocking on slow subscribers.

**Acceptance Criteria:**
- [x] Non-blocking channel send with `select`/`default`
- [x] Events dropped if channel full
- [x] Failed subscriber removal
- [ ] 5-second timeout on Send()
- [ ] Warning logged for slow sends
- [ ] Metrics for dispatch latency

**Current Implementation:**
- `internal/hub/hub.go:128-154` - Non-blocking send, drops on full channel
- Missing: Explicit timeout-based subscriber eviction

---

### PERF-003: Add pprof Endpoints ✅
**Priority:** P2 - Medium
**Status:** ✅ Completed (January 2026)

**Description:**
Add profiling endpoints for performance debugging.

**Acceptance Criteria:**
- [x] `/debug/pprof/` endpoints
- [x] Configurable enable/disable
- [x] Protected by localhost-only binding (default)

**Implementation:**
- `internal/config/config.go` - Added `DebugConfig` with `enabled` and `pprof_enabled` flags
- `internal/server/http/debug.go` - New file with `DebugHandler`:
  - `/debug/` - Index page with links to all debug endpoints
  - `/debug/runtime` - Go runtime statistics (JSON)
  - `/debug/pprof/*` - Full pprof suite (heap, goroutine, profile, trace, etc.)
- `internal/server/http/server.go` - Added `SetDebugHandler()` method
- `internal/app/app.go` - Conditional initialization based on config
- Disabled by default (`debug.enabled: false`)
- Timeout middleware skips `/debug/*` paths for long-running profiles

---

### PERF-004: Memory Profiling
**Priority:** P2 - Medium
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Profile memory usage and optimize where needed.

**Acceptance Criteria:**
- [ ] Baseline memory profile
- [ ] Identify memory hotspots
- [ ] Optimize buffer sizes
- [ ] Document memory characteristics

**Deliverables:**
- Memory profile report
- Optimization recommendations

---

## Phase 4: Production Features

**Goal:** Add features required for production deployment.
**Status:** Partial (1/6 items)

### PROD-001: TLS/HTTPS Support
**Priority:** P1 - High
**Effort:** 8 hours
**Status:** Not Started

**Description:**
Add TLS support for secure communication.

**Acceptance Criteria:**
- [ ] HTTPS server option
- [ ] WSS (secure WebSocket) support
- [ ] Self-signed cert generation
- [ ] Custom cert configuration
- [ ] Automatic HTTPS redirect

**Files to Modify:**
- `internal/server/http/server.go`
- `internal/server/websocket/server.go`
- `internal/config/config.go`
- `cmd/cdev/cmd/start.go`

---

### PROD-002: Prometheus Metrics
**Priority:** P2 - Medium
**Effort:** 6 hours
**Status:** Not Started

**Description:**
Add Prometheus metrics endpoint for monitoring.

**Metrics to Add:**
- [ ] `cdev_events_published_total`
- [ ] `cdev_claude_runs_total`
- [ ] `cdev_claude_duration_seconds`
- [ ] `cdev_websocket_connections`
- [ ] `cdev_http_requests_total`
- [ ] `cdev_http_request_duration_seconds`

**Files to Create:**
- `internal/metrics/metrics.go`

**Dependencies:**
- `github.com/prometheus/client_golang`

---

### PROD-003: Structured Error Codes ✅
**Priority:** P2 - Medium
**Status:** ✅ Completed (January 2026)

**Description:**
Standardize error responses with machine-readable codes.

**Acceptance Criteria:**
- [x] Error code enum
- [x] Consistent error response format
- [x] Error codes documented in API reference
- [x] Client-friendly error messages

**Implementation:**
- `internal/domain/errors.go` (105 lines)
- Error constants: `ErrCodeClaudeAlreadyRunning`, `ErrCodeClaudeNotRunning`, `ErrCodeInvalidCommand`, `ErrCodeInvalidPayload`, `ErrCodePathOutsideRepo`, `ErrCodeFileNotFound`, `ErrCodeFileTooLarge`, `ErrCodeGitError`, `ErrCodeInternalError`
- Custom error types: `ClaudeError`, `GitError`, `ValidationError` with `Error()` and `Unwrap()` methods

---

### PROD-004: Graceful Shutdown Improvements
**Priority:** P2 - Medium
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Improve shutdown handling for clean container/service stops.

**Acceptance Criteria:**
- [ ] Drain WebSocket connections
- [ ] Complete in-flight requests
- [ ] Configurable shutdown timeout
- [ ] Health check returns unhealthy during shutdown

**Files to Modify:**
- `internal/app/app.go`
- `internal/server/http/server.go`
- `internal/server/websocket/server.go`

---

### PROD-005: systemd/launchd Service Files
**Priority:** P3 - Low
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Provide service files for production deployment.

**Deliverables:**
- [ ] systemd unit file (Linux)
- [ ] launchd plist (macOS)
- [ ] Windows service wrapper docs
- [ ] Installation guide

**Files to Create:**
- `deploy/systemd/cdev.service`
- `deploy/launchd/com.cdev.plist`
- `docs/DEPLOYMENT.md`

---

### PROD-006: Docker Container
**Priority:** P3 - Low
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Create Docker container for containerized deployment.

**Acceptance Criteria:**
- [ ] Multi-stage build
- [ ] Minimal image size
- [ ] Non-root user
- [ ] Health check
- [ ] docker-compose example

**Files to Create:**
- `Dockerfile`
- `docker-compose.yml`
- `.dockerignore`

---

## Phase 5: Future Enhancements (Backlog)

### FUTURE-001: Cloud Relay Integration
**Priority:** P3 - Low
**Effort:** 40 hours
**Status:** Backlog

**Description:**
Connect to cloud relay server for remote access outside LAN.

---

### FUTURE-002: Multi-Session Support
**Priority:** P3 - Low
**Effort:** 24 hours
**Status:** Backlog

**Description:**
Support multiple concurrent Claude sessions per agent.

---

### FUTURE-003: iOS App Development
**Priority:** P3 - Low
**Effort:** 80+ hours
**Status:** Backlog

**Description:**
Native iOS app for mobile monitoring and control.

---

### FUTURE-004: Real-Time Collaboration
**Priority:** P4 - Low
**Effort:** 40 hours
**Status:** Backlog

**Description:**
Multiple users viewing/controlling same session.

---

### FUTURE-005: Plugin System
**Priority:** P4 - Low
**Effort:** 24 hours
**Status:** Backlog

**Description:**
Extensible plugin architecture for custom integrations.

---

## Tracking

### Completed Items
| Item | Description | Completed |
|------|-------------|-----------|
| CORE-001 | Claude CLI Management | ✅ Dec 2025 |
| CORE-002 | Session Cache | ✅ Dec 2025 |
| CORE-003 | Message Cache with Pagination | ✅ Dec 2025 |
| CORE-004 | File Watcher (Rename/Delete) | ✅ Dec 2025 |
| CORE-005 | Repository Indexer | ✅ Dec 2025 |
| SEC-001 | Restrict CORS | ✅ Jan 2026 |
| SEC-002 | Token Authentication | ✅ Jan 2026 |
| SEC-003 | Replace `cat` with `os.ReadFile` | ✅ Jan 2026 |
| SEC-004 | Path Validation | ✅ Jan 2026 |
| SEC-005 | Rate Limiting | ✅ Jan 2026 |
| SEC-006 | Log Rotation | ✅ Jan 2026 |
| TEST-001 | Path Validation Tests | ✅ Jan 2026 |
| TEST-002 | Event Hub Tests | ✅ Jan 2026 |
| TEST-003 | Claude Manager Tests | ✅ Jan 2026 |
| TEST-004 | HTTP Handler Tests | ✅ Jan 2026 |
| TEST-005 | WebSocket Tests | ✅ Jan 2026 |
| TEST-007 | CI/CD Pipeline | ✅ Jan 2026 |
| PROD-003 | Structured Error Codes | ✅ Jan 2026 |
| PERF-003 | pprof Endpoints | ✅ Jan 2026 |

### MVP Sprint 1: Security ✅ COMPLETE
| Item | Description | Effort | Status |
|------|-------------|--------|--------|
| SEC-001 | Restrict CORS | 2h | ✅ Completed |
| SEC-002 | Token Authentication | 8h | ✅ Completed |
| SEC-003 | Replace `cat` with `os.ReadFile` | 1h | ✅ Completed |
| SEC-004 | Path Validation | 4h | ✅ Completed |

### MVP Sprint 2: Stability ✅ COMPLETE
| Item | Description | Effort | Status |
|------|-------------|--------|--------|
| SEC-005 | Rate Limiting | 4h | ✅ Completed |
| SEC-006 | Log Rotation | 2h | ✅ Completed |
| PROD-003 | Structured Error Codes | 4h | ✅ Completed |

### MVP Sprint 3: Testing & CI ✅ COMPLETE
| Item | Description | Effort | Status |
|------|-------------|--------|--------|
| TEST-001 | Path Validation Tests | 4h | ✅ Completed |
| TEST-002 | Event Hub Tests | 4h | ✅ Completed |
| TEST-007 | CI/CD Pipeline | 4h | ✅ Completed |

---

## MVP Definition

**Minimum Viable Product includes:**
- ✅ Core Features (Phase 0) - Completed
- ✅ Security Hardening (SEC-001 to SEC-006) - **All Completed**
- ✅ Basic Testing (TEST-001 to TEST-005, TEST-007) - **All Completed**
- ✅ CI/CD Pipeline - **Operational**
- ✅ Structured Error Codes (PROD-003) - **Completed**

**MVP Status: ✅ COMPLETE**

**Remaining Post-MVP Work:**
- TEST-006: Integration Test Framework (deferred)
- PERF-001 to PERF-004: Performance optimization
- PROD-001, PROD-002, PROD-004 to PROD-006: Production features
- Phase 5: Future enhancements

---

*Document Version: 2.0.0*
*Last Updated: January 2026*
