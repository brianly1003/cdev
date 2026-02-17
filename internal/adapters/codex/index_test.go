package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeDecodeProjectPath(t *testing.T) {
	tests := []struct {
		path    string
		encoded string
	}{
		{"/Users/brian/Projects/cdev", "-Users-brian-Projects-cdev"},
		{"/home/user/code", "-home-user-code"},
		{"/repo", "-repo"},
	}

	for _, tt := range tests {
		encoded := encodeProjectPath(tt.path)
		if encoded != tt.encoded {
			t.Errorf("encodeProjectPath(%q) = %q, want %q", tt.path, encoded, tt.encoded)
		}

		decoded := decodeProjectPath(encoded)
		if decoded != tt.path {
			t.Errorf("decodeProjectPath(%q) = %q, want %q", encoded, decoded, tt.path)
		}
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 10, "hello w..."},
		{"short", 100, "short"},
		{"a b\nc\td", 20, "a b c d"},
	}

	for _, tt := range tests {
		result := truncateString(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestParseSessionFileForIndex(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "rollout-2026-01-31T12-00-00-sess-123.jsonl")

	lines := []string{
		`{"timestamp":"2026-01-31T12:00:00Z","type":"session_meta","payload":{"id":"sess-123","timestamp":"2026-01-31T12:00:00Z","cwd":"/repo","model_provider":"openai","cli_version":"0.92.0","git":{"branch":"main","commit_hash":"abc123","repository_url":"git@github.com:user/repo.git"}}}`,
		`{"timestamp":"2026-01-31T12:00:01Z","type":"turn_context","payload":{"cwd":"/repo","model":"gpt-5"}}`,
		`{"timestamp":"2026-01-31T12:00:02Z","type":"event_msg","payload":{"type":"user_message","message":"Hello, help me with code"}}`,
		`{"timestamp":"2026-01-31T12:00:03Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Sure, I can help!"}]}}`,
		`{"timestamp":"2026-01-31T12:00:04Z","type":"event_msg","payload":{"type":"agent_message","message":"Working on it"}}`,
	}

	if err := os.WriteFile(filePath, []byte(joinTestLines(lines)), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	entry, err := parseSessionFileForIndex(filePath)
	if err != nil {
		t.Fatalf("parseSessionFileForIndex: %v", err)
	}

	if entry.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, "sess-123")
	}
	if entry.ProjectPath != "/repo" {
		t.Errorf("ProjectPath = %q, want %q", entry.ProjectPath, "/repo")
	}
	if entry.ModelProvider != "openai" {
		t.Errorf("ModelProvider = %q, want %q", entry.ModelProvider, "openai")
	}
	if entry.Model != "gpt-5" {
		t.Errorf("Model = %q, want %q", entry.Model, "gpt-5")
	}
	if entry.CLIVersion != "0.92.0" {
		t.Errorf("CLIVersion = %q, want %q", entry.CLIVersion, "0.92.0")
	}
	if entry.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want %q", entry.GitBranch, "main")
	}
	if entry.GitCommit != "abc123" {
		t.Errorf("GitCommit = %q, want %q", entry.GitCommit, "abc123")
	}
	if entry.GitRepo != "git@github.com:user/repo.git" {
		t.Errorf("GitRepo = %q, want %q", entry.GitRepo, "git@github.com:user/repo.git")
	}
	if entry.FirstPrompt != "Hello, help me with code" {
		t.Errorf("FirstPrompt = %q, want %q", entry.FirstPrompt, "Hello, help me with code")
	}
	if entry.MessageCount != 1 { // Only user messages are counted
		t.Errorf("MessageCount = %d, want %d", entry.MessageCount, 1)
	}
	if entry.FullPath != filePath {
		t.Errorf("FullPath = %q, want %q", entry.FullPath, filePath)
	}
}

