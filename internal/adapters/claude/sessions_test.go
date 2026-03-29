package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSessions_IncludesMetadataOnlySessions(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	repoPath := "/tmp/test-repo"
	sessionsDir := getSessionsDir(repoPath)
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatalf("failed to create sessions dir: %v", err)
	}

	files := map[string]string{
		"11111111-1111-1111-1111-111111111111": `{"type":"progress","gitBranch":"main_livechat"}
{"type":"user","gitBranch":"main_livechat","message":{"role":"user","content":"Read CLAUDE.md for project conventions."}}
{"type":"assistant","gitBranch":"main_livechat","message":{"role":"assistant","content":[{"type":"text","text":"Understood"}]}}
`,
		"22222222-2222-2222-2222-222222222222": `{"type":"last-prompt","lastPrompt":"/ai-agents-architect investigate callback behavior","sessionId":"22222222-2222-2222-2222-222222222222"}
`,
		"33333333-3333-3333-3333-333333333333": `{"type":"progress","gitBranch":"main_livechat"}
{"type":"system","gitBranch":"main_livechat","subtype":"local_command","content":"<command-name>/resume</command-name>"}
`,
		"44444444-4444-4444-4444-444444444444": `{"type":"progress","gitBranch":"main_livechat"}
`,
	}

	for sessionID, content := range files {
		sessionFile := filepath.Join(sessionsDir, sessionID+".jsonl")
		if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", sessionID, err)
		}
	}

	sessions, err := ListSessions(repoPath)
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}

	if got, want := len(sessions), 4; got != want {
		t.Fatalf("ListSessions() count = %d, want %d", got, want)
	}

	byID := make(map[string]SessionInfo, len(sessions))
	for _, session := range sessions {
		byID[session.SessionID] = session
	}

	if got := byID["11111111-1111-1111-1111-111111111111"].Summary; got != "Read CLAUDE.md for project conventions." {
		t.Fatalf("conversation summary = %q", got)
	}
	if got := byID["22222222-2222-2222-2222-222222222222"].Summary; got != "/ai-agents-architect investigate callback behavior" {
		t.Fatalf("last-prompt summary = %q", got)
	}
	if got := byID["33333333-3333-3333-3333-333333333333"].Summary; got != "33333333" {
		t.Fatalf("system-only summary = %q", got)
	}
	if got := byID["44444444-4444-4444-4444-444444444444"].Summary; got != "(session)" {
		t.Fatalf("progress-only summary = %q", got)
	}
}
