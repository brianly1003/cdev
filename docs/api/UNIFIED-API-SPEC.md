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

**Result:**
```json
{
  "branch": "main",
  "staged": ["file1.go"],
  "unstaged": ["file2.go"],
  "untracked": ["file3.go"],
  "is_clean": false
}
```

#### `git/diff`
Get diff for a file.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | Yes | File path relative to repo root |

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
  "size": 1234,
  "truncated": false
}
```

---

### Session Methods

#### `session/list`
List available sessions.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| limit | number | No | Max sessions to return (default: 20) |

#### `session/watch`
Start watching a session for real-time updates.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | Yes | Session ID to watch |

#### `session/unwatch`
Stop watching the current session.

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
