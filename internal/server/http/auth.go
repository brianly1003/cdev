// Package http provides the HTTP server handlers for cdev.
package http

import (
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/brianly1003/cdev/internal/security"
	"github.com/rs/zerolog/log"
)

// AuthHandler handles authentication-related HTTP endpoints.
type AuthHandler struct {
	tokenManager    *security.TokenManager
	registry        *security.AuthRegistry
	onOrphaned      func([]string, string)
	pairingApproval *security.PairingApprovalManager
	trustedProxies  []*net.IPNet
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(tm *security.TokenManager, registry *security.AuthRegistry, onOrphaned func([]string, string)) *AuthHandler {
	return &AuthHandler{
		tokenManager: tm,
		registry:     registry,
		onOrphaned:   onOrphaned,
	}
}

// SetPairingApproval enables manual approve/reject flow for pairing exchange.
func (h *AuthHandler) SetPairingApproval(manager *security.PairingApprovalManager, trustedProxies []*net.IPNet) {
	h.pairingApproval = manager
	h.trustedProxies = trustedProxies
}

// TokenExchangeRequest is the request body for token exchange.
type TokenExchangeRequest struct {
	PairingToken string `json:"pairing_token"`
}

// TokenRefreshRequest is the request body for token refresh.
type TokenRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// TokenRevokeRequest is the request body for token revocation.
type TokenRevokeRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// PairingDecisionRequest is the request body for pairing approval decisions.
type PairingDecisionRequest struct {
	RequestID string `json:"request_id"`
}

// TokenPairResponse is the response containing access and refresh tokens.
type TokenPairResponse struct {
	AccessToken        string `json:"access_token"`
	AccessTokenExpiry  string `json:"access_token_expires_at"`
	RefreshToken       string `json:"refresh_token"`
	RefreshTokenExpiry string `json:"refresh_token_expires_at"`
	TokenType          string `json:"token_type"`
	ExpiresIn          int    `json:"expires_in"` // seconds until access token expires
}

// PairingExchangePendingResponse is returned when exchange requires approval.
type PairingExchangePendingResponse struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	ExpiresAt string `json:"expires_at"`
}

