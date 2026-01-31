# Tunnel/Proxy Hardening Checklist

Use this checklist before exposing cdev via a tunnel (VS Code, ngrok, Cloudflare) or reverse proxy.

## Required (Do These First)
- **Keep cdev bound to localhost**: `server.host: 127.0.0.1`
- **Enable auth**: `security.require_auth: true`
- **Disable debug/pprof**: `debug.enabled: false`, `debug.pprof_enabled: false`
- **Enable rate limiting**: `security.rate_limit.enabled: true`
- **Restrict origins**: set `security.allowed_origins` to your tunnel domain(s)

## Reverse Proxy Controls
- **TLS termination**: terminate HTTPS at the proxy/tunnel (cdev has no TLS listener)
- **Auth at the edge**: optional but recommended (defense in depth) via Basic/Auth/OIDC
- **IP allowlist**: restrict to your devices or VPN egress if possible
- **Header hygiene**: strip untrusted `X-Forwarded-*` headers unless your proxy sets them

## WebSocket Guidance
- **Authorization header required** for tokens; query‑string tokens are not supported
- Ensure the proxy forwards `Upgrade` and `Connection` headers for WS

## HTTP API Risk
- The HTTP API **requires bearer auth**. If you expose it:
  - Ensure your proxy forwards the `Authorization` header
  - Gate behind proxy auth for defense in depth
  - Restrict by IP allowlist where possible
  - Monitor logs for unexpected access

## Operational Checks
- Confirm `external_url` matches the tunnel URL (for QR pairing)
- Verify CORS is limited to your tunnel domain
- Rotate tokens after any suspected exposure

## Recommended Quick Test
1. Open tunnel and attempt unauthenticated HTTP call (should be 401).
2. Connect via WebSocket with Authorization header (should succeed).
3. Call `/debug/` (should be disabled).
