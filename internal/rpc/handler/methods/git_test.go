package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/handler"
	"github.com/brianly1003/cdev/internal/rpc/message"
)

// --- Mock GitProvider ---

type mockGitProvider struct {
	statusResult   GitStatusInfo
	statusErr      error
	diffResult     string
	diffIsStaged   bool
	diffIsNew      bool
	diffErr        error
	stageErr       error
	unstageErr     error
	discardErr     error
	commitResult   *CommitResult
	commitErr      error
	pushResult     *PushResult
	pushErr        error
	pullResult     *PullResult
	pullErr        error
	branchesResult *BranchesResult
	branchesErr    error
	checkoutResult *CheckoutResult
	checkoutErr    error

	// Capture calls
	stageCalled    []string
	unstageCalled  []string
	discardCalled  []string
	commitMessage  string
	commitPush     bool
	checkoutBranch string
}

func (m *mockGitProvider) Status(ctx context.Context) (GitStatusInfo, error) {
	return m.statusResult, m.statusErr
}

func (m *mockGitProvider) Diff(ctx context.Context, path string) (string, bool, bool, error) {
	return m.diffResult, m.diffIsStaged, m.diffIsNew, m.diffErr
}

func (m *mockGitProvider) Stage(ctx context.Context, paths []string) error {
	m.stageCalled = paths
	return m.stageErr
}

func (m *mockGitProvider) Unstage(ctx context.Context, paths []string) error {
	m.unstageCalled = paths
	return m.unstageErr
}

func (m *mockGitProvider) Discard(ctx context.Context, paths []string) error {
	m.discardCalled = paths
	return m.discardErr
}

func (m *mockGitProvider) Commit(ctx context.Context, message string, push bool) (*CommitResult, error) {
	m.commitMessage = message
	m.commitPush = push
	return m.commitResult, m.commitErr
}

func (m *mockGitProvider) Push(ctx context.Context) (*PushResult, error) {
	return m.pushResult, m.pushErr
}

func (m *mockGitProvider) Pull(ctx context.Context) (*PullResult, error) {
	return m.pullResult, m.pullErr
}

func (m *mockGitProvider) Branches(ctx context.Context) (*BranchesResult, error) {
	return m.branchesResult, m.branchesErr
}

func (m *mockGitProvider) Checkout(ctx context.Context, branch string) (*CheckoutResult, error) {
	m.checkoutBranch = branch
	return m.checkoutResult, m.checkoutErr
}

// --- Git Service Tests ---

func TestNewGitService(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	if svc == nil {
		t.Fatal("NewGitService returned nil")
	}
	if svc.provider != provider {
		t.Error("provider not set correctly")
	}
}

func TestGitService_RegisterMethods(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)
	registry := handler.NewRegistry()

	svc.RegisterMethods(registry)

	expectedMethods := []string{
		"git/status",
		"git/diff",
		"git/stage",
		"git/unstage",
		"git/discard",
		"git/commit",
		"git/push",
		"git/pull",
		"git/branches",
		"git/checkout",
	}

	for _, method := range expectedMethods {
		if !registry.Has(method) {
			t.Errorf("method %s not registered", method)
		}
	}
}

func TestGitService_Status_Success(t *testing.T) {
	provider := &mockGitProvider{
		statusResult: GitStatusInfo{
			Branch:   "main",
			Upstream: "origin/main",
			Ahead:    1,
			Behind:   0,
			Staged:   []GitFileStatus{{Path: "file.go", Status: "M"}},
			Unstaged: []GitFileStatus{},
		},
	}
	svc := NewGitService(provider)

	result, err := svc.Status(context.Background(), nil)

	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}

	status, ok := result.(GitStatusInfo)
	if !ok {
		t.Fatal("result is not GitStatusInfo")
	}

	if status.Branch != "main" {
		t.Errorf("Branch = %s, want main", status.Branch)
	}
	if len(status.Staged) != 1 {
		t.Errorf("Staged length = %d, want 1", len(status.Staged))
	}
}

