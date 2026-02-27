# cdev Protocol Specification

**Version:** 2.2.0
**Status:** Implemented
**Last Updated:** January 30, 2026

---

## Overview

The cdev protocol defines the communication standard between cdev (server) and clients (cdev-ios, VS Code extensions, etc.) for remote supervision and control of AI coding assistant sessions.

> **Note:** This document tracks the current JSON-RPC protocol. For complete method-by-method examples and payloads, see `docs/api/UNIFIED-API-SPEC.md`.

### Protocol Evolution

| Version | Description |
|---------|-------------|
| 2.0 | JSON-RPC 2.0 with agent-agnostic naming (current) |

### Design Principles

1. **JSON-RPC 2.0 Standard** - Industry-standard message format for IDE integration
2. **Agent-Agnostic** - Methods use `agent/*` prefix to support Claude, Gemini, Codex, etc.
3. **Single Protocol** - WebSocket endpoint accepts JSON-RPC 2.0 messages
4. **OpenRPC Discovery** - Auto-generated API specification at `/api/rpc/discover`
5. **Mobile-Optimized** - Handles network transitions, background states, reconnection
6. **Capability Negotiation** - LSP-style initialize/initialized handshake
7. **Runtime Capability Registry** - Server-driven runtime behavior contract via `initialize.capabilities.runtimeRegistry`

### Transport Layers

| Layer | Port | Purpose |
|-------|------|---------|
| HTTP | 8766 | REST API, health checks, OpenRPC discovery |
| WebSocket | 8766 | Real-time events via `/ws` endpoint (JSON-RPC 2.0) |

**Note:** Port consolidation complete - single port 8766 serves all traffic.

### Authentication

When `security.require_auth = true` (default), HTTP and WebSocket endpoints require bearer auth except for pairing and auth bootstrap endpoints below.

```
Authorization: Bearer <access-token>
```

**Unauthenticated allowlist** (pairing + token bootstrap):
- `/health`
- `/pair`
- `/api/pair/*`
- `/api/auth/exchange`
- `/api/auth/refresh`
- `/api/auth/revoke`
- `/api/auth/pairing/*` (local-only pairing approval endpoints)

**Token flow (summary):**
1. Pairing token is displayed in `/pair` or `/api/pair/info` (QR includes token).
2. Exchange pairing token via `POST /api/auth/exchange`.
3. If `security.require_pairing_approval = false` (default), exchange returns access + refresh tokens immediately (`200`).
4. If `security.require_pairing_approval = true`, first exchange returns `202` with `{"status":"pending_approval","request_id":"..."}`.
5. Local operator approves/rejects via:
   - `GET /api/auth/pairing/pending`
   - `POST /api/auth/pairing/approve`
   - `POST /api/auth/pairing/reject`
6. Client retries `POST /api/auth/exchange` after approval, then uses returned access token for HTTP + WebSocket requests.
7. Refresh via `POST /api/auth/refresh`.
8. Revoke a refresh token via `POST /api/auth/revoke`.

**Note:** Query‑string tokens are not supported.

---

## Table of Contents

