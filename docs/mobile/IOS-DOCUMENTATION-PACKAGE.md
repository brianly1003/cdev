# Documentation Package for cdev-ios Integration

**Last Updated:** 2025-12-23

This document lists all documentation needed for the cdev-ios team to integrate with the workspace manager.

---

## Essential Documents (Share These)

### 1. 🚀 **iOS Integration Guide** (START HERE)
**File:** `docs/mobile/IOS-WORKSPACE-INTEGRATION.md`

**What's included:**
- Quick start guide
- Swift code examples
- JSON-RPC methods reference
- Common workflows
- Error handling
- Testing guide

**Target Audience:** iOS developers implementing workspace management

---

### 2. 📋 **Multi-Workspace Usage Guide**
**File:** `docs/architecture/MULTI-WORKSPACE-USAGE.md`

**What's included:**
- Complete API reference (JSON-RPC + REST)
- Configuration options
- Process monitoring details
- Troubleshooting guide
- Best practices

**When to use:** Reference for detailed API parameters and behavior

---

### 3. 🔌 **JSON-RPC 2.0 Protocol Spec**
**File:** `docs/api/UNIFIED-API-SPEC.md`

**What's included:**
- JSON-RPC 2.0 message format
- All agent methods (agent/run, agent/stop, etc.)
- Event format
- Error codes
- Swift integration examples

**When to use:** Understanding the underlying JSON-RPC protocol

---

### 4. 🏗️ **Architecture & Design**
**File:** `docs/architecture/MULTI-WORKSPACE-DESIGN.md`

**What's included:**
- System architecture diagrams
- Port allocation strategy
- Process management
- Security considerations

**When to use:** Understanding how the system works internally

---

## Optional Reference Documents

### 5. **Complete API Reference**
**File:** `docs/api/API-REFERENCE.md`

Comprehensive HTTP + WebSocket API for single workspace mode. Useful for understanding existing patterns.

### 6. **Protocol Specification**
**File:** `docs/api/PROTOCOL.md`

Detailed protocol evolution and event specifications.

### 7. **Security & Performance**
**File:** `docs/architecture/SECURITY-AND-PERFORMANCE.md`

Security analysis, performance benchmarks, and best practices.

---

## Quick Links for iOS Team

### For First-Time Integration

1. Read: **iOS Integration Guide** (`IOS-WORKSPACE-INTEGRATION.md`)
2. Implement: Swift models and WebSocket service
3. Test: Connect to manager, list workspaces
4. Reference: Multi-Workspace Usage Guide for detailed API

### For Specific Tasks

| Task | Document to Check |
|------|-------------------|
| Implementing workspace switcher | iOS Integration Guide → Workflow 2 |
| Handling JSON-RPC errors | iOS Integration Guide → Error Handling |
| Understanding workspace status | Multi-Workspace Usage Guide → Process Monitoring |
| Discovering repositories | iOS Integration Guide → workspace/discover |
| Testing connection | iOS Integration Guide → Testing Guide |

### For Protocol Questions

| Question | Document to Check |
|----------|-------------------|
| How to format JSON-RPC requests? | UNIFIED-API-SPEC.md |
| What error codes exist? | UNIFIED-API-SPEC.md → Error Codes |
| How do WebSocket events work? | API-REFERENCE.md → WebSocket Events |
| What's the difference between JSON-RPC and REST? | Multi-Workspace Usage Guide → Protocol Support |

---

## Implementation Checklist for iOS Team

### Phase 1: Basic Integration
- [ ] Add `WorkspaceInfo` and `DiscoveredRepo` models
- [ ] Create `WorkspaceManagerService` class
- [ ] Implement `workspace/list` method
- [ ] Implement `workspace/start` method
- [ ] Test connection to manager

### Phase 2: UI Integration
- [ ] Create workspace switcher view
- [ ] Show workspace status (running/stopped)
- [ ] Implement start/stop buttons
- [ ] Handle workspace selection
- [ ] Switch WebSocket connections when changing workspace

