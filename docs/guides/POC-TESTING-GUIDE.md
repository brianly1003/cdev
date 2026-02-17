# cdev POC Testing Guide

This guide explains how to test the cdev POC before developing cdev-mobile (iOS app).

---

## Table of Contents

1. [Testing Tools](#1-testing-tools)
2. [Quick Start Testing](#2-quick-start-testing)
3. [WebSocket Testing](#3-websocket-testing)
4. [HTTP API Testing](#4-http-api-testing)
5. [End-to-End Test Scenarios](#5-end-to-end-test-scenarios)
6. [Simple Web Test Client](#6-simple-web-test-client)
7. [Automated Test Scripts](#7-automated-test-scripts)
8. [Troubleshooting](#8-troubleshooting)

---

## 1. Testing Tools

### Recommended Tools

| Tool | Purpose | Install |
|------|---------|---------|
| **websocat** | WebSocket CLI client | `brew install websocat` (macOS) |
| **curl** | HTTP API testing | Pre-installed on macOS/Linux |
| **jq** | JSON parsing | `brew install jq` |
| **wscat** | Alternative WebSocket client | `npm install -g wscat` |
| **Postman** | GUI API testing | [Download](https://postman.com) |
| **Insomnia** | GUI API testing | [Download](https://insomnia.rest) |

### Install Testing Tools (macOS)

```bash
# Install Homebrew tools
brew install websocat jq

# Alternative: wscat via npm
npm install -g wscat
```

### Install Testing Tools (Windows)

```powershell
# Using Chocolatey
choco install websocat jq

# Alternative: wscat via npm
npm install -g wscat

# Or use Windows Subsystem for Linux (WSL)
```

---

## 2. Quick Start Testing

### Step 1: Start the Agent

```bash
# Build the agent first
cd cdev
make build

# Start with current directory as repo
./bin/cdev start

# Or specify repo path
./bin/cdev start --repo /path/to/your/project

# With verbose logging
./bin/cdev start --repo /path/to/your/project -v
```

Expected output:
```
6:30PM INF starting cdev mode=terminal port=8766 repo=/path/to/your/project version=e313dc8
6:30PM INF git repository detected name=project root=/path/to/your/project
6:30PM INF session started repo_name=project repo_path=/path/to/your/project session_id=abc123...

╔════════════════════════════════════════════════════════════╗
║                     cdev ready                             ║
╠════════════════════════════════════════════════════════════╣
║  Session ID: abc123...                                     ║
║  Repository: project                                       ║
╠════════════════════════════════════════════════════════════╣
║  API:        http://127.0.0.1:8766                         ║
║  WebSocket:  ws://127.0.0.1:8766/ws                        ║
╠════════════════════════════════════════════════════════════╣
║  Scan QR code with cdev mobile app to connect              ║
╚════════════════════════════════════════════════════════════╝

  [QR CODE DISPLAYED HERE]

6:30PM INF Unified server background tasks started
6:30PM INF HTTP server starting addr=127.0.0.1:8766
6:30PM INF file watcher started debounce_ms=100 path=/path/to/your/project
```

### Step 2: Verify Agent is Running

```bash
# Check health endpoint
curl -s http://127.0.0.1:8766/health | jq .

# Expected response
{
  "status": "ok",
  "time": "2025-12-17T11:30:00Z"
}

# Check full status
curl -s http://127.0.0.1:8766/api/status | jq .

# Expected response
{
  "session_id": "abc123-...",
  "version": "e313dc8",
  "repo_path": "/path/to/your/project",
  "repo_name": "project",
  "uptime_seconds": 60,
  "claude_state": "idle",
  "connected_clients": 0,
  "watcher_enabled": true,
  "git_enabled": true,
  "is_git_repo": true
}
```

### Step 3: Connect WebSocket Client

```bash
# Using websocat
websocat ws://localhost:8765

# Using wscat
wscat -c ws://localhost:8765
```

You should see a `session_start` event:
```json
{"event":"session_start","timestamp":"2024-01-15T10:30:00.000Z","payload":{"session_id":"abc123","repo_path":"/path/to/project","repo_name":"project","agent_version":"1.0.0"}}
```

---

## 3. WebSocket Testing

### Connecting with websocat

```bash
# Basic connection
websocat ws://localhost:8765

# With JSON formatting (pipe to jq)
websocat ws://localhost:8765 | jq .
```

### Send Commands

After connecting, type commands directly:

**Run Claude:**
```json
{"command":"run_claude","payload":{"prompt":"List all TypeScript files in the src directory"}}
```

**Stop Claude:**
```json
{"command":"stop_claude"}
```

**Get Status:**
```json
{"command":"get_status"}
```

**Get File Content:**
```json
{"command":"get_file","payload":{"path":"src/index.ts"}}
```

### Expected Event Stream

When Claude is running, you'll see events like:
```json
{"event":"claude_status","timestamp":"...","payload":{"state":"running","prompt":"List all TypeScript files..."}}
{"event":"claude_log","timestamp":"...","payload":{"line":"Analyzing the codebase...","stream":"stdout"}}
{"event":"claude_log","timestamp":"...","payload":{"line":"Found 15 TypeScript files","stream":"stdout"}}
{"event":"file_changed","timestamp":"...","payload":{"path":"src/auth.ts","change":"modified"}}
{"event":"git_diff","timestamp":"...","payload":{"file":"src/auth.ts","diff":"--- a/src/auth.ts\n+++ b/src/auth.ts\n..."}}
{"event":"claude_status","timestamp":"...","payload":{"state":"idle"}}
```

### Using wscat with Commands

```bash
# Connect
wscat -c ws://localhost:8765

# Then type commands
Connected (press CTRL+C to quit)
> {"command":"get_status"}
< {"event":"status_response","payload":{"claude_state":"idle","connected_clients":1}}
```

---

## 4. HTTP API Testing

### Health Check

```bash
curl -s http://localhost:8766/health | jq .
```

### Get Status

```bash
curl -s http://localhost:8766/api/status | jq .
```

Response:
```json
{
  "claude_state": "idle",
  "connected_clients": 1,
  "repo_path": "/path/to/project",
  "repo_name": "project",
  "uptime_seconds": 120,
  "agent_version": "1.0.0"
}
```

### Run Claude

```bash
curl -X POST http://localhost:8766/api/claude/run \
  -H "Content-Type: application/json" \
  -d '{"prompt": "List all files in the src directory"}' | jq .
```

Response:
```json
{
  "status": "started",
  "prompt": "List all files in the src directory"
}
```

### Stop Claude

```bash
curl -X POST http://localhost:8766/api/claude/stop | jq .
```

### Get File Content

```bash
# URL encode the path
curl -s "http://localhost:8766/api/file?path=src/index.ts" | jq .
```

Response:
```json
{
  "path": "src/index.ts",
  "content": "import { App } from './app';\n...",
  "encoding": "utf-8",
  "truncated": false,
  "size": 1024
}
```

### Get Git Diff

```bash
curl -s "http://localhost:8766/api/git/diff?path=src/auth.ts" | jq .
```

---

## 5. End-to-End Test Scenarios

### Scenario 1: Basic Connection Test

**Goal**: Verify agent starts and accepts WebSocket connections.

```bash
# Terminal 1: Start agent
./cdev start --repo /path/to/project

# Terminal 2: Connect WebSocket
websocat ws://localhost:8765

# Expected: Receive session_start event
# Pass if: Connected and received valid JSON event
```

### Scenario 2: Claude Execution Test

**Goal**: Verify Claude CLI can be started and outputs are streamed.

```bash
# Terminal 1: Start agent (already running)

# Terminal 2: WebSocket client (already connected)
# Send command:
{"command":"run_claude","payload":{"prompt":"Print hello world"}}

# Expected:
# 1. claude_status event with state="running"
# 2. Multiple claude_log events with output
# 3. claude_status event with state="idle"

# Pass if: All events received in order
```

### Scenario 3: File Change Detection Test

**Goal**: Verify file changes are detected and reported.

```bash
# Terminal 1: Agent running
# Terminal 2: WebSocket connected

# Terminal 3: Modify a file
echo "// test comment" >> /path/to/project/src/test.ts

# Terminal 2: Should receive:
# 1. file_changed event for src/test.ts
# 2. git_diff event with the diff

# Pass if: Both events received within 1 second
```

### Scenario 4: Git Diff Test

**Goal**: Verify git diffs are generated correctly.

```bash
# Setup: Make a change to a tracked file
echo "const x = 1;" >> /path/to/project/src/index.ts

# Request diff via HTTP
curl -s "http://localhost:8766/api/git/diff?path=src/index.ts" | jq .

# Expected: Response contains unified diff format
# Pass if: diff field contains +const x = 1;
```

### Scenario 5: Path Security Test

**Goal**: Verify path traversal attacks are blocked.

```bash
# Try to access file outside repo
curl -s "http://localhost:8766/api/file?path=../../../etc/passwd" | jq .

# Expected: Error response
{
  "error": "path is outside repository",
  "code": "PATH_OUTSIDE_REPO"
}

# Pass if: Request is rejected with error
```

### Scenario 6: Claude Stop Test

**Goal**: Verify Claude can be stopped gracefully.

```bash
# Terminal 2: Start a long-running Claude task
{"command":"run_claude","payload":{"prompt":"Analyze the entire codebase in detail"}}

# Wait for running status, then send:
{"command":"stop_claude"}

# Expected:
# 1. claude_status with state="stopped"

# Pass if: Process terminates gracefully
```

### Scenario 7: Multiple Client Test

**Goal**: Verify multiple WebSocket clients receive events.

```bash
# Terminal 2: First WebSocket client
websocat ws://localhost:8765

# Terminal 3: Second WebSocket client
websocat ws://localhost:8765

# Terminal 4: Trigger a file change
touch /path/to/project/test.txt

# Expected: Both terminals 2 and 3 receive file_changed event

# Pass if: All connected clients receive the same events
```

### Scenario 8: Reconnection Test

**Goal**: Verify client can reconnect after disconnect.

```bash
# Terminal 2: Connect
websocat ws://localhost:8765
# Ctrl+C to disconnect

# Wait 2 seconds, reconnect
websocat ws://localhost:8765

# Expected: Receive new session_start event
# Pass if: Connection succeeds and session starts
```

---

## 6. Simple Web Test Client

Create a simple HTML file to test the WebSocket connection visually.

### test-client.html

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>cdev Test Client</title>
    <style>
        * { box-sizing: border-box; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; }
        body { margin: 0; padding: 20px; background: #1a1a2e; color: #eee; }
        h1 { margin: 0 0 20px 0; color: #00d9ff; }
        .container { max-width: 1200px; margin: 0 auto; }
        .panels { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
        .panel { background: #16213e; border-radius: 8px; padding: 15px; }
        .panel h2 { margin: 0 0 15px 0; font-size: 14px; color: #888; text-transform: uppercase; }
        .status { padding: 10px; border-radius: 4px; margin-bottom: 15px; }
        .status.connected { background: #0a3622; color: #4ade80; }
        .status.disconnected { background: #3b1219; color: #f87171; }
        #events { height: 400px; overflow-y: auto; background: #0f0f23; padding: 10px; border-radius: 4px; font-family: monospace; font-size: 12px; }
        .event { padding: 5px; margin: 2px 0; border-radius: 3px; }
        .event.claude_log { background: #1e3a5f; }
        .event.claude_status { background: #3d1f5c; }
        .event.file_changed { background: #1f4d3d; }
        .event.git_diff { background: #4d3d1f; }
        .event.session_start { background: #1f4d4d; }
        .event.error { background: #5c1f1f; }
        input, textarea, button { width: 100%; padding: 10px; margin: 5px 0; border: none; border-radius: 4px; }
        input, textarea { background: #0f0f23; color: #eee; }
        textarea { height: 80px; resize: vertical; font-family: monospace; }
        button { background: #00d9ff; color: #000; cursor: pointer; font-weight: bold; }
        button:hover { background: #00b8d9; }
        button:disabled { background: #444; cursor: not-allowed; }
        .btn-danger { background: #f87171; }
        .btn-danger:hover { background: #ef4444; }
        .quick-commands { display: flex; gap: 10px; flex-wrap: wrap; margin-top: 10px; }
        .quick-commands button { width: auto; padding: 8px 15px; font-size: 12px; }
        pre { background: #0f0f23; padding: 10px; border-radius: 4px; overflow-x: auto; font-size: 11px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>cdev Test Client</h1>

        <div class="panels">
            <div class="panel">
                <h2>Connection</h2>
                <div id="connectionStatus" class="status disconnected">Disconnected</div>

                <input type="text" id="wsUrl" value="ws://localhost:8765" placeholder="WebSocket URL">
                <button id="connectBtn" onclick="connect()">Connect</button>
                <button id="disconnectBtn" onclick="disconnect()" disabled class="btn-danger">Disconnect</button>

                <h2 style="margin-top: 20px;">Send Command</h2>
                <textarea id="commandInput" placeholder='{"command":"get_status"}'></textarea>
                <button onclick="sendCommand()" id="sendBtn" disabled>Send Command</button>

                <div class="quick-commands">
                    <button onclick="sendQuickCommand('get_status')">Get Status</button>
                    <button onclick="sendQuickCommand('stop_claude')" class="btn-danger">Stop Claude</button>
                </div>

                <h2 style="margin-top: 20px;">Run Claude</h2>
                <textarea id="promptInput" placeholder="Enter prompt for Claude..."></textarea>
                <button onclick="runClaude()" id="runClaudeBtn" disabled>Run Claude</button>
            </div>

            <div class="panel">
                <h2>Events <button onclick="clearEvents()" style="width:auto;padding:5px 10px;font-size:11px;float:right;">Clear</button></h2>
                <div id="events"></div>
            </div>
        </div>

        <div class="panel" style="margin-top: 20px;">
            <h2>Latest Response</h2>
            <pre id="lastResponse">No response yet</pre>
        </div>
    </div>

    <script>
        let ws = null;

        function connect() {
            const url = document.getElementById('wsUrl').value;
            ws = new WebSocket(url);

            ws.onopen = () => {
                updateStatus(true);
                addEvent('Connected to ' + url, 'session_start');
            };

            ws.onclose = () => {
                updateStatus(false);
                addEvent('Disconnected', 'error');
            };

            ws.onerror = (error) => {
                addEvent('WebSocket error: ' + error.message, 'error');
            };

            ws.onmessage = (event) => {
                try {
                    const data = JSON.parse(event.data);
                    addEvent(JSON.stringify(data, null, 2), data.event || 'unknown');
                    document.getElementById('lastResponse').textContent = JSON.stringify(data, null, 2);
                } catch (e) {
                    addEvent(event.data, 'unknown');
                }
            };
        }

        function disconnect() {
            if (ws) {
                ws.close();
                ws = null;
            }
        }

        function updateStatus(connected) {
            const status = document.getElementById('connectionStatus');
            const connectBtn = document.getElementById('connectBtn');
            const disconnectBtn = document.getElementById('disconnectBtn');
            const sendBtn = document.getElementById('sendBtn');
            const runClaudeBtn = document.getElementById('runClaudeBtn');

            if (connected) {
                status.textContent = 'Connected';
                status.className = 'status connected';
                connectBtn.disabled = true;
                disconnectBtn.disabled = false;
                sendBtn.disabled = false;
                runClaudeBtn.disabled = false;
            } else {
                status.textContent = 'Disconnected';
                status.className = 'status disconnected';
                connectBtn.disabled = false;
                disconnectBtn.disabled = true;
                sendBtn.disabled = true;
                runClaudeBtn.disabled = true;
            }
        }

        function sendCommand() {
            const command = document.getElementById('commandInput').value;
            if (ws && command) {
                ws.send(command);
                addEvent('Sent: ' + command, 'sent');
            }
        }

        function sendQuickCommand(cmd) {
            if (ws) {
                const command = JSON.stringify({ command: cmd });
                ws.send(command);
                addEvent('Sent: ' + command, 'sent');
            }
        }

        function runClaude() {
            const prompt = document.getElementById('promptInput').value;
            if (ws && prompt) {
                const command = JSON.stringify({
                    command: 'run_claude',
                    payload: { prompt: prompt }
                });
                ws.send(command);
                addEvent('Sent: ' + command, 'sent');
            }
        }

        function addEvent(message, type) {
            const events = document.getElementById('events');
            const div = document.createElement('div');
            div.className = 'event ' + type;
            div.textContent = new Date().toLocaleTimeString() + ' - ' + message;
            events.appendChild(div);
            events.scrollTop = events.scrollHeight;
        }

        function clearEvents() {
            document.getElementById('events').innerHTML = '';
        }
    </script>
</body>
</html>
```

### Using the Test Client

1. Save the HTML file
2. Open it in a browser: `open test-client.html` (macOS) or double-click
3. Click "Connect" to connect to the agent
4. Use the interface to send commands and view events

---

## 7. Automated Test Scripts

### test-connection.sh

```bash
#!/bin/bash

# Test basic connectivity

echo "=== cdev Connection Test ==="
echo ""

# Test HTTP health endpoint
echo "1. Testing HTTP health endpoint..."
HEALTH=$(curl -s -w "\n%{http_code}" http://localhost:8766/health)
HTTP_CODE=$(echo "$HEALTH" | tail -n1)
BODY=$(echo "$HEALTH" | head -n-1)

if [ "$HTTP_CODE" == "200" ]; then
    echo "   ✅ Health check passed"
    echo "   Response: $BODY"
else
    echo "   ❌ Health check failed (HTTP $HTTP_CODE)"
    exit 1
fi

echo ""

# Test HTTP status endpoint
echo "2. Testing HTTP status endpoint..."
STATUS=$(curl -s -w "\n%{http_code}" http://localhost:8766/api/status)
HTTP_CODE=$(echo "$STATUS" | tail -n1)
BODY=$(echo "$STATUS" | head -n-1)

if [ "$HTTP_CODE" == "200" ]; then
    echo "   ✅ Status endpoint passed"
    echo "   Response: $BODY"
else
    echo "   ❌ Status endpoint failed (HTTP $HTTP_CODE)"
    exit 1
fi

echo ""

# Test WebSocket connection
echo "3. Testing WebSocket connection..."
WS_RESULT=$(echo '{"command":"get_status"}' | timeout 5 websocat ws://localhost:8765 2>&1 | head -n1)

if echo "$WS_RESULT" | grep -q "session_start\|status_response"; then
    echo "   ✅ WebSocket connection passed"
    echo "   First event: $WS_RESULT"
else
    echo "   ❌ WebSocket connection failed"
    echo "   Result: $WS_RESULT"
    exit 1
fi

echo ""
echo "=== All connection tests passed! ==="
```

### test-claude.sh

```bash
#!/bin/bash

# Test Claude execution

echo "=== cdev Claude Execution Test ==="
echo ""

# Start Claude with a simple prompt
echo "1. Starting Claude with test prompt..."
RESPONSE=$(curl -s -X POST http://localhost:8766/api/claude/run \
    -H "Content-Type: application/json" \
    -d '{"prompt": "Print the text: TEST_SUCCESS"}')

echo "   Response: $RESPONSE"

if echo "$RESPONSE" | grep -q "started"; then
    echo "   ✅ Claude started successfully"
else
    echo "   ❌ Failed to start Claude"
    exit 1
fi

echo ""

# Wait and check status
echo "2. Waiting for Claude to process (10s)..."
sleep 10

STATUS=$(curl -s http://localhost:8766/api/status)
STATE=$(echo "$STATUS" | jq -r '.claude_state')

echo "   Current state: $STATE"

echo ""
echo "=== Claude execution test completed ==="
```

### test-filewatcher.sh

```bash
#!/bin/bash

# Test file watcher

REPO_PATH="${1:-.}"
TEST_FILE="$REPO_PATH/cdev-test-file-$(date +%s).txt"

echo "=== cdev File Watcher Test ==="
echo ""
echo "Repository: $REPO_PATH"
echo "Test file: $TEST_FILE"
echo ""

# Connect WebSocket and capture events in background
echo "1. Connecting WebSocket and waiting for events..."
(timeout 10 websocat ws://localhost:8765 > /tmp/ws_events.txt 2>&1) &
WS_PID=$!
sleep 2

# Create test file
echo "2. Creating test file..."
echo "Test content $(date)" > "$TEST_FILE"
sleep 2

# Modify test file
echo "3. Modifying test file..."
echo "Modified content $(date)" >> "$TEST_FILE"
sleep 2

# Check captured events
echo "4. Checking captured events..."
wait $WS_PID 2>/dev/null

if grep -q "file_changed" /tmp/ws_events.txt; then
    echo "   ✅ file_changed event received"
    grep "file_changed" /tmp/ws_events.txt | head -n1
else
    echo "   ❌ No file_changed event received"
fi

if grep -q "git_diff" /tmp/ws_events.txt; then
    echo "   ✅ git_diff event received"
else
    echo "   ⚠️  No git_diff event (file may not be tracked)"
fi

# Cleanup
echo ""
echo "5. Cleaning up test file..."
rm -f "$TEST_FILE"
rm -f /tmp/ws_events.txt

echo ""
echo "=== File watcher test completed ==="
```

### run-all-tests.sh

```bash
#!/bin/bash

# Run all POC tests

echo "========================================"
echo "    cdev POC Test Suite"
echo "========================================"
echo ""

# Check if agent is running
if ! curl -s http://localhost:8766/health > /dev/null 2>&1; then
    echo "❌ Error: cdev is not running!"
    echo "   Please start the agent first: ./cdev start"
    exit 1
fi

echo "✅ Agent is running"
echo ""

# Run connection tests
./test-connection.sh
echo ""

# Run file watcher tests
./test-filewatcher.sh
echo ""

echo "========================================"
echo "    All tests completed!"
echo "========================================"
```

---

## 8. Troubleshooting

### Agent Won't Start

**Symptom**: Error message "address already in use"

```bash
# Check if ports are in use
lsof -i :8765
lsof -i :8766

# Kill existing cdev processes
pkill -f cdev

# Or kill by PID
kill -9 <PID>
```

### Stopping the Agent

```bash
# Graceful stop (sends SIGTERM)
pkill -f cdev

# Or use Ctrl+C in the terminal where agent is running

# Verify ports are free
lsof -i :8765 -i :8766 || echo "Ports are free"
```

### WebSocket Connection Refused

**Symptom**: `Connection refused` when connecting

```bash
# Verify agent is running
curl http://localhost:8766/health

# Check firewall (macOS)
sudo /usr/libexec/ApplicationFirewall/socketfilterfw --listapps
```

### No Events Received

**Symptom**: Connected but no events appear

```bash
# Check agent logs for errors
./cdev start 2>&1 | tee agent.log

# Verify you're in a git repository
git status
```

### Claude Not Starting

**Symptom**: `run_claude` returns error or Claude exits immediately

```bash
# Verify Claude CLI is installed
claude --version

# Check Claude CLI location
which claude

# Test Claude directly in print mode (how cdev runs it)
claude -p --verbose --output-format stream-json "What is 2+2?"
```

**Common Issues:**

1. **Missing --verbose flag**: `stream-json` format requires `--verbose`
2. **Permission denied**: Claude needs `--dangerously-skip-permissions` for non-interactive mode
3. **Wrong working directory**: Claude runs in repo path, verify it's set correctly

**Claude CLI flags used by cdev:**
- `-p` (print mode) - Non-interactive output
- `--verbose` - Required for stream-json
- `--output-format stream-json` - JSON streaming for parsing
- `--dangerously-skip-permissions` - Auto-approve tool use (POC only)

### Claude Stops Immediately

**Symptom**: Claude starts but stops with exit code -1

This was a known issue where the HTTP request context was cancelling the Claude process. Fixed by using `context.Background()` for long-running operations.

```bash
# Check the Claude log file for details
cat /path/to/repo/.cdev/logs/claude_*.jsonl
```

### File Changes Not Detected

**Symptom**: Modify files but no `file_changed` events

```bash
# Check if file is in ignored patterns
cat ~/.cdev/config.yaml | grep ignore

# Verify watcher is enabled
curl http://localhost:8766/api/status | jq '.watcher_enabled'
```

### Permission Errors (macOS)

**Symptom**: `Operation not permitted` errors

```bash
# Grant Full Disk Access
# System Preferences > Security & Privacy > Privacy > Full Disk Access
# Add Terminal.app or your terminal emulator
```

### Windows-Specific Issues

**Symptom**: Various errors on Windows

```powershell
# Run as Administrator if needed
# Check Windows Defender/Firewall

# Verify Git is in PATH
git --version

# Verify Claude is in PATH
claude --version
```

---

## Test Checklist

Use this checklist before considering the POC complete:

### Connection Tests
- [ ] Health endpoint returns 200
- [ ] Status endpoint returns valid JSON
- [ ] WebSocket connects successfully
- [ ] Session start event received on connect
- [ ] Multiple clients can connect simultaneously

### Claude Tests
- [ ] Claude starts with prompt
- [ ] Log events stream in real-time
- [ ] Status changes to "running" then "idle"
- [ ] Claude can be stopped mid-execution
- [ ] Error status on Claude failure

### File Watcher Tests
- [ ] File creation detected
- [ ] File modification detected
- [ ] File deletion detected
- [ ] Ignored files (.git, node_modules) not reported
- [ ] Events debounced (no duplicates)

### Git Tests
- [ ] Git diff generated on file change
- [ ] Diff contains correct content
- [ ] Staged vs unstaged differentiated
- [ ] New untracked files handled

### Security Tests
- [ ] Path traversal blocked (../../../etc/passwd)
- [ ] Large file requests handled
- [ ] Invalid commands return error

### Cross-Platform Tests
- [ ] Works on macOS Intel
- [ ] Works on macOS Apple Silicon
- [ ] Works on Windows 10/11

---

## Next Steps After POC Validation

Once all tests pass:

1. **Document any issues** found during testing
2. **Create issue tickets** for bugs or improvements
3. **Proceed to cdev-mobile development** using this working agent
4. **Use the web test client** as reference for iOS implementation

The POC is ready for mobile development when:
- All checklist items pass
- Agent runs stable for 1+ hours
- No memory leaks observed
- Cross-platform builds work
