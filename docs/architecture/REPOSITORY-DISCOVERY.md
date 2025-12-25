# Repository Discovery Architecture

This document describes the enterprise-grade repository discovery system implemented in cdev-agent, based on research from industry leaders including Apple Spotlight, JetBrains IDEs, Sourcegraph, and VS Code.

## Overview

The repository discovery system finds git repositories on the local filesystem and provides them to clients for workspace management. It is designed to be:

- **Fast**: Returns results in <100ms using cache-first strategy
- **Cross-Platform**: Works on macOS, Linux, and Windows
- **Scalable**: Handles large filesystems with 100K+ directories
- **Responsive**: Never blocks the UI; uses background refresh

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      CLIENT (Mobile App)                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │ Skeleton UI  │  │ Progressive  │  │ Cache First + Refresh│  │
│  │ (immediate)  │  │ Results List │  │ (show stale, update) │  │
│  └──────────────┘  └──────────────┘  └──────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      API LAYER (cdev)                           │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  workspace/discover  →  Returns cached + triggers scan   │  │
│  │  workspace/discover { fresh: true }  →  Force rescan     │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                   DISCOVERY ENGINE                              │
│  ┌────────────┐  ┌────────────┐  ┌─────────────────────────┐  │
│  │ Parallel   │  │ Smart      │  │ Depth-Limited           │  │
│  │ Workers    │  │ Exclusions │  │ Scanning                │  │
│  │ (N=CPU)    │  │ (70+ dirs) │  │ (max 4 levels)          │  │
│  └────────────┘  └────────────┘  └─────────────────────────┘  │
│  ┌────────────┐  ┌────────────┐  ┌─────────────────────────┐  │
│  │ Timeout    │  │ Background │  │ Priority Paths          │  │
│  │ (10s max)  │  │ Refresh    │  │ (likely dirs first)     │  │
│  └────────────┘  └────────────┘  └─────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                    PERSISTENT CACHE                             │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  ~/.cache/cdev/discovered-repos.json                      │  │
│  │  - Last scan timestamp                                    │  │
│  │  - Discovered repositories                                │  │
│  │  - TTL: 1 hour (configurable)                            │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

## Research & Industry Patterns

### Apple Spotlight (macOS)
- **Pattern**: Daemon-based continuous indexing with pre-built index
- **Applied**: Persistent cache that survives restarts

### JetBrains IDEs
- **Pattern**: "Dumb Mode" - UI remains responsive during indexing
- **Applied**: Cache-first strategy returns immediately, background refresh

### Sourcegraph Zoekt
- **Pattern**: Eventual consistency - cache is updated in background
- **Applied**: Background refresh triggered when cache is stale

### VS Code
- **Pattern**: Debouncing + aggressive exclusion lists
- **Applied**: 70+ skip directories, smart default paths

### Golang Parallel Walking (cwalk, fastwalk)
- **Pattern**: Worker pool for concurrent directory scanning
- **Applied**: N workers (where N = CPU cores) process directories in parallel

## Implementation Details

### File Location
```
internal/workspace/discovery.go
```

### Key Components

#### RepoDiscovery
The main discovery engine that handles:
- Parallel directory walking
- Cache management
- Background refresh
- Streaming results

#### DiscoveryConfig
```go
type DiscoveryConfig struct {
    MaxDepth  int           // Default: 4 levels
    Timeout   time.Duration // Default: 10 seconds
    Workers   int           // Default: runtime.NumCPU()
    CacheTTL  time.Duration // Default: 1 hour
    CachePath string        // Default: ~/.cache/cdev/discovered-repos.json
}
```

#### DiscoveryResult
```go
type DiscoveryResult struct {
    Repositories      []DiscoveredRepo
    Count             int
    Cached            bool    // True if results came from cache
    CacheAgeSeconds   int64   // Age of cache in seconds
    RefreshInProgress bool    // True if background refresh is running
    ElapsedMs         int64   // Time taken for this request
    ScannedPaths      int     // Number of directories scanned
    SkippedPaths      int     // Number of directories skipped
}
```

### Skip Directories (70+ patterns)

The system skips directories that are unlikely to contain git repositories:

**Version Control**: `.git`, `.svn`, `.hg`, `.bzr`

**Package Managers**:
- JavaScript: `node_modules`, `bower_components`, `.npm`, `.yarn`, `.pnpm`
- Other: `vendor`, `packages`, `Pods`, `Carthage`, `.bundle`

**Python**: `__pycache__`, `.pytest_cache`, `.mypy_cache`, `.venv`, `venv`

**Build Output**: `dist`, `build`, `target`, `out`, `bin`, `obj`, `.next`, `.nuxt`

**IDE/Editor**: `.idea`, `.vscode`, `.vs`, `.settings`

