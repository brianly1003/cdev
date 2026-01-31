// Package http implements the HTTP API server for cdev.
package http

import (
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/pairing"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/rs/zerolog/log"
	"github.com/skip2/go-qrcode"
)

// PairingHandler handles pairing-related HTTP endpoints.
type PairingHandler struct {
	tokenManager *security.TokenManager
	requireAuth  bool
	pairingInfo  func() *pairing.PairingInfo
	// allowRequestExternal enables deriving HTTP/WS URLs from request headers
	// when no explicit external URL is configured.
	allowRequestExternal bool
}

// NewPairingHandler creates a new pairing handler.
func NewPairingHandler(tokenManager *security.TokenManager, requireAuth bool, pairingInfoFn func() *pairing.PairingInfo, allowRequestExternal bool) *PairingHandler {
	return &PairingHandler{
		tokenManager:         tokenManager,
		requireAuth:          requireAuth,
		pairingInfo:          pairingInfoFn,
		allowRequestExternal: allowRequestExternal,
	}
}

// HandlePairInfo handles GET /api/pair/info
// Returns connection info as JSON for the mobile app.
//
//	@Summary		Get pairing info
//	@Description	Returns connection info for mobile app pairing (WebSocket URL, HTTP URL, session ID, token)
//	@Tags			pairing
//	@Produce		json
//	@Success		200	{object}	PairingInfoResponse
//	@Router			/api/pair/info [get]
func (h *PairingHandler) HandlePairInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := h.pairingInfoForRequest(r)
	if info == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Pairing info not available",
		})
		return
	}

	response := PairingInfoResponse{
		WebSocket: info.WebSocket,
		HTTP:      info.HTTP,
		SessionID: info.SessionID,
		RepoName:  info.RepoName,
	}

	// Include token if authentication is required
	if h.requireAuth && h.tokenManager != nil {
		token, expiresAt, err := h.tokenManager.GeneratePairingToken()
		if err == nil {
			response.Token = token
			response.TokenExpiresAt = expiresAt.Format(time.RFC3339)
		}
	}

	writeJSON(w, http.StatusOK, response)
}