1. [Message Format](#message-format)
2. [WebSocket Events (Server → Client)](#websocket-events-server--client)
3. [WebSocket Commands (Client → Server)](#websocket-commands-client--server)
4. [HTTP API](#http-api)
5. [Connection Lifecycle](#connection-lifecycle)
6. [Error Handling](#error-handling)
7. [Protocol Gaps & TODOs](#protocol-gaps--todos)
8. [Version History](#version-history)

---

## Message Format

### JSON-RPC 2.0 Format (Recommended)

The preferred format for new clients. Follows the JSON-RPC 2.0 specification.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "agent/run",
  "params": {
    "prompt": "Fix the bug in app.js"
  }
}
```

**Response (Success):**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "status": "started",
    "session_id": "sess_abc123"
  }
}
```

**Response (Error):**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32001,
    "message": "Agent already running"
  }
}
```

**Notification (Server → Client):**
```json
{
  "jsonrpc": "2.0",
  "method": "event/agent_status",
  "params": {
    "state": "running",
    "agent_type": "claude"
  }
}
```

### JSON-RPC 2.0 Methods

| Method | Description |
|--------|-------------|
| `initialize` | Capability negotiation (LSP-style) |
| `initialized` | Confirm initialization complete |
| `shutdown` | Graceful shutdown |
| `agent/run` | Start AI agent with prompt |
| `agent/stop` | Stop running agent |
| `agent/respond` | Respond to tool use request |
| `status/get` | Get server status |
| `file/get` | Get file content |
| `git/status` | Get git status |
| `git/diff` | Get file diff |
| `git/stage` | Stage files |
| `git/unstage` | Unstage files |
| `git/discard` | Discard changes |
| `git/commit` | Create commit |
| `git/push` | Push to remote |
| `git/pull` | Pull from remote |
| `git/branches` | List branches |
| `git/checkout` | Checkout branch |
| `session/list` | List sessions |
| `session/get` | Get session details |
| `session/messages` | Get session messages |
| `session/watch` | Watch session for updates |
| `session/unwatch` | Stop watching |
| `repository/index/status` | Get repository index status |
| `repository/search` | Search files in repository |
| `repository/files/list` | List files in directory |
| `repository/files/tree` | Get directory tree |
| `repository/stats` | Get repository statistics |
| `repository/index/rebuild` | Trigger index rebuild |

Runtime registry contract reference: `docs/api/RUNTIME-CAPABILITY-REGISTRY.md`

---

### Base Notification Structure (JSON-RPC)

All server-to-client events are JSON-RPC notifications:

```json
{
  "jsonrpc": "2.0",
  "method": "event/<event_type>",
  "params": { ... }
}
```

### Base Request Structure (JSON-RPC)

All client-to-server commands are JSON-RPC requests:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "<method_name>",
  "params": { ... }
}
```

---

## WebSocket Events (Server → Client)

### Claude Events

#### `claude_log`

Raw output from Claude CLI with optional parsed content.

```json
{
  "event": "claude_log",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "line": "{\"type\":\"assistant\",\"message\":{...}}",
    "stream": "stdout",
    "parsed": {
      "type": "assistant",
      "session_id": "abc123",
      "content": [
        {"type": "text", "text": "I'll help you..."},
        {"type": "tool_use", "tool_name": "Read", "tool_id": "xyz", "tool_input": "{...}"}
      ],
      "stop_reason": "tool_use",
      "output_tokens": 150,
      "is_thinking": false,
      "is_context_compaction": false
    }
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `line` | string | Raw JSON line from Claude CLI |
| `stream` | `stdout` \| `stderr` | Output stream |
| `parsed` | object \| null | Parsed content (only for valid JSON stdout) |
| `parsed.type` | string | `assistant`, `user`, `result`, `system`, `init` |
| `parsed.session_id` | string | Claude session ID |
| `parsed.content` | array | Content blocks |
| `parsed.stop_reason` | string | `end_turn`, `tool_use`, or empty |
| `parsed.output_tokens` | number | Tokens generated so far |
| `parsed.is_thinking` | boolean | Claude is in thinking mode |
| `parsed.is_context_compaction` | boolean | Auto-generated context summary |

#### `claude_message`

**STATUS: DEPRECATED** - Redundant with `claude_log.parsed`. Will be removed in v2.0.

Structured message for UI rendering. Currently emitted from two sources causing duplicates.

```json
{
  "event": "claude_message",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "session_id": "abc123",
    "type": "assistant",
    "role": "assistant",
    "content": [
      {"type": "text", "text": "I'll create that file for you."},
      {"type": "tool_use", "tool_name": "Write", "tool_id": "xyz", "tool_input": {...}}
    ],
    "stop_reason": "tool_use",
    "is_context_compaction": false
  }
}
```

**TODO: Remove from manager.go, keep only in streamer.go for historical playback.**

#### `claude_status`

Claude CLI process state changes.

```json
{
  "event": "claude_status",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "state": "running",
    "prompt": "Fix the bug in app.js",
    "pid": 12345,
    "started_at": "2025-12-20T15:29:55.000Z",
    "error": null,
    "exit_code": null
  }
}
```

| State | Description |
|-------|-------------|
| `idle` | No Claude process running |
| `running` | Claude is actively processing |
| `waiting` | Waiting for user input (permission/prompt) |
| `stopped` | Process terminated normally |
| `error` | Process terminated with error |

#### `claude_permission`

Claude is requesting permission for a tool operation.

```json
{
  "event": "claude_permission",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "tool_use_id": "toolu_abc123",
    "tool_name": "Write",
    "input": "{\"file_path\":\"/src/app.js\",\"content\":\"...\"}",
    "description": "Write to file: /src/app.js"
  }
}
```

**Client Response Required:** Send `agent/respond` JSON-RPC request with approval or denial.

#### `claude_waiting`

Claude is waiting for interactive user input (AskUserQuestion tool).

```json
{
  "event": "claude_waiting",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "tool_use_id": "toolu_xyz789",
    "tool_name": "AskUserQuestion",
    "input": "{\"question\":\"Which database should I use?\",\"options\":[...]}"
  }
}
```

#### `claude_session_info`

Session information captured when Claude starts.

```json
{
  "event": "claude_session_info",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "session_id": "abc123-def456",
    "model": "claude-sonnet-4-20250514",
    "version": "1.0.0"
  }
}
```

### File Events

#### `file_changed`

File system change detected by watcher.

```json
{
  "event": "file_changed",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "path": "src/app.js",
    "change": "modified",
    "size": 1234,
    "old_path": null
  }
}
```

| Change Type | Description |
|-------------|-------------|
| `created` | New file created |
| `modified` | File content changed |
| `deleted` | File removed |
| `renamed` | File renamed (includes `old_path`) |

#### `file_content`

Response to `file/get` request.

```json
{
  "event": "file_content",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "request_id": "req-123",
  "payload": {
    "path": "src/app.js",
    "content": "const app = ...",
    "encoding": "utf-8",
    "truncated": false,
    "size": 1234,
    "error": null
  }
}
```

### Git Events

#### `git_status_changed`

Git repository status changed. This event is emitted in two scenarios:

1. **Real-time git state watcher** (automatic) - Detects changes from external tools (terminal, IDE, SourceTree)
2. **After git operations** - Emitted after `git/stage`, `git/commit`, `git/push`, etc.

**Real-Time Git State Watcher:**

The server monitors the `.git` directory for state changes, enabling real-time updates when:
- Files are staged/unstaged (`git add`, `git reset`)
- Commits are created (`git commit`)
- Branches are switched (`git checkout`, `git switch`)
- Remote changes are fetched/pulled (`git fetch`, `git pull`)
- Merges/rebases occur (`git merge`, `git rebase`)

**Key Features:**
- **IDE-safe**: Only watches `.git` directory, not working directory (won't conflict with VS Code, IntelliJ, SourceTree)
- **Debounced**: Uses 500ms debounce + 1 second minimum interval to prevent event spam
- **Startup delay**: 2 second delay on startup to avoid initial event burst

```json
{
  "event": "git_status_changed",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "branch": "main",
    "ahead": 2,
    "behind": 0,
    "staged_count": 3,
    "unstaged_count": 1,
    "untracked_count": 2,
    "has_conflicts": false,
    "changed_files": ["src/app.js", "src/utils.js"]
  }
}
```

#### `git_diff`

Git diff content.

```json
{
  "event": "git_diff",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "path": "src/app.js",
    "diff": "--- a/src/app.js\n+++ b/src/app.js\n@@ -1,3 +1,4 @@...",
    "is_staged": false,
    "is_new": false
  }
}
```

#### `git_operation_completed`

Result of a git operation.

```json
{
  "event": "git_operation_completed",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "operation": "commit",
    "success": true,
    "message": "Committed 3 files",
    "error": null,
    "sha": "abc123def",
    "files_affected": 3
  }
}
```

| Operation | Additional Fields |
|-----------|-------------------|
| `commit` | `sha`, `files_affected` |
| `push` | `commits_pushed` |
| `pull` | `commits_pulled`, `conflicted_files` |
| `checkout` | `branch` |
| `stage`, `unstage`, `discard` | `files_affected` |

### Session Events

#### `session_start`

Agent session started.

```json
{
  "event": "session_start",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "session_id": "agent-session-uuid",
    "repo_path": "/Users/dev/myproject",
    "repo_name": "myproject",
    "agent_version": "1.0.0"
  }
}
```

#### `session_end`

Agent session ended.

```json
{
  "event": "session_end",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "session_id": "agent-session-uuid",
    "reason": "shutdown"
  }
}
```

#### `session_watch_started`

Started watching a Claude session for real-time updates.

```json
{
  "event": "session_watch_started",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "session_id": "claude-session-id",
    "watching": true
  }
}
```

#### `session_watch_stopped`

Stopped watching a Claude session.

```json
{
  "event": "session_watch_stopped",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "session_id": "claude-session-id",
    "watching": false,
    "reason": "client_request"
  }
}
```

### Connection Events

#### `heartbeat`

Periodic health check (every 30 seconds).

```json
{
  "event": "heartbeat",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "payload": {
    "server_time": "2025-12-20T15:30:00.000Z",
    "sequence": 42,
    "claude_status": "idle",
    "uptime_seconds": 3600
  }
}
```

#### `status_response`

Response to `status/get` request.

```json
{
  "event": "status_response",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "request_id": "req-456",
  "payload": {
    "claude_state": "idle",
    "connected_clients": 2,
    "repo_path": "/Users/dev/myproject",
    "repo_name": "myproject",
    "uptime_seconds": 3600,
    "agent_version": "1.0.0",
    "watcher_enabled": true,
    "git_enabled": true
  }
}
```

#### `error`

Error response.

```json
{
  "event": "error",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "request_id": "req-789",
  "payload": {
    "code": "CLAUDE_ALREADY_RUNNING",
    "message": "Claude is already running. Stop it first.",
    "request_id": "req-789",
    "details": {
      "current_state": "running"
    }
  }
}
```

---

## WebSocket Commands (Client → Server)

Commands are sent as JSON-RPC 2.0 requests.

### Core Methods

- `agent/run` - Start an agent with prompt/mode/session context
- `agent/stop` - Stop the running agent
- `agent/respond` - Respond to permission or interactive prompt
- `status/get` - Retrieve current server/agent status
- `file/get` - Fetch file content
- `session/watch` - Subscribe to real-time session updates
- `session/unwatch` - Unsubscribe from real-time updates

### Example Requests

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "agent/run",
  "params": {
    "prompt": "Fix the bug in app.js",
    "mode": "new",
    "agent_type": "claude"
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "agent/respond",
  "params": {
    "tool_use_id": "toolu_abc123",
    "response": "approved",
    "is_error": false
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/watch",
  "params": {
    "session_id": "claude-session-id",
    "agent_type": "claude"
  }
}
```

