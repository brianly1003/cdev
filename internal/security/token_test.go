package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestTokenManager creates a TokenManager with an isolated secret file for testing.
func newTestTokenManager(t *testing.T, expirySecs int) *TokenManager {
	t.Helper()

	// Create a temp directory for this specific test
	tempDir, err := os.MkdirTemp("", "cdev-token-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	// Override the secret path for this manager
	originalGetSecretPath := getSecretPath
	secretPath := filepath.Join(tempDir, "token_secret.json")

	// Create manager with custom path by manipulating env or direct instantiation
	// Since getSecretPath is a function, we need a different approach.
	// We'll use NewTokenManagerWithPath instead.
	tm, err := NewTokenManagerWithPath(expirySecs, secretPath)
	if err != nil {
		t.Fatalf("NewTokenManagerWithPath error: %v", err)
	}

	// Restore is automatic since we didn't modify originalGetSecretPath
	_ = originalGetSecretPath

	return tm
}

func TestNewTokenManager(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	if tm.ServerID() == "" {
		t.Error("ServerID should not be empty")
	}
}

func TestGeneratePairingToken(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, expiresAt, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Check prefix
	if !strings.HasPrefix(token, PairingTokenPrefix) {
		t.Errorf("Token should have prefix %s, got %s", PairingTokenPrefix, token[:10])
	}

	// Check expiry is in the future
	if expiresAt.Before(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}

	// Check expiry is approximately 1 hour from now
	expectedExpiry := time.Now().Add(time.Hour)
	if expiresAt.After(expectedExpiry.Add(time.Minute)) || expiresAt.Before(expectedExpiry.Add(-time.Minute)) {
		t.Errorf("ExpiresAt should be ~1 hour from now, got %v", expiresAt)
	}
}

func TestGenerateSessionToken(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, expiresAt, err := tm.GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken error: %v", err)
	}

	// Check prefix
	if !strings.HasPrefix(token, SessionTokenPrefix) {
		t.Errorf("Token should have prefix %s, got %s", SessionTokenPrefix, token[:10])
	}

	// Check expiry is approximately 5 minutes from now
	expectedExpiry := time.Now().Add(5 * time.Minute)
	if expiresAt.After(expectedExpiry.Add(time.Minute)) || expiresAt.Before(expectedExpiry.Add(-time.Minute)) {
		t.Errorf("ExpiresAt should be ~5 minutes from now, got %v", expiresAt)
	}
}

func TestValidateToken_Valid(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	payload, err := tm.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}

	if payload.Type != TokenTypePairing {
		t.Errorf("Type = %s, want %s", payload.Type, TokenTypePairing)
	}

	if payload.ServerID != tm.ServerID() {
		t.Errorf("ServerID = %s, want %s", payload.ServerID, tm.ServerID())
	}
}

func TestValidateToken_Expired(t *testing.T) {
	tm, err := NewTokenManager(1) // 1 second expiry
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Wait for token to expire
	time.Sleep(2 * time.Second)

	_, err = tm.ValidateToken(token)
	if err != ErrExpiredToken {
		t.Errorf("Expected ErrExpiredToken, got %v", err)
	}
}

func TestValidateToken_InvalidFormat(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no prefix", "abc123"},
		{"invalid prefix", "cdev_x_abc123"},
		{"invalid base64", PairingTokenPrefix + "!!!invalid!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tm.ValidateToken(tt.token)
			if err != ErrInvalidFormat {
				t.Errorf("Expected ErrInvalidFormat for %q, got %v", tt.token, err)
			}
		})
	}
}

func TestValidateToken_TamperedToken(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Tamper with the token by changing a character
	tamperedToken := token[:len(token)-1] + "X"

	_, err = tm.ValidateToken(tamperedToken)
	if err == nil {
		t.Error("Expected error for tampered token")
	}
}

func TestValidateToken_DifferentServer(t *testing.T) {
	// Use isolated token managers with different secrets
	tm1 := newTestTokenManager(t, 3600)
	tm2 := newTestTokenManager(t, 3600)

	token, _, err := tm1.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Try to validate with different server (different secret)
	_, err = tm2.ValidateToken(token)
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken for token from different server, got %v", err)
	}
}

func TestRevokeToken(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Token should be valid
	_, err = tm.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error before revoke: %v", err)
	}

	// Revoke
	if err := tm.RevokeToken(token); err != nil {
		t.Fatalf("RevokeToken error: %v", err)
	}

	// Token should now be invalid
	_, err = tm.ValidateToken(token)
	if err != ErrTokenRevoked {
		t.Errorf("Expected ErrTokenRevoked after revoke, got %v", err)
	}
}

