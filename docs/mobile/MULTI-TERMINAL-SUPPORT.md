# Multi-Terminal (Multi-Chat Window) Support for Claude Code / Codex in cdev-ios

## Goal
Enable `cdev-ios` to run and display multiple concurrent terminal/chat windows (sessions) for Claude Code and Codex, similar to Auto-Claude.

## Current-State Assessment

### What already works
- Backend already exposes multi-session primitives and session-aware APIs:
  - `session/list`
  - `session/watch`
  - `session/unwatch`
  - `session/send`
  - workspace-scoped session and workspace/session status/watch flows
- Session model is already multi-entity in both backend and iOS domain:
  - workspaces can contain multiple sessions
  - events and workspace metadata include `session_id`
- iOS has session-awareness infra already:
  - focused session tracking
  - watch/unwatch APIs in repository interface
  - explicit session events for resolve/watch start/stop

### Current limitation
iOS currently behaves like a **single-focused session UI**, with state centered around one active `sessionId` and one active watch stream in the app layer.

So the gap is primarily **client architecture**, not backend capability.

---

## Proposed Architecture

## 1) Runtime model in iOS: one window = one session context
Introduce an explicit window/pane model:

```swift
struct TerminalWindow: Identifiable, Equatable {
  let id: UUID
  let workspaceId: String
  var sessionId: String?
  var agentType: AgentType
  var focused: Bool
  var title: String
  var isOpen: Bool
}
```

Replace singular session state (`selectedSessionId`, `focusedSessionId`) usage in the presentation layer with a collection of `TerminalWindow`s and an active-window pointer.

### Required iOS state changes
- Replace single-session assumptions in app state with:
  - `[TerminalWindow]` (active windows)
  - `activeWindowId`
  - per-window `sessionId`
- Keep `setSessionFocus` as **focus hint** (good for collaboration notifications), not as the app’s only session source.

---

## 2) Per-window session lifecycle
Each window must own its own session lifecycle and stream subscription:
- `createWindow()` -> create/load `sessionId` -> `watchSession(window)`
- `sendMessage(window, text)` -> dispatch to selected session for that window
- `switchWindow(window)` -> set active UI focus only
- `closeWindow(window)` -> `unwatchSession(window)` and cancel stream

This is the key difference vs current behavior.

---

## 3) Event routing by session
Demultiplex all incoming session events into the right window by `session_id` in the message handler:
- `session_id` + `workspace_id` identifies destination window
- Unknown/expired session IDs should be routed to an error/rehydrate flow (e.g. auto-refresh sessions)

---

## 4) Backend compatibility
Keep backend API unchanged initially. Verify assumptions first:
- Can a single client maintain multiple active session watches concurrently?
- Do `watch` subscriptions isolate event streams by `session_id`?
- Do events include sufficient routing fields consistently (`session_id`, workspace context)?

If not, introduce a minimal backend enhancement:
- Ensure watch subscription identity is session-specific and supports multiple concurrent subscriptions from one client.

---

## API/Code Change Plan (High level)

### Backend (low risk / verify first)
1. Add integration test: two sessions in same workspace, start two watches, confirm independent events.
2. Add any required guards for multi-watch fanout if concurrency fails.

### iOS (main work)
1. Domain model update
   - Add `TerminalWindow` model and window collection APIs in `AppState`.
2. Repository interface
   - Keep existing session endpoints (`watchSession`, `unwatchSession`, `send`) but add session-scoped context on call sites.
3. ViewModel/UI shell
   - Introduce window manager VM for creation/activation/closure.
   - Maintain one streaming task per window.
4. Event dispatcher
   - Route session events by `session_id`.
5. UX
   - Add multi-tab or split-pane terminal container.
   - Actions: new window, close window, rename/pin title, duplicate session, detach/reopen.
6. Persistence
   - Persist window layout (open session ids, active window) per workspace.
7. Error/reconnect behavior
   - Reconcile stale sessions, auto-refresh list if watch fails/restarts.

---

## Acceptance Criteria
- User can open at least 2 independent terminal windows in same app session.
- Each window streams and sends messages independently.
- Session-switch in one window does not affect another.
- Closing one window cleans up only that stream.
- Focus telemetry (`setSessionFocus`) continues to work without coupling to the only active stream.
- No crash/resource leak after repeated open/close cycles.

---

## Suggested Phases

### Phase 1: Proof of Concept (1-2 days)
- Implement window model + two-window UI + per-window watch/send.
- Keep single workspace first.
- Validate with manual event routing.

### Phase 2: Hardening (2-3 days)
- Persist layout/state
- Reconnect recovery and stale session handling
- Add telemetry/error instrumentation

### Phase 3: Feature polish (1-2 days)
- Add window naming, keyboard shortcuts, quick-swap
- Tune memory and stream lifecycle, add tests

---

## Risks
- If backend watch is still session-serialized per client, client-side multi-window will require backend support.
- Event storms/reconnect churn with many windows (mitigate with per-window cancellable async tasks and bounded buffering).
- Existing UI assumptions around one selected session will need careful incremental refactor to avoid regressions.

