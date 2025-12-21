# Multi-Workspace Architecture Design

## Executive Summary

This document outlines the design for supporting multiple workspaces/repositories in cdev, enabling the iOS app to switch between different projects.

## Current State Analysis

The cdev-agent is currently a **single-workspace daemon by design**:
- One repository path in configuration
- All components (Claude, Git, Watcher, Sessions) bound to single repo
- Events broadcast to all clients without repo context
- API endpoints assume single repo context

## Design Options Evaluated

### Option A: Multi-Workspace in Single Server
```
┌─────────────────────────────────────────┐
│           cdev-agent (port 8766)        │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐   │
│  │ Repo A  │ │ Repo B  │ │ Repo C  │   │
│  │Components│ │Components│ │Components│  │
│  └─────────┘ └─────────┘ └─────────┘   │
│         Unified API + Event Hub         │
└─────────────────────────────────────────┘
```
- **Pros**: Single connection, unified API
- **Cons**: Complex routing, resource contention, major refactoring needed

### Option B: One Server Per Workspace (Recommended)
```
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ cdev-agent   │  │ cdev-agent   │  │ cdev-agent   │
│ (port 8766)  │  │ (port 8767)  │  │ (port 8768)  │
│   Repo A     │  │   Repo B     │  │   Repo C     │
└──────────────┘  └──────────────┘  └──────────────┘
        │                │                │
        └────────────────┼────────────────┘
                         │
              ┌──────────────────┐
              │ Workspace Manager│
              │   (port 8765)    │
              └──────────────────┘
                         │
              ┌──────────────────┐
              │    cdev-ios      │
              └──────────────────┘
```
- **Pros**: Clean isolation, minimal server changes, natural Claude CLI fit
- **Cons**: Multiple connections to manage

### Option C: Hybrid Coordinator + Workers
- Most complex, overkill for current needs

## Recommended Architecture: Option B

### Rationale

1. **Minimal code changes** - Current server works perfectly for single repo
2. **Natural isolation** - Each workspace independent, no cross-talk
3. **Claude CLI alignment** - Claude runs in working directory, one-at-a-time makes sense
4. **Scalability** - Add/remove workspaces without affecting others
5. **Failure isolation** - One workspace crash doesn't affect others

### Components

#### 1. Workspace Manager Service (New)
A lightweight coordinator that manages workspace configurations and server lifecycle.

```go
// internal/workspace/manager.go
type WorkspaceManager struct {
    configPath  string
    workspaces  map[string]*Workspace
    mu          sync.RWMutex
}

type Workspace struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Path        string    `json:"path"`
    Port        int       `json:"port"`
    Status      string    `json:"status"` // "running", "stopped", "error"
    PID         int       `json:"pid,omitempty"`
    LastActive  time.Time `json:"last_active"`
    AutoStart   bool      `json:"auto_start"`
}

type WorkspaceManager interface {
    // Workspace CRUD
    ListWorkspaces() []Workspace
    AddWorkspace(name, path string) (*Workspace, error)
    RemoveWorkspace(id string) error
    UpdateWorkspace(id string, updates WorkspaceUpdate) error

    // Lifecycle
    StartWorkspace(id string) error
    StopWorkspace(id string) error
    RestartWorkspace(id string) error

    // Discovery
    GetWorkspace(id string) (*Workspace, error)
    GetWorkspaceByPath(path string) (*Workspace, error)
    GetAvailablePort() int
}
```

#### 2. Workspace Configuration File

```yaml
# ~/.cdev/workspaces.yaml
workspaces:
  - id: "ws-001"
    name: "cdev"
    path: "/Users/brianly/Projects/cdev"
    port: 8766
    auto_start: true

  - id: "ws-002"
    name: "messenger-integrator"
    path: "/Users/brianly/Projects/messenger-integrator"
    port: 8767
    auto_start: false

  - id: "ws-003"
    name: "my-ios-app"
    path: "/Users/brianly/Projects/my-ios-app"
    port: 8768
    auto_start: false

settings:
  port_range_start: 8766
  port_range_end: 8799
  max_workspaces: 10
  auto_stop_idle_minutes: 30
```

#### 3. Workspace Manager API (New Service)

Runs on a dedicated port (e.g., 8765) to manage all workspaces:

```
# Workspace Manager Endpoints (port 8765)

GET  /api/workspaces              # List all workspaces
POST /api/workspaces              # Add new workspace
GET  /api/workspaces/{id}         # Get workspace details
PUT  /api/workspaces/{id}         # Update workspace
DELETE /api/workspaces/{id}       # Remove workspace

POST /api/workspaces/{id}/start   # Start workspace server
POST /api/workspaces/{id}/stop    # Stop workspace server
POST /api/workspaces/{id}/restart # Restart workspace server

GET  /api/workspaces/discover     # Auto-discover repos in common paths
GET  /api/health                  # Manager health check
```

#### 4. JSON-RPC Methods for Workspace Manager

