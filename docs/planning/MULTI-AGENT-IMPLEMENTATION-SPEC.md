# Multi-Agent Parallel Execution Dashboard
## Complete Implementation Specification

**Document Version:** 1.0.0
**Created:** 2026-01-06
**Status:** Design Phase
**Estimated Effort:** 80-100 hours (2-3 sprints)

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Deep Dive: Multi-Agent Dashboard Architecture](#1-deep-dive-multi-agent-dashboard)
3. [Sprint Planning: Detailed Technical Specs](#2-sprint-planning)
4. [Auto-Claude Architecture Analysis](#3-auto-claude-analysis)
5. [Mobile UI Mockups & Design](#4-mobile-ui-design)
6. [Implementation Roadmap](#5-implementation-roadmap)

---

## Executive Summary

This document provides a complete technical specification for implementing **parallel multi-agent execution** in cdev/cdev-ios, inspired by Auto-Claude's ability to run up to 12 concurrent AI agents. The implementation will enable:

- **Simultaneous supervision** of 2-6 Claude sessions from a single iOS device
- **Aggregate status monitoring** across all active agents
- **Quick-switch navigation** between sessions
- **Resource monitoring** (CPU, memory, token usage per agent)
- **Unified permission management** across multiple agents

**Key Differentiator:** Unlike Auto-Claude's autonomous batch execution, cdev maintains **real-time interactive supervision** while enabling parallelism.

---

## 1. DEEP DIVE: Multi-Agent Dashboard

### 1.1 Current Architecture Limitations

#### Backend (cdev)
**File:** `/Users/brianly/Projects/cdev/internal/session/manager.go`

**Current State:**
```go
type Manager struct {
    sessions                map[string]*Session             // keyed by session ID
    workspaces              map[string]*workspace.Workspace // keyed by workspace ID
    activeSessions          map[string]string               // workspace ID -> ONE active session ID
    activeSessionWorkspaces map[string]string               // session ID -> workspace ID
    // ...
}
```

**Limitation:** The `activeSessions` map enforces **one active session per workspace**. To support multi-agent execution, we need:
1. Support multiple concurrent sessions per workspace
2. Aggregate status tracking across sessions
3. Resource monitoring per session
4. Unified event broadcasting with session context

#### iOS (cdev-ios)
**File:** `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift`

**Current State:**
```swift
@Published var claudeState: ClaudeState = .idle
@Published var logs: [LogEntry] = []
@Published var chatElements: [ChatElement] = []
```

**Limitation:** Single-session UI - one terminal view, one chat view, one state tracker. To support multi-agent, we need:
1. Multi-session state management
2. Grid/split-screen layout
3. Aggregate status indicators
4. Session-aware event filtering

---

### 1.2 Proposed Backend Architecture

#### 1.2.1 Session Manager Enhancements

**File to Modify:** `/Users/brianly/Projects/cdev/internal/session/manager.go`

```go
// NEW: Support multiple concurrent sessions per workspace
type Manager struct {
    sessions                map[string]*Session             // session ID -> session
    workspaces              map[string]*workspace.Workspace // workspace ID -> workspace

    // CHANGED: Support multiple active sessions per workspace
    activeSessions          map[string][]string             // workspace ID -> []session IDs
    activeSessionWorkspaces map[string]string               // session ID -> workspace ID (unchanged)

    // NEW: Aggregate metrics
    sessionMetrics          map[string]*SessionMetrics      // session ID -> metrics

    // NEW: Resource monitor
    resourceMonitor         *ResourceMonitor

    // ... existing fields
}

// NEW: Per-session metrics
type SessionMetrics struct {
    SessionID       string
    WorkspaceID     string
    StartTime       time.Time
    LastActivity    time.Time
    MessageCount    int
    TokensUsed      int64
    PermissionsAsked    int
    PermissionsApproved int
    CPUUsagePercent     float64
    MemoryUsageMB       float64
    State               events.ClaudeState
}

// NEW: Resource monitoring
type ResourceMonitor struct {
    mu      sync.RWMutex
    samples map[string][]ResourceSample  // session ID -> samples
}

type ResourceSample struct {
    Timestamp       time.Time
    CPUPercent      float64
    MemoryMB        float64
    ProcessPID      int
}
```

#### 1.2.2 New RPC Methods

**File to Create:** `/Users/brianly/Projects/cdev/internal/rpc/handler/methods/multisession.go`

```go
package methods

import (
    "context"
    "encoding/json"
    "github.com/brianly1003/cdev/internal/rpc/handler"
    "github.com/brianly1003/cdev/internal/rpc/message"
)

// MultiSessionService handles multi-session orchestration
type MultiSessionService struct {
    sessionManager *session.Manager
}

// AggregateStatusParams for multisession/aggregate-status
type AggregateStatusParams struct {
    WorkspaceID string `json:"workspace_id,omitempty"`
}

// AggregateStatusResult aggregates status across all sessions
type AggregateStatusResult struct {
    TotalSessions    int                  `json:"total_sessions"`
    RunningCount     int                  `json:"running_count"`
    WaitingCount     int                  `json:"waiting_count"`
    IdleCount        int                  `json:"idle_count"`
    ErrorCount       int                  `json:"error_count"`
    Sessions         []SessionStatusItem  `json:"sessions"`
    AggregateMetrics AggregateMetrics     `json:"aggregate_metrics"`
}

type SessionStatusItem struct {
    SessionID       string             `json:"session_id"`
    WorkspaceID     string             `json:"workspace_id"`
    State           string             `json:"state"`
    CurrentPrompt   string             `json:"current_prompt,omitempty"`
    StartTime       string             `json:"start_time"`
    LastActivity    string             `json:"last_activity"`
    MessageCount    int                `json:"message_count"`
    TokensUsed      int64              `json:"tokens_used"`
    CPUPercent      float64            `json:"cpu_percent"`
    MemoryMB        float64            `json:"memory_mb"`
    PID             int                `json:"pid,omitempty"`
}

type AggregateMetrics struct {
    TotalMessages       int     `json:"total_messages"`
    TotalTokens         int64   `json:"total_tokens"`
    TotalCPUPercent     float64 `json:"total_cpu_percent"`
    TotalMemoryMB       float64 `json:"total_memory_mb"`
    AvgSessionDuration  float64 `json:"avg_session_duration_seconds"`
}

// RegisterMethods registers multi-session methods
func (s *MultiSessionService) RegisterMethods(r *handler.Registry) {
    r.RegisterWithMeta("multisession/aggregate-status", s.AggregateStatus, handler.MethodMeta{
        Summary: "Get aggregate status across all sessions",
        Description: "Returns aggregated metrics and status for all active sessions, optionally filtered by workspace.",
        Params: []handler.OpenRPCParam{
            {
                Name: "workspace_id",
                Description: "Filter by workspace ID (optional, omit for all workspaces)",
                Required: false,
                Schema: map[string]interface{}{"type": "string"},
            },
        },
        Result: &handler.OpenRPCResult{
            Name: "AggregateStatusResult",
            Schema: map[string]interface{}{"$ref": "#/components/schemas/AggregateStatusResult"},
        },
    })

    r.RegisterWithMeta("multisession/start-batch", s.StartBatch, handler.MethodMeta{
        Summary: "Start multiple sessions in parallel",
        Description: "Starts multiple Claude sessions with different prompts simultaneously.",
        Params: []handler.OpenRPCParam{
            {Name: "workspace_id", Description: "Workspace to start sessions in", Required: true, Schema: map[string]interface{}{"type": "string"}},
            {Name: "prompts", Description: "Array of prompts (one per session)", Required: true, Schema: map[string]interface{}{"type": "array"}},
            {Name: "max_parallel", Description: "Max concurrent sessions (default 4)", Required: false, Schema: map[string]interface{}{"type": "integer"}},
        },
    })

    r.RegisterWithMeta("multisession/stop-all", s.StopAll, handler.MethodMeta{
        Summary: "Stop all sessions in workspace",
        Description: "Stops all active sessions in a workspace or globally.",
        Params: []handler.OpenRPCParam{
            {Name: "workspace_id", Description: "Workspace ID (optional, omit for all)", Required: false, Schema: map[string]interface{}{"type": "string"}},
        },
    })
}

// AggregateStatus returns aggregate status across sessions
func (s *MultiSessionService) AggregateStatus(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
    var p AggregateStatusParams
    if params != nil {
        if err := json.Unmarshal(params, &p); err != nil {
            return nil, message.ErrInvalidParams("invalid params: " + err.Error())
        }
    }

    // Get all sessions (or filtered by workspace)
    sessions := s.sessionManager.GetActiveSessions(p.WorkspaceID)

    result := AggregateStatusResult{
        TotalSessions: len(sessions),
        Sessions:      make([]SessionStatusItem, 0, len(sessions)),
    }

    var totalMessages int
    var totalTokens int64
    var totalCPU, totalMemory float64
    var totalDuration float64

    for _, sess := range sessions {
        metrics := s.sessionManager.GetSessionMetrics(sess.ID)

        item := SessionStatusItem{
            SessionID:    sess.ID,
            WorkspaceID:  sess.WorkspaceID,
            State:        string(sess.State),
            CurrentPrompt: sess.CurrentPrompt,
            StartTime:    sess.StartTime.Format(time.RFC3339),
            LastActivity: metrics.LastActivity.Format(time.RFC3339),
            MessageCount: metrics.MessageCount,
            TokensUsed:   metrics.TokensUsed,
            CPUPercent:   metrics.CPUUsagePercent,
            MemoryMB:     metrics.MemoryUsageMB,
            PID:          sess.PID,
        }

        result.Sessions = append(result.Sessions, item)

        // Count by state
        switch sess.State {
        case events.ClaudeStateRunning:
            result.RunningCount++
        case events.ClaudeStateWaiting:
            result.WaitingCount++
        case events.ClaudeStateIdle:
            result.IdleCount++
        case events.ClaudeStateError:
            result.ErrorCount++
        }

        // Aggregate metrics
        totalMessages += metrics.MessageCount
        totalTokens += metrics.TokensUsed
        totalCPU += metrics.CPUUsagePercent
        totalMemory += metrics.MemoryUsageMB
        totalDuration += time.Since(sess.StartTime).Seconds()
    }

    result.AggregateMetrics = AggregateMetrics{
        TotalMessages:   totalMessages,
        TotalTokens:     totalTokens,
        TotalCPUPercent: totalCPU,
        TotalMemoryMB:   totalMemory,
    }

    if len(sessions) > 0 {
        result.AggregateMetrics.AvgSessionDuration = totalDuration / float64(len(sessions))
    }

    return result, nil
}

// StartBatchParams for starting multiple sessions
type StartBatchParams struct {
    WorkspaceID string   `json:"workspace_id"`
    Prompts     []string `json:"prompts"`
    MaxParallel int      `json:"max_parallel,omitempty"`
}

type StartBatchResult struct {
    StartedSessions []string `json:"started_sessions"`
    FailedPrompts   []string `json:"failed_prompts,omitempty"`
}

// StartBatch starts multiple sessions in parallel
func (s *MultiSessionService) StartBatch(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
    var p StartBatchParams
    if err := json.Unmarshal(params, &p); err != nil {
        return nil, message.ErrInvalidParams("invalid params: " + err.Error())
    }

    if p.WorkspaceID == "" {
        return nil, message.ErrInvalidParams("workspace_id is required")
    }

    if len(p.Prompts) == 0 {
        return nil, message.ErrInvalidParams("prompts array is required")
    }

    maxParallel := p.MaxParallel
    if maxParallel <= 0 {
        maxParallel = 4 // Default: 4 concurrent sessions
    }
    if maxParallel > 12 {
        maxParallel = 12 // Max: 12 (like Auto-Claude)
    }

    result := StartBatchResult{
        StartedSessions: make([]string, 0),
        FailedPrompts:   make([]string, 0),
    }

    // Use semaphore to limit parallelism
    sem := make(chan struct{}, maxParallel)
    var wg sync.WaitGroup
    var mu sync.Mutex

    for i, prompt := range p.Prompts {
        wg.Add(1)
        go func(index int, prompt string) {
            defer wg.Done()

            // Acquire semaphore
            sem <- struct{}{}
            defer func() { <-sem }()

            // Start session
            sessionID, err := s.sessionManager.StartSession(ctx, p.WorkspaceID, prompt, "new")

            mu.Lock()
            if err != nil {
                result.FailedPrompts = append(result.FailedPrompts, prompt)
            } else {
                result.StartedSessions = append(result.StartedSessions, sessionID)
            }
            mu.Unlock()
        }(i, prompt)
    }

    wg.Wait()

    return result, nil
}
```

#### 1.2.3 Resource Monitoring Implementation

**File to Create:** `/Users/brianly/Projects/cdev/internal/monitoring/resource.go`

```go
package monitoring

import (
    "context"
    "sync"
    "time"
    "github.com/shirou/gopsutil/v3/process"
)

// ResourceMonitor tracks CPU and memory usage per session
type ResourceMonitor struct {
    mu          sync.RWMutex
    samples     map[string]*SessionResources  // session ID -> resources
    sampleInterval time.Duration
    ctx         context.Context
    cancel      context.CancelFunc
}

type SessionResources struct {
    SessionID      string
    PID            int
    CurrentCPU     float64
    CurrentMemory  float64
    PeakCPU        float64
    PeakMemory     float64
    SampleHistory  []ResourceSample
    LastUpdate     time.Time
}

type ResourceSample struct {
    Timestamp  time.Time
    CPUPercent float64
    MemoryMB   float64
}

func NewResourceMonitor(sampleInterval time.Duration) *ResourceMonitor {
    ctx, cancel := context.WithCancel(context.Background())

    rm := &ResourceMonitor{
        samples:        make(map[string]*SessionResources),
        sampleInterval: sampleInterval,
        ctx:            ctx,
        cancel:         cancel,
    }

    go rm.monitorLoop()
    return rm
}

// RegisterSession starts monitoring a session's process
func (rm *ResourceMonitor) RegisterSession(sessionID string, pid int) {
    rm.mu.Lock()
    defer rm.mu.Unlock()

    rm.samples[sessionID] = &SessionResources{
        SessionID:     sessionID,
        PID:           pid,
        SampleHistory: make([]ResourceSample, 0, 60), // Keep last 60 samples
        LastUpdate:    time.Now(),
    }
}

// UnregisterSession stops monitoring a session
func (rm *ResourceMonitor) UnregisterSession(sessionID string) {
    rm.mu.Lock()
    defer rm.mu.Unlock()
    delete(rm.samples, sessionID)
}

// GetMetrics returns current metrics for a session
func (rm *ResourceMonitor) GetMetrics(sessionID string) *SessionResources {
    rm.mu.RLock()
    defer rm.mu.RUnlock()

    if res, ok := rm.samples[sessionID]; ok {
        // Return a copy to avoid race conditions
        return &SessionResources{
            SessionID:     res.SessionID,
            PID:           res.PID,
            CurrentCPU:    res.CurrentCPU,
            CurrentMemory: res.CurrentMemory,
            PeakCPU:       res.PeakCPU,
            PeakMemory:    res.PeakMemory,
            LastUpdate:    res.LastUpdate,
        }
    }
    return nil
}

// monitorLoop samples resources periodically
func (rm *ResourceMonitor) monitorLoop() {
    ticker := time.NewTicker(rm.sampleInterval)
    defer ticker.Stop()

    for {
        select {
        case <-rm.ctx.Done():
            return
        case <-ticker.C:
            rm.sampleAllSessions()
        }
    }
}

// sampleAllSessions collects metrics for all registered sessions
func (rm *ResourceMonitor) sampleAllSessions() {
    rm.mu.Lock()
    defer rm.mu.Unlock()

    for sessionID, res := range rm.samples {
        // Get process metrics
        proc, err := process.NewProcess(int32(res.PID))
        if err != nil {
            continue // Process may have terminated
        }

        cpuPercent, err := proc.CPUPercent()
        if err != nil {
            continue
        }

        memInfo, err := proc.MemoryInfo()
        if err != nil {
            continue
        }

        memoryMB := float64(memInfo.RSS) / 1024 / 1024

        // Update current values
        res.CurrentCPU = cpuPercent
        res.CurrentMemory = memoryMB
        res.LastUpdate = time.Now()

        // Track peaks
        if cpuPercent > res.PeakCPU {
            res.PeakCPU = cpuPercent
        }
        if memoryMB > res.PeakMemory {
            res.PeakMemory = memoryMB
        }

        // Add to history
        sample := ResourceSample{
            Timestamp:  time.Now(),
            CPUPercent: cpuPercent,
            MemoryMB:   memoryMB,
        }

        res.SampleHistory = append(res.SampleHistory, sample)

        // Keep only last 60 samples (5 minutes at 5s interval)
        if len(res.SampleHistory) > 60 {
            res.SampleHistory = res.SampleHistory[1:]
        }
    }
}

// Stop stops the resource monitor
func (rm *ResourceMonitor) Stop() {
    rm.cancel()
}
```

**Dependencies to Add:**
```bash
go get github.com/shirou/gopsutil/v3
```

---

### 1.3 Proposed iOS Architecture

#### 1.3.1 Multi-Session State Management

**File to Create:** `/Users/brianly/Projects/cdev-ios/cdev/Domain/Models/MultiSessionState.swift`

```swift
import Foundation

/// Manages state for multiple concurrent sessions
@MainActor
class MultiSessionState: ObservableObject {
    /// All active sessions
    @Published var sessions: [String: SessionState] = [:]

    /// Aggregate status across all sessions
    @Published var aggregateStatus: AggregateStatus = AggregateStatus()

    /// Currently focused session (for full-screen view)
    @Published var focusedSessionID: String?

    /// Layout mode
    @Published var layoutMode: LayoutMode = .grid

    enum LayoutMode {
        case single      // One full-screen session
        case grid        // 2x2 or 3x2 grid
        case list        // Vertical list
        case splitView   // Side-by-side (iPad)
    }
}

/// State for a single session in multi-session view
struct SessionState: Identifiable {
    let id: String  // session_id
    var workspaceID: String
    var state: ClaudeState
    var currentPrompt: String?
    var startTime: Date
    var lastActivity: Date
    var messageCount: Int
    var tokensUsed: Int64
    var cpuPercent: Double
    var memoryMB: Double
    var pid: Int?
    var logs: [LogEntry]  // Last N logs
    var hasPermission: Bool
    var isSelected: Bool
}

/// Aggregate metrics across all sessions
struct AggregateStatus {
    var totalSessions: Int = 0
    var runningCount: Int = 0
    var waitingCount: Int = 0
    var idleCount: Int = 0
    var errorCount: Int = 0
    var totalMessages: Int = 0
    var totalTokens: Int64 = 0
    var totalCPUPercent: Double = 0
    var totalMemoryMB: Double = 0
    var avgSessionDuration: Double = 0
}
```

#### 1.3.2 Multi-Session ViewModel

**File to Create:** `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/MultiSessionViewModel.swift`

```swift
import Foundation
import Combine

@MainActor
final class MultiSessionViewModel: ObservableObject {
    // MARK: - Published State

    @Published var multiSessionState = MultiSessionState()
    @Published var isLoading: Bool = false
    @Published var error: AppError?

    // Layout controls
    @Published var gridColumns: Int = 2  // 2 or 3 columns
    @Published var showResourceGraphs: Bool = false

    // Batch operations
    @Published var showBatchStartSheet: Bool = false
    @Published var batchPrompts: [String] = ["", "", "", ""]  // Template for 4 prompts

    // MARK: - Dependencies

    private let webSocketService: WebSocketServiceProtocol
    private let workspaceManager = WorkspaceManagerService.shared

    private var eventTask: Task<Void, Never>?
    private var refreshTimer: Timer?

    // MARK: - Initialization

    init(webSocketService: WebSocketServiceProtocol) {
        self.webSocketService = webSocketService
        startMonitoring()
    }

    // MARK: - Public Methods

    /// Refresh aggregate status from server
    func refreshAggregateStatus() async {
        isLoading = true
        defer { isLoading = false }

        do {
            let params = AggregateStatusParams(workspace_id: nil)  // All workspaces
            let response: AggregateStatusResponse = try await webSocketService.sendRequest(
                method: "multisession/aggregate-status",
                params: params
            )

            // Update aggregate status
            multiSessionState.aggregateStatus = AggregateStatus(
                totalSessions: response.total_sessions,
                runningCount: response.running_count,
                waitingCount: response.waiting_count,
                idleCount: response.idle_count,
                errorCount: response.error_count,
                totalMessages: response.aggregate_metrics.total_messages,
                totalTokens: response.aggregate_metrics.total_tokens,
                totalCPUPercent: response.aggregate_metrics.total_cpu_percent,
                totalMemoryMB: response.aggregate_metrics.total_memory_mb,
                avgSessionDuration: response.aggregate_metrics.avg_session_duration_seconds
            )

            // Update individual session states
            for sessionItem in response.sessions {
                let sessionState = SessionState(
                    id: sessionItem.session_id,
                    workspaceID: sessionItem.workspace_id,
                    state: ClaudeState(rawValue: sessionItem.state) ?? .idle,
                    currentPrompt: sessionItem.current_prompt,
                    startTime: ISO8601DateFormatter().date(from: sessionItem.start_time) ?? Date(),
                    lastActivity: ISO8601DateFormatter().date(from: sessionItem.last_activity) ?? Date(),
                    messageCount: sessionItem.message_count,
                    tokensUsed: sessionItem.tokens_used,
                    cpuPercent: sessionItem.cpu_percent,
                    memoryMB: sessionItem.memory_mb,
                    pid: sessionItem.pid,
                    logs: multiSessionState.sessions[sessionItem.session_id]?.logs ?? [],
                    hasPermission: false,
                    isSelected: multiSessionState.focusedSessionID == sessionItem.session_id
                )

                multiSessionState.sessions[sessionItem.session_id] = sessionState
            }

        } catch {
            self.error = .serverUnreachable
        }
    }

    /// Start multiple sessions with batch prompts
    func startBatchSessions(prompts: [String]) async {
        guard !prompts.isEmpty else { return }

        isLoading = true
        defer { isLoading = false }

        do {
            let workspaceID = WorkspaceStore.shared.activeWorkspace?.remoteWorkspaceId ?? ""
            let params = StartBatchParams(
                workspace_id: workspaceID,
                prompts: prompts.filter { !$0.isEmpty },
                max_parallel: 4
            )

            let response: StartBatchResponse = try await webSocketService.sendRequest(
                method: "multisession/start-batch",
                params: params
            )

            AppLogger.log("[MultiSession] Started \(response.started_sessions.count) sessions")

            // Refresh to get new session states
            await refreshAggregateStatus()

        } catch {
            self.error = .serverUnreachable
        }
    }

    /// Stop all sessions in workspace
    func stopAllSessions() async {
        isLoading = true
        defer { isLoading = false }

        do {
            let workspaceID = WorkspaceStore.shared.activeWorkspace?.remoteWorkspaceId
            let params = StopAllParams(workspace_id: workspaceID)

            try await webSocketService.sendRequest(
                method: "multisession/stop-all",
                params: params
            )

            multiSessionState.sessions.removeAll()

        } catch {
            self.error = .serverUnreachable
        }
    }

    /// Focus on a specific session (switch to single view)
    func focusSession(_ sessionID: String) {
        multiSessionState.focusedSessionID = sessionID
        multiSessionState.layoutMode = .single
    }

    /// Switch layout mode
    func switchLayout(_ mode: MultiSessionState.LayoutMode) {
        multiSessionState.layoutMode = mode
    }

    // MARK: - Private Methods

    private func startMonitoring() {
        // Refresh every 5 seconds
        refreshTimer = Timer.scheduledTimer(withTimeInterval: 5.0, repeats: true) { [weak self] _ in
            Task { @MainActor in
                await self?.refreshAggregateStatus()
            }
        }

        // Listen to real-time events
        eventTask = Task {
            for await event in webSocketService.eventStream {
                await handleEvent(event)
            }
        }
    }

    private func handleEvent(_ event: WebSocketEvent) async {
        switch event.type {
        case "claude_log":
            // Update logs for specific session
            if let sessionID = event.payload["session_id"] as? String,
               let line = event.payload["line"] as? String {
                updateSessionLog(sessionID: sessionID, line: line)
            }

        case "claude_status":
            // Update session state
            if let sessionID = event.payload["session_id"] as? String,
               let state = event.payload["state"] as? String {
                updateSessionState(sessionID: sessionID, state: state)
            }

        case "pty_permission":
            // Mark session as waiting for permission
            if let sessionID = event.payload["session_id"] as? String {
                markSessionHasPermission(sessionID: sessionID)
            }

        default:
            break
        }
    }

    private func updateSessionLog(sessionID: String, line: String) {
        guard var session = multiSessionState.sessions[sessionID] else { return }

        let logEntry = LogEntry(
            id: UUID().uuidString,
            line: line,
            timestamp: Date(),
            stream: .stdout
        )

        session.logs.append(logEntry)

        // Keep only last 100 logs per session
        if session.logs.count > 100 {
            session.logs = Array(session.logs.suffix(100))
        }

        multiSessionState.sessions[sessionID] = session
    }

    private func updateSessionState(sessionID: String, state: String) {
        guard var session = multiSessionState.sessions[sessionID] else { return }
        session.state = ClaudeState(rawValue: state) ?? .idle
        session.lastActivity = Date()
        multiSessionState.sessions[sessionID] = session
    }

    private func markSessionHasPermission(sessionID: String) {
        guard var session = multiSessionState.sessions[sessionID] else { return }
        session.hasPermission = true
        multiSessionState.sessions[sessionID] = session
    }

    deinit {
        refreshTimer?.invalidate()
        eventTask?.cancel()
    }
}

// MARK: - Request/Response Types

struct AggregateStatusParams: Codable {
    let workspace_id: String?
}

struct AggregateStatusResponse: Codable {
    let total_sessions: Int
    let running_count: Int
    let waiting_count: Int
    let idle_count: Int
    let error_count: Int
    let sessions: [SessionStatusItem]
    let aggregate_metrics: AggregateMetrics
}

struct SessionStatusItem: Codable {
    let session_id: String
    let workspace_id: String
    let state: String
    let current_prompt: String?
    let start_time: String
    let last_activity: String
    let message_count: Int
    let tokens_used: Int64
    let cpu_percent: Double
    let memory_mb: Double
    let pid: Int?
}

struct AggregateMetrics: Codable {
    let total_messages: Int
    let total_tokens: Int64
    let total_cpu_percent: Double
    let total_memory_mb: Double
    let avg_session_duration_seconds: Double
}

struct StartBatchParams: Codable {
    let workspace_id: String
    let prompts: [String]
    let max_parallel: Int
}

struct StartBatchResponse: Codable {
    let started_sessions: [String]
    let failed_prompts: [String]?
}

struct StopAllParams: Codable {
    let workspace_id: String?
}
```

---

## 2. SPRINT PLANNING

### Sprint 1: Backend Foundation (2 weeks)

#### Week 1: Multi-Session Manager

**Goal:** Enable backend to track multiple concurrent sessions per workspace

**Tasks:**

| Task | File | Effort | Assignee |
|------|------|--------|----------|
| Update SessionManager to support multiple active sessions | `/Users/brianly/Projects/cdev/internal/session/manager.go` | 6h | Backend |
| Add SessionMetrics struct and tracking | `/Users/brianly/Projects/cdev/internal/session/manager.go` | 4h | Backend |
| Implement ResourceMonitor | `/Users/brianly/Projects/cdev/internal/monitoring/resource.go` | 8h | Backend |
| Add gopsutil dependency | `go.mod` | 1h | Backend |
| Unit tests for ResourceMonitor | `/Users/brianly/Projects/cdev/internal/monitoring/resource_test.go` | 4h | Backend |

**Total Effort:** 23 hours

**Acceptance Criteria:**
- [ ] SessionManager allows multiple active sessions per workspace
- [ ] SessionMetrics tracks message count, tokens, CPU, memory
- [ ] ResourceMonitor samples process metrics every 5 seconds
- [ ] Tests cover edge cases (process termination, no permission, etc.)

---

#### Week 2: RPC Methods & Events

**Goal:** Expose multi-session APIs for iOS consumption

**Tasks:**

| Task | File | Effort | Assignee |
|------|------|--------|----------|
| Create MultiSessionService | `/Users/brianly/Projects/cdev/internal/rpc/handler/methods/multisession.go` | 8h | Backend |
| Implement multisession/aggregate-status | same | 4h | Backend |
| Implement multisession/start-batch | same | 6h | Backend |
| Implement multisession/stop-all | same | 2h | Backend |
| Add OpenRPC schemas | `/Users/brianly/Projects/cdev/internal/rpc/handler/schemas.go` | 2h | Backend |
| Integration tests | `/Users/brianly/Projects/cdev/test/integration/multisession_test.go` | 6h | Backend |
| Update Swagger docs | Run `make swagger` | 1h | Backend |

**Total Effort:** 29 hours

**Acceptance Criteria:**
- [ ] `multisession/aggregate-status` returns all active sessions with metrics
- [ ] `multisession/start-batch` starts up to 12 sessions concurrently
- [ ] `multisession/stop-all` gracefully stops all sessions in workspace
- [ ] OpenRPC discovery includes new methods
- [ ] Integration tests verify concurrent session execution

---

### Sprint 2: iOS Multi-Session UI (2 weeks)

#### Week 3: State Management & Grid Layout

**Goal:** Build iOS infrastructure for multi-session display

**Tasks:**

| Task | File | Effort | Assignee |
|------|------|--------|----------|
| Create MultiSessionState model | `/Users/brianly/Projects/cdev-ios/cdev/Domain/Models/MultiSessionState.swift` | 4h | iOS |
| Create MultiSessionViewModel | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/MultiSessionViewModel.swift` | 8h | iOS |
| Implement GridDashboardView (2x2 layout) | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/GridDashboardView.swift` | 10h | iOS |
| Create SessionCardView (mini terminal) | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/SessionCardView.swift` | 6h | iOS |
| Add layout switcher (grid/list/single) | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/LayoutSwitcher.swift` | 4h | iOS |

**Total Effort:** 32 hours

**UI Components:**
```
GridDashboardView
├─ AggregateStatusBar (top)
├─ LazyVGrid (2 columns)
│  ├─ SessionCardView (session 1)
│  ├─ SessionCardView (session 2)
│  ├─ SessionCardView (session 3)
│  └─ SessionCardView (session 4)
└─ FloatingActionButton (+)
```

---

#### Week 4: Batch Start & Polish

**Goal:** Enable batch session starting and polish UX

**Tasks:**

| Task | File | Effort | Assignee |
|------|------|--------|----------|
| Create BatchStartSheet | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/BatchStartSheet.swift` | 6h | iOS |
| Implement resource graphs (CPU/memory) | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Components/ResourceGraph.swift` | 8h | iOS |
| Add session card tap → full screen | `MultiSessionViewModel.swift` | 4h | iOS |
| Implement stop-all confirmation dialog | `MultiSessionView.swift` | 2h | iOS |
| Add session filtering (by state) | `MultiSessionViewModel.swift` | 4h | iOS |
| Polish animations & transitions | All views | 6h | iOS |

**Total Effort:** 30 hours

---

### Sprint 3: Advanced Features (1-2 weeks)

#### Week 5: Permission Management & Analytics

**Tasks:**

| Task | File | Effort | Assignee |
|------|------|--------|----------|
| Unified permission panel (multi-session) | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/UnifiedPermissionPanel.swift` | 8h | iOS |
| Session comparison view | `/Users/brianly/Projects/cdev-ios/cdev/Presentation/Screens/MultiSession/SessionComparisonView.swift` | 6h | iOS |
| Export aggregate metrics (JSON/CSV) | `MultiSessionViewModel.swift` | 4h | iOS |
| Session templates (save/load prompts) | `/Users/brianly/Projects/cdev-ios/cdev/Data/Storage/SessionTemplateStore.swift` | 6h | iOS |

**Total Effort:** 24 hours

---

## 3. AUTO-CLAUDE ANALYSIS

### 3.1 What We Can Learn Without Source Access

Based on the GitHub repository structure, README, and package.json analysis:

#### 3.1.1 Architectural Patterns

**Electron + Python Monorepo**
```
auto-claude/
├── apps/
│   └── frontend/          # Electron/React frontend
├── backend/               # Python agents & orchestration
│   ├── agents/           # AI agent implementations
│   ├── spec_runner.py    # Task specification interpreter
│   ├── run.py            # Main execution engine
│   └── qa_pipeline/      # Quality assurance logic
└── package.json          # npm workspaces
```

**Key Insights:**
1. **Separation of Concerns:** UI (Electron) completely decoupled from execution (Python)
2. **Specification-Driven:** Tasks defined as specs, agents execute autonomously
3. **Built-in QA:** Validation layer before human review
4. **Git Worktree Isolation:** Each agent gets isolated workspace

#### 3.1.2 Execution Flow (Inferred)

```
User → Spec Definition (spec_runner.py)
         ↓
    Task Queue (FIFO or priority-based)
         ↓
    Agent Pool (max 12 concurrent)
         ↓
    Git Worktree Creation (isolated workspace)
         ↓
    Claude API Calls (autonomous execution)
         ↓
    QA Pipeline (tests, lints, type checks)
         ↓
    Review UI (show results to user)
         ↓
    Merge to Main (if approved)
```

#### 3.1.3 Electron Frontend Patterns

From `package.json`:
```json
{
  "scripts": {
    "dev": "...",              // Development mode
    "build": "...",            // Production build
    "package:mac": "...",      // macOS packaging
    "package:win": "...",      // Windows packaging
    "package:linux": "..."     // Linux packaging
  }
}
```

**Lessons for cdev:**
- Consider Electron desktop app (complementing iOS)
- Cross-platform packaging from day 1
- Separate dev/prod build pipelines

---

### 3.2 Hypothetical Implementation Analysis

#### 3.2.1 Agent Pool Management (Python)

**Likely Pattern:**
```python
# backend/agents/pool.py (hypothetical)
class AgentPool:
    def __init__(self, max_agents=12):
        self.max_agents = max_agents
        self.active_agents = []
        self.queue = Queue()

    def spawn_agent(self, task_spec):
        """Spawn agent in isolated worktree"""
        worktree_path = f".auto-claude/worktrees/{task_spec.id}"

        # Create git worktree
        subprocess.run(["git", "worktree", "add", worktree_path, "-b", f"task-{task_spec.id}"])

        # Start Claude in worktree
        agent = ClaudeAgent(
            cwd=worktree_path,
            prompt=task_spec.prompt,
            qa_pipeline=self.qa_pipeline
        )

        self.active_agents.append(agent)
        agent.start()

    def wait_for_completion(self):
        """Block until all agents finish"""
        for agent in self.active_agents:
            agent.join()
```

**cdev Equivalent (Go):**
```go
// internal/agents/pool.go
type AgentPool struct {
    maxAgents     int
    activeAgents  []*Agent
    queue         chan TaskSpec
    worktreeRoot  string
}

func (p *AgentPool) SpawnAgent(spec TaskSpec) error {
    worktreePath := filepath.Join(p.worktreeRoot, spec.ID)

    // Create git worktree
    cmd := exec.Command("git", "worktree", "add", worktreePath, "-b", "task-"+spec.ID)
    if err := cmd.Run(); err != nil {
        return err
    }

    // Start Claude manager in worktree
    agent := claude.NewManager(
        "claude",
        []string{"-p", spec.Prompt},
        30,
        p.hub,
        false,
    )
    agent.SetWorkingDirectory(worktreePath)

    p.activeAgents = append(p.activeAgents, &Agent{
        ID:      spec.ID,
        Manager: agent,
        Spec:    spec,
    })

    return agent.StartWithSession(context.Background(), spec.Prompt, "new", "", "")
}
```

---

#### 3.2.2 QA Pipeline (Python)

**Likely Pattern:**
```python
# backend/qa_pipeline/validator.py (hypothetical)
class QAPipeline:
    def __init__(self):
        self.validators = [
            TestValidator(),
            LintValidator(),
            BuildValidator(),
            TypeCheckValidator(),
        ]

    def validate(self, worktree_path):
        """Run all validators, return pass/fail"""
        results = {}

        for validator in self.validators:
            result = validator.run(worktree_path)
            results[validator.name] = result

            if not result.passed:
                return ValidationResult(passed=False, failures=[result])

        return ValidationResult(passed=True, failures=[])

class TestValidator:
    def run(self, worktree_path):
        # Detect test framework (pytest, jest, go test, etc.)
        if os.path.exists(f"{worktree_path}/pytest.ini"):
            return self.run_pytest(worktree_path)
        elif os.path.exists(f"{worktree_path}/package.json"):
            return self.run_npm_test(worktree_path)
        # ...
```

**cdev Implementation:**
```go
// internal/validation/pipeline.go
type ValidationPipeline struct {
    validators []Validator
}

type Validator interface {
    Name() string
    Validate(ctx context.Context, workspacePath string) ValidationResult
}

type TestValidator struct{}

func (v *TestValidator) Validate(ctx context.Context, path string) ValidationResult {
    // Auto-detect test framework
    if fileExists(filepath.Join(path, "go.mod")) {
        return v.runGoTest(ctx, path)
    } else if fileExists(filepath.Join(path, "package.json")) {
        return v.runNpmTest(ctx, path)
    }

    return ValidationResult{Passed: true, Message: "No tests found"}
}

func (v *TestValidator) runGoTest(ctx context.Context, path string) ValidationResult {
    cmd := exec.CommandContext(ctx, "go", "test", "./...")
    cmd.Dir = path

    output, err := cmd.CombinedOutput()
    if err != nil {
        return ValidationResult{
            Passed: false,
            Message: "Tests failed",
            Output: string(output),
        }
    }

    return ValidationResult{Passed: true, Message: "All tests passed"}
}
```

---

### 3.3 Integration Ideas for cdev

#### 3.3.1 Hybrid Execution Model

**Combine Auto-Claude's autonomy with cdev's supervision:**

```
┌────────────────────────────────────────────────────────┐
│                   cdev Execution Modes                 │
├────────────────────────────────────────────────────────┤
│                                                        │
│  1. Interactive Mode (current)                         │
│     - Real-time supervision                            │
│     - Approve every permission                         │
│     - Mobile control                                   │
│                                                        │
│  2. Autonomous Mode (new - Auto-Claude inspired)       │
│     - Specify task + acceptance criteria               │
│     - Auto-approve known patterns                      │
│     - Run QA pipeline                                  │
│     - Alert only on failures or completion             │
│                                                        │
│  3. Hybrid Mode (new - best of both)                   │
│     - Auto-approve safe operations (read, search)      │
│     - Ask for risky operations (write, delete, exec)   │
│     - Run QA before notifying completion               │
│                                                        │
└────────────────────────────────────────────────────────┘
```

**Implementation:**
```go
// internal/execution/mode.go
type ExecutionMode int

const (
    ModeInteractive ExecutionMode = iota  // Current behavior
    ModeAutonomous                        // Full auto (Auto-Claude style)
    ModeHybrid                            // Supervised auto
)

type ExecutionConfig struct {
    Mode               ExecutionMode
    AutoApprovePatterns []string          // Glob patterns for safe ops
    RequireApproval     []string          // Always ask for these
    RunQAPipeline       bool
    QAValidators        []string          // ["test", "lint", "build"]
    NotifyOnCompletion  bool
}
```

---

## 4. MOBILE UI DESIGN

### 4.1 Grid Dashboard View (Primary)

```
┌────────────────────────────────────────────────────────┐
│  Multi-Agent Dashboard                      [⚙️] [+]  │
├────────────────────────────────────────────────────────┤
│                                                        │
│  📊 Aggregate Status                                   │
│  ┌──────────────────────────────────────────────────┐ │
│  │ 4 Running  2 Waiting  1 Idle  0 Errors           │ │
│  │ CPU: 145%  Memory: 2.3GB  Tokens: 45K            │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  ┌─────────────────────┬─────────────────────┐        │
│  │ Session 1           │ Session 2           │        │
│  │ ▶️ Running          │ ⏸️ Waiting          │        │
│  │ ──────────────────  │ ──────────────────  │        │
│  │ > npm install...    │ 🔒 Permission       │        │
│  │ > Installing        │ Write file.ts?      │        │
│  │ > 45 packages...    │ [Approve] [Deny]    │        │
│  │                     │                     │        │
│  │ 💬 24  🎫 5.2K      │ 💬 8   🎫 1.1K      │        │
│  │ ⚡ 35%  🧠 512MB    │ ⚡ 12%  🧠 256MB    │        │
│  └─────────────────────┴─────────────────────┘        │
│                                                        │
│  ┌─────────────────────┬─────────────────────┐        │
│  │ Session 3           │ Session 4           │        │
│  │ ▶️ Running          │ ⏹️ Idle             │        │
│  │ ──────────────────  │ ──────────────────  │        │
│  │ > Running tests...  │ Completed ✓         │        │
│  │ ✓ All tests pass   │ Waiting for next    │        │
│  │ > Committing...     │ task                │        │
│  │                     │                     │        │
│  │ 💬 32  🎫 8.9K      │ 💬 15  🎫 2.5K      │        │
│  │ ⚡ 48%  🧠 768MB    │ ⚡ 0%   🧠 0MB      │        │
│  └─────────────────────┴─────────────────────┘        │
│                                                        │
│  [Grid] [List] [Single]         [Stop All Sessions]   │
│                                                        │
└────────────────────────────────────────────────────────┘
```

**SessionCardView Components:**
```swift
struct SessionCardView: View {
    let session: SessionState
    let onTap: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            // Header: State + Session ID
            HStack {
                stateIcon
                Text(session.id.prefix(8))
                    .font(.caption)
                    .foregroundColor(.secondary)
                Spacer()
                Menu {
                    Button("Focus", action: { onTap() })
                    Button("Stop", role: .destructive, action: { stopSession() })
                } label: {
                    Image(systemName: "ellipsis")
                }
            }

            Divider()

            // Terminal Preview (last 3 lines)
            VStack(alignment: .leading, spacing: 2) {
                ForEach(session.logs.suffix(3)) { log in
                    Text(log.line)
                        .font(.system(size: 10, design: .monospaced))
                        .lineLimit(1)
                }
            }
            .frame(height: 50)

            // Permission Indicator
            if session.hasPermission {
                HStack {
                    Image(systemName: "lock.shield")
                        .foregroundColor(.orange)
                    Text("Permission Required")
                        .font(.caption)
                    Spacer()
                }
                .padding(4)
                .background(Color.orange.opacity(0.2))
                .cornerRadius(4)
            }

            Spacer()

            // Metrics Footer
            HStack {
                Label("\(session.messageCount)", systemImage: "bubble.left")
                Label("\(formatTokens(session.tokensUsed))", systemImage: "ticket")
                Spacer()
                Label("\(Int(session.cpuPercent))%", systemImage: "cpu")
                Label("\(Int(session.memoryMB))MB", systemImage: "memorychip")
            }
            .font(.caption2)
            .foregroundColor(.secondary)
        }
        .padding(12)
        .background(Color(.systemBackground))
        .cornerRadius(12)
        .shadow(radius: 2)
        .onTapGesture(perform: onTap)
    }

    var stateIcon: some View {
        switch session.state {
        case .running:
            return Image(systemName: "play.circle.fill").foregroundColor(.green)
        case .waiting:
            return Image(systemName: "pause.circle.fill").foregroundColor(.orange)
        case .idle:
            return Image(systemName: "stop.circle.fill").foregroundColor(.gray)
        case .error:
            return Image(systemName: "exclamationmark.circle.fill").foregroundColor(.red)
        }
    }
}
```

---

### 4.2 Batch Start Sheet

```
┌────────────────────────────────────────────────────────┐
│  Start Batch Sessions                           [✕]    │
├────────────────────────────────────────────────────────┤
│                                                        │
│  Define up to 4 prompts to run in parallel:           │
│                                                        │
│  Session 1                                             │
│  ┌──────────────────────────────────────────────────┐ │
│  │ Implement user authentication with JWT          │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  Session 2                                             │
│  ┌──────────────────────────────────────────────────┐ │
│  │ Add unit tests for UserService                  │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  Session 3                                             │
│  ┌──────────────────────────────────────────────────┐ │
│  │ Create API endpoint for user profile            │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  Session 4                                             │
│  ┌──────────────────────────────────────────────────┐ │
│  │ Update documentation for auth flow              │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  ⚙️ Max Concurrent: [4] ▼                             │
│                                                        │
│  📋 Or load from template:                            │
│  ┌──────────────────────────────────────────────────┐ │
│  │ Full Stack Feature ▼                             │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  ┌──────────────────────────────────────────────────┐ │
│  │            [Start All Sessions]                  │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
└────────────────────────────────────────────────────────┘
```

**Implementation:**
```swift
struct BatchStartSheet: View {
    @Binding var prompts: [String]
    @State private var maxConcurrent: Int = 4
    @State private var selectedTemplate: SessionTemplate?

    let onStart: ([String], Int) -> Void
    @Environment(\.dismiss) var dismiss

    var body: some View {
        NavigationView {
            Form {
                Section("Prompts") {
                    ForEach(0..<4) { index in
                        TextField("Session \(index + 1)", text: $prompts[index])
                            .textFieldStyle(.roundedBorder)
                    }
                }

                Section("Settings") {
                    Stepper("Max Concurrent: \(maxConcurrent)", value: $maxConcurrent, in: 1...12)
                }

                Section("Templates") {
                    Picker("Load Template", selection: $selectedTemplate) {
                        Text("None").tag(SessionTemplate?.none)
                        ForEach(SessionTemplateStore.shared.templates) { template in
                            Text(template.name).tag(template as SessionTemplate?)
                        }
                    }
                    .onChange(of: selectedTemplate) { template in
                        if let template = template {
                            prompts = template.prompts
                        }
                    }
                }

                Button("Start All Sessions") {
                    let nonEmptyPrompts = prompts.filter { !$0.isEmpty }
                    onStart(nonEmptyPrompts, maxConcurrent)
                    dismiss()
                }
                .buttonStyle(.borderedProminent)
                .disabled(prompts.allSatisfy { $0.isEmpty })
            }
            .navigationTitle("Start Batch Sessions")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
        }
    }
}
```

---

### 4.3 Resource Graph View

```
┌────────────────────────────────────────────────────────┐
│  Session Resources                                     │
├────────────────────────────────────────────────────────┤
│                                                        │
│  CPU Usage (Last 5 Minutes)                            │
│  100% ┤                                               │
│       │              ╭─╮                              │
│   75% │          ╭───╯ ╰─╮                            │
│       │      ╭───╯       ╰───╮                        │
│   50% │  ╭───╯               ╰──╮                     │
│       │╭─╯                      ╰──╮                  │
│   25% ┼╯                           ╰────              │
│       │                                               │
│    0% └───────────────────────────────────            │
│       0s    1m    2m    3m    4m    5m                │
│                                                        │
│  Current: 35%    Peak: 48%    Avg: 28%                │
│                                                        │
│  ────────────────────────────────────────────────────│
│                                                        │
│  Memory Usage (Last 5 Minutes)                         │
│  1.0GB┤                                               │
│       │                  ╭──╮                         │
│  750MB│              ╭───╯  ╰─╮                       │
│       │          ╭───╯        ╰───╮                   │
│  500MB│      ╭───╯                ╰──╮                │
│       │  ╭───╯                       ╰──╮             │
│  250MB┼──╯                              ╰───          │
│       │                                               │
│    0MB└───────────────────────────────────            │
│       0s    1m    2m    3m    4m    5m                │
│                                                        │
│  Current: 512MB  Peak: 768MB  Avg: 456MB              │
│                                                        │
└────────────────────────────────────────────────────────┘
```

**Implementation:**
```swift
struct ResourceGraph: View {
    let samples: [ResourceSample]
    let metric: Metric

    enum Metric {
        case cpu, memory
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(metric == .cpu ? "CPU Usage" : "Memory Usage")
                .font(.headline)

            // Line chart
            LineChart(samples: samples, metric: metric)
                .frame(height: 120)

            // Stats
            HStack {
                stat("Current", value: currentValue)
                Spacer()
                stat("Peak", value: peakValue)
                Spacer()
                stat("Avg", value: avgValue)
            }
            .font(.caption)
        }
    }

    var currentValue: String {
        guard let last = samples.last else { return "0" }
        return metric == .cpu ? "\(Int(last.cpuPercent))%" : "\(Int(last.memoryMB))MB"
    }

    var peakValue: String {
        let peak = samples.max(by: { a, b in
            metric == .cpu ? a.cpuPercent < b.cpuPercent : a.memoryMB < b.memoryMB
        })
        guard let peak = peak else { return "0" }
        return metric == .cpu ? "\(Int(peak.cpuPercent))%" : "\(Int(peak.memoryMB))MB"
    }

    var avgValue: String {
        guard !samples.isEmpty else { return "0" }
        let sum = samples.reduce(0.0) { sum, sample in
            sum + (metric == .cpu ? sample.cpuPercent : sample.memoryMB)
        }
        let avg = sum / Double(samples.count)
        return metric == .cpu ? "\(Int(avg))%" : "\(Int(avg))MB"
    }

    func stat(_ label: String, value: String) -> some View {
        VStack(alignment: .leading) {
            Text(label)
                .foregroundColor(.secondary)
            Text(value)
                .font(.body.bold())
        }
    }
}

// Use SwiftUI Charts (iOS 16+)
import Charts

struct LineChart: View {
    let samples: [ResourceSample]
    let metric: ResourceGraph.Metric

    var body: some View {
        Chart {
            ForEach(samples.indices, id: \.self) { index in
                LineMark(
                    x: .value("Time", samples[index].timestamp),
                    y: .value("Value", getValue(samples[index]))
                )
                .foregroundStyle(.blue.gradient)
            }
        }
    }

    func getValue(_ sample: ResourceSample) -> Double {
        metric == .cpu ? sample.cpuPercent : sample.memoryMB
    }
}
```

---

### 4.4 Unified Permission Panel

```
┌────────────────────────────────────────────────────────┐
│  Permissions Pending (3)                               │
├────────────────────────────────────────────────────────┤
│                                                        │
│  Session 1: abc-123                                    │
│  ┌──────────────────────────────────────────────────┐ │
│  │ 🔨 Bash                                          │ │
│  │ rm -rf node_modules                              │ │
│  │                                                  │ │
│  │ [Allow Once] [Allow for Session] [Deny]         │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  Session 2: def-456                                    │
│  ┌──────────────────────────────────────────────────┐ │
│  │ ✏️ Write                                          │ │
│  │ /Users/brianly/Projects/cdev/config.yaml         │ │
│  │                                                  │ │
│  │ [Show Diff] [Allow] [Deny]                      │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  Session 4: ghi-789                                    │
│  ┌──────────────────────────────────────────────────┐ │
│  │ 🔒 Trust Folder                                  │ │
│  │ /Users/brianly/Projects/cdev                     │ │
│  │                                                  │ │
│  │ [Trust] [Deny]                                   │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
│  ┌──────────────────────────────────────────────────┐ │
│  │         [Approve All Safe]  [Deny All]           │ │
│  └──────────────────────────────────────────────────┘ │
│                                                        │
└────────────────────────────────────────────────────────┘
```

---

## 5. IMPLEMENTATION ROADMAP

### 5.1 Milestone Timeline

```
Sprint 1 (Weeks 1-2): Backend Foundation
├─ Week 1: Multi-Session Manager
│  └─ Deliverable: Multiple concurrent sessions supported
├─ Week 2: RPC Methods
   └─ Deliverable: aggregate-status, start-batch, stop-all APIs

Sprint 2 (Weeks 3-4): iOS Multi-Session UI
├─ Week 3: Grid Layout
│  └─ Deliverable: 2x2 grid view with session cards
├─ Week 4: Batch Start
   └─ Deliverable: Batch start sheet + resource graphs

Sprint 3 (Week 5): Polish & Advanced
└─ Deliverable: Unified permissions, templates, analytics
```

### 5.2 Success Metrics

**Technical:**
- [ ] Support 4-6 concurrent sessions without performance degradation
- [ ] <100ms latency for aggregate status queries
- [ ] <5% CPU overhead for resource monitoring
- [ ] Zero session state corruption under load

**User Experience:**
- [ ] <3 taps to start batch sessions
- [ ] Real-time UI updates (<500ms delay from event)
- [ ] Smooth 60fps animations on grid view
- [ ] Intuitive permission management across sessions

**Business:**
- [ ] 30% reduction in context switching time
- [ ] 50% increase in parallel task completion
- [ ] 80% user satisfaction in usability testing

---

## 6. APPENDIX

### 6.1 Dependencies

**Backend (Go):**
```bash
go get github.com/shirou/gopsutil/v3  # Process metrics
```

**iOS (Swift):**
```swift
// Built-in frameworks only:
- SwiftUI Charts (iOS 16+)
- Combine
- Foundation
```

### 6.2 Configuration

**cdev config.yaml:**
```yaml
multisession:
  enabled: true
  max_concurrent: 12
  resource_monitoring:
    enabled: true
    sample_interval: 5s
    history_size: 60
  batch_start:
    default_max_parallel: 4
```

### 6.3 Testing Strategy

**Backend Unit Tests:**
```bash
go test ./internal/session -v
go test ./internal/monitoring -v
go test ./internal/rpc/handler/methods -v
```

**Integration Tests:**
```bash
go test ./test/integration -tags=integration -v
```

**iOS UI Tests:**
```swift
// XCTest UI automation
func testMultiSessionGrid() {
    // Test grid layout with 4 sessions
    // Verify session cards render correctly
    // Test tap to focus
}
```

---

**Document End**

*This specification is a living document and will be updated as implementation progresses.*
