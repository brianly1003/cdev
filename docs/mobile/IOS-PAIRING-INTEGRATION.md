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

| Prefix | Type | Expiry | Purpose |
|--------|------|--------|---------|
| `cdev_p_` | Pairing token | 1 hour | Initial connection from QR scan |
| `cdev_s_` | Access token | 15 min | API/WebSocket authentication |
| `cdev_r_` | Refresh token | 7 days | Obtain new access tokens |

### Recommended Flow

```
┌─────────────┐     Scan QR      ┌─────────────┐
│  iOS App    │ ───────────────► │  Pairing    │
└─────────────┘                  │  Token      │
                                 └──────┬──────┘
                                        │
                                        ▼ POST /api/auth/exchange
                                 ┌──────────────┐
                                 │ Access Token │ ◄── Use for API calls
                                 │ Refresh Token│ ◄── Store in Keychain
                                 └──────┬───────┘
                                        │
                    ┌───────────────────┴───────────────────┐
                    │                                       │
                    ▼ (Every 15 min)                       ▼ (Every 7 days)
             POST /api/auth/refresh                   Re-scan QR
             with refresh_token                       (or auto-refresh)
```

### Step 1: Exchange Pairing Token

After scanning QR, exchange the pairing token for access + refresh tokens:

```swift
struct TokenPairResponse: Codable {
    let access_token: String
    let access_token_expires_at: String
    let refresh_token: String
    let refresh_token_expires_at: String
    let token_type: String
    let expires_in: Int
}

func exchangePairingToken(baseURL: String, pairingToken: String) async throws -> TokenPairResponse {
    let url = URL(string: "\(baseURL)/api/auth/exchange")!
    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")

    let body = ["pairing_token": pairingToken]
    request.httpBody = try JSONEncoder().encode(body)

    let (data, response) = try await URLSession.shared.data(for: request)

    guard let httpResponse = response as? HTTPURLResponse,
          httpResponse.statusCode == 200 else {
        throw AuthError.exchangeFailed
    }

    return try JSONDecoder().decode(TokenPairResponse.self, from: data)
}
```

### Step 2: Use Access Token for API Calls

```swift
class APIClient {
    private var accessToken: String?
    private var refreshToken: String?
    private var accessTokenExpiry: Date?

    func setTokens(_ pair: TokenPairResponse) {
        self.accessToken = pair.access_token
        self.refreshToken = pair.refresh_token
        self.accessTokenExpiry = ISO8601DateFormatter().date(from: pair.access_token_expires_at)

        // Store refresh token securely
        KeychainService.saveToken(pair.refresh_token, for: "refresh")
    }

    func request(_ endpoint: String) async throws -> Data {
        // Check if access token is about to expire (within 1 min)
        if let expiry = accessTokenExpiry, expiry.timeIntervalSinceNow < 60 {
            try await refreshAccessToken()
        }

        var request = URLRequest(url: URL(string: "\(baseURL)\(endpoint)")!)
        request.setValue("Bearer \(accessToken ?? "")", forHTTPHeaderField: "Authorization")

        let (data, _) = try await URLSession.shared.data(for: request)
        return data
    }
}
```

### Step 3: Refresh Access Token

When access token expires, use refresh token to get a new pair:

```swift
func refreshAccessToken() async throws {
    guard let refreshToken = self.refreshToken else {
        throw AuthError.noRefreshToken
    }

    let url = URL(string: "\(baseURL)/api/auth/refresh")!
    var request = URLRequest(url: url)
    request.httpMethod = "POST"
    request.setValue("application/json", forHTTPHeaderField: "Content-Type")

    let body = ["refresh_token": refreshToken]
    request.httpBody = try JSONEncoder().encode(body)

    let (data, response) = try await URLSession.shared.data(for: request)

    guard let httpResponse = response as? HTTPURLResponse else {
        throw AuthError.refreshFailed
    }

    switch httpResponse.statusCode {
    case 200:
        let pair = try JSONDecoder().decode(TokenPairResponse.self, from: data)
        setTokens(pair)
    case 401:
        // Refresh token expired - need to re-pair
        throw AuthError.refreshTokenExpired
    default:
        throw AuthError.refreshFailed
    }
}
```

### Token Storage

Store tokens securely in Keychain:

```swift
class TokenStorage {
    private static let service = "com.cdev.ios"

    static func saveTokenPair(_ pair: TokenPairResponse) {
        save(key: "access_token", value: pair.access_token)
        save(key: "refresh_token", value: pair.refresh_token)
        save(key: "access_expiry", value: pair.access_token_expires_at)
        save(key: "refresh_expiry", value: pair.refresh_token_expires_at)
    }

    static func loadAccessToken() -> String? {
        return load(key: "access_token")
    }

    static func loadRefreshToken() -> String? {
        return load(key: "refresh_token")
    }

    static func clearTokens() {
        delete(key: "access_token")
        delete(key: "refresh_token")
        delete(key: "access_expiry")
        delete(key: "refresh_expiry")
    }

    // ... Keychain helper methods
}
```

### Auto-Refresh Strategy

```swift
class TokenRefreshManager {
    private var refreshTimer: Timer?

    func startAutoRefresh(expiresIn: Int) {
        // Refresh 1 minute before expiry
        let refreshInterval = max(TimeInterval(expiresIn - 60), 30)

        refreshTimer = Timer.scheduledTimer(withTimeInterval: refreshInterval, repeats: false) { [weak self] _ in
            Task {
                try await self?.refreshAccessToken()
            }
        }
    }

    func stopAutoRefresh() {
        refreshTimer?.invalidate()
        refreshTimer = nil
    }
}
```

### Legacy Token Flow

For backwards compatibility, the old pairing token flow still works:
- QR code pairing token can be used directly for WebSocket/API
- `/api/pair/refresh` regenerates the QR code pairing token
- This is simpler but requires re-scanning QR when token expires

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

## Future: Cloud Relay Preparation

The current token structure is forward-compatible with a future Cloud Relay mode. Key points for iOS developers:

### Token Payload Structure (v1)

The decoded token payload includes fields for future cloud integration:

```json
{
  "v": 1,
  "type": "pairing",
  "server_id": "abc123...",
  "agent_id": "abc123...",
  "mode": "local",
  "issued_at": 1735516800,
  "expires_at": 1735520400,
  "nonce": "xyz789..."
}
```

### Mode Field

| Mode | Description | Validation |
|------|-------------|------------|
| `local` | Current behavior | Validate with local agent |
| `cloud` | Future Cloud Relay | Validate with cloud (JWT) |

### Preparing for Cloud Relay

1. **Store the `mode` field** when parsing tokens
2. **Use `agent_id`** instead of `server_id` in new code (they're currently identical)
3. **Plan for JWT tokens** (`cdev_c_*` prefix) which will be cloud-issued
4. **Consider device registration** - future versions may require device fingerprinting

When Cloud Relay launches, the QR code will include:
```json
{
  "ws": "wss://relay.cdev.io/ws",
  "http": "https://relay.cdev.io",
  "session": "...",
  "repo": "cdev",
  "token": "cdev_c_<jwt_token>",
  "mode": "cloud",
  "agent_id": "abc123..."
}
```

For detailed token architecture, see [TOKEN-ARCHITECTURE.md](../security/TOKEN-ARCHITECTURE.md).

---

## Related Files

- Server pairing handler: `internal/server/http/pairing.go`
- Token manager: `internal/security/token.go`
- Token architecture: `docs/security/TOKEN-ARCHITECTURE.md`
- Config example: `configs/config.example.yaml`

---

*cdev iOS Pairing Integration Guide v1.1*
