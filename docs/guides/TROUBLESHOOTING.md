# Troubleshooting Guide

This document covers common issues and their solutions when running cdev.

---

## Table of Contents

1. [Run Doctor First](#run-doctor-first)
2. [Session Cache Errors](#session-cache-errors)
3. [WebSocket Connection Issues](#websocket-connection-issues)
4. [Claude CLI Issues](#claude-cli-issues)
5. [Performance Issues](#performance-issues)

---

## Run Doctor First

Before debugging manually, run the local diagnostics command:

```bash
cdev doctor
```

Machine-readable output:

```bash
cdev doctor --json
```

Strict mode (non-zero exit on warnings or failures):

```bash
cdev doctor --strict
```

### Doctor Output Contract (v1.0)

`cdev doctor --json` returns:

```json
{
  "version": "1.0",
  "generated_at": "2026-02-17T12:00:00Z",
  "overall_status": "ok|warn|fail",
  "summary": {
    "total": 12,
    "ok": 10,
    "warn": 2,
    "fail": 0
  },
  "checks": [
    {
      "id": "config.load",
      "status": "ok|warn|fail",
      "message": "human-readable summary",
      "details": { "key": "value" },
      "remediation": "action to fix (present for warn/fail)"
    }
  ],
  "config_search_paths": [
    "config.yaml",
    "~/.cdev/config.yaml",
    "/etc/cdev/config.yaml"
  ]
}
```

### Current Check IDs

| Check ID | What it validates | Common remediation |
|----------|-------------------|--------------------|
| `config.load` | Effective config can be loaded and validated | Fix YAML or regenerate with `cdev config init --force` |
| `config.directory` | `~/.cdev` directory exists and is accessible | Run `cdev config init` |
| `auth.registry_file` | `auth_registry.json` exists and is valid JSON | Start cdev / pair device to initialize auth |
| `auth.token_secret` | `token_secret.json` exists and is valid JSON | Start cdev once to generate secret |
| `workspace.registry_file` | `workspaces.yaml` exists and is valid YAML | Start cdev or add workspace from cdev-ios |
| `repository.path` | Configured `repository.path` exists and is a directory | Fix `repository.path` in config |
| `runtime.claude_cli` | Claude CLI executable is available | Install `claude` or update `claude.command` |
| `runtime.codex_cli` | Codex CLI executable is available | Install `codex` |
| `runtime.git` | Git executable is available | Install `git` or update `git.command` |
| `sessions.claude_dir` | Claude sessions directory exists | Run a Claude session to bootstrap |
| `sessions.codex_dir` | Codex sessions directory exists | Run a Codex session to bootstrap |
| `server.health_endpoint` | `http://<host>:<port>/health` is reachable | Start cdev and verify host/port config |

### Exit Code Rules

- `overall_status = ok` -> exit code `0`
- `overall_status = warn` -> exit code `0` (or non-zero with `--strict`)
- `overall_status = fail` -> non-zero exit code

---

## Session Cache Errors

### "bufio.Scanner: token too long" Error

**Symptoms:**
```
DBG failed to parse session error="bufio.Scanner: token too long" file=06d8a491-b850-4f9d-bcd5-927869756344.jsonl
```

**Cause:**
This error occurs when parsing Claude Code session JSONL files where a single JSON line exceeds the buffer limit. This commonly happens with:

- **Extended thinking** (`ultrathink` mode) - Claude's thinking blocks can be very large
- **Large code blocks** - Assistant responses with substantial code
- **Tool results** - Tool outputs like file reads or search results embedded in JSON

**Technical Details:**
Go's `bufio.Scanner` has a default 64KB line buffer. cdev increases this to handle larger lines, but extremely long Claude responses (especially with extended thinking) can exceed even larger limits.

**Files Affected:**
- `internal/adapters/sessioncache/cache.go` - Session listing/indexing
- `internal/adapters/sessioncache/messages.go` - Message pagination
- `internal/adapters/sessioncache/streamer.go` - Real-time streaming

**Solution:**
The buffer limit has been increased to 10MB to accommodate extended thinking responses. If you still encounter this error:

1. **Update to the latest version** - Ensure you have the fix with 10MB buffers
2. **Clear the session cache** (if needed):
   ```bash
   rm -rf /tmp/cdev/cache/
   rm -rf /tmp/cdev/message-cache/
   ```

**Impact:**
This error is logged at DEBUG level and doesn't crash the application. Sessions that fail to parse are simply skipped in the listing. After updating, previously unparseable sessions will be indexed correctly.

---

## WebSocket Connection Issues

### Connection Drops Frequently

**Symptoms:**
- WebSocket disconnects after short periods
- Mobile app loses connection to desktop agent

**Possible Causes:**

1. **Network instability** - WiFi switching, mobile network changes
2. **Firewall/antivirus** - Blocking WebSocket connections
3. **Idle timeout** - Connection idle for too long

**Solutions:**

1. **Enable heartbeat monitoring** - cdev sends heartbeat events every 30 seconds
2. **Check firewall settings** - Ensure port 8766 (or configured port) is allowed
3. **Use the iOS app's auto-reconnect** - The mobile app automatically reconnects

### Unable to Connect

**Symptoms:**
- "Connection refused" errors
- QR code scans but app can't connect

**Solutions:**

1. **Verify cdev is running:**
   ```bash
   curl http://127.0.0.1:8766/health
   ```

2. **Check port availability:**
   ```bash
   lsof -i :8766
   ```

3. **Ensure same network** - Mobile device must be on same network as desktop

4. **Try explicit IP** - Use machine's IP instead of localhost in QR code

---

## Claude CLI Issues

### Claude Process Won't Start

**Symptoms:**
- "failed to start claude" errors
- No response after sending prompt

**Solutions:**

1. **Verify Claude CLI is installed:**
   ```bash
   which claude
   claude --version
   ```

2. **Check Claude authentication:**
   ```bash
   claude --version  # Should show authenticated status
   ```

3. **Check working directory** - Ensure the repository path exists and is accessible

### Permission Prompts Not Working

**Symptoms:**
- Claude hangs waiting for permission
- No `claude_permission` events received

**Solutions:**

1. **Check skip_permissions setting:**
   ```yaml
   claude:
     skip_permissions: false  # Set to true for development
   ```

2. **Verify WebSocket connection** - Permission events are sent via WebSocket

3. **Check logs** - Look for permission-related log entries:
   ```bash
   cdev start -v  # Verbose logging
   ```

### Session Continue/Resume Not Working

**Symptoms:**
- Can't continue previous sessions
- "session not found" errors

**Solutions:**

1. **List available sessions:**
   ```bash
   curl http://127.0.0.1:8766/api/claude/sessions
   ```

2. **Check session directory exists:**
   ```bash
   ls ~/.claude/projects/-Users-*your-project-path*/
   ```

3. **Force cache rebuild:**
   ```bash
   rm -rf /tmp/cdev/cache/
   # Restart cdev
   ```

---

## Performance Issues

### High Memory Usage

**Symptoms:**
- cdev using excessive memory
- System slowdown when running

**Possible Causes:**

1. **Large session files** - Many/large Claude sessions being cached
2. **Many file watchers** - Large repository with many files

**Solutions:**

1. **Limit watched files** - Configure ignore patterns:
   ```yaml
   watcher:
     ignore_patterns:
       - "node_modules/**"
       - ".git/**"
       - "*.log"
   ```

2. **Clean old sessions:**
   ```bash
   # Delete sessions older than 30 days
   find ~/.claude/projects/ -name "*.jsonl" -mtime +30 -delete
   ```

### Slow Session Listing

**Symptoms:**
- `/api/claude/sessions` takes long to respond
- Session list slow to load in mobile app

**Solutions:**

1. **Check SQLite cache:**
   ```bash
   ls -la /tmp/cdev/cache/
   ```

2. **Force cache rebuild:**
   ```bash
   rm /tmp/cdev/cache/*.db
   # Restart cdev
   ```

3. **Use pagination:**
   ```bash
   curl "http://127.0.0.1:8766/api/claude/sessions?limit=20&offset=0"
   ```

---

## Logging and Debugging

### Enable Verbose Logging

```bash
# Via command line
cdev start -v

# Via config
logging:
  level: "debug"

# Via environment
CDEV_LOGGING_LEVEL=debug cdev start
```

### Check Claude Output Logs

Claude output is logged to `.cdev/logs/` in the repository:

```bash
ls .cdev/logs/
cat .cdev/logs/claude_<pid>.jsonl
```

### View Real-time Events

Use wscat to monitor WebSocket events:

```bash
npx wscat -c ws://127.0.0.1:8766/ws
```

---

## Getting Help

If you encounter issues not covered here:

1. **Check existing issues:** [GitHub Issues](https://github.com/brianly1003/cdev/issues)
2. **Enable debug logging** and capture relevant output
3. **Include version info:** `cdev version`
4. **Report new issues** with reproduction steps

---

## Version History

| Date | Change |
|------|--------|
| 2026-02-17 | Added `cdev doctor` usage, output contract (v1.0), check IDs, and exit code behavior |
| 2024-12-23 | Initial troubleshooting guide |
| 2024-12-23 | Added "token too long" fix (10MB buffer) |