---

## Migration Strategy
Do this as an incremental refactor:
1. Keep existing single-window path for fallback.
2. Add terminal window abstraction behind a feature flag.
3. Roll out behind a toggle and switch users only after all tests pass.

## Concrete File-by-File Plan (Implementation Checklist)

## Backend (validate first, then change only if needed)

1. `internal/rpc/handler/methods/session_manager.go`
2. Add integration test for concurrent watches in a single runtime/session workspace, covering two `session/watch` subscriptions from the same client and assert event isolation by `session_id`.
3. Add explicit test around `session/watch`/`unwatch` lifecycle to verify one watch unsubscribe does not cancel another session.

1. `internal/session/manager.go`
2. Confirm watcher registration is keyed per `session_id` (or add map keyed by session when currently keyed by runtime only).
3. Add a test for "unsubscribe one session does not remove remaining subscribers" if code path is shared.

## iOS Domain and State Layer

1. `cdev-ios/App/AppState.swift`
2. Add `TerminalWindow` collection state (`windows`, `activeWindowId`) and persistence fields for per-workspace layout.
3. Add APIs: `openWindow`, `closeWindow`, `activateWindow`, `setWindowSession`.
4. Keep current single-session fields for compatibility, but funnel all new reads through window collection.

1. `cdev-ios/App/SessionAwarenessManager.swift`
2. Move from single focused IDs to window-scoped focus APIs: `setWindowFocus(windowId:sessionId:)`, `clearWindowFocus(windowId:)`.
3. Preserve `setSessionFocus` call path as a compatibility shim used by backend coordination telemetry.

1. `cdev-ios/Domain/Interfaces/AgentRepositoryProtocol.swift`
2. Audit session APIs (`watchSession`, `unwatchSession`, `sendMessage`) for explicit window/session-scoped call semantics.
3. If needed, add non-breaking overloads or wrappers that carry `runtimeId` and `windowId` context for routing.

1. `cdev-ios/Data/Repositories/AgentRepository.swift`
2. Propagate new window context into `watchSession` and `unwatchSession` calls without changing protocol behavior.
3. Ensure repository can route active watchers by runtime/session key and supports parallel watchers.

1. `cdev-ios/Domain/Models/AgentEvent.swift`
2. Confirm session event model has stable `session_id` and `workspace_id` fields for deterministic dispatching.
3. Add derived helper(s) if needed to make routing logic explicit.

1. `cdev-ios/Domain/UseCases/RespondToClaudeUseCase.swift`
2. Ensure message send path accepts explicit target window/session; preserve default current-window behavior.

1. `cdev-ios/Domain/UseCases/DisconnectAgentUseCase.swift`
2. Confirm disconnect/unwatch cleanup can act on specific window/session and does not call global teardown.

## iOS Connection and WebSocket Layer

1. `cdev-ios/Domain/Interfaces/AgentConnectionProtocol.swift`
2. Keep websocket method signatures stable; document per-window watch contract in comments.

1. `cdev-ios/Data/Services/WebSocket/WebSocketService.swift`
2. Replace single `_watchedSessionId/_watchedWorkspaceId/_watchedRuntime` with a window map keyed by `windowId`.
3. Refactor `watchSession` to create/replace only one session for that window, not globally.
4. Refactor `unwatchSession` to cancel only the target window stream and keep others intact.
5. Ensure event forwarder carries window identity or deterministic `session_id` so VM can dispatch safely.

## iOS Presentation and UI Wiring

1. `cdev-ios/Presentation/Screens/Dashboard/DashboardRuntimeCoordinator.swift`
2. Replace singular runtime session bindings with window registry bindings and per-window runtime selection.
3. Route window create/switch/close events into `DashboardViewModel`.

1. `cdev-ios/Presentation/Screens/Dashboard/DashboardViewModel.swift`
2. Replace `watchingSession`, `watchingSessionId`, `userSelectedSessionId` with window registry state.
3. Introduce `startWatching(window:)`, `stopWatching(window:)`, `switchWindow(window:)`, and `sendMessage(window:)`.
4. Route incoming events in `handleEvent` by `session_id` into the matching window message stream/state.
5. Update session-left/joined workarounds to be window-scoped and remove global re-watch hacks.

1. `cdev-ios/Domain/UseCases/ConnectToAgentUseCase.swift`
2. Ensure connect/disconnect sequences can initialize/teardown watchers per window.

1. `cdev-ios/Presentation/Screens/Dashboard/DashboardView.swift`
2. Add terminal window UI pattern (tabs or split panes): create new window, switch, close, and per-window scroll/input binding.
3. Add per-window title/status rendering (`active`, `disconnected`, `no session`).

1. `cdev-ios/Presentation/Screens/Dashboard/DashboardListItem.swift` (if session list is reused)
2. Wire selection and actions to the active window instead of global selected session.

## Recovery, Persistence, and Feature Flag

1. `cdev-ios/Domain/Models/RemoteWorkspace.swift`
2. Add optional `windowState`/`preferredWindowSessions` metadata for startup restore.

