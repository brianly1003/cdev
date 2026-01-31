# iOS Integration Guide: Claude Code Hooks

This document details what cdev-ios needs to implement to fully integrate the new Claude Code hooks feature, which enables capturing events from external Claude sessions (VS Code, Cursor, terminal).

## Overview

cdev now auto-installs hooks into Claude Code's `settings.json` on startup. These hooks forward events from **any** Claude Code session (not just cdev-managed ones) to the cdev server. This allows the iOS app to:

1. **Monitor** Claude activity happening in VS Code, Cursor, or any terminal
2. **Approve/Deny permissions** from mobile for external Claude sessions
3. **"Allow for Session"** - approve patterns once and auto-approve matching requests

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                     EXTERNAL CLAUDE PERMISSION FLOW                          │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  VS Code / Cursor / Terminal                                                 │
│       │                                                                      │
│       │ PreToolUse Hook (blocking)                                           │
│       ▼                                                                      │
│  ┌─────────────────┐         HTTP POST          ┌──────────────────────┐    │
│  │ permission.sh   │ ──────────────────────────►│  cdev server         │    │
│  │ (waits for      │                            │                      │    │
│  │  response)      │◄────────────────────────── │  1. Check memory     │    │
│  └─────────────────┘    {decision: "allow"}     │  2. Forward to iOS   │    │
│       │                                         │  3. Wait for response│    │
│       │                                         └──────────┬───────────┘    │
│       ▼                                                    │                │
│  Returns to Claude:                                        │ pty_permission │
│  - allow → tool runs                                       ▼                │
│  - deny → tool blocked                          ┌──────────────────────┐    │
│  - ask → desktop prompt                         │       iOS App        │    │
│                                                 │  ┌────────────────┐  │    │
│                                                 │  │ Permission UI  │  │    │
│                                                 │  │ ─────────────  │  │    │
│                                                 │  │ Allow Once     │  │    │
│                                                 │  │ Allow Session  │◄─┼── Stores pattern
│                                                 │  │ Deny           │  │    │
│                                                 │  └────────────────┘  │    │
│                                                 └──────────┬───────────┘    │
│                                                            │                │
│                                                            │ permission/    │
│                                                            │ respond RPC    │
│                                                            ▼                │
│                                         Response flows back to hook script  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## New Event Types

Five WebSocket event types are used for hook integration:

| Event Type | Description |
|------------|-------------|
| `claude_hook_session` | External Claude session started |
| `claude_hook_permission` | Permission prompt notification (informational only) |
| `claude_hook_tool_start` | Tool execution started in external session |
| `claude_hook_tool_end` | Tool execution completed in external session |
| `pty_permission` | **Blocking permission request** - iOS can respond to approve/deny |

**Important:** The `pty_permission` event is the one iOS should respond to. It's emitted when the blocking `permission.sh` hook is triggered, allowing iOS to approve/deny the request via the `permission/respond` RPC.

## Event Payload Structures

### 1. Session Start Event (`claude_hook_session`)

```json
{
  "type": "claude_hook_session",
  "timestamp": "2025-01-13T10:30:00Z",
  "data": {
    "session_id": "abc123",
    "cwd": "/Users/dev/my-project",
    "tool": "claude_code",
    "source": "hook"
  }
}
```

**Fields:**
- `session_id`: Claude's internal session identifier
- `cwd`: Working directory where Claude was started
- `tool`: Always `"claude_code"` for hook events
- `source`: Always `"hook"` to distinguish from cdev-managed sessions

### 2. Permission Request Event (`claude_hook_permission`)

```json
{
  "type": "claude_hook_permission",
  "timestamp": "2025-01-13T10:30:05Z",
  "data": {
    "session_id": "86336939-a034-4da8-8ebc-df50e259f63c",
    "cwd": "/Users/dev/my-project",
    "message": "Claude needs your permission to use Bash",
    "notification_type": "permission_prompt",
    "transcript_path": "/Users/dev/.claude/projects/.../session.jsonl",
    "hook_event_name": "Notification"
  }
}
```

**Fields:**
- `session_id`: Session where permission was requested
- `cwd`: Working directory of the Claude session
- `message`: Human-readable permission message (e.g., "Claude needs your permission to use Bash")
- `notification_type`: Always `"permission_prompt"` for permissions
- `transcript_path`: Path to the session's JSONL transcript file
- `hook_event_name`: Always `"Notification"` for permission events

### 3. Tool Start Event (`claude_hook_tool_start`)

