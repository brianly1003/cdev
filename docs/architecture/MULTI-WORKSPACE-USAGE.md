# Multi-Workspace Architecture - User Guide

## Overview

The multi-workspace feature allows you to manage multiple cdev-agent instances from a single workspace manager. Each workspace runs its own cdev-agent server on a unique port, enabling you to work with multiple repositories simultaneously.

## Purpose: IDE & 3rd Party Integration

**cdev-agent is designed as a platform for IDE integration and 3rd party tools.**

### Target Integrations

- **VS Code Extensions** - Control AI coding from your editor
- **Cursor IDE** - Deep integration with Cursor's AI features
- **JetBrains IDEs** - IntelliJ IDEA, PyCharm, WebStorm, etc.
- **Neovim** - LSP-compatible integration
- **Custom Tools** - Build your own tools on top of cdev

### Standard Protocol: JSON-RPC 2.0

**cdev uses JSON-RPC 2.0 over WebSocket** - the industry standard for IDE-tool communication:

✅ **Same protocol as Language Server Protocol (LSP)** - Used by all major IDEs
✅ **LSP-compatible** - Familiar to IDE extension developers
✅ **Bidirectional communication** - Events and requests over single connection
✅ **Well-documented** - Standard JSON-RPC 2.0 specification
✅ **Future-proof** - Industry-wide adoption

**REST HTTP is also available** for simple use cases (debugging with curl, quick scripts), but **will be deprecated in future versions**. All new integrations should use JSON-RPC 2.0.

### Why JSON-RPC 2.0?

1. **VS Code compatibility** - Extensions can speak the same language
2. **MCP alignment** - Anthropic's Model Context Protocol uses JSON-RPC
3. **LSP ecosystem** - Leverage existing LSP tooling and knowledge
4. **IDE vendors expect it** - JetBrains, Microsoft, all use JSON-RPC for tool protocols

## Architecture

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

## Quick Start

### 1. Start the Workspace Manager

```bash
cdev workspace-manager start
```

The manager will start on `http://127.0.0.1:8765` with:
- **WebSocket (JSON-RPC 2.0)**: `ws://127.0.0.1:8765/ws` - **PRIMARY/RECOMMENDED**
- REST API: `http://127.0.0.1:8765/api/workspaces` - Also available (will be deprecated)
- Health check: `http://127.0.0.1:8765/health`

**For IDE Integration:** Use JSON-RPC 2.0 over WebSocket (industry standard, LSP-compatible)

### 2. Add Workspaces

```bash
# Add current directory
cdev workspace add . --name "My Project"

# Add with auto-start
cdev workspace add /path/to/repo --name "Backend" --auto-start

# Add with specific port
cdev workspace add /path/to/repo --name "Frontend" --port 8770
```

### 3. List Workspaces

```bash
cdev workspace list
```

Output:
```
ID          NAME       PATH                  PORT   AUTO-START   STATUS
--          ----       ----                  ----   ----------   ------
ws-a1b2c3   Backend    /Users/you/backend    8766   yes          running
ws-d4e5f6   Frontend   /Users/you/frontend   8770   no           stopped
```

### 4. Start/Stop Workspaces

```bash
# Start a workspace
cdev workspace start ws-a1b2c3

# Stop a workspace
cdev workspace stop ws-a1b2c3

# View workspace details
cdev workspace info ws-a1b2c3
```

### 5. Discover Repositories

```bash
cdev workspace discover
```

This scans `~/Projects`, `~/Code`, and `~/Desktop` for Git repositories and displays them in a table.

## Workspace Management

### Adding Workspaces

```bash
cdev workspace add <path> [flags]
```

**Flags:**
- `--name` - Workspace name (default: directory name)
- `--auto-start` - Auto-start on manager launch
- `--port` - Specific port (default: auto-allocate from 8766-8799)
- `--config` - Workspace-specific config file

**Examples:**
```bash
# Basic add
cdev workspace add /path/to/repo

# With custom name and auto-start
cdev workspace add /path/to/repo --name "My API" --auto-start

# With specific port
cdev workspace add /path/to/repo --port 8780
```

### Removing Workspaces

```bash
cdev workspace remove <id-or-name>
```

This removes the workspace configuration. It does **not** delete any files, only the workspace entry.

### Starting/Stopping Workspaces

