package http

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/permission"
)

// hooksWorkspaceResolver implements WorkspaceResolver for hooks tests.
type hooksWorkspaceResolver struct {
	resolveFunc func(path string) (string, error)
}

func (m *hooksWorkspaceResolver) ResolveWorkspaceID(path string) (string, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(path)
	}
	return "", fmt.Errorf("not configured")
}

// hooksPermissionManager implements PermissionManager for hooks tests.
type hooksPermissionManager struct {
	checkMemoryResult *permission.StoredDecision
	addedRequests     []*permission.Request
	removedToolUseIDs []string
}

func (m *hooksPermissionManager) CheckMemory(sessionID, toolName string, toolInput map[string]interface{}) *permission.StoredDecision {
	return m.checkMemoryResult
}

func (m *hooksPermissionManager) StoreDecision(sessionID, workspaceID, pattern string, decision permission.Decision) {
}

func (m *hooksPermissionManager) AddPendingRequest(req *permission.Request) {
	m.addedRequests = append(m.addedRequests, req)
}

func (m *hooksPermissionManager) GetAndRemovePendingRequest(toolUseID string) *permission.Request {
	m.removedToolUseIDs = append(m.removedToolUseIDs, toolUseID)
	return nil
}

// hooksEventCapture is a hub subscriber that captures published events.
type hooksEventCapture struct {
	mu     sync.Mutex
	events []events.Event
	done   chan struct{}
}

func (c *hooksEventCapture) ID() string { return "hooks-test-capture" }
func (c *hooksEventCapture) Send(e events.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}
func (c *hooksEventCapture) Close() error          { return nil }
func (c *hooksEventCapture) Done() <-chan struct{} { return c.done }

// getEvents returns a snapshot of captured events (safe for concurrent access).
func (c *hooksEventCapture) getEvents() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]events.Event, len(c.events))
	copy(result, c.events)
	return result
}

// setupHooksTest creates a HooksHandler with a hub that captures events.
func setupHooksTest(resolver WorkspaceResolver) (*HooksHandler, *hub.Hub, *hooksEventCapture) {
	h := hub.New()
	_ = h.Start()

	capture := &hooksEventCapture{done: make(chan struct{})}
	h.Subscribe(capture)

	handler := NewHooksHandler(h)
	if resolver != nil {
		handler.SetWorkspaceResolver(resolver)
	}

	return handler, h, capture
}

func stopTestHub(t *testing.T, h *hub.Hub) {
	t.Helper()
	if err := h.Stop(); err != nil {
		t.Fatalf("failed to stop test hub: %v", err)
	}
}

// drainHookEvents gives the hub a moment to deliver events.
func drainHookEvents() {
	time.Sleep(20 * time.Millisecond)
}

// --- resolveWorkspaceID unit tests ---

func TestResolveWorkspaceID_NilResolver(t *testing.T) {
	handler := &HooksHandler{}
	got := handler.resolveWorkspaceID("/some/path")
	if got != "" {
		t.Errorf("expected empty string with nil resolver, got %q", got)
	}
}

func TestResolveWorkspaceID_EmptyCwd(t *testing.T) {
	handler := &HooksHandler{
		workspaceResolver: &hooksWorkspaceResolver{
			resolveFunc: func(path string) (string, error) {
				return "should-not-be-called", nil
			},
		},
	}
	got := handler.resolveWorkspaceID("")
	if got != "" {
		t.Errorf("expected empty string with empty cwd, got %q", got)
	}
}

func TestResolveWorkspaceID_Success(t *testing.T) {
	handler := &HooksHandler{
		workspaceResolver: &hooksWorkspaceResolver{
			resolveFunc: func(path string) (string, error) {
				return "my-workspace", nil
			},
		},
	}
	got := handler.resolveWorkspaceID("/Users/dev/Projects/my-workspace")
	if got != "my-workspace" {
		t.Errorf("expected %q, got %q", "my-workspace", got)
	}
}

func TestResolveWorkspaceID_ErrorReturnsEmpty(t *testing.T) {
	handler := &HooksHandler{
		workspaceResolver: &hooksWorkspaceResolver{
			resolveFunc: func(path string) (string, error) {
				return "", fmt.Errorf("resolve failed")
			},
		},
	}
	got := handler.resolveWorkspaceID("/some/bad/path")
	if got != "" {
		t.Errorf("expected empty string on error, got %q", got)
	}
}

// --- Hook endpoint tests: workspace ID on events ---

