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
func (h *mockEventHub) Unsubscribe(id string)          {}
func (h *mockEventHub) IsRunning() bool                { return h.started && !h.stopped }
func (h *mockEventHub) SubscriberCount() int           { return 0 }

// --- Manager Tests ---

func TestNewManager(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", []string{"-p"}, 5, hub, false, nil)

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
	m := NewManager("claude", nil, 10, hub, true, nil)

	if !m.skipPermissions {
		t.Error("skipPermissions should be true")
	}
}

func TestNewManagerWithContext(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", []string{"-p"}, 5, false, "/tmp/workspace", "ws-123", "session-456", nil)

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
	m := NewManagerWithContext(hub, "claude", nil, 5, false, "", "ws-123", "", nil)

	if m.WorkspaceID() != "ws-123" {
		t.Errorf("WorkspaceID() = %s, want ws-123", m.WorkspaceID())
	}
}

func TestManager_SessionID(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", nil, 5, false, "", "", "session-123", nil)

	if m.SessionID() != "session-123" {
		t.Errorf("SessionID() = %s, want session-123", m.SessionID())
	}
}

func TestManager_SetSessionID(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	m.SetSessionID("new-session-id")

	if m.SessionID() != "new-session-id" {
		t.Errorf("SessionID() = %s, want new-session-id", m.SessionID())
	}
}

func TestManager_SetSessionID_ThreadSafe(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

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
	m := NewManager("claude", nil, 5, hub, false, nil)

	state := m.State()

	if state != events.ClaudeStateIdle {
		t.Errorf("State() = %v, want Idle", state)
	}
}

func TestManager_IsRunning_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	if m.IsRunning() {
		t.Error("IsRunning() should be false initially")
	}
}

func TestManager_PID_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

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
	m := NewManager("claude", nil, 5, nil, false, nil)

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
			m := NewManager("claude", nil, tt.timeoutMinutes, hub, false, nil)

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
	m := NewManager("claude", args, 5, hub, false, nil)

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
	m := NewManagerWithContext(hub, "claude", nil, 5, false, tempDir, "ws-123", "", nil)

	// logDir should be set to workDir/.cdev/logs
	if m.logDir == "" {
		t.Error("logDir should be set when workDir is provided")
	}
}

func TestManager_LogDir_NoWorkDir(t *testing.T) {
	hub := newMockEventHub()
	m := NewManagerWithContext(hub, "claude", nil, 5, false, "", "ws-123", "", nil)

	if m.logDir != "" {
		t.Error("logDir should be empty when workDir is not provided")
	}
}

// --- State Machine Tests ---

func TestManager_State_ThreadSafe(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	var wg sync.WaitGroup
	numGoroutines := 100

	// Multiple goroutines reading state concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.State()
			_ = m.IsRunning()
			_ = m.PID()
		}()
	}

	wg.Wait()
	// No panic means thread-safe
}

func TestManager_InitialState(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	// Verify all initial state values
	if m.State() != events.ClaudeStateIdle {
		t.Errorf("initial State() = %v, want Idle", m.State())
	}
	if m.IsRunning() {
		t.Error("initial IsRunning() should be false")
	}
	if m.PID() != 0 {
		t.Errorf("initial PID() = %d, want 0", m.PID())
	}
	if m.IsWaitingForInput() {
		t.Error("initial IsWaitingForInput() should be false")
	}
	if m.ClaudeSessionID() != "" {
		t.Errorf("initial ClaudeSessionID() = %q, want empty", m.ClaudeSessionID())
	}
}

func TestManager_IsWaitingForInput_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	if m.IsWaitingForInput() {
		t.Error("IsWaitingForInput() should be false initially")
	}
}

func TestManager_ClaudeSessionID_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	if m.ClaudeSessionID() != "" {
		t.Errorf("ClaudeSessionID() = %q, want empty string", m.ClaudeSessionID())
	}
}

func TestManager_GetPendingToolUse_Initial(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	id, name := m.GetPendingToolUse()
	if id != "" {
		t.Errorf("pending tool use ID = %q, want empty", id)
	}
	if name != "" {
		t.Errorf("pending tool name = %q, want empty", name)
	}
}

// --- PTY State Tests ---

func TestPTYState_Values(t *testing.T) {
	tests := []struct {
		state    PTYState
		expected string
	}{
		{PTYStateIdle, "idle"},
		{PTYStateThinking, "thinking"},
		{PTYStatePermission, "permission"},
		{PTYStateQuestion, "question"},
		{PTYStateError, "error"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("PTYState = %s, want %s", tt.state, tt.expected)
			}
		})
	}
}

// --- Permission Type Tests ---

func TestPermissionType_Values(t *testing.T) {
	tests := []struct {
		permType PermissionType
		expected string
	}{
		{PermissionTypeWriteFile, "write_file"},
		{PermissionTypeEditFile, "edit_file"},
		{PermissionTypeDeleteFile, "delete_file"},
		{PermissionTypeBashCommand, "bash_command"},
		{PermissionTypeMCP, "mcp_tool"},
		{PermissionTypeTrustFolder, "trust_folder"},
		{PermissionTypeUnknown, "unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.permType), func(t *testing.T) {
			if string(tt.permType) != tt.expected {
				t.Errorf("PermissionType = %s, want %s", tt.permType, tt.expected)
			}
		})
	}
}

// --- Concurrent Access Tests ---

func TestManager_ConcurrentStateAccess(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	var wg sync.WaitGroup

	// Concurrent readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.State()
				_ = m.IsRunning()
				_ = m.IsWaitingForInput()
				_ = m.ClaudeSessionID()
				_, _ = m.GetPendingToolUse()
			}
		}()
	}

	wg.Wait()
	// No panic or deadlock means thread-safe
}

