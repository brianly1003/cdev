# VS Code Integration Strategy

**Version:** 1.0.0
**Status:** Strategic Planning
**Last Updated:** December 2025

---

## Executive Summary

This document provides a deep technical analysis of how to make cdev seamlessly integrable with VS Code and similar IDEs. It identifies specific protocol gaps, provides migration strategies, and outlines what to change NOW vs LATER to minimize future restructuring.

### Key Insight

> **VS Code's power comes from protocols, not code.**

VS Code's extension ecosystem is built on standardized protocols:
- **Language Server Protocol (LSP)** - Language intelligence
- **Debug Adapter Protocol (DAP)** - Debugging
- **Extension API** - UI/workspace integration

By aligning cdev with these patterns, we become a natural fit for any IDE acquisition.

---

## Current State Analysis

### Current Protocol Structure

```
┌─────────────────────────────────────────────────────────────────┐
│                    CURRENT cdev PROTOCOL                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Commands (Client → Server):                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ {                                                         │  │
│  │   "command": "agent/run",        ← snake_case            │  │
│  │   "request_id": "req-001",        ← custom correlation    │  │
│  │   "payload": { "prompt": "..." }  ← nested payload        │  │
│  │ }                                                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Events (Server → Client):                                      │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ {                                                         │  │
│  │   "event": "claude_log",          ← snake_case            │  │
│  │   "timestamp": "...",             ← custom field          │  │
│  │   "payload": { ... }              ← nested payload        │  │
│  │ }                                                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Transport: WebSocket (port 8765) + HTTP (port 16180)           │
│  No: Initialize handshake, capability negotiation, stdio       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### VS Code Expected Protocol Structure (LSP/DAP Pattern)

```
┌─────────────────────────────────────────────────────────────────┐
│                    JSON-RPC 2.0 PROTOCOL                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  Request (Client → Server):                                     │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ {                                                         │  │
│  │   "jsonrpc": "2.0",               ← protocol identifier   │  │
│  │   "id": 1,                        ← integer/string id     │  │
│  │   "method": "claude/run",         ← namespace/method      │  │
│  │   "params": { "prompt": "..." }   ← flat params           │  │
│  │ }                                                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Response (Server → Client):                                    │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ {                                                         │  │
│  │   "jsonrpc": "2.0",                                       │  │
│  │   "id": 1,                        ← matches request       │  │
│  │   "result": { ... }               ← or "error": {...}     │  │
│  │ }                                                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Notification (no response expected):                           │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ {                                                         │  │
│  │   "jsonrpc": "2.0",                                       │  │
│  │   "method": "claude/log",         ← no id = notification  │  │
│  │   "params": { ... }                                       │  │
│  │ }                                                         │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                  │
│  Transport: stdio (primary), WebSocket, TCP                     │
│  Required: Initialize handshake, capability negotiation         │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## Gap Analysis

### Critical Gaps (MUST fix for VS Code integration)

| Gap | Current | Required | Impact | Effort |
|-----|---------|----------|--------|--------|
| **Message Format** | Custom `{command, payload}` | JSON-RPC 2.0 `{jsonrpc, method, params}` | Breaking | 1 week |
| **Transport** | WebSocket only | stdio (primary), WebSocket | Blocking | 3 days |
| **Initialize** | None | `initialize` / `initialized` handshake | Blocking | 2 days |
| **Capability Negotiation** | None | Client/server capability exchange | Required | 2 days |
| **Method Names** | `agent/run` | `claude/run` | Easy | 1 day |
| **Error Format** | Custom | JSON-RPC error codes | Required | 1 day |

### Important Gaps (Should fix)

| Gap | Current | Required | Impact | Effort |
|-----|---------|----------|--------|--------|
| **Shutdown** | None | `shutdown` / `exit` sequence | Important | 1 day |
| **Progress Reporting** | Ad-hoc events | `$/progress` notifications | Nice-to-have | 2 days |
| **Cancellation** | `agent/stop` | `$/cancelRequest` | Nice-to-have | 1 day |
| **Tracing** | Logs | `$/logTrace` | Nice-to-have | 1 day |

---

## Protocol Mapping

### Method Name Migration

