// Package workspace provides workspace configuration management.
package workspace

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

// DiscoveryConfig holds configuration for repository discovery.
type DiscoveryConfig struct {
	// MaxDepth limits how deep to scan (0 = unlimited, recommended: 4)
	MaxDepth int
	// Timeout for the entire discovery operation
	Timeout time.Duration
	// Workers is the number of parallel workers (0 = NumCPU)
	Workers int
	// CacheTTL is how long cached results are valid
	CacheTTL time.Duration
	// CachePath is where to store the cache file
	CachePath string
	// UserSearchPaths are additional paths from config.yaml (scanned before defaults)
	UserSearchPaths []string
}

// DefaultDiscoveryConfig returns sensible defaults.
func DefaultDiscoveryConfig() DiscoveryConfig {
	cacheDir, _ := os.UserCacheDir()
	if cacheDir == "" {
		cacheDir = os.TempDir()
	}

	return DiscoveryConfig{
		MaxDepth:  4,                     // Don't go too deep
		Timeout:   10 * time.Second,      // Max 10 seconds
		Workers:   runtime.NumCPU(),      // Use all cores
		CacheTTL:  1 * time.Hour,         // Cache for 1 hour
		CachePath: filepath.Join(cacheDir, "cdev", "discovered-repos.json"),
	}
}

// DiscoveryResult contains the results of a discovery operation.
type DiscoveryResult struct {
	Repositories      []DiscoveredRepo `json:"repositories"`
	Count             int              `json:"count"`
	Cached            bool             `json:"cached"`
	CacheAgeSeconds   int64            `json:"cache_age_seconds,omitempty"`
	RefreshInProgress bool             `json:"refresh_in_progress,omitempty"`
	ElapsedMs         int64            `json:"elapsed_ms"`
	ScannedPaths      int              `json:"scanned_paths"`
	SkippedPaths      int              `json:"skipped_paths"`
}

// DiscoveryCache represents the cached discovery results.
type DiscoveryCache struct {
	Repositories []DiscoveredRepo `json:"repositories"`
	SearchPaths  []string         `json:"search_paths"`
	Timestamp    time.Time        `json:"timestamp"`
	Version      int              `json:"version"`
}

const cacheVersion = 1

// RepoDiscovery handles repository discovery with caching and parallel scanning.
type RepoDiscovery struct {
	config           DiscoveryConfig
	configuredPaths  map[string]bool
	mu               sync.RWMutex
	refreshing       bool
	lastRefreshStart time.Time
}

// NewRepoDiscovery creates a new repository discovery engine.
func NewRepoDiscovery(cfg DiscoveryConfig) *RepoDiscovery {
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.NumCPU()
	}
	if cfg.MaxDepth <= 0 {
		cfg.MaxDepth = 4
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 1 * time.Hour
	}

	return &RepoDiscovery{
		config:          cfg,
		configuredPaths: make(map[string]bool),
	}
}

// SetConfiguredPaths updates the list of already-configured workspace paths.
func (d *RepoDiscovery) SetConfiguredPaths(paths map[string]bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.configuredPaths = paths
}

