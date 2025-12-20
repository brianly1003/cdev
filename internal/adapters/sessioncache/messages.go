// Package sessioncache provides message caching with SQLite for fast retrieval.
package sessioncache

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// MessageCache provides SQLite-backed message caching with pagination.
type MessageCache struct {
	db          *sql.DB
	sessionsDir string
	mu          sync.RWMutex
}

// CachedMessage represents a message stored in the cache.
type CachedMessage struct {
	ID                  int64           `json:"id"`
	SessionID           string          `json:"session_id"`
	Type                string          `json:"type"`
	UUID                string          `json:"uuid,omitempty"`
	Timestamp           string          `json:"timestamp,omitempty"`
	GitBranch           string          `json:"git_branch,omitempty"`
	Message             json.RawMessage `json:"message"`
	IsContextCompaction bool            `json:"is_context_compaction,omitempty"`
	LineNum             int             `json:"-"` // Line number in source file
}

// MessagesPage represents a paginated response.
type MessagesPage struct {
	Messages   []CachedMessage `json:"messages"`
	Total      int             `json:"total"`
	Limit      int             `json:"limit"`
	Offset     int             `json:"offset"`
	HasMore    bool            `json:"has_more"`
	CacheHit   bool            `json:"cache_hit"`
	QueryTimeMS float64        `json:"query_time_ms"`
}

// messageSchemaVersion tracks message cache schema changes.
// Bump this when schema changes to force re-indexing.
const messageSchemaVersion = 2

// contextCompactionPrefix is the prefix for context compaction messages.
const contextCompactionPrefix = "This session is being continued from a previous conversation"

// NewMessageCache creates a new message cache.
func NewMessageCache(sessionsDir string) (*MessageCache, error) {
	// Create cache directory
	cacheDir := filepath.Join(os.TempDir(), "cdev", "message-cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, err
	}

	// Database path
	encodedPath := strings.ReplaceAll(filepath.Clean(sessionsDir), "/", "-")
	dbPath := filepath.Join(cacheDir, encodedPath+"-messages.db")

	// Open database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Optimize for read-heavy workload
	db.Exec("PRAGMA synchronous=NORMAL")
	db.Exec("PRAGMA cache_size=-64000") // 64MB cache
	db.Exec("PRAGMA temp_store=MEMORY")

	// Create schema
	if err := createMessageSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	cache := &MessageCache{
		db:          db,
		sessionsDir: sessionsDir,
	}

	log.Info().Str("db_path", dbPath).Msg("message cache initialized")
	return cache, nil
}

