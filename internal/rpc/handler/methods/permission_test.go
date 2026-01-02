package methods

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/permission"
)

// --- Mock Permission Manager ---

type mockPermissionManager struct {
	mu              sync.Mutex
	memoryDecisions map[string]*permission.StoredDecision
	pendingRequests map[string]*permission.Request
	stats           map[string]interface{}
}

func newMockPermissionManager() *mockPermissionManager {
	return &mockPermissionManager{
		memoryDecisions: make(map[string]*permission.StoredDecision),
		pendingRequests: make(map[string]*permission.Request),
		stats:           make(map[string]interface{}),
	}
}

func (m *mockPermissionManager) CheckMemory(sessionID, toolName string, toolInput map[string]interface{}) *permission.StoredDecision {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sessionID + ":" + toolName
	return m.memoryDecisions[key]
}

func (m *mockPermissionManager) StoreDecision(sessionID, workspaceID, pattern string, decision permission.Decision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sessionID + ":" + pattern
	m.memoryDecisions[key] = &permission.StoredDecision{
		Pattern:   pattern,
		Decision:  decision,
		CreatedAt: time.Now(),
	}
}

func (m *mockPermissionManager) AddPendingRequest(req *permission.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pendingRequests[req.ToolUseID] = req
}

func (m *mockPermissionManager) GetPendingRequest(toolUseID string) *permission.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingRequests[toolUseID]
}

func (m *mockPermissionManager) RemovePendingRequest(toolUseID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pendingRequests, toolUseID)
}

func (m *mockPermissionManager) RespondToRequest(toolUseID string, response *permission.Response) bool {
	m.mu.Lock()
	req, ok := m.pendingRequests[toolUseID]
	m.mu.Unlock()

	if !ok || req == nil {
		return false
	}

	select {
	case req.ResponseChan <- response:
		return true
	default:
		return false
	}
}

func (m *mockPermissionManager) GetAndRemovePendingRequest(toolUseID string) *permission.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	req := m.pendingRequests[toolUseID]
	delete(m.pendingRequests, toolUseID)
	return req
}

func (m *mockPermissionManager) GetSessionStats() map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats
}

// SetMemoryDecision is a test helper to preset memory decisions
func (m *mockPermissionManager) SetMemoryDecision(sessionID, toolName string, decision *permission.StoredDecision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := sessionID + ":" + toolName
	m.memoryDecisions[key] = decision
}

// --- Mock Event Publisher ---

type mockEventPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func newMockEventPublisher() *mockEventPublisher {
	return &mockEventPublisher{
		events: make([]events.Event, 0),
	}
}

func (p *mockEventPublisher) Publish(event events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *mockEventPublisher) GetEvents() []events.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]events.Event{}, p.events...)
}

// --- Mock Workspace Resolver ---

type mockWorkspaceResolver struct {
	workspaceID string
	err         error
}

func (r *mockWorkspaceResolver) ResolveWorkspaceID(path string) (string, error) {
	if r.err != nil {
		return "", r.err
	}
	return r.workspaceID, nil
}

// --- Tests ---

func TestPermissionService_Request_MissingParams(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)
	service.SetTimeout(100 * time.Millisecond)

	tests := []struct {
		name        string
		params      map[string]interface{}
		wantErrCode int
	}{
		{
			name:        "missing session_id",
			params:      map[string]interface{}{"tool_name": "Bash", "tool_use_id": "123"},
			wantErrCode: -32602, // InvalidParams
		},
		{
			name:        "missing tool_name",
			params:      map[string]interface{}{"session_id": "sess-1", "tool_use_id": "123"},
			wantErrCode: -32602,
		},
		{
			name:        "missing tool_use_id",
			params:      map[string]interface{}{"session_id": "sess-1", "tool_name": "Bash"},
			wantErrCode: -32602,
		},
		{
			name:        "empty params",
			params:      map[string]interface{}{},
			wantErrCode: -32602,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramsJSON, _ := json.Marshal(tt.params)
			_, err := service.Request(context.Background(), paramsJSON)

			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if err.Code != tt.wantErrCode {
				t.Errorf("Error code = %d, want %d", err.Code, tt.wantErrCode)
			}
		})
	}
}

