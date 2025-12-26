package live

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// Injector sends input to a terminal for LIVE sessions.
// Supports multiple platforms with different injection methods.
type Injector struct {
	platform     Platform
	lastInject   time.Time
	minInterval  time.Duration // Minimum time between injections (rate limiting)
	mu           sync.Mutex
}

// Platform defines the interface for platform-specific injection.
type Platform interface {
	SendText(text string) error
	SendKey(key string) error
	SendKeyCode(keyCode int) error
	Name() string
}

// NewInjector creates a new injector for the current platform.
func NewInjector() *Injector {
	var platform Platform

	switch runtime.GOOS {
	case "darwin":
		platform = &MacOSPlatform{}
	case "windows":
		platform = &WindowsPlatform{}
	default:
		platform = &UnsupportedPlatform{os: runtime.GOOS}
	}

	log.Debug().Str("platform", platform.Name()).Msg("injector initialized")
	return &Injector{
		platform:    platform,
		minInterval: 500 * time.Millisecond, // Rate limit: max 2 injections per second
	}
}

// checkRateLimit ensures we don't inject too frequently.
func (i *Injector) checkRateLimit() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := time.Now()
	if now.Sub(i.lastInject) < i.minInterval {
		return fmt.Errorf("rate limited: please wait %v before next injection", i.minInterval-now.Sub(i.lastInject))
	}
	i.lastInject = now
	return nil
}

// SendToApp sends text as keystrokes to a specific terminal application.
func (i *Injector) SendToApp(text string, terminalApp string) error {
	// Rate limit check
	if err := i.checkRateLimit(); err != nil {
		log.Warn().Err(err).Msg("injection rate limited")
		return err
	}

	log.Info().
		Str("text", truncateText(text, 50)).
		Str("terminal_app", terminalApp).
		Str("platform", i.platform.Name()).
		Msg("injecting text to app")

	// Set target app if platform supports it
	if macos, ok := i.platform.(*MacOSPlatform); ok {
		macos.SetTargetApp(terminalApp)
	}

	return i.platform.SendText(text)
}

// SendWithEnterToApp sends text followed by Enter key to a specific app.
func (i *Injector) SendWithEnterToApp(text string, terminalApp string) error {
	// Rate limit check
	if err := i.checkRateLimit(); err != nil {
		log.Warn().Err(err).Msg("injection rate limited")
		return err
	}

	log.Info().
		Str("text", truncateText(text, 50)).
		Str("terminal_app", terminalApp).
		Str("platform", i.platform.Name()).
		Msg("injecting text with Enter to app")

	// Set target app if platform supports it
	if macos, ok := i.platform.(*MacOSPlatform); ok {
		macos.SetTargetApp(terminalApp)
	}

	if err := i.platform.SendText(text); err != nil {
		return err
	}
	// Small delay before sending Enter
	time.Sleep(100 * time.Millisecond)
	return i.platform.SendKey("return")
}

// SendKeyToApp sends a special key to a specific terminal application.
func (i *Injector) SendKeyToApp(key string, terminalApp string) error {
	// Rate limit check
	if err := i.checkRateLimit(); err != nil {
		log.Warn().Err(err).Msg("injection rate limited")
		return err
	}

	log.Info().
		Str("key", key).
		Str("terminal_app", terminalApp).
		Str("platform", i.platform.Name()).
		Msg("injecting key to app")

	// Set target app if platform supports it
	if macos, ok := i.platform.(*MacOSPlatform); ok {
		macos.SetTargetApp(terminalApp)
	}

	// Handle number keys as text
	if len(key) == 1 && key >= "1" && key <= "9" {
		return i.platform.SendText(key)
	}

	return i.platform.SendKey(key)
}

// Legacy methods for backward compatibility (use auto-detect)

// Send sends text as keystrokes to the terminal application.
func (i *Injector) Send(tty string, text string) error {
	return i.SendToApp(text, "") // Empty = auto-detect
}

// SendWithEnter sends text followed by Enter key.
func (i *Injector) SendWithEnter(tty string, text string) error {
	return i.SendWithEnterToApp(text, "") // Empty = auto-detect
}

// SendKey sends a special key to the terminal application.
func (i *Injector) SendKey(tty string, key string) error {
	return i.SendKeyToApp(key, "") // Empty = auto-detect
}

// ============================================================================
// macOS Platform (AppleScript)
// ============================================================================

// MacOSPlatform implements injection for macOS using AppleScript.
type MacOSPlatform struct {
	targetApp string // The terminal app to target (Terminal, iTerm2, Code, Cursor, etc.)
}

func (p *MacOSPlatform) Name() string {
	return "macos"
}

// SetTargetApp sets which application to send keystrokes to.
func (p *MacOSPlatform) SetTargetApp(app string) {
	p.targetApp = app
	log.Info().Str("app", app).Msg("target app set for keystroke injection")
}

// DetectTerminalApp tries to detect which terminal app is running Claude.
func (p *MacOSPlatform) DetectTerminalApp(tty string) string {
	// Try to find which app owns this TTY
	// Common terminal apps on macOS
	apps := []string{"Terminal", "iTerm2", "iTerm", "Code", "Cursor", "Warp", "Alacritty", "Hyper", "kitty"}

	// Check which terminal apps are running
	for _, app := range apps {
		cmd := exec.Command("pgrep", "-x", app)
		if err := cmd.Run(); err == nil {
			log.Debug().Str("app", app).Msg("detected running terminal app")
			return app
		}
	}

	// Default to Terminal if we can't detect
	return "Terminal"
}