// createMessageSchema creates the database schema, dropping old tables if version changed.
func createMessageSchema(db *sql.DB) error {
	// Check existing schema version
	var existingVersion int
	_ = db.QueryRow("SELECT value FROM message_metadata WHERE key = 'schema_version'").Scan(&existingVersion)
	if existingVersion != 0 && existingVersion != messageSchemaVersion {
		// Schema changed - drop and recreate
		log.Info().Int("old_version", existingVersion).Int("new_version", messageSchemaVersion).Msg("message cache schema changed, recreating")
		db.Exec("DROP TABLE IF EXISTS messages")
		db.Exec("DROP TABLE IF EXISTS session_files")
		db.Exec("DROP TABLE IF EXISTS message_metadata")
	}

	schema := `
		CREATE TABLE IF NOT EXISTS message_metadata (
			key TEXT PRIMARY KEY,
			value TEXT
		);

		CREATE TABLE IF NOT EXISTS session_files (
			session_id TEXT PRIMARY KEY,
			file_mtime INTEGER,
			message_count INTEGER,
			indexed_at TEXT
		);

		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			line_num INTEGER NOT NULL,
			type TEXT NOT NULL,
			uuid TEXT,
			timestamp TEXT,
			git_branch TEXT,
			message_json TEXT NOT NULL,
			is_context_compaction INTEGER DEFAULT 0,
			UNIQUE(session_id, line_num)
		);

		CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id);
		CREATE INDEX IF NOT EXISTS idx_messages_session_line ON messages(session_id, line_num);
		CREATE INDEX IF NOT EXISTS idx_messages_type ON messages(session_id, type);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Set schema version
	_, err = db.Exec("INSERT OR REPLACE INTO message_metadata (key, value) VALUES ('schema_version', ?)", messageSchemaVersion)
	return err
}

// GetMessages returns paginated messages for a session.
// Parameters:
//   - sessionID: the session to fetch
//   - limit: max messages to return (0 = all, default 50)
//   - offset: starting position
//   - order: "asc" (oldest first) or "desc" (newest first, default)
func (mc *MessageCache) GetMessages(sessionID string, limit, offset int, order string) (*MessagesPage, error) {
	start := time.Now()

	// Default values
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500 // Cap at 500 to prevent huge responses
	}
	if order != "asc" {
		order = "desc"
	}

	// Ensure session is cached
	cacheHit, err := mc.ensureSessionCached(sessionID)
	if err != nil {
		return nil, err
	}

	// Get total count
	var total int
	err = mc.db.QueryRow("SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&total)
	if err != nil {
		return nil, err
	}

	if total == 0 {
		return &MessagesPage{
			Messages:    []CachedMessage{},
			Total:       0,
			Limit:       limit,
			Offset:      offset,
			HasMore:     false,
			CacheHit:    cacheHit,
			QueryTimeMS: float64(time.Since(start).Microseconds()) / 1000,
		}, nil
	}

	// Build query
	orderDir := "ASC"
	if order == "desc" {
		orderDir = "DESC"
	}

	query := fmt.Sprintf(`
		SELECT id, session_id, line_num, type, uuid, timestamp, git_branch, message_json, is_context_compaction
		FROM messages
		WHERE session_id = ?
		ORDER BY line_num %s
		LIMIT ? OFFSET ?
	`, orderDir)

	rows, err := mc.db.Query(query, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []CachedMessage
	for rows.Next() {
		var m CachedMessage
		var messageJSON string
		var uuid, timestamp, gitBranch sql.NullString
		var isContextCompaction int

		err := rows.Scan(&m.ID, &m.SessionID, &m.LineNum, &m.Type, &uuid, &timestamp, &gitBranch, &messageJSON, &isContextCompaction)
		if err != nil {
			continue
		}

		m.UUID = uuid.String
		m.Timestamp = timestamp.String
		m.GitBranch = gitBranch.String
		m.Message = json.RawMessage(messageJSON)
		m.IsContextCompaction = isContextCompaction == 1

		messages = append(messages, m)
	}

	return &MessagesPage{
		Messages:    messages,
		Total:       total,
		Limit:       limit,
		Offset:      offset,
		HasMore:     offset+len(messages) < total,
		CacheHit:    cacheHit,
		QueryTimeMS: float64(time.Since(start).Microseconds()) / 1000,
	}, nil
}

// ensureSessionCached checks if session is cached and up-to-date, re-indexes if needed.
// Returns true if cache was hit (no re-indexing needed).
func (mc *MessageCache) ensureSessionCached(sessionID string) (bool, error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	filePath := filepath.Join(mc.sessionsDir, sessionID+".jsonl")

	// Get file info
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		return false, fmt.Errorf("session not found: %s", sessionID)
	}
	fileMtime := fileInfo.ModTime().Unix()

	// Check if already cached and up-to-date
	var cachedMtime int64
	err = mc.db.QueryRow("SELECT file_mtime FROM session_files WHERE session_id = ?", sessionID).Scan(&cachedMtime)
	if err == nil && cachedMtime == fileMtime {
		return true, nil // Cache hit
	}

	// Need to re-index
	log.Debug().Str("session_id", sessionID).Msg("indexing session messages")

	if err := mc.indexSession(sessionID, filePath, fileMtime); err != nil {
		return false, err
	}

	return false, nil // Cache miss - had to re-index
}

// indexSession parses a session file and stores messages in SQLite.
func (mc *MessageCache) indexSession(sessionID, filePath string, mtime int64) error {
	start := time.Now()

	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Begin transaction
	tx, err := mc.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete existing messages for this session
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return err
	}

	// Prepare insert statement
	stmt, err := tx.Prepare(`
		INSERT INTO messages (session_id, line_num, type, uuid, timestamp, git_branch, message_json, is_context_compaction)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 2*1024*1024) // 2MB max line

	lineNum := 0
	messageCount := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Parse just enough to get type and metadata
		var raw struct {
			Type      string          `json:"type"`
			Subtype   string          `json:"subtype,omitempty"`   // "compact_boundary" for context compaction marker
			UserType  string          `json:"userType,omitempty"`  // "external" for auto-generated messages
			UUID      string          `json:"uuid,omitempty"`
			SessionID string          `json:"sessionId,omitempty"`
			Timestamp string          `json:"timestamp,omitempty"`
			GitBranch string          `json:"gitBranch,omitempty"`
			Content   string          `json:"content,omitempty"`   // For system messages
			Message   json.RawMessage `json:"message"`
		}

		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}

		// Detect context compaction
		isContextCompaction := false

		// Case 1: System message with compact_boundary subtype
		if raw.Type == "system" && raw.Subtype == "compact_boundary" {
			isContextCompaction = true
		}

		// Case 2: User message with userType "external" and continuation prefix
		if raw.Type == "user" && raw.UserType == "external" {
			// Check if message content starts with continuation prefix
			var msgContent struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(raw.Message, &msgContent); err == nil {
				if strings.HasPrefix(msgContent.Content, contextCompactionPrefix) {
					isContextCompaction = true
				}
			}
		}

		// Only cache user, assistant, and system (compact_boundary) messages
		if raw.Type != "user" && raw.Type != "assistant" && !(raw.Type == "system" && raw.Subtype == "compact_boundary") {
			continue
		}

		// Store the message portion as JSON string
		messageJSON := "{}"
		if raw.Message != nil {
			messageJSON = string(raw.Message)
		}

		_, err = stmt.Exec(
			sessionID,
			lineNum,
			raw.Type,
			nullString(raw.UUID),
			nullString(raw.Timestamp),
			nullString(raw.GitBranch),
			messageJSON,
			isContextCompaction,
		)
		if err != nil {
			log.Debug().Err(err).Int("line", lineNum).Msg("failed to insert message")
			continue
		}
		messageCount++
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Update session metadata
	_, err = tx.Exec(`
		INSERT OR REPLACE INTO session_files (session_id, file_mtime, message_count, indexed_at)
		VALUES (?, ?, ?, ?)
	`, sessionID, mtime, messageCount, time.Now().Format(time.RFC3339))
	if err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	log.Info().
		Str("session_id", sessionID).
		Int("messages", messageCount).
		Int("lines", lineNum).
		Dur("elapsed", time.Since(start)).
		Msg("indexed session messages")

	return nil
}

// InvalidateSession removes a session from the cache.
func (mc *MessageCache) InvalidateSession(sessionID string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	_, err := mc.db.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return err
	}
	_, err = mc.db.Exec("DELETE FROM session_files WHERE session_id = ?", sessionID)
	return err
}

// GetStats returns cache statistics.
func (mc *MessageCache) GetStats() map[string]interface{} {
	var sessionCount, messageCount int
	mc.db.QueryRow("SELECT COUNT(*) FROM session_files").Scan(&sessionCount)
	mc.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&messageCount)

	return map[string]interface{}{
		"sessions_cached": sessionCount,
		"messages_cached": messageCount,
	}
}

// Close closes the database connection.
func (mc *MessageCache) Close() error {
	return mc.db.Close()
}

// nullString converts empty string to sql.NullString.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
