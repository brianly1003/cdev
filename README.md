<!-- Favicon -->
<link rel="icon" type="image/png" href="assets/icon.png">

<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/logo-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="assets/logo.png">
    <img alt="Cdev Logo" src="assets/logo.png" width="140" height="140" />
  </picture>

  <h1>cdev+</h1>

  **Mobile AI Coding Monitor & Controller Agent**

  The laptop/desktop component of the cdev+ system

  [![Go Version](https://img.shields.io/github/go-mod/go-version/brianly1003/cdev?style=flat-square)](https://golang.org/)
  [![License](https://img.shields.io/badge/license-MIT-blue.svg?style=flat-square)](LICENSE)
  [![GitHub Issues](https://img.shields.io/github/issues/brianly1003/cdev?style=flat-square)](https://github.com/brianly1003/cdev/issues)

  **Looking for the mobile app?** &nbsp; [cdev-ios](https://github.com/brianly1003/cdev-ios) — the native iOS companion app for monitoring and controlling AI coding sessions from your iPhone or iPad.
</div>

---

## Overview

`cdev+` is a lightweight Go daemon that enables remote monitoring and control of AI coding agents (Claude Code, Codex, etc.) from mobile devices. It serves as the bridge between your development machine and the [cdev-ios](https://github.com/brianly1003/cdev-ios) mobile app.

## Features

- **AI Agent Management**: Spawn, monitor, and control AI coding agents (Claude Code, Codex)
- **LIVE Session Support**: Detect Claude running in your terminal and send messages from mobile via keystroke injection
- **Workspace Management (Default)**: Manage multiple repositories simultaneously with the built-in workspace manager
- **Multi-Runtime Support**: Claude, Codex via `agent_type` routing with runtime-specific dispatch
- **Interactive PTY Mode**: Full terminal experience with parsed permission prompts and state tracking
- **Real-time Streaming**: Stream stdout/stderr output in real-time via WebSocket
- **Session Management**: Multi-session orchestration with managed, live, and historical session types
- **File Watching**: Monitor repository for file changes with debouncing
- **Git Integration**: Generate and stream git diffs automatically
- **Process Monitoring**: Auto-restart crashed workspaces with exponential backoff
- **Workspace Discovery**: Scan directories for Git repositories
- **Cross-Platform**: Runs on macOS, Windows, and Linux
- **QR Code Pairing**: Easy mobile device pairing via QR code scan

## AI Agent Transformation Plan

cdev+ is evolving from a monitor/controller into a goal-driven AI Agent runtime platform.
The implementation plan is documented here:

- [AI Agent Runtime Roadmap](docs/planning/AI-AGENT-RUNTIME-ROADMAP.md)
- Future workspace direction: multi-workspace is the default model, and legacy single-workspace assumptions are being phased out.
- Future runtime direction: Gemini CLI support is planned and will be added in a later phase.

The roadmap is phased and security-first so current users can keep running existing workflows during the transition.

## Quick Start

### Start the Daemon

```bash
# Build
make build

# Run the daemon
./bin/cdev start
```

### Manage Workspaces (Default Behavior)

**cdev+ is designed as a platform for IDE integration** - enabling VS Code extensions, Cursor, JetBrains, and other tools to control AI coding agents.

**Standard Protocol: JSON-RPC 2.0** - Industry standard used by LSP, MCP, and all major IDEs.

Workspaces are managed via JSON-RPC 2.0 over WebSocket (`ws://127.0.0.1:16180/ws`):

```json
// Add a workspace
{"jsonrpc": "2.0", "id": 1, "method": "workspace/add", "params": {"path": "/path/to/backend", "name": "Backend API"}}

// List workspaces
{"jsonrpc": "2.0", "id": 2, "method": "workspace/list"}

// Start/stop a workspace
{"jsonrpc": "2.0", "id": 3, "method": "workspace/start", "params": {"id": "ws-abc123"}}
{"jsonrpc": "2.0", "id": 4, "method": "workspace/stop", "params": {"id": "ws-abc123"}}
```

**Server runs on:** `http://127.0.0.1:16180`
- **WebSocket JSON-RPC 2.0**: `/ws` - **PRIMARY (recommended for IDE integration)**
- REST API: `/api/*` - Also available
- Health check: `/health`

**See full guide:** [Workspace Manager Design](docs/architecture/MULTI-WORKSPACE-DESIGN.md)

## Installation

### Homebrew (Recommended)

```bash
brew tap brianly1003/tap
brew install cdev
```

### From Source

```bash
# Clone the repository
git clone https://github.com/brianly1003/cdev.git
cd cdev/cdev

# Build
make build

# Install to /usr/local/bin (optional)
make install
```

### Pre-built Binaries

Download from the [releases page](https://github.com/brianly1003/cdev/releases).

Available platforms:
- macOS (Apple Silicon): `cdev-darwin-arm64`
- macOS (Intel): `cdev-darwin-amd64`
- Windows: `cdev-windows-amd64.exe`
- Linux: `cdev-linux-amd64`

### Platform Notes

cdev runs on **macOS**, **Linux**, and **Windows**. Most features work identically across platforms, with a few differences:

| Feature | macOS / Linux | Windows |
|---------|--------------|---------|
| Config directory | `~/.cdev/` | `%USERPROFILE%\.cdev\` |
| Hook scripts | Bash (`.sh`) | PowerShell (`.ps1`) |
| LIVE session detection | Full (macOS), partial (Linux) | Not yet supported |
| Process management | SIGTERM/SIGKILL | `taskkill /T` |
| Shell commands | `bash -c` | `cmd.exe /C` |

**Windows quick start:**

```powershell
# Download the pre-built binary (or build from source with `go build`)
# Place cdev.exe in a directory on your PATH

# Start the daemon
cdev.exe start

# Config file location
# %USERPROFILE%\.cdev\config.yaml
```

**Windows environment overrides** use the same `CDEV_` prefix:
```powershell
$env:CDEV_SERVER_PORT = "16180"
$env:CDEV_SECURITY_REQUIRE_AUTH = "true"
```

## Usage

### Start the Agent

```bash
# Start with default settings
cdev start

# Start with custom port
cdev start --port 16180

# Start with verbose logging
cdev start -v
```

### Configuration

cdev works with sensible defaults - no configuration file required. Workspaces are managed dynamically via the `workspace/add` API from cdev-ios.

For advanced settings, create `~/.cdev/config.yaml`:

```yaml
server:
  port: 16180           # Single unified port (HTTP + WebSocket)
  host: "127.0.0.1"

logging:
  level: "info"
  format: "console"

claude:
  command: "claude"
  skip_permissions: false
  timeout_minutes: 30
```

Configuration is loaded from (in order):
1. `--config` flag (if provided)
2. `./config.yaml` (current directory)
3. `~/.cdev/config.yaml` (macOS/Linux) or `%USERPROFILE%\.cdev\config.yaml` (Windows)
4. `/etc/cdev/config.yaml` (macOS/Linux only)

Environment variables override config file values (prefix: `CDEV_`):
```bash
# macOS / Linux
export CDEV_SERVER_PORT=16180

# Windows (PowerShell)
$env:CDEV_SERVER_PORT = "16180"
```

### Workspace Discovery

When the cdev-ios app scans for repositories, the agent searches common directories under `$HOME`:

> `~/Projects`, `~/Code`, `~/Developer`, `~/dev`, `~/Repos`, `~/src`, `~/go/src`, `~/workspace`, `~/Desktop`

**If your repositories live elsewhere**, add custom search paths in `~/.cdev/config.yaml`:

```yaml
# macOS / Linux
discovery:
  search_paths:
    - ~/work
    - ~/company-name
    - /opt/repos
```

```yaml
# Windows (use forward slashes or escaped backslashes)
discovery:
  search_paths:
    - C:/Users/you/work
    - D:/repos
    - ~/company-name   # ~ expands to %USERPROFILE%
```

```yaml
# Shared settings
discovery:
  max_depth: 4           # How deep to recurse (default: 4)
  timeout_seconds: 10    # Max scan time (default: 10s)
  cache_ttl_minutes: 60  # Cache validity (default: 60 min)
```

Custom paths are scanned **first**, then the built-in defaults. Results are cached for 1 hour.

You can also pass paths per-request via the `workspace/discover` JSON-RPC method:
```json
{"jsonrpc": "2.0", "id": 1, "method": "workspace/discover", "params": {"paths": ["/opt/repos"]}}
```

### Diagnostics

```bash
# Run local setup/runtime diagnostics
cdev doctor

# JSON output for tooling
cdev doctor --json

# Fail on warnings (CI-friendly)
cdev doctor --strict
```

### VS Code Port Forwarding

When using VS Code Dev Tunnels for remote access, simply pass the forwarded URL:

```bash
cdev start --external-url "https://your-tunnel.devtunnels.ms"
```

This auto-derives both HTTP and WebSocket URLs for QR code pairing.

## API

### API Documentation (Swagger UI)

When the agent is running, access the interactive API documentation at:
```
http://localhost:16180/swagger/
```

The OpenAPI 3.0 spec is also available at:
- JSON: `http://localhost:16180/swagger/doc.json`
- YAML: `docs/swagger.yaml` (in repository)

### WebSocket API

Connect to `ws://localhost:16180/ws` for real-time events and commands.

**Protocol Support:**
- **JSON-RPC 2.0** - Standard protocol with request/response correlation

**Events received:**
- `session_start` - When connected
- `claude_log` - Claude CLI output lines
- `claude_status` - Claude state changes
- `claude_waiting` - Claude is waiting for user input (AskUserQuestion)
- `claude_permission` - Claude is requesting permission for a tool (Write, Edit, Bash, etc.)
- `file_changed` - File modifications
- `git_diff` - Git diff content
- `heartbeat` - Server health check (every 30s)

**JSON-RPC 2.0 Commands (Recommended):**
```json
// Start new conversation
{"jsonrpc": "2.0", "id": 1, "method": "agent/run", "params": {"prompt": "Your prompt here"}}

// Continue a specific session
{"jsonrpc": "2.0", "id": 2, "method": "agent/run", "params": {"prompt": "Continue with...", "mode": "continue", "session_id": "550e8400-..."}}

// Stop agent
{"jsonrpc": "2.0", "id": 3, "method": "agent/stop"}

// Get status
{"jsonrpc": "2.0", "id": 4, "method": "status/get"}

// Git operations
{"jsonrpc": "2.0", "id": 5, "method": "git/status"}
{"jsonrpc": "2.0", "id": 6, "method": "git/stage", "params": {"paths": ["src/main.ts"]}}
```

### HTTP API

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/health` | GET | Health check |
| `/api/status` | GET | Current agent status |
| `/api/claude/sessions` | GET | List available sessions for resume |
| `/api/claude/run` | POST | Start Claude with prompt |
| `/api/claude/stop` | POST | Stop Claude |
| `/api/claude/respond` | POST | Send response to Claude's interactive prompt |
| `/api/file?path=...` | GET | Get file content |
| `/api/git/status` | GET | Get git status |
| `/api/git/diff?path=...` | GET | Get git diff (all or specific file) |

**Example - Start New Claude Conversation:**
```bash
curl -X POST http://127.0.0.1:16180/api/claude/run \
  -H 'Content-Type: application/json' \
  -d '{"prompt": "Create a hello world function"}'
```

**Example - Continue a Session by ID:**
```bash
curl -X POST http://127.0.0.1:16180/api/claude/run \
  -H 'Content-Type: application/json' \
  -d '{"prompt": "Continue the task", "mode": "continue", "session_id": "550e8400-e29b-41d4-a716-446655440000"}'
```

**Session Modes:**
| Mode | Description |
|------|-------------|
| `new` | Start a new conversation (default) |
| `continue` | Continue a specific session by ID (requires `session_id`) |

**Example - List Available Sessions:**
```bash
curl http://127.0.0.1:16180/api/claude/sessions
```

Response:
```json
{
  "current": "",
  "sessions": [
    {
      "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
      "summary": "Create a hello world function",
      "message_count": 4,
      "last_updated": "2025-12-17T21:14:45Z",
      "branch": "main"
    }
  ]
}
```

**Example - Get Status:**
```bash
curl http://127.0.0.1:16180/api/status
```

## Claude CLI Integration

The agent runs Claude Code CLI with bidirectional streaming using the following flags:
- `-p` (print mode) - Non-interactive output
- `--verbose` - Enable verbose output
- `--output-format stream-json` - JSON streaming for output parsing
- `--input-format stream-json` - JSON streaming for input (interactive responses)

**Permission Handling:**
By default, cdev requires permission approval for tool use. When Claude wants to use a tool like `Write`, `Edit`, `Bash`, etc., it broadcasts a `claude_permission` event with:
- `tool_use_id` - The ID to use when responding
- `tool_name` - The tool requesting permission (e.g., "Write", "Bash")
- `input` - The tool parameters (file path, command, etc.)
- `description` - Human-readable description (e.g., "Write to file: src/main.ts")

**Responding to Permissions:**
Mobile clients can approve or deny via:
- WebSocket JSON-RPC: `{"jsonrpc":"2.0","id":7,"method":"agent/respond","params":{"tool_use_id":"...","response":"approved"}}`
- HTTP: `POST /api/claude/respond` with JSON body
- To deny: set `"is_error": true` and `"response": "Permission denied by user"`

**Skip Permissions Mode (POC/Development):**
For development, you can skip permission prompts:
```yaml
claude:
  skip_permissions: true
```
Or via environment variable: `CDEV_CLAUDE_SKIP_PERMISSIONS=true`

**Interactive Questions:**
When Claude uses `AskUserQuestion`, a `claude_waiting` event is broadcast. Respond with the user's answer.

**Claude Output Logging:**
Claude output is automatically logged to `.cdev/logs/claude_<pid>.jsonl` in the repository directory for debugging.

**Important Notes:**
- Claude runs in the repository's working directory
- Skip permissions mode bypasses all permission checks - use only for development

## Development

```bash
# Install dependencies
make tidy

# Run tests
make test

# Run with race detection
make test-race

# Format code
make fmt

# Run linter
make lint

# Generate OpenAPI docs
make swagger

# Build for all platforms
make build-all
```

## Project Structure

```
cdev/
├── cmd/cdev/                # CLI entry point
├── internal/
│   ├── app/                 # Application orchestration
│   ├── config/              # Configuration management
│   ├── domain/              # Domain types and interfaces
│   │   ├── events/          # Event definitions
│   │   ├── commands/        # Command definitions
│   │   └── ports/           # Interface definitions
│   ├── adapters/            # External system adapters
│   │   ├── claude/          # Claude CLI adapter (process mgmt, PTY, streaming)
│   │   ├── codex/           # Codex CLI adapter
│   │   ├── git/             # Git tracker
│   │   ├── jsonl/           # JSONL file reading
│   │   ├── live/            # LIVE session detection & keystroke injection
│   │   ├── repository/      # Repository indexing
│   │   ├── sessioncache/    # Session message cache (SQLite)
│   │   └── watcher/         # File system watcher
│   ├── hooks/               # Claude Code hook handling
│   ├── hub/                 # Event hub
│   ├── pairing/             # QR code pairing
│   ├── permission/          # Permission management
│   ├── rpc/                 # JSON-RPC 2.0 layer
│   │   ├── transport/       # WebSocket & stdio transports
│   │   ├── message/         # JSON-RPC message types
│   │   └── handler/         # Method registry & dispatcher
│   │       └── methods/     # RPC method implementations
│   ├── security/            # Auth tokens, cdev_access_token gate, HMAC cookies
│   ├── server/
│   │   ├── common/          # Shared server utilities
│   │   ├── http/            # HTTP API endpoints
│   │   ├── unified/         # Unified server (HTTP + WebSocket on single port)
│   │   ├── websocket/       # WebSocket implementation
│   │   └── workspacehttp/   # Workspace-specific HTTP
│   ├── services/            # Optional services (image storage, etc.)
│   ├── session/             # Session manager (multi-session orchestration)
│   ├── sync/                # Sync utilities
│   ├── terminal/            # Terminal runner
│   ├── testutil/            # Testing utilities
│   └── workspace/           # Workspace management
├── configs/                 # Configuration examples
└── test/                    # Integration tests
```

## Implementation Status

| Component | Status | Description |
|-----------|--------|-------------|
| CLI (Cobra) | ✅ Done | Commands: start, version, config, pair, auth, hook, doctor |
| Config (Viper) | ✅ Done | YAML + env vars + defaults |
| Event Types | ✅ Done | All events from spec |
| Event Hub | ✅ Done | Central dispatcher with fan-out |
| HTTP Server | ✅ Done | `/health`, `/api/status`, `/api/claude/*`, `/api/git/*`, `/api/file` |
| Claude Manager | ✅ Done | Process spawning, bidirectional streaming, interactive prompt handling |
| Session Continuity | ✅ Done | Continue conversations with `new`/`continue` modes |
| File Watcher | ✅ Done | fsnotify integration with debouncing |
| Git Tracker | ✅ Done | Git CLI wrapper for status/diff/stage/commit/push/pull |
| WebSocket Server | ✅ Done | Real-time event streaming with Hub pattern |
| QR Code Generator | ✅ Done | Terminal QR code display for mobile pairing |
| **JSON-RPC 2.0** | ✅ Done | Unified protocol with agent-agnostic methods |
| **Unified Server** | ✅ Done | Single port (16180) serving HTTP + WebSocket |
| **OpenRPC Discovery** | ✅ Done | Auto-generated API spec at `/api/rpc/discover` |
| **Session Manager** | ✅ Done | Multi-session orchestration across workspaces |
| **LIVE Sessions** | ✅ Done | Detect and inject into Claude running in user's terminal |
| **Multi-Runtime** | ✅ Done | Claude, Codex support via `agent_type` routing |
| **PTY Mode** | ✅ Done | Interactive terminal mode with permission parsing |

## Documentation

| Document | Description |
|----------|-------------|
| [docs/api/PROTOCOL.md](./docs/api/PROTOCOL.md) | Protocol specification (JSON-RPC 2.0) |
| [docs/api/UNIFIED-API-SPEC.md](./docs/api/UNIFIED-API-SPEC.md) | JSON-RPC 2.0 API specification with examples |
| [docs/api/API-REFERENCE.md](./docs/api/API-REFERENCE.md) | Complete HTTP/WebSocket API for mobile integration |
| [docs/architecture/ARCHITECTURE.md](./docs/architecture/ARCHITECTURE.md) | Detailed architecture and technical specification |
| [docs/architecture/DESIGN-SPEC.md](./docs/architecture/DESIGN-SPEC.md) | Original design specification with implementation status |
| [docs/mobile/LIVE-SESSION-INTEGRATION.md](./docs/mobile/LIVE-SESSION-INTEGRATION.md) | LIVE session detection, injection, and limitations |
| [docs/mobile/INTERACTIVE-PTY-MODE.md](./docs/mobile/INTERACTIVE-PTY-MODE.md) | Interactive PTY mode specification |
| [docs/planning/BACKLOG.md](./docs/planning/BACKLOG.md) | Product backlog with prioritized work items |
| [docs/security/SECURITY.md](./docs/security/SECURITY.md) | Security guidelines and best practices |

### Related Projects

| Project | Description |
|---------|-------------|
| [cdev-ios](https://github.com/brianly1003/cdev-ios) | Native iOS app for monitoring and controlling AI coding sessions from iPhone/iPad |

## Security Notice

**Important:** Security measures implemented:
- Bearer token authentication for HTTP and WebSocket connections
- CORS restrictions with origin validation
- Binds to localhost only by default (intentional security measure)
- LIVE session injection requires same-user process ownership
- `security.cdev_access_token` (or `CDEV_ACCESS_TOKEN`) gates access to protected web routes (`/pair`, `/api/pair/*`, `/api/auth/pairing/*`)
- Cookie tokens stored as HMAC hashes (raw token never persisted in cookie)
- `Referrer-Policy: no-referrer` on pairing responses to prevent token leakage
- Query-string tokens rejected; Authorization header required

See [docs/security/SECURITY.md](./docs/security/SECURITY.md) for details.

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a PR.
