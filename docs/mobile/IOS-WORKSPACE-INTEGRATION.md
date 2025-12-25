# cdev Workspace Manager - iOS Integration Guide

**Target Audience:** iOS developers integrating cdev-ios with the workspace manager

**Last Updated:** 2025-12-23

---

## Quick Start

### What is the Workspace Manager?

The workspace manager allows users to manage multiple cdev-agent instances (workspaces) from a single server. Each workspace runs its own cdev-agent on a unique port.

**Key Benefit for iOS:** Users can switch between multiple projects/repositories without restarting the agent or reconfiguring the app.

### Connection Details

**Manager Endpoint:** `ws://127.0.0.1:8765/ws` (or user's IP for network access)

**Protocol:** JSON-RPC 2.0 over WebSocket (same protocol as single workspace mode)

**Manager Port:** 8765 (fixed)
**Workspace Ports:** 8766-8799 (auto-allocated)

---

## Architecture Overview

```
┌──────────────────────────────────────────────────┐
│                  cdev-ios App                    │
└──────────────────┬───────────────────────────────┘
                   │
                   │ JSON-RPC 2.0
                   ↓
┌──────────────────────────────────────────────────┐
│         Workspace Manager (port 8765)            │
│  • List workspaces                               │
│  • Start/stop workspaces                         │
│  • Discover repositories                         │
└────────┬──────────────┬──────────────────────────┘
         │              │
         ↓              ↓
┌─────────────┐  ┌─────────────┐
│ Workspace A │  │ Workspace B │
│ (port 8766) │  │ (port 8767) │
│   Backend   │  │  Frontend   │
└─────────────┘  └─────────────┘
```

**iOS App Connections:**
1. **Manager connection** (port 8765) - List/manage workspaces
2. **Workspace connections** (ports 8766-8799) - Connect to active workspace for Claude operations

---

## Essential Documentation

Share these docs with the iOS team:

### 1. Protocol Specification
📄 **`docs/api/UNIFIED-API-SPEC.md`**
- JSON-RPC 2.0 format
- Request/response examples
- Error codes

### 2. Workspace Manager API
📄 **`docs/architecture/MULTI-WORKSPACE-USAGE.md`**
- Complete workspace API reference
- JSON-RPC methods
- Example requests/responses

### 3. Architecture & Design
📄 **`docs/architecture/MULTI-WORKSPACE-DESIGN.md`**
- System architecture
- Port allocation
- Process management

### 4. This Integration Guide
📄 **`docs/mobile/IOS-WORKSPACE-INTEGRATION.md`** (this document)

---

## JSON-RPC 2.0 Methods

All workspace operations use JSON-RPC 2.0 over WebSocket at `ws://127.0.0.1:8765/ws`.

### Connection Flow

```swift
// 1. Connect to workspace manager
let managerWS = WebSocket(url: "ws://192.168.1.100:8765/ws")

// 2. List available workspaces
managerWS.send(jsonRPC: "workspace/list")

// 3. User selects workspace "Backend API"

// 4. Start workspace (if not running)
managerWS.send(jsonRPC: "workspace/start", params: ["id": "ws-abc123"])

// 5. Connect to workspace's cdev-agent
let workspaceWS = WebSocket(url: "ws://192.168.1.100:8766/ws")

// 6. Use workspace connection for Claude operations
workspaceWS.send(jsonRPC: "agent/run", params: ["prompt": "..."])
```

### Available Methods

#### workspace/list

List all configured workspaces.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "workspace/list",
  "params": {}
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "workspaces": [
      {
        "id": "ws-a1b2c3d4",
        "name": "Backend API",
        "path": "/Users/dev/backend",
        "port": 8766,
        "status": "running",
        "auto_start": true,
        "pid": 12345,
        "restart_count": 0,
        "last_active": "2025-12-23T10:30:00Z"
      },
      {
        "id": "ws-e5f6g7h8",
        "name": "Frontend App",
        "path": "/Users/dev/frontend",
        "port": 8767,
        "status": "stopped",
        "auto_start": false,
        "pid": 0,
        "restart_count": 0,
        "last_active": "2025-12-22T15:00:00Z"
      }
    ]
  }
}
```

**Workspace Status Values:**
- `"running"` - Workspace server is running (can connect)
- `"stopped"` - Workspace server is stopped
- `"starting"` - Workspace is starting (wait for "running")
- `"stopping"` - Workspace is stopping
- `"error"` - Workspace failed to start

#### workspace/start

Start a workspace server.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "workspace/start",
  "params": {
    "id": "ws-a1b2c3d4"
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "id": "ws-a1b2c3d4",
    "name": "Backend API",
    "status": "starting",
    "port": 8766,
    ...
  }
}
```

