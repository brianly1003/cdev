# Image Upload Security Analysis

This document provides a comprehensive security and performance analysis of the image upload feature in cdev, including identified vulnerabilities, their fixes, and reproduction steps.

## Executive Summary

The image upload feature was reviewed for security vulnerabilities and performance issues. Six issues were identified and fixed:

| Issue | Severity | Status |
|-------|----------|--------|
| IP Spoofing in Rate Limiter | CRITICAL | Fixed |
| Incomplete Path Traversal Validation | MEDIUM | Fixed |
| Missing Image ID Format Validation | MEDIUM | Fixed |
| Double-Close Panic | LOW | Fixed |
| Inefficient Storage Size Calculation | LOW | Fixed |
| Incomplete WebP Validation | LOW | Fixed |

---

## 1. IP Spoofing in Rate Limiter (CRITICAL)

### Description
The rate limiter trusted `X-Forwarded-For` and `X-Real-IP` headers without validation. Attackers could bypass rate limiting by spoofing these headers with different values on each request.

### Affected Code
`internal/server/http/middleware/ratelimit.go:190-201`

### Original Vulnerable Code
```go
func IPKeyExtractor(r *http.Request) string {
    // VULNERABLE: Trusts client-provided headers
    if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
        return xff
    }
    if xri := r.Header.Get("X-Real-IP"); xri != "" {
        return xri
    }
    return r.RemoteAddr
}
```

### Reproduction Steps
```bash
# Without fix: Each request uses a different "IP" and bypasses rate limit
for i in {1..100}; do
  curl -X POST http://localhost:8766/api/images \
    -H "X-Forwarded-For: fake-ip-$i" \
    -F "file=@test.jpg"
done
# Result: All 100 requests succeed (should be limited to 10/minute)
```

### Fix Applied
- Added `TrustProxy` flag (defaults to `false`)
- Only trust headers when behind a known reverse proxy
- Parse first IP from comma-separated `X-Forwarded-For`
- Strip port from `RemoteAddr` for consistent rate limiting

### Post-Fix Behavior
```bash
# With fix: All requests use actual RemoteAddr
for i in {1..15}; do
  curl -X POST http://localhost:8766/api/images \
    -H "X-Forwarded-For: fake-ip-$i" \
    -F "file=@test.jpg"
done
# Result: First 10 succeed, next 5 get HTTP 429 Too Many Requests
```

---

## 2. Incomplete Path Traversal Validation (MEDIUM)

### Description
The path validation only checked for literal ".." strings. Attackers could potentially bypass this using:
- Clean path resolution tricks
- Symlink attacks
- Null byte injection

### Affected Code
`internal/services/imagestorage/storage.go:403-436`

### Original Vulnerable Code
```go
func (s *Storage) ValidatePath(path string) error {
    // VULNERABLE: Simple string check is insufficient
    if strings.Contains(path, "..") {
        return fmt.Errorf("invalid path: path traversal not allowed")
    }
    // ...
}
```

### Reproduction Steps
```bash
# Attempt to read files outside images directory
curl "http://localhost:8766/api/images/validate?path=.cdev/images/../../../etc/passwd"

# Attempt with null byte (may bypass some checks)
curl "http://localhost:8766/api/images/validate?path=.cdev/images/test%00../../etc/passwd"
```

### Fix Applied
1. Use `filepath.Clean()` to resolve path components
2. Verify cleaned path is still under the base directory
3. Check for null bytes in path
4. Use absolute path comparison to prevent escapes
5. Reject symlinks (could point outside the images directory)
6. Reject subdirectories within images folder

### Post-Fix Behavior
```bash
# All traversal attempts now fail with appropriate error messages
curl "http://localhost:8766/api/images/validate?path=.cdev/images/../../../etc/passwd"
# Response: {"valid":false,"message":"invalid path: must be within .cdev/images"}
```

---

## 3. Missing Image ID Format Validation (MEDIUM)

### Description
Image IDs from user input were used directly without format validation. While the storage layer uses a map lookup (safe from injection), malformed IDs could cause unexpected behavior.

### Affected Code
`internal/server/http/images.go:194, 250`

### Reproduction Steps
```bash
# Send malformed image ID
curl "http://localhost:8766/api/images?id=<script>alert(1)</script>"
curl "http://localhost:8766/api/images?id=../../etc/passwd"
```

