# Codex CLI Agent Integration

## Purpose

Integrate Codex CLI as an **agent runtime** (not a model provider) alongside Claude Code.
This document captures observed session schema, the minimal integration surface, and a phased plan.

## Current implementation status (as of 2026-02-16)

### Implemented now

- Codex session discovery + history:
  - `session/list` with `agent_type=codex`
  - `session/get` with `agent_type=codex`
  - `session/messages` with `agent_type=codex`
  - `session/elements` with `agent_type=codex`
  - `session/delete` with `agent_type=codex`
- Codex live stream watching:
  - `workspace/session/watch` with `agent_type=codex`
  - `workspace/session/unwatch`
- Event routing now includes `agent_type` in event envelopes.
  - Codex stream events are emitted with `agent_type: "codex"`.
  - Claude stream events are emitted with `agent_type: "claude"`.

### Implemented runtime control APIs

- The workspace/session runtime-control methods now accept `agent_type`:
  - `session/start`
  - `session/stop`
  - `session/send`
  - `session/input`
  - `session/respond`
- Default remains `agent_type="claude"` when omitted.
- `agent_type="codex"` now runs Codex CLI interactively via PTY (`codex` with `cmd.Dir` set to the workspace path).
- `session/input` and `session/respond` route to the active Codex PTY process.

### Remaining gaps

- Codex permission prompt parsing is still best-effort (`pty_output`) and does not yet
  provide Claude-style structured `pty_permission` options.

## Codex CLI facts (from docs)

- Codex CLI supports session resume/fork and accepts the same global flags. Use
  `codex resume` / `codex fork` with `--cd` to bind to a workspace. citeturn0search0
- Codex CLI exposes approvals + sandbox controls via flags such as
  `--ask-for-approval`, `--sandbox`, `--full-auto`, and
  `--dangerously-bypass-approvals-and-sandbox`. citeturn0search0
- Codex CLI can emit machine-readable output via `--json` and can return the last
  assistant message with `--output-last-message`. citeturn0search0
- Codex state lives under `CODEX_HOME` (default `~/.codex`) with config + history
  files; session files are stored under the `sessions/` tree. citeturn0search1

## Observed session schema (local logs)

From `~/.codex/sessions/**/rollout-*.jsonl`:

- Top-level `type` values seen: `session_meta`, `turn_context`, `response_item`, `event_msg`.
- `response_item` includes:
  - `message` (role: user/assistant; content array with `input_text` / `output_text`)
  - `function_call` / `function_call_output`
  - `custom_tool_call` / `custom_tool_call_output`
  - `reasoning` (has `summary` plus **`encrypted_content`**)
- `event_msg` includes `agent_message`, `user_message`, `token_count`, etc.

**Encrypted reasoning note**
- `response_item.reasoning.encrypted_content` is opaque.
- `response_item.reasoning.summary` is readable and sufficient for a POC that
  focuses on logs, tool calls, and message history.

## Agent runtime abstraction

Introduce a single **AgentRuntime** interface with two implementations:

- `ClaudeCodeRuntime` (existing Claude CLI integration)
- `CodexCliRuntime` (new)

Key behaviors:

- Session history: list + message pagination.
- Live stream: watch JSONL append events and emit normalized `AgentEvent`s.
- Resume/attach: use `session/start` with `session_id` + `agent_type=codex` to attach history; `session/send` starts PTY if no history exists.

## Runtime dispatch architecture (implemented)

Session control methods now use a runtime dispatch registry in:

- `internal/rpc/handler/methods/session_manager_runtime.go`

Dispatch registration:

- `ensureRuntimeDispatch()` registers the operation set for each runtime.
- Current runtimes: `claude`, `codex`.
- Supported operations per runtime: `start`, `stop`, `send`, `input`, `respond`.

Benefits:

- RPC entrypoints (`session/start|stop|send|input|respond`) stay thin and runtime-agnostic.
- Runtime-specific behavior is isolated in runtime helpers.
- Adding a new runtime no longer requires branching edits across all RPC methods.

### Adding a new runtime (Gemini-ready checklist)

1. Add runtime key constant (e.g. `sessionManagerAgentGemini`).
2. Implement runtime helpers in `session_manager_runtime.go`:
   - start, stop, send, input, respond
3. Register the runtime in `ensureRuntimeDispatch()`.
4. Register session history provider + optional streamer in:
   - `internal/app/adapters.go`
   - `internal/app/app.go`
5. Add/extend tests in:
   - `internal/rpc/handler/methods/session_manager_test.go`
   - runtime/provider-specific adapter tests

## Event mapping (Codex -> cdev)

| Codex entry | cdev event |
|------------|------------|
| `response_item.message` | `claude_message` (text blocks only, role=user/assistant) |
| `streamer catch-up` | `stream_read_complete` |

Notes:
- Current Codex streamer only emits `response_item.message` events for live UI.
- Tool calls and token usage can be mapped later if needed.

## API surface (cdev)

### JSON-RPC (recommended)

- `session/list` with `agent_type=codex`
- `session/get` with `agent_type=codex`
- `session/messages` with `agent_type=codex`
- `session/elements` with `agent_type=codex`
- `workspace/session/watch` with `agent_type=codex`
- `session/unwatch`
- `session/start|stop|send|input|respond` execute by runtime using `agent_type`.
- Codex uses PTY interactive control and emits runtime-tagged events (`agent_type=codex`).

### HTTP (REST)

- `GET /api/agent/sessions?runtime=codex`
- `DELETE /api/agent/sessions?runtime=codex&session_id=...`
- `GET /api/agent/sessions/messages?runtime=codex&session_id=...`
- `GET /api/agent/sessions/elements?runtime=codex&session_id=...`

## Phased implementation

### Phase 1 (Read + Resume)
- Parse Codex JSONL sessions (list + messages).
- Watch for new log lines and emit events.
- Resume sessions via CLI (`codex resume --cd <workspace>`).

### Phase 2 (Richer interaction)
- Improve Codex permission prompt extraction into structured `pty_permission` payloads.
- Add runtime adapter interface so Gemini/OpenRouter can plug into the same control surface.

## Security + sandbox alignment

Codex CLI provides native sandbox/approval flags. For cdev:

- **Default**: require approvals and sandbox.
- **Opt-in**: expose `full-auto` / `dangerous` modes with explicit warnings. citeturn0search0
