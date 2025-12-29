package handler

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/message"
)

// --- Dispatcher Tests ---

func TestNewDispatcher(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	if dispatcher == nil {
		t.Fatal("NewDispatcher returned nil")
	}
	if dispatcher.Registry() != registry {
		t.Error("Registry() should return the same registry")
	}
}

func TestDispatcher_Dispatch_ValidRequest(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test/echo", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return map[string]string{"echo": string(params)}, nil
	})

	dispatcher := NewDispatcher(registry)

	req, _ := message.NewRequest(message.StringID("1"), "test/echo", map[string]string{"msg": "hello"})
	resp := dispatcher.Dispatch(context.Background(), req)

	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.IsError() {
		t.Errorf("expected success, got error: %v", resp.Error)
	}
	if resp.ID.String() != "1" {
		t.Errorf("ID = %s, want '1'", resp.ID.String())
	}
}

func TestDispatcher_Dispatch_MethodNotFound(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	req, _ := message.NewRequest(message.StringID("1"), "nonexistent/method", nil)
	resp := dispatcher.Dispatch(context.Background(), req)

	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if !resp.IsError() {
		t.Error("expected error response")
	}
	if resp.Error.Code != message.MethodNotFound {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, message.MethodNotFound)
	}
}

func TestDispatcher_Dispatch_Notification(t *testing.T) {
	called := false
	registry := NewRegistry()
	registry.Register("event/notify", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		called = true
		return nil, nil
	})

	dispatcher := NewDispatcher(registry)

	// Notification has nil ID
	notif, _ := message.NewNotification("event/notify", map[string]string{"event": "test"})
	req := &message.Request{
		JSONRPC: message.Version,
		ID:      nil,
		Method:  notif.Method,
		Params:  notif.Params,
	}

	resp := dispatcher.Dispatch(context.Background(), req)

	if resp != nil {
		t.Error("notifications should not return response")
	}
	if !called {
		t.Error("notification handler was not called")
	}
}

func TestDispatcher_Dispatch_NotificationMethodNotFound(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	req := &message.Request{
		JSONRPC: message.Version,
		ID:      nil,
		Method:  "nonexistent/method",
	}

	resp := dispatcher.Dispatch(context.Background(), req)

	if resp != nil {
		t.Error("notifications should not return error response even for unknown methods")
	}
}

func TestDispatcher_Dispatch_HandlerError(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test/fail", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, message.NewError(message.InternalError, "something went wrong")
	})

	dispatcher := NewDispatcher(registry)

	req, _ := message.NewRequest(message.StringID("1"), "test/fail", nil)
	resp := dispatcher.Dispatch(context.Background(), req)

	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if !resp.IsError() {
		t.Error("expected error response")
	}
	if resp.Error.Code != message.InternalError {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, message.InternalError)
	}
	if resp.Error.Message != "something went wrong" {
		t.Errorf("Error.Message = %s, want 'something went wrong'", resp.Error.Message)
	}
}

func TestDispatcher_DispatchBytes_Valid(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test/ping", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return map[string]string{"pong": "ok"}, nil
	})

	dispatcher := NewDispatcher(registry)

	data := []byte(`{"jsonrpc":"2.0","id":1,"method":"test/ping"}`)
	respBytes, err := dispatcher.DispatchBytes(context.Background(), data)

	if err != nil {
		t.Fatalf("DispatchBytes error: %v", err)
	}
	if respBytes == nil {
		t.Fatal("expected response bytes")
	}

	resp, err := message.ParseResponse(respBytes)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if resp.IsError() {
		t.Errorf("expected success, got error: %v", resp.Error)
	}
}

func TestDispatcher_DispatchBytes_ParseError(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	data := []byte(`invalid json`)
	respBytes, err := dispatcher.DispatchBytes(context.Background(), data)

	if err != nil {
		t.Fatalf("DispatchBytes should not return error for parse errors, got: %v", err)
	}

	resp, err := message.ParseResponse(respBytes)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if !resp.IsError() {
		t.Error("expected error response for parse error")
	}
	if resp.Error.Code != message.ParseError {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, message.ParseError)
	}
}

func TestDispatcher_DispatchBytes_InvalidVersion(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	data := []byte(`{"jsonrpc":"1.0","id":1,"method":"test"}`)
	respBytes, err := dispatcher.DispatchBytes(context.Background(), data)

	if err != nil {
		t.Fatalf("DispatchBytes error: %v", err)
	}

	resp, err := message.ParseResponse(respBytes)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if !resp.IsError() {
		t.Error("expected error response for invalid version")
	}
}

func TestDispatcher_DispatchBytes_Notification(t *testing.T) {
	called := false
	registry := NewRegistry()
	registry.Register("event/notify", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		called = true
		return nil, nil
	})

	dispatcher := NewDispatcher(registry)

	data := []byte(`{"jsonrpc":"2.0","method":"event/notify"}`)
	respBytes, err := dispatcher.DispatchBytes(context.Background(), data)

	if err != nil {
		t.Fatalf("DispatchBytes error: %v", err)
	}
	if respBytes != nil {
		t.Error("notifications should return nil response bytes")
	}
	if !called {
		t.Error("notification handler was not called")
	}
}

