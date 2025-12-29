// Package http implements the HTTP API server for cdev.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/brianly1003/cdev/internal/pairing"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/skip2/go-qrcode"
)

// PairingHandler handles pairing-related HTTP endpoints.
type PairingHandler struct {
	tokenManager *security.TokenManager
	requireAuth  bool
	pairingInfo  func() *pairing.PairingInfo
}

// NewPairingHandler creates a new pairing handler.
func NewPairingHandler(tokenManager *security.TokenManager, requireAuth bool, pairingInfoFn func() *pairing.PairingInfo) *PairingHandler {
	return &PairingHandler{
		tokenManager: tokenManager,
		requireAuth:  requireAuth,
		pairingInfo:  pairingInfoFn,
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

	info := h.pairingInfo()
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

	info := h.pairingInfo()
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

	info := h.pairingInfo()
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
    <title>cdev Pairing</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: linear-gradient(135deg, #1a1a2e 0%, #16213e 100%);
            min-height: 100vh;
            display: flex;
            justify-content: center;
            align-items: center;
            color: #fff;
        }
        .container {
            background: rgba(255,255,255,0.1);
            border-radius: 20px;
            padding: 40px;
            text-align: center;
            backdrop-filter: blur(10px);
            border: 1px solid rgba(255,255,255,0.2);
            max-width: 400px;
        }
        h1 {
            font-size: 24px;
            margin-bottom: 10px;
        }
        .subtitle {
            color: #aaa;
            margin-bottom: 30px;
            font-size: 14px;
        }
        .qr-container {
            background: #fff;
            padding: 10px;
            border-radius: 12px;
            display: inline-block;
            margin-bottom: 25px;
        }
        .qr-container img {
            display: block;
        }
        .info {
            text-align: left;
            background: rgba(0,0,0,0.3);
            padding: 15px;
            border-radius: 10px;
            font-family: monospace;
            font-size: 12px;
            margin-bottom: 20px;
        }
        .info-row {
            display: flex;
            margin-bottom: 8px;
        }
        .info-label {
            color: #888;
            width: 80px;
            flex-shrink: 0;
        }
        .info-value {
            color: #4ade80;
            word-break: break-all;
        }
        .btn {
            background: #4ade80;
            color: #000;
            border: none;
            padding: 12px 24px;
            border-radius: 8px;
            cursor: pointer;
            font-size: 14px;
            font-weight: 600;
            transition: transform 0.2s;
        }
        .btn:hover {
            transform: scale(1.05);
        }
        .actions {
            display: flex;
            flex-direction: column;
            align-items: center;
            gap: 15px;
        }
        .auth-badge {
            display: inline-block;
            padding: 6px 12px;
            border-radius: 20px;
            font-size: 11px;
            font-weight: 500;
            letter-spacing: 0.5px;
        }
        .auth-enabled { background: #4ade80; color: #000; }
        .auth-disabled { background: rgba(255,255,255,0.15); color: #aaa; }
        .timer {
            color: #888;
            font-size: 12px;
            margin-top: 5px;
        }
        .timer span {
            color: #4ade80;
            font-weight: 600;
        }
        .qr-expired {
            opacity: 0.3;
            filter: blur(2px);
        }
        .expired-overlay {
            position: absolute;
            top: 50%;
            left: 50%;
            transform: translate(-50%, -50%);
            background: rgba(0,0,0,0.8);
            padding: 10px 20px;
            border-radius: 8px;
            color: #ff6b6b;
            font-weight: 600;
            display: none;
        }
        .qr-wrapper {
            position: relative;
            display: inline-block;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>cdev Pairing</h1>
        <p class="subtitle">Scan with cdev mobile app to connect</p>

        <div class="qr-wrapper">
            <div class="qr-container" id="qrContainer">
                <img src="/api/pair/qr?size=200" alt="QR Code" width="200" height="200" id="qrImage">
            </div>
            <div class="expired-overlay" id="expiredOverlay">Expired</div>
        </div>
        <div class="timer">Refreshes in <span id="countdown">60</span>s</div>

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
            <button class="btn" onclick="refreshQR()">Refresh QR Code</button>
            <span class="auth-badge ` + authBadgeClass(h.requireAuth) + `">` + authBadgeText(h.requireAuth) + `</span>
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
            img.src = '/api/pair/qr?size=200&t=' + Date.now();
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
