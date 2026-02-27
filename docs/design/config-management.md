# Config Management Design

## Overview

cdev follows the **zero-config default** pattern used by popular CLI tools like `git`, `docker`, and `kubectl`. The tool works immediately after installation with sensible defaults, and users only create configuration files when they need to customize behavior.

## Design Principles

1. **Zero-config by default** - `cdev start` works immediately after `brew install cdev`
2. **Explicit over implicit** - Users must explicitly run `cdev config init` to create a config file
3. **Layered configuration** - Defaults → Config file → Environment variables → CLI flags
4. **No magic** - Clear, predictable behavior at every step

## Configuration Hierarchy

```
Priority (highest to lowest):
┌─────────────────────────────────────┐
│ 1. CLI flags (--config, --port)     │  Highest priority
├─────────────────────────────────────┤
│ 2. Environment variables (CDEV_*)   │
├─────────────────────────────────────┤
│ 3. Config file                      │
│    - ./config.yaml (project-local)  │
│    - ~/.cdev/config.yaml (user)     │
│    - /etc/cdev/config.yaml (system) │
├─────────────────────────────────────┤
│ 4. Compiled defaults                │  Lowest priority
└─────────────────────────────────────┘
```

## User Journey

```
┌──────────────────────────────────────────────────────────────────────┐
│                         INSTALLATION                                  │
│                                                                       │
│   $ brew tap brianly1003/tap                                         │
│   $ brew install cdev                                                │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌──────────────────────────────────────────────────────────────────────┐
│                         FIRST RUN                                     │
│                                                                       │
│   $ cdev start                                                       │
│   🚀 cdev started on http://127.0.0.1:16180                           │
│      Config: using defaults (run 'cdev config init' to customize)    │
│                                                                       │
│   ✓ Works immediately with sensible defaults                         │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘
                                   │
                                   ▼
┌──────────────────────────────────────────────────────────────────────┐
│                    CUSTOMIZATION (OPTIONAL)                           │
│                                                                       │
│   Option A: Initialize config file                                   │
│   ────────────────────────────────                                   │
│   $ cdev config init                                                 │
│   Created ~/.cdev/config.yaml                                        │
│   Edit this file to customize cdev behavior.                         │
│                                                                       │
│   Option B: Quick config changes                                     │
│   ──────────────────────────────                                     │
│   $ cdev config set server.port 9000                                 │
│   $ cdev config set logging.level debug                              │
│                                                                       │
│   Option C: Environment variables                                    │
│   ───────────────────────────────                                    │
│   $ export CDEV_SERVER_PORT=9000                                     │
│   $ cdev start                                                       │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘
```

## CLI Commands

### `cdev config`

Display current effective configuration (merged from all sources).

```bash
$ cdev config
server:
  port: 16180
  host: 127.0.0.1
logging:
  level: info
...
```

### `cdev config init`

Create a config file with defaults and documentation comments.

```bash
# Create user config (default)
$ cdev config init
Created ~/.cdev/config.yaml

# Create project-local config
$ cdev config init --local
Created ./config.yaml

# Force overwrite existing
$ cdev config init --force
Overwrote ~/.cdev/config.yaml
```

**Behavior:**
- Creates `~/.cdev/config.yaml` by default
- With `--local`, creates `./config.yaml` in current directory
- Fails if file exists (unless `--force`)
- Includes all defaults with documentation comments

### `cdev config path`

Show where config would be loaded from.

```bash
$ cdev config path
Config file: ~/.cdev/config.yaml (exists)
Config dir:  ~/.cdev/

$ cdev config path
Config file: not found (using defaults)
Config dir:  ~/.cdev/
```

### `cdev config set <key> <value>`

Set a configuration value. Creates config file if it doesn't exist.

```bash
$ cdev config set server.port 9000
Set server.port = 9000 in ~/.cdev/config.yaml

$ cdev config set logging.level debug
Set logging.level = debug in ~/.cdev/config.yaml
```

