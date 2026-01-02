// Package permission implements the Permission Hook Bridge for handling
// permission requests from external Claude Code instances via Claude's hook system.
package permission

import (
	"time"
)

// Decision represents the user's decision for a permission request.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// Scope represents the scope of a permission decision.
type Scope string

const (
	ScopeOnce    Scope = "once"    // Allow/deny just this one request
	ScopeSession Scope = "session" // Allow/deny for the rest of the session
	ScopePath    Scope = "path"    // Allow for this path pattern (persisted to settings)
)

// HookInput represents the JSON input from Claude Code's hook system.
// This is what Claude sends to the hook command via stdin.
type HookInput struct {
	SessionID      string                 `json:"session_id"`
	TranscriptPath string                 `json:"transcript_path"`
	Cwd            string                 `json:"cwd"`
	PermissionMode string                 `json:"permission_mode"`
	HookEventName  string                 `json:"hook_event_name"`
	ToolName       string                 `json:"tool_name"`
	ToolInput      map[string]interface{} `json:"tool_input"`
	ToolUseID      string                 `json:"tool_use_id"`
}

// HookOutput represents the JSON output to Claude Code's hook system.
// This is what the hook command returns to Claude via stdout.
type HookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
}

// HookSpecificOutput contains the permission-specific response.
// Uses the new permissionDecision format expected by Claude Code.
type HookSpecificOutput struct {
	HookEventName            string                 `json:"hookEventName"`
	PermissionDecision       string                 `json:"permissionDecision"`                 // "allow", "deny", or "ask"
	PermissionDecisionReason string                 `json:"permissionDecisionReason,omitempty"` // Explanation shown to user
	UpdatedInput             map[string]interface{} `json:"updatedInput,omitempty"`             // Optional modified input
}

// Request represents a permission request being tracked by cdev.
type Request struct {
	ID            string                 `json:"id"`             // Unique request ID (typically tool_use_id)
	SessionID     string                 `json:"session_id"`     // Claude session ID
	WorkspaceID   string                 `json:"workspace_id"`   // cdev workspace ID (derived from cwd)
	ToolName      string                 `json:"tool_name"`      // e.g., "Bash", "Write", "Edit"
	ToolInput     map[string]interface{} `json:"tool_input"`     // The tool's input parameters
	ToolUseID     string                 `json:"tool_use_id"`    // Claude's tool use ID
	CreatedAt     time.Time              `json:"created_at"`     // When the request was received
	ResponseChan  chan *Response         `json:"-"`              // Channel to receive response (not serialized)
}

// Response represents a response to a permission request.
type Response struct {
	Decision     Decision               `json:"decision"`                // allow or deny
	Scope        Scope                  `json:"scope"`                   // once, session, or path
	Pattern      string                 `json:"pattern,omitempty"`       // Pattern for session/path scope
	UpdatedInput map[string]interface{} `json:"updated_input,omitempty"` // Optional modified input
	Message      string                 `json:"message,omitempty"`       // Optional message
	Interrupt    bool                   `json:"interrupt,omitempty"`     // If true, interrupt Claude
}

// StoredDecision represents a decision stored in session memory.
type StoredDecision struct {
	Pattern    string    `json:"pattern"`
	Decision   Decision  `json:"decision"`
	CreatedAt  time.Time `json:"created_at"`
	UsageCount int       `json:"usage_count"` // How many times this pattern was matched
}

// SessionMemory holds permission decisions for a single Claude session.
type SessionMemory struct {
	SessionID   string                    `json:"session_id"`
	WorkspaceID string                    `json:"workspace_id"`
	Decisions   map[string]StoredDecision `json:"decisions"` // pattern -> decision
	CreatedAt   time.Time                 `json:"created_at"`
	LastAccess  time.Time                 `json:"last_access"`
}

// PermissionEvent represents the event sent to mobile app for permission prompts.
type PermissionEvent struct {
	ToolUseID   string            `json:"tool_use_id"`
	Type        string            `json:"type"`        // "bash_command", "write_file", "edit_file"
	Target      string            `json:"target"`      // filename or command
	Description string            `json:"description"` // human-readable description
	Preview     string            `json:"preview"`     // content preview
	Options     []PermissionOption `json:"options"`     // available choices
	SessionID   string            `json:"session_id"`
	WorkspaceID string            `json:"workspace_id"`
}

// PermissionOption represents a choice in a permission prompt.
type PermissionOption struct {
	Key         string `json:"key"`                   // "allow_once", "allow_session", "deny"
	Label       string `json:"label"`                 // "Allow Once", "Allow for Session"
	Description string `json:"description,omitempty"` // optional extra info
}

// Config holds configuration for the permission system.
type Config struct {
	SessionMemory SessionMemoryConfig `mapstructure:"session_memory"`
}

// SessionMemoryConfig holds configuration for session memory.
type SessionMemoryConfig struct {
	Enabled     bool          `mapstructure:"enabled"`
	TTL         time.Duration `mapstructure:"ttl"`          // Idle timeout for session memory
	MaxPatterns int           `mapstructure:"max_patterns"` // Max patterns per session
}

// DefaultConfig returns the default permission configuration.
func DefaultConfig() Config {
	return Config{
		SessionMemory: SessionMemoryConfig{
			Enabled:     true,
			TTL:         time.Hour,     // 1 hour idle timeout
			MaxPatterns: 100,           // Max 100 patterns per session
		},
	}
}
