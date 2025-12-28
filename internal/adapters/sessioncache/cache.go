// Package sessioncache provides a SQLite-backed cache for Claude sessions.
// It watches the sessions directory for changes and keeps the cache in sync.
package sessioncache

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

// SessionInfo represents cached session information.
type SessionInfo struct {
	SessionID    string    `json:"session_id"`
	Summary      string    `json:"summary"`
	MessageCount int       `json:"message_count"`
	LastUpdated  time.Time `json:"last_updated"`
	Branch       string    `json:"branch,omitempty"`
}

// Cache manages the SQLite session cache with file watching.
type Cache struct {
	db          *sql.DB
	dbPath      string
	sessionsDir string
	repoPath    string
	watcher     *fsnotify.Watcher

	mu      sync.RWMutex
	running bool
	done    chan struct{}

	// Debounce file events
	pendingSync map[string]time.Time
	syncMu      sync.Mutex
}

// uuidPattern matches UUID format session IDs.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// schemaVersion is incremented when the counting logic or schema changes.
// This forces a complete rebuild of the cache to ensure correct data.
const schemaVersion = 5 // v5: Count assistant with text OR thinking (not tool_use-only)

// New creates a new session cache.
func New(repoPath string) (*Cache, error) {
	// Get sessions directory
	sessionsDir := getSessionsDir(repoPath)

	// Create cache directory if needed
	cacheDir := filepath.Join(os.TempDir(), "cdev", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	// Database path uses encoded repo path
	encodedPath := strings.ReplaceAll(filepath.Clean(repoPath), "/", "-")
	dbPath := filepath.Join(cacheDir, encodedPath+".db")

	// Open database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Create schema
	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	cache := &Cache{
		db:          db,
		dbPath:      dbPath,
		sessionsDir: sessionsDir,
		repoPath:    repoPath,
		pendingSync: make(map[string]time.Time),
	}

	return cache, nil
}

// createSchema creates the database schema, handling version migrations.
func createSchema(db *sql.DB) error {
	// Create metadata table for version tracking
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, value TEXT)`)
	if err != nil {
		return err
	}

	// Check current schema version
	var currentVersion int
	row := db.QueryRow("SELECT value FROM metadata WHERE key = 'schema_version'")
	if err := row.Scan(&currentVersion); err != nil {
		// No version found, this is a new database
		currentVersion = 0
	}

	// If schema version is outdated, drop and recreate sessions table
	if currentVersion < schemaVersion {
		log.Info().
			Int("old_version", currentVersion).
			Int("new_version", schemaVersion).
			Msg("schema version changed, rebuilding session cache")

		// Drop existing sessions table to force rebuild
		_, _ = db.Exec("DROP TABLE IF EXISTS sessions")
	}

	// Create sessions table
	schema := `
		CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			summary TEXT,
			message_count INTEGER,
			last_updated TEXT,
			branch TEXT,
			file_path TEXT,
			file_mtime INTEGER,
			indexed_at TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_sessions_last_updated ON sessions(last_updated DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	// Update schema version
	_, err = db.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', ?)", schemaVersion)
	return err
}

// Start begins the cache service with initial scan and file watching.
func (c *Cache) Start() error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = true
	c.done = make(chan struct{})
	c.mu.Unlock()

	log.Info().Str("sessions_dir", c.sessionsDir).Msg("starting session cache")

	// Initial full scan
	if err := c.fullSync(); err != nil {
		log.Warn().Err(err).Msg("initial session scan failed")
		// Continue anyway - watcher will pick up changes
	}

	// Log that cache is ready to query
	log.Info().Msg("session cache ready to query")

	// Start file watcher
	if err := c.startWatcher(); err != nil {
		log.Warn().Err(err).Msg("failed to start session watcher")
		// Continue without watcher - will rely on periodic sync
	}

	// Start background jobs
	go c.reconciliationLoop()
	go c.debounceLoop()

	return nil
}

// Stop stops the cache service.
func (c *Cache) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	close(c.done)
	c.mu.Unlock()

	if c.watcher != nil {
		_ = c.watcher.Close()
	}

	return c.db.Close()
}

