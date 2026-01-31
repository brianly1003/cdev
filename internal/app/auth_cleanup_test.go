package app

import (
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/brianly1003/cdev/internal/workspace"
	"log/slog"
)

func TestCleanupOrphanedWorkspaces_RemovesWhenInactive(t *testing.T) {
	workspacePath := t.TempDir()
	workspacesPath := filepath.Join(t.TempDir(), "workspaces.yaml")

	def := config.WorkspaceDefinition{
		ID:        "workspace-1",
		Name:      "Test Workspace",
		Path:      workspacePath,
		CreatedAt: time.Now().UTC(),
	}

	cfg := &config.WorkspacesConfig{
		Workspaces: []config.WorkspaceDefinition{def},
	}
	if err := config.SaveWorkspaces(workspacesPath, cfg); err != nil {
		t.Fatalf("failed to save workspaces config: %v", err)
	}

	cfgMgr := workspace.NewConfigManager(cfg, workspacesPath)
	appCfg := newTestConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessMgr := session.NewManager(nil, appCfg, logger)

	for _, ws := range cfgMgr.ListWorkspaces() {
		sessMgr.RegisterWorkspace(ws)
	}

	a := &App{
		workspaceConfigManager: cfgMgr,
		sessionManager:         sessMgr,
	}

	a.cleanupOrphanedWorkspaces([]string{def.ID}, "test")

	if _, err := cfgMgr.GetWorkspace(def.ID); err == nil {
		t.Fatalf("expected workspace to be removed")
	}
}

func TestCleanupOrphanedWorkspaces_RetainsWhenActive(t *testing.T) {
	workspacePath := t.TempDir()
	workspacesPath := filepath.Join(t.TempDir(), "workspaces.yaml")

	def := config.WorkspaceDefinition{
		ID:        "workspace-2",
		Name:      "Active Workspace",
		Path:      workspacePath,
		CreatedAt: time.Now().UTC(),
	}

	cfg := &config.WorkspacesConfig{
		Workspaces: []config.WorkspaceDefinition{def},
	}
	if err := config.SaveWorkspaces(workspacesPath, cfg); err != nil {
		t.Fatalf("failed to save workspaces config: %v", err)
	}

	cfgMgr := workspace.NewConfigManager(cfg, workspacesPath)
	appCfg := newTestConfig()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sessMgr := session.NewManager(nil, appCfg, logger)

	for _, ws := range cfgMgr.ListWorkspaces() {
		sessMgr.RegisterWorkspace(ws)
	}

	if _, err := sessMgr.StartSession(def.ID); err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	a := &App{
		workspaceConfigManager: cfgMgr,
		sessionManager:         sessMgr,
	}

	a.cleanupOrphanedWorkspaces([]string{def.ID}, "test")

	if _, err := cfgMgr.GetWorkspace(def.ID); err != nil {
		t.Fatalf("expected workspace to be retained")
	}
}

func newTestConfig() *config.Config {
	return &config.Config{
		Claude: config.ClaudeConfig{
			Command: "claude",
		},
		Git: config.GitConfig{
			Enabled: false,
			Command: "git",
		},
		Watcher: config.WatcherConfig{
			Enabled: false,
		},
	}
}