// Discover finds git repositories using cache-first strategy.
// Returns cached results immediately if available, triggers background refresh if stale.
func (d *RepoDiscovery) Discover(ctx context.Context, searchPaths []string) (*DiscoveryResult, error) {
	start := time.Now()

	// Normalize search paths
	searchPaths = d.normalizeSearchPaths(searchPaths)

	// Try to load from cache first
	cache, cacheErr := d.loadCache()
	if cacheErr == nil && cache != nil {
		cacheAge := time.Since(cache.Timestamp)

		// Check if cache is still valid
		if cacheAge < d.config.CacheTTL && d.pathsMatch(cache.SearchPaths, searchPaths) {
			// Cache is fresh - return it
			repos := d.enrichWithConfiguredStatus(cache.Repositories)
			return &DiscoveryResult{
				Repositories:      repos,
				Count:             len(repos),
				Cached:            true,
				CacheAgeSeconds:   int64(cacheAge.Seconds()),
				RefreshInProgress: d.isRefreshing(),
				ElapsedMs:         time.Since(start).Milliseconds(),
			}, nil
		}

		// Cache is stale - return it but trigger background refresh
		if !d.isRefreshing() {
			go d.backgroundRefresh(searchPaths)
		}

		repos := d.enrichWithConfiguredStatus(cache.Repositories)
		return &DiscoveryResult{
			Repositories:      repos,
			Count:             len(repos),
			Cached:            true,
			CacheAgeSeconds:   int64(cacheAge.Seconds()),
			RefreshInProgress: true,
			ElapsedMs:         time.Since(start).Milliseconds(),
		}, nil
	}

	// No cache - do a fresh scan (with timeout)
	scanCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()

	repos, scanned, skipped, err := d.parallelScan(scanCtx, searchPaths)
	if err != nil && err != context.DeadlineExceeded {
		return nil, err
	}

	// Save to cache (even partial results on timeout)
	d.saveCache(repos, searchPaths)

	repos = d.enrichWithConfiguredStatus(repos)
	return &DiscoveryResult{
		Repositories: repos,
		Count:        len(repos),
		Cached:       false,
		ElapsedMs:    time.Since(start).Milliseconds(),
		ScannedPaths: scanned,
		SkippedPaths: skipped,
	}, nil
}

// DiscoverFresh forces a fresh scan, ignoring cache.
func (d *RepoDiscovery) DiscoverFresh(ctx context.Context, searchPaths []string) (*DiscoveryResult, error) {
	start := time.Now()
	searchPaths = d.normalizeSearchPaths(searchPaths)

	scanCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()

	repos, scanned, skipped, err := d.parallelScan(scanCtx, searchPaths)
	if err != nil && err != context.DeadlineExceeded {
		return nil, err
	}

	// Save to cache
	d.saveCache(repos, searchPaths)

	repos = d.enrichWithConfiguredStatus(repos)
	return &DiscoveryResult{
		Repositories: repos,
		Count:        len(repos),
		Cached:       false,
		ElapsedMs:    time.Since(start).Milliseconds(),
		ScannedPaths: scanned,
		SkippedPaths: skipped,
	}, nil
}

// DiscoverStreaming discovers repositories and sends them to a channel as found.
// This is useful for progressive UI updates.
func (d *RepoDiscovery) DiscoverStreaming(ctx context.Context, searchPaths []string, results chan<- DiscoveredRepo) error {
	defer close(results)

	searchPaths = d.normalizeSearchPaths(searchPaths)
	scanCtx, cancel := context.WithTimeout(ctx, d.config.Timeout)
	defer cancel()

	return d.parallelScanStreaming(scanCtx, searchPaths, results)
}