// --- SetLogDir Tests ---

func TestManager_SetLogDir(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	// Initially empty
	if m.logDir != "" {
		t.Error("initial logDir should be empty")
	}

	// Set log dir
	m.SetLogDir("/tmp/logs")

	if m.logDir != "/tmp/logs" {
		t.Errorf("logDir = %s, want /tmp/logs", m.logDir)
	}
}

// --- SetWorkDir Tests ---

func TestManager_SetWorkDir(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	// Initially empty
	if m.workDir != "" {
		t.Error("initial workDir should be empty")
	}

	// Set work dir
	m.SetWorkDir("/tmp/workspace")

	if m.workDir != "/tmp/workspace" {
		t.Errorf("workDir = %s, want /tmp/workspace", m.workDir)
	}
}

// --- Event Publishing Tests ---

func (h *mockEventHub) GetEvents() []events.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]events.Event, len(h.events))
	copy(result, h.events)
	return result
}

func (h *mockEventHub) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = make([]events.Event, 0)
}

func TestManager_EventPublishing_NilHub(t *testing.T) {
	// Manager with nil hub should not panic when publishing events
	m := NewManager("claude", nil, 5, nil, false, nil)

	// These should not panic
	m.publishEvent(events.NewClaudeIdleEvent())
	m.publishEvent(events.NewClaudeStatusEvent(events.ClaudeStateRunning, "test", 12345))
}

// --- SkipPermissions Tests ---

func TestManager_SkipPermissions_Default(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	if m.skipPermissions {
		t.Error("skipPermissions should be false by default")
	}
}

func TestManager_SkipPermissions_Enabled(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, true, nil)

	if !m.skipPermissions {
		t.Error("skipPermissions should be true when enabled")
	}
}

// --- PTYPermissionPrompt Tests ---

func TestPTYPermissionPrompt_FullStruct(t *testing.T) {
	prompt := PTYPermissionPrompt{
		Type:        PermissionTypeBashCommand,
		Target:      "rm -rf /tmp/test",
		Description: "Run bash command: rm -rf /tmp/test",
	}

	if prompt.Type != PermissionTypeBashCommand {
		t.Errorf("Type = %v, want BashCommand", prompt.Type)
	}
	if prompt.Target != "rm -rf /tmp/test" {
		t.Errorf("Target = %s, want 'rm -rf /tmp/test'", prompt.Target)
	}
	if prompt.Description != "Run bash command: rm -rf /tmp/test" {
		t.Errorf("Description mismatch")
	}
}

func TestPTYPermissionPrompt_AllTypes(t *testing.T) {
	tests := []struct {
		permType PermissionType
		target   string
	}{
		{PermissionTypeWriteFile, "/path/to/file.txt"},
		{PermissionTypeEditFile, "/path/to/edit.go"},
		{PermissionTypeDeleteFile, "/path/to/delete.tmp"},
		{PermissionTypeBashCommand, "npm install"},
		{PermissionTypeMCP, "some-mcp-operation"},
		{PermissionTypeTrustFolder, "/path/to/folder"},
		{PermissionTypeUnknown, "unknown operation"},
	}

	for _, tt := range tests {
		t.Run(string(tt.permType), func(t *testing.T) {
			prompt := PTYPermissionPrompt{
				Type:        tt.permType,
				Target:      tt.target,
				Description: "Test description",
			}

			if prompt.Type != tt.permType {
				t.Errorf("Type = %v, want %v", prompt.Type, tt.permType)
			}
			if prompt.Target != tt.target {
				t.Errorf("Target = %s, want %s", prompt.Target, tt.target)
			}
		})
	}
}

// --- Rotation Config Tests ---

func TestManager_RotationConfig_Nil(t *testing.T) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	if m.rotationConfig != nil {
		t.Error("rotationConfig should be nil when not provided")
	}
}

// --- WorkDir Tests ---

func TestManager_WorkDir_WithLogDir(t *testing.T) {
	hub := newMockEventHub()
	tempDir := t.TempDir()

	m := NewManagerWithContext(hub, "claude", nil, 5, false, tempDir, "", "", nil)

	if m.workDir != tempDir {
		t.Errorf("workDir = %s, want %s", m.workDir, tempDir)
	}

	// Log dir should be set to workDir/.cdev/logs
	expectedLogDir := tempDir + "/.cdev/logs"
	if m.logDir != expectedLogDir {
		t.Errorf("logDir = %s, want %s", m.logDir, expectedLogDir)
	}
}

// --- State Constants Tests ---

func TestClaudeState_Values(t *testing.T) {
	tests := []struct {
		state    events.ClaudeState
		expected string
	}{
		{events.ClaudeStateIdle, "idle"},
		{events.ClaudeStateRunning, "running"},
		{events.ClaudeStateWaiting, "waiting"},
		{events.ClaudeStateError, "error"},
		{events.ClaudeStateStopped, "stopped"},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if string(tt.state) != tt.expected {
				t.Errorf("ClaudeState = %s, want %s", tt.state, tt.expected)
			}
		})
	}
}

// --- Benchmark Tests ---

func BenchmarkManager_State(b *testing.B) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.State()
	}
}

func BenchmarkManager_IsRunning(b *testing.B) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.IsRunning()
	}
}

func BenchmarkManager_SessionID(b *testing.B) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.SessionID()
	}
}

func BenchmarkManager_ConcurrentAccess(b *testing.B) {
	hub := newMockEventHub()
	m := NewManager("claude", nil, 5, hub, false, nil)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = m.State()
			_ = m.IsRunning()
			_ = m.SessionID()
		}
	})
}
