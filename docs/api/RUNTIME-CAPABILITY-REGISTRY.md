# Runtime Capability Registry Contract

**Version:** 1.0 (Draft Contract)  
**Status:** Design Contract (Day 1 complete)  
**Last Updated:** 2026-02-17  
**Owner:** cdev core team

---

## Purpose

Define a server-driven runtime contract so clients (cdev-ios, IDE extensions, future apps) can route session behavior without hardcoded runtime branching.

Primary outcomes:
- add new runtimes with minimal client rewrites
- keep runtime behavior deterministic across Claude, Codex, Gemini, and future adapters
- preserve backward compatibility with current clients

---

## Contract Placement

The registry is returned in the `initialize` response under:

`result.capabilities.runtimeRegistry`

This is an additive extension and does not remove existing capability fields.

---

## Top-Level Shape

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "1.0",
    "serverInfo": {
      "name": "cdev",
      "version": "0.1.3"
    },
    "capabilities": {
      "supportedAgents": ["claude", "codex"],
      "runtimeRegistry": {
        "schemaVersion": "1.0",
        "generatedAt": "2026-02-17T12:00:00Z",
        "defaultRuntime": "claude",
        "routing": {
          "agentTypeField": "agent_type",
          "defaultAgentType": "claude",
          "requiredOnMethods": [
            "session/start",
            "session/send",
            "session/stop",
            "session/input",
            "session/respond"
          ]
        },
        "runtimes": [
          {
            "id": "claude",
            "displayName": "Claude",
            "status": "active",
            "sessionListSource": "workspaceHistory",
            "sessionMessagesSource": "workspaceScoped",
            "sessionWatchSource": "workspaceScoped",
            "requiresWorkspaceActivationOnResume": true,
            "requiresSessionResolutionOnNewSession": true,
            "supportsResume": true,
            "supportsInteractiveQuestions": true,
            "supportsPermissions": true,
            "methods": {
              "history": "workspace/session/history",
              "messages": "workspace/session/messages",
              "watch": "workspace/session/watch",
              "unwatch": "workspace/session/unwatch",
              "start": "session/start",
              "send": "session/send",
              "stop": "session/stop",
              "input": "session/input",
              "respond": "session/respond",
              "state": "session/state"
            }
          },
          {
            "id": "codex",
            "displayName": "Codex",
            "status": "active",
            "sessionListSource": "workspaceHistory",
            "sessionMessagesSource": "workspaceScoped",
            "sessionWatchSource": "workspaceScoped",
            "requiresWorkspaceActivationOnResume": false,
            "requiresSessionResolutionOnNewSession": false,
            "supportsResume": true,
            "supportsInteractiveQuestions": true,
            "supportsPermissions": true,
            "methods": {
              "history": "workspace/session/history",
              "messages": "workspace/session/messages",
              "watch": "workspace/session/watch",
              "unwatch": "workspace/session/unwatch",
              "start": "session/start",
              "send": "session/send",
              "stop": "session/stop",
              "input": "session/input",
              "respond": "session/respond",
              "state": "session/state"
            }
          }
        ]
      }
    }
  }
}
```

---

## Field Definitions

### `runtimeRegistry`

| Field | Type | Required | Description |
|---|---|---|---|
| `schemaVersion` | string | Yes | Contract schema version for parsing and compatibility. |
| `generatedAt` | string (RFC3339) | Yes | Server generation timestamp. |
| `defaultRuntime` | string | Yes | Runtime ID used when client does not specify runtime. |
| `routing` | object | Yes | Global routing rules for runtime-scoped methods. |
| `runtimes` | array | Yes | Runtime capability records. |

### `routing`

| Field | Type | Required | Description |
|---|---|---|---|
| `agentTypeField` | string | Yes | Parameter key for runtime routing (currently `agent_type`). |
| `defaultAgentType` | string | Yes | Fallback runtime ID when method omits runtime. |
| `requiredOnMethods` | string[] | Yes | Methods that clients must send runtime selector on for deterministic routing. |

### `runtimes[]`

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Stable runtime ID (`claude`, `codex`, `gemini`, ...). |
| `displayName` | string | Yes | UI-safe runtime label. |
| `status` | string enum | Yes | `active`, `preview`, `deprecated`, `disabled`. |
| `sessionListSource` | string enum | Yes | `workspaceHistory` or `runtimeScoped`. |
| `sessionMessagesSource` | string enum | Yes | `workspaceScoped` or `runtimeScoped`. |
| `sessionWatchSource` | string enum | Yes | `workspaceScoped` or `runtimeScoped`. |
| `requiresWorkspaceActivationOnResume` | boolean | Yes | Whether client should call workspace activation during resume flow. |
| `requiresSessionResolutionOnNewSession` | boolean | Yes | Whether client must wait for session ID resolution event before history/watch APIs. |
| `supportsResume` | boolean | Yes | Runtime supports continuing a historical session. |
| `supportsInteractiveQuestions` | boolean | Yes | Runtime emits questions requiring user input. |
| `supportsPermissions` | boolean | Yes | Runtime emits permission requests requiring approval. |
| `methods` | object | Yes | Runtime operation to RPC method mapping. |

### `runtimes[].methods`

Required keys:
- `history`
- `messages`
- `watch`
- `unwatch`
- `start`
- `send`
- `stop`
- `input`
- `respond`

Optional keys:
- `state`
- future keys are allowed and must be ignored by old clients

---

## Client Behavior Contract

1. Client must call `initialize` before other RPC methods.
2. If `runtimeRegistry` is present, client must use it as source of truth for runtime behavior.
3. If `runtimeRegistry` is absent, client falls back to local defaults (legacy mode).
4. Unknown `runtimeRegistry` fields must be ignored.
5. Unknown runtime IDs must be ignored unless explicitly selected by user.
6. If selected runtime status is `disabled`, client must block session actions and show reason.
7. If selected runtime status is `deprecated`, client should allow use and show a non-blocking warning.

---

## Backward Compatibility Rules

1. Existing fields in `capabilities` remain unchanged.
2. `supportedAgents` remains valid and should match `runtimes[].id` set where possible.
3. Runtime selection fallback remains `claude` when runtime is omitted and no registry is available.
4. Old clients that do not understand `runtimeRegistry` continue to function with current behavior.

---

## Error Handling Contract

1. If client sends runtime not present in `runtimes[].id`, server should return `InvalidParams`.
2. Error payload should include:
- `agent_type`
- `method`
- `supported_agent_types`
3. If runtime is known but temporarily unavailable, server should return `InternalError` with runtime context.

---

## Security and Safety Notes

1. `runtimeRegistry` is metadata only and must not include secrets.
2. Runtime status must reflect server policy; disabled runtimes should not be invokable even if client bypasses UI.
3. Server remains authoritative for method access control and policy enforcement.

---

## Day 2 Implementation Target (Next Step)

1. Extend lifecycle capabilities payload generation in backend.
2. Populate `runtimeRegistry` from runtime dispatch/adapter registration.
3. Add OpenRPC schema entries for new fields.
4. Add contract tests for `initialize` payload shape and runtime consistency.

---

## Related Docs

- `docs/planning/MARKET-READINESS-EXECUTION-PLAN-2026.md`
- `docs/api/UNIFIED-API-SPEC.md`
- `docs/api/PROTOCOL.md`
