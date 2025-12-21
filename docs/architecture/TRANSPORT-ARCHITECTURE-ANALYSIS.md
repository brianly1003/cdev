# Transport Architecture Analysis

**Version:** 1.0.0
**Status:** Technical Review
**Last Updated:** December 2025

---

## Executive Summary

This document analyzes cdev's current dual-protocol architecture (WebSocket + HTTP) and evaluates whether it's the optimal solution. After deep analysis, the recommendation is to **consolidate to a unified protocol on a single port** while keeping HTTP only for health checks and optional tooling.

### Key Finding

> **The current dual-protocol approach creates unnecessary complexity and maintenance burden. Most HTTP endpoints duplicate WebSocket functionality.**

---

## Current Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        CURRENT ARCHITECTURE                              │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│    Port 8765 (WebSocket)              Port 8766 (HTTP)                  │
│    ┌─────────────────────┐            ┌─────────────────────┐           │
│    │                     │            │                     │           │
│    │  EVENTS (→ Client)  │            │  REST API           │           │
│    │  • claude_log       │            │  • GET /health      │           │
│    │  • claude_status    │            │  • GET /api/status  │           │
│    │  • file_changed     │            │  • POST /api/claude/│           │
│    │  • git_diff         │            │    run              │           │
│    │  • heartbeat        │            │  • POST /api/claude/│           │
│    │                     │            │    stop             │           │
│    │  COMMANDS (← Client)│            │  • GET /api/git/*   │           │
│    │  • run_claude       │            │  • GET /api/file    │           │
│    │  • stop_claude      │            │  • GET /swagger/*   │           │
│    │  • respond_to_claude│            │                     │           │
│    │  • get_status       │            │                     │           │
│    │  • get_file         │            │                     │           │
│    │                     │            │                     │           │
│    └─────────────────────┘            └─────────────────────┘           │
│              │                                  │                        │
│              └──────────┬───────────────────────┘                        │
│                         │                                                │
│                         ▼                                                │
│              ┌─────────────────────┐                                    │
│              │    Claude Manager   │                                    │
│              │    Git Tracker      │                                    │
│              │    File Watcher     │                                    │
│              └─────────────────────┘                                    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Duplication Analysis

| Operation | WebSocket Command | HTTP Endpoint | Duplicated? |
|-----------|-------------------|---------------|-------------|
| Run Claude | `run_claude` | `POST /api/claude/run` | **Yes** |
| Stop Claude | `stop_claude` | `POST /api/claude/stop` | **Yes** |
| Respond | `respond_to_claude` | `POST /api/claude/respond` | **Yes** |
| Get Status | `get_status` | `GET /api/status` | **Yes** |
| Get File | `get_file` | `GET /api/file` | **Yes** |
| Git Status | - | `GET /api/git/status` | No (HTTP only) |
| Git Diff | - | `GET /api/git/diff` | No (HTTP only) |
| Health Check | - | `GET /health` | No (HTTP only) |
| Swagger UI | - | `GET /swagger/*` | No (HTTP only) |

**Finding:** 5 out of 9 core operations are duplicated across both protocols.

---

## Problems with Current Approach

### 1. Maintenance Burden

```go
// HTTP Server (server.go) - 800+ lines
func (s *Server) handleClaudeRun(w http.ResponseWriter, r *http.Request) {
    // Parse request
    // Validate
    // Call Claude Manager
    // Send response
}

// WebSocket Handler (app.go) - Similar logic
func (a *App) handleCommand(clientID string, message []byte) {
    case commands.CommandRunClaude:
        // Parse command
        // Validate
        // Call Claude Manager
        // Send event response
}
```

Every feature change requires updating **both** handlers.

### 2. Port Management Complexity

- Two ports to expose through firewalls
- Two ports in Docker/Kubernetes configs
- Two ports for load balancers
- Client must track two connections

### 3. Authentication Duplication

```
┌─────────────────────────────────────────────────────────────────┐
│                  AUTHENTICATION NIGHTMARE                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│   WebSocket Auth:                 HTTP Auth:                    │
│   ┌─────────────────┐            ┌─────────────────┐           │
│   │ ws://host:8765? │            │ Authorization:  │           │
│   │   token=xxx     │            │   Bearer xxx    │           │
│   └─────────────────┘            └─────────────────┘           │
│                                                                  │
│   Two implementations. Two attack surfaces. Two places to fix.  │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 4. State Synchronization Risk

What if HTTP and WebSocket have different views of the same state?

```
Timeline:
1. Client A: WebSocket run_claude
2. Client B: HTTP GET /api/status  ← May miss in-flight state
3. Claude starts
4. Client A: Receives claude_status
5. Client B: Has stale data
```

### 5. Testing Overhead

- Unit tests for HTTP handlers
- Unit tests for WebSocket handlers
- Integration tests for HTTP → Claude
- Integration tests for WebSocket → Claude
- Cross-protocol consistency tests

### 6. Documentation Confusion

Users must understand:
- When to use WebSocket
- When to use HTTP
- How they interact
- Which is authoritative

---

## Alternative Architectures Evaluated

### Option A: Keep Dual Protocol (Current)

```
Ports: 8765 (WS) + 8766 (HTTP)
```

**Pros:**
- No migration required
- HTTP for simple curl testing
- Swagger UI available

**Cons:**
- All the problems listed above
- Not industry standard
- Complex for clients

**Score: 4/10**

---

### Option B: Single Port with Protocol Upgrade

```
Port 8766:
├── GET /health              → HTTP
├── GET /swagger/*           → HTTP (Swagger UI)
├── GET /api/*               → HTTP (REST facade)
└── GET /ws (Upgrade)        → WebSocket
```

**Pros:**
- Single port
- HTTP for simple operations
- WebSocket for streaming

**Cons:**
- Still two protocols to maintain
- HTTP API is redundant facade

**Score: 6/10**

---

### Option C: HTTP + SSE (Server-Sent Events)

```
Port 8766:
├── GET /health              → HTTP
├── GET /events              → SSE (event stream)
├── POST /api/claude/run     → HTTP
├── POST /api/claude/stop    → HTTP
└── ...
```

**Pros:**
- HTTP-only (proxy friendly)
- SSE for streaming
- Simpler than WebSocket

**Cons:**
- SSE is one-way (server → client only)
- Less efficient than WebSocket
- Poor mobile support
- Not standard for AI agents

**Score: 5/10**

---

### Option D: Unified Protocol (RECOMMENDED)

```
Port 8766:
├── GET /health              → HTTP (Kubernetes probes)
├── GET /info                → HTTP (capabilities discovery)
├── GET /swagger/*           → HTTP (optional, dev only)
└── WS /                     → JSON-RPC 2.0 (all operations)
    OR
    stdio                    → JSON-RPC 2.0 (VS Code mode)
```

**Design Principles:**
1. **Single Protocol**: All operations via JSON-RPC 2.0
2. **Multiple Transports**: WebSocket, stdio, future gRPC
3. **HTTP for Infrastructure**: Health checks, discovery only
4. **No Duplication**: One code path per operation

**Pros:**
- Single protocol to maintain
- Industry standard (LSP, MCP, DAP all use this)
- Easy transport switching (WS ↔ stdio)
- Simple authentication (one place)
- Clear documentation
- Easy testing

**Cons:**
- Migration required
- No curl testing (use wscat instead)
- Swagger UI becomes less useful

**Score: 9/10**

---

### Option E: gRPC with HTTP Transcoding

```
Port 8766:
├── gRPC                     → Primary protocol
└── HTTP                     → Auto-generated from proto (grpc-gateway)
```

**Pros:**
- Strongly typed
- Efficient binary protocol
- Auto-generated clients
- HTTP transcoding for REST

**Cons:**
- Complex setup
- Proto files to maintain
- Overkill for current scale
- Browser support needs grpc-web

**Score: 6/10**

---

## Recommendation: Option D (Unified Protocol)

### Target Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                      RECOMMENDED ARCHITECTURE                            │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│                          Port 8766                                      │
│    ┌─────────────────────────────────────────────────────────┐         │
│    │                                                         │         │
│    │  HTTP (Infrastructure Only)                             │         │
│    │  ├── GET /health        → Kubernetes liveness probe    │         │
│    │  ├── GET /ready         → Kubernetes readiness probe   │         │
│    │  ├── GET /info          → Capabilities discovery       │         │
│    │  └── GET /swagger/*     → Dev tools (optional)         │         │
│    │                                                         │         │
│    │  WebSocket / (Primary Protocol)                         │         │
│    │  └── JSON-RPC 2.0                                       │         │
│    │      ├── initialize      → Capability negotiation      │         │
│    │      ├── claude/run      → Start Claude                │         │
│    │      ├── claude/stop     → Stop Claude                 │         │
│    │      ├── claude/respond  → Interactive response        │         │
│    │      ├── git/status      → Git status                  │         │
│    │      ├── files/read      → Read file                   │         │
│    │      └── notifications   → Events (no response)        │         │
│    │          ├── claude/log                                 │         │
│    │          ├── claude/status                              │         │
│    │          ├── files/changed                              │         │
│    │          └── git/diff                                   │         │
│    │                                                         │         │
│    └─────────────────────────────────────────────────────────┘         │
│                              │                                          │
│                              ▼                                          │
│                 ┌─────────────────────────┐                            │
│                 │    Protocol Handler     │                            │
│                 │    (Single Code Path)   │                            │
│                 └───────────┬─────────────┘                            │
│                             │                                          │
│              ┌──────────────┼──────────────┐                           │
│              ▼              ▼              ▼                           │
│    ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                  │
│    │   Claude    │  │     Git     │  │    Files    │                  │
│    │   Manager   │  │   Tracker   │  │   Watcher   │                  │
│    └─────────────┘  └─────────────┘  └─────────────┘                  │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Why This is Better

#### 1. Single Code Path

```go
// Before: Two handlers for same operation
func (s *HTTPServer) handleClaudeRun(...) { /* HTTP version */ }
func (a *App) handleRunClaude(...) { /* WebSocket version */ }

