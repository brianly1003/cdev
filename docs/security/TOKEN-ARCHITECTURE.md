# Token Architecture

This document describes the cdev token system, its current implementation, and its forward-compatible design for Cloud Relay integration.

## Overview

cdev uses HMAC-SHA256 signed tokens for authentication between mobile clients and the local agent. The system is designed to be:

1. **Secure** - Tokens are cryptographically signed and can't be forged
2. **Persistent** - Server secrets survive restarts (tokens remain valid)
3. **Forward-compatible** - Token structure supports future Cloud Relay

## Token Types

### Current (Local Mode)

| Prefix | Type | Default Expiry | Purpose |
|--------|------|----------------|---------|
| `cdev_p_` | Pairing | 1 hour | Initial QR code pairing (one-time exchange) |
| `cdev_s_` | Access | 15 minutes | API/WebSocket authentication |
| `cdev_r_` | Refresh | 7 days | Obtain new access tokens without re-pairing |

### Token Flow

```
QR Code Scan → Pairing Token → Exchange → Access Token + Refresh Token
                                              │              │
                                              │              └─► Stored in Keychain
                                              └─► Used for API calls
                                                    │
                                                    ▼ (expires in 15 min)
                                              Refresh Token → New Access + Refresh
```

### Future (Cloud Relay) - Reserved

| Prefix | Type | Purpose |
|--------|------|---------|
| `cdev_d_` | Device | Long-lived device identity |
| `cdev_a_` | Agent | Agent registration with cloud |
| `cdev_c_` | Channel | Cloud-issued JWT for relay |

## Token Payload Structure

```go
type TokenPayload struct {
    // Version for payload format migration
    Version int `json:"v,omitempty"`  // Currently: 1

    // Core fields (always present)
    Type      TokenType `json:"type"`       // "pairing" or "session"
    ServerID  string    `json:"server_id"`  // Local agent identifier
    IssuedAt  int64     `json:"issued_at"`  // Unix timestamp
    ExpiresAt int64     `json:"expires_at"` // Unix timestamp
    Nonce     string    `json:"nonce"`      // Unique per token

    // Cloud Relay fields (optional, for future use)
    Mode     TokenMode `json:"mode,omitempty"`      // "local" or "cloud"
    AgentID  string    `json:"agent_id,omitempty"`  // Alias for ServerID
    DeviceID string    `json:"device_id,omitempty"` // Client fingerprint
    UserID   string    `json:"user_id,omitempty"`   // Cloud user ID
}
```

## Token Format

Tokens are structured as:
```
<prefix><base64url(json)>
```

The JSON structure contains:
```json
{
  "p": "<base64url(payload_json)>",
  "s": "<base64url(hmac_signature)>"
}
```

Example token (truncated):
```
cdev_p_eyJwIjoiZXlKMklqb3hMQ0owZVhCbElqb...
```

## Secret Persistence

Server secrets are stored in `~/.cdev/token_secret.json`:

```json
{
  "server_id": "abc123def456...",
  "secret": "<base64_encoded_32_byte_secret>"
}
```

**Security:**
- File permissions: `0600` (owner read/write only)
- Directory permissions: `0700`
- Secrets regenerated only if file is missing/corrupted

This ensures tokens remain valid across agent restarts.

## Validation Flow

```
┌──────────────┐     token      ┌──────────────┐
│   Client     │ ────────────► │    Agent     │
└──────────────┘                └──────┬───────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │ Parse Prefix  │ ──► Determine type
                               └───────┬───────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │ Decode Base64 │
                               └───────┬───────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │ Verify HMAC   │ ──► Check signature
                               └───────┬───────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │ Check ServerID│ ──► Must match
                               └───────┬───────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │ Check Revoked │ ──► Not in revoke list
                               └───────┬───────┘
                                       │
                                       ▼
                               ┌───────────────┐
                               │ Check Expiry  │ ──► Not expired
                               └───────┬───────┘
                                       │
                                       ▼
                                   ✓ VALID
```

## Error Types

| Error | Description |
|-------|-------------|
| `ErrInvalidFormat` | Token structure is malformed |
| `ErrInvalidToken` | Signature verification failed |
| `ErrExpiredToken` | Token has passed its expiry time |
| `ErrTokenRevoked` | Token was explicitly revoked |
| `ErrTokenNotFound` | Token doesn't exist (for lookup ops) |

## Cloud Relay Architecture (Future)

