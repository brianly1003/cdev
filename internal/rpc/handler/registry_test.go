package handler

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/message"
)

// --- Registry Tests ---

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()

	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.handlers == nil {
		t.Error("handlers map is nil")
	}
	if r.meta == nil {
		t.Error("meta map is nil")
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()

	handler := func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "test", nil
	}

	r.Register("test/method", handler)

	if !r.Has("test/method") {
		t.Error("Has() returned false for registered method")
	}

	h := r.Get("test/method")
	if h == nil {
		t.Error("Get() returned nil for registered method")
	}
}

func TestRegistry_Register_Replace(t *testing.T) {
	r := NewRegistry()

	// First handler returns "first"
	r.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "first", nil
	})

	// Replace with handler that returns "second"
	r.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "second", nil
	})

	h := r.Get("test/method")
	result, _ := h(context.Background(), nil)

	if result != "second" {
		t.Errorf("expected 'second', got %v", result)
	}
}

func TestRegistry_RegisterWithMeta(t *testing.T) {
	r := NewRegistry()

	meta := MethodMeta{
		Summary:     "Test method",
		Description: "A test method for testing",
	}

	r.RegisterWithMeta("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	}, meta)

	if !r.Has("test/method") {
		t.Error("method not registered")
	}

	gotMeta := r.GetMeta("test/method")
	if gotMeta.Summary != meta.Summary {
		t.Errorf("Summary = %s, want %s", gotMeta.Summary, meta.Summary)
	}
	if gotMeta.Description != meta.Description {
		t.Errorf("Description = %s, want %s", gotMeta.Description, meta.Description)
	}
}

func TestRegistry_GetMeta_NotFound(t *testing.T) {
	r := NewRegistry()

	// Register method without meta
	r.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})

	meta := r.GetMeta("test/method")

	// Should return default meta with method as summary
	if meta.Summary != "test/method" {
		t.Errorf("default Summary = %s, want method name 'test/method'", meta.Summary)
	}
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()

	h := r.Get("nonexistent/method")
	if h != nil {
		t.Error("Get() should return nil for unregistered method")
	}
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry()

	if r.Has("test/method") {
		t.Error("Has() should return false for unregistered method")
	}

	r.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})

	if !r.Has("test/method") {
		t.Error("Has() should return true for registered method")
	}
}

func TestRegistry_Methods(t *testing.T) {
	r := NewRegistry()

	r.Register("b/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})
	r.Register("a/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})
	r.Register("c/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})

	methods := r.Methods()
	sort.Strings(methods)

	expected := []string{"a/method", "b/method", "c/method"}
	if len(methods) != len(expected) {
		t.Fatalf("Methods() length = %d, want %d", len(methods), len(expected))
	}
	for i, m := range methods {
		if m != expected[i] {
			t.Errorf("Methods()[%d] = %s, want %s", i, m, expected[i])
		}
	}
}

func TestRegistry_RegisterAll(t *testing.T) {
	r := NewRegistry()

	handlers := map[string]HandlerFunc{
		"method/a": func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
			return "a", nil
		},
		"method/b": func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
			return "b", nil
		},
	}

	r.RegisterAll(handlers)

	if !r.Has("method/a") {
		t.Error("method/a not registered")
	}
	if !r.Has("method/b") {
		t.Error("method/b not registered")
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()

	r.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})

	if !r.Has("test/method") {
		t.Fatal("method should be registered")
	}

	r.Unregister("test/method")

	if r.Has("test/method") {
		t.Error("method should be unregistered")
	}
}

func TestRegistry_Unregister_NotFound(t *testing.T) {
	r := NewRegistry()

	// Should not panic
	r.Unregister("nonexistent/method")
}

func TestRegistry_Clear(t *testing.T) {
	r := NewRegistry()

	r.Register("method/a", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})
	r.Register("method/b", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return nil, nil
	})
	r.Use(func(next HandlerFunc) HandlerFunc {
		return next
	})

	r.Clear()

	if len(r.Methods()) != 0 {
		t.Errorf("Methods() length = %d, want 0", len(r.Methods()))
	}
}

func TestRegistry_Use_Middleware(t *testing.T) {
	r := NewRegistry()

	// Track middleware calls
	calls := []string{}

	// First middleware
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
			calls = append(calls, "mw1-before")
			result, err := next(ctx, params)
			calls = append(calls, "mw1-after")
			return result, err
		}
	})

	// Second middleware
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
			calls = append(calls, "mw2-before")
			result, err := next(ctx, params)
			calls = append(calls, "mw2-after")
			return result, err
		}
	})

	// Handler
	r.Register("test/method", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		calls = append(calls, "handler")
		return "result", nil
	})

	h := r.Get("test/method")
	result, _ := h(context.Background(), nil)

	if result != "result" {
		t.Errorf("result = %v, want 'result'", result)
	}

	// Middleware should wrap in order: mw1 wraps mw2 wraps handler
	expected := []string{"mw1-before", "mw2-before", "handler", "mw2-after", "mw1-after"}
	if len(calls) != len(expected) {
		t.Fatalf("calls length = %d, want %d: %v", len(calls), len(expected), calls)
	}
	for i, call := range calls {
		if call != expected[i] {
			t.Errorf("calls[%d] = %s, want %s", i, call, expected[i])
		}
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry()

	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrent reads and writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			method := "method/" + string(rune('a'+id%26))

			// Register
			r.Register(method, func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
				return id, nil
			})

			// Check existence
			_ = r.Has(method)

			// Get handler
			h := r.Get(method)
			if h != nil {
				_, _ = h(context.Background(), nil)
			}

			// List methods
			_ = r.Methods()
		}(i)
	}

	wg.Wait()
}

// --- MethodService Tests ---

type mockService struct {
	registered []string
}

func (m *mockService) RegisterMethods(r *Registry) {
	r.Register("mock/method1", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "method1", nil
	})
	r.Register("mock/method2", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		return "method2", nil
	})
	m.registered = []string{"mock/method1", "mock/method2"}
}

func TestRegistry_RegisterService(t *testing.T) {
	r := NewRegistry()
	svc := &mockService{}

	r.RegisterService(svc)

	if !r.Has("mock/method1") {
		t.Error("mock/method1 not registered")
	}
	if !r.Has("mock/method2") {
		t.Error("mock/method2 not registered")
	}
}

// --- Context Tests ---

func TestRegistry_ContextKey(t *testing.T) {
	r := NewRegistry()

	r.Register("test/context", func(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
		clientID := ctx.Value(ClientIDKey)
		if clientID == nil {
			return nil, message.NewError(message.InternalError, "no client ID")
		}
		return clientID, nil
	})

	ctx := context.WithValue(context.Background(), ClientIDKey, "client-123")
	h := r.Get("test/context")
	result, err := h(ctx, nil)

	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result != "client-123" {
		t.Errorf("result = %v, want 'client-123'", result)
	}
}
