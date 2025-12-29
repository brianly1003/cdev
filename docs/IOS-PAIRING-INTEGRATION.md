# cdev iOS Pairing Integration Guide

> Documentation for integrating QR code pairing with the cdev mobile app.

## QR Code Data Structure

When scanning the QR code, the iOS app will receive a JSON payload:

```json
{
  "ws": "ws://127.0.0.1:8766/ws",
  "http": "http://127.0.0.1:8766",
  "session": "6f57d617-8d94-4810-b135-cddff5bc1007",
  "repo": "cdev",
  "token": "cdev_p_eyJwIjoiZXlKMGVYQmxJam9pY0dGcGNt..."
}
```

### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ws` | string | Yes | WebSocket URL for real-time events |
| `http` | string | Yes | HTTP API base URL |
| `session` | string | Yes | Server session UUID |
| `repo` | string | Yes | Repository name |
| `token` | string | **Conditional** | Authentication token (only present when `require_auth: true`) |

### Token Presence

- **Token present**: Server has authentication enabled. Use token for all requests.
- **Token absent**: Server allows unauthenticated connections.

---

## Authentication Flow

### 1. Check Token Presence

```swift
struct PairingInfo: Codable {
    let ws: String
    let http: String
    let session: String
    let repo: String
    let token: String?  // Optional - may not be present
}

func handleQRScan(_ jsonString: String) {
    let info = try JSONDecoder().decode(PairingInfo.self, from: data)

    if let token = info.token {
        // Auth required - store token securely
        KeychainService.store(token: token, for: info.session)
    }
}
```

### 2. WebSocket Connection with Token

When connecting to WebSocket, include the token as a query parameter:

```swift
func connect(to info: PairingInfo) {
    var urlString = info.ws

    // Append token if available
    if let token = info.token {
        urlString += "?token=\(token)"
    }

    let url = URL(string: urlString)!
    let request = URLRequest(url: url)

    webSocket = URLSession.shared.webSocketTask(with: request)
    webSocket.resume()
}
```

**WebSocket URL formats:**
- Without auth: `ws://127.0.0.1:8766/ws`
- With auth: `ws://127.0.0.1:8766/ws?token=cdev_p_xxx`

### 3. HTTP API Requests with Token

For HTTP requests, include the token in the `Authorization` header:

```swift
func makeAPIRequest(to endpoint: String, info: PairingInfo) -> URLRequest {
    let url = URL(string: "\(info.http)\(endpoint)")!
    var request = URLRequest(url: url)

    if let token = info.token {
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
    }

    return request
}
```

---

## API Endpoints

### Pairing Endpoints (No Auth Required)

These endpoints are always accessible without authentication:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/pair` | GET | HTML pairing page with QR code |
| `/api/pair/info` | GET | JSON pairing info |
| `/api/pair/qr` | GET | PNG QR code image |
| `/api/pair/refresh` | POST | Generate new token (revokes old) |

### Response: `/api/pair/info`

```json
{
  "ws": "ws://127.0.0.1:8766/ws",
  "http": "http://127.0.0.1:8766",
  "session": "6f57d617-8d94-4810-b135-cddff5bc1007",
  "repo": "cdev",
  "token": "cdev_p_xxx",
  "token_expires_at": "2025-12-30T01:00:00Z"
}
```

### Protected Endpoints (Auth Required When Enabled)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/ws` | WebSocket | Real-time event stream |
| `/api/status` | GET | Server status |
| `/api/claude/*` | Various | Claude CLI operations |
| `/api/git/*` | Various | Git operations |
| `/api/file` | GET | File content retrieval |

---

## Token Lifecycle

### Token Types

| Prefix | Type | Purpose |
|--------|------|---------|
| `cdev_p_` | Pairing token | Initial connection from QR scan |
| `cdev_s_` | Session token | Long-lived session access |

### Token Expiry

- Default expiry: **3600 seconds (1 hour)**
- Configurable via `security.token_expiry_secs`
- QR code auto-refreshes every **60 seconds** for fresh tokens

### Handling Token Expiry

```swift
func handleWebSocketError(_ error: Error) {
    if isAuthenticationError(error) {
        // Token expired - prompt user to re-scan QR code
        showRescanPrompt()
    }
}
```

### Refreshing Tokens

Call `/api/pair/refresh` to get a new token (revokes all previous tokens):

```swift
func refreshToken(baseURL: String) async throws -> String {
    let url = URL(string: "\(baseURL)/api/pair/refresh")!
    var request = URLRequest(url: url)
    request.httpMethod = "POST"

    let (data, _) = try await URLSession.shared.data(for: request)
    let response = try JSONDecoder().decode(RefreshResponse.self, from: data)

    return response.token
}

struct RefreshResponse: Codable {
    let token: String
    let expires_at: String
    let message: String
}
```

---

## Error Handling

### WebSocket Connection Errors

| Error | Cause | Resolution |
|-------|-------|------------|
| 401 Unauthorized | Missing or invalid token | Re-scan QR code |
| 403 Forbidden | Token expired | Call `/api/pair/refresh` or re-scan |
| Connection refused | Server not running | Show "Server offline" message |

### HTTP API Errors

```swift
enum PairingError: Error {
    case noToken
    case tokenExpired
    case serverOffline
    case invalidQRCode
}

func handleHTTPResponse(_ response: HTTPURLResponse) throws {
    switch response.statusCode {
    case 200...299:
        return // Success
    case 401:
        throw PairingError.tokenExpired
    case 503:
        throw PairingError.serverOffline
    default:
        throw PairingError.unknown
    }
}
```

---

## UI Recommendations

### Connection Status Indicators

| State | Color | Icon |
|-------|-------|------|
| Connected | `#68D391` (success) | Checkmark |
| Connecting | `#F6C85D` (warning) | Spinner |
| Disconnected | `#FC8181` (error) | X mark |
| Auth Required | `#4FD1C5` (primary) | Lock |

### QR Scanner View

1. Use camera permission prompt with clear explanation
2. Show scanning frame overlay
3. Provide haptic feedback on successful scan
4. Auto-dismiss scanner on valid QR detection

### Connection View

Display parsed pairing info:
- Repository name (prominent)
- Session ID (truncated: `6f57d617...`)
- Auth status badge
- Connect/Disconnect button

---

## Testing

### Test with Auth Disabled (Default)

```bash
./bin/cdev start
# QR code will NOT contain token
```

### Test with Auth Enabled

```bash
# Create config with auth
cat > ~/.cdev/config.yaml << EOF
security:
  require_auth: true
EOF

./bin/cdev start
# QR code WILL contain token
```

### Verify Token in QR

```bash
./bin/cdev pair --json
# Check if "token" field is present
```

---

## Migration Checklist

- [ ] Update `PairingInfo` model to include optional `token` field
- [ ] Update WebSocket connection to append `?token=` query param
- [ ] Update HTTP requests to include `Authorization: Bearer` header
- [ ] Add token storage in Keychain
- [ ] Handle 401/403 errors with re-scan prompt
- [ ] Update connection status UI with auth indicator
- [ ] Test both auth-enabled and auth-disabled modes

---

## Related Files

- Server pairing handler: `internal/server/http/pairing.go`
- Token manager: `internal/security/token.go`
- Config example: `configs/config.example.yaml`

---

*cdev iOS Pairing Integration Guide v1.0*
