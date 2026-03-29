// Package taskstore provides SQLite-backed persistence for AgentTasks.
// It follows the same patterns as the repository indexer (WAL mode, prepared statements).
package taskstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/domain/task"
	"github.com/brianly1003/cdev/internal/pathutil"
	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

const schemaVersion = 1

// Store provides CRUD operations for AgentTasks backed by SQLite.
type Store struct {
	db *sql.DB
	mu sync.RWMutex
}

// NewStore creates a new task store, initializing the database and schema.
func NewStore() (*Store, error) {
	// Create data directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home dir: %w", err)
	}
	dataDir := filepath.Join(homeDir, ".cdev", "data")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "agent-tasks.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure SQLite for performance and durability
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-32000", // 32MB
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to set pragma %q: %w", pragma, err)
		}
	}

	s := &Store{db: db}

	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	log.Info().Str("path", dbPath).Msg("task store initialized")
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS metadata (
		key TEXT PRIMARY KEY,
		value TEXT
	);

	CREATE TABLE IF NOT EXISTS agent_tasks (
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		case_id INTEGER,
		task_type TEXT NOT NULL,
		title TEXT NOT NULL,
		description TEXT,
		severity TEXT DEFAULT 'medium',
		labels TEXT DEFAULT '[]',
		status TEXT DEFAULT 'pending',
		assignee TEXT DEFAULT 'cdev',
		task_yaml TEXT,
		trigger_json TEXT,
		anchors_json TEXT,
		policy_json TEXT,
		result_json TEXT,
		timeline_json TEXT DEFAULT '[]',
		session_id TEXT,
		branch_name TEXT,
		worktree_path TEXT,
		created_by TEXT,
		created_at INTEGER NOT NULL,
		started_at INTEGER,
		completed_at INTEGER
	);

	CREATE INDEX IF NOT EXISTS idx_tasks_status ON agent_tasks(status);
	CREATE INDEX IF NOT EXISTS idx_tasks_workspace ON agent_tasks(workspace_id);
	CREATE INDEX IF NOT EXISTS idx_tasks_created ON agent_tasks(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_tasks_type ON agent_tasks(task_type);

	CREATE TABLE IF NOT EXISTS agent_task_revisions (
		id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL REFERENCES agent_tasks(id) ON DELETE CASCADE,
		revision_no INTEGER NOT NULL,
		feedback TEXT NOT NULL,
		result_summary TEXT,
		created_by TEXT,
		created_at INTEGER NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_revisions_task ON agent_task_revisions(task_id);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Set schema version
	_, err = s.db.Exec(
		"INSERT OR REPLACE INTO metadata (key, value) VALUES ('schema_version', ?)",
		fmt.Sprintf("%d", schemaVersion),
	)
	if err != nil {
		return err
	}

	// Schema migrations — add columns that may not exist yet
	migrations := []string{
		"ALTER TABLE agent_tasks ADD COLUMN origin_json TEXT",
		"ALTER TABLE agent_tasks ADD COLUMN case_context_json TEXT",
		"ALTER TABLE agent_tasks ADD COLUMN prompt TEXT DEFAULT ''",
	}
	for _, m := range migrations {
		_, _ = s.db.Exec(m) // ignore errors (column already exists)
	}

	return nil
}

// Create inserts a new AgentTask.
func (s *Store) Create(t *task.AgentTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	labelsJSON, _ := json.Marshal(t.Labels)
	triggerJSON, _ := json.Marshal(t.Trigger)
	anchorsJSON, _ := json.Marshal(t.Anchors)
	policyJSON, _ := json.Marshal(t.Policy)
	resultJSON, _ := json.Marshal(t.Result)
	timelineJSON, _ := json.Marshal(t.Timeline)
	originJSON, _ := marshalOrigin(t.Origin)
	caseCtxJSON := nullableRawJSON(t.CaseContext)

	_, err := s.db.Exec(`
		INSERT INTO agent_tasks (
			id, workspace_id, case_id, task_type, title, description,
			severity, labels, status, assignee, task_yaml,
			trigger_json, anchors_json, policy_json, result_json, timeline_json,
			origin_json, case_context_json, prompt,
			session_id, branch_name, worktree_path,
			created_by, created_at, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.WorkspaceID, t.CaseID, string(t.TaskType), t.Title, t.Description,
		string(t.Severity), string(labelsJSON), string(t.Status), t.Assignee, t.TaskYAML,
		string(triggerJSON), string(anchorsJSON), string(policyJSON), string(resultJSON), string(timelineJSON),
		string(originJSON), caseCtxJSON, t.Prompt,
		t.SessionID, t.BranchName, t.WorktreePath,
		t.CreatedBy, t.CreatedAt.Unix(), timeToUnix(t.StartedAt), timeToUnix(t.CompletedAt),
	)
	return err
}

// Update saves changes to an existing AgentTask.
func (s *Store) Update(t *task.AgentTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	labelsJSON, _ := json.Marshal(t.Labels)
	triggerJSON, _ := json.Marshal(t.Trigger)
	anchorsJSON, _ := json.Marshal(t.Anchors)
	policyJSON, _ := json.Marshal(t.Policy)
	resultJSON, _ := json.Marshal(t.Result)
	timelineJSON, _ := json.Marshal(t.Timeline)
	originJSON, _ := marshalOrigin(t.Origin)
	caseCtxJSON := nullableRawJSON(t.CaseContext)

	result, err := s.db.Exec(`
		UPDATE agent_tasks SET
			workspace_id = ?, case_id = ?, task_type = ?, title = ?, description = ?,
			severity = ?, labels = ?, status = ?, assignee = ?, task_yaml = ?,
			trigger_json = ?, anchors_json = ?, policy_json = ?, result_json = ?, timeline_json = ?,
			origin_json = ?, case_context_json = ?, prompt = ?,
			session_id = ?, branch_name = ?, worktree_path = ?,
			started_at = ?, completed_at = ?
		WHERE id = ?`,
		t.WorkspaceID, t.CaseID, string(t.TaskType), t.Title, t.Description,
		string(t.Severity), string(labelsJSON), string(t.Status), t.Assignee, t.TaskYAML,
		string(triggerJSON), string(anchorsJSON), string(policyJSON), string(resultJSON), string(timelineJSON),
		string(originJSON), caseCtxJSON, t.Prompt,
		t.SessionID, t.BranchName, t.WorktreePath,
		timeToUnix(t.StartedAt), timeToUnix(t.CompletedAt),
		t.ID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found: %s", t.ID)
	}
	return nil
}

// ReplaceSessionID updates persisted tasks that still reference a temporary
// session ID after Claude resolved the real transcript ID.
func (s *Store) ReplaceSessionID(oldSessionID, newSessionID string) (int64, error) {
	if strings.TrimSpace(oldSessionID) == "" || strings.TrimSpace(newSessionID) == "" {
		return 0, fmt.Errorf("both old and new session IDs are required")
	}
	if oldSessionID == newSessionID {
		return 0, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		"UPDATE agent_tasks SET session_id = ? WHERE session_id = ?",
		newSessionID,
		oldSessionID,
	)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return rows, nil
}

// RepairSessionIDs fixes persisted worktree task session IDs when a task still
// points at a temporary cdev session ID but the Claude transcript exists under
// the real session UUID on disk.
func (s *Store) RepairSessionIDs() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
		SELECT id, session_id, worktree_path
		FROM agent_tasks
		WHERE session_id IS NOT NULL AND session_id != ''
		  AND worktree_path IS NOT NULL AND worktree_path != ''`)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()

	type candidate struct {
		taskID       string
		sessionID    string
		worktreePath string
	}

	var tasks []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.taskID, &item.sessionID, &item.worktreePath); err != nil {
			return 0, err
		}
		tasks = append(tasks, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	repaired := 0
	for _, item := range tasks {
		resolvedID, err := recoverSessionIDFromWorktree(item.worktreePath, item.sessionID)
		if err != nil {
			return repaired, err
		}
		if resolvedID == "" || resolvedID == item.sessionID {
			continue
		}

		result, err := s.db.Exec(
			"UPDATE agent_tasks SET session_id = ? WHERE id = ? AND session_id = ?",
			resolvedID,
			item.taskID,
			item.sessionID,
		)
		if err != nil {
			return repaired, err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return repaired, err
		}
		if rowsAffected == 0 {
			continue
		}

		repaired++
		log.Info().
			Str("task_id", item.taskID).
			Str("temporary_session_id", item.sessionID).
			Str("real_session_id", resolvedID).
			Str("worktree_path", item.worktreePath).
			Msg("repaired persisted task session ID")
	}

	return repaired, nil
}

// ResolveHistoricalSessionProjectPath returns the stored task worktree path for
// a historical session when that path is no longer discoverable from the live
// git worktree list.
func (s *Store) ResolveHistoricalSessionProjectPath(workspaceID, sessionID string) (string, bool, error) {
	if strings.TrimSpace(workspaceID) == "" || strings.TrimSpace(sessionID) == "" {
		return "", false, nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var worktreePath sql.NullString
	err := s.db.QueryRow(`
		SELECT worktree_path
		FROM agent_tasks
		WHERE workspace_id = ?
		  AND session_id = ?
		  AND worktree_path IS NOT NULL
		  AND worktree_path != ''
		ORDER BY created_at DESC
		LIMIT 1`,
		workspaceID,
		sessionID,
	).Scan(&worktreePath)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !worktreePath.Valid || strings.TrimSpace(worktreePath.String) == "" {
		return "", false, nil
	}

	return worktreePath.String, true, nil
}

func recoverSessionIDFromWorktree(worktreePath, currentSessionID string) (string, error) {
	sessionsDir := claudeSessionsDirForProjectPath(worktreePath)

	if currentSessionID != "" {
		currentSessionFile := filepath.Join(sessionsDir, currentSessionID+".jsonl")
		if _, err := os.Stat(currentSessionFile); err == nil {
			return currentSessionID, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	var bestID string
	var bestModTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return "", err
		}
		if bestID == "" || info.ModTime().After(bestModTime) {
			bestID = strings.TrimSuffix(entry.Name(), ".jsonl")
			bestModTime = info.ModTime()
		}
	}

	return bestID, nil
}

func claudeSessionsDirForProjectPath(projectPath string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "~"
	}
	return filepath.Join(homeDir, ".claude", "projects", pathutil.EncodePath(projectPath))
}

// GetByID retrieves a task by its ID.
func (s *Store) GetByID(id string) (*task.AgentTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`
		SELECT id, workspace_id, case_id, task_type, title, description,
			severity, labels, status, assignee, task_yaml,
			trigger_json, anchors_json, policy_json, result_json, timeline_json,
			origin_json, case_context_json, prompt,
			session_id, branch_name, worktree_path,
			created_by, created_at, started_at, completed_at
		FROM agent_tasks WHERE id = ?`, id)

	return scanTask(row)
}