// After: One handler
func (h *ProtocolHandler) HandleClaudeRun(params ClaudeRunParams) (ClaudeRunResult, error) {
    // Single implementation
    // Works for WebSocket, stdio, future transports
}
```

#### 2. Transport Abstraction

```go
// Transport interface (from VSCODE-INTEGRATION-STRATEGY.md)
type Transport interface {
    Read() ([]byte, error)
    Write(data []byte) error
    Close() error
}

// Same protocol, different transports
type WebSocketTransport struct { ... }
type StdioTransport struct { ... }      // VS Code
type GRPCTransport struct { ... }       // Future
```

#### 3. Industry Alignment

| Protocol | Message Format | Transport | Used By |
|----------|---------------|-----------|---------|
| **LSP** | JSON-RPC 2.0 | stdio/TCP | VS Code, all IDEs |
| **DAP** | JSON messages | stdio/TCP | All debuggers |
| **MCP** | JSON-RPC 2.0 | stdio/SSE/WS | Claude, AI tools |
| **cdev** | JSON-RPC 2.0 | WebSocket/stdio | Our target |

#### 4. Simplified Configuration

```yaml
# Before
server:
  websocket_port: 8765
  http_port: 8766

# After
server:
  port: 8766
  transport: websocket  # or "stdio" for VS Code
