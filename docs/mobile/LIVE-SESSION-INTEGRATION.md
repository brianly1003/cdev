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
│  6. CDEV INJECTS INTO TTY                                        │
│     echo "Also check tests" > /dev/ttys002                      │
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

### Live Session Detector

```go
// internal/adapters/live/detector.go

package live

import (
    "os"
    "os/exec"
    "path/filepath"
    "regexp"
    "strconv"
    "strings"
)

// Session represents a detected Claude process
type Session struct {
    PID       int
    TTY       string
    WorkDir   string
    SessionID string
    StartTime time.Time
}

// Detector finds running Claude processes
type Detector struct {
    managedPIDs map[int]bool
}

// NewDetector creates a live session detector
func NewDetector() *Detector {
    return &Detector{
        managedPIDs: make(map[int]bool),
    }
}

// RegisterManagedPID marks a PID as managed by cdev
func (d *Detector) RegisterManagedPID(pid int) {
    d.managedPIDs[pid] = true
}

// UnregisterManagedPID removes a PID from managed list
func (d *Detector) UnregisterManagedPID(pid int) {
    delete(d.managedPIDs, pid)
}

// DetectSessions finds Claude processes not managed by cdev
func (d *Detector) DetectSessions() ([]Session, error) {
    // Get all processes with 'claude' in command
    out, err := exec.Command("ps", "-eo", "pid,tty,lstart,command").Output()
    if err != nil {
        return nil, err
    }

    var sessions []Session
    lines := strings.Split(string(out), "\n")

    // Regex to parse ps output
    // PID TTY LSTART COMMAND
    re := regexp.MustCompile(`^\s*(\d+)\s+(\S+)\s+(.+claude.*)$`)

    for _, line := range lines {
        // Skip if not a claude process
        if !strings.Contains(line, "claude") {
            continue
        }

        // Skip helper processes
        if strings.Contains(line, "claude-") {
            continue
        }

        matches := re.FindStringSubmatch(line)
        if matches == nil {
            continue
        }

        pid, _ := strconv.Atoi(matches[1])
        tty := matches[2]

        // Skip managed PIDs
        if d.managedPIDs[pid] {
            continue
        }

        // Get working directory
        workDir := d.getWorkDir(pid)

        // Get session ID from recent JSONL files
        sessionID := d.findSessionID(workDir, pid)

        if tty != "?" && tty != "" {
            sessions = append(sessions, Session{
                PID:       pid,
                TTY:       "/dev/" + tty,
                WorkDir:   workDir,
                SessionID: sessionID,
            })
        }
    }

    return sessions, nil
}

// getWorkDir gets the working directory of a process
func (d *Detector) getWorkDir(pid int) string {
    // macOS: use lsof
    out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-Fn").Output()
    if err != nil {
        return ""
    }

    // Parse cwd from lsof output
    lines := strings.Split(string(out), "\n")
    for _, line := range lines {
        if strings.HasPrefix(line, "n") && strings.HasPrefix(line[1:], "/") {
            // Check if it's the cwd
            path := line[1:]
            if info, err := os.Stat(path); err == nil && info.IsDir() {
                return path
            }
        }
    }

    return ""
}

// findSessionID finds the most recent session file for this process
func (d *Detector) findSessionID(workDir string, pid int) string {
    if workDir == "" {
        return ""
    }

    // Convert workDir to Claude's project path format
    // /Users/brian/Projects/cdev -> -Users-brian-Projects-cdev
    projectPath := strings.ReplaceAll(workDir, "/", "-")
    if !strings.HasPrefix(projectPath, "-") {
        projectPath = "-" + projectPath
    }

    homeDir, _ := os.UserHomeDir()
    sessionsDir := filepath.Join(homeDir, ".claude", "projects", projectPath)

    // Find most recently modified JSONL file
    var newestFile string
    var newestTime time.Time

    filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() {
            return nil
        }

        if strings.HasSuffix(path, ".jsonl") {
            if info.ModTime().After(newestTime) {
                newestTime = info.ModTime()
                newestFile = filepath.Base(path)
            }
        }
        return nil
    })

    // Return session ID (filename without .jsonl)
    return strings.TrimSuffix(newestFile, ".jsonl")
}
```

### TTY Injector

```go
// internal/adapters/live/injector.go

package live

import (
    "fmt"
    "os"
)

// Injector sends input to a TTY
type Injector struct{}

// NewInjector creates a TTY injector
func NewInjector() *Injector {
    return &Injector{}
}

// Send writes text to a TTY device
func (i *Injector) Send(tty string, text string) error {
    // Open TTY for writing
    f, err := os.OpenFile(tty, os.O_WRONLY, 0)
    if err != nil {
        return fmt.Errorf("failed to open TTY %s: %w", tty, err)
    }
    defer f.Close()

    // Write text with newline
    _, err = f.WriteString(text + "\n")
    if err != nil {
        return fmt.Errorf("failed to write to TTY: %w", err)
    }

    return nil
}

// SendResponse sends a formatted response for permission/question
func (i *Injector) SendResponse(tty string, response string) error {
    // For interactive prompts, just send the response
    return i.Send(tty, response)
}
```

### Enhanced Session Manager

```go
// Modify internal/rpc/handler/methods/session_manager.go

// Add to History method
func (s *SessionManagerService) History(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
    // ... existing code ...

    // Get historical sessions from files
    historicalSessions := s.getHistoricalSessions(workspaceID)

    // Detect live sessions
    liveSessions := s.detectLiveSessions(workspace.Path)

    // Merge and deduplicate
    allSessions := s.mergeSessions(historicalSessions, liveSessions, managedSessions)

    // Sort by last_updated
    sort.Slice(allSessions, func(i, j int) bool {
        return allSessions[i].LastUpdated.After(allSessions[j].LastUpdated)
    })

    return SessionHistoryResult{
        Sessions: allSessions,
        Total:    len(allSessions),
    }, nil
}

// Add to Send method
func (s *SessionManagerService) Send(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
    // ... parse params ...

    // Get session info
    session := s.getSession(sessionID)

    switch session.Source {
    case "managed":
        // Use existing stdin method
        return s.sendViaStdin(session, prompt)

    case "live":
        // Use TTY injection
        return s.sendViaTTY(session, prompt)

    case "historical":
        return nil, message.NewError(-32001, "Cannot send to historical session")
    }
}

func (s *SessionManagerService) sendViaTTY(session *Session, prompt string) (interface{}, *message.Error) {
    injector := live.NewInjector()

    if err := injector.Send(session.TTY, prompt); err != nil {
        return nil, message.ErrInternalError(err.Error())
    }

    return map[string]interface{}{
        "status": "sent",
        "method": "tty_injection",
        "tty":    session.TTY,
    }, nil
}
```

---

## Security Considerations

### TTY Access

1. **Same User Only**: TTY injection only works for processes owned by the same user
2. **No Elevation**: cdev cannot inject into root-owned terminals
3. **Audit Trail**: All injected messages are logged

### Permissions

On macOS, TTY access may require:
- Terminal app to have "Full Disk Access" or
- User to explicitly allow cdev to access TTY devices

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

## Version History

| Date | Version | Changes |
|------|---------|---------|
| 2024-12-25 | 1.0 | Initial LIVE session integration design |
