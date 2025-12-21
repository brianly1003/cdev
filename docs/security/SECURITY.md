# Security Guide for cdev

**Document Type:** Security Guidelines
**Version:** 1.0.0
**Date:** December 2025
**Status:** Draft

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

### Implemented Protections

| Protection | Status | Notes |
|------------|--------|-------|
| Localhost binding | ✅ Active | Default: `127.0.0.1` only |
| Path validation | ⚠️ Partial | String prefix check (fragile) |
| File size limits | ✅ Active | 200KB default |
| Command whitelist | ✅ Active | Only allowed commands processed |

### Known Vulnerabilities (To Be Fixed)

| Vulnerability | Severity | Status |
|---------------|----------|--------|
| CORS allow-all | Critical | Open |
| No authentication | Critical | Open |
| File read via `cat` | High | Open |
| Fragile path validation | High | Open |
| No rate limiting | High | Open |
| No log rotation | Medium | Open |

---

## Security Configuration

### Recommended `config.yaml` for Development

```yaml
server:
  host: "127.0.0.1"  # Localhost only
  websocket_port: 8765
  http_port: 8766

# When authentication is implemented:
# security:
#   enabled: true
#   allowed_origins:
#     - "http://localhost:3000"
#     - "http://127.0.0.1:3000"
#   rate_limit:
#     requests_per_minute: 100
#     claude_starts_per_minute: 5

limits:
  max_file_size_kb: 200
  max_diff_size_kb: 500
  max_prompt_length: 5000

logging:
  level: "info"
  # Don't log sensitive data
  redact_secrets: true
```

### Environment Variables

Never store sensitive data in config files. Use environment variables:

```bash
export CDEV_SECURITY_TOKEN="your-secret-token"
export CDEV_TLS_CERT_PATH="/path/to/cert.pem"
export CDEV_TLS_KEY_PATH="/path/to/key.pem"
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

## API Security (When Implemented)

### Token Authentication

```
# HTTP Header
X-Auth-Token: <token>

# WebSocket Query Parameter
ws://localhost:8765?token=<token>

# WebSocket First Message
{"type": "auth", "token": "<token>"}
```

### Token Generation

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

### Rate Limiting

| Endpoint | Limit | Window |
|----------|-------|--------|
| All HTTP | 100 requests | 1 minute |
| Claude start | 5 requests | 1 minute |
| File read | 30 requests | 1 minute |
| WebSocket messages | 60 messages | 1 minute |

---

## Security Checklist

### Before First Use

- [ ] Verify binding to localhost only (`127.0.0.1`)
- [ ] Review config.yaml for sensitive defaults
- [ ] Ensure agent runs as non-privileged user
- [ ] Verify repository path is correct

### Before Remote Access

- [ ] Enable authentication (when available)
- [ ] Enable TLS (when available)
- [ ] Configure allowed origins
- [ ] Enable rate limiting
- [ ] Review logging configuration

### Regular Maintenance

- [ ] Rotate authentication tokens
- [ ] Review and clean old logs
- [ ] Update dependencies
- [ ] Review audit logs for anomalies

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
- [ ] Fix CORS configuration
- [ ] Implement authentication
- [ ] Fix file reading (`cat` → `os.ReadFile`)
- [ ] Improve path validation

### Phase 2 (Short-term - P1)
- [ ] Add rate limiting
- [ ] Implement log rotation
- [ ] Add TLS support
- [ ] Security audit

### Phase 3 (Medium-term - P2)
- [ ] JWT with expiration
- [ ] Audit logging
- [ ] Secret detection in prompts
- [ ] Container security hardening

---

## Compliance Notes

### OWASP Top 10 Coverage

| Vulnerability | Status |
|--------------|--------|
| A01 Broken Access Control | ⚠️ Needs auth |
| A02 Cryptographic Failures | ⚠️ Needs TLS |
| A03 Injection | ✅ Using exec.Command properly |
| A05 Security Misconfiguration | ⚠️ CORS issue |
| A06 Vulnerable Components | ✅ Dependencies current |
| A07 Auth Failures | ⚠️ Needs auth |
| A09 Security Logging | ⚠️ Basic logging only |

---

## References

- [OWASP Go Security Cheatsheet](https://cheatsheetseries.owasp.org/cheatsheets/Go_Security_Cheatsheet.html)
- [CWE-22: Path Traversal](https://cwe.mitre.org/data/definitions/22.html)
- [CWE-352: CSRF](https://cwe.mitre.org/data/definitions/352.html)

---

*Document Version: 1.0.0*
*Generated: December 2025*
