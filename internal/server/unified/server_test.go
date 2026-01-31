// Package unified provides unit tests for the JSON-RPC 2.0 WebSocket server.
package unified

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/brianly1003/cdev/internal/testutil"
	"github.com/gorilla/websocket"
)

// --- Server Tests ---

func TestNewServer(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()

	server := NewServer("localhost", 8766, dispatcher, hub)

	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.addr != "localhost:8766" {
		t.Errorf("expected addr localhost:8766, got %s", server.addr)
	}
	if server.dispatcher != dispatcher {
		t.Error("dispatcher not set correctly")
	}
	if server.hub != hub {
		t.Error("hub not set correctly")
	}
	if server.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", server.ClientCount())
	}
}

func TestServer_SetStatusProvider(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 8766, dispatcher, hub)

	provider := &mockStatusProvider{status: "running", uptime: 3600}
	server.SetStatusProvider(provider)

	if server.statusProvider == nil {
		t.Error("statusProvider not set")
	}
}

func TestServer_ClientCount(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, dispatcher, hub)

	if server.ClientCount() != 0 {
		t.Errorf("expected 0 clients initially, got %d", server.ClientCount())
	}
}

func TestServer_GetClient_NotFound(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, dispatcher, hub)

	client := server.GetClient("non-existent")
	if client != nil {
		t.Error("expected nil for non-existent client")
	}
}

func TestServer_Broadcast(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, dispatcher, hub)

	// Broadcast to empty server should not panic
	server.Broadcast([]byte("test message"))
}

func TestServer_RemoveClient(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, dispatcher, hub)

	// removeClient should not panic for non-existent client
	server.removeClient("non-existent")
}

// --- WebSocket Connection Tests ---

func TestServer_WebSocketConnection(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	dispatcher := handler.NewDispatcher(handler.NewRegistry())

	server := NewServer("127.0.0.1", 0, dispatcher, hub)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect WebSocket client
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	// Give server time to register client
	time.Sleep(100 * time.Millisecond)

	if server.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", server.ClientCount())
	}
}

func TestServer_WebSocketAuth_RejectsMissingToken(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	server := NewServer("127.0.0.1", 0, dispatcher, hub)
	server.SetSecurity(newTestTokenManager(t), nil, true)

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		_ = ws.Close()
		t.Fatal("expected handshake failure without token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 response, got %v", resp)
	}
}

func TestServer_WebSocketAuth_AllowsAccessToken(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	server := NewServer("127.0.0.1", 0, dispatcher, hub)
	tokenManager := newTestTokenManager(t)
	server.SetSecurity(tokenManager, nil, true)

	accessToken, _, err := tokenManager.GenerateAccessToken()
	if err != nil {
		t.Fatalf("failed to generate access token: %v", err)
	}

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		t.Fatalf("expected successful connection, got %v", err)
	}
	_ = ws.Close()
}

func TestServer_WebSocketAuth_RejectsPairingToken(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	server := NewServer("127.0.0.1", 0, dispatcher, hub)
	tokenManager := newTestTokenManager(t)
	server.SetSecurity(tokenManager, nil, true)

	pairingToken, _, err := tokenManager.GeneratePairingToken()
	if err != nil {
		t.Fatalf("failed to generate pairing token: %v", err)
	}

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+pairingToken)
	ws, resp, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err == nil {
		_ = ws.Close()
		t.Fatal("expected handshake failure with pairing token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 response, got %v", resp)
	}
}

func TestServer_JSONRPCCommand(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	registry := handler.NewRegistry()
	// Register a test method
	registry.Register("test/echo", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return map[string]string{"echo": "hello"}, nil
	})

	dispatcher := handler.NewDispatcher(registry)
	server := NewServer("127.0.0.1", 0, dispatcher, hub)

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	time.Sleep(100 * time.Millisecond)

	// Send a JSON-RPC request
	jsonRPCReq := `{"jsonrpc":"2.0","id":1,"method":"test/echo","params":{}}`
	err = ws.WriteMessage(websocket.TextMessage, []byte(jsonRPCReq))
	if err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Read the response
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(msg, &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", response["jsonrpc"])
	}

	if response["id"].(float64) != 1 {
		t.Errorf("expected id 1, got %v", response["id"])
	}

	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	if result["echo"] != "hello" {
		t.Errorf("expected echo hello, got %v", result["echo"])
	}
}

