# QR Code Pairing Design

## Overview

cdev uses QR codes to enable mobile devices to connect to the development server. This document outlines the current implementation, industry best practices, and recommended improvements.

## Current Implementation

```
┌─────────────────────────────────────────────────────────────┐
│ cdev start                                                  │
│   └── Auto-generates QR in terminal                         │
│       └── Contains: ws_url, http_url, session_id, repo_name │
│                                                             │
│ Config: pairing.show_qr_in_terminal: true/false             │
└─────────────────────────────────────────────────────────────┘
```

### QR Code Contents

```json
{
  "ws": "ws://127.0.0.1:8766/ws",
  "http": "http://127.0.0.1:8766",
  "session": "uuid-session-id",
  "repo": "repository-name",
  "token": "optional-auth-token"
}
```

### Current Limitations

- Device management CLI not implemented (`cdev devices ...`)
- QR SVG endpoint not implemented (`/api/pair/qr?format=svg`)
- Pairing page shows a refresh timer but does not display token expiry details
- CLI output needs explicit `server.external_url` or `--external-url` to show public tunnel URLs

## Industry Patterns Comparison

| Tool | Pattern | QR Timing | Token | Security |
|------|---------|-----------|-------|----------|
| **WhatsApp Web** | On-demand | When opening web page | Rotating, encrypted | High |
| **Expo Go** | Auto on start | Terminal output | Session-based | Low |
| **VS Code Remote** | Device code flow | No QR | OAuth | High |
| **Home Assistant** | Web page | On-demand `/pair` page | One-time token | Medium |
| **Tailscale** | Account-based | No QR | OAuth | High |
| **ngrok** | URL display | Terminal output | URL-based | Low |
| **cdev (current)** | Auto + on-demand | Terminal + `/pair` | One-time pairing token + refresh tokens | Medium |

### Analysis

- **Development tools** (Expo, ngrok, cdev): Auto-show on start is acceptable
- **Security-focused tools** (WhatsApp, Tailscale): On-demand with token rotation
- **Enterprise tools** (VS Code Remote): OAuth/device code flow

**Conclusion:** For a development tool like cdev, auto-QR on start is acceptable but should be enhanced with on-demand regeneration and web-based access.

## Recommended Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                         PAIRING FLOW                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  Option 1: Terminal (current + enhanced)                            │
│  ────────────────────────────────────────                           │
│  $ cdev start              → Shows QR (configurable)                │
│  $ cdev start --no-qr      → Silent start                           │
│  $ cdev pair               → Show QR on demand                      │
│  $ cdev pair --refresh     → New token + QR                         │
│  $ cdev pair --page        → Print pairing page URL                 │
│  $ cdev pair --external-url https://<tunnel>  → Public URLs         │
│                                                                     │
│  Option 2: Web Page (new)                                           │
│  ────────────────────────────                                       │
│  http://localhost:8766/pair (local)                                 │
│  https://<tunnel>/pair (remote)                                     │
│  ├── Shows QR code in browser                                       │
│  ├── Works with VS Code port forwarding                             │
│  ├── Auto-refresh option                                            │
│  └── Copy connection URL button                                     │
│                                                                     │
│  Option 3: API (new)                                                │
│  ────────────────────                                               │
│  GET /api/pair/info        → JSON with connection details           │
│  GET /api/pair/qr          → QR code as PNG image                   │
│  POST /api/pair/refresh    → Generate new token                     │
│  POST /api/auth/revoke     → Revoke refresh token (disconnect)      │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## CLI Commands

### Current Commands

```bash
cdev start                    # Shows QR by default (if configured)
cdev pair                     # Show QR code in terminal
cdev pair --refresh           # Generate new token + show QR
cdev pair --json              # Output connection info as JSON
cdev pair --url               # Output connection URL only
cdev pair --page              # Output pairing page URL (/pair)
cdev pair --external-url https://<tunnel>  # Override public URL
```

### Recommended Commands

```bash
# Start options
cdev start                    # Shows QR by default
cdev start --no-qr            # Start without QR display
cdev start --headless         # Daemon mode, no terminal output

# On-demand pairing (new)
cdev pair                     # Show QR code in terminal
cdev pair --refresh           # Generate new token + show QR
cdev pair --json              # Output connection info as JSON
cdev pair --url               # Output connection URL only
cdev pair --page              # Output pairing page URL (/pair)
cdev pair --external-url https://<tunnel>  # Override public URL

# Future: Device management
cdev devices list             # List paired devices
cdev devices revoke <id>      # Revoke device access
```

## HTTP Endpoints

### Current Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/pair` | GET | HTML page with QR code |
| `/api/pair/info` | GET | JSON connection info |
| `/api/pair/qr` | GET | PNG QR code |
| `/api/pair/refresh` | POST | Generate new pairing token (revokes existing tokens) |
| `/api/auth/exchange` | POST | Exchange pairing token for access + refresh tokens |
| `/api/auth/refresh` | POST | Refresh access token using refresh token |
| `/api/auth/revoke` | POST | Revoke refresh token (explicit disconnect) |

### Recommended Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/pair/qr?format=svg` | GET | SVG QR code |

### `/api/pair/info` Response

```json
{
  "ws_url": "ws://127.0.0.1:8766/ws",
  "http_url": "http://127.0.0.1:8766",
  "session_id": "uuid-session-id",
  "repo_name": "my-project",
  "token": "abc123",
  "token_expires_at": "2024-01-01T12:00:00Z"
}
```

### `/pair` Web Page

Simple HTML page that displays:
- QR code (auto-generated)
- Connection URLs (copyable)
- Refresh button
- Mobile app download links

