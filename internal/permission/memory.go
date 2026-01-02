package permission

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// MemoryManager manages permission decisions across Claude sessions.
// It provides "Allow for Session" functionality by remembering decisions
// and auto-responding to matching permission requests.
type MemoryManager struct {
	mu       sync.RWMutex
	sessions map[string]*SessionMemory // sessionID -> memory
	config   SessionMemoryConfig
	logger   zerolog.Logger

	// Pending requests waiting for mobile app response
	pendingMu sync.RWMutex
	pending   map[string]*Request // toolUseID -> request
}

// NewMemoryManager creates a new permission memory manager.
func NewMemoryManager(config SessionMemoryConfig, logger zerolog.Logger) *MemoryManager {
	m := &MemoryManager{
		sessions: make(map[string]*SessionMemory),
		config:   config,
		logger:   logger.With().Str("component", "permission_memory").Logger(),
		pending:  make(map[string]*Request),
	}

	return m
}

// StartCleanup starts a goroutine that periodically cleans up expired sessions.
func (m *MemoryManager) StartCleanup(ctx context.Context) {
	go m.cleanupLoop(ctx)
}

// cleanupLoop periodically cleans up expired sessions.
func (m *MemoryManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

// cleanup removes expired sessions.
func (m *MemoryManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for sessionID, mem := range m.sessions {
		if now.Sub(mem.LastAccess) > m.config.TTL {
			delete(m.sessions, sessionID)
			m.logger.Info().
				Str("session_id", sessionID).
				Dur("idle_duration", now.Sub(mem.LastAccess)).
				Msg("Session memory expired")
		}
	}
}

// GetOrCreateSession gets or creates session memory for a Claude session.
func (m *MemoryManager) GetOrCreateSession(sessionID, workspaceID string) *SessionMemory {
	m.mu.Lock()
	defer m.mu.Unlock()

	mem, exists := m.sessions[sessionID]
	if !exists {
		mem = &SessionMemory{
			SessionID:   sessionID,
			WorkspaceID: workspaceID,
			Decisions:   make(map[string]StoredDecision),
			CreatedAt:   time.Now(),
			LastAccess:  time.Now(),
		}
		m.sessions[sessionID] = mem
		m.logger.Debug().
			Str("session_id", sessionID).
			Str("workspace_id", workspaceID).
			Msg("Created new session memory")
	} else {
		mem.LastAccess = time.Now()
	}

	return mem
}

// CheckMemory checks if there's a stored decision for a permission request.
// Returns the decision if found, or nil if no matching pattern exists.
// This method is thread-safe and holds the lock for the entire operation
// to prevent TOCTOU race conditions.
func (m *MemoryManager) CheckMemory(sessionID, toolName string, toolInput map[string]interface{}) *StoredDecision {
	// Generate pattern BEFORE acquiring lock (no shared state needed)
	pattern := GeneratePattern(toolName, toolInput)

	// Hold the lock for the entire check-and-update operation
	m.mu.Lock()
	defer m.mu.Unlock()

	mem, exists := m.sessions[sessionID]
	if !exists {
		return nil
	}

	// Update last access time
	mem.LastAccess = time.Now()

	// Check for exact pattern match
	if decision, found := mem.Decisions[pattern]; found {
		decision.UsageCount++
		mem.Decisions[pattern] = decision

		m.logger.Debug().
			Str("session_id", sessionID).
			Str("pattern", pattern).
			Str("decision", string(decision.Decision)).
			Int("usage_count", decision.UsageCount).
			Msg("Found matching pattern in session memory")

		// Return a copy to avoid data races on the returned struct
		result := decision
		return &result
	}

	// Check for wildcard pattern matches
	for storedPattern, decision := range mem.Decisions {
		if MatchPattern(storedPattern, toolName, toolInput) {
			decision.UsageCount++
			mem.Decisions[storedPattern] = decision

			m.logger.Debug().
				Str("session_id", sessionID).
				Str("stored_pattern", storedPattern).
				Str("request_pattern", pattern).
				Str("decision", string(decision.Decision)).
				Msg("Found matching wildcard pattern in session memory")

			// Return a copy to avoid data races on the returned struct
			result := decision
			return &result
		}
	}

	return nil
}

// StoreDecision stores a permission decision in session memory.
// This method is thread-safe and atomic.
func (m *MemoryManager) StoreDecision(sessionID, workspaceID, pattern string, decision Decision) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get or create session within the same lock
	mem, exists := m.sessions[sessionID]
	if !exists {
		mem = &SessionMemory{
			SessionID:   sessionID,
			WorkspaceID: workspaceID,
			Decisions:   make(map[string]StoredDecision),
			CreatedAt:   time.Now(),
			LastAccess:  time.Now(),
		}
		m.sessions[sessionID] = mem
		m.logger.Debug().
			Str("session_id", sessionID).
			Str("workspace_id", workspaceID).
			Msg("Created new session memory")
	}

	// Check max patterns limit
	if len(mem.Decisions) >= m.config.MaxPatterns {
		// Remove oldest decision
		var oldestPattern string
		var oldestTime time.Time
		for p, d := range mem.Decisions {
			if oldestPattern == "" || d.CreatedAt.Before(oldestTime) {
				oldestPattern = p
				oldestTime = d.CreatedAt
			}
		}
		if oldestPattern != "" {
			delete(mem.Decisions, oldestPattern)
			m.logger.Debug().
				Str("session_id", sessionID).
				Str("removed_pattern", oldestPattern).
				Msg("Removed oldest pattern to make room")
		}
	}

	mem.Decisions[pattern] = StoredDecision{
		Pattern:    pattern,
		Decision:   decision,
		CreatedAt:  time.Now(),
		UsageCount: 1,
	}
	mem.LastAccess = time.Now()

	m.logger.Info().
		Str("session_id", sessionID).
		Str("pattern", pattern).
		Str("decision", string(decision)).
		Int("total_patterns", len(mem.Decisions)).
		Msg("Stored permission decision in session memory")
}

