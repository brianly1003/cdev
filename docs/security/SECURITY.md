# Security Guide for cdev

**Document Type:** Security Guidelines
**Version:** 1.2.0
**Date:** January 30, 2026
**Status:** Active

---

## Security Overview

cdev is designed to run on a developer's local machine and provide remote access to Claude Code CLI sessions. This creates a significant attack surface that must be carefully managed.

### Threat Model

| Asset | Threat | Risk Level |
|-------|--------|------------|
| File system access | Unauthorized file reading via path traversal | High |
| Claude CLI control | Unauthorized code execution | Critical |
| Git repository | Information disclosure | Medium |
| Agent availability | Denial of Service | Medium |

---

## Current Security Posture

### Implemented Protections (Current)

| Protection | Status | Notes |
|------------|--------|-------|
| Localhost binding | ✅ Active | Default `server.host = 127.0.0.1`; `security.bind_localhost_only = true` |
| HTTP token auth | ✅ Active | Bearer token required for all HTTP endpoints (except pairing/auth/health allowlist) |
| WebSocket token auth | ✅ Active | Bearer token required when `require_auth = true`; no localhost bypass |
| Origin/CORS enforcement | ✅ Active | Origin checker enforced; no `*` wildcard responses |
| File read limits | ✅ Active | `limits.max_file_size_kb` enforced with streaming read + truncation |
| Image upload hardening | ✅ Active | Size caps, magic‑byte validation, per‑IP rate limiting |
| Rate limiting | ⚠️ Partial | Optional global HTTP limiter + fixed image upload limiter |
| Log rotation | ✅ Active | Claude JSONL logs rotate via lumberjack config |
| Path validation | ✅ Active | `GetFileContent` and `/api/files/list` use Rel + symlink resolution to block escapes |
| Diff size cap | ✅ Active | `limits.max_diff_size_kb` enforced for HTTP/RPC/event diffs |
| Token-in-query removal | ✅ Active | Query-string tokens rejected; Authorization header only |
| Debug/pprof default off | ✅ Active | `debug.pprof_enabled` defaults to `false` |

### Outstanding Risks (Pending)

| Risk | Severity | Status | Notes |
|------|----------|--------|-------|
| WebSocket message rate limiting | Medium | Open | Message flood protection not implemented |
| No built‑in TLS | Medium | Open | Requires tunnel/reverse proxy for TLS termination |

---

## Security Configuration

### Recommended `config.yaml` (Local Dev or Tunnel)

```yaml
server:
  host: "127.0.0.1"
  port: 8766

security:
  require_auth: true
  token_expiry_secs: 3600
  bind_localhost_only: true
  allowed_origins: []
  rate_limit:
    enabled: true
    requests_per_minute: 100

limits:
  max_file_size_kb: 200
  max_diff_size_kb: 500
  max_prompt_len: 10000

debug:
  enabled: false
  pprof_enabled: false

logging:
  level: "info"
  rotation:
    enabled: true
    max_size_mb: 50
    max_backups: 5
    max_age_days: 30
    compress: true
```

### Environment Variables

Any config value can be overridden with `CDEV_`‑prefixed environment variables (Viper). Example:

```bash
export CDEV_SERVER_HOST=127.0.0.1
export CDEV_SECURITY_REQUIRE_AUTH=true
export CDEV_SECURITY_RATE_LIMIT_ENABLED=true
```

---

## Security Best Practices

### 1. Network Exposure

**DO NOT:**
- Bind to `0.0.0.0` (all interfaces)
- Expose ports to the public internet
- Run without authentication on shared networks

**DO:**
- Keep default localhost binding
- Use SSH tunneling for remote access
- Use VPN for mobile access
- Enable TLS when remote access is needed

Note: There is no built-in TLS listener; use a tunnel or reverse proxy to add TLS.

```bash
# Safe remote access via SSH tunnel
ssh -L 8765:localhost:8765 -L 8766:localhost:8766 user@devmachine
```

### 2. File System Protection

**Path Validation Requirements:**

```go
// UNSAFE - String prefix can be bypassed
if strings.HasPrefix(absPath, absRoot) { ... }

// SAFE - Use filepath.Rel and check for traversal
rel, _ := filepath.Rel(absRoot, absPath)
if strings.HasPrefix(rel, "..") {
    return ErrPathOutsideRepo
}
```

**Forbidden Paths:**
- `~/.claude/` (session data, may contain secrets)
- `~/.ssh/` (SSH keys)
- `~/.aws/` (AWS credentials)
- `.env` files
- `credentials.json`, `secrets.yaml`

### 3. Process Isolation

Claude CLI runs with the same permissions as the agent. Mitigations:
- Run agent as non-root user
- Use separate user for agent if possible
- Consider container isolation in production

### 4. Logging Security