```html
<!-- Example structure -->
<div class="pair-container">
  <h1>Connect Mobile App</h1>
  <div class="qr-code">
    <img src="/api/pair/qr" alt="QR Code" />
  </div>
  <div class="connection-info">
    <p>WebSocket: ws://127.0.0.1:8766/ws</p>
    <p>HTTP: http://127.0.0.1:8766</p>
    <button onclick="copyUrl()">Copy URL</button>
  </div>
  <button onclick="refresh()">Refresh QR Code</button>
</div>
```

## Security Considerations

### Current Security

| Feature | Status |
|---------|--------|
| Token in QR | Not implemented |
| Token expiry | Not implemented |
| Token refresh | Not implemented |
| Device registration | Not implemented |
| Device revocation | Not implemented |

### Recommended Security

| Feature | Priority | Description |
|---------|----------|-------------|
| Token in QR | High | Include short-lived token in QR data |
| Token expiry | High | Tokens expire after configurable time (default: 1hr) |
| Token refresh | Medium | `cdev pair --refresh` generates new token |
| One-time tokens | Low | Token invalidated after first use |
| Device registration | Low | Track paired devices |
| Device revocation | Low | Ability to revoke device access |

### Token Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                         TOKEN FLOW                                  │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  1. Server Start                                                    │
│     └── Generate initial pairing token                              │
│         └── Token expires in pairing.token_expiry_secs (3600)       │
│                                                                     │
│  2. QR Code Display                                                 │
│     └── Include token in QR data                                    │
│         └── Mobile app extracts token on scan                       │
│                                                                     │
│  3. Mobile Connection                                               │
│     └── App sends token in WebSocket handshake                      │
│         └── Server validates token                                  │
│             ├── Valid: Accept connection                            │
│             └── Invalid/Expired: Reject with 401                    │
│                                                                     │
│  4. Token Refresh (optional)                                        │
│     └── cdev pair --refresh                                         │
│         └── Invalidates old token                                   │
│         └── Generates new token                                     │
│         └── Displays new QR                                         │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

## Configuration

### Current Config

```yaml
pairing:
  token_expiry_secs: 3600      # Token validity period
  show_qr_in_terminal: true    # Show QR on cdev start
```

### Recommended Config

```yaml
pairing:
  # Display settings
  show_qr_in_terminal: true    # Show QR on cdev start

  # Token settings
  token_expiry_secs: 3600      # Token validity (seconds)
  token_enabled: false         # Require token for connections (future)

  # Web pairing page
  web_pair_enabled: true       # Enable /pair endpoint
```

## Implementation Phases

### Phase 1: Core Enhancements (MVP)

| Feature | Status | Effort |
|---------|--------|--------|
| Add `cdev pair` command | Done | Low |
| Add `--no-qr` flag to start | Pending | Low |
| Add `/api/pair/info` endpoint | Done | Low |
| Add `/api/pair/qr` endpoint | Done | Low |

### Phase 2: Web Pairing

| Feature | Status | Effort |
|---------|--------|--------|
| Add `/pair` HTML page | Done | Medium |
| QR code refresh button | Done | Low |
| Copy URL functionality | Pending | Low |
| Token expiry display | Pending | Low |

### Phase 3: Security Enhancements

| Feature | Status | Effort |
|---------|--------|--------|
| Token validation on connect | Done | Medium |
| Token refresh mechanism | Done | Medium |
| Connection authentication | Done | Medium |

### Phase 4: Device Management (Future)

| Feature | Status | Effort |
|---------|--------|--------|
| Device registration | Pending | High |
| `cdev devices list` | Pending | Medium |
| `cdev devices revoke` | Pending | Medium |
| Device activity tracking | Pending | High |

## Use Cases

### Use Case 1: Local Development

```bash
# Developer starts cdev, scans QR with phone
$ cdev start
🚀 cdev started on http://127.0.0.1:8766

Scan QR code with cdev mobile app:
  ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  █ QR CODE HERE █
  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

### Use Case 2: VS Code Port Forwarding

```bash
# Developer uses VS Code tunnel, needs web-based QR
$ cdev start --external-url https://my-tunnel.devtunnels.ms

# On mobile browser, open:
# https://my-tunnel.devtunnels.ms/pair
# Scan QR code displayed on web page
```

### Use Case 3: Headless/CI Environment

```bash
# Start without terminal output
$ cdev start --headless

# Get connection info programmatically
$ curl http://localhost:8766/api/pair/info
{"ws_url": "ws://...", "http_url": "http://...", ...}
```

### Use Case 4: Regenerate QR (token compromised)

```bash
# Show current QR
$ cdev pair

# Generate new token and show new QR
$ cdev pair --refresh
New pairing token generated.
Previous connections will be disconnected.

Scan QR code with cdev mobile app:
  ▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄
  █ QR CODE HERE █
  ▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀
```

## Summary

### Current State Assessment

| Aspect | Current | Recommended |
|--------|---------|-------------|
| Auto-show QR on start | ✅ Implemented | Keep as default |
| On-demand regeneration | ✅ Implemented | Keep as default |
| Web-based pairing | ✅ Implemented | Keep as default |
| Token mechanism | ✅ Implemented | Keep as default |
| API endpoints | ✅ Implemented | Keep as default |

### Verdict

**Current approach (auto-QR on start) is acceptable** for a development tool, similar to Expo Go. However, it can still be enhanced with:

1. **`--no-qr` flag** - For scripting/headless use
2. **Token expiry display** - Optional UI improvement
3. **Device management** - Future device list/revoke

## References

- [WhatsApp Web QR Implementation](https://faq.whatsapp.com/1317564962315842)
- [Expo Go Development](https://docs.expo.dev/get-started/expo-go/)
- [OAuth 2.0 Device Authorization Grant](https://oauth.net/2/device-flow/)
- [go-qrcode Library](https://github.com/skip2/go-qrcode)
