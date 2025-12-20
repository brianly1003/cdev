package websocket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/testutil"
	"github.com/gorilla/websocket"
)

func TestNewServer(t *testing.T) {
	hub := testutil.NewMockEventHub()
	handler := func(clientID string, message []byte) {}

	server := NewServer("localhost", 8765, handler, hub)

	if server.addr != "localhost:8765" {
		t.Errorf("expected addr localhost:8765, got %s", server.addr)
	}
	if server.commandHandler == nil {
		t.Error("expected commandHandler to be set")
	}
	if server.hub == nil {
		t.Error("expected hub to be set")
	}
	if server.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", server.ClientCount())
	}
}

func TestServer_StartStop(t *testing.T) {
	hub := testutil.NewMockEventHub()
	handler := func(clientID string, message []byte) {}

	// Use port 0 to get a random available port
	server := NewServer("127.0.0.1", 0, handler, hub)

	err := server.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = server.Stop(ctx)
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestServer_ClientCount(t *testing.T) {
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, nil, hub)

	if server.ClientCount() != 0 {
		t.Errorf("expected 0 clients initially, got %d", server.ClientCount())
	}
}

func TestServer_GetClient_NotFound(t *testing.T) {
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, nil, hub)

	client := server.GetClient("non-existent")
	if client != nil {
		t.Error("expected nil for non-existent client")
	}
}

func TestServer_Broadcast(t *testing.T) {
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, nil, hub)

	// Broadcast to empty server should not panic
	server.Broadcast([]byte("test message"))
}

func TestServer_SetStatusProvider(t *testing.T) {
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, nil, hub)

	provider := &mockStatusProvider{
		status: "running",
		uptime: 3600,
	}
	server.SetStatusProvider(provider)

	if server.statusProvider == nil {
		t.Error("expected statusProvider to be set")
	}
}

// mockStatusProvider implements StatusProvider for testing
type mockStatusProvider struct {
	status string
	uptime int64
}

func (m *mockStatusProvider) GetClaudeStatus() string {
	return m.status
}

func (m *mockStatusProvider) GetUptimeSeconds() int64 {
	return m.uptime
}

func TestServer_WebSocketConnection(t *testing.T) {
	hub := testutil.NewMockEventHub()
	hub.Start()
	defer hub.Stop()

	var receivedMessages [][]byte
	var mu sync.Mutex

	handler := func(clientID string, message []byte) {
		mu.Lock()
		receivedMessages = append(receivedMessages, message)
		mu.Unlock()
	}

	server := NewServer("127.0.0.1", 0, handler, hub)

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(server.handleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect WebSocket client
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer ws.Close()

	// Give server time to register client
	time.Sleep(100 * time.Millisecond)

	if server.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", server.ClientCount())
	}

	// Send a message
	testMessage := []byte(`{"command":"test"}`)
	err = ws.WriteMessage(websocket.TextMessage, testMessage)
	if err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Give handler time to process
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(receivedMessages) != 1 {
		t.Errorf("expected 1 received message, got %d", len(receivedMessages))
	}
	mu.Unlock()
}

func TestServer_RemoveClient(t *testing.T) {
	hub := testutil.NewMockEventHub()
	server := NewServer("localhost", 0, nil, hub)

	// removeClient should not panic for non-existent client
	server.removeClient("non-existent")
}