func TestDispatcher_BatchDispatch_MultipleRequests(t *testing.T) {
	registry := NewRegistry()
	registry.Register("math/add", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return map[string]int{"result": 42}, nil
	})
	registry.Register("math/multiply", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return map[string]int{"result": 100}, nil
	})

	dispatcher := NewDispatcher(registry)

	req1, _ := message.NewRequest(message.NumberID(1), "math/add", nil)
	req2, _ := message.NewRequest(message.NumberID(2), "math/multiply", nil)

	responses := dispatcher.BatchDispatch(context.Background(), []*message.Request{req1, req2})

	if len(responses) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(responses))
	}
	for _, resp := range responses {
		if resp.IsError() {
			t.Errorf("expected success, got error: %v", resp.Error)
		}
	}
}

func TestDispatcher_BatchDispatch_WithNotifications(t *testing.T) {
	requestCount := 0
	registry := NewRegistry()
	registry.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		requestCount++
		return "ok", nil
	})

	dispatcher := NewDispatcher(registry)

	req1, _ := message.NewRequest(message.NumberID(1), "test/method", nil)
	notif := &message.Request{JSONRPC: message.Version, Method: "test/method"} // notification

	responses := dispatcher.BatchDispatch(context.Background(), []*message.Request{req1, notif})

	if len(responses) != 1 {
		t.Errorf("expected 1 response (notification excluded), got %d", len(responses))
	}
	if requestCount != 2 {
		t.Errorf("expected 2 handlers called, got %d", requestCount)
	}
}

func TestDispatcher_HandleMessage_SingleRequest(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test/ping", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "pong", nil
	})

	dispatcher := NewDispatcher(registry)

	data := []byte(`{"jsonrpc":"2.0","id":"req-1","method":"test/ping"}`)
	respBytes, err := dispatcher.HandleMessage(context.Background(), data)

	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	resp, err := message.ParseResponse(respBytes)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if resp.IsError() {
		t.Errorf("expected success, got error: %v", resp.Error)
	}
}

func TestDispatcher_HandleMessage_BatchRequest(t *testing.T) {
	registry := NewRegistry()
	registry.Register("test/ping", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "pong", nil
	})

	dispatcher := NewDispatcher(registry)

	data := []byte(`[
		{"jsonrpc":"2.0","id":1,"method":"test/ping"},
		{"jsonrpc":"2.0","id":2,"method":"test/ping"}
	]`)
	respBytes, err := dispatcher.HandleMessage(context.Background(), data)

	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var responses []*message.Response
	if err := json.Unmarshal(respBytes, &responses); err != nil {
		t.Fatalf("Unmarshal responses error: %v", err)
	}
	if len(responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(responses))
	}
}

func TestDispatcher_HandleMessage_EmptyBatch(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	data := []byte(`[]`)
	respBytes, err := dispatcher.HandleMessage(context.Background(), data)

	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	resp, err := message.ParseResponse(respBytes)
	if err != nil {
		t.Fatalf("ParseResponse error: %v", err)
	}
	if !resp.IsError() {
		t.Error("expected error response for empty batch")
	}
	if resp.Error.Code != message.InvalidRequest {
		t.Errorf("Error.Code = %d, want %d", resp.Error.Code, message.InvalidRequest)
	}
}

func TestDispatcher_HandleMessage_InvalidBatch(t *testing.T) {
	registry := NewRegistry()
	dispatcher := NewDispatcher(registry)

	data := []byte(`[1, 2, 3]`) // Not valid JSON-RPC requests
	respBytes, err := dispatcher.HandleMessage(context.Background(), data)

	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	var responses []*message.Response
	if err := json.Unmarshal(respBytes, &responses); err != nil {
		t.Fatalf("Unmarshal responses error: %v", err)
	}
	// Each invalid request should generate a parse error
	if len(responses) != 3 {
		t.Errorf("expected 3 error responses, got %d", len(responses))
	}
	for _, resp := range responses {
		if !resp.IsError() {
			t.Error("expected error response for invalid batch item")
		}
	}
}

func TestDispatcher_HandleMessage_AllNotifications(t *testing.T) {
	count := 0
	registry := NewRegistry()
	registry.Register("event/notify", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		count++
		return nil, nil
	})

	dispatcher := NewDispatcher(registry)

	data := []byte(`[
		{"jsonrpc":"2.0","method":"event/notify"},
		{"jsonrpc":"2.0","method":"event/notify"}
	]`)
	respBytes, err := dispatcher.HandleMessage(context.Background(), data)

	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if respBytes != nil {
		t.Error("all notifications should return nil response")
	}
	if count != 2 {
		t.Errorf("expected 2 notifications handled, got %d", count)
	}
}

func TestDispatcher_ConcurrentDispatch(t *testing.T) {
	registry := NewRegistry()
	counter := 0
	var mu sync.Mutex

	registry.Register("test/increment", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		mu.Lock()
		counter++
		mu.Unlock()
		return counter, nil
	})

	dispatcher := NewDispatcher(registry)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req, _ := message.NewRequest(message.NumberID(int64(id)), "test/increment", nil)
			resp := dispatcher.Dispatch(context.Background(), req)
			if resp == nil || resp.IsError() {
				t.Errorf("goroutine %d: unexpected error", id)
			}
		}(i)
	}

	wg.Wait()

	if counter != numGoroutines {
		t.Errorf("counter = %d, want %d", counter, numGoroutines)
	}
}
