# cdev Product Backlog

**Last Updated:** December 2025
**Owner:** Technical Lead

---

## Backlog Overview

| Phase | Theme | Items | Status |
|-------|-------|-------|--------|
| 0 | Core Features | 5 | ✅ Completed |
| 1 | Security Hardening | 6 | Not Started |
| 2 | Testing Foundation | 7 | Not Started |
| 3 | Performance Optimization | 4 | Partial |
| 4 | Production Features | 6 | Not Started |
| 5 | Future Enhancements | 5 | Backlog |

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
- Session continuity: `new`, `continue`, `resume` modes
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

## Phase 1: Security Hardening

**Goal:** Address critical security vulnerabilities before any external deployment.
**Estimated Effort:** 1-2 weeks

### SEC-001: Restrict CORS Configuration
**Priority:** P0 - Critical
**Effort:** 2 hours
**Status:** Not Started

**Description:**
Replace wildcard CORS (`Access-Control-Allow-Origin: *`) with configurable allowed origins.

**Acceptance Criteria:**
- [ ] CORS restricted to configurable origins list
- [ ] Default allows only `localhost` and `127.0.0.1`
- [ ] WebSocket origin check implemented
- [ ] Configuration in `config.yaml`

**Files to Modify:**
- `internal/server/http/server.go`
- `internal/server/websocket/server.go`
- `internal/config/config.go`

---

### SEC-002: Implement Token Authentication
**Priority:** P0 - Critical
**Effort:** 8 hours
**Status:** Not Started

**Description:**
Add token-based authentication for all API endpoints and WebSocket connections.

**Acceptance Criteria:**
- [ ] Random token generated on startup
- [ ] Token displayed in terminal and QR code
- [ ] HTTP endpoints require `X-Auth-Token` header
- [ ] WebSocket requires token in query param or first message
- [ ] Token configurable or auto-generated

**Files to Modify:**
- `internal/server/http/middleware.go` (new)
- `internal/server/http/server.go`
- `internal/server/websocket/server.go`
- `internal/pairing/qrcode.go`
- `internal/config/config.go`

---

### SEC-003: Replace `cat` with `os.ReadFile`
**Priority:** P0 - Critical
**Effort:** 1 hour
**Status:** Not Started

**Description:**
Use native Go file reading instead of shelling out to `cat` command.

**Acceptance Criteria:**
- [ ] `GetFileContent` uses `os.ReadFile`
- [ ] Works on Windows, macOS, Linux
- [ ] Error handling improved
- [ ] Tests added for file reading

**Files to Modify:**
- `internal/adapters/git/tracker.go`

---

### SEC-004: Improve Path Validation
**Priority:** P1 - High
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Replace fragile string prefix matching with robust path validation.

**Acceptance Criteria:**
- [ ] Use `filepath.Rel` for validation
- [ ] Check for `..` prefix in relative path
- [ ] Handle symlinks with `filepath.EvalSymlinks`
- [ ] Case-insensitive comparison on Windows
- [ ] Comprehensive test coverage

**Files to Modify:**
- `internal/adapters/git/tracker.go`
- `internal/adapters/git/tracker_test.go` (new)

---

### SEC-005: Add Rate Limiting
**Priority:** P1 - High
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Implement rate limiting to prevent DoS attacks.

**Acceptance Criteria:**
- [ ] HTTP endpoint rate limiting (100 req/min default)
- [ ] WebSocket message rate limiting
- [ ] Claude start rate limiting (5/min)
- [ ] Configurable limits in config.yaml

**Files to Modify:**
- `internal/server/http/middleware.go`
- `internal/server/websocket/client.go`
- `internal/config/config.go`

**Dependencies:**
- `golang.org/x/time/rate`

---

### SEC-006: Implement Log Rotation
**Priority:** P1 - High
**Effort:** 2 hours
**Status:** Not Started

**Description:**
Add log rotation to prevent disk exhaustion from Claude output logs.

**Acceptance Criteria:**
- [ ] Max log file size: 50MB
- [ ] Keep last 3 rotated files
- [ ] Compress old logs
- [ ] Auto-cleanup after 7 days
- [ ] Configurable in config.yaml

**Files to Modify:**
- `internal/adapters/claude/manager.go`
- `internal/config/config.go`

**Dependencies:**
- `gopkg.in/natefinch/lumberjack.v2`

---

## Phase 2: Testing Foundation

**Goal:** Establish comprehensive test coverage and CI/CD pipeline.
**Estimated Effort:** 2-3 weeks

### TEST-001: Path Validation Tests
**Priority:** P0 - Critical
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Add thorough tests for path validation security.

**Test Cases:**
- [ ] Valid relative paths
- [ ] Path traversal attempts (`../`, `..\\`)
- [ ] Absolute paths
- [ ] Symlink following
- [ ] Unicode paths
- [ ] Edge cases (empty path, very long paths)

**Files to Create:**
- `internal/adapters/git/tracker_test.go`

---

### TEST-002: Event Hub Tests
**Priority:** P0 - Critical
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Test hub pub-sub functionality and concurrent access.

**Test Cases:**
- [ ] Subscribe/unsubscribe
- [ ] Event broadcasting
- [ ] Concurrent publishers
- [ ] Slow subscriber handling
- [ ] Buffer overflow behavior
- [ ] Graceful shutdown

**Files to Create:**
- `internal/hub/hub_test.go`

---

### TEST-003: Claude Manager Tests
**Priority:** P1 - High
**Effort:** 8 hours
**Status:** Not Started

