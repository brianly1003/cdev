# iOS Integration Guide

> **Version:** 2.4 (Real-Time Git State Watcher)
> **Last Updated:** December 2025

This guide helps the iOS team integrate with the new cdev server architecture.

## Related Documentation

| Document | Description |
|----------|-------------|
| [IOS-WORKSPACE-INTEGRATION.md](./IOS-WORKSPACE-INTEGRATION.md) | Multi-workspace support |
| [LIVE-SESSION-INTEGRATION.md](./LIVE-SESSION-INTEGRATION.md) | **NEW: LIVE session support** |
| [../guides/WORKSPACE-MANAGER-SETUP.md](../guides/WORKSPACE-MANAGER-SETUP.md) | Backend setup guide |

---

## Quick Start

### Server Connection

```
Server:     http://127.0.0.1:8766
WebSocket:  ws://127.0.0.1:8766/ws
Swagger:    http://127.0.0.1:8766/swagger/
OpenRPC:    http://127.0.0.1:8766/api/rpc/discover
```

### Protocol

All communication uses **JSON-RPC 2.0** over WebSocket.

```json
// Request
{"jsonrpc": "2.0", "id": 1, "method": "session/start", "params": {"workspace_id": "ws-123"}}

// Response
{"jsonrpc": "2.0", "id": 1, "result": {...}}
```

### DateTime Format Standard

**All datetime values use RFC3339 format in UTC timezone.**

```
Format:  YYYY-MM-DDTHH:mm:ssZ
Example: 2024-12-24T10:30:00Z
```

| Field | Format | Example |
|-------|--------|---------|
| `timestamp` | RFC3339 UTC | `"2024-12-24T10:30:00Z"` |
| `started_at` | RFC3339 UTC | `"2024-12-24T10:30:00Z"` |
| `last_active` | RFC3339 UTC | `"2024-12-24T10:35:00Z"` |
| `created_at` | RFC3339 UTC | `"2024-12-24T09:00:00Z"` |

**Swift Parsing:**

```swift
let formatter = ISO8601DateFormatter()
formatter.formatOptions = [.withInternetDateTime]

// Parse from API
let date = formatter.date(from: "2024-12-24T10:30:00Z")

// Format for display
let displayFormatter = DateFormatter()
displayFormatter.dateStyle = .medium
displayFormatter.timeStyle = .short
displayFormatter.timeZone = .current  // Convert to local time for display
let localString = displayFormatter.string(from: date!)
```

```json
// Event (notification)
{"jsonrpc": "2.0", "method": "event/claude_message", "params": {...}}
```

---

## Architecture Overview

### Hybrid Architecture (Git Tracker + File Watcher)

```
┌─────────────────────────────────────────────────────────────────────┐
│                     cdev Server (port 8766)                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐ │
│  │               GitTrackerManager (Always Available)             │ │
│  │   • Cached git trackers per workspace                          │ │
│  │   • Lazy initialization on first git operation                 │ │
│  │   • Health monitoring & auto-recovery                          │ │
│  └────────────────────────────────────────────────────────────────┘ │
│            │                              │                          │
│            ▼                              ▼                          │
│  ┌──────────────────────┐    ┌──────────────────────┐               │
│  │ Workspace A (config) │    │ Workspace B (config) │               │
│  │  path: /proj/app-a   │    │  path: /proj/app-b   │               │
│  │  ┌────────────────┐  │    │  (no active session) │               │
│  │  │ Session A1     │  │    │                      │               │
│  │  │ • Claude CLI   │  │    │  Git ops available   │               │
│  │  │ • File Watcher │  │    │  via cached tracker  │               │
│  │  └────────────────┘  │    └──────────────────────┘               │
│  └──────────────────────┘                                            │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
         │                                    │
         ▼                                    ▼
    ┌─────────┐                         ┌─────────┐
    │ iPhone  │                         │  iPad   │
    └─────────┘                         └─────────┘
```

### Resource Allocation

| Component | Created On | Lifecycle | Purpose |
|-----------|-----------|-----------|---------|
| **Git Tracker** | `workspace/add` | Cached (lazy init) | Git status, diff, commit, etc. |
| **File Watcher** | `session/start` | Per-session | Real-time file change events |
| **Claude CLI** | `session/start` | Per-session | AI coding assistant |

### Key Concepts

| Concept | Description |
|---------|-------------|
| **Workspace** | A git repository registered with the server |
| **Session** | An active Claude CLI instance for a workspace |
| **Event** | Real-time notifications (messages, status changes) |
| **Git Tracker** | Cached git operations manager (always available) |

---

## Connection Lifecycle

### Initialize Connection

When connecting to the WebSocket, the first call must be `initialize`. The response includes your unique `clientId` which is used for multi-device awareness.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "1.0",
    "clientInfo": {
      "name": "cdev-ios",
      "version": "1.0.0"
    }
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "1.0",
    "serverInfo": {
      "name": "cdev",
      "version": "1.0.0"
    },
    "capabilities": {
      "agent": {"run": true, "stop": true, "respond": true, "sessions": true, "sessionWatch": true},
      "git": {"status": true, "diff": true, "stage": true, "commit": true, "push": true, "pull": true},
      "file": {"get": true, "list": true}
    },
    "clientId": "40c1e40b-5b25-4991-84a7-3a763133e6c7"
  }
}
```

**Important:** Store the `clientId` from the response. You'll need it to:
- Identify yourself in `session_joined`/`session_left` events
- Recognize your own entries in the `viewers` array of sessions
- Track which events are about you vs other devices

### Swift Implementation

```swift
class WebSocketManager {
    var myClientId: String?

    func handleInitializeResponse(_ result: InitializeResult) {
        myClientId = result.clientId
        print("Connected as client: \(myClientId ?? "unknown")")
    }

