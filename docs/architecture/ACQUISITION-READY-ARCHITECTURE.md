# Acquisition-Ready Architecture Strategy

**Version:** 1.0.0
**Status:** Strategic Planning
**Last Updated:** 21 Dec 2025

---

## Executive Summary

This document outlines architectural decisions to make cdev seamlessly integrable by major technology companies (Microsoft/VS Code, JetBrains, Cursor, Anthropic, GitHub, etc.) without significant restructuring.

### Core Principle

> **Design as a Protocol, not a Product**

The most valuable asset isn't the cdev application itself - it's the **AI Agent Protocol (AAP)** that defines how AI coding assistants communicate with IDEs, mobile clients, and development tools.

---

## Potential Acquirers & Their Requirements

| Company | Product | Integration Point | Key Requirements |
|---------|---------|-------------------|------------------|
| **Microsoft** | VS Code, GitHub Copilot | Extension API, LSP | JSON-RPC 2.0, stdio transport, capability negotiation |
| **JetBrains** | IntelliJ, Fleet | Plugin API | Kotlin/Java SDK, LSP-compatible |
| **Cursor** | Cursor IDE | Native integration | TypeScript SDK, real-time streaming |
| **Anthropic** | Claude Code | Official tooling | Go SDK, protocol ownership |
| **GitHub** | Codespaces, Copilot | Cloud integration | Container-ready, OAuth, REST API |
| **Amazon** | CodeWhisperer, Cloud9 | AWS integration | Serverless, IAM, CloudWatch |
| **Google** | Duet AI, Cloud Shell | GCP integration | gRPC, Cloud Run, IAM |

---

## Strategic Architecture Decisions

### 1. Adopt JSON-RPC 2.0 Message Format

**Why:** VS Code's Language Server Protocol (LSP) and Debug Adapter Protocol (DAP) both use JSON-RPC 2.0. This is the de facto standard for IDE-tool communication.

**Current Format:**
```json
{
  "command": "run_claude",
  "request_id": "req-001",
  "payload": { "prompt": "..." }
}
```

**Recommended Format (JSON-RPC 2.0):**
```json
{
  "jsonrpc": "2.0",
  "id": "req-001",
  "method": "claude/run",
  "params": { "prompt": "..." }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": "req-001",
  "result": { "status": "started", "sessionId": "..." }
}
```

**Notification (no response expected):**
```json
{
  "jsonrpc": "2.0",
  "method": "claude/log",
  "params": { "line": "...", "stream": "stdout" }
}
```

**Benefits:**
- Immediate compatibility with LSP tooling
- Standard error format (`{"code": -32600, "message": "..."}`)
- Batch request support built-in
- Well-documented specification

---

### 2. Support Multiple Transports

**Why:** Different integration points require different transports.

| Transport | Use Case | Priority |
|-----------|----------|----------|
| **WebSocket** | Mobile apps, web clients | Current (P0) |
| **stdio** | VS Code extensions, CLI pipes | High (P1) |
| **Unix Domain Socket** | Local IDE integration | Medium (P2) |
| **Named Pipes** | Windows native apps | Medium (P2) |
| **gRPC** | Cloud services, high-performance | Future (P3) |

**Implementation Strategy:**

```go
// internal/transport/transport.go

// Transport defines the communication layer interface
type Transport interface {
    // Send sends a message to the client
    Send(ctx context.Context, msg Message) error

    // Receive returns a channel of incoming messages
    Receive() <-chan Message

    // Close closes the transport
    Close() error

    // Info returns transport metadata
    Info() TransportInfo
}

// TransportInfo contains transport metadata
type TransportInfo struct {
    Type     string // "websocket", "stdio", "unix", "grpc"
    Endpoint string // Connection endpoint
    ClientID string // Unique client identifier
}

// Implementations
type WebSocketTransport struct { ... }
type StdioTransport struct { ... }
type UnixSocketTransport struct { ... }
type GRPCTransport struct { ... }
```

**stdio Transport for VS Code:**
```go
// internal/transport/stdio.go

func NewStdioTransport() *StdioTransport {
    return &StdioTransport{
        reader: bufio.NewReader(os.Stdin),
        writer: os.Stdout,
        // Content-Length header like LSP
        headerMode: true,
    }
}

// Message format (LSP-style):
// Content-Length: 123\r\n
// \r\n
// {"jsonrpc":"2.0","method":"claude/run",...}
```

---

### 3. Define Capability Negotiation

**Why:** LSP's capability negotiation allows clients and servers to agree on supported features. This enables graceful degradation and forward compatibility.

