// Package sessioncache provides session streaming for real-time updates.
package sessioncache

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/jsonl"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// SessionStreamer watches a specific session file and streams new messages.
type SessionStreamer struct {
	sessionsDir string
	hub         ports.EventHub

	mu             sync.RWMutex
	watchedSession string // Currently watched session ID
	watchedFile    string // Full path to watched file
	lastOffset     int64  // Last read position in file
	lastSize       int64  // Last known file size
	watcher        *fsnotify.Watcher
	done           chan struct{}
	running        bool

	// For delayed stream_read_complete event
	completeTimer       *time.Timer   // Timer for 3-second delay before emitting complete
	pendingCompleteInfo *completeInfo // Info to include in the complete event
}

// completeInfo stores info for the pending stream_read_complete event
type completeInfo struct {
	sessionID     string
	messagesCount int
	offset        int64
	fileSize      int64
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
		_ = watcher.Close()
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
		_ = s.watcher.Close()
		s.watcher = nil
	}

	// Cancel pending complete timer
	if s.completeTimer != nil {
		s.completeTimer.Stop()
		s.completeTimer = nil
	}
	s.pendingCompleteInfo = nil

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

	// Channel to receive complete timer signals
	completeChan := make(chan *completeInfo, 1)

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

				// Cancel pending complete timer since new content is being written
				s.cancelPendingComplete()
			}

		case <-debounceTimer.C:
			// Only read if no new events in last 200ms
			if time.Since(lastEvent) >= 200*time.Millisecond {
				s.checkForNewContent(completeChan)
			}

		case info := <-completeChan:
			// Complete timer fired - emit the event
			s.emitStreamReadComplete(info)

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("session streamer watcher error")

		case <-ticker.C:
			// Periodic check in case fsnotify misses events
			// Only check if no recent fsnotify events (avoid duplicate reads)
			if time.Since(lastEvent) >= 500*time.Millisecond {
				s.checkForNewContent(completeChan)
			}
		}
	}
}

// cancelPendingComplete cancels any pending stream_read_complete event.
func (s *SessionStreamer) cancelPendingComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.completeTimer != nil {
		s.completeTimer.Stop()
		s.completeTimer = nil
		s.pendingCompleteInfo = nil
		log.Debug().Msg("cancelled pending stream_read_complete due to new content")
	}
}

// schedulePendingComplete schedules a stream_read_complete event after 3 seconds.
func (s *SessionStreamer) schedulePendingComplete(info *completeInfo, completeChan chan<- *completeInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel any existing timer
	if s.completeTimer != nil {
		s.completeTimer.Stop()
	}

	s.pendingCompleteInfo = info
	s.completeTimer = time.AfterFunc(3*time.Second, func() {
		s.mu.Lock()
		pending := s.pendingCompleteInfo
		s.completeTimer = nil
		s.pendingCompleteInfo = nil
		s.mu.Unlock()

		if pending != nil {
			// Send to channel to be handled in watchLoop
			select {
			case completeChan <- pending:
			default:
				// Channel full, skip (shouldn't happen with buffer of 1)
			}
		}
	})

	log.Debug().
		Str("session_id", info.sessionID).
		Int64("offset", info.offset).
		Int64("file_size", info.fileSize).
		Msg("scheduled stream_read_complete in 3 seconds")
}

// emitStreamReadComplete emits the stream_read_complete event.
func (s *SessionStreamer) emitStreamReadComplete(info *completeInfo) {
	log.Info().
		Str("session_id", info.sessionID).
		Int("messages", info.messagesCount).
		Int64("offset", info.offset).
		Int64("file_size", info.fileSize).
		Msg("emitting stream_read_complete event (after 3s delay)")
	event := events.NewStreamReadCompleteEvent(info.sessionID, info.messagesCount, info.offset, info.fileSize)
	event.SetAgentType("claude")
	s.hub.Publish(event)
}