```bash
# Start
cdev workspace start <id-or-name>

# Stop
cdev workspace stop <id-or-name>
```

When you start a workspace, the manager:
1. Verifies the port is available
2. Spawns a new `cdev` process: `cdev start --repo <path> --http-port <port>`
3. Monitors the process for crashes
4. Auto-restarts up to 3 times with exponential backoff

### Workspace Discovery

```bash
cdev workspace discover
```

Scans common directories for Git repositories:
- `~/Projects`
- `~/Code`
- `~/Desktop`
- `~/Documents`

Shows repository name, path, remote URL, and configuration status.

## Configuration

### Workspaces Config File

Location: `~/.cdev/workspaces.yaml`

```yaml
manager:
  enabled: true
  port: 8765
  host: 127.0.0.1
  auto_start_workspaces: false
  log_level: info
  max_concurrent_workspaces: 5
  auto_stop_idle_minutes: 30
  port_range_start: 8766
  port_range_end: 8799
  restart_on_crash: true
  max_restart_attempts: 3
  restart_backoff_seconds: 5

workspaces:
  - id: ws-a1b2c3d4
    name: Backend API
    path: /Users/you/projects/backend
    port: 8766
    auto_start: true
    created_at: 2025-12-22T10:00:00Z
    last_accessed: 2025-12-22T15:30:00Z

  - id: ws-e5f6g7h8
    name: Frontend App
    path: /Users/you/projects/frontend
    port: 8767
    auto_start: false
    created_at: 2025-12-22T10:05:00Z
    last_accessed: 2025-12-22T14:00:00Z

defaults:
  watcher:
    enabled: true
    debounce_ms: 100
    ignore_patterns:
      - .git
      - .DS_Store
      - node_modules
      - .vscode
      - .idea
  git:
    enabled: true
    command: git
    diff_on_change: true
  logging:
    level: info
    format: console
  limits:
    max_file_size_kb: 200
    max_diff_size_kb: 500
    max_log_buffer: 1000
    max_prompt_len: 10000
```

### Manager Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `port` | 8765 | Manager HTTP/WebSocket port |
| `host` | 127.0.0.1 | Bind address |
| `auto_start_workspaces` | false | Auto-start workspaces on manager launch |
| `max_concurrent_workspaces` | 5 | Maximum running workspaces |
| `auto_stop_idle_minutes` | 30 | Auto-stop idle workspaces (0 = disabled) |
| `port_range_start` | 8766 | First port for workspaces |
| `port_range_end` | 8799 | Last port for workspaces |
| `restart_on_crash` | true | Auto-restart crashed workspaces |
| `max_restart_attempts` | 3 | Max restart attempts before giving up |
| `restart_backoff_seconds` | 5 | Base delay between restarts (exponential) |

## API Reference

### Protocol Support

The workspace manager supports **two protocols**:

1. **JSON-RPC 2.0 over WebSocket** (PRIMARY/RECOMMENDED)
   - Industry standard for IDE integration
   - LSP-compatible
   - Future-proof
   - Used by: VS Code, Cursor, JetBrains, Neovim

2. **REST HTTP** (ALSO AVAILABLE)
   - Simple HTTP calls
   - Good for debugging with curl
   - Will be deprecated in future versions

**Recommendation:** Use JSON-RPC 2.0 for all new integrations, especially IDE extensions.

### JSON-RPC 2.0 API (PRIMARY)

**Endpoint:** `ws://127.0.0.1:8765/ws`

**Example - List Workspaces:**
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
        "path": "/Users/you/backend",
        "port": 8766,
        "status": "running",
        "auto_start": true
      }
    ]
  }
}
```

**Example - Start Workspace:**
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

### REST API (ALSO AVAILABLE)

**Base URL:** `http://127.0.0.1:8765/api`

**Note:** REST API will be deprecated in future versions. Use JSON-RPC 2.0 for new integrations.

#### List Workspaces
```
GET /workspaces
```

Response:
```json
{
  "workspaces": [
    {
      "id": "ws-a1b2c3d4",
      "name": "Backend API",
      "path": "/Users/you/backend",
      "port": 8766,
      "status": "running",
      "auto_start": true,
      "pid": 12345,
      "restart_count": 0,
      "last_active": "2025-12-22T15:30:00Z"
    }
  ]
}
```

#### Get Workspace
```
GET /workspaces/{id}
```

