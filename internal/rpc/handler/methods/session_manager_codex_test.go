package methods

import "testing"

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
