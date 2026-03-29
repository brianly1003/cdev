// Package task defines the AgentTask domain model for autonomous code-fixing tasks.
// Tasks are created from webhooks (LazyAdmin) or manually, then executed by spawning
// AI coding agents (Claude Code, Codex, etc.) in isolated git worktrees.
package task

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TaskType classifies what kind of work the agent should perform.
type TaskType string

const (
	TaskTypeFixIssue        TaskType = "fix-issue"
	TaskTypeFixReplay       TaskType = "fix-replay"
	TaskTypeImplementCR     TaskType = "implement-cr"
	TaskTypeAddTest         TaskType = "add-test"
	TaskTypeRefactor        TaskType = "refactor"
	TaskTypeAutoFix         TaskType = "auto-fix"
	TaskTypePlanCase TaskType = "plan-case"
)

// Severity indicates the urgency of the task.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// AgentTask represents an autonomous coding task to be executed by an AI agent.
type AgentTask struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	CaseID       *int            `json:"case_id,omitempty"`
	TaskType     TaskType        `json:"task_type"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Severity     Severity        `json:"severity"`
	Labels       []string        `json:"labels,omitempty"`
	Status       Status          `json:"status"`
	Assignee     string          `json:"assignee"`
	TaskYAML     string          `json:"task_yaml,omitempty"`
	Trigger      *Trigger        `json:"trigger,omitempty"`
	Anchors      *Anchors        `json:"anchors,omitempty"`
	Policy       *Policy         `json:"policy,omitempty"`
	Result       *Result         `json:"result,omitempty"`
	Timeline     []Event         `json:"timeline,omitempty"`
	SessionID    string          `json:"session_id,omitempty"`
	BranchName   string          `json:"branch_name,omitempty"`
	WorktreePath string          `json:"worktree_path,omitempty"`
	Origin       *Origin         `json:"origin,omitempty"`
	CaseContext  json.RawMessage `json:"case_context,omitempty"`
	Prompt       string          `json:"prompt,omitempty"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	CompletedAt  *time.Time      `json:"completed_at,omitempty"`
}

// IsPlanCase returns true if this task is a plan_case trigger.
func (t *AgentTask) IsPlanCase() bool {
	return t.Trigger != nil && t.Trigger.Type == "plan_case"
}

// PlanCasePolicy returns the hardcoded policy for plan_case tasks.
// These values are not overridable by the caller.
func PlanCasePolicy() *Policy {
	return &Policy{
		MaxFilesChanged: 0,
		MaxRounds:       1,
		MaxDurationMins: 30,
		MustPassTests:   false,
		MustPassBuild:   false,
		Autonomy:        "bounded-auto",
		RequireApproval: []string{},
		AgentType:       "claude",
	}
}

// Origin tracks where the task came from (LazyAdmin, CLI, etc.)
// and provides callback credentials for status updates.
// APIKey is excluded from JSON serialization to prevent leakage via REST API.
type Origin struct {
	System string `json:"system"`            // "lazyadmin", "cli", "cdev-ios"
	TaskID int    `json:"task_id,omitempty"` // External task ID in the origin system
	CaseID *int   `json:"case_id,omitempty"` // External case ID if applicable
	URL    string `json:"url,omitempty"`     // Callback URL (e.g., LazyAdmin base URL)
	APIKey string `json:"-"`                 // API key for callbacks (never serialized to REST responses)
}

// NewTask creates a new AgentTask with a generated ID and pending status.
func NewTask(workspaceID string, taskType TaskType, title, description string) *AgentTask {
	now := time.Now().UTC()
	return &AgentTask{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		TaskType:    taskType,
		Title:       title,
		Description: description,
		Severity:    SeverityMedium,
		Status:      StatusPending,
		Assignee:    "cdev",
		Labels:      []string{},
		Timeline:    []Event{},
		CreatedBy:   "system",
		CreatedAt:   now,
	}
}

// Trigger describes what initiated the task (webhook, manual, schedule).
type Trigger struct {
	Type      string `json:"type"`                // "webhook", "manual", "schedule", "regression"
	Source    string `json:"source,omitempty"`     // "lazyadmin", "cdev-ios", "cli"
	Ref       string `json:"ref,omitempty"`        // External reference (case ID, webhook ID)
	Hash      string `json:"hash,omitempty"`       // HMAC hash for verification
	Timestamp time.Time `json:"timestamp"`
}

// Anchors provides code location hints for the agent.
type Anchors struct {
	Files    []string `json:"files,omitempty"`
	Methods  []string `json:"methods,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
}

// Policy defines execution constraints for the task.
type Policy struct {
	MaxFilesChanged  int      `json:"max_files_changed"`
	MaxRounds        int      `json:"max_rounds"`
	MaxDurationMins  int      `json:"max_duration_mins"`
	MustPassTests    bool     `json:"must_pass_tests"`
	MustPassBuild    bool     `json:"must_pass_build"`
	Autonomy         string   `json:"autonomy"`          // "supervised", "semi-auto", "full-auto-bounded"
	RequireApproval  []string `json:"require_approval"`   // ["git-push", "file-delete"]
	AgentType        string   `json:"agent_type"`         // "claude", "codex", "gemini"
}

// DefaultPolicy returns sensible defaults for task execution.
func DefaultPolicy() *Policy {
	return &Policy{
		MaxFilesChanged: 10,
		MaxRounds:       3,
		MaxDurationMins: 30,
		MustPassTests:   true,
		MustPassBuild:   true,
		Autonomy:        "supervised",
		RequireApproval: []string{"git-push"},
		AgentType:       "claude",
	}
}

// Result contains the outcome of task execution.
type Result struct {
	VerdictStatus  string       `json:"verdict_status"`   // "converged", "stuck", "max_rounds", "failed"
	VerdictSummary string       `json:"verdict_summary"`
	DiffContent    string       `json:"diff_content,omitempty"`
	FilesChanged   []FileChange `json:"files_changed,omitempty"`
	RoundsCompleted int         `json:"rounds_completed"`
	TestsPassed    bool         `json:"tests_passed"`
	BuildPassed    bool         `json:"build_passed"`
	PRUrl          string       `json:"pr_url,omitempty"`
}

// FileChange records a single file modification.
type FileChange struct {
	Path         string `json:"path"`
	LinesAdded   int    `json:"lines_added"`
	LinesRemoved int    `json:"lines_removed"`
}

// Event represents a timeline entry for task lifecycle tracking.
type Event struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`    // "status_change", "log", "error", "revision"
	Message   string    `json:"message"`
	Actor     string    `json:"actor"`   // "system", "agent", "user"
	Data      json.RawMessage `json:"data,omitempty"`
}

// NewTimelineEvent creates a new timeline event.
func NewTimelineEvent(eventType, message, actor string) Event {
	return Event{
		Timestamp: time.Now().UTC(),
		Type:      eventType,
		Message:   message,
		Actor:     actor,
	}
}

// AddTimelineEvent appends an event to the task's timeline.
func (t *AgentTask) AddTimelineEvent(eventType, message, actor string) {
	t.Timeline = append(t.Timeline, NewTimelineEvent(eventType, message, actor))
}

// Revision represents a human feedback loop on a task.
type Revision struct {
	ID            string    `json:"id"`
	TaskID        string    `json:"task_id"`
	RevisionNo    int       `json:"revision_no"`
	Feedback      string    `json:"feedback"`
	ResultSummary string    `json:"result_summary,omitempty"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
}
