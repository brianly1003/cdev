# cdev Documentation Index

This directory contains all project documentation organized by category.

---

## Quick Reference

| I need to... | Read this |
|--------------|-----------|
| Run setup diagnostics | [guides/TROUBLESHOOTING.md](./guides/TROUBLESHOOTING.md) (`cdev doctor`) |
| Integrate with the API | [api/API-REFERENCE.md](./api/API-REFERENCE.md) |
| Understand the protocol | [api/PROTOCOL.md](./api/PROTOCOL.md) |
| Understand runtime capability contract | [api/RUNTIME-CAPABILITY-REGISTRY.md](./api/RUNTIME-CAPABILITY-REGISTRY.md) |
| Understand the architecture | [architecture/ARCHITECTURE.md](./architecture/ARCHITECTURE.md) |
| Review security concerns | [security/SECURITY.md](./security/SECURITY.md) |
| Safely access remotely | [guides/SAFE-REMOTE-ACCESS.md](./guides/SAFE-REMOTE-ACCESS.md) |
| See planned work | [planning/BACKLOG.md](./planning/BACKLOG.md) |
| Turn cdev into an AI Agent runtime | [planning/AI-AGENT-RUNTIME-ROADMAP.md](./planning/AI-AGENT-RUNTIME-ROADMAP.md) |
| Execute market readiness plan | [planning/MARKET-READINESS-EXECUTION-PLAN-2026.md](./planning/MARKET-READINESS-EXECUTION-PLAN-2026.md) |
| **Setup workspace manager** | [guides/WORKSPACE-MANAGER-SETUP.md](./guides/WORKSPACE-MANAGER-SETUP.md) |
| **Fix a problem** | [guides/TROUBLESHOOTING.md](./guides/TROUBLESHOOTING.md) |
| **Integrate iOS app** | [mobile/IOS-INTEGRATION-GUIDE.md](./mobile/IOS-INTEGRATION-GUIDE.md) |
| **LIVE session support** | [mobile/LIVE-SESSION-INTEGRATION.md](./mobile/LIVE-SESSION-INTEGRATION.md) |
| **Integrate with VS Code** | [architecture/VSCODE-INTEGRATION-STRATEGY.md](./architecture/VSCODE-INTEGRATION-STRATEGY.md) |
| **Acquisition strategy** | [architecture/ACQUISITION-READY-ARCHITECTURE.md](./architecture/ACQUISITION-READY-ARCHITECTURE.md) |
| **Multi-agent (Claude/Gemini/Codex)** | [architecture/MULTI-AGENT-ARCHITECTURE.md](./architecture/MULTI-AGENT-ARCHITECTURE.md) |

---

## Directory Structure