### Phase 3: Discovery & Setup
- [ ] Implement `workspace/discover` method
- [ ] Create repository discovery UI
- [ ] Allow adding workspaces from mobile
- [ ] Handle network connectivity issues

### Phase 4: Polish
- [ ] Error handling for all methods
- [ ] Loading states and animations
- [ ] Workspace status polling or events
- [ ] Connection management (reconnect logic)
- [ ] User preferences (default workspace, etc.)

---

## Code Examples Quick Reference

### Connect to Manager (Swift)
```swift
let url = URL(string: "ws://192.168.1.100:8765/ws")!
try await webSocketService.connect(to: url)
```

### List Workspaces (Swift)
```swift
let workspaces = try await workspaceManager.listWorkspaces()
```

### Start Workspace (Swift)
```swift
try await workspaceManager.startWorkspace("ws-abc123")
```

### Switch to Workspace (Swift)
```swift
// 1. Disconnect from current workspace
currentWorkspaceWS.disconnect()

// 2. Start target workspace if needed
if workspace.status != .running {
    try await manager.startWorkspace(workspace.id)
}

// 3. Connect to target workspace
let url = URL(string: "ws://\(host):\(workspace.port)/ws")!
newWorkspaceWS.connect(to: url)
```

---

## JSON-RPC Method Summary

| Method | Purpose | Required Params |
|--------|---------|-----------------|
| `workspace/list` | Get all workspaces | None |
| `workspace/get` | Get workspace details | `workspace_id` |
| `workspace/start` | Start a workspace | `id` |
| `workspace/stop` | Stop a workspace | `id` |
| `workspace/restart` | Restart a workspace | `id` |
| `workspace/discover` | Find Git repos | `paths` (optional) |

**For Claude operations** (on workspace connection):
- `agent/run` - Start Claude with prompt
- `agent/stop` - Stop Claude
- `status/get` - Get agent status
- `git/status` - Get git status
- `file/get` - Get file content

---

## Testing Endpoints

### Laptop Setup
```bash
# Start workspace manager
./bin/cdev workspace-manager start

# Add a workspace
./bin/cdev workspace add ~/Projects/my-app --name "My App" --auto-start

# Check status
./bin/cdev workspace list
```

### iOS Testing
```swift
// Manager endpoint
let managerURL = "ws://192.168.1.100:8765/ws"

// Workspace endpoint (after getting port from list)
let workspaceURL = "ws://192.168.1.100:8766/ws"
```

---

## Common Issues & Solutions

### Issue: Can't connect from iPhone
**Solution:** Manager binds to localhost by default. Configure to bind to `0.0.0.0` or user's IP.

### Issue: Workspace status shows "starting" for too long
**Solution:** Poll `workspace/get` every 1-2 seconds until status becomes "running".

### Issue: Connection drops when switching WiFi
**Solution:** Implement reconnection logic with exponential backoff.

### Issue: Port 8765 already in use
**Solution:** User may have another workspace manager running. Stop it first.

---

## Support Channels

**For Questions:**
- GitHub Issues: https://github.com/brianly1003/cdev/issues
- Tag: `ios-integration`, `workspace-manager`

**For Bugs:**
- Report in: cdev-ios repository
- Include: Manager version, iOS version, connection URL

**For Feature Requests:**
- Discuss in: cdev repository issues
- Label: `enhancement`, `mobile`

---

## Version Information

**Current Version:** 1.0.0
**Protocol Version:** JSON-RPC 2.0
**Compatibility:** iOS 15+

**Changelog:**
- 2025-12-23: Initial workspace manager release
- JSON-RPC 2.0 as primary protocol
- REST HTTP also available (will be deprecated)

---

## Next Steps

1. **Share this package** with iOS team
2. **Schedule kickoff meeting** to walk through integration guide
3. **Provide test environment** with workspace manager running
4. **Create iOS-specific issues** for tracking integration tasks

**Estimated Integration Time:** 2-3 days for basic functionality

---

**Questions?** Start with `IOS-WORKSPACE-INTEGRATION.md` and refer to this package for additional resources.