#### Add Workspace
```
POST /workspaces
Content-Type: application/json

{
  "name": "My Project",
  "path": "/path/to/repo",
  "auto_start": true,
  "port": 0
}
```

#### Start Workspace
```
POST /workspaces/{id}/start
```

#### Stop Workspace
```
POST /workspaces/{id}/stop
```

#### Discover Repositories
```
POST /workspaces/discover
Content-Type: application/json

{
  "paths": ["/custom/search/path"]
}
```

### JSON-RPC 2.0 Methods

All methods follow the JSON-RPC 2.0 specification over WebSocket at `ws://127.0.0.1:8765/ws`.

**Available Methods:**

| Method | Description | Parameters |
|--------|-------------|------------|
| `workspace/list` | List all workspaces | None |
| `workspace/get` | Get workspace details | `{"id": "ws-xxx"}` |
| `workspace/add` | Add new workspace | `{"name": "...", "path": "...", "auto_start": bool, "port": int}` |
| `workspace/remove` | Remove workspace | `{"id": "ws-xxx"}` |
| `workspace/start` | Start workspace server | `{"id": "ws-xxx"}` |
| `workspace/stop` | Stop workspace server | `{"id": "ws-xxx"}` |
| `workspace/restart` | Restart workspace server | `{"id": "ws-xxx"}` |
| `workspace/discover` | Discover Git repositories | `{"paths": ["..."]}` (optional) |

**Go Client Example:**
```go
import "cdev/internal/rpc/client"

// Connect via JSON-RPC
wc, err := client.NewWorkspaceClient("ws://127.0.0.1:8765/ws")
defer wc.Close()

// List workspaces
ctx, cancel := client.WithTimeout(5 * time.Second)
defer cancel()

workspaces, err := wc.List(ctx)
for _, ws := range workspaces {
    fmt.Printf("Workspace: %s (%s)\n", ws.Name, ws.Status)
}

// Start a workspace
info, err := wc.Start(ctx, "ws-a1b2c3d4")
```

**JavaScript/TypeScript Example:**
```typescript
const ws = new WebSocket('ws://127.0.0.1:8765/ws');

// Send JSON-RPC request
ws.send(JSON.stringify({
  jsonrpc: "2.0",
  id: 1,
  method: "workspace/list",
  params: {}
}));

// Handle response
ws.onmessage = (event) => {
  const response = JSON.parse(event.data);
  if (response.id === 1) {
    console.log("Workspaces:", response.result.workspaces);
  }
};
```

## Process Monitoring

The workspace manager includes robust process monitoring:

### Health Checks
- Runs every 30 seconds
- Verifies PIDs are alive
- Detects zombie processes
- Updates workspace status

### Auto-Restart
- Monitors all running workspace processes
- Detects crashes and unexpected exits
- Restarts up to 3 times (configurable)
- Uses exponential backoff: 5s, 10s, 20s
- Gives up after max attempts and marks as error

### Idle Timeout
- Optional feature (disabled by default)
- Checks idle time every minute
- Stops workspaces idle longer than threshold
- Configurable via `auto_stop_idle_minutes`

## Troubleshooting

### Manager Won't Start

**Error:** `address already in use`

**Solution:** Check if port 8765 is in use:
```bash
lsof -i :8765
```

Change the port in `~/.cdev/workspaces.yaml`:
```yaml
manager:
  port: 8770
```

### Workspace Won't Start

**Error:** `max concurrent workspaces (5) reached`

**Solution:** Stop an idle workspace or increase the limit:
```yaml
manager:
  max_concurrent_workspaces: 10
```

**Error:** `port X is in use by another process`

**Solution:** Remove the port assignment and let it auto-allocate:
```bash
# Edit ~/.cdev/workspaces.yaml and set port: 0
# Or remove and re-add the workspace
cdev workspace remove ws-xxxxx
cdev workspace add /path/to/repo --name "My Project"
```

### Workspace Keeps Crashing

Check the workspace logs:
```bash
cat /path/to/repo/.cdev/logs/workspace_ws-xxxxx.log
```

Common issues:
- Invalid repository path
- Missing dependencies
- Port conflicts
- Permission errors

Disable auto-restart if needed:
```yaml
manager:
  restart_on_crash: false
```

### Discovery Finds No Repositories

Discovery scans these directories by default:
- `~/Projects`
- `~/Code`
- `~/Desktop`
- `~/Documents`