func (p *MacOSPlatform) getTargetApp(tty string) string {
	if p.targetApp != "" {
		return p.targetApp
	}
	// Auto-detect if not set
	return p.DetectTerminalApp(tty)
}

func (p *MacOSPlatform) SendText(text string) error {
	return p.SendTextToApp(text, "")
}

func (p *MacOSPlatform) SendTextToApp(text string, tty string) error {
	// Escape special characters for AppleScript
	escaped := strings.ReplaceAll(text, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")

	app := p.getTargetApp(tty)

	// First activate the app, then send keystroke
	script := fmt.Sprintf(`
		tell application "%s" to activate
		delay 0.1
		tell application "System Events"
			keystroke "%s"
		end tell
	`, app, escaped)

	return p.runAppleScript(script)
}

func (p *MacOSPlatform) SendKey(key string) error {
	return p.SendKeyToApp(key, "")
}

func (p *MacOSPlatform) SendKeyToApp(key string, tty string) error {
	// Map key names to macOS key codes
	keyCodes := map[string]int{
		"return":    36,
		"enter":     36,
		"tab":       48,
		"delete":    51,
		"backspace": 51,
		"escape":    53,
		"esc":       53,
		"left":      123,
		"right":     124,
		"down":      125,
		"up":        126,
		"space":     49,
	}

	if code, ok := keyCodes[key]; ok {
		return p.SendKeyCodeToApp(code, tty)
	}

	return fmt.Errorf("unknown key: %s", key)
}

func (p *MacOSPlatform) SendKeyCode(keyCode int) error {
	return p.SendKeyCodeToApp(keyCode, "")
}

func (p *MacOSPlatform) SendKeyCodeToApp(keyCode int, tty string) error {
	app := p.getTargetApp(tty)

	// First activate the app, then send key code
	script := fmt.Sprintf(`
		tell application "%s" to activate
		delay 0.1
		tell application "System Events"
			key code %d
		end tell
	`, app, keyCode)

	return p.runAppleScript(script)
}

func (p *MacOSPlatform) runAppleScript(script string) error {
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Warn().
			Err(err).
			Str("output", string(output)).
			Msg("AppleScript failed")
		return fmt.Errorf("AppleScript failed: %w", err)
	}
	return nil
}

// ============================================================================
// Windows Platform (PowerShell/SendKeys)
// ============================================================================

// WindowsPlatform implements injection for Windows using PowerShell SendKeys.
type WindowsPlatform struct{}

func (p *WindowsPlatform) Name() string {
	return "windows"
}

func (p *WindowsPlatform) SendText(text string) error {
	// Escape special characters for SendKeys
	// See: https://docs.microsoft.com/en-us/dotnet/api/system.windows.forms.sendkeys
	escaped := text
	for _, char := range []string{"+", "^", "%", "~", "(", ")", "{", "}", "[", "]"} {
		escaped = strings.ReplaceAll(escaped, char, "{"+char+"}")
	}

	script := fmt.Sprintf(`
		Add-Type -AssemblyName System.Windows.Forms
		[System.Windows.Forms.SendKeys]::SendWait("%s")
	`, escaped)

	return p.runPowerShell(script)
}

func (p *WindowsPlatform) SendKey(key string) error {
	// Map key names to SendKeys codes
	keyCodes := map[string]string{
		"return":    "{ENTER}",
		"enter":     "{ENTER}",
		"tab":       "{TAB}",
		"delete":    "{DELETE}",
		"backspace": "{BACKSPACE}",
		"escape":    "{ESC}",
		"esc":       "{ESC}",
		"left":      "{LEFT}",
		"right":     "{RIGHT}",
		"down":      "{DOWN}",
		"up":        "{UP}",
		"space":     " ",
	}

	if code, ok := keyCodes[key]; ok {
		script := fmt.Sprintf(`
			Add-Type -AssemblyName System.Windows.Forms
			[System.Windows.Forms.SendKeys]::SendWait("%s")
		`, code)
		return p.runPowerShell(script)
	}

	return fmt.Errorf("unknown key: %s", key)
}

func (p *WindowsPlatform) SendKeyCode(keyCode int) error {
	// Windows doesn't use key codes the same way, use virtual key codes
	// This is a simplified implementation
	return fmt.Errorf("SendKeyCode not implemented for Windows, use SendKey instead")
}

func (p *WindowsPlatform) runPowerShell(script string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Warn().
			Err(err).
			Str("output", string(output)).
			Msg("PowerShell failed")
		return fmt.Errorf("PowerShell failed: %w", err)
	}
	return nil
}

// ============================================================================
// Unsupported Platform
// ============================================================================

// UnsupportedPlatform is used for platforms without injection support.
type UnsupportedPlatform struct {
	os string
}

func (p *UnsupportedPlatform) Name() string {
	return p.os + " (unsupported)"
}

func (p *UnsupportedPlatform) SendText(text string) error {
	return fmt.Errorf("keystroke injection not supported on %s", p.os)
}

func (p *UnsupportedPlatform) SendKey(key string) error {
	return fmt.Errorf("keystroke injection not supported on %s", p.os)
}

func (p *UnsupportedPlatform) SendKeyCode(keyCode int) error {
	return fmt.Errorf("keystroke injection not supported on %s", p.os)
}

// ============================================================================
// Helpers
// ============================================================================

// truncateText truncates text for logging.
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}
