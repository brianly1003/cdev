package methods

import (
	"context"
	"encoding/json"

	"github.com/brianly1003/cdev/internal/adapters/git"
	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// GitProvider provides git operations.
type GitProvider interface {
	// Status returns the git status.
	Status(ctx context.Context) (GitStatusInfo, error)

	// Diff returns the diff for a file or all files.
	Diff(ctx context.Context, path string) (string, bool, bool, error)

	// Stage stages files.
	Stage(ctx context.Context, paths []string) error

	// Unstage unstages files.
	Unstage(ctx context.Context, paths []string) error

	// Discard discards changes to files.
	Discard(ctx context.Context, paths []string) error

	// Commit creates a commit and optionally pushes. Returns commit result.
	Commit(ctx context.Context, message string, push bool) (*CommitResult, error)

	// Push pushes to remote. Returns push result.
	Push(ctx context.Context) (*PushResult, error)

	// Pull pulls from remote. Returns pull result.
	Pull(ctx context.Context) (*PullResult, error)

	// Branches returns the list of branches with full info.
	Branches(ctx context.Context) (*BranchesResult, error)

	// Checkout checks out a branch. Returns checkout result.
	Checkout(ctx context.Context, branch string) (*CheckoutResult, error)
}

// GitFileStatus represents a file in git status.
type GitFileStatus struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// GitStatusInfo represents git status information matching HTTP API format.
type GitStatusInfo struct {
	Branch     string          `json:"branch"`
	Upstream   string          `json:"upstream,omitempty"`
	Ahead      int             `json:"ahead"`
	Behind     int             `json:"behind"`
	Staged     []GitFileStatus `json:"staged"`
	Unstaged   []GitFileStatus `json:"unstaged"`
	Untracked  []GitFileStatus `json:"untracked"`
	Conflicted []GitFileStatus `json:"conflicted"`
	RepoName   string          `json:"repo_name"`
	RepoRoot   string          `json:"repo_root"`
}

// BranchInfo represents information about a git branch.
type BranchInfo struct {
	Name    string `json:"name"`
	Current bool   `json:"current"`
}

// GitService provides git-related RPC methods.
type GitService struct {
	provider      GitProvider
	maxDiffSizeKB int
}

// NewGitService creates a new git service.
func NewGitService(provider GitProvider, maxDiffSizeKB int) *GitService {
	return &GitService{
		provider:      provider,
		maxDiffSizeKB: maxDiffSizeKB,
	}
}

// RegisterMethods registers all git methods with the registry.
func (s *GitService) RegisterMethods(r *handler.Registry) {
	pathsSchema := map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}}

	r.RegisterWithMeta("git/status", s.Status, handler.MethodMeta{
		Summary:     "Get git repository status",
		Description: "Returns the current git status including staged, unstaged, and untracked files.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "GitStatusResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/GitStatusResult"}},
		Errors:      []string{"GitError"},
	})

	r.RegisterWithMeta("git/diff", s.Diff, handler.MethodMeta{
		Summary:     "Get file diff",
		Description: "Returns the git diff for a specific file or all files.",
		Params: []handler.OpenRPCParam{
			{Name: "path", Description: "File path relative to repository root (optional, omit for all files)", Required: false, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "GitDiffResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/GitDiffResult"}},
		Errors: []string{"FileNotFound", "GitError"},
	})

	r.RegisterWithMeta("git/stage", s.Stage, handler.MethodMeta{
		Summary:     "Stage files",
		Description: "Stages the specified files for commit.",
		Params: []handler.OpenRPCParam{
			{Name: "paths", Description: "Array of file paths to stage", Required: true, Schema: pathsSchema},
		},
		Result: &handler.OpenRPCResult{Name: "OperationResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/OperationResult"}},
		Errors: []string{"GitError"},
	})

	r.RegisterWithMeta("git/unstage", s.Unstage, handler.MethodMeta{
		Summary:     "Unstage files",
		Description: "Unstages the specified files.",
		Params: []handler.OpenRPCParam{
			{Name: "paths", Description: "Array of file paths to unstage", Required: true, Schema: pathsSchema},
		},
		Result: &handler.OpenRPCResult{Name: "OperationResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/OperationResult"}},
		Errors: []string{"GitError"},
	})

	r.RegisterWithMeta("git/discard", s.Discard, handler.MethodMeta{
		Summary:     "Discard changes",
		Description: "Discards changes to the specified files.",
		Params: []handler.OpenRPCParam{
			{Name: "paths", Description: "Array of file paths to discard", Required: true, Schema: pathsSchema},
		},
		Result: &handler.OpenRPCResult{Name: "OperationResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/OperationResult"}},
		Errors: []string{"GitError"},
	})

	r.RegisterWithMeta("git/commit", s.Commit, handler.MethodMeta{
		Summary:     "Create a commit",
		Description: "Creates a commit with the staged changes.",
		Params: []handler.OpenRPCParam{
			{Name: "message", Description: "Commit message", Required: true, Schema: map[string]interface{}{"type": "string"}},
			{Name: "push", Description: "Push after commit", Required: false, Schema: map[string]interface{}{"type": "boolean", "default": false}},
		},
		Result: &handler.OpenRPCResult{Name: "CommitResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/CommitResult"}},
		Errors: []string{"GitError"},
	})

	r.RegisterWithMeta("git/push", s.Push, handler.MethodMeta{
		Summary:     "Push to remote",
		Description: "Pushes commits to the remote repository.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "OperationResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/OperationResult"}},
		Errors:      []string{"GitError"},
	})

	r.RegisterWithMeta("git/pull", s.Pull, handler.MethodMeta{
		Summary:     "Pull from remote",
		Description: "Pulls changes from the remote repository.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "OperationResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/OperationResult"}},
		Errors:      []string{"GitError"},
	})

	r.RegisterWithMeta("git/branches", s.Branches, handler.MethodMeta{
		Summary:     "List branches",
		Description: "Returns the list of git branches.",
		Params:      []handler.OpenRPCParam{},
		Result:      &handler.OpenRPCResult{Name: "BranchesResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/BranchesResult"}},
		Errors:      []string{"GitError"},
	})

	r.RegisterWithMeta("git/checkout", s.Checkout, handler.MethodMeta{
		Summary:     "Checkout a branch",
		Description: "Checks out the specified branch.",
		Params: []handler.OpenRPCParam{
			{Name: "branch", Description: "Branch name to checkout", Required: true, Schema: map[string]interface{}{"type": "string"}},
		},
		Result: &handler.OpenRPCResult{Name: "CheckoutResult", Schema: map[string]interface{}{"$ref": "#/components/schemas/CheckoutResult"}},
		Errors: []string{"GitError"},
	})
}

