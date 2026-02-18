# Market Readiness Execution Plan (2026)

**Status:** Draft for execution  
**Updated:** 2026-02-17  
**Scope:** cdev (Go backend) + cdev-ios (SwiftUI client)

---

## Purpose

This document turns the product/architecture review into an actionable plan you can execute.

Primary goal:
- move cdev/cdev-ios from "feature accumulation" to "reliable, extensible AI agent platform" quality.

Secondary goal:
- close critical product gaps against top developer AI tools while keeping cdev's mobile-first supervision advantage.

---

## Strategic Outcome (Target in 90 Days)

1. Add new runtimes (Gemini, others) without major cross-codebase rewrites.
2. Reduce runtime/session regressions by introducing protocol contracts and automated tests.
3. Improve activation and reliability with guided onboarding and self-healing diagnostics.
4. Deliver a clearer AI-native UX on mobile (timeline-first, command palette, better iPhone/iPad adaptation).

---

## What To Do First (Start Here)

If you do only one thing first, do this:

1. Define and ship the **Runtime Capability Registry contract**.

Why first:
- It is the dependency for safe multi-agent scaling (Claude, Codex, Gemini, future runtimes).
- It reduces code branching in both backend and iOS before more features are added.
- It lowers regression risk for session/watch/send logic immediately.

Execution order for Week 1:

1. Day 1: Write capability schema doc and examples.
2. Day 2: Expose capability metadata from backend (`initialize` or dedicated RPC method).
3. Day 3: Add iOS capability model and fallback defaults.
4. Day 4: Route one real flow by capability (session watch strategy).
5. Day 5: Add protocol contract tests for `session/start`, `session/send`, `workspace/session/watch`.
6. Day 6: Add `cdev doctor` command skeleton (read-only diagnostics).
7. Day 7: Run full regression pass and document findings.

Hard rule:
- Do not start new UX features before steps 1-5 above are complete.

---

## Current Gaps (High Priority)

1. Runtime extensibility is still hardcoded.
- Backend runtime dispatch is fixed in `internal/rpc/handler/methods/session_manager_runtime.go`.
- RPC schemas still explicitly enumerate `claude|codex` in `internal/rpc/handler/methods/session_manager.go`.
- iOS runtime behavior is centralized but still enum-hardcoded in `cdev/Domain/Models/AgentRuntime.swift`.

2. cdev-ios orchestration complexity is too high in one ViewModel.
- `cdev/Presentation/Screens/Dashboard/DashboardViewModel.swift` currently owns too many concerns.

3. Command UX discoverability is limited.
- Slash command suggestions are strict/prefix-based in `cdev/Presentation/Screens/Dashboard/DashboardView.swift`.

4. Automated testing is not strong enough for safe runtime evolution.
- cdev has solid Go tests, but cross-runtime protocol contracts should be expanded.
- cdev-ios unit/UI test implementation is still thin relative to current feature complexity.

5. Operator onboarding and self-healing are below market standard.
- cdev has good status/health primitives, but no first-class `onboard` and `doctor` UX flow.

---

## Workstreams

### W1. Runtime Capability Registry (Backend + iOS)

Objective:
- Replace hardcoded runtime branching with capability-driven behavior.

Deliverables:
- Add runtime capability metadata endpoint (via `initialize` capabilities or dedicated method).
- Define capability model:
  - session list/messages/watch source
  - supports resume
  - permission interaction mode
  - session resolution behavior
  - command support matrix
- Refactor backend dispatch to use adapter registration instead of hardcoded enum checks.
- Refactor iOS runtime switching and method routing to consume capability metadata.

Definition of done:
- New runtime can be added by adapter + capability config with minimal UI changes.
- Existing Claude/Codex behavior remains backward-compatible.

---

### W2. Session Reliability + Protocol Contracts

Objective:
- Make session/watch/send behavior deterministic across reconnects and runtime switches.

Deliverables:
- Add protocol contract tests for:
  - `session/start`, `session/send`, `workspace/session/watch`, `workspace/session/messages`
  - runtime filtering by `agent_type`
  - error mapping and recovery flow (session not found, permission pending, trust declined)
- Add JSON fixture replay tests using real session transcripts for Claude/Codex.
- Add reconnect-state tests for iOS (foreground/background, runtime switching, history reload).

Definition of done:
- Regression suite fails on protocol drift before release.
- Known watch/send race conditions are reproduced and covered by tests.

---

### W3. cdev Doctor + Onboard Experience

Objective:
- Reduce setup friction and support burden with guided install/repair.

Deliverables:
- Add `cdev onboard` command:
  - guided local setup
  - auth token setup
  - workspace discovery and validation
  - initial runtime smoke test
- Add `cdev doctor` command:
  - config validation
  - token/auth checks
  - workspace/session index checks
  - watcher health checks
  - auto-fix or guided fix suggestions
- Add docs: troubleshooting matrix and quick "repair flow".

Definition of done:
- New user can pair and run first session in less than 10 minutes.
- Common failures can be diagnosed and fixed from CLI without manual deep debugging.

---

### W4. Mobile UX Upgrade (AI-Native, Multi-Form-Factor)

Objective:
- Improve comprehension and control on iPhone/iPad during active agent runs.

Deliverables:
- Add timeline-first rendering mode:
  - Plan step
  - Tool action
  - Diff evidence
  - Risk/approval
  - Result