**Important:** Status may be `"starting"` in response. Poll `workspace/get` or subscribe to events to know when it's `"running"`.

#### workspace/stop

Stop a workspace server.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "workspace/stop",
  "params": {
    "id": "ws-a1b2c3d4"
  }
}
```

#### workspace/discover

Scan for Git repositories (for workspace setup from mobile).

Uses **cache-first strategy**: returns cached results immediately if available, triggers background refresh if cache is stale. This ensures fast response times (<100ms for cached results).

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "method": "workspace/discover",
  "params": {
    "paths": ["/Users/dev/Projects"],
    "fresh": false
  }
}
```

**Parameters:**
- `paths` (optional): Custom paths to scan. If not provided, scans default paths.
- `fresh` (optional): Set to `true` to force a fresh scan, ignoring cache.

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 4,
  "result": {
    "repositories": [
      {
        "name": "my-backend",
        "path": "/Users/dev/Projects/my-backend",
        "remote_url": "https://github.com/user/my-backend.git",
        "last_modified": "2025-12-23T10:00:00Z",
        "is_configured": false
      }
    ],
    "count": 1,
    "cached": true,
    "cache_age_seconds": 1800,
    "refresh_in_progress": true,
    "elapsed_ms": 5,
    "scanned_paths": 0,
    "skipped_paths": 0
  }
}
```

**Response Fields:**
- `cached`: Whether results came from cache
- `cache_age_seconds`: Age of cached results in seconds
- `refresh_in_progress`: Whether a background refresh is running
- `elapsed_ms`: Time taken for this request

See [REPOSITORY-DISCOVERY.md](../architecture/REPOSITORY-DISCOVERY.md) for full architecture details.

---

## Swift Integration Example

### 1. Models

```swift
// Workspace model
struct WorkspaceInfo: Codable {
    let id: String
    let name: String
    let path: String
    let port: Int
    let status: WorkspaceStatus
    let autoStart: Bool
    let pid: Int?
    let restartCount: Int?
    let lastActive: Date?

    enum CodingKeys: String, CodingKey {
        case id, name, path, port, status, pid
        case autoStart = "auto_start"
        case restartCount = "restart_count"
        case lastActive = "last_active"
    }
}

enum WorkspaceStatus: String, Codable {
    case running
    case stopped
    case starting
    case stopping
    case error
}

// Discovered repository
struct DiscoveredRepo: Codable {
    let name: String
    let path: String
    let remoteUrl: String?
    let lastModified: Date?
    let isConfigured: Bool

    enum CodingKeys: String, CodingKey {
        case name, path
        case remoteUrl = "remote_url"
        case lastModified = "last_modified"
        case isConfigured = "is_configured"
    }
}

// Discovery result with cache metadata
struct DiscoveryResult: Codable {
    let repositories: [DiscoveredRepo]
    let count: Int
    let cached: Bool
    let cacheAgeSeconds: Int64?
    let refreshInProgress: Bool?
    let elapsedMs: Int64
    let scannedPaths: Int?
    let skippedPaths: Int?

    enum CodingKeys: String, CodingKey {
        case repositories, count, cached
        case cacheAgeSeconds = "cache_age_seconds"
        case refreshInProgress = "refresh_in_progress"
        case elapsedMs = "elapsed_ms"
        case scannedPaths = "scanned_paths"
        case skippedPaths = "skipped_paths"
    }
}
```

### 2. Workspace Manager Service

```swift
import Foundation

class WorkspaceManagerService {
    private let webSocketService: WebSocketService
    private let managerHost: String
    private let managerPort: Int = 8765

    @Published var workspaces: [WorkspaceInfo] = []

    init(host: String, webSocketService: WebSocketService) {
        self.managerHost = host
        self.webSocketService = webSocketService
    }

    func connect() async throws {
        let url = URL(string: "ws://\(managerHost):\(managerPort)/ws")!
        try await webSocketService.connect(to: url)
    }

    func listWorkspaces() async throws -> [WorkspaceInfo] {
        let response = try await webSocketService.sendRequest(
            method: "workspace/list",
            params: [String: Any]()
        )

        let result = response["workspaces"] as! [[String: Any]]
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601

        let data = try JSONSerialization.data(withJSONObject: result)
        let workspaces = try decoder.decode([WorkspaceInfo].self, from: data)

        await MainActor.run {
            self.workspaces = workspaces
        }

        return workspaces
    }

    func startWorkspace(_ id: String) async throws -> WorkspaceInfo {
        let response = try await webSocketService.sendRequest(
            method: "workspace/start",
            params: ["id": id]
        )

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let data = try JSONSerialization.data(withJSONObject: response)
        return try decoder.decode(WorkspaceInfo.self, from: data)
    }