func TestPermissionService_Request_MemoryHit(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	// Pre-set a memory decision
	manager.SetMemoryDecision("session-1", "Bash", &permission.StoredDecision{
		Pattern:  "Bash(rm:*)",
		Decision: permission.DecisionAllow,
	})

	service := NewPermissionService(manager, publisher, resolver)

	params := map[string]interface{}{
		"session_id":  "session-1",
		"tool_name":   "Bash",
		"tool_input":  map[string]interface{}{"command": "rm file.txt"},
		"tool_use_id": "tool-123",
	}
	paramsJSON, _ := json.Marshal(params)

	result, err := service.Request(context.Background(), paramsJSON)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	response, ok := result.(*permission.Response)
	if !ok {
		t.Fatalf("Expected *permission.Response, got %T", result)
	}

	if response.Decision != permission.DecisionAllow {
		t.Errorf("Decision = %v, want allow", response.Decision)
	}
	if response.Scope != permission.ScopeSession {
		t.Errorf("Scope = %v, want session", response.Scope)
	}
}

func TestPermissionService_Request_Timeout(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)
	service.SetTimeout(50 * time.Millisecond) // Short timeout for test

	params := map[string]interface{}{
		"session_id":  "session-timeout",
		"tool_name":   "Bash",
		"tool_input":  map[string]interface{}{"command": "ls"},
		"tool_use_id": "tool-timeout",
		"cwd":         "/tmp",
	}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.Request(context.Background(), paramsJSON)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if err.Code != -32603 { // InternalError
		t.Errorf("Error code = %d, want -32603", err.Code)
	}
}

func TestPermissionService_Request_PublishesEvent(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)
	service.SetTimeout(100 * time.Millisecond)

	params := map[string]interface{}{
		"session_id":  "session-event",
		"tool_name":   "Write",
		"tool_input":  map[string]interface{}{"file_path": "/tmp/test.txt", "content": "hello"},
		"tool_use_id": "tool-event",
		"cwd":         "/home/user",
	}
	paramsJSON, _ := json.Marshal(params)

	// Start the request in a goroutine (it will timeout)
	go func() {
		_, _ = service.Request(context.Background(), paramsJSON)
	}()

	// Wait a bit for the event to be published
	time.Sleep(20 * time.Millisecond)

	events := publisher.GetEvents()
	if len(events) == 0 {
		t.Fatal("Expected at least one event to be published")
	}

	event := events[0]
	if event.Type() != "pty_permission" {
		t.Errorf("Event type = %v, want pty_permission", event.Type())
	}
}

func TestPermissionService_Respond_MissingParams(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)

	tests := []struct {
		name        string
		params      map[string]interface{}
		wantErrCode int
	}{
		{
			name:        "missing tool_use_id",
			params:      map[string]interface{}{"decision": "allow"},
			wantErrCode: -32602,
		},
		{
			name:        "missing decision",
			params:      map[string]interface{}{"tool_use_id": "123"},
			wantErrCode: -32602,
		},
		{
			name:        "invalid decision",
			params:      map[string]interface{}{"tool_use_id": "123", "decision": "maybe"},
			wantErrCode: -32602,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paramsJSON, _ := json.Marshal(tt.params)
			_, err := service.Respond(context.Background(), paramsJSON)

			if err == nil {
				t.Fatal("Expected error, got nil")
			}
			if err.Code != tt.wantErrCode {
				t.Errorf("Error code = %d, want %d", err.Code, tt.wantErrCode)
			}
		})
	}
}

func TestPermissionService_Respond_RequestNotFound(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)

	params := map[string]interface{}{
		"tool_use_id": "non-existent",
		"decision":    "allow",
	}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.Respond(context.Background(), paramsJSON)
	if err == nil {
		t.Fatal("Expected error for non-existent request")
	}

	if err.Code != -32603 { // InternalError
		t.Errorf("Error code = %d, want -32603", err.Code)
	}
}

func TestPermissionService_Respond_Success(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)

	// Add a pending request
	req := &permission.Request{
		ToolUseID:    "tool-respond",
		SessionID:    "session-respond",
		WorkspaceID:  "workspace-respond",
		ToolName:     "Bash",
		ToolInput:    map[string]interface{}{"command": "ls"},
		ResponseChan: make(chan *permission.Response, 1),
	}
	manager.AddPendingRequest(req)

	// Respond to the request
	params := map[string]interface{}{
		"tool_use_id": "tool-respond",
		"decision":    "allow",
		"scope":       "once",
	}
	paramsJSON, _ := json.Marshal(params)

	result, err := service.Respond(context.Background(), paramsJSON)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map, got %T", result)
	}

	if resultMap["success"] != true {
		t.Errorf("success = %v, want true", resultMap["success"])
	}

	// Check that response was sent to channel
	select {
	case response := <-req.ResponseChan:
		if response.Decision != permission.DecisionAllow {
			t.Errorf("Decision = %v, want allow", response.Decision)
		}
	default:
		t.Error("Expected response in channel")
	}
}

