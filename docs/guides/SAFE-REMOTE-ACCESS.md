# Safe Remote Access Guide

Use this guide to access cdev remotely (mobile, tablet, or another machine) **without exposing your machine to the public internet**.

---

## 1. Required Configuration

Keep cdev bound to localhost and require auth. Example:

```yaml
server:
  host: "127.0.0.1"
  port: 8766

security:
  require_auth: true
  token_expiry_secs: 3600
  bind_localhost_only: true
  allowed_origins:
    - "https://your-tunnel.example.com"
  rate_limit:
    enabled: true
    requests_per_minute: 100

debug:
  enabled: false
  pprof_enabled: false
```

**Do not** set `server.host` to `0.0.0.0`.

---

## 2. Use a Tunnel (Recommended)

Pick one:

### SSH tunnel (simple + safe)
```bash
ssh -L 8766:localhost:8766 user@devmachine
```

### VS Code port forwarding
- Forward port `8766`
- Set `security.allowed_origins` to the VS Code tunnel domain
- Open the pairing page via the tunnel URL (e.g. `https://<tunnel>/pair`) so the QR embeds the public URL

### Cloudflare / ngrok / other
- Terminate TLS at the tunnel
- Forward `Authorization` header
- Restrict to a single domain in `security.allowed_origins`

---

## 3. Authentication (Required)

All HTTP + WebSocket requests require a bearer token:

```
Authorization: Bearer <access-token>
```

**Unauthenticated allowlist** (pairing + exchange only):
- `/health`
- `/pair`
- `/api/pair/*`
- `/api/auth/exchange`
- `/api/auth/refresh`
- `/api/auth/revoke`

**Token flow:**
1. Pairing token is displayed at `/pair` (QR code) or `/api/pair/info`.
2. Exchange via `POST /api/auth/exchange` with `{ "pairing_token": "..." }`.
3. Use returned access token for all HTTP + WebSocket requests.
4. Refresh via `POST /api/auth/refresh`.
5. On explicit disconnect, revoke via `POST /api/auth/revoke`.

**Note:** Query‑string tokens are not supported.

---

## 4. Verification Checklist

- Unauthenticated HTTP call returns **401**
- WebSocket connection with `Authorization` header succeeds
- `/debug/` and `/debug/pprof/*` are disabled
- CORS only allows your tunnel domain

---

## 5. Operational Tips

- Rotate tokens if exposed (refresh token exchange)
- Keep tunnel URLs private
- Prefer IP allowlisting at the tunnel/proxy
- Monitor logs for unexpected access

---

If you need a hardened checklist, also read:
- `docs/security/SECURITY.md`
- `docs/security/TUNNEL-PROXY-HARDENING.md`