```

---

## Migration Strategy

### Phase 1: Add Transport Abstraction (Week 1)

```go
// internal/transport/transport.go
package transport

type Transport interface {
    Send(ctx context.Context, msg []byte) error
    Receive() <-chan []byte
    Close() error
}

type WebSocketTransport struct { ... }
```

### Phase 2: Add JSON-RPC Layer (Week 2)

```go
// internal/protocol/jsonrpc/handler.go
package jsonrpc

type Handler struct {
    claudeManager *claude.Manager
    gitTracker    *git.Tracker
    // ...
}

func (h *Handler) Handle(ctx context.Context, msg []byte) ([]byte, error) {
    req, err := ParseRequest(msg)
    if err != nil {
        return ErrorResponse(err)
    }

    switch req.Method {
    case "claude/run":
        return h.handleClaudeRun(ctx, req.Params)
    case "claude/stop":
        return h.handleClaudeStop(ctx, req.Params)
    // ...
    }
}
```

### Phase 3: Consolidate to Single Port (Week 3)

```go
// internal/server/server.go
package server

func New(cfg *config.Config) *Server {
    mux := http.NewServeMux()

    // Infrastructure endpoints (HTTP)
    mux.HandleFunc("/health", handleHealth)
    mux.HandleFunc("/ready", handleReady)
    mux.HandleFunc("/info", handleInfo)

    // Protocol endpoint (WebSocket upgrade)
    mux.HandleFunc("/", handleProtocol)

    return &Server{
        httpServer: &http.Server{
            Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
            Handler: mux,
        },
    }
}
```

### Phase 4: Deprecate Old HTTP API (Week 4)

1. Add deprecation warnings to HTTP endpoints
2. Update iOS app to use WebSocket exclusively
3. Update documentation
4. Remove HTTP handlers in next major version

---

## Backward Compatibility

### During Migration

```go
// Support both protocols during transition
func (s *Server) handleProtocol(w http.ResponseWriter, r *http.Request) {
    // Check for WebSocket upgrade
    if websocket.IsWebSocketUpgrade(r) {
        s.handleWebSocket(w, r)
        return
    }

    // Legacy: Forward to HTTP handlers (deprecated)
    log.Warn().Msg("HTTP API is deprecated, use WebSocket")
    s.handleLegacyHTTP(w, r)
}
```

### Client Migration Path

```typescript
// Old iOS client
const ws = new WebSocket('ws://host:8765');
const http = 'http://host:8766';