// parallelScan performs parallel directory scanning using worker pool.
func (d *RepoDiscovery) parallelScan(ctx context.Context, searchPaths []string) ([]DiscoveredRepo, int, int, error) {
	var (
		results      []DiscoveredRepo
		resultsMu    sync.Mutex
		seen         = make(map[string]bool)
		seenMu       sync.Mutex
		scannedCount int
		skippedCount int
		countMu      sync.Mutex
		inFlight     int64 // Track number of jobs being processed
		closed       int32 // Flag to indicate channel is closed
	)

	// Channel for directories to scan
	dirQueue := make(chan scanJob, 10000)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < d.config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range dirQueue {
				select {
				case <-ctx.Done():
					atomic.AddInt64(&inFlight, -1)
					return
				default:
				}

				d.processDirectory(ctx, job, dirQueue, &results, &resultsMu, seen, &seenMu, &scannedCount, &skippedCount, &countMu, &inFlight, &closed)
				atomic.AddInt64(&inFlight, -1)
			}
		}()
	}

	// Seed the queue with initial paths
	for _, path := range searchPaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			atomic.AddInt64(&inFlight, 1)
			dirQueue <- scanJob{path: path, depth: 0}
		}
	}

	// Wait for all workers to finish (with timeout awareness)
	done := make(chan struct{})
	go func() {
		// Give workers time to start processing
		time.Sleep(100 * time.Millisecond)

		for {
			select {
			case <-ctx.Done():
				atomic.StoreInt32(&closed, 1)
				close(dirQueue)
				wg.Wait()
				close(done)
				return
			default:
				// Check if queue is empty AND no workers are processing
				queueLen := len(dirQueue)
				processing := atomic.LoadInt64(&inFlight)
				if queueLen == 0 && processing == 0 {
					atomic.StoreInt32(&closed, 1)
					close(dirQueue)
					wg.Wait()
					close(done)
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	<-done

	// Sort results by path for consistent ordering
	sort.Slice(results, func(i, j int) bool {
		return results[i].Path < results[j].Path
	})

	return results, scannedCount, skippedCount, ctx.Err()
}

type scanJob struct {
	path  string
	depth int
}

func (d *RepoDiscovery) processDirectory(
	ctx context.Context,
	job scanJob,
	queue chan<- scanJob,
	results *[]DiscoveredRepo,
	resultsMu *sync.Mutex,
	seen map[string]bool,
	seenMu *sync.Mutex,
	scannedCount, skippedCount *int,
	countMu *sync.Mutex,
	inFlight *int64,
	closed *int32,
) {
	// Check context
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Check depth limit
	if d.config.MaxDepth > 0 && job.depth > d.config.MaxDepth {
		countMu.Lock()
		*skippedCount++
		countMu.Unlock()
		return
	}

	countMu.Lock()
	*scannedCount++
	countMu.Unlock()

	// Read directory entries
	entries, err := os.ReadDir(job.path)
	if err != nil {
		return
	}

	for _, entry := range entries {
		// Skip symlinks to prevent traversal outside intended search paths.
		// A symlink could point to /etc, ~/.ssh, or create infinite loops.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		fullPath := filepath.Join(job.path, name)

		// Check if this is a .git directory (found a repo!)
		if name == ".git" {
			repoPath := job.path

			// Use normalized path for deduplication (handles case-insensitive filesystems)
			normalizedRepoPath := normalizePathForComparison(repoPath)

			seenMu.Lock()
			if seen[normalizedRepoPath] {
				seenMu.Unlock()
				continue
			}
			seen[normalizedRepoPath] = true
			seenMu.Unlock()

			repo := getRepoInfo(repoPath)
			resultsMu.Lock()
			*results = append(*results, repo)
			resultsMu.Unlock()

			log.Debug().Str("path", repoPath).Msg("discovered repository")
			continue
		}

		// Skip directories that should be ignored
		if d.shouldSkipDirectory(name) {
			countMu.Lock()
			*skippedCount++
			countMu.Unlock()
			continue
		}

		// Skip hidden directories (except we already handled .git)
		if strings.HasPrefix(name, ".") {
			countMu.Lock()
			*skippedCount++
			countMu.Unlock()
			continue
		}

		// Check if channel is closed before sending
		if atomic.LoadInt32(closed) != 0 {
			countMu.Lock()
			*skippedCount++
			countMu.Unlock()
			continue
		}

		// Queue subdirectory for scanning - increment inFlight before sending
		atomic.AddInt64(inFlight, 1)
		select {
		case queue <- scanJob{path: fullPath, depth: job.depth + 1}:
			// Successfully queued
		default:
			// Queue full, skip this directory
			atomic.AddInt64(inFlight, -1)
			countMu.Lock()
			*skippedCount++
			countMu.Unlock()
		}
	}
}

// parallelScanStreaming is like parallelScan but sends results to a channel.
func (d *RepoDiscovery) parallelScanStreaming(ctx context.Context, searchPaths []string, results chan<- DiscoveredRepo) error {
	var (
		seen     = make(map[string]bool)
		seenMu   sync.Mutex
		inFlight int64 // Track number of jobs being processed
		closed   int32 // Flag to indicate channel is closed
	)

	dirQueue := make(chan scanJob, 10000)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < d.config.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range dirQueue {
				select {
				case <-ctx.Done():
					atomic.AddInt64(&inFlight, -1)
					return
				default:
				}

				d.processDirectoryStreaming(ctx, job, dirQueue, results, seen, &seenMu, &inFlight, &closed)
				atomic.AddInt64(&inFlight, -1)
			}
		}()
	}

	// Seed the queue
	for _, path := range searchPaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			atomic.AddInt64(&inFlight, 1)
			dirQueue <- scanJob{path: path, depth: 0}
		}
	}

	// Wait for completion
	done := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)

		for {
			select {
			case <-ctx.Done():
				atomic.StoreInt32(&closed, 1)
				close(dirQueue)
				wg.Wait()
				close(done)
				return
			default:
				// Check if queue is empty AND no workers are processing
				queueLen := len(dirQueue)
				processing := atomic.LoadInt64(&inFlight)
				if queueLen == 0 && processing == 0 {
					atomic.StoreInt32(&closed, 1)
					close(dirQueue)
					wg.Wait()
					close(done)
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	<-done
	return ctx.Err()
}

func (d *RepoDiscovery) processDirectoryStreaming(
	ctx context.Context,
	job scanJob,
	queue chan<- scanJob,
	results chan<- DiscoveredRepo,
	seen map[string]bool,
	seenMu *sync.Mutex,
	inFlight *int64,
	closed *int32,
) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	if d.config.MaxDepth > 0 && job.depth > d.config.MaxDepth {
		return
	}

	entries, err := os.ReadDir(job.path)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		fullPath := filepath.Join(job.path, name)

		if name == ".git" {
			repoPath := job.path

			// Use normalized path for deduplication (handles case-insensitive filesystems)
			normalizedRepoPath := normalizePathForComparison(repoPath)

			seenMu.Lock()
			if seen[normalizedRepoPath] {
				seenMu.Unlock()
				continue
			}
			seen[normalizedRepoPath] = true
			seenMu.Unlock()

			repo := getRepoInfo(repoPath)

			// Send to results channel (non-blocking)
			select {
			case results <- repo:
			case <-ctx.Done():
				return
			}
			continue
		}

		if d.shouldSkipDirectory(name) || strings.HasPrefix(name, ".") {
			continue
		}

		// Check if channel is closed before sending
		if atomic.LoadInt32(closed) != 0 {
			continue
		}

		// Queue subdirectory for scanning - increment inFlight before sending
		atomic.AddInt64(inFlight, 1)
		select {
		case queue <- scanJob{path: fullPath, depth: job.depth + 1}:
			// Successfully queued
		default:
			// Queue full, skip this directory
			atomic.AddInt64(inFlight, -1)
		}
	}
}

