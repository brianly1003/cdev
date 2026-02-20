# cdev API Reference

This document provides a complete API reference for mobile app developers and integration services.

## Overview

cdev exposes a unified server on a single port:
- **HTTP API** (`http://127.0.0.1:8766`) - Request/response operations
- **WebSocket** (`ws://127.0.0.1:8766/ws`) - Real-time event streaming and commands

### Protocol Support

The WebSocket endpoint supports two protocols:
- **JSON-RPC 2.0** (recommended) - Standard protocol with request/response correlation. See [UNIFIED-API-SPEC.md](./UNIFIED-API-SPEC.md) for complete method reference.
- **Legacy commands** (deprecated) - Original command format, will be removed in v3.0

### Runtime Notes (JSON-RPC)

- JSON-RPC is the only interface that supports multiple runtimes (Claude + Codex). HTTP endpoints are Claude-only.
- Always include `agent_type` in JSON-RPC calls (`session/start`, `session/send`, `session/respond`, `session/input`, `workspace/session/history`, `workspace/session/messages`, `workspace/session/watch`) to avoid defaulting to Claude.
- `session/start` returns a `status` value: `attached` (history or live session), `existing` (already running), `started` (new managed session), `not_found` (session_id invalid).
- Codex may return a temporary session ID with prefix `codex-temp-...` until it writes history; listen for the `event/session_id_resolved` JSON-RPC notification to switch to the real session ID.
- JSON-RPC events include `agent_type` for runtime filtering.

## Authentication

When `security.require_auth = true` (default), **all HTTP and WebSocket endpoints require bearer auth**.

```
Authorization: Bearer <access-token>
```

**Unauthenticated allowlist** (for pairing + token exchange):
- `/health`
- `/pair`
- `/api/pair/*`
- `/api/auth/exchange`
- `/api/auth/refresh`

**Token flow (summary):**
1. Pairing token is displayed in `/pair` or `/api/pair/info` (QR includes token).
2. Exchange pairing token via `POST /api/auth/exchange`.
3. Use returned access token for HTTP + WebSocket requests.
4. Refresh via `POST /api/auth/refresh`.

**Note:** Query‑string tokens are not supported.

---

## HTTP API

### Health Check

```
GET /health
```

Returns the health status of the agent.

**Response:**
```json
{
  "status": "ok",
  "time": "2025-12-17T21:14:45Z"
}
```

---

### Get Agent Status

```
GET /api/status
```

Returns the current status of the agent including Claude state, connected clients, and repository info.

**Response:**
```json
{
  "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
  "agent_session_id": "01ce425c-5b91-4f8a-b8dd-5d14644c3494",
  "version": "1.0.0",
  "repo_path": "/Users/brianly/Projects/messenger-integrator",
  "repo_name": "messenger-integrator",
  "uptime_seconds": 120,
  "claude_state": "idle",
  "connected_clients": 1,
  "watcher_enabled": true,
  "git_enabled": true,
  "is_git_repo": true
}
```

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | **Claude session ID** for continue operations. Empty string if no Claude session has been started. |
| `agent_session_id` | string | Agent instance ID (generated on startup). Changes when agent restarts. |
| `version` | string | Agent version |
| `repo_path` | string | Full path to repository |
| `repo_name` | string | Repository directory name |
| `uptime_seconds` | number | Agent uptime in seconds |
| `claude_state` | string | Current Claude state (see below) |
| `connected_clients` | number | Number of WebSocket clients connected |
| `watcher_enabled` | boolean | Whether file watcher is enabled |
| `git_enabled` | boolean | Whether git tracking is enabled |
| `is_git_repo` | boolean | Whether repository is a git repo |

**Important:** The `session_id` field is the **Claude session ID** captured from Claude CLI. Use this ID for `continue` mode operations. It will be empty (`""`) if:
- Agent just started and no Claude session has run yet
- Claude has not been started in this agent session

**Claude States:**
| State | Description |
|-------|-------------|
| `idle` | No Claude process running |
| `running` | Claude is processing |
| `waiting` | Claude is waiting for user input (AskUserQuestion) |
| `error` | Claude encountered an error |
| `stopped` | Claude was explicitly stopped |

---

### List Sessions

```
GET /api/claude/sessions
GET /api/claude/sessions?limit=20&offset=0
```

Returns a paginated list of available Claude sessions for the repository that can be continued. Sessions are cached in SQLite for fast access and synced in real-time via file watcher.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | number | 20 | Maximum number of sessions to return (max: 100) |
| `offset` | number | 0 | Number of sessions to skip for pagination |

**Response:**
```json
{
  "current": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
  "sessions": [
    {
      "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
      "summary": "Create a hello world function",
      "message_count": 4,
      "last_updated": "2025-12-17T21:14:45Z",
      "branch": "main"
    },
    {
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "summary": "Implement user authentication",
      "message_count": 12,
      "last_updated": "2025-12-16T15:30:00Z",
      "branch": "feature/auth"
    }
  ],
  "total": 45,
  "limit": 20,
  "offset": 0
}
```

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `current` | string | Currently active session ID (empty if none) |
| `sessions` | array | List of available sessions |
| `sessions[].session_id` | string | UUID to use with `continue` mode |
| `sessions[].summary` | string | First user message or conversation summary |
| `sessions[].message_count` | number | Number of messages in conversation |
| `sessions[].last_updated` | string | ISO 8601 timestamp of last activity |
| `sessions[].branch` | string | Git branch when session was active |
| `total` | number | Total number of sessions available |
| `limit` | number | Requested limit (clamped to 1-100) |
| `offset` | number | Requested offset |

**Pagination Example (iOS Swift):**
```swift
// Load first page
let firstPage = try await api.sessions(limit: 20, offset: 0)

// Load next page
let nextPage = try await api.sessions(limit: 20, offset: 20)

// Check if more pages available
let hasMore = firstPage.offset + firstPage.sessions.count < firstPage.total
```

---

### Get Session Messages (Paginated)

```
GET /api/claude/sessions/messages?session_id={id}
GET /api/claude/sessions/messages?session_id={id}&limit=50&offset=0&order=asc
```

Returns messages for a specific Claude session with pagination support. Messages are cached in SQLite for fast retrieval, making this suitable for sessions with thousands of messages.

**Performance:**
- First request indexes session to SQLite (~100ms for 3000 messages)
- Subsequent requests use cache (~5-10ms)
- Supports sessions with 10,000+ messages efficiently

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `session_id` | string | (required) | UUID of the session |
| `limit` | number | 50 | Max messages to return (1-500) |
| `offset` | number | 0 | Starting position for pagination |
| `order` | string | `asc` | Sort order: `asc` (oldest first) or `desc` (newest first) |