```json
{
  "type": "claude_hook_tool_start",
  "timestamp": "2025-01-13T10:30:10Z",
  "data": {
    "session_id": "86336939-a034-4da8-8ebc-df50e259f63c",
    "cwd": "/Users/dev/my-project",
    "tool_name": "Bash",
    "tool_input": {
      "command": "npm test",
      "description": "Run tests"
    },
    "tool_use_id": "toolu_01CoRXH54EUAxoVDzVsHA1PT",
    "transcript_path": "/Users/dev/.claude/projects/.../session.jsonl",
    "permission_mode": "default",
    "hook_event_name": "PreToolUse"
  }
}
```

**Fields:**
- `session_id`: Session where tool is executing
- `cwd`: Working directory
- `tool_name`: Tool being executed (Bash, Read, Write, Edit, etc.)
- `tool_input`: Tool's input parameters (varies by tool)
- `tool_use_id`: Unique identifier for this tool invocation
- `transcript_path`: Path to session transcript
- `permission_mode`: Permission mode (default, etc.)
- `hook_event_name`: Always `"PreToolUse"`

### 4. Tool End Event (`claude_hook_tool_end`)

```json
{
  "type": "claude_hook_tool_end",
  "timestamp": "2025-01-13T10:30:15Z",
  "data": {
    "session_id": "86336939-a034-4da8-8ebc-df50e259f63c",
    "cwd": "/Users/dev/my-project",
    "tool_name": "Bash",
    "tool_result": {
      "stdout": "All tests passed",
      "stderr": "",
      "exit_code": 0
    },
    "tool_use_id": "toolu_01CoRXH54EUAxoVDzVsHA1PT",
    "transcript_path": "/Users/dev/.claude/projects/.../session.jsonl",
    "hook_event_name": "PostToolUse"
  }
}
```

**Fields:**
- `session_id`: Session where tool executed
- `cwd`: Working directory
- `tool_name`: Tool that executed
- `tool_result`: Result of the tool execution (structure varies by tool)
- `tool_use_id`: Matches the corresponding tool_start event
- `transcript_path`: Path to session transcript
- `hook_event_name`: Always `"PostToolUse"`

### 5. Blocking Permission Request Event (`pty_permission`)

This is the **key event for mobile permission approval**. When Claude's PreToolUse hook fires, cdev publishes this event and waits for iOS to respond.

```json
{
  "jsonrpc": "2.0",
  "method": "event/pty_permission",
  "params": {
    "tool_use_id": "toolu_01CoRXH54EUAxoVDzVsHA1PT",
    "type": "bash_command",
    "target": "npm test",
    "description": "npm test",
    "preview": "",
    "session_id": "86336939-a034-4da8-8ebc-df50e259f63c",
    "workspace_id": "",
    "options": [
      {
        "key": "allow_once",
        "label": "Allow Once",
        "description": "Allow this one request"
      },
      {
        "key": "allow_session",
        "label": "Allow for Session",
        "description": "Allow similar requests for this session"
      },
      {
        "key": "deny",
        "label": "Deny",
        "description": "Deny this request"
      }
    ]
  }
}
```

**Fields:**
- `tool_use_id`: Unique identifier - **use this when responding**
- `type`: Permission type (e.g., `bash_command`, `file_write`, `file_read`)
- `target`: What the tool wants to access (command, file path, etc.)
- `description`: Human-readable description
- `preview`: Preview of content (for file writes)
- `session_id`: Claude session ID
- `options`: Available response options

### Responding to Permission Requests

iOS responds using the `permission/respond` JSON-RPC method:

```json
{
  "jsonrpc": "2.0",
  "id": "unique-request-id",
  "method": "permission/respond",
  "params": {
    "tool_use_id": "toolu_01CoRXH54EUAxoVDzVsHA1PT",
    "decision": "allow",
    "scope": "once"
  }
}
```

**Parameters:**
- `tool_use_id`: Must match the `tool_use_id` from `pty_permission` event
- `decision`: `"allow"` or `"deny"`
- `scope`: `"once"` (single request) or `"session"` (pattern-based auto-approve)

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": "unique-request-id",
  "result": {
    "success": true,
    "decision": "allow",
    "scope": "once"
  }
}
```

## iOS Implementation Requirements

### 1. WebSocket Handler Updates

Update the WebSocket event handler to recognize new event types:

```swift
enum CdevEventType: String, Codable {
    // Existing types
    case claudeLog = "claude_log"
    case claudeStatus = "claude_status"
    case claudePermission = "claude_permission_request"
    case claudeOutput = "claude_output"

