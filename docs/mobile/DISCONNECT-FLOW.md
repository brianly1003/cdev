# Disconnect and Server Switch Flow

This document describes what happens when a mobile device disconnects from cdev-agent, either due to:
- App going to background/closing
- Network disconnection
- User switching to a different server
- WebSocket timeout

## Server-Side Disconnect Handling

When a WebSocket connection closes, the server executes cleanup in `unified/server.go:removeClient()`:

```
┌─────────────────────────────────────────────────────────────────┐
│  WebSocket Disconnect Detected (readPump exits)                 │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 1: Capture subscribed workspaces                          │
│  (before removing client from maps)                             │
│  subscribedWorkspaces = filtered.GetSubscribedWorkspaces()      │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 2: Remove from client maps                                │
│  - delete(s.clients, id)                                        │
│  - delete(s.filteredClients, id)                                │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 3: Clear session focus                                    │
│  clearClientFocus(id)                                           │
│  - Removes from sessionFocus map                                │
│  - Emits session_left event to remaining viewers                │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│  Step 4: Disconnect handler cleanup                             │
│  OnClientDisconnect(clientID, subscribedWorkspaces)             │
│  - Decrements git watcher reference counts                      │
│  - Removes client from streamerWatchers map                     │
│  - Closes streamer only if last watcher                         │
└─────────────────────────────────────────────────────────────────┘
```

## Detailed Cleanup Steps

### Step 3: clearClientFocus() - server.go:377-387

```go
func (s *Server) clearClientFocus(clientID string) {
    s.sessionFocusMu.Lock()
    focus, ok := s.sessionFocus[clientID]
    delete(s.sessionFocus, clientID)
    s.sessionFocusMu.Unlock()

    // Emit session_left if client was viewing a session
    if ok && focus != nil {
        s.emitSessionLeft(clientID, focus.WorkspaceID, focus.SessionID)
    }
}
```

This ensures other devices viewing the same session are notified that a viewer left.

### Step 4: OnClientDisconnect() - manager.go:290-325

```go
func (m *Manager) OnClientDisconnect(clientID string, subscribedWorkspaces []string) {
    // Decrement git watcher counts for each subscribed workspace
    for _, workspaceID := range subscribedWorkspaces {
        m.StopGitWatcher(workspaceID)  // Uses reference counting
    }

    // Remove client from session streamer watchers (only if they were watching)
    m.streamerMu.Lock()
    if m.streamerWatchers[clientID] {
        delete(m.streamerWatchers, clientID)
        if len(m.streamerWatchers) == 0 && m.streamer != nil {
            // Last watcher - close the streamer
            m.streamer.Close()
            m.streamer = nil
            m.streamerWorkspaceID = ""
            m.streamerSessionID = ""
        }
    }
    m.streamerMu.Unlock()
}
```

Key point: Uses map-based tracking (`streamerWatchers[clientID]`) to prevent double-decrement bugs.

## Events Other Devices Receive

When Device A disconnects, Device B may receive:

| Event | Condition | Purpose |
|-------|-----------|---------|
| `session_left` | Device A was viewing same session | Update viewer count in UI |
| `claude_message` | Continues if any watcher remains | Session streaming continues |

**Important**: The streamer only closes when the LAST watcher disconnects. Other devices continue receiving `claude_message` events.

## iOS Implementation

### Recommended: Graceful Disconnect

Call cleanup methods before disconnecting for cleaner server logs:

```swift
class ServerConnectionManager {
    private var currentWatchedSession: SessionInfo?
    private var subscribedWorkspaces: Set<String> = []

    func switchToServer(_ newServer: ServerInfo) async {
        // 1. Unwatch active session (optional - cleanup happens on disconnect)
        if currentWatchedSession != nil {
            try? await rpcClient.call("session/unwatch", params: [:])
            currentWatchedSession = nil
        }

        // 2. Unsubscribe from workspaces (optional - cleanup happens on disconnect)
        for workspaceID in subscribedWorkspaces {
            try? await rpcClient.call("workspace/unsubscribe",
                                       params: ["workspace_id": workspaceID])
        }
        subscribedWorkspaces.removeAll()

        // 3. Close WebSocket
        webSocket.disconnect()

        // 4. Connect to new server
        await connectToServer(newServer)
    }
}
```

