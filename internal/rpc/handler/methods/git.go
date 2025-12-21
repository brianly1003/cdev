package methods

import (
	"context"
	"encoding/json"

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

	// Commit creates a commit.
	Commit(ctx context.Context, message string) (string, error)

	// Push pushes to remote.
	Push(ctx context.Context) error

	// Pull pulls from remote.
	Pull(ctx context.Context) error

	// Branches returns the list of branches.
	Branches(ctx context.Context) ([]BranchInfo, error)

	// Checkout checks out a branch.
	Checkout(ctx context.Context, branch string) error
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
	provider GitProvider
}

// NewGitService creates a new git service.
func NewGitService(provider GitProvider) *GitService {
	return &GitService{provider: provider}
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
	Path     string `json:"path,omitempty"`
	Diff     string `json:"diff"`
	IsStaged bool   `json:"is_staged"`
	IsNew    bool   `json:"is_new"`
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

	return DiffResult{
		Path:     p.Path,
		Diff:     diff,
		IsStaged: isStaged,
		IsNew:    isNew,
	}, nil
}

// PathsParams for git operations that take paths.
type PathsParams struct {
	Paths []string `json:"paths"`
}

// OperationResult for simple git operations.
type OperationResult struct {
	Status        string `json:"status"`
	FilesAffected int    `json:"files_affected"`
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

	return OperationResult{
		Status:        "staged",
		FilesAffected: len(p.Paths),
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

	return OperationResult{
		Status:        "unstaged",
		FilesAffected: len(p.Paths),
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

	return OperationResult{
		Status:        "discarded",
		FilesAffected: len(p.Paths),
	}, nil
}

// CommitParams for git/commit method.
type CommitParams struct {
	Message string `json:"message"`
	Push    bool   `json:"push,omitempty"`
}

// CommitResult for git/commit method.
type CommitResult struct {
	Status string `json:"status"`
	SHA    string `json:"sha"`
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

	sha, err := s.provider.Commit(ctx, p.Message)
	if err != nil {
		return nil, message.ErrGitOperationFailed("commit", err.Error())
	}

	// Push if requested
	if p.Push {
		if err := s.provider.Push(ctx); err != nil {
			return nil, message.ErrGitOperationFailed("push", err.Error())
		}
	}

	return CommitResult{
		Status: "committed",
		SHA:    sha,
	}, nil
}

// Push pushes to remote.
func (s *GitService) Push(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	if err := s.provider.Push(ctx); err != nil {
		return nil, message.ErrGitOperationFailed("push", err.Error())
	}

	return OperationResult{Status: "pushed"}, nil
}

// Pull pulls from remote.
func (s *GitService) Pull(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	if err := s.provider.Pull(ctx); err != nil {
		return nil, message.ErrGitOperationFailed("pull", err.Error())
	}

	return OperationResult{Status: "pulled"}, nil
}

// BranchesResult for git/branches method.
type BranchesResult struct {
	Branches []BranchInfo `json:"branches"`
	Current  string       `json:"current"`
}

// Branches returns the list of branches.
func (s *GitService) Branches(ctx context.Context, params json.RawMessage) (interface{}, *message.Error) {
	if s.provider == nil {
		return nil, message.ErrNotAGitRepo()
	}

	branches, err := s.provider.Branches(ctx)
	if err != nil {
		return nil, message.ErrGitOperationFailed("branches", err.Error())
	}

	// Find current branch
	current := ""
	for _, b := range branches {
		if b.Current {
			current = b.Name
			break
		}
	}

	return BranchesResult{
		Branches: branches,
		Current:  current,
	}, nil
}

// CheckoutParams for git/checkout method.
type CheckoutParams struct {
	Branch string `json:"branch"`
}

// CheckoutResult for git/checkout method.
type CheckoutResult struct {
	Status string `json:"status"`
	Branch string `json:"branch"`
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

	if err := s.provider.Checkout(ctx, p.Branch); err != nil {
		return nil, message.ErrGitOperationFailed("checkout", err.Error())
	}

	return CheckoutResult{
		Status: "checked_out",
		Branch: p.Branch,
	}, nil
}