// shouldSkipDirectory returns true if the directory should be skipped.
func (d *RepoDiscovery) shouldSkipDirectory(name string) bool {
	return skipDirectories[strings.ToLower(name)]
}

// skipDirectories is a comprehensive list of directories to skip.
// This is based on enterprise patterns from VS Code, JetBrains, etc.
var skipDirectories = map[string]bool{
	// Version Control
	".git": true, ".svn": true, ".hg": true, ".bzr": true,

	// Package Managers - JavaScript/Node
	"node_modules": true, "bower_components": true, ".npm": true, ".yarn": true, ".pnpm": true,

	// Package Managers - Other Languages
	"vendor": true, "packages": true, ".packages": true, "Pods": true,
	"Carthage": true, ".bundle": true, ".pub-cache": true,

	// Python
	"__pycache__": true, ".pytest_cache": true, ".mypy_cache": true,
	".tox": true, ".venv": true, "venv": true, "env": true,
	".eggs": true, ".ruff_cache": true, ".uv": true,

	// Build Output
	"dist": true, "build": true, "target": true, "out": true, "output": true,
	"bin": true, "obj": true, ".next": true, ".nuxt": true, ".output": true,
	"_build": true, ".build": true,

	// IDE/Editor
	".idea": true, ".vscode": true, ".vs": true, ".settings": true,
	".project": true, ".classpath": true,

	// Cache/Temp
	".cache": true, ".parcel-cache": true, ".turbo": true,
	".gradle": true, ".m2": true, ".cargo": true, ".rustup": true,
	"tmp": true, "temp": true, ".tmp": true,

	// macOS Specific
	"Library":          true,
	".Trash":           true,
	"Applications":     true,
	"Movies":           true,
	"Music":            true,
	"Pictures":         true,
	".Spotlight-V100":  true,
	".fseventsd":       true,
	".DocumentRevisions-V100": true,

	// Windows Specific
	"AppData":        true,
	"$RECYCLE.BIN":   true,
	"System Volume Information": true,

	// Linux Specific
	".local": true, ".config": true,

	// Testing/Coverage
	"coverage": true, ".coverage": true, ".nyc_output": true,
	"htmlcov": true, ".hypothesis": true, "__snapshots__": true,

	// Logs
	"logs": true, "log": true,

	// Our own tool
	".cdev": true, ".claude": true,

	// Other common excludes
	"DerivedData":      true, // Xcode
	".terraform":       true,
	".vagrant":         true,
	".docker":          true,
	"CMakeFiles":       true,
	".ccache":          true,
	".stack-work":      true, // Haskell
	"_deps":            true, // CMake
	"bazel-bin":        true,
	"bazel-out":        true,
	"bazel-testlogs":   true,
}

