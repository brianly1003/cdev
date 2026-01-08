# Permission Hook Bridge Design

## Overview

This document describes how cdev can detect and respond to permission prompts from **existing Claude Code instances** (not spawned by cdev) using Claude Code's built-in hook system.

## Problem Statement

When Claude Code runs independently on a laptop/PC:
- cdev cannot capture PTY output directly
- JSONL session files don't record "waiting for permission" state
- Mobile app (cdev-ios) cannot detect or respond to permission prompts

## Solution: Hook-Based Permission Bridge

Claude Code has a built-in hook system that fires when permissions are requested. cdev can leverage this to bridge external Claude instances with the mobile app.

### Architecture

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────┐
│  Claude Code    │────▶│  cdev CLI Hook   │────▶│    cdev     │
│  (existing)     │     │  (bridge script) │     │   server    │
└─────────────────┘     └──────────────────┘     └─────────────┘
                                                        │
                               ┌────────────────────────┘
                               ▼
                        ┌─────────────┐
                        │  cdev-ios   │
                        │ (mobile app)│
                        └─────────────┘
```

### Flow

1. Claude Code shows permission prompt
2. Claude Code calls the configured hook (`cdev hook permission-request`)
3. Hook sends permission details to cdev server
4. cdev emits `pty_permission` event to mobile app
5. User approves/denies on mobile
6. cdev returns response to hook
7. Hook returns decision to Claude Code

## User Configuration

### Step 1: Configure Claude Code Hooks

Add to `~/.claude/settings.json` or `~/.claude/settings.local.json`:

```json
{
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "cdev hook permission-request",
            "timeout": 300
          }
        ]
      }
    ]
  }
}
```

### Configuration Options

| Field | Description |
|-------|-------------|
| `matcher` | Tool pattern to match (`*` for all, or specific like `Bash`, `Write\|Edit`) |
| `command` | cdev CLI command to execute |
| `timeout` | Max wait time in seconds (default: 60, recommended: 300 for mobile response) |

## Hook Input/Output Formats

### Input (from Claude Code to hook via stdin)

```json
{
  "session_id": "abc123",
  "transcript_path": "/path/to/transcript.jsonl",
  "cwd": "/Users/brianly/Projects/test001",
  "permission_mode": "default",
  "hook_event_name": "PermissionRequest",
  "tool_name": "Bash",
  "tool_input": {
    "command": "rm /Users/brianly/Projects/test001/add.py",
    "description": "Remove add.py file"
  },
  "tool_use_id": "toolu_01ABC123..."
}
```

### Output (from hook to Claude Code via stdout)

#### Allow

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PermissionRequest",
    "decision": {
      "behavior": "allow"
    }
  }
}
```

#### Allow with Modified Input

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PermissionRequest",
    "decision": {
      "behavior": "allow",
      "updatedInput": {
        "command": "rm -i /Users/brianly/Projects/test001/add.py"
      }
    }
  }
}
```

#### Deny

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PermissionRequest",
    "decision": {
      "behavior": "deny",
      "message": "User denied from mobile app"
    }
  }
}
```

#### Deny and Interrupt

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PermissionRequest",
    "decision": {
      "behavior": "deny",
      "message": "Operation cancelled by user",
      "interrupt": true
    }
  }
}
```

## Session Memory (Workaround for "Allow for Session")

### Background

Claude Code's hook API only supports:
- `allow` - Allow this ONE request
- `deny` - Deny this ONE request

It does **NOT** support:
- `allow_session` - Allow and don't ask again this session
- This is a [known feature request (GitHub #3389)](https://github.com/anthropics/claude-code/issues/3389)

### cdev Workaround

cdev implements its own session memory to provide "Allow for Session" functionality:

```
┌─────────────────┐     ┌──────────────────────────────────────┐
│  Claude Code    │────▶│         cdev hook bridge             │
│                 │     │                                      │
│  Permission     │     │  ┌─────────────────────────────────┐ │
│  Request #1     │     │  │  Session Memory (in-memory map) │ │
│  Bash(rm foo)   │     │  │                                 │ │
└─────────────────┘     │  │  "Bash(rm:*)" → allow           │ │
                        │  │  "Write(*.py)" → allow          │ │
        │               │  └─────────────────────────────────┘ │
        │               │                                      │
        ▼               │  1. Check session memory             │
┌─────────────────┐     │  2. If pattern exists → auto-allow   │
│  Claude Code    │────▶│  3. If not → ask mobile app          │
│                 │     │  4. Store decision in memory         │
│  Permission     │     │                                      │
│  Request #2     │     └──────────────────────────────────────┘
│  Bash(rm bar)   │              │
└─────────────────┘              ▼
        │               ┌─────────────┐
        │               │  cdev-ios   │
   Auto-approved!       └─────────────┘
   (same pattern)
