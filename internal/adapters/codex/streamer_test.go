package codex

import "testing"

func TestSummarizeToolForExplored_SkipsViewImage(t *testing.T) {
	got := summarizeToolForExplored("view_image", map[string]interface{}{
		"path": ".cdev/images/img_6ab0243f-1a6.jpg",
	})
	if got != "" {
		t.Fatalf("got %q, want empty summary", got)
	}
}

func TestSummarizeToolForExplored_SkipsApplyPatch(t *testing.T) {
	got := summarizeToolForExplored("apply_patch", map[string]interface{}{
		"input": "*** Begin Patch\n*** Update File: a.txt\n+hi\n*** End Patch\n",
	})
	if got != "" {
		t.Fatalf("got %q, want empty summary", got)
	}
}

func TestSummarizeToolForExplored_FormatsReadCommands(t *testing.T) {
	got := summarizeToolForExplored("exec_command", map[string]interface{}{
		"cmd": "sed -n '1170,1275p' /Users/brianly/Projects/cdev/internal/app/adapters.go",
	})
	if got != "Read adapters.go" {
		t.Fatalf("got %q, want %q", got, "Read adapters.go")
	}

	got = summarizeToolForExplored("exec_command", map[string]interface{}{
		"cmd": "nl -ba internal/app/adapters.go | sed -n '1,40p'",
	})
	if got != "Read adapters.go" {
		t.Fatalf("got %q, want %q", got, "Read adapters.go")
	}
}

func TestSummarizeToolForExplored_FormatsSearchCommands(t *testing.T) {
	got := summarizeToolForExplored("exec_command", map[string]interface{}{
		"cmd": "rg -n \"claude_message|tool_use\" internal -S",
	})
	if got != "Search claude_message|tool_use in internal" {
		t.Fatalf("got %q, want %q", got, "Search claude_message|tool_use in internal")
	}
}

func TestSummarizeToolForExplored_SkipsExecutionCommands(t *testing.T) {
	got := summarizeToolForExplored("exec_command", map[string]interface{}{
		"cmd": "python3 - <<'PY'\nprint('hello')\nPY",
	})
	if got != "" {
		t.Fatalf("got %q, want empty summary", got)
	}
}