1. `cdev-ios/Data/Storage` (or existing persistence directory used by app state)
2. Persist terminal window layout and restore on app relaunch/workspace switch.

1. `cdev-ios/FeatureFlag` definitions (wherever existing flags live)
2. Add feature flag: `multiTerminalEnabled` and gate new screen path.

## Validation & QA

1. Add UI/integration tests around:
2. Independent watch/send/receive for two windows same runtime.
3. Window close does not tear down other window streams.
4. Switching windows does not mutate other window input or history.
5. Reconnect and stale-session recovery for one window while another remains active.

1. Add end-to-end manual script:
2. open workspace, start two windows, send different prompts, confirm responses remain separated.
3. close one window during active streaming, ensure remaining window continues uninterrupted.
4. background/foreground cycle and verify windows rehydrate.

## Rollout Plan

1. Keep old single-window flow untouched behind `multiTerminalEnabled == false`.
2. Release multi-window path as hidden/internal toggle for one cycle.
3. Enable progressively after backend concurrency behavior is verified.

## Progress Update (February 23, 2026)

### Completed (backend validation + compatibility)
- Added backend multi-watch tests in `internal/session/manager_multiwatch_test.go`:
  - concurrent watches from one client across multiple sessions
  - isolated unwatch behavior (removing one session watch does not tear down others)
  - unknown `session_id` unwatch is a safe no-op
- Updated backend unwatch behavior to support targeted unwatch:
  - `workspace/session/unwatch` now accepts optional `session_id`
  - backward compatibility retained when `session_id` is omitted (deterministic legacy behavior)
- Updated OpenRPC metadata in `internal/rpc/handler/methods/session_manager.go`:
  - `workspace/session/watch` description now reflects concurrent watches
  - `workspace/session/unwatch` documents optional `session_id`
- Added contract assertion for the new unwatch parameter in `internal/rpc/handler/methods/session_manager_contract_test.go`.

### Completed (stability fix discovered during validation)
- Fixed a race in `internal/adapters/sessioncache/streamer.go` where watcher channels could be dereferenced after unwatch; watcher channels are now captured once at loop start.

### Completed (iOS foundation - phase start)
- Added terminal window domain/state model in `cdev-ios/cdev/App/AppState.swift`:
  - `TerminalWindow` model
  - `terminalWindows` + `activeTerminalWindowId`
  - window lifecycle APIs (`openTerminalWindow`, `activateTerminalWindow`, `closeTerminalWindow`, `setTerminalWindowSession`, `setTerminalWindowRuntime`)
- Updated iOS watch/unwatch protocol and transport plumbing:
  - `cdev-ios/cdev/Domain/Interfaces/AgentConnectionProtocol.swift` now supports session-targeted unwatch (`sessionId`, `ownerId`) with compatibility overloads.
  - `cdev-ios/cdev/Data/Services/JSONRPC/JSONRPCMethods.swift` unwatch request params now include optional `session_id`.
  - `cdev-ios/cdev/Data/Services/WebSocket/WebSocketService.swift` now tracks watch ownership per owner -> session target, enabling multi-window-safe watch ownership and session-targeted unwatch dispatch.
- Wired Dashboard runtime/session watch flow to active window ownership:
  - `cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift` now derives watch owner IDs from active `TerminalWindow` and syncs window session/runtime context.
  - `cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardRuntimeCoordinator.swift` now requests watch owner ID dynamically during runtime switches.

### Completed (iOS multi-window step: 1 -> 2 -> 3)
- Added terminal window controls in `cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardView.swift`:
  - horizontal window strip
  - create/select/close controls
  - workspace-change window bootstrap
- Added session-id based event routing buffer in `cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift`:
  - session-scoped buffering for inactive windows
  - replay on window activation
  - active-window-aware session filtering for `claude_message` and `pty_output`
- Added close-window targeted cleanup in `cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift`:
  - immediate `unwatchSession(sessionId:ownerId:)` for the closed window owner
  - buffered session cleanup for the closed window
  - active-window handoff after close
- Verified with local iOS build:
  - `xcodebuild -project /Users/brianly/Projects/cdev-ios/cdev.xcodeproj -scheme cdev -destination 'generic/platform=iOS Simulator' build`
  - result: `BUILD SUCCEEDED`

### Completed (iOS multi-window step: per-window in-memory independence)
- Added per-window in-memory state snapshots in `cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift`:
  - window-specific chat/log/message pagination state cache keyed by `windowId`
  - snapshot persist on switch + session/watch state transitions
  - snapshot restore on window activation
- Window activation now prefers in-memory restore path before history API reload:
  - session windows restore immediately and continue streaming/watch flow
  - no-session windows restore/initialize independent empty state
- Close-window cleanup now also removes cached in-memory state for that window.

### Next
- iOS-side architecture refactor from single focused session to per-window session contexts:
  - window model/state in app layer
  - one watch task per window
  - event routing by `session_id`
  - per-window unwatch on close
