// Package security provides authentication and authorization for cdev.
package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Token prefixes for identification
const (
	PairingTokenPrefix = "cdev_p_" // Pairing token (for initial connection)
	SessionTokenPrefix = "cdev_s_" // Session token (for ongoing communication)
	RefreshTokenPrefix = "cdev_r_" // Refresh token (for obtaining new access tokens)

	// Future Cloud Relay token prefixes (reserved)
	// DeviceTokenPrefix  = "cdev_d_" // Device identity token (long-lived)
	// AgentTokenPrefix   = "cdev_a_" // Agent registration token
	// ChannelTokenPrefix = "cdev_c_" // Cloud channel token (JWT)
)

// Default token expiry durations
const (
	DefaultAccessTokenExpiry  = 15 * 60      // 15 minutes
	DefaultRefreshTokenExpiry = 7 * 24 * 3600 // 7 days
)

// Common errors
var (
	ErrInvalidToken   = errors.New("invalid token")
	ErrExpiredToken   = errors.New("token has expired")
	ErrInvalidFormat  = errors.New("invalid token format")
	ErrTokenNotFound  = errors.New("token not found")
	ErrTokenRevoked   = errors.New("token has been revoked")
)

// TokenType represents the type of token.
type TokenType string

const (
	TokenTypePairing TokenType = "pairing"
	TokenTypeSession TokenType = "session"
	TokenTypeRefresh TokenType = "refresh"
	TokenTypeAccess  TokenType = "access" // Alias for session (clearer semantics)
)

// TokenMode indicates how the token should be validated.
type TokenMode string

const (
	TokenModeLocal TokenMode = "local" // Validated by local agent (current)
	TokenModeCloud TokenMode = "cloud" // Validated by cloud relay (future)
)

// TokenPayload is the data encoded in a token.
// Fields are designed to be forward-compatible with Cloud Relay.
type TokenPayload struct {
	// Version for payload format migration (default 1)
	Version int `json:"v,omitempty"`

	// Core fields
	Type      TokenType `json:"type"`
	ServerID  string    `json:"server_id"`  // Local agent ID (legacy name for compatibility)
	IssuedAt  int64     `json:"issued_at"`
	ExpiresAt int64     `json:"expires_at"`
	Nonce     string    `json:"nonce"`

	// Cloud Relay fields (optional, for future use)
	Mode     TokenMode `json:"mode,omitempty"`      // "local" or "cloud"
	AgentID  string    `json:"agent_id,omitempty"`  // Alias for ServerID (semantic name)
	DeviceID string    `json:"device_id,omitempty"` // Client device fingerprint
	UserID   string    `json:"user_id,omitempty"`   // Cloud user ID (future)
}

// TokenManager handles token generation and validation.
type TokenManager struct {
	serverID     string
	serverSecret []byte

	mu           sync.RWMutex
	revokedNonces map[string]time.Time // nonce -> revoked at (for cleanup)

	defaultExpirySecs int
}

// secretData holds the persisted secret and server ID.
type secretData struct {
	ServerID string `json:"server_id"`
	Secret   string `json:"secret"` // base64 encoded
}

// NewTokenManager creates a new token manager with a persisted server secret.
// The secret is stored in ~/.cdev/token_secret.json and reused across restarts.
func NewTokenManager(expirySecs int) (*TokenManager, error) {
	return NewTokenManagerWithPath(expirySecs, getSecretPath())
}

// NewTokenManagerWithPath creates a token manager with a custom secret path.
// This is primarily useful for testing with isolated configurations.
func NewTokenManagerWithPath(expirySecs int, secretPath string) (*TokenManager, error) {

	var serverID string
	var secret []byte

	// Try to load existing secret
	if data, err := os.ReadFile(secretPath); err == nil {
		var sd secretData
		if err := json.Unmarshal(data, &sd); err == nil {
			secret, _ = base64.StdEncoding.DecodeString(sd.Secret)
			serverID = sd.ServerID
		}
	}

	// Generate new secret if not loaded
	if len(secret) == 0 || serverID == "" {
		var err error
		serverID, err = generateRandomString(16)
		if err != nil {
			return nil, fmt.Errorf("failed to generate server ID: %w", err)
		}

		secret = make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return nil, fmt.Errorf("failed to generate server secret: %w", err)
		}

		// Save to file
		if err := saveSecret(secretPath, serverID, secret); err != nil {
			// Log warning but continue - tokens will work for this session
			fmt.Fprintf(os.Stderr, "Warning: could not persist token secret: %v\n", err)
		}
	}

	return &TokenManager{
		serverID:          serverID,
		serverSecret:      secret,
		revokedNonces:     make(map[string]time.Time),
		defaultExpirySecs: expirySecs,
	}, nil
}

// getSecretPath returns the path to the secret file.
func getSecretPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cdev", "token_secret.json")
}