---

## HTTP API

### Health & Status

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api/status` | Agent status |

### Claude Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/claude/sessions` | List Claude sessions |
| GET | `/api/claude/sessions/messages` | Get session messages (paginated) |
| GET | `/api/claude/sessions/elements` | Get UI elements for rendering |
| POST | `/api/claude/run` | Start Claude |
| POST | `/api/claude/stop` | Stop Claude |
| POST | `/api/claude/respond` | Respond to permission/prompt |

### File Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/file` | Get file content |
| GET | `/api/files/list` | List files in directory |

### Git Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/git/status` | Get git status |
| GET | `/api/git/diff` | Get file diff |
| GET | `/api/git/branches` | List branches |
| POST | `/api/git/stage` | Stage files |
| POST | `/api/git/unstage` | Unstage files |
| POST | `/api/git/discard` | Discard changes |
| POST | `/api/git/commit` | Create commit |
| POST | `/api/git/push` | Push to remote |
| POST | `/api/git/pull` | Pull from remote |
| POST | `/api/git/checkout` | Checkout branch |

### Repository Indexer

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/repository/index/status` | Indexer status |
| POST | `/api/repository/index/rebuild` | Rebuild index |
| GET | `/api/repository/search` | Search files |
| GET | `/api/repository/files/list` | List indexed files |
| GET | `/api/repository/files/tree` | Get file tree |
| GET | `/api/repository/stats` | Repository statistics |

---

## Connection Lifecycle

### Initial Connection

```
┌─────────────┐                              ┌─────────────┐
│  cdev-ios   │                              │ cdev  │
└──────┬──────┘                              └──────┬──────┘
       │                                            │
       │  1. WebSocket connect                      │
       │───────────────────────────────────────────▶│
       │                                            │
       │  2. session_start event                    │
       │◀───────────────────────────────────────────│
       │                                            │
       │  3. heartbeat (every 30s)                  │
       │◀───────────────────────────────────────────│
       │                                            │