func TestIndexCache(t *testing.T) {
	// Create a temp directory structure mimicking Codex sessions
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "01", "31")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Create session file for project A (use paths without hyphens to test basic encoding)
	sessionA := `{"timestamp":"2026-01-31T10:00:00Z","type":"session_meta","payload":{"id":"sess-a1","cwd":"/Users/test/projectA","model_provider":"openai","git":{"branch":"main"}}}
{"timestamp":"2026-01-31T10:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"First prompt A1"}}
`
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-2026-01-31T10-00-00-sess-a1.jsonl"), []byte(sessionA), 0600); err != nil {
		t.Fatalf("write session A: %v", err)
	}

	// Create session file for project B
	sessionB := `{"timestamp":"2026-01-31T11:00:00Z","type":"session_meta","payload":{"id":"sess-b1","cwd":"/Users/test/projectB","model_provider":"anthropic","git":{"branch":"develop"}}}
{"timestamp":"2026-01-31T11:00:01Z","type":"event_msg","payload":{"type":"user_message","message":"First prompt B1"}}
`
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-2026-01-31T11-00-00-sess-b1.jsonl"), []byte(sessionB), 0600); err != nil {
		t.Fatalf("write session B: %v", err)
	}

	// Create index cache with custom codex home
	cache := NewIndexCache(tmpDir)

	// Test ListProjects
	projects, err := cache.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("len(projects) = %d, want 2. Got: %+v", len(projects), projects)
	}

	// Verify project A and B (find them in the slice - order may vary by last activity)
	var projectA, projectB *ProjectSummary
	for i := range projects {
		switch projects[i].ProjectPath {
		case "/Users/test/projectA":
			projectA = &projects[i]
		case "/Users/test/projectB":
			projectB = &projects[i]
		}
	}

	if projectA == nil {
		t.Fatalf("project A not found in: %+v", projects)
	}
	if projectA.SessionCount != 1 {
		t.Errorf("projectA.SessionCount = %d, want 1", projectA.SessionCount)
	}
	if projectA.EncodedPath != "-Users-test-projectA" {
		t.Errorf("projectA.EncodedPath = %q, want %q", projectA.EncodedPath, "-Users-test-projectA")
	}

	if projectB == nil {
		t.Fatal("project B not found")
	}
	if projectB.GitBranch != "develop" {
		t.Errorf("projectB.GitBranch = %q, want %q", projectB.GitBranch, "develop")
	}

	// Test GetProjectIndex
	idx, err := cache.GetProjectIndex("/Users/test/projectA")
	if err != nil {
		t.Fatalf("GetProjectIndex: %v", err)
	}
	if idx.OriginalPath != "/Users/test/projectA" {
		t.Errorf("idx.OriginalPath = %q, want %q", idx.OriginalPath, "/Users/test/projectA")
	}
	if len(idx.Entries) != 1 {
		t.Errorf("len(idx.Entries) = %d, want 1", len(idx.Entries))
	}
	if idx.Entries[0].SessionID != "sess-a1" {
		t.Errorf("idx.Entries[0].SessionID = %q, want %q", idx.Entries[0].SessionID, "sess-a1")
	}
	if idx.Entries[0].FirstPrompt != "First prompt A1" {
		t.Errorf("idx.Entries[0].FirstPrompt = %q, want %q", idx.Entries[0].FirstPrompt, "First prompt A1")
	}

	// Test FindSessionByID
	entry, err := cache.FindSessionByID("sess-b1")
	if err != nil {
		t.Fatalf("FindSessionByID: %v", err)
	}
	if entry.SessionID != "sess-b1" {
		t.Errorf("entry.SessionID = %q, want %q", entry.SessionID, "sess-b1")
	}
	if entry.ProjectPath != "/Users/test/projectB" {
		t.Errorf("entry.ProjectPath = %q, want %q", entry.ProjectPath, "/Users/test/projectB")
	}
	if entry.ModelProvider != "anthropic" {
		t.Errorf("entry.ModelProvider = %q, want %q", entry.ModelProvider, "anthropic")
	}

	// Test GetSessionsForProject
	sessions, err := cache.GetSessionsForProject("/Users/test/projectA")
	if err != nil {
		t.Fatalf("GetSessionsForProject: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("len(sessions) = %d, want 1", len(sessions))
	}

	// Test GetAllSessions - returns all sessions from all projects
	allSessions, err := cache.GetAllSessions()
	if err != nil {
		t.Fatalf("GetAllSessions: %v", err)
	}
	if len(allSessions) != 2 {
		t.Errorf("len(allSessions) = %d, want 2", len(allSessions))
	}

	// Verify sessions are sorted by modified time (most recent first)
	// Session B was created at 11:00, Session A at 10:00
	if allSessions[0].SessionID != "sess-b1" {
		t.Errorf("allSessions[0].SessionID = %q, want %q (most recent)", allSessions[0].SessionID, "sess-b1")
	}
	if allSessions[1].SessionID != "sess-a1" {
		t.Errorf("allSessions[1].SessionID = %q, want %q", allSessions[1].SessionID, "sess-a1")
	}
}

func TestIndexCacheRefreshInterval(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "01", "31")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	session := `{"timestamp":"2026-01-31T10:00:00Z","type":"session_meta","payload":{"id":"sess-1","cwd":"/repo"}}
`
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-2026-01-31T10-00-00-sess-1.jsonl"), []byte(session), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cache := NewIndexCache(tmpDir)
	cache.scanInterval = 10 * time.Millisecond // Short interval for testing

	// First call triggers refresh
	projects1, _ := cache.ListProjects()
	if len(projects1) != 1 {
		t.Errorf("first call: len(projects) = %d, want 1", len(projects1))
	}

	// Add another session
	session2 := `{"timestamp":"2026-01-31T11:00:00Z","type":"session_meta","payload":{"id":"sess-2","cwd":"/repo2"}}
`
	if err := os.WriteFile(filepath.Join(sessionsDir, "rollout-2026-01-31T11-00-00-sess-2.jsonl"), []byte(session2), 0600); err != nil {
		t.Fatalf("write session 2: %v", err)
	}

	// Immediate call should use cache (no refresh)
	projects2, _ := cache.ListProjects()
	if len(projects2) != 1 {
		t.Errorf("cached call: len(projects) = %d, want 1 (should use cache)", len(projects2))
	}

	// Wait for cache to expire
	time.Sleep(15 * time.Millisecond)

	// Should see both projects now
	projects3, _ := cache.ListProjects()
	if len(projects3) != 2 {
		t.Errorf("after refresh: len(projects) = %d, want 2", len(projects3))
	}
}

func joinTestLines(lines []string) string {
	out := ""
	for i, line := range lines {
		out += line
		if i < len(lines)-1 {
			out += "\n"
		}
	}
	return out
}