- Keep raw terminal stream as collapsible secondary layer.
- Add command palette behavior:
  - `/` and quick command search
  - supports inline invocation (not only first character)
- Improve compact layout behavior:
  - prevent truncation and unstable input area shifts
  - define iPhone compact mode and iPad expanded mode variants.

Definition of done:
- Users can understand "what happened and what is next" without parsing raw logs.
- Input/action area remains stable during streaming and keyboard transitions.

---

### W5. Quality Gates + Release Discipline

Objective:
- Stabilize release confidence as feature scope grows.

Deliverables:
- Add release gates:
  - protocol contract tests pass
  - reconnect scenarios pass
  - runtime switch scenarios pass
  - performance baseline checks pass
- Add changelog template with migration notes when protocol changes.
- Add staged release process:
  - internal dogfood
  - beta testers
  - wider rollout

Definition of done:
- Fewer production regressions after runtime/session changes.
- Faster diagnosis when failures occur.

---

## 30/60/90 Day Plan

## Day 0-30 (Foundation)

1. Implement Runtime Capability Registry skeleton.
2. Add contract test suite scaffold with Claude/Codex fixtures.
3. Start splitting `DashboardViewModel` into focused coordinators.
4. Deliver initial `cdev doctor` command with read-only diagnostics.

Exit criteria:
- Capability schema exists and is used by both backend and iOS for at least one flow.
- Contract test job runs in CI.
- Doctor command identifies major setup/runtime issues.

## Day 31-60 (Stabilization)

1. Complete runtime adapter registration refactor.
2. Complete reconnect/runtime-switch test coverage.
3. Add `cdev onboard` guided flow.
4. Ship timeline-first UI beta and command palette improvements.

Exit criteria:
- New runtime prototype can be integrated without broad code rewrites.
- Runtime/session incidents drop measurably.
- Onboarding success rate improves.

## Day 61-90 (Scale Readiness)

1. Add Gemini runtime using new capability model (pilot).
2. Complete doctor auto-fix actions for common failures.
3. Harden iPhone/iPad responsive variants and interaction polish.
4. Finalize release gates and rollout policy.

Exit criteria:
- Gemini (or next runtime) functions via capability-driven path.
- Support load from setup/runtime issues reduced.
- Product readiness for broader distribution improves.

---

## Prioritized Backlog (Execution Order)

### P0 (Do first)

- Runtime capability schema + adapter registration.
- Protocol contract tests for session lifecycle and watch/send.
- DashboardViewModel decomposition plan and first extraction.
- `cdev doctor` read-only diagnostics.

### P1 (Do next)

- `cdev onboard` guided setup.
- Timeline-first UI mode + terminal fallback.
- Command palette and inline slash command resolution.
- iPhone/iPad compact/expanded layout hardening.

### P2 (Then)

- Doctor auto-fix actions.
- Gemini runtime pilot integration.
- Advanced mobile features (widgets/live activity/watch) after core reliability is stable.

---

## KPIs to Track Weekly

1. Activation:
- time-to-first-successful-session
- pairing success rate

2. Reliability:
- session/watch failures per active user
- reconnect recovery success rate
- runtime switch failure rate

3. UX:
- command success rate (slash/palette)
- approval response completion rate
- session comprehension proxy (timeline usage vs raw log fallback)

4. Engineering velocity:
- lead time for runtime-related changes
- escaped defects per release

---

## Immediate Checklist (Next 7 Days)

- [x] Create Runtime Capability Registry spec doc and JSON schema (`docs/api/RUNTIME-CAPABILITY-REGISTRY.md`).
- [x] Add backend capability response endpoint (or `initialize` extension) with `runtimeRegistry` in `initialize.capabilities`.
- [x] Add iOS capability consumer model and fallback defaults.
- [x] Add contract tests for `session/start`, `session/send`, `workspace/session/watch`.
- [ ] Extract first coordinator from `DashboardViewModel` (runtime switching).
- [x] Implement `cdev doctor` command skeleton and output format.
- [x] Document doctor output and remediation hints in `docs/guides/TROUBLESHOOTING.md`.
- [ ] Define release gate checklist for runtime/session changes.
- [ ] Add dashboard command palette task to iOS backlog.
- [ ] Run baseline metrics collection before refactor for comparison.

### Execution Status Snapshot (as of 2026-02-17)

Completed:
- Day 1: Runtime Capability Registry contract (`docs/api/RUNTIME-CAPABILITY-REGISTRY.md`).
- Day 2: Backend capability payload (`initialize.capabilities.runtimeRegistry`).
- Day 3: iOS capability consumer + fallback defaults.
- Day 4: Session watch routing through runtime capability metadata.
- Day 5: Contract tests for session lifecycle methods.
- Day 6: `cdev doctor` command skeleton + troubleshooting/output contract docs.

Next:
- Day 7 regression pass and findings summary.

---

## References

- cdev roadmap: `docs/planning/AI-AGENT-RUNTIME-ROADMAP.md`
- backlog: `docs/planning/BACKLOG.md`
- positioning: `docs/planning/POSITIONING-GTM-SOLO-DEV.md`
- external benchmark reference: OpenClaw architecture and operations model
- market context references:
  - JetBrains AI tools analysis (2026)
  - Times of India coverage on AI and software engineering role shift