func TestHandleHook_Session_SetsWorkspaceID(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			return "cdev-ios", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-1",
		Cwd:       "/Users/dev/Projects/cdev-ios",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/session", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}

	evt := captured[0]
	if evt.Type() != events.EventTypeClaudeHookSession {
		t.Errorf("expected event type %s, got %s", events.EventTypeClaudeHookSession, evt.Type())
	}
	if evt.GetWorkspaceID() != "cdev-ios" {
		t.Errorf("expected workspace_id %q, got %q", "cdev-ios", evt.GetWorkspaceID())
	}
}

func TestHandleHook_Session_NoResolver_EmptyWorkspaceID(t *testing.T) {
	handler, h, capture := setupHooksTest(nil)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-1",
		Cwd:       "/Users/dev/Projects/cdev-ios",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/session", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}
	if captured[0].GetWorkspaceID() != "" {
		t.Errorf("expected empty workspace_id without resolver, got %q", captured[0].GetWorkspaceID())
	}
}

func TestHandleHook_Notification_SetsWorkspaceID(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			return "my-project", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID:        "sess-2",
		Cwd:              "/Users/dev/Projects/my-project",
		Message:          "Claude needs your permission to use Bash",
		NotificationType: "permission_prompt",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/notification", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}

	evt := captured[0]
	if evt.Type() != events.EventTypeClaudeHookPermission {
		t.Errorf("expected event type %s, got %s", events.EventTypeClaudeHookPermission, evt.Type())
	}
	if evt.GetWorkspaceID() != "my-project" {
		t.Errorf("expected workspace_id %q, got %q", "my-project", evt.GetWorkspaceID())
	}
}

func TestHandleHook_Permission_SetsWorkspaceID(t *testing.T) {
	// "permission" is the old endpoint name, should behave same as "notification"
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			return "old-endpoint-ws", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-3",
		Cwd:       "/Users/dev/Projects/old-endpoint-ws",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/permission", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}
	if captured[0].GetWorkspaceID() != "old-endpoint-ws" {
		t.Errorf("expected workspace_id %q, got %q", "old-endpoint-ws", captured[0].GetWorkspaceID())
	}
}

func TestHandleHook_ToolStart_SetsWorkspaceID(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			return "tool-ws", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-4",
		Cwd:       "/Users/dev/Projects/tool-ws",
		ToolName:  "Bash",
		ToolUseID: "tu-1",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/tool-start", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}

	evt := captured[0]
	if evt.Type() != events.EventTypeClaudeHookToolStart {
		t.Errorf("expected event type %s, got %s", events.EventTypeClaudeHookToolStart, evt.Type())
	}
	if evt.GetWorkspaceID() != "tool-ws" {
		t.Errorf("expected workspace_id %q, got %q", "tool-ws", evt.GetWorkspaceID())
	}
}

func TestHandleHook_ToolEnd_SetsWorkspaceID(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			return "tool-end-ws", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-5",
		Cwd:       "/Users/dev/Projects/tool-end-ws",
		ToolName:  "Write",
		ToolUseID: "tu-2",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/tool-end", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}

	evt := captured[0]
	if evt.Type() != events.EventTypeClaudeHookToolEnd {
		t.Errorf("expected event type %s, got %s", events.EventTypeClaudeHookToolEnd, evt.Type())
	}
	if evt.GetWorkspaceID() != "tool-end-ws" {
		t.Errorf("expected workspace_id %q, got %q", "tool-end-ws", evt.GetWorkspaceID())
	}
}

