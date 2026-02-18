// Package codex provides Codex CLI session streaming for real-time updates.
package codex

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/jsonl"
	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// SessionStreamer watches a Codex session file and streams new messages.
type SessionStreamer struct {
	workspacePath string
	hub           ports.EventHub

	mu             sync.RWMutex
	watchedSession string
	watchedFile    string
	lastOffset     int64
	lastSize       int64
	watcher        *fsnotify.Watcher
	done           chan struct{}
	running        bool

	completeTimer       *time.Timer
	pendingCompleteInfo *completeInfo

	// Best-effort de-dupe: Codex logs can include both event_msg.agent_reasoning and
	// response_item.reasoning for the same content in close succession.
	lastThinkingText string
	lastThinkingAt   time.Time

	// Pending "Explored" summaries for contiguous tool-use batches.
	pendingExplored   []string
	pendingExploredTS string
}

type completeInfo struct {
	sessionID     string
	messagesCount int
	offset        int64
	fileSize      int64
}

// NewSessionStreamer creates a new Codex session streamer.
func NewSessionStreamer(workspacePath string, hub ports.EventHub) *SessionStreamer {
	return &SessionStreamer{
		workspacePath: workspacePath,
		hub:           hub,
	}
}

// WatchSession starts watching a Codex session for new messages.
func (s *SessionStreamer) WatchSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		s.stopWatchingLocked()
	}

	// Prefer the global index cache so session watching works even when the current
	// cdev workspace differs from the Codex session's cwd.
	filePath := ""
	if idx := GetGlobalIndexCache(); idx != nil {
		if entry, err := idx.FindSessionByID(sessionID); err == nil && entry != nil {
			filePath = entry.FullPath
		}
	}
	if filePath == "" {
		_, foundPath, err := FindSessionByID(s.workspacePath, sessionID)
		if err != nil {
			return err
		}
		filePath = foundPath
	}
	if filePath == "" {
		return os.ErrNotExist
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}

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
	s.lastOffset = info.Size()
	s.done = make(chan struct{})
	s.running = true

	go s.watchLoop()

	log.Info().
		Str("session_id", sessionID).
		Int64("file_size", info.Size()).
		Msg("started watching codex session for new messages")

	return nil
}

// UnwatchSession stops watching the current session.
func (s *SessionStreamer) UnwatchSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopWatchingLocked()
}

// GetWatchedSession returns the currently watched session ID.
func (s *SessionStreamer) GetWatchedSession() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watchedSession
}

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

	if s.completeTimer != nil {
		s.completeTimer.Stop()
		s.completeTimer = nil
	}
	s.pendingCompleteInfo = nil

	log.Info().
		Str("session_id", s.watchedSession).
		Msg("stopped watching codex session")

	s.watchedSession = ""
	s.watchedFile = ""
	s.lastOffset = 0
	s.lastSize = 0
	s.lastThinkingText = ""
	s.lastThinkingAt = time.Time{}
	s.pendingExplored = nil
	s.pendingExploredTS = ""
}

func (s *SessionStreamer) watchLoop() {
	var lastEvent time.Time
	debounceTimer := time.NewTimer(time.Hour)
	debounceTimer.Stop()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

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
				lastEvent = time.Now()
				debounceTimer.Reset(200 * time.Millisecond)
				s.cancelPendingComplete()
			}

		case <-debounceTimer.C:
			if time.Since(lastEvent) >= 200*time.Millisecond {
				s.checkForNewContent(completeChan)
			}

		case info := <-completeChan:
			s.emitStreamReadComplete(info)

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("codex session streamer watcher error")

		case <-ticker.C:
			if time.Since(lastEvent) >= 500*time.Millisecond {
				s.checkForNewContent(completeChan)
			}
		}
	}
}

func (s *SessionStreamer) cancelPendingComplete() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.completeTimer != nil {
		s.completeTimer.Stop()
		s.completeTimer = nil
		s.pendingCompleteInfo = nil
		log.Debug().Msg("cancelled pending stream_read_complete due to new codex content")
	}
}

func (s *SessionStreamer) schedulePendingComplete(info *completeInfo, completeChan chan<- *completeInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
			select {
			case completeChan <- pending:
			default:
			}
		}
	})

	log.Debug().
		Str("session_id", info.sessionID).
		Int64("offset", info.offset).
		Int64("file_size", info.fileSize).
		Msg("scheduled codex stream_read_complete in 3 seconds")
}

