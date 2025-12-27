# Logging & Tracing Design for cdev

> **Status**: Proposed
> **Created**: 2025-12-27
> **Purpose**: Enable fast debugging and issue tracing in multi-device, multi-workspace environment

## Table of Contents

1. [Problem Statement](#problem-statement)
2. [Current State Analysis](#current-state-analysis)
3. [Design Goals](#design-goals)
4. [Architecture](#architecture)
5. [Implementation Details](#implementation-details)
6. [Debug Endpoints](#debug-endpoints)
7. [Log Format Standards](#log-format-standards)
8. [iOS Integration](#ios-integration)
9. [Implementation Plan](#implementation-plan)

---

## Problem Statement

### Challenges in Debugging cdev

1. **Multi-device concurrency**: Multiple iOS devices connect simultaneously, making it hard to trace which device caused an issue
2. **Event flow complexity**: Events flow through multiple components (WebSocket → Hub → Subscribers → Clients)
3. **Reference counting bugs**: Watcher counts can get out of sync, causing streams to stop unexpectedly
4. **Async operations**: Callbacks and goroutines make linear tracing difficult
5. **Workspace/session hierarchy**: Need to correlate logs within workspace and session context

### Recent Bug Example

The session watcher double-decrement bug was hard to debug because:
- No correlation between `session/unwatch` call and `OnClientDisconnect`
- No visibility into `streamerWatcherCount` state
- No way to see which clients were actually watching

---

## Current State Analysis

### Strengths

| Feature | Status |
|---------|--------|
| Structured logging (zerolog) | ✅ Implemented |
| `request_id` for event correlation | ✅ Partial |
| `client_id` in context | ✅ Implemented |
| Log statements coverage | ✅ ~825 across 64 files |

### Gaps

| Gap | Impact |
|-----|--------|
| No end-to-end trace ID | Cannot trace request from iOS through all components |
| Inconsistent context fields | Some logs missing workspace_id, session_id |
| No state snapshots | Cannot see reference counting state |
| No debug endpoints | Cannot inspect live state |
| No log export | Cannot collect logs from user devices |

---

## Design Goals

1. **Trace any request end-to-end** in under 30 seconds
2. **Identify state inconsistencies** (watcher counts, subscriptions) instantly
3. **Correlate multi-device interactions** easily
4. **Enable user-reported issue diagnosis** without direct access
5. **Minimal performance overhead** in production

---

## Architecture

### Trace ID Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              iOS Device                                  │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ Request: workspace/session/watch                                 │   │
│  │ _trace_id: "ios-a1b2c3d4" (optional, for user-initiated traces) │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           WebSocket Handler                              │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ Generate trace_id if not provided: "srv-e5f6g7h8"               │   │
│  │ Add to context: ctx = WithTraceID(ctx, traceID)                 │   │
│  │ Log: → incoming message trace=e5f6g7h8 client=fe61035a          │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                             RPC Dispatcher                               │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ Log: dispatching trace=e5f6g7h8 method=workspace/session/watch  │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           Session Manager                                │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ Log: streamer.client.add trace=e5f6g7h8 client=fe61035a         │   │
│  │      watchers=[fe61035a] workspace=1ea27585 session=46e054a5    │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                              Response                                    │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │ Log: ← response trace=e5f6g7h8 method=workspace/session/watch   │   │
│  │      status=success duration=12ms                                │   │
│  └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────┘
```

### Component Logging Responsibilities

```
┌────────────────────┬─────────────────────────────────────────────────────┐
│ Component          │ What to Log                                         │
├────────────────────┼─────────────────────────────────────────────────────┤
│ WebSocket Server   │ Connect, disconnect, message in/out, errors         │
│ RPC Dispatcher     │ Method dispatch, duration, errors                   │
│ Session Manager    │ Watch/unwatch, state changes, client tracking       │
│ Event Hub          │ Publish, subscribe, unsubscribe                     │
│ Session Streamer   │ Start, stop, emit events, client add/remove         │
│ Git Tracker        │ Status changes, watcher lifecycle                   │
│ Workspace Config   │ CRUD operations, events broadcast                   │
└────────────────────┴─────────────────────────────────────────────────────┘
```

---

## Implementation Details

### 1. Tracing Package

Create `internal/tracing/context.go`:

```go
package tracing

import (
    "context"
    "github.com/google/uuid"
    "github.com/rs/zerolog"
    "github.com/rs/zerolog/log"
)

type contextKey string

const (
    TraceIDKey     contextKey = "trace_id"
    ClientIDKey    contextKey = "client_id"
    WorkspaceIDKey contextKey = "workspace_id"
    SessionIDKey   contextKey = "session_id"
)

// NewTraceID generates a short trace ID (8 chars for readability in logs)
func NewTraceID() string {
    return uuid.New().String()[:8]
}

// WithTraceID adds trace ID to context
func WithTraceID(ctx context.Context, traceID string) context.Context {
    return context.WithValue(ctx, TraceIDKey, traceID)
}

// WithContext adds all tracing fields to context
func WithContext(ctx context.Context, traceID, clientID, workspaceID, sessionID string) context.Context {
    ctx = context.WithValue(ctx, TraceIDKey, traceID)
    ctx = context.WithValue(ctx, ClientIDKey, clientID)
    ctx = context.WithValue(ctx, WorkspaceIDKey, workspaceID)
    ctx = context.WithValue(ctx, SessionIDKey, sessionID)
    return ctx
}

// GetTraceID extracts trace ID from context
func GetTraceID(ctx context.Context) string {
    if v, ok := ctx.Value(TraceIDKey).(string); ok {
        return v
    }
    return ""
}

// Logger returns a zerolog logger enriched with all context fields
func Logger(ctx context.Context) zerolog.Logger {
    l := log.Logger

    if traceID := ctx.Value(TraceIDKey); traceID != nil {
        l = l.With().Str("trace_id", traceID.(string)).Logger()
    }
    if clientID := ctx.Value(ClientIDKey); clientID != nil {
        l = l.With().Str("client_id", clientID.(string)).Logger()
    }
    if workspaceID := ctx.Value(WorkspaceIDKey); workspaceID != nil {
        l = l.With().Str("workspace_id", workspaceID.(string)).Logger()
    }
    if sessionID := ctx.Value(SessionIDKey); sessionID != nil {
        l = l.With().Str("session_id", sessionID.(string)).Logger()
    }

    return l
}
```

### 2. Logging Events Package

Create `internal/logging/events.go`:

```go
package logging

import (
    "context"
    "time"

    "github.com/brianly1003/cdev/internal/tracing"
)

// EventType categorizes log events for filtering
type EventType string

const (
    // Client lifecycle
    EventClientConnect    EventType = "client.connect"
    EventClientDisconnect EventType = "client.disconnect"

    // RPC operations
    EventRPCRequest  EventType = "rpc.request"
    EventRPCResponse EventType = "rpc.response"
    EventRPCError    EventType = "rpc.error"

    // Session operations
    EventSessionWatch   EventType = "session.watch"
    EventSessionUnwatch EventType = "session.unwatch"
    EventSessionStart   EventType = "session.start"
    EventSessionStop    EventType = "session.stop"

    // Streamer operations
    EventStreamerStart        EventType = "streamer.start"
    EventStreamerStop         EventType = "streamer.stop"
    EventStreamerClientAdd    EventType = "streamer.client.add"
    EventStreamerClientRemove EventType = "streamer.client.remove"

    // Workspace operations
    EventWorkspaceSubscribe   EventType = "workspace.subscribe"
    EventWorkspaceUnsubscribe EventType = "workspace.unsubscribe"
    EventWorkspaceAdd         EventType = "workspace.add"
    EventWorkspaceRemove      EventType = "workspace.remove"

    // Event hub operations
    EventHubPublish   EventType = "hub.publish"
    EventHubDeliver   EventType = "hub.deliver"
    EventHubSubscribe EventType = "hub.subscribe"

    // Git operations
    EventGitWatcherStart EventType = "git.watcher.start"
    EventGitWatcherStop  EventType = "git.watcher.stop"
    EventGitStatusChange EventType = "git.status.change"
)

// LogEvent logs a structured event with consistent formatting
func LogEvent(ctx context.Context, event EventType, fields map[string]interface{}) {
    l := tracing.Logger(ctx).With().
        Str("event", string(event)).
        Logger()

    for k, v := range fields {
        l = l.With().Interface(k, v).Logger()
    }

    l.Info().Msg("")
}

// LogEventWithDuration logs an event with duration measurement
func LogEventWithDuration(ctx context.Context, event EventType, start time.Time, fields map[string]interface{}) {
    if fields == nil {
        fields = make(map[string]interface{})
    }
    fields["duration_ms"] = time.Since(start).Milliseconds()
    LogEvent(ctx, event, fields)
}
```

### 3. Debug State Interface

Add to `internal/session/manager.go`:

```go
// DebugState represents the current state of the session manager for debugging
type DebugState struct {
    Timestamp          time.Time         `json:"timestamp"`
    StreamerActive     bool              `json:"streamer_active"`
    StreamerWorkspace  string            `json:"streamer_workspace"`
    StreamerSession    string            `json:"streamer_session"`
    StreamerWatchers   []string          `json:"streamer_watchers"`
    WatcherCount       int               `json:"watcher_count"`
    GitWatcherCounts   map[string]int    `json:"git_watcher_counts"`
    ActiveSessions     map[string]string `json:"active_sessions"`
    RegisteredWorkspaces []string        `json:"registered_workspaces"`
}

// GetDebugState returns the current state for debugging
func (m *Manager) GetDebugState() *DebugState {
    m.streamerMu.Lock()
    defer m.streamerMu.Unlock()

    m.mu.RLock()
    defer m.mu.RUnlock()

    watchers := make([]string, 0, len(m.streamerWatchers))
    for clientID := range m.streamerWatchers {
        watchers = append(watchers, clientID)
    }

    workspaces := make([]string, 0, len(m.workspaces))
    for wsID := range m.workspaces {
        workspaces = append(workspaces, wsID)
    }

    // Copy maps to avoid race conditions
    gitCounts := make(map[string]int)
    for k, v := range m.gitWatcherCounts {
        gitCounts[k] = v
    }

    activeSess := make(map[string]string)
    for k, v := range m.activeSessions {
        activeSess[k] = v
    }

    return &DebugState{
        Timestamp:           time.Now().UTC(),
        StreamerActive:      m.streamer != nil,
        StreamerWorkspace:   m.streamerWorkspaceID,
        StreamerSession:     m.streamerSessionID,
        StreamerWatchers:    watchers,
        WatcherCount:        len(m.streamerWatchers),
        GitWatcherCounts:    gitCounts,
        ActiveSessions:      activeSess,
        RegisteredWorkspaces: workspaces,
    }
}
```

### 4. Enhanced Streamer Logging

Update `internal/session/manager.go` key functions:

```go
func (m *Manager) WatchWorkspaceSession(clientID, workspaceID, sessionID string) (*WatchInfo, error) {
    m.streamerMu.Lock()
    defer m.streamerMu.Unlock()

    // Log state BEFORE change
    m.logger.Info("session.watch requested",
        "client_id", clientID,
        "workspace_id", workspaceID,
        "session_id", sessionID,
        "current_watchers", m.getWatchersList(),
        "current_session", m.streamerSessionID,
    )

    // ... existing logic ...

    // Log state AFTER change
    m.logger.Info("session.watch completed",
        "client_id", clientID,
        "workspace_id", workspaceID,
        "session_id", sessionID,
        "watchers", m.getWatchersList(),
        "watcher_count", len(m.streamerWatchers),
    )
}

// Helper to get watchers list (call with lock held)
func (m *Manager) getWatchersList() []string {
    watchers := make([]string, 0, len(m.streamerWatchers))
    for id := range m.streamerWatchers {
        watchers = append(watchers, id)
    }
    return watchers
}
```

---

## Debug Endpoints

### RPC Methods

| Method | Description | Response |
|--------|-------------|----------|
| `debug/state` | Get full system state | DebugState object |
| `debug/clients` | List connected clients | Client list with subscriptions |
| `debug/logs/level` | Get/set log level | Current level |
| `debug/trace/enable` | Enable verbose tracing for session | Confirmation |

### Example: debug/state Response

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "result": {
        "timestamp": "2025-12-27T10:30:45.123Z",
        "session_manager": {
            "streamer_active": true,
            "streamer_workspace": "1ea27585-e154-4e31-a8a8-b0f16118b862",
            "streamer_session": "46e054a5-5c1f-4764-a883-8d4572fc2956",
            "streamer_watchers": ["fe61035a-149c-4f6c-9cfa-fd1b954a4cd5"],
            "watcher_count": 1,
            "git_watcher_counts": {
                "1ea27585-e154-4e31-a8a8-b0f16118b862": 1,
                "4f663898-0b5d-4c35-81e5-a03ff46ca941": 2
            },
            "active_sessions": {
                "1ea27585-e154-4e31-a8a8-b0f16118b862": "46e054a5-5c1f-4764-a883-8d4572fc2956"
            }
        },
        "connected_clients": [
            {
                "id": "fe61035a-149c-4f6c-9cfa-fd1b954a4cd5",
                "connected_at": "2025-12-27T10:25:00Z",
                "subscribed_workspaces": ["1ea27585-e154-4e31-a8a8-b0f16118b862"],
                "session_focus": {
                    "workspace_id": "1ea27585-e154-4e31-a8a8-b0f16118b862",
                    "session_id": "46e054a5-5c1f-4764-a883-8d4572fc2956"
                }
            }
        ],
        "hub": {
            "subscriber_count": 1
        }
    }
}
```

---

## Log Format Standards

### Console Format (Development)

```
[17:30:35.166] INFO  → rpc.request trace=a1b2c3d4 client=fe61035a method=workspace/session/watch
[17:30:35.167] INFO  streamer.client.add trace=a1b2c3d4 client=fe61035a session=46e054a5 watchers=[fe61035a] count=1
[17:30:35.168] INFO  ← rpc.response trace=a1b2c3d4 method=workspace/session/watch duration_ms=12 status=success
[17:30:36.605] INFO  hub.publish type=claude_message session=46e054a5 subscribers=1
[17:30:39.118] INFO  client.disconnect client=0ba973d6 subscriptions=[1ea27585]
[17:30:39.119] INFO  streamer.client.remove client=0ba973d6 watchers=[fe61035a] count=1 reason=disconnect
```

### JSON Format (Production/Aggregation)

```json
{"level":"info","trace_id":"a1b2c3d4","client_id":"fe61035a","event":"rpc.request","method":"workspace/session/watch","time":"2025-12-27T17:30:35.166Z"}
{"level":"info","trace_id":"a1b2c3d4","client_id":"fe61035a","event":"streamer.client.add","session_id":"46e054a5","watchers":["fe61035a"],"count":1,"time":"2025-12-27T17:30:35.167Z"}
```

### Required Fields by Event Type

| Event Type | Required Fields |
|------------|-----------------|
| `client.*` | client_id |
| `rpc.*` | trace_id, client_id, method |
| `session.*` | trace_id, client_id, workspace_id, session_id |
| `streamer.*` | client_id, session_id, watchers, count |
| `workspace.*` | client_id, workspace_id |
| `hub.*` | event_type, subscriber_count |

---

## iOS Integration

### Optional Trace ID in Requests

iOS can include a trace ID for user-initiated debugging:

```json
{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "workspace/session/watch",
    "params": {
        "workspace_id": "1ea27585-...",
        "session_id": "46e054a5-...",
        "_trace_id": "ios-debug-12345"
    }
}
```

If `_trace_id` is provided, the server uses it instead of generating one. This allows iOS to:
1. Log the same trace ID locally
2. Share with support for cross-referencing
3. Enable verbose logging for specific operations

### Debug Screen in iOS App

Consider adding a debug screen that shows:
- Current client ID
- Active subscriptions
- Session focus
- Recent trace IDs (for support)

---

## Implementation Plan

### Phase 1: Foundation (Priority: High)

| Task | Effort | Description |
|------|--------|-------------|
| Create `internal/tracing` package | 2h | Context helpers, trace ID generation |
| Add trace ID to RPC dispatcher | 2h | Generate/extract trace ID, add to context |
| Add `debug/state` endpoint | 2h | Expose session manager state |
| Enhanced streamer logging | 2h | Log watchers list on state changes |

### Phase 2: Comprehensive Logging (Priority: Medium)

| Task | Effort | Description |
|------|--------|-------------|
| Create `internal/logging` package | 2h | Structured event logging helpers |
| Update all RPC methods | 4h | Add consistent logging with context |
| Add hub event logging | 2h | Log publish/deliver with counts |
| Add client lifecycle logging | 2h | Connect/disconnect with full state |

### Phase 3: Advanced Features (Priority: Low)

| Task | Effort | Description |
|------|--------|-------------|
| Runtime log level control | 2h | `debug/logs/level` endpoint |
| Per-session trace mode | 3h | Verbose logging for specific session |
| Log export endpoint | 4h | Download recent logs as JSON |
| iOS debug integration | 4h | Trace ID support, debug screen |

---

## Success Metrics

After implementation, we should be able to:

1. **Trace a bug report in < 5 minutes**: Given a timestamp and client ID, find all related logs
2. **Identify state issues instantly**: `debug/state` shows exact watcher counts and subscriptions
3. **Correlate multi-device issues**: Trace ID links all operations for a single request
4. **Debug without code changes**: Runtime log level and trace mode enable deep debugging

---

## References

- [Zerolog Documentation](https://github.com/rs/zerolog)
- [OpenTelemetry Tracing Concepts](https://opentelemetry.io/docs/concepts/signals/traces/)
- [Structured Logging Best Practices](https://www.honeycomb.io/blog/structured-logging-and-your-team)