```
docs/
├── README.md                  # This index file
├── api/                       # API & Integration docs
│   ├── PROTOCOL.md            # Protocol specification (JSON-RPC 2.0 + legacy)
│   ├── UNIFIED-API-SPEC.md    # JSON-RPC 2.0 method reference
│   ├── RUNTIME-CAPABILITY-REGISTRY.md # Runtime contract for server-driven runtime behavior
│   ├── API-REFERENCE.md       # Complete HTTP and WebSocket API
│   ├── ELEMENTS-API.md        # Pre-parsed UI elements for mobile
│   ├── WEBSOCKET-STABILITY.md # WebSocket connection stability guide
│   └── REALTIME-CHAT-INTEGRATION.md
├── architecture/              # Architecture & Design docs
│   ├── ARCHITECTURE.md        # Technical architecture
│   ├── DESIGN-SPEC.md         # Design specification with status
│   ├── REPOSITORY-INDEXER.md  # File search/browsing API
│   ├── DESKTOP-APP-DESIGN.md  # Desktop app design spec
│   ├── ACQUISITION-READY-ARCHITECTURE.md  # Acquisition strategy
│   ├── VSCODE-INTEGRATION-STRATEGY.md     # VS Code integration guide
│   ├── TRANSPORT-ARCHITECTURE-ANALYSIS.md # WebSocket vs HTTP analysis
│   └── MULTI-AGENT-ARCHITECTURE.md        # Multi-agent (Claude/Gemini/Codex)
├── security/                  # Security docs
│   ├── SECURITY.md            # Security guidelines & threat model
│   ├── TOKEN-ARCHITECTURE.md  # Token model and lifecycle
│   ├── TUNNEL-PROXY-HARDENING.md # Tunnel/proxy deployment hardening
│   └── IMAGE-UPLOAD-SECURITY-ANALYSIS.md
├── mobile/                    # Mobile Integration docs
│   ├── IOS-INTEGRATION-GUIDE.md       # iOS integration reference
│   ├── IOS-WORKSPACE-INTEGRATION.md   # Multi-workspace iOS guide
│   └── LIVE-SESSION-INTEGRATION.md    # LIVE session support for terminal sessions
├── guides/                    # Guides & Testing docs
│   ├── WORKSPACE-MANAGER-SETUP.md  # Workspace manager setup with VS Code tunnels
│   ├── SAFE-REMOTE-ACCESS.md  # Safe remote access (tunnels + auth)
│   ├── POC-TESTING-GUIDE.md   # POC testing guide
│   ├── CLAUDE-CLI.md          # Claude CLI reference
│   └── TROUBLESHOOTING.md     # Common issues and solutions
└── planning/                  # Project Management docs
    ├── BACKLOG.md             # Product backlog
    ├── AI-AGENT-RUNTIME-ROADMAP.md # Transformation plan: controller -> AI Agent runtime
    ├── READINESS-ROADMAP-SOURCE-OF-TRUTH.md # Authoritative readiness/roadmap
    └── POSITIONING-GTM-SOLO-DEV.md # Positioning + go-to-market plan
```

---

## Documentation Categories

### 1. Getting Started
*For new developers and users*

| Document | Description |
|----------|-------------|
| [../README.md](../README.md) | Project overview, installation, and quick start |

---

### 2. API & Integration
*For mobile app developers and integration services*