**Description:**
Test Claude process management state machine.

**Test Cases:**
- [ ] Start process
- [ ] Stop process (graceful)
- [ ] Kill process (force)
- [ ] State transitions
- [ ] Output stream parsing
- [ ] Permission detection
- [ ] Session ID capture
- [ ] Timeout handling

**Files to Create:**
- `internal/adapters/claude/manager_test.go`
- `test/fixtures/claude_output/` (sample outputs)

---

### TEST-004: HTTP Handler Tests
**Priority:** P1 - High
**Effort:** 6 hours
**Status:** Not Started

**Description:**
Test HTTP API request handling and responses.

**Test Cases:**
- [ ] Health endpoint
- [ ] Status endpoint
- [ ] Claude run/stop
- [ ] File content retrieval
- [ ] Git status/diff
- [ ] Error responses
- [ ] Input validation

**Files to Create:**
- `internal/server/http/handlers_test.go`

---

### TEST-005: WebSocket Tests
**Priority:** P1 - High
**Effort:** 6 hours
**Status:** Not Started

**Description:**
Test WebSocket connection handling and message routing.

**Test Cases:**
- [ ] Connection establish
- [ ] Message sending/receiving
- [ ] Command routing
- [ ] Event broadcasting
- [ ] Ping/pong health
- [ ] Connection cleanup

**Files to Create:**
- `internal/server/websocket/client_test.go`
- `internal/server/websocket/hub_test.go`

---

### TEST-006: Integration Test Framework
**Priority:** P2 - Medium
**Effort:** 8 hours
**Status:** Not Started

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

### TEST-007: CI/CD Pipeline
**Priority:** P2 - Medium
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Set up GitHub Actions for automated testing and builds.

**Acceptance Criteria:**
- [ ] Run tests on PR
- [ ] Run linting (golangci-lint)
- [ ] Build for all platforms
- [ ] Coverage report
- [ ] Release automation

**Files to Create:**
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`

---

## Phase 3: Performance Optimization

**Goal:** Optimize for high-load scenarios and reduce resource usage.
**Estimated Effort:** 1-2 weeks

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
**Status:** Not Started

**Description:**
Add timeout to event dispatch to prevent blocking on slow subscribers.

**Acceptance Criteria:**
- [ ] 5-second timeout on Send()
- [ ] Slow subscribers auto-unregistered
- [ ] Warning logged for slow sends
- [ ] Metrics for dispatch latency

**Files to Modify:**
- `internal/hub/hub.go`

---

### PERF-003: Add pprof Endpoints
**Priority:** P2 - Medium
**Effort:** 2 hours
**Status:** Not Started

**Description:**
Add profiling endpoints for performance debugging.

**Acceptance Criteria:**
- [ ] `/debug/pprof/` endpoints
- [ ] Configurable enable/disable
- [ ] Requires authentication

**Files to Modify:**
- `internal/server/http/server.go`
- `internal/config/config.go`

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
**Estimated Effort:** 2-4 weeks

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

### PROD-003: Structured Error Codes
**Priority:** P2 - Medium
**Effort:** 4 hours
**Status:** Not Started

**Description:**
Standardize error responses with machine-readable codes.

**Acceptance Criteria:**
- [ ] Error code enum
- [ ] Consistent error response format
- [ ] Error codes documented in API reference
- [ ] Client-friendly error messages

**Error Codes:**
```
CDEV_ERR_AUTH_REQUIRED
CDEV_ERR_INVALID_TOKEN
CDEV_ERR_RATE_LIMITED
CDEV_ERR_CLAUDE_RUNNING
CDEV_ERR_CLAUDE_NOT_RUNNING
CDEV_ERR_INVALID_PATH
CDEV_ERR_FILE_NOT_FOUND
CDEV_ERR_FILE_TOO_LARGE
CDEV_ERR_GIT_ERROR
CDEV_ERR_INTERNAL
```

**Files to Create:**
- `internal/domain/errors/codes.go`

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

### MVP Sprint 1: Security (Priority)
| Item | Description | Effort | Status |
|------|-------------|--------|--------|
| SEC-001 | Restrict CORS | 2h | Not Started |
| SEC-002 | Token Authentication | 8h | Not Started |
| SEC-003 | Replace `cat` with `os.ReadFile` | 1h | Not Started |
| SEC-004 | Path Validation | 4h | Not Started |

### MVP Sprint 2: Stability
| Item | Description | Effort | Status |
|------|-------------|--------|--------|
| SEC-005 | Rate Limiting | 4h | Not Started |
| SEC-006 | Log Rotation | 2h | Not Started |
| PROD-003 | Structured Error Codes | 4h | Not Started |

### MVP Sprint 3: Testing & CI
| Item | Description | Effort | Status |
|------|-------------|--------|--------|
| TEST-001 | Path Validation Tests | 4h | Not Started |
| TEST-002 | Event Hub Tests | 4h | Not Started |
| TEST-007 | CI/CD Pipeline | 4h | Not Started |

---

## MVP Definition

**Minimum Viable Product includes:**
- ✅ Core Features (Phase 0) - Completed
- Security Hardening (SEC-001 to SEC-004) - Required
- Basic Testing (TEST-001, TEST-002) - Required
- CI/CD Pipeline - Recommended

**Total Remaining MVP Effort:** ~35-40 hours

---

*Document Version: 1.1.0*
*Last Updated: December 2025*
