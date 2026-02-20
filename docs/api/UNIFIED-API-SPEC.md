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

All git methods require a `workspace_id` parameter to specify which workspace to operate on.

#### `git/status`
Get git repository status.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |

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
| workspace_id | string | Yes | Workspace ID |
| path | string | No | File path relative to repo root (omit for all files) |
| staged | boolean | No | Get staged diff (default: false) |

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
| workspace_id | string | Yes | Workspace ID |
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
| workspace_id | string | Yes | Workspace ID |
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
| workspace_id | string | Yes | Workspace ID |
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
| workspace_id | string | Yes | Workspace ID |
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

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| force | boolean | No | Force push (default: false) |
| set_upstream | boolean | No | Set upstream tracking (default: false) |
| remote | string | No | Remote name (default: "origin") |
| branch | string | No | Branch name (default: current branch) |

**Result:**
```json
{
  "status": "pushed"
}
```

---

#### `git/pull`
Pull changes from remote repository.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| rebase | boolean | No | Use rebase instead of merge (default: false) |

**Result:**
```json
{
  "status": "pulled"
}
```

---

#### `git/branches`
List all git branches.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |

**Result:**
```json
{
  "branches": [
    {"name": "main", "current": true},
    {"name": "feature/auth", "current": false}
  ],
  "current": "main"
}

---

#### `git/checkout`
Checkout a branch.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| branch | string | Yes | Branch name to checkout |
| create | boolean | No | Create branch if it doesn't exist (default: false) |

**Result:**
```json
{
  "success": true,
  "branch": "feature/auth",
  "from_branch": "main",
  "message": "Switched to branch 'feature/auth'"
}
```

**Note:** When the branch changes, a `git_branch_changed` event is emitted.

---

#### `git/branch/delete`
Delete a git branch.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| branch | string | Yes | Branch name to delete |
| force | boolean | No | Force delete even if not fully merged (default: false) |

**Result:**
```json
{
  "success": true,
  "branch": "feature/old",
  "was_current": false,
  "message": "Deleted branch feature/old"
}
```

---

#### `git/fetch`
Fetch updates from remote repository.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| remote | string | No | Remote name (default: "origin") |
| prune | boolean | No | Prune deleted remote branches (default: false) |

**Result:**
```json
{
  "success": true,
  "remote": "origin",
  "message": "Fetched from origin"
}
```

---

#### `git/log`
Get commit history with optional graph layout for visualization.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| limit | number | No | Max commits to return (default: 50) |
| skip | number | No | Number of commits to skip (default: 0) |
| branch | string | No | Branch to get history for (default: current) |
| path | string | No | Filter by file path |
| graph | boolean | No | Include graph layout info for visualization (default: false) |

**Result:**
```json
{
  "commits": [
    {
      "sha": "abc123def456...",
      "short_sha": "abc123d",
      "author": "John Doe",
      "author_email": "john@example.com",
      "date": "2025-12-28T10:30:00Z",
      "message": "feat: add new feature",
      "parents": ["def789..."],
      "lane": 0,
      "merge_lanes": []
    }
  ],
  "total": 150,
  "has_more": true
}
```

**Graph Fields (when `graph: true`):**
| Field | Type | Description |
|-------|------|-------------|
| lane | number | Horizontal lane (0 = leftmost) for graph rendering |
| merge_lanes | number[] | Lanes that merge into this commit |

---

#### `git/stash`
Create a stash of current changes.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| message | string | No | Stash message |
| include_untracked | boolean | No | Include untracked files (default: false) |

**Result:**
```json
{
  "success": true,
  "message": "Saved working directory"
}
```

---

#### `git/stash/list`
List all stashes.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |

**Result:**
```json
{
  "stashes": [
    {
      "index": 0,
      "message": "WIP on main: abc123 feat: add feature",
      "branch": "main",
      "date": "2025-12-28T10:30:00Z"
    }
  ],
  "total": 1
}
```

---

#### `git/stash/apply`
Apply a stash without removing it.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| index | number | No | Stash index to apply (default: 0) |

**Result:**
```json
{
  "success": true,
  "message": "Applied stash@{0}"
}
```

---

#### `git/stash/pop`
Apply and remove a stash.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| index | number | No | Stash index to pop (default: 0) |

**Result:**
```json
{
  "success": true,
  "message": "Applied and removed stash@{0}"
}
```

---

#### `git/stash/drop`
Remove a stash without applying it.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| index | number | No | Stash index to drop (default: 0) |

**Result:**
```json
{
  "success": true,
  "message": "Dropped stash@{0}"
}
```

---

#### `git/merge`
Merge a branch into the current branch.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| branch | string | Yes | Branch to merge |
| no_ff | boolean | No | Create merge commit even if fast-forward possible (default: false) |
| message | string | No | Custom merge commit message |

**Result:**
```json
{
  "success": true,
  "merged_branch": "feature/auth",
  "fast_forward": false,
  "has_conflicts": false,
  "message": "Merge made by the 'ort' strategy"
}
```

**Conflict Result:**
```json
{
  "success": false,
  "has_conflicts": true,
  "conflicted_files": ["src/auth.go", "src/user.go"],
  "message": "Automatic merge failed; fix conflicts and commit"
}
```

---

#### `git/merge/abort`
Abort an in-progress merge.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |

**Result:**
```json
{
  "success": true,
  "message": "Merge aborted"
}
```

---

#### `git/init`
Initialize a new git repository.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| initial_branch | string | No | Initial branch name (default: "main") |
| initial_commit | boolean | No | Create initial empty commit (default: false) |
| commit_message | string | No | Initial commit message |

**Result:**
```json
{
  "success": true,
  "branch": "main",
  "has_initial_commit": true,
  "message": "Initialized empty Git repository"
}
```

---

#### `git/remote/add`
Add a remote repository.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| name | string | Yes | Remote name (e.g., "origin") |
| url | string | Yes | Remote URL |
| fetch | boolean | No | Fetch after adding (default: true) |

**Result:**
```json
{
  "success": true,
  "name": "origin",
  "url": "git@github.com:user/repo.git",
  "message": "Remote 'origin' added"
}
```

---

#### `git/remote/list`
List configured remotes.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |

**Result:**
```json
{
  "remotes": [
    {
      "name": "origin",
      "fetch_url": "git@github.com:user/repo.git",
      "push_url": "git@github.com:user/repo.git"
    }
  ]
}
```

---

#### `git/remote/remove`
Remove a remote.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| name | string | Yes | Remote name to remove |

**Result:**
```json
{
  "success": true,
  "name": "origin",
  "message": "Remote 'origin' removed"
}
```

---

#### `git/upstream/set`
Set upstream tracking branch.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |
| branch | string | Yes | Local branch name |
| upstream | string | Yes | Upstream branch (e.g., "origin/main") |

**Result:**
```json
{
  "success": true,
  "branch": "main",
  "upstream": "origin/main",
  "message": "Branch 'main' set up to track 'origin/main'"
}
```

---

#### `git/get_status`
Get comprehensive git status including workspace state.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| workspace_id | string | Yes | Workspace ID |

**Result:**
```json
{
  "is_git_repo": true,
  "has_commits": true,
  "state": "synced",
  "branch": "main",
  "upstream": "origin/main",
  "ahead": 0,
  "behind": 0,
  "staged": [
    {"path": "src/main.go", "status": "M", "additions": 10, "deletions": 5}
  ],
  "unstaged": [],
  "untracked": [],
  "conflicted": [],
  "has_conflicts": false,
  "remotes": [
    {"name": "origin", "fetch_url": "git@github.com:user/repo.git", "push_url": "git@github.com:user/repo.git"}
  ],
  "repo_name": "my-project",
  "repo_root": "/Users/dev/my-project"
}
```

**State Values:**
| State | Description |
|-------|-------------|
| `no_git` | Directory is not a git repository |
| `git_init` | Git initialized but no commits yet |
| `no_remote` | Has commits but no remote configured |
| `no_push` | Has remote but no upstream set or never pushed |
| `synced` | In sync with remote |
| `diverged` | Local and remote have diverged |
| `conflict` | Merge conflict in progress |

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

**Note:** Prefer `workspace/session/watch` for workspace-scoped streaming with explicit `agent_type`.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| session_id | string | Yes | Session ID to watch |
| agent_type | string | No | Agent type (optional, recommended for deterministic runtime routing) |

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

**Params:** None. Prefer `workspace/session/unwatch` with explicit `agent_type`.

**Result:**
```json
{
  "status": "unwatched",
  "watching": false
}
```

---

#### Runtime-Scoped Session Control Methods
The following methods are runtime-scoped and accept optional `agent_type`:

- `session/start`
- `session/stop`
- `session/send`
- `session/input`
- `session/respond`

Current behavior:

- If `agent_type` is omitted, cdev defaults to `"claude"` (legacy behavior).
- **Recommended:** always send `agent_type` explicitly to avoid accidental Claude fallbacks.
- `agent_type="claude"` is supported for these methods.
- `agent_type="codex"` is supported for interactive runtime control using Codex CLI (`codex`) with PTY-backed input routing.
- Codex history/realtime methods (`session/list|get|messages|elements|watch`) remain available via `agent_type="codex"`.

`session/start` may return status values: `attached`, `started`, `existing`, `not_found`.
For Codex, `started` can return a temporary session ID (`codex-temp-...`) until the first session file is created, after which the server emits `session_id_resolved` (with `agent_type`) to map the real ID.

---

### Workspace Methods

#### `workspace/list`
List all configured workspaces with optional git status information.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| include_git | boolean | No | Include git status for each workspace (default: false) |
| git_limit | integer | No | Limit git status fetching to first N workspaces (0 = all) |

**Example:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "workspace/list",
  "params": {
    "include_git": true,
    "git_limit": 10
  }
}
```

