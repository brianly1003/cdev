# cdev Documentation Index

This directory contains all project documentation organized by category.

---

## Quick Reference

| I need to... | Read this |
|--------------|-----------|
| Integrate with the API | [api/API-REFERENCE.md](./api/API-REFERENCE.md) |
| Understand the protocol | [api/PROTOCOL.md](./api/PROTOCOL.md) |
| Understand the architecture | [architecture/ARCHITECTURE.md](./architecture/ARCHITECTURE.md) |
| Review security concerns | [security/SECURITY.md](./security/SECURITY.md) |
| See planned work | [planning/BACKLOG.md](./planning/BACKLOG.md) |
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
│   ├── TECHNICAL-REVIEW.md    # Security analysis
│   └── IMAGE-UPLOAD-SECURITY-ANALYSIS.md
├── guides/                    # Guides & Testing docs
│   ├── POC-TESTING-GUIDE.md   # POC testing guide
│   └── CLAUDE-CLI.md          # Claude CLI reference
└── planning/                  # Project Management docs
    ├── BACKLOG.md             # Product backlog
    └── STRATEGIC-ROADMAP.md   # Strategic roadmap
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
| [security/TECHNICAL-REVIEW.md](./security/TECHNICAL-REVIEW.md) | Security analysis with specific vulnerabilities |
| [security/IMAGE-UPLOAD-SECURITY-ANALYSIS.md](./security/IMAGE-UPLOAD-SECURITY-ANALYSIS.md) | Image upload security analysis and fixes |

**Key Topics:**
- Current security posture
- Known vulnerabilities and fixes
- Configuration best practices
- Image upload security

---

### 5. Guides & Testing
*For testing and development*

| Document | Description |
|----------|-------------|
| [guides/POC-TESTING-GUIDE.md](./guides/POC-TESTING-GUIDE.md) | POC testing guide with examples |
| [guides/CLAUDE-CLI.md](./guides/CLAUDE-CLI.md) | Claude CLI reference and flags |

---

### 6. Project Management & Strategy
*For planning, tracking, and strategic direction*

| Document | Description |
|----------|-------------|
| [planning/STRATEGIC-ROADMAP.md](./planning/STRATEGIC-ROADMAP.md) | Strategic technology roadmap and scaling plan |
| [planning/BACKLOG.md](./planning/BACKLOG.md) | Product backlog with prioritized work items |

**Key Topics:**
- Strategic roadmap (Production -> Protocol -> Cloud -> Enterprise)
- Core technology assets and ownership
- Prioritized backlog items

---

### 7. Operations & Deployment
*For production deployment* (Planned)

| Document | Status |
|----------|--------|
| DEPLOYMENT.md | Planned |
| MONITORING.md | Planned |
| TROUBLESHOOTING.md | Planned |

---

## Document Status

| Document | Category | Version | Status |
|----------|----------|---------|--------|
| PROTOCOL.md | api | 2.0.0 | Current |
| UNIFIED-API-SPEC.md | api | 1.0 | Current |
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
| SECURITY.md | security | 1.0 | Draft |
| TECHNICAL-REVIEW.md | security | 1.0 | Current |
| IMAGE-UPLOAD-SECURITY-ANALYSIS.md | security | 1.0 | Current |
| POC-TESTING-GUIDE.md | guides | 1.0 | Current |
| CLAUDE-CLI.md | guides | 1.0 | Current |
| STRATEGIC-ROADMAP.md | planning | 1.0 | Active |
| BACKLOG.md | planning | 1.0 | Active |

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
