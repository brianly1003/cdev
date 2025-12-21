# Real-time Chat Integration Guide

This document describes the architecture for building a real-time chat UI that integrates with Claude via cdev.

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Solution Architecture](#solution-architecture)
3. [API Reference](#api-reference)
4. [WebSocket Events](#websocket-events)
5. [iOS Integration Guide](#ios-integration-guide)
6. [Message Data Structures](#message-data-structures)
7. [Error Handling](#error-handling)

---

## Problem Statement

### Current Limitations

When building a chat UI, fetching the entire conversation history on every update is inefficient:

```
Problem: Full history fetch
┌─────────────┐                              ┌─────────────┐
│   iOS App   │  GET /sessions/messages      │ cdev  │
│             │ ─────────────────────────────▶│             │
│             │ ◀───────────────────────────── │             │
└─────────────┘   Returns ALL 85+ messages   └─────────────┘
                  (grows with conversation)
```

**Issues:**
- Bandwidth increases as conversation grows
- Slow UI updates
- Redundant data transfer
- Poor user experience on mobile networks

---

## Solution Architecture

### Event Sourcing + Incremental Sync Pattern

Similar to WhatsApp/Telegram/Slack - only transfer what's needed:

```
┌─────────────────────────────────────────────────────────────────────┐
│                           iOS App                                    │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  STARTUP:                                                            │
│    1. Connect WebSocket (wss://host:port/ws)                         │
│    2. GET /api/claude/sessions/messages?limit=20 (last 20 only)      │
│    3. Store cursors: oldest_uuid, newest_uuid                        │
│                                                                      │
│  REAL-TIME (during Claude execution):                                │
│    4. Receive "claude_message" WebSocket events                      │
│    5. Append SINGLE message to chat UI                               │
│    6. Update newest_uuid cursor                                      │
│                                                                      │
│  SCROLL UP (load older messages):                                    │
│    7. GET /api/claude/sessions/messages?before=<oldest_uuid>&limit=20│
│    8. Prepend messages to chat UI                                    │
│    9. Update oldest_uuid cursor                                      │
│                                                                      │
│  RECONNECT/RESUME:                                                   │
│    10. GET /api/claude/sessions/messages?after=<newest_uuid>         │
│    11. Append missed messages                                        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Data Flow Diagram

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────────────┐
│  Claude CLI  │────▶│  cdev  │────▶│  Session File (.jsonl)   │
│              │     │              │     │  ~/.claude/projects/...  │
└──────────────┘     └──────┬───────┘     └──────────────────────────┘
                           │                         │
                           │ Watches file            │
                           │◀────────────────────────┘
                           │
              ┌────────────┴────────────┐
              │                         │
              ▼                         ▼
    ┌─────────────────┐      ┌─────────────────────┐
    │  WebSocket      │      │  REST API           │
    │  claude_message │      │  /sessions/messages │
    │  (real-time)    │      │  (pagination)       │
    └────────┬────────┘      └──────────┬──────────┘
             │                          │
             └──────────┬───────────────┘
                        │
                        ▼
                 ┌─────────────┐
                 │   iOS App   │
                 └─────────────┘
```

---

## API Reference

### GET /api/claude/sessions/messages

Retrieves messages from a Claude session with pagination support.

**Endpoint:** `GET /api/claude/sessions/messages`

**Query Parameters:**

| Parameter    | Type    | Required | Default | Description |
|--------------|---------|----------|---------|-------------|
| `session_id` | string  | Yes      | -       | Claude session UUID |
| `limit`      | integer | No       | 50      | Number of messages to return (max 100) |
| `before`     | string  | No       | -       | Return messages BEFORE this UUID (for scroll up) |
| `after`      | string  | No       | -       | Return messages AFTER this UUID (for catch-up) |

**Response:**

```json
{
  "messages": [
    {
      "type": "user",
      "uuid": "dda4439d-13cc-42e3-9e13-5463c4eb05dd",
      "sessionId": "decda6d9-5311-42ff-90f3-9b8895a03cdd",
      "timestamp": "2025-12-18T13:49:39.753Z",
      "message": {
        "role": "user",
        "content": "can you check length of main.ts file"
      }
    },
    {
      "type": "assistant",
      "uuid": "8ffb5143-b3a5-454e-85a0-f41fb22ba22c",
      "sessionId": "decda6d9-5311-42ff-90f3-9b8895a03cdd",
      "timestamp": "2025-12-18T13:49:43.552Z",
      "message": {
        "role": "assistant",
        "content": [
          {
            "type": "tool_use",
            "id": "toolu_01APUK14Bdtn9KdFSBbSHVwG",
            "name": "Bash",
            "input": {
              "command": "wc -l src/main.ts",
              "description": "Count lines in main.ts file"
            }
          }
        ]
      }
    }
  ],
  "pagination": {
    "total": 85,
    "returned": 20,
    "has_more_before": true,
    "has_more_after": false,
    "oldest_uuid": "dda4439d-13cc-42e3-9e13-5463c4eb05dd",
    "newest_uuid": "8ffb5143-b3a5-454e-85a0-f41fb22ba22c"
  }
}
```

**Usage Examples:**

```bash
# Initial load - get last 20 messages
curl "http://localhost:8766/api/claude/sessions/messages?session_id=decda6d9-5311-42ff-90f3-9b8895a03cdd&limit=20"

# Scroll up - load older messages
curl "http://localhost:8766/api/claude/sessions/messages?session_id=decda6d9-5311-42ff-90f3-9b8895a03cdd&before=dda4439d-13cc-42e3-9e13-5463c4eb05dd&limit=20"

# Catch-up - load messages after reconnect
curl "http://localhost:8766/api/claude/sessions/messages?session_id=decda6d9-5311-42ff-90f3-9b8895a03cdd&after=8ffb5143-b3a5-454e-85a0-f41fb22ba22c"
```

---

## WebSocket Events

### Connection

```
WebSocket URL: ws://localhost:8766/ws
```

### Event: `claude_message`

Emitted in real-time when Claude writes a new message to the session.

**Payload:**

```json
{
  "event": "claude_message",
  "timestamp": "2025-12-19T02:30:00.123Z",
  "payload": {
    "type": "assistant",
    "uuid": "8ffb5143-b3a5-454e-85a0-f41fb22ba22c",
    "sessionId": "decda6d9-5311-42ff-90f3-9b8895a03cdd",
    "timestamp": "2025-12-19T02:30:00.000Z",
    "message": {
      "role": "assistant",
      "content": [
        {
          "type": "text",
          "text": "I'll help you with that."
        },
        {
          "type": "tool_use",
          "id": "toolu_01XYZ",
          "name": "Read",
          "input": {
            "file_path": "/path/to/file.ts"
          }
        }
      ]
    }
  }
}
```

### Other Relevant Events

| Event | Description |
|-------|-------------|
| `claude_status` | Claude state changes (running, idle, error) |
| `claude_session_info` | Session ID when Claude starts |
| `heartbeat` | Connection keep-alive (every 30s) |

---

## iOS Integration Guide

### Swift Implementation Example

```swift
import Foundation

// MARK: - Data Models

struct SessionMessage: Codable {
    let type: String  // "user" or "assistant"
    let uuid: String
    let sessionId: String
    let timestamp: String
    let message: MessageContent
}

struct MessageContent: Codable {
    let role: String
    let content: MessageContentValue
}

// Content can be string (user) or array (assistant)
enum MessageContentValue: Codable {
    case text(String)
    case blocks([ContentBlock])

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let text = try? container.decode(String.self) {
            self = .text(text)
        } else if let blocks = try? container.decode([ContentBlock].self) {
            self = .blocks(blocks)
        } else {
            throw DecodingError.typeMismatch(MessageContentValue.self, .init(codingPath: [], debugDescription: "Unknown content type"))
        }
    }
}

struct ContentBlock: Codable {
    let type: String  // "text", "tool_use", "tool_result", "thinking"
    let text: String?
    let id: String?
    let name: String?
    let input: [String: AnyCodable]?
    let content: String?
    let toolUseId: String?
    let isError: Bool?
}

struct PaginationInfo: Codable {
    let total: Int
    let returned: Int
    let hasMoreBefore: Bool
    let hasMoreAfter: Bool
    let oldestUuid: String?
    let newestUuid: String?
}

struct MessagesResponse: Codable {
    let messages: [SessionMessage]
    let pagination: PaginationInfo
}

// MARK: - Chat Manager

class ChatManager: ObservableObject {
    @Published var messages: [SessionMessage] = []
    @Published var isLoading = false
    @Published var hasMoreOlder = true

    private var oldestUuid: String?
    private var newestUuid: String?
    private var sessionId: String?
    private var webSocket: URLSessionWebSocketTask?

    private let baseURL = "http://localhost:8766"

    // MARK: - Initial Load

    func loadInitialMessages(sessionId: String) async throws {
        self.sessionId = sessionId
        isLoading = true

        let url = URL(string: "\(baseURL)/api/claude/sessions/messages?session_id=\(sessionId)&limit=20")!
        let (data, _) = try await URLSession.shared.data(from: url)
        let response = try JSONDecoder().decode(MessagesResponse.self, from: data)

        await MainActor.run {
            self.messages = response.messages
            self.oldestUuid = response.pagination.oldestUuid
            self.newestUuid = response.pagination.newestUuid
            self.hasMoreOlder = response.pagination.hasMoreBefore
            self.isLoading = false
        }

        // Connect WebSocket for real-time updates
        connectWebSocket()
    }

    // MARK: - Load Older (Scroll Up)

    func loadOlderMessages() async throws {
        guard let sessionId = sessionId,
              let oldestUuid = oldestUuid,
              hasMoreOlder,
              !isLoading else { return }

        isLoading = true

        let url = URL(string: "\(baseURL)/api/claude/sessions/messages?session_id=\(sessionId)&before=\(oldestUuid)&limit=20")!
        let (data, _) = try await URLSession.shared.data(from: url)
        let response = try JSONDecoder().decode(MessagesResponse.self, from: data)

        await MainActor.run {
            // Prepend older messages
            self.messages.insert(contentsOf: response.messages, at: 0)
            self.oldestUuid = response.pagination.oldestUuid
            self.hasMoreOlder = response.pagination.hasMoreBefore
            self.isLoading = false
        }
    }

    // MARK: - WebSocket Real-time Updates

    private func connectWebSocket() {
        let url = URL(string: "ws://localhost:8766/ws")!
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
                self?.receiveMessage() // Continue listening

            case .failure(let error):
                print("WebSocket error: \(error)")
                // Reconnect after delay
                DispatchQueue.main.asyncAfter(deadline: .now() + 5) {
                    self?.connectWebSocket()
                }
            }
        }
    }

    private func handleWebSocketMessage(_ text: String) {
        guard let data = text.data(using: .utf8) else { return }

        struct WebSocketEvent: Codable {
            let event: String
            let payload: SessionMessage?
        }

        guard let event = try? JSONDecoder().decode(WebSocketEvent.self, from: data),
              event.event == "claude_message",
              let message = event.payload else { return }

        DispatchQueue.main.async {
            // Append new message
            self.messages.append(message)
            self.newestUuid = message.uuid
        }
    }

    // MARK: - Reconnect Catch-up

    func catchUpMissedMessages() async throws {
        guard let sessionId = sessionId,
              let newestUuid = newestUuid else { return }

        let url = URL(string: "\(baseURL)/api/claude/sessions/messages?session_id=\(sessionId)&after=\(newestUuid)")!
        let (data, _) = try await URLSession.shared.data(from: url)
        let response = try JSONDecoder().decode(MessagesResponse.self, from: data)

        await MainActor.run {
            // Append missed messages
            self.messages.append(contentsOf: response.messages)
            if let newest = response.pagination.newestUuid {
                self.newestUuid = newest
            }
        }
    }
}
```

### SwiftUI Chat View Example

```swift
import SwiftUI

struct ChatView: View {
    @StateObject private var chatManager = ChatManager()
    @State private var sessionId: String

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    // Load more indicator
                    if chatManager.hasMoreOlder {
                        ProgressView()
                            .onAppear {
                                Task {
                                    try? await chatManager.loadOlderMessages()
                                }
                            }
                    }

                    // Messages
                    ForEach(chatManager.messages, id: \.uuid) { message in
                        MessageBubble(message: message)
                            .id(message.uuid)
                    }
                }
                .padding()
            }
            .onChange(of: chatManager.messages.count) { _ in
                // Scroll to bottom on new message
                if let lastId = chatManager.messages.last?.uuid {
                    withAnimation {
                        proxy.scrollTo(lastId, anchor: .bottom)
                    }
                }
            }
        }
        .task {
            try? await chatManager.loadInitialMessages(sessionId: sessionId)
        }
    }
}

struct MessageBubble: View {
    let message: SessionMessage

    var body: some View {
        HStack {
            if message.type == "assistant" {
                assistantBubble
                Spacer()
            } else {
                Spacer()
                userBubble
            }
        }
    }

    @ViewBuilder
    private var assistantBubble: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Render content blocks
            switch message.message.content {
            case .text(let text):
                Text(text)
            case .blocks(let blocks):
                ForEach(blocks.indices, id: \.self) { index in
                    ContentBlockView(block: blocks[index])
                }
            }
        }
        .padding()
        .background(Color.gray.opacity(0.2))
        .cornerRadius(12)
    }

    @ViewBuilder
    private var userBubble: some View {
        // Similar implementation for user messages
        Text(getUserText())
            .padding()
            .background(Color.blue)
            .foregroundColor(.white)
            .cornerRadius(12)
    }

    private func getUserText() -> String {
        switch message.message.content {
        case .text(let text):
            return text
        case .blocks(let blocks):
            return blocks.first?.content ?? blocks.first?.text ?? ""
        }
    }
}

struct ContentBlockView: View {
    let block: ContentBlock

    var body: some View {
        switch block.type {
        case "text":
            Text(block.text ?? "")

        case "thinking":
            DisclosureGroup("Thinking...") {
                Text(block.text ?? "")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

        case "tool_use":
            ToolCallView(name: block.name ?? "", input: block.input)

        case "tool_result":
            ToolResultView(content: block.content ?? "", isError: block.isError ?? false)

        default:
            EmptyView()
        }
    }
}
```

---

## Message Data Structures

### Content Block Types

| Type | Description | Fields |
|------|-------------|--------|
| `text` | Plain text response | `text` |
| `thinking` | Claude's reasoning (collapsible) | `text` |
| `tool_use` | Tool call (Bash, Read, Write, etc.) | `id`, `name`, `input` |
| `tool_result` | Result from tool execution | `tool_use_id`, `content`, `is_error` |

### Tool Use Examples

**Bash Command:**
```json
{
  "type": "tool_use",
  "id": "toolu_01ABC",
  "name": "Bash",
  "input": {
    "command": "ls -la",
    "description": "List directory contents"
  }
}
```

**File Read:**
```json
{
  "type": "tool_use",
  "id": "toolu_01DEF",
  "name": "Read",
  "input": {
    "file_path": "/path/to/file.ts"
  }
}
```

**File Edit:**
```json
{
  "type": "tool_use",
  "id": "toolu_01GHI",
  "name": "Edit",
  "input": {
    "file_path": "/path/to/file.ts",
    "old_string": "original code",
    "new_string": "updated code"
  }
}
```

### Tool Result Example

```json
{
  "type": "tool_result",
  "tool_use_id": "toolu_01ABC",
  "content": "total 1152\ndrwxr-xr-x  41 user  staff  1312 Dec 18 03:11 .\n...",
  "is_error": false
}
```

---

## Error Handling

### API Errors

| Status Code | Description | Action |
|-------------|-------------|--------|
| 400 | Missing session_id | Check request parameters |
| 404 | Session not found | Session may have been deleted |
| 500 | Internal error | Retry with exponential backoff |
| 504 | Timeout | Retry request |

### WebSocket Reconnection

```swift
// Recommended reconnection strategy
func reconnect() {
    let delays = [1, 2, 4, 8, 16, 32] // seconds
    var attempt = 0

    func tryConnect() {
        connectWebSocket()

        // If failed, retry with backoff
        DispatchQueue.main.asyncAfter(deadline: .now() + Double(delays[min(attempt, delays.count - 1)])) {
            if !isConnected {
                attempt += 1
                tryConnect()
            }
        }
    }

    tryConnect()
}
```

---

## Summary

### Key Endpoints

| Endpoint | Purpose |
|----------|---------|
| `GET /api/claude/sessions/messages?limit=20` | Initial load |
| `GET /api/claude/sessions/messages?before=<uuid>` | Scroll up (older) |
| `GET /api/claude/sessions/messages?after=<uuid>` | Catch-up (newer) |
| `WebSocket /ws` → `claude_message` event | Real-time updates |

### Benefits

- **Efficient**: Only transfer needed messages
- **Real-time**: Instant updates via WebSocket
- **Scalable**: Works with any conversation length
- **Resumable**: Catch up from any point
- **Mobile-friendly**: Minimal bandwidth usage

---

## Implementation Status

- [ ] API: `before` parameter for pagination
- [ ] API: `after` parameter for catch-up
- [ ] API: Pagination metadata in response
- [ ] WebSocket: `claude_message` event with structured data
- [ ] Session file watcher for real-time updates

---

*Document Version: 1.0*
*Last Updated: 2025-12-19*
