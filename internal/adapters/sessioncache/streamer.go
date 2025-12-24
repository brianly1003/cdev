// Package sessioncache provides session streaming for real-time updates.
package sessioncache

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// SessionStreamer watches a specific session file and streams new messages.
type SessionStreamer struct {
	sessionsDir string
	hub         ports.EventHub

	mu              sync.RWMutex
	watchedSession  string    // Currently watched session ID
	watchedFile     string    // Full path to watched file
	lastOffset      int64     // Last read position in file
	lastSize        int64     // Last known file size
	watcher         *fsnotify.Watcher
	done            chan struct{}
	running         bool
}

// NewSessionStreamer creates a new session streamer.
func NewSessionStreamer(sessionsDir string, hub ports.EventHub) *SessionStreamer {
	return &SessionStreamer{
		sessionsDir: sessionsDir,
		hub:         hub,
	}
}

// WatchSession starts watching a specific session for new messages.
// Call this when iOS selects a session to view.
func (s *SessionStreamer) WatchSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Stop watching previous session if any
	if s.running {
		s.stopWatchingLocked()
	}

	filePath := filepath.Join(s.sessionsDir, sessionID+".jsonl")

	// Check if file exists
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	// Create watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := watcher.Add(filePath); err != nil {
		watcher.Close()
		return err
	}

	s.watcher = watcher
	s.watchedSession = sessionID
	s.watchedFile = filePath
	s.lastSize = info.Size()
	s.lastOffset = info.Size() // Start from end of file (only new content)
	s.done = make(chan struct{})
	s.running = true

	go s.watchLoop()

	log.Info().
		Str("session_id", sessionID).
		Int64("file_size", info.Size()).
		Msg("started watching session for new messages")

	return nil
}

// UnwatchSession stops watching the current session.
func (s *SessionStreamer) UnwatchSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopWatchingLocked()
}

// stopWatchingLocked stops watching (must hold lock).
func (s *SessionStreamer) stopWatchingLocked() {
	if !s.running {
		return
	}

	s.running = false
	close(s.done)

	if s.watcher != nil {
		s.watcher.Close()
		s.watcher = nil
	}

	log.Info().
		Str("session_id", s.watchedSession).
		Msg("stopped watching session")

	s.watchedSession = ""
	s.watchedFile = ""
	s.lastOffset = 0
	s.lastSize = 0
}

// GetWatchedSession returns the currently watched session ID.
func (s *SessionStreamer) GetWatchedSession() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watchedSession
}

// watchLoop monitors the session file for changes.
func (s *SessionStreamer) watchLoop() {
	// Debounce: wait for writes to settle before reading
	// This prevents reading while Claude is mid-write
	var lastEvent time.Time
	debounceTimer := time.NewTimer(time.Hour) // Initially disabled
	debounceTimer.Stop()

	// Poll interval for checking file size (backup, in case fsnotify misses events)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			debounceTimer.Stop()
			return

		case event, ok := <-s.watcher.Events:
			if !ok {
				return
			}

			if event.Op&fsnotify.Write == fsnotify.Write {
				// Debounce: wait 200ms after last write before reading
				// This ensures Claude has finished writing the line
				lastEvent = time.Now()
				debounceTimer.Reset(200 * time.Millisecond)
			}

		case <-debounceTimer.C:
			// Only read if no new events in last 200ms
			if time.Since(lastEvent) >= 200*time.Millisecond {
				s.checkForNewContent()
			}

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("session streamer watcher error")

		case <-ticker.C:
			// Periodic check in case fsnotify misses events
			// Only check if no recent fsnotify events (avoid duplicate reads)
			if time.Since(lastEvent) >= 500*time.Millisecond {
				s.checkForNewContent()
			}
		}
	}
}