**Result:**
```json
{
  "workspaces": [
    {
      "id": "ws-abc12345",
      "name": "my-project",
      "path": "/Users/dev/Projects/my-project",
      "port": 8767,
      "auto_start": true,
      "created_at": "2025-12-20T10:30:00Z",
      "is_git_repo": true,
      "git_state": "synced",
      "git": {
        "initialized": true,
        "branch": "main",
        "has_remotes": true,
        "ahead": 0,
        "behind": 0,
        "staged_count": 0,
        "unstaged_count": 2,
        "untracked_count": 1,
        "has_conflicts": false,
        "state": "synced"
      },
      "sessions": []
    }
  ],
  "count": 1
}
```

**Workspace Fields:**
| Field | Type | Description |
|-------|------|-------------|
| id | string | Unique workspace identifier (e.g., `ws-abc12345`) |
| name | string | Workspace display name |
| path | string | Absolute filesystem path |
| port | integer | Assigned port number |
| auto_start | boolean | Whether workspace starts automatically |
| created_at | string | ISO 8601 timestamp of creation |
| is_git_repo | boolean | Whether path is a git repository (only when `include_git: true`) |
| git_state | string | Git state enum value (only when `include_git: true`) |
| git | object | Detailed git info (only when `include_git: true`) |
| sessions | array | Active sessions in this workspace |