**Initialize Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "protocolVersion": "1.0.0",
    "clientInfo": {
      "name": "vscode-cdev",
      "version": "0.1.0"
    },
    "capabilities": {
      "claude": {
        "streaming": true,
        "permissions": true,
        "interactivePrompts": true,
        "sessionContinuation": true
      },
      "git": {
        "status": true,
        "diff": true,
        "operations": true
      },
      "files": {
        "watch": true,
        "read": true,
        "search": true
      },
      "experimental": {
        "imageUpload": true
      }
    }
  }
}
```

**Initialize Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "1.0.0",
    "serverInfo": {
      "name": "cdev",
      "version": "1.0.0"
    },
    "capabilities": {
      "claude": {
        "streaming": true,
        "permissions": true,
        "interactivePrompts": true,
        "sessionContinuation": true,
        "models": ["claude-sonnet-4", "claude-opus-4"]
      },
      "git": {
        "status": true,
        "diff": true,
        "operations": true,
        "branches": true
      },
      "files": {
        "watch": true,
        "read": true,
        "search": true,
        "maxFileSize": 10485760
      },
      "rateLimit": {
        "requestsPerMinute": 100,
        "claudeRunsPerMinute": 10
      }
    }
  }
}
```

---

### 4. Create Language-Agnostic SDKs

**Why:** Acquirers work in different languages. Provide SDKs to reduce integration friction.

**SDK Priority:**

| Language | Target | Priority | Use Case |
|----------|--------|----------|----------|
| **TypeScript** | npm | P0 | VS Code, Cursor, web |
| **Python** | PyPI | P0 | AI/ML tools, scripts |
| **Go** | Module | P1 | CLI tools, servers |
| **Rust** | Crates | P2 | High-performance clients |
| **Java/Kotlin** | Maven | P2 | JetBrains plugins |
| **Swift** | SPM | P2 | macOS/iOS native |

**TypeScript SDK Example:**

```typescript
// @anthropic/cdev-client

import { CdevClient, Transport } from '@anthropic/cdev-client';

// WebSocket transport (current)
const wsClient = new CdevClient({
  transport: Transport.WebSocket('ws://localhost:8765')
});

// stdio transport (VS Code extension)
const stdioClient = new CdevClient({
  transport: Transport.Stdio(process.stdin, process.stdout)
});

// Usage is identical regardless of transport
await client.initialize({
  clientInfo: { name: 'my-extension', version: '1.0.0' },
  capabilities: { claude: { streaming: true } }
});

// Run Claude with streaming
const stream = client.claude.run({
  prompt: 'Fix the bug in app.js',
  mode: 'new'
});

for await (const event of stream) {
  if (event.type === 'claude/log') {
    console.log(event.params.line);
  }
}
```

---

### 5. Formalize as "AI Agent Protocol" (AAP)

**Why:** Position cdev's protocol as an industry standard, similar to how LSP became the standard for language tooling.

**Protocol Specification Structure:**

```
specs/
├── aap-1.0.0.md              # Main specification
├── aap-transport-websocket.md # WebSocket binding
├── aap-transport-stdio.md     # stdio binding
├── aap-transport-grpc.md      # gRPC binding
├── aap-capabilities.md        # Capability definitions
├── aap-errors.md              # Error codes
└── schemas/
    ├── initialize.json
    ├── claude-run.json
    ├── claude-log.json
    └── ...
```

