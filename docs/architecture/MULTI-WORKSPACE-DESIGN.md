# Multi-Workspace Architecture Design

> Status: Implemented (current cdev behavior)
> Scope: cdev daemon + JSON-RPC workspace/session orchestration

## Executive Summary

cdev uses a **single daemon architecture** with **multi-workspace management built in**.

- One server process
- One transport endpoint (`http://127.0.0.1:16180`, `ws://127.0.0.1:16180/ws`)
- Multiple registered workspaces
- Workspace-scoped sessions, git state, and event routing

The previous alternatives ("one server per workspace" and "coordinator + workers") are no longer the target architecture for cdev.

## Current Architecture

```
┌──────────────────────────────────────────────────────────────┐
│ cdev daemon (single process, single port 16180)             │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ Workspace registry (persistent)                       │  │
│  │ - workspace IDs, names, paths, settings               │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ Session manager (runtime)                             │  │
│  │ - session lifecycle per workspace                     │  │
│  │ - LIVE/PTY/historical session handling                │  │
│  │ - workspace-aware active session tracking             │  │
│  └────────────────────────────────────────────────────────┘  │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │ Event hub + workspace subscriptions                   │  │
│  │ - workspace/subscribe filtering                       │  │
│  │ - session/file/git events with workspace context      │  │
│  └────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────┘
```

## Design Principles

1. Keep transport simple: one endpoint for HTTP + WebSocket.
2. Model workspaces as configuration/state, not separate daemon processes.
3. Keep runtime isolation at the session/workspace level inside one process.
4. Route events by workspace context and subscription filters.
5. Preserve backward compatibility where practical, but document current truth clearly.

## Workspace Model

Workspaces are persisted in `~/.cdev/workspaces.yaml` and loaded at startup.

Each workspace record contains:
- `id`
- `name`
- `path`
- `auto_start`
- `created_at`

Workspaces are registered dynamically via JSON-RPC (`workspace/add`) and do not require a static `repository.path` in `config.yaml`.

## Runtime Model

For each workspace, cdev can manage:
- Active managed sessions
- LIVE sessions detected from user terminal state
- Historical sessions (from Claude session files)
- Git watcher subscription state

This is handled by `internal/session/manager.go` with workspace-aware mappings:
- workspace -> active session
- session -> workspace
- session ID reconciliation when real Claude IDs are resolved

## Event Routing Model

Clients can scope their event stream to specific workspaces:
- `workspace/subscribe`
- `workspace/unsubscribe`
- `workspace/subscriptions`
- `workspace/subscribeAll`

When filtering is enabled, only events for subscribed workspaces (plus global events) are delivered to that client.

This keeps one shared WebSocket connection while preserving workspace context.

## JSON-RPC Surface (Current)

### Workspace configuration methods

- `workspace/list`
- `workspace/get`
- `workspace/add`
- `workspace/update`
- `workspace/remove`
- `workspace/discover`
- `workspace/status`
- `workspace/cache/invalidate`

### Workspace subscription methods

- `workspace/subscribe`
- `workspace/unsubscribe`
- `workspace/subscriptions`
- `workspace/subscribeAll`

### Session methods commonly used with workspaces

- `session/start`
- `session/send`
- `session/stop`
- `session/active`
- `session/info`
- `session/state`
- `session/history`

## iOS / IDE Integration Flow

Recommended high-level flow:

1. Connect once to `ws://<host>:16180/ws`.
2. Call `workspace/list`.
3. If needed, call `workspace/add` or `workspace/discover`.
4. Subscribe with `workspace/subscribe` for selected workspaces (or `workspace/subscribeAll`).
5. Use session APIs with explicit `workspace_id`/`session_id` as needed.
6. Update UI state from workspace-scoped events.

No workspace-specific daemon ports are required in this model.

## Deprecated / Removed from Architecture

The following concepts are outdated for current cdev architecture and should not be treated as recommended design:

- Option B: one cdev server process per workspace
- Option C: coordinator + worker daemon topology
- Dedicated workspace-manager daemon port for normal operation
- Port-pool planning based on per-workspace server instances
- Workspace lifecycle RPCs that imply spawning/stopping per-workspace daemons

If older documents still reference those concepts, treat them as historical context only.

## Operational Notes

- Default server endpoint remains `127.0.0.1:16180`.
- Git watcher lifecycle is subscription-driven (starts on workspace subscribe, stops on unsubscribe/reference count zero).
- Session ID reconciliation is built in so clients can follow real Claude session IDs cleanly when they appear.

## Summary

The authoritative architecture is:

- **Single cdev daemon**
- **Multi-workspace registry in-process**
- **Workspace-scoped runtime/session management**
- **Subscription-based event filtering**

This is the model to use for new development and integration work.
