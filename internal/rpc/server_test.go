package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/rpc/transport"
)

type stubTransport struct {
	id string
}

func (s *stubTransport) ID() string { return s.id }

func (s *stubTransport) Read(context.Context) ([]byte, error) {
	return nil, transport.ErrTransportClosed
}

func (s *stubTransport) Write(context.Context, []byte) error {
	return nil
}

func (s *stubTransport) Close() error { return nil }

func (s *stubTransport) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func TestClientSendEvent_IncludesContextInNotificationParams(t *testing.T) {
	tp := &stubTransport{id: "test-client"}
	client := NewClient(tp, nil)

	event := events.NewEvent(events.EventTypeClaudeMessage, map[string]interface{}{
		"type": "assistant",
		"role": "assistant",
		"text": "hello",
	})
	event.SetContext("ws-123", "sess-456")
	event.SetAgentType("claude")

	if err := client.SendEvent(event); err != nil {
		t.Fatalf("SendEvent returned error: %v", err)
	}

	select {
	case data := <-client.send:
		var notif struct {
			Method string                 `json:"method"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.Unmarshal(data, &notif); err != nil {
			t.Fatalf("failed to decode notification: %v", err)
		}

		if notif.Method != "event/claude_message" {
			t.Fatalf("method = %q, want %q", notif.Method, "event/claude_message")
		}
		if got := notif.Params["workspace_id"]; got != "ws-123" {
			t.Fatalf("workspace_id = %v, want ws-123", got)
		}
		if got := notif.Params["session_id"]; got != "sess-456" {
			t.Fatalf("session_id = %v, want sess-456", got)
		}
		if got := notif.Params["agent_type"]; got != "claude" {
			t.Fatalf("agent_type = %v, want claude", got)
		}
		if got := notif.Params["role"]; got != "assistant" {
			t.Fatalf("role = %v, want assistant", got)
		}
		if _, ok := notif.Params["timestamp"]; !ok {
			t.Fatal("timestamp missing in params")
		}
	default:
		t.Fatal("expected notification queued in client.send")
	}
}