func TestRevokeAllTokens(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token1, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	token2, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Both should be valid
	if _, err := tm.ValidateToken(token1); err != nil {
		t.Fatalf("token1 should be valid: %v", err)
	}
	if _, err := tm.ValidateToken(token2); err != nil {
		t.Fatalf("token2 should be valid: %v", err)
	}

	// Revoke all
	if err := tm.RevokeAllTokens(); err != nil {
		t.Fatalf("RevokeAllTokens error: %v", err)
	}

	// Both should now be invalid (server secret changed)
	_, err = tm.ValidateToken(token1)
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken for token1 after RevokeAll, got %v", err)
	}

	_, err = tm.ValidateToken(token2)
	if err != ErrInvalidToken {
		t.Errorf("Expected ErrInvalidToken for token2 after RevokeAll, got %v", err)
	}

	// New tokens should still work
	token3, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error after revoke: %v", err)
	}
	if _, err := tm.ValidateToken(token3); err != nil {
		t.Fatalf("token3 should be valid: %v", err)
	}
}

func TestCleanupExpiredRevocations(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Revoke
	if err := tm.RevokeToken(token); err != nil {
		t.Fatalf("RevokeToken error: %v", err)
	}

	// Check revoked count
	tm.mu.RLock()
	countBefore := len(tm.revokedNonces)
	tm.mu.RUnlock()

	if countBefore != 1 {
		t.Errorf("Expected 1 revoked nonce, got %d", countBefore)
	}

	// Cleanup with very short max age should remove it
	tm.CleanupExpiredRevocations(0)

	tm.mu.RLock()
	countAfter := len(tm.revokedNonces)
	tm.mu.RUnlock()

	if countAfter != 0 {
		t.Errorf("Expected 0 revoked nonces after cleanup, got %d", countAfter)
	}
}

func TestTokenUniqueness(t *testing.T) {
	tm, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, _, err := tm.GeneratePairingToken()
		if err != nil {
			t.Fatalf("GeneratePairingToken error: %v", err)
		}
		if tokens[token] {
			t.Errorf("Duplicate token generated")
		}
		tokens[token] = true
	}
}

func TestGenerateRefreshToken(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	token, expiresAt, err := tm.GenerateRefreshToken()
	if err != nil {
		t.Fatalf("GenerateRefreshToken error: %v", err)
	}

	if !strings.HasPrefix(token, RefreshTokenPrefix) {
		t.Errorf("Expected refresh token prefix %s, got %s", RefreshTokenPrefix, token[:len(RefreshTokenPrefix)])
	}

	// Refresh tokens should have long expiry (7 days)
	expectedExpiry := time.Now().Add(time.Duration(DefaultRefreshTokenExpiry) * time.Second)
	if expiresAt.Before(expectedExpiry.Add(-time.Minute)) || expiresAt.After(expectedExpiry.Add(time.Minute)) {
		t.Errorf("Unexpected expiry time: got %v, expected around %v", expiresAt, expectedExpiry)
	}

	// Should be valid
	payload, err := tm.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}

	if payload.Type != TokenTypeRefresh {
		t.Errorf("Expected token type %s, got %s", TokenTypeRefresh, payload.Type)
	}
}

func TestGenerateAccessToken(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	token, expiresAt, err := tm.GenerateAccessToken()
	if err != nil {
		t.Fatalf("GenerateAccessToken error: %v", err)
	}

	// Access tokens use session prefix
	if !strings.HasPrefix(token, SessionTokenPrefix) {
		t.Errorf("Expected session token prefix %s, got %s", SessionTokenPrefix, token[:len(SessionTokenPrefix)])
	}

	// Access tokens should have 15 min expiry
	expectedExpiry := time.Now().Add(time.Duration(DefaultAccessTokenExpiry) * time.Second)
	if expiresAt.Before(expectedExpiry.Add(-time.Minute)) || expiresAt.After(expectedExpiry.Add(time.Minute)) {
		t.Errorf("Unexpected expiry time: got %v, expected around %v", expiresAt, expectedExpiry)
	}

	// Should be valid
	payload, err := tm.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken error: %v", err)
	}

	if payload.Type != TokenTypeAccess {
		t.Errorf("Expected token type %s, got %s", TokenTypeAccess, payload.Type)
	}
}

