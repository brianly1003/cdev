package methods

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// mockSessionStreamer is a mock implementation of SessionStreamer for testing.
type mockSessionStreamer struct {
	watchedSession string
	watchError     error
	watchCalled    bool
	unwatchCalled  bool
}

func (m *mockSessionStreamer) WatchSession(sessionID string) error {
	m.watchCalled = true
	if m.watchError != nil {
		return m.watchError
	}
	m.watchedSession = sessionID
	return nil
}

func (m *mockSessionStreamer) UnwatchSession() {
	m.unwatchCalled = true
	m.watchedSession = ""
}

func (m *mockSessionStreamer) GetWatchedSession() string {
	return m.watchedSession
}

// mockSessionProvider is a mock implementation of SessionProvider for testing.
type mockSessionProvider struct {
	sessions map[string]*SessionInfo
}

func (m *mockSessionProvider) ListSessions(ctx context.Context) ([]SessionInfo, error) {
	var result []SessionInfo
	for _, s := range m.sessions {
		result = append(result, *s)
	}
	return result, nil
}

func (m *mockSessionProvider) GetSession(ctx context.Context, sessionID string) (*SessionInfo, error) {
	if s, ok := m.sessions[sessionID]; ok {
		return s, nil
	}
	return nil, errors.New("session not found")
}

func (m *mockSessionProvider) GetSessionMessages(ctx context.Context, sessionID string, limit, offset int, order string) ([]SessionMessage, int, error) {
	return nil, 0, nil
}

func (m *mockSessionProvider) GetSessionElements(ctx context.Context, sessionID string, limit int, beforeID, afterID string) ([]SessionElement, int, error) {
	return nil, 0, nil
}

func (m *mockSessionProvider) DeleteSession(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockSessionProvider) DeleteAllSessions(ctx context.Context) (int, error) {
	return 0, nil
}

func (m *mockSessionProvider) AgentType() string {
	return "claude"
}

func TestNewSessionService(t *testing.T) {
	streamer := &mockSessionStreamer{}
	service := NewSessionService(streamer)

	if service == nil {
		t.Fatal("NewSessionService returned nil")
	}
	if service.streamer != streamer {
		t.Error("streamer not set correctly")
	}
	if service.providers == nil {
		t.Error("providers map not initialized")
	}
}

func TestNewSessionServiceWithNilStreamer(t *testing.T) {
	service := NewSessionService(nil)

	if service == nil {
		t.Fatal("NewSessionService returned nil")
	}
	if service.streamer != nil {
		t.Error("streamer should be nil")
	}
}

func TestWatchSession(t *testing.T) {
	streamer := &mockSessionStreamer{}
	service := NewSessionService(streamer)

	// Register a provider with a session
	provider := &mockSessionProvider{
		sessions: map[string]*SessionInfo{
			"session-123": {SessionID: "session-123", AgentType: "claude"},
		},
	}
	service.RegisterProvider(provider)

	// Test watching an existing session
	params := WatchSessionParams{SessionID: "session-123"}
	paramsJSON, _ := json.Marshal(params)

	result, err := service.WatchSession(context.Background(), paramsJSON)

	if err != nil {
		t.Fatalf("WatchSession returned error: %v", err)
	}
	if !streamer.watchCalled {
		t.Error("streamer.WatchSession was not called")
	}
	if streamer.watchedSession != "session-123" {
		t.Errorf("wrong session watched: got %s, want session-123", streamer.watchedSession)
	}

	watchResult, ok := result.(WatchSessionResult)
	if !ok {
		t.Fatalf("result is not WatchSessionResult: %T", result)
	}
	if !watchResult.Watching {
		t.Error("Watching should be true")
	}
	if watchResult.Status != "watching" {
		t.Errorf("Status should be 'watching', got %s", watchResult.Status)
	}
}

func TestWatchSessionNotFound(t *testing.T) {
	streamer := &mockSessionStreamer{}
	service := NewSessionService(streamer)

	// Register a provider with no sessions
	provider := &mockSessionProvider{
		sessions: map[string]*SessionInfo{},
	}
	service.RegisterProvider(provider)

	params := WatchSessionParams{SessionID: "nonexistent"}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.WatchSession(context.Background(), paramsJSON)

	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	// streamer.WatchSession should NOT be called for nonexistent session
	if streamer.watchCalled {
		t.Error("expected WatchSession not to be called for nonexistent session")
	}
}

