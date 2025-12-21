# cdev Unified API Specification

## Overview

cdev uses a unified WebSocket endpoint that supports both JSON-RPC 2.0 and legacy command formats.

**Endpoint:** `ws://{host}:{port}/ws`
**Default Port:** 8766
**Protocol:** JSON-RPC 2.0 (recommended) or Legacy Commands
**Status:** ✅ Fully Implemented

### Implementation Status

| Feature | Status |
|---------|--------|
| JSON-RPC 2.0 format | ✅ Done |
| Dual-protocol detection | ✅ Done |
| Agent-agnostic naming (agent/* vs claude/*) | ✅ Done |
| OpenRPC auto-generation | ✅ Done |
| Capability negotiation (initialize) | ✅ Done |
| Legacy command support | ✅ Done (backward compatible) |

### API Discovery

The OpenRPC specification is auto-generated from method metadata:

```
GET /api/rpc/discover
```

Returns the complete JSON-RPC API specification with all registered methods.

---

## Connection

### WebSocket URL
```
ws://192.168.1.100:8766/ws
```

### From QR Code
```json
{
  "ws": "ws://192.168.1.100:8766/ws",
  "http": "http://192.168.1.100:8766",
  "session": "abc123",
  "repo": "my-project"
}
```

---

## JSON-RPC 2.0 Format

### Request
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "agent/run",
  "params": {
    "prompt": "Hello, Claude"
  }
}
```

### Response (Success)
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

### Response (Error)
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

### Notification (Server → Client)
```json
{
  "jsonrpc": "2.0",
  "method": "event/heartbeat",
  "params": {
    "server_time": "2025-01-15T10:30:00Z",
    "sequence": 42,
    "agent_status": "running",
    "uptime_seconds": 3600
  }
}
```

---

## Methods

### Agent Methods

#### `agent/run`
Start an AI agent with a prompt.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| prompt | string | Yes | The prompt to send |
| mode | string | No | `"new"` (default) or `"continue"` |
| session_id | string | No | Required if mode is `"continue"` |
| agent_type | string | No | `"claude"` (default), `"gemini"`, `"codex"` |

**Example:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "agent/run",
  "params": {
    "prompt": "Fix the bug in auth.go",
    "mode": "new"
  }
}
```

**Result:**
```json
{
  "status": "started",
  "session_id": "sess_abc123",
  "agent_type": "claude"
}
```

---

#### `agent/stop`
Stop the running agent.

**Params:** None

**Example:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "agent/stop"
}
```

**Result:**
```json
{
  "status": "stopped"
}
```

---

#### `agent/respond`
Send a response to an agent's tool use request.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| tool_use_id | string | Yes | The tool use ID from the request |
| response | string | Yes | The response content |
| is_error | boolean | No | Whether this is an error response |

**Example:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "agent/respond",
  "params": {
    "tool_use_id": "toolu_abc123",
    "response": "approved",
    "is_error": false
  }
}
```

---

### Status Methods

#### `status/get`
Get current server status.

**Params:** None

**Result:**
```json
{
  "session_id": "sess_abc123",
  "agent_session_id": "agent_xyz789",
  "agent_state": "running",
  "agent_type": "claude",
  "connected_clients": 2,
  "repo_path": "/Users/dev/my-project",
  "repo_name": "my-project",
  "uptime_seconds": 3600,
  "version": "1.0.0",
  "watcher_enabled": true,
  "git_enabled": true
}
```

---

### Git Methods

#### `git/status`
Get git repository status.

**Params:** None

**Result:**
```json
{
  "branch": "main",
  "ahead": 2,
  "behind": 0,
  "staged_count": 1,
  "unstaged_count": 2,
  "untracked_count": 1,
  "has_conflicts": false,
  "changed_files": ["file1.go", "file2.go"]
}
```

---

#### `git/diff`
Get diff for a file or all files.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | No | File path relative to repo root (omit for all files) |

**Result:**
```json
{
  "path": "src/main.go",
  "diff": "--- a/src/main.go\n+++ b/src/main.go\n@@ -1,3 +1,4 @@...",
  "is_staged": false,
  "is_new": false
}
```

---

#### `git/stage`
Stage files for commit.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| paths | string[] | Yes | Array of file paths to stage |

**Result:**
```json
{
  "status": "staged",
  "files_affected": 2
}
```

---

#### `git/unstage`
Unstage files from the staging area.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| paths | string[] | Yes | Array of file paths to unstage |

**Result:**
```json
{
  "status": "unstaged",
  "files_affected": 2
}
```

---

#### `git/discard`
Discard unstaged changes to files.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| paths | string[] | Yes | Array of file paths to discard changes |

**Result:**
```json
{
  "status": "discarded",
  "files_affected": 2
}
```

---

#### `git/commit`
Create a commit with staged changes.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| message | string | Yes | Commit message |
| push | boolean | No | Push to remote after commit (default: false) |

**Result:**
```json
{
  "status": "committed",
  "sha": "a1b2c3d4e5f6..."
}
```

---

#### `git/push`
Push commits to remote repository.

**Params:** None

**Result:**
```json
{
  "status": "pushed"
}
```

---

#### `git/pull`
Pull changes from remote repository.

**Params:** None

**Result:**
```json
{
  "status": "pulled"
}
```

---

#### `git/branches`
List all git branches.

**Params:** None

**Result:**
```json
{
  "branches": [
    {"name": "main", "current": true},
    {"name": "feature/auth", "current": false}
  ],
  "current": "main"
}
```

---

#### `git/checkout`
Checkout a branch.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| branch | string | Yes | Branch name to checkout |

**Result:**
```json
{
  "status": "checked_out",
  "branch": "feature/auth"
}
```

---

### File Methods

#### `file/get`
Get file content.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | Yes | File path relative to repo root |

**Result:**
```json
{
  "path": "src/main.go",
  "content": "package main...",
  "encoding": "utf-8",
  "size": 1234,
  "truncated": false
}
```

---

#### `file/list`
List directory contents.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | No | Relative path from repo root (empty for root directory) |

**Result:**
```json
{
  "path": "src",
  "entries": [
    {"name": "main.go", "type": "file", "size": 1234, "modified": "2025-01-15T10:30:00Z"},
    {"name": "utils", "type": "directory", "children_count": 5}
  ],
  "total_count": 2
}
```

---

### Session Methods

#### `session/list`
List available sessions from all configured AI agents.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| agent_type | string | No | Filter by agent type (claude, gemini, codex) |
| limit | number | No | Max sessions to return (default: 50) |

**Result:**
```json
{
  "sessions": [
    {
      "session_id": "sess_abc123",
      "agent_type": "claude",
      "summary": "Fix authentication bug",
      "message_count": 15,
      "start_time": "2025-01-15T10:00:00Z",
      "last_updated": "2025-01-15T10:30:00Z"
    }
  ],
  "total": 1
}
```

---

#### `session/get`
Get detailed information about a specific session.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | Yes | Session ID to retrieve |
| agent_type | string | No | Agent type (optional, searches all if not specified) |

**Result:**
```json
{
  "session_id": "sess_abc123",
  "agent_type": "claude",
  "summary": "Fix authentication bug",
  "message_count": 15,
  "start_time": "2025-01-15T10:00:00Z",
  "last_updated": "2025-01-15T10:30:00Z",
  "branch": "main",
  "project_path": "/Users/dev/my-project"
}
```

---

#### `session/messages`
Get paginated messages for a specific session.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | Yes | Session ID to get messages from |
| agent_type | string | No | Agent type (optional) |
| limit | number | No | Max messages to return (default: 50, max: 500) |
| offset | number | No | Offset for pagination (default: 0) |

**Result:**
```json
{
  "messages": [
    {
      "id": "msg_001",
      "session_id": "sess_abc123",
      "timestamp": "2025-01-15T10:00:00Z",
      "role": "user",
      "content": "Fix the authentication bug"
    }
  ],
  "total": 15,
  "limit": 50,
  "offset": 0,
  "has_more": false
}
```

---

#### `session/elements`
Get pre-parsed UI elements for a session, ready for rendering in mobile apps.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | Yes | Session ID to get elements from |
| agent_type | string | No | Agent type (optional) |
| limit | number | No | Max elements to return (default: 50, max: 100) |
| before | string | No | Return elements before this ID (for pagination) |
| after | string | No | Return elements after this ID (for catch-up) |

**Result:**
```json
{
  "session_id": "sess_abc123",
  "elements": [
    {
      "id": "elem_001",
      "type": "user_input",
      "timestamp": "2025-01-15T10:00:00Z",
      "content": {"text": "Fix the authentication bug"}
    },
    {
      "id": "elem_002",
      "type": "assistant_text",
      "timestamp": "2025-01-15T10:00:05Z",
      "content": {"text": "I'll analyze the authentication code..."}
    }
  ],
  "pagination": {
    "total": 25,
    "returned": 2,
    "has_more_before": false,
    "has_more_after": true,
    "oldest_id": "elem_001",
    "newest_id": "elem_002"
  }
}
```

---

#### `session/delete`
Delete a specific session or all sessions.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | No | Session ID to delete (if omitted, deletes all) |
| agent_type | string | No | Agent type (optional) |

**Result (single deletion):**
```json
{
  "status": "deleted",
  "session_id": "sess_abc123",
  "deleted": 1
}
```

**Result (all deletion):**
```json
{
  "status": "deleted",
  "deleted": 5
}
```

---

#### `session/watch`
Start watching a session for real-time updates. The client will receive notifications when new messages are added.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | Yes | Session ID to watch |

**Result:**
```json
{
  "status": "watching",
  "watching": true
}
```

---

#### `session/unwatch`
Stop watching the current session.

**Params:** None

**Result:**
```json
{
  "status": "unwatched"
}
```

---

### Lifecycle Methods

#### `initialize`
Initialize the connection and negotiate capabilities. This follows the LSP-style initialization pattern.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| protocolVersion | string | No | Protocol version the client supports |
| clientInfo | object | No | Client information (name, version) |
| capabilities | object | No | Client capabilities |

**Result:**
```json
{
  "protocolVersion": "1.0",
  "serverInfo": {
    "name": "cdev",
    "version": "1.0.0"
  },
  "capabilities": {
    "agent": {
      "run": true,
      "stop": true,
      "respond": true,
      "sessions": true,
      "sessionWatch": true
    },
    "git": {
      "status": true,
      "diff": true,
      "stage": true,
      "unstage": true,
      "discard": true,
      "commit": true,
      "push": true,
      "pull": true,
      "branches": true,
      "checkout": true
    },
    "file": {
      "get": true,
      "list": true,
      "maxFileSize": 10485760
    },
    "repository": {
      "index": true,
      "search": true,
      "tree": true
    },
    "notifications": [
      "event/agent_log",
      "event/agent_status",
      "event/agent_message",
      "event/file_changed",
      "event/git_status_changed",
      "event/heartbeat"
    ],
    "supportedAgents": ["claude"]
  }
}
```

#### `initialized`
Notification from client that initialization is complete. After this, the client can start sending other requests.

**Params:** None (notification, no response expected)

#### `shutdown`
Request the server to shut down gracefully.

**Result:**
```json
{
  "success": true
}
```

---

## Events (Server → Client)

Events are sent as JSON-RPC notifications (no `id` field).

### `event/heartbeat`
Periodic heartbeat (every 30 seconds).

```json
{
  "jsonrpc": "2.0",
  "method": "event/heartbeat",
  "params": {
    "server_time": "2025-01-15T10:30:00Z",
    "sequence": 42,
    "agent_status": "running",
    "uptime_seconds": 3600
  }
}
```

### `event/agent_started`
Agent has started.

```json
{
  "jsonrpc": "2.0",
  "method": "event/agent_started",
  "params": {
    "session_id": "sess_abc123",
    "agent_type": "claude",
    "prompt": "Fix the bug..."
  }
}
```

### `event/agent_output`
Agent output (text, tool use, etc).

```json
{
  "jsonrpc": "2.0",
  "method": "event/agent_output",
  "params": {
    "type": "text",
    "content": "I'll fix that bug for you..."
  }
}
```

### `event/agent_stopped`
Agent has stopped.

```json
{
  "jsonrpc": "2.0",
  "method": "event/agent_stopped",
  "params": {
    "session_id": "sess_abc123",
    "reason": "completed"
  }
}
```

### `event/file_changed`
File change detected.

```json
{
  "jsonrpc": "2.0",
  "method": "event/file_changed",
  "params": {
    "path": "src/main.go",
    "change": "modified"
  }
}
```

### `event/git_status_changed`
Git status changed.

```json
{
  "jsonrpc": "2.0",
  "method": "event/git_status_changed",
  "params": {
    "branch": "main",
    "staged": ["file1.go"],
    "unstaged": [],
    "untracked": []
  }
}
```

---

## Error Codes

| Code | Name | Description |
|------|------|-------------|
| -32700 | Parse Error | Invalid JSON |
| -32600 | Invalid Request | Not a valid JSON-RPC request |
| -32601 | Method Not Found | Method does not exist |
| -32602 | Invalid Params | Invalid method parameters |
| -32603 | Internal Error | Internal server error |
| -32001 | Agent Already Running | An agent is already running |
| -32002 | Agent Not Running | No agent is currently running |
| -32003 | Agent Error | Agent execution error |
| -32004 | Agent Not Configured | Agent type not configured |
| -32010 | File Not Found | Requested file not found |
| -32011 | Git Error | Git operation failed |
| -32012 | Session Not Found | Session not found |

---

## Legacy Command Format (Deprecated)

For backward compatibility, the server also accepts legacy commands:

```json
{
  "command": "run_claude",
  "payload": {
    "prompt": "Hello"
  },
  "request_id": "req_123"
}
```

**Supported Legacy Commands:**
- `run_claude` → `agent/run`
- `stop_claude` → `agent/stop`
- `respond_to_claude` → `agent/respond`
- `get_status` → `status/get`
- `get_file` → `file/get`
- `watch_session` → `session/watch`
- `unwatch_session` → `session/unwatch`

---

## Swift Types (iOS)

```swift
// MARK: - JSON-RPC Types

struct JSONRPCRequest<T: Encodable>: Encodable {
    let jsonrpc = "2.0"
    let id: Int
    let method: String
    let params: T?
}

struct JSONRPCResponse<T: Decodable>: Decodable {
    let jsonrpc: String
    let id: Int?
    let result: T?
    let error: JSONRPCError?
}

struct JSONRPCNotification<T: Decodable>: Decodable {
    let jsonrpc: String
    let method: String
    let params: T?
}

struct JSONRPCError: Decodable {
    let code: Int
    let message: String
    let data: AnyCodable?
}

// MARK: - Agent Types

struct AgentRunParams: Encodable {
    let prompt: String
    let mode: String?
    let sessionId: String?
    let agentType: String?

    enum CodingKeys: String, CodingKey {
        case prompt
        case mode
        case sessionId = "session_id"
        case agentType = "agent_type"
    }
}

struct AgentRunResult: Decodable {
    let status: String
    let sessionId: String
    let agentType: String

    enum CodingKeys: String, CodingKey {
        case status
        case sessionId = "session_id"
        case agentType = "agent_type"
    }
}

struct AgentRespondParams: Encodable {
    let toolUseId: String
    let response: String
    let isError: Bool?

    enum CodingKeys: String, CodingKey {
        case toolUseId = "tool_use_id"
        case response
        case isError = "is_error"
    }
}

// MARK: - Event Types

struct HeartbeatEvent: Decodable {
    let serverTime: String
    let sequence: Int
    let agentStatus: String
    let uptimeSeconds: Int

    enum CodingKeys: String, CodingKey {
        case serverTime = "server_time"
        case sequence
        case agentStatus = "agent_status"
        case uptimeSeconds = "uptime_seconds"
    }
}

struct StatusResult: Decodable {
    let sessionId: String?
    let agentSessionId: String
    let agentState: String
    let agentType: String?
    let connectedClients: Int
    let repoPath: String
    let repoName: String
    let uptimeSeconds: Int
    let version: String
    let watcherEnabled: Bool
    let gitEnabled: Bool

    enum CodingKeys: String, CodingKey {
        case sessionId = "session_id"
        case agentSessionId = "agent_session_id"
        case agentState = "agent_state"
        case agentType = "agent_type"
        case connectedClients = "connected_clients"
        case repoPath = "repo_path"
        case repoName = "repo_name"
        case uptimeSeconds = "uptime_seconds"
        case version
        case watcherEnabled = "watcher_enabled"
        case gitEnabled = "git_enabled"
    }
}
```

---

## Migration Guide

### From Legacy to JSON-RPC 2.0

1. **Update WebSocket URL:**
   ```swift
   // Old
   let url = URL(string: "ws://\(host):8765")!

   // New
   let url = URL(string: "ws://\(host):8766/ws")!
   // Or use QR code's ws field directly
   ```

2. **Update message format:**
   ```swift
   // Old
   let message = """
   {"command": "run_claude", "payload": {"prompt": "\(prompt)"}}
   """

   // New
   let request = JSONRPCRequest(
       id: nextId(),
       method: "agent/run",
       params: AgentRunParams(prompt: prompt, mode: nil, sessionId: nil, agentType: nil)
   )
   let message = try JSONEncoder().encode(request)
   ```

3. **Update event handling:**
   ```swift
   // Old
   if json["event"] == "heartbeat" { ... }

   // New
   if json["method"] == "event/heartbeat" { ... }
   ```