    func stopWorkspace(_ id: String) async throws {
        _ = try await webSocketService.sendRequest(
            method: "workspace/stop",
            params: ["id": id]
        )
    }

    func discoverRepositories(paths: [String]? = nil, fresh: Bool = false) async throws -> DiscoveryResult {
        var params: [String: Any] = [:]
        if let paths = paths {
            params["paths"] = paths
        }
        if fresh {
            params["fresh"] = true
        }

        let response = try await webSocketService.sendRequest(
            method: "workspace/discover",
            params: params
        )

        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601

        let data = try JSONSerialization.data(withJSONObject: response)
        return try decoder.decode(DiscoveryResult.self, from: data)
    }
}
```

### 3. UI - Workspace Switcher

```swift
import SwiftUI

struct WorkspaceSwitcherView: View {
    @StateObject private var manager: WorkspaceManagerService
    @State private var selectedWorkspace: WorkspaceInfo?

    var body: some View {
        List(manager.workspaces, id: \.id) { workspace in
            WorkspaceRow(workspace: workspace)
                .onTapGesture {
                    Task {
                        await selectWorkspace(workspace)
                    }
                }
        }
        .task {
            await loadWorkspaces()
        }
    }

    private func loadWorkspaces() async {
        do {
            try await manager.connect()
            _ = try await manager.listWorkspaces()
        } catch {
            print("Failed to load workspaces: \(error)")
        }
    }

    private func selectWorkspace(_ workspace: WorkspaceInfo) async {
        // If not running, start it
        if workspace.status != .running {
            do {
                _ = try await manager.startWorkspace(workspace.id)
                // Wait a bit for startup
                try await Task.sleep(nanoseconds: 1_000_000_000)
            } catch {
                print("Failed to start workspace: \(error)")
                return
            }
        }

        // Connect to workspace's cdev-agent
        let workspaceURL = URL(string: "ws://\(manager.managerHost):\(workspace.port)/ws")!
        // Switch to this workspace connection
        selectedWorkspace = workspace
    }
}

struct WorkspaceRow: View {
    let workspace: WorkspaceInfo

