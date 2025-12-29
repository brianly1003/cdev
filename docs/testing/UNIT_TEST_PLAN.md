# Unit Test Implementation Plan

## Current Coverage Overview

| Package | Coverage | Priority |
|---------|----------|----------|
| `internal/rpc/handler` | 0% | **Critical** |
| `internal/rpc/message` | 0% | **Critical** |
| `internal/session` | 0% | **Critical** |
| `internal/workspace` | 0% | **Critical** |
| `internal/adapters/sessioncache` | 0% | **High** |
| `internal/adapters/git` | 6% | **High** |
| `internal/rpc/handler/methods` | 1.3% | **High** |
| `internal/adapters/claude` | 16.7% | **Medium** |

---

## Phase 1: Critical - RPC Layer (Week 1)

### 1.1 RPC Dispatcher (`internal/rpc/handler/dispatcher_test.go`)

```go
// Test Cases:
- TestDispatcher_Dispatch_ValidRequest
- TestDispatcher_Dispatch_MethodNotFound
- TestDispatcher_Dispatch_Notification
- TestDispatcher_Dispatch_HandlerError
- TestDispatcher_DispatchBytes_ParseError
- TestDispatcher_DispatchBytes_InvalidVersion
- TestDispatcher_HandleBatch_MultipleRequests
- TestDispatcher_HandleBatch_EmptyArray
- TestDispatcher_ConcurrentDispatch
```

### 1.2 JSON-RPC Messages (`internal/rpc/message/jsonrpc_test.go`)

```go
// Test Cases:
- TestID_MarshalUnmarshal_String
- TestID_MarshalUnmarshal_Number
- TestID_MarshalUnmarshal_Null
- TestNewRequest_WithParams
- TestNewRequest_WithoutParams
- TestNewNotification
- TestNewSuccessResponse
- TestNewErrorResponse
- TestParseRequest_Valid
- TestParseRequest_MissingMethod
- TestParseRequest_WrongVersion
- TestIsJSONRPC_Detection
```

### 1.3 RPC Registry (`internal/rpc/handler/registry_test.go`)

```go
// Test Cases:
- TestRegistry_Register
- TestRegistry_RegisterWithMeta
- TestRegistry_Get_Found
- TestRegistry_Get_NotFound
- TestRegistry_Methods_List
- TestRegistry_ConcurrentAccess
```

---

## Phase 2: Critical - Session & Workspace (Week 2)

### 2.1 Session Manager (`internal/session/manager_test.go`)

```go
// Test Cases:
- TestManager_New
- TestManager_StartClaude
- TestManager_StopClaude
- TestManager_StopClaude_NotRunning
- TestManager_GetStatus
- TestManager_SendResponse
- TestManager_RegisterWorkspace
- TestManager_UnregisterWorkspace
- TestManager_GetWorkspace
- TestManager_GitGetStatus
- TestManager_GitStage
- TestManager_GitCommit
- TestManager_GitPush
- TestManager_GitPull
- TestManager_ConcurrentOperations
```

### 2.2 Workspace State (`internal/workspace/workspace_test.go`)

```go
// Test Cases:
- TestWorkspace_New
- TestWorkspace_StatusTransitions
- TestWorkspace_SetStatus_ThreadSafe
- TestWorkspace_SetPID
- TestWorkspace_IsRunning
- TestWorkspace_RestartCount
- TestWorkspace_IdleTime
- TestWorkspace_ToInfo
```

### 2.3 Workspace Config Manager (`internal/workspace/config_manager_test.go`)

```go
// Test Cases:
- TestConfigManager_LoadConfig
- TestConfigManager_SaveConfig
- TestConfigManager_AddWorkspace
- TestConfigManager_RemoveWorkspace
- TestConfigManager_GetWorkspace
- TestConfigManager_ListWorkspaces
- TestConfigManager_UpdateWorkspace
- TestConfigManager_AutoSave
```

### 2.4 Git Tracker Manager (`internal/workspace/git_tracker_manager_test.go`)

```go
// Test Cases:
- TestGitTrackerManager_RegisterWorkspace
- TestGitTrackerManager_GetTracker
- TestGitTrackerManager_RefreshTracker
- TestGitTrackerManager_LazyInitialization
- TestGitTrackerManager_NonGitRepo
- TestGitTrackerManager_ConcurrentAccess
```

---

## Phase 3: High - Session Cache & Git (Week 3)

### 3.1 Session Cache (`internal/adapters/sessioncache/cache_test.go`)

```go
// Test Cases:
- TestCache_New
- TestCache_SchemaVersionMigration
- TestCache_StartStop
- TestCache_ListSessions
- TestCache_ListSessionsPaginated
- TestCache_FullSync
- TestCache_SyncFile
- TestCache_ParseSessionFile
- TestCache_MessageCounting
- TestCache_DeleteSession
- TestCache_DeleteAllSessions
- TestCache_ConcurrentAccess
- TestSessionMessage_IsUserTextMessage
- TestSessionMessage_HasTextContent
```

### 3.2 Git Tracker Operations (`internal/adapters/git/tracker_operations_test.go`)

```go
// Test Cases:
- TestTracker_Status
- TestTracker_Diff
- TestTracker_DiffStaged
- TestTracker_DiffNewFile
- TestTracker_Stage
- TestTracker_Unstage
- TestTracker_Discard
- TestTracker_Commit
- TestTracker_Commit_EmptyMessage
- TestTracker_Push
- TestTracker_Push_SetUpstream
- TestTracker_Pull
- TestTracker_Pull_Rebase
- TestTracker_Branches
- TestTracker_Checkout
- TestTracker_CreateBranch
- TestTracker_DeleteBranch
- TestTracker_SetUpstream
- TestTracker_RemoteAdd
- TestTracker_RemoteRemove
- TestTracker_Init
- TestTracker_GetStatus_StateMachine
```

