# Multi-Agent Architecture: Claude, Gemini, and Codex Integration

> **Solution Architecture Document**
> Version: 1.0.0
> Date: 2025-12-21

## Executive Summary

This document presents a comprehensive architecture for integrating multiple AI CLI agents (Claude Code, Gemini CLI, OpenAI Codex) into cdev. The goal is to provide a unified mobile control interface while maintaining agent-specific capabilities and respecting each CLI's unique interaction patterns.

---

## Table of Contents

1. [Current State Analysis](#1-current-state-analysis)
2. [CLI Comparison Matrix](#2-cli-comparison-matrix)
3. [Unified Agent Protocol](#3-unified-agent-protocol)
4. [Adapter Architecture](#4-adapter-architecture)
5. [Event Normalization](#5-event-normalization)
6. [Session Management](#6-session-management)
7. [Security Model](#7-security-model)
8. [Implementation Roadmap](#8-implementation-roadmap)
9. [Protocol Enhancements](#9-protocol-enhancements)

---

## 1. Current State Analysis

### 1.1 Existing Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        cdev Server                               │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐    ┌──────────────┐    ┌─────────────────────┐ │
│  │ Unified     │    │  Event Hub   │    │  Session Cache      │ │
│  │ Server      │◄──►│  (Fan-out)   │◄──►│  (SQLite)           │ │
│  │ (Port 16180) │    │              │    │                     │ │
│  └──────┬──────┘    └──────┬───────┘    └─────────────────────┘ │
│         │                  │                                     │
│         │           ┌──────┴───────┐                            │
│         │           │              │                            │
│  ┌──────▼──────┐    │   ┌──────────▼──────────┐                │
│  │ RPC Handler │    │   │ Claude Manager      │                │
│  │ (JSON-RPC)  │────┼──►│ (Process Control)   │                │
│  └─────────────┘    │   └─────────────────────┘                │
│                     │                                           │
│              ┌──────┴───────┐    ┌─────────────────────┐       │
│              │ Git Tracker  │    │ File Watcher        │       │
│              └──────────────┘    └─────────────────────┘       │
└─────────────────────────────────────────────────────────────────┘
```

### 1.2 Current AgentManager Interface

```go
// internal/rpc/handler/methods/agent.go
type AgentManager interface {
    StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string) error
    Stop(ctx context.Context) error
    SendResponse(toolUseID, response string, isError bool) error
    State() AgentState
    PID() int
    SessionID() string
    AgentType() string  // "claude", "gemini", "codex"
}
```

**Current Limitations:**
1. Single agent instance at a time
2. Claude-specific event types
3. No agent capability discovery
4. Hardcoded permission tools list

---

## 2. CLI Comparison Matrix

### 2.1 Feature Comparison

| Feature | Claude Code | Gemini CLI | OpenAI Codex |
|---------|------------|------------|--------------|
| **Output Format** | `--output-format stream-json` | `--output-format json` or `stream-json` | TBD |
| **Input Format** | `--input-format stream-json` | stdin JSON (IDE mode) | TBD |
| **Session Resume** | `--resume <session_id>` | `--resume [session_id]` | TBD |
| **Non-Interactive** | `-p` flag | `--prompt` flag | TBD |
| **Permission Skip** | `--dangerously-skip-permissions` | Sandbox profiles | TBD |
| **Working Dir** | Runs in specified dir | Runs in specified dir | TBD |
| **Context File** | `CLAUDE.md` | `GEMINI.md` | TBD |
| **MCP Support** | Yes (built-in) | Yes (configurable) | TBD |

### 2.2 Output Event Comparison

| Event Type | Claude Code | Gemini CLI | Unified Name |
|------------|-------------|------------|--------------|
| Start message | `{"type":"init",...}` | `{"type":"init",...}` | `agent_init` |
| Assistant text | `{"type":"assistant",...}` | `{"type":"message",...}` | `agent_message` |
| Tool call | `stop_reason: "tool_use"` | `{"type":"tool_call",...}` | `agent_tool_call` |
| Tool result | `{"type":"user",...}` | `{"type":"tool_result",...}` | `agent_tool_result` |
| Completion | `{"type":"result",...}` | `{"type":"done",...}` | `agent_complete` |
| Error | `{"type":"error",...}` | `{"type":"error",...}` | `agent_error` |
| Thinking | `content.type: "thinking"` | Not supported | `agent_thinking` |

### 2.3 Interactive Patterns

**Claude Code:**
```json
// Tool use requiring permission
{
  "type": "assistant",
  "message": {
    "stop_reason": "tool_use",
    "content": [{"type": "tool_use", "id": "...", "name": "Bash", "input": {...}}]
  }
}

// Response via stdin
{
  "type": "user",
  "content": [{"type": "tool_result", "tool_use_id": "...", "content": "..."}]
}
```

**Gemini CLI (IDE Mode):**
```json
// Tool call
{"type": "tool_call", "id": "...", "name": "shell", "arguments": {...}}

// Response via stdin
{"type": "tool_response", "id": "...", "result": {...}}
```

---

## 3. Unified Agent Protocol

### 3.1 Protocol Design Principles

1. **Agent-Agnostic**: Protocol should work with any AI CLI that supports JSON output
2. **Capability-Based**: Clients discover agent capabilities at runtime
3. **Extensible**: New agents can be added without protocol changes
4. **Backward Compatible**: Existing Claude-only clients continue to work

### 3.2 Enhanced JSON-RPC Methods

#### Agent Discovery (New)

```json
// Request
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "agent/discover",
  "params": {}
}

// Response
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "available": [
      {
        "type": "claude",
        "name": "Claude Code",
        "version": "1.0.0",
        "capabilities": {
          "streaming": true,
          "sessionResume": true,
          "thinking": true,
          "mcp": true,
          "sandbox": false
        },
        "tools": ["Read", "Write", "Edit", "Bash", "Glob", "Grep", "WebFetch", "WebSearch"]
      },
      {
        "type": "gemini",
        "name": "Gemini CLI",
        "version": "0.1.0",
        "capabilities": {
          "streaming": true,
          "sessionResume": true,
          "thinking": false,
          "mcp": true,
          "sandbox": true
        },
        "tools": ["shell", "read_file", "write_file", "edit_file", "search_code"]
      }
    ],
    "active": "claude",
    "status": "idle"
  }
}
```

#### Agent Selection (New)

```json
// Request: Switch active agent
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "agent/select",
  "params": {
    "type": "gemini"
  }
}

// Response
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": {
    "status": "selected",
    "type": "gemini",
    "name": "Gemini CLI"
  }
}
```

#### Enhanced Agent Run

```json
// Request with agent-specific options
{
  "jsonrpc": "2.0",
  "id": 3,
  "method": "agent/run",
  "params": {
    "prompt": "Fix the login bug",
    "mode": "new",
    "options": {
      "sandbox": "docker",        // Gemini-specific
      "model": "gemini-2.0-pro",  // Gemini-specific
      "allowedTools": ["shell", "read_file"]  // Gemini-specific
    }
  }
}
```

### 3.3 Unified Event Types

All agent-emitted events are normalized to a common format:

```typescript
interface AgentEvent {
  event: "agent_message" | "agent_tool_call" | "agent_tool_result" |
         "agent_complete" | "agent_error" | "agent_thinking" | "agent_permission";
  timestamp: string;
  agent_type: "claude" | "gemini" | "codex";
  session_id: string;
  payload: AgentMessagePayload | AgentToolCallPayload | ...;
}

interface AgentMessagePayload {
  role: "assistant" | "user";
  content: ContentBlock[];
  stop_reason?: string;
}

interface AgentToolCallPayload {
  tool_id: string;
  tool_name: string;
  arguments: Record<string, any>;
  requires_permission: boolean;
  description?: string;
}

interface AgentPermissionPayload {
  tool_id: string;
  tool_name: string;
  operation: string;        // Normalized: "file_write", "shell_execute", "web_fetch"
  target: string;           // Path, URL, or command
  risk_level: "low" | "medium" | "high";
  auto_approve_hint: boolean;
}
```

---

## 4. Adapter Architecture

### 4.1 Multi-Agent Manager

```go
// internal/adapters/agent/manager.go

// AgentRegistry manages multiple agent adapters
type AgentRegistry struct {
    adapters map[string]AgentAdapter
    active   string
    mu       sync.RWMutex
}

// AgentAdapter is the common interface all CLI adapters must implement
type AgentAdapter interface {
    // Core AgentManager methods
    AgentManager

    // Extended capabilities
    Capabilities() AgentCapabilities
    ToolMapping() map[string]string  // Maps generic tool names to CLI-specific names
    ParseOutput(line []byte) (*NormalizedEvent, error)
    FormatInput(event *ToolResponse) ([]byte, error)
}

type AgentCapabilities struct {
    Streaming      bool
    SessionResume  bool
    Thinking       bool
    MCP            bool
    Sandbox        bool
    CustomModels   bool
    FileWatching   bool
    GitIntegration bool
}
```

### 4.2 Claude Adapter

```go
// internal/adapters/agent/claude/adapter.go

type ClaudeAdapter struct {
    manager *claude.Manager  // Existing implementation
}

func (a *ClaudeAdapter) AgentType() string { return "claude" }

func (a *ClaudeAdapter) Capabilities() AgentCapabilities {
    return AgentCapabilities{
        Streaming:     true,
        SessionResume: true,
        Thinking:      true,  // Extended thinking mode
        MCP:           true,
        Sandbox:       false, // No built-in sandbox
    }
}

func (a *ClaudeAdapter) ToolMapping() map[string]string {
    return map[string]string{
        "shell":      "Bash",
        "read_file":  "Read",
        "write_file": "Write",
        "edit_file":  "Edit",
        "search":     "Grep",
        "glob":       "Glob",
        "web_fetch":  "WebFetch",
        "web_search": "WebSearch",
    }
}

func (a *ClaudeAdapter) ParseOutput(line []byte) (*NormalizedEvent, error) {
    // Parse Claude's stream-json format
    var msg claudeStreamMessage
    if err := json.Unmarshal(line, &msg); err != nil {
        return nil, err
    }

    // Normalize to common event format
    switch msg.Type {
    case "assistant":
        return a.normalizeAssistantMessage(msg)
    case "user":
        return a.normalizeUserMessage(msg)
    case "result":
        return a.normalizeResult(msg)
    // ...
    }
}
```

### 4.3 Gemini Adapter

```go
// internal/adapters/agent/gemini/adapter.go

type GeminiAdapter struct {
    command  string
    args     []string
    hub      ports.EventHub
    workDir  string

    // Gemini-specific config
    sandbox  string       // "docker", "podman", "custom", or ""
    model    string       // "gemini-2.0-flash", "gemini-2.0-pro", etc.

    mu       sync.RWMutex
    state    AgentState
    cmd      *exec.Cmd
    stdin    io.WriteCloser
    cancel   context.CancelFunc
    pid      int
    sessionID string
}

func NewGeminiAdapter(hub ports.EventHub) *GeminiAdapter {
    return &GeminiAdapter{
        command: "gemini",  // Or "npx @anthropic/gemini-cli" if not globally installed
        args: []string{
            "--output-format", "stream-json",
            "--non-interactive",  // Don't prompt for input
        },
        hub:   hub,
        state: AgentStateIdle,
    }
}

func (a *GeminiAdapter) AgentType() string { return "gemini" }

func (a *GeminiAdapter) Capabilities() AgentCapabilities {
    return AgentCapabilities{
        Streaming:     true,
        SessionResume: true,
        Thinking:      false,
        MCP:           true,
        Sandbox:       true,  // Docker/Podman sandbox support
        CustomModels:  true,  // Model selection
    }
}

func (a *GeminiAdapter) StartWithSession(ctx context.Context, prompt string, mode SessionMode, sessionID string) error {
    a.mu.Lock()
    if a.state == AgentStateRunning {
        a.mu.Unlock()
        return ErrAgentAlreadyRunning
    }

    cmdArgs := make([]string, len(a.args))
    copy(cmdArgs, a.args)

    // Add session resume if continuing
    if mode == SessionModeContinue && sessionID != "" {
        cmdArgs = append(cmdArgs, "--resume", sessionID)
    }

    // Add sandbox if configured
    if a.sandbox != "" {
        cmdArgs = append(cmdArgs, "--sandbox", a.sandbox)
    }

    // Add model if specified
    if a.model != "" {
        cmdArgs = append(cmdArgs, "--model", a.model)
    }

    // Add prompt
    cmdArgs = append(cmdArgs, "--prompt", prompt)

    // ... rest of process management similar to Claude adapter
}

func (a *GeminiAdapter) ParseOutput(line []byte) (*NormalizedEvent, error) {
    // Parse Gemini's JSON output format
    var msg geminiMessage
    if err := json.Unmarshal(line, &msg); err != nil {
        return nil, err
    }

    switch msg.Type {
    case "message":
        return a.normalizeMessage(msg)
    case "tool_call":
        return a.normalizeToolCall(msg)
    case "tool_result":
        return a.normalizeToolResult(msg)
    case "done":
        return a.normalizeDone(msg)
    case "error":
        return a.normalizeError(msg)
    }
    return nil, nil
}

func (a *GeminiAdapter) FormatInput(event *ToolResponse) ([]byte, error) {
    // Format tool response for Gemini's stdin
    response := map[string]interface{}{
        "type":   "tool_response",
        "id":     event.ToolID,
        "result": event.Content,
    }
    return json.Marshal(response)
}

func (a *GeminiAdapter) ToolMapping() map[string]string {
    // Gemini uses different tool names
    return map[string]string{
        "Bash":      "shell",
        "Read":      "read_file",
        "Write":     "write_file",
        "Edit":      "edit_file",
        "Grep":      "search_code",
        "Glob":      "find_files",
        "WebFetch":  "web_fetch",
    }
}
```

### 4.4 Codex Adapter (Placeholder)

```go
// internal/adapters/agent/codex/adapter.go

type CodexAdapter struct {
    // Will be implemented when Codex CLI is available
    // Structure similar to Claude/Gemini adapters
}

func (a *CodexAdapter) AgentType() string { return "codex" }

func (a *CodexAdapter) Capabilities() AgentCapabilities {
    return AgentCapabilities{
        Streaming:     true,  // Expected
        SessionResume: true,  // Expected
        Thinking:      false, // Unknown
        MCP:           false, // Unknown
        Sandbox:       false, // Unknown
    }
}
```

---

## 5. Event Normalization

### 5.1 Event Normalizer

```go
// internal/adapters/agent/normalizer.go

type EventNormalizer struct {
    adapters map[string]AgentAdapter
}

// NormalizedEvent is the common event format for all agents
type NormalizedEvent struct {
    Type      string          // "message", "tool_call", "tool_result", "complete", "error"
    Timestamp time.Time
    AgentType string
    SessionID string
    Payload   interface{}
}

// Normalize converts agent-specific events to common format
func (n *EventNormalizer) Normalize(agentType string, raw []byte) (*NormalizedEvent, error) {
    adapter, ok := n.adapters[agentType]
    if !ok {
        return nil, fmt.Errorf("unknown agent type: %s", agentType)
    }
    return adapter.ParseOutput(raw)
}
```

### 5.2 Tool Name Normalization

To provide a consistent experience, we normalize tool names across agents:

| Normalized Name | Claude | Gemini | Codex |
|-----------------|--------|--------|-------|
| `shell` | Bash | shell | run_command |
| `read_file` | Read | read_file | read |
| `write_file` | Write | write_file | write |
| `edit_file` | Edit | edit_file | edit |
| `search_content` | Grep | search_code | grep |
| `find_files` | Glob | find_files | find |
| `web_fetch` | WebFetch | web_fetch | fetch_url |
| `web_search` | WebSearch | web_search | search_web |

### 5.3 Permission Risk Levels

Standardize permission risk assessment:

```go
type RiskLevel string

const (
    RiskLow    RiskLevel = "low"     // Read operations, safe web fetches
    RiskMedium RiskLevel = "medium"  // Write operations, git commands
    RiskHigh   RiskLevel = "high"    // Shell execution, destructive operations
)

func AssessRisk(toolName string, args map[string]interface{}) RiskLevel {
    switch toolName {
    case "shell", "Bash":
        // Analyze command for risk
        cmd, _ := args["command"].(string)
        if containsDestructive(cmd) {
            return RiskHigh
        }
        return RiskMedium

    case "write_file", "Write":
        return RiskMedium

    case "read_file", "Read", "find_files", "Glob", "search_content", "Grep":
        return RiskLow

    default:
        return RiskMedium
    }
}
```

---

## 6. Session Management

### 6.1 Unified Session Storage

Sessions from all agents are stored in a unified format:

```go
// internal/adapters/sessioncache/unified.go

type UnifiedSession struct {
    ID          string     `json:"id"`
    AgentType   string     `json:"agent_type"`   // "claude", "gemini", "codex"
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
    Status      string     `json:"status"`       // "active", "completed", "error"
    RepoPath    string     `json:"repo_path"`
    MessageCount int       `json:"message_count"`
    TokensUsed  int        `json:"tokens_used"`
    CostUSD     float64    `json:"cost_usd"`
}
```

### 6.2 Session Location Mapping

| Agent | Session Storage |
|-------|-----------------|
| Claude | `~/.claude/projects/<hash>/<session>.jsonl` |
| Gemini | `~/.gemini/sessions/<session>.jsonl` |
| Codex | TBD |

```go
func (c *UnifiedSessionCache) GetSessionPath(agentType, sessionID string) string {
    switch agentType {
    case "claude":
        return c.claudeSessionPath(sessionID)
    case "gemini":
        return c.geminiSessionPath(sessionID)
    case "codex":
        return c.codexSessionPath(sessionID)
    }
    return ""
}
```

---

## 7. Security Model

### 7.1 Agent-Specific Security

#### Claude Security
- Permission mode: `--dangerously-skip-permissions` or interactive
- No built-in sandbox (relies on OS permissions)
- Trust based on CLAUDE.md directives

#### Gemini Security
- Sandbox modes: Docker, Podman, custom profiles
- Trusted folders feature
- Tool auto-acceptance with granular control
- Hooks for intercepting tool calls

```go
type GeminiSecurityConfig struct {
    Sandbox         string            // "docker", "podman", "custom", ""
    TrustedFolders  []string          // Folders with full access
    AutoAcceptTools []string          // Tools that don't need approval
    HooksEnabled    bool              // Enable before/after hooks
    CustomSandbox   string            // Custom sandbox profile path
}
```

### 7.2 Unified Permission Model

```go
type PermissionRequest struct {
    ToolID      string
    ToolName    string        // Normalized name
    AgentType   string
    Operation   string        // "read", "write", "execute", "network"
    Target      string        // Path, URL, or command
    RiskLevel   RiskLevel
    AutoApprove bool          // Based on config
    Context     string        // Additional context for user
}

type PermissionPolicy struct {
    AutoApprove  map[string]bool    // Tool -> auto-approve
    DenyPatterns []string           // Patterns to always deny (e.g., "rm -rf /")
    AuditLog     bool               // Log all permission decisions
}
```

---

## 8. Implementation Roadmap

### Phase 1: Agent Abstraction Layer (Current Priority)

**Goal**: Create adapter interface without breaking existing Claude functionality.

| Task | Effort | Priority |
|------|--------|----------|
| Create `AgentAdapter` interface | 1 day | P0 |
| Create `AgentRegistry` | 1 day | P0 |
| Wrap existing Claude manager as `ClaudeAdapter` | 1 day | P0 |
| Add `agent/discover` RPC method | 0.5 day | P0 |
| Update tests | 1 day | P0 |

**Files to Create:**
- `internal/adapters/agent/adapter.go` - Interface definitions
- `internal/adapters/agent/registry.go` - Agent registry
- `internal/adapters/agent/claude/adapter.go` - Claude wrapper
- `internal/adapters/agent/normalizer.go` - Event normalization

### Phase 2: Gemini Integration

**Goal**: Add Gemini CLI as second agent type.

| Task | Effort | Priority |
|------|--------|----------|
| Implement `GeminiAdapter` | 3 days | P1 |
| Add Gemini output parsing | 2 days | P1 |
| Add Gemini stdin formatting | 1 day | P1 |
| Add sandbox configuration | 1 day | P1 |
| Add `agent/select` RPC method | 0.5 day | P1 |
| Integration tests | 2 days | P1 |

**Files to Create:**
- `internal/adapters/agent/gemini/adapter.go` - Gemini adapter
- `internal/adapters/agent/gemini/parser.go` - Output parsing
- `internal/config/gemini.go` - Gemini configuration

### Phase 3: Unified Events

**Goal**: Normalize all agent events to common format.

| Task | Effort | Priority |
|------|--------|----------|
| Define unified event types | 1 day | P2 |
| Update EventHub for multi-agent | 1 day | P2 |
| Add agent_type to all events | 0.5 day | P2 |
| Update WebSocket event format | 1 day | P2 |
| Update iOS client | 2 days | P2 |

### Phase 4: Session Unification

**Goal**: Unified session management across agents.

| Task | Effort | Priority |
|------|--------|----------|
| Unified session schema | 1 day | P3 |
| Multi-agent session cache | 2 days | P3 |
| Session list by agent type | 0.5 day | P3 |
| Cross-agent session analytics | 1 day | P3 |

### Phase 5: Codex Integration (Future)

**Goal**: Add Codex when CLI is available.

| Task | Effort | Priority |
|------|--------|----------|
| Implement `CodexAdapter` | 3 days | P4 |
| Parse Codex output | 2 days | P4 |
| Integration tests | 2 days | P4 |

---

## 9. Protocol Enhancements

### 9.1 New RPC Methods

```yaml
methods:
  agent/discover:
    description: "List available agents and their capabilities"
    params: {}
    result: AgentDiscoveryResult

  agent/select:
    description: "Select active agent type"
    params:
      type: string  # "claude" | "gemini" | "codex"
    result: AgentSelectResult

  agent/configure:
    description: "Configure agent-specific options"
    params:
      sandbox?: string       # Gemini only
      model?: string         # Model override
      autoApprove?: string[] # Tools to auto-approve
    result: AgentConfigureResult
```

### 9.2 Enhanced Event Notifications

```yaml
notifications:
  agent_message:
    description: "Agent sent a message"
    payload:
      agent_type: string
      session_id: string
      role: string
      content: ContentBlock[]

  agent_tool_call:
    description: "Agent is calling a tool"
    payload:
      agent_type: string
      tool_id: string
      tool_name: string        # Normalized name
      tool_name_native: string # Original CLI name
      arguments: object
      requires_permission: boolean
      risk_level: string

  agent_permission:
    description: "Agent requests permission"
    payload:
      agent_type: string
      tool_id: string
      operation: string
      target: string
      risk_level: string
      description: string
```

### 9.3 Configuration Extensions

```yaml
# config.yaml
agents:
  default: claude

  claude:
    command: claude
    args: ["-p", "--verbose", "--output-format", "stream-json"]
    skip_permissions: false

  gemini:
    command: gemini
    args: ["--output-format", "stream-json"]
    sandbox: docker
    model: gemini-2.0-flash
    trusted_folders:
      - ~/Projects
    auto_accept:
      - read_file
      - find_files

  codex:
    enabled: false  # Future
```

---

## Appendix A: Gemini CLI Output Examples

### A.1 Init Message
```json
{
  "type": "init",
  "session_id": "abc123",
  "model": "gemini-2.0-flash",
  "version": "0.1.0"
}
```

### A.2 Message with Tool Call
```json
{
  "type": "message",
  "role": "assistant",
  "content": [
    {"type": "text", "text": "I'll read the file for you."},
    {"type": "tool_call", "id": "tc_1", "name": "read_file", "arguments": {"path": "main.go"}}
  ]
}
```

### A.3 Tool Result
```json
{
  "type": "tool_result",
  "id": "tc_1",
  "content": "package main\n\nfunc main() {...}"
}
```

### A.4 Completion
```json
{
  "type": "done",
  "usage": {
    "input_tokens": 1500,
    "output_tokens": 500
  },
  "duration_ms": 3500
}
```

---

## Appendix B: Migration Guide

### B.1 iOS Client Migration

**Before (Claude-only):**
```swift
struct ClaudeLogEvent: Decodable {
    let line: String
    let stream: String
    let parsed: ParsedClaudeMessage?
}
```

**After (Multi-agent):**
```swift
struct AgentMessageEvent: Decodable {
    let agentType: String      // "claude", "gemini", "codex"
    let sessionId: String
    let role: String
    let content: [ContentBlock]
    let stopReason: String?
}

// Handle all agents uniformly
func handleAgentMessage(_ event: AgentMessageEvent) {
    // Same UI for all agents
    displayMessage(role: event.role, content: event.content)
}
```

### B.2 Backward Compatibility

Legacy events will continue to work:
- `claude_log` → Still emitted for Claude
- `claude_status` → Still emitted for Claude
- `claude_waiting` → Still emitted for Claude

New unified events emitted in parallel:
- `agent_message` → For all agents
- `agent_status` → For all agents
- `agent_permission` → For all agents

---

## Appendix C: Decision Log

| Decision | Rationale | Date |
|----------|-----------|------|
| Agent-agnostic naming (`agent/*`) | Future-proofs API for new CLIs | 2025-12-21 |
| Single active agent | Simplifies state management | 2025-12-21 |
| Parallel legacy + unified events | Backward compatibility | 2025-12-21 |
| Normalized tool names | Consistent mobile UI | 2025-12-21 |
| Risk level assessment | Better permission UX | 2025-12-21 |
