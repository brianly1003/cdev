package security

import (
	"strings"
	"testing"
	"time"
)

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
	tm1, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	tm2, err := NewTokenManager(3600)
	if err != nil {
		t.Fatalf("NewTokenManager error: %v", err)
	}

	token, _, err := tm1.GeneratePairingToken()
	if err != nil {
		t.Fatalf("GeneratePairingToken error: %v", err)
	}

	// Try to validate with different server
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