// New iOS client
const ws = new WebSocket('ws://host:8766/');  // Single port
// No HTTP needed for operations
```

---

## What HTTP Should Remain

Keep HTTP endpoints for:

| Endpoint | Purpose | Why HTTP? |
|----------|---------|-----------|
| `GET /health` | Kubernetes liveness | K8s requires HTTP |
| `GET /ready` | Kubernetes readiness | K8s requires HTTP |
| `GET /info` | Capability discovery | One-time fetch |
| `GET /swagger/*` | Developer tooling | Existing tooling |
| `GET /metrics` | Prometheus scraping | Standard practice |

Remove HTTP for:
- All `/api/claude/*` endpoints
- All `/api/git/*` endpoints
- All `/api/file*` endpoints
- `GET /api/status`

---

## Impact Assessment

### iOS App Changes

```swift
// Before: Two connections
let wsConnection = WebSocket(url: "ws://\(host):8765")
let httpClient = HTTPClient(baseURL: "http://\(host):8766")

// After: Single connection
let protocol = CdevProtocol(url: "ws://\(host):8766")
// All operations through protocol
```

### Desktop App Changes

Minimal - already uses WebSocket primarily.

### CLI Changes

```bash
# Before
cdev start --ws-port 8765 --http-port 8766

# After
cdev start --port 8766
cdev start --transport stdio  # VS Code mode
```

---

## Summary

| Aspect | Current (Dual) | Recommended (Unified) |
|--------|----------------|----------------------|
| Ports | 2 | 1 |
| Protocols | 2 | 1 (+ HTTP for health) |
| Code duplication | High | None |
| Authentication | 2 implementations | 1 implementation |
| Testing overhead | 2x | 1x |
| Industry alignment | Custom | LSP/MCP compatible |
| VS Code ready | No | Yes |
| Migration effort | - | 4 weeks |

### Recommendation

**Migrate to Option D (Unified Protocol)** over 4 weeks:

1. Week 1: Transport abstraction
2. Week 2: JSON-RPC layer
3. Week 3: Single port consolidation
4. Week 4: HTTP deprecation

This positions cdev for:
- VS Code integration
- Other IDE acquisitions
- Protocol standardization (AAP)
- Reduced maintenance burden

---

## Appendix: Testing Without HTTP

### Using wscat (WebSocket CLI)

```bash
# Install
npm install -g wscat

# Connect
wscat -c ws://localhost:8766

# Send command
> {"jsonrpc":"2.0","id":1,"method":"claude/run","params":{"prompt":"Hello"}}

# Receive events
< {"jsonrpc":"2.0","method":"claude/log","params":{"line":"..."}}
```

### Using websocat (Rust CLI)

```bash
# Install
brew install websocat

# Connect and send
echo '{"jsonrpc":"2.0","id":1,"method":"cdev/status"}' | websocat ws://localhost:8766
```

### Programmatic Testing

```typescript
// test/e2e/protocol.test.ts
import WebSocket from 'ws';

describe('Protocol', () => {
    let ws: WebSocket;

    beforeEach(() => {
        ws = new WebSocket('ws://localhost:8766');
    });

    it('should run claude', async () => {
        const response = await sendRequest(ws, {
            jsonrpc: '2.0',
            id: 1,
            method: 'claude/run',
            params: { prompt: 'Hello' }
        });

        expect(response.result.status).toBe('started');
    });
});
```

---

## References

- [VSCODE-INTEGRATION-STRATEGY.md](./VSCODE-INTEGRATION-STRATEGY.md)
- [ACQUISITION-READY-ARCHITECTURE.md](./ACQUISITION-READY-ARCHITECTURE.md)
- [Language Server Protocol](https://microsoft.github.io/language-server-protocol/)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
