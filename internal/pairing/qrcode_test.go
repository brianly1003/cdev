package pairing

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewQRGenerator(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "test-session", "myrepo")

	if gen.host != "localhost" {
		t.Errorf("expected host localhost, got %s", gen.host)
	}
	if gen.port != 8766 {
		t.Errorf("expected port 8766, got %d", gen.port)
	}
	if gen.sessionID != "test-session" {
		t.Errorf("expected sessionID test-session, got %s", gen.sessionID)
	}
	if gen.repoName != "myrepo" {
		t.Errorf("expected repoName myrepo, got %s", gen.repoName)
	}
}

func TestQRGenerator_GetPairingInfo(t *testing.T) {
	gen := NewQRGenerator("192.168.1.100", 8766, "sess-123", "testrepo")

	info := gen.GetPairingInfo()

	// With unified port, WS is on same port with /ws path
	if info.WebSocket != "ws://192.168.1.100:8766/ws" {
		t.Errorf("expected ws://192.168.1.100:8766/ws, got %s", info.WebSocket)
	}
	if info.HTTP != "http://192.168.1.100:8766" {
		t.Errorf("expected http://192.168.1.100:8766, got %s", info.HTTP)
	}
	if info.SessionID != "sess-123" {
		t.Errorf("expected sess-123, got %s", info.SessionID)
	}
	if info.RepoName != "testrepo" {
		t.Errorf("expected testrepo, got %s", info.RepoName)
	}
	if info.Token != "" {
		t.Errorf("expected empty token, got %s", info.Token)
	}
}

func TestQRGenerator_SetExternalURL(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "sess-123", "testrepo")

	// Set external URL (e.g., for VS Code port forwarding)
	gen.SetExternalURL("https://example.com")

	info := gen.GetPairingInfo()

	// WS URL should be auto-derived from HTTP URL
	if info.WebSocket != "wss://example.com/ws" {
		t.Errorf("expected wss://example.com/ws, got %s", info.WebSocket)
	}
	if info.HTTP != "https://example.com" {
		t.Errorf("expected https://example.com, got %s", info.HTTP)
	}
}

func TestQRGenerator_SetExternalURL_HTTP(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "sess-123", "testrepo")

	// Set external URL with http scheme
	gen.SetExternalURL("http://example.com/")

	info := gen.GetPairingInfo()

	// WS URL should be auto-derived (http→ws)
	if info.WebSocket != "ws://example.com/ws" {
		t.Errorf("expected ws://example.com/ws, got %s", info.WebSocket)
	}
	if info.HTTP != "http://example.com" {
		t.Errorf("expected http://example.com, got %s", info.HTTP)
	}
}

func TestQRGenerator_SetToken(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "sess-123", "testrepo")

	gen.SetToken("secret-token-123")

	info := gen.GetPairingInfo()

	if info.Token != "secret-token-123" {
		t.Errorf("expected secret-token-123, got %s", info.Token)
	}
}

func TestQRGenerator_GenerateJSON(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "sess-123", "testrepo")

	jsonStr, err := gen.GenerateJSON()
	if err != nil {
		t.Fatalf("GenerateJSON failed: %v", err)
	}

	// Parse the JSON to verify structure
	var info PairingInfo
	if err := json.Unmarshal([]byte(jsonStr), &info); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if info.WebSocket != "ws://localhost:8766/ws" {
		t.Errorf("expected ws://localhost:8766/ws, got %s", info.WebSocket)
	}
	if info.HTTP != "http://localhost:8766" {
		t.Errorf("expected http://localhost:8766, got %s", info.HTTP)
	}
	if info.SessionID != "sess-123" {
		t.Errorf("expected sess-123, got %s", info.SessionID)
	}
	if info.RepoName != "testrepo" {
		t.Errorf("expected testrepo, got %s", info.RepoName)
	}
}

func TestQRGenerator_GenerateTerminal(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "sess-123", "testrepo")

	qrStr, err := gen.GenerateTerminal()
	if err != nil {
		t.Fatalf("GenerateTerminal failed: %v", err)
	}

	// QR code should be non-empty and contain block characters
	if qrStr == "" {
		t.Error("expected non-empty QR code string")
	}

	// The QR code should contain multiple lines
	lines := strings.Split(qrStr, "\n")
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines in QR code, got %d", len(lines))
	}
}

func TestQRGenerator_GeneratePNG(t *testing.T) {
	gen := NewQRGenerator("localhost", 8766, "sess-123", "testrepo")

	pngData, err := gen.GeneratePNG(256)
	if err != nil {
		t.Fatalf("GeneratePNG failed: %v", err)
	}

	// PNG should start with the PNG signature
	if len(pngData) < 8 {
		t.Fatalf("PNG data too short: %d bytes", len(pngData))
	}

	// Check PNG magic bytes
	pngSignature := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	for i, b := range pngSignature {
		if pngData[i] != b {
			t.Errorf("PNG signature mismatch at byte %d: expected %x, got %x", i, b, pngData[i])
		}
	}
}

func TestSimpleTerminalQR(t *testing.T) {
	result := SimpleTerminalQR("test data")

	if result == "" {
		t.Error("expected non-empty result")
	}

	// Should contain placeholder text
	if !strings.Contains(result, "[QR CODE]") {
		t.Error("expected placeholder text [QR CODE]")
	}

	// Should have box characters
	if !strings.Contains(result, "┌") || !strings.Contains(result, "┘") {
		t.Error("expected box drawing characters")
	}
}

func TestPairingInfo_JSONSerialization(t *testing.T) {
	info := PairingInfo{
		WebSocket: "ws://localhost:8766/ws",
		HTTP:      "http://localhost:8766",
		SessionID: "test-session",
		Token:     "secret",
		RepoName:  "myrepo",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var parsed PairingInfo
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed.WebSocket != info.WebSocket {
		t.Errorf("WebSocket mismatch: expected %s, got %s", info.WebSocket, parsed.WebSocket)
	}
	if parsed.HTTP != info.HTTP {
		t.Errorf("HTTP mismatch: expected %s, got %s", info.HTTP, parsed.HTTP)
	}
	if parsed.SessionID != info.SessionID {
		t.Errorf("SessionID mismatch: expected %s, got %s", info.SessionID, parsed.SessionID)
	}
	if parsed.Token != info.Token {
		t.Errorf("Token mismatch: expected %s, got %s", info.Token, parsed.Token)
	}
	if parsed.RepoName != info.RepoName {
		t.Errorf("RepoName mismatch: expected %s, got %s", info.RepoName, parsed.RepoName)
	}
}

func TestPairingInfo_JSONFields(t *testing.T) {
	info := PairingInfo{
		WebSocket: "ws://test:8766/ws",
		HTTP:      "http://test:8766",
		SessionID: "sess",
		RepoName:  "repo",
	}

	data, _ := json.Marshal(info)
	jsonStr := string(data)

	// Check that JSON uses the correct field names
	if !strings.Contains(jsonStr, `"ws":`) {
		t.Error("expected JSON field 'ws'")
	}
	if !strings.Contains(jsonStr, `"http":`) {
		t.Error("expected JSON field 'http'")
	}
	if !strings.Contains(jsonStr, `"session":`) {
		t.Error("expected JSON field 'session'")
	}
	if !strings.Contains(jsonStr, `"repo":`) {
		t.Error("expected JSON field 'repo'")
	}

	// Token should be omitted when empty
	if strings.Contains(jsonStr, `"token":`) {
		t.Error("expected token field to be omitted when empty")
	}
}
