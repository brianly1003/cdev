package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

const schemaVersion = 2 // v2: Added file_id column for rename detection

// SQLiteIndexer implements the Indexer interface using SQLite with FTS5.
type SQLiteIndexer struct {
	db       *sql.DB
	dbPath   string
	scanner  *Scanner
	repoPath string
	skipDirs []string // Custom skip directories

	mu      sync.RWMutex
	running bool
	status  IndexStatus

	// Prepared statements
	stmtInsertFile   *sql.Stmt
	stmtUpdateFile   *sql.Stmt
	stmtDeleteFile   *sql.Stmt
	stmtGetFile      *sql.Stmt
	stmtSearchFuzzy  *sql.Stmt
	stmtFindByFileID *sql.Stmt // For rename detection

	// Stats cache - invalidated on file changes
	statsMu         sync.RWMutex
	statsCache      *RepositoryStats
	statsCacheTime  time.Time
	statsCacheTTL   time.Duration // Fallback TTL (default: 5 minutes)
	statsCacheValid bool

	// Background tasks
	done chan struct{}
}

// NewIndexer creates a new SQLite-based repository indexer.
// If skipDirs is nil or empty, the default SkipDirectories list is used.
func NewIndexer(repoPath string, skipDirs []string) (*SQLiteIndexer, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("invalid repo path: %w", err)
	}

	// Create cache directory
	cacheDir := filepath.Join(os.TempDir(), "cdev", "repo-index")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}

	// Create unique database path based on repo path
	encodedPath := strings.ReplaceAll(filepath.Clean(absPath), string(filepath.Separator), "-")
	if len(encodedPath) > 100 {
		encodedPath = encodedPath[len(encodedPath)-100:]
	}
	dbPath := filepath.Join(cacheDir, encodedPath+"-index.db")

	// Open database
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure SQLite for performance and concurrency
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-64000",    // 64MB cache
		"PRAGMA temp_store=MEMORY",
		"PRAGMA mmap_size=268435456",  // 256MB mmap
		"PRAGMA busy_timeout=5000",    // Wait up to 5 seconds if database is locked
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			log.Warn().Err(err).Str("pragma", pragma).Msg("failed to set pragma")
		}
	}

	// Create scanner with custom skip directories
	scanner, err := NewScanner(absPath, skipDirs)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create scanner: %w", err)
	}

	indexer := &SQLiteIndexer{
		db:            db,
		dbPath:        dbPath,
		scanner:       scanner,
		repoPath:      absPath,
		skipDirs:      skipDirs,
		done:          make(chan struct{}),
		statsCacheTTL: 5 * time.Minute, // Fallback TTL for stats cache
		status: IndexStatus{
			Status:    "initializing",
			IsGitRepo: scanner.IsGitRepo(),
		},
	}

	// Initialize schema
	if err := indexer.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	// Prepare statements
	if err := indexer.prepareStatements(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to prepare statements: %w", err)
	}

	return indexer, nil
}

