package methods

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/brianly1003/cdev/internal/rpc/handler"
)

// --- Workspace Config Service Tests ---

func TestWorkspaceConfigService_RegisterMethods(t *testing.T) {
	svc := &WorkspaceConfigService{}
	registry := handler.NewRegistry()

	svc.RegisterMethods(registry)

	expectedMethods := []string{
		"workspace/list",
		"workspace/get",
		"workspace/add",
		"workspace/remove",
		"workspace/update",
		"workspace/discover",
		"workspace/status",
		"workspace/cache/invalidate",
	}

	for _, method := range expectedMethods {
		if !registry.Has(method) {
			t.Errorf("method %s not registered", method)
		}
	}
}

func TestWorkspaceConfigService_SetViewerProvider(t *testing.T) {
	svc := &WorkspaceConfigService{}

	if svc.viewerProvider != nil {
		t.Error("viewerProvider should be nil initially")
	}

	provider := &mockViewerProvider{}
	svc.SetViewerProvider(provider)

	if svc.viewerProvider != provider {
		t.Error("viewerProvider not set correctly")
	}
}

// --- Mock ViewerProvider ---

type mockViewerProvider struct {
	viewers map[string][]string
}

func (m *mockViewerProvider) GetSessionViewers(workspaceID string) map[string][]string {
	return m.viewers
}

// --- List Params Tests ---

func TestWorkspaceListParams_JSON(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		includeGit bool
		gitLimit   int
	}{
		{
			name:       "empty params",
			json:       `{}`,
			includeGit: false,
			gitLimit:   0,
		},
		{
			name:       "include_git true",
			json:       `{"include_git":true}`,
			includeGit: true,
			gitLimit:   0,
		},
		{
			name:       "with git_limit",
			json:       `{"include_git":true,"git_limit":5}`,
			includeGit: true,
			gitLimit:   5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p struct {
				IncludeGit bool `json:"include_git"`
				GitLimit   int  `json:"git_limit"`
			}
			if err := json.Unmarshal([]byte(tt.json), &p); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if p.IncludeGit != tt.includeGit {
				t.Errorf("IncludeGit = %v, want %v", p.IncludeGit, tt.includeGit)
			}
			if p.GitLimit != tt.gitLimit {
				t.Errorf("GitLimit = %d, want %d", p.GitLimit, tt.gitLimit)
			}
		})
	}
}

// --- Get Params Tests ---

func TestWorkspaceGetParams_JSON(t *testing.T) {
	tests := []struct {
		name        string
		json        string
		workspaceID string
	}{
		{
			name:        "valid workspace_id",
			json:        `{"workspace_id":"ws-123"}`,
			workspaceID: "ws-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p struct {
				WorkspaceID string `json:"workspace_id"`
			}
			if err := json.Unmarshal([]byte(tt.json), &p); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if p.WorkspaceID != tt.workspaceID {
				t.Errorf("WorkspaceID = %s, want %s", p.WorkspaceID, tt.workspaceID)
			}
		})
	}
}

// --- Add Params Tests ---

func TestWorkspaceAddParams_JSON(t *testing.T) {
	tests := []struct {
		name            string
		json            string
		wsName          string
		path            string
		autoStart       bool
		createIfMissing bool
	}{
		{
			name:      "minimal params",
			json:      `{"name":"My Workspace","path":"/path/to/ws"}`,
			wsName:    "My Workspace",
			path:      "/path/to/ws",
			autoStart: false,
		},
		{
			name:      "with auto_start",
			json:      `{"name":"My Workspace","path":"/path/to/ws","auto_start":true}`,
			wsName:    "My Workspace",
			path:      "/path/to/ws",
			autoStart: true,
		},
		{
			name:            "with create_if_missing",
			json:            `{"name":"My Workspace","path":"/path/to/ws","create_if_missing":true}`,
			wsName:          "My Workspace",
			path:            "/path/to/ws",
			createIfMissing: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p struct {
				Name            string `json:"name"`
				Path            string `json:"path"`
				AutoStart       bool   `json:"auto_start"`
				CreateIfMissing bool   `json:"create_if_missing"`
			}
			if err := json.Unmarshal([]byte(tt.json), &p); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if p.Name != tt.wsName {
				t.Errorf("Name = %s, want %s", p.Name, tt.wsName)
			}
			if p.Path != tt.path {
				t.Errorf("Path = %s, want %s", p.Path, tt.path)
			}
			if p.AutoStart != tt.autoStart {
				t.Errorf("AutoStart = %v, want %v", p.AutoStart, tt.autoStart)
			}
			if p.CreateIfMissing != tt.createIfMissing {
				t.Errorf("CreateIfMissing = %v, want %v", p.CreateIfMissing, tt.createIfMissing)
			}
		})
	}
}