**Git Object Fields (when `include_git: true`):**
| Field | Type | Description |
|-------|------|-------------|
| initialized | boolean | Whether git is initialized |
| branch | string \| null | Current branch name |
| has_remotes | boolean | Whether remote(s) are configured |
| ahead | integer | Commits ahead of upstream |
| behind | integer | Commits behind upstream |
| staged_count | integer | Number of staged files |
| unstaged_count | integer | Number of unstaged modified files |
| untracked_count | integer | Number of untracked files |
| has_conflicts | boolean | Whether merge conflicts exist |
| state | string | Git state enum value |

**Git State Values:**
| Value | Description | Typical UI |
|-------|-------------|------------|
| `no_git` | Not a git repository | Gray / No icon |
| `git_init` | Git initialized, no commits yet | Gray / Init icon |
| `no_remote` | Has commits, no remote configured | Yellow / Local icon |
| `no_push` | Has remote, never pushed | Yellow / Unpushed icon |
| `synced` | In sync with remote | Green / Check icon |
| `diverged` | Local and remote have diverged | Orange / Warning icon |
| `conflict` | Has merge conflicts | Red / Conflict icon |

**Performance Notes:**
- Git status fetching uses parallel execution with max 10 concurrent operations
- Expected latency: ~50-150ms per workspace when fetched in parallel
- For 50 workspaces: ~500-800ms total (vs ~7.5s sequential)
- Use `git_limit` to fetch only visible workspaces for better UX

---

### Repository Methods

#### `repository/index/status`
Get the current status of the repository index.

**Params:** None

**Result:**
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