// checkForNewContent checks if the file has grown and reads new lines.
func (s *SessionStreamer) checkForNewContent() {
	s.mu.Lock()
	filePath := s.watchedFile
	sessionID := s.watchedSession
	lastOffset := s.lastOffset
	s.mu.Unlock()

	if filePath == "" {
		return
	}

	// Check current file size
	info, err := os.Stat(filePath)
	if err != nil {
		return
	}

	currentSize := info.Size()

	// File hasn't grown
	if currentSize <= lastOffset {
		return
	}

	// Read new content
	file, err := os.Open(filePath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to open session file for streaming")
		return
	}
	defer file.Close()

	// Seek to last position
	_, err = file.Seek(lastOffset, io.SeekStart)
	if err != nil {
		log.Warn().Err(err).Msg("failed to seek in session file")
		return
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max to handle extended thinking

	newOffset := lastOffset
	messagesEmitted := 0

	for scanner.Scan() {
		line := scanner.Text()
		newOffset += int64(len(line)) + 1 // +1 for newline

		if line == "" {
			continue
		}

		// Parse and emit message
		if msg := s.parseAndEmitMessage(line, sessionID); msg != nil {
			messagesEmitted++
		}
	}

	if scanner.Err() != nil {
		log.Warn().Err(scanner.Err()).Msg("error scanning session file")
	}

	// Update offset
	s.mu.Lock()
	s.lastOffset = newOffset
	s.lastSize = currentSize
	s.mu.Unlock()

	if messagesEmitted > 0 {
		log.Debug().
			Str("session_id", sessionID).
			Int("messages", messagesEmitted).
			Int64("new_offset", newOffset).
			Msg("streamed new session messages")
	}
}

// parseAndEmitMessage parses a JSONL line and emits a claude_message event.
func (s *SessionStreamer) parseAndEmitMessage(line, sessionID string) *events.ClaudeMessagePayload {
	// Parse the raw message
	var raw struct {
		Type      string `json:"type"`
		Subtype   string `json:"subtype,omitempty"`  // "compact_boundary" for context compaction marker
		UserType  string `json:"userType,omitempty"` // "external" for auto-generated messages
		Content   string `json:"content,omitempty"`  // For system messages (e.g., "Conversation compacted")
		UUID      string `json:"uuid,omitempty"`
		Timestamp string `json:"timestamp,omitempty"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}

	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil
	}

	// Handle context compaction boundary (system message)
	// Emit this as a special message so iOS knows context was compacted
	if raw.Type == "system" && raw.Subtype == "compact_boundary" {
		payload := &events.ClaudeMessagePayload{
			SessionID:           sessionID,
			Type:                "system",
			Role:                "system",
			IsContextCompaction: true,
			Content: []events.ClaudeMessageContent{
				{Type: "text", Text: raw.Content},
			},
		}
		s.hub.Publish(events.NewClaudeMessageEventFull(*payload))
		return payload
	}

	// Only emit user and assistant messages
	if raw.Type != "user" && raw.Type != "assistant" {
		return nil
	}

	// Parse content blocks
	var contentBlocks []events.ClaudeMessageContent

	// Try parsing content as array of blocks
	var blocks []struct {
		Type    string          `json:"type"`
		Text    string          `json:"text,omitempty"`
		ID      string          `json:"id,omitempty"`
		Name    string          `json:"name,omitempty"`
		Input   json.RawMessage `json:"input,omitempty"`
		Content string          `json:"content,omitempty"`
	}

	if err := json.Unmarshal(raw.Message.Content, &blocks); err == nil {
		for _, block := range blocks {
			cb := events.ClaudeMessageContent{
				Type: block.Type,
			}
			switch block.Type {
			case "text":
				cb.Text = block.Text
			case "thinking":
				cb.Text = block.Text
			case "tool_use":
				cb.ToolName = block.Name
				cb.ToolID = block.ID
				if block.Input != nil {
					// Parse input as map
					var inputMap map[string]interface{}
					if json.Unmarshal(block.Input, &inputMap) == nil {
						cb.ToolInput = inputMap
					}
				}
			case "tool_result":
				cb.Content = block.Content
			}
			contentBlocks = append(contentBlocks, cb)
		}
	} else {
		// Try as simple string
		var textContent string
		if json.Unmarshal(raw.Message.Content, &textContent) == nil && textContent != "" {
			contentBlocks = append(contentBlocks, events.ClaudeMessageContent{
				Type: "text",
				Text: textContent,
			})
		}
	}

	if len(contentBlocks) == 0 {
		return nil
	}

	// Detect context compaction messages
	// These are auto-generated when Claude Code's context window is maxed out
	isContextCompaction := false
	if raw.Type == "user" && raw.UserType == "external" {
		// Check if content starts with the compaction prefix
		for _, block := range contentBlocks {
			if block.Type == "text" && strings.HasPrefix(block.Text, contextCompactionPrefix) {
				isContextCompaction = true
				break
			}
		}
	}

	// Create and emit event
	payload := &events.ClaudeMessagePayload{
		SessionID:           sessionID,
		Type:                raw.Type,
		Role:                raw.Message.Role,
		Content:             contentBlocks,
		IsContextCompaction: isContextCompaction,
	}

	s.hub.Publish(events.NewClaudeMessageEventFull(*payload))

	return payload
}

// Close stops the streamer and releases resources.
func (s *SessionStreamer) Close() {
	s.UnwatchSession()
}
