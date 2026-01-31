package events

import (
	"encoding/json"
	"testing"
)

func TestNewGitDiffEvent(t *testing.T) {
	diff := `--- a/file.go
+++ b/file.go
@@ -1,3 +1,4 @@
 package main
+import "fmt"
 func main() {}`

	event := NewGitDiffEvent("file.go", diff, 1, 0, false, false, false)

	if event.Type() != EventTypeGitDiff {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeGitDiff)
	}

	payload, ok := event.Payload.(GitDiffPayload)
	if !ok {
		t.Fatal("Payload is not GitDiffPayload")
	}

	if payload.File != "file.go" {
		t.Errorf("File = %q, want file.go", payload.File)
	}
	if payload.Diff != diff {
		t.Errorf("Diff content mismatch")
	}
	if payload.Additions != 1 {
		t.Errorf("Additions = %d, want 1", payload.Additions)
	}
	if payload.Deletions != 0 {
		t.Errorf("Deletions = %d, want 0", payload.Deletions)
	}
	if payload.IsStaged {
		t.Error("IsStaged should be false")
	}
	if payload.IsNewFile {
		t.Error("IsNewFile should be false")
	}
}

func TestNewGitDiffEvent_StagedNewFile(t *testing.T) {
	event := NewGitDiffEvent("new.go", "new file content", 10, 0, true, true, false)

	payload := event.Payload.(GitDiffPayload)

	if !payload.IsStaged {
		t.Error("IsStaged should be true")
	}
	if !payload.IsNewFile {
		t.Error("IsNewFile should be true")
	}
}

func TestGitDiffPayload_JSON(t *testing.T) {
	event := NewGitDiffEvent("src/main.go", "diff content here", 5, 3, true, false, false)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			File      string `json:"file"`
			Diff      string `json:"diff"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
			IsStaged  bool   `json:"is_staged"`
			IsNewFile bool   `json:"is_new_file"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Event != string(EventTypeGitDiff) {
		t.Errorf("event = %q, want git_diff", parsed.Event)
	}
	if parsed.Payload.File != "src/main.go" {
		t.Errorf("file = %q, want src/main.go", parsed.Payload.File)
	}
	if parsed.Payload.Additions != 5 {
		t.Errorf("additions = %d, want 5", parsed.Payload.Additions)
	}
	if parsed.Payload.Deletions != 3 {
		t.Errorf("deletions = %d, want 3", parsed.Payload.Deletions)
	}
	if !parsed.Payload.IsStaged {
		t.Error("is_staged should be true")
	}
	if parsed.Payload.IsNewFile {
		t.Error("is_new_file should be false")
	}
}

func TestGitDiffPayload_EmptyDiff(t *testing.T) {
	event := NewGitDiffEvent("unchanged.go", "", 0, 0, false, false, false)

	payload := event.Payload.(GitDiffPayload)

	if payload.Diff != "" {
		t.Errorf("Diff should be empty, got %q", payload.Diff)
	}
	if payload.Additions != 0 {
		t.Errorf("Additions = %d, want 0", payload.Additions)
	}
	if payload.Deletions != 0 {
		t.Errorf("Deletions = %d, want 0", payload.Deletions)
	}
}

func TestGitDiffPayload_LargeDiff(t *testing.T) {
	// Create a large diff content
	largeDiff := ""
	for i := 0; i < 1000; i++ {
		largeDiff += "+line " + string(rune('0'+i%10)) + "\n"
	}

	event := NewGitDiffEvent("large.go", largeDiff, 1000, 0, false, true, false)

	payload := event.Payload.(GitDiffPayload)

	if payload.Additions != 1000 {
		t.Errorf("Additions = %d, want 1000", payload.Additions)
	}

	// Verify it can still serialize to JSON
	_, err := event.ToJSON()
	if err != nil {
		t.Errorf("ToJSON() error = %v", err)
	}
}

func TestGitDiffPayload_SpecialCharacters(t *testing.T) {
	// Diff with unicode and special characters
	diff := `--- a/unicode.go
+++ b/unicode.go
@@ -1,2 +1,3 @@
 package main
+// 日本語コメント
+// "quotes" and 'apostrophes' and \backslashes\`

	event := NewGitDiffEvent("unicode.go", diff, 2, 0, false, false, false)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	// Verify JSON is valid
	var parsed map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON with special chars: %v", err)
	}
}

func TestGitDiffPayload_PathWithSpaces(t *testing.T) {
	event := NewGitDiffEvent("path with spaces/file name.go", "diff", 1, 1, false, false, false)

	payload := event.Payload.(GitDiffPayload)

	if payload.File != "path with spaces/file name.go" {
		t.Errorf("File = %q, want 'path with spaces/file name.go'", payload.File)
	}

	// Verify JSON encoding handles spaces
	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Payload struct {
			File string `json:"file"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Payload.File != "path with spaces/file name.go" {
		t.Errorf("parsed file = %q, want 'path with spaces/file name.go'", parsed.Payload.File)
	}
}
