package sessioncache

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseSessionToElements_ParsesTurnAbortedStringAsInterrupted(t *testing.T) {
	msg := map[string]interface{}{
		"type":      "user",
		"uuid":      "u1",
		"timestamp": "2026-02-24T00:00:00Z",
		"message": map[string]interface{}{
			"role":    "user",
			"content": "<turn_aborted>\nThe user interrupted the previous turn on purpose.\n</turn_aborted>",
		},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	elements, err := ParseSessionToElements([]json.RawMessage{raw}, "sess-1")
	if err != nil {
		t.Fatalf("ParseSessionToElements error: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("len(elements)=%d, want 1", len(elements))
	}
	if elements[0].Type != ElementTypeInterrupted {
		t.Fatalf("type=%q, want %q", elements[0].Type, ElementTypeInterrupted)
	}

	var content InterruptedContent
	if err := json.Unmarshal(elements[0].Content, &content); err != nil {
		t.Fatalf("unmarshal interrupted content: %v", err)
	}
	if strings.TrimSpace(content.Message) == "" {
		t.Fatal("expected non-empty interrupted message")
	}
	if strings.Contains(content.Message, "<turn_aborted>") {
		t.Fatalf("unexpected raw tag in message: %q", content.Message)
	}
}

func TestParseSessionToElements_ParsesTurnAbortedTextBlockAsInterrupted(t *testing.T) {
	msg := map[string]interface{}{
		"type":      "user",
		"uuid":      "u2",
		"timestamp": "2026-02-24T00:00:01Z",
		"message": map[string]interface{}{
			"role": "user",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "<turn_aborted>\nThe previous turn was interrupted.\n</turn_aborted>",
				},
			},
		},
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal message: %v", err)
	}

	elements, err := ParseSessionToElements([]json.RawMessage{raw}, "sess-1")
	if err != nil {
		t.Fatalf("ParseSessionToElements error: %v", err)
	}
	if len(elements) != 1 {
		t.Fatalf("len(elements)=%d, want 1", len(elements))
	}
	if elements[0].Type != ElementTypeInterrupted {
		t.Fatalf("type=%q, want %q", elements[0].Type, ElementTypeInterrupted)
	}
}