```

### Heartbeat Protocol

- Server sends `heartbeat` event every 30 seconds
- Client should consider connection dead if no activity for 45 seconds
- WebSocket ping/pong (81s interval) provides transport-level health check

### Reconnection Strategy

```
Attempt 1: Wait 1s
Attempt 2: Wait 2s
Attempt 3: Wait 4s
Attempt 4: Wait 8s
Attempt 5: Wait 16s
Attempt 6+: Wait 30s (max)

After 10 failed attempts: Enter "failed" state
In failed state: Retry every 60s
```

---

## Error Handling

### Error Codes

| Code | Description |
|------|-------------|
| `CLAUDE_ALREADY_RUNNING` | Claude process already active |
| `CLAUDE_NOT_RUNNING` | No Claude process to stop |
| `INVALID_COMMAND` | Unknown command type |
| `INVALID_PAYLOAD` | Malformed command payload |
| `FILE_NOT_FOUND` | Requested file doesn't exist |
| `FILE_TOO_LARGE` | File exceeds size limit |
| `PATH_TRAVERSAL` | Path escapes repository root |
| `GIT_ERROR` | Git operation failed |
| `SESSION_NOT_FOUND` | Session ID not found |
| `INTERNAL_ERROR` | Unexpected server error |

### Error Response Format

```json
{
  "event": "error",
  "timestamp": "2025-12-20T15:30:00.000Z",
  "request_id": "req-123",
  "payload": {
    "code": "CLAUDE_ALREADY_RUNNING",
    "message": "Human-readable error description",
    "request_id": "req-123",
    "details": { }
  }
}
```

---

## Protocol Gaps & TODOs

### Critical (MVP Blockers)

| Issue | Description | Priority | Effort |
|-------|-------------|----------|--------|
| **Duplicate Events** | `claude_message` emitted from both manager.go and streamer.go | P0 | 2-3 days |

### High Priority

| Issue | Description | Priority | Effort |
|-------|-------------|----------|--------|
| Missing EventType constants | `session_watch_started/stopped` not in types.go | P1 | 1 hour |
| No protocol versioning | No way to negotiate compatible versions | P1 | 2 days |
| Inconsistent error codes | Some errors return raw messages, not codes | P1 | 1 day |
| No rate limiting | Client can spam commands | P1 | 2 days |
| No message acknowledgment | No way to confirm message received | P1 | 3 days |

### Medium Priority

| Issue | Description | Priority | Effort |
|-------|-------------|----------|--------|
| `claude_message` redundant | Same data as `claude_log.parsed` | P2 | 1 day |
| No compression | Large messages not compressed | P2 | 1 day |
| No binary protocol | JSON-only, no efficient binary option | P2 | 1 week |
| Missing batch operations | No way to stage multiple files at once | P2 | 2 days |

### Future Enhancements

| Feature | Description | Priority |
|---------|-------------|----------|
| Protocol version negotiation | Client/server agree on version | P3 |
| Event filtering | Client subscribes to specific event types | P3 |
| Binary file support | Handle images, PDFs in file operations | P3 |
| Streaming file uploads | Upload large files in chunks | P3 |
| End-to-end encryption | Encrypt payloads for security | P3 |

---

## Immediate Action Items

### Week 1: Fix Critical Issues

```
[ ] Fix duplicate claude_message emission
    - Remove from manager.go lines 390-398
    - Keep only in streamer.go for historical playback
    - iOS should use claude_log.parsed for real-time

