# Multi-Agent Dashboard: Quick Start Guide

**For:** Developers implementing the multi-agent feature
**Last Updated:** 2026-01-06

---

## 🚀 Quick Implementation Checklist

### Backend (Go) - Week 1-2

- [ ] **Install dependency:** `go get github.com/shirou/gopsutil/v3`
- [ ] **Update SessionManager** (`internal/session/manager.go`)
  - [ ] Change `activeSessions` from `map[string]string` to `map[string][]string`
  - [ ] Add `sessionMetrics map[string]*SessionMetrics`
- [ ] **Create ResourceMonitor** (`internal/monitoring/resource.go`)
  - [ ] Implement 5-second sampling loop
  - [ ] Track CPU/memory per PID
- [ ] **Create MultiSessionService** (`internal/rpc/handler/methods/multisession.go`)
  - [ ] `aggregate-status` method
  - [ ] `start-batch` method
  - [ ] `stop-all` method
- [ ] **Write tests**
  - [ ] `internal/monitoring/resource_test.go`
  - [ ] `test/integration/multisession_test.go`
- [ ] **Update Swagger:** `make swagger`

---

### iOS (Swift) - Week 3-4

- [ ] **Create models** (`Domain/Models/MultiSessionState.swift`)
  - [ ] `MultiSessionState`
  - [ ] `SessionState`
  - [ ] `AggregateStatus`
- [ ] **Create ViewModel** (`Presentation/Screens/MultiSession/MultiSessionViewModel.swift`)
  - [ ] `refreshAggregateStatus()` method
  - [ ] Event handling for multi-session
  - [ ] 5-second polling timer
- [ ] **Create GridDashboardView** (`Presentation/Screens/MultiSession/GridDashboardView.swift`)
  - [ ] 2-column LazyVGrid
  - [ ] Aggregate status bar
  - [ ] Layout switcher
- [ ] **Create SessionCardView** (`Presentation/Screens/MultiSession/SessionCardView.swift`)
  - [ ] Mini terminal preview
  - [ ] State indicator
  - [ ] Metrics footer
- [ ] **Create BatchStartSheet** (`Presentation/Screens/MultiSession/BatchStartSheet.swift`)
  - [ ] 4 prompt text fields
  - [ ] Max concurrent stepper
  - [ ] Template picker
- [ ] **Add navigation**
  - [ ] Tab bar item or toolbar button
  - [ ] Deep link support

---

## 📁 Files to Create

### Backend (cdev)

```
/Users/brianly/Projects/cdev/
├── internal/
│   ├── monitoring/
│   │   ├── resource.go              [NEW] 200 lines
│   │   └── resource_test.go         [NEW] 150 lines
│   ├── rpc/handler/methods/
│   │   └── multisession.go          [NEW] 400 lines
│   └── session/
│       └── manager.go               [MODIFY] +150 lines
└── test/integration/
    └── multisession_test.go         [NEW] 200 lines
```

**Total Backend:** ~1000 new lines

---

### iOS (cdev-ios)

```
/Users/brianly/Projects/cdev-ios/cdev/
├── Domain/Models/
│   └── MultiSessionState.swift      [NEW] 80 lines
├── Presentation/Screens/MultiSession/
│   ├── MultiSessionViewModel.swift  [NEW] 350 lines
│   ├── GridDashboardView.swift      [NEW] 250 lines
│   ├── SessionCardView.swift        [NEW] 180 lines
│   ├── BatchStartSheet.swift        [NEW] 150 lines
│   └── ResourceGraphView.swift      [NEW] 200 lines
└── Presentation/Components/
    └── AggregateStatusBar.swift     [NEW] 100 lines
```

**Total iOS:** ~1300 new lines

---

## 🧪 Testing Strategy

### Backend Unit Tests

```bash
# Test resource monitoring
go test ./internal/monitoring -v -run TestResourceMonitor

# Test multi-session service
go test ./internal/rpc/handler/methods -v -run TestMultiSession

# Integration test
go test ./test/integration -tags=integration -v -run TestMultiAgent
```

### iOS UI Tests

```swift
// XCTest: Test grid layout
func testGridDashboardLayout() {
    // Given: 4 active sessions
    // When: Open multi-session view
    // Then: 2x2 grid displayed with 4 cards
}

// XCTest: Test batch start
func testBatchSessionStart() {
    // Given: Batch start sheet open
    // When: Fill 3 prompts and tap Start
    // Then: 3 sessions created
}
```

---

## 🎯 Key Metrics to Monitor

### Performance

- **Backend:**
  - CPU overhead of ResourceMonitor: <5%
  - Memory overhead per session: <50MB
  - Aggregate status query time: <100ms
  - Max concurrent sessions: 12 (tested)

- **iOS:**
  - UI update latency: <500ms
  - Grid scroll FPS: 60fps
  - Memory footprint: <150MB total

### User Experience

- Time to start 4 sessions: <10 seconds
- Permission response time: <2 seconds
- Session switching time: <1 second

---

## 🐛 Common Issues & Solutions

### Issue 1: Sessions not appearing in grid

**Symptom:** Grid shows 0 sessions despite sessions running

**Solution:**
```swift
// Check if events include session_id
if let sessionID = event.payload["session_id"] as? String {
    // Filter events by session
}

// Verify WebSocket connection
print("WS State: \(webSocketService.connectionState)")
```

### Issue 2: CPU usage metrics always 0

**Symptom:** ResourceMonitor returns 0% CPU

