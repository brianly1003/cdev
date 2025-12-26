// Package live provides adapters for detecting and interacting with LIVE Claude sessions
// running in the user's terminal (not started by cdev).
package live

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LiveSession represents a detected Claude process running in user's terminal.
type LiveSession struct {
	PID         int       `json:"pid"`
	TTY         string    `json:"tty"`          // e.g., "/dev/ttys002"
	WorkDir     string    `json:"work_dir"`     // Working directory of the process
	SessionID   string    `json:"session_id"`   // Claude session ID from JSONL file
	TerminalApp string    `json:"terminal_app"` // The terminal app (Terminal, iTerm2, Code, Cursor, etc.)
	StartTime   time.Time `json:"start_time"`
}

// Detector finds and tracks running Claude processes not managed by cdev.
type Detector struct {
	workspacePath string              // Filter to only detect sessions for this workspace
	managedPIDs   map[int]bool        // PIDs managed by cdev (exclude from detection)
	cache         map[string]*LiveSession // session_id -> LiveSession cache
	cacheTTL      time.Duration
	lastScan      time.Time
	mu            sync.RWMutex
}

// NewDetector creates a new live session detector for a specific workspace.
func NewDetector(workspacePath string) *Detector {
	return &Detector{
		workspacePath: workspacePath,
		managedPIDs:   make(map[int]bool),
		cache:         make(map[string]*LiveSession),
		cacheTTL:      5 * time.Second, // Refresh cache every 5 seconds
	}
}

// RegisterManagedPID marks a PID as managed by cdev (will be excluded from detection).
func (d *Detector) RegisterManagedPID(pid int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.managedPIDs[pid] = true
	log.Debug().Int("pid", pid).Msg("registered managed PID")
}

// UnregisterManagedPID removes a PID from the managed list.
func (d *Detector) UnregisterManagedPID(pid int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.managedPIDs, pid)
	log.Debug().Int("pid", pid).Msg("unregistered managed PID")
}

// GetLiveSession returns a LIVE session by session ID if it exists.
// For the POC, we verify the session file exists in .claude/projects and
// find any Claude process running in the workspace to send keystrokes to.
func (d *Detector) GetLiveSession(sessionID string) *LiveSession {
	// First, verify the session file exists in .claude/projects
	if !d.sessionFileExists(sessionID) {
		log.Debug().Str("session_id", sessionID).Msg("session file not found in .claude/projects")
		return nil
	}

	// Refresh cache to find Claude processes
	d.refreshCache()

	d.mu.RLock()
	defer d.mu.RUnlock()

	// Return the first Claude process found in this workspace
	// For POC, we don't need exact session_id matching - just need a Claude process
	for _, session := range d.cache {
		log.Debug().
			Str("requested_session_id", sessionID).
			Str("found_session_id", session.SessionID).
			Str("terminal_app", session.TerminalApp).
			Msg("found LIVE Claude process for workspace")
		// Return with the requested session_id but the found process info
		return &LiveSession{
			PID:         session.PID,
			TTY:         session.TTY,
			WorkDir:     session.WorkDir,
			SessionID:   sessionID, // Use the requested session_id
			TerminalApp: session.TerminalApp,
			StartTime:   session.StartTime,
		}
	}

	log.Debug().Str("session_id", sessionID).Msg("no Claude process found in workspace")
	return nil
}