// ClearSession clears all memory for a specific session.
func (m *MemoryManager) ClearSession(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[sessionID]; exists {
		delete(m.sessions, sessionID)
		m.logger.Info().
			Str("session_id", sessionID).
			Msg("Cleared session memory")
	}
}

// ClearWorkspace clears all memory for sessions in a workspace.
func (m *MemoryManager) ClearWorkspace(workspaceID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for sessionID, mem := range m.sessions {
		if mem.WorkspaceID == workspaceID {
			delete(m.sessions, sessionID)
			count++
		}
	}

	if count > 0 {
		m.logger.Info().
			Str("workspace_id", workspaceID).
			Int("sessions_cleared", count).
			Msg("Cleared workspace session memories")
	}
}

// AddPendingRequest adds a request waiting for mobile response.
func (m *MemoryManager) AddPendingRequest(req *Request) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	m.pending[req.ToolUseID] = req

	m.logger.Debug().
		Str("tool_use_id", req.ToolUseID).
		Str("session_id", req.SessionID).
		Str("tool_name", req.ToolName).
		Msg("Added pending permission request")
}

// GetPendingRequest retrieves a pending request by tool use ID.
// Returns nil if the request doesn't exist.
func (m *MemoryManager) GetPendingRequest(toolUseID string) *Request {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()

	return m.pending[toolUseID]
}

// RemovePendingRequest removes a pending request and closes its response channel.
func (m *MemoryManager) RemovePendingRequest(toolUseID string) {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	if req, exists := m.pending[toolUseID]; exists {
		// Close the channel to unblock any waiting goroutines
		// This is safe because we only close once (under lock)
		select {
		case <-req.ResponseChan:
			// Channel already has a response, drain it
		default:
			// Channel is empty, close it
		}
		delete(m.pending, toolUseID)

		m.logger.Debug().
			Str("tool_use_id", toolUseID).
			Msg("Removed pending permission request")
	}
}

// RespondToRequest sends a response to a pending request.
// Returns true if the request was found and responded to.
// This method atomically removes the request and sends the response.
func (m *MemoryManager) RespondToRequest(toolUseID string, response *Response) bool {
	m.pendingMu.Lock()
	req, exists := m.pending[toolUseID]
	if exists {
		delete(m.pending, toolUseID)
	}
	m.pendingMu.Unlock()

	if !exists {
		m.logger.Warn().
			Str("tool_use_id", toolUseID).
			Msg("Attempted to respond to non-existent request")
		return false
	}

	// Store decision if scope is session (do this before sending response)
	if response.Scope == ScopeSession && response.Pattern != "" {
		m.StoreDecision(req.SessionID, req.WorkspaceID, response.Pattern, response.Decision)
	}

	// Send response through channel (non-blocking)
	// The channel has buffer size 1, so this should succeed unless
	// the request already timed out
	select {
	case req.ResponseChan <- response:
		m.logger.Info().
			Str("tool_use_id", toolUseID).
			Str("decision", string(response.Decision)).
			Str("scope", string(response.Scope)).
			Msg("Sent response to permission request")
		return true
	default:
		// Channel is full or closed - request likely timed out
		m.logger.Warn().
			Str("tool_use_id", toolUseID).
			Msg("Response channel not ready - request may have timed out")
		return false
	}
}

// GetAndRemovePendingRequest atomically retrieves and removes a pending request.
// Returns nil if the request doesn't exist.
// This prevents race conditions where the request is accessed after removal.
func (m *MemoryManager) GetAndRemovePendingRequest(toolUseID string) *Request {
	m.pendingMu.Lock()
	defer m.pendingMu.Unlock()

	req, exists := m.pending[toolUseID]
	if exists {
		delete(m.pending, toolUseID)
	}
	return req
}

// ListPendingRequests returns all pending permission requests.
// This is used when cdev-ios reconnects to get any permissions that were missed.
func (m *MemoryManager) ListPendingRequests() []*Request {
	m.pendingMu.RLock()
	defer m.pendingMu.RUnlock()

	requests := make([]*Request, 0, len(m.pending))
	for _, req := range m.pending {
		requests = append(requests, req)
	}
	return requests
}

// GetSessionStats returns statistics about session memory.
// This method acquires locks in a consistent order to prevent deadlocks.
func (m *MemoryManager) GetSessionStats() map[string]interface{} {
	// Always acquire mu first, then pendingMu (consistent lock ordering)
	m.mu.RLock()
	sessionCount := len(m.sessions)
	totalPatterns := 0
	for _, mem := range m.sessions {
		totalPatterns += len(mem.Decisions)
	}
	m.mu.RUnlock()

	m.pendingMu.RLock()
	pendingCount := len(m.pending)
	m.pendingMu.RUnlock()

	return map[string]interface{}{
		"active_sessions":  sessionCount,
		"total_patterns":   totalPatterns,
		"pending_requests": pendingCount,
	}
}