// initSchema creates or updates the database schema.
func (idx *SQLiteIndexer) initSchema() error {
	// Check current schema version
	var currentVersion int
	err := idx.db.QueryRow("SELECT value FROM metadata WHERE key = 'schema_version'").Scan(&currentVersion)
	if err != nil && err != sql.ErrNoRows {
		// Table might not exist
		currentVersion = 0
	}

	if currentVersion >= schemaVersion {
		return nil // Schema is up to date
	}

	log.Info().Int("current", currentVersion).Int("target", schemaVersion).Msg("updating repository index schema")

	// Drop existing tables if schema changed
	if currentVersion > 0 {
		tables := []string{
			"repository_files_fts",
			"repository_files",
			"repository_directories",
			"metadata",
		}
		for _, table := range tables {
			idx.db.Exec("DROP TABLE IF EXISTS " + table)
		}
	}

	// Create schema
	schema := `
		-- Metadata table
		CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT
		);

		-- Main files table
		CREATE TABLE IF NOT EXISTS repository_files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT NOT NULL UNIQUE,
			path_normalized TEXT NOT NULL,
			name TEXT NOT NULL,
			extension TEXT,
			directory TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			modified_at INTEGER NOT NULL,
			indexed_at INTEGER NOT NULL,
			is_binary INTEGER NOT NULL DEFAULT 0,
			is_symlink INTEGER NOT NULL DEFAULT 0,
			is_sensitive INTEGER NOT NULL DEFAULT 0,
			git_tracked INTEGER NOT NULL DEFAULT 0,
			git_ignored INTEGER NOT NULL DEFAULT 0,
			content_hash TEXT,
			line_count INTEGER,
			file_id INTEGER NOT NULL DEFAULT 0
		);

		-- FTS5 virtual table for full-text search
		CREATE VIRTUAL TABLE IF NOT EXISTS repository_files_fts USING fts5(
			path,
			name,
			directory,
			content='repository_files',
			content_rowid='id',
			tokenize='porter unicode61'
		);

		-- Triggers to keep FTS5 in sync
		CREATE TRIGGER IF NOT EXISTS repository_files_ai AFTER INSERT ON repository_files BEGIN
			INSERT INTO repository_files_fts(rowid, path, name, directory)
			VALUES (new.id, new.path, new.name, new.directory);
		END;

		CREATE TRIGGER IF NOT EXISTS repository_files_ad AFTER DELETE ON repository_files BEGIN
			DELETE FROM repository_files_fts WHERE rowid = old.id;
		END;

		CREATE TRIGGER IF NOT EXISTS repository_files_au AFTER UPDATE ON repository_files BEGIN
			UPDATE repository_files_fts
			SET path = new.path, name = new.name, directory = new.directory
			WHERE rowid = new.id;
		END;

		-- Directory statistics table
		CREATE TABLE IF NOT EXISTS repository_directories (
			path TEXT PRIMARY KEY,
			file_count INTEGER NOT NULL DEFAULT 0,
			total_size_bytes INTEGER NOT NULL DEFAULT 0,
			last_modified INTEGER NOT NULL,
			indexed_at INTEGER NOT NULL
		);

		-- Indexes for fast queries
		CREATE INDEX IF NOT EXISTS idx_files_directory ON repository_files(directory);
		CREATE INDEX IF NOT EXISTS idx_files_extension ON repository_files(extension);
		CREATE INDEX IF NOT EXISTS idx_files_modified ON repository_files(modified_at DESC);
		CREATE INDEX IF NOT EXISTS idx_files_size ON repository_files(size_bytes);
		CREATE INDEX IF NOT EXISTS idx_files_path_normalized ON repository_files(path_normalized);
		CREATE INDEX IF NOT EXISTS idx_files_binary ON repository_files(is_binary) WHERE is_binary = 0;
		CREATE INDEX IF NOT EXISTS idx_files_sensitive ON repository_files(is_sensitive) WHERE is_sensitive = 1;
		CREATE INDEX IF NOT EXISTS idx_files_file_id ON repository_files(file_id) WHERE file_id > 0;
	`

	// Execute schema
	if _, err := idx.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Update schema version
	_, err = idx.db.Exec("INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', ?)", schemaVersion)
	return err
}