If your repositories are elsewhere, add them manually:
```bash
cdev workspace add /custom/path/to/repo
```

## Best Practices

### 1. Use Auto-Start for Main Projects

Mark frequently-used workspaces for auto-start:
```bash
cdev workspace add /path/to/repo --auto-start
```

### 2. Set Idle Timeout for Resource Management

Enable idle timeout to auto-stop unused workspaces:
```yaml
manager:
  auto_stop_idle_minutes: 60  # Stop after 1 hour idle
```

### 3. Monitor Resource Usage

Limit concurrent workspaces based on your system:
```yaml
manager:
  max_concurrent_workspaces: 3  # For 8GB RAM
  max_concurrent_workspaces: 5  # For 16GB RAM
  max_concurrent_workspaces: 10 # For 32GB+ RAM
```

### 4. Use Descriptive Names

```bash
# Good
cdev workspace add . --name "Backend API"
cdev workspace add . --name "React Frontend"

# Less clear
cdev workspace add . --name "Project 1"
```

### 5. Regular Cleanup

Remove unused workspaces:
```bash
cdev workspace list
cdev workspace remove old-project-id
```

## iOS App Integration

The iOS app can connect to the workspace manager to:
- List all available workspaces
- Switch between workspaces
- Start/stop workspace servers
- Monitor workspace status

Connection details:
- **Manager URL:** `http://127.0.0.1:8765` (local) or `http://<your-ip>:8765` (network)
- **WebSocket:** `ws://127.0.0.1:8765/ws`

See the iOS app documentation for setup instructions.

## Migration from Single Workspace

### Backward Compatibility

The original `cdev start` command works unchanged:
```bash
cdev start                    # Still works!
cdev start --http-port 9000   # Still works!
```

### Converting to Multi-Workspace

1. Start the workspace manager:
```bash
cdev workspace-manager start
```

2. Add your current project:
```bash
cd /path/to/project
cdev workspace add . --name "My Project" --auto-start
```

3. Connect iOS app to workspace manager instead of single instance

4. Enjoy managing multiple repositories!

## Limitations

- Maximum 100 workspaces configured
- Maximum 100 concurrent running workspaces (configurable to lower)
- Port range: 8766-8799 (34 ports by default)
- Manager port 8765 must not conflict with workspace range
- Workspace paths cannot be parent/child relationships

## FAQ

**Q: Can I run the workspace manager and single workspace mode together?**

A: Yes! They use different ports. Workspace manager uses 8765, single mode defaults to 8766.

**Q: What happens if a workspace crashes?**

A: The manager auto-restarts it up to 3 times with exponential backoff (5s, 10s, 20s). After max attempts, it marks the workspace as error and stops trying.

**Q: Can I use the same workspace in multiple managers?**

A: No, each workspace path can only be managed by one manager instance.

**Q: How do I backup my workspace configuration?**

A: Copy `~/.cdev/workspaces.yaml` to a safe location.

**Q: Can I edit workspaces.yaml manually?**

A: Yes, but stop the manager first, edit the file, then restart. The CLI commands are recommended for safety.

**Q: Does this work on Windows?**

A: Yes! Process management uses platform-specific commands (taskkill on Windows, SIGTERM on Unix).

## Advanced Usage

### Custom Search Paths for Discovery

Create a script:
```bash
#!/bin/bash
curl -X POST http://127.0.0.1:8765/api/workspaces/discover \
  -H "Content-Type: application/json" \
  -d '{
    "paths": [
      "/custom/path/1",
      "/custom/path/2"
    ]
  }'
```

### Monitoring with Health Endpoint

```bash
# Check manager health
curl http://127.0.0.1:8765/health

# Monitor continuously
watch -n 5 'curl -s http://127.0.0.1:8765/api/workspaces | jq'
```

### Automated Workspace Setup

```bash
#!/bin/bash
# setup-workspaces.sh

cdev workspace add ~/projects/backend --name "Backend" --auto-start
cdev workspace add ~/projects/frontend --name "Frontend" --auto-start
cdev workspace add ~/projects/mobile --name "Mobile" --port 8770
```

## Support

For issues, feature requests, or questions:
- GitHub Issues: https://github.com/brianly1003/cdev/issues
- Documentation: https://github.com/brianly1003/cdev/docs