    // Hook event types (informational)
    case claudeHookSession = "claude_hook_session"
    case claudeHookPermission = "claude_hook_permission"
    case claudeHookToolStart = "claude_hook_tool_start"
    case claudeHookToolEnd = "claude_hook_tool_end"

    // Blocking permission request - iOS should respond to this!
    case ptyPermission = "pty_permission"
}
```

### 2. External Session Manager

Create a new manager to track external sessions:

```swift
class ExternalSessionManager: ObservableObject {
    @Published var activeSessions: [ExternalSession] = []

    struct ExternalSession: Identifiable {
        let id: String  // session_id
        let workingDirectory: String
        let startTime: Date
        var lastActivity: Date
        var currentTool: String?
        var toolHistory: [ToolExecution] = []
        var hasPendingPermission: Bool = false
    }

    struct ToolExecution {
        let toolName: String
        let input: [String: Any]
        let startTime: Date
        var endTime: Date?
        var output: String?
        var isError: Bool
    }

    func handleSessionStart(_ event: HookSessionEvent) {
        // Add or update session in activeSessions
    }

    func handleToolStart(_ event: HookToolEvent) {
        // Update session's currentTool
        // Add to toolHistory
    }

    func handleToolEnd(_ event: HookToolEvent) {
        // Clear currentTool
        // Update toolHistory entry
    }

    func handlePermission(_ event: HookPermissionEvent) {
        // Set hasPendingPermission = true
        // This is informational only - user must respond on desktop
    }
}
```

### 3. UI Components

#### External Sessions List View

```swift
struct ExternalSessionsView: View {
    @StateObject var sessionManager: ExternalSessionManager

    var body: some View {
        List(sessionManager.activeSessions) { session in
            ExternalSessionRow(session: session)
        }
        .navigationTitle("External Sessions")
    }
}

struct ExternalSessionRow: View {
    let session: ExternalSession

    var body: some View {
        VStack(alignment: .leading) {
            // Project name from cwd
            Text(session.workingDirectory.lastPathComponent)
                .font(.headline)

            // Status indicator
            HStack {
                if session.hasPendingPermission {
                    Label("Permission Required", systemImage: "exclamationmark.triangle")
                        .foregroundColor(.orange)
                } else if let tool = session.currentTool {
                    Label("Running: \(tool)", systemImage: "gearshape.fill")
                        .foregroundColor(.blue)
                } else {
                    Label("Idle", systemImage: "checkmark.circle")
                        .foregroundColor(.green)
                }
            }
            .font(.caption)

            // Last activity
            Text("Last activity: \(session.lastActivity.relative)")
                .font(.caption2)
                .foregroundColor(.secondary)
        }
    }
}
```

#### Permission Alert Banner

When an external session has a pending permission, show a non-interactive alert:

```swift
struct ExternalPermissionBanner: View {
    let session: ExternalSession
    let permission: PendingPermission

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundColor(.orange)
                Text("Permission Required")
                    .font(.headline)
            }

            Text("\(permission.toolName) wants to execute:")
                .font(.subheadline)

            Text(permission.description)
                .font(.caption)
                .foregroundColor(.secondary)

            Text("Respond on your desktop to continue")
                .font(.caption)
                .italic()
                .foregroundColor(.orange)
        }
        .padding()
        .background(Color.orange.opacity(0.1))
        .cornerRadius(8)
    }
}
```

### 4. Session Activity Timeline

Show tool execution history for external sessions:

```swift
struct SessionTimelineView: View {
    let session: ExternalSession

    var body: some View {
        List(session.toolHistory.reversed()) { tool in
            HStack {
                // Tool icon
                Image(systemName: iconFor(tool.toolName))

                VStack(alignment: .leading) {
                    Text(tool.toolName)
                        .font(.headline)

                    if let output = tool.output {
                        Text(output.prefix(100))
                            .font(.caption)
                            .foregroundColor(.secondary)
                    }
                }

                Spacer()

                // Duration or status
                if let endTime = tool.endTime {
                    Text(formatDuration(tool.startTime, endTime))
                        .font(.caption)
                } else {
                    ProgressView()
                        .scaleEffect(0.7)
                }
            }
        }
    }
}
```

### 5. Unified Dashboard

Combine cdev-managed and external sessions in one view:

```swift
struct UnifiedDashboardView: View {
    @StateObject var cdevSessionManager: SessionManager  // existing
    @StateObject var externalSessionManager: ExternalSessionManager  // new