**Response:**
```json
{
  "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
  "messages": [
    {
      "type": "user",
      "uuid": "msg-001",
      "timestamp": "2025-12-17T21:14:45Z",
      "git_branch": "main",
      "message": { "role": "user", "content": "Create a hello world function" }
    },
    {
      "type": "assistant",
      "uuid": "msg-002",
      "timestamp": "2025-12-17T21:14:50Z",
      "message": { "role": "assistant", "content": [...] }
    }
  ],
  "total": 3000,
  "limit": 50,
  "offset": 0,
  "has_more": true,
  "cache_hit": true,
  "query_time_ms": 5.2
}
```

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Session UUID |
| `messages` | array | Array of message objects |
| `total` | number | Total messages in session |
| `limit` | number | Requested limit |
| `offset` | number | Requested offset |
| `has_more` | boolean | True if more messages available |
| `cache_hit` | boolean | True if served from cache (fast) |
| `query_time_ms` | number | Query execution time in milliseconds |

**Message Object Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `id` | number | Auto-incremented message ID |
| `session_id` | string | Session UUID |
| `type` | string | Message type: `user`, `assistant`, or `system` |
| `uuid` | string | Unique message identifier |
| `timestamp` | string | ISO 8601 timestamp |
| `git_branch` | string | Git branch at time of message |
| `message` | object | Message content with `role` and `content` |
| `is_context_compaction` | boolean | `true` if auto-generated by Claude Code context compaction |

**Context Compaction Messages:**

When Claude Code's context window is maxed out, it automatically compacts the conversation. These messages have `is_context_compaction: true`:

1. **System message** (`type: "system"`) - "Conversation compacted"
2. **User message** (`type: "user"`) - Auto-generated summary starting with "This session is being continued from a previous conversation..."

**Example - Context Compaction Messages:**
```json
{
  "messages": [
    {
      "id": 52500,
      "type": "system",
      "timestamp": "2025-12-19T17:09:22.303Z",
      "is_context_compaction": true,
      "message": { "role": "system", "content": "Conversation compacted" }
    },
    {
      "id": 52501,
      "type": "user",
      "timestamp": "2025-12-19T17:09:22.303Z",
      "is_context_compaction": true,
      "message": {
        "role": "user",
        "content": "This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:\n..."
      }
    }
  ]
}
```

**iOS should display these messages differently** (e.g., as a collapsible system notice) rather than as regular user messages.

**Pagination Example (iOS Swift):**
```swift
// Load first 50 messages (oldest first for chat history)
let firstPage = try await api.sessionMessages(
    sessionId: sessionId,
    limit: 50,
    offset: 0,
    order: "asc"
)

// Load next page
let nextPage = try await api.sessionMessages(
    sessionId: sessionId,
    limit: 50,
    offset: 50,
    order: "asc"
)

// Load latest messages (newest first for notifications)
let latest = try await api.sessionMessages(
    sessionId: sessionId,
    limit: 20,
    offset: 0,
    order: "desc"
)

// Infinite scroll implementation
func loadMoreMessages() async {
    guard page.hasMore else { return }
    let next = try await api.sessionMessages(
        sessionId: sessionId,
        limit: 50,
        offset: messages.count
    )
    messages.append(contentsOf: next.messages)
}

// Handle context compaction messages
func renderMessage(_ msg: SessionMessage) -> some View {
    if msg.isContextCompaction {
        // Display as collapsible system notice
        return ContextCompactionView(message: msg)
    } else {
        // Regular message
        return MessageBubbleView(message: msg)
    }
}

// Model
struct SessionMessage: Codable {
    let id: Int
    let type: String
    let uuid: String?
    let timestamp: String?
    let gitBranch: String?
    let message: MessageContent
    let isContextCompaction: Bool?

    enum CodingKeys: String, CodingKey {
        case id, type, uuid, timestamp, message
        case gitBranch = "git_branch"
        case isContextCompaction = "is_context_compaction"
    }
}
```

**Performance Tips:**
- Use `limit=50` for initial load, then paginate
- Use `order=desc` to get newest messages first (useful for "load more" at top)
- The `cache_hit=true` indicates fast SQLite cache was used
- First access to a session indexes it (~100ms), subsequent requests are ~5ms

---

### Start Claude

```
POST /api/claude/run
```

Spawns a Claude CLI process with the given prompt.

**Request Body:**
```json
{
  "prompt": "Create a hello world function",
  "mode": "new",
  "session_id": ""
}
```

**Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `prompt` | string | Yes | The prompt to send to Claude |
| `mode` | string | No | Session mode: `new` or `continue` (default: `new`) |
| `session_id` | string | Conditional | Required when mode is `continue` |

**Session Modes:**
| Mode | Description | Use Case |
|------|-------------|----------|
| `new` | Start a fresh conversation | New tasks, clean context |
| `continue` | Continue a specific session by ID | Follow-up questions, returning to previous work |

**Response (Success):**
```json
{
  "status": "started",
  "prompt": "Create a hello world function",
  "pid": 62771,
  "mode": "new",
  "session_id": ""
}
```

**Note:** The `session_id` in the response may be empty initially. Subscribe to the `claude_session_info` WebSocket event to receive the session ID once Claude starts.

**Error Responses:**
| Status | Error | Description |
|--------|-------|-------------|
| 400 | Invalid request | Missing prompt or invalid mode |
| 409 | Already running | Claude process is already active |
| 503 | Not available | Claude manager not initialized |

---

### Stop Claude

```
POST /api/claude/stop
```

Gracefully stops the running Claude CLI process by sending SIGTERM to the process group.

**Important: Async Behavior**

This endpoint returns **immediately** after sending the termination signal. The process may still be running when the response is received. To confirm termination:

1. **Listen for `claude_stopped` WebSocket event** (recommended)
2. **Or poll `/api/status`** until `claude_state` changes to `"stopped"` or `"idle"`

**Response:**
```json
{
  "status": "stopped"
}
```

**Note:** `"status": "stopped"` means the stop signal was sent, not that the process has terminated.

**Error Responses:**
| Status | Error | Description |
|--------|-------|-------------|
| 409 | Not running | No Claude process to stop |
| 503 | Not available | Claude manager not initialized |

**WebSocket Event (async confirmation):**

After the process terminates, a `claude_status` event is published:

```json
{
  "event": "claude_status",
  "timestamp": "2025-12-20T01:45:00Z",
  "payload": {
    "state": "stopped",
    "exit_code": 0
  }
}
```

**iOS Swift Example:**

```swift
// Stop Claude and wait for confirmation
func stopClaude() async {
    // 1. Send stop request
    let response = try await api.post("/api/claude/stop")

    // 2. Wait for confirmation via WebSocket event
    // The claude_status event with state="stopped" will be received
}

// Handle WebSocket events
func handleWebSocketEvent(_ event: AgentEvent) {
    if event.type == "claude_status" {
        let status = event.payload as! ClaudeStatusPayload
        if status.state == "stopped" {
            // Process has terminated
            updateUI(claudeState: .stopped)
        }
    }
}

// Alternative: Poll for status
func stopClaudeWithPolling() async throws {
    try await api.post("/api/claude/stop")

    // Poll until stopped
    for _ in 0..<30 {  // Max 3 seconds
        let status = try await api.get("/api/status")
        if status.claudeState == "stopped" || status.claudeState == "idle" {
            return
        }
        try await Task.sleep(nanoseconds: 100_000_000)  // 100ms
    }
    throw StopError.timeout
}
```

