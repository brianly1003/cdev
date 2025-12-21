# Repository Indexer

The Repository Indexer provides fast file search and browsing capabilities for mobile apps and desktop clients. It uses SQLite with FTS5 (Full-Text Search) for high-performance queries.

## Overview

The indexer automatically:
- Scans and indexes all files in the repository on startup
- Detects sensitive files (.env, credentials, keys)
- Identifies binary files (images, executables, etc.)
- Updates incrementally when files change
- Performs background reconciliation every 10 minutes

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Repository Indexer                        │
├─────────────────────────────────────────────────────────────┤
│  Scanner           │  SQLite + FTS5    │  HTTP API          │
│  ─────────────     │  ──────────────   │  ──────────        │
│  • Path validation │  • WAL mode       │  • Search          │
│  • Symlink checks  │  • Prepared stmts │  • List files      │
│  • Binary detect   │  • Full-text      │  • Directory tree  │
│  • Sensitive flags │  • Trigram index  │  • Statistics      │
└─────────────────────────────────────────────────────────────┘
```

## API Endpoints

### Get Index Status

```
GET /api/repository/index/status
```

Returns the current status of the repository index.

**Response:**
```json
{
  "status": "ready",
  "total_files": 277,
  "indexed_files": 277,
  "total_size_bytes": 18940805,
  "last_full_scan": "2025-12-19T15:14:26Z",
  "last_update": "2025-12-19T15:14:26Z",
  "database_size_bytes": 4096,
  "is_git_repo": true
}
```

**Response Fields:**
| Field | Type | Description |
|-------|------|-------------|
| `status` | string | Index state: `initializing`, `indexing`, `ready`, `error` |
| `total_files` | number | Total files in repository |
| `indexed_files` | number | Files successfully indexed |
| `total_size_bytes` | number | Total size of all indexed files |
| `last_full_scan` | string | ISO 8601 timestamp of last full scan |
| `last_update` | string | ISO 8601 timestamp of last incremental update |
| `database_size_bytes` | number | SQLite database file size |
| `is_git_repo` | boolean | Whether repository is a git repo |

---

### Search Files

```
GET /api/repository/search?q=<query>&mode=<mode>&limit=<limit>
```

Search for files using various matching strategies.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `q` | string | required | Search query |
| `mode` | string | `fuzzy` | Search mode (see below) |
| `limit` | number | 50 | Maximum results (max: 500) |
| `offset` | number | 0 | Pagination offset |
| `extensions` | string | - | Comma-separated extensions (e.g., `ts,js,go`) |
| `exclude_binaries` | boolean | true | Exclude binary files from results |
| `git_tracked_only` | boolean | false | Only return git-tracked files |

**Search Modes:**
| Mode | Description | Use Case |
|------|-------------|----------|
| `fuzzy` | Full-text search with FTS5 | General file finding, typo tolerance |
| `exact` | Case-insensitive substring match | Finding specific file patterns |
| `prefix` | Matches file names starting with query | Quick filename completion |
| `extension` | Finds files by extension | "Show all TypeScript files" |

**Example - Fuzzy Search:**
```
GET /api/repository/search?q=index&limit=5
```

**Response:**
```json
{
  "query": "index",
  "mode": "fuzzy",
  "results": [
    {
      "path": "src/index.ts",
      "name": "index.ts",
      "directory": "src",
      "extension": "ts",
      "size_bytes": 8097,
      "modified_at": "2025-10-21T11:17:45Z",
      "is_binary": false,
      "is_sensitive": false,
      "git_tracked": false,
      "match_score": 0.162
    }
  ],
  "total": 10,
  "elapsed_ms": 1
}
```

**Example - Extension Search:**
```
GET /api/repository/search?q=ts&mode=extension&limit=10
```

**Example - Exact Search with Filters:**
```
GET /api/repository/search?q=config&mode=exact&extensions=json,yaml&exclude_binaries=true
```

---

### List Files

```
GET /api/repository/files/list
```

Returns a paginated list of files in a directory with subdirectory information.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `directory` | string | `""` (root) | Directory path to list |
| `recursive` | boolean | false | Include files from subdirectories |
| `limit` | number | 100 | Maximum files to return (max: 1000) |
| `offset` | number | 0 | Pagination offset |
| `sort` | string | `name` | Sort by: `name`, `size`, `modified`, `path` |
| `order` | string | `asc` | Sort order: `asc`, `desc` |
| `extensions` | string | - | Comma-separated extensions filter |
| `min_size` | number | - | Minimum file size in bytes |
| `max_size` | number | - | Maximum file size in bytes |

**Example - List Root Directory:**
```
GET /api/repository/files/list?limit=10
```

**Response:**
```json
{
  "directory": "",
  "files": [
    {
      "path": "package.json",
      "name": "package.json",
      "directory": "",
      "extension": "json",
      "size_bytes": 3581,
      "modified_at": "2025-10-21T11:17:45Z",
      "is_binary": false,
      "is_sensitive": false,
      "git_tracked": false
    }
  ],
  "directories": [
    {
      "path": "src",
      "name": "src",
      "file_count": 47,
      "total_size_bytes": 324277,
      "last_modified": "2025-12-19T14:05:15Z"
    }
  ],
  "total_files": 26,
  "total_directories": 6,
  "pagination": {
    "limit": 10,
    "offset": 0,
    "has_more": true
  }
}
```

**Example - List Subdirectory:**
```
GET /api/repository/files/list?directory=src/components&sort=modified&order=desc
```

**Example - Recursive Listing:**
```
GET /api/repository/files/list?directory=src&recursive=true&extensions=ts,tsx
```

---

### Get Directory Tree

```
GET /api/repository/files/tree
```

Returns a hierarchical tree structure of the repository.

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `path` | string | `""` (root) | Root path for tree |
| `depth` | number | 2 | Maximum depth to traverse (max: 10) |

**Example:**
```
GET /api/repository/files/tree?depth=2
```

**Response:**
```json
{
  "path": "",
  "name": "/Users/project/repo",
  "type": "directory",
  "children": [
    {
      "path": "src",
      "name": "src",
      "type": "directory",
      "file_count": 47,
      "total_size_bytes": 324277,
      "children": [
        {
          "path": "src/index.ts",
          "name": "index.ts",
          "type": "file",
          "size_bytes": 8097,
          "extension": "ts"
        }
      ]
    },
    {
      "path": "package.json",
      "name": "package.json",
      "type": "file",
      "size_bytes": 3581,
      "extension": "json"
    }
  ]
}
```

---

### Get Repository Statistics

```
GET /api/repository/stats
```

Returns aggregate statistics about the repository.

**Response:**
```json
{
  "total_files": 277,
  "total_directories": 59,
  "total_size_bytes": 18940805,
  "files_by_extension": {
    "ts": 119,
    "json": 20,
    "md": 5,
    "yaml": 6
  },
  "largest_files": [
    {
      "path": "package-lock.json",
      "name": "package-lock.json",
      "size_bytes": 480928,
      "is_binary": false
    }
  ],
  "git_tracked_files": 0,
  "git_ignored_files": 0,
  "binary_files": 53,
  "sensitive_files": 15
}
```

---

### Rebuild Index

```
POST /api/repository/index/rebuild
```

Triggers a full re-index of the repository. Runs in background.

**Response:**
```json
{
  "status": "started",
  "message": "Repository index rebuild started"
}
```

---

## Security Features

### Path Traversal Prevention

All file paths are validated to prevent directory traversal attacks:

```go
// These requests are blocked:
GET /api/repository/files/list?directory=../../../etc
GET /api/repository/search?q=../../passwd
```

**Response:**
```json
{
  "error": "path traversal detected"
}
```

### Sensitive File Detection

Files matching these patterns are flagged as sensitive:
- `.env`, `.env.*`
- `*credentials*`, `*secrets*`
- `*.key`, `*.pem`, `*.p12`
- `id_rsa`, `id_dsa`, `id_ed25519`
- `*.keystore`, `*.jks`

Sensitive files have `is_sensitive: true` in responses.

### Symlink Safety

Symlinks pointing outside the repository are:
- Not followed during scanning
- Flagged as sensitive
- Excluded from content operations

---

## Resource Limits

| Limit | Value | Description |
|-------|-------|-------------|
| Max file size | 100 MB | Files larger than this are skipped |
| Max total index | 1 GB | Total size limit for indexed files |
| Max files per scan | 1,000,000 | Maximum files to index |
| Scan timeout | 5 minutes | Maximum time for full scan |
| Max search results | 500 | Maximum results per search |
| Max list results | 1,000 | Maximum files per list |
| Max tree depth | 10 | Maximum directory depth |

---

## iOS Integration Example

### File Browser View

```swift
import Foundation