| Current | JSON-RPC 2.0 | Type |
|---------|--------------|------|
| `agent/run` | `claude/run` | Request |
| `agent/stop` | `claude/stop` | Request |
| `agent/respond` | `claude/respond` | Request |
| `status/get` | `cdev/status` | Request |
| `file/get` | `files/read` | Request |
| `session/watch` | `session/watch` | Request |
| `session/unwatch` | `session/unwatch` | Request |

### Event to Notification Migration

| Current Event | JSON-RPC Notification | Notes |
|---------------|----------------------|-------|
| `claude_log` | `claude/log` | Main output stream |
| `claude_status` | `claude/status` | State changes |
| `claude_permission` | `claude/permission` | Tool approval requests |
| `claude_waiting` | `claude/waiting` | Interactive prompts |
| `file_changed` | `files/changed` | File system events |
| `git_diff` | `git/diff` | Diff notifications |
| `git_status_changed` | `git/status` | Git state changes |
| `session_start` | `session/started` | Connection established |
| `heartbeat` | `$/heartbeat` | Keep-alive |

---

## Implementation Strategy: Dual Protocol Support

### Phase 1: Add JSON-RPC 2.0 Support (Parallel)

Instead of breaking existing clients, support BOTH protocols during migration.

```go
// internal/protocol/router.go

type MessageRouter struct {
    legacyHandler  LegacyHandler   // Current protocol
    jsonrpcHandler JSONRPCHandler  // New protocol
}

func (r *MessageRouter) Route(data []byte) ([]byte, error) {
    // Detect protocol by checking for "jsonrpc" field
    if isJSONRPC(data) {
        return r.jsonrpcHandler.Handle(data)
    }
    // Fall back to legacy protocol
    return r.legacyHandler.Handle(data)
}

func isJSONRPC(data []byte) bool {
    // Quick check for "jsonrpc" field
    return bytes.Contains(data, []byte(`"jsonrpc"`))
}
```

### Phase 2: stdio Transport for VS Code

VS Code extensions typically spawn a child process and communicate via stdio.

```go
// internal/transport/stdio.go

package transport

import (
    "bufio"
    "fmt"
    "io"
    "strconv"
    "strings"
)

// StdioTransport implements LSP-style stdio communication
type StdioTransport struct {
    reader *bufio.Reader
    writer io.Writer
}

func NewStdioTransport(r io.Reader, w io.Writer) *StdioTransport {
    return &StdioTransport{
        reader: bufio.NewReader(r),
        writer: w,
    }
}

// ReadMessage reads an LSP-style message with Content-Length header
func (t *StdioTransport) ReadMessage() ([]byte, error) {
    // Read headers
    var contentLength int
    for {
        line, err := t.reader.ReadString('\n')
        if err != nil {
            return nil, err
        }
        line = strings.TrimSpace(line)
        if line == "" {
            break // End of headers
        }
        if strings.HasPrefix(line, "Content-Length:") {
            lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
            contentLength, err = strconv.Atoi(lengthStr)
            if err != nil {
                return nil, fmt.Errorf("invalid Content-Length: %w", err)
            }
        }
    }

    if contentLength == 0 {
        return nil, fmt.Errorf("missing Content-Length header")
    }

    // Read body
    body := make([]byte, contentLength)
    _, err := io.ReadFull(t.reader, body)
    return body, err
}

// WriteMessage writes an LSP-style message with Content-Length header
func (t *StdioTransport) WriteMessage(data []byte) error {
    header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
    _, err := t.writer.Write([]byte(header))
    if err != nil {
        return err
    }
    _, err = t.writer.Write(data)
    return err
}
```

### Phase 3: Initialize Handshake

