# Security Design for cdev Agent

## Overview

cdev is a daemon that enables remote monitoring and control of Claude Code CLI sessions. Given its capabilities (executing Claude prompts, reading files, git operations), security is critical. This document outlines the threat model, current security state, and recommended security architecture.

## Threat Model

### Assets to Protect

| Asset | Sensitivity | Impact if Compromised |
|-------|-------------|----------------------|
| Source code | High | IP theft, vulnerability exposure |
| Claude CLI access | Critical | Arbitrary code execution via prompts |
| File system access | High | Data exfiltration, malware injection |
| Git credentials | High | Repository compromise |
| System resources | Medium | DoS, resource exhaustion |

### Threat Actors

| Actor | Motivation | Capability |
|-------|------------|------------|
| **Network Attacker** | Access to dev environment | Medium - can scan local network |
| **Remote Attacker** | Access via exposed tunnels | High - full internet access |
| **Malicious App** | Data theft | Medium - can mimic cdev mobile app |
| **Insider Threat** | Various | High - knows system internals |

### Attack Vectors

```
┌─────────────────────────────────────────────────────────────────────┐
│                         ATTACK SURFACE                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. Network Layer                                                   │
│     ├── Port scanning (8766 discovery)                              │
│     ├── Man-in-the-middle (unencrypted traffic)                     │
│     └── DNS rebinding attacks                                       │
│                                                                     │
│  2. WebSocket Connection                                            │
│     ├── Unauthorized connection (no auth)                           │
│     ├── Session hijacking                                           │
│     ├── Cross-site WebSocket hijacking (CSWSH)                      │
│     └── Connection flooding (DoS)                                   │
│                                                                     │
│  3. API Endpoints                                                   │
│     ├── Unauthorized API access                                     │
│     ├── Command injection via prompts                               │
│     ├── Path traversal in file operations                           │
│     └── Rate limit bypass                                           │
│                                                                     │
│  4. Data Layer                                                      │
│     ├── Sensitive data in QR codes                                  │
│     ├── Session token theft                                         │
│     └── Log file exposure                                           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Current Security State

### What's Implemented

| Feature | Status | Location |
|---------|--------|----------|
| Localhost binding | ✅ Default | `config.yaml` - `server.host: 127.0.0.1` |
| Rate limiting | ✅ Partial | `middleware/ratelimit.go` |
| Message size limits | ✅ Yes | `websocket/server.go` - 512KB |
| Ping/pong health checks | ✅ Yes | WebSocket keepalive |

### What's Missing

| Feature | Status | Risk Level |
|---------|--------|------------|
| WebSocket authentication | ❌ None | **Critical** |
| Origin validation | ❌ Allows all | High |
| Token-based auth | ❌ None | **Critical** |
| TLS/HTTPS | ❌ None | High |
| Audit logging | ❌ None | Medium |
| Device registration | ❌ None | Medium |
| Connection encryption | ❌ Plain text | High |

### Current Code Analysis

```go
// websocket/server.go - Line 41-48
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        // ⚠️ SECURITY: Allows ALL origins
        return true
    },
}
```

```go
// unified/server.go - Line 27-33
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin: func(r *http.Request) bool {
        return true // ⚠️ SECURITY: Allow all origins
    },
}
```

## Security Architecture

### Defense in Depth

```
┌─────────────────────────────────────────────────────────────────────┐
│                      SECURITY LAYERS                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Layer 1: Network Security                                          │
│  ─────────────────────────                                          │
│  ├── Bind to localhost only (default)                               │
│  ├── Firewall rules (user responsibility)                           │
│  └── TLS encryption (when exposed)                                  │
│                                                                     │
│  Layer 2: Connection Security                                       │
│  ────────────────────────────                                       │
│  ├── Origin validation                                              │
│  ├── Token authentication                                           │
│  └── Rate limiting                                                  │
│                                                                     │
│  Layer 3: Session Security                                          │
│  ─────────────────────────                                          │
│  ├── Session tokens                                                 │
│  ├── Token expiry                                                   │
│  └── Device binding                                                 │
│                                                                     │
│  Layer 4: Application Security                                      │
│  ─────────────────────────────                                      │
│  ├── Input validation                                               │
│  ├── Path traversal prevention                                      │
│  └── Command sanitization                                           │
│                                                                     │
│  Layer 5: Audit & Monitoring                                        │
│  ────────────────────────────                                       │
│  ├── Connection logging                                             │
│  ├── Command logging                                                │
│  └── Anomaly detection                                              │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Authentication Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                    AUTHENTICATION FLOW                              │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. Server Start                                                    │
│     └── Generate server secret (random 32 bytes)                    │
│         └── Store in memory (never persisted)                       │
│                                                                     │
│  2. Pairing Request                                                 │
│     └── Generate pairing token                                      │
│         ├── token = HMAC-SHA256(server_secret, timestamp + random)  │
│         ├── expires_at = now + token_expiry_secs                    │
│         └── Include in QR code                                      │
│                                                                     │
│  3. Client Connection                                               │
│     └── WebSocket upgrade request                                   │
│         ├── Header: Authorization: Bearer <token>                   │
│         └── Or: Query param: ?token=<token>                         │
│                                                                     │
│  4. Token Validation                                                │
│     ├── Check token format                                          │
│     ├── Verify HMAC signature                                       │
│     ├── Check expiry                                                │
│     └── Accept or reject connection                                 │
│                                                                     │
│  5. Session Establishment                                           │
│     └── Generate session token for ongoing use                      │
│         ├── Shorter expiry (e.g., 5 minutes)                        │
│         └── Auto-refresh on activity                                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## JSON-RPC 2.0 Authentication Best Practices

