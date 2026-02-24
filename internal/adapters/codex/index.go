// Package codex provides Codex CLI session parsing, discovery, and indexing.
package codex

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/adapters/jsonl"
	"github.com/rs/zerolog/log"
)

// ErrProjectNotFound indicates the requested project has no indexed Codex sessions.
var ErrProjectNotFound = errors.New("project not found")

// SessionIndexEntry represents a single session in the project index.
// Modeled after Claude Code's sessions-index.json entries.
type SessionIndexEntry struct {
	// SessionID is the unique session identifier (UUID).
	SessionID string `json:"sessionId"`

	// FullPath is the absolute path to the session file.
	FullPath string `json:"fullPath"`

	// FileMtime is the file modification time in milliseconds.
	FileMtime int64 `json:"fileMtime"`

	// FirstPrompt is the first user message in the session.
	FirstPrompt string `json:"firstPrompt,omitempty"`

	// Summary is the last assistant message or agent summary.
	Summary string `json:"summary,omitempty"`

	// MessageCount is the number of user/assistant message pairs.
	MessageCount int `json:"messageCount"`

	// Created is when the session was created.
	Created time.Time `json:"created"`

	// Modified is when the session was last modified.
	Modified time.Time `json:"modified"`

	// GitBranch is the git branch at session start.
	GitBranch string `json:"gitBranch,omitempty"`

	// GitCommit is the git commit hash at session start.
	GitCommit string `json:"gitCommit,omitempty"`

	// GitRepo is the repository URL.
	GitRepo string `json:"gitRepo,omitempty"`

	// ProjectPath is the working directory (cwd) for this session.
	ProjectPath string `json:"projectPath"`

	// ModelProvider is the AI provider (openai, anthropic, etc.).
	ModelProvider string `json:"modelProvider,omitempty"`

	// Model is the specific model used.
	Model string `json:"model,omitempty"`

	// CLIVersion is the Codex CLI version.
	CLIVersion string `json:"cliVersion,omitempty"`

	// FileSize is the session file size in bytes.
	FileSize int64 `json:"fileSize,omitempty"`

	// LineCount is the number of lines in the session file.
	LineCount int `json:"lineCount,omitempty"`
}

// SessionIndex represents the index for a project (like Claude Code's sessions-index.json).
type SessionIndex struct {
	// Version is the index format version.
	Version int `json:"version"`

	// Entries are the indexed sessions.
	Entries []SessionIndexEntry `json:"entries"`

	// OriginalPath is the project directory path.
	OriginalPath string `json:"originalPath"`

	// LastIndexed is when the index was last built.
	LastIndexed time.Time `json:"lastIndexed"`
}

// ProjectSummary represents a project with its session statistics.
type ProjectSummary struct {
	// ProjectPath is the working directory.
	ProjectPath string `json:"projectPath"`

	// EncodedPath is the URL-safe encoded path (like Claude Code).
	EncodedPath string `json:"encodedPath"`

	// SessionCount is the number of sessions in this project.
	SessionCount int `json:"sessionCount"`

	// TotalSize is the total size of all session files in bytes.
	TotalSize int64 `json:"totalSize"`

	// LastActivity is the most recent session modification time.
	LastActivity time.Time `json:"lastActivity"`

	// GitRepo is the repository URL (from most recent session).
	GitRepo string `json:"gitRepo,omitempty"`

	// GitBranch is the current/last branch (from most recent session).
	GitBranch string `json:"gitBranch,omitempty"`
}

// IndexCache provides cached access to Codex session indexes.
type IndexCache struct {
	mu             sync.RWMutex
	codexHome      string
	projectIndex   map[string]*SessionIndex // keyed by encoded project path
	allProjects    []ProjectSummary
	lastScan       time.Time
	scanInterval   time.Duration
	fileEntryCache map[string]SessionIndexEntry // keyed by full path; enables incremental refresh
}

// NewIndexCache creates a new index cache.
func NewIndexCache(codexHome string) *IndexCache {
	if codexHome == "" {
		codexHome = DefaultCodexHome()
	}
	return &IndexCache{
		codexHome:      codexHome,
		projectIndex:   make(map[string]*SessionIndex),
		fileEntryCache: make(map[string]SessionIndexEntry),
		scanInterval:   30 * time.Second, // Refresh every 30 seconds at most
	}
}

// encodeProjectPath converts a path to Claude Code-style encoded format.
// e.g., "/Users/brian/Projects/cdev" -> "-Users-brian-Projects-cdev"
func encodeProjectPath(path string) string {
	return strings.ReplaceAll(path, string(filepath.Separator), "-")
}