    var body: some View {
        List {
            // cdev-managed sessions (full control)
            Section("cdev Sessions") {
                ForEach(cdevSessionManager.sessions) { session in
                    CdevSessionRow(session: session)  // existing UI
                }
            }

            // External sessions (read-only monitoring)
            Section("External Sessions") {
                ForEach(externalSessionManager.activeSessions) { session in
                    ExternalSessionRow(session: session)
                }
            }
        }
    }
}
```

## Feature Comparison: cdev vs External Sessions

| Feature | cdev-Managed | External (Hook) |
|---------|-------------|-----------------|
| View output | Full streaming | Tool summaries only |
| Send prompts | Yes | No |
| Approve permissions | Yes (via PTY) | **Yes (via Hook Bridge)** |
| "Allow for Session" | Yes | **Yes (pattern memory)** |
| Stop session | Yes | No |
| View tool history | Yes | Yes |
| See permission alerts | Yes | Yes |
| Working directory | Yes | Yes |
| Session ID | Yes | Yes |

**Note:** External session permission approval requires cdev to be running. If cdev is not running or times out, Claude falls back to desktop prompts.

## State Management

### Session Lifecycle

```
External Session States:
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│  [Session Start] ──► [Active] ◄──► [Tool Running]          │
│        │                │                │                  │
│        │                │                ▼                  │
│        │                │      [Permission Pending]         │
│        │                │                │                  │
│        │                ▼                │                  │
│        │          [Inactive]◄────────────┘                  │
│        │                │                                   │
│        ▼                ▼                                   │
│  [Stale/Timeout] ──► [Removed]                              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Session Timeout

External sessions should be marked as stale after inactivity:

```swift
extension ExternalSessionManager {
    private let staleTimeout: TimeInterval = 300  // 5 minutes

    func pruneStale Sessions() {
        let now = Date()
        activeSessions.removeAll { session in
            now.timeIntervalSince(session.lastActivity) > staleTimeout
        }
    }
}
```

## Push Notifications (Optional)

Consider sending push notifications for external session events:

```swift
// When permission is requested in external session
func sendPermissionNotification(session: ExternalSession, permission: PendingPermission) {
    let content = UNMutableNotificationContent()
    content.title = "Permission Required"
    content.body = "\(permission.toolName) in \(session.projectName) needs approval"
    content.sound = .default
    content.categoryIdentifier = "EXTERNAL_PERMISSION"
    // Note: User must respond on desktop - notification is informational only
}
```

## Testing Checklist

- [ ] WebSocket connects and receives hook events
- [ ] Session start events create new external session entries
- [ ] Tool start/end events update session state correctly
- [ ] `pty_permission` events show interactive permission UI
- [ ] iOS can respond to `pty_permission` via `permission/respond` RPC
- [ ] "Allow Once" approves single request
- [ ] "Allow for Session" stores pattern and auto-approves similar requests
- [ ] "Deny" blocks the tool execution
- [ ] Sessions are pruned after timeout
- [ ] UI correctly distinguishes cdev vs external sessions
- [ ] Multiple simultaneous external sessions are tracked correctly
- [ ] Session activity timeline shows correct tool history
- [ ] Dashboard shows unified view of all sessions

## Troubleshooting

### Permission requests not received on iOS

If you see `claude_hook_tool_start` events but NOT `pty_permission` events:

1. **Claude needs to be restarted** - Claude Code caches `settings.json` at startup. Quit and restart VS Code/Cursor or start a new terminal Claude session.

2. **Verify hooks are installed correctly:**
   ```bash
   cat ~/.claude/settings.json | jq '.hooks.PreToolUse'
   ```
   Should show:
   ```json
   [{
     "_cdev_managed": true,
     "hooks": [{"command": "/Users/.../.cdev/hooks/permission.sh", "type": "command"}],
     "matcher": "*"
   }]
   ```

3. **Check cdev logs** for:
   - `path=/api/hooks/permission-request` - correct (blocking)
   - `path=/api/hooks/tool-start` - wrong (non-blocking, old config)

### Permission times out

The default timeout is 60 seconds. If iOS doesn't respond in time, Claude falls back to desktop prompt.

Check cdev logs for: `permission request timed out - returning 'ask'`

## Migration Notes

No breaking changes to existing functionality. Hook events are additive - existing cdev-managed session handling remains unchanged.

## Related Documentation

- [Live Session Integration](./LIVE-SESSION-INTEGRATION.md)
- [Session Awareness](./SESSION-AWARENESS.md)
- [WebSocket Protocol](../api/PROTOCOL.md)
- [Interactive PTY Mode](./INTERACTIVE-PTY-MODE.md)