// sessionFileExists checks if a session file exists in .claude/projects
func (d *Detector) sessionFileExists(sessionID string) bool {
	if d.workspacePath == "" {
		return false
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	// Convert workspace path to Claude's project path format
	projectPath := strings.ReplaceAll(d.workspacePath, "/", "-")
	if !strings.HasPrefix(projectPath, "-") {
		projectPath = "-" + projectPath
	}

	sessionFile := filepath.Join(homeDir, ".claude", "projects", projectPath, sessionID+".jsonl")
	_, err = os.Stat(sessionFile)
	exists := err == nil

	log.Debug().
		Str("session_id", sessionID).
		Str("session_file", sessionFile).
		Bool("exists", exists).
		Msg("checking session file existence")

	return exists
}

// DetectAll finds all LIVE Claude processes.
func (d *Detector) DetectAll() ([]*LiveSession, error) {
	d.refreshCache()

	d.mu.RLock()
	defer d.mu.RUnlock()

	sessions := make([]*LiveSession, 0, len(d.cache))
	for _, session := range d.cache {
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// refreshCache scans for Claude processes and updates the cache.
func (d *Detector) refreshCache() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear old cache
	d.cache = make(map[string]*LiveSession)
	d.lastScan = time.Now()

	// Get all processes with 'claude' in command
	// ps -eo pid,tty,command
	out, err := exec.Command("ps", "-eo", "pid,tty,command").Output()
	if err != nil {
		log.Warn().Err(err).Msg("failed to run ps command")
		return
	}

	lines := strings.Split(string(out), "\n")

	// Regex to parse: PID TTY COMMAND
	// Example: " 9804 ttys034  claude"
	re := regexp.MustCompile(`^\s*(\d+)\s+(\S+)\s+(.*)$`)

	for _, line := range lines {
		// Skip if not a claude process
		if !strings.Contains(line, "claude") {
			continue
		}

		// Skip helper processes like claude-code-hook
		if strings.Contains(line, "claude-") {
			continue
		}

		// Skip grep itself
		if strings.Contains(line, "grep") {
			continue
		}

		matches := re.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		pid, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		tty := matches[2]

		// Skip if TTY is "?" (no TTY) or empty
		if tty == "?" || tty == "" {
			continue
		}

		// Skip managed PIDs
		if d.managedPIDs[pid] {
			continue
		}

		// Get working directory of the process
		workDir := d.getProcessWorkDir(pid)

		// Filter by workspace path if specified
		if d.workspacePath != "" && workDir != d.workspacePath {
			continue
		}

		// Find session ID from recent JSONL files
		sessionID := d.findSessionID(workDir, pid)
		if sessionID == "" {
			continue
		}

		// Convert tty to device path
		ttyPath := "/dev/" + tty

		// Find which terminal app owns this Claude process
		terminalApp := d.findTerminalApp(pid, tty)

		session := &LiveSession{
			PID:         pid,
			TTY:         ttyPath,
			WorkDir:     workDir,
			SessionID:   sessionID,
			TerminalApp: terminalApp,
			StartTime:   time.Now(), // Could get actual start time from ps lstart
		}

		d.cache[sessionID] = session
		log.Info().
			Int("pid", pid).
			Str("tty", ttyPath).
			Str("session_id", sessionID).
			Str("work_dir", workDir).
			Str("terminal_app", terminalApp).
			Msg("detected LIVE Claude session")
	}
}

// findTerminalApp finds the terminal application that owns the Claude process.
// It walks up the process tree to find the parent terminal app.
func (d *Detector) findTerminalApp(claudePID int, tty string) string {
	// Known terminal apps (in order of priority)
	knownApps := map[string]string{
		"Terminal":  "Terminal",
		"iTerm2":    "iTerm2",
		"iTerm.app": "iTerm2",
		"Code":      "Code",           // VS Code
		"Cursor":    "Cursor",
		"Warp":      "Warp",
		"Alacritty": "Alacritty",
		"Hyper":     "Hyper",
		"kitty":     "kitty",
	}

	// Walk up the process tree from Claude to find the terminal app
	currentPID := claudePID
	for i := 0; i < 10; i++ { // Max 10 levels up
		// Get parent PID
		out, err := exec.Command("ps", "-p", strconv.Itoa(currentPID), "-o", "ppid=").Output()
		if err != nil {
			break
		}
		ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil || ppid <= 1 {
			break
		}

		// Get the command name of the parent
		out, err = exec.Command("ps", "-p", strconv.Itoa(ppid), "-o", "comm=").Output()
		if err != nil {
			break
		}
		comm := strings.TrimSpace(string(out))

		// Check if this is a known terminal app
		for pattern, appName := range knownApps {
			if strings.Contains(comm, pattern) {
				return appName
			}
		}

		currentPID = ppid
	}

	// Fallback: check which apps are using this TTY
	out, err := exec.Command("lsof", "-t", "/dev/"+tty).Output()
	if err == nil {
		pids := strings.Split(strings.TrimSpace(string(out)), "\n")
		for _, pidStr := range pids {
			pid, err := strconv.Atoi(pidStr)
			if err != nil {
				continue
			}
			// Get command name
			out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
			if err != nil {
				continue
			}
			comm := strings.TrimSpace(string(out))
			for pattern, appName := range knownApps {
				if strings.Contains(comm, pattern) {
					return appName
				}
			}
		}
	}

	// Default fallback
	return "Terminal"
}

// getProcessWorkDir gets the current working directory of a process.
func (d *Detector) getProcessWorkDir(pid int) string {
	// macOS: use lsof to find cwd
	out, err := exec.Command("lsof", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return ""
	}

	// Parse lsof output to find cwd
	// Format: "n/path/to/dir" where first n after fcwd is the working dir
	lines := strings.Split(string(out), "\n")
	foundCwd := false
	for _, line := range lines {
		if line == "fcwd" {
			foundCwd = true
			continue
		}
		if foundCwd && strings.HasPrefix(line, "n/") {
			return line[1:] // Remove the 'n' prefix
		}
	}

	return ""
}

// findSessionID finds the most recently active session for a Claude process.
func (d *Detector) findSessionID(workDir string, pid int) string {
	if workDir == "" {
		return ""
	}

	// Convert workDir to Claude's project path format
	// /Users/brian/Projects/cdev -> -Users-brian-Projects-cdev
	projectPath := strings.ReplaceAll(workDir, "/", "-")
	if !strings.HasPrefix(projectPath, "-") {
		projectPath = "-" + projectPath
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	sessionsDir := filepath.Join(homeDir, ".claude", "projects", projectPath)

	// Find most recently modified JSONL file
	var newestFile string
	var newestTime time.Time

	err = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}

		if strings.HasSuffix(path, ".jsonl") {
			if info.ModTime().After(newestTime) {
				newestTime = info.ModTime()
				newestFile = filepath.Base(path)
			}
		}
		return nil
	})

	if err != nil || newestFile == "" {
		return ""
	}

	// Return session ID (filename without .jsonl)
	return strings.TrimSuffix(newestFile, ".jsonl")
}
