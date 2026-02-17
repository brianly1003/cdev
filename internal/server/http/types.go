// Package http implements the HTTP API server for cdev.
package http

import "encoding/json"

// HealthResponse represents the health check response.
type HealthResponse struct {
	Status string `json:"status" example:"ok"`
	Time   string `json:"time" example:"2024-01-15T10:30:00Z"`
}

// StatusResponse represents the agent status response.
type StatusResponse struct {
	SessionID        string `json:"session_id" example:"550e8400-e29b-41d4-a716-446655440000"`       // Claude session ID (for continue) - empty if no active session
	AgentSessionID   string `json:"agent_session_id" example:"01ce425c-5b91-4f8a-b8dd-5d14644c3494"` // Agent instance ID (generated on startup)
	Version          string `json:"version" example:"1.0.0"`
	RepoPath         string `json:"repo_path" example:"/Users/dev/myproject"`
	RepoName         string `json:"repo_name" example:"myproject"`
	UptimeSeconds    int64  `json:"uptime_seconds" example:"3600"`
	ClaudeState      string `json:"claude_state" example:"idle"`
	ConnectedClients int    `json:"connected_clients" example:"1"`
	WatcherEnabled   bool   `json:"watcher_enabled" example:"true"`
	GitEnabled       bool   `json:"git_enabled" example:"true"`
	IsGitRepo        bool   `json:"is_git_repo" example:"true"`
}

// SessionMode defines how to handle conversation sessions.
type SessionMode string

const (
	// SessionModeNew starts a new conversation (default).
	SessionModeNew SessionMode = "new"
	// SessionModeContinue continues a conversation by session ID.
	// Requires session_id parameter.
	SessionModeContinue SessionMode = "continue"
)

