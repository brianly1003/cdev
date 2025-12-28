// Package unified provides unit tests for the dual-protocol WebSocket server.
package unified

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/commands"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
	"github.com/brianly1003/cdev/internal/testutil"
	"github.com/gorilla/websocket"
)

// --- Protocol Detection Tests ---

func TestDetectProtocol_JSONRPC(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    Protocol
	}{
		{
			name:    "valid JSON-RPC request",
			message: `{"jsonrpc":"2.0","id":1,"method":"agent/run","params":{}}`,
			want:    ProtocolJSONRPC,
		},
		{
			name:    "JSON-RPC notification",
			message: `{"jsonrpc":"2.0","method":"event/heartbeat","params":{}}`,
			want:    ProtocolJSONRPC,
		},
		{
			name:    "JSON-RPC with string id",
			message: `{"jsonrpc":"2.0","id":"req-1","method":"status/get"}`,
			want:    ProtocolJSONRPC,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectProtocol([]byte(tt.message))
			if got != tt.want {
				t.Errorf("detectProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectProtocol_Legacy(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    Protocol
	}{
		{
			name:    "legacy run_claude command",
			message: `{"command":"run_claude","payload":{"prompt":"hello"}}`,
			want:    ProtocolLegacy,
		},
		{
			name:    "legacy stop_claude command",
			message: `{"command":"stop_claude"}`,
			want:    ProtocolLegacy,
		},
		{
			name:    "legacy get_status command",
			message: `{"command":"get_status","request_id":"req-1"}`,
			want:    ProtocolLegacy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectProtocol([]byte(tt.message))
			if got != tt.want {
				t.Errorf("detectProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectProtocol_Unknown(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    Protocol
	}{
		{
			name:    "empty message",
			message: "",
			want:    ProtocolUnknown,
		},
		{
			name:    "invalid JSON",
			message: "not json at all",
			want:    ProtocolUnknown,
		},
		{
			name:    "JSON without command or jsonrpc",
			message: `{"foo":"bar"}`,
			want:    ProtocolUnknown,
		},
		{
			name:    "empty JSON object",
			message: `{}`,
			want:    ProtocolUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectProtocol([]byte(tt.message))
			if got != tt.want {
				t.Errorf("detectProtocol() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProtocolName(t *testing.T) {
	tests := []struct {
		protocol Protocol
		want     string
	}{
		{ProtocolJSONRPC, "json-rpc"},
		{ProtocolLegacy, "legacy"},
		{ProtocolUnknown, "unknown"},
		{Protocol(99), "unknown"}, // Invalid protocol value
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := protocolName(tt.protocol)
			if got != tt.want {
				t.Errorf("protocolName(%v) = %v, want %v", tt.protocol, got, tt.want)
			}
		})
	}
}

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

func TestServer_SetLegacyHandler(t *testing.T) {
	dispatcher := handler.NewDispatcher(handler.NewRegistry())
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 8766, dispatcher, hub)

	handlerCalled := false
	server.SetLegacyHandler(func(clientID string, cmd *commands.Command) {
		handlerCalled = true
	})

	if server.legacyHandler == nil {
		t.Error("legacyHandler not set")
	}

	// Call it to verify it's the right one
	server.legacyHandler("test", &commands.Command{})
	if !handlerCalled {
		t.Error("legacyHandler not called")
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

	var receivedCommands []*commands.Command
	var mu sync.Mutex

	legacyHandler := func(clientID string, cmd *commands.Command) {
		mu.Lock()
		receivedCommands = append(receivedCommands, cmd)
		mu.Unlock()
	}

	server := NewServer("127.0.0.1", 0, dispatcher, hub)
	server.SetLegacyHandler(legacyHandler)

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

func TestServer_LegacyCommandWithDeprecationWarning(t *testing.T) {
	hub := testutil.NewMockEventHub()
	_ = hub.Start()
	defer func() { _ = hub.Stop() }()

	dispatcher := handler.NewDispatcher(handler.NewRegistry())

	legacyHandlerCalled := false
	legacyHandler := func(clientID string, cmd *commands.Command) {
		legacyHandlerCalled = true
	}

	server := NewServer("127.0.0.1", 0, dispatcher, hub)
	server.SetLegacyHandler(legacyHandler)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.HandleWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = ws.Close() }()

	time.Sleep(100 * time.Millisecond)

	// Send a legacy command
	legacyCmd := `{"command":"get_status","request_id":"test-1"}`
	err = ws.WriteMessage(websocket.TextMessage, []byte(legacyCmd))
	if err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Read the deprecation warning response
	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	// Verify deprecation warning was sent
	var warning map[string]interface{}
	if err := json.Unmarshal(msg, &warning); err != nil {
		t.Fatalf("Failed to parse warning: %v", err)
	}

	if warning["event"] != "deprecation_warning" {
		t.Errorf("expected deprecation_warning event, got %v", warning["event"])
	}

	payload, ok := warning["payload"].(map[string]interface{})
	if !ok {
		t.Fatal("expected payload to be a map")
	}

	if payload["command"] != "get_status" {
		t.Errorf("expected command get_status, got %v", payload["command"])
	}

	migration, ok := payload["migration"].(map[string]interface{})
	if !ok {
		t.Fatal("expected migration to be a map")
	}

	if migration["get_status"] != "status/get" {
		t.Errorf("expected get_status -> status/get migration, got %v", migration["get_status"])
	}

	// Give handler time to process
	time.Sleep(100 * time.Millisecond)

	if !legacyHandlerCalled {
		t.Error("legacy handler should have been called")
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

func TestUnifiedClient_Send_LegacyFormat(t *testing.T) {
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

	// Get any client
	var client *UnifiedClient
	for _, c := range server.clients {
		client = c
		break
	}

	if client == nil {
		t.Fatal("no client found")
	}

	// Send an event (client protocol is unknown, so it should use legacy format)
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

	// Should be in legacy event format
	if received["event"] != "heartbeat" {
		t.Errorf("expected event heartbeat, got %v", received["event"])
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