func (s *SessionStreamer) emitStreamReadComplete(info *completeInfo) {
	s.emitPendingExplored(info.sessionID, "")

	log.Info().
		Str("session_id", info.sessionID).
		Int("messages", info.messagesCount).
		Int64("offset", info.offset).
		Int64("file_size", info.fileSize).
		Msg("emitting stream_read_complete event (codex)")
	event := events.NewStreamReadCompleteEvent(info.sessionID, info.messagesCount, info.offset, info.fileSize)
	event.SetAgentType("codex")
	s.hub.Publish(event)
}

func (s *SessionStreamer) checkForNewContent(completeChan chan<- *completeInfo) {
	s.mu.Lock()
	filePath := s.watchedFile
	sessionID := s.watchedSession
	lastOffset := s.lastOffset
	s.mu.Unlock()

	if filePath == "" {
		return
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return
	}

	currentSize := info.Size()
	if currentSize <= lastOffset {
		return
	}

	s.cancelPendingComplete()

	log.Debug().
		Str("session_id", sessionID).
		Int64("current_size", currentSize).
		Int64("last_offset", lastOffset).
		Int64("bytes_to_read", currentSize-lastOffset).
		Msg("codex checkForNewContent: found new content to read")

	file, err := os.Open(filePath)
	if err != nil {
		log.Warn().Err(err).Msg("failed to open codex session file for streaming")
		return
	}
	defer func() { _ = file.Close() }()

	_, err = file.Seek(lastOffset, io.SeekStart)
	if err != nil {
		log.Warn().Err(err).Msg("failed to seek in codex session file")
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
			log.Warn().Err(err).Msg("error scanning codex session file")
			break
		}
		newOffset += int64(line.BytesRead)

		if line.TooLong {
			log.Warn().
				Str("session_id", sessionID).
				Int("bytes_read", line.BytesRead).
				Msg("codex JSONL line exceeded max size; skipping")
			continue
		}
		if len(line.Data) == 0 {
			continue
		}

		if msg := s.parseAndEmitMessage(string(line.Data), sessionID); msg != nil {
			messagesEmitted++
		}
	}

	log.Debug().
		Str("session_id", sessionID).
		Int("messages", messagesEmitted).
		Int64("new_offset", newOffset).
		Int64("current_size", currentSize).
		Bool("caught_up", newOffset >= currentSize).
		Msg("codex checkForNewContent: finished reading")

	s.mu.Lock()
	s.lastOffset = newOffset
	s.lastSize = currentSize
	s.mu.Unlock()

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
			Msg("codex checkForNewContent: NOT caught up - gap between read position and file size")
	}
}

func (s *SessionStreamer) parseAndEmitMessage(line, sessionID string) *events.ClaudeMessagePayload {
	item, err := ParseConversationLine(line)
	if err != nil || item == nil {
		return nil
	}

	if shouldFlushExploredBeforeItem(item) {
		s.emitPendingExplored(sessionID, item.Timestamp)
	}
	for _, summary := range collectToolSummaries(item.Content) {
		if strings.TrimSpace(summary) == "" {
			continue
		}
		s.pendingExplored = append(s.pendingExplored, summary)
		s.pendingExploredTS = item.Timestamp
	}

	// De-dupe thinking blocks when Codex emits near-identical entries back-to-back.
	if len(item.Content) == 1 && item.Content[0].Type == "thinking" {
		text := strings.TrimSpace(item.Content[0].Text)
		if text == "" {
			return nil
		}
		if lastTS, ok := parseTimestampForDedup(item.Timestamp); ok && s.lastThinkingText == text && !s.lastThinkingAt.IsZero() {
			delta := lastTS.Sub(s.lastThinkingAt)
			if delta < 0 {
				delta = -delta
			}
			if delta <= 2*time.Second {
				return nil
			}
		}
		if ts, ok := parseTimestampForDedup(item.Timestamp); ok {
			s.lastThinkingAt = ts
		} else {
			s.lastThinkingAt = time.Now()
		}
		s.lastThinkingText = text
	}

	msg := &events.ClaudeMessagePayload{
		SessionID:           sessionID,
		Type:                item.Role,
		Role:                item.Role,
		Content:             item.Content,
		IsContextCompaction: item.IsContextCompaction,
		Timestamp:           item.Timestamp,
	}

	event := events.NewClaudeMessageEventFull(*msg)
	event.SetAgentType("codex")
	s.hub.Publish(event)
	return msg
}