**Cache/Temp**: `.cache`, `.parcel-cache`, `.turbo`, `.gradle`, `.m2`

**macOS Specific**: `Library`, `.Trash`, `Applications`, `Movies`, `Music`, `Pictures`

**Windows Specific**: `AppData`, `$RECYCLE.BIN`, `System Volume Information`

### Default Search Paths

The system searches these paths by default (in priority order):
```
~/Projects
~/projects
~/Code
~/code
~/Developer
~/dev
~/Repos
~/repos
~/src
~/go/src
~/workspace
~/Workspace
~/Desktop
```

**Note**: `~/Documents` is intentionally excluded by default because:
1. It typically contains large numbers of non-code files (PDFs, downloads, etc.)
2. Scanning it can take 30+ seconds
3. It rarely contains git repositories

## API Usage

### JSON-RPC: workspace/discover

**Request (use cache)**:
```json
{
    "jsonrpc": "2.0",
    "method": "workspace/discover",
    "params": {},
    "id": 1
}
```

**Request (force fresh scan)**:
```json
{
    "jsonrpc": "2.0",
    "method": "workspace/discover",
    "params": { "fresh": true },
    "id": 1
}
```

**Request (custom paths)**:
```json
{
    "jsonrpc": "2.0",
    "method": "workspace/discover",
    "params": {
        "paths": ["/Users/dev/work", "/Users/dev/personal"]
    },
    "id": 1
}
```

**Response**:
```json
{
    "jsonrpc": "2.0",
    "result": {
        "repositories": [
            {
                "name": "cdev",
                "path": "/Users/dev/Projects/cdev",
                "is_git_repo": true,
                "is_configured": false,
                "default_branch": "main",
                "last_commit_message": "feat: add discovery engine",
                "last_commit_time": "2024-12-25T10:30:00Z"
            }
        ],
        "count": 1,
        "cached": true,
        "cache_age_seconds": 1800,
        "refresh_in_progress": true,
        "elapsed_ms": 5,
        "scanned_paths": 0,
        "skipped_paths": 0
    },
    "id": 1
}
```

## Performance Characteristics

| Scenario | Time | Notes |
|----------|------|-------|
| Cache hit (fresh) | <10ms | Returns immediately |
| Cache hit (stale) | <10ms | Returns + triggers background refresh |
| First scan (small) | 1-2s | ~1000 directories |
| First scan (medium) | 3-5s | ~10,000 directories |
| First scan (large) | 8-10s | ~50,000 directories (timeout may apply) |

## Client Integration Guide

### Recommended UX Pattern

1. **Initial Load**: Show skeleton UI immediately
2. **Cache Available**: Display cached results, show "refreshing" indicator if stale
3. **Background Refresh**: Update UI when fresh results arrive
4. **No Cache**: Show loading state, display results progressively as found

### Handling the Response

```swift
// iOS Example
func discoverRepositories() async {
    let result = await rpcClient.call("workspace/discover")

    // Always display results immediately
    self.repositories = result.repositories

    // Show indicator if refresh is happening
    self.isRefreshing = result.refresh_in_progress

    // Optionally show cache age
    if result.cached && result.cache_age_seconds > 3600 {
        self.showStaleDataWarning = true
    }
}
```

## Configuration

The discovery engine can be configured via environment variables or config file:

```yaml
# config.yaml
discovery:
  max_depth: 4          # Maximum directory depth to scan
  timeout: 10s          # Maximum scan duration
  workers: 0            # 0 = use all CPU cores
  cache_ttl: 1h         # How long to cache results
  skip_directories:     # Additional directories to skip
    - "my-custom-dir"
```

## Troubleshooting

### Discovery is slow

1. Check if you're scanning `~/Documents` or other large directories
2. Increase `max_depth` limit if needed
3. Add slow directories to skip list

### Cache not working

1. Verify cache directory is writable: `~/.cache/cdev/`
2. Check disk space
3. Try `workspace/discover { fresh: true }` to force refresh

### Missing repositories

1. Verify the repository has a `.git` directory
2. Check if the parent directory is in the skip list
3. Increase `max_depth` if repos are deeply nested
4. Add the specific path to `paths` parameter

## References

- [Apple Spotlight Architecture](https://en.wikipedia.org/wiki/Spotlight_(Apple))
- [JetBrains Indexing Documentation](https://www.jetbrains.com/help/idea/indexing.html)
- [Sourcegraph Zoekt](https://github.com/sourcegraph/zoekt)
- [VS Code Performance Wiki](https://github.com/microsoft/vscode/wiki/performance-issues)
- [cwalk - Concurrent filepath.Walk](https://github.com/iafan/cwalk)
- [NN/G Skeleton Screens](https://www.nngroup.com/articles/skeleton-screens/)
