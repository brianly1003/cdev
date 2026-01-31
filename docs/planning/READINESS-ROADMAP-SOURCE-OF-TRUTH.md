# cdev Readiness + Roadmap (Single Source of Truth)

**Version:** 1.0  
**Status:** Active  
**Last Updated:** January 30, 2026  
**Owner:** Solution Architecture

---

## Purpose

This document is the **authoritative source of truth** for current readiness and near-term roadmap.  
If any other document conflicts with this one, **this document wins** until updated.

**Primary evidence sources (as of Jan 30, 2026):**
- `docs/architecture/ARCHITECTURE.md`
- `docs/architecture/DESIGN-SPEC.md`
- `docs/architecture/MULTI-AGENT-ARCHITECTURE.md`
- `docs/architecture/MULTI-WORKSPACE-DESIGN.md`
- `docs/architecture/TRANSPORT-ARCHITECTURE-ANALYSIS.md`
- `docs/architecture/LOGGING-TRACING-DESIGN.md`
- `docs/security/SECURITY.md`

---

## Executive Summary (Reality Snapshot)

- The **core Go agent** is implemented and functions for local, single-repo workflows.
- **Security posture is partial**; multiple critical/high risks are still open per the security docs.
- **Cloud relay** and **iOS app** are **not implemented in this repo** (iOS is a separate project).
- **Multi-agent** and **multi-workspace** exist as design docs only.
- **Observability** (metrics/tracing) is missing.
- Transport remains **dual-protocol (HTTP + WebSocket)**; consolidation is recommended but not done.

---

## Current Status Matrix

| Area | Status | Evidence | Notes |
|------|--------|----------|-------|
| Core agent (process mgmt, diff, watcher) | Implemented | `docs/architecture/DESIGN-SPEC.md` | Single workspace, local-first |
| JSON-RPC 2.0 protocol docs | Implemented | `docs/api/PROTOCOL.md`, `docs/api/UNIFIED-API-SPEC.md` | Spec exists; verify parity in code when needed |
| Dual HTTP + WebSocket transport | Implemented (duplicated) | `docs/architecture/TRANSPORT-ARCHITECTURE-ANALYSIS.md` | Duplication risk noted |
| Security (auth, CORS, limits) | **Partial / Open Risks** | `docs/security/SECURITY.md` | HTTP unauth, WS localhost bypass, diff/file caps, token in query |
| Observability (metrics/tracing) | Not implemented | `docs/architecture/LOGGING-TRACING-DESIGN.md` | Design exists; implementation missing |
| Multi-agent | Design only | `docs/architecture/MULTI-AGENT-ARCHITECTURE.md` | Current limitation: single agent instance |
| Multi-workspace | Design only | `docs/architecture/MULTI-WORKSPACE-DESIGN.md` | Current limitation: single repo |
| Cloud relay | Not implemented | `docs/architecture/DESIGN-SPEC.md` | LAN only |
| iOS app | Separate project, not in repo | `docs/architecture/DESIGN-SPEC.md` | Status not verifiable in this repo |
| Desktop app (Wails) | In-repo assets, not validated | `frontend/`, `wails.json` | Treat as experimental until verified |

---

## Conflicting Claims and Resolution

| Conflict | Where It Appears | Resolution (Authoritative) |
|----------|------------------|-----------------------------|
| “Security hardening complete / production readiness ~90%” | Legacy planning docs (removed Jan 30, 2026) | **Not accepted**. Security docs list open critical/high risks; treat security as partial until verified and closed. |
| “Auth not a POC goal” vs “Auth implemented” | `docs/architecture/ARCHITECTURE.md` vs `docs/security/SECURITY.md` | Auth may exist partially, but **HTTP API is unauthenticated** per security docs. POC doc is outdated. |
| “iOS app not yet implemented” vs “Production-grade iOS app” | `docs/architecture/DESIGN-SPEC.md` vs legacy planning docs (removed Jan 30, 2026) | **iOS app is not in this repo**; status cannot be claimed here. |
| “Dual protocol OK” vs “Consolidate protocols” | Legacy planning docs (removed Jan 30, 2026) vs `docs/architecture/TRANSPORT-ARCHITECTURE-ANALYSIS.md` | **Recommendation stands:** consolidation reduces duplication. Track as roadmap item. |

---

## Readiness Levels (Definitions)

**POC (Local Solo Dev)**
- Run agent locally, single repo
- WebSocket streaming, basic control
- No guarantees for external exposure

**Beta (Solo Dev Daily Use)**
- Auth enforced for HTTP + WebSocket
- No localhost auth bypass
- Path validation hardened
- Diff/file size caps enforced
- Clear remote-access guidance (tunnels)

**Production (Paid / Public)**
- Security hardening verified by tests
- Observability (metrics/logs)
- Robust transport (single protocol or hardened dual)
- Multi-workspace or clean isolation strategy
- Cloud relay or secure remote access story

---

## Roadmap (Proposed, Reality-Based)

### Phase 0 — Truth + Safety (Now → 2 weeks)
- Update conflicting docs to align with this source
- Close security gaps listed in `docs/security/SECURITY.md`
- Add tests for auth, path validation, diff/file limits
- Publish a short “Safe Remote Access” guide

### Phase 1 — Solo Dev Reliability (2 → 6 weeks)
- Transport consolidation decision: single port + JSON-RPC
- Improve connection health UX (mobile)
- Dev server preview guidance (documented)
- Stabilize LIVE session handling (terminal + PTY)

### Phase 2 — Segment Expansion (6 → 12 weeks)
- Multi-workspace manager (Option B)
- Observability (metrics + tracing)
- Optional cloud relay MVP

---

## Action Items to Resolve Documentation Drift

- Keep `docs/README.md` pointing to this document as the authoritative source.
- Update or remove any future planning docs that conflict with this status.

---

## Change Log

- 2026-01-30: Initial authoritative draft created.