```go
// internal/protocol/jsonrpc/initialize.go

package jsonrpc

// InitializeParams matches LSP pattern
type InitializeParams struct {
    // Protocol version
    ProtocolVersion string `json:"protocolVersion"`

    // Client information
    ClientInfo *ClientInfo `json:"clientInfo,omitempty"`

    // Client capabilities
    Capabilities ClientCapabilities `json:"capabilities"`

    // Root path of the workspace
    RootPath string `json:"rootPath,omitempty"`
}

type ClientInfo struct {
    Name    string `json:"name"`
    Version string `json:"version,omitempty"`
}

type ClientCapabilities struct {
    Claude *ClaudeClientCapabilities `json:"claude,omitempty"`
    Git    *GitClientCapabilities    `json:"git,omitempty"`
    Files  *FilesClientCapabilities  `json:"files,omitempty"`
}

type ClaudeClientCapabilities struct {
    Streaming           bool `json:"streaming,omitempty"`
    Permissions         bool `json:"permissions,omitempty"`
    InteractivePrompts  bool `json:"interactivePrompts,omitempty"`
    SessionContinuation bool `json:"sessionContinuation,omitempty"`
}

// InitializeResult is the server's response
type InitializeResult struct {
    ProtocolVersion string             `json:"protocolVersion"`
    ServerInfo      *ServerInfo        `json:"serverInfo"`
    Capabilities    ServerCapabilities `json:"capabilities"`
}

type ServerInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

type ServerCapabilities struct {
    Claude *ClaudeServerCapabilities `json:"claude,omitempty"`
    Git    *GitServerCapabilities    `json:"git,omitempty"`
    Files  *FilesServerCapabilities  `json:"files,omitempty"`
}
```

---

## VS Code Extension Architecture

### Extension Structure

```
vscode-cdev/
├── package.json              # Extension manifest
├── src/
│   ├── extension.ts          # Entry point
│   ├── client/
│   │   ├── index.ts          # CdevClient class
│   │   ├── protocol.ts       # JSON-RPC types
│   │   └── transport.ts      # Transport abstraction
│   ├── views/
│   │   ├── claudePanel.ts    # Webview for Claude output
│   │   ├── permissionDialog.ts
│   │   └── statusBar.ts
│   └── commands/
│       ├── runClaude.ts
│       ├── stopClaude.ts
│       └── approvePermission.ts
└── syntaxes/
    └── claude-output.tmLanguage.json
```

### Extension Entry Point

```typescript
// src/extension.ts

import * as vscode from 'vscode';
import { CdevClient, StdioTransport, WebSocketTransport } from './client';

let client: CdevClient | undefined;

export async function activate(context: vscode.ExtensionContext) {
    // Determine transport based on configuration
    const config = vscode.workspace.getConfiguration('cdev');
    const transport = config.get<string>('transport', 'stdio');

    if (transport === 'stdio') {
        // Spawn cdev process
        const serverPath = config.get<string>('serverPath', 'cdev');
        const args = ['start', '--transport', 'stdio'];

        client = new CdevClient({
            transport: new StdioTransport(serverPath, args)
        });
    } else {
        // Connect via WebSocket
        const wsUrl = config.get<string>('wsUrl', 'ws://localhost:8765');
        client = new CdevClient({
            transport: new WebSocketTransport(wsUrl)
        });
    }

    // Initialize
    const result = await client.initialize({
        protocolVersion: '1.0.0',
        clientInfo: {
            name: 'vscode-cdev',
            version: context.extension.packageJSON.version
        },
        capabilities: {
            claude: {
                streaming: true,
                permissions: true,
                interactivePrompts: true
            }
        },
        rootPath: vscode.workspace.rootPath
    });

    // Register commands
    context.subscriptions.push(
        vscode.commands.registerCommand('cdev.runClaude', runClaudeCommand),
        vscode.commands.registerCommand('cdev.stopClaude', stopClaudeCommand),
        vscode.commands.registerCommand('cdev.approvePermission', approvePermissionCommand)
    );

    // Subscribe to notifications
    client.onNotification('claude/log', handleClaudeLog);
    client.onNotification('claude/permission', handlePermissionRequest);
    client.onNotification('claude/status', handleStatusChange);

    // Status bar
    const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left);
    statusBar.text = '$(robot) Claude: Ready';
    statusBar.show();
    context.subscriptions.push(statusBar);
}

export function deactivate() {
    if (client) {
        return client.shutdown();
    }
}
```

### TypeScript Client SDK