| Document | Description |
|----------|-------------|
| [api/PROTOCOL.md](./api/PROTOCOL.md) | **Protocol specification** - JSON-RPC 2.0 + legacy formats |
| [api/UNIFIED-API-SPEC.md](./api/UNIFIED-API-SPEC.md) | **JSON-RPC 2.0 API** - Complete method reference with examples |
| [api/RUNTIME-CAPABILITY-REGISTRY.md](./api/RUNTIME-CAPABILITY-REGISTRY.md) | Runtime Capability Registry contract (`initialize.capabilities.runtimeRegistry`) |
| [api/API-REFERENCE.md](./api/API-REFERENCE.md) | Complete HTTP and WebSocket API documentation |
| [api/ELEMENTS-API.md](./api/ELEMENTS-API.md) | Pre-parsed UI elements for rich mobile rendering |
| [api/WEBSOCKET-STABILITY.md](./api/WEBSOCKET-STABILITY.md) | WebSocket connection stability and heartbeat |
| [api/REALTIME-CHAT-INTEGRATION.md](./api/REALTIME-CHAT-INTEGRATION.md) | Real-time chat integration guide |
| [Swagger UI](http://localhost:8766/swagger/) | Interactive API explorer (when agent is running) |
| [OpenRPC Discovery](http://localhost:8766/api/rpc/discover) | JSON-RPC 2.0 method discovery (when agent is running) |

**Key Topics:**
- JSON-RPC 2.0 protocol (recommended) and legacy command format
- HTTP endpoints (`/api/claude/*`, `/api/git/*`, `/api/repository/*`, `/api/images/*`)
- WebSocket events and commands (`ws://localhost:8766/ws`)
- Session management (new/continue)
- Permission and interactive prompt handling
- Image upload API

---

### 3. Architecture & Design
*For contributors and maintainers*

| Document | Description |
|----------|-------------|
| [architecture/ARCHITECTURE.md](./architecture/ARCHITECTURE.md) | Technical architecture and component design |
| [architecture/DESIGN-SPEC.md](./architecture/DESIGN-SPEC.md) | Original design specification with implementation status |
| [architecture/REPOSITORY-INDEXER.md](./architecture/REPOSITORY-INDEXER.md) | File search and browsing API with iOS examples |
| [architecture/DESKTOP-APP-DESIGN.md](./architecture/DESKTOP-APP-DESIGN.md) | Desktop app design specification |
| [architecture/ACQUISITION-READY-ARCHITECTURE.md](./architecture/ACQUISITION-READY-ARCHITECTURE.md) | **Strategy** - Making cdev acquisition-ready for VS Code/Microsoft |
| [architecture/VSCODE-INTEGRATION-STRATEGY.md](./architecture/VSCODE-INTEGRATION-STRATEGY.md) | **Strategy** - Detailed VS Code integration and JSON-RPC 2.0 migration |
| [architecture/TRANSPORT-ARCHITECTURE-ANALYSIS.md](./architecture/TRANSPORT-ARCHITECTURE-ANALYSIS.md) | **Analysis** - WebSocket vs HTTP dual-protocol evaluation |
| [architecture/MULTI-AGENT-ARCHITECTURE.md](./architecture/MULTI-AGENT-ARCHITECTURE.md) | **Strategy** - Multi-agent support for Claude, Gemini, and Codex |

**Key Topics:**
- Hexagonal architecture (ports & adapters)
- Event hub pattern
- Component interactions
- Cross-platform considerations
- JSON-RPC 2.0 protocol alignment (LSP-compatible)
- VS Code extension architecture
- Acquisition readiness and multi-IDE support
- Multi-agent support (Claude, Gemini, Codex)

---

### 4. Security
*For security review and production deployment*

| Document | Description |
|----------|-------------|
| [security/SECURITY.md](./security/SECURITY.md) | Security guidelines, threat model, and best practices |
| [security/TOKEN-ARCHITECTURE.md](./security/TOKEN-ARCHITECTURE.md) | Token lifecycle and auth model |
| [security/IMAGE-UPLOAD-SECURITY-ANALYSIS.md](./security/IMAGE-UPLOAD-SECURITY-ANALYSIS.md) | Image upload security analysis and fixes |
| [security/TUNNEL-PROXY-HARDENING.md](./security/TUNNEL-PROXY-HARDENING.md) | Tunnel/proxy deployment hardening checklist |

**Key Topics:**
- Current security posture
- Known vulnerabilities and fixes
- Configuration best practices
- Image upload security

---

### 5. Mobile Integration
*For iOS/mobile app developers*

| Document | Description |
|----------|-------------|
| [mobile/IOS-INTEGRATION-GUIDE.md](./mobile/IOS-INTEGRATION-GUIDE.md) | Complete iOS integration reference |
| [mobile/IOS-WORKSPACE-INTEGRATION.md](./mobile/IOS-WORKSPACE-INTEGRATION.md) | Multi-workspace support for iOS |
| [mobile/LIVE-SESSION-INTEGRATION.md](./mobile/LIVE-SESSION-INTEGRATION.md) | **LIVE session support** - Watch and interact with terminal sessions |

**Key Topics:**
- JSON-RPC 2.0 WebSocket integration
- Session types: managed, live, historical
- Real-time session watching
- Permission handling UI
- TTY injection for LIVE sessions

---

### 6. Guides & Testing
*For testing and development*

| Document | Description |
|----------|-------------|
| [guides/WORKSPACE-MANAGER-SETUP.md](./guides/WORKSPACE-MANAGER-SETUP.md) | **Step-by-step workspace manager setup with VS Code tunnels** |
| [guides/SAFE-REMOTE-ACCESS.md](./guides/SAFE-REMOTE-ACCESS.md) | Safe remote access with tunnels + auth |
| [guides/POC-TESTING-GUIDE.md](./guides/POC-TESTING-GUIDE.md) | POC testing guide with examples |
| [guides/CLAUDE-CLI.md](./guides/CLAUDE-CLI.md) | Claude CLI reference and flags |
| [guides/TROUBLESHOOTING.md](./guides/TROUBLESHOOTING.md) | Common issues and solutions |

---

### 7. Project Management & Strategy
*For planning, tracking, and strategic direction*

| Document | Description |
|----------|-------------|
| [planning/BACKLOG.md](./planning/BACKLOG.md) | Product backlog with prioritized work items |
| [planning/AI-AGENT-RUNTIME-ROADMAP.md](./planning/AI-AGENT-RUNTIME-ROADMAP.md) | Phased plan to evolve cdev into a secure AI Agent runtime |
| [planning/MARKET-READINESS-EXECUTION-PLAN-2026.md](./planning/MARKET-READINESS-EXECUTION-PLAN-2026.md) | Actionable 30/60/90 strategy for cdev + cdev-ios market readiness |
| [planning/READINESS-ROADMAP-SOURCE-OF-TRUTH.md](./planning/READINESS-ROADMAP-SOURCE-OF-TRUTH.md) | **Authoritative** readiness + roadmap status |
| [planning/POSITIONING-GTM-SOLO-DEV.md](./planning/POSITIONING-GTM-SOLO-DEV.md) | Positioning + go-to-market plan (solo devs) |

**Key Topics:**
- Authoritative readiness + roadmap status
- Positioning + go-to-market
- Prioritized backlog items

---

### 8. Operations & Deployment
*For production deployment*

| Document | Description |
|----------|-------------|
| [guides/TROUBLESHOOTING.md](./guides/TROUBLESHOOTING.md) | Common issues and solutions |
| DEPLOYMENT.md | Planned |
| MONITORING.md | Planned |

---

## Document Status

| Document | Category | Version | Status |
|----------|----------|---------|--------|
| PROTOCOL.md | api | 2.0.0 | Current |
| UNIFIED-API-SPEC.md | api | 1.0 | Current |
| RUNTIME-CAPABILITY-REGISTRY.md | api | 1.0 | Draft |
| API-REFERENCE.md | api | 1.1 | Current |
| ELEMENTS-API.md | api | 1.0 | Current |
| WEBSOCKET-STABILITY.md | api | 1.0 | Current |
| ARCHITECTURE.md | architecture | 1.0 | Current |
| DESIGN-SPEC.md | architecture | 1.0 | Current |
| REPOSITORY-INDEXER.md | architecture | 1.0 | Current |
| DESKTOP-APP-DESIGN.md | architecture | 1.0 | Current |
| ACQUISITION-READY-ARCHITECTURE.md | architecture | 1.0 | Active |
| VSCODE-INTEGRATION-STRATEGY.md | architecture | 1.0 | Active |
| TRANSPORT-ARCHITECTURE-ANALYSIS.md | architecture | 1.0 | Active |
| MULTI-AGENT-ARCHITECTURE.md | architecture | 1.0 | Active |
| SECURITY.md | security | 1.1 | Active |
| TOKEN-ARCHITECTURE.md | security | 1.0 | Current |
| IMAGE-UPLOAD-SECURITY-ANALYSIS.md | security | 1.0 | Current |
| TUNNEL-PROXY-HARDENING.md | security | 1.0 | Current |
| WORKSPACE-MANAGER-SETUP.md | guides | 1.0 | Current |
| POC-TESTING-GUIDE.md | guides | 1.0 | Current |
| CLAUDE-CLI.md | guides | 1.0 | Current |
| TROUBLESHOOTING.md | guides | 1.1 | Current |
| BACKLOG.md | planning | 1.0 | Active |
| READINESS-ROADMAP-SOURCE-OF-TRUTH.md | planning | 1.0 | Active |
| AI-AGENT-RUNTIME-ROADMAP.md | planning | 1.0 | Active |
| MARKET-READINESS-EXECUTION-PLAN-2026.md | planning | 1.0 | Draft |
| POSITIONING-GTM-SOLO-DEV.md | planning | 1.0 | Draft |

---

## Documentation Standards

### File Naming
- Use `UPPERCASE-KEBAB-CASE.md` for documentation files
- Use lowercase for code-related files

### Folder Structure
- `api/` - API documentation for integrators
- `architecture/` - Technical architecture for contributors
- `security/` - Security documentation
- `guides/` - How-to guides and tutorials
- `planning/` - Project management docs

### Updates
- Update document status when making changes
- Keep BACKLOG.md synchronized with actual work
