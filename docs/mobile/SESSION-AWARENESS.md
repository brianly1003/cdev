# Multi-Device Session Awareness

## Overview

The multi-device session awareness system enables real-time notification and coordination when multiple devices are viewing the same Claude session. This creates a collaborative experience where users can see who else is currently viewing their work.

## Key Features

- **Real-time Notifications**: When Device B joins a session that Device A is viewing, Device A receives immediate notification
- **Independent Device Focus**: Each device maintains its own session focus independently - Device A can view session-1 while Device B views session-2
- **Leave Notifications**: When a device disconnects or switches sessions, other viewers are notified
- **Multi-Workspace Support**: Focus tracking is workspace-aware, supporting independent session tracking per workspace
- **Minimal Metadata**: MVP design tracks only client IDs for simplicity and performance

## Architecture

### Server-Side Components

**SessionFocus Struct** (`internal/server/unified/server.go`):
```go
type SessionFocus struct {
    ClientID    string    // UUID of connected device
    WorkspaceID string    // Workspace being accessed
    SessionID   string    // Session being viewed
    FocusedAt   time.Time // When focus was set
}
```

**Focus Tracking**:
- In-memory map keyed by client ID
- Thread-safe with RWMutex
- Automatically cleared on client disconnect
- No persistence between server restarts (MVP design)

### RPC Method

**`client/session/focus`** - Notify server of session focus change

**Request**:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "client/session/focus",
  "params": {
    "workspace_id": "workspace-123",
    "session_id": "session-456"
  }
}
```

**Response**:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "workspace_id": "workspace-123",
    "session_id": "session-456",
    "other_viewers": ["device-uuid-2", "device-uuid-3"],
    "viewer_count": 3,
    "success": true
  }
}
```

**Parameters**:
- `workspace_id` (required): The workspace ID
- `session_id` (required): The session ID to focus on

**Response Fields**:
- `workspace_id`: The workspace ID
- `session_id`: The session ID
- `other_viewers`: List of client UUIDs currently viewing the same session
- `viewer_count`: Total number of devices viewing this session (including caller)
- `success`: Whether the operation succeeded

### Event Types

#### `session_joined`

Emitted when a device joins a session that other devices are viewing.

**Event Payload**:
```json
{
  "event": "session_joined",
  "timestamp": "2025-12-25T10:30:00Z",
  "workspace_id": "workspace-123",
  "session_id": "session-456",
  "payload": {
    "joining_client_id": "device-uuid-1",
    "other_viewers": ["device-uuid-2"],
    "viewer_count": 2
  }
}
```

**Sent to**: All other clients viewing the same session

#### `session_left`

Emitted when a device leaves a session that other devices are viewing.

**Event Payload**:
```json
{
  "event": "session_left",
  "timestamp": "2025-12-25T10:31:00Z",
  "workspace_id": "workspace-123",
  "session_id": "session-456",
  "payload": {
    "leaving_client_id": "device-uuid-1",
    "remaining_viewers": ["device-uuid-3"],
    "viewer_count": 1
  }
}
```

**Sent to**: All remaining clients viewing the same session

**Note**: `session_left` is only sent if there are remaining viewers. If no other devices are viewing the session, no event is broadcast.

## Implementation Details

### Focus Update Flow

```
1. User taps on a session in the iOS app
2. Client calls client/session/focus RPC method with workspace_id and session_id
3. Server:
   a. Updates client's focus entry in sessionFocus map
   b. Finds other clients viewing the SAME session
   c. If joining viewers exist: emits session_joined event to them
   d. If client previously viewed different session: emits session_left event
4. Returns FocusChangeResult with other_viewers list
5. iOS app updates UI (viewer count badge, shows who's viewing)
```

### Disconnect Flow

```
1. Client connection closes (network error, app closed, etc.)
2. Server's removeClient() is called
3. Server calls clearClientFocus(clientID)
4. If client had active focus:
   a. Find remaining viewers of that session
   b. Emit session_left event if viewers remain
5. Remove client from all tracking maps
```

### Event Broadcasting

Events are published through the event hub to all connected clients. The hub automatically:
- Filters events based on workspace subscriptions
- Routes only relevant events to each client
- Maintains separate filtered subscriber streams per client

## Client Integration (Swift Example)

### 1. Notify Server of Focus Change

```swift
import Foundation

// When user taps on a session in the UI
func didSelectSession(workspaceId: String, sessionId: String) async {
    do {
        // Notify server about focus change
        let result: FocusChangeResult = try await rpcClient.call(
            "client/session/focus",
            params: [
                "workspace_id": workspaceId,
                "session_id": sessionId
            ]
        )

        // Update UI with viewer information
        updateViewerCount(result.viewerCount)

        // Show list of other viewers if any
        if !result.otherViewers.isEmpty {
            showViewersList(result.otherViewers)
        }
    } catch {
        print("Failed to set session focus: \(error)")
    }
}

// Response model
struct FocusChangeResult: Codable {
    let workspaceId: String
    let sessionId: String
    let otherViewers: [String]
    let viewerCount: Int
    let success: Bool
}
```

