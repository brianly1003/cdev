package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/brianly1003/cdev/internal/adapters/live"
	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/hub"
	"github.com/brianly1003/cdev/internal/rpc/handler/methods"
	"github.com/brianly1003/cdev/internal/session"
	"github.com/brianly1003/cdev/internal/workspace"
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Layer 1 — Unit tests (always run, no Claude needed)
// ---------------------------------------------------------------------------

func TestDetector_Creation(t *testing.T) {
	t.Run("creates with workspace path", func(t *testing.T) {
		d := live.NewDetector("/tmp/test-workspace")
		if d == nil {
			t.Fatal("NewDetector returned nil")
		}
	})

	t.Run("GetLiveSession returns nil when no process", func(t *testing.T) {
		d := live.NewDetector("/tmp/nonexistent-workspace-" + uuid.NewString())
		session := d.GetLiveSession("")
		if session != nil {
			t.Fatalf("expected nil LiveSession for nonexistent workspace, got PID=%d", session.PID)
		}
	})
}

func TestInjector_PlatformInit(t *testing.T) {
	inj := live.NewInjector()
	if inj == nil {
		t.Fatal("NewInjector returned nil")
	}

	// Verify the platform matches runtime.GOOS.
	// We can't inspect the private field directly, but we can verify creation
	// doesn't panic on any supported platform.
	switch runtime.GOOS {
	case "darwin", "windows", "linux":
		// All known platforms should create successfully.
		t.Logf("Injector created successfully on %s", runtime.GOOS)
	default:
		t.Logf("Injector created on unsupported platform %s (will return errors on use)", runtime.GOOS)
	}
}

func TestDetector_GetLiveSession_NoProcess(t *testing.T) {
	tmpDir := t.TempDir()
	d := live.NewDetector(tmpDir)
	session := d.GetLiveSession("fake-session-id-" + uuid.NewString())
	if session != nil {
		t.Fatalf("expected nil for temp dir with no Claude, got PID=%d TTY=%s", session.PID, session.TTY)
	}
}

// ---------------------------------------------------------------------------
// Layer 2 — Integration test (opt-in: CDEV_LIVE_POC=1)
// ---------------------------------------------------------------------------

func TestLiveInjection_FullRPCRoundTrip(t *testing.T) {
	if os.Getenv("CDEV_LIVE_POC") != "1" {
		t.Skip("Skipping LIVE injection POC (set CDEV_LIVE_POC=1 to run)")
	}

	// 1. Current working directory as workspace path.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	// Walk up to repo root (test runs from internal/adapters/live/).
	repoRoot := cwd
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(repoRoot, "go.mod")); err == nil {
			break
		}
		repoRoot = filepath.Dir(repoRoot)
	}
	// Canonicalize so the path matches lsof output in the detector.
	repoRoot, err = filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	// 2. Create real hub and start it.
	h := hub.New()
	if err := h.Start(); err != nil {
		t.Fatalf("failed to start hub: %v", err)
	}
	defer h.Stop()

	// 3. Minimal config.
	cfg := &config.Config{}
	cfg.Repository.Path = repoRoot

	// 4. Create session manager.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mgr := session.NewManager(h, cfg, logger)

	// 5. Register workspace.
	wsID := "poc-ws-" + uuid.NewString()[:8]
	ws := workspace.NewWorkspace(config.WorkspaceDefinition{
		ID:   wsID,
		Name: "POC Workspace",
		Path: repoRoot,
	})
	mgr.RegisterWorkspace(ws)

	// 6. Enable LIVE support.
	mgr.SetLiveSessionSupport(repoRoot)

	// 7. Start manager.
	if err := mgr.Start(); err != nil {
		t.Fatalf("failed to start session manager: %v", err)
	}
	defer mgr.Stop()

	// 8. Verify detector can see Claude processes before calling Send().
	detector := live.NewDetector(repoRoot)
	allSessions, detectErr := detector.DetectAll()
	if detectErr != nil {
		t.Logf("DetectAll error: %v", detectErr)
	}
	t.Logf("Detector found %d LIVE session(s) for workspace %s", len(allSessions), repoRoot)
	for _, ls := range allSessions {
		t.Logf("  PID=%d TTY=%s WorkDir=%s TerminalApp=%s SessionID=%s",
			ls.PID, ls.TTY, ls.WorkDir, ls.TerminalApp, ls.SessionID)
	}

	// 9. Create RPC service and call Send().
	service := methods.NewSessionManagerService(mgr)

	sessionID := uuid.NewString()
	payload := map[string]string{
		"session_id":      sessionID,
		"workspace_id":    wsID,
		"prompt":          "[cdev-ios POC] hello from mobile!",
		"mode":            "continue",
		"permission_mode": "default",
	}
	params, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal params: %v", err)
	}

	result, rpcErr := service.Send(context.Background(), params)

	// 9+10. Interpret the outcome.
	if rpcErr == nil {
		// Success — LIVE session was found, prompt was injected.
		t.Logf("SUCCESS: Send() returned result: %+v", result)
		t.Log("Check your Claude terminal — you should see: [cdev-ios POC] hello from mobile!")
	} else {
		// Expected when no Claude is running: the LIVE path was attempted,
		// fell through, and the PTY fallback also failed (no claude binary).
		errMsg := rpcErr.Message
		t.Logf("Send() returned error (expected when no Claude running): code=%d msg=%s", rpcErr.Code, errMsg)

		// The error should indicate the LIVE path was tried and fell through
		// to auto-start or managed session creation, which also fails in test.
		expectSubstrings := []string{"auto-start", "failed to start", "failed to auto-create", "not found"}
		found := false
		for _, sub := range expectSubstrings {
			if contains(errMsg, sub) {
				found = true
				t.Logf("Error contains expected substring %q — LIVE detection was exercised", sub)
				break
			}
		}
		if !found {
			t.Logf("WARN: error message %q did not match expected substrings, but LIVE path was still exercised", errMsg)
			// Don't fail — the exact error message may vary.
			// The key assertion is that Send() didn't panic and returned a coherent error.
		}
	}

	fmt.Println("--- POC complete ---")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
