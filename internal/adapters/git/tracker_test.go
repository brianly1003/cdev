package git

import (
	"testing"

	"github.com/brianly1003/cdev/internal/domain/ports"
)

func TestTracker_parseStatus(t *testing.T) {
	tracker := &Tracker{}

	tests := []struct {
		name     string
		output   string
		expected []ports.GitFileStatus
	}{
		{
			name:     "empty output",
			output:   "",
			expected: []ports.GitFileStatus{},
		},
		{
			name:   "single modified file",
			output: " M file.go\n",
			expected: []ports.GitFileStatus{
				{Path: "file.go", Status: " M", IsStaged: false, IsUntracked: false},
			},
		},
		{
			name:   "staged modified file",
			output: "M  file.go\n",
			expected: []ports.GitFileStatus{
				{Path: "file.go", Status: "M ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "untracked file",
			output: "?? newfile.go\n",
			expected: []ports.GitFileStatus{
				{Path: "newfile.go", Status: "??", IsStaged: false, IsUntracked: true},
			},
		},
		{
			name:   "added file",
			output: "A  newfile.go\n",
			expected: []ports.GitFileStatus{
				{Path: "newfile.go", Status: "A ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "deleted file",
			output: "D  removed.go\n",
			expected: []ports.GitFileStatus{
				{Path: "removed.go", Status: "D ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "renamed file",
			output: "R  old.go -> new.go\n",
			expected: []ports.GitFileStatus{
				{Path: "new.go", Status: "R ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name: "multiple files",
			output: ` M file1.go
M  file2.go
?? file3.go
A  file4.go
`,
			expected: []ports.GitFileStatus{
				{Path: "file1.go", Status: " M", IsStaged: false, IsUntracked: false},
				{Path: "file2.go", Status: "M ", IsStaged: true, IsUntracked: false},
				{Path: "file3.go", Status: "??", IsStaged: false, IsUntracked: true},
				{Path: "file4.go", Status: "A ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "file with path in subdirectory",
			output: " M src/internal/file.go\n",
			expected: []ports.GitFileStatus{
				{Path: "src/internal/file.go", Status: " M", IsStaged: false, IsUntracked: false},
			},
		},
		{
			name:   "both staged and unstaged changes",
			output: "MM file.go\n",
			expected: []ports.GitFileStatus{
				{Path: "file.go", Status: "MM", IsStaged: true, IsUntracked: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tracker.parseStatus(tt.output)

			if len(result) != len(tt.expected) {
				t.Errorf("parseStatus() returned %d files, want %d", len(result), len(tt.expected))
				return
			}

			for i, got := range result {
				want := tt.expected[i]
				if got.Path != want.Path {
					t.Errorf("file[%d].Path = %q, want %q", i, got.Path, want.Path)
				}
				if got.Status != want.Status {
					t.Errorf("file[%d].Status = %q, want %q", i, got.Status, want.Status)
				}
				if got.IsStaged != want.IsStaged {
					t.Errorf("file[%d].IsStaged = %v, want %v", i, got.IsStaged, want.IsStaged)
				}
				if got.IsUntracked != want.IsUntracked {
					t.Errorf("file[%d].IsUntracked = %v, want %v", i, got.IsUntracked, want.IsUntracked)
				}
			}
		})
	}
}

func TestCountDiffLines(t *testing.T) {
	tests := []struct {
		name              string
		diff              string
		expectedAdditions int
		expectedDeletions int
	}{
		{
			name:              "empty diff",
			diff:              "",
			expectedAdditions: 0,
			expectedDeletions: 0,
		},
		{
			name: "only additions",
			diff: `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,3 +1,5 @@
 package main
+
+import "fmt"
+
 func main() {}`,
			expectedAdditions: 3,
			expectedDeletions: 0,
		},
		{
			name: "only deletions",
			diff: `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,5 +1,3 @@
 package main
-
-import "fmt"
-
 func main() {}`,
			expectedAdditions: 0,
			expectedDeletions: 3,
		},
		{
			name: "mixed additions and deletions",
			diff: `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,5 +1,5 @@
 package main

-import "fmt"
+import "log"

 func main() {}`,
			expectedAdditions: 1,
			expectedDeletions: 1,
		},
		{
			name: "new file",
			diff: `diff --git a/newfile.go b/newfile.go
new file mode 100644
--- /dev/null
+++ b/newfile.go
@@ -0,0 +1,3 @@
+package main
+
+func hello() {}`,
			expectedAdditions: 3,
			expectedDeletions: 0,
		},
		{
			name: "multiple hunks",
			diff: `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"

 func main() {}
@@ -10,3 +11,4 @@
 func other() {
-    return
+    fmt.Println("hello")
+    return
 }`,
			expectedAdditions: 3,
			expectedDeletions: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			additions, deletions := countDiffLines(tt.diff)

			if additions != tt.expectedAdditions {
				t.Errorf("additions = %d, want %d", additions, tt.expectedAdditions)
			}
			if deletions != tt.expectedDeletions {
				t.Errorf("deletions = %d, want %d", deletions, tt.expectedDeletions)
			}
		})
	}
}

func TestTruncateMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "short message",
			input:    "fix bug",
			maxLen:   50,
			expected: "fix bug",
		},
		{
			name:     "exact length",
			input:    "12345",
			maxLen:   5,
			expected: "12345",
		},
		{
			name:     "truncated message",
			input:    "This is a very long commit message that exceeds the limit",
			maxLen:   20,
			expected: "This is a very lo...",
		},
		{
			name:     "multiline message",
			input:    "First line\nSecond line\nThird line",
			maxLen:   50,
			expected: "First line",
		},
		{
			name:     "multiline with truncation",
			input:    "Very long first line that should be truncated\nSecond line",
			maxLen:   20,
			expected: "Very long first l...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateMessage(tt.input, tt.maxLen)

			if result != tt.expected {
				t.Errorf("truncateMessage(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

func TestIsBinaryContent(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		expected bool
	}{
		{
			name:     "empty content",
			content:  []byte{},
			expected: false,
		},
		{
			name:     "text content",
			content:  []byte("hello world"),
			expected: false,
		},
		{
			name:     "go source code",
			content:  []byte("package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"),
			expected: false,
		},
		{
			name:     "binary with null byte at start",
			content:  []byte{0x00, 0x01, 0x02},
			expected: true,
		},
		{
			name:     "binary with null byte in middle",
			content:  []byte("hello\x00world"),
			expected: true,
		},
		{
			name:     "unicode text",
			content:  []byte("こんにちは世界"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBinaryContent(tt.content)

			if result != tt.expected {
				t.Errorf("isBinaryContent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNewTracker(t *testing.T) {
	// Create a temp directory that is not a git repo
	tempDir := t.TempDir()

	tracker := NewTracker(tempDir, "git", nil)

	if tracker == nil {
		t.Fatal("NewTracker() returned nil")
	}

	if tracker.repoPath != tempDir {
		t.Errorf("repoPath = %s, want %s", tracker.repoPath, tempDir)
	}

	if tracker.command != "git" {
		t.Errorf("command = %s, want git", tracker.command)
	}

	// Not a git repo, so these should be empty/false
	if tracker.IsGitRepo() {
		t.Error("IsGitRepo() should be false for non-git directory")
	}

	if tracker.GetRepoRoot() != "" {
		t.Errorf("GetRepoRoot() = %s, want empty string", tracker.GetRepoRoot())
	}

	if tracker.GetRepoName() != "" {
		t.Errorf("GetRepoName() = %s, want empty string", tracker.GetRepoName())
	}
}

func TestTracker_EnhancedStatus_Categorization(t *testing.T) {
	// Test the status parsing logic for categorization
	tracker := &Tracker{
		isRepo:   true,
		repoRoot: t.TempDir(),
		repoName: "test",
		command:  "git",
	}

	// We can't easily test the full GetEnhancedStatus without a real git repo,
	// but we can verify the tracker is set up correctly
	if !tracker.IsGitRepo() {
		t.Error("IsGitRepo() should return true when isRepo is set")
	}

	if tracker.GetRepoName() != "test" {
		t.Errorf("GetRepoName() = %s, want test", tracker.GetRepoName())
	}
}

// BenchmarkParseStatus benchmarks the status parsing function.
func BenchmarkParseStatus(b *testing.B) {
	tracker := &Tracker{}
	output := ` M file1.go
M  file2.go
?? file3.go
A  file4.go
 M src/internal/package/file5.go
MM file6.go
D  deleted.go
R  old.go -> new.go
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracker.parseStatus(output)
	}
}

// BenchmarkCountDiffLines benchmarks the diff line counting function.
func BenchmarkCountDiffLines(b *testing.B) {
	diff := `diff --git a/file.go b/file.go
--- a/file.go
+++ b/file.go
@@ -1,20 +1,25 @@
 package main

-import "fmt"
+import (
+    "fmt"
+    "log"
+)

 func main() {
-    fmt.Println("hello")
+    log.Println("starting")
+    fmt.Println("hello world")
+    log.Println("done")
 }

-func helper() {}
+func helper() error {
+    return nil
+}
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		countDiffLines(diff)
	}
}

// Additional parseStatus edge cases
func TestTracker_parseStatus_EdgeCases(t *testing.T) {
	tracker := &Tracker{}

	tests := []struct {
		name     string
		output   string
		expected []ports.GitFileStatus
	}{
		{
			name:     "whitespace only",
			output:   "   \n\n   \n",
			expected: []ports.GitFileStatus{},
		},
		{
			name:   "copied file",
			output: "C  original.go -> copy.go\n",
			expected: []ports.GitFileStatus{
				{Path: "copy.go", Status: "C ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "file with spaces in name",
			output: " M path/to/file with spaces.go\n",
			expected: []ports.GitFileStatus{
				{Path: "path/to/file with spaces.go", Status: " M", IsStaged: false, IsUntracked: false},
			},
		},
		{
			name:   "deleted unstaged file",
			output: " D deleted.go\n",
			expected: []ports.GitFileStatus{
				{Path: "deleted.go", Status: " D", IsStaged: false, IsUntracked: false},
			},
		},
		{
			name:   "added and modified",
			output: "AM newfile.go\n",
			expected: []ports.GitFileStatus{
				{Path: "newfile.go", Status: "AM", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "ignored file indicator",
			output: "!! ignored.log\n",
			expected: []ports.GitFileStatus{
				{Path: "ignored.log", Status: "!!", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:   "complex rename path",
			output: "R  src/old/path.go -> src/new/renamed_path.go\n",
			expected: []ports.GitFileStatus{
				{Path: "src/new/renamed_path.go", Status: "R ", IsStaged: true, IsUntracked: false},
			},
		},
		{
			name:     "short line ignored",
			output:   "AB\nM  valid.go\n",
			expected: []ports.GitFileStatus{
				{Path: "valid.go", Status: "M ", IsStaged: true, IsUntracked: false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tracker.parseStatus(tt.output)

			if len(result) != len(tt.expected) {
				t.Errorf("parseStatus() returned %d files, want %d", len(result), len(tt.expected))
				return
			}

			for i, got := range result {
				want := tt.expected[i]
				if got.Path != want.Path {
					t.Errorf("file[%d].Path = %q, want %q", i, got.Path, want.Path)
				}
				if got.Status != want.Status {
					t.Errorf("file[%d].Status = %q, want %q", i, got.Status, want.Status)
				}
			}
		})
	}
}

// Additional countDiffLines edge cases
func TestCountDiffLines_EdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		diff              string
		expectedAdditions int
		expectedDeletions int
	}{
		{
			name:              "no content lines",
			diff:              "diff --git a/f b/f\n--- a/f\n+++ b/f\n",
			expectedAdditions: 0,
			expectedDeletions: 0,
		},
		{
			name:              "binary file",
			diff:              "Binary files a/image.png and b/image.png differ\n",
			expectedAdditions: 0,
			expectedDeletions: 0,
		},
		{
			name: "context lines only",
			diff: `@@ -1,3 +1,3 @@
 line1
 line2
 line3`,
			expectedAdditions: 0,
			expectedDeletions: 0,
		},
		{
			name: "multiple plus and minus in line content",
			diff: `@@ -1,2 +1,2 @@
-x = a - b + c
+x = a + b - c`,
			expectedAdditions: 1,
			expectedDeletions: 1,
		},
		{
			name: "empty lines as additions",
			diff: `@@ -1,1 +1,3 @@
 content
+
+`,
			expectedAdditions: 2,
			expectedDeletions: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			additions, deletions := countDiffLines(tt.diff)

			if additions != tt.expectedAdditions {
				t.Errorf("additions = %d, want %d", additions, tt.expectedAdditions)
			}
			if deletions != tt.expectedDeletions {
				t.Errorf("deletions = %d, want %d", deletions, tt.expectedDeletions)
			}
		})
	}
}

// Additional truncateMessage edge cases
func TestTruncateMessage_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "empty message",
			input:    "",
			maxLen:   50,
			expected: "",
		},
		{
			name:     "very short maxLen",
			input:    "Hello",
			maxLen:   4,
			expected: "H...",
		},
		{
			name:     "unicode characters within limit",
			input:    "日本語テキスト",
			maxLen:   50,
			expected: "日本語テキスト",
		},
		{
			name:     "only newline",
			input:    "first\nsecond",
			maxLen:   50,
			expected: "first",
		},
		{
			name:     "exact boundary",
			input:    "12345",
			maxLen:   5,
			expected: "12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateMessage(tt.input, tt.maxLen)

			if result != tt.expected {
				t.Errorf("truncateMessage(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
			}
		})
	}
}

// Additional isBinaryContent edge cases
func TestIsBinaryContent_EdgeCases(t *testing.T) {
	t.Run("null byte at end", func(t *testing.T) {
		content := []byte("hello world\x00")
		if !isBinaryContent(content) {
			t.Error("expected true for content with null byte")
		}
	})

	t.Run("large text file without null", func(t *testing.T) {
		content := make([]byte, 10000)
		for i := range content {
			content[i] = 'a'
		}
		if isBinaryContent(content) {
			t.Error("expected false for text-only content")
		}
	})

	t.Run("control characters but no null", func(t *testing.T) {
		content := []byte("hello\x01\x02\x03world")
		if isBinaryContent(content) {
			t.Error("expected false for content without null bytes")
		}
	})

	t.Run("newlines and tabs", func(t *testing.T) {
		content := []byte("line1\n\tline2\r\nline3")
		if isBinaryContent(content) {
			t.Error("expected false for text with whitespace")
		}
	})

	t.Run("null byte within 8KB limit", func(t *testing.T) {
		// Create content with null byte at position 1000 (within 8KB limit)
		content := make([]byte, 2000)
		for i := range content {
			content[i] = 'a'
		}
		content[1000] = 0x00
		if !isBinaryContent(content) {
			t.Error("expected true for content with null byte within check limit")
		}
	})
}

// Test tracker with real temp directory
func TestTracker_NonGitDirectory(t *testing.T) {
	tempDir := t.TempDir()

	tracker := NewTracker(tempDir, "git", nil)

	if tracker.IsGitRepo() {
		t.Error("IsGitRepo() should be false for non-git directory")
	}
	if tracker.GetRepoRoot() != "" {
		t.Errorf("GetRepoRoot() = %q, want empty string", tracker.GetRepoRoot())
	}
	if tracker.GetRepoName() != "" {
		t.Errorf("GetRepoName() = %q, want empty string", tracker.GetRepoName())
	}
}

// Test FileEntry struct
func TestFileEntry(t *testing.T) {
	entry := FileEntry{
		Path:      "src/main.go",
		Status:    "M",
		Additions: 10,
		Deletions: 5,
	}

	if entry.Path != "src/main.go" {
		t.Errorf("Path = %q, want src/main.go", entry.Path)
	}
	if entry.Additions != 10 {
		t.Errorf("Additions = %d, want 10", entry.Additions)
	}
}

// Test EnhancedStatus struct initialization
func TestEnhancedStatus_Initialization(t *testing.T) {
	status := &EnhancedStatus{
		Branch:     "main",
		Upstream:   "origin/main",
		Ahead:      2,
		Behind:     1,
		Staged:     make([]FileEntry, 0),
		Unstaged:   make([]FileEntry, 0),
		Untracked:  make([]FileEntry, 0),
		Conflicted: make([]FileEntry, 0),
		RepoName:   "test-repo",
		RepoRoot:   "/path/to/repo",
	}

	if status.Branch != "main" {
		t.Errorf("Branch = %q, want main", status.Branch)
	}
	if len(status.Staged) != 0 {
		t.Errorf("Staged should be empty, got %d items", len(status.Staged))
	}
}

// Test result types
func TestCommitResult(t *testing.T) {
	result := &CommitResult{
		Success:        true,
		SHA:            "abc1234",
		Message:        "Committed successfully",
		FilesCommitted: 3,
		Pushed:         true,
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.SHA != "abc1234" {
		t.Errorf("SHA = %q, want abc1234", result.SHA)
	}
}

func TestPushResult(t *testing.T) {
	result := &PushResult{
		Success:       true,
		Message:       "Pushed to origin/main",
		CommitsPushed: 5,
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.CommitsPushed != 5 {
		t.Errorf("CommitsPushed = %d, want 5", result.CommitsPushed)
	}
}

func TestPullResult(t *testing.T) {
	result := &PullResult{
		Success:         false,
		Error:           "Merge conflict",
		ConflictedFiles: []string{"file1.go", "file2.go"},
	}

	if result.Success {
		t.Error("Success should be false")
	}
	if len(result.ConflictedFiles) != 2 {
		t.Errorf("ConflictedFiles length = %d, want 2", len(result.ConflictedFiles))
	}
}

func TestBranchInfo(t *testing.T) {
	info := BranchInfo{
		Name:      "feature/test",
		IsCurrent: true,
		IsRemote:  false,
		Upstream:  "origin/feature/test",
		Ahead:     3,
		Behind:    0,
	}

	if info.Name != "feature/test" {
		t.Errorf("Name = %q, want feature/test", info.Name)
	}
	if !info.IsCurrent {
		t.Error("IsCurrent should be true")
	}
}

func TestBranchesResult(t *testing.T) {
	result := &BranchesResult{
		Current:  "main",
		Upstream: "origin/main",
		Ahead:    1,
		Behind:   2,
		Branches: []BranchInfo{
			{Name: "main", IsCurrent: true},
			{Name: "develop", IsCurrent: false},
		},
	}

	if result.Current != "main" {
		t.Errorf("Current = %q, want main", result.Current)
	}
	if len(result.Branches) != 2 {
		t.Errorf("Branches length = %d, want 2", len(result.Branches))
	}
}

func TestCheckoutResult(t *testing.T) {
	result := &CheckoutResult{
		Success: true,
		Branch:  "new-feature",
		Message: "Switched to branch 'new-feature'",
	}

	if !result.Success {
		t.Error("Success should be true")
	}
	if result.Branch != "new-feature" {
		t.Errorf("Branch = %q, want new-feature", result.Branch)
	}
}
