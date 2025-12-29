package claude

import (
	"sync"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
)

// --- Mock EventHub ---

type mockEventHub struct {
	mu       sync.Mutex
	events   []events.Event
	started  bool
	stopped  bool
}

func newMockEventHub() *mockEventHub {
	return &mockEventHub{
		events: make([]events.Event, 0),
	}
}

func (h *mockEventHub) Start() error {
	h.started = true
	return nil
}

func (h *mockEventHub) Stop() error {
	h.stopped = true
	return nil
}

func (h *mockEventHub) Publish(event events.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
}

func (h *mockEventHub) Subscribe(sub ports.Subscriber) {}
func (h *mockEventHub) Unsubscribe(id string)     {}
func (h *mockEventHub) IsRunning() bool           { return h.started && !h.stopped }
func (h *mockEventHub) SubscriberCount() int      { return 0 }

func (h *mockEventHub) getEvents() []events.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]events.Event, len(h.events))
	copy(result, h.events)
	return result
}

// --- Manager Tests ---

func TestNewManager(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", []string{"-p"}, 5, hub, false)

	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.command != "claude" {
		t.Errorf("command = %s, want claude", m.command)
	}
	if m.timeout != 5*time.Minute {
		t.Errorf("timeout = %v, want 5m", m.timeout)
	}
	if m.state != events.ClaudeStateIdle {
		t.Errorf("state = %v, want Idle", m.state)
	}
	if m.skipPermissions {
		t.Error("skipPermissions should be false")
	}
}

func TestNewManager_WithSkipPermissions(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 10, hub, true)

	if !m.skipPermissions {
		t.Error("skipPermissions should be true")
	}
}

func TestNewManagerWithContext(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", []string{"-p"}, 5, false, "/tmp/workspace", "ws-123", "session-456")

	if m == nil {
		t.Fatal("NewManagerWithContext returned nil")
	}
	if m.workspaceID != "ws-123" {
		t.Errorf("workspaceID = %s, want ws-123", m.workspaceID)
	}
	if m.sessionID != "session-456" {
		t.Errorf("sessionID = %s, want session-456", m.sessionID)
	}
	if m.workDir != "/tmp/workspace" {
		t.Errorf("workDir = %s, want /tmp/workspace", m.workDir)
	}
}

func TestManager_WorkspaceID(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", nil, 5, false, "", "ws-123", "")

	if m.WorkspaceID() != "ws-123" {
		t.Errorf("WorkspaceID() = %s, want ws-123", m.WorkspaceID())
	}
}

func TestManager_SessionID(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", nil, 5, false, "", "", "session-123")

	if m.SessionID() != "session-123" {
		t.Errorf("SessionID() = %s, want session-123", m.SessionID())
	}
}

func TestManager_SetSessionID(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false)

	m.SetSessionID("new-session-id")

	if m.SessionID() != "new-session-id" {
		t.Errorf("SessionID() = %s, want new-session-id", m.SessionID())
	}
}

func TestManager_SetSessionID_ThreadSafe(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)
		go func(id int) {
			defer wg.Done()
			m.SetSessionID(string(rune('a' + id%26)))
		}(i)
		go func() {
			defer wg.Done()
			_ = m.SessionID()
		}()
	}

	wg.Wait()
	// No panic means thread-safe
}

func TestManager_State_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false)

	state := m.State()

	if state != events.ClaudeStateIdle {
		t.Errorf("State() = %v, want Idle", state)
	}
}

func TestManager_IsRunning_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false)

	if m.IsRunning() {
		t.Error("IsRunning() should be false initially")
	}
}

func TestManager_PID_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false)

	if m.PID() != 0 {
		t.Errorf("PID() = %d, want 0", m.PID())
	}
}

// --- Session Mode Tests ---

func TestSessionMode_Constants(t *testing.T) {
	if SessionModeNew != "new" {
		t.Errorf("SessionModeNew = %s, want 'new'", SessionModeNew)
	}
	if SessionModeContinue != "continue" {
		t.Errorf("SessionModeContinue = %s, want 'continue'", SessionModeContinue)
	}
}

// --- PTY State Tests ---

func TestPTYState_Constants(t *testing.T) {
	states := []PTYState{
		PTYStateIdle,
		PTYStateThinking,
		PTYStatePermission,
		PTYStateQuestion,
		PTYStateError,
	}

	// Just verify these constants are defined
	for _, s := range states {
		if s == "" {
			t.Error("PTYState constant is empty")
		}
	}
}

// --- Permission Type Tests ---

func TestPermissionType_Constants(t *testing.T) {
	types := []PermissionType{
		PermissionTypeWriteFile,
		PermissionTypeEditFile,
		PermissionTypeDeleteFile,
		PermissionTypeBashCommand,
		PermissionTypeMCP,
		PermissionTypeTrustFolder,
		PermissionTypeUnknown,
	}

	for _, pt := range types {
		if pt == "" {
			t.Error("PermissionType constant is empty")
		}
	}
}

// --- PTYPermissionPrompt Tests ---

func TestPTYPermissionPrompt_Struct(t *testing.T) {
	prompt := PTYPermissionPrompt{
		Type:        PermissionTypeWriteFile,
		Target:      "test.go",
		Description: "Write to file test.go",
	}

	if prompt.Type != PermissionTypeWriteFile {
		t.Errorf("Type = %v, want WriteFile", prompt.Type)
	}
	if prompt.Target != "test.go" {
		t.Errorf("Target = %s, want test.go", prompt.Target)
	}
}

// --- Manager with nil hub ---

func TestNewManager_NilHub(t *testing.T) {
	// Manager should work with nil hub (just won't publish events)
	m := NewManager("claude", nil, 5, nil, false)

	if m == nil {
		t.Fatal("NewManager returned nil with nil hub")
	}
	if m.hub != nil {
		t.Error("hub should be nil")
	}
}

// --- Timeout Tests ---

func TestManager_Timeout(t *testing.T) {
	tests := []struct {
		name           string
		timeoutMinutes int
		expected       time.Duration
	}{
		{"1 minute", 1, 1 * time.Minute},
		{"5 minutes", 5, 5 * time.Minute},
		{"30 minutes", 30, 30 * time.Minute},
		{"zero", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hub := newMockEventHub()
			m := NewManager("claude", nil, tt.timeoutMinutes, hub, false)

			if m.timeout != tt.expected {
				t.Errorf("timeout = %v, want %v", m.timeout, tt.expected)
			}
		})
	}
}

// --- Args Tests ---

func TestManager_Args(t *testing.T) {
	hub := newMockEventHub()
	args := []string{"-p", "--verbose", "--output-format", "stream-json"}
	m := NewManager("claude", args, 5, hub, false)

	if len(m.args) != len(args) {
		t.Errorf("args length = %d, want %d", len(m.args), len(args))
	}
	for i, arg := range args {
		if m.args[i] != arg {
			t.Errorf("args[%d] = %s, want %s", i, m.args[i], arg)
		}
	}
}

// --- LogDir Tests ---

func TestManager_LogDir_WithWorkDir(t *testing.T) {
	hub := newMockEventHub()
	tempDir := t.TempDir()
	m := NewManagerWithContext(hub, "claude", nil, 5, false, tempDir, "ws-123", "")

	// logDir should be set to workDir/.cdev/logs
	if m.logDir == "" {
		t.Error("logDir should be set when workDir is provided")
	}
}

func TestManager_LogDir_NoWorkDir(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", nil, 5, false, "", "ws-123", "")

	if m.logDir != "" {
		t.Error("logDir should be empty when workDir is not provided")
	}
}
