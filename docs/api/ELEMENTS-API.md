# Elements API - Claude Code UI Integration

This document describes the Elements API for building iOS/mobile UIs that replicate the Claude Code CLI experience.

## Table of Contents

1. [Overview](#overview)
2. [Element Types](#element-types)
3. [REST API](#rest-api)
4. [WebSocket Events](#websocket-events)
5. [iOS Integration](#ios-integration)
6. [Data Structures](#data-structures)

---

## Overview

The Elements API transforms raw Claude session data into **UI-ready elements** that map directly to SwiftUI/UIKit components. This eliminates complex parsing logic on the client side.

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│   Session Files              cdev                    iOS App          │
│   (.jsonl)                   Parser                        (Render)         │
│                                                                              │
│   ┌──────────────┐     ┌──────────────────┐     ┌──────────────────┐       │
│   │ Raw JSON     │     │ Element Parser   │     │ SwiftUI Views    │       │
│   │ with XML     │────▶│                  │────▶│                  │       │
│   │ embedded     │     │ - Parse XML tags │     │ - UserInputView  │       │
│   │ tool calls   │     │ - Extract params │     │ - ToolCallView   │       │
│   │              │     │ - Parse diffs    │     │ - ToolResultView │       │
│   │              │     │ - Calc summaries │     │ - DiffView       │       │
│   └──────────────┘     └──────────────────┘     └──────────────────┘       │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Benefits

| Without Elements API | With Elements API |
|---------------------|-------------------|
| Parse XML from text | Decode JSON directly |
| Regex tool extraction | Structured `tool_call` objects |
| Manual diff parsing | Pre-parsed `hunks` and `lines` |
| Calculate line counts | Pre-calculated summaries |
| Complex error handling | Simple `is_error` boolean |

---

## Element Types

### 1. `user_input`

User's prompt/message to Claude.

```json
{
  "id": "elem_001",
  "type": "user_input",
  "timestamp": "2025-12-19T03:00:00Z",
  "content": {
    "text": "commit changes to github"
  }
}
```

**UI Rendering:**
```
> commit changes to github
```

---

### 2. `assistant_text`

Claude's text response (not a tool call).

```json
{
  "id": "elem_002",
  "type": "assistant_text",
  "timestamp": "2025-12-19T03:00:01Z",
  "content": {
    "text": "I'll commit your changes to GitHub."
  }
}
```

**UI Rendering:**
```
● I'll commit your changes to GitHub.
```

---

### 3. `tool_call`

A tool invocation by Claude.

```json
{
  "id": "elem_003",
  "type": "tool_call",
  "timestamp": "2025-12-19T03:00:02Z",
  "content": {
    "tool": "Bash",
    "tool_id": "toolu_01ABC123",
    "display": "Bash(git -C /Users/brianly/Projects/cdev-ios status)",
    "params": {
      "command": "git -C /Users/brianly/Projects/cdev-ios status",
      "description": "Check git status"
    },
    "status": "completed",
    "duration_ms": 150
  }
}
```

**Tool Names:**
- `Bash` - Shell command execution
- `Read` - File reading
- `Write` - File creation
- `Edit` / `Update` - File modification
- `Glob` / `Search` - File pattern search
- `Grep` - Content search
- `WebFetch` - URL fetching
- `WebSearch` - Web search
- `TodoWrite` - Task management

**Status Values:**
- `running` - Currently executing
- `completed` - Successfully finished
- `error` - Failed with error
- `interrupted` - User interrupted

**UI Rendering:**
```
● Bash(git -C /Users/brianly/Projects/cdev-ios status)
```

---

### 4. `tool_result`

Result from a tool execution.

```json
{
  "id": "elem_004",
  "type": "tool_result",
  "timestamp": "2025-12-19T03:00:03Z",
  "content": {
    "tool_call_id": "elem_003",
    "tool_name": "Bash",
    "is_error": false,
    "summary": "On branch main",
    "full_content": "On branch main\nYour branch is up to date with 'origin/main'.\n\nChanges to be committed:\n  (use \"git restore --staged <file>...\" to unstage)\n\tmodified:   src/app.ts\n\tnew file:   src/utils.ts",
    "line_count": 8,
    "expandable": true,
    "truncated": false
  }
}
```

**UI Rendering (Collapsed):**
```
└ On branch main
  … +7 lines (ctrl+o to expand)
```

**UI Rendering (Expanded):**
```
└ On branch main
  Your branch is up to date with 'origin/main'.

  Changes to be committed:
    (use "git restore --staged <file>..." to unstage)
      modified:   src/app.ts
      new file:   src/utils.ts
```

---

### 5. `tool_result` (Error)

Error result from a tool.

```json
{
  "id": "elem_005",
  "type": "tool_result",
  "timestamp": "2025-12-19T03:00:04Z",
  "content": {
    "tool_call_id": "elem_003",
    "tool_name": "Bash",
    "is_error": true,
    "error_code": 128,
    "summary": "Error: Exit code 128",
    "full_content": "fatal: The current branch main has no upstream branch.\nTo push the current branch and set the remote as upstream, use\n\n    git push --set-upstream origin main",
    "line_count": 5,
    "expandable": true
  }
}
```

**UI Rendering:**
```
└ Error: Exit code 128
  fatal: The current branch main has no upstream branch.
  To push the current branch and set the remote as upstream, use

      git push --set-upstream origin main
```
(Error text in red)

---

### 6. `diff`

File diff/changes display.

```json
{
  "id": "elem_006",
  "type": "diff",
  "timestamp": "2025-12-19T03:00:05Z",
  "content": {
    "tool_call_id": "elem_005",
    "file_path": "cdev/Presentation/Screens/DiffViewer/DiffListView.swift",
    "summary": {
      "added": 16,
      "removed": 5,
      "display": "Added 16 lines, removed 5 lines"
    },
    "hunks": [
      {
        "header": "@@ -4,15 +4,26 @@",
        "old_start": 4,
        "old_count": 15,
        "new_start": 4,
        "new_count": 26,
        "lines": [
          {
            "type": "context",
            "old_line": 4,
            "new_line": 4,
            "content": "struct DiffListView: View {"
          },
          {
            "type": "context",
            "old_line": 5,
            "new_line": 5,
            "content": "    let diffs: [DiffEntry]"
          },
          {
            "type": "removed",
            "old_line": 14,
            "new_line": null,
            "content": "        EmptyStateView("
          },
          {
            "type": "removed",
            "old_line": 15,
            "new_line": null,
            "content": "            icon: Icons.changes,"
          },
          {
            "type": "added",
            "old_line": null,
            "new_line": 14,
            "content": "        // Empty state with pull-to-refresh"
          },
          {
            "type": "added",
            "old_line": null,
            "new_line": 15,
            "content": "        ScrollView {"
          }
        ]
      }
    ]
  }
}
```

**Line Types:**
- `context` - Unchanged line (no highlight)
- `added` - New line (green background, `+` prefix)
- `removed` - Deleted line (red background, `-` prefix)

**UI Rendering:**
```
└ Added 16 lines, removed 5 lines
   4    struct DiffListView: View {
   5        let diffs: [DiffEntry]
  14  -         EmptyStateView(              ← Red background
  15  -             icon: Icons.changes,     ← Red background
  14  +         // Empty state with pull...  ← Green background
  15  +         ScrollView {                 ← Green background
```

---

### 7. `thinking`

Claude's internal reasoning (collapsible).

```json
{
  "id": "elem_007",
  "type": "thinking",
  "timestamp": "2025-12-19T03:00:06Z",
  "content": {
    "text": "Let me analyze the codebase structure. I should first check the existing files to understand the architecture before making changes...",
    "collapsed": true
  }
}
```

**UI Rendering (Collapsed):**
```
▶ Thinking...
```

**UI Rendering (Expanded):**
```
▼ Thinking...
  Let me analyze the codebase structure. I should first
  check the existing files to understand the architecture
  before making changes...
```

---

### 8. `interrupted`

User interrupted Claude's action.

```json
{
  "id": "elem_008",
  "type": "interrupted",
  "timestamp": "2025-12-19T03:00:07Z",
  "content": {
    "tool_call_id": "elem_007",
    "message": "What should Claude do instead?"
  }
}
```

**UI Rendering:**
```
└ Interrupted · What should Claude do instead?
```
(Yellow "Interrupted" text)

---

## REST API

### GET /api/claude/sessions/elements

Retrieves UI elements for a session with pagination support.

**Endpoint:** `GET /api/claude/sessions/elements`

**Query Parameters:**

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `session_id` | string | Yes | - | Claude session UUID |
| `limit` | integer | No | 50 | Elements to return (max 100) |
| `before` | string | No | - | Elements before this ID (scroll up) |
| `after` | string | No | - | Elements after this ID (catch-up) |

**Response:**

```json
{
  "session_id": "decda6d9-5311-42ff-90f3-9b8895a03cdd",
  "elements": [
    // ... element objects
  ],
  "pagination": {
    "total": 145,
    "returned": 50,
    "has_more_before": true,
    "has_more_after": false,
    "oldest_id": "elem_001",
    "newest_id": "elem_050"
  }
}
```

**Example Requests:**

```bash
# Initial load - get last 20 elements
curl "http://localhost:16180/api/claude/sessions/elements?session_id=xxx&limit=20"

# Scroll up - load older elements
curl "http://localhost:16180/api/claude/sessions/elements?session_id=xxx&before=elem_020&limit=20"

# Catch-up after reconnect
curl "http://localhost:16180/api/claude/sessions/elements?session_id=xxx&after=elem_050"
```

---

## WebSocket Events

### Event: `claude_element`

Emitted in real-time when a new element is created during Claude execution.

**Connection:** `ws://localhost:16180/ws`

**Payload:**

```json
{
  "event": "claude_element",
  "timestamp": "2025-12-19T03:00:02Z",
  "payload": {
    "session_id": "decda6d9-5311-42ff-90f3-9b8895a03cdd",
    "element": {
      "id": "elem_003",
      "type": "tool_call",
      "timestamp": "2025-12-19T03:00:02Z",
      "content": {
        "tool": "Bash",
        "display": "Bash(git status)",
        "params": {"command": "git status"},
        "status": "running"
      }
    }
  }
}
```

### Event: `claude_element_update`

Emitted when an element's status changes (e.g., tool_call completes).

```json
{
  "event": "claude_element_update",
  "timestamp": "2025-12-19T03:00:03Z",
  "payload": {
    "session_id": "decda6d9-5311-42ff-90f3-9b8895a03cdd",
    "element_id": "elem_003",
    "updates": {
      "status": "completed",
      "duration_ms": 150
    }
  }
}
```

---

## iOS Integration

### Data Models

```swift
import Foundation

// MARK: - Element Types

enum ElementType: String, Codable {
    case userInput = "user_input"
    case assistantText = "assistant_text"
    case toolCall = "tool_call"
    case toolResult = "tool_result"
    case diff = "diff"
    case thinking = "thinking"
    case interrupted = "interrupted"
}

enum ToolStatus: String, Codable {
    case running
    case completed
    case error
    case interrupted
}

enum DiffLineType: String, Codable {
    case context
    case added
    case removed
}

// MARK: - Element

struct ChatElement: Codable, Identifiable {
    let id: String
    let type: ElementType
    let timestamp: String
    let content: ElementContent
}

// MARK: - Content Types

enum ElementContent: Codable {
    case userInput(UserInputContent)
    case assistantText(AssistantTextContent)
    case toolCall(ToolCallContent)
    case toolResult(ToolResultContent)
    case diff(DiffContent)
    case thinking(ThinkingContent)
    case interrupted(InterruptedContent)

    // Custom decoding based on element type
}

struct UserInputContent: Codable {
    let text: String
}

struct AssistantTextContent: Codable {
    let text: String
}

struct ToolCallContent: Codable {
    let tool: String
    let toolId: String?
    let display: String
    let params: [String: AnyCodable]
    let status: ToolStatus
    let durationMs: Int?
}

struct ToolResultContent: Codable {
    let toolCallId: String
    let toolName: String
    let isError: Bool
    let errorCode: Int?
    let summary: String
    let fullContent: String
    let lineCount: Int
    let expandable: Bool
    let truncated: Bool?
}

struct DiffContent: Codable {
    let toolCallId: String
    let filePath: String
    let summary: DiffSummary
    let hunks: [DiffHunk]
}

struct DiffSummary: Codable {
    let added: Int
    let removed: Int
    let display: String
}

struct DiffHunk: Codable {
    let header: String
    let oldStart: Int
    let oldCount: Int
    let newStart: Int
    let newCount: Int
    let lines: [DiffLine]
}

struct DiffLine: Codable, Identifiable {
    var id: String { "\(oldLine ?? 0)-\(newLine ?? 0)-\(content.hashValue)" }
    let type: DiffLineType
    let oldLine: Int?
    let newLine: Int?
    let content: String
}

struct ThinkingContent: Codable {
    let text: String
    let collapsed: Bool
}

struct InterruptedContent: Codable {
    let toolCallId: String
    let message: String
}
```

### SwiftUI Views

```swift
import SwiftUI

// MARK: - Element Container

struct ElementView: View {
    let element: ChatElement

    var body: some View {
        switch element.type {
        case .userInput:
            UserInputView(content: element.userInputContent!)
        case .assistantText:
            AssistantTextView(content: element.assistantTextContent!)
        case .toolCall:
            ToolCallView(content: element.toolCallContent!)
        case .toolResult:
            ToolResultView(content: element.toolResultContent!)
        case .diff:
            DiffView(content: element.diffContent!)
        case .thinking:
            ThinkingView(content: element.thinkingContent!)
        case .interrupted:
            InterruptedView(content: element.interruptedContent!)
        }
    }
}

// MARK: - User Input

struct UserInputView: View {
    let content: UserInputContent

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text(">")
                .foregroundColor(.gray)
                .font(.system(.body, design: .monospaced))

            Text(content.text)
                .foregroundColor(.white)
                .font(.system(.body, design: .monospaced))

            Spacer()
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Assistant Text

struct AssistantTextView: View {
    let content: AssistantTextContent

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Circle()
                .fill(Color.yellow)
                .frame(width: 8, height: 8)
                .padding(.top, 6)

            Text(content.text)
                .foregroundColor(.white)
                .font(.system(.body, design: .monospaced))

            Spacer()
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Tool Call

struct ToolCallView: View {
    let content: ToolCallContent

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
                .padding(.top, 6)

            Text(content.tool)
                .foregroundColor(.cyan)
                .fontWeight(.medium)
            + Text("(")
                .foregroundColor(.white)
            + Text(displayParams)
                .foregroundColor(.white)
            + Text(")")
                .foregroundColor(.white)

            Spacer()

            if let duration = content.durationMs {
                Text("\(duration)ms")
                    .foregroundColor(.gray)
                    .font(.caption)
            }

            if content.status == .running {
                ProgressView()
                    .scaleEffect(0.7)
            }
        }
        .font(.system(.body, design: .monospaced))
        .padding(.vertical, 4)
    }

    var statusColor: Color {
        switch content.status {
        case .running: return .blue
        case .completed: return .yellow
        case .error: return .red
        case .interrupted: return .orange
        }
    }

    var displayParams: String {
        // Extract key params for display
        if let cmd = content.params["command"] as? String {
            return cmd.count > 60 ? String(cmd.prefix(60)) + "..." : cmd
        }
        if let path = content.params["file_path"] as? String {
            return path
        }
        if let pattern = content.params["pattern"] as? String {
            return "pattern: \"\(pattern)\""
        }
        return ""
    }
}

// MARK: - Tool Result

struct ToolResultView: View {
    let content: ToolResultContent
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .top, spacing: 8) {
                Text("└")
                    .foregroundColor(.gray)

                if content.isError {
                    Text(content.summary)
                        .foregroundColor(.red)
                } else {
                    Text(content.summary)
                        .foregroundColor(.white)
                }

                Spacer()

                if content.expandable && content.lineCount > 1 {
                    Button(action: { isExpanded.toggle() }) {
                        Text(isExpanded ? "collapse" : "… +\(content.lineCount - 1) lines")
                            .foregroundColor(.gray)
                            .font(.caption)
                    }
                }
            }

            if isExpanded {
                Text(content.fullContent)
                    .foregroundColor(content.isError ? .red : .white)
                    .padding(.leading, 24)
            }
        }
        .font(.system(.body, design: .monospaced))
        .padding(.vertical, 2)
    }
}

// MARK: - Diff View

struct DiffView: View {
    let content: DiffContent
    @State private var isExpanded = true

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            // Summary
            HStack(spacing: 8) {
                Text("└")
                    .foregroundColor(.gray)
                Text(content.summary.display)
                    .foregroundColor(.white)

                Spacer()

                Button(action: { isExpanded.toggle() }) {
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .foregroundColor(.gray)
                }
            }

            if isExpanded {
                // Diff lines
                ForEach(content.hunks, id: \.header) { hunk in
                    ForEach(hunk.lines) { line in
                        DiffLineView(line: line)
                    }
                }
            }
        }
        .font(.system(.body, design: .monospaced))
        .padding(.vertical, 2)
    }
}

struct DiffLineView: View {
    let line: DiffLine

    var body: some View {
        HStack(spacing: 0) {
            // Line number
            Text(lineNumber)
                .frame(width: 32, alignment: .trailing)
                .foregroundColor(.gray)

            // Change indicator
            Text(prefix)
                .frame(width: 16)
                .foregroundColor(prefixColor)

            // Content
            Text(line.content)
                .foregroundColor(.white)

            Spacer()
        }
        .background(backgroundColor)
        .padding(.leading, 24)
    }

    var lineNumber: String {
        if let num = line.newLine ?? line.oldLine {
            return String(num)
        }
        return ""
    }

    var prefix: String {
        switch line.type {
        case .added: return "+"
        case .removed: return "-"
        case .context: return " "
        }
    }

    var prefixColor: Color {
        switch line.type {
        case .added: return .green
        case .removed: return .red
        case .context: return .gray
        }
    }

    var backgroundColor: Color {
        switch line.type {
        case .added: return Color.green.opacity(0.2)
        case .removed: return Color.red.opacity(0.2)
        case .context: return Color.clear
        }
    }
}

// MARK: - Thinking View

struct ThinkingView: View {
    let content: ThinkingContent
    @State private var isExpanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button(action: { isExpanded.toggle() }) {
                HStack(spacing: 8) {
                    Image(systemName: isExpanded ? "chevron.down" : "chevron.right")
                        .foregroundColor(.gray)
                    Text("Thinking...")
                        .foregroundColor(.gray)
                        .italic()
                }
            }

            if isExpanded {
                Text(content.text)
                    .foregroundColor(.gray)
                    .font(.system(.caption, design: .monospaced))
                    .padding(.leading, 24)
            }
        }
        .padding(.vertical, 2)
    }
}

// MARK: - Interrupted View

struct InterruptedView: View {
    let content: InterruptedContent

    var body: some View {
        HStack(spacing: 8) {
            Text("└")
                .foregroundColor(.gray)
            Text("Interrupted")
                .foregroundColor(.orange)
                .fontWeight(.medium)
            Text("·")
                .foregroundColor(.gray)
            Text(content.message)
                .foregroundColor(.gray)
        }
        .font(.system(.body, design: .monospaced))
        .padding(.vertical, 2)
    }
}
```

### Chat Manager

```swift
import Foundation
import Combine

@MainActor
class ElementsChatManager: ObservableObject {
    @Published var elements: [ChatElement] = []
    @Published var isLoading = false
    @Published var hasMoreBefore = true

    private var oldestId: String?
    private var newestId: String?
    private var sessionId: String?
    private var webSocket: URLSessionWebSocketTask?
    private var cancellables = Set<AnyCancellable>()

    private let baseURL: String

    init(baseURL: String = "http://localhost:16180") {
        self.baseURL = baseURL
    }

    // MARK: - Initial Load

    func loadSession(sessionId: String) async throws {
        self.sessionId = sessionId
        isLoading = true

        let url = URL(string: "\(baseURL)/api/claude/sessions/elements?session_id=\(sessionId)&limit=30")!
        let (data, _) = try await URLSession.shared.data(from: url)
        let response = try JSONDecoder().decode(ElementsResponse.self, from: data)

        elements = response.elements
        oldestId = response.pagination.oldestId
        newestId = response.pagination.newestId
        hasMoreBefore = response.pagination.hasMoreBefore
        isLoading = false

        connectWebSocket()
    }

    // MARK: - Load Older (Scroll Up)

    func loadOlder() async throws {
        guard let sessionId = sessionId,
              let oldestId = oldestId,
              hasMoreBefore,
              !isLoading else { return }

        isLoading = true

        let url = URL(string: "\(baseURL)/api/claude/sessions/elements?session_id=\(sessionId)&before=\(oldestId)&limit=20")!
        let (data, _) = try await URLSession.shared.data(from: url)
        let response = try JSONDecoder().decode(ElementsResponse.self, from: data)

        elements.insert(contentsOf: response.elements, at: 0)
        self.oldestId = response.pagination.oldestId
        hasMoreBefore = response.pagination.hasMoreBefore
        isLoading = false
    }

    // MARK: - WebSocket

    private func connectWebSocket() {
        guard let url = URL(string: "ws://\(baseURL.replacingOccurrences(of: "http://", with: ""))/ws") else { return }

        webSocket = URLSession.shared.webSocketTask(with: url)
        webSocket?.resume()
        receiveMessage()
    }

    private func receiveMessage() {
        webSocket?.receive { [weak self] result in
            switch result {
            case .success(let message):
                if case .string(let text) = message {
                    self?.handleWebSocketMessage(text)
                }
                self?.receiveMessage()

            case .failure:
                // Reconnect after delay
                DispatchQueue.main.asyncAfter(deadline: .now() + 3) {
                    self?.connectWebSocket()
                }
            }
        }
    }

    private func handleWebSocketMessage(_ text: String) {
        guard let data = text.data(using: .utf8) else { return }

        do {
            let event = try JSONDecoder().decode(WebSocketEvent.self, from: data)

            DispatchQueue.main.async {
                switch event.event {
                case "claude_element":
                    if let element = event.payload?.element {
                        self.elements.append(element)
                        self.newestId = element.id
                    }

                case "claude_element_update":
                    if let elementId = event.payload?.elementId,
                       let updates = event.payload?.updates,
                       let index = self.elements.firstIndex(where: { $0.id == elementId }) {
                        // Apply updates to element
                        self.applyUpdates(at: index, updates: updates)
                    }

                default:
                    break
                }
            }
        } catch {
            print("Failed to decode WebSocket message: \(error)")
        }
    }

    private func applyUpdates(at index: Int, updates: [String: AnyCodable]) {
        // Update element properties based on updates dictionary
        // This would modify status, duration_ms, etc.
    }

    // MARK: - Cleanup

    func disconnect() {
        webSocket?.cancel(with: .goingAway, reason: nil)
        webSocket = nil
    }
}

// MARK: - Response Types

struct ElementsResponse: Codable {
    let sessionId: String
    let elements: [ChatElement]
    let pagination: ElementsPagination
}

struct ElementsPagination: Codable {
    let total: Int
    let returned: Int
    let hasMoreBefore: Bool
    let hasMoreAfter: Bool
    let oldestId: String?
    let newestId: String?
}

struct WebSocketEvent: Codable {
    let event: String
    let timestamp: String
    let payload: WebSocketPayload?
}

struct WebSocketPayload: Codable {
    let sessionId: String?
    let element: ChatElement?
    let elementId: String?
    let updates: [String: AnyCodable]?
}
```

---

## Implementation Status

- [ ] Session element parser (`internal/adapters/sessioncache/parser.go`)
- [ ] Elements API endpoint (`GET /api/claude/sessions/elements`)
- [ ] WebSocket `claude_element` event
- [ ] WebSocket `claude_element_update` event
- [ ] Diff parser for Update/Edit tool results
- [ ] Pagination support (before/after cursors)

---

*Document Version: 1.0*
*Last Updated: 2025-12-19*