---

### Respond to Claude

```
POST /api/claude/respond
```

Send a response to Claude's interactive prompt or permission request.

**Request Body:**
```json
{
  "tool_use_id": "toolu_01ABC123...",
  "response": "approved",
  "is_error": false
}
```

**Parameters:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `tool_use_id` | string | Yes | The tool_use_id from `claude_permission` or `claude_waiting` event |
| `response` | string | Yes | The response text (e.g., "approved", user's answer, or denial reason) |
| `is_error` | boolean | No | Set to `true` to deny/reject the request |

**Permission Response Examples:**

Approve a permission:
```json
{
  "tool_use_id": "toolu_01ABC123...",
  "response": "approved",
  "is_error": false
}
```

Deny a permission:
```json
{
  "tool_use_id": "toolu_01ABC123...",
  "response": "Permission denied by user",
  "is_error": true
}
```

Answer an interactive question:
```json
{
  "tool_use_id": "toolu_01XYZ789...",
  "response": "Yes, proceed with option A",
  "is_error": false
}
```

---

### Get File Content

```
GET /api/file?path=<relative_path>
```

Returns the content of a file in the repository.

**Query Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | Yes | Relative path from repository root |

**Response:**
```json
{
  "path": "src/main.ts",
  "content": "export function main() {\n  console.log('Hello');\n}",
  "encoding": "utf-8",
  "truncated": false,
  "size": 52
}
```

---

### Git Status (Enhanced)

```
GET /api/git/status
```

Returns enhanced git status with staging info, branch tracking, and diff stats.

**Response:**
```json
{
  "branch": "main",
  "upstream": "origin/main",
  "ahead": 2,
  "behind": 0,
  "staged": [
    {"path": "src/main.ts", "status": "M", "additions": 10, "deletions": 5}
  ],
  "unstaged": [
    {"path": "src/utils.ts", "status": "M", "additions": 3, "deletions": 1}
  ],
  "untracked": [
    {"path": "src/new-file.ts", "status": "?", "additions": 0, "deletions": 0}
  ],
  "conflicted": [],
  "repo_name": "messenger-integrator",
  "repo_root": "/Users/brianly/Projects/messenger-integrator"
}
```

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `branch` | string | Current branch name |
| `upstream` | string | Upstream tracking branch (if set) |
| `ahead` | number | Commits ahead of upstream |
| `behind` | number | Commits behind upstream |
| `staged` | array | Files in staging area |
| `unstaged` | array | Modified files not staged |
| `untracked` | array | New files not tracked |
| `conflicted` | array | Files with merge conflicts |

**File Entry Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `path` | string | File path relative to repo root |
| `status` | string | Status code (M, A, D, R, ?) |
| `additions` | number | Lines added |
| `deletions` | number | Lines deleted |

---

### Git Stage

```
POST /api/git/stage
```

Stage files for commit.

**Request Body:**
```json
{
  "paths": ["src/main.ts", "src/utils.ts"]
}
```

**Response:**
```json
{
  "success": true,
  "staged": ["src/main.ts", "src/utils.ts"]
}
```

---

### Git Unstage

```
POST /api/git/unstage
```

Unstage files from the staging area.

**Request Body:**
```json
{
  "paths": ["src/main.ts"]
}
```

**Response:**
```json
{
  "success": true,
  "unstaged": ["src/main.ts"]
}
```

---

### Git Discard

```
POST /api/git/discard
```

Discard unstaged changes to files.

**Request Body:**
```json
{
  "paths": ["src/main.ts"]
}
```

**Response:**
```json
{
  "success": true,
  "discarded": ["src/main.ts"]
}
```

---

### Git Commit

```
POST /api/git/commit
```

Create a commit with staged changes. Optionally push after commit.

**Request Body:**
```json
{
  "message": "feat: add new feature",
  "push": false
}
```

**Response:**
```json
{
  "success": true,
  "sha": "abc123def",
  "message": "feat: add new feature",
  "files_committed": 3,
  "pushed": false
}
```

**Error Response:**
```json
{
  "success": false,
  "error": "nothing to commit"
}
```

---

### Git Push

```
POST /api/git/push
```

Push local commits to remote.

**Response:**
```json
{
  "success": true,
  "message": "Pushed 2 commits to origin/main",
  "commits_pushed": 2
}
```

---

### Git Pull

```
POST /api/git/pull
```

Pull remote commits to local.

**Response:**
```json
{
  "success": true,
  "message": "Pulled 3 commits",
  "commits_pulled": 3,
  "files_changed": 5,
  "conflicted_files": []
}
```

**Conflict Response:**
```json
{
  "success": false,
  "error": "Merge conflict",
  "conflicted_files": ["src/main.ts", "src/utils.ts"]
}
```

---

### Git Branches

```
GET /api/git/branches
```

List all local and remote branches.

**Response:**
```json
{
  "current": "main",
  "upstream": "origin/main",
  "ahead": 0,
  "behind": 0,
  "branches": [
    {"name": "main", "is_current": true, "is_remote": false, "upstream": "origin/main", "ahead": 0, "behind": 0},
    {"name": "feature/auth", "is_current": false, "is_remote": false, "upstream": "origin/feature/auth", "ahead": 1, "behind": 0},
    {"name": "origin/main", "is_current": false, "is_remote": true}
  ]
}
```

---

### Git Checkout

```
POST /api/git/checkout
```

Switch to a different branch or create a new one.

**Request Body:**
```json
{
  "branch": "feature/new-branch",
  "create": true
}
```

**Response:**
```json
{
  "success": true,
  "branch": "feature/new-branch",
  "message": "Switched to a new branch 'feature/new-branch'"
}
```

---

### Git Diff

```
GET /api/git/diff?path=<relative_path>
```

Returns git diff for a specific file or all files.

**Query Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `path` | string | No | Relative path (omit for all diffs) |

**Response (single file):**
```json
{
  "path": "src/main.ts",
  "diff": "diff --git a/src/main.ts...\n-old line\n+new line"
}
```

**Response (all files):**
```json
{
  "diffs": [
    {
      "path": "src/main.ts",
      "diff": "diff --git..."
    },
    {
      "path": "src/utils.ts",
      "diff": "diff --git..."
    }
  ]
}
```

---

## Repository API

The Repository API provides fast file search and browsing capabilities. See [REPOSITORY-INDEXER.md](../architecture/REPOSITORY-INDEXER.md) for detailed documentation.

### JSON-RPC Methods (Recommended)

All repository endpoints have JSON-RPC 2.0 equivalents available via WebSocket at `/ws`:

| HTTP Endpoint | JSON-RPC Method |
|--------------|-----------------|
| `GET /api/repository/index/status` | `repository/index/status` |
| `GET /api/repository/search` | `repository/search` |
| `GET /api/repository/files/list` | `repository/files/list` |
| `GET /api/repository/files/tree` | `repository/files/tree` |
| `GET /api/repository/stats` | `repository/stats` |
| `POST /api/repository/index/rebuild` | `repository/index/rebuild` |

See [UNIFIED-API-SPEC.md](./UNIFIED-API-SPEC.md) for complete JSON-RPC documentation.

### HTTP Endpoints

### Get Index Status

```
GET /api/repository/index/status
```

Returns the current status of the repository index.

**Response:**
```json
{
  "status": "ready",
  "total_files": 277,
  "indexed_files": 277,
  "total_size_bytes": 18940805,
  "last_full_scan": "2025-12-19T15:14:26Z",
  "last_update": "2025-12-19T15:14:26Z",
  "database_size_bytes": 4096,
  "is_git_repo": true
}
```

---

### Search Files

```
GET /api/repository/search?q=<query>&mode=<mode>&limit=<limit>
```

Search for files using various matching strategies.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `q` | string | required | Search query |
| `mode` | string | `fuzzy` | `fuzzy`, `exact`, `prefix`, `extension` |
| `limit` | number | 50 | Maximum results (max: 500) |
| `offset` | number | 0 | Pagination offset |
| `extensions` | string | - | Comma-separated extensions filter |
| `exclude_binaries` | boolean | true | Exclude binary files |

**Response:**
```json
{
  "query": "index",
  "mode": "fuzzy",
  "results": [
    {
      "path": "src/index.ts",
      "name": "index.ts",
      "directory": "src",
      "extension": "ts",
      "size_bytes": 8097,
      "modified_at": "2025-10-21T11:17:45Z",
      "is_binary": false,
      "is_sensitive": false,
      "match_score": 0.162
    }
  ],
  "total": 10,
  "elapsed_ms": 1
}
```

---

### List Files

```
GET /api/repository/files/list?directory=<path>&limit=<limit>
```

Returns a paginated list of files in a directory.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `directory` | string | `""` | Directory path to list |
| `recursive` | boolean | false | Include subdirectories |
| `limit` | number | 100 | Maximum files (max: 1000) |
| `offset` | number | 0 | Pagination offset |
| `sort` | string | `name` | `name`, `size`, `modified`, `path` |
| `order` | string | `asc` | `asc`, `desc` |

**Response:**
```json
{
  "directory": "",
  "files": [...],
  "directories": [
    {
      "path": "src",
      "name": "src",
      "file_count": 47,
      "total_size_bytes": 324277
    }
  ],
  "total_files": 26,
  "total_directories": 6,
  "pagination": {
    "limit": 100,
    "offset": 0,
    "has_more": false
  }
}
```

---

### Get Directory Tree

```
GET /api/repository/files/tree?path=<path>&depth=<depth>
```

Returns a hierarchical tree structure of the repository.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | `""` | Root path for tree |
| `depth` | number | 2 | Maximum depth (max: 10) |

---

### Get Repository Statistics

```
GET /api/repository/stats
```

Returns aggregate statistics about the repository.

**Response:**
```json
{
  "total_files": 277,
  "total_directories": 59,
  "total_size_bytes": 18940805,
  "files_by_extension": {"ts": 119, "json": 20},
  "binary_files": 53,
  "sensitive_files": 15
}
```

---

### Rebuild Index

```
POST /api/repository/index/rebuild
```

Triggers a full re-index of the repository (runs in background).

---

## WebSocket API

Connect to `ws://127.0.0.1:8766/ws` for real-time events and commands.

> **Note:** For JSON-RPC 2.0 protocol examples, see [UNIFIED-API-SPEC.md](./UNIFIED-API-SPEC.md). The legacy command format documented below is deprecated and will be removed in v3.0.

### Connection

```javascript
const ws = new WebSocket('ws://127.0.0.1:8766/ws');

ws.onopen = () => {
  console.log('Connected to cdev');
};

ws.onmessage = (event) => {
  const data = JSON.parse(event.data);
  console.log('Event:', data.event, data.payload);
};
```

### Sending Commands (Legacy Format)

> **Deprecated:** Use JSON-RPC 2.0 format instead. See [UNIFIED-API-SPEC.md](./UNIFIED-API-SPEC.md).

Commands are sent as JSON objects:

```javascript
// Start Claude
ws.send(JSON.stringify({
  command: "run_claude",
  payload: {
    prompt: "Create a hello world function",
    mode: "new"
  }
}));

// Continue conversation
ws.send(JSON.stringify({
  command: "run_claude",
  payload: {
    prompt: "Now add error handling",
    mode: "continue"
  }
}));

// Continue specific session
ws.send(JSON.stringify({
  command: "run_claude",
  payload: {
    prompt: "Continue the task",
    mode: "continue",
    session_id: "bd2ddce2-d50a-43b9-8129-602e7cdba072"
  }
}));

// Stop Claude
ws.send(JSON.stringify({
  command: "stop_claude"
}));

// Respond to permission/question
ws.send(JSON.stringify({
  command: "respond_to_claude",
  payload: {
    tool_use_id: "toolu_01ABC123...",
    response: "approved"
  }
}));

// Get current status
ws.send(JSON.stringify({
  command: "get_status"
}));

// Get file content
ws.send(JSON.stringify({
  command: "get_file",
  payload: {
    path: "src/main.ts"
  }
}));

// Watch a session for real-time updates (when Claude Code runs on laptop)
ws.send(JSON.stringify({
  command: "watch_session",
  payload: {
    session_id: "bd2ddce2-d50a-43b9-8129-602e7cdba072"
  }
}));

// Stop watching session
ws.send(JSON.stringify({
  command: "unwatch_session"
}));
```

### Real-Time Session Watching

The `watch_session` command enables real-time message streaming when Claude Code runs directly on the laptop (not through cdev). This is useful when:
- User is working with Claude Code in terminal
- iOS app wants to observe the conversation in real-time
- You want to sync messages without running Claude through cdev

**How it works:**
1. iOS sends `watch_session` with the session ID to observe
2. cdev watches the session's JSONL file for changes
3. When Claude Code writes new messages, cdev emits `claude_message` events
4. iOS receives real-time updates via WebSocket

**Safety:**
- Read-only access to session files (won't interfere with Claude Code)
- Debounced reads (waits 200ms for writes to complete before reading)
- Only one session can be watched at a time per client

**Expected Behavior:**

| Scenario | Server Action |
|----------|---------------|
| Client sends `watch_session` | Send `session_watch_started`, then stream `claude_message` events |
| Client sends `unwatch_session` | Send `session_watch_stopped`, stop streaming |
| Client sends `watch_session` for new session | Auto-unwatch previous, watch new session |
| Client disconnects | Clean up watch subscription (no event sent) |
| Session file doesn't exist | Send error response |

**Commands:**

**watch_session** - Subscribe to real-time updates
```json
{
  "command": "watch_session",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072"
  }
}
```

**unwatch_session** - Unsubscribe from updates
```json
{
  "command": "unwatch_session"
}
```

**Events:**

**session_watch_started** - Confirms subscription started
```json
{
  "event": "session_watch_started",
  "timestamp": "2025-12-20T01:45:00Z",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "watching": true
  }
}
```

**session_watch_stopped** - Confirms subscription ended
```json
{
  "event": "session_watch_stopped",
  "timestamp": "2025-12-20T01:50:00Z",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "watching": false,
    "reason": "client_request"
  }
}
```

**session_watch_stopped.reason values:**
| Reason | Description |
|--------|-------------|
| `client_request` | Client sent `unwatch_session` command |
| `session_ended` | Session file was deleted or session completed (future) |
| `error` | Error occurred while watching (future) |

**claude_message** - Real-time message update (streamed while watching)
```json
{
  "event": "claude_message",
  "timestamp": "2025-12-20T01:45:05Z",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "type": "assistant",
    "role": "assistant",
    "content": [
      {"type": "text", "text": "Here's the implementation..."}
    ],
    "stop_reason": ""
  }
}
```

**claude_message Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | Claude session ID |
| `type` | string | Message type: `user`, `assistant`, `system` |
| `role` | string | Message role |
| `content` | array | Content blocks |
| `stop_reason` | string | Empty = still working, `end_turn` = finished, `tool_use` = calling tool |
| `is_context_compaction` | boolean | `true` for auto-generated context compaction messages |

**stop_reason Values:**
| Value | Description |
|-------|-------------|
| `""` (empty) | Claude is still generating or intermediate message |
| `"end_turn"` | Claude finished its response |
| `"tool_use"` | Claude is calling a tool |

**Final message example (Claude finished):**
```json
{
  "event": "claude_message",
  "timestamp": "2025-12-20T01:46:00Z",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "type": "assistant",
    "role": "assistant",
    "content": [
      {"type": "text", "text": "Done! The implementation is complete."}
    ],
    "stop_reason": "end_turn"
  }
}
```

**iOS LIVE Indicator:**

Show a LIVE indicator when watching is active:
```swift
var isWatching = false

func handleEvent(_ event: WebSocketEvent) {
    switch event.type {
    case "session_watch_started":
        let payload = event.payload as! SessionWatchPayload
        isWatching = payload.watching  // true
        showLiveIndicator()

    case "session_watch_stopped":
        let payload = event.payload as! SessionWatchStoppedPayload
        isWatching = payload.watching  // false
        hideLiveIndicator()

        // Handle different stop reasons
        switch payload.reason {
        case "client_request":
            // User stopped watching - normal case
            break
        case "session_ended":
            showToast("Session ended")
        case "error":
            showToast("Watch error occurred")
        default:
            break
        }

    case "claude_message":
        if isWatching {
            appendMessageToTerminal(event.payload)
        }
    }
}

// Models
struct SessionWatchPayload: Codable {
    let sessionId: String
    let watching: Bool

    enum CodingKeys: String, CodingKey {
        case sessionId = "session_id"
        case watching
    }
}

struct SessionWatchStoppedPayload: Codable {
    let sessionId: String
    let watching: Bool
    let reason: String

    enum CodingKeys: String, CodingKey {
        case sessionId = "session_id"
        case watching
        case reason
    }
}
```

**Context Compaction Detection:**

When Claude Code's context window is maxed out, it automatically compacts the conversation. This results in two special messages:

1. **Compact Boundary** (system message):
```json
{
  "event": "claude_message",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "type": "system",
    "role": "system",
    "is_context_compaction": true,
    "content": [
      {"type": "text", "text": "Conversation compacted"}
    ]
  }
}
```

2. **Continuation Summary** (auto-generated user message):
```json
{
  "event": "claude_message",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "type": "user",
    "role": "user",
    "is_context_compaction": true,
    "content": [
      {"type": "text", "text": "This session is being continued from a previous conversation..."}
    ]
  }
}
```

**iOS should detect `is_context_compaction: true`** and display these messages differently (e.g., as a system notice or collapsed summary) rather than as regular user/assistant messages.

**iOS Swift Example:**
```swift
// When user selects a session to view
func selectSession(_ sessionId: String) {
    ws.send(WatchSessionCommand(sessionId: sessionId))
}

// Handle real-time messages
func handleEvent(_ event: WebSocketEvent) {
    switch event.type {
    case "session_watch_started":
        print("Now watching session: \(event.payload.sessionId)")
        showStopButton()  // Show stop button while Claude is working

    case "claude_message":
        let message = event.payload

        // Check for context compaction
        if message.isContextCompaction == true {
            // Display as system notice (collapsible, different styling)
            if message.type == "system" {
                showSystemNotice("Context compacted")
            } else {
                // Continuation summary - show collapsed by default
                showCollapsedSummary(message.content)
            }
        } else {
            // Regular message - add to UI
            appendMessage(message)
        }

        // Check if Claude finished (stop_reason indicates completion)
        if message.stopReason == "end_turn" {
            hideStopButton()
            showCompletionIndicator()
        }

    case "session_watch_stopped":
        print("Stopped watching session")
        hideStopButton()
    }
}

// When leaving session view
func leaveSessionView() {
    ws.send(UnwatchSessionCommand())
}

// Claude message model with stop_reason
struct ClaudeMessagePayload: Codable {
    let sessionId: String
    let type: String
    let role: String
    let content: [MessageContent]
    let stopReason: String?
    let isContextCompaction: Bool?

    enum CodingKeys: String, CodingKey {
        case sessionId = "session_id"
        case type, role, content
        case stopReason = "stop_reason"
        case isContextCompaction = "is_context_compaction"
    }
}
```

---

## WebSocket Events

Events are received as JSON with this structure:

```json
{
  "event": "<event_type>",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": { ... }
}
```

### session_start

Sent when a WebSocket client connects.

```json
{
  "event": "session_start",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "session_id": "33dc860a-6129-4d5b-987e-f38871233d2f",
    "repo_path": "/Users/brianly/Projects/messenger-integrator",
    "repo_name": "messenger-integrator"
  }
}
```

---

### claude_status

Sent when Claude's state changes. **Subscribe to this event to track Claude lifecycle.**

**State: running**
```json
{
  "event": "claude_status",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "state": "running",
    "prompt": "Create a hello world function",
    "pid": 62771,
    "started_at": "2025-12-17T21:14:45Z"
  }
}
```

**State: stopped** (sent after `/api/claude/stop` or process exits normally)
```json
{
  "event": "claude_status",
  "timestamp": "2025-12-17T21:15:30Z",
  "payload": {
    "state": "stopped",
    "exit_code": 0
  }
}
```

**State: idle** (sent when Claude completes successfully)
```json
{
  "event": "claude_status",
  "timestamp": "2025-12-17T21:15:30Z",
  "payload": {
    "state": "idle"
  }
}
```

**State: error** (sent when Claude exits with error)
```json
{
  "event": "claude_status",
  "timestamp": "2025-12-17T21:15:30Z",
  "payload": {
    "state": "error",
    "error": "exit code 1",
    "exit_code": 1
  }
}
```

**Payload Fields by State:**

| State | Fields | Description |
|-------|--------|-------------|
| `idle` | `state` | Claude completed successfully |
| `running` | `state`, `prompt`, `pid`, `started_at` | Claude is processing |
| `waiting` | `state` | Claude waiting for user input (reserved) |
| `error` | `state`, `error`, `exit_code` | Claude exited with error |
| `stopped` | `state`, `exit_code` | Claude was stopped by user (`/api/claude/stop`) |

**iOS State Machine:**

```
     ┌──────────────────────────────────────┐
     │                                      │
     ▼                                      │
  ┌──────┐    /api/claude/run    ┌─────────┐
  │ idle │ ───────────────────► │ running │
  └──────┘                       └─────────┘
     ▲                               │
     │                               │ (process exits)
     │                               ▼
     │         ┌─────────┬─────────┬─────────┐
     │         │         │         │         │
     │      success    error    stopped   timeout
     │         │         │         │         │
     │         ▼         ▼         ▼         ▼
     │      ┌──────┐  ┌───────┐  ┌─────────┐
     └──────┤ idle │  │ error │  │ stopped │
            └──────┘  └───────┘  └─────────┘
```

---

### claude_session_info

Sent when Claude's session ID is captured (shortly after starting).

```json
{
  "event": "claude_session_info",
  "timestamp": "2025-12-17T21:14:47Z",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "model": "",
    "version": ""
  }
}
```

**Important:** This event is broadcast asynchronously after Claude starts. Subscribe to this event to get the session ID for future `continue` operations.

---

### claude_log

Sent for each line of Claude output. Includes parsed structured data for rich UI rendering.

```json
{
  "event": "claude_log",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "line": "{\"type\":\"assistant\",\"message\":{...}}",
    "stream": "stdout",
    "parsed": {
      "type": "assistant",
      "session_id": "7d610ced-1938-46ed-8049-369b32fce2af",
      "stop_reason": "",
      "output_tokens": 245,
      "is_thinking": true,
      "content": [
        {
          "type": "text",
          "text": "<thinking>\nLet me analyze this...\n</thinking>"
        }
      ]
    }
  }
}
```

**Stream Types:**
| Stream | Description |
|--------|-------------|
| `stdout` | Standard output (JSON messages from Claude) |
| `stderr` | Standard error (debug/error messages) |

**Parsed Fields (for stdout JSON messages):**
| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Message type: `assistant`, `user`, `result`, `system`, `init` |
| `session_id` | string | Claude session ID |
| `stop_reason` | string | Empty = still generating, `end_turn` = done, `tool_use` = calling tool |
| `output_tokens` | int | Tokens generated so far (for "↓ 9.2k tokens" display) |
| `is_thinking` | bool | True when Claude is in thinking/ideating mode |
| `is_context_compaction` | bool | True for context compaction messages (see below) |
| `content` | array | Content blocks with type, text, tool info |

**Context Compaction:** When `is_context_compaction` is `true`, the message is auto-generated due to context window limit. This includes:
- System messages with `subtype: "compact_boundary"` (content: "Conversation compacted")
- User messages starting with "This session is being continued from a previous conversation..."

iOS should display these differently (e.g., as collapsed system notices).

**Detecting "Ideating..." State (iOS Example):**
```swift
func handleClaudeLog(_ event: ClaudeLogEvent) {
    guard let parsed = event.payload.parsed else { return }

    // Check if Claude is thinking/ideating
    if parsed.stopReason.isEmpty && parsed.isThinking {
        // Show "Ideating..." animation
        showThinkingIndicator(tokens: parsed.outputTokens)
    }

    // Check if Claude finished
    if parsed.stopReason == "end_turn" || parsed.stopReason == "tool_use" {
        hideThinkingIndicator()
    }
}

func showThinkingIndicator(tokens: Int) {
    let tokenStr = tokens > 1000 ? String(format: "%.1fk", Double(tokens)/1000) : "\(tokens)"
    statusLabel.text = "✻ Ideating... (↓ \(tokenStr) tokens)"
}
```

---

### PTY Events (Interactive Mode)

The following events are specific to **interactive PTY mode** (`permission_mode: "interactive"`). See [INTERACTIVE-PTY-MODE.md](../mobile/INTERACTIVE-PTY-MODE.md) for detailed documentation.

#### pty_permission

Sent when a permission prompt is detected in PTY mode.

```json
{
  "event": "pty_permission",
  "timestamp": "2025-12-26T12:00:01Z",
  "payload": {
    "type": "write_file",
    "target": "UserProfile.swift",
    "description": "Claude wants to create a file",
    "preview": "struct UserProfile: View {\n    var body: some View {\n        Text(\"Hello\")\n    }\n}",
    "options": [
      {"key": "1", "label": "Yes", "description": null, "selected": true},
      {"key": "2", "label": "Yes, and don't ask again for this file type", "description": null, "selected": false},
      {"key": "3", "label": "No", "description": null, "selected": false}
    ],
    "session_id": "sess-456"
  }
}
```

**Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Permission type: `write_file`, `edit_file`, `delete_file`, `bash_command`, `trust_folder`, `mcp_tool` |
| `target` | string | Target filename or command |
| `description` | string | Human-readable description |
| `preview` | string | Content preview |
| `options` | array | Available choices |
| `options[].selected` | boolean | `true` if cursor is on this option (❯) |
| `session_id` | string | Session ID |

---

#### pty_state

Sent when PTY state changes. **Important for detecting when Claude finishes.**

```json
{
  "event": "pty_state",
  "timestamp": "2025-12-26T12:00:02Z",
  "payload": {
    "state": "idle",
    "waiting_for_input": false,
    "prompt_type": "",
    "session_id": "sess-456"
  }
}
```

**State Values:**
| State | Description | `waiting_for_input` |
|-------|-------------|---------------------|
| `idle` | Claude finished or not running | `false` |
| `thinking` | Claude is processing | `false` |
| `permission` | Waiting for permission approval | `true` |
| `question` | Waiting for user answer | `true` |
| `error` | Error occurred | `false` |

**iOS Usage:**
```swift
func handlePTYState(_ event: PTYStateEvent) {
    if event.payload.state == "idle" {
        hideStopButton()
        setClaudeFinished()
    }
}
```

---

### claude_permission

Sent when Claude requests permission for a tool.

```json
{
  "event": "claude_permission",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "tool_use_id": "toolu_01ABC123XYZ...",
    "tool_name": "Write",
    "input": "{\"file_path\":\"/path/to/file.ts\",\"content\":\"...\"}",
    "description": "Write to file: src/main.ts"
  }
}
```

**Common Tool Names:**
| Tool | Description |
|------|-------------|
| `Write` | Write/create a file |
| `Edit` | Edit an existing file |
| `Bash` | Execute a shell command |
| `Read` | Read file contents |
| `Glob` | Search for files |
| `Grep` | Search file contents |

**Responding to Permissions:**

To approve:
```javascript
ws.send(JSON.stringify({
  command: "respond_to_claude",
  payload: {
    tool_use_id: "toolu_01ABC123XYZ...",
    response: "approved"
  }
}));
```

To deny:
```javascript
ws.send(JSON.stringify({
  command: "respond_to_claude",
  payload: {
    tool_use_id: "toolu_01ABC123XYZ...",
    response: "Permission denied by user",
    is_error: true
  }
}));
```

---

### claude_waiting

Sent when Claude is waiting for user input (AskUserQuestion tool).

```json
{
  "event": "claude_waiting",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "tool_use_id": "toolu_01XYZ789...",
    "tool_name": "AskUserQuestion",
    "input": "{\"question\":\"Which database should we use?\",\"options\":[...]}"
  }
}
```

**Responding to Questions:**
```javascript
ws.send(JSON.stringify({
  command: "respond_to_claude",
  payload: {
    tool_use_id: "toolu_01XYZ789...",
    response: "PostgreSQL"
  }
}));
```

---

### file_changed

Sent when a file in the repository is created, modified, deleted, or renamed.

**Created/Modified:**
```json
{
  "event": "file_changed",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "path": "src/main.ts",
    "change": "created",
    "size": 1024
  }
}
```

**Deleted:**
```json
{
  "event": "file_changed",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "path": "src/old-file.ts",
    "change": "deleted"
  }
}
```

**Renamed:**
```json
{
  "event": "file_changed",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "path": "src/new-name.ts",
    "change": "renamed",
    "old_path": "src/old-name.ts"
  }
}
```

**Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `path` | string | File path (new path for renames) |
| `change` | string | Change type (see below) |
| `size` | number | File size in bytes (for created/modified) |
| `old_path` | string | Previous path (only for renames) |

**Change Types:**
| Change | Description |
|--------|-------------|
| `created` | File was created |
| `modified` | File was modified |
| `deleted` | File was deleted |
| `renamed` | File was renamed (includes `old_path`) |

**iOS Swift Example - Handling Renames:**
```swift
func handleFileChanged(_ payload: FileChangedPayload) {
    switch payload.change {
    case "renamed":
        if let oldPath = payload.oldPath {
            // Update file list: remove old, add new
            removeFileFromList(oldPath)
            addFileToList(payload.path)

            // Update any open file views
            if currentlyViewingFile == oldPath {
                currentlyViewingFile = payload.path
            }

            // Show notification
            showToast("Renamed: \(oldPath) → \(payload.path)")
        }
    case "deleted":
        removeFileFromList(payload.path)
    case "created", "modified":
        refreshFileInList(payload.path)
    default:
        break
    }
}
```

---

### git_diff

Sent when git diff is generated (can be triggered by file changes).

```json
{
  "event": "git_diff",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "path": "src/main.ts",
    "diff": "diff --git a/src/main.ts b/src/main.ts\n..."
  }
}
```

---

### git_status_changed

Sent when git state changes. This event is emitted in two scenarios:

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
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "branch": "main",
    "ahead": 0,
    "behind": 0,
    "staged_count": 2,
    "unstaged_count": 1,
    "untracked_count": 0,
    "has_conflicts": false,
    "changed_files": ["src/main.ts", "src/utils.ts", "src/app.ts"]
  }
}
```

**Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `branch` | string | Current branch name |
| `ahead` | number | Commits ahead of upstream |
| `behind` | number | Commits behind upstream |
| `staged_count` | number | Number of staged files |
| `unstaged_count` | number | Number of unstaged modified files |
| `untracked_count` | number | Number of untracked files |
| `has_conflicts` | boolean | Whether there are merge conflicts |
| `changed_files` | array | List of all changed file paths |

**iOS Swift Example:**
```swift
func handleGitStatusChanged(_ payload: GitStatusPayload) {
    // Update git status UI in real-time
    updateBranchLabel(payload.branch)
    updateStagedBadge(count: payload.stagedCount)

    // No need to manually refresh - events arrive automatically
    // when user runs git commands in terminal or IDE
}
```

---

### git_operation_completed

Sent after any git operation completes (success or failure).

```json
{
  "event": "git_operation_completed",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "operation": "commit",
    "success": true,
    "message": "Created commit abc123",
    "sha": "abc123def",
    "files_affected": 3
  }
}
```

**Operations:** `stage`, `unstage`, `discard`, `commit`, `push`, `pull`, `checkout`, `delete_branch`, `fetch`, `stash`, `stash_apply`, `stash_pop`, `stash_drop`, `merge`, `merge_abort`, `init`, `remote_add`, `remote_remove`, `set_upstream`

**Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `operation` | string | Operation type |
| `success` | boolean | Whether operation succeeded |
| `message` | string | Success/info message |
| `error` | string | Error message (if failed) |
| `sha` | string | Commit SHA (for commit) |
| `branch` | string | Branch name (for checkout) |
| `files_affected` | number | Number of files affected |
| `commits_pushed` | number | Commits pushed (for push) |
| `commits_pulled` | number | Commits pulled (for pull) |
| `conflicted_files` | array | Conflicted files (for pull with conflicts) |

---

### git_branch_changed

Sent when a git branch changes (after checkout operation).

```json
{
  "event": "git_branch_changed",
  "timestamp": "2025-12-28T10:30:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "",
  "payload": {
    "from_branch": "main",
    "to_branch": "feature/auth",
    "session_id": ""
  }
}
```

**Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `from_branch` | string | Previous branch name |
| `to_branch` | string | New branch name |
| `session_id` | string | Session ID (if applicable) |

**iOS Swift Example:**
```swift
func handleGitBranchChanged(_ payload: GitBranchChangedPayload) {
    // Update branch display
    updateBranchLabel(payload.toBranch)

    // Optionally show notification
    showToast("Switched from \(payload.fromBranch) to \(payload.toBranch)")
}