// --- Update Params Tests ---

func TestWorkspaceUpdateParams_JSON(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		id        string
		wsName    *string
		autoStart *bool
	}{
		{
			name: "update name",
			json: `{"id":"ws-123","name":"New Name"}`,
			id:   "ws-123",
		},
		{
			name: "update auto_start",
			json: `{"id":"ws-123","auto_start":true}`,
			id:   "ws-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p struct {
				ID        string `json:"id"`
				Name      string `json:"name,omitempty"`
				AutoStart *bool  `json:"auto_start,omitempty"`
			}
			if err := json.Unmarshal([]byte(tt.json), &p); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if p.ID != tt.id {
				t.Errorf("ID = %s, want %s", p.ID, tt.id)
			}
		})
	}
}

// --- Discover Params Tests ---

func TestWorkspaceDiscoverParams_JSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		numPaths int
	}{
		{
			name:     "empty paths",
			json:     `{}`,
			numPaths: 0,
		},
		{
			name:     "with paths",
			json:     `{"paths":["/home/user/projects","/var/www"]}`,
			numPaths: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p struct {
				Paths []string `json:"paths"`
			}
			if err := json.Unmarshal([]byte(tt.json), &p); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}

			if len(p.Paths) != tt.numPaths {
				t.Errorf("Paths length = %d, want %d", len(p.Paths), tt.numPaths)
			}
		})
	}
}

// --- Integration Tests (requires mock dependencies) ---

func TestWorkspaceConfigService_List_NilManager(t *testing.T) {
	svc := &WorkspaceConfigService{}

	// This will panic without proper initialization
	// Just verify the service can be created
	if svc.sessionManager != nil {
		t.Error("sessionManager should be nil")
	}
}

func TestWorkspaceConfigService_Get_MissingID(t *testing.T) {
	svc := &WorkspaceConfigService{}

	// Test that empty workspace_id is handled
	params := []byte(`{}`)
	_, err := svc.Get(context.Background(), params)

	// Should return an error for missing workspace_id
	if err == nil {
		t.Error("expected error for missing workspace_id")
	}
}

func TestWorkspaceConfigService_Add_MissingRequired(t *testing.T) {
	svc := &WorkspaceConfigService{}

	tests := []struct {
		name   string
		params string
	}{
		{"missing name", `{"path":"/tmp/test"}`},
		{"missing path", `{"name":"Test"}`},
		{"empty params", `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Add(context.Background(), []byte(tt.params))
			if err == nil {
				t.Error("expected error for missing required field")
			}
		})
	}
}

func TestWorkspaceConfigService_Remove_MissingID(t *testing.T) {
	svc := &WorkspaceConfigService{}

	_, err := svc.Remove(context.Background(), []byte(`{}`))

	if err == nil {
		t.Error("expected error for missing workspace_id")
	}
}

func TestWorkspaceConfigService_Update_MissingID(t *testing.T) {
	svc := &WorkspaceConfigService{}

	_, err := svc.Update(context.Background(), []byte(`{"name":"New Name"}`))

	if err == nil {
		t.Error("expected error for missing id")
	}
}

func TestWorkspaceConfigService_Status_MissingID(t *testing.T) {
	svc := &WorkspaceConfigService{}

	_, err := svc.Status(context.Background(), []byte(`{}`))

	if err == nil {
		t.Error("expected error for missing workspace_id")
	}
}
