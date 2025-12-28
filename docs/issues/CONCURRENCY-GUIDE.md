# Concurrency Guide & Known Issues

This document covers concurrency patterns, known issues, and best practices for the cdev codebase.

---

## Known Issues

### 1. ABBA Deadlock in Session Manager (Fixed Dec 2025)

**Problem**: `session/stop` would hang indefinitely when stopping a LIVE session.

**Root Cause**: Inconsistent lock ordering between two methods:

```
WatchWorkspaceSession: streamerMu.Lock() → m.mu.Lock()
StopSession (old):     m.mu.Lock()       → streamerMu.Lock()
```

When both methods run concurrently:
- Thread A holds `streamerMu`, waiting for `m.mu`
- Thread B holds `m.mu`, waiting for `streamerMu`
- **Deadlock!**

**Fix**: Release `m.mu` before acquiring `streamerMu` in `StopSession`.

---

## Lock Ordering Rules

The session manager has two main mutexes:

| Mutex | Protects |
|-------|----------|
| `m.mu` | `sessions`, `workspaces`, `activeSessions`, `activeSessionWorkspaces` |
| `m.streamerMu` | `streamer`, `streamerSessionID`, `streamerWorkspaceID`, `streamerWatchers` |

**Required Lock Order**: `streamerMu` → `m.mu` (if both needed)

---

## Best Practices

### GOOD: Consistent Lock Ordering

```go
// GOOD: Always acquire streamerMu first, then m.mu
func (m *Manager) WatchWorkspaceSession(...) {
    m.streamerMu.Lock()
    defer m.streamerMu.Unlock()

    // ... do streamer work ...

    // Need to update activeSessions? Acquire m.mu while holding streamerMu
    m.mu.Lock()
    m.activeSessions[workspaceID] = sessionID
    m.mu.Unlock()
}
```

### BAD: Inconsistent Lock Ordering (DEADLOCK RISK)

```go
// BAD: Acquires m.mu first, then streamerMu - opposite order!
func (m *Manager) StopSession(sessionID string) error {
    m.mu.Lock()
    defer m.mu.Unlock()  // Holds m.mu for entire function

    // ... check sessions ...

    // DEADLOCK: If WatchWorkspaceSession holds streamerMu waiting for m.mu
    m.streamerMu.Lock()  // <-- Will block forever!
    // ...
    m.streamerMu.Unlock()
}
```

### GOOD: Release First Lock Before Acquiring Second

```go
// GOOD: Release m.mu before acquiring streamerMu
func (m *Manager) StopSession(sessionID string) error {
    m.mu.Lock()
    session, ok := m.sessions[sessionID]
    if ok {
        err := m.stopSessionInternal(session)
        m.mu.Unlock()  // Release BEFORE returning
        return err
    }

    // Get what we need, then release
    workspaceID := m.findActiveWorkspace(sessionID)
    m.mu.Unlock()  // Release m.mu BEFORE touching streamerMu

    // Now safe to acquire streamerMu
    m.streamerMu.Lock()
    // ...
    m.streamerMu.Unlock()
}
```

### GOOD: Sequential Lock Acquisition (No Nesting)

```go
// GOOD: Acquire and release locks sequentially, never nested
func (m *Manager) stopWatchingLiveSession(workspaceID, sessionID string) {
    // Step 1: Handle streamer (acquire streamerMu, release it)
    m.streamerMu.Lock()
    if m.streamerSessionID == sessionID {
        m.streamer.UnwatchSession()
        m.streamerSessionID = ""
    }
    m.streamerMu.Unlock()  // Released before next lock

    // Step 2: Handle active sessions (acquire m.mu, release it)
    m.mu.Lock()
    delete(m.activeSessions, workspaceID)
    m.mu.Unlock()

    // Step 3: No locks held - safe to publish events
    m.hub.Publish(events.NewSessionStoppedEvent(...))
}
```

### BAD: Nested Locks with Different Order

```go
// BAD: Nested locks in different order than other code paths
func (m *Manager) badExample() {
    m.mu.Lock()
    defer m.mu.Unlock()

    // BAD: Acquiring streamerMu while holding m.mu
    // If another goroutine holds streamerMu and wants m.mu = DEADLOCK
    m.streamerMu.Lock()
    // ... do work ...
    m.streamerMu.Unlock()
}
```

---

## Debugging Deadlocks

### Symptoms
- Request hangs indefinitely (no response, no error)
- Logs show request dispatched but never completed
- CPU usage is low (threads waiting, not spinning)

### How to Diagnose

1. **Check logs**: Look for the last action before hang
   ```
   11:44PM DBG dispatching request method=session/stop
   # No further logs = deadlock
   ```

2. **Send SIGQUIT** to get goroutine dump:
   ```bash
   kill -QUIT <pid>
   ```

3. **Look for "blocked" goroutines** waiting on mutex

### Prevention Checklist

- [ ] Document lock order for each mutex pair
- [ ] Always acquire locks in the same order
- [ ] Prefer releasing locks before acquiring new ones
- [ ] Avoid `defer mu.Unlock()` when you need to release early
- [ ] Review any code that acquires multiple locks

---

## Thread-Safe Patterns Used in cdev

### 1. Copy-on-Read Pattern

```go
// Get a snapshot while holding lock, then work with copy
func (m *Manager) GetWorkspaces() []*Workspace {
    m.mu.RLock()
    result := make([]*Workspace, 0, len(m.workspaces))
    for _, ws := range m.workspaces {
        result = append(result, ws)
    }
    m.mu.RUnlock()
    return result  // Safe to use without lock
}
```

### 2. Check-Then-Act with Lock

```go
// Hold lock for entire check-and-modify operation
func (m *Manager) CreateIfNotExists(id string) *Session {
    m.mu.Lock()
    defer m.mu.Unlock()

    if existing, ok := m.sessions[id]; ok {
        return existing  // Already exists
    }

    session := NewSession(id)
    m.sessions[id] = session
    return session
}
```

### 3. Callback Without Lock

```go
// Release lock before calling external code (callbacks, events)
func (m *Manager) StopAndNotify(sessionID string) {
    m.mu.Lock()
    session := m.sessions[sessionID]
    delete(m.sessions, sessionID)
    m.mu.Unlock()  // Release before callback

    // Safe: no lock held, can't deadlock with callback
    m.hub.Publish(events.SessionStopped{ID: sessionID})
}
```