// saveSecret saves the server ID and secret to a file.
func saveSecret(path, serverID string, secret []byte) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	sd := secretData{
		ServerID: serverID,
		Secret:   base64.StdEncoding.EncodeToString(secret),
	}

	data, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return err
	}

	// Write with restrictive permissions (owner only)
	return os.WriteFile(path, data, 0600)
}

// ServerID returns the server's unique ID.
func (tm *TokenManager) ServerID() string {
	return tm.serverID
}

// GeneratePairingToken generates a new pairing token.
func (tm *TokenManager) GeneratePairingToken() (string, time.Time, error) {
	return tm.generateToken(TokenTypePairing, tm.defaultExpirySecs)
}

// GeneratePairingTokenWithExpiry generates a pairing token with custom expiry.
func (tm *TokenManager) GeneratePairingTokenWithExpiry(expirySecs int) (string, time.Time, error) {
	return tm.generateToken(TokenTypePairing, expirySecs)
}

// GenerateSessionToken generates a new session token (shorter expiry).
func (tm *TokenManager) GenerateSessionToken() (string, time.Time, error) {
	// Session tokens have shorter expiry (5 minutes default)
	return tm.generateToken(TokenTypeSession, 300)
}

// GenerateAccessToken generates a new access token.
func (tm *TokenManager) GenerateAccessToken() (string, time.Time, error) {
	return tm.generateToken(TokenTypeAccess, DefaultAccessTokenExpiry)
}

// GenerateRefreshToken generates a new refresh token.
func (tm *TokenManager) GenerateRefreshToken() (string, time.Time, error) {
	return tm.generateToken(TokenTypeRefresh, DefaultRefreshTokenExpiry)
}

// TokenPair represents an access token and refresh token pair.
type TokenPair struct {
	AccessToken        string    `json:"access_token"`
	AccessTokenExpiry  time.Time `json:"access_token_expires_at"`
	RefreshToken       string    `json:"refresh_token"`
	RefreshTokenExpiry time.Time `json:"refresh_token_expires_at"`
}

// GenerateTokenPair generates a new access/refresh token pair.
// This is typically called after initial pairing.
func (tm *TokenManager) GenerateTokenPair() (*TokenPair, error) {
	accessToken, accessExpiry, err := tm.GenerateAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	refreshToken, refreshExpiry, err := tm.GenerateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:        accessToken,
		AccessTokenExpiry:  accessExpiry,
		RefreshToken:       refreshToken,
		RefreshTokenExpiry: refreshExpiry,
	}, nil
}

// RefreshTokenPair validates a refresh token and issues a new token pair.
// The old refresh token is revoked after successful refresh.
func (tm *TokenManager) RefreshTokenPair(refreshToken string) (*TokenPair, error) {
	// Validate the refresh token
	payload, err := tm.ValidateToken(refreshToken)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	// Ensure it's a refresh token
	if payload.Type != TokenTypeRefresh {
		return nil, fmt.Errorf("invalid token type: expected refresh, got %s", payload.Type)
	}

	// Generate new token pair
	pair, err := tm.GenerateTokenPair()
	if err != nil {
		return nil, err
	}

	// Revoke the old refresh token (one-time use)
	tm.mu.Lock()
	tm.revokedNonces[payload.Nonce] = time.Now()
	tm.mu.Unlock()

	return pair, nil
}

// ExchangePairingToken exchanges a valid pairing token for an access/refresh token pair.
// The pairing token is revoked after successful exchange.
func (tm *TokenManager) ExchangePairingToken(pairingToken string) (*TokenPair, error) {
	// Validate the pairing token
	payload, err := tm.ValidateToken(pairingToken)
	if err != nil {
		return nil, fmt.Errorf("invalid pairing token: %w", err)
	}

	// Ensure it's a pairing token
	if payload.Type != TokenTypePairing {
		return nil, fmt.Errorf("invalid token type: expected pairing, got %s", payload.Type)
	}

	// Generate new token pair
	pair, err := tm.GenerateTokenPair()
	if err != nil {
		return nil, err
	}

	// Revoke the pairing token (one-time use)
	tm.mu.Lock()
	tm.revokedNonces[payload.Nonce] = time.Now()
	tm.mu.Unlock()

	return pair, nil
}

// generateToken creates a token of the specified type.
func (tm *TokenManager) generateToken(tokenType TokenType, expirySecs int) (string, time.Time, error) {
	nonce, err := generateRandomString(16)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(expirySecs) * time.Second)

	payload := TokenPayload{
		Version:   1,
		Type:      tokenType,
		ServerID:  tm.serverID,
		AgentID:   tm.serverID, // Same as ServerID for local mode
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
		Nonce:     nonce,
		Mode:      TokenModeLocal,
	}

	// Encode payload
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Calculate HMAC signature
	signature := tm.calculateHMAC(payloadJSON)

	// Combine payload and signature
	combined := struct {
		Payload   string `json:"p"`
		Signature string `json:"s"`
	}{
		Payload:   base64.RawURLEncoding.EncodeToString(payloadJSON),
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}

	combinedJSON, err := json.Marshal(combined)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to marshal token: %w", err)
	}

	// Encode as base64 with prefix
	var prefix string
	switch tokenType {
	case TokenTypePairing:
		prefix = PairingTokenPrefix
	case TokenTypeSession, TokenTypeAccess:
		prefix = SessionTokenPrefix
	case TokenTypeRefresh:
		prefix = RefreshTokenPrefix
	default:
		prefix = SessionTokenPrefix
	}

	token := prefix + base64.RawURLEncoding.EncodeToString(combinedJSON)
	return token, expiresAt, nil
}