### 2. Handle Session Joined Event

```swift
// In your WebSocket event handler
func handleWebSocketMessage(_ data: Data) {
    guard let event = try? JSONDecoder().decode(WebSocketEvent.self, from: data) else {
        return
    }

    if event.event == "session_joined" {
        handleSessionJoined(event)
    }
}

func handleSessionJoined(_ event: WebSocketEvent) {
    guard let payload = event.payload as? SessionJoinedPayload else {
        return
    }

    // Check if this event is for the session we're currently viewing
    guard event.workspaceId == currentWorkspaceId,
          event.sessionId == currentSessionId else {
        return
    }

    // Show notification to user
    showNotification(
        title: "Collaborator Joined",
        message: "\(payload.joiningClientId) is now viewing this session"
    )

    // Update viewer count badge in UI
    updateViewerCount(payload.viewerCount)

    // Animate viewer list update
    animateViewerListUpdate()
}

struct SessionJoinedPayload: Codable {
    let joiningClientId: String
    let otherViewers: [String]
    let viewerCount: Int
}
```

### 3. Handle Session Left Event

```swift
func handleSessionLeft(_ event: WebSocketEvent) {
    guard let payload = event.payload as? SessionLeftPayload else {
        return
    }

    // Check if this event is for the session we're currently viewing
    guard event.workspaceId == currentWorkspaceId,
          event.sessionId == currentSessionId else {
        return
    }

    // Show notification (optional, can be subtle)
    showNotification(
        title: "Collaborator Left",
        message: "\(payload.leavingClientId) stopped viewing this session"
    )

    // Update viewer count
    updateViewerCount(payload.viewerCount)

    // If this was the last viewer, you might hide the viewer UI
    if payload.viewerCount == 1 {
        hideViewersList()
    }
}

struct SessionLeftPayload: Codable {
    let leavingClientId: String
    let remainingViewers: [String]
    let viewerCount: Int
}
```

### 4. Complete Example with UI Updates

```swift
class SessionViewController: UIViewController {
    @IBOutlet weak var viewerCountBadge: UILabel!
    @IBOutlet weak var viewersListView: UICollectionView!

    var currentWorkspaceId: String?
    var currentSessionId: String?
    var viewerUUIDs: [String] = []

    func selectSession(workspaceId: String, sessionId: String) {
        currentWorkspaceId = workspaceId
        currentSessionId = sessionId

        Task {
            await updateSessionFocus(workspaceId: workspaceId, sessionId: sessionId)
        }
    }

    private func updateSessionFocus(workspaceId: String, sessionId: String) async {
        do {
            let result: FocusChangeResult = try await rpcClient.call(
                "client/session/focus",
                params: [
                    "workspace_id": workspaceId,
                    "session_id": sessionId
                ]
            )

            // Update UI on main thread
            DispatchQueue.main.async {
                self.updateViewerUI(viewerCount: result.viewerCount, otherViewers: result.otherViewers)
            }
        } catch {
            print("Error setting focus: \(error)")
        }
    }

    private func updateViewerUI(viewerCount: Int, otherViewers: [String]) {
        // Update badge
        viewerCountBadge.text = "\(viewerCount)"
        viewerCountBadge.isHidden = viewerCount <= 1

        // Update list
        viewerUUIDs = otherViewers
        viewersListView.reloadData()
    }

    // Handle incoming events
    func onSessionJoined(_ payload: SessionJoinedPayload) {
        guard currentWorkspaceId == eventWorkspaceId,
              currentSessionId == eventSessionId else {
            return
        }

        updateViewerUI(
            viewerCount: payload.viewerCount,
            otherViewers: payload.otherViewers
        )

        // Show notification
        let alert = UIAlertController(
            title: "Collaborator Joined",
            message: "\(payload.joiningClientId) is viewing this session",
            preferredStyle: .alert
        )
        alert.addAction(UIAlertAction(title: "OK", style: .default))
        present(alert, animated: true)
    }

    func onSessionLeft(_ payload: SessionLeftPayload) {
        guard currentWorkspaceId == eventWorkspaceId,
              currentSessionId == eventSessionId else {
            return
        }

        updateViewerUI(
            viewerCount: payload.viewerCount,
            otherViewers: payload.remainingViewers
        )
    }
}
```

## Limitations & MVP Design

The current MVP implementation has the following characteristics:

