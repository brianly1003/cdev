package permission

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestMemoryManager_ConcurrentCheckAndStore(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         time.Hour,
		MaxPatterns: 100,
	}
	m := NewMemoryManager(config, logger)

	sessionID := "test-session"
	workspaceID := "test-workspace"

	// Run concurrent operations
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(3)

		// Concurrent CheckMemory
		go func(i int) {
			defer wg.Done()
			toolInput := map[string]interface{}{"command": "rm file.txt"}
			m.CheckMemory(sessionID, "Bash", toolInput)
		}(i)

		// Concurrent StoreDecision
		go func(i int) {
			defer wg.Done()
			pattern := GeneratePattern("Bash", map[string]interface{}{"command": "rm file.txt"})
			m.StoreDecision(sessionID, workspaceID, pattern, DecisionAllow)
		}(i)

		// Concurrent GetOrCreateSession
		go func(i int) {
			defer wg.Done()
			m.GetOrCreateSession(sessionID, workspaceID)
		}(i)
	}

	wg.Wait()
}

func TestMemoryManager_ConcurrentPendingRequests(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         time.Hour,
		MaxPatterns: 100,
	}
	m := NewMemoryManager(config, logger)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		toolUseID := "tool-" + string(rune(i))
		wg.Add(4)

		// Add request
		go func(id string) {
			defer wg.Done()
			req := &Request{
				ToolUseID:    id,
				SessionID:    "session",
				WorkspaceID:  "workspace",
				ToolName:     "Bash",
				ResponseChan: make(chan *Response, 1),
			}
			m.AddPendingRequest(req)
		}(toolUseID)

		// Get request
		go func(id string) {
			defer wg.Done()
			m.GetPendingRequest(id)
		}(toolUseID)

		// Remove request
		go func(id string) {
			defer wg.Done()
			m.RemovePendingRequest(id)
		}(toolUseID)

		// Get stats
		go func() {
			defer wg.Done()
			m.GetSessionStats()
		}()
	}

	wg.Wait()
}

func TestMemoryManager_RespondToRequest_Timeout(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         time.Hour,
		MaxPatterns: 100,
	}
	m := NewMemoryManager(config, logger)

	toolUseID := "test-tool-use"
	req := &Request{
		ToolUseID:    toolUseID,
		SessionID:    "session",
		WorkspaceID:  "workspace",
		ToolName:     "Bash",
		ToolInput:    map[string]interface{}{"command": "rm file.txt"},
		ResponseChan: make(chan *Response, 1),
	}

	m.AddPendingRequest(req)

	// Simulate timeout by removing the request
	m.RemovePendingRequest(toolUseID)

	// Now try to respond - should fail
	response := &Response{
		Decision: DecisionAllow,
		Scope:    ScopeOnce,
	}
	success := m.RespondToRequest(toolUseID, response)
	if success {
		t.Error("Expected RespondToRequest to fail after request was removed")
	}
}

func TestMemoryManager_GetAndRemovePendingRequest_Atomic(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         time.Hour,
		MaxPatterns: 100,
	}
	m := NewMemoryManager(config, logger)

	toolUseID := "test-tool-use"
	req := &Request{
		ToolUseID:    toolUseID,
		SessionID:    "session",
		WorkspaceID:  "workspace",
		ToolName:     "Bash",
		ResponseChan: make(chan *Response, 1),
	}

	m.AddPendingRequest(req)

	// Concurrent GetAndRemove - only one should succeed
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := m.GetAndRemovePendingRequest(toolUseID); r != nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successCount != 1 {
		t.Errorf("Expected exactly 1 successful GetAndRemove, got %d", successCount)
	}
}

func TestMemoryManager_CheckMemory_ReturnsACopy(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         time.Hour,
		MaxPatterns: 100,
	}
	m := NewMemoryManager(config, logger)

	sessionID := "test-session"
	workspaceID := "test-workspace"
	pattern := "Bash(rm:*)"

	// Store a decision
	m.StoreDecision(sessionID, workspaceID, pattern, DecisionAllow)

	// Get the decision
	toolInput := map[string]interface{}{"command": "rm file.txt"}
	result := m.CheckMemory(sessionID, "Bash", toolInput)
	if result == nil {
		t.Fatal("Expected to find stored decision")
	}

	// Modify the returned copy
	result.Decision = DecisionDeny

	// Check that the original is unchanged
	result2 := m.CheckMemory(sessionID, "Bash", toolInput)
	if result2.Decision != DecisionAllow {
		t.Error("Original decision was modified through returned copy")
	}
}

func TestMemoryManager_CleanupWithConcurrentAccess(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         1 * time.Millisecond, // Very short TTL for testing
		MaxPatterns: 100,
	}
	m := NewMemoryManager(config, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start cleanup goroutine
	m.StartCleanup(ctx)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		// Concurrent access
		go func(i int) {
			defer wg.Done()
			sessionID := "session-" + string(rune(i%10))
			m.GetOrCreateSession(sessionID, "workspace")
			toolInput := map[string]interface{}{"command": "rm file.txt"}
			m.CheckMemory(sessionID, "Bash", toolInput)
		}(i)

		// Concurrent store
		go func(i int) {
			defer wg.Done()
			sessionID := "session-" + string(rune(i%10))
			pattern := "Bash(rm:*)"
			m.StoreDecision(sessionID, "workspace", pattern, DecisionAllow)
		}(i)
	}

	wg.Wait()

	// Let cleanup run
	time.Sleep(10 * time.Millisecond)
}

func TestMemoryManager_MaxPatternsLimit(t *testing.T) {
	logger := zerolog.Nop()
	config := SessionMemoryConfig{
		Enabled:     true,
		TTL:         time.Hour,
		MaxPatterns: 3, // Small limit for testing
	}
	m := NewMemoryManager(config, logger)

	sessionID := "test-session"
	workspaceID := "test-workspace"

	// Store more patterns than the limit
	for i := 0; i < 5; i++ {
		pattern := "Bash(cmd" + string(rune('a'+i)) + ":*)"
		m.StoreDecision(sessionID, workspaceID, pattern, DecisionAllow)
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	}

	// Check that we only have MaxPatterns stored
	stats := m.GetSessionStats()
	totalPatterns := stats["total_patterns"].(int)
	if totalPatterns > config.MaxPatterns {
		t.Errorf("Expected at most %d patterns, got %d", config.MaxPatterns, totalPatterns)
	}
}