func (s *SessionStreamer) emitPendingExplored(sessionID, fallbackTS string) {
	if len(s.pendingExplored) == 0 {
		return
	}

	ts := s.pendingExploredTS
	if ts == "" {
		ts = fallbackTS
	}

	msg := events.ClaudeMessagePayload{
		SessionID: sessionID,
		Type:      "assistant",
		Role:      "assistant",
		Content: []events.ClaudeMessageContent{
			{Type: "text", Text: formatExploredText(s.pendingExplored)},
		},
		Timestamp: ts,
	}

	event := events.NewClaudeMessageEventFull(msg)
	event.SetAgentType("codex")
	s.hub.Publish(event)

	s.pendingExplored = nil
	s.pendingExploredTS = ""
}

func shouldFlushExploredBeforeItem(item *ConversationItem) bool {
	if item == nil {
		return false
	}
	if item.Role != "assistant" {
		return true
	}
	for _, block := range item.Content {
		if block.Type != "tool_use" && block.Type != "tool_result" {
			return true
		}
	}
	return false
}

func collectToolSummaries(blocks []events.ClaudeMessageContent) []string {
	var out []string
	for _, block := range blocks {
		if block.Type != "tool_use" {
			continue
		}
		if summary := summarizeToolForExplored(block.ToolName, block.ToolInput); strings.TrimSpace(summary) != "" {
			out = append(out, summary)
		}
	}
	return out
}

func summarizeToolForExplored(toolName string, params map[string]interface{}) string {
	switch toolName {
	case "exec_command":
		if cmd, ok := params["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			return summarizeExecCommand(cmd)
		}
		if cmd, ok := params["cmd"].(string); ok && strings.TrimSpace(cmd) != "" {
			return summarizeExecCommand(cmd)
		}
	case "apply_patch":
		// apply_patch already appears as its own tool row with patch preview.
		// Avoid duplicating it in synthetic "Explored" summaries.
		return ""
	case "view_image":
		// view_image already renders as a dedicated tool call row; avoid redundant "Explored" line.
		return ""
	}

	if toolName != "" {
		return toolName
	}
	return "tool"
}

func summarizeExecCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "Run command"
	}

	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "Run command"
	}

	switch fields[0] {
	case "find":
		root := "."
		if len(fields) > 1 && !strings.HasPrefix(fields[1], "-") {
			root = trimToolToken(fields[1])
		}

		names := extractToolFlagValues(fields, "-name")
		if len(names) > 0 {
			return fmt.Sprintf("Search %s in %s", names[0], root)
		}

		paths := extractToolFlagValues(fields, "-path")
		if len(paths) > 0 {
			return fmt.Sprintf("Search %s in %s", paths[0], root)
		}

		if strings.Contains(cmd, "-type") {
			return fmt.Sprintf("List %s", root)
		}
		return fmt.Sprintf("Search in %s", root)

	case "ls":
		target := "."
		for _, tok := range fields[1:] {
			if strings.HasPrefix(tok, "-") {
				continue
			}
			target = trimToolToken(tok)
			break
		}
		return fmt.Sprintf("List %s", target)

	case "cat", "sed", "nl", "tail", "head":
		if summary, ok := summarizeReadCommand(fields); ok {
			return summary
		}
		return "Read file"

	case "rg", "grep":
		return summarizeSearchCommand(cmd, fields)
	}

	// Handle piped read forms like: nl -ba file | sed -n '1,40p'
	if strings.Contains(cmd, "| sed ") || strings.Contains(cmd, "| nl ") || strings.Contains(cmd, "| cat ") {
		if summary, ok := summarizeReadCommand(fields); ok {
			return summary
		}
	}

	// Non-exploration commands (e.g., python/node/go execution) should stay in
	// their own "Ran ..." tool rows, not in synthetic "Explored" summaries.
	return ""
}

func summarizeSearchCommand(cmd string, fields []string) string {
	target := "."
	if t := extractSearchTarget(fields); t != "" {
		target = t
	}

	pattern := extractSearchPattern(cmd)
	if pattern == "" {
		return fmt.Sprintf("Search in %s", target)
	}
	return fmt.Sprintf("Search %s in %s", truncateSearchPattern(pattern), target)
}

func summarizeReadCommand(fields []string) (string, bool) {
	targets := extractReadTargets(fields)
	if len(targets) == 0 {
		return "", false
	}
	return "Read " + strings.Join(targets, ", "), true
}