// prepareStatements prepares frequently used SQL statements.
func (idx *SQLiteIndexer) prepareStatements() error {
	var err error

	idx.stmtInsertFile, err = idx.db.Prepare(`
		INSERT OR REPLACE INTO repository_files
		(path, path_normalized, name, extension, directory, size_bytes,
		 modified_at, indexed_at, is_binary, is_symlink, is_sensitive,
		 git_tracked, git_ignored, content_hash, line_count, file_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}

	idx.stmtUpdateFile, err = idx.db.Prepare(`
		UPDATE repository_files SET
			size_bytes = ?, modified_at = ?, indexed_at = ?,
			is_binary = ?, is_sensitive = ?, content_hash = ?, line_count = ?
		WHERE path = ?
	`)
	if err != nil {
		return err
	}

	idx.stmtDeleteFile, err = idx.db.Prepare(`DELETE FROM repository_files WHERE path = ?`)
	if err != nil {
		return err
	}

	idx.stmtGetFile, err = idx.db.Prepare(`
		SELECT id, path, name, extension, directory, size_bytes, modified_at, indexed_at,
		       is_binary, is_symlink, is_sensitive, git_tracked, git_ignored, content_hash, line_count
		FROM repository_files WHERE path = ?
	`)
	if err != nil {
		return err
	}

	idx.stmtSearchFuzzy, err = idx.db.Prepare(`
		SELECT f.id, f.path, f.name, f.extension, f.directory, f.size_bytes,
		       f.modified_at, f.is_binary, f.is_sensitive, f.git_tracked,
		       bm25(repository_files_fts) as rank
		FROM repository_files f
		JOIN repository_files_fts fts ON f.id = fts.rowid
		WHERE repository_files_fts MATCH ?
		  AND (? = 0 OR f.is_binary = 0)
		ORDER BY rank
		LIMIT ? OFFSET ?
	`)
	if err != nil {
		return err
	}

	// For rename detection: find existing entry with same file_id but different path
	idx.stmtFindByFileID, err = idx.db.Prepare(`
		SELECT path FROM repository_files
		WHERE file_id = ? AND file_id > 0 AND path != ?
		LIMIT 1
	`)
	if err != nil {
		return err
	}

	return nil
}

// Start begins the indexer and performs initial scan.
func (idx *SQLiteIndexer) Start(ctx context.Context) error {
	idx.mu.Lock()
	if idx.running {
		idx.mu.Unlock()
		return nil
	}
	idx.running = true
	idx.mu.Unlock()

	// Initial full scan in background
	go func() {
		if err := idx.FullScan(context.Background()); err != nil {
			log.Error().Err(err).Msg("initial repository scan failed")
			idx.mu.Lock()
			idx.status.Status = "error"
			idx.status.ErrorMessage = err.Error()
			idx.mu.Unlock()
		}
	}()

	// Start background reconciliation
	go idx.reconciliationLoop()

	log.Info().Str("repo", idx.repoPath).Str("db", idx.dbPath).Msg("repository indexer started")
	return nil
}

// Stop gracefully stops the indexer.
func (idx *SQLiteIndexer) Stop() error {
	idx.mu.Lock()
	if !idx.running {
		idx.mu.Unlock()
		return nil
	}
	idx.running = false
	idx.mu.Unlock()

	close(idx.done)

	// Close prepared statements
	if idx.stmtInsertFile != nil {
		idx.stmtInsertFile.Close()
	}
	if idx.stmtUpdateFile != nil {
		idx.stmtUpdateFile.Close()
	}
	if idx.stmtDeleteFile != nil {
		idx.stmtDeleteFile.Close()
	}
	if idx.stmtGetFile != nil {
		idx.stmtGetFile.Close()
	}
	if idx.stmtSearchFuzzy != nil {
		idx.stmtSearchFuzzy.Close()
	}
	if idx.stmtFindByFileID != nil {
		idx.stmtFindByFileID.Close()
	}

	// Close database
	if err := idx.db.Close(); err != nil {
		return err
	}

	log.Info().Msg("repository indexer stopped")
	return nil
}

// IsReady returns true if the indexer is ready for queries.
func (idx *SQLiteIndexer) IsReady() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.status.Status == "ready"
}

// FullScan performs a complete scan of the repository.
func (idx *SQLiteIndexer) FullScan(ctx context.Context) error {
	start := time.Now()

	idx.mu.Lock()
	idx.status.Status = "indexing"
	idx.status.IndexedFiles = 0
	idx.mu.Unlock()

	defer func() {
		idx.mu.Lock()
		if idx.status.Status == "indexing" {
			idx.status.Status = "ready"
		}
		idx.status.LastFullScan = time.Now()
		idx.status.LastUpdate = time.Now()
		idx.mu.Unlock()
	}()

	// Scan all files
	files, err := idx.scanner.ScanAll(ctx)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	// Begin transaction for batch insert
	tx, err := idx.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing data
	if _, err := tx.Exec("DELETE FROM repository_files"); err != nil {
		return fmt.Errorf("failed to clear files: %w", err)
	}

	// Prepare insert statement within transaction
	stmt, err := tx.Prepare(`
		INSERT INTO repository_files
		(path, path_normalized, name, extension, directory, size_bytes,
		 modified_at, indexed_at, is_binary, is_symlink, is_sensitive,
		 git_tracked, git_ignored, content_hash, line_count, file_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("failed to prepare insert: %w", err)
	}
	defer stmt.Close()

	// Insert files in batches
	var totalSize int64
	for i, f := range files {
		_, err := stmt.Exec(
			f.Path,
			strings.ToLower(f.Path),
			f.Name,
			f.Extension,
			f.Directory,
			f.SizeBytes,
			f.ModifiedAt.Unix(),
			f.IndexedAt.Unix(),
			boolToInt(f.IsBinary),
			boolToInt(f.IsSymlink),
			boolToInt(f.IsSensitive),
			boolToInt(f.GitTracked),
			boolToInt(f.GitIgnored),
			f.ContentHash,
			f.LineCount,
			f.FileID,
		)
		if err != nil {
			log.Warn().Err(err).Str("path", f.Path).Msg("failed to insert file")
			continue
		}

		totalSize += f.SizeBytes

		// Update progress periodically
		if i%1000 == 0 {
			idx.mu.Lock()
			idx.status.IndexedFiles = i
			idx.mu.Unlock()
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Rebuild FTS5 index (required for external content FTS tables after bulk operations)
	if _, err := idx.db.Exec("INSERT INTO repository_files_fts(repository_files_fts) VALUES('rebuild')"); err != nil {
		log.Warn().Err(err).Msg("failed to rebuild FTS5 index")
	}

	// Update directory statistics
	idx.updateDirectoryStats()

	// Invalidate stats cache since we just rebuilt everything
	idx.InvalidateStatsCache()

	// Update final status
	idx.mu.Lock()
	idx.status.TotalFiles = len(files)
	idx.status.IndexedFiles = len(files)
	idx.status.TotalSizeBytes = totalSize
	idx.mu.Unlock()

	// Get database size
	if info, err := os.Stat(idx.dbPath); err == nil {
		idx.mu.Lock()
		idx.status.DatabaseSizeBytes = info.Size()
		idx.mu.Unlock()
	}

	log.Info().
		Int("files", len(files)).
		Int64("total_size", totalSize).
		Dur("elapsed", time.Since(start)).
		Msg("repository scan complete")

	return nil
}

// IndexFile indexes or updates a single file.
// It also detects renames by checking if a file with the same file_id exists at a different path.
func (idx *SQLiteIndexer) IndexFile(ctx context.Context, relPath string) error {
	fileInfo, err := idx.scanner.ScanFile(ctx, relPath)
	if err != nil {
		return err
	}

	log.Debug().
		Str("path", relPath).
		Uint64("file_id", fileInfo.FileID).
		Int64("size", fileInfo.SizeBytes).
		Msg("indexing file")

	// Check for rename: is there an existing entry with same file_id but different path?
	if fileInfo.FileID > 0 {
		var oldPath string
		err := idx.stmtFindByFileID.QueryRowContext(ctx, fileInfo.FileID, relPath).Scan(&oldPath)
		if err == nil && oldPath != "" {
			// This is a rename - remove the old entry
			log.Info().
				Str("old_path", oldPath).
				Str("new_path", relPath).
				Uint64("file_id", fileInfo.FileID).
				Msg("detected file rename, removing old entry")
			if _, err := idx.stmtDeleteFile.ExecContext(ctx, oldPath); err != nil {
				log.Warn().Err(err).Str("path", oldPath).Msg("failed to remove old file entry after rename")
			}
		} else if err != nil && err.Error() != "sql: no rows in result set" {
			log.Debug().Err(err).Uint64("file_id", fileInfo.FileID).Msg("error checking for rename")
		}
	} else {
		log.Debug().Str("path", relPath).Msg("file has no file_id (inode), skip rename detection")
	}

	// Insert or update the file entry
	_, err = idx.stmtInsertFile.ExecContext(ctx,
		fileInfo.Path,
		strings.ToLower(fileInfo.Path),
		fileInfo.Name,
		fileInfo.Extension,
		fileInfo.Directory,
		fileInfo.SizeBytes,
		fileInfo.ModifiedAt.Unix(),
		fileInfo.IndexedAt.Unix(),
		boolToInt(fileInfo.IsBinary),
		boolToInt(fileInfo.IsSymlink),
		boolToInt(fileInfo.IsSensitive),
		boolToInt(fileInfo.GitTracked),
		boolToInt(fileInfo.GitIgnored),
		fileInfo.ContentHash,
		fileInfo.LineCount,
		fileInfo.FileID,
	)
	if err != nil {
		return fmt.Errorf("failed to index file: %w", err)
	}

	idx.mu.Lock()
	idx.status.LastUpdate = time.Now()
	idx.mu.Unlock()

	// Invalidate stats cache since file data changed
	idx.InvalidateStatsCache()

	return nil
}

// RemoveFile removes a file from the index.
func (idx *SQLiteIndexer) RemoveFile(ctx context.Context, relPath string) error {
	result, err := idx.stmtDeleteFile.ExecContext(ctx, relPath)
	if err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	log.Debug().
		Str("path", relPath).
		Int64("rows_affected", rowsAffected).
		Msg("removed file from repository index")

	idx.mu.Lock()
	idx.status.LastUpdate = time.Now()
	if rowsAffected > 0 {
		idx.status.TotalFiles--
	}
	idx.mu.Unlock()

	// Invalidate stats cache since file was removed
	if rowsAffected > 0 {
		idx.InvalidateStatsCache()
	}

	return nil
}

// GetStatus returns the current index status.
func (idx *SQLiteIndexer) GetStatus() IndexStatus {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.status
}

// InvalidateStatsCache invalidates the cached repository statistics.
// This should be called when files are added, modified, or removed.
func (idx *SQLiteIndexer) InvalidateStatsCache() {
	idx.statsMu.Lock()
	defer idx.statsMu.Unlock()
	idx.statsCacheValid = false
	idx.statsCache = nil
	log.Debug().Msg("repository stats cache invalidated")
}

// isStatsCacheValid checks if the stats cache is valid.
func (idx *SQLiteIndexer) isStatsCacheValid() bool {
	idx.statsMu.RLock()
	defer idx.statsMu.RUnlock()

	if !idx.statsCacheValid || idx.statsCache == nil {
		return false
	}

	// Also check TTL as a safety net
	if time.Since(idx.statsCacheTime) > idx.statsCacheTTL {
		return false
	}

	return true
}

// updateDirectoryStats updates the directory statistics table.
// This includes parent directories that may not have direct files.
// Optimized to use a single query instead of O(N×2) queries per directory.
func (idx *SQLiteIndexer) updateDirectoryStats() {
	start := time.Now()
	now := start.Unix()

	// Clear existing directory stats
	if _, err := idx.db.Exec("DELETE FROM repository_directories"); err != nil {
		log.Warn().Err(err).Msg("failed to clear directory stats")
		return
	}

	// Single optimized query that:
	// 1. Gets all unique directories (including parent paths)
	// 2. Calculates file counts and sizes for each directory (including subdirectories)
	// 3. Inserts all results in one batch
	//
	// This replaces O(N×2) individual queries with a single aggregated query.
	query := `
		WITH RECURSIVE
		-- Extract all unique directories from files
		file_dirs AS (
			SELECT DISTINCT directory FROM repository_files WHERE directory != ''
		),
		-- Recursively generate all parent directories
		all_dirs AS (
			SELECT directory FROM file_dirs
			UNION
			SELECT
				CASE
					WHEN INSTR(directory, '/') > 0
					THEN SUBSTR(directory, 1, LENGTH(directory) - LENGTH(SUBSTR(directory, INSTR(directory, '/') + 1)) - 1)
					ELSE ''
				END as directory
			FROM all_dirs
			WHERE directory != '' AND INSTR(directory, '/') > 0
		),
		-- Get unique non-empty directories
		unique_dirs AS (
			SELECT DISTINCT directory FROM all_dirs WHERE directory != ''
		),
		-- Calculate stats for each directory (including subdirectories)
		dir_stats AS (
			SELECT
				ud.directory,
				(SELECT COUNT(*) FROM repository_files rf
				 WHERE rf.directory = ud.directory
				    OR rf.directory LIKE ud.directory || '/%') as file_count,
				(SELECT COALESCE(SUM(size_bytes), 0) FROM repository_files rf
				 WHERE rf.directory = ud.directory
				    OR rf.directory LIKE ud.directory || '/%') as total_size,
				(SELECT COALESCE(MAX(modified_at), 0) FROM repository_files rf
				 WHERE rf.directory = ud.directory
				    OR rf.directory LIKE ud.directory || '/%') as last_modified
			FROM unique_dirs ud
		)
		INSERT INTO repository_directories (path, file_count, total_size_bytes, last_modified, indexed_at)
		SELECT directory, file_count, total_size, last_modified, ?
		FROM dir_stats
		WHERE file_count > 0
	`

	result, err := idx.db.Exec(query, now)
	if err != nil {
		log.Warn().Err(err).Msg("failed to update directory stats with optimized query")
		// Fall back to simple root-only stats
		idx.updateRootDirectoryStats(now)
		return
	}

	rowsAffected, _ := result.RowsAffected()

	// Also add entry for root (files with empty directory)
	idx.updateRootDirectoryStats(now)

	log.Debug().
		Int64("directories", rowsAffected).
		Dur("elapsed", time.Since(start)).
		Msg("updated directory stats")
}

// updateRootDirectoryStats adds stats for files in the root directory.
func (idx *SQLiteIndexer) updateRootDirectoryStats(now int64) {
	var rootCount int
	var rootSize int64
	var rootModified int64

	err := idx.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(size_bytes), 0), COALESCE(MAX(modified_at), 0)
		FROM repository_files WHERE directory = ''
	`).Scan(&rootCount, &rootSize, &rootModified)

	if err != nil {
		log.Debug().Err(err).Msg("failed to query root directory stats")
		return
	}

	if rootCount > 0 {
		_, err = idx.db.Exec(`
			INSERT OR REPLACE INTO repository_directories (path, file_count, total_size_bytes, last_modified, indexed_at)
			VALUES ('', ?, ?, ?, ?)
		`, rootCount, rootSize, rootModified, now)
		if err != nil {
			log.Warn().Err(err).Msg("failed to insert root directory stats")
		}
	}
}

// reconciliationLoop periodically syncs the index with the file system.
func (idx *SQLiteIndexer) reconciliationLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-idx.done:
			return
		case <-ticker.C:
			log.Debug().Msg("running repository index reconciliation")
			ctx := context.Background()
			if err := idx.FullScan(ctx); err != nil {
				log.Warn().Err(err).Msg("reconciliation scan failed")
			}
		}
	}
}

// boolToInt converts a boolean to an integer for SQLite storage.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// intToBool converts an integer to a boolean.
func intToBool(i int) bool {
	return i != 0
}