### Industry Patterns Comparison

| Protocol | Auth Method | When Validated | Token Refresh |
|----------|-------------|----------------|---------------|
| **MCP** (Model Context Protocol) | `initialize` params | After connect | Not specified |
| **LSP** (Language Server Protocol) | None (trusted) | N/A | N/A |
| **Ethereum JSON-RPC** | HTTP header | Before connect | Per-request |
| **OpenRPC** | Not specified | Transport-level | Transport-level |

### Recommended: Hybrid Approach (Transport + Lifecycle)

cdev should use a **two-phase authentication** that combines:

1. **Transport-level** (WebSocket upgrade) - Initial pairing token
2. **Lifecycle method** (`initialize`) - Session token exchange

```
┌─────────────────────────────────────────────────────────────────────┐
│              JSON-RPC 2.0 AUTHENTICATION FLOW                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Phase 1: Transport Authentication (WebSocket Upgrade)              │
│  ─────────────────────────────────────────────────────              │
│                                                                     │
│  Mobile App                           cdev Server                   │
│      │                                     │                        │
│      │─── GET /ws?token=<pairing_token> ──►│                        │
│      │                                     │                        │
│      │    [Server validates pairing token] │                        │
│      │    [If invalid: 401 Unauthorized]   │                        │
│      │    [If valid: Upgrade to WebSocket] │                        │
│      │                                     │                        │
│      │◄── 101 Switching Protocols ─────────│                        │
│      │                                     │                        │
│                                                                     │
│  Phase 2: JSON-RPC Initialize (Session Establishment)               │
│  ────────────────────────────────────────────────────               │
│                                                                     │
│      │─── {"jsonrpc":"2.0",               │                        │
│      │     "method":"initialize",          │                        │
│      │     "params":{                      │                        │
│      │       "clientInfo":{...},           │                        │
│      │       "auth":{"token":"<pairing>"}  │  ◄── Optional: can     │
│      │     },                              │      re-validate here  │
│      │     "id":1} ───────────────────────►│                        │
│      │                                     │                        │
│      │◄── {"jsonrpc":"2.0",               │                        │
│      │     "result":{                      │                        │
│      │       "serverInfo":{...},           │                        │
│      │       "capabilities":{...},         │                        │
│      │       "clientId":"uuid",            │                        │
│      │       "sessionToken":"<session>",   │  ◄── New session token │
│      │       "sessionExpires":"ISO8601"    │                        │
│      │     },                              │                        │
│      │     "id":1} ────────────────────────│                        │
│      │                                     │                        │
│      │─── {"jsonrpc":"2.0",               │                        │
│      │     "method":"initialized"} ────────►│  (notification)       │
│      │                                     │                        │
│                                                                     │
│  Phase 3: Ongoing Requests (Session Token)                          │
│  ─────────────────────────────────────────                          │
│                                                                     │
│      │─── {"jsonrpc":"2.0",               │                        │
│      │     "method":"agent/run",           │                        │
│      │     "params":{...},                 │                        │
│      │     "id":2} ───────────────────────►│                        │
│      │                                     │                        │
│      │    [Server validates via client_id] │                        │
│      │    [Session token auto-refreshed]   │                        │
│      │                                     │                        │
│                                                                     │
│  Phase 4: Token Refresh (When Needed)                               │
│  ────────────────────────────────────                               │
│                                                                     │
│      │─── {"jsonrpc":"2.0",               │                        │
│      │     "method":"auth/refresh",        │                        │
│      │     "params":{                      │                        │
│      │       "sessionToken":"<current>"    │                        │
│      │     },                              │                        │
│      │     "id":99} ──────────────────────►│                        │
│      │                                     │                        │
│      │◄── {"jsonrpc":"2.0",               │                        │
│      │     "result":{                      │                        │
│      │       "sessionToken":"<new>",       │                        │
│      │       "expiresAt":"ISO8601"         │                        │
│      │     },                              │                        │
│      │     "id":99} ───────────────────────│                        │
│      │                                     │                        │
└─────────────────────────────────────────────────────────────────────┘
```