    var body: some View {
        HStack {
            VStack(alignment: .leading) {
                Text(workspace.name)
                    .font(.headline)
                Text(workspace.path)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            Spacer()

            // Status indicator
            Circle()
                .fill(statusColor)
                .frame(width: 12, height: 12)

            Text(workspace.status.rawValue)
                .font(.caption)
                .foregroundColor(.secondary)
        }
        .padding(.vertical, 4)
    }

    private var statusColor: Color {
        switch workspace.status {
        case .running: return .green
        case .stopped: return .gray
        case .starting: return .orange
        case .stopping: return .orange
        case .error: return .red
        }
    }
}
```

---

## Common Workflows

### Workflow 1: Initial Setup (First Launch)

```swift
// 1. User enters manager host (e.g., "192.168.1.100")
// 2. Connect to manager
try await managerService.connect()

// 3. Discover repositories on user's laptop
// Results may come from cache for instant display
let result = try await managerService.discoverRepositories()

// 4. Show list of discovered repos
// Check if a background refresh is happening
if result.cached && result.refreshInProgress == true {
    showRefreshingIndicator()
}

// 5. User selects repos to add

// 6. Add workspace via REST API (or future JSON-RPC method)
// POST http://192.168.1.100:8765/api/workspaces
// {
//   "name": "Backend API",
//   "path": "/Users/dev/backend",
//   "auto_start": true,
//   "port": 0  // auto-allocate
// }

// 7. List workspaces to get the new workspace
let workspaces = try await managerService.listWorkspaces()
```

### Workflow 2: Daily Use

```swift
// 1. User opens app
// 2. Connect to manager
try await managerService.connect()

// 3. List workspaces
let workspaces = try await managerService.listWorkspaces()

// 4. Show workspace switcher UI
// 5. User selects "Backend API"
// 6. Check status:
if workspace.status == .running {
    // Connect directly
    connectToWorkspace(workspace)
} else {
    // Start then connect
    try await managerService.startWorkspace(workspace.id)
    // Poll until running or subscribe to events
    // Then connect
}
```

### Workflow 3: Switching Workspaces

```swift
// User is working on Backend, wants to switch to Frontend

// 1. Disconnect from current workspace WebSocket
currentWorkspaceWS.disconnect()

// 2. Start target workspace (if stopped)
if targetWorkspace.status != .running {
    try await managerService.startWorkspace(targetWorkspace.id)
}

// 3. Connect to target workspace
let url = URL(string: "ws://\(host):\(targetWorkspace.port)/ws")!
newWorkspaceWS.connect(to: url)

// 4. Update UI to show current workspace
currentWorkspace = targetWorkspace
```

---

## Error Handling

### Common Errors

| Error Code | Message | Solution |
|------------|---------|----------|
| -32001 | Workspace not found | Refresh workspace list |
| -32002 | Workspace already running | Connect to existing port |
| -32003 | Max concurrent workspaces | Stop an idle workspace |
| -32004 | Port already in use | Use different port or stop conflicting process |

### Error Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32001,
    "message": "Workspace not found: ws-invalid"
  }
}
```

### Swift Error Handling

```swift
do {
    try await manager.startWorkspace(id)
} catch let error as JSONRPCError {
    switch error.code {
    case -32001:
        showAlert("Workspace not found. Please refresh.")
    case -32003:
        showAlert("Too many workspaces running. Stop one first.")
    default:
        showAlert("Error: \(error.message)")
    }
}
```

---

## Testing Guide

### 1. Test Manager Connection

```bash
# On laptop, start workspace manager
./bin/cdev workspace-manager start

# Find laptop IP
ifconfig | grep "inet "

# On iPhone, connect to ws://192.168.1.100:8765/ws
```

### 2. Test JSON-RPC Methods

Use a WebSocket testing tool or curl:

```bash
# List workspaces
wscat -c ws://127.0.0.1:8765/ws
> {"jsonrpc":"2.0","id":1,"method":"workspace/list","params":{}}

# Start workspace
> {"jsonrpc":"2.0","id":2,"method":"workspace/start","params":{"id":"ws-abc123"}}
```

### 3. Test Workspace Connection

```bash
# Connect to running workspace
wscat -c ws://127.0.0.1:8766/ws
> {"jsonrpc":"2.0","id":1,"method":"status/get","params":{}}
```

---

## Migration from Single Workspace

**Existing cdev-ios** connects to a single cdev-agent instance:
```
ws://192.168.1.100:8766/ws
```

**With Workspace Manager:**
1. Connect to manager: `ws://192.168.1.100:8765/ws`
2. List workspaces
3. Connect to selected workspace: `ws://192.168.1.100:8766/ws` (or 8767, 8768, etc.)

**Backward Compatibility:**
- Old single workspace mode still works (`cdev start` unchanged)
- Users can choose: single workspace OR workspace manager
- Same JSON-RPC protocol for both modes

---

## Performance Considerations

### Connection Management

- **Manager connection:** Keep alive for workspace management
- **Workspace connection:** Active connection to current workspace only
- **Don't connect to all workspaces** - only the active one

### Polling vs Events

**For workspace status updates:**
- Option A: Poll `workspace/list` every 10 seconds
- Option B: Subscribe to workspace events (future enhancement)

**Recommended:** Start with polling, migrate to events when available.

### Network Transitions

- **WiFi → Cellular:** Manager becomes unreachable (localhost only)
- **Handle gracefully:** Show "Connect to same WiFi as laptop" message
- **Reconnection:** Auto-reconnect when back on WiFi

---

## Security Notes

⚠️ **Important:** Workspace manager binds to `127.0.0.1` by default (localhost only).

For iOS to connect over network:
1. User must configure manager to bind to `0.0.0.0` or their IP
2. Or use SSH tunnel (more secure)

**No authentication currently implemented** - assumes trusted local network.

---

## Quick Reference

### Endpoints

| Service | URL | Purpose |
|---------|-----|---------|
| Manager WebSocket | `ws://<host>:8765/ws` | Workspace management (JSON-RPC) |
| Manager REST API | `http://<host>:8765/api/workspaces` | Workspace management (HTTP) |
| Workspace Agent | `ws://<host>:8766-8799/ws` | Claude operations (JSON-RPC) |

### JSON-RPC Methods

| Method | Purpose | Params |
|--------|---------|--------|
| `workspace/list` | List all workspaces | None |
| `workspace/get` | Get workspace details | `{id}` |
| `workspace/start` | Start workspace | `{id}` |
| `workspace/stop` | Stop workspace | `{id}` |
| `workspace/discover` | Find Git repos | `{paths}` (optional) |

---

## Support

**Questions?** Check:
- `docs/architecture/MULTI-WORKSPACE-USAGE.md` - Full API reference
- `docs/api/UNIFIED-API-SPEC.md` - JSON-RPC 2.0 protocol
- GitHub Issues: https://github.com/brianly1003/cdev/issues

**iOS-Specific Integration Issues:**
- Post in cdev-ios repository
- Tag workspace manager integration