// decodeProjectPath converts an encoded path back to the original.
func decodeProjectPath(encoded string) string {
	// Handle leading dash
	if strings.HasPrefix(encoded, "-") {
		return string(filepath.Separator) + strings.ReplaceAll(encoded[1:], "-", string(filepath.Separator))
	}
	return strings.ReplaceAll(encoded, "-", string(filepath.Separator))
}

// ListProjects returns all projects with Codex sessions.
func (c *IndexCache) ListProjects() ([]ProjectSummary, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refreshIfNeeded(); err != nil {
		return nil, err
	}

	// Return a copy to avoid race conditions
	result := make([]ProjectSummary, len(c.allProjects))
	copy(result, c.allProjects)
	return result, nil
}

// GetProjectIndex returns the session index for a specific project.
func (c *IndexCache) GetProjectIndex(projectPath string) (*SessionIndex, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refreshIfNeeded(); err != nil {
		return nil, err
	}

	encoded := encodeProjectPath(projectPath)
	if idx, ok := c.projectIndex[encoded]; ok {
		return idx, nil
	}

	return nil, ErrProjectNotFound
}

// GetSessionsForProject returns all sessions for a specific project path.
func (c *IndexCache) GetSessionsForProject(projectPath string) ([]SessionIndexEntry, error) {
	idx, err := c.GetProjectIndex(projectPath)
	if err != nil {
		// If exact match fails, try prefix match (for subdirectories)
		c.mu.RLock()
		defer c.mu.RUnlock()

		absPath, _ := filepath.Abs(projectPath)
		var matches []SessionIndexEntry

		for _, index := range c.projectIndex {
			for _, entry := range index.Entries {
				absCwd, _ := filepath.Abs(entry.ProjectPath)
				if isWithinPath(absPath, absCwd) {
					matches = append(matches, entry)
				}
			}
		}

		if len(matches) > 0 {
			// Sort by modified time (most recent first)
			sort.Slice(matches, func(i, j int) bool {
				return matches[i].Modified.After(matches[j].Modified)
			})
			return matches, nil
		}

		return nil, err
	}

	return idx.Entries, nil
}

// GetAllSessions returns all sessions from all projects.
// Sessions are sorted by modification time (most recent first).
// Use this when you want to list sessions without filtering by project path.
func (c *IndexCache) GetAllSessions() ([]SessionIndexEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refreshIfNeeded(); err != nil {
		return nil, err
	}

	var allSessions []SessionIndexEntry
	for _, idx := range c.projectIndex {
		allSessions = append(allSessions, idx.Entries...)
	}

	// Sort by modified time (most recent first)
	sort.Slice(allSessions, func(i, j int) bool {
		return allSessions[i].Modified.After(allSessions[j].Modified)
	})

	return allSessions, nil
}

// FindSessionByID searches all projects for a session by ID.
func (c *IndexCache) FindSessionByID(sessionID string) (*SessionIndexEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.refreshIfNeeded(); err != nil {
		return nil, err
	}

	for _, idx := range c.projectIndex {
		for _, entry := range idx.Entries {
			if entry.SessionID == sessionID {
				return &entry, nil
			}
		}
	}

	return nil, errors.New("session not found")
}

// Refresh forces a refresh of the index cache.
func (c *IndexCache) Refresh() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.doRefresh()
}

// InvalidateByPath removes a single session from the in-memory cache by its file path.
// Use this after deleting a session file to keep the cache consistent without a
// full directory rescan.
func (c *IndexCache) InvalidateByPath(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.fileEntryCache[path]
	if !ok {
		return nil // not cached; nothing to do
	}

	delete(c.fileEntryCache, path)

	if entry.ProjectPath == "" {
		return nil
	}

	encoded := encodeProjectPath(entry.ProjectPath)
	if idx, ok := c.projectIndex[encoded]; ok {
		filtered := make([]SessionIndexEntry, 0, len(idx.Entries))
		for _, e := range idx.Entries {
			if e.FullPath != path {
				filtered = append(filtered, e)
			}
		}
		if len(filtered) == 0 {
			delete(c.projectIndex, encoded)
		} else {
			idx.Entries = filtered
		}
	}

	c.rebuildProjectSummaries()
	return nil
}