#### `repository/search`
Search for files using various matching strategies.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| query | string | Yes | Search query string |
| mode | string | No | `fuzzy` (default), `exact`, `prefix`, `extension` |
| limit | number | No | Max results (default: 50, max: 500) |
| offset | number | No | Pagination offset (default: 0) |
| extensions | string[] | No | Filter by file extensions |
| exclude_binaries | boolean | No | Exclude binary files (default: true) |
| git_tracked_only | boolean | No | Only git-tracked files (default: false) |
| min_size | number | No | Minimum file size in bytes |
| max_size | number | No | Maximum file size in bytes |

**Example:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "repository/search",
  "params": {
    "query": "index",
    "mode": "fuzzy",
    "limit": 20,
    "extensions": ["ts", "js"]
  }
}
```

**Result:**
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
      "git_tracked": true,
      "match_score": 0.95
    }
  ],
  "total": 10,
  "elapsed_ms": 2
}
```

---

#### `repository/files/list`
List files in a directory with pagination and sorting.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| directory | string | No | Directory path (empty for root) |
| recursive | boolean | No | Include subdirectories (default: false) |
| limit | number | No | Max files (default: 100, max: 1000) |
| offset | number | No | Pagination offset (default: 0) |
| sort_by | string | No | `name` (default), `size`, `modified`, `path` |
| sort_order | string | No | `asc` (default), `desc` |
| extensions | string[] | No | Filter by extensions |
| min_size | number | No | Minimum file size in bytes |
| max_size | number | No | Maximum file size in bytes |

**Result:**
```json
{
  "directory": "src",
  "files": [
    {
      "path": "src/index.ts",
      "name": "index.ts",
      "directory": "src",
      "extension": "ts",
      "size_bytes": 8097,
      "modified_at": "2025-10-21T11:17:45Z",
      "is_binary": false,
      "is_sensitive": false,
      "git_tracked": true
    }
  ],
  "directories": [
    {
      "path": "src/components",
      "name": "components",
      "file_count": 15,
      "total_size_bytes": 45000
    }
  ],
  "total_files": 47,
  "total_directories": 5,
  "pagination": {
    "limit": 100,
    "offset": 0,
    "has_more": false
  }
}
```

---

#### `repository/files/tree`
Get a hierarchical directory tree structure.

**Params:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| path | string | No | Root path (empty for repository root) |
| depth | number | No | Max depth (default: 2, max: 10) |

**Result:**
```json
{
  "path": "",
  "name": "my-project",
  "type": "directory",
  "children": [
    {
      "path": "src",
      "name": "src",
      "type": "directory",
      "file_count": 47,
      "total_size_bytes": 324277,
      "children": [
        {
          "path": "src/index.ts",
          "name": "index.ts",
          "type": "file",
          "size_bytes": 8097,
          "extension": "ts"
        }
      ]
    }
  ]
}
```

---

#### `repository/stats`
Get aggregate statistics about the repository.

**Params:** None

**Result:**
```json
{
  "total_files": 277,
  "total_directories": 59,
  "total_size_bytes": 18940805,
  "files_by_extension": {
    "ts": 119,
    "json": 20,
    "md": 5
  },
  "largest_files": [
    {
      "path": "package-lock.json",
      "name": "package-lock.json",
      "size_bytes": 480928
    }
  ],
  "git_tracked_files": 250,
  "git_ignored_files": 27,
  "binary_files": 53,
  "sensitive_files": 15
}
```

---

#### `repository/index/rebuild`
Trigger a full re-index of the repository (runs in background).

**Params:** None

**Result:**
```json
{
  "status": "started",
  "message": "Repository index rebuild started in background"
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
      "list": true,
      "tree": true,
      "stats": true,
      "rebuild": true
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

**Runtime Capability Registry (contract extension):**
- The runtime contract is defined in `docs/api/RUNTIME-CAPABILITY-REGISTRY.md`.
- Contract field location: `result.capabilities.runtimeRegistry`.
- This is an additive extension for server-driven runtime behavior.
- Backend now exposes this payload; clients should still keep legacy fallback for backward compatibility.

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

### `event/git_branch_changed`
Git branch changed (emitted after `git/checkout`).

```json
{
  "jsonrpc": "2.0",
  "method": "event/git_branch_changed",
  "params": {
    "workspace_id": "ws-abc123",
    "from_branch": "main",
    "to_branch": "feature/auth",
    "session_id": ""
  }
}
```

**Params:**
| Field | Type | Description |
|-------|------|-------------|
| workspace_id | string | Workspace where branch changed |
| from_branch | string | Previous branch name |
| to_branch | string | New branch name |
| session_id | string | Session ID (if applicable) |

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
