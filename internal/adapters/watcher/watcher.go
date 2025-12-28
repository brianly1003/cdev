// Package watcher implements the file system watcher using fsnotify.
package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/brianly1003/cdev/internal/domain/events"
	"github.com/brianly1003/cdev/internal/domain/ports"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

// pendingRename tracks a file that was renamed (we have the old path but not the new one yet)
type pendingRename struct {
	oldPath   string
	timestamp time.Time
}

// Watcher implements the FileWatcher port interface.
type Watcher struct {
	rootPath   string
	hub        ports.EventHub
	debounceMS int

	mu             sync.RWMutex
	watcher        *fsnotify.Watcher
	ignorePatterns []string
	running        bool
	cancel         context.CancelFunc

	// Debounce state
	debouncer *Debouncer

	// Rename tracking: maps directory -> pending rename info
	pendingRenames   map[string]pendingRename
	pendingRenamesMu sync.Mutex
}

// NewWatcher creates a new file system watcher.
func NewWatcher(rootPath string, hub ports.EventHub, debounceMS int, ignorePatterns []string) *Watcher {
	return &Watcher{
		rootPath:       rootPath,
		hub:            hub,
		debounceMS:     debounceMS,
		ignorePatterns: ignorePatterns,
		pendingRenames: make(map[string]pendingRename),
	}
}

// Start begins watching the repository directory.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.mu.Unlock()
		return err
	}
	w.watcher = watcher

	// Create cancellable context
	watchCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	// Create debouncer
	w.debouncer = NewDebouncer(time.Duration(w.debounceMS)*time.Millisecond, w.handleDebouncedEvent)

	w.running = true
	w.mu.Unlock()

	// Add watches recursively
	if err := w.addWatchRecursive(w.rootPath); err != nil {
		_ = w.Stop()
		return err
	}

	// Start event loop
	go w.eventLoop(watchCtx)

	// Start pending rename cleanup goroutine
	// On macOS, file deletions often come as RENAME events without a following CREATE
	// This goroutine detects stale pending renames and treats them as deletions
	go w.pendingRenameCleanup(watchCtx)

	log.Info().
		Str("path", w.rootPath).
		Int("debounce_ms", w.debounceMS).
		Msg("file watcher started")

	return nil
}

// Stop terminates file watching.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	w.running = false

	if w.cancel != nil {
		w.cancel()
	}

	if w.debouncer != nil {
		w.debouncer.Stop()
	}

	if w.watcher != nil {
		err := w.watcher.Close()
		w.watcher = nil
		log.Info().Msg("file watcher stopped")
		return err
	}

	return nil
}

// AddIgnorePattern adds a pattern to the ignore list.
func (w *Watcher) AddIgnorePattern(pattern string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ignorePatterns = append(w.ignorePatterns, pattern)
}

// RemoveIgnorePattern removes a pattern from the ignore list.
func (w *Watcher) RemoveIgnorePattern(pattern string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, p := range w.ignorePatterns {
		if p == pattern {
			w.ignorePatterns = append(w.ignorePatterns[:i], w.ignorePatterns[i+1:]...)
			return
		}
	}
}

// IsRunning returns true if the watcher is active.
func (w *Watcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// addWatchRecursive adds watches to a directory and all subdirectories.
func (w *Watcher) addWatchRecursive(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files/dirs we can't access
		}

		// Only watch directories
		if !info.IsDir() {
			return nil
		}

		// Check if should be ignored
		if w.shouldIgnore(path) {
			return filepath.SkipDir
		}

		// Add watch
		if err := w.watcher.Add(path); err != nil {
			log.Warn().Err(err).Str("path", path).Msg("failed to add watch")
			return nil // Continue even if we can't watch this dir
		}

		return nil
	})
}

// eventLoop handles fsnotify events.
func (w *Watcher) eventLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Warn().Err(err).Msg("watcher error")
		}
	}
}

// pendingRenameCleanup periodically checks for stale pending renames and treats them as deletions.
// On macOS, file deletions often trigger RENAME events without a following CREATE event.
// This goroutine runs every 500ms and emits delete events for pending renames older than 1 second.
func (w *Watcher) pendingRenameCleanup(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processStalePendingRenames()
		}
	}
}

