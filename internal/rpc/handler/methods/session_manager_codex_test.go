package methods

import (
	"reflect"
	"strings"
	"testing"
)

func TestRemapCodexSessionID_UpdatesSessionAndMovesDedupeState(t *testing.T) {
	service := &SessionManagerService{
		codexSessions:       make(map[string]*codexPTYSession),
		codexLastPTYLogLine: make(map[string]string),
	}

	session := &codexPTYSession{sessionID: "old-session"}
	service.codexSessions["old-session"] = session
	service.codexLastPTYLogLine["old-session"] = "same line"

	service.remapCodexSessionID("old-session", "new-session")

	if got := session.SessionID(); got != "new-session" {
		t.Fatalf("session id = %q, want %q", got, "new-session")
	}
	if _, exists := service.codexSessions["old-session"]; exists {
		t.Fatalf("old session map key should be removed")
	}
	if got := service.codexSessions["new-session"]; got != session {
		t.Fatalf("new session map key should point to same session object")
	}
	if _, exists := service.codexLastPTYLogLine["old-session"]; exists {
		t.Fatalf("old dedupe key should be removed")
	}
	if got := service.codexLastPTYLogLine["new-session"]; got != "same line" {
		t.Fatalf("new dedupe key = %q, want %q", got, "same line")
	}
}

func TestRemapCodexSessionID_DedupeCarriesAcrossRemap(t *testing.T) {
	service := &SessionManagerService{
		codexSessions:       make(map[string]*codexPTYSession),
		codexLastPTYLogLine: make(map[string]string),
	}

	session := &codexPTYSession{sessionID: "temp"}
	service.codexSessions["temp"] = session

	if logged := service.shouldLogCodexPTYOutput("temp", "line"); !logged {
		t.Fatalf("first line should log for temp session")
	}

	service.remapCodexSessionID("temp", "real")

	if logged := service.shouldLogCodexPTYOutput("real", "line"); logged {
		t.Fatalf("duplicate line should be deduped after remap")
	}
}

func TestBuildCodexCLIArgs_NewSessionWithPrompt(t *testing.T) {
	got := buildCodexCLIArgs("/Users/brianly/Projects/AIQA", "", "hello", false)
	want := []string{"exec", "hello"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildCodexCLIArgs_ResumeSessionWithoutPrompt(t *testing.T) {
	got := buildCodexCLIArgs("/Users/brianly/Projects/AIQA", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "", false)
	want := []string{"exec", "resume", "019c6572-6e3f-78a1-9d70-7af2973de2a4"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildCodexCLIArgs_ResumeSessionWithPrompt(t *testing.T) {
	got := buildCodexCLIArgs("/Users/brianly/Projects/AIQA", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello", false)
	want := []string{"exec", "resume", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildCodexCLIArgs_WithoutWorkspacePath_OmitsCDFlag(t *testing.T) {
	got := buildCodexCLIArgs("", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello", false)
	want := []string{"exec", "resume", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildCodexCLIArgs_WorkspacePathDoesNotAffectArgs(t *testing.T) {
	withPath := buildCodexCLIArgs("/Users/brianly/Projects/AIQA", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello", false)
	withoutPath := buildCodexCLIArgs("", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello", false)
	if !reflect.DeepEqual(withPath, withoutPath) {
		t.Fatalf("withPath = %#v, withoutPath = %#v, want equal", withPath, withoutPath)
	}
}

func TestBuildCodexCLIArgs_YoloModeAddsBypassFlag(t *testing.T) {
	got := buildCodexCLIArgs("/Users/brianly/Projects/AIQA", "019c6572-6e3f-78a1-9d70-7af2973de2a4", "hello", true)
	want := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"resume",
		"019c6572-6e3f-78a1-9d70-7af2973de2a4",
		"hello",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestSanitizeCodexPTYOutputText_StripsANSIAndControl(t *testing.T) {
	input := "\x1b[31mERROR:\x1b[0m stream disconnected\x1b[?25h"
	got := sanitizeCodexPTYOutputText(input)
	want := "ERROR: stream disconnected"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSanitizeCodexPTYOutputText_OnlyControlSequencesReturnsEmpty(t *testing.T) {
	input := "\x1b[?2026h\x1b[33;2H\x1b[0m\x1b[49m\x1b[K\x1b[?2026l"
	got := sanitizeCodexPTYOutputText(input)
	if strings.TrimSpace(got) != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestExtractCodexContextWindowErrorText_ReturnsMatchingLine(t *testing.T) {
	input := "ERROR: Codex ran out of room in the model's context window. Start a new thread or clear earlier history before retrying.\ntokens used\n0"
	got := extractCodexContextWindowErrorText(input)
	want := "ERROR: Codex ran out of room in the model's context window. Start a new thread or clear earlier history before retrying."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractCodexContextWindowErrorText_NoMatch(t *testing.T) {
	input := "mcp: playwright ready\nmcp startup: ready: playwright"
	got := extractCodexContextWindowErrorText(input)
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