// HandlePairQR handles GET /api/pair/qr
// Returns a QR code image (PNG) containing the pairing info.
//
//	@Summary		Get pairing QR code
//	@Description	Returns a PNG QR code image containing connection info for mobile app
//	@Tags			pairing
//	@Produce		png
//	@Param			size	query		int	false	"QR code size in pixels (default 256, max 512)"
//	@Success		200		{file}		binary
//	@Failure		500		{object}	ErrorResponse	"Failed to generate QR code"
//	@Router			/api/pair/qr [get]
func (h *PairingHandler) HandlePairQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := h.pairingInfoForRequest(r)
	if info == nil {
		http.Error(w, "Pairing info not available", http.StatusServiceUnavailable)
		return
	}

	// Build QR data
	qrData := map[string]string{
		"ws":      info.WebSocket,
		"http":    info.HTTP,
		"session": info.SessionID,
		"repo":    info.RepoName,
	}

	// Include token if authentication is required
	if h.requireAuth && h.tokenManager != nil {
		token, _, err := h.tokenManager.GeneratePairingToken()
		if err == nil {
			qrData["token"] = token
		}
	}

	jsonData, err := json.Marshal(qrData)
	// Debug: Log QR data (remove in production)
	log.Debug().
		Bool("require_auth", h.requireAuth).
		Bool("has_token_manager", h.tokenManager != nil).
		Bool("token_included", qrData["token"] != "").
		Int("json_len", len(jsonData)).
		Msg("QR code data generated")
	if err != nil {
		http.Error(w, "Failed to encode pairing data", http.StatusInternalServerError)
		return
	}

	// Parse size parameter
	size := 256
	if sizeStr := r.URL.Query().Get("size"); sizeStr != "" {
		if s, err := parseInt(sizeStr); err == nil && s > 0 && s <= 512 {
			size = s
		}
	}

	// Generate QR code
	png, err := qrcode.Encode(string(jsonData), qrcode.Medium, size)
	if err != nil {
		http.Error(w, "Failed to generate QR code", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// HandlePairPage handles GET /pair
// Returns an HTML page with QR code for browser-based pairing.
//
//	@Summary		Pairing page
//	@Description	Returns an HTML page with QR code for browser-based mobile app pairing
//	@Tags			pairing
//	@Produce		html
//	@Success		200	{string}	string	"HTML page"
//	@Router			/pair [get]
func (h *PairingHandler) HandlePairPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	info := h.pairingInfoForRequest(r)
	if info == nil {
		http.Error(w, "Pairing info not available", http.StatusServiceUnavailable)
		return
	}

	// Simple HTML page with embedded QR code
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Cdev Pairing</title>
    <style>
        /* Cdev Signature Design System - Eye-Friendly Edition */
        :root {
            /* Terminal Backgrounds */
            --bg-primary: #16181D;
            --bg-elevated: #1E2128;
            --bg-highlight: #282D36;
            --bg-selected: #343B47;
            /* Text Colors (WCAG Compliant) */
            --text-primary: #E2E8F0;
            --text-secondary: #A0AEC0;
            --text-tertiary: #718096;
            /* Brand */
            --brand: #FF8C5A;
            --brand-dim: #E67A4A;
            /* Semantic Colors (Desaturated) */
            --success: #68D391;
            --primary: #4FD1C5;
            --error: #FC8181;
            --warning: #F6C85D;
            /* Responsive sizing */
            --qr-size: 180px;
            --container-padding: 24px;
        }
        * { margin: 0; padding: 0; box-sizing: border-box; }
        html, body {
            height: 100%;
            overflow: hidden;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: var(--bg-primary);
            height: 100%;
            display: flex;
            justify-content: center;
            align-items: center;
            color: var(--text-primary);
            padding: 16px;
        }
        .container {
            background: var(--bg-elevated);
            border-radius: 16px;
            padding: var(--container-padding);
            text-align: center;
            border: 1px solid var(--bg-highlight);
            max-width: 380px;
            width: 100%;
            max-height: calc(100vh - 32px);
            box-shadow: 0 4px 24px rgba(0,0,0,0.3);
        }
        h1 {
            font-size: 22px;
            margin-bottom: 4px;
            color: var(--brand);
            font-weight: 700;
        }
        .subtitle {
            color: var(--text-secondary);
            margin-bottom: 16px;
            font-size: 13px;
        }
        .qr-container {
            background: #FAFAF8;
            padding: 8px;
            border-radius: 12px;
            display: inline-block;
        }
        .qr-container img {
            display: block;
            width: var(--qr-size);
            height: var(--qr-size);
        }
        .timer {
            color: var(--text-tertiary);
            font-size: 11px;
            margin-top: 8px;
            margin-bottom: 12px;
        }
        .timer span {
            color: var(--primary);
            font-weight: 600;
        }
        .info {
            text-align: left;
            background: var(--bg-highlight);
            padding: 12px;
            border-radius: 10px;
            font-family: 'SF Mono', 'Monaco', 'Inconsolata', monospace;
            font-size: 11px;
            margin-bottom: 16px;
        }
        .info-row {
            display: flex;
            margin-bottom: 6px;
            gap: 2px;
        }
        .info-row:last-child {
            margin-bottom: 0;
        }
        .info-label {
            color: var(--text-tertiary);
            width: 70px;
            flex-shrink: 0;
        }
        .info-value {
            color: var(--success);
            word-break: break-all;
            overflow: hidden;
            text-overflow: ellipsis;
        }
        .actions {
            display: flex;
            align-items: center;
            justify-content: center;
            gap: 12px;
        }
        .btn {
            background: var(--success);
            color: var(--bg-primary);
            border: none;
            padding: 10px 20px;
            border-radius: 8px;
            cursor: pointer;
            font-size: 13px;
            font-weight: 600;
            transition: all 0.2s ease;
        }
        .btn:hover {
            background: #7BE0A4;
        }
        .btn:active {
            transform: scale(0.98);
        }
        .auth-badge {
            display: inline-block;
            padding: 6px 10px;
            border-radius: 20px;
            font-size: 10px;
            font-weight: 500;
            letter-spacing: 0.5px;
            text-transform: uppercase;
        }
        .auth-enabled { background: var(--success); color: var(--bg-primary); }
        .auth-disabled { background: var(--bg-selected); color: var(--text-secondary); }
        .qr-expired {
            opacity: 0.3;
            filter: blur(2px);
        }
        .expired-overlay {
            position: absolute;
            top: 50%;
            left: 50%;
            transform: translate(-50%, -50%);
            background: var(--bg-elevated);
            padding: 8px 16px;
            border-radius: 8px;
            color: var(--error);
            font-weight: 600;
            font-size: 13px;
            display: none;
            border: 1px solid var(--error);
        }
        .qr-wrapper {
            position: relative;
            display: inline-block;
        }

        /* iPhone SE, small phones */
        @media (max-height: 600px) {
            :root {
                --qr-size: 140px;
                --container-padding: 16px;
            }
            h1 { font-size: 18px; }
            .subtitle { font-size: 12px; margin-bottom: 12px; }
            .info { padding: 10px; font-size: 10px; margin-bottom: 12px; }
            .info-row { margin-bottom: 4px; }
            .info-label { width: 60px; }
            .btn { padding: 8px 16px; font-size: 12px; }
            .timer { font-size: 10px; margin-bottom: 10px; }
        }

        /* Standard iPhone */
        @media (min-height: 601px) and (max-height: 750px) {
            :root {
                --qr-size: 160px;
                --container-padding: 20px;
            }
        }

        /* iPhone Pro Max, iPad */
        @media (min-height: 751px) {
            :root {
                --qr-size: 200px;
                --container-padding: 28px;
            }
            h1 { font-size: 24px; }
            .subtitle { margin-bottom: 20px; }
        }

        /* iPad landscape / Desktop */
        @media (min-width: 768px) and (min-height: 600px) {
            :root {
                --qr-size: 220px;
                --container-padding: 32px;
            }
            .container {
                max-width: 420px;
            }
            h1 { font-size: 26px; }
            .info { font-size: 12px; }
            .info-row { gap: 4px; }
        }

        /* Landscape mode - horizontal layout */
        @media (orientation: landscape) and (max-height: 500px) {
            .container {
                display: flex;
                flex-direction: row;
                align-items: center;
                gap: 24px;
                max-width: 700px;
                text-align: left;
            }
            .qr-section {
                flex-shrink: 0;
            }
            .content-section {
                flex: 1;
                min-width: 0;
            }
            :root {
                --qr-size: 150px;
            }
            h1 { text-align: left; }
            .subtitle { text-align: left; }
            .actions { justify-content: flex-start; }
            .info-row { gap: 4px; }
        }
    </style>
</head>
<body>
    <div class="container">
        <div class="qr-section">
            <h1>Cdev Pairing</h1>
            <p class="subtitle">Scan with cdev mobile app to connect</p>

            <div class="qr-wrapper">
                <div class="qr-container" id="qrContainer">
                    <img src="/api/pair/qr?size=256" alt="QR Code" id="qrImage">
                </div>
                <div class="expired-overlay" id="expiredOverlay">Expired</div>
            </div>
            <div class="timer">Refreshes in <span id="countdown">60</span>s</div>
        </div>

        <div class="content-section">
            <div class="info">
                <div class="info-row">
                    <span class="info-label">WebSocket:</span>
                    <span class="info-value">` + info.WebSocket + `</span>
                </div>
                <div class="info-row">
                    <span class="info-label">HTTP:</span>
                    <span class="info-value">` + info.HTTP + `</span>
                </div>
                <div class="info-row">
                    <span class="info-label">Session:</span>
                    <span class="info-value">` + info.SessionID[:8] + `...</span>
                </div>
                <div class="info-row">
                    <span class="info-label">Repo:</span>
                    <span class="info-value">` + info.RepoName + `</span>
                </div>
            </div>

            <div class="actions">
                <button class="btn" onclick="refreshQR()">Refresh</button>
                <span class="auth-badge ` + authBadgeClass(h.requireAuth) + `">` + authBadgeText(h.requireAuth) + `</span>
            </div>
        </div>
    </div>

    <script>
        const REFRESH_INTERVAL = 60;
        let countdown = REFRESH_INTERVAL;
        let expired = false;

        function updateCountdown() {
            countdown--;
            document.getElementById('countdown').textContent = countdown;

            if (countdown <= 0) {
                expired = true;
                document.getElementById('qrContainer').classList.add('qr-expired');
                document.getElementById('expiredOverlay').style.display = 'block';
                refreshQR();
            }
        }

        function refreshQR() {
            const img = document.getElementById('qrImage');
            img.src = '/api/pair/qr?size=256&t=' + Date.now();
            countdown = REFRESH_INTERVAL;
            expired = false;
            document.getElementById('qrContainer').classList.remove('qr-expired');
            document.getElementById('expiredOverlay').style.display = 'none';
            document.getElementById('countdown').textContent = countdown;
        }

        setInterval(updateCountdown, 1000);
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}

// HandlePairRefresh handles POST /api/pair/refresh
// Generates a new pairing token (revokes old ones).
//
//	@Summary		Refresh pairing token
//	@Description	Generates a new pairing token and revokes all previous tokens
//	@Tags			pairing
//	@Produce		json
//	@Success		200	{object}	PairingRefreshResponse
//	@Failure		503	{object}	ErrorResponse	"Token manager not available"
//	@Router			/api/pair/refresh [post]
func (h *PairingHandler) HandlePairRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.tokenManager == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Token manager not available",
		})
		return
	}

	// Revoke all existing tokens
	if err := h.tokenManager.RevokeAllTokens(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to revoke tokens: " + err.Error(),
		})
		return
	}

	// Generate new token
	token, expiresAt, err := h.tokenManager.GeneratePairingToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to generate token: " + err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, PairingRefreshResponse{
		Token:     token,
		ExpiresAt: expiresAt.Format(time.RFC3339),
		Message:   "All previous tokens have been revoked",
	})
}