struct GitBranchChangedPayload: Codable {
    let fromBranch: String
    let toBranch: String
    let sessionId: String?

    enum CodingKeys: String, CodingKey {
        case fromBranch = "from_branch"
        case toBranch = "to_branch"
        case sessionId = "session_id"
    }
}
```

---

### error

Sent when an error occurs.

```json
{
  "event": "error",
  "timestamp": "2025-12-17T21:14:45Z",
  "payload": {
    "error": "Failed to start Claude: process already running",
    "code": "CLAUDE_ALREADY_RUNNING"
  }
}
```

---

### heartbeat

Sent every 30 seconds as an application-level health check. Use this to detect stale connections beyond WebSocket ping/pong frames.

```json
{
  "event": "heartbeat",
  "timestamp": "2025-12-18T12:00:00Z",
  "payload": {
    "server_time": "2025-12-18T12:00:00Z",
    "sequence": 42,
    "claude_status": "idle",
    "uptime_seconds": 3600
  }
}
```

**Payload Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `server_time` | string | Server timestamp (ISO 8601) |
| `sequence` | number | Monotonically increasing sequence number |
| `claude_status` | string | Current Claude state: `idle`, `running`, `waiting`, `error` |
| `uptime_seconds` | number | Agent uptime in seconds |

**Client Implementation:**

Monitor heartbeats to detect connection issues:

```swift
// iOS Example
var lastHeartbeat = Date()