```typescript
// @anthropic/cdev-client

export interface Transport {
    send(message: string): Promise<void>;
    onMessage(handler: (message: string) => void): void;
    close(): Promise<void>;
}

export class CdevClient {
    private transport: Transport;
    private pendingRequests: Map<number | string, {
        resolve: (result: any) => void;
        reject: (error: any) => void;
    }> = new Map();
    private nextId = 1;
    private notificationHandlers: Map<string, (params: any) => void> = new Map();

    constructor(options: { transport: Transport }) {
        this.transport = options.transport;
        this.transport.onMessage(this.handleMessage.bind(this));
    }

    async initialize(params: InitializeParams): Promise<InitializeResult> {
        return this.request('initialize', params);
    }

    async shutdown(): Promise<void> {
        await this.request('shutdown', {});
        await this.notification('exit', {});
        await this.transport.close();
    }

    // Claude methods
    async claudeRun(params: ClaudeRunParams): Promise<ClaudeRunResult> {
        return this.request('claude/run', params);
    }

    async claudeStop(): Promise<void> {
        return this.request('claude/stop', {});
    }

    async claudeRespond(params: ClaudeRespondParams): Promise<void> {
        return this.request('claude/respond', params);
    }

    // Subscribe to notifications
    onNotification(method: string, handler: (params: any) => void): void {
        this.notificationHandlers.set(method, handler);
    }

    private async request<T>(method: string, params: any): Promise<T> {
        const id = this.nextId++;
        const message: JSONRPCRequest = {
            jsonrpc: '2.0',
            id,
            method,
            params
        };

        return new Promise((resolve, reject) => {
            this.pendingRequests.set(id, { resolve, reject });
            this.transport.send(JSON.stringify(message));
        });
    }

    private async notification(method: string, params: any): Promise<void> {
        const message: JSONRPCNotification = {
            jsonrpc: '2.0',
            method,
            params
        };
        await this.transport.send(JSON.stringify(message));
    }

    private handleMessage(data: string): void {
        const message = JSON.parse(data);

        if ('id' in message) {
            // Response to a request
            const pending = this.pendingRequests.get(message.id);
            if (pending) {
                this.pendingRequests.delete(message.id);
                if ('error' in message) {
                    pending.reject(new JSONRPCError(message.error));
                } else {
                    pending.resolve(message.result);
                }
            }
        } else if ('method' in message) {
            // Notification
            const handler = this.notificationHandlers.get(message.method);
            if (handler) {
                handler(message.params);
            }
        }
    }
}
```

---

## Error Code Mapping

### JSON-RPC 2.0 Standard Errors

| Code | Message | Meaning |
|------|---------|---------|
| -32700 | Parse error | Invalid JSON |
| -32600 | Invalid Request | Not valid JSON-RPC |
| -32601 | Method not found | Unknown method |
| -32602 | Invalid params | Invalid method parameters |
| -32603 | Internal error | Server error |
| -32000 to -32099 | Server error | Reserved for implementation |

### cdev Custom Error Codes

| Code | Message | Current Mapping |
|------|---------|-----------------|
| -32001 | Claude already running | `CLAUDE_ALREADY_RUNNING` |
| -32002 | Claude not running | `CLAUDE_NOT_RUNNING` |
| -32003 | Session not found | `SESSION_NOT_FOUND` |
| -32004 | File not found | `FILE_NOT_FOUND` |
| -32005 | File too large | `FILE_TOO_LARGE` |
| -32006 | Path traversal | `PATH_TRAVERSAL` |
| -32007 | Git error | `GIT_ERROR` |

---

## Migration Timeline

### Immediate (Week 1-2)

```
┌─────────────────────────────────────────────────────────────────┐
│                    IMMEDIATE ACTIONS                            │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Create transport abstraction layer                          │
│     ├── internal/transport/transport.go (interface)             │
│     ├── internal/transport/websocket.go (existing, refactored)  │
│     └── internal/transport/stdio.go (new)                       │
│                                                                  │
│  2. Create JSON-RPC message types                               │
│     └── internal/protocol/jsonrpc/types.go                      │
│                                                                  │
│  3. Create message router with protocol detection               │
│     └── internal/protocol/router.go                             │
│                                                                  │
│  4. Add --transport flag to CLI                                 │
│     └── cmd/cdev/start.go                                       │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Short-term (Week 3-4)

```
┌─────────────────────────────────────────────────────────────────┐
│                    SHORT-TERM ACTIONS                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Implement initialize/shutdown lifecycle                     │
│                                                                  │
│  2. Implement capability negotiation                            │
│                                                                  │
│  3. Create TypeScript SDK with dual transport support           │
│                                                                  │
│  4. Create VS Code extension proof-of-concept                   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### Medium-term (Month 2-3)

