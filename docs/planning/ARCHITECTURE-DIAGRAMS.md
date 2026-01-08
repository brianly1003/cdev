# Multi-Agent System Architecture Diagrams

**Version:** 1.0.0
**Last Updated:** 2026-01-06

---

## System Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        cdev-ios (iPhone)                        │
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │            MultiSessionViewModel                          │ │
│  │  - Manages 4-6 concurrent session states                  │ │
│  │  - Polls aggregate status every 5s                        │ │
│  │  - Handles real-time events (logs, permissions)           │ │
│  └────────────┬──────────────────────────────┬───────────────┘ │
│               │                              │                 │
│               ▼                              ▼                 │
│  ┌─────────────────────┐      ┌──────────────────────────┐    │
│  │  GridDashboardView  │      │  WebSocketService        │    │
│  │  (2x2 Session Cards)│      │  (JSON-RPC 2.0 Client)   │    │
│  └─────────────────────┘      └──────────────┬───────────┘    │
│                                               │                 │
└───────────────────────────────────────────────┼─────────────────┘
                                                │
                                        WebSocket (port 8766)
                                                │
┌───────────────────────────────────────────────┼─────────────────┐
│                        cdev (Go Daemon)       ▼                 │
│                                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Unified Server (port 8766)                  │   │
│  │  - HTTP + WebSocket on single port                       │   │
│  │  - JSON-RPC 2.0 dispatcher                               │   │
│  └────────────┬─────────────────────────────────────────────┘   │
│               │                                                 │
│               ▼                                                 │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │         RPC Method Handlers                              │   │
│  │  ┌──────────────────┐  ┌────────────────────────────┐   │   │
│  │  │ MultiSession     │  │ Session Service            │   │   │
│  │  │ Service          │  │ (single session ops)       │   │   │
│  │  │ - aggregate-     │  │ - session/list             │   │   │
│  │  │   status         │  │ - session/watch            │   │   │
│  │  │ - start-batch    │  │ - session/messages         │   │   │
│  │  │ - stop-all       │  │                            │   │   │
│  │  └─────────┬────────┘  └────────────┬───────────────┘   │   │
│  └────────────┼──────────────────────────┼──────────────────┘   │
│               │                          │                      │
│               ▼                          ▼                      │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │              Session Manager                              │  │
│  │  ┌──────────────────────────────────────────────────┐    │  │
│  │  │ sessions: map[string]*Session                    │    │  │
│  │  │   - abc-123 → Session (Claude instance #1)       │    │  │
│  │  │   - def-456 → Session (Claude instance #2)       │    │  │
│  │  │   - ghi-789 → Session (Claude instance #3)       │    │  │
│  │  │   - jkl-012 → Session (Claude instance #4)       │    │  │
│  │  └──────────────────────────────────────────────────┘    │  │
│  │  ┌──────────────────────────────────────────────────┐    │  │
│  │  │ activeSessions: map[string][]string              │    │  │
│  │  │   - workspace-1 → [abc-123, def-456, ghi-789]    │    │  │
│  │  └──────────────────────────────────────────────────┘    │  │
│  │  ┌──────────────────────────────────────────────────┐    │  │
│  │  │ sessionMetrics: map[string]*SessionMetrics       │    │  │
│  │  │   - Tracks tokens, messages, CPU, memory         │    │  │
│  │  └──────────────────────────────────────────────────┘    │  │
│  └─────────────┬────────────────────────────────────────────┘  │
│                │                                               │
│                ▼                                               │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │              Resource Monitor                             │ │
│  │  - Samples process metrics every 5s                       │ │
│  │  - Uses gopsutil to read CPU/memory                       │ │
│  │  - Maintains 60 sample history per session                │ │
│  └─────────────┬────────────────────────────────────────────┘ │
│                │                                               │
│                ▼                                               │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │         4 Claude Manager Instances (Adapters)             │ │
│  │  ┌───────────────┐  ┌───────────────┐                    │ │
│  │  │ Claude #1     │  │ Claude #2     │                    │ │
│  │  │ PID: 12345    │  │ PID: 12346    │                    │ │
│  │  │ State: Running│  │ State: Waiting│                    │ │
│  │  └───────┬───────┘  └───────┬───────┘                    │ │
│  │          │                  │                             │ │
│  │  ┌───────────────┐  ┌───────────────┐                    │ │
│  │  │ Claude #3     │  │ Claude #4     │                    │ │
│  │  │ PID: 12347    │  │ PID: 12348    │                    │ │
│  │  │ State: Running│  │ State: Idle   │                    │ │
│  │  └───────┬───────┘  └───────┬───────┘                    │ │
│  └──────────┼──────────────────┼────────────────────────────┘ │
│             │                  │                              │
│             ▼                  ▼                              │
│  ┌──────────────────────────────────────────────────────────┐ │
│  │              Event Hub (Fan-out Broadcaster)              │ │
│  │  - Publishes claude_log, claude_status, pty_permission   │ │
│  │  - All events include session_id context                 │ │
│  │  - WebSocket clients auto-subscribed                     │ │
│  └──────────────────────────────────────────────────────────┘ │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

---

## Data Flow: Aggregate Status Query

```
┌─────────────┐
│  cdev-ios   │
└──────┬──────┘
       │
       │ 1. Send RPC Request
       │    {"method": "multisession/aggregate-status"}
       │
       ▼
┌────────────────────────┐
│  JSON-RPC Dispatcher   │
└──────┬─────────────────┘
       │
       │ 2. Route to Handler
       │
       ▼
┌──────────────────────────┐
│  MultiSessionService     │
│  AggregateStatus()       │
└──────┬───────────────────┘
       │
       │ 3. Query Session Manager
       │    GetActiveSessions()
       │    GetSessionMetrics()
       │
       ▼
┌─────────────────────────────────┐
│      Session Manager             │
│  ┌────────────────────────────┐  │
│  │ sessions map               │  │
│  │ - abc-123 → *Session       │  │
│  │ - def-456 → *Session       │  │
│  │ - ghi-789 → *Session       │  │
│  │ - jkl-012 → *Session       │  │
│  └────────────────────────────┘  │
└──────┬──────────────────────────┘
       │
       │ 4. Collect Metrics
       │
       ▼
┌─────────────────────────────────┐
│      Resource Monitor            │
│  ┌────────────────────────────┐  │
│  │ samples map                │  │
│  │ - abc-123 → CPU: 35%, Mem: 512MB │
│  │ - def-456 → CPU: 12%, Mem: 256MB │
│  │ - ghi-789 → CPU: 48%, Mem: 768MB │
│  │ - jkl-012 → CPU: 0%,  Mem: 0MB   │
│  └────────────────────────────┘  │
└──────┬──────────────────────────┘
       │
       │ 5. Aggregate Results
       │
       ▼
┌──────────────────────────┐
│  MultiSessionService     │
│  - Count by state        │
│  - Sum metrics           │
│  - Build response        │
└──────┬───────────────────┘
       │
       │ 6. Return JSON-RPC Response
       │    {
       │      "total_sessions": 4,
       │      "running_count": 2,
       │      "waiting_count": 1,
       │      "idle_count": 1,
       │      "sessions": [...],
       │      "aggregate_metrics": {
       │        "total_cpu_percent": 95,
       │        "total_memory_mb": 1536,
       │        ...
       │      }
       │    }
       │
       ▼
┌─────────────┐
│  cdev-ios   │
│  - Update   │
│    UI       │
└─────────────┘
```

---

## Event Flow: Real-Time Logs

```
Claude Process #2 outputs line
         │
         ▼
┌─────────────────────────┐
│  Claude Manager #2      │
│  - Read from stdout     │
│  - Parse JSONL          │
└──────┬──────────────────┘
       │
       │ Publish Event
       │
       ▼
┌──────────────────────────────────┐
│         Event Hub                │
│  Event: claude_log               │
│  Payload: {                      │
│    "session_id": "def-456",      │
│    "workspace_id": "workspace-1",│
│    "line": "> npm install...",   │
│    "stream": "stdout"            │
│  }                               │
└──────┬───────────────────────────┘
       │
       │ Fan-out to all subscribers
       │
       ├────────────────────┬────────────────────┐
       │                    │                    │
       ▼                    ▼                    ▼
┌──────────────┐   ┌──────────────┐   ┌──────────────┐
│ WebSocket    │   │ WebSocket    │   │ Internal     │
│ Client #1    │   │ Client #2    │   │ Logger       │
│ (iPhone)     │   │ (iPad)       │   │              │
└──────┬───────┘   └──────┬───────┘   └──────────────┘
       │                  │
       │                  │
       ▼                  ▼
┌──────────────┐   ┌──────────────┐
│ cdev-ios     │   │ cdev-ios     │
│ - Filter by  │   │ - Filter by  │
│   session_id │   │   session_id │
│ - Append to  │   │ - Append to  │
│   logs array │   │   logs array │
│ - Update UI  │   │ - Update UI  │
└──────────────┘   └──────────────┘
```

---

## Batch Start Flow

```
User fills BatchStartSheet with 4 prompts
         │
         ▼
┌─────────────────────────┐
│  MultiSessionViewModel  │
│  startBatchSessions()   │
└──────┬──────────────────┘
       │
       │ Send RPC Request
       │ {
       │   "method": "multisession/start-batch",
       │   "params": {
       │     "workspace_id": "workspace-1",
       │     "prompts": [
       │       "Implement auth",
       │       "Add tests",
       │       "Create API",
       │       "Update docs"
       │     ],
       │     "max_parallel": 4
       │   }
       │ }
       │
       ▼
┌──────────────────────────┐
│  MultiSessionService     │
│  StartBatch()            │
└──────┬───────────────────┘
       │
       │ Use semaphore to limit parallelism
       │
       ├─────────┬─────────┬─────────┬─────────┐
       │         │         │         │         │
       ▼         ▼         ▼         ▼         │
    Start     Start     Start     Start       │
    Session   Session   Session   Session     │
    #1        #2        #3        #4          │
       │         │         │         │         │
       └─────────┴─────────┴─────────┴─────────┘
                         │
                         │ Wait for all to start
                         │
                         ▼
┌──────────────────────────────────────────┐
│  Session Manager                         │
│  - Creates 4 Session instances           │
│  - Spawns 4 Claude processes             │
│  - Registers with ResourceMonitor        │
│  - Adds to activeSessions map            │
└──────┬───────────────────────────────────┘
       │
       │ Return started session IDs
       │
       ▼
┌─────────────────────────┐
│  MultiSessionViewModel  │
│  - Refresh aggregate    │
│  - Update UI            │
└─────────────────────────┘
```

---

## Resource Monitoring Loop

```
┌──────────────────────────┐
│  Resource Monitor        │
│  (goroutine)             │
└──────┬───────────────────┘
       │
       │ Every 5 seconds
       │
       ▼
┌──────────────────────────────────────┐
│  For each registered session:        │
│                                      │
│  1. Get process by PID               │
│  2. Read CPU usage (gopsutil)        │
│  3. Read memory usage (gopsutil)     │
│  4. Store in samples map             │
│  5. Update peaks                     │
│  6. Trim history to 60 samples       │
└──────┬───────────────────────────────┘
       │
       │ Samples available for queries
       │
       ▼
┌──────────────────────────────────────┐
│  Sample History (per session)        │
│  ┌────────────────────────────────┐  │
│  │ [                              │  │
│  │   {t: 0s,  cpu: 10%, mem: 256} │  │
│  │   {t: 5s,  cpu: 25%, mem: 384} │  │
│  │   {t: 10s, cpu: 35%, mem: 512} │  │
│  │   {t: 15s, cpu: 40%, mem: 640} │  │
│  │   ...                          │  │
│  │   {t: 295s, cpu: 30%, mem: 480}│  │
│  │ ]                              │  │
│  └────────────────────────────────┘  │
└───────────────────────────────────────┘
```

---

## Permission Flow (Multi-Session)

```
Claude #2 shows permission prompt
         │
         ▼
┌─────────────────────────┐
│  PTY Parser (Manager #2)│
│  - Detects [Y/n] prompt │
└──────┬──────────────────┘
       │
       │ Publish Event
       │
       ▼
┌──────────────────────────────────┐
│         Event Hub                │
│  Event: pty_permission           │
│  Payload: {                      │
│    "session_id": "def-456",      │
│    "tool_name": "Bash",          │
│    "target": "rm node_modules",  │
│    "tool_use_id": "toolu_abc"    │
│  }                               │
└──────┬───────────────────────────┘
       │
       │ Broadcast to all clients
       │
       ▼
┌─────────────────────────┐
│  cdev-ios               │
│  MultiSessionViewModel  │
└──────┬──────────────────┘
       │
       │ Update session state
       │ session.hasPermission = true
       │
       ▼
┌──────────────────────────────────┐
│  GridDashboardView               │
│  - Highlights Session #2 card    │
│  - Shows "Permission Required"   │
│  - User taps to see details      │
└──────┬───────────────────────────┘
       │
       │ User approves
       │
       ▼
┌──────────────────────────┐
│  Send RPC Request        │
│  {                       │
│    "method": "agent/respond", │
│    "params": {           │
│      "tool_use_id": "toolu_abc", │
│      "response": "y"     │
│    }                     │
│  }                       │
└──────┬───────────────────┘
       │
       ▼
┌──────────────────────────┐
│  Claude Manager #2       │
│  - Injects "y\n" to PTY  │
│  - Claude continues      │
└──────────────────────────┘
```

---

## State Machine: Session Lifecycle

```
           ┌─────────┐
           │  IDLE   │ (initial state, no Claude process)
           └────┬────┘
                │
                │ agent/run RPC
                │
                ▼
         ┌──────────────┐
         │  STARTING    │ (spawning Claude process)
         └──────┬───────┘
                │
                │ Process started, PID acquired
                │
                ▼
         ┌──────────────┐
         │  RUNNING     │ (Claude executing)
         └──────┬───────┘
                │
                ├──────────────────────┐
                │                      │
                │ Permission request   │ Normal completion
                │                      │
                ▼                      ▼
         ┌──────────────┐       ┌──────────────┐
         │  WAITING     │       │  COMPLETED   │
         └──────┬───────┘       └──────┬───────┘
                │                      │
                │ User responds        │
                │                      │
                └──────────┬───────────┘
                           │
                           │ agent/stop RPC or timeout
                           │
                           ▼
                    ┌──────────────┐
                    │  STOPPING    │ (sending SIGTERM)
                    └──────┬───────┘
                           │
                           │ Process terminated
                           │
                           ▼
                    ┌──────────────┐
                    │  IDLE        │ (ready for next run)
                    └──────────────┘

Error Path:
  Any State ──(error)──▶ ERROR ──(cleanup)──▶ IDLE
```

---

## iOS View Hierarchy

```
AppState
  │
  ├─ DashboardViewModel (single session, existing)
  │
  └─ MultiSessionViewModel (new)
       │
       ├─ MultiSessionState
       │    ├─ sessions: [SessionState]
       │    ├─ aggregateStatus: AggregateStatus
       │    ├─ focusedSessionID: String?
       │    └─ layoutMode: LayoutMode
       │
       └─ Views
            │
            ├─ GridDashboardView (2x2 or 3x2 grid)
            │    └─ ForEach(sessions) { session in
            │         SessionCardView(session)
            │       }
            │
            ├─ ListDashboardView (vertical list)
            │    └─ List(sessions) { session in
            │         SessionRowView(session)
            │       }
            │
            ├─ SingleDashboardView (full-screen)
            │    └─ DashboardView (existing, reused)
            │
            ├─ BatchStartSheet
            │    └─ 4x TextField for prompts
            │         + Start button
            │
            ├─ UnifiedPermissionPanel
            │    └─ List of pending permissions
            │         from multiple sessions
            │
            └─ ResourceGraphView
                 └─ Charts (CPU & memory)
```

---

## Database Schema (SQLite)

### Session Metrics Table (New)

```sql
CREATE TABLE IF NOT EXISTS session_metrics (
    session_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    start_time TIMESTAMP NOT NULL,
    last_activity TIMESTAMP NOT NULL,
    message_count INTEGER DEFAULT 0,
    tokens_used INTEGER DEFAULT 0,
    permissions_asked INTEGER DEFAULT 0,
    permissions_approved INTEGER DEFAULT 0,
    state TEXT NOT NULL,
    current_prompt TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_session_metrics_workspace ON session_metrics(workspace_id);
CREATE INDEX idx_session_metrics_state ON session_metrics(state);
```

### Resource Samples Table (New)

```sql
CREATE TABLE IF NOT EXISTS resource_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    cpu_percent REAL NOT NULL,
    memory_mb REAL NOT NULL,
    process_pid INTEGER,
    FOREIGN KEY (session_id) REFERENCES session_metrics(session_id) ON DELETE CASCADE
);

CREATE INDEX idx_resource_samples_session ON resource_samples(session_id, timestamp DESC);
```

---

## Configuration Files

### cdev config.yaml (Enhanced)

```yaml
# Multi-session configuration
multisession:
  enabled: true
  max_concurrent: 12                # Max sessions per workspace
  resource_monitoring:
    enabled: true
    sample_interval: 5s             # How often to sample CPU/memory
    history_size: 60                # Keep last 60 samples (5 minutes)
    persist_samples: false          # Don't persist to DB (in-memory only)
  batch_start:
    default_max_parallel: 4         # Default parallelism for batch starts
    allow_oversubscribe: true       # Allow more than max_concurrent if requested
  auto_cleanup:
    idle_timeout: 30m               # Stop idle sessions after 30 minutes
    enable_auto_stop: false         # Don't auto-stop by default
```

### cdev-ios UserDefaults Keys (New)

```swift
// MultiSession settings
"multisession.preferredLayout"          // "grid" | "list" | "single"
"multisession.gridColumns"              // 2 | 3
"multisession.showResourceGraphs"       // true | false
"multisession.autoRefreshInterval"      // 5 (seconds)
"multisession.enableBatchStart"         // true | false
```

---

## API Reference

### New RPC Methods

#### `multisession/aggregate-status`

**Request:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "multisession/aggregate-status",
  "params": {
    "workspace_id": "workspace-1"  // optional, omit for all
  }
}
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "total_sessions": 4,
    "running_count": 2,
    "waiting_count": 1,
    "idle_count": 1,
    "error_count": 0,
    "sessions": [
      {
        "session_id": "abc-123",
        "workspace_id": "workspace-1",
        "state": "running",
        "current_prompt": "Implement auth",
        "start_time": "2026-01-06T10:00:00Z",
        "last_activity": "2026-01-06T10:05:32Z",
        "message_count": 24,
        "tokens_used": 5200,
        "cpu_percent": 35.2,
        "memory_mb": 512.0,
        "pid": 12345
      }
    ],
    "aggregate_metrics": {
      "total_messages": 79,
      "total_tokens": 18300,
      "total_cpu_percent": 95.0,
      "total_memory_mb": 1536.0,
      "avg_session_duration_seconds": 320.5
    }
  }
}
```

---

**Document End**