---

## Phase 4: High - RPC Methods (Week 4)

### 4.1 Git Service (`internal/rpc/handler/methods/git_test.go`)

```go
// Test Cases:
- TestGitService_Status
- TestGitService_Status_NoProvider
- TestGitService_Diff
- TestGitService_Diff_InvalidParams
- TestGitService_Stage
- TestGitService_Stage_EmptyPaths
- TestGitService_Unstage
- TestGitService_Discard
- TestGitService_Commit
- TestGitService_Commit_EmptyMessage
- TestGitService_Push
- TestGitService_Pull
- TestGitService_Branches
- TestGitService_Checkout
```

### 4.2 Session Service (`internal/rpc/handler/methods/session_test.go`)

```go
// Extend existing tests with:
- TestSessionService_ListSessions
- TestSessionService_ListSessions_FilterByAgent
- TestSessionService_GetSession
- TestSessionService_GetSession_NotFound
- TestSessionService_GetSessionMessages
- TestSessionService_GetSessionMessages_Pagination
- TestSessionService_DeleteSession
- TestSessionService_DeleteAllSessions
```

### 4.3 Workspace Service (`internal/rpc/handler/methods/workspace_config_test.go`)

```go
// Test Cases:
- TestWorkspaceService_List
- TestWorkspaceService_List_IncludeGit
- TestWorkspaceService_Add
- TestWorkspaceService_Add_InvalidPath
- TestWorkspaceService_Get
- TestWorkspaceService_Get_NotFound
- TestWorkspaceService_Remove
- TestWorkspaceService_Update
- TestWorkspaceService_GitState
```

### 4.4 Agent Service (`internal/rpc/handler/methods/agent_test.go`)

```go
// Test Cases:
- TestAgentService_Run
- TestAgentService_Run_InvalidParams
- TestAgentService_Stop
- TestAgentService_Stop_NotRunning
- TestAgentService_Status
- TestAgentService_Respond
- TestAgentService_Input
```

---

## Phase 5: Medium - Claude Manager (Week 5)

### 5.1 Claude Manager (`internal/adapters/claude/manager_test.go`)

```go
// Test Cases:
- TestManager_Start
- TestManager_Start_AlreadyRunning
- TestManager_StartWithSession_Continue
- TestManager_StartWithSession_Resume
- TestManager_Stop_Graceful
- TestManager_Stop_Force
- TestManager_Kill
- TestManager_SendResponse
- TestManager_SendResponse_NotRunning
- TestManager_StateTransitions
- TestManager_ParseClaudeJSON
- TestManager_DetectToolUse
- TestManager_PermissionDetection
- TestManager_SessionIDCapture
- TestManager_ConcurrentAccess
```

### 5.2 Claude Parser (`internal/adapters/claude/parser_test.go`)

```go
// Test Cases:
- TestParser_ParseStreamJSON
- TestParser_ParseStreamJSON_InvalidJSON
- TestParser_DetectPermissionRequest
- TestParser_DetectToolUse
- TestParser_ExtractSessionID
- TestParser_ExtractResult
```

---

## Race Condition Tests

All packages should include race condition tests:

```go
func TestXxx_ConcurrentAccess(t *testing.T) {
    // Run with: go test -race ./...
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            // Concurrent operations
        }()
    }
    wg.Wait()
}
```

---

## Test Utilities to Add (`internal/testutil/`)

### Mock Providers

```go
// mockgit.go
type MockGitTracker struct {
    StatusFunc    func(ctx context.Context) (*git.Status, error)
    DiffFunc      func(ctx context.Context, path string) (string, error)
    StageFunc     func(ctx context.Context, paths []string) error
    CommitFunc    func(ctx context.Context, msg string) (*git.CommitResult, error)
    // ... other methods
}

// mocksession.go
type MockSessionCache struct {
    Sessions map[string]*sessioncache.SessionInfo
    // ... mock behavior
}

// mockworkspace.go
type MockWorkspaceManager struct {
    Workspaces map[string]*workspace.Workspace
    // ... mock behavior
}
```

### Test Fixtures

```go
// fixtures.go
func CreateTestWorkspace(t *testing.T) string {
    dir := t.TempDir()
    // Setup test workspace
    return dir
}

func CreateTestGitRepo(t *testing.T) string {
    dir := t.TempDir()
    // Initialize git repo
    return dir
}

func CreateTestSessionFile(t *testing.T, dir string) string {
    // Create test session JSONL file
    return filepath.Join(dir, "session.jsonl")
}
```

---

## CI Integration

### GitHub Actions Updates (`.github/workflows/ci.yml`)

```yaml
- name: Run tests with race detection
  run: go test -race -coverprofile=coverage.out ./...

- name: Check coverage threshold
  run: |
    COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
    if (( $(echo "$COVERAGE < 40" | bc -l) )); then
      echo "Coverage $COVERAGE% is below 40% threshold"
      exit 1
    fi
```

---

## Target Coverage Goals

| Phase | Target Coverage |
|-------|----------------|
| Phase 1 Complete | 25% |
| Phase 2 Complete | 40% |
| Phase 3 Complete | 50% |
| Phase 4 Complete | 60% |
| Phase 5 Complete | 70% |

---

## Notes

1. **Use table-driven tests** for methods with multiple input variations
2. **Mock external dependencies** (git CLI, file system, network)
3. **Run with `-race` flag** in CI to catch race conditions
4. **Use `t.Parallel()`** where safe to speed up test execution
5. **Clean up resources** using `t.Cleanup()` or `defer`