func TestGenerateTokenPair(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	pair, err := tm.GenerateTokenPair()
	if err != nil {
		t.Fatalf("GenerateTokenPair error: %v", err)
	}

	// Verify access token
	if !strings.HasPrefix(pair.AccessToken, SessionTokenPrefix) {
		t.Errorf("Access token should have session prefix")
	}

	accessPayload, err := tm.ValidateToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("Access token validation error: %v", err)
	}
	if accessPayload.Type != TokenTypeAccess {
		t.Errorf("Expected access token type, got %s", accessPayload.Type)
	}

	// Verify refresh token
	if !strings.HasPrefix(pair.RefreshToken, RefreshTokenPrefix) {
		t.Errorf("Refresh token should have refresh prefix")
	}

	refreshPayload, err := tm.ValidateToken(pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh token validation error: %v", err)
	}
	if refreshPayload.Type != TokenTypeRefresh {
		t.Errorf("Expected refresh token type, got %s", refreshPayload.Type)
	}

	// Verify expiry times
	if pair.RefreshTokenExpiry.Before(pair.AccessTokenExpiry) {
		t.Error("Refresh token should expire after access token")
	}
}

func TestExchangePairingToken(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	// Generate a pairing token
	pairingToken, _, err := tm.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Exchange for token pair
	pair, err := tm.ExchangePairingToken(pairingToken)
	if err != nil {
		t.Fatalf("ExchangePairingToken error: %v", err)
	}

	// Verify we got valid tokens
	if _, err := tm.ValidateToken(pair.AccessToken); err != nil {
		t.Errorf("Access token should be valid: %v", err)
	}
	if _, err := tm.ValidateToken(pair.RefreshToken); err != nil {
		t.Errorf("Refresh token should be valid: %v", err)
	}

	// Pairing token should now be revoked (one-time use)
	_, err = tm.ValidateToken(pairingToken)
	if err != ErrTokenRevoked {
		t.Errorf("Expected pairing token to be revoked, got: %v", err)
	}
}

func TestExchangePairingToken_WrongType(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	// Try to exchange a session token (wrong type)
	sessionToken, _, _ := tm.GenerateSessionToken()

	_, err := tm.ExchangePairingToken(sessionToken)
	if err == nil {
		t.Error("Expected error when exchanging non-pairing token")
	}
}

func TestRefreshTokenPair(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	// Generate initial token pair
	pair1, err := tm.GenerateTokenPair()
	if err != nil {
		t.Fatalf("GenerateTokenPair error: %v", err)
	}

	// Refresh using the refresh token
	pair2, err := tm.RefreshTokenPair(pair1.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshTokenPair error: %v", err)
	}

	// New tokens should be valid
	if _, err := tm.ValidateToken(pair2.AccessToken); err != nil {
		t.Errorf("New access token should be valid: %v", err)
	}
	if _, err := tm.ValidateToken(pair2.RefreshToken); err != nil {
		t.Errorf("New refresh token should be valid: %v", err)
	}

	// Old refresh token should be revoked
	_, err = tm.ValidateToken(pair1.RefreshToken)
	if err != ErrTokenRevoked {
		t.Errorf("Expected old refresh token to be revoked, got: %v", err)
	}

	// Old access token should still be valid (not revoked)
	if _, err := tm.ValidateToken(pair1.AccessToken); err != nil {
		t.Errorf("Old access token should still be valid: %v", err)
	}
}

func TestRefreshTokenPair_WrongType(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	// Try to refresh with an access token (wrong type)
	accessToken, _, _ := tm.GenerateAccessToken()

	_, err := tm.RefreshTokenPair(accessToken)
	if err == nil {
		t.Error("Expected error when refreshing with non-refresh token")
	}
}

func TestRefreshTokenPair_DoubleUse(t *testing.T) {
	tm := newTestTokenManager(t, 3600)

	// Generate initial token pair
	pair1, err := tm.GenerateTokenPair()
	if err != nil {
		t.Fatalf("GenerateTokenPair error: %v", err)
	}

	// First refresh should succeed
	_, err = tm.RefreshTokenPair(pair1.RefreshToken)
	if err != nil {
		t.Fatalf("First RefreshTokenPair error: %v", err)
	}

	// Second refresh with same token should fail (one-time use)
	_, err = tm.RefreshTokenPair(pair1.RefreshToken)
	if err == nil {
		t.Error("Expected error on second use of refresh token")
	}
}