// PairingInfoResponse is the response for GET /api/pair/info.
type PairingInfoResponse struct {
	WebSocket      string `json:"ws"`
	HTTP           string `json:"http"`
	SessionID      string `json:"session"`
	RepoName       string `json:"repo"`
	Token          string `json:"token,omitempty"`
	TokenExpiresAt string `json:"token_expires_at,omitempty"`
}

// PairingRefreshResponse is the response for POST /api/pair/refresh.
type PairingRefreshResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Message   string `json:"message"`
}

// Helper functions
func authBadgeClass(requireAuth bool) string {
	if requireAuth {
		return "auth-enabled"
	}
	return "auth-disabled"
}

func authBadgeText(requireAuth bool) string {
	if requireAuth {
		return "Auth Required"
	}
	return "No Auth Required"
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, json.Unmarshal([]byte("invalid"), &n)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func (h *PairingHandler) pairingInfoForRequest(r *http.Request) *pairing.PairingInfo {
	info := h.pairingInfo()
	if info == nil || r == nil || !h.allowRequestExternal {
		return info
	}

	if !shouldOverridePairingInfo(info) {
		return info
	}

	baseURL, ok := requestBaseURL(r)
	if !ok {
		return info
	}

	adjusted := *info
	adjusted.HTTP = baseURL
	adjusted.WebSocket = deriveWebSocketURL(baseURL)
	return &adjusted
}

func shouldOverridePairingInfo(info *pairing.PairingInfo) bool {
	if info == nil {
		return false
	}
	host := hostFromURL(info.HTTP)
	if host == "" {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	switch strings.ToLower(host) {
	case "localhost", "0.0.0.0":
		return true
	default:
		return false
	}
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func requestBaseURL(r *http.Request) (string, bool) {
	host := firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	if host == "" {
		return "", false
	}

	proto := firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))
	if proto == "" {
		proto = firstForwardedValue(r.Header.Get("X-Forwarded-Scheme"))
	}
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}

	proto = strings.ToLower(strings.TrimSpace(proto))
	if proto != "http" && proto != "https" {
		return "", false
	}

	forwardedPort := firstForwardedValue(r.Header.Get("X-Forwarded-Port"))
	if forwardedPort != "" && !strings.Contains(host, ":") {
		if (proto == "http" && forwardedPort != "80") || (proto == "https" && forwardedPort != "443") {
			host = host + ":" + forwardedPort
		}
	}

	return proto + "://" + host, true
}

func deriveWebSocketURL(httpURL string) string {
	base := strings.TrimRight(httpURL, "/")
	wsURL := base
	if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	} else if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	}
	return wsURL + "/ws"
}

func firstForwardedValue(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}