### Updated Initialize Method

Current `InitializeParams`:
```go
type InitializeParams struct {
    ProtocolVersion string            `json:"protocolVersion"`
    ClientInfo      *ClientInfo       `json:"clientInfo,omitempty"`
    Capabilities    *ClientCapabilities `json:"capabilities,omitempty"`
    RootPath        string            `json:"rootPath,omitempty"`
}
```

Proposed `InitializeParams` (with auth):
```go
type InitializeParams struct {
    ProtocolVersion string              `json:"protocolVersion"`
    ClientInfo      *ClientInfo         `json:"clientInfo,omitempty"`
    Capabilities    *ClientCapabilities `json:"capabilities,omitempty"`
    RootPath        string              `json:"rootPath,omitempty"`

    // Authentication (optional, for re-validation)
    Auth            *AuthParams         `json:"auth,omitempty"`
}

type AuthParams struct {
    // Pairing token from QR code (for initial auth)
    Token    string `json:"token,omitempty"`

    // Device identifier (for device tracking)
    DeviceID string `json:"deviceId,omitempty"`

    // Device name (for display in device list)
    DeviceName string `json:"deviceName,omitempty"`
}
```

Proposed `InitializeResult` (with session token):
```go
type InitializeResult struct {
    ProtocolVersion string             `json:"protocolVersion"`
    ServerInfo      ServerInfo         `json:"serverInfo"`
    Capabilities    ServerCapabilities `json:"capabilities"`
    ClientID        string             `json:"clientId"`

    // Session authentication (when auth enabled)
    Session         *SessionInfo       `json:"session,omitempty"`
}

type SessionInfo struct {
    // Session token for ongoing requests
    Token     string    `json:"token"`

    // When the session expires
    ExpiresAt time.Time `json:"expiresAt"`

    // Whether token will auto-refresh on activity
    AutoRefresh bool    `json:"autoRefresh"`
}
```

### New Auth Methods

```go
// auth/refresh - Refresh session token
{
    "jsonrpc": "2.0",
    "method": "auth/refresh",
    "params": {
        "sessionToken": "current_token"
    },
    "id": 1
}

// Response
{
    "jsonrpc": "2.0",
    "result": {
        "sessionToken": "new_token",
        "expiresAt": "2024-01-01T12:00:00Z"
    },
    "id": 1
}

// auth/revoke - Revoke current session (logout)
{
    "jsonrpc": "2.0",
    "method": "auth/revoke",
    "id": 2
}
```