class RepositoryService {
    private let baseURL: URL

    init(baseURL: URL) {
        self.baseURL = baseURL
    }

    // MARK: - Search

    func searchFiles(query: String, mode: SearchMode = .fuzzy, limit: Int = 50) async throws -> SearchResult {
        var components = URLComponents(url: baseURL.appendingPathComponent("/api/repository/search"), resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "q", value: query),
            URLQueryItem(name: "mode", value: mode.rawValue),
            URLQueryItem(name: "limit", value: String(limit))
        ]

        let (data, _) = try await URLSession.shared.data(from: components.url!)
        return try JSONDecoder().decode(SearchResult.self, from: data)
    }

    // MARK: - File Listing

    func listFiles(directory: String = "", recursive: Bool = false, limit: Int = 100) async throws -> FileList {
        var components = URLComponents(url: baseURL.appendingPathComponent("/api/repository/files/list"), resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "directory", value: directory),
            URLQueryItem(name: "recursive", value: String(recursive)),
            URLQueryItem(name: "limit", value: String(limit))
        ]

        let (data, _) = try await URLSession.shared.data(from: components.url!)
        return try JSONDecoder().decode(FileList.self, from: data)
    }

    // MARK: - Tree

    func getTree(path: String = "", depth: Int = 2) async throws -> DirectoryTree {
        var components = URLComponents(url: baseURL.appendingPathComponent("/api/repository/files/tree"), resolvingAgainstBaseURL: false)!
        components.queryItems = [
            URLQueryItem(name: "path", value: path),
            URLQueryItem(name: "depth", value: String(depth))
        ]

        let (data, _) = try await URLSession.shared.data(from: components.url!)
        return try JSONDecoder().decode(DirectoryTree.self, from: data)
    }
}

