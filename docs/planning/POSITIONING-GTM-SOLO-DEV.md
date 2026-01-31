# Positioning + GTM (Solo Developer Segment)

**Version:** 1.0  
**Status:** Draft  
**Last Updated:** January 30, 2026  
**Owner:** Solution Architecture

---

## Segment Choice (Why Solo Devs First)

**Pick:** Solo developers using Claude Code / Codex / Gemini CLI who want to monitor and approve AI coding while away from their desk.

**Reason:** The current architecture is single-tenant, single-workspace, and optimized for local-first workflows. This matches solo dev needs and avoids enterprise gaps (SSO, audit, multi-user).

---

## Positioning Statement

**cdev** is the **mobile supervision layer for AI coding**.  
It lets solo developers **approve tool use, see diffs, and keep their repo safe** while Claude/Codex works.

---

## Core Jobs-to-be-Done

- “Let my AI code while I’m away, but keep me in control.”
- “Show me exactly what changed, so I can decide fast.”
- “Let me stop or redirect the agent without opening my laptop.”

---

## Differentiators (Solo Dev Lens)

- **Agent is the source of truth** (not the IDE)
- **Git diffs + file watcher** are first-class
- **Mobile-first supervision** (approval flow, live logs)
- **Local-first security model** (no cloud required)

---

## Target User Profile

- Uses Claude Code or similar CLI daily
- Prefers terminal workflows
- Wants “vibe coding” with safeguards
- Values speed + visibility over heavy IDE tooling

---

## Product Scope for This Segment

**Must-have (Launch):**
- Start/stop agent from mobile
- View logs + diffs in real-time
- Permission prompts in mobile
- Simple pairing (QR or token)
- Clear “safe remote access” guide

**Nice-to-have (Next):**
- Dev server preview guidance
- Session history + search
- Live session attach (terminal mode)

**Not required (Later):**
- Multi-user collaboration
- Multi-tenant SaaS
- Enterprise policy controls

---

## Messaging (Examples)

**Tagline:**  
“Keep your AI coding in check — from your phone.”

**Hero Copy:**  
“cdev lets you supervise Claude/Codex in real time. Approve tool use, review diffs, and stop runs from anywhere.”

---

## Go-To-Market Plan (90-Day)

### Phase 1 — Prove the Workflow (Weeks 1–4)
- Publish a **5‑minute demo**: “Claude edits repo → phone approves → diff reviewed”
- Ship a **safe remote access guide**
- Add a **quickstart** that pairs in under 3 minutes
- Collect 10–20 design partner users (solo devs)

### Phase 2 — Grow OSS Adoption (Weeks 5–8)
- Post weekly “vibe coding” clips + screenshots
- Launch on Hacker News / Reddit / X (dev tools focus)
- Encourage contributions via “first-issue” labels

### Phase 3 — Tighten Retention (Weeks 9–12)
- Improve reliability (session reconnects, status indicators)
- Add **session history + search** polish
- Publish a “best practices” guide (prompts, permissions, safe ops)

---

## Distribution Channels (Solo Dev)

- GitHub (primary)
- Homebrew (macOS install)
- App Store / TestFlight (iOS client)
- Dev tool communities (HN, r/commandline, r/programming)
- Claude/Codex community forums

---

## Success Metrics

**Activation:**
- % of users who pair phone and run first session within 10 minutes

**Retention:**
- Weekly active agents per user
- % of runs where mobile approves at least one action

**Engagement:**
- Avg sessions per week
- Avg diff views per session

---

## Risks + Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Security concerns when tunneling | High | Document safe tunnel patterns; keep defaults localhost |
| App Store friction | Medium | Start with TestFlight, add clear privacy policy |
| Competing IDE features | Medium | Emphasize mobile supervision + agent control |

---

## Exit Criteria (Segment Success)

- 500–1,000 active solo devs using weekly
- 30%+ weekly retention
- Positive sentiment on “mobile supervision” unique value

---

## Next Decision Point

If solo dev adoption is strong, decide between:
1) **Teams** (multi-workspace, shared sessions), or  
2) **Enterprise** (auth, audit, policy, hosted relay).