### Error Codes (JSON-RPC 2.0 Standard)

Following JSON-RPC 2.0 specification, use standard error codes:

| Code | Message | When |
|------|---------|------|
| `-32000` | Authentication required | No token provided |
| `-32001` | Invalid token | Token validation failed |
| `-32002` | Token expired | Token past expiry |
| `-32003` | Session expired | Session token expired |
| `-32004` | Permission denied | Insufficient permissions |

```go
// Example error response
{
    "jsonrpc": "2.0",
    "error": {
        "code": -32001,
        "message": "Invalid token",
        "data": {
            "reason": "Token signature verification failed",
            "hint": "Regenerate QR code with 'cdev pair --refresh'"
        }
    },
    "id": 1
}
```

### Why Two-Phase Authentication?

| Phase | Purpose | Benefits |
|-------|---------|----------|
| **Transport (WebSocket)** | Gate-keep connections | Reject before resource allocation |
| **Lifecycle (initialize)** | Exchange session info | Token refresh, device tracking |

**Benefits:**
1. **Early rejection** - Invalid tokens rejected before WebSocket upgrade
2. **Resource protection** - No resources allocated for invalid clients
3. **Flexibility** - Session tokens can be refreshed without reconnecting
4. **MCP/LSP compatible** - Follows industry standard lifecycle pattern
5. **Device tracking** - Can associate sessions with devices

## QR Code vs Authentication

**Important distinction:**

| Concept | Purpose | Always Required? |
|---------|---------|------------------|
| **QR Code** | Connection info (URL, session ID) | Yes - for mobile app to find server |
| **Token Auth** | Verify client identity | No - configurable per security mode |

```
┌─────────────────────────────────────────────────────────────────────┐
│                  QR CODE + AUTH RELATIONSHIP                        │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  QR Code Contains:                                                  │
│  ─────────────────                                                  │
│  {                                                                  │
│    "ws": "ws://127.0.0.1:8766/ws",    // Always present             │
│    "http": "http://127.0.0.1:8766",   // Always present             │
│    "session": "uuid",                  // Always present             │
│    "repo": "my-project",               // Always present             │
│    "token": "cdev_p_xxx"               // Optional (when auth enabled)│
│  }                                                                  │
│                                                                     │
│  Connection Flow:                                                   │
│  ────────────────                                                   │
│                                                                     │
│  [Mobile App] ──scan QR──► [Get Connection Info]                    │
│       │                                                             │
│       ▼                                                             │
│  [Connect to ws://...]                                              │
│       │                                                             │
│       ├── Auth Disabled: Connect immediately ✓                      │
│       │                                                             │
│       └── Auth Enabled: Validate token first                        │
│              ├── Valid token: Connect ✓                             │
│              └── Invalid/missing: Reject ✗                          │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Security Modes

### Mode 1: Local Development (Default)

```yaml
security:
  mode: local
  require_auth: false          # No auth required
  bind_localhost_only: true    # Only localhost
```

**Use case:** Developer working on local machine, no external access.

**Security:** Low risk - localhost only.

**QR Code:** Still works! Contains connection info without token.

### Mode 2: Authenticated Local

```yaml
security:
  mode: authenticated
  require_auth: true           # Token required
  bind_localhost_only: true    # Only localhost
  token_expiry_secs: 3600
```

**Use case:** Developer wants extra security even locally.

**Security:** Medium - auth required but unencrypted.

**QR Code:** Contains connection info + token. Token validated on connect.

### Mode 3: Remote Access (Tunnels)

```yaml
security:
  mode: remote
  require_auth: true           # Token required
  bind_localhost_only: false   # Allow external (via tunnel)
  token_expiry_secs: 1800      # Shorter expiry
  require_tls: true            # TLS required
```

**Use case:** Accessing via VS Code tunnel, ngrok, etc.

**Security:** High - auth + encryption required.

**QR Code:** Contains tunnel URL + token. Must use HTTPS/WSS.

### Mode 4: Team/Enterprise (Future)

```yaml
security:
  mode: enterprise
  require_auth: true
  auth_provider: oauth         # SSO integration
  allowed_origins:
    - "https://company.com"
  ip_whitelist:
    - "10.0.0.0/8"
  audit_logging: true