// normalizeSearchPaths normalizes and filters search paths.
// Uses case-insensitive comparison to avoid duplicate scanning on macOS/Windows.
func (d *RepoDiscovery) normalizeSearchPaths(paths []string) []string {
	if len(paths) == 0 {
		return d.defaultSearchPaths()
	}

	result := make([]string, 0, len(paths))
	seen := make(map[string]bool)

	for _, p := range paths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}

		// Use lowercase for deduplication (case-insensitive filesystems like macOS/Windows)
		normalizedPath := normalizePathForComparison(absPath)
		if seen[normalizedPath] {
			continue
		}
		seen[normalizedPath] = true

		// Verify path exists
		if _, err := os.Stat(absPath); err == nil {
			result = append(result, absPath)
		}
	}

	return result
}

// normalizePathForComparison returns a normalized path for case-insensitive comparison.
// This handles macOS (APFS/HFS+) and Windows (NTFS) which are case-insensitive by default.
func normalizePathForComparison(path string) string {
	// Use lowercase for comparison to handle case-insensitive filesystems
	return strings.ToLower(filepath.Clean(path))
}

// defaultSearchPaths returns smart default paths to search.
// User-configured paths (from config.yaml discovery.search_paths) are scanned first,
// followed by built-in defaults. This ensures developers who store repos in
// non-standard locations get their repositories discovered automatically.
// NOTE: Excludes Documents by default as it's typically slow and rarely contains repos.
func (d *RepoDiscovery) defaultSearchPaths() []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	// Start with user-configured paths (highest priority, scanned first).
	// Tilde (~) is expanded to $HOME for convenience.
	candidates := make([]string, 0, len(d.config.UserSearchPaths)+13)
	for _, p := range d.config.UserSearchPaths {
		if strings.HasPrefix(p, "~/") || p == "~" {
			p = filepath.Join(homeDir, p[1:])
		}
		candidates = append(candidates, p)
	}

	// Built-in defaults (most likely to contain repos first).
	// Include both case variants to support Linux (case-sensitive) properly.
	candidates = append(candidates,
		filepath.Join(homeDir, "Projects"),
		filepath.Join(homeDir, "projects"),
		filepath.Join(homeDir, "Code"),
		filepath.Join(homeDir, "code"),
		filepath.Join(homeDir, "Developer"),
		filepath.Join(homeDir, "dev"),
		filepath.Join(homeDir, "Repos"),
		filepath.Join(homeDir, "repos"),
		filepath.Join(homeDir, "src"),
		filepath.Join(homeDir, "go", "src"),
		filepath.Join(homeDir, "workspace"),
		filepath.Join(homeDir, "Workspace"),
		filepath.Join(homeDir, "Desktop"),
		// Documents intentionally excluded - too slow, rarely has repos
	)

	// Deduplicate paths (handles case-insensitive filesystems like macOS/Windows)
	result := make([]string, 0)
	seen := make(map[string]bool)

	for _, p := range candidates {
		absPath, absErr := filepath.Abs(p)
		if absErr != nil {
			continue
		}
		normalizedPath := normalizePathForComparison(absPath)
		if seen[normalizedPath] {
			continue
		}

		if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
			seen[normalizedPath] = true
			result = append(result, absPath)
		}
	}

	return result
}