// processStalePendingRenames checks for pending renames that have been waiting too long
// and treats them as file deletions.
func (w *Watcher) processStalePendingRenames() {
	w.pendingRenamesMu.Lock()
	defer w.pendingRenamesMu.Unlock()

	now := time.Now()
	for dir, pending := range w.pendingRenames {
		// If pending rename is older than 1 second, treat it as a deletion
		if now.Sub(pending.timestamp) > time.Second {
			delete(w.pendingRenames, dir)

			// Emit delete event for the old path
			log.Info().
				Str("path", pending.oldPath).
				Msg("stale pending rename treated as deletion")

			w.hub.Publish(events.NewFileChangedEvent(pending.oldPath, events.FileChangeDeleted, 0))
		}
	}
}

// handleEvent processes a single fsnotify event.
func (w *Watcher) handleEvent(event fsnotify.Event) {
	// Get relative path
	relPath, err := filepath.Rel(w.rootPath, event.Name)
	if err != nil {
		relPath = event.Name
	}

	// Check if should be ignored
	if w.shouldIgnore(event.Name) || w.shouldIgnore(relPath) {
		return
	}

	// Determine change type
	var changeType events.FileChangeType
	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		changeType = events.FileChangeCreated
		// If a directory was created, add watch to it
		if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
			_ = w.addWatchRecursive(event.Name)
		}
	case event.Op&fsnotify.Write == fsnotify.Write:
		changeType = events.FileChangeModified
	case event.Op&fsnotify.Remove == fsnotify.Remove:
		changeType = events.FileChangeDeleted
	case event.Op&fsnotify.Rename == fsnotify.Rename:
		// Track this rename - the old path is being renamed to something else
		// We'll match it with a CREATE event in the same directory
		dir := filepath.Dir(relPath)
		w.pendingRenamesMu.Lock()
		w.pendingRenames[dir] = pendingRename{
			oldPath:   relPath,
			timestamp: time.Now(),
		}
		w.pendingRenamesMu.Unlock()
		log.Debug().Str("old_path", relPath).Str("dir", dir).Msg("tracking pending rename")
		return // Don't emit event yet - wait for CREATE to get new path
	case event.Op&fsnotify.Chmod == fsnotify.Chmod:
		return // Ignore chmod events
	default:
		return
	}

	// Queue for debouncing
	w.debouncer.Add(relPath, changeType)
}

// handleDebouncedEvent is called after debounce window expires.
func (w *Watcher) handleDebouncedEvent(path string, changeType events.FileChangeType) {
	// Get file size for created/modified files
	var size int64
	if changeType != events.FileChangeDeleted {
		fullPath := filepath.Join(w.rootPath, path)
		if info, err := os.Stat(fullPath); err == nil {
			size = info.Size()
		}
	}

	// Check if this CREATE is actually part of a rename
	if changeType == events.FileChangeCreated {
		dir := filepath.Dir(path)
		w.pendingRenamesMu.Lock()
		pending, hasPending := w.pendingRenames[dir]
		if hasPending {
			// Check if the pending rename is recent (within 1 second)
			if time.Since(pending.timestamp) < time.Second {
				delete(w.pendingRenames, dir)
				w.pendingRenamesMu.Unlock()

				// This is a rename! Emit proper rename event with both paths
				w.hub.Publish(events.NewFileRenamedEvent(pending.oldPath, path))

				log.Info().
					Str("old_path", pending.oldPath).
					Str("new_path", path).
					Msg("file renamed")
				return
			}
			// Stale pending rename - clean it up
			delete(w.pendingRenames, dir)
		}
		w.pendingRenamesMu.Unlock()
	}

	// Publish regular event
	w.hub.Publish(events.NewFileChangedEvent(path, changeType, size))

	log.Debug().
		Str("path", path).
		Str("change", string(changeType)).
		Int64("size", size).
		Msg("file changed")
}

// shouldIgnore checks if a path should be ignored.
func (w *Watcher) shouldIgnore(path string) bool {
	// Get the base name and all path components
	base := filepath.Base(path)

	for _, pattern := range w.ignorePatterns {
		// Check if pattern matches base name
		if matched, _ := filepath.Match(pattern, base); matched {
			return true
		}

		// Check if any path component matches
		parts := splitPath(path)
		for _, part := range parts {
			if matched, _ := filepath.Match(pattern, part); matched {
				return true
			}
		}
	}

	return false
}

// splitPath splits a path into its components.
func splitPath(path string) []string {
	var parts []string
	for path != "" && path != "/" && path != "." {
		dir, file := filepath.Split(path)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		path = filepath.Clean(dir)
	}
	return parts
}
