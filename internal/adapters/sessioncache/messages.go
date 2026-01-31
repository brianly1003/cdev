// Package sessioncache provides message caching with SQLite for fast retrieval.
package sessioncache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/jsonl"

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
	IsMeta              bool            `json:"is_meta,omitempty"`
	LineNum             int             `json:"-"` // Line number in source file
	// ImageMetadata contains image info from toolUseResult.file (for Read tool on images)
	ImageMetadata *ImageMetadata `json:"image_metadata,omitempty"`
}

// ImageMetadata contains image information from Claude's toolUseResult.
type ImageMetadata struct {
	// FilePath is the path to the file that was read
	FilePath string `json:"file_path,omitempty"`
	// Type is the MIME type (e.g., "image/jpeg")
	Type string `json:"type"`
	// OriginalSize is the file size in bytes
	OriginalSize int64 `json:"original_size"`
	// Dimensions contains the image dimensions
	Dimensions *ImageDimensions `json:"dimensions,omitempty"`
}

// ImageDimensions contains width/height info for images.
type ImageDimensions struct {
	// OriginalWidth is the original image width in pixels
	OriginalWidth int `json:"original_width"`
	// OriginalHeight is the original image height in pixels
	OriginalHeight int `json:"original_height"`
	// DisplayWidth is the scaled width for display
	DisplayWidth int `json:"display_width"`
	// DisplayHeight is the scaled height for display
	DisplayHeight int `json:"display_height"`
}

// MessagesPage represents a paginated response.
type MessagesPage struct {
	Messages    []CachedMessage `json:"messages"`
	Total       int             `json:"total"`
	Limit       int             `json:"limit"`
	Offset      int             `json:"offset"`
	HasMore     bool            `json:"has_more"`
	CacheHit    bool            `json:"cache_hit"`
	QueryTimeMS float64         `json:"query_time_ms"`
}