// checkForNewContent checks if the file has grown and reads new lines.
func (s *SessionStreamer) checkForNewContent(completeChan chan<- *completeInfo) {
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
		// log.Debug().
		// 	Str("session_id", sessionID).
		// 	Int64("current_size", currentSize).
		// 	Int64("last_offset", lastOffset).
		// 	Msg("checkForNewContent: no new content")
		return
	}

	// New content found - cancel any pending complete timer
	s.cancelPendingComplete()

	log.Debug().
		Str("session_id", sessionID).
		Int64("current_size", currentSize).
		Int64("last_offset", lastOffset).
		Int64("bytes_to_read", currentSize-lastOffset).
		Msg("checkForNewContent: found new content to read")

	// Read new content
	file, err := os.Open(filePath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to open session file for streaming")
		return
	}
	defer func() { _ = file.Close() }()

	// Seek to last position
	_, err = file.Seek(lastOffset, io.SeekStart)
	if err != nil {
		log.Warn().Err(err).Msg("failed to seek in session file")
		return
	}

	newOffset := lastOffset
	messagesEmitted := 0

	reader := jsonl.NewReader(file, 0)

	for {
		line, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Warn().Err(err).Msg("error scanning session file")
			break
		}
		newOffset += int64(line.BytesRead)

		if line.TooLong {
			log.Warn().
				Str("session_id", sessionID).
				Int("bytes_read", line.BytesRead).
				Msg("session JSONL line exceeded max size; skipping")
			continue
		}
		if len(line.Data) == 0 {
			continue
		}

		// Parse and emit message
		if msg := s.parseAndEmitMessage(string(line.Data), sessionID); msg != nil {
			messagesEmitted++
		}
	}

	// Log state after reading
	log.Debug().
		Str("session_id", sessionID).
		Int("messages", messagesEmitted).
		Int64("new_offset", newOffset).
		Int64("current_size", currentSize).
		Bool("caught_up", newOffset >= currentSize).
		Msg("checkForNewContent: finished reading")

	// Update offset
	s.mu.Lock()
	s.lastOffset = newOffset
	s.lastSize = currentSize
	s.mu.Unlock()

	// Schedule stream_read_complete when we've caught up to current file end
	// Wait 3 seconds before emitting to ensure no more content is written
	// This signals to iOS that we've finished reading all available content
	if newOffset >= currentSize {
		info := &completeInfo{
			sessionID:     sessionID,
			messagesCount: messagesEmitted,
			offset:        newOffset,
			fileSize:      currentSize,
		}
		s.schedulePendingComplete(info, completeChan)
	} else {
		log.Warn().
			Str("session_id", sessionID).
			Int64("new_offset", newOffset).
			Int64("current_size", currentSize).
			Int64("gap", currentSize-newOffset).
			Msg("checkForNewContent: NOT caught up - gap between read position and file size")
	}
}

// parseAndEmitMessage parses a JSONL line and emits a claude_message event.
func (s *SessionStreamer) parseAndEmitMessage(line, sessionID string) *events.ClaudeMessagePayload {
	// Parse the raw message
	var raw struct {
		Type       string `json:"type"`
		Subtype    string `json:"subtype,omitempty"`  // "compact_boundary" for context compaction marker
		UserType   string `json:"userType,omitempty"` // "external" for auto-generated messages
		Content    string `json:"content,omitempty"`  // For system messages (e.g., "Conversation compacted")
		UUID       string `json:"uuid,omitempty"`
		Timestamp  string `json:"timestamp,omitempty"`
		StopReason string `json:"stop_reason,omitempty"` // "end_turn", "tool_use", etc.
		Message    struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			Model      string          `json:"model,omitempty"`       // Model used (e.g., "claude-opus-4-5-20251101")
			StopReason string          `json:"stop_reason,omitempty"` // Also check inside message
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
			Timestamp: raw.Timestamp,
		}
		event := events.NewClaudeMessageEventFull(*payload)
		event.SetAgentType("claude")
		s.hub.Publish(event)
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
						normalizeStreamerToolInput(cb.ToolName, inputMap)
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

	// Get stop_reason (check both top-level and inside message)
	stopReason := raw.StopReason
	if stopReason == "" {
		stopReason = raw.Message.StopReason
	}

	// Debug: log all parsed messages with their stop_reason
	log.Debug().
		Str("session_id", sessionID).
		Str("type", raw.Type).
		Str("stop_reason_top", raw.StopReason).
		Str("stop_reason_msg", raw.Message.StopReason).
		Str("stop_reason_final", stopReason).
		Msg("parsed JSONL message")

	// Create and emit event
	payload := &events.ClaudeMessagePayload{
		SessionID:           sessionID,
		Type:                raw.Type,
		Role:                raw.Message.Role,
		Content:             contentBlocks,
		Model:               raw.Message.Model,
		IsContextCompaction: isContextCompaction,
		StopReason:          stopReason,
		Timestamp:           raw.Timestamp,
	}

	// Log when we emit stop_reason for debugging
	if stopReason != "" {
		log.Info().
			Str("session_id", sessionID).
			Str("stop_reason", stopReason).
			Msg("emitting claude_message with stop_reason")
	}

	event := events.NewClaudeMessageEventFull(*payload)
	event.SetAgentType("claude")
	s.hub.Publish(event)

	return payload
}

func normalizeStreamerToolInput(toolName string, input map[string]interface{}) {
	if len(input) == 0 {
		return
	}

	switch toolName {
	case "view_image":
		if path, ok := input["path"].(string); ok {
			if compact := compactDotCdevStreamerPath(path); compact != "" {
				input["path"] = compact
			}
		}
	}
}

func compactDotCdevStreamerPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if idx := strings.Index(path, "/.cdev/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// Close stops the streamer and releases resources.
func (s *SessionStreamer) Close() {
	s.UnwatchSession()
}