// QueryFilter defines filters for listing tasks.
type QueryFilter struct {
	Status      string
	TaskType    string
	WorkspaceID string
	Limit       int
	Offset      int
}

// List retrieves tasks matching the given filters.
func (s *Store) List(filter QueryFilter) ([]*task.AgentTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := `
		SELECT id, workspace_id, case_id, task_type, title, description,
			severity, labels, status, assignee, task_yaml,
			trigger_json, anchors_json, policy_json, result_json, timeline_json,
			origin_json, case_context_json, prompt,
			session_id, branch_name, worktree_path,
			created_by, created_at, started_at, completed_at
		FROM agent_tasks WHERE 1=1`
	args := []interface{}{}

	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.TaskType != "" {
		query += " AND task_type = ?"
		args = append(args, filter.TaskType)
	}
	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	} else {
		query += " LIMIT 50"
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tasks []*task.AgentTask
	for rows.Next() {
		t, err := scanTaskRow(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// Delete removes a task by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec("DELETE FROM agent_tasks WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	return nil
}

// AddRevision inserts a new revision for a task.
func (s *Store) AddRevision(r *task.Revision) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`
		INSERT INTO agent_task_revisions (id, task_id, revision_no, feedback, result_summary, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.RevisionNo, r.Feedback, r.ResultSummary, r.CreatedBy, r.CreatedAt.Unix(),
	)
	return err
}

// GetRevisions retrieves all revisions for a task, ordered by revision number.
func (s *Store) GetRevisions(taskID string) ([]task.Revision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
		SELECT id, task_id, revision_no, feedback, result_summary, created_by, created_at
		FROM agent_task_revisions WHERE task_id = ? ORDER BY revision_no ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var revisions []task.Revision
	for rows.Next() {
		var r task.Revision
		var createdAtUnix int64
		var resultSummary sql.NullString
		err := rows.Scan(&r.ID, &r.TaskID, &r.RevisionNo, &r.Feedback, &resultSummary, &r.CreatedBy, &createdAtUnix)
		if err != nil {
			return nil, err
		}
		r.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
		if resultSummary.Valid {
			r.ResultSummary = resultSummary.String
		}
		revisions = append(revisions, r)
	}
	return revisions, rows.Err()
}

// CountByStatus returns counts of tasks grouped by status.
func (s *Store) CountByStatus() (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query("SELECT status, COUNT(*) FROM agent_tasks GROUP BY status")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[string]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts[status] = count
	}
	return counts, rows.Err()
}

// --- Helpers ---

func scanTask(row *sql.Row) (*task.AgentTask, error) {
	t := &task.AgentTask{}
	var caseID sql.NullInt64
	var labelsJSON, triggerJSON, anchorsJSON, policyJSON, resultJSON, timelineJSON string
	var originJSON, caseCtxJSON sql.NullString
	var taskYAML, sessionID, branchName, worktreePath, createdBy sql.NullString
	var prompt sql.NullString
	var startedAtUnix, completedAtUnix sql.NullInt64
	var createdAtUnix int64

	err := row.Scan(
		&t.ID, &t.WorkspaceID, &caseID, &t.TaskType, &t.Title, &t.Description,
		&t.Severity, &labelsJSON, &t.Status, &t.Assignee, &taskYAML,
		&triggerJSON, &anchorsJSON, &policyJSON, &resultJSON, &timelineJSON,
		&originJSON, &caseCtxJSON, &prompt,
		&sessionID, &branchName, &worktreePath,
		&createdBy, &createdAtUnix, &startedAtUnix, &completedAtUnix,
	)
	if err != nil {
		return nil, err
	}

	return populateTask(t, caseID, labelsJSON, triggerJSON, anchorsJSON, policyJSON,
		resultJSON, timelineJSON, originJSON, caseCtxJSON, taskYAML, prompt, sessionID,
		branchName, worktreePath, createdBy, createdAtUnix, startedAtUnix, completedAtUnix), nil
}

func scanTaskRow(rows *sql.Rows) (*task.AgentTask, error) {
	t := &task.AgentTask{}
	var caseID sql.NullInt64
	var labelsJSON, triggerJSON, anchorsJSON, policyJSON, resultJSON, timelineJSON string
	var originJSON, caseCtxJSON sql.NullString
	var taskYAML, sessionID, branchName, worktreePath, createdBy sql.NullString
	var prompt sql.NullString
	var startedAtUnix, completedAtUnix sql.NullInt64
	var createdAtUnix int64

	err := rows.Scan(
		&t.ID, &t.WorkspaceID, &caseID, &t.TaskType, &t.Title, &t.Description,
		&t.Severity, &labelsJSON, &t.Status, &t.Assignee, &taskYAML,
		&triggerJSON, &anchorsJSON, &policyJSON, &resultJSON, &timelineJSON,
		&originJSON, &caseCtxJSON, &prompt,
		&sessionID, &branchName, &worktreePath,
		&createdBy, &createdAtUnix, &startedAtUnix, &completedAtUnix,
	)
	if err != nil {
		return nil, err
	}

	return populateTask(t, caseID, labelsJSON, triggerJSON, anchorsJSON, policyJSON,
		resultJSON, timelineJSON, originJSON, caseCtxJSON, taskYAML, prompt, sessionID,
		branchName, worktreePath, createdBy, createdAtUnix, startedAtUnix, completedAtUnix), nil
}

func populateTask(t *task.AgentTask, caseID sql.NullInt64,
	labelsJSON, triggerJSON, anchorsJSON, policyJSON, resultJSON, timelineJSON string,
	originJSON, caseCtxJSON sql.NullString,
	taskYAML, prompt, sessionID, branchName, worktreePath, createdBy sql.NullString,
	createdAtUnix int64, startedAtUnix, completedAtUnix sql.NullInt64,
) *task.AgentTask {
	if caseID.Valid {
		id := int(caseID.Int64)
		t.CaseID = &id
	}
	if originJSON.Valid && originJSON.String != "null" {
		t.Origin = unmarshalOrigin(originJSON.String)
	}
	if caseCtxJSON.Valid && caseCtxJSON.String != "" {
		t.CaseContext = json.RawMessage(caseCtxJSON.String)
	}
	if taskYAML.Valid {
		t.TaskYAML = taskYAML.String
	}
	if prompt.Valid {
		t.Prompt = prompt.String
	}
	if sessionID.Valid {
		t.SessionID = sessionID.String
	}
	if branchName.Valid {
		t.BranchName = branchName.String
	}
	if worktreePath.Valid {
		t.WorktreePath = worktreePath.String
	}
	if createdBy.Valid {
		t.CreatedBy = createdBy.String
	}

	if err := json.Unmarshal([]byte(labelsJSON), &t.Labels); err != nil {
		log.Warn().Str("task_id", t.ID).Err(err).Msg("failed to unmarshal task labels")
	}
	if err := json.Unmarshal([]byte(triggerJSON), &t.Trigger); err != nil {
		log.Warn().Str("task_id", t.ID).Err(err).Msg("failed to unmarshal task trigger")
	}
	if err := json.Unmarshal([]byte(anchorsJSON), &t.Anchors); err != nil {
		log.Warn().Str("task_id", t.ID).Err(err).Msg("failed to unmarshal task anchors")
	}
	if err := json.Unmarshal([]byte(policyJSON), &t.Policy); err != nil {
		log.Warn().Str("task_id", t.ID).Err(err).Msg("failed to unmarshal task policy")
	}
	if err := json.Unmarshal([]byte(resultJSON), &t.Result); err != nil {
		log.Warn().Str("task_id", t.ID).Err(err).Msg("failed to unmarshal task result")
	}
	if err := json.Unmarshal([]byte(timelineJSON), &t.Timeline); err != nil {
		log.Warn().Str("task_id", t.ID).Err(err).Msg("failed to unmarshal task timeline")
	}

	t.CreatedAt = time.Unix(createdAtUnix, 0).UTC()
	if startedAtUnix.Valid {
		st := time.Unix(startedAtUnix.Int64, 0).UTC()
		t.StartedAt = &st
	}
	if completedAtUnix.Valid {
		ct := time.Unix(completedAtUnix.Int64, 0).UTC()
		t.CompletedAt = &ct
	}

	// Ensure non-nil slices
	if t.Labels == nil {
		t.Labels = []string{}
	}
	if t.Timeline == nil {
		t.Timeline = []task.Event{}
	}

	return t
}

// originStorage mirrors task.Origin but includes APIKey for persistence.
// task.Origin uses json:"-" on APIKey to prevent leakage in REST responses,
// so we need a separate struct for database storage.
type originStorage struct {
	System string `json:"system"`
	TaskID int    `json:"task_id,omitempty"`
	CaseID *int   `json:"case_id,omitempty"`
	URL    string `json:"url,omitempty"`
	APIKey string `json:"api_key,omitempty"`
}

func marshalOrigin(o *task.Origin) ([]byte, error) {
	if o == nil {
		return json.Marshal(nil)
	}
	return json.Marshal(originStorage{
		System: o.System,
		TaskID: o.TaskID,
		CaseID: o.CaseID,
		URL:    o.URL,
		APIKey: o.APIKey,
	})
}

func unmarshalOrigin(data string) *task.Origin {
	var s originStorage
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return nil
	}
	if s.System == "" {
		return nil
	}
	return &task.Origin{
		System: s.System,
		TaskID: s.TaskID,
		CaseID: s.CaseID,
		URL:    s.URL,
		APIKey: s.APIKey,
	}
}

func timeToUnix(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// nullableRawJSON returns nil for empty/null RawMessage, or the string value.
func nullableRawJSON(raw json.RawMessage) interface{} {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