func TestHandleHook_PermissionRequest_SetsWorkspaceID(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			return "perm-ws", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	pm := &hooksPermissionManager{}
	handler.SetPermissionManager(pm)
	handler.SetPermissionTimeout(100 * time.Millisecond)

	payload := ClaudeHookPayload{
		SessionID: "sess-6",
		Cwd:       "/Users/dev/Projects/perm-ws",
		ToolName:  "Bash",
		ToolUseID: "tu-perm-1",
		ToolInput: map[string]interface{}{"command": "ls"},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/permission-request", strings.NewReader(string(body)))
	w := httptest.NewRecorder()

	// This will timeout (no mobile response), but we can still verify
	// the workspace ID was set on the pending request.
	handler.HandleHook(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the pending request had workspace ID set
	if len(pm.addedRequests) == 0 {
		t.Fatal("expected a pending request to be added")
	}
	if pm.addedRequests[0].WorkspaceID != "perm-ws" {
		t.Errorf("expected pending request workspace_id %q, got %q", "perm-ws", pm.addedRequests[0].WorkspaceID)
	}

	// Verify the pty_permission event was published with workspace ID
	drainHookEvents()
	captured := capture.getEvents()
	var permEvent events.Event
	for _, e := range captured {
		if e.Type() == events.EventTypePTYPermission {
			permEvent = e
			break
		}
	}
	if permEvent == nil {
		t.Fatal("expected a pty_permission event to be published")
	}

	// Verify response indicates timeout
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]string
	_ = json.Unmarshal(respBody, &result)
	if result["decision"] != "ask" {
		t.Errorf("expected decision 'ask' on timeout, got %q", result["decision"])
	}
}

func TestHandleHook_PermissionRequest_NoResolver_EmptyWorkspaceID(t *testing.T) {
	handler, h, _ := setupHooksTest(nil)
	defer stopTestHub(t, h)

	pm := &hooksPermissionManager{}
	handler.SetPermissionManager(pm)
	handler.SetPermissionTimeout(100 * time.Millisecond)

	payload := ClaudeHookPayload{
		SessionID: "sess-7",
		Cwd:       "/Users/dev/Projects/no-resolver",
		ToolName:  "Bash",
		ToolUseID: "tu-perm-2",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/permission-request", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)

	if len(pm.addedRequests) == 0 {
		t.Fatal("expected a pending request to be added")
	}
	if pm.addedRequests[0].WorkspaceID != "" {
		t.Errorf("expected empty workspace_id without resolver, got %q", pm.addedRequests[0].WorkspaceID)
	}
}

func TestHandleHook_PermissionRequest_NoPermissionManager(t *testing.T) {
	handler, h, _ := setupHooksTest(nil)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-8",
		ToolName:  "Bash",
		ToolUseID: "tu-perm-3",
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/permission-request", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]string
	_ = json.Unmarshal(respBody, &result)
	if result["decision"] != "ask" {
		t.Errorf("expected decision 'ask' without permission manager, got %q", result["decision"])
	}
}

func TestHandleHook_MethodNotAllowed(t *testing.T) {
	handler, h, _ := setupHooksTest(nil)
	defer stopTestHub(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/hooks/session", nil)
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", resp.StatusCode)
	}
}

func TestHandleHook_EmptyCwd_EmptyWorkspaceID(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			t.Error("resolver should not be called with empty cwd")
			return "should-not-reach", nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	payload := ClaudeHookPayload{
		SessionID: "sess-empty-cwd",
		Cwd:       "", // empty cwd
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/session", strings.NewReader(string(body)))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) == 0 {
		t.Fatal("expected at least one event")
	}
	if captured[0].GetWorkspaceID() != "" {
		t.Errorf("expected empty workspace_id with empty cwd, got %q", captured[0].GetWorkspaceID())
	}
}

func TestHandleHook_UnknownHookType(t *testing.T) {
	handler, h, _ := setupHooksTest(nil)
	defer stopTestHub(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/hooks/unknown-type", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	handler.HandleHook(w, req)

	resp := w.Result()
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for unknown hook type, got %d", resp.StatusCode)
	}
}

func TestHandleHook_DifferentWorkspaces_DifferentIDs(t *testing.T) {
	resolver := &hooksWorkspaceResolver{
		resolveFunc: func(path string) (string, error) {
			parts := strings.Split(path, "/")
			return parts[len(parts)-1], nil
		},
	}
	handler, h, capture := setupHooksTest(resolver)
	defer stopTestHub(t, h)

	workspaces := []struct {
		cwd      string
		expected string
	}{
		{"/Users/dev/Projects/cdev-ios", "cdev-ios"},
		{"/Users/dev/Projects/Lazy", "Lazy"},
	}

	for _, ws := range workspaces {
		payload := ClaudeHookPayload{
			SessionID: "sess-multi",
			Cwd:       ws.cwd,
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/hooks/session", strings.NewReader(string(body)))
		w := httptest.NewRecorder()
		handler.HandleHook(w, req)
	}
	drainHookEvents()

	captured := capture.getEvents()
	if len(captured) != 2 {
		t.Fatalf("expected 2 events, got %d", len(captured))
	}

	for i, ws := range workspaces {
		got := captured[i].GetWorkspaceID()
		if got != ws.expected {
			t.Errorf("event %d: expected workspace_id %q, got %q", i, ws.expected, got)
		}
	}
}

func TestSetWorkspaceResolver(t *testing.T) {
	h := hub.New()
	handler := NewHooksHandler(h)

	if handler.workspaceResolver != nil {
		t.Error("expected nil resolver initially")
	}

	resolver := &hooksWorkspaceResolver{}
	handler.SetWorkspaceResolver(resolver)

	if handler.workspaceResolver == nil {
		t.Error("expected resolver to be set")
	}
}