// Status returns the git status.
func (s *GitService) Status(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	status, err := s.provider.Status(ctx)
	if err != nil {
		return nil, message.ErrGitOperationFailed("status", err.Error())
	}

	return status, nil
}

// DiffParams for git/diff method.
type DiffParams struct {
	// Path is the file path. If empty, returns diff for all files.
	Path string `json:"path,omitempty"`
}

// DiffResult for git/diff method.
type DiffResult struct {
	Path        string `json:"path,omitempty"`
	Diff        string `json:"diff"`
	IsStaged    bool   `json:"is_staged"`
	IsNew       bool   `json:"is_new"`
	IsTruncated bool   `json:"is_truncated"`
}

// Diff returns the diff for a file or all files.
func (s *GitService) Diff(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	var p DiffParams
	if params != nil {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, message.ErrInvalidParams("invalid params: " + err.Error())
		}
	}

	diff, isStaged, isNew, err := s.provider.Diff(ctx, p.Path)
	if err != nil {
		return nil, message.ErrGitOperationFailed("diff", err.Error())
	}

	diff, truncated := git.TruncateDiff(diff, s.maxDiffSizeKB)
	return DiffResult{
		Path:        p.Path,
		Diff:        diff,
		IsStaged:    isStaged,
		IsNew:       isNew,
		IsTruncated: truncated,
	}, nil
}

// PathsParams for git operations that take paths.
type PathsParams struct {
	Paths []string `json:"paths"`
}

// StageResult for git/stage method - matches HTTP API format.
type StageResult struct {
	Success bool     `json:"success"`
	Staged  []string `json:"staged"`
}

// UnstageResult for git/unstage method - matches HTTP API format.
type UnstageResult struct {
	Success  bool     `json:"success"`
	Unstaged []string `json:"unstaged"`
}

// DiscardResult for git/discard method - matches HTTP API format.
type DiscardResult struct {
	Success   bool     `json:"success"`
	Discarded []string `json:"discarded"`
}