// ValidateToken validates a token and returns the payload if valid.
func (tm *TokenManager) ValidateToken(token string) (*TokenPayload, error) {
	// Determine token type from prefix
	var expectedType TokenType
	var tokenData string

	switch {
	case len(token) > len(PairingTokenPrefix) && token[:len(PairingTokenPrefix)] == PairingTokenPrefix:
		expectedType = TokenTypePairing
		tokenData = token[len(PairingTokenPrefix):]
	case len(token) > len(SessionTokenPrefix) && token[:len(SessionTokenPrefix)] == SessionTokenPrefix:
		expectedType = TokenTypeSession
		tokenData = token[len(SessionTokenPrefix):]
	case len(token) > len(RefreshTokenPrefix) && token[:len(RefreshTokenPrefix)] == RefreshTokenPrefix:
		expectedType = TokenTypeRefresh
		tokenData = token[len(RefreshTokenPrefix):]
	default:
		return nil, ErrInvalidFormat
	}

	// Decode base64
	combinedJSON, err := base64.RawURLEncoding.DecodeString(tokenData)
	if err != nil {
		return nil, ErrInvalidFormat
	}

	// Parse combined structure
	var combined struct {
		Payload   string `json:"p"`
		Signature string `json:"s"`
	}
	if err := json.Unmarshal(combinedJSON, &combined); err != nil {
		return nil, ErrInvalidFormat
	}

	// Decode payload
	payloadJSON, err := base64.RawURLEncoding.DecodeString(combined.Payload)
	if err != nil {
		return nil, ErrInvalidFormat
	}

	// Decode signature
	signature, err := base64.RawURLEncoding.DecodeString(combined.Signature)
	if err != nil {
		return nil, ErrInvalidFormat
	}

	// Verify signature
	expectedSig := tm.calculateHMAC(payloadJSON)
	if !hmac.Equal(signature, expectedSig) {
		return nil, ErrInvalidToken
	}

	// Parse payload
	var payload TokenPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, ErrInvalidFormat
	}

	// Verify server ID
	if payload.ServerID != tm.serverID {
		return nil, ErrInvalidToken
	}

	// Verify token type (access and session both use session prefix)
	validType := payload.Type == expectedType
	if expectedType == TokenTypeSession && payload.Type == TokenTypeAccess {
		validType = true
	}
	if !validType {
		return nil, ErrInvalidFormat
	}

	// Check if revoked
	tm.mu.RLock()
	_, revoked := tm.revokedNonces[payload.Nonce]
	tm.mu.RUnlock()
	if revoked {
		return nil, ErrTokenRevoked
	}

	// Check expiry
	if time.Now().Unix() > payload.ExpiresAt {
		return nil, ErrExpiredToken
	}

	return &payload, nil
}

// RevokeToken revokes a token by its nonce.
func (tm *TokenManager) RevokeToken(token string) error {
	payload, err := tm.ValidateToken(token)
	if err != nil && err != ErrExpiredToken {
		return err
	}

	tm.mu.Lock()
	tm.revokedNonces[payload.Nonce] = time.Now()
	tm.mu.Unlock()

	return nil
}

// RevokeAllTokens revokes all issued tokens.
// This is done by regenerating the server secret.
func (tm *TokenManager) RevokeAllTokens() error {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return fmt.Errorf("failed to regenerate server secret: %w", err)
	}

	tm.mu.Lock()
	tm.serverSecret = secret
	tm.revokedNonces = make(map[string]time.Time) // Clear revoked list
	tm.mu.Unlock()

	return nil
}

// CleanupExpiredRevocations removes old entries from the revoked nonces map.
func (tm *TokenManager) CleanupExpiredRevocations(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	tm.mu.Lock()
	for nonce, revokedAt := range tm.revokedNonces {
		if revokedAt.Before(cutoff) {
			delete(tm.revokedNonces, nonce)
		}
	}
	tm.mu.Unlock()
}

// calculateHMAC calculates HMAC-SHA256 for the given data.
func (tm *TokenManager) calculateHMAC(data []byte) []byte {
	h := hmac.New(sha256.New, tm.serverSecret)
	h.Write(data)
	return h.Sum(nil)
}

// generateRandomString generates a random alphanumeric string.
func generateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes)[:length], nil
}