// rebuildProjectSummaries regenerates allProjects from the current projectIndex.
// Must be called with c.mu held.
func (c *IndexCache) rebuildProjectSummaries() {
	newProjects := make([]ProjectSummary, 0, len(c.projectIndex))
	for encoded, idx := range c.projectIndex {
		originalPath := decodeProjectPath(encoded)
		var totalSize int64
		var lastActivity time.Time
		var gitRepo, gitBranch string
		for i, e := range idx.Entries {
			totalSize += e.FileSize
			if e.Modified.After(lastActivity) {
				lastActivity = e.Modified
			}
			if i == 0 {
				gitRepo = e.GitRepo
				gitBranch = e.GitBranch
			}
		}
		newProjects = append(newProjects, ProjectSummary{
			ProjectPath:  originalPath,
			EncodedPath:  encoded,
			SessionCount: len(idx.Entries),
			TotalSize:    totalSize,
			LastActivity: lastActivity,
			GitRepo:      gitRepo,
			GitBranch:    gitBranch,
		})
	}
	sort.Slice(newProjects, func(i, j int) bool {
		return newProjects[i].LastActivity.After(newProjects[j].LastActivity)
	})
	c.allProjects = newProjects
}

func (c *IndexCache) refreshIfNeeded() error {
	if time.Since(c.lastScan) < c.scanInterval {
		return nil
	}
	return c.doRefresh()
}

func (c *IndexCache) doRefresh() error {
	startTime := time.Now()

	sessionsRoot := SessionsDir(c.codexHome)
	if _, err := os.Stat(sessionsRoot); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			c.projectIndex = make(map[string]*SessionIndex)
			c.fileEntryCache = make(map[string]SessionIndexEntry)
			c.allProjects = nil
			c.lastScan = time.Now()
			return nil
		}
		return err
	}

	// Collect all session files grouped by project (cwd).
	// seenPaths tracks every rollout-*.jsonl visited so we can evict stale cache entries.
	projectSessions := make(map[string][]SessionIndexEntry)
	seenPaths := make(map[string]struct{})

	err := filepath.WalkDir(sessionsRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip errors, continue walking
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
			return nil
		}
		// Only process rollout-*.jsonl files
		if !strings.HasPrefix(d.Name(), "rollout-") {
			return nil
		}

		seenPaths[path] = struct{}{}

		// Reuse the cached entry when the file's mtime is unchanged — avoids re-parsing.
		if info, infoErr := d.Info(); infoErr == nil {
			mtimeMs := info.ModTime().UnixMilli()
			if cached, ok := c.fileEntryCache[path]; ok && cached.FileMtime == mtimeMs && cached.ProjectPath != "" {
				encoded := encodeProjectPath(cached.ProjectPath)
				projectSessions[encoded] = append(projectSessions[encoded], cached)
				return nil
			}
		}

		entry, err := parseSessionFileForIndex(path)
		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("failed to parse session file for index")
			return nil
		}
		if entry == nil || entry.ProjectPath == "" {
			return nil
		}

		// Update the per-file cache for future refreshes.
		c.fileEntryCache[path] = *entry

		encoded := encodeProjectPath(entry.ProjectPath)
		projectSessions[encoded] = append(projectSessions[encoded], *entry)
		return nil
	})

	if err != nil {
		return err
	}

	// Evict cache entries for session files that no longer exist on disk.
	for path := range c.fileEntryCache {
		if _, seen := seenPaths[path]; !seen {
			delete(c.fileEntryCache, path)
		}
	}

	// Build indexes for each project
	newProjectIndex := make(map[string]*SessionIndex)
	var newProjects []ProjectSummary

	for encoded, entries := range projectSessions {
		// Sort by modified time (most recent first)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Modified.After(entries[j].Modified)
		})

		originalPath := decodeProjectPath(encoded)

		idx := &SessionIndex{
			Version:      1,
			Entries:      entries,
			OriginalPath: originalPath,
			LastIndexed:  time.Now(),
		}
		newProjectIndex[encoded] = idx

		// Build project summary
		var totalSize int64
		var lastActivity time.Time
		var gitRepo, gitBranch string

		for i, e := range entries {
			totalSize += e.FileSize
			if e.Modified.After(lastActivity) {
				lastActivity = e.Modified
			}
			// Use git info from most recent session
			if i == 0 {
				gitRepo = e.GitRepo
				gitBranch = e.GitBranch
			}
		}

		newProjects = append(newProjects, ProjectSummary{
			ProjectPath:  originalPath,
			EncodedPath:  encoded,
			SessionCount: len(entries),
			TotalSize:    totalSize,
			LastActivity: lastActivity,
			GitRepo:      gitRepo,
			GitBranch:    gitBranch,
		})
	}

	// Sort projects by last activity
	sort.Slice(newProjects, func(i, j int) bool {
		return newProjects[i].LastActivity.After(newProjects[j].LastActivity)
	})

	c.projectIndex = newProjectIndex
	c.allProjects = newProjects
	c.lastScan = time.Now()

	log.Debug().
		Int("projects", len(newProjects)).
		Dur("duration", time.Since(startTime)).
		Msg("refreshed codex session index cache")

	return nil
}