    func isMe(_ clientId: String) -> Bool {
        return clientId == myClientId
    }
}
```

---

## API Reference

### Workspace Methods

#### `workspace/list` - List all workspaces

```json
// Request
{"jsonrpc": "2.0", "id": 1, "method": "workspace/list", "params": {}}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "workspaces": [
      {
        "id": "ws-abc123",
        "name": "my-project",
        "path": "/Users/dev/my-project",
        "auto_start": false,
        "created_at": "2024-12-24T10:00:00Z",
        "sessions": [
          {
            "id": "sess-xyz789",
            "workspace_id": "ws-abc123",
            "status": "running",
            "started_at": "2024-12-24T10:30:00Z",
            "last_active": "2024-12-24T10:35:00Z",
            "viewers": ["client-id-1", "client-id-2"]
          },
          {
            "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
            "workspace_id": "ws-abc123",
            "status": "historical",
            "summary": "Refactored authentication module",
            "message_count": 42,
            "last_updated": "2024-12-23T14:20:00Z"
          }
        ],
        "active_session_count": 1,
        "has_active_session": true,
        "active_session_id": "sess-xyz789"
      }
    ]
  }
}
```

**Session Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Unique session identifier (Claude CLI session UUID) |
| `workspace_id` | string | Parent workspace ID |
| `status` | string | `running` or `historical` (see below) |
| `started_at` | string | RFC3339 timestamp (running sessions only) |
| `last_active` | string | RFC3339 timestamp (running sessions only) |
| `viewers` | string[] | List of client IDs currently viewing this session |
| `summary` | string | Session summary (historical sessions only) |
| `message_count` | int | Number of messages (historical sessions only) |
| `last_updated` | string | RFC3339 timestamp (historical sessions only) |

**Session Status Types:**

| Status | Description | Can Use `session/send`? |
|--------|-------------|-------------------------|
| `running` | Active Claude CLI process, ready for prompts | Yes |
| `historical` | Past session from `~/.claude/projects/`, not currently running | No - must resume first |

**Important:** The `sessions` array includes both running sessions AND historical sessions from Claude's storage. To send prompts to a historical session, you must first resume it using `session/start` with `resume_session_id`.

**Multi-Device Awareness:**
- Each connected iOS device has a unique `clientId` (returned in `initialize` response)
- When a device calls `client/session/focus`, it registers as a viewer of that session
- The `viewers` array shows which devices are currently viewing each session
- Use your own `clientId` to identify yourself in the viewers list

#### `workspace/get` - Get workspace details

```json
// Request
{"jsonrpc": "2.0", "id": 2, "method": "workspace/get", "params": {"id": "ws-abc123"}}
```

#### `workspace/add` - Register a new workspace

```json
// Request
{"jsonrpc": "2.0", "id": 3, "method": "workspace/add", "params": {
  "name": "my-project",
  "path": "/Users/dev/my-project",
  "auto_start": false
}}
```

#### `workspace/remove` - Unregister a workspace

```json
// Request
{"jsonrpc": "2.0", "id": 4, "method": "workspace/remove", "params": {"id": "ws-abc123"}}
```

#### `workspace/status` - Get workspace status

**Returns detailed status for a specific workspace including git tracker state, active sessions, and watch status.**

```json
// Request
{"jsonrpc": "2.0", "id": 5, "method": "workspace/status", "params": {
  "workspace_id": "ws-abc123"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "workspace_id": "ws-abc123",
    "workspace_name": "my-project",
    "path": "/Users/dev/my-project",
    "auto_start": false,
    "created_at": "2024-12-24T10:00:00Z",

    "sessions": [
      {
        "id": "sess-xyz789",
        "workspace_id": "ws-abc123",
        "status": "running",
        "started_at": "2024-12-24T10:30:00Z",
        "last_active": "2024-12-24T10:35:00Z"
      }
    ],
    "active_session_count": 1,
    "has_active_session": true,

    "git_tracker_state": "healthy",
    "git_repo_name": "my-project",
    "is_git_repo": true,
    "git_last_error": "",

    "is_being_watched": true,
    "watched_session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
  }
}
```

**Response Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `workspace_id` | string | Workspace ID |
| `workspace_name` | string | Display name |
| `path` | string | Full repository path |
| `auto_start` | bool | Whether to auto-start session on server boot |
| `created_at` | string | When workspace was added (RFC3339) |
| `sessions` | array | List of active sessions |
| `active_session_count` | int | Number of running sessions |
| `has_active_session` | bool | Whether any session is running |
| `git_tracker_state` | string | Git tracker state: `healthy`, `unhealthy`, `unavailable`, `not_git` |
| `git_repo_name` | string | Repository name |
| `is_git_repo` | bool | Whether path is a git repository |
| `git_last_error` | string | Last git error (if any) |
| `is_being_watched` | bool | Whether a session is being watched for live updates |
| `watched_session_id` | string | Session ID being watched (if any) |

---

### File Methods

#### `workspace/files/list` - List files in a workspace directory

**Returns a paginated list of files and directories. Matches the format of `/api/repository/files/list`.**

```json
// Request
{"jsonrpc": "2.0", "id": 6, "method": "workspace/files/list", "params": {
  "workspace_id": "ws-abc123",
  "directory": "",
  "limit": 500,
  "offset": 0
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 6,
  "result": {
    "directory": "",
    "directories": [
      {
        "path": "src",
        "name": "src",
        "file_count": 42,
        "total_size_bytes": 156000,
        "last_modified": "2024-12-24T10:30:00Z"
      },
      {
        "path": "docs",
        "name": "docs",
        "file_count": 10,
        "total_size_bytes": 45000,
        "last_modified": "2024-12-23T14:00:00Z"
      }
    ],
    "files": [
      {
        "path": "README.md",
        "name": "README.md",
        "directory": "",
        "extension": "md",
        "size_bytes": 1595,
        "modified_at": "2024-12-24T10:00:00Z",
        "is_binary": false,
        "is_symlink": false,
        "is_sensitive": false,
        "git_tracked": false,
        "git_ignored": false
      },
      {
        "path": "package.json",
        "name": "package.json",
        "directory": "",
        "extension": "json",
        "size_bytes": 2456,
        "modified_at": "2024-12-24T09:00:00Z",
        "is_binary": false,
        "is_symlink": false,
        "is_sensitive": false,
        "git_tracked": false,
        "git_ignored": false
      }
    ],
    "total_files": 5,
    "total_directories": 3,
    "pagination": {
      "limit": 500,
      "offset": 0,
      "has_more": false
    }
  }
}
```

**Parameters:**

| Parameter | Required | Type | Default | Description |
|-----------|----------|------|---------|-------------|
| `workspace_id` | Yes | string | - | Workspace ID |
| `directory` | No | string | `""` | Relative path from workspace root |
| `limit` | No | int | 100 | Max entries to return (max 500) |
| `offset` | No | int | 0 | Pagination offset |

---

#### `workspace/file/get` - Get file content from a workspace

**Returns the content of a file from a workspace. Use this for multi-workspace mode instead of `file/get`.**

```json
// Request
{"jsonrpc": "2.0", "id": 7, "method": "workspace/file/get", "params": {
  "workspace_id": "ws-abc123",
  "path": "src/main.ts"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 7,
  "result": {
    "path": "src/main.ts",
    "content": "import { App } from './app';\n\nconst app = new App();\napp.run();",
    "encoding": "utf-8",
    "truncated": false,
    "size": 65
  }
}
```

**Parameters:**

| Parameter | Required | Type | Default | Description |
|-----------|----------|------|---------|-------------|
| `workspace_id` | Yes | string | - | Workspace ID |
| `path` | Yes | string | - | File path relative to workspace root |
| `max_size_kb` | No | int | 500 | Max file size to return (max 10000 KB) |

**Response Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | File path |
| `content` | string | File content (UTF-8) |
| `encoding` | string | Always "utf-8" |
| `truncated` | boolean | `true` if content was truncated due to size limit |
| `size` | int | Size of returned content in bytes |

---

### Session Methods

#### `session/start` - Start a Claude session

Starts a new Claude CLI session for the specified workspace. Use `resume_session_id` to continue a historical session.

**Parameters:**

| Parameter | Required | Type | Description |
|-----------|----------|------|-------------|
| `workspace_id` | Yes | string | Workspace ID to start session in |
| `resume_session_id` | No | string | Historical session ID to resume (from `workspace/session/history`) |

```json
// Request (new session)
{"jsonrpc": "2.0", "id": 10, "method": "session/start", "params": {
  "workspace_id": "ws-abc123"
}}

// Request (resume historical session)
{"jsonrpc": "2.0", "id": 10, "method": "session/start", "params": {
  "workspace_id": "ws-abc123",
  "resume_session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 10,
  "result": {
    "id": "sess-xyz789",
    "workspace_id": "ws-abc123",
    "status": "running",
    "started_at": "2024-12-24T10:30:00Z",
    "last_active": "2024-12-24T10:30:00Z"
  }
}
```

**Resuming Historical Sessions:**

When a user selects a historical session (status: `historical`) and wants to continue the conversation:
1. Call `session/start` with `resume_session_id` set to the historical session's ID
2. The new session will continue where the historical session left off
3. The session will have a new cdev session ID but the same Claude CLI session ID

#### `session/stop` - Stop a session

```json
// Request
{"jsonrpc": "2.0", "id": 11, "method": "session/stop", "params": {
  "session_id": "sess-xyz789"
}}
```

#### `session/send` - Send a prompt to Claude

**Important:** This method only works on sessions with `status: "running"`. For historical sessions, you must first resume them using `session/start` with `resume_session_id`.

```json
// Request
{"jsonrpc": "2.0", "id": 12, "method": "session/send", "params": {
  "session_id": "sess-xyz789",
  "prompt": "Help me refactor this code",
  "mode": "new"  // or "continue"
}}

// Response (success)
{"jsonrpc": "2.0", "id": 12, "result": {"status": "sent"}}

// Response (error - session not running)
{
  "jsonrpc": "2.0",
  "id": 12,
  "error": {
    "code": -32602,
    "message": "session not running: sess-xyz789 (use session/start with resume_session_id to resume historical sessions)"
  }
}
```

#### `session/respond` - Respond to permission/question

```json
// Permission response
{"jsonrpc": "2.0", "id": 13, "method": "session/respond", "params": {
  "session_id": "sess-xyz789",
  "type": "permission",
  "response": "yes"  // or "no"
}}

// Question response
{"jsonrpc": "2.0", "id": 14, "method": "session/respond", "params": {
  "session_id": "sess-xyz789",
  "type": "question",
  "response": "Use the singleton pattern"
}}
```

#### `session/state` - Get runtime state (for reconnection)

**Use this when reconnecting to sync the current state.**

```json
// Request
{"jsonrpc": "2.0", "id": 15, "method": "session/state", "params": {
  "session_id": "sess-xyz789"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 15,
  "result": {
    "id": "sess-xyz789",
    "workspace_id": "ws-abc123",
    "status": "running",
    "started_at": "2024-12-24T10:30:00Z",
    "last_active": "2024-12-24T10:35:00Z",

    "claude_state": "waiting",
    "claude_session_id": "claude-session-abc",
    "is_running": true,
    "waiting_for_input": true,
    "pending_tool_use_id": "tool-123",
    "pending_tool_name": "Bash"
  }
}
```

#### `session/active` - List active sessions

```json
// Request (all sessions)
{"jsonrpc": "2.0", "id": 16, "method": "session/active", "params": {}}

// Request (filtered by workspace)
{"jsonrpc": "2.0", "id": 17, "method": "session/active", "params": {
  "workspace_id": "ws-abc123"
}}
```

#### `workspace/session/history` - Get historical sessions for a workspace

**Returns Claude session history from `~/.claude/projects/<encoded-path>`.**

```json
// Request
{"jsonrpc": "2.0", "id": 18, "method": "workspace/session/history", "params": {
  "workspace_id": "ws-abc123",
  "limit": 20
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 18,
  "result": {
    "sessions": [
      {
        "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
        "summary": "Refactored authentication module",
        "message_count": 42,
        "last_updated": "2024-12-24T10:30:00Z",
        "branch": "feature/auth"
      },
      {
        "session_id": "f9e8d7c6-b5a4-3210-fedc-ba0987654321",
        "summary": "Fixed bug in user profile",
        "message_count": 15,
        "last_updated": "2024-12-23T14:20:00Z",
        "branch": "main"
      }
    ],
    "total": 2
  }
}
```

**Path Mapping:**
- Workspace path: `/Users/brianly/Projects/cdev`
- Session storage: `~/.claude/projects/-Users-brianly-Projects-cdev`

#### `workspace/session/messages` - Get messages from a historical session

**Returns paginated messages from a Claude session file.**

```json
// Request
{"jsonrpc": "2.0", "id": 19, "method": "workspace/session/messages", "params": {
  "workspace_id": "ws-abc123",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "limit": 50,
  "offset": 0,
  "order": "asc"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 19,
  "result": {
    "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "messages": [
      {
        "id": 1,
        "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
        "type": "user",
        "uuid": "msg-uuid-123",
        "timestamp": "2024-12-24T10:30:00Z",
        "git_branch": "main",
        "message": {"role": "user", "content": "Help me refactor this code"},
        "is_context_compaction": false,
        "is_meta": false
      },
      {
        "id": 2,
        "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
        "type": "assistant",
        "uuid": "msg-uuid-124",
        "timestamp": "2024-12-24T10:30:05Z",
        "git_branch": "main",
        "message": {"role": "assistant", "content": [...]},
        "is_context_compaction": false,
        "is_meta": false
      }
    ],
    "total": 42,
    "limit": 50,
    "offset": 0,
    "has_more": false,
    "query_time_ms": 12.5
  }
}
```

**Message Fields:**

| Field | Description |
|-------|-------------|
| `is_context_compaction` | `true` when this is an auto-generated message created by Claude Code when the context window was maxed out |
| `is_meta` | `true` for system-generated metadata messages (e.g., command caveats) |

#### `workspace/session/watch` - Start watching for live updates

**Starts watching a session file for real-time message updates.** When new messages are added to the session file, the server emits `claude_message` events. Only one session can be watched at a time.

```json
// Request
{"jsonrpc": "2.0", "id": 40, "method": "workspace/session/watch", "params": {
  "workspace_id": "ws-abc123",
  "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 40,
  "result": {
    "status": "watching",
    "watching": true,
    "workspace_id": "ws-abc123",
    "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
  }
}
```

**How it works:**
1. Call `workspace/session/watch` with the workspace and session ID
2. The server starts monitoring the session file for changes
3. When new messages are appended, you receive `claude_message` events
4. Call `workspace/session/unwatch` when done, or watch a different session

#### `workspace/session/unwatch` - Stop watching session

**Stops watching the currently watched session.** No more `claude_message` events will be sent for that session.

```json
// Request
{"jsonrpc": "2.0", "id": 41, "method": "workspace/session/unwatch", "params": {}}

// Response
{
  "jsonrpc": "2.0",
  "id": 41,
  "result": {
    "status": "unwatched",
    "watching": false,
    "workspace_id": "ws-abc123",
    "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
  }
}
```

#### `workspace/session/activate` - Set active session for a workspace

**Sets the active session that this device is viewing.** This is tracked server-side and reflected in `workspace/list` responses.

**Note:** This is automatically called when you:
- Start a new session with `session/start`
- Start watching a session with `workspace/session/watch`

Use this method when you want to explicitly switch active sessions without starting a watch.

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 42,
  "method": "workspace/session/activate",
  "params": {
    "workspace_id": "ws-abc123",
    "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 42,
  "result": {
    "success": true,
    "workspace_id": "ws-abc123",
    "session_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "message": "Session activated"
  }
}
```

---

### Session History & Live Streaming Flow

**Recommended pattern for viewing Claude sessions:**

```
┌─────────────────────────────────────────────────────────────┐
│  1. Get session list with workspace/session/history          │
│     → Returns list of historical sessions for workspace      │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  2. User selects a session to view                           │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  3. Load existing messages with workspace/session/messages   │
│     → Returns paginated messages from session file           │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  4. Start live watching with workspace/session/watch         │
│     → Receive claude_message events for new messages         │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  5. When user leaves, call workspace/session/unwatch         │
└─────────────────────────────────────────────────────────────┘
```

**Swift Example:**

```swift
// View a historical session with live updates
func viewSession(workspaceId: String, sessionId: String) async throws {
    // 1. Load existing messages
    let messages: SessionMessagesResult = try await client.rpc(
        "workspace/session/messages",
        params: [
            "workspace_id": workspaceId,
            "session_id": sessionId,
            "order": "asc"
        ]
    )

    // 2. Display messages in UI
    updateUI(with: messages.messages)

    // 3. Start watching for new messages
    try await client.rpc(
        "workspace/session/watch",
        params: [
            "workspace_id": workspaceId,
            "session_id": sessionId
        ]
    )

    // 4. Handle claude_message events in your event handler
    // New messages will arrive as events and can be appended to the UI
}

// When user leaves the session view
func leaveSessionView() async throws {
    try await client.rpc("workspace/session/unwatch", params: [:])
}
```

---

### Multi-Device Session Awareness Methods

When multiple iOS devices are viewing the same Claude session, each device can notify the server about which session they're focused on, enabling real-time notifications when other devices join or leave.

#### `client/session/focus` - Notify server of session focus

Notify the server which session this device is currently viewing. Other devices viewing the same session will be notified.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "method": "client/session/focus",
  "params": {
    "workspace_id": "ws-abc123",
    "session_id": "session-456"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "result": {
    "workspace_id": "ws-abc123",
    "session_id": "session-456",
    "other_viewers": ["device-uuid-2"],
    "viewer_count": 2,
    "success": true
  }
}
```

**Parameters:**
- `workspace_id` (string, required): The workspace ID containing the session
- `session_id` (string, required): The session ID to focus on

**Response Fields:**
- `workspace_id`: The workspace ID
- `session_id`: The session ID being focused on
- `other_viewers`: Array of client UUIDs currently viewing the same session
- `viewer_count`: Total number of devices viewing this session (including caller)
- `success`: Whether the operation succeeded

**Typical Flow:**
```swift
// When user taps on a session in the list
func selectSession(workspaceId: String, sessionId: String) async throws {
    // Notify server about focus change
    let result: FocusChangeResult = try await client.rpc(
        "client/session/focus",
        params: [
            "workspace_id": workspaceId,
            "session_id": sessionId
        ]
    )

    // Update UI with viewer count
    updateViewerCount(result.viewerCount)

    // Show who else is viewing
    if !result.otherViewers.isEmpty {
        showNotification("Other devices are viewing this session")
    }
}

struct FocusChangeResult: Codable {
    let workspaceId: String
    let sessionId: String
    let otherViewers: [String]
    let viewerCount: Int
    let success: Bool
}
```

#### Event: `session_joined` - Device joined session

**Emitted when:** Another device joins a session you're currently viewing

**Event:**
```json
{
  "event": "session_joined",
  "timestamp": "2025-12-25T10:30:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "session-456",
  "payload": {
    "joining_client_id": "device-uuid-1",
    "other_viewers": ["device-uuid-2"],
    "viewer_count": 2
  }
}
```

**Handling in Swift:**
```swift
func handleSessionJoined(_ event: Event) {
    guard event.event == "session_joined",
          let payload = event.payload as? SessionJoinedPayload else {
        return
    }

    // Only notify if this is for the session user is viewing
    guard payload.sessionId == currentSessionId else { return }

    // Show notification
    showNotification(
        title: "Collaborator Joined",
        message: "Another device is now viewing this session"
    )

    // Update viewer count badge
    updateViewerCount(payload.viewerCount)
}

struct SessionJoinedPayload: Codable {
    let joiningClientId: String
    let otherViewers: [String]
    let viewerCount: Int
}
```

#### Event: `session_left` - Device left session

**Emitted when:** A device leaves a session other devices are viewing

**Event:**
```json
{
  "event": "session_left",
  "timestamp": "2025-12-25T10:31:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "session-456",
  "payload": {
    "leaving_client_id": "device-uuid-1",
    "remaining_viewers": ["device-uuid-3"],
    "viewer_count": 1
  }
}
```

**Handling in Swift:**
```swift
func handleSessionLeft(_ event: Event) {
    guard event.event == "session_left",
          let payload = event.payload as? SessionLeftPayload else {
        return
    }

    // Only handle if this is for the session user is viewing
    guard payload.sessionId == currentSessionId else { return }

    // Update viewer count
    updateViewerCount(payload.viewerCount)

    // Optionally show notification
    if payload.viewerCount <= 1 {
        // Last viewer left, hide collaborator UI
        hideCollaboratorUI()
    }
}

struct SessionLeftPayload: Codable {
    let leavingClientId: String
    let remainingViewers: [String]
    let viewerCount: Int
}
```

---

### Subscription Methods (Event Filtering)

By default, clients receive **all events**. Use these methods to filter events by workspace.

#### `workspace/subscribe` - Subscribe to workspace events

```json
// Request
{"jsonrpc": "2.0", "id": 20, "method": "workspace/subscribe", "params": {
  "workspace_id": "ws-abc123"
}}

// Response
{
  "jsonrpc": "2.0",
  "id": 20,
  "result": {
    "success": true,
    "workspace_id": "ws-abc123",
    "subscribed": ["ws-abc123"]
  }
}
```

#### `workspace/unsubscribe` - Unsubscribe from workspace

```json
{"jsonrpc": "2.0", "id": 21, "method": "workspace/unsubscribe", "params": {
  "workspace_id": "ws-abc123"
}}
```

#### `workspace/subscriptions` - List current subscriptions

```json
// Request
{"jsonrpc": "2.0", "id": 22, "method": "workspace/subscriptions", "params": {}}

// Response
{
  "jsonrpc": "2.0",
  "id": 22,
  "result": {
    "workspaces": ["ws-abc123", "ws-def456"],
    "is_filtering": true,
    "count": 2
  }
}
```

#### `workspace/subscribeAll` - Receive all events (reset filter)

```json
{"jsonrpc": "2.0", "id": 23, "method": "workspace/subscribeAll", "params": {}}
```

---

### Git Methods

> **All git methods require `workspace_id`** to operate on the correct repository.

#### `workspace/git/status` - Get git status

```json
{"jsonrpc": "2.0", "id": 30, "method": "workspace/git/status", "params": {
  "workspace_id": "ws-abc123"
}}

// Response
{
  "branch": "main",
  "upstream": "origin/main",
  "ahead": 2,
  "behind": 0,
  "staged": [{"path": "src/app.ts", "status": "M", "additions": 10, "deletions": 2}],
  "unstaged": [{"path": "README.md", "status": "M"}],
  "untracked": [{"path": "new-file.ts", "status": "?"}],
  "conflicted": [],
  "repo_name": "my-project",
  "repo_root": "/Users/dev/my-project"
}
```

#### `workspace/git/diff` - Get git diff

```json
{"jsonrpc": "2.0", "id": 31, "method": "workspace/git/diff", "params": {
  "workspace_id": "ws-abc123",
  "path": "src/app.ts"
}}

// Response
{
  "path": "src/app.ts",
  "diff": "diff --git a/src/app.ts b/src/app.ts\n...",
  "is_staged": false,
  "is_new": false
}
```

#### `workspace/git/stage` - Stage files

```json
{"jsonrpc": "2.0", "id": 32, "method": "workspace/git/stage", "params": {
  "workspace_id": "ws-abc123",
  "paths": ["src/app.ts", "src/utils.ts"]
}}

// Response
{"success": true, "staged": ["src/app.ts", "src/utils.ts"]}
```

#### `workspace/git/unstage` - Unstage files

```json
{"jsonrpc": "2.0", "id": 33, "method": "workspace/git/unstage", "params": {
  "workspace_id": "ws-abc123",
  "paths": ["src/app.ts"]
}}

// Response
{"success": true, "unstaged": ["src/app.ts"]}
```

#### `workspace/git/discard` - Discard changes

```json
{"jsonrpc": "2.0", "id": 34, "method": "workspace/git/discard", "params": {
  "workspace_id": "ws-abc123",
  "paths": ["src/app.ts"]
}}

// Response
{"success": true, "discarded": ["src/app.ts"]}
```

#### `workspace/git/commit` - Commit staged changes

```json
{"jsonrpc": "2.0", "id": 35, "method": "workspace/git/commit", "params": {
  "workspace_id": "ws-abc123",
  "message": "feat: add new feature",
  "push": true  // Optional: push after commit
}}

// Response
{
  "success": true,
  "sha": "abc1234",
  "message": "Committed and pushed to origin/main",
  "files_committed": 3,
  "pushed": true
}
```

#### `workspace/git/push` - Push commits

```json
{"jsonrpc": "2.0", "id": 36, "method": "workspace/git/push", "params": {
  "workspace_id": "ws-abc123",
  "force": false,
  "set_upstream": false,
  "remote": "origin",  // Optional
  "branch": "main"     // Optional
}}

// Response
{"success": true, "message": "Pushed to origin/main", "commits_pushed": 2}
```

#### `workspace/git/pull` - Pull changes

```json
{"jsonrpc": "2.0", "id": 37, "method": "workspace/git/pull", "params": {
  "workspace_id": "ws-abc123",
  "rebase": false
}}

// Response
{"success": true, "message": "Pulled from origin/main"}

// Response with conflicts
{
  "success": false,
  "error": "Merge conflict",
  "conflicted_files": ["src/app.ts", "README.md"]
}
```

#### `workspace/git/branches` - List branches

```json
{"jsonrpc": "2.0", "id": 38, "method": "workspace/git/branches", "params": {
  "workspace_id": "ws-abc123"
}}

// Response
{
  "current": "main",
  "upstream": "origin/main",
  "ahead": 0,
  "behind": 0,
  "branches": [
    {"name": "main", "is_current": true, "is_remote": false, "upstream": "origin/main"},
    {"name": "feature/new", "is_current": false, "is_remote": false},
    {"name": "origin/main", "is_current": false, "is_remote": true}
  ]
}
```

#### `workspace/git/checkout` - Checkout branch

```json
{"jsonrpc": "2.0", "id": 39, "method": "workspace/git/checkout", "params": {
  "workspace_id": "ws-abc123",
  "branch": "feature/new",
  "create": false  // Set true to create new branch
}}

// Response
{"success": true, "branch": "feature/new", "message": "Switched to branch 'feature/new'"}

// Response with error (uncommitted changes)
{"success": false, "error": "Cannot switch branches: You have unstaged changes"}
```

---

## Events

All events now include `workspace_id` and `session_id` for filtering.

### Event Structure

```json
{
  "event": "claude_message",
  "timestamp": "2024-12-24T10:35:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "sess-xyz789",
  "payload": { ... }
}
```

### Event Types

| Event | Description |
|-------|-------------|
| `claude_message` | Claude response content |
| `claude_status` | Claude state change (running, idle, error) |
| `claude_waiting` | Waiting for user input (question) |
| `claude_permission` | Permission request |
| `claude_session_info` | Session ID captured |
| `claude_log` | Raw Claude CLI output (stream-json format) |
| `heartbeat` | Connection keepalive |
| `git_status_changed` | Git state changed (staging, commits, branches) |
| `file_changed` | File created, modified, deleted, or renamed |

### `claude_log` Event

**Raw Claude CLI output in stream-json format.** This event contains the raw output from Claude CLI, useful for debugging or advanced processing. All events include `workspace_id` and `session_id` for filtering.

```json
{
  "event": "claude_log",
  "timestamp": "2024-12-24T10:35:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "sess-xyz789",
  "payload": {
    "session_id": "claude-session-abc",
    "type": "assistant",
    "message": {
      "id": "msg_01...",
      "type": "message",
      "role": "assistant",
      "content": [...],
      "model": "claude-sonnet-4-20250514",
      "stop_reason": null,
      "usage": {"input_tokens": 100, "output_tokens": 50}
    }
  }
}
```

**Key Fields:**
| Field | Description |
|-------|-------------|
| `workspace_id` | The workspace this event belongs to |
| `session_id` | The cdev session ID (from `session/start`) |
| `payload.session_id` | The Claude CLI session ID (used for `--resume`) |
| `payload.type` | Message type: `user`, `assistant`, `result` |
| `payload.message` | Raw Claude API message structure |

**Filtering Events by Session:**

```swift
func handleEvent(_ event: Event) {
    // Filter events by session
    guard event.sessionId == currentSessionId else {
        return // Ignore events from other sessions
    }

    switch event.event {
    case "claude_log":
        handleClaudeLog(event.payload)
    case "claude_message":
        handleClaudeMessage(event.payload)
    // ... other events
    }
}
```

### `claude_message` Event

```json
{
  "event": "claude_message",
  "timestamp": "2024-12-24T10:35:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "sess-xyz789",
  "payload": {
    "session_id": "claude-session-abc",
    "type": "assistant",
    "role": "assistant",
    "content": [
      {
        "type": "text",
        "text": "Here's how to refactor..."
      },
      {
        "type": "tool_use",
        "id": "tool-123",
        "name": "Edit",
        "input": "{\"file_path\": \"...\"}"
      }
    ]
  }
}
```

### `claude_permission` Event

```json
{
  "event": "claude_permission",
  "timestamp": "2024-12-24T10:35:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "sess-xyz789",
  "payload": {
    "tool_use_id": "tool-123",
    "tool_name": "Bash",
    "input": "{\"command\": \"npm install\"}",
    "description": "Run command: npm install"
  }
}
```

### `claude_waiting` Event (Question)

```json
{
  "event": "claude_waiting",
  "timestamp": "2024-12-24T10:35:00Z",
  "workspace_id": "ws-abc123",
  "session_id": "sess-xyz789",
  "payload": {
    "tool_use_id": "tool-456",
    "tool_name": "AskUserQuestion",
    "input": "{\"question\": \"Which database should I use?\", ...}"
  }
}
```

### `git_status_changed` Event (Real-Time Git Watcher)

**Emitted automatically when git state changes.** The server monitors the `.git` directory for changes, enabling real-time updates when users run git commands from terminal, IDE, or tools like SourceTree.

**Triggers:**
- Files staged/unstaged (`git add`, `git reset`)
- Commits created (`git commit`)
- Branches switched (`git checkout`, `git switch`)
- Remote changes fetched/pulled (`git fetch`, `git pull`)
- Merges/rebases (`git merge`, `git rebase`)

**Key Features:**
- **IDE-safe**: Only watches `.git` directory (won't conflict with VS Code, IntelliJ, SourceTree)
- **Debounced**: 500ms debounce + 1 second minimum interval to prevent spam
- **No manual refresh needed**: iOS app receives updates automatically

```json
{
  "event": "git_status_changed",
  "timestamp": "2024-12-24T10:35:00Z",
  "workspace_id": "ws-abc123",
  "payload": {
    "branch": "main",
    "staged_count": 3,
    "unstaged_count": 1,
    "untracked_count": 2,
    "changed_files": ["src/app.ts", "src/utils.ts"]
  }
}
```

**Payload Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `branch` | string | Current branch name |
| `staged_count` | number | Number of staged files |
| `unstaged_count` | number | Number of unstaged modified files |
| `untracked_count` | number | Number of untracked files |
| `changed_files` | array | List of all changed file paths |

**Swift Example:**

```swift
func handleGitStatusChanged(_ event: Event) {
    guard event.event == "git_status_changed",
          let payload = event.payload as? GitStatusPayload else {
        return
    }

    // Update git status badge/indicator
    updateBranchLabel(payload.branch)
    updateStagedBadge(count: payload.stagedCount)
    updateUnstagedBadge(count: payload.unstagedCount)

    // Optionally show toast for significant changes
    if payload.stagedCount > 0 {
        showToast("Files staged: \(payload.stagedCount)")
    }
}

struct GitStatusPayload: Codable {
    let branch: String
    let stagedCount: Int
    let unstagedCount: Int
    let untrackedCount: Int
    let changedFiles: [String]

    enum CodingKeys: String, CodingKey {
        case branch
        case stagedCount = "staged_count"
        case unstagedCount = "unstaged_count"
        case untrackedCount = "untracked_count"
        case changedFiles = "changed_files"
    }
}
```

**Use Cases:**
1. **Real-time git status badge**: Update UI when user stages files in terminal
2. **Branch indicator**: Show current branch and update when switched
3. **Commit notification**: Detect when commits are made outside the app
4. **Pull/fetch detection**: Know when remote changes arrive

---

## Multi-Device Support

Multiple devices can connect to the same workspace simultaneously.

### Recommended Flow

```
┌─────────────────────────────────────────────────────────────┐
│                     Device Connects                         │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  1. Call workspace/list to get available workspaces         │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  2. Call workspace/subscribe for workspaces of interest     │
│     (Optional - reduces bandwidth)                          │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  3. For each active session, call session/state to sync     │
└─────────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────────┐
│  4. Listen for events and update UI                         │
└─────────────────────────────────────────────────────────────┘
```

### Reconnection Logic

```swift
func reconnect() async {
    // 1. Connect to WebSocket
    await connectWebSocket()

    // 2. Get workspace list
    let workspaces = await rpc("workspace/list")

    // 3. Subscribe to relevant workspaces (optional)
    for ws in selectedWorkspaces {
        await rpc("workspace/subscribe", ["workspace_id": ws.id])
    }

    // 4. Sync state for active sessions
    for ws in workspaces where ws.hasActiveSession {
        for session in ws.sessions where session.status == "running" {
            let state = await rpc("session/state", ["session_id": session.id])
            updateUIWithState(state)
        }
    }
}
```

---

## Swift Code Examples

### WebSocket Manager

```swift
import Foundation

class CDEVClient: ObservableObject {
    private var webSocket: URLSessionWebSocketTask?
    private var requestId = 0
    private var pendingRequests: [Int: CheckedContinuation<Any, Error>] = [:]

    let serverURL = URL(string: "ws://127.0.0.1:8766/ws")!

    func connect() {
        let session = URLSession(configuration: .default)
        webSocket = session.webSocketTask(with: serverURL)
        webSocket?.resume()
        receiveMessages()
    }

    func rpc<T: Decodable>(_ method: String, params: [String: Any] = [:]) async throws -> T {
        requestId += 1
        let id = requestId

        let request: [String: Any] = [
            "jsonrpc": "2.0",
            "id": id,
            "method": method,
            "params": params
        ]

        let data = try JSONSerialization.data(withJSONObject: request)
        try await webSocket?.send(.data(data))

        return try await withCheckedThrowingContinuation { continuation in
            pendingRequests[id] = continuation as! CheckedContinuation<Any, Error>
        } as! T
    }

    private func receiveMessages() {
        webSocket?.receive { [weak self] result in
            switch result {
            case .success(let message):
                self?.handleMessage(message)
                self?.receiveMessages()
            case .failure(let error):
                print("WebSocket error: \(error)")
            }
        }
    }

    private func handleMessage(_ message: URLSessionWebSocketTask.Message) {
        guard case .data(let data) = message,
              let json = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            return
        }

        // Handle response
        if let id = json["id"] as? Int,
           let continuation = pendingRequests.removeValue(forKey: id) {
            if let result = json["result"] {
                continuation.resume(returning: result)
            } else if let error = json["error"] as? [String: Any] {
                continuation.resume(throwing: NSError(domain: "RPC", code: error["code"] as? Int ?? -1))
            }
            return
        }

        // Handle event
        if let event = json["event"] as? String {
            handleEvent(event, payload: json["payload"],
                       workspaceId: json["workspace_id"] as? String,
                       sessionId: json["session_id"] as? String)
        }
    }

    private func handleEvent(_ event: String, payload: Any?, workspaceId: String?, sessionId: String?) {
        // Dispatch to appropriate handler
        switch event {
        case "claude_message":
            // Update chat UI
            break
        case "claude_permission":
            // Show permission dialog
            break
        case "claude_waiting":
            // Show question input
            break
        default:
            break
        }
    }
}
```

### Usage Example

```swift
let client = CDEVClient()
client.connect()

// List workspaces
let workspaces: WorkspaceListResponse = try await client.rpc("workspace/list")

// Start a session
let session: SessionInfo = try await client.rpc("session/start", params: [
    "workspace_id": "ws-abc123"
])

// Send a prompt
try await client.rpc("session/send", params: [
    "session_id": session.id,
    "prompt": "Help me fix this bug",
    "mode": "new"
])

// Respond to permission
try await client.rpc("session/respond", params: [
    "session_id": session.id,
    "type": "permission",
    "response": "yes"
])
```

---

## Error Handling

### JSON-RPC Error Codes

| Code | Description |
|------|-------------|
| -32700 | Parse error |
| -32600 | Invalid request |
| -32601 | Method not found |
| -32602 | Invalid params |
| -32603 | Internal error |

### Error Response

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32602,
    "message": "workspace_id is required"
  }
}
```

---

## Testing

### Health Check

```bash
curl http://127.0.0.1:8766/health
```

### WebSocket Test (wscat)

```bash
# Install wscat
npm install -g wscat

# Connect
wscat -c ws://127.0.0.1:8766/ws

# Send request
{"jsonrpc":"2.0","id":1,"method":"workspace/list","params":{}}
```

### OpenRPC Schema

Get the full API schema:

```bash
curl http://127.0.0.1:8766/api/rpc/discover | jq .
```

---

## Migration from v1.x

> **CRITICAL: Do NOT use the legacy `agent/*` API for multi-workspace support!**

### API Path Behavior

| API | Config Source | Path Used |
|-----|---------------|-----------|
| `agent/run` (LEGACY) | `config.yaml` | `repository.path` (single repo) |
| `session/start` (NEW) | `workspaces.yaml` | Workspace's `path` (multi-repo) |

**Problem:** If you call `agent/run`, it ignores the workspace selection and uses `config.yaml`'s `repository.path`. This is a legacy single-repo API.

**Solution:** Always use the session-based API for multi-workspace:

```
❌ WRONG: agent/run → Uses config.yaml path (ignores workspace selection)
✅ CORRECT: session/start + session/send → Uses selected workspace path
```

### API Migration Table

| Old API (DEPRECATED) | New API (USE THIS) |
|---------|---------|
| `agent/run` | `session/start` + `session/send` |
| `agent/stop` | `session/stop` |
| `agent/respond` | `session/respond` |
| Port 8765 | Port 8766 |
| Single workspace | Multi-workspace |

### Correct Multi-Workspace Flow

```swift
// 1. Get list of workspaces
let workspaces = await rpc("workspace/list")

// 2. User selects "cdev" workspace (id: "ws-abc123", path: "/Users/dev/cdev")
let selectedWorkspace = workspaces.first { $0.name == "cdev" }

// 3. Start session for THAT workspace
let session = await rpc("session/start", [
    "workspace_id": selectedWorkspace.id  // This ensures correct path is used
])

// 4. Send prompts to the session
await rpc("session/send", [
    "session_id": session.id,
    "prompt": "Help me fix the bug",
    "mode": "new"
])
```

### Key Changes

1. **Session-based**: Start a session first with `workspace_id`, then send prompts
2. **Multi-workspace**: One server manages multiple repositories
3. **Workspace path**: The session uses the workspace's path, not `config.yaml`
4. **Event context**: All events include `workspace_id` and `session_id`
5. **Subscription filtering**: Optional workspace-based event filtering

### Legacy API Deprecation Notice

The following APIs are **DEPRECATED** and will be removed in a future version:

- `agent/run` → Use `session/start` + `session/send`
- `agent/stop` → Use `session/stop`
- `agent/respond` → Use `session/respond`
- `agent/status` → Use `session/state` or `session/info`

These legacy APIs operate on the single-repo `config.yaml` path, not the selected workspace.

---

## Support

- **Swagger UI**: http://127.0.0.1:8766/swagger/
- **OpenRPC Schema**: http://127.0.0.1:8766/api/rpc/discover
- **Server Logs**: `~/.cdev/server.log`
