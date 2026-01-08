# Push Notifications for Permission Requests

## Overview

When Claude requires permission approval (via PTY or PreToolUse hook), cdev should send a push notification to registered iOS devices so users can respond even when the app is in background or closed.

## Architecture

```
┌─────────────┐     pty_permission      ┌─────────────┐
│   Claude    │ ──────────────────────► │    cdev     │
│  (PTY/Hook) │                         │   server    │
└─────────────┘                         └──────┬──────┘
                                               │
                    ┌──────────────────────────┼──────────────────────────┐
                    │                          │                          │
                    ▼                          ▼                          ▼
            ┌───────────────┐          ┌───────────────┐          ┌───────────────┐
            │  WebSocket    │          │    APNs       │          │  Device Token │
            │  Broadcast    │          │  Push Notify  │          │    Store      │
            └───────────────┘          └───────┬───────┘          └───────────────┘
                    │                          │
                    │                          ▼
                    │                  ┌───────────────┐
                    └─────────────────►│   cdev-ios    │
                                       └───────────────┘
```

## RPC Methods

### device/register

Register a device for push notifications.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "device/register",
  "params": {
    "device_token": "abc123...",           // APNs device token (hex string)
    "device_id": "unique-device-uuid",     // Persistent device identifier
    "device_name": "iPhone 15 Pro",        // Human-readable name
    "app_version": "1.0.0",                // cdev-ios version
    "os_version": "iOS 17.2",              // OS version
    "environment": "production"            // "production" or "sandbox" (for APNs)
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "registered": true,
    "device_id": "unique-device-uuid"
  }
}
```

### device/unregister

Unregister a device from push notifications.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "device/unregister",
  "params": {
    "device_id": "unique-device-uuid"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "unregistered": true
  }
}
```

### device/list

List all registered devices.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "device/list",
  "params": {}
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "devices": [
      {
        "device_id": "unique-device-uuid",
        "device_name": "iPhone 15 Pro",
        "app_version": "1.0.0",
        "registered_at": "2026-01-03T10:00:00Z",
        "last_seen": "2026-01-03T12:00:00Z"
      }
    ]
  }
}
```

## Push Notification Payload

When a permission request is detected, cdev sends a push notification:

```json
{
  "aps": {
    "alert": {
      "title": "Permission Required",
      "subtitle": "cdev - myproject",
      "body": "Claude wants to execute: npm install express"
    },
    "sound": "default",
    "badge": 1,
    "category": "PERMISSION_REQUEST",
    "mutable-content": 1,
    "interruption-level": "time-sensitive"
  },
  "data": {
    "type": "pty_permission",
    "workspace_id": "myproject",
    "session_id": "abc-123",
    "tool_use_id": "tool-456",
    "tool_name": "Bash",
    "target": "npm install express",
    "permission_type": "bash",
    "timestamp": "2026-01-03T12:00:00Z"
  }
}
```

## Notification Categories (iOS)

Define actionable notifications in cdev-ios:

```swift
// UNNotificationCategory for permission requests
let allowAction = UNNotificationAction(
    identifier: "ALLOW_ACTION",
    title: "Allow",
    options: [.foreground]
)

let denyAction = UNNotificationAction(
    identifier: "DENY_ACTION",
    title: "Deny",
    options: [.destructive]
)

let allowSessionAction = UNNotificationAction(
    identifier: "ALLOW_SESSION_ACTION",
    title: "Allow for Session",
    options: []
)

let permissionCategory = UNNotificationCategory(
    identifier: "PERMISSION_REQUEST",
    actions: [allowAction, allowSessionAction, denyAction],
    intentIdentifiers: [],
    options: [.customDismissAction]
)

