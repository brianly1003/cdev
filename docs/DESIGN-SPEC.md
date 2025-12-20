# Mobile AI Coding Monitor & Controller — Original Design Specification

> **Document Type:** Original Design Specification (MVP)
> **Status:** Living document with implementation status tracking
> **Last Updated:** December 2025

This document captures the original vision and technical specification for the cdev project. Implementation status is marked throughout:
- ✅ **Implemented** — Feature is fully built and working
- 🔄 **Partial** — Feature is partially implemented or differs from spec
- ❌ **Not Yet** — Feature is planned but not implemented
- 🔀 **Modified** — Implementation differs significantly from original spec

---

## 1. Purpose

Build a system that allows a developer to:

- Run **Claude Code (CLI)** on their laptop ✅
- Leave the desk
- Monitor AI-generated code changes, diffs, and logs from an iOS app ❌ (Agent ready, iOS app separate project)
- Optionally send new AI prompts from iOS to Claude Code ✅ (API ready)
- Track all changes via Git, independent of VS Code ✅

VS Code is optional and acts only as a viewer/editor of the same repository.

---

## 2. Core Principle

**The Agent is the source of truth.**
VS Code is NOT required for control or observation. ✅

The system must NOT:

- Scrape VS Code UI ✅
- Read VS Code terminal buffers ✅
- Depend on VS Code APIs for execution ✅

---

## 3. System Components

### 3.1 Components Overview

```
iOS App <-> Cloud Relay (WebSocket) <-> Laptop Agent (Go)
                                              |
                                              |-- Claude Code CLI
                                              |-- Git Repository
                                              |-- File System Watcher
```

| Component | Status | Notes |
|-----------|--------|-------|
| Laptop Agent (Go) | ✅ Implemented | Full functionality |
| Cloud Relay | ❌ Not Yet | Direct WebSocket connection works for LAN |
| iOS App | ❌ Not Yet | Separate project |

---

## 4. Laptop Agent (Go) ✅

### 4.1 Responsibilities

The Agent MUST:

| Responsibility | Status |
|----------------|--------|
| Spawn and manage Claude Code CLI | ✅ |
| Capture Claude stdout/stderr | ✅ |
| Watch repository file changes | ✅ |
| Generate Git diffs | ✅ |
| Stream events in real time | ✅ |
| Accept AI prompts from iOS | ✅ |
| Work without VS Code running | ✅ |

---

### 4.2 Claude Code Integration ✅

**Original Spec:**
```bash
claude code "<prompt>"
```

**Actual Implementation:** 🔀 Enhanced
```bash
claude -p --verbose --output-format stream-json --input-format stream-json
```

| Feature | Status | Implementation Notes |
|---------|--------|---------------------|
| Capture stdout/stderr | ✅ | Bidirectional streaming with 64KB buffer |
| Stream logs line-by-line | ✅ | Real-time JSON event parsing |
| Support start/stop (SIGTERM) | ✅ | Cross-platform: Unix signals, Windows taskkill |
| **Additional Features** | | |
| Permission request handling | ✅ | Detects tool permission requests |
| Interactive prompt handling | ✅ | AskUserQuestion tool support |
| Session management | ✅ | new/continue/resume modes |
| Session ID tracking | ✅ | Captures from stream-json output |
| Session listing | ✅ | Reads ~/.claude/projects history |
| File logging | ✅ | Logs to .cdev/logs/claude_<pid>.jsonl |

### 4.3 File System Monitoring ✅

- Watch repository root recursively ✅
- Detect:
  - file created ✅
  - file modified ✅
  - file deleted ✅
- Ignore (configurable): ✅
  - .git/
  - node_modules/
  - .venv/
  - build artifacts
  - __pycache__/
  - .DS_Store

**Implementation:** fsnotify with 100ms debouncing

### 4.4 Git Tracking ✅

| Feature | Status |
|---------|--------|
| git status --porcelain | ✅ |
| git diff | ✅ |
| git diff --cached | ✅ |
| Unified diffs per file | ✅ |
| Auto-diff on file change | ✅ (configurable) |

Git is the authoritative change tracker. ✅

### 4.5 Agent -> iOS Events ✅

All events are JSON with consistent structure:

```json
{
  "type": "event_type",
  "payload": {},
  "timestamp": "ISO8601"
}
```

| Event | Status | Notes |
|-------|--------|-------|
| claude_log | ✅ | Line-by-line streaming |
| claude_status | ✅ | running/idle/error/stopped |
| file_changed | ✅ | path + change type |
| git_diff | ✅ | file + unified diff |
| session_start | ✅ | |
| session_end | ✅ | |
| **Additional Events** | | |
| claude_waiting | ✅ | Interactive prompt detection |
| claude_permission | ✅ | Permission request with tool info |
| claude_session_info | ✅ | Session metadata |
| file_content | ✅ | File content response |
| status_response | ✅ | Agent status response |
| error | ✅ | Error responses |