```

### Mobile App Options

| Option | Hook Response | cdev Action |
|--------|---------------|-------------|
| **Allow Once** | `{"behavior": "allow"}` | No memory storage |
| **Allow for Session** | `{"behavior": "allow"}` | Store pattern in cdev's session memory |
| **Allow for Path** | `{"behavior": "allow"}` | Store in memory + modify settings.json |
| **Deny Once** | `{"behavior": "deny"}` | No memory storage |
| **Deny for Session** | `{"behavior": "deny"}` | Store deny pattern in memory |

### Pattern Matching Examples

| Tool Use | Generated Pattern |
|----------|-------------------|
| `Bash(rm /path/to/file.txt)` | `Bash(rm:*)` |
| `Write(/path/to/file.py)` | `Write(*.py)` or `Write(/path/to/*)` |
| `Edit(/path/to/config.json)` | `Edit(*.json)` |

## Implementation Plan

### New CLI Command

```bash
cdev hook permission-request
```

This command:
1. Reads JSON from stdin (permission details from Claude)
2. Checks session memory for matching patterns
3. If no match, connects to cdev server and waits for mobile response
4. Returns JSON decision to stdout

### New RPC Methods

#### `permission/request`

Called by hook to request permission decision from mobile app.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "permission/request",
  "params": {
    "session_id": "abc123",
    "tool_name": "Bash",
    "tool_input": {
      "command": "rm file.txt"
    },
    "tool_use_id": "toolu_01ABC123"
  },
  "id": 1
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "result": {
    "decision": "allow",
    "scope": "session",
    "pattern": "Bash(rm:*)"
  },
  "id": 1
}
```

#### `permission/respond`

Called by mobile app to respond to a permission request.

**Request:**
```json
{
  "jsonrpc": "2.0",
  "method": "permission/respond",
  "params": {
    "tool_use_id": "toolu_01ABC123",
    "decision": "allow",
    "scope": "session"
  },
  "id": 1
}
```

### Events

#### `pty_permission` (existing, enhanced)

Emitted to mobile app when permission is requested.

```json
{
  "type": "pty_permission",
  "payload": {
    "tool_use_id": "toolu_01ABC123",
    "type": "bash_command",
    "target": "rm file.txt",
    "description": "Remove file.txt",
    "preview": "",
    "options": [
      {"key": "allow_once", "label": "Allow Once"},
      {"key": "allow_session", "label": "Allow for Session"},
      {"key": "deny", "label": "Deny"}
    ],
    "session_id": "abc123"
  }
}
```

## Comparison of Approaches

| Approach | Works with Existing Claude? | Reliability | Setup Complexity |
|----------|----------------------------|-------------|------------------|
| **Hooks (this design)** | Yes (after config) | High | Medium |
| PTY Mode | No (cdev spawns) | High | None |
| JSONL Timing Heuristic | Yes | Low | Low |
| MCP `--permission-prompt-tool` | No (needs flag) | High | High |

## Session Memory Lifecycle

### When Memory is Created

- Created when first permission request is received for a Claude session
- Keyed by Claude's `session_id` from hook input

### When Memory is Released

| Trigger | Behavior |
|---------|----------|
| **cdev server restart** | All session memory cleared |
| **Claude session ends** | Memory for that session cleared (detected via hook or JSONL) |
| **Idle timeout** | Memory cleared after configurable period (default: 1 hour) |
| **User explicit clear** | Via `cdev session clear-permissions <session_id>` |
| **Workspace closed** | Memory for all sessions in workspace cleared |

### Implementation

```go
type SessionMemory struct {
    mu          sync.RWMutex
    patterns    map[string]Decision   // pattern → decision
    lastAccess  time.Time             // for idle timeout
    sessionID   string                // Claude session ID
}

type PermissionMemoryManager struct {
    mu       sync.RWMutex
    sessions map[string]*SessionMemory  // sessionID → memory
    ttl      time.Duration              // idle timeout (default: 1 hour)
}

// Cleanup goroutine runs periodically
func (m *PermissionMemoryManager) cleanup() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        m.mu.Lock()
        for sessionID, mem := range m.sessions {
            if time.Since(mem.lastAccess) > m.ttl {
                delete(m.sessions, sessionID)
                log.Info().Str("session_id", sessionID).Msg("session memory expired")
            }
        }
        m.mu.Unlock()
    }
}
```

### Configuration

In `config.yaml`:

```yaml
permissions:
  session_memory:
    enabled: true
    ttl: 3600          # seconds (1 hour)
    max_patterns: 100  # max patterns per session
```

## Limitations

1. **Requires User Configuration**: User must add hook config to Claude Code settings
2. **Timeout**: Default 60s may be too short; recommend 300s for mobile response
3. **No Hot-Reload**: Changes to settings.json require Claude restart
4. **Session Memory Scope**: cdev's session memory is per-cdev-server instance

## Security Considerations

1. **Hook Execution**: Hooks run with user permissions; validate all inputs
2. **Settings Modification**: When modifying settings.json, use proper file locking
3. **Session Memory**: Clear on cdev restart; don't persist sensitive patterns

## References

- [Claude Code Hooks Reference](https://code.claude.com/docs/en/hooks)
- [GitHub Issue #3389: PreToolUse approve for session](https://github.com/anthropics/claude-code/issues/3389)
- [GitHub Issue #1175: --permission-prompt-tool documentation](https://github.com/anthropics/claude-code/issues/1175)
- [Permission Model in Claude Code](https://skywork.ai/blog/permission-model-claude-code-vs-code-jetbrains-cli/)