**Benefits of Standardization:**
- Other AI assistants (Copilot, Codeium, etc.) could adopt the protocol
- Creates network effects and ecosystem lock-in
- Makes acquisition more valuable (you're acquiring a standard)
- Enables third-party tooling

---

### 6. Design for Enterprise from Day One

**Why:** Enterprise features are often requested post-acquisition but are difficult to retrofit.

**Multi-Tenancy:**
```json
{
  "jsonrpc": "2.0",
  "method": "initialize",
  "params": {
    "tenant": {
      "id": "org-12345",
      "name": "Acme Corp",
      "tier": "enterprise"
    },
    "user": {
      "id": "user-67890",
      "email": "dev@acme.com",
      "roles": ["developer", "admin"]
    }
  }
}
```

**Authentication Providers:**
```go
// internal/auth/provider.go

type AuthProvider interface {
    Authenticate(ctx context.Context, token string) (*User, error)
    Authorize(ctx context.Context, user *User, action string) (bool, error)
}

// Built-in providers
type APIKeyProvider struct { ... }
type JWTProvider struct { ... }
type OAuth2Provider struct { ... }
type SAMLProvider struct { ... }

// Enterprise SSO
type OktaProvider struct { ... }
type AzureADProvider struct { ... }
type GoogleWorkspaceProvider struct { ... }
```

**Audit Logging:**
```json
{
  "timestamp": "2025-12-21T10:30:00.000Z",
  "event": "claude.run",
  "tenant": "org-12345",
  "user": "user-67890",
  "action": "claude/run",
  "resource": "/projects/myapp",
  "request": { "prompt": "Fix bug..." },
  "response": { "status": "started" },
  "duration_ms": 45,
  "ip": "192.168.1.100",
  "user_agent": "vscode-cdev/1.0.0"
}
```

---

### 7. Plugin/Extension Architecture

**Why:** Allow third-party extensions without modifying core code.

**Extension Points:**

```go
// internal/extensions/extension.go

type Extension interface {
    // Metadata
    Name() string
    Version() string
    Capabilities() []string

    // Lifecycle
    Initialize(ctx context.Context, config ExtensionConfig) error
    Shutdown(ctx context.Context) error

    // Request handling
    HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, error)

    // Event subscription
    OnEvent(ctx context.Context, event Event) error
}

// Extension registry
type ExtensionRegistry struct {
    extensions map[string]Extension
}

func (r *ExtensionRegistry) Register(ext Extension) error
func (r *ExtensionRegistry) Route(method string) (Extension, bool)
```

**Example Extension (Custom AI Provider):**

```go
type CustomAIExtension struct {
    client *openai.Client
}

func (e *CustomAIExtension) Name() string { return "custom-ai" }
func (e *CustomAIExtension) Capabilities() []string {
    return []string{"ai/completions", "ai/embeddings"}
}

func (e *CustomAIExtension) HandleRequest(ctx context.Context, method string, params json.RawMessage) (any, error) {
    switch method {
    case "ai/completions":
        return e.handleCompletions(ctx, params)
    default:
        return nil, ErrMethodNotFound
    }
}
```

---

### 8. Container-Ready Architecture

**Why:** Cloud deployments (GitHub Codespaces, AWS Cloud9, GCP Cloud Shell) run in containers.

**Docker Support:**
```dockerfile
# Dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o cdev ./cmd/cdev

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/cdev .

# Support multiple transports
EXPOSE 8765 8766
ENV CDEV_TRANSPORT=websocket
ENV CDEV_HOST=0.0.0.0

ENTRYPOINT ["./cdev"]
CMD ["start"]
```

**Kubernetes Ready:**
```yaml
# k8s/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cdev
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: cdev
        image: cdev:latest
        ports:
        - containerPort: 8765
        - containerPort: 8766
        env:
        - name: CDEV_TRANSPORT
          value: "websocket"
        - name: CDEV_AUTH_PROVIDER
          value: "jwt"
        - name: CDEV_JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: cdev-secrets
              key: jwt-secret
        livenessProbe:
          httpGet:
            path: /health
            port: 8766
        readinessProbe:
          httpGet:
            path: /health
            port: 8766
```

---

### 9. Observability & Metrics

**Why:** Enterprise deployments require monitoring, tracing, and metrics.

**OpenTelemetry Integration:**
```go
// internal/observability/telemetry.go

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
)

var (
    tracer  = otel.Tracer("cdev")
    meter   = otel.Meter("cdev")

    // Metrics
    requestCounter  metric.Int64Counter
    requestDuration metric.Float64Histogram
    activeClients   metric.Int64UpDownCounter
    claudeRuns      metric.Int64Counter
)

func RecordRequest(ctx context.Context, method string, duration time.Duration, err error) {
    requestCounter.Add(ctx, 1,
        metric.WithAttributes(
            attribute.String("method", method),
            attribute.Bool("error", err != nil),
        ),
    )
    requestDuration.Record(ctx, duration.Seconds(),
        metric.WithAttributes(attribute.String("method", method)),
    )
}
```

**Prometheus Metrics Endpoint:**
```
GET /metrics

# HELP cdev_requests_total Total number of requests
# TYPE cdev_requests_total counter
cdev_requests_total{method="claude/run",status="success"} 1234
cdev_requests_total{method="claude/run",status="error"} 12

# HELP cdev_request_duration_seconds Request duration in seconds
# TYPE cdev_request_duration_seconds histogram
cdev_request_duration_seconds_bucket{method="claude/run",le="0.1"} 100
cdev_request_duration_seconds_bucket{method="claude/run",le="1"} 500

# HELP cdev_active_clients Current number of connected clients
# TYPE cdev_active_clients gauge
cdev_active_clients 5
```

---

## Implementation Roadmap

### Phase 1: Protocol Foundation (Q1) ✅ COMPLETE

| Task | Priority | Status |
|------|----------|--------|
| Adopt JSON-RPC 2.0 message format | P0 | ✅ Done |
| Add capability negotiation | P0 | ✅ Done |
| Add stdio transport | P0 | ✅ Done |
| Agent-agnostic method naming (agent/* vs claude/*) | P0 | ✅ Done |
| OpenRPC auto-generation from registry | P0 | ✅ Done |
| Dual-protocol support (legacy + JSON-RPC) | P0 | ✅ Done |
| Port consolidation (single port 8766) | P0 | ✅ Done |
| Create TypeScript SDK | P0 | Planned |
| Write formal protocol specification | P0 | ✅ Done |

### Phase 2: Enterprise Features (Q2)

| Task | Priority | Effort |
|------|----------|--------|
| Add JWT authentication | P1 | 1 week |
| Add OAuth2/OIDC support | P1 | 2 weeks |
| Add audit logging | P1 | 1 week |
| Add multi-tenancy support | P1 | 2 weeks |
| Create Python SDK | P1 | 2 weeks |

### Phase 3: Ecosystem (Q3)

| Task | Priority | Effort |
|------|----------|--------|
| VS Code extension (proof-of-concept) | P1 | 2 weeks |
| Extension/plugin architecture | P2 | 3 weeks |
| gRPC transport | P2 | 2 weeks |
| Kubernetes Helm chart | P2 | 1 week |
| OpenTelemetry integration | P2 | 1 week |

### Phase 4: Standardization (Q4)

| Task | Priority | Effort |
|------|----------|--------|
| Publish AAP specification | P2 | 2 weeks |
| Conformance test suite | P2 | 2 weeks |
| Reference implementations | P2 | 4 weeks |
| Community governance | P3 | Ongoing |

---

## Immediate Action Items

### 1. Create Protocol Version Header

Add to every message:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "claude/run",
  "params": { ... },
  "_protocol": "aap/1.0.0"
}
```

### 2. Namespace Methods

Current: `run_claude`, `stop_claude`
Recommended: `claude/run`, `claude/stop`, `git/status`, `files/read`

### 3. Add Server Info Endpoint

```
GET /api/info

{
  "name": "cdev",
  "version": "1.0.0",
  "protocol": "aap/1.0.0",
  "transports": ["websocket", "http"],
  "capabilities": { ... }
}
```

### 4. Create OpenAPI + AsyncAPI Specs

- OpenAPI 3.0 for HTTP endpoints (already have)
- AsyncAPI 2.0 for WebSocket events (create)

### 5. License Clarity

Ensure license is acquisition-friendly:
- MIT or Apache 2.0 (not GPL)
- CLA for contributors
- Clear IP ownership

---

## Summary: Acquisition Readiness Checklist

| Category | Requirement | Status |
|----------|-------------|--------|
| **Protocol** | JSON-RPC 2.0 format | ✅ Done |
| **Protocol** | Capability negotiation | ✅ Done |
| **Protocol** | Agent-agnostic method naming (agent/* vs claude/*) | ✅ Done |
| **Protocol** | OpenRPC auto-generation | ✅ Done |
| **Protocol** | Formal specification | ✅ Done |
| **Protocol** | Semantic versioning | ✅ Done |
| **Protocol** | Dual-protocol support (legacy + JSON-RPC) | ✅ Done |
| **Transport** | WebSocket | ✅ Done |
| **Transport** | stdio (LSP-style) | ✅ Done |
| **Transport** | Port consolidation (single port 8766) | ✅ Done |
| **Transport** | gRPC | Future |
| **SDK** | TypeScript | Planned |
| **SDK** | Python | Planned |
| **SDK** | Go | Planned |
| **Enterprise** | JWT authentication | Planned |
| **Enterprise** | OAuth2/OIDC | Planned |
| **Enterprise** | Audit logging | Planned |
| **Enterprise** | Multi-tenancy | Planned |
| **DevOps** | Docker container | Planned |
| **DevOps** | Kubernetes ready | Planned |
| **DevOps** | OpenTelemetry | Planned |
| **Ecosystem** | VS Code extension | Planned |
| **Ecosystem** | Extension API | Planned |
| **Legal** | MIT/Apache license | ✅ Done |
| **Legal** | CLA | Planned |

---

## Conclusion

The key insight is: **sell the protocol, not just the product**.

By designing cdev as a reference implementation of the "AI Agent Protocol" (AAP), we create:

1. **Multiple acquisition paths** - Any company can integrate via the standard protocol
2. **Ecosystem value** - Third parties build on our protocol, increasing value
3. **Reduced integration friction** - Standard transports and formats
4. **Enterprise readiness** - Auth, audit, multi-tenancy from day one
5. **Future-proofing** - Capability negotiation handles evolution

The investment in protocol standardization pays dividends regardless of acquisition outcome - it makes the product better for all users.