---

## 5. iOS -> Agent Commands ✅

| Command | Status | Notes |
|---------|--------|-------|
| run_claude | ✅ | Supports new/continue/resume modes |
| stop_claude | ✅ | Graceful + force stop |
| get_status | ✅ | Returns agent state |
| get_file | ✅ | With path validation |
| **Additional Commands** | | |
| respond_to_claude | ✅ | Answer prompts/permissions |

---

## 6. Cloud Relay (MVP) ❌

**Status:** Not implemented

**Original Spec:**
- WebSocket relay only
- No persistence required
- Routes messages by sessionId
- Agent initiates connection
- iOS subscribes to session

**Current Implementation:**
- Direct WebSocket connection on LAN
- No cloud relay needed for local development
- QR code pairing for easy connection

---

## 7. iOS App (MVP Scope) ❌

**Status:** Separate project (not in this repository)

Required Screens:
- Session Dashboard
- Claude Log Viewer
- Diff Viewer
- Prompt Input

The Agent provides all APIs needed for iOS app implementation.

---

## 8. Out of Scope (MVP) ✅

All items remain out of scope as originally planned:
- Editing files from iOS
- Reading VS Code terminal UI
- Remote desktop
- Cursor-level sync
- Multi-user collaboration
- Cloud code execution

---

## 9. Security Requirements 🔄

| Requirement | Status | Notes |
|-------------|--------|-------|
| Agent uses outbound connections only | 🔄 | Currently accepts inbound for local dev |
| No inbound ports on laptop | ❌ | HTTP/WS servers bind to localhost |
| TLS everywhere | ❌ | HTTP only (add for production) |
| Session tokens expire | ❌ | Config exists, not enforced |
| Diffs only (not full source) | ✅ | Configurable file size limits |
| **Implemented Security** | | |
| Path traversal protection | ✅ | Repo root jail |
| File size limits | ✅ | Configurable max size |

---

## 10. Performance Expectations ✅

| Metric | Target | Status |
|--------|--------|--------|
| Diff latency | < 1 second | ✅ |
| Log streaming | line-by-line | ✅ |
| Agent idle CPU | minimal | ✅ |
| Agent memory | < 100MB | ✅ |

---

## 11. Success Criteria

| Criterion | Status |
|-----------|--------|
| Claude runs headlessly via Agent | ✅ |
| iOS receives Claude logs and diffs | 🔄 (API ready) |
| Git accurately reflects all changes | ✅ |
| VS Code can open the repo and show the same changes | ✅ |
| User can supervise AI coding from iOS | 🔄 (Agent ready) |

---

## 12. Architectural Rule ✅

**Claude CLI + Git + Agent is the control plane.**

VS Code is optional and passive.

---

## 13. File Request Scenarios ✅

### iOS -> Agent: File Request Command

```json
{
  "command": "get_file",
  "payload": {
    "path": "src/auth.ts"
  }
}
```

### Agent -> iOS: File Content Response

```json
{
  "type": "file_content",
  "payload": {
    "path": "src/auth.ts",
    "content": "export function validateJWT(token) { ... }",
    "encoding": "utf-8",
    "truncated": false
  }
}
```

### Mandatory Safety Rules ✅

#### 1. Repo Root Jail ✅

The Agent:
- Resolves absolute path ✅
- Verifies it starts with repo root ✅
- Rejects path traversal attempts (../) ✅

#### 2. File Size Limit ✅

- Max file size: 200 KB (configurable)
- Returns truncated flag when exceeded

---

## 14. Implementation Additions (Beyond Original Spec)

Features implemented that were not in the original specification:

### HTTP REST API
- Full RESTful API alongside WebSocket
- OpenAPI 3.0 documentation with Swagger UI
- Endpoints: `/health`, `/api/status`, `/api/claude/*`, `/api/git/*`, `/api/file`

### QR Code Pairing
- Terminal QR code display for easy mobile connection
- Encodes WebSocket URL, HTTP URL, session ID, repo name

### Advanced Claude Integration
- Permission request detection and approval/denial
- Interactive prompt (AskUserQuestion) handling
- Session continuity across restarts
- Session history browsing

### Configuration System
- YAML configuration files
- Environment variable overrides (CDEV_ prefix)
- Sensible defaults for all settings

---

## 15. What's Next

### High Priority
1. **Unit Tests** — No test coverage currently
2. **TLS/HTTPS** — Required for production security
3. **Authentication** — Token-based auth for API endpoints

### Medium Priority
4. **Cloud Relay** — For remote access outside LAN
5. **iOS App** — Mobile client implementation

### Low Priority
6. **Rate Limiting** — API protection
7. **Metrics/Observability** — Prometheus endpoints