// parseSessionFileForIndex extracts metadata from a session file for indexing.
// This is optimized to read only what's needed for the index (first ~100 lines).
func parseSessionFileForIndex(path string) (*SessionIndexEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	entry := &SessionIndexEntry{
		FullPath:  path,
		FileMtime: fileInfo.ModTime().UnixMilli(),
		Modified:  fileInfo.ModTime(),
		FileSize:  fileInfo.Size(),
	}

	reader := jsonl.NewReader(file, 0)

	var firstTimestamp time.Time
	var lastTimestamp time.Time
	var firstPrompt string
	var lastMessage string
	messageCount := 0
	lineCount := 0
	maxLines := 1000 // Limit for large files to keep indexing fast

	for lineCount < maxLines {
		line, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		lineCount++

		if line.TooLong || len(line.Data) == 0 {
			continue
		}

		var logEntry struct {
			Timestamp string          `json:"timestamp"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(line.Data, &logEntry); err != nil {
			continue
		}

		// Parse timestamp
		if ts, ok := parseTimestamp(logEntry.Timestamp); ok {
			if firstTimestamp.IsZero() {
				firstTimestamp = ts
			}
			lastTimestamp = ts
		}

		switch logEntry.Type {
		case "session_meta":
			var payload struct {
				ID            string `json:"id"`
				Cwd           string `json:"cwd"`
				ModelProvider string `json:"model_provider"`
				CLIVersion    string `json:"cli_version"`
				Git           struct {
					Branch        string `json:"branch"`
					CommitHash    string `json:"commit_hash"`
					RepositoryURL string `json:"repository_url"`
				} `json:"git"`
			}
			if err := json.Unmarshal(logEntry.Payload, &payload); err == nil {
				if payload.ID != "" {
					entry.SessionID = payload.ID
				}
				if payload.Cwd != "" {
					entry.ProjectPath = payload.Cwd
				}
				entry.ModelProvider = payload.ModelProvider
				entry.CLIVersion = payload.CLIVersion
				entry.GitBranch = payload.Git.Branch
				entry.GitCommit = payload.Git.CommitHash
				entry.GitRepo = payload.Git.RepositoryURL
			}

		case "turn_context":
			var payload struct {
				Cwd   string `json:"cwd"`
				Model string `json:"model"`
			}
			if err := json.Unmarshal(logEntry.Payload, &payload); err == nil {
				if payload.Cwd != "" && entry.ProjectPath == "" {
					entry.ProjectPath = payload.Cwd
				}
				if payload.Model != "" && entry.Model == "" {
					entry.Model = payload.Model
				}
			}

		case "event_msg":
			var payload struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(logEntry.Payload, &payload); err == nil {
				if payload.Type == "user_message" && payload.Message != "" {
					if firstPrompt == "" {
						firstPrompt = truncateString(payload.Message, 200)
					}
					messageCount++
				}
				if payload.Type == "agent_message" && payload.Message != "" {
					lastMessage = payload.Message
				}
			}

		case "response_item":
			var payload struct {
				Type    string          `json:"type"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(logEntry.Payload, &payload); err == nil {
				if payload.Type == "message" {
					content := extractContent(payload.Content)
					if content != "" {
						if payload.Role == "user" {
							if firstPrompt == "" {
								firstPrompt = truncateString(content, 200)
							}
							messageCount++
						} else if payload.Role == "assistant" {
							lastMessage = content
						}
					}
				}
			}
		}
	}

	entry.FirstPrompt = firstPrompt
	entry.Summary = truncateString(lastMessage, 200)
	entry.MessageCount = messageCount
	entry.LineCount = lineCount

	if !firstTimestamp.IsZero() {
		entry.Created = firstTimestamp
	} else {
		entry.Created = entry.Modified
	}

	if !lastTimestamp.IsZero() {
		entry.Modified = lastTimestamp
	}

	return entry, nil
}

// truncateString truncates a string to maxLen and adds "..." if truncated.
func truncateString(s string, maxLen int) string {
	// First, remove newlines and extra whitespace
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Global index cache singleton
var (
	globalIndexCache     *IndexCache
	globalIndexCacheOnce sync.Once
)

// GetGlobalIndexCache returns the global index cache singleton.
func GetGlobalIndexCache() *IndexCache {
	globalIndexCacheOnce.Do(func() {
		globalIndexCache = NewIndexCache("")
	})
	return globalIndexCache
}