```json
// workspace/list
{
  "jsonrpc": "2.0",
  "method": "workspace/list",
  "id": 1
}

// Response
{
  "jsonrpc": "2.0",
  "result": {
    "workspaces": [
      {
        "id": "ws-001",
        "name": "cdev",
        "path": "/Users/brianly/Projects/cdev",
        "port": 8766,
        "status": "running",
        "url": "ws://localhost:8766/ws"
      }
    ]
  },
  "id": 1
}

// workspace/add
{
  "jsonrpc": "2.0",
  "method": "workspace/add",
  "params": {
    "name": "new-project",
    "path": "/Users/brianly/Projects/new-project"
  },
  "id": 2
}

// workspace/start
{
  "jsonrpc": "2.0",
  "method": "workspace/start",
  "params": {
    "id": "ws-002"
  },
  "id": 3
}
```

### iOS App Changes

#### Connection Management
```swift
class WorkspaceConnectionManager {
    private var managerConnection: WebSocketConnection  // Port 8765
    private var workspaceConnections: [String: WebSocketConnection]
    private var activeWorkspaceId: String?

    // Connect to workspace manager first
    func connectToManager(host: String) async throws

    // Get list of workspaces
    func listWorkspaces() async throws -> [Workspace]

    // Connect to specific workspace
    func connectToWorkspace(_ workspace: Workspace) async throws

    // Switch active workspace
    func switchWorkspace(to id: String) async throws

    // Disconnect from workspace
    func disconnectWorkspace(_ id: String)
}
```

#### UI Flow
```
1. App Launch
   └── Connect to Workspace Manager (port 8765)
       └── Fetch workspace list
           └── Show workspace selector

2. Select Workspace
   └── Start workspace if not running
       └── Connect to workspace (e.g., port 8766)
           └── Show workspace UI (sessions, files, git)

3. Switch Workspace
   └── Keep manager connection
       └── Connect to new workspace port
           └── Update UI context
```

### Server Changes Required

#### 1. New Workspace Manager Command
```bash
# Start workspace manager only
cdev workspace-manager start

# Or integrated mode (manager + default workspace)
cdev start --with-manager

# Workspace CLI commands
cdev workspace list
cdev workspace add /path/to/repo --name "My Project"
cdev workspace start ws-001
cdev workspace stop ws-001
```

#### 2. Config Changes
```yaml
# config.yaml - add workspace manager section
workspace_manager:
  enabled: true
  port: 8765
  config_path: "~/.cdev/workspaces.yaml"
```

#### 3. Existing Server Changes (Minimal)
- Add workspace ID to server metadata
- Include workspace info in `status/get` response
- No API changes needed for existing endpoints

### Implementation Phases

#### Phase 1: Workspace Configuration (1-2 days)
- [ ] Create workspace config schema
- [ ] Implement workspace config loader
- [ ] Add workspace CLI commands (`cdev workspace list/add/remove`)

#### Phase 2: Workspace Manager Service (2-3 days)
- [ ] Create workspace manager package
- [ ] Implement process lifecycle management
- [ ] Add HTTP API for workspace operations
- [ ] Add JSON-RPC methods

#### Phase 3: Multi-Instance Support (1-2 days)
- [ ] Port allocation management
- [ ] Process monitoring
- [ ] Auto-restart on crash
- [ ] Idle timeout handling

#### Phase 4: Integration & Testing (2-3 days)
- [ ] End-to-end testing
- [ ] iOS app integration guide
- [ ] Documentation

### Migration Path

1. **Backward Compatible**: Existing `cdev start` works unchanged
2. **Opt-in**: Use `--with-manager` or separate `workspace-manager` command
3. **Gradual Adoption**: iOS app can use single workspace mode initially

### Security Considerations

- Workspace manager only listens on localhost by default
- Each workspace server has independent auth (if configured)
- Path validation to prevent arbitrary directory access
- Port range restrictions

### Resource Management

- Limit concurrent running workspaces
- Auto-stop idle workspaces after configurable timeout
- Memory/CPU monitoring per workspace
- Graceful shutdown on system sleep/wake

### Future Enhancements

1. **Remote workspaces**: Connect to remote cdev-agent instances
2. **Workspace templates**: Quick-start configurations
3. **Workspace sync**: Share workspace configs across devices
4. **Workspace groups**: Organize related projects

## API Reference

### Workspace Manager Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/workspaces` | List all workspaces |
| POST | `/api/workspaces` | Add new workspace |
| GET | `/api/workspaces/{id}` | Get workspace details |
| PUT | `/api/workspaces/{id}` | Update workspace |
| DELETE | `/api/workspaces/{id}` | Remove workspace |
| POST | `/api/workspaces/{id}/start` | Start workspace server |
| POST | `/api/workspaces/{id}/stop` | Stop workspace server |
| GET | `/api/health` | Manager health |

### JSON-RPC Methods

| Method | Description |
|--------|-------------|
| `workspace/list` | List all workspaces |
| `workspace/add` | Add new workspace |
| `workspace/remove` | Remove workspace |
| `workspace/start` | Start workspace server |
| `workspace/stop` | Stop workspace server |
| `workspace/get` | Get workspace details |

## Summary

The recommended approach is **Option B: One Server Per Workspace** with a lightweight Workspace Manager coordinator. This:

1. **Preserves current architecture** - Minimal changes to existing codebase
2. **Provides clean isolation** - Each workspace is independent
3. **Aligns with Claude CLI** - One working directory per instance
4. **Enables flexible deployment** - Run only needed workspaces
5. **Supports future growth** - Easy to add remote workspaces later

The iOS app will connect to the Workspace Manager to discover and manage workspaces, then connect to individual workspace servers as needed.
