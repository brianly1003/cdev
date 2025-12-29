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
	"sync"
	"time"
)

// Token prefixes for identification
const (
	PairingTokenPrefix = "cdev_p_" // Pairing token (for initial connection)
	SessionTokenPrefix = "cdev_s_" // Session token (for ongoing communication)
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
)

// TokenPayload is the data encoded in a token.
type TokenPayload struct {
	Type      TokenType `json:"type"`
	ServerID  string    `json:"server_id"`
	IssuedAt  int64     `json:"issued_at"`
	ExpiresAt int64     `json:"expires_at"`
	Nonce     string    `json:"nonce"`
}

// TokenManager handles token generation and validation.
type TokenManager struct {
	serverID     string
	serverSecret []byte

	mu           sync.RWMutex
	revokedNonces map[string]time.Time // nonce -> revoked at (for cleanup)

	defaultExpirySecs int
}

// NewTokenManager creates a new token manager with a random server secret.
func NewTokenManager(expirySecs int) (*TokenManager, error) {
	// Generate server ID
	serverID, err := generateRandomString(16)
	if err != nil {
		return nil, fmt.Errorf("failed to generate server ID: %w", err)
	}

	// Generate server secret (32 bytes for HMAC-SHA256)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("failed to generate server secret: %w", err)
	}

	return &TokenManager{
		serverID:         serverID,
		serverSecret:     secret,
		revokedNonces:    make(map[string]time.Time),
		defaultExpirySecs: expirySecs,
	}, nil
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

// generateToken creates a token of the specified type.
func (tm *TokenManager) generateToken(tokenType TokenType, expirySecs int) (string, time.Time, error) {
	nonce, err := generateRandomString(16)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to generate nonce: %w", err)
	}

	now := time.Now()
	expiresAt := now.Add(time.Duration(expirySecs) * time.Second)

	payload := TokenPayload{
		Type:      tokenType,
		ServerID:  tm.serverID,
		IssuedAt:  now.Unix(),
		ExpiresAt: expiresAt.Unix(),
		Nonce:     nonce,
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
	prefix := PairingTokenPrefix
	if tokenType == TokenTypeSession {
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

	// Verify token type
	if payload.Type != expectedType {
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