// MARK: - Models

enum SearchMode: String {
    case fuzzy
    case exact
    case prefix
    case `extension`
}

struct SearchResult: Codable {
    let query: String
    let mode: String
    let results: [FileInfo]
    let total: Int
    let elapsedMs: Int

    enum CodingKeys: String, CodingKey {
        case query, mode, results, total
        case elapsedMs = "elapsed_ms"
    }
}

struct FileInfo: Codable {
    let path: String
    let name: String
    let directory: String
    let `extension`: String
    let sizeBytes: Int64
    let modifiedAt: Date
    let isBinary: Bool
    let isSensitive: Bool
    let gitTracked: Bool
    let matchScore: Double?

    enum CodingKeys: String, CodingKey {
        case path, name, directory
        case `extension`
        case sizeBytes = "size_bytes"
        case modifiedAt = "modified_at"
        case isBinary = "is_binary"
        case isSensitive = "is_sensitive"
        case gitTracked = "git_tracked"
        case matchScore = "match_score"
    }
}

struct FileList: Codable {
    let directory: String
    let files: [FileInfo]
    let directories: [DirectoryInfo]
    let totalFiles: Int
    let totalDirectories: Int
    let pagination: PaginationInfo

    enum CodingKeys: String, CodingKey {
        case directory, files, directories
        case totalFiles = "total_files"
        case totalDirectories = "total_directories"
        case pagination
    }
}

struct DirectoryInfo: Codable {
    let path: String
    let name: String
    let fileCount: Int
    let totalSizeBytes: Int64
    let lastModified: Date

    enum CodingKeys: String, CodingKey {
        case path, name
        case fileCount = "file_count"
        case totalSizeBytes = "total_size_bytes"
        case lastModified = "last_modified"
    }
}

struct PaginationInfo: Codable {
    let limit: Int
    let offset: Int
    let hasMore: Bool

    enum CodingKeys: String, CodingKey {
        case limit, offset
        case hasMore = "has_more"
    }
}

struct DirectoryTree: Codable {
    let path: String
    let name: String
    let type: String
    let children: [DirectoryTree]?
    let sizeBytes: Int64?
    let `extension`: String?
    let fileCount: Int?
    let totalSizeBytes: Int64?

    enum CodingKeys: String, CodingKey {
        case path, name, type, children
        case sizeBytes = "size_bytes"
        case `extension`
        case fileCount = "file_count"
        case totalSizeBytes = "total_size_bytes"
    }
}
```

### SwiftUI File Browser

```swift
import SwiftUI

struct FileBrowserView: View {
    @StateObject private var viewModel = FileBrowserViewModel()
    @State private var searchText = ""

