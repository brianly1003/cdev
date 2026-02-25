# AI Agent Runtime Roadmap (cdev)

**Status:** Draft for execution  
**Updated:** 2026-02-16  
**Owner:** cdev core team

---

## Why this roadmap exists

cdev currently works well as a monitor and controller for AI coding CLIs.  
The next step is to turn cdev into an AI Agent runtime platform that can plan, execute, and supervise long-running goals safely.

This roadmap explains what changes first, why they matter, and how we will roll them out without breaking current users.

## Current state (already strong)

- JSON-RPC 2.0 transport and API discovery are in place.
- Multi-workspace orchestration already exists.
- Agent adapters (Claude/Codex/Gemini) already exist.
- Real-time event streaming and mobile integration already exist.
- Permission flows and session memory already exist.

## Target state (what "AI Agent" means here)

cdev becomes a goal-driven runtime with:

- explicit goal lifecycle (`create`, `plan`, `execute`, `pause`, `resume`, `cancel`, `result`)
- policy-gated tools and events
- memory + planning loop per goal
- autonomous triggers (cron/webhook/repo events)
- optional multi-agent collaboration for specialized subtasks

## Guiding principles

1. Security before autonomy.
2. Backward compatibility for existing mobile and API clients.
3. Incremental delivery by phase (no rewrite).
4. Observable behavior: every step leaves an audit trail.
5. Keep adapters provider-agnostic.

## Phased plan

### Phase 0: Security and protocol foundation (P0)

**Goal:** Harden cdev control boundaries before adding autonomy.

Scope:

- add method/event role-scope authorization matrix
- add strict JSON-RPC request/param validation at boundary
- add dedicated WebSocket auth failure rate limiting
- migrate critical state writes to atomic write helpers

Why first:

- autonomy multiplies blast radius
- this phase reduces unauthorized access, malformed input risk, and state corruption risk

Exit criteria:

- unauthorized methods/events are rejected consistently
- malformed payloads never reach handlers
- repeated auth failures are throttled
- config/auth/cache writes are atomic

---

### Phase 1: Agent runtime kernel (P1)

**Goal:** Introduce goal-oriented runtime on top of current session engine.

Scope:

- define goal model and state machine
- add RPC methods for goal lifecycle
- map goal execution to existing session manager
- persist goal state and execution timeline

Exit criteria:

- a goal can be created, planned, executed, paused, resumed, canceled
- all goal transitions are persisted and queryable
- existing `agent/*` and `session/*` paths remain functional

---

### Phase 2: Planning, memory, and tool registry (P1)

**Goal:** Move from command execution to agentic decision loops.

Scope:

- planner interface (task decomposition + retries)
- structured memory per goal/session/workspace
- tool registry with schema, risk tier, and policy requirements
- approval model for high-risk tools

Exit criteria:

- planner can produce and track multi-step plans
- tool usage is policy checked before execution
- memory is used for better continuation and recovery

---

### Phase 3: Controlled autonomy and multi-agent orchestration (P2)

**Goal:** Enable automation and parallel specialization safely.

Scope:

- trigger engine (cron, webhook, git events)
- policy budgets (time/tool/cost limits)
- specialized worker agents (planner, coder, reviewer)
- supervisor reconciliation and final output synthesis

Exit criteria:

- unattended runs execute within explicit guardrails
- multi-agent flows can be monitored and audited end to end
- failure handling is deterministic and recoverable

## Architecture change map

| Area | Existing component | Planned evolution |
|------|--------------------|-------------------|
| Authz | `internal/rpc/handler/dispatcher.go` | Central method/event policy gate before dispatch |
| Protocol validation | `internal/rpc/message/jsonrpc.go` | Strict frame + params validation layer |
| Runtime core | `internal/session/manager.go` | Goal lifecycle coordinator above sessions |
| Tooling | `internal/rpc/handler/methods/*` | Tool registry + policy metadata |
| Security ops | `internal/security/*` | `security audit` and safe-fix workflow |
| Persistence | config/security/cache writers | Atomic writes and stronger consistency |

## First 30-day execution plan

### Week 1

- finalize method/event policy matrix
- define strict JSON-RPC validation contract
- write test cases for deny/allow behavior

### Week 2

- implement policy gate in dispatcher path
- implement validation gate in request parse/dispatch path
- add WebSocket auth limiter

### Week 3

- implement atomic write helper and migrate critical writers
- document new security behavior and migration notes
- release behind compatibility-safe defaults

### Week 4

- define goal entity schema + state machine
- add `goal/create`, `goal/list`, `goal/get` (read-first)
- start `goal/execute` integration with session manager

## Success metrics

- Security: unauthorized RPC success rate = 0 for protected methods
- Reliability: zero truncated state file incidents after atomic write migration
- Runtime: goal lifecycle APIs stable and documented
- DX: existing clients continue working without forced migration

## Non-goals (initial roadmap)

- replacing all existing `agent/*` and `session/*` APIs immediately
- forcing one provider model (Claude-only, Codex-only, etc.)
- full autonomous mode by default

## Open decisions

- how strict default policy should be for older clients
- whether goal planning uses one model or provider-specific planners
- how to expose policy failures in mobile UX without noise

---

## Related docs

- [Backlog](./BACKLOG.md)
- [Readiness roadmap source of truth](./READINESS-ROADMAP-SOURCE-OF-TRUTH.md)
- [Architecture](../architecture/ARCHITECTURE.md)
- [Security](../security/SECURITY.md)