**Supported keys:**

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `server.port` | int | 16180 | Unified port for HTTP API and WebSocket |
| `server.host` | string | 127.0.0.1 | Bind address |
| `server.external_url` | string | - | Optional public URL for tunnels/port forwarding (e.g., https://tunnel.devtunnels.ms) |
| `logging.level` | string | info | Log level: debug, info, warn, error |
| `logging.format` | string | console | Log format: console, json |
| `claude.command` | string | claude | Path to claude CLI |
| `claude.timeout_minutes` | int | 30 | Timeout for Claude operations |
| `claude.skip_permissions` | bool | false | Skip permission prompts |
| `git.enabled` | bool | true | Enable git integration |
| `git.command` | string | git | Path to git CLI |
| `git.diff_on_change` | bool | true | Auto-generate diff on file change |
| `watcher.enabled` | bool | true | Enable file watcher |
| `watcher.debounce_ms` | int | 100 | Debounce time in milliseconds |
| `indexer.enabled` | bool | true | Enable repository indexer |

> **Note:** `repository.path` is deprecated. Workspaces are now managed via the `workspace/add` API.

### `cdev config get <key>`

Get a configuration value.

```bash
$ cdev config get server.port
16180

$ cdev config get logging.level
info
```

## Config File Format

The config file uses YAML format with documentation comments:

```yaml
# ~/.cdev/config.yaml
# cdev configuration file
# Documentation: https://github.com/brianly1003/cdev

# Server settings
server:
  # Port for HTTP API and WebSocket connections
  port: 16180

  # Bind address (use 0.0.0.0 to allow external connections)
  host: "127.0.0.1"

# Logging settings
logging:
  # Log level: debug, info, warn, error
  level: "info"

  # Log format: console (human-readable) or json
  format: "console"

# Claude CLI settings
claude:
  # Path to claude CLI executable
  command: "claude"

  # Set to true to skip permission prompts (--dangerously-skip-permissions)
  skip_permissions: false

  # Timeout for Claude operations in minutes
  timeout_minutes: 30

# Git integration
git:
  # Enable git status and diff tracking
  enabled: true

  # Auto-generate diff when files change
  diff_on_change: true

# File watcher
watcher:
  # Enable file system watching
  enabled: true

  # Debounce rapid changes (milliseconds)
  debounce_ms: 100

# Repository indexer (for fast file search)
indexer:
  # Enable repository indexing
  enabled: true
```

## Environment Variables

All config values can be overridden via environment variables with `CDEV_` prefix:

| Config Key | Environment Variable |
|------------|---------------------|
| `server.port` | `CDEV_SERVER_PORT` |
| `server.host` | `CDEV_SERVER_HOST` |
| `logging.level` | `CDEV_LOGGING_LEVEL` |
| `claude.command` | `CDEV_CLAUDE_COMMAND` |
| `claude.skip_permissions` | `CDEV_CLAUDE_SKIP_PERMISSIONS` |

## Implementation Status

### Implemented Commands
- [x] `cdev config` - Show current config
- [x] `cdev config init` - Create config file
- [x] `cdev config init --local` - Create config in current directory
- [x] `cdev config init --force` - Overwrite existing config
- [x] `cdev config path` - Show config file location
- [x] `cdev config get <key>` - Get single value
- [x] `cdev config set <key> <value>` - Set single value

### Future Enhancements
- [ ] `cdev config edit` - Open config in $EDITOR
- [ ] `cdev config reset` - Reset to defaults
- [ ] `cdev config validate` - Validate config file

## Comparison with Other Tools

| Feature | git | docker | kubectl | cdev |
|---------|-----|--------|---------|------|
| Zero-config default | ✅ | ✅ | ✅ | ✅ |
| Init command | ❌ | ❌ | ❌ | ✅ |
| Config command | ✅ | ❌ | ✅ | ✅ |
| Config file optional | ✅ | ✅ | ✅ | ✅ |
| Env var override | ✅ | ✅ | ✅ | ✅ |
| Per-project config | ✅ | ✅ | ✅ | ✅ |

## File Locations

| Platform | User Config | System Config |
|----------|-------------|---------------|
| macOS | `~/.cdev/config.yaml` | `/etc/cdev/config.yaml` |
| Linux | `~/.cdev/config.yaml` | `/etc/cdev/config.yaml` |
| Windows | `%USERPROFILE%\.cdev\config.yaml` | - |

## References

- [12-Factor App Config](https://12factor.net/config)
- [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html)
- [Viper Configuration Library](https://github.com/spf13/viper)