// ListSessions returns all cached sessions, sorted by last_updated desc.
func (c *Cache) ListSessions() ([]SessionInfo, error) {
	start := time.Now()

	rows, err := c.db.Query(`
		SELECT session_id, summary, message_count, last_updated, branch
		FROM sessions
		ORDER BY last_updated DESC
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var sessions []SessionInfo
	for rows.Next() {
		var s SessionInfo
		var lastUpdated string
		if err := rows.Scan(&s.SessionID, &s.Summary, &s.MessageCount, &lastUpdated, &s.Branch); err != nil {
			continue
		}
		s.LastUpdated, _ = time.Parse(time.RFC3339, lastUpdated)
		sessions = append(sessions, s)
	}

	log.Debug().
		Int("count", len(sessions)).
		Dur("elapsed_ms", time.Since(start)).
		Msg("listed sessions from cache")

	return sessions, nil
}

// fullSync performs a full scan of the sessions directory.
func (c *Cache) fullSync() error {
	start := time.Now()

	// Check if directory exists
	if _, err := os.Stat(c.sessionsDir); os.IsNotExist(err) {
		log.Debug().Msg("sessions directory does not exist")
		return nil
	}

	// Read directory
	entries, err := os.ReadDir(c.sessionsDir)
	if err != nil {
		return err
	}

	// Track existing session IDs for cleanup
	existingIDs := make(map[string]bool)

	// Begin transaction
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO sessions
		(session_id, summary, message_count, last_updated, branch, file_path, file_mtime, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	fileCount := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !uuidPattern.MatchString(entry.Name()) {
			continue
		}

		fileCount++
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		filePath := filepath.Join(c.sessionsDir, entry.Name())

		// Get file modification time
		info, err := entry.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime().Unix()

		// Check if we need to re-parse (file changed)
		var cachedMtime int64
		_ = c.db.QueryRow("SELECT file_mtime FROM sessions WHERE session_id = ?", sessionID).Scan(&cachedMtime)
		if cachedMtime == mtime {
			existingIDs[sessionID] = true
			continue // File unchanged, skip parsing
		}

		// Parse the session file
		session, err := parseSessionFile(filePath, sessionID)
		if err != nil {
			log.Debug().Err(err).Str("file", entry.Name()).Msg("failed to parse session")
			continue
		}

		if session.MessageCount == 0 {
			continue
		}

		existingIDs[sessionID] = true

		_, err = stmt.Exec(
			session.SessionID,
			session.Summary,
			session.MessageCount,
			session.LastUpdated.Format(time.RFC3339),
			session.Branch,
			filePath,
			mtime,
			time.Now().Format(time.RFC3339),
		)
		if err != nil {
			log.Debug().Err(err).Str("session_id", sessionID).Msg("failed to cache session")
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Clean up deleted sessions
	rows, _ := c.db.Query("SELECT session_id FROM sessions")
	if rows != nil {
		var toDelete []string
		for rows.Next() {
			var id string
			_ = rows.Scan(&id)
			if !existingIDs[id] {
				toDelete = append(toDelete, id)
			}
		}
		_ = rows.Close()

		for _, id := range toDelete {
			_, _ = c.db.Exec("DELETE FROM sessions WHERE session_id = ?", id)
			log.Debug().Str("session_id", id).Msg("removed deleted session from cache")
		}
	}

	log.Info().
		Int("files_scanned", fileCount).
		Int("sessions_cached", len(existingIDs)).
		Dur("elapsed_ms", time.Since(start)).
		Msg("full session sync complete")

	return nil
}

// startWatcher sets up the file watcher.
func (c *Cache) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	c.watcher = watcher

	// Ensure sessions directory exists before watching
	if _, err := os.Stat(c.sessionsDir); os.IsNotExist(err) {
		// Create directory so we can watch it
		_ = os.MkdirAll(c.sessionsDir, 0755)
	}

	if err := watcher.Add(c.sessionsDir); err != nil {
		return err
	}

	go c.watchLoop()

	log.Debug().Str("dir", c.sessionsDir).Msg("watching sessions directory")
	return nil
}

// watchLoop handles file system events.
func (c *Cache) watchLoop() {
	for {
		select {
		case <-c.done:
			return

		case event, ok := <-c.watcher.Events:
			if !ok {
				return
			}

			// Only care about .jsonl files matching UUID pattern
			filename := filepath.Base(event.Name)
			if !uuidPattern.MatchString(filename) {
				continue
			}

			// Queue for debounced sync
			c.syncMu.Lock()
			c.pendingSync[event.Name] = time.Now()
			c.syncMu.Unlock()

			log.Debug().
				Str("file", filename).
				Str("op", event.Op.String()).
				Msg("session file changed")

		case err, ok := <-c.watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("session watcher error")
		}
	}
}

// debounceLoop processes pending file syncs with debouncing.
func (c *Cache) debounceLoop() {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return

		case <-ticker.C:
			c.syncMu.Lock()
			now := time.Now()
			var toSync []string

			for path, queuedAt := range c.pendingSync {
				// Wait 200ms after last change before syncing
				if now.Sub(queuedAt) >= 200*time.Millisecond {
					toSync = append(toSync, path)
					delete(c.pendingSync, path)
				}
			}
			c.syncMu.Unlock()

			for _, path := range toSync {
				c.syncFile(path)
			}
		}
	}
}

// syncFile syncs a single session file to the cache.
func (c *Cache) syncFile(filePath string) {
	filename := filepath.Base(filePath)
	sessionID := strings.TrimSuffix(filename, ".jsonl")

	// Check if file exists
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// File deleted, remove from cache
		_, _ = c.db.Exec("DELETE FROM sessions WHERE session_id = ?", sessionID)
		log.Debug().Str("session_id", sessionID).Msg("removed deleted session")
		return
	}

	if err != nil {
		return
	}

	// Parse and update
	session, err := parseSessionFile(filePath, sessionID)
	if err != nil {
		log.Debug().Err(err).Str("file", filename).Msg("failed to parse session")
		return
	}

	if session.MessageCount == 0 {
		return
	}

	_, err = c.db.Exec(`
		INSERT OR REPLACE INTO sessions
		(session_id, summary, message_count, last_updated, branch, file_path, file_mtime, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.SessionID,
		session.Summary,
		session.MessageCount,
		session.LastUpdated.Format(time.RFC3339),
		session.Branch,
		filePath,
		info.ModTime().Unix(),
		time.Now().Format(time.RFC3339),
	)

	if err != nil {
		log.Debug().Err(err).Str("session_id", sessionID).Msg("failed to update cache")
	} else {
		log.Debug().Str("session_id", sessionID).Msg("synced session to cache")
	}
}

// reconciliationLoop runs periodic full syncs to catch missed changes.
func (c *Cache) reconciliationLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return

		case <-ticker.C:
			log.Debug().Msg("running session reconciliation")
			if err := c.fullSync(); err != nil {
				log.Warn().Err(err).Msg("reconciliation sync failed")
			}
		}
	}
}

