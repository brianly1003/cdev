package message

import "encoding/json"

// Standard JSON-RPC 2.0 error codes.
const (
	// ParseError indicates invalid JSON was received.
	ParseError = -32700

	// InvalidRequest indicates the JSON is not a valid Request object.
	InvalidRequest = -32600

	// MethodNotFound indicates the method does not exist.
	MethodNotFound = -32601

	// InvalidParams indicates invalid method parameters.
	InvalidParams = -32602

	// InternalError indicates an internal JSON-RPC error.
	InternalError = -32603

	// ServerError codes are reserved for implementation-defined server-errors.
	// Range: -32000 to -32099
)

// cdev-specific error codes (-32001 to -32050).
// These are CLI-agnostic and work with Claude, Gemini, Codex, etc.
const (
	// Agent errors (generic for any AI CLI)
	AgentAlreadyRunning = -32001
	AgentNotRunning     = -32002
	AgentError          = -32003
	AgentNotConfigured  = -32004

	// Legacy aliases for backward compatibility
	ClaudeAlreadyRunning = AgentAlreadyRunning
	ClaudeNotRunning     = AgentNotRunning
	ClaudeError          = AgentError

	// Session errors
	SessionNotFound = -32010
	SessionInvalid  = -32011

	// File errors
	FileNotFound      = -32020
	FileTooLarge      = -32021
	PathTraversal     = -32022
	FileReadError     = -32023
	DirectoryNotFound = -32024

	// Git errors
	NotAGitRepo        = -32030
	GitOperationFailed = -32031
	GitConflict        = -32032

	// Repository errors
	IndexNotReady    = -32040
	SearchError      = -32041
	IndexRebuildFail = -32042
)

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.Message
}

// NewError creates a new JSON-RPC error.
func NewError(code int, message string) *Error {
	return &Error{
		Code:    code,
		Message: message,
	}
}

// NewErrorWithData creates a new JSON-RPC error with additional data.
func NewErrorWithData(code int, message string, data interface{}) *Error {
	err := &Error{
		Code:    code,
		Message: message,
	}

	if data != nil {
		if d, e := json.Marshal(data); e == nil {
			err.Data = d
		}
	}

	return err
}

// Standard error constructors.

// ErrParseError creates a parse error.
func ErrParseError(message string) *Error {
	if message == "" {
		message = "Parse error"
	}
	return NewError(ParseError, message)
}

// ErrInvalidRequest creates an invalid request error.
func ErrInvalidRequest(message string) *Error {
	if message == "" {
		message = "Invalid Request"
	}
	return NewError(InvalidRequest, message)
}

// ErrMethodNotFound creates a method not found error.
func ErrMethodNotFound(method string) *Error {
	return NewError(MethodNotFound, "Method not found: "+method)
}

// ErrInvalidParams creates an invalid params error.
func ErrInvalidParams(message string) *Error {
	if message == "" {
		message = "Invalid params"
	}
	return NewError(InvalidParams, message)
}

// ErrInternalError creates an internal error.
func ErrInternalError(message string) *Error {
	if message == "" {
		message = "Internal error"
	}
	return NewError(InternalError, message)
}

// cdev-specific error constructors.

// ErrAgentAlreadyRunning creates an agent already running error.
func ErrAgentAlreadyRunning(agentType string) *Error {
	return NewErrorWithData(AgentAlreadyRunning, "Agent is already running", map[string]string{
		"agent_type": agentType,
	})
}

// ErrAgentNotRunning creates an agent not running error.
func ErrAgentNotRunning(agentType string) *Error {
	return NewErrorWithData(AgentNotRunning, "Agent is not running", map[string]string{
		"agent_type": agentType,
	})
}

// ErrAgentError creates an agent error with message.
func ErrAgentError(agentType, message string) *Error {
	return NewErrorWithData(AgentError, message, map[string]string{
		"agent_type": agentType,
	})
}

// ErrAgentNotConfigured creates an agent not configured error.
func ErrAgentNotConfigured(agentType string) *Error {
	return NewErrorWithData(AgentNotConfigured, "Agent not configured", map[string]string{
		"agent_type": agentType,
	})
}

// Legacy error constructors for backward compatibility.

// ErrClaudeAlreadyRunning creates a claude already running error.
// Deprecated: Use ErrAgentAlreadyRunning instead.
func ErrClaudeAlreadyRunning() *Error {
	return ErrAgentAlreadyRunning("claude")
}

// ErrClaudeNotRunning creates a claude not running error.
// Deprecated: Use ErrAgentNotRunning instead.
func ErrClaudeNotRunning() *Error {
	return ErrAgentNotRunning("claude")
}

// ErrClaudeError creates a claude error with message.
// Deprecated: Use ErrAgentError instead.
func ErrClaudeError(message string) *Error {
	return ErrAgentError("claude", message)
}

// ErrSessionNotFound creates a session not found error.
func ErrSessionNotFound(sessionID string) *Error {
	return NewErrorWithData(SessionNotFound, "Session not found", map[string]string{
		"session_id": sessionID,
	})
}

// ErrFileNotFound creates a file not found error.
func ErrFileNotFound(path string) *Error {
	return NewErrorWithData(FileNotFound, "File not found", map[string]string{
		"path": path,
	})
}

// ErrFileTooLarge creates a file too large error.
func ErrFileTooLarge(path string, size, maxSize int64) *Error {
	return NewErrorWithData(FileTooLarge, "File too large", map[string]interface{}{
		"path":     path,
		"size":     size,
		"max_size": maxSize,
	})
}

// ErrPathTraversal creates a path traversal error.
func ErrPathTraversal(path string) *Error {
	return NewErrorWithData(PathTraversal, "Path traversal not allowed", map[string]string{
		"path": path,
	})
}

// ErrNotAGitRepo creates a not a git repository error.
func ErrNotAGitRepo() *Error {
	return NewError(NotAGitRepo, "Not a git repository")
}

// ErrGitOperationFailed creates a git operation failed error.
func ErrGitOperationFailed(operation, message string) *Error {
	return NewErrorWithData(GitOperationFailed, "Git operation failed: "+message, map[string]string{
		"operation": operation,
	})
}

// ErrorCodeName returns a human-readable name for an error code.
func ErrorCodeName(code int) string {
	switch code {
	case ParseError:
		return "ParseError"
	case InvalidRequest:
		return "InvalidRequest"
	case MethodNotFound:
		return "MethodNotFound"
	case InvalidParams:
		return "InvalidParams"
	case InternalError:
		return "InternalError"
	case AgentAlreadyRunning:
		return "AgentAlreadyRunning"
	case AgentNotRunning:
		return "AgentNotRunning"
	case AgentError:
		return "AgentError"
	case AgentNotConfigured:
		return "AgentNotConfigured"
	case SessionNotFound:
		return "SessionNotFound"
	case SessionInvalid:
		return "SessionInvalid"
	case FileNotFound:
		return "FileNotFound"
	case FileTooLarge:
		return "FileTooLarge"
	case PathTraversal:
		return "PathTraversal"
	case FileReadError:
		return "FileReadError"
	case DirectoryNotFound:
		return "DirectoryNotFound"
	case NotAGitRepo:
		return "NotAGitRepo"
	case GitOperationFailed:
		return "GitOperationFailed"
	case GitConflict:
		return "GitConflict"
	case IndexNotReady:
		return "IndexNotReady"
	case SearchError:
		return "SearchError"
	case IndexRebuildFail:
		return "IndexRebuildFail"
	default:
		return "UnknownError"
	}
}
