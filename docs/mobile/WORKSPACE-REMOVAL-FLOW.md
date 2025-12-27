# Workspace Removal Flow for cdev-ios

> **Status**: Specification
> **Created**: 2025-12-27
> **Purpose**: Define the UX flow and API calls for removing workspaces in multi-device scenarios

## Table of Contents

1. [Overview](#overview)
2. [Current Backend Behavior](#current-backend-behavior)
3. [Decision Tree](#decision-tree)
4. [User Scenarios](#user-scenarios)
5. [API Calls Reference](#api-calls-reference)
6. [iOS Implementation](#ios-implementation)
7. [Event Handling](#event-handling)
8. [Edge Cases](#edge-cases)

---

## Overview

Removing a workspace in a multi-device environment requires careful handling because:

1. **Active Sessions**: Claude processes may be running in the workspace
2. **Multiple Viewers**: Other devices may be viewing sessions in this workspace
3. **Global vs Local**: User may want to remove for everyone or just hide from their device

### Key Distinction

| Action | Scope | Effect |
|--------|-------|--------|
| **Leave** | This device only | Workspace hidden locally, still exists for others |
| **Remove** | Global (all devices) | Workspace deleted from server, all devices notified |

---

## Current Backend Behavior

### workspace/remove Constraints

The `workspace/remove` RPC method has the following behavior:

```
┌─────────────────────────────────────────────────────────────────┐
│                      workspace/remove                            │
├─────────────────────────────────────────────────────────────────┤
│ 1. Check for active sessions (Claude processes running)         │
│    → If active: Return error "cannot remove workspace with      │
│                 N active session(s)"                            │
│                                                                 │
│ 2. Unregister from session manager                              │
│                                                                 │
│ 3. Remove from config file                                      │
│                                                                 │
│ 4. Broadcast workspace_removed event to ALL clients             │
└─────────────────────────────────────────────────────────────────┘
```

### What Blocks Removal

| Blocker | Check Method | Resolution |
|---------|--------------|------------|
| Running Claude session | `active_session_count > 0` | Call `session/stop` first |
| Other viewers | Not blocked (just notified) | N/A |
| Subscribed clients | Not blocked (just notified) | N/A |

---

## Decision Tree

```
┌─────────────────────────────────────────────────────────────────┐
│                    User taps "Remove Workspace"                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
               ┌───────────────────────────┐
               │  Fetch workspace status   │
               │  (workspace/list or       │
               │   workspace/status)       │
               └───────────────────────────┘
                              │
                              ▼
                 ┌────────────────────────┐
                 │ active_session_count   │
                 │       > 0 ?            │
                 └────────────────────────┘
                    │              │
                   YES             NO
                    │              │
                    ▼              │
        ┌──────────────────────┐   │
        │ POPUP: Session       │   │
        │        Running       │   │
        │                      │   │
        │ "A Claude session is │   │
        │  running. Stop it    │   │
        │  to remove."         │   │
        │                      │   │
        │ [Stop Session]       │   │
        │ [Cancel]             │   │
        └──────────────────────┘   │
                 │                 │
                 │ (if Stop)       │
                 ▼                 │
        ┌──────────────────────┐   │
        │ session/stop         │   │
        │ Wait for completion  │   │
        │ Then restart flow    │◄──┘
        └──────────────────────┘
                              │
                              ▼
               ┌───────────────────────────┐
               │  Check for other viewers  │
               │  (sessions[].viewers)     │
               └───────────────────────────┘
                              │
                              ▼
                 ┌────────────────────────┐
                 │ Other devices viewing  │
                 │ sessions in workspace? │
                 └────────────────────────┘
                    │              │
                   YES             NO
                    │              │
                    ▼              ▼
        ┌──────────────────────┐  ┌──────────────────────┐
        │ POPUP: Other Viewers │  │ POPUP: Confirm       │
        │                      │  │                      │
        │ "Other devices are   │  │ "Remove workspace    │
        │  viewing this        │  │  'project-name'?"    │
        │  workspace."         │  │                      │
        │                      │  │ [Remove]             │
        │ [Leave Only]         │  │ [Cancel]             │
        │ [Remove for Everyone]│  └──────────────────────┘
        │ [Cancel]             │           │
        └──────────────────────┘           │
             │         │                   │
             │         │                   │
             ▼         ▼                   ▼
    ┌────────────┐ ┌────────────┐  ┌────────────────┐
    │ LEAVE ONLY │ │ REMOVE ALL │  │ REMOVE         │
    │            │ │            │  │                │
    │ unwatch    │ │ workspace/ │  │ workspace/     │
    │ unsubscribe│ │ remove     │  │ remove         │
    │ hide local │ │            │  │                │
    └────────────┘ └────────────┘  └────────────────┘
```

---

## User Scenarios

### Scenario 1: No Active Sessions, No Other Viewers

**Context**: Workspace is idle, only this device has it visible.

**Flow**:
```
1. User taps "Remove"
2. Show confirmation: "Remove workspace 'project-name'?"
3. User confirms
4. Call workspace/remove
5. Remove from local UI
```

**API Calls**:
```
→ workspace/remove {workspace_id: "ws-123"}
← {success: true}
```

---

### Scenario 2: No Active Sessions, Other Devices Viewing

**Context**: Workspace is idle, but Device B is viewing a historical session.

**Flow**:
```
1. User taps "Remove"
2. Check: sessions[0].viewers = ["device-a", "device-b"]
3. Show popup: "Other devices are viewing this workspace"
   - [Leave Only] - Just hide from this device
   - [Remove for Everyone] - Delete globally
   - [Cancel]
4a. If "Leave Only":
    - Unwatch session
    - Unsubscribe from workspace
    - Hide from local UI (don't call workspace/remove)
4b. If "Remove for Everyone":
    - Call workspace/remove
    - Server broadcasts workspace_removed to Device B
```

---

### Scenario 3: Active Claude Session Running

**Context**: Claude is actively running in this workspace (via PTY or managed session).

**Flow**:
```
1. User taps "Remove"
2. Check: active_session_count = 1
3. Show popup: "A Claude session is running. Stop it first?"
   - [Stop Session]
   - [Cancel]
4. If "Stop Session":
    - Call session/stop
    - Wait for session to end
    - Restart the removal flow (go to step 1)
```

**API Calls**:
```
→ session/stop {session_id: "sess-456"}
← {status: "stopped"}

(wait for session_end event or poll)

→ workspace/remove {workspace_id: "ws-123"}
← {success: true}
```

---

### Scenario 4: Active Session + Other Viewers

**Context**: Claude is running AND other devices are viewing.

**Flow**:
```
1. User taps "Remove"
2. Check: active_session_count = 1
3. Show popup: "A Claude session is running. Stop it first?"
4. User taps "Stop Session"
5. Call session/stop
6. Wait for completion
7. Now check viewers again
8. Show popup: "Other devices are viewing..."
9. Continue with Scenario 2 flow
```

---

## API Calls Reference

### Check Workspace State

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "workspace/status",
  "params": {
    "workspace_id": "1ea27585-e154-4e31-a8a8-b0f16118b862"
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "workspace_id": "1ea27585-...",
    "workspace_name": "cdev",
    "active_session_count": 1,
    "has_active_session": true,
    "active_session_id": "46e054a5-...",
    "sessions": [
      {
        "id": "46e054a5-...",
        "status": "running",
        "workspace_id": "1ea27585-...",
        "viewers": ["fe61035a-...", "0ba973d6-..."]
      }
    ]
  }
}
```

### Stop Session

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "session/stop",
  "params": {
    "session_id": "46e054a5-..."
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "status": "stopped",
    "session_id": "46e054a5-..."
  }
}
```

### Leave Workspace (This Device Only)

```json
// Step 1: Unwatch session (if watching)
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "workspace/session/unwatch",
  "params": {}
}

// Step 2: Unsubscribe from workspace events
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "workspace/unsubscribe",
  "params": {
    "workspace_id": "1ea27585-..."
  }
}

// Step 3: Remove from local UI only (no server call)
```

### Remove Workspace (Global)

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 5,
  "method": "workspace/remove",
  "params": {
    "workspace_id": "1ea27585-..."
  }
}

// Success Response
{
  "jsonrpc": "2.0",
  "id": 5,
  "result": {
    "success": true,
    "message": "Workspace removed"
  }
}

// Error Response (if session still running)
{
  "jsonrpc": "2.0",
  "id": 5,
  "error": {
    "code": -32603,
    "message": "cannot remove workspace with 1 active session(s)"
  }
}
```

---

## iOS Implementation

### Main Entry Point

```swift
import Foundation

enum RemoveWorkspaceResult {
    case removed           // Workspace deleted globally
    case leftOnly          // Only hidden on this device
    case cancelled         // User cancelled
    case error(String)     // Error occurred
}

class WorkspaceRemovalHandler {
    private let rpc: RPCClient
    private let myClientId: String

    init(rpc: RPCClient, clientId: String) {
        self.rpc = rpc
        self.myClientId = clientId
    }

    /// Main entry point for workspace removal
    func removeWorkspace(_ workspace: Workspace) async -> RemoveWorkspaceResult {
        // Step 1: Get current state
        guard let status = await getWorkspaceStatus(workspace.id) else {
            return .error("Failed to get workspace status")
        }

        // Step 2: Check for running sessions
        if status.activeSessionCount > 0 {
            return await handleActiveSession(workspace, status: status)
        }

        // Step 3: Check for other viewers
        if hasOtherViewers(status) {
            return await handleOtherViewers(workspace, status: status)
        }

        // Step 4: Simple removal (no conflicts)
        return await confirmAndRemove(workspace)
    }

    // MARK: - Private Methods

    private func getWorkspaceStatus(_ workspaceId: String) async -> WorkspaceStatus? {
        do {
            return try await rpc.call("workspace/status", [
                "workspace_id": workspaceId
            ])
        } catch {
            return nil
        }
    }

    private func hasOtherViewers(_ status: WorkspaceStatus) -> Bool {
        for session in status.sessions {
            guard let viewers = session.viewers else { continue }
            let otherViewers = viewers.filter { $0 != myClientId }
            if !otherViewers.isEmpty {
                return true
            }
        }
        return false
    }

    private func handleActiveSession(
        _ workspace: Workspace,
        status: WorkspaceStatus
    ) async -> RemoveWorkspaceResult {
        let choice = await AlertPresenter.show(
            title: "Session Running",
            message: "A Claude session is running in '\(workspace.name)'. Stop it to remove the workspace.",
            actions: [
                .init(title: "Stop Session", style: .destructive),
                .init(title: "Cancel", style: .cancel)
            ]
        )

        guard choice == "Stop Session" else {
            return .cancelled
        }

        // Stop the session
        do {
            try await rpc.call("session/stop", [
                "session_id": status.activeSessionId ?? ""
            ])

            // Wait a moment for session to fully stop
            try await Task.sleep(nanoseconds: 500_000_000) // 0.5s

            // Retry removal flow
            return await removeWorkspace(workspace)

        } catch {
            return .error("Failed to stop session: \(error.localizedDescription)")
        }
    }

    private func handleOtherViewers(
        _ workspace: Workspace,
        status: WorkspaceStatus
    ) async -> RemoveWorkspaceResult {
        let viewerCount = countOtherViewers(status)

        let choice = await AlertPresenter.show(
            title: "Other Devices Viewing",
            message: "\(viewerCount) other device(s) are viewing sessions in '\(workspace.name)'.",
            actions: [
                .init(title: "Leave Only (This Device)", style: .default),
                .init(title: "Remove for Everyone", style: .destructive),
                .init(title: "Cancel", style: .cancel)
            ]
        )

        switch choice {
        case "Leave Only (This Device)":
            return await leaveWorkspace(workspace.id)

        case "Remove for Everyone":
            return await forceRemove(workspace.id)

        default:
            return .cancelled
        }
    }

    private func confirmAndRemove(_ workspace: Workspace) async -> RemoveWorkspaceResult {
        let choice = await AlertPresenter.show(
            title: "Remove Workspace",
            message: "Remove '\(workspace.name)' from all devices?",
            actions: [
                .init(title: "Remove", style: .destructive),
                .init(title: "Cancel", style: .cancel)
            ]
        )

        guard choice == "Remove" else {
            return .cancelled
        }

        return await forceRemove(workspace.id)
    }

    private func countOtherViewers(_ status: WorkspaceStatus) -> Int {
        var count = Set<String>()
        for session in status.sessions {
            guard let viewers = session.viewers else { continue }
            for viewer in viewers where viewer != myClientId {
                count.insert(viewer)
            }
        }
        return count.count
    }

    /// Leave workspace (hide from this device only)
    private func leaveWorkspace(_ workspaceId: String) async -> RemoveWorkspaceResult {
        do {
            // 1. Unwatch session if watching
            try? await rpc.call("workspace/session/unwatch", [:])

            // 2. Unsubscribe from workspace events
            try await rpc.call("workspace/unsubscribe", [
                "workspace_id": workspaceId
            ])

            // 3. Hide from local UI
            await MainActor.run {
                WorkspaceStore.shared.hideWorkspace(workspaceId)
            }

            return .leftOnly

        } catch {
            return .error("Failed to leave workspace: \(error.localizedDescription)")
        }
    }

    /// Remove workspace globally
    private func forceRemove(_ workspaceId: String) async -> RemoveWorkspaceResult {
        do {
            try await rpc.call("workspace/remove", ["workspace_id": workspaceId])
            return .removed
        } catch {
            return .error("Failed to remove workspace: \(error.localizedDescription)")
        }
    }
}
```

### Usage in SwiftUI

```swift
struct WorkspaceRowView: View {
    let workspace: Workspace
    @State private var showingRemoveAlert = false
    @State private var removalResult: RemoveWorkspaceResult?

    var body: some View {
        HStack {
            Text(workspace.name)
            Spacer()
        }
        .swipeActions(edge: .trailing) {
            Button(role: .destructive) {
                Task {
                    let handler = WorkspaceRemovalHandler(
                        rpc: RPCClient.shared,
                        clientId: AppState.shared.clientId
                    )
                    removalResult = await handler.removeWorkspace(workspace)
                    handleResult()
                }
            } label: {
                Label("Remove", systemImage: "trash")
            }
        }
    }

    private func handleResult() {
        switch removalResult {
        case .removed:
            // UI will update via workspace_removed event
            break
        case .leftOnly:
            // Workspace hidden locally
            break
        case .cancelled:
            break
        case .error(let message):
            // Show error toast
            ToastManager.show(message, type: .error)
        case .none:
            break
        }
    }
}
```

---

## Event Handling

### workspace_removed Event

When another device removes a workspace, your device receives this event:

```json
{
  "event": "workspace_removed",
  "timestamp": "2025-12-27T10:30:45.123Z",
  "workspace_id": "1ea27585-...",
  "payload": {
    "id": "1ea27585-...",
    "name": "cdev",
    "path": "/Users/dev/cdev"
  }
}
```

### iOS Event Handler

```swift
class WorkspaceEventHandler {
    func handleEvent(_ event: WebSocketEvent) {
        switch event.type {
        case "workspace_removed":
            handleWorkspaceRemoved(event)
        default:
            break
        }
    }

    private func handleWorkspaceRemoved(_ event: WebSocketEvent) {
        guard let payload = event.payload as? WorkspaceRemovedPayload else { return }

        Task { @MainActor in
            // Remove from local store
            WorkspaceStore.shared.remove(payload.id)

            // Show notification
            ToastManager.show(
                "Workspace '\(payload.name)' was removed by another device",
                type: .info
            )

            // If currently viewing this workspace, navigate away
            if NavigationState.shared.currentWorkspaceId == payload.id {
                NavigationState.shared.navigateToWorkspaceList()
            }
        }
    }
}
```

---

## Edge Cases

### 1. Session Starts While Showing Confirmation

**Problem**: User is viewing "Remove?" confirmation, another device starts a session.

**Solution**:
```swift
// Before calling workspace/remove, re-check status
let currentStatus = await getWorkspaceStatus(workspaceId)
if currentStatus.activeSessionCount > 0 {
    // Show new alert about running session
    return await handleActiveSession(workspace, status: currentStatus)
}
```

### 2. Workspace Removed While Viewing It

**Problem**: User is viewing workspace details, another device removes it.

**Solution**: Listen for `workspace_removed` event and navigate away:
```swift
.onReceive(workspaceRemovedPublisher) { event in
    if event.workspaceId == currentWorkspace.id {
        dismiss()
        showToast("Workspace was removed")
    }
}
```

### 3. Network Error During Removal

**Problem**: `workspace/remove` call fails due to network.

**Solution**: Show retry option:
```swift
case .error(let message):
    let retry = await AlertPresenter.show(
        title: "Error",
        message: message,
        actions: [
            .init(title: "Retry", style: .default),
            .init(title: "Cancel", style: .cancel)
        ]
    )
    if retry == "Retry" {
        return await removeWorkspace(workspace)
    }
```

### 4. Multiple Devices Try to Remove Simultaneously

**Problem**: Two devices tap "Remove" at the same time.

**Solution**: Server handles this - first one wins, second gets error. Handle gracefully:
```swift
} catch let error as RPCError {
    if error.message.contains("not found") {
        // Already removed by another device
        return .removed
    }
    return .error(error.message)
}
```

---

## Summary

| User Action | Condition | Result |
|-------------|-----------|--------|
| Remove | No sessions, no viewers | Direct `workspace/remove` |
| Remove | No sessions, has viewers | Show "Leave Only" / "Remove for Everyone" |
| Remove | Active session | Must stop session first |
| Leave Only | Any | Unsubscribe + hide locally |
| Remove for Everyone | No active sessions | `workspace/remove` + broadcast event |

### Quick Reference: API Sequence

```
Leave Only:
  1. workspace/session/unwatch
  2. workspace/unsubscribe
  3. (local UI update)

Remove (with session):
  1. session/stop
  2. (wait)
  3. workspace/remove

Remove (no session):
  1. workspace/remove
```