// messageSchemaVersion tracks message cache schema changes.
// Bump this when schema changes to force re-indexing.
const messageSchemaVersion = 4 // Added image_metadata column

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
		_ = db.Close()
		return nil, err
	}

	// Optimize for read-heavy workload
	_, _ = db.Exec("PRAGMA synchronous=NORMAL")
	_, _ = db.Exec("PRAGMA cache_size=-64000") // 64MB cache
	_, _ = db.Exec("PRAGMA temp_store=MEMORY")

	// Create schema
	if err := createMessageSchema(db); err != nil {
		_ = db.Close()
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
		_, _ = db.Exec("DROP TABLE IF EXISTS messages")
		_, _ = db.Exec("DROP TABLE IF EXISTS session_files")
		_, _ = db.Exec("DROP TABLE IF EXISTS message_metadata")
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
			is_meta INTEGER DEFAULT 0,
			image_metadata TEXT,
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
		SELECT id, session_id, line_num, type, uuid, timestamp, git_branch, message_json, is_context_compaction, is_meta, image_metadata
		FROM messages
		WHERE session_id = ?
		ORDER BY line_num %s
		LIMIT ? OFFSET ?
	`, orderDir)

	rows, err := mc.db.Query(query, sessionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var messages []CachedMessage
	for rows.Next() {
		var m CachedMessage
		var messageJSON string
		var uuid, timestamp, gitBranch, imageMetadataJSON sql.NullString
		var isContextCompaction, isMeta int

		err := rows.Scan(&m.ID, &m.SessionID, &m.LineNum, &m.Type, &uuid, &timestamp, &gitBranch, &messageJSON, &isContextCompaction, &isMeta, &imageMetadataJSON)
		if err != nil {
			continue
		}

		m.UUID = uuid.String
		m.Timestamp = timestamp.String
		m.GitBranch = gitBranch.String
		m.Message = json.RawMessage(messageJSON)
		m.IsContextCompaction = isContextCompaction == 1
		m.IsMeta = isMeta == 1

		// Parse image metadata if present
		if imageMetadataJSON.Valid && imageMetadataJSON.String != "" {
			var metadata ImageMetadata
			if err := json.Unmarshal([]byte(imageMetadataJSON.String), &metadata); err == nil {
				m.ImageMetadata = &metadata
			}
		}

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
	defer func() { _ = file.Close() }()

	// Begin transaction
	tx, err := mc.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Delete existing messages for this session
	_, err = tx.Exec("DELETE FROM messages WHERE session_id = ?", sessionID)
	if err != nil {
		return err
	}

	// Prepare insert statement
	stmt, err := tx.Prepare(`
		INSERT INTO messages (session_id, line_num, type, uuid, timestamp, git_branch, message_json, is_context_compaction, is_meta, image_metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	lineNum := 0
	messageCount := 0

	// Track tool_use calls to link file_path to tool_result
	toolUseFilePaths := make(map[string]string) // tool_use_id -> file_path

	reader := jsonl.NewReader(file, 0)

	for {
		line, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		lineNum++
		if line.TooLong {
			log.Warn().
				Str("session_id", sessionID).
				Int("line_num", lineNum).
				Int("bytes_read", line.BytesRead).
				Msg("session JSONL line exceeded max size; skipping")
			continue
		}
		if len(line.Data) == 0 {
			continue
		}

		// Parse just enough to get type and metadata
		var raw struct {
			Type      string          `json:"type"`
			Subtype   string          `json:"subtype,omitempty"`  // "compact_boundary" for context compaction marker
			UserType  string          `json:"userType,omitempty"` // "external" for auto-generated messages
			UUID      string          `json:"uuid,omitempty"`
			SessionID string          `json:"sessionId,omitempty"`
			Timestamp string          `json:"timestamp,omitempty"`
			GitBranch string          `json:"gitBranch,omitempty"`
			Content   string          `json:"content,omitempty"` // For system messages
			IsMeta    bool            `json:"isMeta,omitempty"`  // True for system-generated metadata messages
			Message   json.RawMessage `json:"message"`
			// toolUseResult contains image metadata for Read tool results
			ToolUseResult *struct {
				Type string `json:"type"`
				File *struct {
					Type         string `json:"type"`
					OriginalSize int64  `json:"originalSize"`
					Dimensions   *struct {
						OriginalWidth  int `json:"originalWidth"`
						OriginalHeight int `json:"originalHeight"`
						DisplayWidth   int `json:"displayWidth"`
						DisplayHeight  int `json:"displayHeight"`
					} `json:"dimensions,omitempty"`
				} `json:"file,omitempty"`
			} `json:"toolUseResult,omitempty"`
		}

		if err := json.Unmarshal(line.Data, &raw); err != nil {
			continue
		}

		// Extract file_path from tool_use calls (for Read tool)
		if raw.Type == "assistant" && raw.Message != nil {
			var msgContent struct {
				Content []struct {
					Type  string `json:"type"`
					ID    string `json:"id,omitempty"`
					Name  string `json:"name,omitempty"`
					Input struct {
						FilePath string `json:"file_path,omitempty"`
					} `json:"input,omitempty"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw.Message, &msgContent); err == nil {
				for _, block := range msgContent.Content {
					if block.Type == "tool_use" && block.Name == "Read" && block.Input.FilePath != "" {
						toolUseFilePaths[block.ID] = block.Input.FilePath
					}
				}
			}
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

		// Extract image metadata from toolUseResult
		var imageMetadataJSON sql.NullString
		if raw.ToolUseResult != nil && raw.ToolUseResult.Type == "image" && raw.ToolUseResult.File != nil {
			file := raw.ToolUseResult.File
			// Find the tool_use_id from the message content to get file_path
			var filePath string
			var msgContent struct {
				Content []struct {
					Type      string `json:"type"`
					ToolUseID string `json:"tool_use_id,omitempty"`
				} `json:"content"`
			}
			if err := json.Unmarshal(raw.Message, &msgContent); err == nil {
				for _, block := range msgContent.Content {
					if block.Type == "tool_result" && block.ToolUseID != "" {
						if fp, ok := toolUseFilePaths[block.ToolUseID]; ok {
							filePath = fp
							break
						}
					}
				}
			}

			metadata := ImageMetadata{
				FilePath:     filePath,
				Type:         file.Type,
				OriginalSize: file.OriginalSize,
			}
			if file.Dimensions != nil {
				metadata.Dimensions = &ImageDimensions{
					OriginalWidth:  file.Dimensions.OriginalWidth,
					OriginalHeight: file.Dimensions.OriginalHeight,
					DisplayWidth:   file.Dimensions.DisplayWidth,
					DisplayHeight:  file.Dimensions.DisplayHeight,
				}
			}
			if metadataBytes, err := json.Marshal(metadata); err == nil {
				imageMetadataJSON = sql.NullString{String: string(metadataBytes), Valid: true}
			}
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
			raw.IsMeta,
			imageMetadataJSON,
		)
		if err != nil {
			log.Debug().Err(err).Int("line", lineNum).Msg("failed to insert message")
			continue
		}
		messageCount++
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
	_ = mc.db.QueryRow("SELECT COUNT(*) FROM session_files").Scan(&sessionCount)
	_ = mc.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&messageCount)

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