```
┌─────────────────────────────────────────────────────────────────┐
│                    MEDIUM-TERM ACTIONS                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. Add progress reporting ($/progress)                         │
│                                                                  │
│  2. Add request cancellation ($/cancelRequest)                  │
│                                                                  │
│  3. Publish TypeScript SDK to npm                               │
│                                                                  │
│  4. Full VS Code extension with UI                              │
│                                                                  │
│  5. JetBrains plugin exploration                                │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## What to Do NOW

### 1. Create Transport Interface (Do First)

This is the foundation for supporting multiple transports:

```go
// internal/transport/transport.go

package transport

import (
    "context"
    "io"
)

// Transport defines the communication layer interface
type Transport interface {
    // Read reads the next message from the transport
    Read() ([]byte, error)

    // Write writes a message to the transport
    Write(data []byte) error

    // Close closes the transport
    Close() error
}

// Info returns transport metadata
type Info struct {
    Type     string // "websocket", "stdio", "tcp"
    ClientID string // Unique client identifier
}

// Manager manages multiple transports
type Manager struct {
    transports map[string]Transport
}
```

### 2. Add Protocol Detection

```go
// internal/protocol/detect.go

package protocol

type ProtocolType int

const (
    ProtocolLegacy ProtocolType = iota
    ProtocolJSONRPC
)

func DetectProtocol(data []byte) ProtocolType {
    // Quick heuristic: JSON-RPC has "jsonrpc" field
    if bytes.Contains(data, []byte(`"jsonrpc"`)) {
        return ProtocolJSONRPC
    }
    // Legacy has "command" or "event" field
    return ProtocolLegacy
}
```

### 3. Namespace Method Names Now

Even before full JSON-RPC migration, start using namespaced method names:

```go
// New method constants (alongside old ones for compatibility)
const (
    // Legacy (keep for backward compatibility)
    CommandRunClaude CommandType = "agent/run"

    // New namespaced versions
    MethodClaudeRun    = "claude/run"
    MethodClaudeStop   = "claude/stop"
    MethodClaudeRespond = "claude/respond"
)
```

---

## Compatibility with Other Protocols

### Model Context Protocol (MCP)

Anthropic's MCP uses similar JSON-RPC 2.0 patterns. Our protocol should be compatible:

```json
// MCP-style resource
{
  "jsonrpc": "2.0",
  "method": "resources/read",
  "params": {
    "uri": "file:///path/to/file.ts"
  }
}

// cdev equivalent
{
  "jsonrpc": "2.0",
  "method": "files/read",
  "params": {
    "path": "/path/to/file.ts"
  }
}
```

### Debug Adapter Protocol (DAP)

DAP also uses JSON messages. cdev can expose DAP-compatible debugging:

```json
{
  "seq": 1,
  "type": "request",
  "command": "launch",
  "arguments": {
    "program": "${workspaceFolder}/main.ts"
  }
}
```

---

## Summary: Acquisition Readiness Score

| Criteria | Current | After Phase 1 | After Phase 2 |
|----------|---------|---------------|---------------|
| JSON-RPC 2.0 | 0% | 100% | 100% |
| stdio Transport | 0% | 100% | 100% |
| Initialize/Shutdown | 0% | 100% | 100% |
| Capability Negotiation | 0% | 100% | 100% |
| TypeScript SDK | 0% | 50% | 100% |
| VS Code Extension | 0% | 30% | 80% |
| Enterprise Auth | 0% | 0% | 50% |
| **Overall Score** | **5%** | **65%** | **85%** |

---

## Conclusion

The key changes for VS Code integration readiness are:

1. **Transport abstraction** - Support stdio alongside WebSocket
2. **JSON-RPC 2.0** - Standard message format
3. **Initialize handshake** - Capability negotiation
4. **Namespaced methods** - `claude/run` instead of `agent/run`

By implementing these in parallel with existing protocol support, we can:
- Maintain backward compatibility with iOS app
- Enable VS Code extension development
- Position for acquisition by any IDE vendor

The investment is approximately 4-6 weeks for Phase 1, with incremental improvements thereafter.

---

## References

- [Language Server Protocol Specification](https://microsoft.github.io/language-server-protocol/)
- [Debug Adapter Protocol](https://microsoft.github.io/debug-adapter-protocol/)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [VS Code Extension API](https://code.visualstudio.com/api)
- [Model Context Protocol (MCP)](https://modelcontextprotocol.io/)
- [ACQUISITION-READY-ARCHITECTURE.md](./ACQUISITION-READY-ARCHITECTURE.md)
