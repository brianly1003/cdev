// Package http provides the HTTP server handlers for cdev.
package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/brianly1003/cdev/internal/security"
	"github.com/rs/zerolog/log"
)

// AuthHandler handles authentication-related HTTP endpoints.
type AuthHandler struct {
	tokenManager *security.TokenManager
}

// NewAuthHandler creates a new auth handler.
func NewAuthHandler(tm *security.TokenManager) *AuthHandler {
	return &AuthHandler{
		tokenManager: tm,
	}
}

// TokenExchangeRequest is the request body for token exchange.
type TokenExchangeRequest struct {
	PairingToken string `json:"pairing_token"`
}

// TokenRefreshRequest is the request body for token refresh.
type TokenRefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
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

	pair, err := h.tokenManager.ExchangePairingToken(req.PairingToken)
	if err != nil {
		log.Warn().Err(err).Msg("Token exchange failed")
		writeJSONError(w, "Invalid or expired pairing token", http.StatusUnauthorized)
		return
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
	json.NewEncoder(w).Encode(resp)
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

	resp := TokenPairResponse{
		AccessToken:        pair.AccessToken,
		AccessTokenExpiry:  pair.AccessTokenExpiry.Format(time.RFC3339),
		RefreshToken:       pair.RefreshToken,
		RefreshTokenExpiry: pair.RefreshTokenExpiry.Format(time.RFC3339),
		TokenType:          "Bearer",
		ExpiresIn:          int(time.Until(pair.AccessTokenExpiry).Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