**Solution:**
```go
// Ensure gopsutil has permissions
proc, err := process.NewProcess(int32(pid))
if err != nil {
    log.Error().Err(err).Msg("Cannot access process")
}

// Call CPUPercent() twice with delay
proc.CPUPercent()  // Ignore first call
time.Sleep(100 * time.Millisecond)
cpuPercent, _ := proc.CPUPercent()  // Use second call
```

### Issue 3: Batch start fails silently

**Symptom:** `start-batch` returns empty array

**Solution:**
```go
// Check max_concurrent limit
if len(activeSessionsForWorkspace) >= cfg.MultiSession.MaxConcurrent {
    return nil, message.ErrRateLimited("max concurrent sessions reached")
}

// Log errors per-prompt
for _, prompt := range prompts {
    if err := startSession(prompt); err != nil {
        log.Error().Err(err).Str("prompt", prompt).Msg("Failed to start")
    }
}
```

---

## 📖 Example Usage

### Backend: Start 4 sessions via RPC

```bash
# Using websocat
echo '{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "multisession/start-batch",
  "params": {
    "workspace_id": "my-workspace",
    "prompts": [
      "Implement user authentication",
      "Add unit tests for UserService",
      "Create REST API for users",
      "Update API documentation"
    ],
    "max_parallel": 4
  }
}' | websocat ws://localhost:8766/ws
```

**Expected Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "started_sessions": [
      "abc-123",
      "def-456",
      "ghi-789",
      "jkl-012"
    ],
    "failed_prompts": []
  }
}
```

---

### iOS: Display grid dashboard

```swift
// In your ContentView or TabView
NavigationLink("Multi-Agent") {
    GridDashboardView(
        viewModel: MultiSessionViewModel(
            webSocketService: container.webSocketService
        )
    )
}

// ViewModel will automatically:
// 1. Poll aggregate-status every 5s
// 2. Listen to real-time events
// 3. Update session cards
```

---

## 🔧 Configuration

### Enable multi-session in cdev

**File:** `/Users/brianly/Projects/cdev/config.yaml`

```yaml
multisession:
  enabled: true
  max_concurrent: 12
  resource_monitoring:
    enabled: true
    sample_interval: 5s
    history_size: 60
```

### Configure iOS layout preference

**File:** `UserDefaults` (Settings screen)

```swift
UserDefaults.standard.set("grid", forKey: "multisession.preferredLayout")
UserDefaults.standard.set(2, forKey: "multisession.gridColumns")
UserDefaults.standard.set(true, forKey: "multisession.showResourceGraphs")
```

---

## 📊 Monitoring & Debugging

### Backend Logs

```bash
# Watch session manager logs
tail -f ~/.cdev/logs/cdev.log | grep "session_manager"

# Watch resource monitor
tail -f ~/.cdev/logs/cdev.log | grep "resource_monitor"

# Watch RPC calls
tail -f ~/.cdev/logs/cdev.log | grep "multisession"
```

### iOS Debug Logs

```swift
// Enable debug logging
AppLogger.log("[MultiSession] Refreshing aggregate status")
AppLogger.log("[MultiSession] Received event: \(event.type)")
AppLogger.log("[MultiSession] Updated session \(sessionID): \(state)")

// View in Xcode console or Settings > Debug Logs
```

---

## 🎓 Learning Resources

### Relevant Code to Study

1. **Existing SessionManager:** `/Users/brianly/Projects/cdev/internal/session/manager.go`
   - Understand how single sessions work
   - See workspace management patterns

2. **Existing DashboardViewModel:** `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift`
   - Learn event handling patterns
   - See WebSocket integration

3. **Resource Monitoring (Example):** `github.com/shirou/gopsutil` docs
   - CPU: `proc.CPUPercent()`
   - Memory: `proc.MemoryInfo()`

### External References

- **JSON-RPC 2.0 Spec:** https://www.jsonrpc.org/specification
- **SwiftUI Charts:** https://developer.apple.com/documentation/charts
- **gopsutil Docs:** https://pkg.go.dev/github.com/shirou/gopsutil/v3

---

## ✅ Definition of Done

### Sprint 1 (Backend)

- [ ] `multisession/aggregate-status` returns correct data for 4+ sessions
- [ ] `multisession/start-batch` starts sessions in parallel (verified with logs)
- [ ] ResourceMonitor samples CPU/memory without errors
- [ ] Unit tests pass with >80% coverage
- [ ] Integration test runs 6 concurrent sessions successfully

### Sprint 2 (iOS)

- [ ] GridDashboardView displays 2x2 grid
- [ ] SessionCardView shows state, logs, metrics
- [ ] Tapping card switches to full-screen view
- [ ] BatchStartSheet starts 4 sessions
- [ ] UI updates in <500ms on event receive
- [ ] No memory leaks (Instruments verification)

### Sprint 3 (Polish)

- [ ] Unified permission panel works across sessions
- [ ] Resource graphs display 5-minute history
- [ ] Session templates save/load
- [ ] All animations smooth (60fps)
- [ ] User acceptance testing passed

---

**Ready to implement? Start with Sprint 1, Week 1! 🚀**

---

## 📞 Support

- Review full spec: `docs/planning/MULTI-AGENT-IMPLEMENTATION-SPEC.md`
- Architecture diagrams: `docs/planning/ARCHITECTURE-DIAGRAMS.md`
- Questions? Check existing session management code first!

---

*Last Updated: 2026-01-06*
