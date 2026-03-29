# cdev Protocol Specification

**Version:** 2.3.0
**Status:** Implemented
**Last Updated:** March 1, 2026

---

## Overview

The cdev protocol defines the communication standard between cdev (server) and clients (cdev-ios, VS Code extensions, etc.) for remote supervision and control of AI coding assistant sessions.

> **Note:** This document tracks the current JSON-RPC protocol. For complete method-by-method examples and payloads, see `docs/api/UNIFIED-API-SPEC.md`.

### Protocol Evolution

| Version | Description |
|---------|-------------|
| 2.3 | Agent Task Protocol — autonomous task lifecycle, revision loop, task events |
| 2.0 | JSON-RPC 2.0 with agent-agnostic naming |

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
| HTTP | 16180 | REST API, health checks, OpenRPC discovery |
| WebSocket | 16180 | Real-time events via `/ws` endpoint (JSON-RPC 2.0) |

**Note:** Port consolidation complete - single port 16180 serves all traffic.

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
5. [Agent Task Protocol](#agent-task-protocol)
6. [Connection Lifecycle](#connection-lifecycle)
7. [Error Handling](#error-handling)
8. [Protocol Gaps & TODOs](#protocol-gaps--todos)
9. [Version History](#version-history)

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
| `agent/task/create` | Create task from manual input |
| `agent/task/create_from_webhook` | Create task from signed webhook event |
| `agent/task/list` | List tasks with filters |
| `agent/task/get` | Get task detail + timeline |
| `agent/task/approve` | Approve and complete a task |
| `agent/task/reject` | Reject a task result |
| `agent/task/revise` | Submit revision feedback |
| `agent/task/cancel` | Cancel a running task |

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

### Agent Task Events

#### `task_created`

New autonomous task submitted.

```json
{
  "event": "task_created",
  "timestamp": "2026-03-01T00:00:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "source": "dashboard-webhook",
    "workspace_id": "lazy",
    "title": "Fix Telegram missing WorkflowConditions",
    "status": "pending",
    "risk_level": "medium",
    "autonomy_mode": "bounded-auto",
    "trigger": {
      "type": "replay",
      "ref": "docs/replays/new-member-withdrawal-wrong-tsm-transfer.json"
    }
  }
}
```

#### `task_status_changed`

Any task state transition.

```json
{
  "event": "task_status_changed",
  "timestamp": "2026-03-01T00:30:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "from": "running",
    "to": "validating",
    "metadata": {
      "files_changed": 3,
      "tests_added": 5
    }
  }
}
```

#### `task_progress`

Agent progress update during execution.

```json
{
  "event": "task_progress",
  "timestamp": "2026-03-01T00:15:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "message": "Running dotnet test...",
    "step": 3,
    "total": 5,
    "iteration": 1,
    "tool_calls_used": 42
  }
}
```

#### `task_needs_approval`

Task ready for human review. Clients should display task summary, diff, and approve/reject/revise controls.

```json
{
  "event": "task_needs_approval",
  "timestamp": "2026-03-01T00:45:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "title": "Fix Telegram missing WorkflowConditions",
    "risk_level": "medium",
    "result": {
      "files_changed": ["ChatTransferService.cs", "TransferWorkflowConditionsTests.cs"],
      "build_status": "pass",
      "test_status": "724 passed, 0 failed",
      "summary": "Added centralized fallback for WorkflowConditions in RecordTransferChatBotAction",
      "branch": "task/fix-telegram-conditions",
      "pr_url": null
    }
  }
}
```

#### `task_revision_requested`

Human submitted revision feedback. Agent will apply delta changes.

```json
{
  "event": "task_revision_requested",
  "timestamp": "2026-03-01T01:00:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "feedback": "Remove promotion remark, change Summary label to Info",
    "revision_number": 1,
    "max_revisions": 3
  }
}
```

#### `task_completed`

Task finished successfully.

```json
{
  "event": "task_completed",
  "timestamp": "2026-03-01T01:15:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "result": {
      "files_changed": ["ChatTransferService.cs", "TransferWorkflowConditionsTests.cs"],
      "build_status": "pass",
      "test_status": "724 passed, 0 failed",
      "summary": "Added centralized fallback for WorkflowConditions...",
      "branch": "task/fix-telegram-conditions",
      "pr_url": "https://github.com/org/repo/pull/42"
    },
    "revisions_applied": 2,
    "total_duration_sec": 285
  }
}
```

#### `task_failed`

Task failed. Includes whether it can be retried.

```json
{
  "event": "task_failed",
  "timestamp": "2026-03-01T00:30:00.000Z",
  "payload": {
    "task_id": "task_abc123",
    "error": "Build failed after 3 retry attempts",
    "retriable": false,
    "attempt": 2,
    "max_retries": 2
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

### Agent Task Methods

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "method": "agent/task/create",
  "params": {
    "workspace_id": "lazy",
    "title": "Fix Telegram missing WorkflowConditions",
    "description": "Plugin conditions not included in transfer notification",
    "autonomy_mode": "bounded-auto",
    "policy": {
      "max_iterations": 50,
      "max_duration_sec": 600,
      "max_tool_calls": 200,
      "max_files_changed": 10,
      "max_revisions": 3,
      "require_approval": ["git-push", "git-merge"]
    }
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "agent/task/list",
  "params": {
    "workspace_id": "lazy",
    "status": "awaiting_approval",
    "limit": 20,
    "offset": 0
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "method": "agent/task/approve",
  "params": {
    "task_id": "task_abc123",
    "merge": true
  }
}
```

```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "method": "agent/task/revise",
  "params": {
    "task_id": "task_abc123",
    "feedback": "Remove promotion remark, change Summary label to Info",
    "files_hint": ["ChatTransferService.cs"]
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

### Task Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/tasks/webhook` | Create task from signed webhook event (any trigger type) |
| POST | `/api/tasks` | Create task from manual input |
| GET | `/api/tasks` | List tasks (filterable by status, workspace, date) |
| GET | `/api/tasks/{id}` | Get task detail + timeline |
| POST | `/api/tasks/{id}/approve` | Approve and complete task |
| POST | `/api/tasks/{id}/reject` | Reject task result |
| POST | `/api/tasks/{id}/revise` | Submit revision feedback |
| POST | `/api/tasks/{id}/cancel` | Cancel running task |
| GET | `/api/events/stream` | SSE stream for all task events |
| GET | `/api/events/stream?task_id={id}` | SSE stream for single task |

---

## Agent Task Protocol

This section defines the autonomous task lifecycle — how external systems (dashboards, CI/CD, monitoring, issue trackers) submit coding tasks, how cdev executes them, and how approval surfaces (cdev-ios, Slack bots, web dashboards) review and refine results. The protocol is project-agnostic: any workspace with build/test commands can participate.

### AgentTask Model

```json
{
  "id":              "task_abc123",
  "source":          "dashboard-webhook",
  "workspace_id":    "lazy",
  "workspace_path":  "/Users/dev/Projects/Lazy",
  "title":           "Fix Telegram missing WorkflowConditions",
  "description":     "Plugin conditions not included in transfer notification",
  "status":          "awaiting_approval",
  "risk_level":      "medium",
  "autonomy_mode":   "bounded-auto",
  "attempt":         1,
  "max_retries":     2,
  "policy": {
    "max_iterations":    50,
    "max_duration_sec":  600,
    "max_tool_calls":    200,
    "max_files_changed": 10,
    "max_revisions":     3,
    "require_approval":  ["git-push", "git-merge"]
  },
  "result": {
    "files_changed":  ["ChatTransferService.cs", "TransferWorkflowConditionsTests.cs"],
    "build_status":   "pass",
    "test_status":    "724 passed, 0 failed",
    "summary":        "Added centralized fallback for WorkflowConditions...",
    "branch":         "task/fix-telegram-conditions",
    "pr_url":         null
  },
  "trigger": {
    "type":          "replay",
    "ref":           "docs/replays/new-member-withdrawal-wrong-tsm-transfer.json",
    "hash":          "sha256:abc123..."
  },
  "timeline":        [],
  "created_at":      "2026-03-01T00:00:00Z",
  "updated_at":      "2026-03-01T00:45:00Z"
}
```

### Task State Machine

```
pending ──► planning ──► running ──► validating ──► awaiting_approval ──► completed
                              │                           │
                              ▼                           ├──► revision ──► validating ──► awaiting_approval
                           failed                         │
                              │                           └──► rejected ──► failed
                              ▼
                     (retry) planning
                        or
                      rolled_back
```

| State | Description | Valid Transitions |
|-------|-------------|-------------------|
| `pending` | Task created, queued for execution | → `planning` |
| `planning` | Agent analyzing task, building plan | → `running`, `failed` |
| `running` | Agent executing code changes | → `validating`, `failed` |
| `validating` | Build + test gates running | → `awaiting_approval`, `failed` |
| `awaiting_approval` | Human review required | → `completed`, `revision`, `rejected` |
| `revision` | Agent applying human feedback | → `validating` |
| `completed` | Task finished successfully | terminal |
| `failed` | Task failed (may retry) | → `planning` (retry), `rolled_back` |
| `rolled_back` | Changes reverted | terminal |
| `rejected` | Human rejected the result | → `failed` |

**Rules:**
- Retry only on transient/tool/runtime errors, capped by `max_retries`
- Any policy violation moves to `failed` immediately (no retry)
- Revision loop capped at `max_revisions` (default: 3)

### Revision Loop

The revision loop handles the reality of autonomous coding: the agent gets 85% right, humans refine the remaining 15%.

```
1. Agent completes task → status: awaiting_approval
2. Human reviews diff/result on any approval surface (cdev-ios, WebAdmin, Slack)
3. Human sends revision: agent/task/revise { feedback: "Remove promotion line" }
4. cdev spawns new Claude Code session on same branch
5. Agent applies delta changes, runs build + test
6. Task returns to: awaiting_approval
7. Repeat up to max_revisions (default: 3)
```

**Revision request payload:**

```json
{
  "task_id":    "task_abc123",
  "feedback":   "Remove promotion remark, change Summary label to Info",
  "priority":   "normal",
  "files_hint": ["ChatTransferService.cs"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `task_id` | string | yes | Task to revise |
| `feedback` | string | yes | Free-text human feedback |
| `priority` | string | no | `normal` (default) or `urgent` |
| `files_hint` | string[] | no | Files to focus on |

**Why revision instead of reject + new task?**
- **Context preservation**: same branch, same worktree, agent only applies delta
- **Speed**: revision takes seconds, new task takes minutes
- **Cost**: revision uses ~5% of the tokens of a full task run
- **Learning**: revision feedback is extractable as preferences for future tasks

### Autonomy Modes

| Mode | Behavior | When to Use |
|------|----------|-------------|
| `supervised` | Human approval required for all write/merge-risk operations | New integrations, untested workflows |
| `bounded-auto` | Auto-approve low-risk ops, human approval for high-risk | Default for production projects |
| `full-auto-bounded` | Agent completes and auto-merges if tests pass | Whitelisted repos, well-tested workflows |

### Webhook Ingestion (Task Sources)

Task sources (dashboards, CI/CD, monitoring) create tasks via signed webhooks. The trigger artifact is generic — cdev does not interpret it, it passes it to the agent session.

**Request:**

```
POST /api/tasks/webhook
Content-Type: application/json
X-Cdev-Signature: sha256=<HMAC-SHA256(body, shared_secret)>
X-Cdev-Timestamp: <unix_seconds>
X-Cdev-Event-Id: <unique_id>
```

**Payload:**

```json
{
  "event_id":       "evt_unique_123",
  "trigger": {
    "type":         "replay",
    "ref":          "docs/replays/2026-02-28-forgot-username-001.json",
    "hash":         "sha256:abc123..."
  },
  "workspace":      "lazy",
  "autonomy_mode":  "bounded-auto",
  "policy": {
    "max_iterations":    50,
    "max_duration_sec":  600,
    "max_tool_calls":    200,
    "max_files_changed": 10,
    "require_approval":  ["git-push", "git-merge"]
  }
}
```

**The `trigger` object:**

The trigger tells the agent *what went wrong* and *where to find the evidence*. Think of it like an email attachment — `type` is the file format, `ref` is the file path or URL, and `hash` is the checksum for deduplication.

- **`type`** — How to interpret the artifact (determines which project command runs)
- **`ref`** — Pointer to the evidence: a file path inside the repo, an external URL, or free text. cdev never opens or parses this value — it passes it directly to the agent session. The workspace's project commands (e.g., `/fix-issue`, `/analyze-replay`) interpret it based on `type`.
- **`hash`** — SHA-256 of the artifact content, used for idempotency (`event_id + trigger.hash` must be unique)

**Trigger types with real-world examples:**

#### `replay` — Failed conversation replay

A customer chatted with a livechat bot, got routed incorrectly, and the dashboard captured the conversation as a JSON fixture.

```json
{
  "type": "replay",
  "ref": "docs/replays/new-member-withdrawal-wrong-tsm-transfer.json",
  "hash": "sha256:9f2c4a..."
}
```

The agent runs: `/analyze-replay docs/replays/new-member-withdrawal-wrong-tsm-transfer.json`
It reads the conversation turns, identifies the routing bug, and fixes it.

#### `error` — Error tracking event (Sentry, Datadog, etc.)

Sentry catches a `NullReferenceException` in production. The monitoring dashboard sends a webhook to cdev.

```json
{
  "type": "error",
  "ref": "https://sentry.io/api/0/issues/12345/?format=json",
  "hash": "sha256:a1b2c3..."
}
```

The agent fetches or reads the error details (stack trace, file, line number) and runs: `/fix-issue NullReferenceException at PaymentService.cs:142 when user has no profile`

`ref` can be a Sentry/Datadog URL, or a local path to an exported error JSON file.

#### `ci_failure` — Failed CI/CD pipeline

GitHub Actions runs tests and 3 fail. CI sends a webhook to cdev.

```json
{
  "type": "ci_failure",
  "ref": "https://github.com/org/repo/actions/runs/987654321",
  "hash": "sha256:c3d4e5..."
}
```

The agent reads the CI run output, identifies which tests failed and why, then fixes the code.

`ref` can be a GitHub Actions run URL, GitLab pipeline URL, or path to a test report artifact.

#### `issue` — Bug tracker issue (GitHub, Linear, Jira)

A developer files a GitHub issue: "Login page shows 500 error when email contains a plus sign".

```json
{
  "type": "issue",
  "ref": "https://github.com/org/repo/issues/789",
  "hash": "sha256:e5f6g7..."
}
```

The agent reads the issue title and body (e.g., via `gh issue view 789`), then runs: `/fix-issue Login page shows 500 error when email contains a plus sign`

`ref` can be a GitHub issue URL, Linear issue ID, or Jira ticket key.

#### `alert` — Monitoring / performance regression

An APM tool detects response time increased 3x after a deploy.

```json
{
  "type": "alert",
  "ref": "https://app.datadoghq.com/apm/traces?query=service:api&start=1709251200",
  "hash": "sha256:h8i9j0..."
}
```

The agent investigates the performance regression, profiles the changed code, and optimizes.

#### `task_yaml` — Structured task definition

A task was created via the WebAdmin dashboard using `/create-task`, producing a structured YAML file with anchors, expected/actual behavior, and constraints.

```json
{
  "type": "task_yaml",
  "ref": "docs/tasks/2026-03-01-fix-telegram-missing-workflow-conditions.yaml",
  "hash": "sha256:k1l2m3..."
}
```

The agent reads the YAML directly — it contains file/method anchors, reproduction steps, and acceptance criteria — then runs: `/fix-issue docs/tasks/2026-03-01-fix-telegram-missing-workflow-conditions.yaml`

This is the richest trigger type because the YAML provides structured context instead of requiring the agent to discover it.

#### `manual` — Human description (no artifact)

A developer describes a bug in free text, with no external artifact.

```json
{
  "type": "manual",
  "ref": "",
  "hash": "sha256:n4o5p6..."
}
```

The task `title` and `description` fields contain all the context. The agent runs: `/fix-issue {task.description}`

`ref` is empty or omitted. The hash is computed from the task description for idempotency.

**Validation rules:**
- Reject if HMAC signature invalid
- Reject if timestamp drift > 5 minutes
- Reject if `event_id + trigger.hash` already processed (idempotency)

### Task Execution Workflow

Per autonomous task:

1. Create isolated git worktree + task branch
2. Spawn AI coding session (Claude Code, Codex, etc.) in worktree workspace
3. Route to project command based on `trigger.type`:
   - `replay` → `/analyze-replay {trigger.ref}` — agent reads conversation turns from the fixture
   - `task_yaml` → `/fix-issue {trigger.ref}` — agent reads structured YAML with anchors and constraints
   - `issue` → `/fix-issue {task.title}` — agent uses title + description as natural language input
   - `error` → `/fix-issue {task.description}` — agent extracts stack trace, file, line from error context
   - `ci_failure` → `/fix-issue {task.description}` — agent reads test report, identifies failing tests
   - `alert` → `/fix-issue {task.description}` — agent investigates performance regression
   - `manual` → `/fix-issue {task.description}` — agent works from developer's free-text description
4. Run workspace-defined validation gates (from workspace config):
   - Build command (e.g., `dotnet build`, `npm run build`, `go build ./...`)
   - Test command (e.g., `dotnet test`, `npm test`, `go test ./...`)
5. Package result: files changed, test/build status, summary, PR metadata
6. Transition to `awaiting_approval` if policy requires, else `completed`

**Note:** cdev does not hardcode project commands. Each workspace defines its own build/test/fix commands in its workspace config. The trigger type routing above is the default convention — workspaces can override it.

### Integration Guide

#### Adding a Task Source (e.g., dashboard, CI/CD, monitoring)

1. Register a webhook secret with cdev admin
2. POST trigger events to `/api/tasks/webhook` with HMAC signature and appropriate `trigger.type`
3. Subscribe to task events via SSE (`GET /api/events/stream`) or WebSocket
4. (Optional) Add UI for revision feedback via `agent/task/revise`

#### Adding an Approval Surface (e.g., Slack bot)

1. Obtain API key with approver role
2. Subscribe to `task_needs_approval` events via SSE or WebSocket
3. Display task summary + diff to human
4. Call `agent/task/approve`, `agent/task/reject`, or `agent/task/revise`

Minimal integration (approve/reject only):
```
GET  /api/tasks?status=awaiting_approval   → show pending tasks
POST /api/tasks/{id}/approve               → approve
POST /api/tasks/{id}/reject                → reject
```

Full integration (with revision):
```
POST /api/tasks/{id}/revise
Body: { "feedback": "Remove promotion remark, change Summary to Info" }
```

#### Adding a Monitoring Dashboard

1. Obtain API key with viewer role
2. `GET /api/tasks` with filters for status, date range, workspace
3. Subscribe to SSE stream for real-time updates
4. Display timeline events for each task

### Authorization Roles

| Role | Permissions |
|------|-------------|
| `task-source` | Create tasks (webhook or manual) |
| `viewer` | List and get tasks, subscribe to events |
| `approver` | Approve, reject, revise tasks |
| `admin` | All operations + cancel + config |

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
| `TASK_NOT_FOUND` | Task ID does not exist |
| `INVALID_TRANSITION` | State transition not allowed (e.g., approve a running task) |
| `REVISION_LIMIT` | Max revisions reached for this task |
| `POLICY_VIOLATION` | Task exceeded budget limits |
| `DUPLICATE_EVENT` | Idempotent replay — event already processed |
| `SIGNATURE_INVALID` | HMAC signature validation failed |
| `TIMESTAMP_DRIFT` | Webhook timestamp too old or too new |
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
| Task preference memory | Learn from revision feedback to reduce future revisions | P3 |
| Task batching | Submit multiple tasks as a batch with dependency ordering | P3 |
| Task templates | Reusable task templates for common fix patterns | P3 |

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

### Week 4-5: Agent Task Protocol Implementation

```
[ ] Add AgentTask domain model
    - internal/domain/events/agent_task.go
    - State machine with transition validation
    - Timeline event recording

[ ] Add agent/task/* JSON-RPC handlers
    - Create, list, get, approve, reject, revise, cancel
    - Idempotent webhook ingestion with HMAC validation
    - Generic trigger artifact support (replay, error, ci_failure, issue, etc.)

[ ] Add task event types
    - task_created, task_status_changed, task_progress
    - task_needs_approval, task_revision_requested
    - task_completed, task_failed

[ ] Add task spawner
    - Worktree isolation per task
    - Claude Code session spawning
    - Policy enforcement (budgets, limits)

[ ] Add SSE endpoint for task events
    - GET /api/events/stream (all tasks)
    - GET /api/events/stream?task_id={id} (single task)

[ ] Add authorization roles
    - task-source, viewer, approver, admin
    - Role-based access control for task endpoints
```

---

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 2.3.0 | 1 Mar 2026 | Agent Task Protocol: `agent/task/*` methods, task lifecycle events, revision loop, webhook ingestion, autonomy modes, authorization roles |
| 2.2.0 | 30 Jan 2026 | Auth pairing flow, runtime capability registry, repository indexer |
| 2.0.0 | 21 Dec 2025 | JSON-RPC 2.0 adoption, agent-agnostic naming, port consolidation |
| 1.0.0-draft | Dec 2025 | Initial draft specification |

### Migration to 2.0

Key changes from 1.0 to 2.0:

1. **JSON-RPC 2.0 Format**: Use `jsonrpc`, `id`, `method`, `params` instead of `command`, `request_id`, `payload`
2. **Agent-Agnostic Naming**: `claude/run` → `agent/run`, `claude/stop` → `agent/stop`
3. **Port Consolidation**: Single port 16180 instead of 8765 (WebSocket) + 16180 (HTTP)
4. **Unified Endpoint**: WebSocket at `/ws` instead of root
5. **OpenRPC Discovery**: Auto-generated spec at `/api/rpc/discover`
6. **Capability Negotiation**: `initialize`/`initialized` handshake

---

### Migration to 2.3

Key changes from 2.2 to 2.3:

1. **Agent Task Methods**: New `agent/task/*` method family for autonomous task lifecycle
2. **Task Events**: 7 new event types for task lifecycle tracking (`task_created`, `task_status_changed`, `task_progress`, `task_needs_approval`, `task_revision_requested`, `task_completed`, `task_failed`)
3. **Webhook Ingestion**: HMAC-signed `POST /api/tasks/replay` for task sources
4. **SSE Streaming**: `GET /api/events/stream` for real-time task event subscriptions
5. **Revision Loop**: First-class `agent/task/revise` for human-in-the-loop refinement
6. **Authorization Roles**: `task-source`, `viewer`, `approver`, `admin`
7. **Task Error Codes**: `TASK_NOT_FOUND`, `INVALID_TRANSITION`, `REVISION_LIMIT`, `POLICY_VIOLATION`, `DUPLICATE_EVENT`, `SIGNATURE_INVALID`, `TIMESTAMP_DRIFT`

---

## References

- [API-REFERENCE.md](./API-REFERENCE.md) - Detailed HTTP API documentation
- [WEBSOCKET-STABILITY.md](./WEBSOCKET-STABILITY.md) - Mobile connection stability
- [ARCHITECTURE.md](./ARCHITECTURE.md) - System architecture
- [READINESS-ROADMAP-SOURCE-OF-TRUTH.md](../planning/READINESS-ROADMAP-SOURCE-OF-TRUTH.md) - Product readiness and roadmap