func TestGitService_Status_NoProvider(t *testing.T) {
	svc := NewGitService(nil)

	_, err := svc.Status(context.Background(), nil)

	if err == nil {
		t.Fatal("expected error for nil provider")
	}
	if err.Code != message.NotAGitRepo {
		t.Errorf("error code = %d, want %d", err.Code, message.NotAGitRepo)
	}
}

func TestGitService_Status_Error(t *testing.T) {
	provider := &mockGitProvider{
		statusErr: fmt.Errorf("git status failed"),
	}
	svc := NewGitService(provider)

	_, err := svc.Status(context.Background(), nil)

	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != message.GitOperationFailed {
		t.Errorf("error code = %d, want %d", err.Code, message.GitOperationFailed)
	}
}

func TestGitService_Diff_Success(t *testing.T) {
	provider := &mockGitProvider{
		diffResult:   "diff content here",
		diffIsStaged: true,
		diffIsNew:    false,
	}
	svc := NewGitService(provider)

	params, _ := json.Marshal(DiffParams{Path: "file.go"})
	result, err := svc.Diff(context.Background(), params)

	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

	diff, ok := result.(DiffResult)
	if !ok {
		t.Fatal("result is not DiffResult")
	}

	if diff.Diff != "diff content here" {
		t.Errorf("Diff = %s, want 'diff content here'", diff.Diff)
	}
	if !diff.IsStaged {
		t.Error("IsStaged = false, want true")
	}
}

func TestGitService_Diff_InvalidParams(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	// Invalid JSON
	_, err := svc.Diff(context.Background(), []byte(`{invalid`))

	if err == nil {
		t.Fatal("expected error for invalid params")
	}
	if err.Code != message.InvalidParams {
		t.Errorf("error code = %d, want %d", err.Code, message.InvalidParams)
	}
}

func TestGitService_Stage_Success(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	params, _ := json.Marshal(PathsParams{Paths: []string{"file1.go", "file2.go"}})
	result, err := svc.Stage(context.Background(), params)

	if err != nil {
		t.Fatalf("Stage() error = %v", err)
	}

	stage, ok := result.(StageResult)
	if !ok {
		t.Fatal("result is not StageResult")
	}

	if !stage.Success {
		t.Error("Success = false, want true")
	}
	if len(stage.Staged) != 2 {
		t.Errorf("Staged length = %d, want 2", len(stage.Staged))
	}
	if len(provider.stageCalled) != 2 {
		t.Errorf("provider.stageCalled length = %d, want 2", len(provider.stageCalled))
	}
}

func TestGitService_Stage_EmptyPaths(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	params, _ := json.Marshal(PathsParams{Paths: []string{}})
	_, err := svc.Stage(context.Background(), params)

	if err == nil {
		t.Fatal("expected error for empty paths")
	}
	if err.Code != message.InvalidParams {
		t.Errorf("error code = %d, want %d", err.Code, message.InvalidParams)
	}
}

func TestGitService_Unstage_Success(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	params, _ := json.Marshal(PathsParams{Paths: []string{"file.go"}})
	result, err := svc.Unstage(context.Background(), params)

	if err != nil {
		t.Fatalf("Unstage() error = %v", err)
	}

	unstage, ok := result.(UnstageResult)
	if !ok {
		t.Fatal("result is not UnstageResult")
	}

	if !unstage.Success {
		t.Error("Success = false, want true")
	}
}

func TestGitService_Discard_Success(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	params, _ := json.Marshal(PathsParams{Paths: []string{"file.go"}})
	result, err := svc.Discard(context.Background(), params)

	if err != nil {
		t.Fatalf("Discard() error = %v", err)
	}

	discard, ok := result.(DiscardResult)
	if !ok {
		t.Fatal("result is not DiscardResult")
	}

	if !discard.Success {
		t.Error("Success = false, want true")
	}
}