The current token system is designed to support a future Cloud Relay mode:

### Current: Local Mode
```
┌─────────────┐     Direct      ┌─────────────┐
│  iOS App    │ ◄─────────────► │  cdev Agent │
└─────────────┘   cdev_p/s_*    └─────────────┘
                  Local tokens
```

### Future: Cloud Relay Mode
```
┌─────────────┐                 ┌───────────────┐                 ┌─────────────┐
│  iOS App    │ ◄─────────────► │  Cloud Relay  │ ◄─────────────► │  cdev Agent │
└─────────────┘   cdev_c_*      └───────────────┘   cdev_a_*      └─────────────┘
                  JWT tokens                        Agent tokens
```

### Migration Path

1. **Phase 1 (Complete)**: Local HMAC tokens with persistence + refresh tokens
2. **Phase 2 (Future)**: Database-backed refresh token storage for multi-device
3. **Phase 3 (Future)**: Cloud Relay with JWT tokens
4. **Phase 4 (Future)**: Hybrid mode (local fallback when offline)

The `Mode` field in TokenPayload allows clients to determine how to validate:
- `mode: "local"` - Validate against local agent secret
- `mode: "cloud"` - Validate against cloud public key (JWT)

## API Usage

### Generate Tokens
```go
tm, _ := security.NewTokenManager(3600) // 1 hour default pairing expiry

// Pairing token (for QR code)
pairingToken, expiresAt, _ := tm.GeneratePairingToken()

// Access token (15 min)
accessToken, expiresAt, _ := tm.GenerateAccessToken()

// Refresh token (7 days)
refreshToken, expiresAt, _ := tm.GenerateRefreshToken()

// Token pair (access + refresh)
pair, _ := tm.GenerateTokenPair()
// pair.AccessToken, pair.RefreshToken
```

### Exchange Pairing Token
```go
// Exchange pairing token for access/refresh pair (one-time use)
pair, err := tm.ExchangePairingToken(pairingToken)
if err != nil {
    // Pairing token invalid or already used
}
// Use pair.AccessToken for API calls
// Store pair.RefreshToken in Keychain
```

### Refresh Tokens
```go
// Use refresh token to get new access/refresh pair
newPair, err := tm.RefreshTokenPair(refreshToken)
if err != nil {
    // Refresh token invalid/expired - need to re-pair
}
// Old refresh token is now revoked (one-time use)
```

### Validate Token
```go
payload, err := tm.ValidateToken(token)
if err != nil {
    switch err {
    case security.ErrExpiredToken:
        // Token expired - use refresh token
    case security.ErrInvalidToken:
        // Bad signature
    case security.ErrTokenRevoked:
        // Token was revoked (e.g., after refresh)
    }
}
```

### Revoke Token
```go
// Revoke single token
tm.RevokeToken(token)

// Revoke all tokens (regenerates secret)
tm.RevokeAllTokens()
```

## HTTP Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/auth/exchange` | POST | No | Exchange pairing token for access/refresh pair |
| `/api/auth/refresh` | POST | No | Refresh tokens using refresh token |

### POST /api/auth/exchange

Request:
```json
{
  "pairing_token": "cdev_p_xxx..."
}
```

Response:
```json
{
  "access_token": "cdev_s_xxx...",
  "access_token_expires_at": "2025-12-30T12:15:00Z",
  "refresh_token": "cdev_r_xxx...",
  "refresh_token_expires_at": "2026-01-06T12:00:00Z",
  "token_type": "Bearer",
  "expires_in": 900
}
```

### POST /api/auth/refresh

Request:
```json
{
  "refresh_token": "cdev_r_xxx..."
}
```

Response: Same as `/api/auth/exchange`

## Configuration

In `config.yaml`:
```yaml
security:
  require_auth: true          # Enable token authentication
  token_expiry_secs: 3600     # Default pairing token expiry
  bind_localhost_only: true   # Security: localhost only
```

## Best Practices

1. **Enable authentication** in production: `require_auth: true`
2. **Use short-lived tokens** where possible
3. **Implement token refresh** for long sessions
4. **Monitor token usage** for anomalies
5. **Never log full tokens** - only prefixes for debugging

## See Also

- [SECURITY.md](./SECURITY.md) - Overall security architecture
- [IOS-PAIRING-INTEGRATION.md](../mobile/IOS-PAIRING-INTEGRATION.md) - iOS client implementation