### Fix Applied
Added `isValidImageID()` function that validates:
- Non-empty, max 36 characters
- Only lowercase hex characters (a-f, 0-9) and dashes
- Applied to both GET and DELETE handlers

### Post-Fix Behavior
```bash
curl "http://localhost:8766/api/images?id=<script>"
# Response: {"error":"invalid_id","message":"Invalid image ID format"}
```

---

## 4. Double-Close Panic (LOW)

### Description
Calling `Close()` twice on the storage or rate limiter would panic due to closing an already-closed channel.

### Affected Code
- `internal/services/imagestorage/storage.go:440`
- `internal/server/http/middleware/ratelimit.go:154`

### Reproduction Steps
```go
storage, _ := imagestorage.New("/tmp/test")
storage.Close()
storage.Close() // PANIC: close of closed channel
```

### Fix Applied
Used `sync.Once` to ensure the channel is only closed once:
```go
func (s *Storage) Close() {
    s.closeOnce.Do(func() {
        close(s.cleanupDone)
    })
}
```

---

## 5. Inefficient Storage Size Calculation (LOW)

### Description
The `canAcceptUploadLocked()` function iterated through all images to calculate total size on every upload check. With 50 images, this is O(n) per request.

### Affected Code
`internal/services/imagestorage/storage.go:372-375`

### Original Code
```go
func (s *Storage) canAcceptUploadLocked(sizeBytes int64) (bool, string) {
    // O(n) iteration on every check
    var totalSize int64
    for _, img := range s.images {
        totalSize += img.Size
    }
    // ...
}
```

### Fix Applied
Added `totalSize` field to `Storage` struct, maintained incrementally:
- Updated on `loadExistingImages()`, `Store()`, `Delete()`, `Clear()`
- Updated in `cleanupExpiredLocked()` and `evictOldestLocked()`
- Now O(1) for size checks

---

## 6. Incomplete WebP Validation (LOW)

### Description
WebP validation only checked the RIFF header (`0x52 0x49 0x46 0x46`) but not the "WEBP" signature at offset 8. A malformed RIFF file could pass validation.

### Affected Code
`internal/services/imagestorage/storage.go:41-46`

### Reproduction Steps
```bash
# Create a fake RIFF file that's not WebP
echo -n "RIFF....FAKE" > fake.webp
curl -X POST http://localhost:8766/api/images \
  -F "file=@fake.webp;type=image/webp"
# Before fix: Might be accepted
```

### Fix Applied
Added check for "WEBP" signature at bytes 8-11:
```go
if mimeType == "image/webp" {
    if len(data) < 12 {
        return false
    }
    if data[8] != 'W' || data[9] != 'E' || data[10] != 'B' || data[11] != 'P' {
        return false
    }
}
```

---

## Remaining Security Considerations

### Design-Level Issues (POC Limitations)

These are known limitations that should be addressed before production:

1. **No Authentication**: Any client can upload/delete images
   - Impact: Unauthorized access
   - Recommendation: Add API key or JWT authentication

2. **No Global Rate Limit**: Only per-IP limiting exists
   - Impact: Distributed attacks from many IPs
   - Recommendation: Add global request queue with concurrency limit

3. **No Request Timeout**: Slow clients can hold connections
   - Impact: Resource exhaustion
   - Recommendation: Add server-side request timeouts

4. **Lock Contention on Store()**: Mutex held during I/O
   - Impact: Reduced throughput under load
   - Recommendation: Use more granular locking

### Configuration Recommendations

```yaml
# For production, enable proxy trust only if behind a reverse proxy
rate_limiter:
  trust_proxy: false  # Set to true only behind trusted proxy (nginx, Cloudflare, etc.)
```

---

## Testing

Run the security-related tests:

```bash
# Rate limiter tests (includes IP extraction tests)
go test -v ./internal/server/http/middleware/... -run "IPKey|HeaderKey"

# Image storage tests (includes path validation)
go test -v ./internal/services/imagestorage/... -run "ValidatePath|MagicBytes"

# Image handler tests (includes ID validation)
go test -v ./internal/server/http/... -run "Image"
```

---

## Changelog

| Date | Version | Changes |
|------|---------|---------|
| 2025-12-21 | 1.0 | Initial security analysis and fixes |
