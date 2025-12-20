# cdev Documentation Index

This directory contains all project documentation organized by category.

---

## Quick Reference

| I need to... | Read this |
|--------------|-----------|
| Integrate with the API | [API-REFERENCE.md](./API-REFERENCE.md) |
| Understand the protocol | [PROTOCOL.md](./PROTOCOL.md) |
| Understand the architecture | [ARCHITECTURE.md](./ARCHITECTURE.md) |
| Review security concerns | [SECURITY.md](./SECURITY.md) |
| See planned work | [BACKLOG.md](./BACKLOG.md) |

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
| [PROTOCOL.md](./PROTOCOL.md) | **Protocol specification** - Events, commands, message formats |
| [API-REFERENCE.md](./API-REFERENCE.md) | Complete HTTP and WebSocket API documentation |
| [REPOSITORY-INDEXER.md](./REPOSITORY-INDEXER.md) | File search and browsing API with iOS examples |
| [ELEMENTS-API.md](./ELEMENTS-API.md) | Pre-parsed UI elements for rich mobile rendering |
| [Swagger UI](http://localhost:8766/swagger/) | Interactive API explorer (when agent is running) |

**Key Topics:**
- Protocol specification and versioning
- HTTP endpoints (`/api/claude/*`, `/api/git/*`, `/api/repository/*`)
- WebSocket events and commands
- Session management (new/continue/resume)
- Permission and interactive prompt handling
- Repository file search and browsing

---

### 3. Architecture & Design
*For contributors and maintainers*

| Document | Description |
|----------|-------------|
| [ARCHITECTURE.md](./ARCHITECTURE.md) | Technical architecture and component design |
| [DESIGN-SPEC.md](./DESIGN-SPEC.md) | Original design specification with implementation status |

**Key Topics:**
- Hexagonal architecture (ports & adapters)
- Event hub pattern
- Component interactions
- Cross-platform considerations
- Protocol specification

---

### 4. Security
*For security review and production deployment*

| Document | Description |
|----------|-------------|
| [SECURITY.md](./SECURITY.md) | Security guidelines, threat model, and best practices |
| [TECHNICAL-REVIEW.md](./TECHNICAL-REVIEW.md) | Security analysis with specific vulnerabilities |

**Key Topics:**
- Current security posture
- Known vulnerabilities and fixes
- Configuration best practices
- Incident response

---

### 5. Project Management & Strategy
*For planning, tracking, and strategic direction*

| Document | Description |
|----------|-------------|
| [STRATEGIC-ROADMAP.md](./STRATEGIC-ROADMAP.md) | Strategic technology roadmap and scaling plan |
| [BACKLOG.md](./BACKLOG.md) | Product backlog with prioritized work items |
| [TECHNICAL-REVIEW.md](./TECHNICAL-REVIEW.md) | Technical review with roadmap |

**Key Topics:**
- Strategic roadmap (Production → Protocol → Cloud → Enterprise)
- Core technology assets and ownership
- Competitive analysis and moat building
- Monetization strategy
- Phased roadmap (Security → Testing → Performance → Production)
- Prioritized backlog items

---

### 6. Operations & Deployment
*For production deployment* (Planned)

| Document | Status |
|----------|--------|
| DEPLOYMENT.md | Planned |
| MONITORING.md | Planned |
| TROUBLESHOOTING.md | Planned |

---

## Document Status

| Document | Version | Status | Last Updated |
|----------|---------|--------|--------------|
| README.md | 1.0 | Current | Dec 2025 |
| PROTOCOL.md | 1.0.0-draft | Draft | Dec 2025 |
| API-REFERENCE.md | 1.1 | Current | Dec 2025 |
| REPOSITORY-INDEXER.md | 1.0 | Current | Dec 2025 |
| ELEMENTS-API.md | 1.0 | Current | Dec 2025 |
| ARCHITECTURE.md | 1.0 | Current | Dec 2025 |
| DESIGN-SPEC.md | 1.0 | Current | Dec 2025 |
| SECURITY.md | 1.0 | Draft | Dec 2025 |
| STRATEGIC-ROADMAP.md | 1.0 | Active | Dec 2025 |
| BACKLOG.md | 1.0 | Active | Dec 2025 |
| TECHNICAL-REVIEW.md | 1.0 | Current | Dec 2025 |

---

## Documentation Standards

### File Naming
- Use `UPPERCASE-KEBAB-CASE.md` for documentation files
- Use lowercase for code-related files

### Content Structure
1. Title and metadata (version, date, status)
2. Executive summary or overview
3. Main content with clear sections
4. References and links

### Updates
- Update "Last Updated" when making changes
- Increment version for significant changes
- Keep BACKLOG.md synchronized with actual work

---

## Contributing to Documentation

1. Follow existing document structure
2. Keep language concise and technical
3. Include code examples where helpful
4. Update the index when adding new documents
5. Mark document status (Draft, Current, Deprecated)