func TestServer_InvalidMessage_Logged(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	server := NewServer("127.0.0.1", 0, dispatcher, hub)

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	time.Sleep(100 * time.Millisecond)

	// Send an invalid message (not JSON-RPC format)
	invalidMsg := `{"command":"get_status"}` // Legacy format - should be ignored
	err = ws.WriteMessage(websocket.TextMessage, []byte(invalidMsg))
	if err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// The server should log a warning but not crash or disconnect
	// Since there's no response for invalid messages, we just verify the connection stays open
	time.Sleep(100 * time.Millisecond)

	// Verify connection is still alive by sending a valid JSON-RPC ping
	pingReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	err = ws.WriteMessage(websocket.TextMessage, []byte(pingReq))
	if err != nil {
		t.Fatalf("Connection should still be alive: %v", err)
	}
}

func newTestTokenManager(t *testing.T) *security.TokenManager {
	t.Helper()
	secretPath := filepath.Join(t.TempDir(), "token_secret.json")
	manager, err := security.NewTokenManagerWithPath(300, secretPath)
	if err != nil {
		t.Fatalf("failed to create token manager: %v", err)
	}
	return manager
}

// --- UnifiedClient Tests ---

func TestUnifiedClient_SendRaw(t *testing.T) {
	// Create a mock connection using httptest
	hub := testutil.NewMockEventHub()
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	server := NewServer("127.0.0.1", 0, dispatcher, hub)

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	time.Sleep(100 * time.Millisecond)

	// Get the client
	if server.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", server.ClientCount())
	}

	// Broadcast a message through the server
	testMsg := []byte(`{"test":"message"}`)
	server.Broadcast(testMsg)

	// Read it on the client side
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read broadcast message: %v", err)
	}

	if string(msg) != string(testMsg) {
		t.Errorf("expected %s, got %s", testMsg, msg)
	}
}

func TestUnifiedClient_Send_JSONRPCNotification(t *testing.T) {
	hub := testutil.NewMockEventHub()
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	server := NewServer("127.0.0.1", 0, dispatcher, hub)

	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	time.Sleep(100 * time.Millisecond)

	// Get any client (with proper locking to avoid race)
	var client *UnifiedClient
	server.mu.RLock()
	for _, c := range server.clients {
		client = c
		break
	}
	server.mu.RUnlock()

	if client == nil {
		t.Fatal("no client found")
	}

	// Send an event (should be formatted as JSON-RPC notification)
	event := events.NewEvent(events.EventTypeHeartbeat, map[string]string{"test": "value"})
	err = client.Send(event)
	if err != nil {
		t.Errorf("Send() error = %v", err)
	}

	// Read the message
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	var received map[string]interface{}
	if err := json.Unmarshal(msg, &received); err != nil {
		t.Fatalf("Failed to parse message: %v", err)
	}

	// Should be in JSON-RPC notification format
	if received["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %v", received["jsonrpc"])
	}

	if received["method"] != "event/heartbeat" {
		t.Errorf("expected method event/heartbeat, got %v", received["method"])
	}

	// Should not have an id (it's a notification)
	if _, hasID := received["id"]; hasID {
		t.Error("notification should not have an id")
	}
}

// --- Localhost Address Tests ---

func TestIsLocalhostAddr(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{
			name:       "IPv4 localhost with port",
			remoteAddr: "127.0.0.1:12345",
			want:       true,
		},
		{
			name:       "IPv4 localhost without port",
			remoteAddr: "127.0.0.1",
			want:       true,
		},
		{
			name:       "IPv6 localhost with port (bracket notation)",
			remoteAddr: "[::1]:12345",
			want:       true,
		},
		{
			name:       "IPv6 localhost without bracket (not supported)",
			remoteAddr: "::1",
			want:       false, // Function expects [::1]:port format for IPv6
		},
		{
			name:       "localhost string with port",
			remoteAddr: "localhost:8080",
			want:       true,
		},
		{
			name:       "localhost string without port",
			remoteAddr: "localhost",
			want:       true,
		},
		{
			name:       "private IP address",
			remoteAddr: "192.168.1.1:12345",
			want:       false,
		},
		{
			name:       "another private IP",
			remoteAddr: "10.0.0.1:8080",
			want:       false,
		},
		{
			name:       "public IP address",
			remoteAddr: "8.8.8.8:443",
			want:       false,
		},
		{
			name:       "random hostname",
			remoteAddr: "example.com:80",
			want:       false,
		},
		{
			name:       "empty string",
			remoteAddr: "",
			want:       false,
		},
		{
			name:       "IPv4 127.0.0.2 (still loopback range)",
			remoteAddr: "127.0.0.2:8080",
			want:       false, // Only 127.0.0.1 is checked, not the whole range
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLocalhostAddr(tt.remoteAddr)
			if got != tt.want {
				t.Errorf("isLocalhostAddr(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
			}
		})
	}
}

// --- Mock Status Provider ---

type mockStatusProvider struct {
	status string
	uptime int64
}

func (m *mockStatusProvider) GetAgentStatus() string {
	return m.status
}

func (m *mockStatusProvider) GetUptimeSeconds() int64 {
	return m.uptime
}