### Current Limitations
- **No Device Names**: Only uses client UUIDs, not readable device names
- **In-Memory Only**: Focus state is lost on server restart
- **No Persistence**: No recording of focus history
- **Minimal Metadata**: No rich presence info (viewing vs. editing, position, etc.)

### Future Enhancement Possibilities

1. **Device Metadata**
   ```go
   type ClientInfo struct {
       ClientID     string
       DeviceName   string
       DeviceType   string // ios, android, web, desktop
       UserID       string // if available
       UserName     string // if available
   }
   ```

2. **Rich Presence**
   ```go
   type PresenceInfo struct {
       ClientID        string
       State           string // viewing, editing, idle
       LastActivity    time.Time
       ScrollPosition  int    // which line
       SelectedText    string // for shared highlighting
   }
   ```

3. **Persistence**
   - Store focus events in database
   - Query historical co-viewing patterns
   - Generate collaboration analytics

4. **Advanced Features**
   - Synchronized scrolling
   - Shared cursors
   - Live session recording
   - Collaborative editing

## Migration Path from MVP

The system is designed to be easily extensible:

1. **Add Device Metadata**: Add optional `client_info` field to events
2. **Add State Machine**: Extend `SessionFocus` with `state` field (viewing, editing, idle)
3. **Add Persistence**: Wrap sessionFocus map access with database layer
4. **Database Schema** (future):
   ```sql
   CREATE TABLE session_focus (
       id INTEGER PRIMARY KEY,
       client_id TEXT NOT NULL,
       workspace_id TEXT NOT NULL,
       session_id TEXT NOT NULL,
       state TEXT DEFAULT 'viewing',
       focused_at TIMESTAMP,
       cleared_at TIMESTAMP
   );
   ```

## Error Handling

### Server-Side Validation
- Client ID must be present in context (provided by RPC framework)
- workspace_id and session_id are required parameters
- Invalid parameters return `ErrInvalidParams` error

### Client-Side Handling
```swift
do {
    let result: FocusChangeResult = try await rpcClient.call(...)
} catch RPCError.invalidParams(let msg) {
    print("Invalid parameters: \(msg)")
    // Show error to user
} catch RPCError.internalError(let msg) {
    print("Server error: \(msg)")
    // Show error to user
} catch {
    print("Connection error: \(error)")
    // Retry or show connection error
}
```

## Testing

### Manual Testing Scenarios

1. **Two Devices Same Session**
   - Open session-1 on Device A
   - See viewer_count: 1
   - Open same session on Device B
   - Verify session_joined event on Device A
   - Verify other_viewers: [device-b-uuid] on both

2. **Device Switching Sessions**
   - Device A viewing session-1
   - Device A switches to session-2
   - Verify session_left event sent to session-1 viewers (if any)
   - Verify session_joined event sent to session-2 viewers

3. **Network Disconnect**
   - Device A viewing session-1
   - Device B viewing session-1
   - Force disconnect Device A
   - Verify session_left event on Device B

4. **Rapid Focus Changes**
   - Device rapidly switches between sessions
   - Verify all events are processed
   - Verify viewer counts are accurate

### Unit Test Examples

```go
func TestSetSessionFocus_JoinExistingViewer(t *testing.T) {
    // Setup: Device A already viewing session-1
    // Action: Device B joins session-1
    // Assert: session_joined event published with otherViewers: [device-a-uuid]
}

func TestSetSessionFocus_SwitchSession(t *testing.T) {
    // Setup: Device A viewing session-1
    // Action: Device A switches to session-2
    // Assert: session_left event for session-1, session_joined event for session-2
}

func TestClearClientFocus_NotifiesViewers(t *testing.T) {
    // Setup: Device A and B viewing same session
    // Action: Disconnect Device A
    // Assert: session_left event sent to Device B
}

func TestSetSessionFocus_FirstViewer(t *testing.T) {
    // Setup: No devices viewing session-1
    // Action: Device A views session-1
    // Assert: No session_joined event (other_viewers is empty)
}
```

## Performance Considerations

- **Memory Usage**: O(n) where n = number of connected clients, one SessionFocus struct per client
- **Event Broadcasting**: O(m) where m = number of viewers of specific session
- **Focus Update**: O(n) to find other viewers (single scan of sessionFocus map)
- **For 1000 concurrent clients**: ~24KB base overhead + ~40 bytes per client ~= 64KB

## Security Notes

- Client IDs are UUIDs generated by clients, no authentication required for MVP
- No sensitive data in session_joined/session_left events
- Events are only sent to authenticated WebSocket connections
- Workspace filtering ensures clients only receive events for subscribed workspaces

## Summary

The multi-device session awareness system provides real-time collaborative feedback with minimal overhead. The MVP design using in-memory focus tracking and client UUIDs creates a solid foundation for future enhancements including device names, rich presence, and persistent collaboration analytics.