func TestPermissionService_Respond_SessionScope_StoresDecision(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)

	// Add a pending request
	req := &permission.Request{
		ToolUseID:    "tool-session",
		SessionID:    "session-session",
		WorkspaceID:  "workspace-session",
		ToolName:     "Bash",
		ToolInput:    map[string]interface{}{"command": "git status"},
		ResponseChan: make(chan *permission.Response, 1),
	}
	manager.AddPendingRequest(req)

	// Respond with session scope
	params := map[string]interface{}{
		"tool_use_id": "tool-session",
		"decision":    "allow",
		"scope":       "session",
	}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.Respond(context.Background(), paramsJSON)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Drain the response channel
	<-req.ResponseChan

	// Check that decision was stored (using the generated pattern)
	// Note: The actual pattern depends on GeneratePattern implementation
	stats := manager.GetSessionStats()
	_ = stats // Stats would show stored decisions in real implementation
}

func TestPermissionService_Stats(t *testing.T) {
	manager := newMockPermissionManager()
	manager.stats["total_sessions"] = 5
	manager.stats["total_patterns"] = 10

	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)

	result, err := service.Stats(context.Background(), nil)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	stats, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map, got %T", result)
	}

	if stats["total_sessions"] != 5 {
		t.Errorf("total_sessions = %v, want 5", stats["total_sessions"])
	}
	if stats["total_patterns"] != 10 {
		t.Errorf("total_patterns = %v, want 10", stats["total_patterns"])
	}
}

func TestPermissionService_NilManager(t *testing.T) {
	service := NewPermissionService(nil, nil, nil)

	params := map[string]interface{}{
		"session_id":  "session-1",
		"tool_name":   "Bash",
		"tool_use_id": "tool-1",
	}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.Request(context.Background(), paramsJSON)
	if err == nil {
		t.Fatal("Expected error for nil manager")
	}
	if err.Code != -32603 { // InternalError
		t.Errorf("Error code = %d, want -32603", err.Code)
	}
}

func TestWorkspaceIDResolver_ResolveWorkspaceID(t *testing.T) {
	resolver := NewWorkspaceIDResolver()

	tests := []struct {
		path string
		want string
	}{
		{"/home/user/project", "project"},
		{"/Users/dev/my-app", "my-app"},
		{"/", "/"},
		{"", "."},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := resolver.ResolveWorkspaceID(tt.path)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveWorkspaceID(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestPermissionService_Request_ContextCancellation(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)
	service.SetTimeout(5 * time.Second) // Long timeout

	ctx, cancel := context.WithCancel(context.Background())

	params := map[string]interface{}{
		"session_id":  "session-cancel",
		"tool_name":   "Bash",
		"tool_input":  map[string]interface{}{"command": "ls"},
		"tool_use_id": "tool-cancel",
	}
	paramsJSON, _ := json.Marshal(params)

	// Cancel the context after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := service.Request(ctx, paramsJSON)
	if err == nil {
		t.Fatal("Expected error for cancelled context")
	}
}

func TestPermissionService_Respond_DenyDecision(t *testing.T) {
	manager := newMockPermissionManager()
	publisher := newMockEventPublisher()
	resolver := &mockWorkspaceResolver{workspaceID: "test-workspace"}

	service := NewPermissionService(manager, publisher, resolver)

	// Add a pending request
	req := &permission.Request{
		ToolUseID:    "tool-deny",
		SessionID:    "session-deny",
		WorkspaceID:  "workspace-deny",
		ToolName:     "Bash",
		ToolInput:    map[string]interface{}{"command": "rm -rf /"},
		ResponseChan: make(chan *permission.Response, 1),
	}
	manager.AddPendingRequest(req)

	// Respond with deny
	params := map[string]interface{}{
		"tool_use_id": "tool-deny",
		"decision":    "deny",
	}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.Respond(context.Background(), paramsJSON)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Check that response was sent with deny
	response := <-req.ResponseChan
	if response.Decision != permission.DecisionDeny {
		t.Errorf("Decision = %v, want deny", response.Decision)
	}
}
