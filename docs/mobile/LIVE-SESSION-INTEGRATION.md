# LIVE Session Integration Guide

This document describes how cdev-ios integrates with LIVE Claude sessions - sessions running in the user's terminal that weren't started by cdev.

> **Scope:** Claude LIVE sessions only. For multi-runtime JSON-RPC flows (Claude + Codex), see `docs/mobile/IOS-INTEGRATION-GUIDE.md` and `docs/api/UNIFIED-API-SPEC.md`. Examples below explicitly set `agent_type: "claude"`.

---

## Table of Contents

1. [Overview](#overview)
2. [Session Types](#session-types)
3. [Architecture](#architecture)
4. [API Reference](#api-reference)
5. [iOS Implementation](#ios-implementation)
6. [Backend Implementation](#backend-implementation)
7. [Security Considerations](#security-considerations)

---

## Overview

### The Problem

Users often run Claude Code directly in their terminal:

```bash
$ claude
> How can I help you today?
```

Previously, cdev-ios could only interact with sessions it started ("managed" sessions). LIVE mode enables cdev-ios to:

1. **Detect** Claude sessions running in the user's terminal
2. **Watch** those sessions in real-time
3. **Send messages** that appear in the terminal

### The Solution

Claude CLI always writes to session files at `~/.claude/projects/{project}/{session_id}.jsonl`. By:

1. Detecting running Claude processes
2. Matching them to session files
3. Using TTY injection for input

We can integrate seamlessly with the existing `workspace/session/watch` infrastructure.

---

## Session Types

### Source Field

Sessions now include a `source` field indicating their origin:

| Source | Description | Input Method | Can Watch | Can Send |
|--------|-------------|--------------|-----------|----------|
| `managed` | Started by cdev | stdin pipe | Yes | Yes |
| `live` | User's terminal | TTY injection | Yes | Yes |
| `historical` | No running process | N/A | Yes | No |

### Session Lifecycle

```
┌─────────────────────────────────────────────────────────────────┐
│                     Session Lifecycle                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  MANAGED SESSION                    LIVE SESSION                 │
│  ────────────────                   ────────────                 │
│                                                                  │
│  iOS: session/start                 User: $ claude              │
│        │                                   │                     │
│        ▼                                   ▼                     │
│  ┌──────────┐                       ┌──────────┐                │
│  │ running  │                       │  active  │                │
│  │ source=  │                       │  source= │                │
│  │ managed  │                       │   live   │                │
│  └────┬─────┘                       └────┬─────┘                │
│       │                                  │                       │
│       │◄─────── workspace/session/watch ──────────►│                       │
│       │◄─────── session/send ───────────►│                       │
│       │                                  │                       │
│       ▼                                  ▼                       │
│  iOS: session/stop              User: Ctrl+C or /exit           │
│        │                                   │                     │
│        ▼                                   ▼                     │
│  ┌──────────────────────────────────────────┐                   │
│  │              historical                   │                   │
│  │              source=historical            │                   │
│  └──────────────────────────────────────────┘                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                                                                  │
│  User's Laptop                                                   │
│  ┌────────────────────┐    ┌─────────────────────────────────┐  │
│  │                    │    │           cdev-agent             │  │
│  │  Terminal          │    │                                  │  │
│  │  ┌──────────────┐  │    │  ┌────────────────────────────┐ │  │
│  │  │ $ claude     │◄─┼────┼──│  Live Session Detector     │ │  │
│  │  │ > Hello!     │  │    │  │  • ps aux | grep claude    │ │  │
│  │  │              │  │    │  │  • Match PID to sessions   │ │  │
│  │  │ > [iOS msg]◄─┼──┼────┼──│  • Get TTY for injection   │ │  │
│  │  └──────────────┘  │    │  └────────────────────────────┘ │  │
│  │         │          │    │               │                  │  │
│  │         ▼          │    │               ▼                  │  │
│  │  ~/.claude/projects/    │  ┌────────────────────────────┐ │  │
│  │  └── {session}.jsonl ───┼──│  Session File Watcher      │ │  │
│  │                    │    │  │  • Watch JSONL files       │ │  │
│  └────────────────────┘    │  │  • Emit claude_message     │ │  │
│                            │  └────────────────────────────┘ │  │
│                            │               │                  │  │
│                            │               ▼                  │  │
│                            │  ┌────────────────────────────┐ │  │
│                            │  │      WebSocket Server      │ │  │
│                            │  │      /ws                   │ │  │
│                            │  └─────────────┬──────────────┘ │  │
│                            └────────────────┼────────────────┘  │
│                                             │                    │
└─────────────────────────────────────────────┼────────────────────┘
                                              │
                                              │ VS Code Tunnel
                                              │ wss://xxx.devtunnels.ms/ws
                                              │
                                              ▼
                                    ┌─────────────────┐
                                    │    cdev-ios     │
                                    │                 │
                                    │  • See sessions │
                                    │  • Watch live   │
                                    │  • Send msgs    │
                                    └─────────────────┘
```

### Data Flow for LIVE Sessions

```
┌─────────────────────────────────────────────────────────────────┐
│                    LIVE Session Data Flow                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. USER TYPES IN TERMINAL                                       │
│     ┌──────────────────────────────────────────────────────┐    │
│     │ $ claude                                              │    │
│     │ > What files are in src/?                            │    │
│     └──────────────────────────────────────────────────────┘    │
│                          │                                       │
│                          ▼                                       │
│  2. CLAUDE WRITES TO JSONL                                       │
│     ~/.claude/projects/-path-to-project/abc123.jsonl            │
│     {"type":"user","message":{"content":"What files..."}}       │
│     {"type":"assistant","message":{"content":"I'll check..."}}  │
│                          │                                       │
│                          ▼                                       │
│  3. CDEV WATCHES FILE (workspace/session/watch)                  │
│     Detects new lines → Emits claude_message events             │
│                          │                                       │
│                          ▼                                       │
│  4. IOS RECEIVES EVENTS                                          │
│     WebSocket: {"type":"claude_message","message":{...}}        │
│                          │                                       │
│                          ▼                                       │
│  5. IOS SENDS MESSAGE (session/send)                             │
│     {"method":"session/send","params":{"prompt":"Also check...","agent_type":"claude"}}│
│                          │                                       │
│                          ▼                                       │
│  6. CDEV INJECTS VIA APPLESCRIPT (macOS)                          │
│     Injector.SendWithEnterToApp("Also check tests", "Code")    │
│                          │                                       │
│                          ▼                                       │
│  7. MESSAGE APPEARS IN TERMINAL                                  │
│     ┌──────────────────────────────────────────────────────┐    │
│     │ > Also check tests                    ← From iOS!     │    │
│     │                                                       │    │
│     │ Claude: I'll also check the tests directory...       │    │
│     └──────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## API Reference

### workspace/session/history (Historical)

Lists historical sessions for a workspace and runtime. LIVE sessions are surfaced via `workspace/list` and `session/active`.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "workspace/session/history",
  "params": {
    "workspace_id": "ws-abc123",
    "limit": 50,
    "agent_type": "claude"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "sessions": [
      {
        "id": "abc-123",
        "summary": "Fix login authentication bug",
        "message_count": 42,
        "last_updated": "2024-12-25T10:30:00Z",
        "agent_type": "claude"
      },
      {
        "id": "def-456",
        "summary": "Added dark mode support",
        "message_count": 128,
        "last_updated": "2024-12-24T15:00:00Z",
        "agent_type": "claude"
      }
    ],
    "total": 2
  }
}
```

### Session Fields

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Session UUID |
| `source` | string | `"managed"`, `"live"`, or `"historical"` |
| `status` | string | `"running"`, `"active"`, `"idle"`, `"completed"` |
| `pid` | int? | Process ID (only for running sessions) |
| `tty` | string? | TTY device path (only for live sessions) |
| `summary` | string | First user message or auto-generated summary |
| `message_count` | int | Number of messages in session |
| `last_updated` | string | ISO 8601 timestamp |
| `created_at` | string | ISO 8601 timestamp |

### workspace/session/watch

Works identically for all session types. Watches the JSONL file for changes.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "workspace/session/watch",
  "params": {
    "workspace_id": "ws-abc123",
    "session_id": "def-456",
    "agent_type": "claude"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "status": "watching",
    "session_id": "def-456"
  }
}
```

**Events (Server → Client):**
```json
{
  "jsonrpc": "2.0",
  "method": "event/claude_message",
  "params": {
    "session_id": "def-456",
    "type": "assistant",
    "role": "assistant",
    "agent_type": "claude",
    "content": [
      {
        "type": "text",
        "text": "I'll check the src directory..."
      },
      {
        "type": "tool_use",
        "tool_name": "Bash",
        "tool_id": "toolu_123",
        "tool_input": {"command": "ls -la src/"}
      }
    ]
  }
}
```

### session/send (Enhanced)

Sends a message to a session. Behavior varies by session source.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "session/send",
  "params": {
    "session_id": "def-456",
    "prompt": "Also check the tests directory",
    "agent_type": "claude"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "status": "sent",
    "agent_type": "claude"
  }
}
```

**Error (Historical session):**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "error": {
    "code": -32001,
    "message": "Cannot send to historical session - no running process"
  }
}
```

### session/respond

Works for both managed and live sessions for permission/question responses.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "session/respond",
  "params": {
    "session_id": "def-456",
    "type": "permission",
    "response": "yes",
    "agent_type": "claude"
  }
}
```

---

## iOS Implementation

> **Note:** The UI examples below are from the original LIVE prototype and use a `source` field. Current responses use `agent_type` and `status` instead, and JSON-RPC notifications use the `event/` prefix (for example `event/claude_message`). Map those fields and event names to your client as needed.

### Session List View

```swift
struct SessionListView: View {
    @State private var sessions: [Session] = []

    var body: some View {
        List(sessions) { session in
            SessionRow(session: session)
                .onTapGesture {
                    watchSession(session)
                }
        }
        .onAppear {
            loadSessions()
        }
    }

    func loadSessions() {
        let request = JSONRPCRequest(
            method: "workspace/session/history",
            params: ["workspace_id": workspaceId, "limit": 50, "agent_type": "claude"]
        )

        webSocket.send(request) { result in
            sessions = result.sessions.sorted { $0.lastUpdated > $1.lastUpdated }
        }
    }
}

struct SessionRow: View {
    let session: Session

    var body: some View {
        HStack {
            // Source indicator
            Circle()
                .fill(sourceColor)
                .frame(width: 12, height: 12)

            VStack(alignment: .leading) {
                HStack {
                    Text(session.summary)
                        .font(.headline)

                    if session.source == .live {
                        Text("LIVE")
                            .font(.caption)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Color.green.opacity(0.2))
                            .foregroundColor(.green)
                            .cornerRadius(4)
                    }
                }

                Text(sourceDescription)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            Spacer()

            Text(session.lastUpdated.relative)
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }

    var sourceColor: Color {
        switch session.source {
        case .live: return .green
        case .managed: return .blue
        case .historical: return .gray
        }
    }

    var sourceDescription: String {
        switch session.source {
        case .live: return "User's terminal • PID \(session.pid ?? 0)"
        case .managed: return "Managed by cdev • \(session.status)"
        case .historical: return "Completed"
        }
    }
}
```

### Session Detail View with LIVE Support

```swift
struct SessionDetailView: View {
    let session: Session
    @State private var messages: [ClaudeMessage] = []
    @State private var inputText: String = ""
    @State private var isWatching: Bool = false

    var body: some View {
        VStack {
            // LIVE indicator banner
            if session.source == .live {
                HStack {
                    Circle()
                        .fill(Color.green)
                        .frame(width: 8, height: 8)
                    Text("LIVE - Connected to user's terminal")
                        .font(.caption)
                    Spacer()
                }
                .padding(.horizontal)
                .padding(.vertical, 8)
                .background(Color.green.opacity(0.1))
            }

            // Messages
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    ForEach(messages) { message in
                        MessageView(message: message)
                    }
                }
                .padding()
            }

            // Input (enabled for live and managed)
            if session.source != .historical {
                HStack {
                    TextField("Send message to Claude...", text: $inputText)
                        .textFieldStyle(RoundedBorderTextFieldStyle())

                    Button(action: sendMessage) {
                        Image(systemName: "arrow.up.circle.fill")
                            .font(.title2)
                    }
                    .disabled(inputText.isEmpty)
                }
                .padding()
            }
        }
        .onAppear {
            startWatching()
        }
        .onDisappear {
            stopWatching()
        }
    }

    func startWatching() {
        // Load existing messages first
        loadMessages()

        // Start watching for new messages
        let request = JSONRPCRequest(
            method: "workspace/session/watch",
            params: [
                "workspace_id": workspaceId,
                "session_id": session.id,
                "agent_type": "claude"
            ]
        )

        webSocket.send(request) { _ in
            isWatching = true
        }

        // Listen for claude_message events
        webSocket.onEvent("event/claude_message") { message in
            if message.sessionId == session.id {
                messages.append(message)
            }
        }
    }

    func sendMessage() {
        let request = JSONRPCRequest(
            method: "session/send",
            params: [
                "session_id": session.id,
                "prompt": inputText,
                "agent_type": "claude"
            ]
        )

        webSocket.send(request) { result in
            // Message will appear via claude_message event
            // when Claude processes it and writes to JSONL
            inputText = ""
        }
    }
}
```

### Permission Handling

```swift
// Listen for permission events
webSocket.onEvent("claude_permission") { event in
    showPermissionDialog(
        toolName: event.toolName,
        description: event.description,
        toolUseId: event.toolUseId
    )
}

func showPermissionDialog(toolName: String, description: String, toolUseId: String) {
    let alert = UIAlertController(
        title: "Permission Request",
        message: "Claude wants to: \(description)",
        preferredStyle: .alert
    )

    alert.addAction(UIAlertAction(title: "Deny", style: .destructive) { _ in
        respondToPermission(toolUseId: toolUseId, approved: false)
    })

    alert.addAction(UIAlertAction(title: "Allow", style: .default) { _ in
        respondToPermission(toolUseId: toolUseId, approved: true)
    })

    present(alert, animated: true)
}

func respondToPermission(toolUseId: String, approved: Bool) {
    let request = JSONRPCRequest(
        method: "session/respond",
        params: [
            "session_id": currentSessionId,
            "type": "permission",
            "tool_use_id": toolUseId,
            "response": approved ? "yes" : "no",
            "is_error": !approved,
            "agent_type": "claude"
        ]
    )

    webSocket.send(request)
}
```

---

## Backend Implementation

### Components

All LIVE session code lives in `internal/adapters/live/`:

| File | Purpose |
|------|---------|
| `detector.go` | Finds running Claude processes via `ps` + `lsof` |
| `injector.go` | Sends keystrokes via platform-specific mechanisms |
| `terminal_reader.go` | Polls terminal content (macOS only) |
| `terminal_reader_darwin.go` | AppleScript-based terminal content reading |
| `terminal_reader_linux.go` | Stub (not supported) |
| `terminal_reader_windows.go` | Stub (not supported) |
| `live_injection_test.go` | Unit + integration tests for the full RPC round-trip |

### Detector (`live.NewDetector(workspacePath)`)

Creates a detector scoped to a workspace path. Filters Claude processes by their working directory (resolved via `lsof`).

**Key methods:**

| Method | Description |
|--------|-------------|
| `GetLiveSession(sessionID)` | Returns `*LiveSession` for the given session (or any session in the workspace if the session file doesn't exist). Returns `nil` if no Claude process found. |
| `DetectAll()` | Returns all detected LIVE Claude sessions in the workspace. |
| `RegisterManagedPID(pid)` | Excludes a cdev-managed PID from detection. |
| `UnregisterManagedPID(pid)` | Re-includes a previously excluded PID. |

**Detection flow:**
1. Runs `ps -eo pid,tty,command` to find `claude` processes
2. Skips helper processes (`claude-*`), managed PIDs, and processes without a TTY
3. Uses `lsof -d cwd` to resolve each process's working directory
4. Filters by workspace path
5. Finds session ID from most recently modified `.jsonl` file in `~/.claude/projects/{project}/`
6. Walks the process tree to identify the terminal app (Terminal, iTerm2, VS Code, Cursor, etc.)

**Caching:** Results are cached with a short TTL to avoid repeated `ps`/`lsof` calls within the same request.

### Injector (`live.NewInjector()`)

Creates a platform-specific keystroke injector. Automatically selects the right backend for `runtime.GOOS`.

**Platform support:**

| Platform | Backend | Status |
|----------|---------|--------|
| macOS | AppleScript (`System Events` keystroke) | Supported |
| Windows | PowerShell `SendKeys` | Supported |
| Linux | — | Not supported |

**Key methods:**

| Method | Description |
|--------|-------------|
| `SendWithEnterToApp(text, terminalApp)` | Sends text + Enter key to the specified terminal app |
| `SendToApp(text, terminalApp)` | Sends text without Enter |
| `SendKeyToApp(key, terminalApp)` | Sends a named key (e.g., "escape", "tab") |
| `Send(tty, text)` | Sends text to a TTY device path directly |
| `SendWithEnter(tty, text)` | Sends text + Enter to a TTY |

**Rate limiting:** Minimum 500ms between injections to prevent overwhelming the terminal.

### SendPrompt Flow (session manager → LIVE injection)

When `session/send` is called with a session ID that isn't a managed session:

```
SessionManagerService.Send()
  → sendClaudePrompt(ctx, workspaceID, sessionID, prompt, mode, permissionMode)
    → Manager.SendPrompt(sessionID, prompt, mode, permissionMode)
      → GetSession(sessionID)                    // not found in managed sessions
      → findWorkspaceForSession(sessionID)        // searches .claude/projects/ and LIVE detection
      → live.NewDetector(workspace.Path)
      → detector.GetLiveSession(sessionID)
        → if found: injector.SendWithEnterToApp(prompt, liveSession.TerminalApp)
        → if not found: falls through to auto-start managed session
```

---

## Known Limitations

### PID → Session ID Mapping

**The detector cannot reliably map a specific Claude process (PID) to a specific session file when multiple Claude instances run in the same workspace.**

Current behavior: `findSessionID()` picks the most recently modified `.jsonl` file in `~/.claude/projects/{project}/`. If two Claude processes share the same workspace directory, they both get assigned whichever session file was modified last.

**Why an exact mapping is not possible externally:**

| Approach Investigated | Result |
|----------------------|--------|
| `lsof -p <pid>` for `.jsonl` files | Claude opens/writes/closes — files are never held open |
| Child process file descriptors | Same — no JSONL files held open |
| Command-line args (`ps -o args`) | Just `claude` — no session ID in args |
| Environment variables | Not exposed by Claude CLI |
| Process start time vs file birth time | Claude creates new session files on resume, so timestamps don't correlate with process start |

**Practical impact:** Low. Each Claude instance typically runs in a separate workspace (separate project directory), and workspace-path filtering handles the mapping correctly. The limitation only matters if a user runs two Claude instances in the exact same directory, which is unusual.

**Possible future fix:** Claude CLI would need to expose its session ID externally — via an environment variable, a PID file, or in its command-line arguments.

### Platform Support

| Feature | macOS | Windows | Linux |
|---------|-------|---------|-------|
| Process detection | `ps` + `lsof` | `ps` + `lsof` | `ps` + `lsof` |
| Keystroke injection | AppleScript | PowerShell SendKeys | Not supported |
| Terminal content reading | AppleScript (Terminal, iTerm2 only) | Not supported | Not supported |

### Terminal App Detection

The detector walks the process tree to identify the terminal app. Known terminal apps: Terminal, iTerm2, VS Code (`Code`), Cursor. Other terminals may not be detected and will show as `"Unknown"`.

## Security Considerations

### TTY Access

1. **Same User Only**: TTY injection only works for processes owned by the same user
2. **No Elevation**: cdev cannot inject into root-owned terminals
3. **Audit Trail**: All injected messages are logged with session ID, PID, TTY, and terminal app

### Permissions

On macOS, keystroke injection via AppleScript requires:
- **Accessibility permissions** for the cdev process (System Settings → Privacy & Security → Accessibility)
- The target terminal app must be running and have a window

### Recommendations

1. **Notify User**: Show notification when LIVE session is detected
2. **Confirmation**: Optionally require user confirmation before first injection
3. **Logging**: Log all TTY injections for audit purposes

---

## Troubleshooting

### LIVE Session Not Detected

1. **Check process is running**:
   ```bash
   ps aux | grep claude
   ```

2. **Verify TTY is accessible**:
   ```bash
   ls -la /dev/ttys*
   ```

3. **Check session file exists**:
   ```bash
   ls ~/.claude/projects/-path-to-project/
   ```

### TTY Injection Fails

1. **Permission denied**: Run cdev as same user as Claude
2. **TTY busy**: Terminal might be in special mode (vim, etc.)
3. **Process exited**: Claude may have terminated

### Messages Not Appearing

1. **Check session/watch is active**
2. **Verify correct session_id**
3. **Check WebSocket connection**

---

## Testing

### Unit Tests (no Claude needed)

```bash
go test -v -run "TestDetector_Creation|TestInjector_PlatformInit|TestDetector_GetLiveSession_NoProcess" \
  ./internal/adapters/live/
```

### Full RPC Round-Trip (requires Claude running in a terminal)

```bash
# 1. Open a separate terminal, cd to this project, run: claude
# 2. Then run:
CDEV_LIVE_POC=1 go test -v -run TestLiveInjection_FullRPCRoundTrip \
  ./internal/adapters/live/ -timeout 30s
```

Expected: `[cdev-ios POC] hello from mobile!` appears in the Claude terminal.

The test exercises the full chain: `Send() → sendClaudePrompt() → Manager.SendPrompt() → Detector.GetLiveSession() → Injector.SendWithEnterToApp()`.

---

## Version History

| Date | Version | Changes |
|------|---------|---------|
| 2026-02-25 | 2.0 | Updated to match current implementation: AppleScript injection, platform abstraction, rate limiting, workspace-scoped detection, caching, terminal app identification. Added known limitations section and testing guide. |
| 2024-12-25 | 1.0 | Initial LIVE session integration design |