func TestGitService_Commit_Success(t *testing.T) {
	provider := &mockGitProvider{
		commitResult: &CommitResult{
			Success:        true,
			SHA:            "abc1234",
			Message:        "test commit",
			FilesCommitted: 2,
		},
	}
	svc := NewGitService(provider)

	params, _ := json.Marshal(CommitParams{Message: "test commit", Push: false})
	result, err := svc.Commit(context.Background(), params)

	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}

	commit, ok := result.(*CommitResult)
	if !ok {
		t.Fatal("result is not *CommitResult")
	}

	if !commit.Success {
		t.Error("Success = false, want true")
	}
	if commit.SHA != "abc1234" {
		t.Errorf("SHA = %s, want abc1234", commit.SHA)
	}
	if provider.commitMessage != "test commit" {
		t.Errorf("commitMessage = %s, want 'test commit'", provider.commitMessage)
	}
}

func TestGitService_Commit_EmptyMessage(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	params, _ := json.Marshal(CommitParams{Message: ""})
	_, err := svc.Commit(context.Background(), params)

	if err == nil {
		t.Error("expected error for empty message")
	}
}

func TestGitService_Push_Success(t *testing.T) {
	provider := &mockGitProvider{
		pushResult: &PushResult{
			Success:       true,
			Message:       "pushed successfully",
			CommitsPushed: 3,
		},
	}
	svc := NewGitService(provider)

	result, err := svc.Push(context.Background(), nil)

	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	push, ok := result.(*PushResult)
	if !ok {
		t.Fatal("result is not *PushResult")
	}

	if !push.Success {
		t.Error("Success = false, want true")
	}
}

func TestGitService_Pull_Success(t *testing.T) {
	provider := &mockGitProvider{
		pullResult: &PullResult{
			Success: true,
			Message: "pulled successfully",
		},
	}
	svc := NewGitService(provider)

	result, err := svc.Pull(context.Background(), nil)

	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	pull, ok := result.(*PullResult)
	if !ok {
		t.Fatal("result is not *PullResult")
	}

	if !pull.Success {
		t.Error("Success = false, want true")
	}
}

func TestGitService_Branches_Success(t *testing.T) {
	provider := &mockGitProvider{
		branchesResult: &BranchesResult{
			Current:  "main",
			Upstream: "origin/main",
			Branches: []BranchInfo{
				{Name: "main", Current: true},
				{Name: "develop", Current: false},
			},
		},
	}
	svc := NewGitService(provider)

	result, err := svc.Branches(context.Background(), nil)

	if err != nil {
		t.Fatalf("Branches() error = %v", err)
	}

	branches, ok := result.(*BranchesResult)
	if !ok {
		t.Fatal("result is not *BranchesResult")
	}

	if branches.Current != "main" {
		t.Errorf("Current = %s, want main", branches.Current)
	}
	if len(branches.Branches) != 2 {
		t.Errorf("Branches length = %d, want 2", len(branches.Branches))
	}
}

func TestGitService_Checkout_Success(t *testing.T) {
	provider := &mockGitProvider{
		checkoutResult: &CheckoutResult{
			Success: true,
			Branch:  "feature-branch",
			Message: "Switched to branch 'feature-branch'",
		},
	}
	svc := NewGitService(provider)

	params, _ := json.Marshal(CheckoutParams{Branch: "feature-branch"})
	result, err := svc.Checkout(context.Background(), params)

	if err != nil {
		t.Fatalf("Checkout() error = %v", err)
	}

	checkout, ok := result.(*CheckoutResult)
	if !ok {
		t.Fatal("result is not *CheckoutResult")
	}

	if !checkout.Success {
		t.Error("Success = false, want true")
	}
	if checkout.Branch != "feature-branch" {
		t.Errorf("Branch = %s, want feature-branch", checkout.Branch)
	}
	if provider.checkoutBranch != "feature-branch" {
		t.Errorf("checkoutBranch = %s, want feature-branch", provider.checkoutBranch)
	}
}

func TestGitService_Checkout_EmptyBranch(t *testing.T) {
	provider := &mockGitProvider{}
	svc := NewGitService(provider)

	params, _ := json.Marshal(CheckoutParams{Branch: ""})
	_, err := svc.Checkout(context.Background(), params)

	if err == nil {
		t.Error("expected error for empty branch")
	}
}