func extractReadTargets(fields []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 2)

	for i, raw := range fields {
		if i == 0 {
			continue
		}

		tok := trimToolToken(raw)
		if tok == "" {
			continue
		}
		switch tok {
		case "|", "||", "&&", ";":
			return out
		}

		if strings.HasPrefix(tok, "-") || isLikelySedScriptToken(tok) {
			continue
		}
		if !isLikelyPathToken(tok) {
			continue
		}

		name := compactReadTarget(tok)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		out = append(out, name)
	}

	return out
}

func extractSearchTarget(fields []string) string {
	nonFlagArgs := 0
	for i, raw := range fields {
		if i == 0 {
			continue
		}

		tok := trimToolToken(raw)
		if tok == "" {
			continue
		}
		switch tok {
		case "|", "||", "&&", ";":
			return ""
		}

		if strings.HasPrefix(tok, "-") {
			continue
		}

		// For rg/grep, the first non-flag arg is usually the pattern; the next
		// non-flag arg is the search target path.
		nonFlagArgs++
		if nonFlagArgs == 1 {
			continue
		}
		return tok
	}
	return ""
}

func extractSearchPattern(cmd string) string {
	quoted := firstQuotedSegment(cmd)
	if quoted == "" {
		return ""
	}
	quoted = trimToolToken(quoted)
	if quoted == "" || isLikelyPathToken(quoted) {
		return ""
	}
	return quoted
}

func firstQuotedSegment(s string) string {
	first := -1
	var quote byte
	for i := 0; i < len(s); i++ {
		if s[i] == '"' || s[i] == '\'' {
			first = i
			quote = s[i]
			break
		}
	}
	if first < 0 {
		return ""
	}

	for i := first + 1; i < len(s); i++ {
		if s[i] == quote {
			return s[first+1 : i]
		}
	}
	return ""
}

func isLikelyPathToken(tok string) bool {
	if tok == "" {
		return false
	}
	if strings.ContainsAny(tok, "*?[]|(){}") {
		return false
	}
	if strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "./") || strings.HasPrefix(tok, "../") || strings.HasPrefix(tok, "~/") {
		return true
	}
	if strings.Contains(tok, "/") {
		return true
	}
	// Plain file names like hub.go, app_test.go.
	if strings.Contains(tok, ".") && !strings.Contains(tok, ":") {
		return true
	}
	return false
}

func isLikelySedScriptToken(tok string) bool {
	s := trimToolToken(tok)
	if strings.HasSuffix(s, "p") {
		body := strings.TrimSuffix(s, "p")
		if body != "" && strings.Trim(body, "0123456789,") == "" {
			return true
		}
	}
	return false
}

func compactReadTarget(tok string) string {
	tok = trimToolToken(tok)
	if tok == "" {
		return ""
	}
	if idx := strings.Index(tok, "/.cdev/"); idx >= 0 {
		return tok[idx+1:]
	}
	tok = strings.TrimSuffix(tok, "/")
	if slash := strings.LastIndex(tok, "/"); slash >= 0 && slash+1 < len(tok) {
		return tok[slash+1:]
	}
	return tok
}

func truncateSearchPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return pattern
	}
	if len(pattern) > 72 {
		return pattern[:69] + "..."
	}
	return pattern
}

func extractToolFlagValues(fields []string, flag string) []string {
	var out []string
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] != flag {
			continue
		}
		value := trimToolToken(fields[i+1])
		if value == "" || strings.HasPrefix(value, "-") {
			continue
		}
		out = append(out, value)
	}
	return out
}

func trimToolToken(token string) string {
	token = strings.TrimSpace(token)
	token = strings.Trim(token, "\"'")
	token = strings.TrimSuffix(token, ";")
	return token
}

func summarizeApplyPatchTool(input string) string {
	for _, prefix := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
		if idx := strings.Index(input, prefix); idx >= 0 {
			rest := input[idx+len(prefix):]
			line := rest
			if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
				line = rest[:nl]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			action := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(prefix, "*** "), ": "))
			return action + " " + line
		}
	}
	return ""
}

func formatExploredText(entries []string) string {
	if len(entries) == 0 {
		return ""
	}

	lines := make([]string, 0, len(entries)+1)
	lines = append(lines, "**Explored**")
	for i, entry := range entries {
		prefix := "├ "
		if i == len(entries)-1 {
			prefix = "└ "
		}
		lines = append(lines, prefix+entry)
	}
	return strings.Join(lines, "\n")
}

func parseTimestampForDedup(ts string) (time.Time, bool) {
	if ts == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
		return parsed, true
	}
	return time.Time{}, false
}

// Close stops the streamer and releases resources.
func (s *SessionStreamer) Close() {
	s.UnwatchSession()
}