func TestWatchSessionMissingSessionID(t *testing.T) {
	streamer := &mockSessionStreamer{}
	service := NewSessionService(streamer)

	params := WatchSessionParams{SessionID: ""}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.WatchSession(context.Background(), paramsJSON)

	if err == nil {
		t.Fatal("expected error for missing session_id")
	}
	if streamer.watchCalled {
		t.Error("streamer.WatchSession should not be called for invalid params")
	}
}

func TestWatchSessionStreamerError(t *testing.T) {
	streamer := &mockSessionStreamer{
		watchError: errors.New("file not found"),
	}
	service := NewSessionService(streamer)

	provider := &mockSessionProvider{
		sessions: map[string]*SessionInfo{
			"session-123": {SessionID: "session-123", AgentType: "claude"},
		},
	}
	service.RegisterProvider(provider)

	params := WatchSessionParams{SessionID: "session-123"}
	paramsJSON, _ := json.Marshal(params)

	_, err := service.WatchSession(context.Background(), paramsJSON)

	if err == nil {
		t.Fatal("expected error when streamer fails")
	}
	if err.Code != -32603 { // InternalError
		t.Errorf("expected InternalError code (-32603), got %d", err.Code)
	}
}

func TestWatchSessionWithNilStreamer(t *testing.T) {
	// Service with nil streamer should still work (just won't watch)
	service := NewSessionService(nil)

	provider := &mockSessionProvider{
		sessions: map[string]*SessionInfo{
			"session-123": {SessionID: "session-123", AgentType: "claude"},
		},
	}
	service.RegisterProvider(provider)

	params := WatchSessionParams{SessionID: "session-123"}
	paramsJSON, _ := json.Marshal(params)

	result, err := service.WatchSession(context.Background(), paramsJSON)

	if err != nil {
		t.Fatalf("WatchSession returned error: %v", err)
	}

	watchResult, ok := result.(WatchSessionResult)
	if !ok {
		t.Fatalf("result is not WatchSessionResult: %T", result)
	}
	if !watchResult.Watching {
		t.Error("Watching should be true even with nil streamer")
	}
}

func TestUnwatchSession(t *testing.T) {
	streamer := &mockSessionStreamer{
		watchedSession: "session-123",
	}
	service := NewSessionService(streamer)

	result, err := service.UnwatchSession(context.Background(), nil)

	if err != nil {
		t.Fatalf("UnwatchSession returned error: %v", err)
	}
	if !streamer.unwatchCalled {
		t.Error("streamer.UnwatchSession was not called")
	}

	unwatchResult, ok := result.(UnwatchSessionResult)
	if !ok {
		t.Fatalf("result is not UnwatchSessionResult: %T", result)
	}
	if unwatchResult.Status != "unwatched" {
		t.Errorf("Status should be 'unwatched', got %s", unwatchResult.Status)
	}
	if unwatchResult.Watching != false {
		t.Error("Watching should be false")
	}
}

func TestUnwatchSessionWithNilStreamer(t *testing.T) {
	service := NewSessionService(nil)

	result, err := service.UnwatchSession(context.Background(), nil)

	if err != nil {
		t.Fatalf("UnwatchSession returned error: %v", err)
	}

	unwatchResult, ok := result.(UnwatchSessionResult)
	if !ok {
		t.Fatalf("result is not UnwatchSessionResult: %T", result)
	}
	if unwatchResult.Status != "unwatched" {
		t.Errorf("Status should be 'unwatched', got %s", unwatchResult.Status)
	}
	if unwatchResult.Watching != false {
		t.Error("Watching should be false")
	}
}

func TestWatchSessionParamsJSON(t *testing.T) {
	// Verify snake_case parsing works
	input := `{"session_id":"test-session-456"}`
	var params WatchSessionParams
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if params.SessionID != "test-session-456" {
		t.Errorf("SessionID mismatch: got %s, want test-session-456", params.SessionID)
	}
}

func TestWatchSessionResultJSON(t *testing.T) {
	result := WatchSessionResult{
		Status:   "watching",
		Watching: true,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	jsonStr := string(data)
	if !containsField(jsonStr, "status") {
		t.Errorf("expected 'status' field, got: %s", jsonStr)
	}
	if !containsField(jsonStr, "watching") {
		t.Errorf("expected 'watching' field, got: %s", jsonStr)
	}
}
