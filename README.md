# cdev

Mobile AI Coding Monitor & Controller Agent - the laptop/desktop component of the cdev system.

## Overview

`cdev` is a lightweight Go daemon that enables remote monitoring and control of Claude Code CLI sessions from mobile devices. It serves as the bridge between your development machine and the cdev mobile app.

## Features

- **Claude CLI Management**: Spawn, monitor, and control Claude Code CLI processes
- **Real-time Streaming**: Stream stdout/stderr output in real-time via WebSocket
- **File Watching**: Monitor repository for file changes with debouncing
- **Git Integration**: Generate and stream git diffs automatically
- **Cross-Platform**: Runs on macOS, Windows, and Linux
- **QR Code Pairing**: Easy mobile device pairing via QR code scan

## Quick Start

```bash
# Build
make build

# Run in your project directory
./bin/cdev start

# Or specify a repository path
./bin/cdev start --repo /path/to/your/project
```

## Installation

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

## Usage

### Start the Agent

```bash
# Start with default settings (current directory)
cdev start

# Start with specific repository
cdev start --repo /path/to/project

# Start with custom ports
cdev start --ws-port 8765 --http-port 8766

# Start with verbose logging
cdev start -v
```

### Configuration

Create a `config.yaml` file (see `configs/config.example.yaml`):

```yaml
server:
  websocket_port: 8765
  http_port: 8766
  host: "127.0.0.1"

repository:
  path: "/path/to/your/project"

watcher:
  enabled: true
  debounce_ms: 100

logging:
  level: "info"
  format: "console"
```

Configuration is loaded from:
1. `--config` flag (if provided)
2. `./config.yaml`
3. `~/.cdev/config.yaml`
4. `/etc/cdev/config.yaml`

Environment variables override config file values (prefix: `CDEV_`):
```bash
export CDEV_SERVER_WEBSOCKET_PORT=9000
```

## API

### API Documentation (Swagger UI)

When the agent is running, access the interactive API documentation at:
```
http://localhost:8766/swagger/
```

The OpenAPI 3.0 spec is also available at:
- JSON: `http://localhost:8766/swagger/doc.json`
- YAML: `docs/swagger.yaml` (in repository)

### WebSocket API

Connect to `ws://localhost:8765` for real-time events.

**Events received:**
- `session_start` - When connected
- `claude_log` - Claude CLI output lines
- `claude_status` - Claude state changes
- `claude_waiting` - Claude is waiting for user input (AskUserQuestion)
- `claude_permission` - Claude is requesting permission for a tool (Write, Edit, Bash, etc.)
- `file_changed` - File modifications
- `git_diff` - Git diff content

**Commands to send:**
```json
// Start new conversation (default)
{"command": "run_claude", "payload": {"prompt": "Your prompt here"}}

// Continue a specific session by ID
{"command": "run_claude", "payload": {"prompt": "Continue with...", "mode": "continue", "session_id": "550e8400-..."}}

// Other commands
{"command": "stop_claude"}
{"command": "respond_to_claude", "payload": {"tool_use_id": "...", "response": "user answer"}}
{"command": "get_status"}
{"command": "get_file", "payload": {"path": "src/main.ts"}}
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
curl -X POST http://127.0.0.1:8766/api/claude/run \
  -H 'Content-Type: application/json' \
  -d '{"prompt": "Create a hello world function"}'
```

**Example - Continue a Session by ID:**
```bash
curl -X POST http://127.0.0.1:8766/api/claude/run \
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
curl http://127.0.0.1:8766/api/claude/sessions
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
curl http://127.0.0.1:8766/api/status
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
- WebSocket: `{"command": "respond_to_claude", "payload": {"tool_use_id": "...", "response": "approved"}}`
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
├── cmd/cdev/          # CLI entry point
├── internal/
│   ├── app/                 # Application orchestration
│   ├── config/              # Configuration management
│   ├── domain/              # Domain types and interfaces
│   │   ├── events/          # Event definitions
│   │   ├── commands/        # Command definitions
│   │   └── ports/           # Interface definitions
│   ├── adapters/            # External system adapters
│   │   ├── claude/          # Claude CLI adapter
│   │   ├── watcher/         # File system watcher
│   │   └── git/             # Git tracker
│   ├── hub/                 # Event hub
│   └── server/              # WebSocket & HTTP servers
├── pkg/protocol/            # Public protocol types
├── configs/                 # Configuration examples
└── test/                    # Integration tests
```

## Implementation Status

| Component | Status | Description |
|-----------|--------|-------------|
| CLI (Cobra) | ✅ Done | Commands: start, version, config |
| Config (Viper) | ✅ Done | YAML + env vars + defaults |
| Event Types | ✅ Done | All events from spec |
| Event Hub | ✅ Done | Central dispatcher with fan-out |
| HTTP Server | ✅ Done | `/health`, `/api/status`, `/api/claude/*`, `/api/git/*`, `/api/file` |
| Claude Manager | ✅ Done | Process spawning, bidirectional streaming, interactive prompt handling |
| Session Continuity | ✅ Done | Continue conversations with `new`/`continue` modes |
| File Watcher | ✅ Done | fsnotify integration with debouncing |
| Git Tracker | ✅ Done | Git CLI wrapper for status/diff |
| WebSocket Server | ✅ Done | Real-time event streaming with Hub pattern |
| QR Code Generator | ✅ Done | Terminal QR code display for mobile pairing |

## Documentation

| Document | Description |
|----------|-------------|
| [docs/architecture/ARCHITECTURE.md](./docs/architecture/ARCHITECTURE.md) | Detailed architecture and technical specification |
| [docs/api/API-REFERENCE.md](./docs/api/API-REFERENCE.md) | Complete API documentation for mobile integration |
| [docs/architecture/DESIGN-SPEC.md](./docs/architecture/DESIGN-SPEC.md) | Original design specification with implementation status |
| [docs/security/TECHNICAL-REVIEW.md](./docs/security/TECHNICAL-REVIEW.md) | Security & performance analysis with roadmap |
| [docs/planning/BACKLOG.md](./docs/planning/BACKLOG.md) | Product backlog with prioritized work items |
| [docs/security/SECURITY.md](./docs/security/SECURITY.md) | Security guidelines and best practices |

## Security Notice

**Important:** This is currently a POC with known security limitations:
- No authentication (any client can connect)
- CORS allows all origins
- Binds to localhost only (intentional security measure)

See [docs/security/SECURITY.md](./docs/security/SECURITY.md) for details and [docs/planning/BACKLOG.md](./docs/planning/BACKLOG.md) for planned fixes.

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please read the contributing guidelines before submitting a PR.