### Alternative: Quick Disconnect

The server handles all cleanup automatically on disconnect:

```swift
func switchToServer(_ newServer: ServerInfo) async {
    // Just disconnect - server handles cleanup
    webSocket.disconnect()

    // Clear local state
    currentWatchedSession = nil
    subscribedWorkspaces.removeAll()

    // Connect to new server
    await connectToServer(newServer)
}
```

Both approaches work correctly because:
1. Server tracks watchers by client ID (map), not just count
2. `OnClientDisconnect()` only decrements if client was actually watching
3. No double-decrement when calling `session/unwatch` + disconnect

### Handling Unexpected Disconnects

```swift
class WebSocketManager {
    func handleDisconnect(error: Error?) {
        // Clear local state
        currentWatchedSession = nil
        subscribedWorkspaces.removeAll()

        if shouldReconnect(error) {
            scheduleReconnect()
        } else {
            notifyUserOfDisconnect()
        }
    }

    private func shouldReconnect(_ error: Error?) -> Bool {
        // Reconnect for network errors, not for intentional closes
        guard let error = error else { return false }
        return isNetworkError(error)
    }
}
```

## Server Switch vs. Simple Disconnect

| Scenario | Behavior |
|----------|----------|
| **App closes** | Server cleanup runs, other devices notified |
| **Network lost** | WebSocket timeout triggers cleanup |
| **Switch servers** | Same as app closes, then new connection |
| **Background** | iOS may close WebSocket, cleanup runs |

## Multi-Device Session Viewing

```
Device A watching Session X    Device B watching Session X
         │                              │
         │  ◄──── session_joined ─────► │
         │                              │
         ▼                              ▼
    [Both receive claude_message events]
         │                              │
    Device A disconnects                │
         │                              │
         └──── session_left ──────────► │
                                        │
                                        ▼
                           [Device B continues receiving
                            claude_message events]
```

## Reference Counting for Git Watchers

Git watchers use reference counting to support multiple subscribers:

```
Device A subscribes to Workspace W    Device B subscribes to Workspace W
              │                                    │
              ▼                                    ▼
    gitWatcherCounts["W"] = 1          gitWatcherCounts["W"] = 2
    (git watcher started)              (reuses existing watcher)
              │                                    │
    Device A disconnects                           │
              │                                    │
              ▼                                    ▼
    gitWatcherCounts["W"] = 1          [Device B still receives
    (watcher continues)                 git_status_changed events]
              │
    Device B disconnects
              │
              ▼
    gitWatcherCounts["W"] = 0
    (git watcher stopped)
```

## Troubleshooting

### Device B stops receiving events after Device A leaves

This was a bug fixed by changing from integer counter to map-based tracking:

**Before (bug)**:
```go
streamerWatcherCount int  // Could be decremented twice
```

**After (fix)**:
```go
streamerWatchers map[string]bool  // Tracks specific client IDs
```

The fix ensures `OnClientDisconnect()` only removes a client if they were actually in the map, preventing double-decrement.

### Session shows "historical" when viewers are present

The `workspace/list` method now checks for active viewers:

```go
if hist.SessionID == activeSessionID || hasViewers {
    status = "running"
}
```

## Related Documentation

- [WORKSPACE-REMOVAL-FLOW.md](./WORKSPACE-REMOVAL-FLOW.md) - Workspace removal with multi-device support
- [LOGGING-TRACING-DESIGN.md](../architecture/LOGGING-TRACING-DESIGN.md) - Debug logging strategy