func handleEvent(_ event: CdevEvent) {
    if event.event == "heartbeat" {
        lastHeartbeat = Date()
    }
}

// Check every 5 seconds
Timer.scheduledTimer(withTimeInterval: 5, repeats: true) { _ in
    let elapsed = Date().timeIntervalSince(lastHeartbeat)
    if elapsed > 45 { // 1.5x heartbeat interval
        reconnect()
    }
}
```

---

## Mobile Integration Example

Here's a complete example for a mobile app (React Native / TypeScript):

```typescript
interface CdotEvent {
  event: string;
  timestamp: string;
  payload: any;
}

class CdotClient {
  private ws: WebSocket | null = null;
  private httpBase: string;
  private onEvent: (event: CdotEvent) => void;

  constructor(httpBase: string, onEvent: (event: CdotEvent) => void) {
    this.httpBase = httpBase;
    this.onEvent = onEvent;
  }

  connect(wsUrl: string): Promise<void> {
    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(wsUrl);

      this.ws.onopen = () => resolve();
      this.ws.onerror = (error) => reject(error);

      this.ws.onmessage = (event) => {
        const data = JSON.parse(event.data);
        this.onEvent(data);
      };
    });
  }

  async listSessions(): Promise<SessionInfo[]> {
    const response = await fetch(`${this.httpBase}/api/claude/sessions`);
    const data = await response.json();
    return data.sessions;
  }

  startClaude(prompt: string, mode: 'new' | 'continue' = 'new', sessionId?: string) {
    this.ws?.send(JSON.stringify({
      command: 'run_claude',
      payload: { prompt, mode, session_id: sessionId }
    }));
  }

  stopClaude() {
    this.ws?.send(JSON.stringify({ command: 'stop_claude' }));
  }

  respondToPermission(toolUseId: string, approved: boolean) {
    this.ws?.send(JSON.stringify({
      command: 'respond_to_claude',
      payload: {
        tool_use_id: toolUseId,
        response: approved ? 'approved' : 'Permission denied by user',
        is_error: !approved
      }
    }));
  }

  answerQuestion(toolUseId: string, answer: string) {
    this.ws?.send(JSON.stringify({
      command: 'respond_to_claude',
      payload: {
        tool_use_id: toolUseId,
        response: answer
      }
    }));
  }

  disconnect() {
    this.ws?.close();
  }
}