UNUserNotificationCenter.current().setNotificationCategories([permissionCategory])
```

## Implementation Steps

### cdev (Go Server)

1. **Device Registry** (`internal/device/registry.go`)
   - Store device tokens in memory (with optional persistence)
   - Track device metadata (name, version, last seen)
   - Handle token refresh/updates

2. **APNs Client** (`internal/push/apns.go`)
   - Use `github.com/sideshow/apns2` library
   - Support both production and sandbox environments
   - Handle APNs authentication (JWT or certificate)
   - Retry logic for failed deliveries

3. **Push Service** (`internal/push/service.go`)
   - Listen for `pty_permission` events
   - Check if WebSocket client responded within N seconds
   - If no response, send push notification to all registered devices
   - Rate limiting to prevent notification spam

4. **RPC Methods** (`internal/rpc/handler/methods/device.go`)
   - Implement device/register, device/unregister, device/list

### cdev-ios (Swift)

1. **Push Registration**
   - Request notification permissions on first launch
   - Get APNs device token
   - Send token to cdev via `device/register`
   - Handle token refresh

2. **Notification Handling**
   - Handle foreground notifications
   - Handle background notifications
   - Implement notification actions (Allow/Deny/Allow for Session)
   - Deep link to permission screen

3. **Token Management**
   - Re-register token on app launch
   - Handle token changes
   - Unregister on logout/disconnect

## Configuration

### cdev config.yaml

```yaml
push:
  enabled: true
  apns:
    key_id: "ABC123DEFG"           # APNs Key ID
    team_id: "TEAM123456"          # Apple Team ID
    key_file: "~/.cdev/AuthKey.p8" # Path to APNs auth key
    bundle_id: "com.example.cdev"  # iOS app bundle ID
    environment: "production"       # "production" or "sandbox"

  # Delay before sending push (wait for WebSocket response)
  delay_seconds: 3

  # Rate limiting
  max_per_minute: 10
```

### APNs Setup

1. Create APNs Key in Apple Developer Portal
2. Download the `.p8` key file
3. Note the Key ID and Team ID
4. Configure in cdev `config.yaml`

## Event Flow

### Permission Request with Push Notification

```
1. Claude triggers permission (PTY or Hook)
2. cdev detects permission request
3. cdev emits pty_permission event via WebSocket
4. cdev starts 3-second timer
5. IF WebSocket client responds within 3s:
   - Process response, cancel timer
6. ELSE (no WebSocket response):
   - Send push notification to all registered devices
   - cdev-ios receives push
   - User taps notification or action button
   - cdev-ios opens and sends response via WebSocket
   - cdev processes response
```

### Offline Device Handling

```
1. Claude triggers permission
2. No WebSocket clients connected
3. cdev sends push notification immediately (no delay)
4. User receives notification
5. User taps notification
6. cdev-ios launches, connects WebSocket
7. cdev-ios calls permission/pending to get pending permissions
8. User responds to permission
```

## Error Handling

| Error | Handling |
|-------|----------|
| Invalid device token | Remove from registry, log warning |
| APNs connection failed | Retry with exponential backoff |
| Device unregistered | Remove from registry |
| Rate limit exceeded | Queue notification, send later |
| Push disabled in config | Skip push, WebSocket only |

## Security Considerations

1. **Token Storage**: Device tokens should be stored securely
2. **Token Validation**: Validate token format before storing
3. **Rate Limiting**: Prevent notification spam
4. **Payload Size**: APNs has 4KB limit, keep payload minimal
5. **Sensitive Data**: Don't include sensitive code/data in notification body

## Testing

### Sandbox Testing
- Use APNs sandbox environment during development
- Set `environment: "sandbox"` in config
- Use development provisioning profile in cdev-ios

### Local Testing
- Use `device/list` to verify registration
- Check cdev logs for push delivery status
- Monitor APNs feedback for delivery issues

## Future Enhancements

1. **Firebase Cloud Messaging**: Support Android devices
2. **Notification Preferences**: Per-workspace notification settings
3. **Quiet Hours**: Don't send notifications during configured hours
4. **Notification Grouping**: Group multiple permissions into single notification
5. **Rich Notifications**: Show code preview in expanded notification