```

**Use case:** Team environment with compliance requirements.

**Security:** Very high - full security stack.

**QR Code:** May use different auth flow (OAuth device code).

## Token Design

### Token Structure

```
┌─────────────────────────────────────────────────────────────────────┐
│                        TOKEN FORMAT                                 │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Pairing Token (for initial connection):                            │
│  ────────────────────────────────────────                           │
│  Format: cdev_p_<base64-encoded-payload>                            │
│                                                                     │
│  Payload:                                                           │
│  {                                                                  │
│    "type": "pairing",                                               │
│    "server_id": "uuid",        // Identifies this server instance   │
│    "issued_at": 1704067200,    // Unix timestamp                    │
│    "expires_at": 1704070800,   // Unix timestamp                    │
│    "nonce": "random-string",   // Prevent replay                    │
│    "signature": "hmac-sha256"  // Integrity check                   │
│  }                                                                  │
│                                                                     │
│  Session Token (for ongoing communication):                         │
│  ──────────────────────────────────────────                         │
│  Format: cdev_s_<base64-encoded-payload>                            │
│                                                                     │
│  Payload:                                                           │
│  {                                                                  │
│    "type": "session",                                               │
│    "client_id": "uuid",        // Assigned client ID                │
│    "device_id": "uuid",        // Device identifier                 │
│    "issued_at": 1704067200,                                         │
│    "expires_at": 1704067500,   // Short-lived (5 min)               │
│    "signature": "hmac-sha256"                                       │
│  }                                                                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Token Lifecycle

```
┌─────────────────────────────────────────────────────────────────────┐
│                      TOKEN LIFECYCLE                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Pairing Token                                                      │
│  ─────────────                                                      │
│  Created:     cdev start / cdev pair                                │
│  Valid for:   1 hour (configurable)                                 │
│  Usage:       Single use (invalidated after successful pairing)     │
│  Refresh:     cdev pair --refresh                                   │
│                                                                     │
│  Session Token                                                      │
│  ─────────────                                                      │
│  Created:     After successful pairing                              │
│  Valid for:   5 minutes (auto-refreshed)                            │
│  Usage:       Multiple use during session                           │
│  Refresh:     Automatic on activity                                 │
│  Revoke:      Server restart / cdev pair --refresh / manual         │
│                                                                     │
│  Timeline:                                                          │
│  ─────────                                                          │
│                                                                     │
│  [Server Start] ──┬──────────────────────────────────────────────►  │
│                   │                                                  │
│                   ▼                                                  │
│           [Pairing Token]                                           │
│                   │ (scan QR)                                       │
│                   ▼                                                  │
│           [Token Validation]                                        │
│                   │ (valid)                                         │
│                   ▼                                                  │
│           [Session Token] ──► [Auto Refresh] ──► [Auto Refresh]     │
│                   │               │                   │             │
│                   └───────────────┴───────────────────┘             │
│                          (active session)                           │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Implementation

### Configuration Schema

```yaml
# ~/.cdev/config.yaml
security:
  # Security mode: local, authenticated, remote
  mode: "local"

  # Require token authentication for connections
  require_auth: false

  # Token settings
  token:
    # Pairing token expiry (seconds)
    pairing_expiry: 3600
    # Session token expiry (seconds)
    session_expiry: 300
    # Auto-refresh session tokens
    auto_refresh: true

  # Origin validation
  origins:
    # Allow all origins (only for local mode)
    allow_all: true
    # Allowed origins list (when allow_all is false)
    allowed:
      - "http://localhost:*"
      - "https://*.devtunnels.ms"

  # TLS settings
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
    # Auto-generate self-signed cert
    auto_cert: false

  # Audit logging
  audit:
    enabled: false
    log_connections: true
    log_commands: true
    log_file: "~/.cdev/audit.log"
```

### New CLI Commands

```bash
# Token management
cdev token list              # List active tokens
cdev token revoke <id>       # Revoke a token
cdev token revoke-all        # Revoke all tokens