    var body: some View {
        NavigationStack {
            List {
                // Search results
                if !searchText.isEmpty {
                    Section("Search Results") {
                        ForEach(viewModel.searchResults, id: \.path) { file in
                            FileRow(file: file)
                        }
                    }
                }

                // Directory listing
                if searchText.isEmpty {
                    // Subdirectories
                    Section("Folders") {
                        ForEach(viewModel.directories, id: \.path) { dir in
                            NavigationLink(value: dir.path) {
                                DirectoryRow(directory: dir)
                            }
                        }
                    }

                    // Files
                    Section("Files") {
                        ForEach(viewModel.files, id: \.path) { file in
                            FileRow(file: file)
                        }
                    }
                }
            }
            .searchable(text: $searchText, prompt: "Search files...")
            .onChange(of: searchText) { _, newValue in
                Task {
                    await viewModel.search(query: newValue)
                }
            }
            .navigationDestination(for: String.self) { path in
                FileBrowserView()
                    .onAppear {
                        viewModel.currentDirectory = path
                    }
            }
            .navigationTitle(viewModel.currentDirectory.isEmpty ? "Repository" : viewModel.currentDirectory)
        }
        .task {
            await viewModel.loadFiles()
        }
    }
}

struct FileRow: View {
    let file: FileInfo

    var body: some View {
        HStack {
            Image(systemName: file.isBinary ? "doc.fill" : "doc.text")
                .foregroundColor(file.isSensitive ? .red : .blue)

            VStack(alignment: .leading) {
                Text(file.name)
                    .font(.body)
                Text(file.directory)
                    .font(.caption)
                    .foregroundColor(.secondary)
            }

            Spacer()

            Text(formatBytes(file.sizeBytes))
                .font(.caption)
                .foregroundColor(.secondary)
        }
    }

    func formatBytes(_ bytes: Int64) -> String {
        let formatter = ByteCountFormatter()
        formatter.countStyle = .file
        return formatter.string(fromByteCount: bytes)
    }
}

struct DirectoryRow: View {
    let directory: DirectoryInfo

    var body: some View {
        HStack {
            Image(systemName: "folder.fill")
                .foregroundColor(.yellow)

            VStack(alignment: .leading) {
                Text(directory.name)
                    .font(.body)
                Text("\(directory.fileCount) files")
                    .font(.caption)
                    .foregroundColor(.secondary)
            }
        }
    }
}

@MainActor
class FileBrowserViewModel: ObservableObject {
    @Published var files: [FileInfo] = []
    @Published var directories: [DirectoryInfo] = []
    @Published var searchResults: [FileInfo] = []
    @Published var currentDirectory = ""

    private let service = RepositoryService(baseURL: URL(string: "http://localhost:8766")!)

    func loadFiles() async {
        do {
            let result = try await service.listFiles(directory: currentDirectory)
            files = result.files
            directories = result.directories
        } catch {
            print("Failed to load files: \(error)")
        }
    }

    func search(query: String) async {
        guard !query.isEmpty else {
            searchResults = []
            return
        }

        do {
            let result = try await service.searchFiles(query: query)
            searchResults = result.results
        } catch {
            print("Search failed: \(error)")
        }
    }
}
```

---

## Performance Considerations

### Index Location

The SQLite database is stored in the system temp directory:
```
/tmp/cdev/repo-index/<encoded-repo-path>-index.db
```

### Optimizations

1. **WAL Mode**: Write-Ahead Logging for concurrent reads during writes
2. **Memory Mapping**: 256MB mmap for fast random access
3. **Prepared Statements**: Pre-compiled queries for repeated operations
4. **FTS5 Porter Stemmer**: Intelligent word matching (e.g., "running" matches "run")
5. **Batch Inserts**: Bulk operations within transactions

### Typical Performance

| Operation | Files | Time |
|-----------|-------|------|
| Full scan | 1,000 | ~100ms |
| Full scan | 10,000 | ~1s |
| Fuzzy search | - | <5ms |
| List directory | - | <1ms |
| Tree (depth=3) | - | <10ms |

---

## Configuration

The repository indexer uses sensible defaults. Currently, there are no configuration options exposed in `config.yaml`. Future versions may add:

```yaml
# Future configuration options (not yet implemented)
repository:
  indexer:
    enabled: true
    scan_interval_minutes: 10
    max_file_size_mb: 100
    excluded_patterns:
      - "*.log"
      - "node_modules/**"
```

---

## Troubleshooting

### Index Not Ready

If searches return empty results immediately after startup, the index may still be building. Check status:

```bash
curl http://localhost:8766/api/repository/index/status
```

Wait for `"status": "ready"` before querying.

### Missing Files

If expected files are missing from results:

1. Check if file size exceeds 100MB limit
2. Check if file is in a skipped directory (`.git`, `node_modules`, etc.)
3. Trigger a rebuild: `POST /api/repository/index/rebuild`

### Slow Queries

For slow searches:

1. Use more specific queries
2. Add extension filters
3. Use `prefix` mode for filename completion
4. Limit results with `limit` parameter
