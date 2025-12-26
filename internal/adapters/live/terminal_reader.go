package live

import (
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// TerminalReader reads terminal content from native terminal applications.
// Platform-specific implementations:
// - macOS: AppleScript (Terminal.app, iTerm2)
// - Windows: UI Automation / PowerShell (Windows Terminal, cmd, PowerShell)
type TerminalReader struct {
	terminalApp  string
	lastContent  string
	pollInterval time.Duration
	outputChan   chan string
	stopChan     chan struct{}
	running      bool
	mu           sync.Mutex
}

// SupportedTerminals returns the list of supported terminals for the current platform.
func SupportedTerminals() []string {
	return supportedTerminals()
}

// NewTerminalReader creates a reader for a specific terminal application.
func NewTerminalReader(terminalApp string) *TerminalReader {
	return &TerminalReader{
		terminalApp:  terminalApp,
		outputChan:   make(chan string, 100),
		stopChan:     make(chan struct{}),
		pollInterval: 500 * time.Millisecond, // Poll every 500ms for permission prompts
	}
}

// SetPollInterval sets how often to poll for terminal content changes.
func (r *TerminalReader) SetPollInterval(interval time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pollInterval = interval
}

// Start begins polling the terminal for content changes.
func (r *TerminalReader) Start() error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return nil
	}
	r.running = true
	r.mu.Unlock()

	// Get initial content
	content, err := r.GetContent()
	if err != nil {
		log.Warn().Err(err).Str("app", r.terminalApp).Msg("failed to get initial terminal content")
	} else {
		r.lastContent = content
	}

	log.Info().
		Str("app", r.terminalApp).
		Msg("starting terminal content reader")

	go r.pollLoop()
	return nil
}

// Stop stops polling the terminal.
func (r *TerminalReader) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return
	}

	close(r.stopChan)
	r.running = false
	log.Info().Str("app", r.terminalApp).Msg("stopped terminal reader")
}

// Output returns the channel that receives new terminal content.
func (r *TerminalReader) Output() <-chan string {
	return r.outputChan
}

// IsRunning returns whether the reader is currently polling.
func (r *TerminalReader) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// pollLoop polls the terminal for content changes.
func (r *TerminalReader) pollLoop() {
	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopChan:
			return
		case <-ticker.C:
			r.checkForNewContent()
		}
	}
}

// checkForNewContent reads terminal content and detects changes.
func (r *TerminalReader) checkForNewContent() {
	content, err := r.GetContent()
	if err != nil {
		log.Debug().Err(err).Msg("failed to read terminal content")
		return
	}

	// Detect new content (simple diff - check if content changed)
	if content != r.lastContent {
		// For permission detection, we care about the current state, not just new content
		// Send the full recent content so PTY parser can analyze it
		select {
		case r.outputChan <- content:
		default:
			log.Warn().Msg("terminal output channel full, dropping content")
		}
	}

	r.lastContent = content
}

// GetContent reads the current content from the terminal window.
// This is implemented per-platform in terminal_reader_darwin.go and terminal_reader_windows.go
func (r *TerminalReader) GetContent() (string, error) {
	return getTerminalContent(r.terminalApp)
}

// GetLastNLines returns the last N lines from the terminal.
func (r *TerminalReader) GetLastNLines(n int) (string, error) {
	content, err := r.GetContent()
	if err != nil {
		return "", err
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= n {
		return content, nil
	}

	return strings.Join(lines[len(lines)-n:], "\n"), nil
}

// GetVisibleContent returns approximately the visible portion of the terminal.
// Default implementation returns last 50 lines.
func (r *TerminalReader) GetVisibleContent() (string, error) {
	return r.GetLastNLines(50)
}