# Device management (future)
cdev devices list            # List paired devices
cdev devices revoke <id>     # Revoke device access

# Security status
cdev security status         # Show security configuration
cdev security audit          # Show recent security events
```

### API Endpoints

```
# Token validation (internal)
POST /api/auth/validate      # Validate a token

# Device management (future)
GET  /api/devices            # List paired devices
DELETE /api/devices/:id      # Revoke device
```

### WebSocket Authentication

```go
// Connection with token
ws://localhost:8766/ws?token=cdev_p_xxx

// Or via header
GET /ws HTTP/1.1
Upgrade: websocket
Authorization: Bearer cdev_p_xxx
```

### Error Responses

```json
// 401 Unauthorized - Missing token
{
  "error": "authentication_required",
  "message": "Token required. Scan QR code to connect."
}

// 401 Unauthorized - Invalid token
{
  "error": "invalid_token",
  "message": "Token is invalid or expired. Regenerate with 'cdev pair --refresh'."
}

// 403 Forbidden - Origin not allowed
{
  "error": "origin_forbidden",
  "message": "Connection from this origin is not allowed."
}
```

## Implementation Phases

### Phase 1: Foundation (MVP)

| Feature | Priority | Effort |
|---------|----------|--------|
| Token generation | High | Low |
| Token validation on connect | High | Medium |
| Origin validation | High | Low |
| `--require-auth` flag | High | Low |
| Security config schema | Medium | Low |

### Phase 2: Enhanced Security

| Feature | Priority | Effort |
|---------|----------|--------|
| Session tokens | High | Medium |
| Token expiry enforcement | High | Low |
| Auto token refresh | Medium | Medium |
| Connection audit logging | Medium | Medium |
| `cdev token` commands | Medium | Low |

### Phase 3: TLS Support

| Feature | Priority | Effort |
|---------|----------|--------|
| TLS configuration | Medium | Medium |
| Self-signed cert generation | Low | Medium |
| Let's Encrypt integration | Low | High |

### Phase 4: Enterprise Features (Future)

| Feature | Priority | Effort |
|---------|----------|--------|
| Device registration | Low | High |
| Device management UI | Low | High |
| OAuth/SSO integration | Low | Very High |
| IP whitelisting | Low | Low |
| Role-based access control | Low | Very High |

## Security Checklist

### For Users

- [ ] Keep cdev bound to localhost when not using tunnels
- [ ] Use `--require-auth` when exposing via tunnels
- [ ] Regenerate tokens periodically (`cdev pair --refresh`)
- [ ] Don't share QR codes publicly
- [ ] Use TLS when accessing remotely

### For Development

- [ ] Never log tokens in plain text
- [ ] Use constant-time comparison for token validation
- [ ] Implement proper HMAC for token signatures
- [ ] Set secure defaults (localhost, auth required)
- [ ] Rate limit authentication attempts
- [ ] Audit log all authentication events

## Comparison with Similar Tools

| Feature | cdev (proposed) | Expo Go | VS Code Remote | ngrok |
|---------|-----------------|---------|----------------|-------|
| Default auth | Optional | None | OAuth | None |
| Token-based | ✅ | ❌ | ✅ | ✅ |
| TLS support | Optional | ❌ | ✅ | ✅ |
| Device mgmt | Future | ❌ | ✅ | ✅ |
| Audit logging | Optional | ❌ | ✅ | ✅ |
| Origin check | ✅ | ❌ | ✅ | N/A |

## References

- [OWASP WebSocket Security](https://cheatsheetseries.owasp.org/cheatsheets/WebSocket_Security.html)
- [RFC 6455 - WebSocket Protocol](https://tools.ietf.org/html/rfc6455)
- [JWT Best Practices](https://datatracker.ietf.org/doc/html/rfc8725)
- [HMAC-SHA256](https://tools.ietf.org/html/rfc2104)
- [gorilla/websocket Security](https://pkg.go.dev/github.com/gorilla/websocket#hdr-Origin_Considerations)
