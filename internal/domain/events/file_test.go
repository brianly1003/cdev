package events

import (
	"encoding/json"
	"testing"
)

func TestFileChangeType_Values(t *testing.T) {
	tests := []struct {
		changeType FileChangeType
		expected   string
	}{
		{FileChangeCreated, "created"},
		{FileChangeModified, "modified"},
		{FileChangeDeleted, "deleted"},
		{FileChangeRenamed, "renamed"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if string(tt.changeType) != tt.expected {
				t.Errorf("FileChangeType = %s, want %s", tt.changeType, tt.expected)
			}
		})
	}
}

func TestNewFileChangedEvent(t *testing.T) {
	event := NewFileChangedEvent("/path/to/file.go", FileChangeModified, 1024)

	if event.Type() != EventTypeFileChanged {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeFileChanged)
	}

	payload, ok := event.Payload.(FileChangedPayload)
	if !ok {
		t.Fatal("Payload is not FileChangedPayload")
	}

	if payload.Path != "/path/to/file.go" {
		t.Errorf("Path = %q, want %q", payload.Path, "/path/to/file.go")
	}
	if payload.Change != FileChangeModified {
		t.Errorf("Change = %v, want %v", payload.Change, FileChangeModified)
	}
	if payload.Size != 1024 {
		t.Errorf("Size = %d, want 1024", payload.Size)
	}
}

func TestNewFileRenamedEvent(t *testing.T) {
	event := NewFileRenamedEvent("/old/path.go", "/new/path.go")

	if event.Type() != EventTypeFileChanged {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeFileChanged)
	}

	payload, ok := event.Payload.(FileChangedPayload)
	if !ok {
		t.Fatal("Payload is not FileChangedPayload")
	}

	if payload.Path != "/new/path.go" {
		t.Errorf("Path = %q, want %q", payload.Path, "/new/path.go")
	}
	if payload.OldPath != "/old/path.go" {
		t.Errorf("OldPath = %q, want %q", payload.OldPath, "/old/path.go")
	}
	if payload.Change != FileChangeRenamed {
		t.Errorf("Change = %v, want %v", payload.Change, FileChangeRenamed)
	}
}

func TestNewFileContentEvent(t *testing.T) {
	requestID := "req-abc"
	event := NewFileContentEvent("/path/to/file.go", "package main", 12, false, requestID)

	if event.Type() != EventTypeFileContent {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeFileContent)
	}
	if event.RequestID != requestID {
		t.Errorf("RequestID = %q, want %q", event.RequestID, requestID)
	}

	payload, ok := event.Payload.(FileContentPayload)
	if !ok {
		t.Fatal("Payload is not FileContentPayload")
	}

	if payload.Path != "/path/to/file.go" {
		t.Errorf("Path = %q, want %q", payload.Path, "/path/to/file.go")
	}
	if payload.Content != "package main" {
		t.Errorf("Content = %q, want %q", payload.Content, "package main")
	}
	if payload.Encoding != "utf-8" {
		t.Errorf("Encoding = %q, want utf-8", payload.Encoding)
	}
	if payload.Truncated {
		t.Error("Truncated should be false")
	}
	if payload.Size != 12 {
		t.Errorf("Size = %d, want 12", payload.Size)
	}
}

func TestNewFileContentEvent_Truncated(t *testing.T) {
	event := NewFileContentEvent("/path/to/large.go", "content...", 10000, true, "req-123")

	payload := event.Payload.(FileContentPayload)
	if !payload.Truncated {
		t.Error("Truncated should be true")
	}
}

func TestNewFileContentErrorEvent(t *testing.T) {
	requestID := "req-xyz"
	event := NewFileContentErrorEvent("/path/to/missing.go", "file not found", requestID)

	if event.Type() != EventTypeFileContent {
		t.Errorf("Type() = %v, want %v", event.Type(), EventTypeFileContent)
	}

	payload, ok := event.Payload.(FileContentPayload)
	if !ok {
		t.Fatal("Payload is not FileContentPayload")
	}

	if payload.Path != "/path/to/missing.go" {
		t.Errorf("Path = %q, want %q", payload.Path, "/path/to/missing.go")
	}
	if payload.Error != "file not found" {
		t.Errorf("Error = %q, want %q", payload.Error, "file not found")
	}
	if payload.Content != "" {
		t.Errorf("Content should be empty, got %q", payload.Content)
	}
}

func TestFileChangedPayload_JSON(t *testing.T) {
	event := NewFileChangedEvent("/src/main.go", FileChangeModified, 2048)

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event   string `json:"event"`
		Payload struct {
			Path   string `json:"path"`
			Change string `json:"change"`
			Size   int64  `json:"size"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.Payload.Path != "/src/main.go" {
		t.Errorf("path = %q, want /src/main.go", parsed.Payload.Path)
	}
	if parsed.Payload.Change != "modified" {
		t.Errorf("change = %q, want modified", parsed.Payload.Change)
	}
	if parsed.Payload.Size != 2048 {
		t.Errorf("size = %d, want 2048", parsed.Payload.Size)
	}
}

func TestFileContentPayload_JSON(t *testing.T) {
	event := NewFileContentEvent("/test.go", "package test", 12, false, "req-1")

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() error = %v", err)
	}

	var parsed struct {
		Event     string `json:"event"`
		RequestID string `json:"request_id"`
		Payload   struct {
			Path      string `json:"path"`
			Content   string `json:"content"`
			Encoding  string `json:"encoding"`
			Truncated bool   `json:"truncated"`
			Size      int64  `json:"size"`
		} `json:"payload"`
	}

	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	if parsed.RequestID != "req-1" {
		t.Errorf("request_id = %q, want req-1", parsed.RequestID)
	}
	if parsed.Payload.Content != "package test" {
		t.Errorf("content = %q, want 'package test'", parsed.Payload.Content)
	}
	if parsed.Payload.Encoding != "utf-8" {
		t.Errorf("encoding = %q, want utf-8", parsed.Payload.Encoding)
	}
}