// getSessionsDir returns the Claude sessions directory for a repo path.
func getSessionsDir(repoPath string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}

	repoPath = filepath.Clean(repoPath)
	encodedPath := strings.ReplaceAll(repoPath, "/", "-")

	return filepath.Join(homeDir, ".claude", "projects", encodedPath)
}

// sessionMessage represents a message in a session file.
type sessionMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionId"`
	GitBranch string `json:"gitBranch"`
	Message   struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Timestamp string `json:"timestamp"`
}

// extractContent extracts text content from the message.
func (m *sessionMessage) extractContent() string {
	if m.Message.Content == nil {
		return ""
	}

	var contentStr string
	if err := json.Unmarshal(m.Message.Content, &contentStr); err == nil {
		return contentStr
	}

	var contentBlocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(m.Message.Content, &contentBlocks); err == nil {
		var texts []string
		for _, block := range contentBlocks {
			if block.Type == "text" && block.Text != "" {
				texts = append(texts, block.Text)
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}

// parseSessionFile reads a session file and extracts info.
// Message counting matches Claude CLI logic:
// - User messages: only count if content is text (not tool_result or system messages)
// - Assistant messages: count if contains "text" OR "thinking" content (not tool_use-only)
func parseSessionFile(filePath string, sessionID string) (SessionInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return SessionInfo{}, err
	}
	defer func() { _ = file.Close() }()

	info := SessionInfo{
		SessionID: sessionID,
	}

	stat, err := file.Stat()
	if err == nil {
		info.LastUpdated = stat.ModTime()
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max to handle extended thinking

	messageCount := 0
	foundSummary := false

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg sessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		// Count messages matching Claude CLI logic
		if msg.Type == "user" && msg.isUserTextMessage() {
			messageCount++
		} else if msg.Type == "assistant" && msg.hasTextOrThinkingContent() {
			// Count assistant messages with "text" or "thinking" content (not tool_use-only)
			messageCount++
		}

		content := msg.extractContent()
		if !foundSummary && msg.Type == "user" && content != "" {
			info.Summary = truncateSummary(content, 100)
			info.Branch = msg.GitBranch
			foundSummary = true
		}
	}

	info.MessageCount = messageCount

	return info, scanner.Err()
}

// isUserTextMessage returns true if the user message contains actual user text.
// Excludes:
// - tool_result messages (API responses)
// - System messages starting with "Caveat:", "<command-name>", "<local-command-stdout>"
func (m *sessionMessage) isUserTextMessage() bool {
	if m.Message.Content == nil {
		return false
	}

	// If content is a string, check if it's a real user message (not system injection)
	var contentStr string
	if err := json.Unmarshal(m.Message.Content, &contentStr); err == nil {
		if contentStr == "" {
			return false
		}
		// Exclude system messages
		if strings.HasPrefix(contentStr, "Caveat:") ||
			strings.HasPrefix(contentStr, "<command-name>") ||
			strings.HasPrefix(contentStr, "<local-command-stdout>") ||
			strings.HasPrefix(contentStr, "<local-command-stderr>") {
			return false
		}
		return true
	}

	// If content is an array, check if it has text type (not just tool_result)
	var contentBlocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(m.Message.Content, &contentBlocks); err == nil {
		for _, block := range contentBlocks {
			if block.Type == "text" {
				return true
			}
		}
	}

	return false
}

// hasTextOrThinkingContent returns true if the assistant message contains "text" or "thinking" content.
// This excludes messages that only have "tool_use" blocks.
func (m *sessionMessage) hasTextOrThinkingContent() bool {
	if m.Message.Content == nil {
		return false
	}

	var contentBlocks []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(m.Message.Content, &contentBlocks); err == nil {
		for _, block := range contentBlocks {
			if block.Type == "text" || block.Type == "thinking" {
				return true
			}
		}
	}

	return false
}

// truncateSummary truncates a string for display.
func truncateSummary(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// GetSessionsDir exports the sessions directory path.
func (c *Cache) GetSessionsDir() string {
	return c.sessionsDir
}

// ForceSync triggers an immediate full sync.
func (c *Cache) ForceSync() error {
	return c.fullSync()
}

// Stats returns cache statistics.
func (c *Cache) Stats() map[string]interface{} {
	var count int
	_ = c.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)

	return map[string]interface{}{
		"sessions_cached": count,
		"db_path":         c.dbPath,
		"sessions_dir":    c.sessionsDir,
	}
}

// ListSessionsPaginated returns sessions with pagination support.
func (c *Cache) ListSessionsPaginated(limit, offset int) ([]SessionInfo, int, error) {
	start := time.Now()

	// Get total count
	var total int
	if err := c.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&total); err != nil {
		return nil, 0, err
	}

	// Build query with pagination
	query := "SELECT session_id, summary, message_count, last_updated, branch FROM sessions ORDER BY last_updated DESC"
	var args []interface{}

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var sessions []SessionInfo
	for rows.Next() {
		var s SessionInfo
		var lastUpdated string
		if err := rows.Scan(&s.SessionID, &s.Summary, &s.MessageCount, &lastUpdated, &s.Branch); err != nil {
			continue
		}
		s.LastUpdated, _ = time.Parse(time.RFC3339, lastUpdated)
		sessions = append(sessions, s)
	}

	log.Debug().
		Int("count", len(sessions)).
		Int("total", total).
		Int("limit", limit).
		Int("offset", offset).
		Dur("elapsed_ms", time.Since(start)).
		Msg("listed sessions with pagination")

	return sessions, total, nil
}

// Ensure sessions are sorted for the original ListSessions method
func init() {
	_ = sort.Slice
}

// DeleteSession deletes a specific session from the cache and disk.
func (c *Cache) DeleteSession(sessionID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get the file path from database
	var filePath string
	err := c.db.QueryRow("SELECT file_path FROM sessions WHERE session_id = ?", sessionID).Scan(&filePath)
	if err == sql.ErrNoRows {
		return nil // Session not found, nothing to delete
	}
	if err != nil {
		return err
	}

	// Delete from database
	_, err = c.db.Exec("DELETE FROM sessions WHERE session_id = ?", sessionID)
	if err != nil {
		return err
	}

	// Delete the file if it exists
	if filePath != "" {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", filePath).Msg("failed to delete session file")
		}
	}

	log.Info().Str("session_id", sessionID).Msg("deleted session")
	return nil
}

// DeleteAllSessions deletes all sessions from the cache and disk.
func (c *Cache) DeleteAllSessions() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get all file paths
	rows, err := c.db.Query("SELECT session_id, file_path FROM sessions")
	if err != nil {
		return 0, err
	}

	var filePaths []string
	for rows.Next() {
		var sessionID, filePath string
		if err := rows.Scan(&sessionID, &filePath); err == nil && filePath != "" {
			filePaths = append(filePaths, filePath)
		}
	}
	_ = rows.Close()

	// Delete all from database
	result, err := c.db.Exec("DELETE FROM sessions")
	if err != nil {
		return 0, err
	}
	deleted, _ := result.RowsAffected()

	// Delete files
	for _, filePath := range filePaths {
		if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
			log.Warn().Err(err).Str("path", filePath).Msg("failed to delete session file")
		}
	}

	log.Info().Int64("count", deleted).Msg("deleted all sessions")
	return int(deleted), nil
}