// Stage stages files.
func (s *GitService) Stage(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	var p PathsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if len(p.Paths) == 0 {
		return nil, message.ErrInvalidParams("paths is required")
	}

	if err := s.provider.Stage(ctx, p.Paths); err != nil {
		return nil, message.ErrGitOperationFailed("stage", err.Error())
	}

	return StageResult{
		Success: true,
		Staged:  p.Paths,
	}, nil
}

// Unstage unstages files.
func (s *GitService) Unstage(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	var p PathsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if len(p.Paths) == 0 {
		return nil, message.ErrInvalidParams("paths is required")
	}

	if err := s.provider.Unstage(ctx, p.Paths); err != nil {
		return nil, message.ErrGitOperationFailed("unstage", err.Error())
	}

	return UnstageResult{
		Success:  true,
		Unstaged: p.Paths,
	}, nil
}

// Discard discards changes to files.
func (s *GitService) Discard(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	var p PathsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if len(p.Paths) == 0 {
		return nil, message.ErrInvalidParams("paths is required")
	}

	if err := s.provider.Discard(ctx, p.Paths); err != nil {
		return nil, message.ErrGitOperationFailed("discard", err.Error())
	}

	return DiscardResult{
		Success:   true,
		Discarded: p.Paths,
	}, nil
}

// CommitParams for git/commit method.
type CommitParams struct {
	Message string `json:"message"`
	Push    bool   `json:"push,omitempty"`
}

// CommitResult for git/commit method - matches HTTP API format.
type CommitResult struct {
	Success        bool   `json:"success"`
	SHA            string `json:"sha,omitempty"`
	Message        string `json:"message,omitempty"`
	FilesCommitted int    `json:"files_committed,omitempty"`
	Pushed         bool   `json:"pushed,omitempty"`
	Error          string `json:"error,omitempty"`
}

// PushResult for git/push method - matches HTTP API format.
type PushResult struct {
	Success       bool   `json:"success"`
	Message       string `json:"message,omitempty"`
	CommitsPushed int    `json:"commits_pushed,omitempty"`
	Error         string `json:"error,omitempty"`
}

// PullResult for git/pull method - matches HTTP API format.
type PullResult struct {
	Success         bool     `json:"success"`
	Message         string   `json:"message,omitempty"`
	CommitsPulled   int      `json:"commits_pulled,omitempty"`
	FilesChanged    int      `json:"files_changed,omitempty"`
	ConflictedFiles []string `json:"conflicted_files,omitempty"`
	Error           string   `json:"error,omitempty"`
}

// CheckoutResult for git/checkout method - matches HTTP API format.
type CheckoutResult struct {
	Success bool   `json:"success"`
	Branch  string `json:"branch,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Commit creates a commit.
func (s *GitService) Commit(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	var p CommitParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.Message == "" {
		return nil, message.ErrInvalidParams("message is required")
	}

	result, err := s.provider.Commit(ctx, p.Message, p.Push)
	if err != nil {
		return nil, message.ErrGitOperationFailed("commit", err.Error())
	}

	return result, nil
}

// Push pushes to remote.
func (s *GitService) Push(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	result, err := s.provider.Push(ctx)
	if err != nil {
		return nil, message.ErrGitOperationFailed("push", err.Error())
	}

	return result, nil
}

// Pull pulls from remote.
func (s *GitService) Pull(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	result, err := s.provider.Pull(ctx)
	if err != nil {
		return nil, message.ErrGitOperationFailed("pull", err.Error())
	}

	return result, nil
}

// BranchesResult for git/branches method - matches HTTP API format.
type BranchesResult struct {
	Current  string       `json:"current"`
	Upstream string       `json:"upstream,omitempty"`
	Ahead    int          `json:"ahead"`
	Behind   int          `json:"behind"`
	Branches []BranchInfo `json:"branches"`
}

// Branches returns the list of branches.
func (s *GitService) Branches(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	result, err := s.provider.Branches(ctx)
	if err != nil {
		return nil, message.ErrGitOperationFailed("branches", err.Error())
	}

	return result, nil
}

// CheckoutParams for git/checkout method.
type CheckoutParams struct {
	Branch string `json:"branch"`
	Create bool   `json:"create,omitempty"`
}

// Checkout checks out a branch.
func (s *GitService) Checkout(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	var p CheckoutParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, message.ErrInvalidParams("invalid params: " + err.Error())
	}

	if p.Branch == "" {
		return nil, message.ErrInvalidParams("branch is required")
	}

	result, err := s.provider.Checkout(ctx, p.Branch)
	if err != nil {
		return nil, message.ErrGitOperationFailed("checkout", err.Error())
	}

	return result, nil
}