**DO NOT log:**
- Authentication tokens
- File contents
- Full prompts (may contain secrets)
- Environment variables

**DO log:**
- Connection events
- Command types (not payloads)
- Error types
- Security events (failed auth, rate limiting)

---

## API Security (Implemented)

### Token Authentication

```
# WebSocket + HTTP Authorization header (required)
Authorization: Bearer <token>
```

**Note:** Query‑string tokens are no longer supported. All authenticated endpoints require the Authorization header.

### Unauthenticated Allowlist

These endpoints remain unauthenticated to support pairing and token exchange:
- `/health`
- `/pair`
- `/api/pair/*`
- `/api/auth/exchange`
- `/api/auth/refresh`
- `/api/auth/revoke`

### Token Generation (Implemented)

Tokens should be:
- Minimum 32 bytes of cryptographically random data
- Base64 or hex encoded
- Rotated periodically
- Never logged

```go
import "crypto/rand"

func generateToken() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return base64.URLEncoding.EncodeToString(b), nil
}
```

### Rate Limiting (Current)

| Endpoint | Limit | Window |
|----------|-------|--------|
| HTTP (global) | `security.rate_limit.requests_per_minute` | 1 minute |
| Image upload | 10 uploads / IP | 1 minute |
| WebSocket messages | Not rate‑limited (message size capped) | N/A |

---

## Security Checklist

### Before First Use

- [ ] Verify binding to localhost only (`127.0.0.1`)
- [ ] Review config.yaml for sensitive defaults
- [ ] Ensure agent runs as non-privileged user
- [ ] Verify repository path is correct

### Before Remote Access

- [ ] Ensure `security.require_auth = true` (HTTP still needs auth hardening)
- [ ] Configure allowed origins
- [ ] Enable rate limiting
- [ ] Disable debug/pprof
- [ ] Review logging configuration for sensitive data

### Regular Maintenance

- [ ] Rotate authentication tokens (`cdev auth reset` or POST `/api/pair/refresh`)
- [ ] Review and clean old logs
- [ ] Update dependencies
- [ ] Review logs for anomalies

---

## Incident Response

### Suspected Unauthorized Access

1. **Immediate:** Stop the agent
   ```bash
   cdev stop  # or Ctrl+C
   ```

2. **Investigate:**
   - Review logs in `.cdev/logs/`
   - Check git history for unauthorized changes
   - Review Claude session history

3. **Remediate:**
   - Rotate any exposed credentials
   - Review and revert unauthorized changes
   - Update security configuration

### Reporting Security Issues

For security vulnerabilities, please:
1. Do NOT open a public GitHub issue
2. Email security concerns to the maintainer
3. Include steps to reproduce
4. Allow reasonable time for fix before disclosure

---

## Security Roadmap

### Phase 1 (Immediate - P0)
- [ ] Fix CORS configuration (partial: main server fixed; OpenRPC/legacy still wildcard)
- [ ] Implement authentication (partial: WebSocket only; HTTP pending)
- [x] Fix file reading (`cat` → `os.ReadFile`)
- [x] Improve path validation (Rel + symlink resolution for file read/list)

### Phase 2 (Short-term - P1)
- [x] Add rate limiting (configurable HTTP + image upload)
- [x] Implement log rotation (Claude JSONL)
- [ ] Add TLS support
- [ ] Security audit (ongoing; track findings in this document)

### Phase 3 (Medium-term - P2)
- [ ] JWT with expiration (current tokens are HMAC)
- [ ] Audit logging
- [ ] Secret detection in prompts
- [ ] Container security hardening

---

## Compliance Notes

### OWASP Top 10 Coverage

| Vulnerability | Status |
|--------------|--------|
| A01 Broken Access Control | ⚠️ HTTP auth missing |
| A02 Cryptographic Failures | ⚠️ No TLS listener |
| A03 Injection | ✅ Uses `exec.Command` (no shell) |
| A05 Security Misconfiguration | ⚠️ OpenRPC CORS + debug config |
| A06 Vulnerable Components | ⚠️ Not assessed here |
| A07 Auth Failures | ⚠️ WebSocket only |
| A09 Security Logging | ⚠️ No audit logs (rotation exists) |

---

## References

- docs/security/IMAGE-UPLOAD-SECURITY-ANALYSIS.md
- docs/security/TOKEN-ARCHITECTURE.md
- docs/security/TUNNEL-PROXY-HARDENING.md
- [OWASP Go Security Cheatsheet](https://cheatsheetseries.owasp.org/cheatsheets/Go_Security_Cheatsheet.html)
- [CWE-22: Path Traversal](https://cwe.mitre.org/data/definitions/22.html)
- [CWE-352: CSRF](https://cwe.mitre.org/data/definitions/352.html)

---

*Document Version: 1.1.0*
*Updated: January 30, 2026*