// enrichWithConfiguredStatus marks repos that are already configured.
func (d *RepoDiscovery) enrichWithConfiguredStatus(repos []DiscoveredRepo) []DiscoveredRepo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for i := range repos {
		repos[i].IsConfigured = d.configuredPaths[repos[i].Path]
	}
	return repos
}

// pathsMatch checks if two path slices contain the same paths.
func (d *RepoDiscovery) pathsMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, p := range a {
		aMap[p] = true
	}

	for _, p := range b {
		if !aMap[p] {
			return false
		}
	}
	return true
}

// isRefreshing returns true if a background refresh is in progress.
func (d *RepoDiscovery) isRefreshing() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.refreshing
}

// backgroundRefresh performs a fresh scan in the background and updates the cache.
func (d *RepoDiscovery) backgroundRefresh(searchPaths []string) {
	d.mu.Lock()
	if d.refreshing {
		d.mu.Unlock()
		return
	}
	d.refreshing = true
	d.lastRefreshStart = time.Now()
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.refreshing = false
		d.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), d.config.Timeout)
	defer cancel()

	log.Debug().Strs("paths", searchPaths).Msg("starting background repository discovery refresh")

	repos, scanned, skipped, err := d.parallelScan(ctx, searchPaths)
	if err != nil && err != context.DeadlineExceeded {
		log.Warn().Err(err).Msg("background discovery refresh failed")
		return
	}

	d.saveCache(repos, searchPaths)

	log.Info().
		Int("repositories", len(repos)).
		Int("scanned", scanned).
		Int("skipped", skipped).
		Dur("elapsed", time.Since(d.lastRefreshStart)).
		Msg("background repository discovery refresh complete")
}

// loadCache loads the cached discovery results.
func (d *RepoDiscovery) loadCache() (*DiscoveryCache, error) {
	data, err := os.ReadFile(d.config.CachePath)
	if err != nil {
		return nil, err
	}

	var cache DiscoveryCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	// Check version compatibility
	if cache.Version != cacheVersion {
		return nil, os.ErrNotExist
	}

	return &cache, nil
}

// saveCache saves the discovery results to cache.
func (d *RepoDiscovery) saveCache(repos []DiscoveredRepo, searchPaths []string) {
	cache := DiscoveryCache{
		Repositories: repos,
		SearchPaths:  searchPaths,
		Timestamp:    time.Now(),
		Version:      cacheVersion,
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		log.Warn().Err(err).Msg("failed to marshal discovery cache")
		return
	}

	// Ensure cache directory exists (owner-only access)
	cacheDir := filepath.Dir(d.config.CachePath)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		log.Warn().Err(err).Msg("failed to create cache directory")
		return
	}

	// Atomic write: write to temp file then rename to prevent partial reads
	tmpPath := d.config.CachePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		log.Warn().Err(err).Msg("failed to write discovery cache temp file")
		return
	}
	if err := os.Rename(tmpPath, d.config.CachePath); err != nil {
		log.Warn().Err(err).Msg("failed to rename discovery cache temp file")
		os.Remove(tmpPath)
	}
}

// InvalidateCache clears the discovery cache.
func (d *RepoDiscovery) InvalidateCache() error {
	return os.Remove(d.config.CachePath)
}
