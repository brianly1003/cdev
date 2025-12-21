# cdev — Architecture & Technical Specification

**Version**: 1.0.0-POC
**Target Platforms**: macOS (Intel/Apple Silicon), Windows 10/11
**Language**: Go 1.22+

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Architecture Overview](#2-architecture-overview)
3. [Project Structure](#3-project-structure)
4. [Core Components](#4-core-components)
5. [Data Flow & Event System](#5-data-flow--event-system)
6. [Session Management](#6-session-management)
7. [Protocol Specification](#7-protocol-specification)
8. [Technical Decisions](#8-technical-decisions)
9. [Cross-Platform Considerations](#9-cross-platform-considerations)
10. [Configuration Management](#10-configuration-management)
11. [Error Handling Strategy](#11-error-handling-strategy)
12. [Security Considerations](#12-security-considerations)
13. [POC Implementation Phases](#13-poc-implementation-phases)
14. [Testing Strategy](#14-testing-strategy)
15. [Build & Distribution](#15-build--distribution)
16. [Future Considerations](#16-future-considerations)

---

## 1. Executive Summary

### Purpose

`cdev` is a lightweight Go daemon that enables remote monitoring and control of Claude Code CLI sessions. It serves as the bridge between the developer's laptop and mobile devices, allowing supervision of AI-assisted coding from anywhere.

### POC Goals

1. Spawn and manage Claude Code CLI processes
2. Stream stdout/stderr in real-time
3. Watch file system changes in the repository
4. Generate and stream Git diffs
5. Expose WebSocket API for real-time communication
6. Work reliably on macOS and Windows

### Non-Goals (POC)

- Cloud relay integration (local WebSocket server only)
- iOS app (use test clients)
- Authentication/authorization
- Multi-session support
- Production-grade security

---

## 2. Architecture Overview

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                            cdev                                    │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐         │
│  │     Claude      │  │      File       │  │       Git       │         │
│  │    Manager      │  │    Watcher      │  │     Tracker     │         │
│  │                 │  │                 │  │                 │         │
│  │  - Process      │  │  - fsnotify     │  │  - status       │         │
│  │  - Streams      │  │  - Filtering    │  │  - diff         │         │
│  │  - Lifecycle    │  │  - Debouncing   │  │  - Parsing      │         │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘         │
│           │                    │                    │                   │
│           └────────────────────┼────────────────────┘                   │
│                                │                                        │
│                                ▼                                        │
│                    ┌───────────────────────┐                           │
│                    │      Event Hub        │                           │
│                    │                       │                           │
│                    │  - Central dispatcher │                           │
│                    │  - Fan-out to clients │                           │
│                    │  - Buffered channels  │                           │
│                    └───────────┬───────────┘                           │
│                                │                                        │
│              ┌─────────────────┴─────────────────┐                     │
│              │                                   │                     │
│              ▼                                   ▼                     │
│    ┌──────────────────┐              ┌──────────────────┐             │
│    │  WebSocket API   │              │    HTTP API      │             │
│    │                  │              │                  │             │
│    │  - Real-time     │              │  - Commands      │             │
│    │  - Bi-directional│              │  - Health check  │             │
│    │  - Events        │              │  - Status        │             │
│    └──────────────────┘              └──────────────────┘             │
│                                                                        │
└─────────────────────────────────────────────────────────────────────────┘
                    │                           │
                    ▼                           ▼
            ┌──────────────┐           ┌──────────────┐
            │  iOS App     │           │  Test Client │
            │  (Future)    │           │  (curl/ws)   │
            └──────────────┘           └──────────────┘
```

### Component Interaction Diagram

```
┌──────────────────────────────────────────────────────────────────┐
│                     External Systems                              │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐         │
│   │ Claude CLI  │    │ File System │    │     Git     │         │
│   └──────┬──────┘    └──────┬──────┘    └──────┬──────┘         │
│          │                  │                  │                 │
└──────────┼──────────────────┼──────────────────┼─────────────────┘
           │                  │                  │
           ▼                  ▼                  ▼
┌──────────────────────────────────────────────────────────────────┐
│                      Adapter Layer                                │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│   ┌─────────────┐    ┌─────────────┐    ┌─────────────┐         │
│   │   Claude    │    │   Watcher   │    │     Git     │         │
│   │   Adapter   │    │   Adapter   │    │   Adapter   │         │
│   └──────┬──────┘    └──────┬──────┘    └──────┬──────┘         │
│          │                  │                  │                 │
└──────────┼──────────────────┼──────────────────┼─────────────────┘
           │                  │                  │
           └──────────────────┼──────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│                      Domain Layer                                 │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│   ┌─────────────────────────────────────────────────────────┐   │
│   │                    Event Hub                             │   │
│   │                                                          │   │
│   │  Events:                                                 │   │
│   │  - ClaudeLogEvent                                        │   │
│   │  - ClaudeStatusEvent                                     │   │
│   │  - FileChangedEvent                                      │   │
│   │  - GitDiffEvent                                          │   │
│   │  - SessionEvent                                          │   │
│   └─────────────────────────────────────────────────────────┘   │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌──────────────────────────────────────────────────────────────────┐
│                      Transport Layer                              │
├──────────────────────────────────────────────────────────────────┤
│                                                                   │
│   ┌─────────────────────┐      ┌─────────────────────┐          │
│   │   WebSocket Server  │      │    HTTP Server      │          │
│   │                     │      │                     │          │
│   │   Port: 8765        │      │   Port: 8766        │          │
│   └─────────────────────┘      └─────────────────────┘          │
│                                                                   │
└──────────────────────────────────────────────────────────────────┘
```

### Architecture Pattern: Hexagonal Architecture (Ports & Adapters)

We adopt **Hexagonal Architecture** for the following reasons:

1. **Testability**: Core logic is isolated from external dependencies
2. **Flexibility**: Easy to swap implementations (e.g., different file watchers)
3. **Clarity**: Clear boundaries between business logic and infrastructure
4. **Go-Idiomatic**: Interfaces as ports align well with Go's design philosophy

```
                    ┌─────────────────────────────────┐
                    │                                 │
     Primary        │         Application Core        │        Secondary
     Adapters       │                                 │        Adapters
     (Driving)      │  ┌─────────────────────────┐   │        (Driven)
                    │  │                         │   │
┌──────────────┐    │  │     Domain Logic        │   │    ┌──────────────┐
│  WebSocket   │◄───┼──┤                         ├───┼───►│  Claude CLI  │
│  Handler     │    │  │  - Event processing     │   │    │  Executor    │
└──────────────┘    │  │  - State management     │   │    └──────────────┘
                    │  │  - Business rules       │   │
┌──────────────┐    │  │                         │   │    ┌──────────────┐
│  HTTP        │◄───┼──┤                         ├───┼───►│  File System │
│  Handler     │    │  │                         │   │    │  Watcher     │
└──────────────┘    │  └─────────────────────────┘   │    └──────────────┘
                    │                                 │
                    │         Ports (Interfaces)      │    ┌──────────────┐
                    │                                 ├───►│  Git Client  │
                    └─────────────────────────────────┘    └──────────────┘
```

---

## 3. Project Structure

### Directory Layout

```
cdev/
├── cmd/
│   └── cdev/
│       └── main.go                 # Application entry point
│
├── internal/                       # Private application code
│   │
│   ├── app/                        # Application orchestration
│   │   ├── app.go                  # Main application struct
│   │   └── options.go              # Functional options pattern
│   │
│   ├── config/                     # Configuration management
│   │   ├── config.go               # Config struct & loading
│   │   ├── defaults.go             # Default values
│   │   └── validation.go           # Config validation
│   │
│   ├── domain/                     # Domain types & interfaces
│   │   ├── events/                 # Event definitions
│   │   │   ├── types.go            # Event type definitions
│   │   │   ├── claude.go           # Claude-specific events
│   │   │   ├── file.go             # File change events
│   │   │   └── git.go              # Git diff events
│   │   │
│   │   ├── commands/               # Command definitions
│   │   │   └── types.go            # Command type definitions
│   │   │
│   │   └── ports/                  # Interface definitions (ports)
│   │       ├── claude.go           # Claude manager port
│   │       ├── watcher.go          # File watcher port
│   │       ├── git.go              # Git tracker port
│   │       └── hub.go              # Event hub port
│   │
│   ├── adapters/                   # Adapter implementations
│   │   │
│   │   ├── claude/                 # Claude CLI adapter
│   │   │   ├── manager.go          # Cross-platform manager
│   │   │   ├── manager_unix.go     # Unix-specific (macOS/Linux)
│   │   │   ├── manager_windows.go  # Windows-specific
│   │   │   ├── parser.go           # Output parsing
│   │   │   └── state.go            # State machine
│   │   │
│   │   ├── watcher/                # File system watcher adapter
│   │   │   ├── watcher.go          # fsnotify wrapper
│   │   │   ├── filter.go           # Ignore patterns
│   │   │   └── debounce.go         # Event debouncing
│   │   │
│   │   └── git/                    # Git adapter
│   │       ├── tracker.go          # Git CLI wrapper
│   │       ├── parser.go           # Output parsing
│   │       └── diff.go             # Diff generation
│   │
│   ├── hub/                        # Event hub implementation
│   │   ├── hub.go                  # Central event dispatcher
│   │   ├── subscriber.go           # Subscriber management
│   │   └── buffer.go               # Event buffering
│   │
│   └── server/                     # Server implementations
│       ├── websocket/              # WebSocket server
│       │   ├── server.go           # Server setup
│       │   ├── client.go           # Client connection handler
│       │   ├── handler.go          # Message handlers
│       │   └── upgrader.go         # HTTP upgrade logic
│       │
│       └── http/                   # HTTP server
│           ├── server.go           # Server setup
│           ├── routes.go           # Route definitions
│           └── handlers.go         # Request handlers
│
├── pkg/                            # Public packages (if needed)
│   └── protocol/                   # Protocol definitions
│       ├── events.go               # Event JSON structures
│       ├── commands.go             # Command JSON structures
│       └── version.go              # Protocol version
│
├── configs/                        # Configuration files
│   ├── config.example.yaml         # Example configuration
│   └── ignore.default              # Default ignore patterns
│
├── scripts/                        # Build & utility scripts
│   ├── build.sh                    # Unix build script
│   └── build.bat                   # Windows build script
│
├── test/                           # Integration tests
│   ├── integration/                # Integration test suites
│   └── fixtures/                   # Test fixtures
│
├── tools/                          # Development tools
│   └── test-client/                # WebSocket test client
│       └── main.go
│
├── .gitignore
├── go.mod
├── go.sum
├── Makefile
├── README.md
└── LICENSE
```

### Directory Responsibilities

| Directory | Responsibility | Import Rules |
|-----------|----------------|--------------|
| `cmd/` | Entry points, CLI parsing, DI wiring | Can import all internal packages |
| `internal/domain/` | Business types, interfaces (ports) | No external dependencies |
| `internal/adapters/` | External system integrations | Implements domain ports |
| `internal/hub/` | Event dispatching core | Only domain types |
| `internal/server/` | Transport layer (WS/HTTP) | Uses hub, domain types |
| `internal/config/` | Configuration loading | Minimal dependencies |
| `internal/app/` | Application lifecycle | Orchestrates all components |
| `pkg/` | Public API types | Stable, versioned |

### Import Dependency Graph

```
cmd/cdev
      │
      ▼
internal/app ──────────────────────────────────────────┐
      │                                                │
      ├──────────────► internal/config                 │
      │                                                │
      ├──────────────► internal/hub                    │
      │                     │                          │
      │                     ▼                          │
      │              internal/domain ◄─────────────────┤
      │                     ▲                          │
      ├──────────────► internal/adapters/*             │
      │                                                │
      └──────────────► internal/server/* ──────────────┘
```

---

## 4. Core Components

### 4.1 Claude Manager

**Purpose**: Spawn, monitor, and control Claude Code CLI processes.

**Interface (Port)**:

```go
// internal/domain/ports/claude.go

package ports

import (
    "context"
    "github.com/your-org/cdev/internal/domain/events"
)

// ClaudeState represents the current state of Claude CLI
type ClaudeState string

const (
    ClaudeStateIdle    ClaudeState = "idle"
    ClaudeStateRunning ClaudeState = "running"
    ClaudeStateError   ClaudeState = "error"
    ClaudeStateStopped ClaudeState = "stopped"
)

// ClaudeManager defines the contract for managing Claude CLI
type ClaudeManager interface {
    // Start spawns Claude CLI with the given prompt
    Start(ctx context.Context, prompt string) error

    // Stop gracefully terminates the running Claude process
    Stop(ctx context.Context) error

    // Kill forcefully terminates the Claude process
    Kill() error

    // State returns the current state
    State() ClaudeState

    // Events returns a channel of Claude events
    Events() <-chan events.Event

    // IsRunning returns true if Claude is currently running
    IsRunning() bool
}
```

**State Machine**:

```
                    ┌──────────────────────────────────┐
                    │                                  │
                    ▼                                  │
    ┌───────┐   Start()   ┌─────────┐   Complete    ┌─┴───┐
    │ IDLE  │────────────►│ RUNNING │──────────────►│IDLE │
    └───────┘             └────┬────┘               └─────┘
        ▲                      │
        │                      │ Error / Stop()
        │                      ▼
        │               ┌──────────┐
        │               │  ERROR   │
        │               └────┬─────┘
        │                    │
        │     Reset          │
        └────────────────────┘
```

**Key Implementation Details**:

- Uses `os/exec` for process management
- Separate goroutines for stdout/stderr streaming
- Platform-specific termination (SIGTERM on Unix, process groups on Windows)
- Implements output buffering to prevent blocking

### 4.2 File Watcher

**Purpose**: Monitor repository for file changes and emit events.

**Interface (Port)**:

```go
// internal/domain/ports/watcher.go

package ports

import (
    "context"
    "github.com/your-org/cdev/internal/domain/events"
)

// FileChangeType represents the type of file change
type FileChangeType string

const (
    FileCreated  FileChangeType = "created"
    FileModified FileChangeType = "modified"
    FileDeleted  FileChangeType = "deleted"
    FileRenamed  FileChangeType = "renamed"
)

// FileWatcher defines the contract for file system monitoring
type FileWatcher interface {
    // Start begins watching the specified directory
    Start(ctx context.Context, rootPath string) error

    // Stop terminates file watching
    Stop() error

    // Events returns a channel of file change events
    Events() <-chan events.Event

    // AddIgnorePattern adds a pattern to the ignore list
    AddIgnorePattern(pattern string)

    // RemoveIgnorePattern removes a pattern from the ignore list
    RemoveIgnorePattern(pattern string)
}
```

**Key Implementation Details**:

- Uses `fsnotify/fsnotify` for cross-platform file notifications
- Recursive watching via `filepath.Walk` (fsnotify doesn't support recursive natively)
- Debouncing to handle rapid successive events (100ms window)
- Configurable ignore patterns (gitignore-style)

**Debounce Algorithm**:

```
Event arrives
     │
     ▼
┌────────────────────┐
│ Is path in buffer? │
└────────┬───────────┘
         │
    ┌────┴────┐
    │ Yes     │ No
    ▼         ▼
Reset timer  Add to buffer
    │        Start timer
    │             │
    └──────┬──────┘
           │
           ▼ (after debounce window)
    Emit consolidated event
    Remove from buffer
```

### 4.3 Git Tracker

**Purpose**: Track repository state and generate diffs.

**Interface (Port)**:

```go
// internal/domain/ports/git.go

package ports

import (
    "context"
    "github.com/your-org/cdev/internal/domain/events"
)

// GitFileStatus represents the status of a file in git
type GitFileStatus struct {
    Path       string
    Status     string  // M, A, D, R, etc.
    IsStaged   bool
    IsUntracked bool
}

// GitTracker defines the contract for git operations
type GitTracker interface {
    // Status returns the current git status
    Status(ctx context.Context) ([]GitFileStatus, error)

    // Diff returns the diff for a specific file
    Diff(ctx context.Context, path string) (string, error)

    // DiffAll returns diffs for all changed files
    DiffAll(ctx context.Context) (map[string]string, error)

    // IsGitRepo checks if the path is a git repository
    IsGitRepo(path string) bool

    // GetRepoRoot returns the root path of the git repository
    GetRepoRoot(ctx context.Context) (string, error)
}
```

**Key Implementation Details**:

- Shells out to `git` CLI (more reliable than libgit2 bindings)
- Parses porcelain format for machine-readable output
- Caches repository root to avoid repeated calls
- Handles both staged and unstaged changes

### 4.4 Event Hub

**Purpose**: Central event dispatcher with fan-out to subscribers.

**Interface (Port)**:

```go
// internal/domain/ports/hub.go

package ports

import (
    "context"
    "github.com/your-org/cdev/internal/domain/events"
)

// Subscriber represents an event subscriber
type Subscriber interface {
    // ID returns a unique identifier for this subscriber
    ID() string

    // Send sends an event to this subscriber
    Send(event events.Event) error

    // Close closes the subscriber
    Close() error
}

// EventHub defines the contract for event distribution
type EventHub interface {
    // Start begins the event hub
    Start(ctx context.Context) error

    // Stop gracefully stops the hub
    Stop() error

    // Publish sends an event to all subscribers
    Publish(event events.Event)

    // Subscribe adds a new subscriber
    Subscribe(sub Subscriber) error

    // Unsubscribe removes a subscriber
    Unsubscribe(id string) error

    // SubscriberCount returns the number of active subscribers
    SubscriberCount() int
}
```

**Hub Pattern (based on Gorilla WebSocket example)**:

```
                    ┌─────────────────────────────────────┐
                    │            Event Hub                │
                    │                                     │
   Claude ──────────┤►  broadcast chan Event              │
   Manager          │                                     │
                    │   ┌─────────────────────────────┐   │
   File    ─────────┤►  │     Main Loop Goroutine     │   │
   Watcher          │   │                             │   │
                    │   │  select {                   │   │
   Git     ─────────┤►  │    case event := <-broadcast│──►├──► Subscriber 1
   Tracker          │   │    case sub := <-register   │   │
                    │   │    case id := <-unregister  │──►├──► Subscriber 2
                    │   │  }                          │   │
                    │   └─────────────────────────────┘   │
                    │                                     │──► Subscriber N
                    │   register   chan Subscriber        │
                    │   unregister chan string            │
                    │   subscribers map[string]Subscriber │
                    │                                     │
                    └─────────────────────────────────────┘
```

### 4.5 WebSocket Server

**Purpose**: Real-time bidirectional communication with clients.

**Architecture**:

```
                        ┌───────────────────────────┐
                        │     WebSocket Server      │
                        │                           │
  HTTP Request ────────►│   Upgrader                │
                        │      │                    │
                        │      ▼                    │
                        │   ┌─────────────────┐     │
                        │   │  Connection     │     │
                        │   │  Handler        │     │
                        │   └────────┬────────┘     │
                        │            │              │
                        │   ┌────────┴────────┐     │
                        │   │                 │     │
                        │   ▼                 ▼     │
                        │ readPump       writePump  │
                        │ goroutine      goroutine  │
                        │   │                 │     │
                        └───┼─────────────────┼─────┘
                            │                 │
                            ▼                 ▼
                        Commands           Events
                        from client        to client
```

**Key Implementation Details**:

- Uses `gorilla/websocket` for WebSocket handling
- One goroutine for reading, one for writing (as per WebSocket spec)
- Implements ping/pong for connection health
- Buffered send channels to prevent blocking
- Automatic reconnection handling on client side

---

## 5. Data Flow & Event System

### Event Flow Diagram

```
┌────────────────────────────────────────────────────────────────────────┐
│                           Event Sources                                 │
├────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐      │
│  │  Claude CLI     │   │   File System   │   │      Git        │      │
│  │  (stdout/err)   │   │   (fsnotify)    │   │   (on change)   │      │
│  └────────┬────────┘   └────────┬────────┘   └────────┬────────┘      │
│           │                     │                     │                │
│           ▼                     ▼                     ▼                │
│  ┌─────────────────┐   ┌─────────────────┐   ┌─────────────────┐      │
│  │ ClaudeLogEvent  │   │FileChangedEvent │   │  GitDiffEvent   │      │
│  │ ClaudeStatus    │   │                 │   │                 │      │
│  └────────┬────────┘   └────────┬────────┘   └────────┬────────┘      │
│           │                     │                     │                │
└───────────┼─────────────────────┼─────────────────────┼────────────────┘
            │                     │                     │
            └─────────────────────┼─────────────────────┘
                                  │
                                  ▼
            ┌─────────────────────────────────────────┐
            │              Event Hub                   │
            │                                          │
            │  - Receives events from all sources     │
            │  - Timestamps events                     │
            │  - Validates event structure             │
            │  - Fans out to all subscribers          │
            │                                          │
            └────────────────────┬────────────────────┘
                                 │
                                 │ Fan-out
                ┌────────────────┼────────────────┐
                │                │                │
                ▼                ▼                ▼
        ┌──────────────┐ ┌──────────────┐ ┌──────────────┐
        │ WebSocket    │ │ WebSocket    │ │   Logger     │
        │ Client 1     │ │ Client 2     │ │ (internal)   │
        └──────────────┘ └──────────────┘ └──────────────┘
```

### Command Flow Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        WebSocket Client                                  │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│   User Action: "Run Claude with prompt: Fix the bug"                    │
│                          │                                               │
│                          ▼                                               │
│   ┌──────────────────────────────────────┐                              │
│   │  {"command": "run_claude",           │                              │
│   │   "payload": {"prompt": "Fix bug"}}  │                              │
│   └──────────────────────┬───────────────┘                              │
│                          │                                               │
└──────────────────────────┼───────────────────────────────────────────────┘
                           │
                           ▼ WebSocket
┌─────────────────────────────────────────────────────────────────────────┐
│                        cdev                                        │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│   WebSocket Server ──► Command Router ──► Command Handler                │
│                                                │                         │
│                                                ▼                         │
│                                    ┌─────────────────────┐              │
│                                    │  Claude Manager     │              │
│                                    │  .Start(ctx, prompt)│              │
│                                    └─────────┬───────────┘              │
│                                              │                          │
│                                              ▼                          │
│                           ┌─────────────────────────────────┐           │
│                           │  Spawns: claude code "Fix bug"  │           │
│                           └─────────────────────────────────┘           │
│                                                                          │
└──────────────────────────────────────────────────────────────────────────┘
```

### Event Triggering Chain

```
File Modified Event Flow:

1. User/Claude modifies file
         │
         ▼
2. OS notifies fsnotify
         │
         ▼
3. FileWatcher receives raw event
         │
         ▼
4. Debounce filter (100ms window)
         │
         ▼
5. Create FileChangedEvent
         │
         ├──────────────────────────────────────┐
         │                                      │
         ▼                                      ▼
6. Publish to EventHub              7. Trigger GitTracker.Diff()
         │                                      │
         ▼                                      ▼
8. Fan-out to subscribers           9. Create GitDiffEvent
         │                                      │
         ▼                                      │
10. WebSocket sends to clients  ◄───────────────┘
```

---

## 6. Session Management

### 6.1 Claude Code Session Storage

Claude Code CLI stores conversation sessions in a directory structure organized by repository path:

```
~/.claude/projects/<encoded-repo-path>/
├── bd2ddce2-d50a-43b9-8129-602e7cdba072.jsonl
├── 550e8400-e29b-41d4-a716-446655440000.jsonl
└── ...
```

**Path Encoding**: The repository absolute path is encoded by replacing `/` with `-`:
- `/Users/brian/Projects/myapp` → `-Users-brian-Projects-myapp`

**Session File Format (.jsonl)**: Each line is a JSON object representing a conversation turn:
```json
{"type":"summary","summary":"Create a hello world function","leafTitle":"Hello World","cwd":"/Users/brian/Projects/myapp"}
{"type":"user","message":{"role":"user","content":"Create a hello world function"}}
{"type":"assistant","message":{"role":"assistant","content":"I'll create..."}}
```

### 6.2 Session Modes

cdev supports two session modes when starting Claude:

| Mode | CLI Flag | Description | Use Case |
|------|----------|-------------|----------|
| `new` | (none) | Start a fresh conversation | New tasks, clean context |
| `continue` | `--resume <id>` | Continue a specific session by UUID | Return to specific conversation |

### 6.3 Session Flow Diagram

```
┌──────────────┐                  ┌──────────────┐                  ┌──────────────┐
│  Mobile App  │                  │  cdev  │                  │  Claude CLI  │
└──────┬───────┘                  └──────┬───────┘                  └──────┬───────┘
       │                                 │                                 │
       │ GET /api/claude/sessions        │                                 │
       │────────────────────────────────►│                                 │
       │                                 │                                 │
       │                                 │ Read ~/.claude/projects/...     │
       │                                 │◄────────────────────────────────│
       │                                 │                                 │
       │ { sessions: [...] }             │                                 │
       │◄────────────────────────────────│                                 │
       │                                 │                                 │
       │ POST /api/claude/run            │                                 │
       │ { mode: "continue",             │                                 │
       │   session_id: "bd2d..." }       │                                 │
       │────────────────────────────────►│                                 │
       │                                 │                                 │
       │                                 │ claude -p --resume bd2d...      │
       │                                 │────────────────────────────────►│
       │                                 │                                 │
       │ { status: "started" }           │                                 │
       │◄────────────────────────────────│                                 │
       │                                 │                                 │
       │                                 │ stdout: {"session_id":"bd2d..."}│
       │                                 │◄────────────────────────────────│
       │                                 │                                 │
       │ WS: claude_session_info         │                                 │
       │ { session_id: "bd2d..." }       │                                 │
       │◄────────────────────────────────│                                 │
       │                                 │                                 │
       │ WS: claude_log (streaming)      │ stdout: assistant messages      │
       │◄────────────────────────────────│◄────────────────────────────────│
       │                                 │                                 │
```

### 6.4 Session ID Capture

The session ID is captured **asynchronously** from Claude's stream-json output. This happens after the process starts, not immediately.

**Why Asynchronous?**
Claude CLI takes several seconds to initialize before outputting any JSON. Blocking the HTTP response would cause timeouts. The async approach allows immediate feedback while delivering the session_id when available.

**Flow:**
1. HTTP/WebSocket receives `run_claude` command
2. cdev spawns Claude CLI process
3. Response returned immediately (session_id may be empty)
4. Claude outputs initial JSON with session_id
5. cdev parses and broadcasts `claude_session_info` event via WebSocket
6. Mobile app receives session_id for future continue operations

### 6.5 Session Listing API

The `GET /api/claude/sessions` endpoint reads session files from Claude's storage directory and returns:

```json
{
  "current": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
  "sessions": [
    {
      "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
      "summary": "Create a hello world function",
      "message_count": 4,
      "last_updated": "2025-12-17T21:14:45Z",
      "branch": "main"
    }
  ]
}
```

This enables mobile apps to show a session picker similar to Claude Code's `/resume` command.

---

## 7. Protocol Specification

### 7.1 Event Types

All events follow this base structure:

```json
{
  "event": "event_type",
  "timestamp": "2024-01-15T10:30:00.000Z",
  "payload": {}
}
```

#### claude_log

Emitted for each line of Claude CLI output.

```json
{
  "event": "claude_log",
  "timestamp": "2024-01-15T10:30:00.123Z",
  "payload": {
    "line": "Analyzing the codebase...",
    "stream": "stdout"
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `line` | string | Single line of output |
| `stream` | string | `stdout` or `stderr` |

#### claude_status

Emitted when Claude state changes.

```json
{
  "event": "claude_status",
  "timestamp": "2024-01-15T10:30:00.456Z",
  "payload": {
    "state": "running",
    "prompt": "Fix the authentication bug",
    "pid": 12345,
    "started_at": "2024-01-15T10:30:00.000Z"
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `state` | string | `idle`, `running`, `error`, `stopped` |
| `prompt` | string | Current prompt (if running) |
| `pid` | number | Process ID (if running) |
| `started_at` | string | ISO timestamp of start |
| `error` | string | Error message (if error state) |
| `exit_code` | number | Exit code (if stopped/error) |

#### file_changed

Emitted when a file in the repository changes.

```json
{
  "event": "file_changed",
  "timestamp": "2024-01-15T10:30:01.000Z",
  "payload": {
    "path": "src/auth/service.ts",
    "change": "modified",
    "size": 2048
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `path` | string | Relative path from repo root |
| `change` | string | `created`, `modified`, `deleted`, `renamed` |
| `size` | number | File size in bytes (optional) |
| `old_path` | string | Previous path (if renamed) |

#### git_diff

Emitted with diff content for changed files.

```json
{
  "event": "git_diff",
  "timestamp": "2024-01-15T10:30:01.100Z",
  "payload": {
    "file": "src/auth/service.ts",
    "diff": "--- a/src/auth/service.ts\n+++ b/src/auth/service.ts\n@@ -10,6 +10,8 @@\n+function validateToken() {\n+  // validation logic\n+}",
    "additions": 3,
    "deletions": 0,
    "is_staged": false,
    "is_new_file": false
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `file` | string | Relative file path |
| `diff` | string | Unified diff content |
| `additions` | number | Lines added |
| `deletions` | number | Lines removed |
| `is_staged` | boolean | True if changes are staged |
| `is_new_file` | boolean | True if file is untracked/new |

#### session_start / session_end

Emitted for session lifecycle.

```json
{
  "event": "session_start",
  "timestamp": "2024-01-15T10:30:00.000Z",
  "payload": {
    "session_id": "abc123",
    "repo_path": "/Users/dev/myproject",
    "repo_name": "myproject",
    "agent_version": "1.0.0"
  }
}
```

#### claude_session_info

Emitted when Claude's session ID is captured from its stream-json output.

```json
{
  "event": "claude_session_info",
  "timestamp": "2024-01-15T10:30:02.000Z",
  "payload": {
    "session_id": "bd2ddce2-d50a-43b9-8129-602e7cdba072",
    "model": "",
    "version": ""
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `session_id` | string | UUID of the Claude conversation session |
| `model` | string | Model name (if available) |
| `version` | string | Claude CLI version (if available) |

**Note**: This event is broadcast asynchronously after Claude starts. Subscribe to this event to get the session ID for future `resume` operations.

#### claude_permission

Emitted when Claude requests permission for a tool operation.

```json
{
  "event": "claude_permission",
  "timestamp": "2024-01-15T10:30:01.500Z",
  "payload": {
    "tool_use_id": "toolu_01ABC123XYZ...",
    "tool_name": "Write",
    "input": "{\"file_path\":\"/path/to/file.ts\",\"content\":\"...\"}",
    "description": "Write to file: src/main.ts"
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `tool_use_id` | string | Unique ID for responding to this request |
| `tool_name` | string | Tool name (Write, Edit, Bash, etc.) |
| `input` | string | JSON string of tool parameters |
| `description` | string | Human-readable description |

#### claude_waiting

Emitted when Claude is waiting for user input (AskUserQuestion tool).

```json
{
  "event": "claude_waiting",
  "timestamp": "2024-01-15T10:30:01.800Z",
  "payload": {
    "tool_use_id": "toolu_01XYZ789...",
    "tool_name": "AskUserQuestion",
    "input": "{\"question\":\"Which database should we use?\",\"options\":[...]}"
  }
}
```

#### file_content

Response to `get_file` command.

```json
{
  "event": "file_content",
  "timestamp": "2024-01-15T10:30:02.000Z",
  "payload": {
    "path": "src/auth/service.ts",
    "content": "export function validateJWT(token: string) { ... }",
    "encoding": "utf-8",
    "truncated": false,
    "size": 1024
  }
}
```

### 7.2 Command Types

All commands follow this structure:

```json
{
  "command": "command_type",
  "request_id": "optional-correlation-id",
  "payload": {}
}
```

#### run_claude

Start Claude CLI with a prompt.

```json
{
  "command": "run_claude",
  "request_id": "req-001",
  "payload": {
    "prompt": "Refactor the auth service to use JWT validation"
  }
}
```

#### stop_claude

Gracefully stop Claude CLI.

```json
{
  "command": "stop_claude",
  "request_id": "req-002"
}
```

#### get_status

Request current agent status.

```json
{
  "command": "get_status",
  "request_id": "req-003"
}
```

Response:

```json
{
  "event": "status_response",
  "request_id": "req-003",
  "payload": {
    "claude_state": "running",
    "connected_clients": 2,
    "repo_path": "/Users/dev/myproject",
    "uptime_seconds": 3600
  }
}
```

#### get_file

Request file content.

```json
{
  "command": "get_file",
  "request_id": "req-004",
  "payload": {
    "path": "src/auth/service.ts"
  }
}
```

#### respond_to_claude

Send a response to Claude's interactive prompt or permission request.

```json
{
  "command": "respond_to_claude",
  "request_id": "req-005",
  "payload": {
    "tool_use_id": "toolu_01ABC123...",
    "response": "approved",
    "is_error": false
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `tool_use_id` | string | The ID from `claude_permission` or `claude_waiting` event |
| `response` | string | The response text (e.g., "approved", user's answer) |
| `is_error` | boolean | Set to `true` to deny/reject the request |

**Examples:**

Approve a permission:
```json
{"tool_use_id": "toolu_01...", "response": "approved", "is_error": false}
```

Deny a permission:
```json
{"tool_use_id": "toolu_01...", "response": "Permission denied by user", "is_error": true}
```

Answer an interactive question:
```json
{"tool_use_id": "toolu_01...", "response": "PostgreSQL", "is_error": false}
```

---

## 8. Technical Decisions

### 8.1 Dependencies

| Dependency | Version | Purpose | Justification |
|------------|---------|---------|---------------|
| `github.com/fsnotify/fsnotify` | v1.7+ | File watching | Cross-platform, mature, well-maintained |
| `github.com/gorilla/websocket` | v1.5+ | WebSocket | Industry standard, battle-tested |
| `github.com/spf13/cobra` | v1.8+ | CLI framework | Used by K8s, Docker; excellent UX |
| `github.com/spf13/viper` | v1.18+ | Configuration | Seamless Cobra integration |
| `github.com/rs/zerolog` | v1.32+ | Logging | Fast, structured, zero-allocation |
| `github.com/google/uuid` | v1.6+ | UUID generation | Standard, simple |

### 8.2 Why Shell Out to Git?

**Decision**: Use `git` CLI via `os/exec` rather than go-git or libgit2.

**Rationale**:

| Approach | Pros | Cons |
|----------|------|------|
| git CLI | Universal, reliable, no CGO, exact same behavior as user's git | External dependency, slightly slower |
| go-git | Pure Go, no external dep | Missing some features, different behavior edge cases |
| libgit2 | Fast, full-featured | CGO required, complex cross-compilation |

Git CLI wins for POC due to simplicity and reliability.

### 8.3 Why Not Use go-cmd/cmd?

**Decision**: Use standard `os/exec` with custom streaming.

**Rationale**:

- `go-cmd/cmd` adds complexity we don't need
- Our streaming requirements are straightforward
- Full control over goroutine management
- No additional dependency

### 8.4 Event Hub vs. Channels

**Decision**: Central Event Hub with subscriber pattern.

**Rationale**:

```
Without Hub (direct channels):
┌────────┐     ┌───────────────┐
│ Claude ├────►│ WebSocket 1   │
│        ├────►│ WebSocket 2   │
│        ├────►│ Logger        │
└────────┘     └───────────────┘
┌────────┐     ┌───────────────┐
│ Watcher├────►│ WebSocket 1   │
│        ├────►│ WebSocket 2   │
│        ├────►│ Logger        │
└────────┘     └───────────────┘

With Hub (centralized):
┌────────┐            ┌───────────────┐
│ Claude ├───┐        │ WebSocket 1   │
└────────┘   │  ┌───► └───────────────┘
             ▼  │     ┌───────────────┐
           ┌────┴──┐  │ WebSocket 2   │
           │  Hub  ├──┼►└───────────────┘
           └────┬──┘  │ ┌───────────────┐
             ▲  │     └►│ Logger        │
┌────────┐   │        └───────────────┘
│ Watcher├───┘
└────────┘
```

Hub provides:
- Single point of event management
- Easy subscriber addition/removal
- Event transformation/filtering in one place
- Better testability

### 8.5 Configuration Priority

Following 12-factor app principles:

```
Priority (highest to lowest):
1. Command-line flags     (--port=8080)
2. Environment variables  (CDEV_PORT=8080)
3. Config file            (config.yaml: port: 8080)
4. Defaults               (8080)
```

---

## 9. Cross-Platform Considerations

### 9.1 Process Management

#### Unix (macOS, Linux)

```go
// internal/adapters/claude/manager_unix.go
//go:build !windows

package claude

import (
    "os"
    "os/exec"
    "syscall"
)

func (m *Manager) setupProcess(cmd *exec.Cmd) {
    // Create new process group
    cmd.SysProcAttr = &syscall.SysProcAttr{
        Setpgid: true,
    }
}

func (m *Manager) terminateProcess(cmd *exec.Cmd) error {
    // Send SIGTERM to process group
    pgid, err := syscall.Getpgid(cmd.Process.Pid)
    if err != nil {
        return err
    }
    return syscall.Kill(-pgid, syscall.SIGTERM)
}

func (m *Manager) killProcess(cmd *exec.Cmd) error {
    // Send SIGKILL to process group
    pgid, err := syscall.Getpgid(cmd.Process.Pid)
    if err != nil {
        return err
    }
    return syscall.Kill(-pgid, syscall.SIGKILL)
}
```

#### Windows

```go
// internal/adapters/claude/manager_windows.go
//go:build windows

package claude

import (
    "os/exec"
    "syscall"
)

func (m *Manager) setupProcess(cmd *exec.Cmd) {
    // Create new process group on Windows
    cmd.SysProcAttr = &syscall.SysProcAttr{
        CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
    }
}

func (m *Manager) terminateProcess(cmd *exec.Cmd) error {
    // Windows: Use taskkill for graceful termination
    kill := exec.Command("taskkill", "/T", "/PID",
        strconv.Itoa(cmd.Process.Pid))
    return kill.Run()
}

func (m *Manager) killProcess(cmd *exec.Cmd) error {
    // Windows: Force kill with /F flag
    kill := exec.Command("taskkill", "/T", "/F", "/PID",
        strconv.Itoa(cmd.Process.Pid))
    return kill.Run()
}
```

### 9.2 Path Handling

```go
// Always use filepath package for cross-platform paths
import "path/filepath"

// Good
fullPath := filepath.Join(repoRoot, relativePath)

// Bad
fullPath := repoRoot + "/" + relativePath
```

### 9.3 File System Differences

| Aspect | macOS | Windows |
|--------|-------|---------|
| Path separator | `/` | `\` |
| Case sensitivity | Usually insensitive | Insensitive |
| Max path length | 1024 | 260 (or 32767 with prefix) |
| Hidden files | `.` prefix | Hidden attribute |
| Line endings | `\n` | `\r\n` |
| Symlinks | Full support | Limited support |

### 9.4 Build Tags

```go
// For platform-specific files:
// manager_unix.go    - //go:build !windows
// manager_windows.go - //go:build windows

// For architecture-specific:
// memory_arm64.go    - //go:build arm64
// memory_amd64.go    - //go:build amd64
```

### 9.5 Cross-Compilation Matrix

```makefile
# Makefile targets

build-all: build-macos-arm64 build-macos-amd64 build-windows-amd64

build-macos-arm64:
	GOOS=darwin GOARCH=arm64 go build -o dist/cdev-darwin-arm64 ./cmd/cdev

build-macos-amd64:
	GOOS=darwin GOARCH=amd64 go build -o dist/cdev-darwin-amd64 ./cmd/cdev

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o dist/cdev-windows-amd64.exe ./cmd/cdev
```

---

## 10. Configuration Management

### 10.1 Configuration File Structure

```yaml
# config.yaml

# Server settings
server:
  websocket_port: 8765
  http_port: 8766
  host: "127.0.0.1"  # Bind to localhost only for security

# Repository settings
repository:
  path: ""  # Empty = current directory

# File watcher settings
watcher:
  enabled: true
  debounce_ms: 100
  ignore_patterns:
    - ".git"
    - "node_modules"
    - ".venv"
    - "__pycache__"
    - "*.pyc"
    - ".DS_Store"
    - "Thumbs.db"
    - "dist"
    - "build"
    - "coverage"
    - ".next"
    - ".nuxt"

# Claude CLI settings
claude:
  command: "claude"  # Path to claude CLI
  args: []           # Additional arguments
  timeout_minutes: 30

# Git settings
git:
  enabled: true
  command: "git"
  diff_on_change: true  # Auto-generate diff on file change

# Logging
logging:
  level: "info"  # debug, info, warn, error
  format: "json" # json, console

# Limits
limits:
  max_file_size_kb: 200
  max_diff_size_kb: 500
  max_log_buffer: 1000
```

### 10.2 Configuration Loading

```go
// internal/config/config.go

package config

import (
    "github.com/spf13/viper"
)

type Config struct {
    Server     ServerConfig     `mapstructure:"server"`
    Repository RepositoryConfig `mapstructure:"repository"`
    Watcher    WatcherConfig    `mapstructure:"watcher"`
    Claude     ClaudeConfig     `mapstructure:"claude"`
    Git        GitConfig        `mapstructure:"git"`
    Logging    LoggingConfig    `mapstructure:"logging"`
    Limits     LimitsConfig     `mapstructure:"limits"`
}

func Load() (*Config, error) {
    viper.SetConfigName("config")
    viper.SetConfigType("yaml")
    viper.AddConfigPath(".")
    viper.AddConfigPath("$HOME/.cdev")
    viper.AddConfigPath("/etc/cdev")

    // Environment variable prefix
    viper.SetEnvPrefix("CDOT")
    viper.AutomaticEnv()

    // Set defaults
    setDefaults()

    // Read config file (optional)
    if err := viper.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            return nil, err
        }
        // Config file not found is OK - use defaults
    }

    var cfg Config
    if err := viper.Unmarshal(&cfg); err != nil {
        return nil, err
    }

    return &cfg, validate(&cfg)
}
```

### 10.3 Environment Variable Mapping

| Config Key | Environment Variable | Example |
|------------|---------------------|---------|
| `server.websocket_port` | `CDEV_SERVER_WEBSOCKET_PORT` | `8765` |
| `server.http_port` | `CDEV_SERVER_HTTP_PORT` | `8766` |
| `repository.path` | `CDEV_REPOSITORY_PATH` | `/path/to/repo` |
| `claude.command` | `CDEV_CLAUDE_COMMAND` | `/usr/local/bin/claude` |
| `logging.level` | `CDEV_LOGGING_LEVEL` | `debug` |

---

## 11. Error Handling Strategy

### 11.1 Error Types

```go
// internal/domain/errors.go

package domain

import "errors"

// Sentinel errors
var (
    ErrClaudeAlreadyRunning = errors.New("claude is already running")
    ErrClaudeNotRunning     = errors.New("claude is not running")
    ErrInvalidPrompt        = errors.New("invalid prompt")
    ErrRepoNotFound         = errors.New("repository not found")
    ErrNotGitRepo           = errors.New("not a git repository")
    ErrPathOutsideRepo      = errors.New("path is outside repository")
    ErrFileTooLarge         = errors.New("file exceeds size limit")
    ErrFileNotFound         = errors.New("file not found")
)

// Wrapped errors with context
type ClaudeError struct {
    Op      string // Operation that failed
    Err     error  // Underlying error
    ExitCode int   // Exit code if process exited
}

func (e *ClaudeError) Error() string {
    if e.ExitCode != 0 {
        return fmt.Sprintf("claude %s: exit code %d: %v", e.Op, e.ExitCode, e.Err)
    }
    return fmt.Sprintf("claude %s: %v", e.Op, e.Err)
}

func (e *ClaudeError) Unwrap() error {
    return e.Err
}
```

### 11.2 Error Response Format

```json
{
  "event": "error",
  "timestamp": "2024-01-15T10:30:00.000Z",
  "payload": {
    "code": "CLAUDE_ALREADY_RUNNING",
    "message": "Claude is already running",
    "request_id": "req-001",
    "details": {
      "current_prompt": "Previous task..."
    }
  }
}
```

### 11.3 Error Codes

| Code | Description |
|------|-------------|
| `CLAUDE_ALREADY_RUNNING` | Tried to start while running |
| `CLAUDE_NOT_RUNNING` | Tried to stop when not running |
| `INVALID_COMMAND` | Unknown command type |
| `INVALID_PAYLOAD` | Malformed payload |
| `PATH_OUTSIDE_REPO` | Security: path traversal attempt |
| `FILE_NOT_FOUND` | Requested file doesn't exist |
| `FILE_TOO_LARGE` | File exceeds size limit |
| `GIT_ERROR` | Git operation failed |
| `INTERNAL_ERROR` | Unexpected server error |

---

## 12. Security Considerations

### 12.1 Path Traversal Prevention

```go
// internal/adapters/file/security.go

package file

import (
    "path/filepath"
    "strings"
)

// ValidatePath ensures the path is within the repository root
func ValidatePath(repoRoot, requestedPath string) (string, error) {
    // Clean and resolve the path
    absRepo, err := filepath.Abs(repoRoot)
    if err != nil {
        return "", err
    }

    // Join with repo root and clean
    fullPath := filepath.Join(absRepo, requestedPath)
    fullPath = filepath.Clean(fullPath)

    // Resolve any symlinks
    realPath, err := filepath.EvalSymlinks(fullPath)
    if err != nil {
        // File might not exist yet, check parent
        realPath = fullPath
    }

    // Verify it's still under repo root
    if !strings.HasPrefix(realPath, absRepo) {
        return "", ErrPathOutsideRepo
    }

    return fullPath, nil
}
```

### 12.2 Input Validation

```go
// Validate prompt input
func ValidatePrompt(prompt string) error {
    if len(prompt) == 0 {
        return ErrInvalidPrompt
    }
    if len(prompt) > 10000 { // Reasonable limit
        return errors.New("prompt too long")
    }
    return nil
}

// Validate command JSON
func ValidateCommand(cmd *Command) error {
    allowedCommands := map[string]bool{
        "run_claude":  true,
        "stop_claude": true,
        "get_status":  true,
        "get_file":    true,
    }
    if !allowedCommands[cmd.Command] {
        return ErrInvalidCommand
    }
    return nil
}
```

### 12.3 Network Security

```go
// Default: Bind to localhost only
server:
  host: "127.0.0.1"
```

For POC, the agent only listens on localhost. Production will use:
- TLS encryption
- Token-based authentication
- Cloud relay (no direct inbound connections)

---

## 13. POC Implementation Phases

### Phase 1: Foundation (Days 1-2)

**Goals**: Project setup, configuration, logging

**Tasks**:
- [ ] Initialize Go module (`go mod init`)
- [ ] Create directory structure
- [ ] Implement configuration loading with Viper
- [ ] Set up structured logging with zerolog
- [ ] Create CLI entry point with Cobra
- [ ] Write configuration validation

**Deliverables**:
- Working CLI that loads config and prints status
- Configuration file example

**Validation**:
```bash
./cdev --help
./cdev --config ./config.yaml
```

---

### Phase 2: Event Hub (Days 3-4)

**Goals**: Central event dispatching system

**Tasks**:
- [ ] Define event types in `internal/domain/events/`
- [ ] Implement Event Hub with channels
- [ ] Implement subscriber interface
- [ ] Add logging subscriber for testing
- [ ] Write unit tests for hub

**Deliverables**:
- Event Hub with publish/subscribe
- Test coverage for hub operations

**Validation**:
```go
hub.Publish(events.NewTestEvent())
// Logger subscriber receives and logs event
```

---

### Phase 3: Claude Manager (Days 5-8)

**Goals**: Process management for Claude CLI

**Tasks**:
- [ ] Define Claude Manager port interface
- [ ] Implement process spawning with `os/exec`
- [ ] Implement stdout/stderr streaming
- [ ] Implement graceful shutdown (SIGTERM/taskkill)
- [ ] Implement state machine
- [ ] Add platform-specific code (build tags)
- [ ] Integrate with Event Hub
- [ ] Write unit and integration tests

**Deliverables**:
- Claude Manager that can start/stop Claude CLI
- Real-time log streaming to Event Hub
- Works on macOS and Windows

**Validation**:
```bash
# Start agent
./cdev start --repo /path/to/repo

# In another terminal, trigger Claude (via HTTP for now)
curl -X POST http://localhost:8766/api/claude/run \
  -H "Content-Type: application/json" \
  -d '{"prompt": "List all files"}'

# Watch logs stream
curl http://localhost:8766/api/claude/status
```

---

### Phase 4: File Watcher (Days 9-11)

**Goals**: File system monitoring with filtering

**Tasks**:
- [ ] Define File Watcher port interface
- [ ] Implement fsnotify wrapper
- [ ] Implement recursive watching
- [ ] Implement ignore pattern filtering
- [ ] Implement debouncing
- [ ] Integrate with Event Hub
- [ ] Write tests

**Deliverables**:
- File watcher that monitors repository
- Debounced events published to hub
- Configurable ignore patterns

**Validation**:
```bash
# Agent running
./cdev start --repo /path/to/repo

# Modify a file
echo "test" >> /path/to/repo/test.txt

# See file_changed event in logs
```

---

### Phase 5: Git Tracker (Days 12-14)

**Goals**: Git status and diff generation

**Tasks**:
- [ ] Define Git Tracker port interface
- [ ] Implement git status parsing
- [ ] Implement git diff generation
- [ ] Trigger diff on file change events
- [ ] Integrate with Event Hub
- [ ] Write tests

**Deliverables**:
- Git status retrieval
- Per-file diff generation
- Automatic diff on file changes

**Validation**:
```bash
# Modify a file in repo
echo "change" >> /path/to/repo/file.ts

# See git_diff event with unified diff content
```

---

### Phase 6: WebSocket Server (Days 15-18)

**Goals**: Real-time communication API

**Tasks**:
- [ ] Implement WebSocket server with gorilla/websocket
- [ ] Implement client connection handling
- [ ] Implement message serialization
- [ ] Subscribe clients to Event Hub
- [ ] Implement command routing
- [ ] Implement ping/pong health checks
- [ ] Write integration tests

**Deliverables**:
- WebSocket server at `ws://localhost:8765`
- Real-time event streaming
- Command handling

**Validation**:
```bash
# Use websocat or similar tool
websocat ws://localhost:8765

# Receive events in real-time
# Send commands and see responses
```

---

### Phase 7: HTTP API (Days 19-20)

**Goals**: REST API for commands and status

**Tasks**:
- [ ] Implement HTTP server
- [ ] Implement routes:
  - `POST /api/claude/run`
  - `POST /api/claude/stop`
  - `GET /api/status`
  - `GET /api/file?path=...`
  - `GET /health`
- [ ] Write API documentation
- [ ] Write tests

**Deliverables**:
- HTTP API at `http://localhost:8766`
- OpenAPI/Swagger documentation

---

### Phase 8: Integration & Polish (Days 21-25)

**Goals**: End-to-end testing, documentation, release

**Tasks**:
- [ ] End-to-end integration tests
- [ ] Cross-platform testing (macOS Intel, macOS ARM, Windows)
- [ ] Build scripts and Makefile
- [ ] Binary releases for all platforms
- [ ] User documentation (README)
- [ ] Create test client tool

**Deliverables**:
- Release binaries for macOS and Windows
- Complete documentation
- Test client for manual testing

---

## 14. Testing Strategy

### 14.1 Test Structure

```
test/
├── unit/                    # Unit tests (alongside code)
├── integration/             # Integration tests
│   ├── claude_test.go       # Claude manager integration
│   ├── watcher_test.go      # File watcher integration
│   ├── git_test.go          # Git tracker integration
│   └── server_test.go       # WebSocket/HTTP integration
└── fixtures/                # Test fixtures
    ├── repos/               # Sample git repos
    └── configs/             # Test configurations
```

### 14.2 Test Commands

```makefile
# Run all tests
test:
	go test ./...

# Run with race detection
test-race:
	go test -race ./...

# Run integration tests only
test-integration:
	go test -tags=integration ./test/integration/...

# Coverage report
test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out
```

### 14.3 Mocking Strategy

```go
// Use interfaces (ports) for easy mocking
type MockClaudeManager struct {
    StartFunc func(ctx context.Context, prompt string) error
    StopFunc  func(ctx context.Context) error
    state     ClaudeState
}

func (m *MockClaudeManager) Start(ctx context.Context, prompt string) error {
    if m.StartFunc != nil {
        return m.StartFunc(ctx, prompt)
    }
    m.state = ClaudeStateRunning
    return nil
}
```

---

## 15. Build & Distribution

### 15.1 Makefile

```makefile
# Makefile

VERSION ?= $(shell git describe --tags --always --dirty)
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build test clean

all: build

build:
	go build $(LDFLAGS) -o bin/cdev ./cmd/cdev

build-all: build-darwin-arm64 build-darwin-amd64 build-windows-amd64

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) \
		-o dist/cdev-darwin-arm64 ./cmd/cdev

build-darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) \
		-o dist/cdev-darwin-amd64 ./cmd/cdev

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) \
		-o dist/cdev-windows-amd64.exe ./cmd/cdev

test:
	go test -v ./...

test-race:
	go test -race ./...

clean:
	rm -rf bin/ dist/

lint:
	golangci-lint run

fmt:
	go fmt ./...
```

### 15.2 Release Artifacts

```
dist/
├── cdev-darwin-arm64      # macOS Apple Silicon
├── cdev-darwin-amd64      # macOS Intel
├── cdev-windows-amd64.exe # Windows 64-bit
├── checksums.txt                # SHA256 checksums
└── config.example.yaml          # Example configuration
```

---

## 16. Future Considerations

### Post-POC Roadmap

1. **Cloud Relay Integration**
   - Outbound WebSocket to relay server
   - Session management
   - Reconnection handling

2. **Authentication & Security**
   - JWT token authentication
   - TLS encryption
   - Session tokens with expiry

3. **Multi-Session Support**
   - Multiple Claude instances
   - Session isolation
   - Resource limits

4. **Enhanced Git Features**
   - Commit support
   - Branch information
   - Blame integration

5. **Performance Optimizations**
   - Event batching
   - Diff caching
   - Memory profiling

---

## References

### Architecture Patterns
- [Clean Architecture in Go](https://threedots.tech/post/introducing-clean-architecture/)
- [Hexagonal Architecture with Go](https://medium.com/@TonyBologni/implementing-domain-driven-design-and-hexagonal-architecture-with-go-3-f9dfd7ab0a78)
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)

### Libraries Documentation
- [fsnotify](https://github.com/fsnotify/fsnotify)
- [gorilla/websocket](https://github.com/gorilla/websocket)
- [Cobra CLI](https://github.com/spf13/cobra)
- [Viper Configuration](https://github.com/spf13/viper)
- [zerolog Logging](https://github.com/rs/zerolog)

### Process Management
- [Advanced Command Execution in Go](https://blog.kowalczyk.info/article/wOYk/advanced-command-execution-in-go-with-osexec.html)
- [Signal Handling in Go](https://medium.com/@AlexanderObregon/signal-handling-in-go-applications-b96eb61ecb69)

### WebSocket Patterns
- [Gorilla WebSocket Chat Example](https://github.com/gorilla/websocket/tree/main/examples/chat)
- [WebSocket Hub Pattern](https://github.com/gorilla/websocket/blob/main/examples/chat/hub.go)

---

*Document Version: 1.0.0-POC*
*Last Updated: 2024-01-15*