// RunClaudeRequest represents the request to start Claude.
type RunClaudeRequest struct {
	Prompt    string      `json:"prompt" example:"Create a hello world function" binding:"required"`
	Mode      SessionMode `json:"mode,omitempty" example:"new" enums:"new,continue"`
	SessionID string      `json:"session_id,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// RunClaudeResponse represents the response after starting Claude.
type RunClaudeResponse struct {
	Status    string `json:"status" example:"started"`
	Prompt    string `json:"prompt" example:"Create a hello world function"`
	PID       int    `json:"pid" example:"12345"`
	SessionID string `json:"session_id,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// SessionInfo represents information about a Claude session.
type SessionInfo struct {
	SessionID    string `json:"session_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Summary      string `json:"summary" example:"Create a hello world function"`
	MessageCount int    `json:"message_count" example:"5"`
	LastUpdated  string `json:"last_updated" example:"2024-01-15T10:30:00Z"`
}

// SessionsResponse represents the list of available sessions.
type SessionsResponse struct {
	Sessions []SessionInfo `json:"sessions"`
	Current  string        `json:"current,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
}

// SessionMessage represents a single message in a session.
type SessionMessage struct {
	Type      string `json:"type" example:"user"`
	Role      string `json:"role" example:"user"`
	Content   string `json:"content" example:"Create a hello world function"`
	Timestamp string `json:"timestamp" example:"2024-01-15T10:30:00Z"`
}

// SessionMessagesResponse represents the messages in a session.
type SessionMessagesResponse struct {
	SessionID string           `json:"session_id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Messages  []SessionMessage `json:"messages"`
	Count     int              `json:"count" example:"10"`
}

// AgentSessionInfo represents information about a session for any agent runtime.
type AgentSessionInfo struct {
	SessionID    string `json:"session_id"`
	AgentType    string `json:"agent_type"`
	Summary      string `json:"summary,omitempty"`
	MessageCount int    `json:"message_count"`
	LastUpdated  string `json:"last_updated"`
	Branch       string `json:"branch,omitempty"`
	ProjectPath  string `json:"project_path,omitempty"`
}

// AgentSessionsResponse represents the list of available sessions for an agent runtime.
type AgentSessionsResponse struct {
	Sessions []AgentSessionInfo `json:"sessions"`
	Current  string             `json:"current,omitempty"`
	Total    int                `json:"total"`
	Limit    int                `json:"limit"`
	Offset   int                `json:"offset"`
}

// AgentSessionMessage represents a single message in a session (raw message format).
type AgentSessionMessage struct {
	Type      string          `json:"type"`
	UUID      string          `json:"uuid,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
	GitBranch string          `json:"git_branch,omitempty"`
	Message   json.RawMessage `json:"message"`
}

// AgentSessionMessagesResponse represents the paginated messages for an agent session.
type AgentSessionMessagesResponse struct {
	SessionID string                `json:"session_id"`
	Messages  []AgentSessionMessage `json:"messages"`
	Total     int                   `json:"total"`
	Limit     int                   `json:"limit"`
	Offset    int                   `json:"offset"`
	HasMore   bool                  `json:"has_more"`
}

// StopClaudeResponse represents the response after stopping Claude.
type StopClaudeResponse struct {
	Status string `json:"status" example:"stopped"`
}

// RespondToClaudeRequest represents the request to respond to Claude.
type RespondToClaudeRequest struct {
	ToolUseID string `json:"tool_use_id" example:"toolu_abc123" binding:"required"`
	Response  string `json:"response" example:"Yes, proceed with the changes"`
	IsError   bool   `json:"is_error" example:"false"`
}

// RespondToClaudeResponse represents the response after sending response to Claude.
type RespondToClaudeResponse struct {
	Status    string `json:"status" example:"sent"`
	ToolUseID string `json:"tool_use_id" example:"toolu_abc123"`
}

// FileContentResponse represents the file content response.
type FileContentResponse struct {
	Path      string `json:"path" example:"src/main.ts"`
	Content   string `json:"content" example:"console.log('hello');"`
	Encoding  string `json:"encoding" example:"utf-8"`
	Truncated bool   `json:"truncated" example:"false"`
	Size      int    `json:"size" example:"25"`
}

// GitStatusResponse represents the git status response.
type GitStatusResponse struct {
	Files    []GitFileStatus `json:"files"`
	RepoRoot string          `json:"repo_root" example:"/Users/dev/myproject"`
	RepoName string          `json:"repo_name" example:"myproject"`
}

// GitFileStatus represents the status of a single file in git.
type GitFileStatus struct {
	Path        string `json:"path" example:"src/main.ts"`
	Status      string `json:"status" example:"M "`
	IsStaged    bool   `json:"is_staged" example:"true"`
	IsUntracked bool   `json:"is_untracked" example:"false"`
}

// GitDiffResponse represents the git diff response for a single file.
type GitDiffResponse struct {
	Path      string `json:"path" example:"src/main.ts"`
	Diff      string `json:"diff" example:"@@ -1,3 +1,4 @@\n+new line"`
	Truncated bool   `json:"is_truncated,omitempty" example:"false"`
}

// GitDiffAllResponse represents the git diff response for all files.
type GitDiffAllResponse struct {
	Diffs          []GitDiffItem `json:"diffs"`
	TruncatedPaths []string      `json:"truncated_paths,omitempty"`
}

// GitDiffItem represents a single file diff.
type GitDiffItem struct {
	Path      string `json:"path" example:"src/main.ts"`
	Diff      string `json:"diff" example:"@@ -1,3 +1,4 @@\n+new line"`
	Truncated bool   `json:"is_truncated,omitempty" example:"false"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error string `json:"error" example:"Something went wrong"`
	Path  string `json:"path,omitempty" example:"src/main.ts"`
}

// --- Enhanced Git API Types ---

// GitEnhancedStatusResponse represents the enhanced git status response.
type GitEnhancedStatusResponse struct {
	Branch     string         `json:"branch" example:"main"`
	Upstream   string         `json:"upstream,omitempty" example:"origin/main"`
	Ahead      int            `json:"ahead" example:"2"`
	Behind     int            `json:"behind" example:"0"`
	Staged     []GitFileEntry `json:"staged"`
	Unstaged   []GitFileEntry `json:"unstaged"`
	Untracked  []GitFileEntry `json:"untracked"`
	Conflicted []GitFileEntry `json:"conflicted"`
	RepoName   string         `json:"repo_name" example:"myproject"`
	RepoRoot   string         `json:"repo_root" example:"/Users/dev/myproject"`
}

// GitFileEntry represents a file entry with diff stats.
type GitFileEntry struct {
	Path      string `json:"path" example:"src/main.ts"`
	Status    string `json:"status" example:"M"`
	Additions int    `json:"additions,omitempty" example:"10"`
	Deletions int    `json:"deletions,omitempty" example:"5"`
}

// GitStageRequest represents the request to stage files.
type GitStageRequest struct {
	Paths []string `json:"paths" example:"[\"src/main.ts\", \"src/utils.ts\"]"`
}

// GitStageResponse represents the response after staging files.
type GitStageResponse struct {
	Success bool     `json:"success" example:"true"`
	Staged  []string `json:"staged" example:"[\"src/main.ts\"]"`
	Error   string   `json:"error,omitempty"`
}

// GitUnstageRequest represents the request to unstage files.
type GitUnstageRequest struct {
	Paths []string `json:"paths" example:"[\"src/main.ts\"]"`
}

// GitUnstageResponse represents the response after unstaging files.
type GitUnstageResponse struct {
	Success  bool     `json:"success" example:"true"`
	Unstaged []string `json:"unstaged" example:"[\"src/main.ts\"]"`
	Error    string   `json:"error,omitempty"`
}

// GitDiscardRequest represents the request to discard changes.
type GitDiscardRequest struct {
	Paths []string `json:"paths" example:"[\"src/main.ts\"]"`
}

// GitDiscardResponse represents the response after discarding changes.
type GitDiscardResponse struct {
	Success   bool     `json:"success" example:"true"`
	Discarded []string `json:"discarded" example:"[\"src/main.ts\"]"`
	Error     string   `json:"error,omitempty"`
}

// GitCommitRequest represents the request to commit changes.
type GitCommitRequest struct {
	Message string `json:"message" example:"feat: add new feature" binding:"required"`
	Push    bool   `json:"push,omitempty" example:"false"`
}

// GitCommitResponse represents the response after committing.
type GitCommitResponse struct {
	Success        bool   `json:"success" example:"true"`
	SHA            string `json:"sha,omitempty" example:"abc123def"`
	Message        string `json:"message,omitempty" example:"feat: add new feature"`
	FilesCommitted int    `json:"files_committed,omitempty" example:"3"`
	Pushed         bool   `json:"pushed,omitempty" example:"false"`
	Error          string `json:"error,omitempty"`
}

// GitPushResponse represents the response after pushing.
type GitPushResponse struct {
	Success       bool   `json:"success" example:"true"`
	Message       string `json:"message,omitempty" example:"Pushed 2 commits to origin/main"`
	CommitsPushed int    `json:"commits_pushed,omitempty" example:"2"`
	Error         string `json:"error,omitempty"`
}

// GitPullResponse represents the response after pulling.
type GitPullResponse struct {
	Success         bool     `json:"success" example:"true"`
	Message         string   `json:"message,omitempty" example:"Pulled 3 commits"`
	CommitsPulled   int      `json:"commits_pulled,omitempty" example:"3"`
	FilesChanged    int      `json:"files_changed,omitempty" example:"5"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// GitBranchInfo represents information about a branch.
type GitBranchInfo struct {
	Name      string `json:"name" example:"feature-branch"`
	IsCurrent bool   `json:"is_current" example:"false"`
	IsRemote  bool   `json:"is_remote" example:"false"`
	Upstream  string `json:"upstream,omitempty" example:"origin/feature-branch"`
	Ahead     int    `json:"ahead,omitempty" example:"2"`
	Behind    int    `json:"behind,omitempty" example:"1"`
}

// GitBranchesResponse represents the response for listing branches.
type GitBranchesResponse struct {
	Current  string          `json:"current" example:"main"`
	Upstream string          `json:"upstream,omitempty" example:"origin/main"`
	Ahead    int             `json:"ahead" example:"0"`
	Behind   int             `json:"behind" example:"0"`
	Branches []GitBranchInfo `json:"branches"`
}

// GitCheckoutRequest represents the request to checkout a branch.
type GitCheckoutRequest struct {
	Branch string `json:"branch" example:"feature-branch" binding:"required"`
	Create bool   `json:"create,omitempty" example:"false"`
}

// GitCheckoutResponse represents the response after checkout.
type GitCheckoutResponse struct {
	Success bool   `json:"success" example:"true"`
	Branch  string `json:"branch,omitempty" example:"feature-branch"`
	Message string `json:"message,omitempty" example:"Switched to branch 'feature-branch'"`
	Error   string `json:"error,omitempty"`
}

// --- File Explorer API Types ---

// FileEntry represents a file or directory entry.
type FileEntry struct {
	Name          string  `json:"name" example:"main.go"`
	Type          string  `json:"type" example:"file"` // "file" or "directory"
	Size          *int64  `json:"size,omitempty" example:"1024"`
	Modified      *string `json:"modified,omitempty" example:"2025-01-15T10:30:00Z"`
	ChildrenCount *int    `json:"children_count,omitempty" example:"5"`
}

// DirectoryListingResponse represents the response for listing a directory.
type DirectoryListingResponse struct {
	Path       string      `json:"path" example:"src/components"`
	Entries    []FileEntry `json:"entries"`
	TotalCount int         `json:"total_count" example:"10"`
}