// Usage with heartbeat monitoring
let lastHeartbeat = Date.now();
let heartbeatTimer: NodeJS.Timer;

const client = new CdotClient('http://127.0.0.1:8766', (event) => {
  switch (event.event) {
    case 'heartbeat':
      // Update heartbeat timestamp for connection health monitoring
      lastHeartbeat = Date.now();
      updateConnectionStatus('connected', event.payload.claude_status);
      break;
    case 'claude_status':
      console.log('Claude state:', event.payload.state);
      break;
    case 'claude_session_info':
      console.log('Session ID:', event.payload.session_id);
      break;
    case 'claude_permission':
      // Show permission dialog to user
      showPermissionDialog(event.payload);
      break;
    case 'claude_waiting':
      // Show question dialog to user
      showQuestionDialog(event.payload);
      break;
    case 'claude_log':
      // Append to output log
      appendLog(event.payload.line);
      break;
    case 'file_changed':
      // Handle file system changes
      handleFileChanged(event.payload);
      break;
  }
});

// Handle file changes including renames
function handleFileChanged(payload: {
  path: string;
  change: 'created' | 'modified' | 'deleted' | 'renamed';
  size?: number;
  old_path?: string;
}) {
  switch (payload.change) {
    case 'renamed':
      if (payload.old_path) {
        // Remove old file from list, add new file
        removeFromFileList(payload.old_path);
        addToFileList(payload.path, payload.size);
        console.log(`Renamed: ${payload.old_path} → ${payload.path}`);
      }
      break;
    case 'deleted':
      removeFromFileList(payload.path);
      break;
    case 'created':
    case 'modified':
      refreshFileInList(payload.path, payload.size);
      break;
  }
}