// HandleExchange exchanges a pairing token for an access/refresh token pair.
// @Summary Exchange pairing token for access/refresh tokens
// @Description Exchanges a valid pairing token for an access token and refresh token pair
// @Tags auth
// @Accept json
// @Produce json
// @Param request body TokenExchangeRequest true "Pairing token to exchange"
// @Success 200 {object} TokenPairResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/auth/exchange [post]
func (h *AuthHandler) HandleExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TokenExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.PairingToken == "" {
		writeJSONError(w, "pairing_token is required", http.StatusBadRequest)
		return
	}

	var payload *security.TokenPayload
	if h.pairingApproval != nil {
		validatedPayload, err := h.tokenManager.ValidateToken(req.PairingToken)
		if err != nil {
			log.Warn().Err(err).Msg("Token exchange failed")
			writeJSONError(w, "Invalid or expired pairing token", http.StatusUnauthorized)
			return
		}
		if validatedPayload.Type != security.TokenTypePairing {
			writeJSONError(w, "Invalid or expired pairing token", http.StatusUnauthorized)
			return
		}
		payload = validatedPayload

		switch h.pairingApproval.Status(payload.Nonce) {
		case security.PairingApprovalStatusApproved:
			// proceed to exchange
		case security.PairingApprovalStatusRejected:
			writeJSONError(w, "Pairing request rejected", http.StatusForbidden)
			return
		default:
			pending, err := h.pairingApproval.EnsurePending(
				payload.Nonce,
				r.RemoteAddr,
				r.UserAgent(),
				time.Unix(payload.ExpiresAt, 0),
			)
			if err != nil {
				writeJSONError(w, "Failed to create pairing approval request", http.StatusInternalServerError)
				return
			}

			log.Info().
				Str("request_id", pending.RequestID).
				Str("remote_addr", pending.RemoteAddr).
				Str("user_agent", pending.UserAgent).
				Time("expires_at", pending.ExpiresAt).
				Msg("pairing request pending approval")

			writeJSON(w, http.StatusAccepted, PairingExchangePendingResponse{
				Status:    "pending_approval",
				RequestID: pending.RequestID,
				ExpiresAt: pending.ExpiresAt.Format(time.RFC3339),
			})
			return
		}
	}

	pair, err := h.tokenManager.ExchangePairingToken(req.PairingToken)
	if err != nil {
		log.Warn().Err(err).Msg("Token exchange failed")
		writeJSONError(w, "Invalid or expired pairing token", http.StatusUnauthorized)
		return
	}

	if h.pairingApproval != nil && payload != nil {
		h.pairingApproval.ClearTokenDecision(payload.Nonce)
	}

	if h.registry != nil {
		if err := h.registry.RegisterDevice(pair.DeviceID, pair.RefreshNonce, pair.RefreshTokenExpiry, pair.AccessNonce, pair.AccessTokenExpiry); err != nil {
			log.Warn().Err(err).Msg("failed to register device tokens")
		}
	}

	resp := TokenPairResponse{
		AccessToken:        pair.AccessToken,
		AccessTokenExpiry:  pair.AccessTokenExpiry.Format(time.RFC3339),
		RefreshToken:       pair.RefreshToken,
		RefreshTokenExpiry: pair.RefreshTokenExpiry.Format(time.RFC3339),
		TokenType:          "Bearer",
		ExpiresIn:          int(time.Until(pair.AccessTokenExpiry).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleRefresh refreshes an access token using a refresh token.
// @Summary Refresh access token
// @Description Uses a valid refresh token to obtain a new access/refresh token pair
// @Tags auth
// @Accept json
// @Produce json
// @Param request body TokenRefreshRequest true "Refresh token"
// @Success 200 {object} TokenPairResponse
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/auth/refresh [post]
func (h *AuthHandler) HandleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TokenRefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RefreshToken == "" {
		writeJSONError(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	pair, err := h.tokenManager.RefreshTokenPair(req.RefreshToken)
	if err != nil {
		log.Warn().Err(err).Msg("Token refresh failed")
		writeJSONError(w, "Invalid or expired refresh token", http.StatusUnauthorized)
		return
	}

	if h.registry != nil {
		if err := h.registry.RegisterDevice(pair.DeviceID, pair.RefreshNonce, pair.RefreshTokenExpiry, pair.AccessNonce, pair.AccessTokenExpiry); err != nil {
			log.Warn().Err(err).Msg("failed to update device tokens")
		}
	}

	resp := TokenPairResponse{
		AccessToken:        pair.AccessToken,
		AccessTokenExpiry:  pair.AccessTokenExpiry.Format(time.RFC3339),
		RefreshToken:       pair.RefreshToken,
		RefreshTokenExpiry: pair.RefreshTokenExpiry.Format(time.RFC3339),
		TokenType:          "Bearer",
		ExpiresIn:          int(time.Until(pair.AccessTokenExpiry).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// HandleRevoke revokes a refresh token and clears any workspace bindings for the device.
// @Summary Revoke refresh token
// @Description Revokes the refresh token for a device and releases any workspace bindings.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body TokenRevokeRequest true "Refresh token"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} ErrorResponse
// @Failure 401 {object} ErrorResponse
// @Router /api/auth/revoke [post]
func (h *AuthHandler) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req TokenRevokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.RefreshToken == "" {
		writeJSONError(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	payload, err := h.tokenManager.ValidateToken(req.RefreshToken)
	if err != nil {
		log.Warn().Err(err).Msg("Token revoke failed")
		writeJSONError(w, "Invalid or expired refresh token", http.StatusUnauthorized)
		return
	}
	if payload.Type != security.TokenTypeRefresh {
		writeJSONError(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	h.tokenManager.RevokeTokenByNonce(payload.Nonce)

	var orphaned []string
	if h.registry != nil && payload.DeviceID != "" {
		if session, ok := h.registry.GetDevice(payload.DeviceID); ok && session != nil {
			if session.AccessNonce != "" {
				h.tokenManager.RevokeTokenByNonce(session.AccessNonce)
			}
		}

		orphaned, err = h.registry.RemoveDevice(payload.DeviceID)
		if err != nil {
			log.Warn().Err(err).Msg("failed to remove device from registry")
		}
		if len(orphaned) > 0 && h.onOrphaned != nil {
			h.onOrphaned(orphaned, "device revoked")
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":             true,
		"orphaned_workspaces": orphaned,
	})
}

// HandlePairingPending lists pending pairing approval requests.
func (h *AuthHandler) HandlePairingPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.pairingApproval == nil {
		writeJSONError(w, "Pairing approval not enabled", http.StatusNotFound)
		return
	}

	if !isLocalRequest(r, h.trustedProxies) {
		writeJSONError(w, "Forbidden", http.StatusForbidden)
		return
	}

	pending := h.pairingApproval.ListPending()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"pending": pending,
		"count":   len(pending),
	})
}

// HandlePairingApprove approves a pending pairing request.
func (h *AuthHandler) HandlePairingApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.pairingApproval == nil {
		writeJSONError(w, "Pairing approval not enabled", http.StatusNotFound)
		return
	}

	if !isLocalRequest(r, h.trustedProxies) {
		writeJSONError(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req PairingDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.RequestID == "" {
		writeJSONError(w, "request_id is required", http.StatusBadRequest)
		return
	}

	approved, err := h.pairingApproval.Approve(req.RequestID)
	if err != nil {
		writeJSONError(w, "Pending request not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"status":     "approved",
		"request_id": approved.RequestID,
	})
}

// HandlePairingReject rejects a pending pairing request.
func (h *AuthHandler) HandlePairingReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.pairingApproval == nil {
		writeJSONError(w, "Pairing approval not enabled", http.StatusNotFound)
		return
	}

	if !isLocalRequest(r, h.trustedProxies) {
		writeJSONError(w, "Forbidden", http.StatusForbidden)
		return
	}

	var req PairingDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if req.RequestID == "" {
		writeJSONError(w, "request_id is required", http.StatusBadRequest)
		return
	}

	rejected, err := h.pairingApproval.Reject(req.RequestID)
	if err != nil {
		writeJSONError(w, "Pending request not found", http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"status":     "rejected",
		"request_id": rejected.RequestID,
	})
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