[ ] Add missing EventType constants
    - Add EventTypeSessionWatchStarted
    - Add EventTypeSessionWatchStopped

[ ] Standardize error codes
    - Define error code enum
    - Use codes consistently across all errors
```

### Week 2: Security & Versioning

```
[x] Require bearer auth for HTTP + WebSocket
    - Header: Authorization: Bearer <access-token>
    - Query-string tokens are not supported

[ ] Add protocol version header
    - Header: X-Cdev-Protocol-Version: 1.0.0
    - Reject incompatible versions

[x] Restrict CORS origins
    - Allow localhost only by default
    - Configurable allowlist
```

### Week 3: Polish

```
[ ] Add rate limiting
    - 100 commands/minute per client
    - 10 agent/run per minute per client

[ ] Add message acknowledgment (optional)
    - Server sends ack with message_id
    - Client can request retransmission
```

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 2.0.0 | 21 Dec 2025 | JSON-RPC 2.0 adoption, agent-agnostic naming, port consolidation |
| 1.0.0-draft | Dec 2025 | Initial draft specification |

### Migration to 2.0

Key changes from 1.0 to 2.0:

1. **JSON-RPC 2.0 Format**: Use `jsonrpc`, `id`, `method`, `params` instead of `command`, `request_id`, `payload`
2. **Agent-Agnostic Naming**: `claude/run` → `agent/run`, `claude/stop` → `agent/stop`
3. **Port Consolidation**: Single port 8766 instead of 8765 (WebSocket) + 8766 (HTTP)
4. **Unified Endpoint**: WebSocket at `/ws` instead of root
5. **OpenRPC Discovery**: Auto-generated spec at `/api/rpc/discover`
6. **Capability Negotiation**: `initialize`/`initialized` handshake

---

## References

- [API-REFERENCE.md](./API-REFERENCE.md) - Detailed HTTP API documentation
- [WEBSOCKET-STABILITY.md](./WEBSOCKET-STABILITY.md) - Mobile connection stability
- [ARCHITECTURE.md](./ARCHITECTURE.md) - System architecture
- [READINESS-ROADMAP-SOURCE-OF-TRUTH.md](../planning/READINESS-ROADMAP-SOURCE-OF-TRUTH.md) - Product readiness and roadmap