// Start heartbeat monitoring (check every 5 seconds)
heartbeatTimer = setInterval(() => {
  const elapsed = Date.now() - lastHeartbeat;
  if (elapsed > 45000) { // 1.5x heartbeat interval (30s)
    console.warn('Connection stale - reconnecting...');
    client.disconnect();
    reconnect();
  }
}, 5000);

await client.connect('ws://127.0.0.1:8766/ws');
client.startClaude('Create a hello world function');
```

---

## OpenAPI / Swagger

Interactive API documentation is available when the agent is running:

- **Swagger UI**: `http://127.0.0.1:8766/swagger/`
- **OpenAPI JSON**: `http://127.0.0.1:8766/swagger/doc.json`

---

## Error Handling

All API errors follow this format:

```json
{
  "error": "Human-readable error message"
}
```

**HTTP Status Codes:**
| Code | Description |
|------|-------------|
| 200 | Success |
| 400 | Bad Request (invalid parameters) |
| 404 | Not Found (file/resource doesn't exist) |
| 409 | Conflict (e.g., Claude already running) |
| 500 | Internal Server Error |
| 503 | Service Unavailable (component not initialized) |

---

## Rate Limits

Currently no rate limits are enforced. The agent is designed for single-user local development.

---

## Connection Stability (Mobile Clients)

The agent includes features specifically designed for mobile client stability:

### Server Configuration

| Parameter | Value | Purpose |
|-----------|-------|---------|
| Heartbeat interval | 30s | Application-level health check |
| Ping interval | 81s | WebSocket-level keep-alive |
| Pong timeout | 90s | Mobile network tolerance |
| Write timeout | 15s | Slow network tolerance |
| Send buffer | 1024 | Handle burst events |

### Recommended Client Implementation

1. **Monitor heartbeats** - Reconnect if no heartbeat for 45 seconds
2. **Handle network changes** - Reconnect on WiFi ↔ Cellular transitions
3. **Handle app lifecycle** - Reconnect when app returns to foreground
4. **Exponential backoff** - Use increasing delays for reconnection attempts

### iOS-Specific Considerations

```swift
// Network monitoring
let monitor = NWPathMonitor()
monitor.pathUpdateHandler = { path in
    if path.status == .satisfied && !isConnected {
        reconnect()
    }
}

// App lifecycle
NotificationCenter.default.addObserver(
    forName: UIApplication.willEnterForegroundNotification,
    object: nil, queue: .main
) { _ in
    reconnect()
}
```

### Reconnection Strategy

```
Attempt 1: immediate
Attempt 2: 1 second delay
Attempt 3: 2 second delay
Attempt 4: 4 second delay
...
Max delay: 30 seconds
Max attempts: 10
```

For detailed analysis, see [WEBSOCKET-STABILITY.md](./WEBSOCKET-STABILITY.md).
