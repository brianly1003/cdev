# Workspace Manager Setup Guide

This guide walks you through setting up the cdev workspace manager for iOS app integration using VS Code Dev Tunnels.

---

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Step 1: Build cdev](#step-1-build-cdev)
4. [Step 2: Configure Workspace Manager](#step-2-configure-workspace-manager)
5. [Step 3: Start Workspace Manager](#step-3-start-workspace-manager)
6. [Step 4: Add Workspaces](#step-4-add-workspaces)
7. [Step 5: Configure VS Code Port Forwarding](#step-5-configure-vs-code-port-forwarding)
8. [Step 6: Connect iOS App](#step-6-connect-ios-app)
9. [Verification](#verification)
10. [Troubleshooting](#troubleshooting)

---

## Overview

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        cdev-ios App                         │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          │ wss://xxx-8765.devtunnels.ms/ws
                          ↓
┌─────────────────────────────────────────────────────────────┐
│                   VS Code Dev Tunnel                        │
│         Port 8765 → Workspace Manager                       │
│         Port 8766 → Workspace Agent                         │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ↓
┌─────────────────────────────────────────────────────────────┐
│                  Workspace Manager (8765)                   │
│   • workspace/list    • workspace/start                     │
│   • workspace/stop    • workspace/discover                  │
└────────────┬────────────────────────────────────────────────┘
             │
             ↓
┌────────────────────┐  ┌────────────────────┐
│  Workspace: cdev   │  │ Workspace: ios-app │
│    (port 8766)     │  │    (port 8767)     │
└────────────────────┘  └────────────────────┘
```

### Ports Summary

| Service | Port | Purpose |
|---------|------|---------|
| Workspace Manager | 8765 | Manage workspaces (list, start, stop) |
| Workspace Agents | 8766-8799 | Claude operations per workspace |

---

## Prerequisites

- **cdev repository** cloned locally
- **Go 1.21+** installed
- **VS Code** with Dev Tunnels enabled
- **Claude CLI** installed and authenticated
- **cdev-ios app** with workspace integration

---

## Step 1: Build cdev

```bash
cd /path/to/cdev

# Build the binary
make build

# Verify build
./bin/cdev version
```

Expected output:
```
cdev version X.X.X (commit: abc1234)
```

---

## Step 2: Configure Workspace Manager

### 2.1 Check Configuration File

The workspace manager configuration is stored at `~/.cdev/workspaces.yaml`.

```bash
# View current config (if exists)
cat ~/.cdev/workspaces.yaml
```

### 2.2 Create/Update Configuration

If the file doesn't exist, create it:

```bash
mkdir -p ~/.cdev
cat > ~/.cdev/workspaces.yaml << 'EOF'
manager:
  enabled: true
  port: 8765
  host: 0.0.0.0          # Important: Use 0.0.0.0 for external access
  auto_start_workspaces: false
  log_level: info
  max_concurrent_workspaces: 5
  auto_stop_idle_minutes: 30
  port_range_start: 8766
  port_range_end: 8799
  restart_on_crash: true
  max_restart_attempts: 3
  restart_backoff_seconds: 5

workspaces: []

defaults:
  watcher:
    enabled: true
    debouncems: 100
    ignorepatterns:
      - .git
      - .DS_Store
      - node_modules
      - .vscode
      - .idea
  git:
    enabled: true
    command: git
    diffonchange: true
  logging:
    level: info
    format: console
  limits:
    maxfilesizekb: 200
    maxdiffsizekb: 500
    maxlogbuffer: 1000
    maxpromptlen: 10000
EOF
```

### 2.3 Key Configuration Options

| Setting | Value | Description |
|---------|-------|-------------|
| `host` | `0.0.0.0` | **Required** for VS Code tunnels (accepts external connections) |
| `port` | `8765` | Workspace manager port |
| `port_range_start` | `8766` | First port for workspace agents |
| `max_concurrent_workspaces` | `5` | Limit running workspaces |

---

## Step 3: Start Workspace Manager

### 3.1 Start the Manager

```bash
./bin/cdev workspace-manager start
```

Expected output:
```
Starting workspace manager...
✓ Workspace manager running on 0.0.0.0:8765
  - HTTP API: http://0.0.0.0:8765/api/workspaces
  - WebSocket: ws://0.0.0.0:8765/ws
  - Health: http://0.0.0.0:8765/health

Configured workspaces: 0

Press Ctrl+C to stop...
```

### 3.2 Verify Manager is Running

```bash
# Check port is listening
lsof -i :8765

# Test health endpoint
curl http://127.0.0.1:8765/health
```

### 3.3 Run in Background (Optional)

```bash
# Start in background
nohup ./bin/cdev workspace-manager start > ~/.cdev/manager.log 2>&1 &

# View logs
tail -f ~/.cdev/manager.log
```

---

## Step 4: Add Workspaces

### 4.1 Add a Workspace via CLI

```bash
# Add current directory
./bin/cdev workspace add . --name "My Project"

# Add specific path with auto-start
./bin/cdev workspace add /path/to/repo --name "Backend API" --auto-start

# Add with specific port
./bin/cdev workspace add /path/to/repo --name "Frontend" --port 8767
```

### 4.2 List Workspaces

```bash
./bin/cdev workspace list
```

Output:
```
ID          NAME           PATH                      PORT   STATUS
ws-abc123   My Project     /Users/you/project        8766   stopped
ws-def456   Backend API    /Users/you/backend        8767   stopped
```

### 4.3 Discover Repositories

Find Git repositories on your machine:

```bash
./bin/cdev workspace discover
```

This scans common development directories like `~/Projects`, `~/Code`, `~/Developer`, `~/Desktop`, etc.

**Note:** `~/Documents` is excluded by default for performance reasons (it typically contains many non-code files). See [REPOSITORY-DISCOVERY.md](../architecture/REPOSITORY-DISCOVERY.md) for the full list of default paths and skip directories.

### 4.4 Start a Workspace

```bash
# Start by ID
./bin/cdev workspace start ws-abc123

# Or start by name
./bin/cdev workspace start "My Project"
```

---

## Step 5: Configure VS Code Port Forwarding

### 5.1 Open Ports Panel

In VS Code:
1. Press `Cmd+Shift+P` (Mac) or `Ctrl+Shift+P` (Windows/Linux)
2. Type "Ports: Focus on Ports View"
3. Press Enter

Or: View → Ports

### 5.2 Forward Port 8765 (Workspace Manager)

1. Click **"Forward a Port"** button
2. Enter `8765`
3. Press Enter
4. Right-click the forwarded port
5. Set **Port Visibility** → **Public**

You'll get a URL like:
```
https://abc123x4-8765.asse.devtunnels.ms
```

### 5.3 Forward Port 8766 (First Workspace)

Repeat the above for port `8766`:
1. Forward port `8766`
2. Set visibility to **Public**

URL example:
```
https://abc123x4-8766.asse.devtunnels.ms
```

### 5.4 Forward Additional Workspace Ports (If Needed)

If you have multiple workspaces, forward their ports too:
- Port `8767` for second workspace
- Port `8768` for third workspace
- etc.

### 5.5 Note Your Tunnel URLs

Record your tunnel URLs:

| Service | Local Port | Tunnel URL |
|---------|------------|------------|
| Manager | 8765 | `https://abc123x4-8765.asse.devtunnels.ms` |
| Workspace 1 | 8766 | `https://abc123x4-8766.asse.devtunnels.ms` |
| Workspace 2 | 8767 | `https://abc123x4-8767.asse.devtunnels.ms` |

---

## Step 6: Connect iOS App

### 6.1 Configure Manager URL in iOS

In cdev-ios app, set the workspace manager URL:

```
Manager URL: https://abc123x4-8765.asse.devtunnels.ms
WebSocket: wss://abc123x4-8765.asse.devtunnels.ms/ws
```

### 6.2 iOS Connection Flow

1. **Connect to Manager** (port 8765)
   ```json
   // Connect to wss://xxx-8765.devtunnels.ms/ws
   // List workspaces
   {"jsonrpc":"2.0","id":1,"method":"workspace/list","params":{}}
   ```

2. **Start Workspace** (if not running)
   ```json
   {"jsonrpc":"2.0","id":2,"method":"workspace/start","params":{"id":"ws-abc123"}}
   ```

3. **Connect to Workspace** (port 8766+)
   ```json
   // Connect to wss://xxx-8766.devtunnels.ms/ws
   // Use Claude operations
   {"jsonrpc":"2.0","id":1,"method":"agent/run","params":{"prompt":"Hello"}}
   ```

### 6.3 Available Manager Methods

| Method | Description | Parameters |
|--------|-------------|------------|
| `workspace/list` | List all workspaces | None |
| `workspace/get` | Get workspace details | `{"workspace_id": "ws-xxx"}` |
| `workspace/start` | Start a workspace | `{"id": "ws-xxx"}` |
| `workspace/stop` | Stop a workspace | `{"id": "ws-xxx"}` |
| `workspace/discover` | Find Git repositories | `{"paths": [...]}` (optional) |

### 6.4 Available Workspace Methods

Once connected to a workspace (port 8766+):

| Method | Description |
|--------|-------------|
| `agent/run` | Start Claude with prompt |
| `agent/stop` | Stop Claude |
| `agent/respond` | Respond to permission/question |
| `session/list` | List Claude sessions |
| `session/messages` | Get session messages |
| `git/status` | Get git status |
| `git/diff` | Get git diff |
| `status/get` | Get agent status |

---

## Verification

### Test 1: Manager Health Check

```bash
curl https://abc123x4-8765.asse.devtunnels.ms/health
```

Expected: `{"status":"ok"}`

### Test 2: List Workspaces via REST

```bash
curl https://abc123x4-8765.asse.devtunnels.ms/api/workspaces
```

### Test 3: List Workspaces via WebSocket

Using wscat:
```bash
npx wscat -c wss://abc123x4-8765.asse.devtunnels.ms/ws
```

Then send:
```json
{"jsonrpc":"2.0","id":1,"method":"workspace/list","params":{}}
```

### Test 4: Start Workspace and Connect

```bash
# Start workspace
curl -X POST https://abc123x4-8765.asse.devtunnels.ms/api/workspaces/ws-abc123/start

# Connect to workspace
npx wscat -c wss://abc123x4-8766.asse.devtunnels.ms/ws
```

---

## Troubleshooting

### Manager Won't Start

**Error:** `address already in use`

```bash
# Check what's using port 8765
lsof -i :8765

# Kill the process
kill -9 <PID>

# Or change port in ~/.cdev/workspaces.yaml
```

### VS Code Tunnel Not Working

1. Ensure you're signed into VS Code with GitHub/Microsoft account
2. Check tunnel is set to **Public** visibility
3. Verify the tunnel URL in browser first

### iOS Can't Connect

1. **Check tunnel URL is correct** - Copy from VS Code Ports panel
2. **Use `wss://` not `ws://`** - Dev tunnels use HTTPS
3. **Check workspace is running** - Call `workspace/list` first
4. **Verify port is forwarded** - Both 8765 AND 8766 needed

### Workspace Won't Start

```bash
# Check logs
cat ~/.cdev/manager.log

# Check workspace config
cat ~/.cdev/workspaces.yaml

# Verify path exists
ls -la /path/to/workspace
```

### Connection Drops

- Dev tunnels may timeout after inactivity
- Implement reconnection logic in iOS app
- Check VS Code is still running and connected

---

## Quick Reference

### Commands

```bash
# Start manager
./bin/cdev workspace-manager start

# Add workspace
./bin/cdev workspace add /path --name "Name" --auto-start

# List workspaces
./bin/cdev workspace list

# Start/stop workspace
./bin/cdev workspace start <id>
./bin/cdev workspace stop <id>

# Discover repos
./bin/cdev workspace discover
```

### URLs (Replace with your tunnel base URL)

```
Manager WebSocket: wss://YOUR-TUNNEL-8765.devtunnels.ms/ws
Manager REST API:  https://YOUR-TUNNEL-8765.devtunnels.ms/api/workspaces
Workspace WebSocket: wss://YOUR-TUNNEL-8766.devtunnels.ms/ws
```

### JSON-RPC Quick Examples

```json
// List workspaces
{"jsonrpc":"2.0","id":1,"method":"workspace/list","params":{}}

// Start workspace
{"jsonrpc":"2.0","id":2,"method":"workspace/start","params":{"id":"ws-xxx"}}

// Run Claude (on workspace connection)
{"jsonrpc":"2.0","id":3,"method":"agent/run","params":{"prompt":"Hello Claude"}}
```

---

## Next Steps

- See [IOS-WORKSPACE-INTEGRATION.md](../mobile/IOS-WORKSPACE-INTEGRATION.md) for iOS implementation details
- See [MULTI-WORKSPACE-USAGE.md](../architecture/MULTI-WORKSPACE-USAGE.md) for full API reference
- See [TROUBLESHOOTING.md](./TROUBLESHOOTING.md) for common issues

---

## Version History

| Date | Change |
|------|--------|
| 2025-12-23 | Initial setup guide for VS Code Dev Tunnels |
