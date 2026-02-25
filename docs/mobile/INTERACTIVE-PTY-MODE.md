# Interactive PTY Mode - Feature Specification

> **Status**: Implemented
> **Version**: 2.0.0
> **Last Updated**: 2025-12-26

## Table of Contents

1. [Overview](#overview)
2. [Running Modes](#running-modes)
3. [Architecture](#architecture)
4. [Server Implementation (cdev)](#server-implementation-cdev)
5. [Client Implementation (cdev-ios)](#client-implementation-cdev-ios)
6. [API Reference](#api-reference)
7. [Event Types](#event-types)
8. [Terminal Output Parsing](#terminal-output-parsing)
9. [LIVE Session Support](#live-session-support)

---

## Overview

### Problem Statement

Claude Code CLI has two modes of operation:

1. **Stream-JSON Mode** (`-p --output-format stream-json`): Programmatic output, but permission prompts require special handling via stdin pipe
2. **Interactive Terminal Mode** (no `-p` flag): Full TUI with visual permission menus, but outputs raw terminal data

The stream-json mode works for automated permission handling (`acceptEdits`, `bypassPermissions`) but doesn't provide the rich interactive experience users expect on mobile.

**Scope note:** This doc focuses on Claude PTY behavior. When calling JSON-RPC methods, include `agent_type: "claude"` explicitly to avoid default routing.

### Solution

cdev now supports **three modes** for different use cases:

| Mode | Command | Description |
|------|---------|-------------|
| **Terminal Mode** | `cdev start` | Claude runs in current terminal, user can interact locally AND via mobile |
| **Headless Mode** | `cdev start --headless` | Claude runs as background process, mobile-only interaction |
| **LIVE Mode** | Auto-detected | Connect to Claude already running in user's terminal |

### Permission Mode Behavior

The `permission_mode` parameter in `session/send` controls how Claude is spawned:

| permission_mode | Behavior | Events |
|-----------------|----------|--------|
| `"interactive"` | **Always spawns new Claude with PTY** (ignores LIVE sessions) | `pty_permission`, `pty_state`, `claude_message` |
| `"default"` | Uses LIVE session if detected, otherwise spawns stream-json | `claude_message` (LIVE) or `claude_log` |
| `"acceptEdits"` | Uses LIVE session if detected, otherwise spawns stream-json | `claude_message` (LIVE) or `claude_log` |
| `"bypassPermissions"` | Uses LIVE session if detected, otherwise spawns stream-json | `claude_message` (LIVE) or `claude_log` |

**Important:** When `permission_mode: "interactive"` is specified:
1. cdev **skips LIVE session detection** (even if Claude is running in your terminal)
2. Spawns a **new Claude process** with PTY wrapper
3. Emits **these event types**:
   - `pty_permission` - Parsed permission prompts with options
   - `pty_state` - State changes (`idle`, `thinking`, `permission`, etc.)
   - `claude_message` - Structured messages from JSONL (for rendering in UI, includes `stop_reason`)
4. The new Claude instance is **separate** from any existing Claude in your IDE

> **Note:** `pty_output` and `claude_log` events are **disabled** for interactive mode to reduce noise. Use `claude_message` for UI rendering and `pty_permission` for permission handling.

> **Note:** `claude_message` events are emitted by watching the JSONL session file that Claude writes to. When Claude finishes, the last `claude_message` will include `stop_reason: "end_turn"`. Additionally, a `pty_state` event with `state: "idle"` is emitted when Claude completes.

### User Experience Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                        cdev-ios App                              │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ Claude is working on your request...                    │    │
│  │                                                          │    │
│  │ ✓ Analyzed codebase structure                           │    │
│  │ ✓ Generated new component                                │    │
│  │ ⏳ Waiting for permission...                             │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ 📝 Claude wants to create a file:                       │    │
│  │                                                          │    │
│  │    UserProfile.swift                                     │    │
│  │    ─────────────────────────────                        │    │
│  │    struct UserProfile: View {                           │    │
│  │        var body: some View {                            │    │
│  │            Text("Hello, World!")                        │    │
│  │        }                                                │    │
│  │    }                                                    │    │
│  │                                                          │    │
│  │  ┌──────────┐  ┌──────────────┐  ┌──────────┐          │    │
│  │  │   Yes    │  │  Yes to All  │  │    No    │          │    │
│  │  └──────────┘  └──────────────┘  └──────────┘          │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Running Modes

### Terminal Mode (Default)

```bash
# Start cdev in terminal mode (default)
cdev start
cdev start --repo /path/to/project

# Or via make
make run
make run REPO=/path/to/project
```

**Features:**
- Claude spawns in the current terminal via PTY
- User sees Claude's output directly in terminal
- User can interact locally (keyboard) AND via mobile app
- Permission prompts visible in terminal AND sent to mobile
- PTY output streams to WebSocket clients as `pty_output` events

**Architecture:**
```
┌─────────────────────────────────────────────────────────────┐
│  Terminal Mode                                               │
│                                                              │
│  User Terminal              Mobile App                      │
│  ┌──────────────┐          ┌──────────────┐                │
│  │ stdin/stdout │◄────────►│ WebSocket    │                │
│  └──────┬───────┘          └──────┬───────┘                │
│         │                         │                         │
│         ▼                         ▼                         │
│  ┌─────────────────────────────────────────┐               │
│  │  terminal.Runner (PTY wrapper)          │               │
│  │  - Input: local stdin + WebSocket       │               │
│  │  - Output: local stdout + WebSocket     │               │
│  └─────────────────────────────────────────┘               │
│                    │                                        │
│                    ▼                                        │
│  ┌─────────────────────────────────────────┐               │
│  │  Claude CLI (spawned via pty.Start)     │               │
│  └─────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────┘
```

### Headless Mode

```bash
# Start cdev in headless mode
cdev start --headless
cdev start --headless --repo /path/to/project

# Or via make
make run-headless
make run-bg  # Background daemon (always headless)
```

**Features:**
- Claude runs as background subprocess
- No terminal UI required
- Mobile-only interaction via WebSocket
- Best for server deployments or automation
- Output streamed via `claude_log` and `claude_message` events

**Architecture:**
```
┌─────────────────────────────────────────────────────────────┐
│  Headless Mode                                               │
│                                                              │
│  Mobile App                                                 │
│  ┌──────────────┐                                          │
│  │ WebSocket    │                                          │
│  └──────┬───────┘                                          │
│         │                                                   │
│         ▼                                                   │
│  ┌─────────────────────────────────────────┐               │
│  │  claude.Manager (subprocess)            │               │
│  │  - Input: WebSocket only                │               │
│  │  - Output: stream-json events           │               │
│  └─────────────────────────────────────────┘               │
│                    │                                        │
│                    ▼                                        │
│  ┌─────────────────────────────────────────┐               │
│  │  Claude CLI (-p --output-format json)   │               │
│  └─────────────────────────────────────────┘               │
└─────────────────────────────────────────────────────────────┘
```

### LIVE Mode (Auto-Detected)

When Claude is already running in the user's terminal (not started by cdev), cdev can detect and connect to it.

**Features:**
- Detects Claude processes via `ps` + `lsof`
- Injects keystrokes via AppleScript (macOS) or UI Automation (Windows)
- Watches JSONL session files for output
- Permission detection via terminal content polling (native terminals only)

**Supported Terminals:**
| Terminal | Input Injection | Output Capture | Permission Detection |
|----------|-----------------|----------------|---------------------|
| Terminal.app | ✅ AppleScript | ✅ JSONL + AppleScript | ✅ AppleScript |
| iTerm2 | ✅ AppleScript | ✅ JSONL + AppleScript | ✅ AppleScript |
| VS Code | ✅ AppleScript | ✅ JSONL only | ❌ Needs extension |
| Cursor | ✅ AppleScript | ✅ JSONL only | ❌ Needs extension |

---

## Architecture

### High-Level Data Flow

```
┌──────────────┐     WebSocket      ┌──────────────┐      PTY       ┌──────────────┐
│   cdev-ios   │◄──────────────────►│     cdev     │◄──────────────►│  Claude CLI  │
│              │    JSON-RPC 2.0    │              │   Raw Terminal │              │
└──────────────┘                    └──────────────┘                └──────────────┘
       │                                   │                               │
       │  1. session/send                  │                               │
       │     permission_mode:"interactive" │                               │
       │──────────────────────────────────►│                               │
       │                                   │  2. pty.Start("claude")       │
       │                                   │──────────────────────────────►│
       │                                   │                               │
       │                                   │  3. Send prompt via PTY       │
       │                                   │──────────────────────────────►│
       │                                   │                               │
       │                                   │  4. Terminal output (ANSI)    │
       │                                   │◄──────────────────────────────│
       │                                   │                               │
       │  5. pty_output event              │  4b. Write to JSONL file      │
       │     (parsed & structured)         │◄──────────────────────────────│
       │◄──────────────────────────────────│                               │
       │                                   │                               │
       │  6. pty_permission event          │                               │
       │     (when prompt detected)        │                               │
       │◄──────────────────────────────────│                               │
       │                                   │                               │
       │  6b. claude_message event         │                               │
       │     (from JSONL file watch)       │                               │
       │◄──────────────────────────────────│                               │
       │                                   │                               │
       │  7. session/input                 │                               │
       │     input: "1" (Yes)              │                               │
       │──────────────────────────────────►│                               │
       │                                   │  8. Write "1\r" to PTY        │
       │                                   │──────────────────────────────►│
       │                                   │                               │
```

**Event Sources:**
- `pty_output` / `pty_permission`: Parsed from PTY terminal output (real-time)
- `claude_message`: Parsed from JSONL session file (structured, slight delay)

### Component Responsibilities

| Component | Responsibility |
|-----------|---------------|
| **cdev** (Server) | PTY management, terminal parsing, event emission, input handling |
| **cdev-ios** (Client) | Event rendering, permission UI, user input capture, session management |
| **Claude CLI** | Code generation, file operations, interactive prompts |

---

## Server Implementation (cdev)

### Implementation Status

| Feature | Status | Notes |
|---------|--------|-------|
| PTY spawning | ✅ Done | Uses `github.com/creack/pty` |
| Terminal mode | ✅ Done | `--headless=false` (default) |
| Headless mode | ✅ Done | `--headless=true` |
| PTY output streaming | ✅ Done | `pty_output` events |
| ANSI code stripping | ✅ Done | `internal/adapters/claude/ansi.go` |
| Permission prompt detection | ✅ Done | `internal/adapters/claude/pty_parser.go` |
| PTY permission events | ✅ Done | `pty_permission` events |
| PTY state tracking | ✅ Done | `pty_state` events |
| PTY input | ✅ Done | `session/input` with key support |
| LIVE session detection | ✅ Done | `internal/adapters/live/detector.go` |
| LIVE session input | ✅ Done | `internal/adapters/live/injector.go` |
| Terminal content polling | ✅ Done | `internal/adapters/live/terminal_reader.go` |

### Key Files

```
internal/
├── adapters/
│   ├── claude/
│   │   ├── ansi.go              # ANSI escape code parser
│   │   ├── ansi_test.go
│   │   ├── pty_parser.go        # Permission prompt detector
│   │   ├── pty_parser_test.go
│   │   └── manager.go           # Claude process management
│   └── live/
│       ├── detector.go          # LIVE session detection
│       ├── injector.go          # Keystroke injection
│       ├── injector_darwin.go   # macOS AppleScript
│       ├── terminal_reader.go   # Terminal content polling
│       └── terminal_reader_darwin.go
├── terminal/
│   └── runner.go                # Terminal mode PTY wrapper
├── domain/events/
│   └── pty.go                   # PTY event types
└── session/
    └── manager.go               # Multi-session orchestration
```

### Permission Detection

The PTY parser detects two formats of permission prompts:

**Format 1: Inline Format**
```
● Write(src/components/Button.tsx)
└─ Allowed
```

**Format 2: Permission Panel Format**
```
────────────────────────────────────────────────────────
Bash command

  rm /path/to/file
  Delete hello.txt

Do you want to proceed?
❯ 1. Yes
  2. Yes, and don't ask again for rm commands
  3. Type here to tell Claude what to do differently

Esc to cancel
```

Detected permission types:
- `write_file` - Creating new files
- `edit_file` - Editing existing files
- `delete_file` - Deleting files
- `bash_command` - Running shell commands
- `trust_folder` - Trusting a directory
- `mcp_tool` - MCP tool execution

---

## Client Implementation (cdev-ios)

### Event Models

```swift
// MARK: - PTY Output Event

struct PTYOutputPayload: Codable {
    let cleanText: String
    let rawText: String
    let state: String
    let sessionId: String?

    enum CodingKeys: String, CodingKey {
        case cleanText = "clean_text"
        case rawText = "raw_text"
        case state
        case sessionId = "session_id"
    }
}

// MARK: - PTY Permission Event

struct PTYPermissionPayload: Codable {
    let type: String
    let target: String
    let description: String
    let preview: String?
    let options: [PTYPromptOption]
    let sessionId: String?

    enum CodingKeys: String, CodingKey {
        case type, target, description, preview, options
        case sessionId = "session_id"
    }
}

struct PTYPromptOption: Codable, Identifiable {
    let key: String
    let label: String
    let description: String?
    let selected: Bool  // true if this option has the cursor (❯)

    var id: String { key }
}

// MARK: - PTY State

enum PTYState: String, Codable {
    case idle
    case thinking
    case permission
    case question
    case error
}

enum PTYPermissionType: String, Codable {
    case writeFile = "write_file"
    case editFile = "edit_file"
    case deleteFile = "delete_file"
    case bashCommand = "bash_command"
    case trustFolder = "trust_folder"
    case mcpTool = "mcp_tool"
    case unknown
}
```

### Event Handling

```swift
extension WebSocketService {
    func handleEvent(_ event: ServerEvent) {
        switch event.type {
        case "pty_permission":
            handlePTYPermission(event)
        case "pty_state":
            handlePTYState(event)
        case "claude_message":
            handleClaudeMessage(event)
        default:
            break
        }
    }

    private func handlePTYPermission(_ event: ServerEvent) {
        guard let payload = try? decode(PTYPermissionPayload.self, from: event.payload) else { return }

        DispatchQueue.main.async {
            self.sessionState.showPermissionPrompt(payload)
        }
    }

    private func handlePTYState(_ event: ServerEvent) {
        guard let payload = try? decode(PTYStatePayload.self, from: event.payload) else { return }

        DispatchQueue.main.async {
            self.sessionState.ptyState = PTYState(rawValue: payload.state) ?? .idle

            // Detect when Claude finishes
            if payload.state == "idle" {
                self.sessionState.hideStopButton()
                self.sessionState.setClaudeFinished()
            }
        }
    }

    private func handleClaudeMessage(_ event: ServerEvent) {
        guard let payload = try? decode(ClaudeMessagePayload.self, from: event.payload) else { return }

        DispatchQueue.main.async {
            // Append message to UI
            self.sessionState.appendMessage(payload)

            // Also check stop_reason for completion detection
            if payload.stopReason == "end_turn" {
                self.sessionState.hideStopButton()
                self.sessionState.setClaudeFinished()
            }
        }
    }
}

// MARK: - Claude Message Payload

struct ClaudeMessagePayload: Codable {
    let sessionId: String
    let type: String
    let role: String
    let content: [ClaudeMessageContent]
    let stopReason: String?
    let isContextCompaction: Bool?

    enum CodingKeys: String, CodingKey {
        case sessionId = "session_id"
        case type, role, content
        case stopReason = "stop_reason"
        case isContextCompaction = "is_context_compaction"
    }
}

struct ClaudeMessageContent: Codable {
    let type: String
    let text: String?
    let toolName: String?
    let toolId: String?
    let toolInput: [String: AnyCodable]?

    enum CodingKeys: String, CodingKey {
        case type, text
        case toolName = "tool_name"
        case toolId = "tool_id"
        case toolInput = "tool_input"
    }
}

// MARK: - PTY State Payload

struct PTYStatePayload: Codable {
    let state: String
    let waitingForInput: Bool
    let promptType: String?
    let sessionId: String?

    enum CodingKeys: String, CodingKey {
        case state
        case waitingForInput = "waiting_for_input"
        case promptType = "prompt_type"
        case sessionId = "session_id"
    }
}
```

### Sending Input

```swift
class SessionState: ObservableObject {
    // Send prompt to start Claude
    func sendPrompt(_ prompt: String, permissionMode: PermissionMode = .interactive) {
        webSocket.send(
            method: "session/send",
            params: [
                "workspace_id": workspaceId,
                "session_id": sessionId,
                "prompt": prompt,
                "permission_mode": permissionMode.rawValue,
                "agent_type": "claude"
            ]
        )
    }

    // Send input response (e.g., "1" for Yes)
    func sendInput(_ input: String) {
        webSocket.send(
            method: "session/input",
            params: [
                "session_id": sessionId,
                "input": input,
                "agent_type": "claude"
            ]
        )
    }

    // Send special key (enter, escape, arrow keys)
    func sendKey(_ key: String) {
        webSocket.send(
            method: "session/input",
            params: [
                "session_id": sessionId,
                "key": key,  // "enter", "escape", "up", "down", etc.
                "agent_type": "claude"
            ]
        )
    }
}

enum PermissionMode: String {
    case `default` = "default"
    case acceptEdits = "acceptEdits"
    case bypassPermissions = "bypassPermissions"
    case interactive = "interactive"
}
```

---

## API Reference

### session/send

Start a prompt with specified permission mode.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "session/send",
    "params": {
        "workspace_id": "ws-123",
        "session_id": "sess-456",
        "prompt": "Create a new Swift file for user authentication",
        "permission_mode": "interactive",
        "agent_type": "claude"
    },
    "id": 1
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "status": "sent",
        "session_id": "sess-456"
    }
}
```

### session/input

Send input to the session (text or special key).

**Text Input:**
```json
{
    "jsonrpc": "2.0",
    "method": "session/input",
    "params": {
        "session_id": "sess-456",
        "input": "1",
        "agent_type": "claude"
    },
    "id": 2
}
```

**Special Key:**
```json
{
    "jsonrpc": "2.0",
    "method": "session/input",
    "params": {
        "session_id": "sess-456",
        "key": "enter",
        "agent_type": "claude"
    },
    "id": 3
}
```

**Supported Keys:**
- `enter`, `return` - Enter/Return key
- `escape`, `esc` - Escape key
- `up`, `down`, `left`, `right` - Arrow keys
- `tab` - Tab key
- `backspace`, `delete` - Delete keys
- `home`, `end`, `pageup`, `pagedown` - Navigation keys
- `space` - Space bar

**Response:**
```json
{
    "jsonrpc": "2.0",
    "id": 2,
    "result": {
        "status": "sent"
    }
}
```

### session/state

Get current session state including PTY status.

**Request:**
```json
{
    "jsonrpc": "2.0",
    "method": "session/state",
    "params": {
        "session_id": "sess-456"
    },
    "id": 4
}
```

**Response:**
```json
{
    "jsonrpc": "2.0",
    "id": 4,
    "result": {
        "session_id": "sess-456",
        "workspace_id": "ws-123",
        "status": "running",
        "is_pty_mode": true,
        "pty_state": "permission",
        "pty_waiting_for_input": true,
        "pty_prompt_type": "write_file",
        "is_live": false
    }
}
```

### workspace/list

List workspaces with session information.

**Response includes session type:**
```json
{
    "workspaces": [
        {
            "id": "ws-123",
            "name": "my-project",
            "path": "/Users/dev/my-project",
            "active_session": {
                "session_id": "sess-456",
                "status": "running",
                "is_live": true,
                "terminal_app": "Terminal"
            }
        }
    ]
}
```

---

## Event Types

### pty_output

Emitted for terminal output (terminal mode and LIVE sessions).

```json
{
    "type": "pty_output",
    "timestamp": "2025-12-26T12:00:00Z",
    "payload": {
        "clean_text": "Creating file UserProfile.swift...",
        "raw_text": "\u001b[32mCreating file UserProfile.swift...\u001b[0m",
        "state": "thinking",
        "session_id": "sess-456"
    }
}
```

### pty_permission

Emitted when a permission prompt is detected.

```json
{
    "type": "pty_permission",
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

### pty_state

Emitted when PTY state changes. Important for detecting when Claude finishes.

**State: idle (Claude finished)**
```json
{
    "type": "pty_state",
    "timestamp": "2025-12-26T12:00:02Z",
    "payload": {
        "state": "idle",
        "waiting_for_input": false,
        "prompt_type": "",
        "session_id": "sess-456"
    }
}
```

**State: permission (waiting for approval)**
```json
{
    "type": "pty_state",
    "timestamp": "2025-12-26T12:00:02Z",
    "payload": {
        "state": "permission",
        "waiting_for_input": true,
        "prompt_type": "write_file",
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

### claude_message

Emitted for Claude's structured messages (headless mode and JSONL watching).

**Regular message (Claude still working):**
```json
{
    "type": "claude_message",
    "timestamp": "2025-12-26T12:00:03Z",
    "payload": {
        "session_id": "sess-456",
        "type": "assistant",
        "role": "assistant",
        "content": [
            {
                "type": "text",
                "text": "I'll create the UserProfile.swift file for you."
            }
        ],
        "stop_reason": ""
    }
}
```

**Final message (Claude finished):**
```json
{
    "type": "claude_message",
    "timestamp": "2025-12-26T12:00:10Z",
    "payload": {
        "session_id": "sess-456",
        "type": "assistant",
        "role": "assistant",
        "content": [
            {
                "type": "text",
                "text": "Done! I've created the UserProfile.swift file."
            }
        ],
        "stop_reason": "end_turn"
    }
}
```

**Payload Fields:**
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

---

## Terminal Output Parsing

### State Detection Patterns

| State | Detection Pattern | Example |
|-------|------------------|---------|
| `thinking` | `Thinking...`, `Scheming...`, `Cooking...` | `✽ Scheming...` |
| `permission` | Permission panel or inline prompt | `Do you want to proceed?` |
| `question` | `?` prompt from AskUserQuestion | `What should the file be named?` |
| `error` | `Error:`, `Failed:` | `Error: File not found` |
| `idle` | Default / no specific pattern | Normal output |

### Permission Type Detection

| Type | Pattern | Example |
|------|---------|---------|
| `write_file` | `Write(`, `Create file` | `Write(hello.swift)` |
| `edit_file` | `Edit(`, `Edit file` | `Edit(main.swift)` |
| `delete_file` | `Delete file`, `Remove` | `Delete hello.swift` |
| `bash_command` | `Bash(`, `Bash command` header | `Bash(npm install)` |
| `trust_folder` | `Do you trust the files` | Trust folder prompt |
| `mcp_tool` | `mcp__` prefix | `mcp__github__create_issue` |

---

## LIVE Session Support

### How LIVE Detection Works

1. **Process Detection**: `ps aux | grep claude` finds running Claude processes
2. **TTY Detection**: `lsof -p <pid>` finds the terminal device
3. **Terminal App Detection**: Matches TTY to terminal application
4. **Session File Detection**: Finds corresponding JSONL in `~/.claude/projects/`

### LIVE Session Flow

```
┌─────────────────────────────────────────────────────────────────┐
│  LIVE Session Flow                                               │
│                                                                  │
│  1. User starts Claude in Terminal.app (not via cdev)           │
│     $ claude                                                    │
│                                                                  │
│  2. Mobile app calls workspace/list                             │
│     cdev detects LIVE session via ps + lsof                     │
│                                                                  │
│  3. Mobile app calls workspace/session/watch                    │
│     cdev starts watching JSONL file for messages                │
│                                                                  │
│  4. Mobile app calls session/send or session/input              │
│     cdev injects keystrokes via AppleScript                     │
│                                                                  │
│  5. For permission detection (native terminals only):           │
│     cdev polls terminal content via AppleScript                 │
│     Parses content with PTY parser                              │
│     Emits pty_permission events                                 │
└─────────────────────────────────────────────────────────────────┘
```

### Limitations

| Feature | Spawned Sessions | LIVE Sessions |
|---------|------------------|---------------|
| Output capture | ✅ Full PTY stream | ✅ JSONL + AppleScript |
| Input injection | ✅ Direct PTY write | ✅ AppleScript keystroke |
| Permission detection | ✅ PTY parser | ⚠️ Native terminals only |
| IDE terminal support | ✅ Full support | ⚠️ Input only, no perm detection |
| PID → session mapping | ✅ Exact (cdev spawned it) | ⚠️ Heuristic (most recent JSONL file) |

> **Note:** When multiple Claude instances share the same workspace directory, the detector cannot reliably map a PID to its session file. See [LIVE Session Integration - Known Limitations](./LIVE-SESSION-INTEGRATION.md#known-limitations) for details.

---

## Related Documents

- [LIVE Session Integration](./LIVE-SESSION-INTEGRATION.md)
- [Session Awareness](./SESSION-AWARENESS.md)
- [iOS Integration Guide](./IOS-INTEGRATION-GUIDE.md)
