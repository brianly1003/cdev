package repository

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Search performs a file search based on the given query.
func (idx *SQLiteIndexer) Search(ctx context.Context, query SearchQuery) (*SearchResult, error) {
	start := time.Now()

	// Normalize query parameters
	if query.Limit <= 0 {
		query.Limit = DefaultSearchLimit
	}
	if query.Limit > MaxSearchLimit {
		query.Limit = MaxSearchLimit
	}
	if query.Mode == "" {
		query.Mode = SearchModeFuzzy
	}

	var results []FileInfo
	var total int
	var err error

	switch query.Mode {
	case SearchModeFuzzy:
		results, total, err = idx.searchFuzzy(ctx, query)
	case SearchModeExact:
		results, total, err = idx.searchExact(ctx, query)
	case SearchModePrefix:
		results, total, err = idx.searchPrefix(ctx, query)
	case SearchModeExtension:
		results, total, err = idx.searchByExtension(ctx, query)
	default:
		results, total, err = idx.searchFuzzy(ctx, query)
	}

	if err != nil {
		return nil, err
	}

	return &SearchResult{
		Query:     query.Query,
		Mode:      query.Mode,
		Results:   results,
		Total:     total,
		ElapsedMS: time.Since(start).Milliseconds(),
	}, nil
}

// searchFuzzy uses FTS5 for fuzzy matching.
func (idx *SQLiteIndexer) searchFuzzy(ctx context.Context, query SearchQuery) ([]FileInfo, int, error) {
	// Build FTS5 query - escape special characters
	ftsQuery := escapeFTSQuery(query.Query)
	if ftsQuery == "" {
		return nil, 0, nil
	}

	// Add wildcard for partial matching
	ftsQuery = ftsQuery + "*"

	excludeBinaries := 0
	if query.ExcludeBinaries {
		excludeBinaries = 1
	}

	rows, err := idx.stmtSearchFuzzy.QueryContext(ctx, ftsQuery, excludeBinaries, query.Limit, query.Offset)
	if err != nil {
		return nil, 0, fmt.Errorf("search query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []FileInfo
	for rows.Next() {
		var f FileInfo
		var modifiedUnix int64
		var isBinary, isSensitive, gitTracked int
		var rank float64

		err := rows.Scan(
			&f.ID, &f.Path, &f.Name, &f.Extension, &f.Directory,
			&f.SizeBytes, &modifiedUnix, &isBinary, &isSensitive, &gitTracked, &rank,
		)
		if err != nil {
			continue
		}

		f.ModifiedAt = time.Unix(modifiedUnix, 0)
		f.IsBinary = intToBool(isBinary)
		f.IsSensitive = intToBool(isSensitive)
		f.GitTracked = intToBool(gitTracked)
		f.MatchScore = 1.0 / (1.0 - rank) // Convert BM25 to 0-1 score

		results = append(results, f)
	}

	// Get total count
	var total int
	countQuery := `
		SELECT COUNT(*) FROM repository_files f
		JOIN repository_files_fts fts ON f.id = fts.rowid
		WHERE repository_files_fts MATCH ?
	`
	if query.ExcludeBinaries {
		countQuery += " AND f.is_binary = 0"
	}
	_ = idx.db.QueryRowContext(ctx, countQuery, ftsQuery).Scan(&total)

	return results, total, nil
}

// searchExact performs exact string matching on path.
func (idx *SQLiteIndexer) searchExact(ctx context.Context, query SearchQuery) ([]FileInfo, int, error) {
	searchTerm := "%" + strings.ToLower(query.Query) + "%"

	sql := `
		SELECT id, path, name, extension, directory, size_bytes, modified_at,
		       is_binary, is_sensitive, git_tracked
		FROM repository_files
		WHERE path_normalized LIKE ?
	`
	args := []interface{}{searchTerm}

	if query.ExcludeBinaries {
		sql += " AND is_binary = 0"
	}
	if query.GitTrackedOnly {
		sql += " AND git_tracked = 1"
	}
	if len(query.Extensions) > 0 {
		placeholders := make([]string, len(query.Extensions))
		for i, ext := range query.Extensions {
			placeholders[i] = "?"
			args = append(args, strings.ToLower(strings.TrimPrefix(ext, ".")))
		}
		sql += " AND extension IN (" + strings.Join(placeholders, ",") + ")"
	}

	sql += " ORDER BY path LIMIT ? OFFSET ?"
	args = append(args, query.Limit, query.Offset)

	rows, err := idx.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	results := idx.scanFileRows(rows)

	// Get total count
	countSQL := `SELECT COUNT(*) FROM repository_files WHERE path_normalized LIKE ?`
	var total int
	_ = idx.db.QueryRowContext(ctx, countSQL, searchTerm).Scan(&total)

	return results, total, nil
}

// searchPrefix finds files starting with the given prefix.
func (idx *SQLiteIndexer) searchPrefix(ctx context.Context, query SearchQuery) ([]FileInfo, int, error) {
	searchTerm := strings.ToLower(query.Query) + "%"

	sql := `
		SELECT id, path, name, extension, directory, size_bytes, modified_at,
		       is_binary, is_sensitive, git_tracked
		FROM repository_files
		WHERE name LIKE ? OR path_normalized LIKE ?
	`
	args := []interface{}{searchTerm, searchTerm}

	if query.ExcludeBinaries {
		sql += " AND is_binary = 0"
	}

	sql += " ORDER BY name LIMIT ? OFFSET ?"
	args = append(args, query.Limit, query.Offset)

	rows, err := idx.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	results := idx.scanFileRows(rows)

	// Get total count
	var total int
	_ = idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM repository_files WHERE name LIKE ? OR path_normalized LIKE ?`,
		searchTerm, searchTerm).Scan(&total)

	return results, total, nil
}

// searchByExtension finds files with the given extension(s).
func (idx *SQLiteIndexer) searchByExtension(ctx context.Context, query SearchQuery) ([]FileInfo, int, error) {
	extensions := query.Extensions
	if len(extensions) == 0 && query.Query != "" {
		extensions = []string{query.Query}
	}
	if len(extensions) == 0 {
		return nil, 0, nil
	}

	// Build placeholders
	placeholders := make([]string, len(extensions))
	args := make([]interface{}, len(extensions))
	for i, ext := range extensions {
		placeholders[i] = "?"
		args[i] = strings.ToLower(strings.TrimPrefix(ext, "."))
	}

	sql := `
		SELECT id, path, name, extension, directory, size_bytes, modified_at,
		       is_binary, is_sensitive, git_tracked
		FROM repository_files
		WHERE extension IN (` + strings.Join(placeholders, ",") + `)`

	if query.ExcludeBinaries {
		sql += " AND is_binary = 0"
	}

	sql += " ORDER BY path LIMIT ? OFFSET ?"
	args = append(args, query.Limit, query.Offset)

	rows, err := idx.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	results := idx.scanFileRows(rows)

	// Get total count
	countSQL := `SELECT COUNT(*) FROM repository_files WHERE extension IN (` + strings.Join(placeholders, ",") + `)`
	var total int
	countArgs := make([]interface{}, len(extensions))
	copy(countArgs, args[:len(extensions)])
	_ = idx.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total)

	return results, total, nil
}

// ListFiles returns a paginated list of files in a directory.
func (idx *SQLiteIndexer) ListFiles(ctx context.Context, opts ListOptions) (*FileList, error) {
	// Normalize options
	if opts.Limit <= 0 {
		opts.Limit = DefaultListLimit
	}
	if opts.Limit > MaxListLimit {
		opts.Limit = MaxListLimit
	}
	if opts.SortBy == "" {
		opts.SortBy = "name"
	}
	if opts.SortOrder == "" {
		opts.SortOrder = "asc"
	}

	// Validate sort options
	validSorts := map[string]bool{"name": true, "size": true, "modified": true, "path": true}
	if !validSorts[opts.SortBy] {
		opts.SortBy = "name"
	}
	if opts.SortOrder != "asc" && opts.SortOrder != "desc" {
		opts.SortOrder = "asc"
	}

	// Build query
	var sql strings.Builder
	var args []interface{}

	sql.WriteString(`
		SELECT id, path, name, extension, directory, size_bytes, modified_at,
		       is_binary, is_sensitive, git_tracked
		FROM repository_files
		WHERE 1=1
	`)

	// Directory filter
	if opts.Directory != "" {
		if opts.Recursive {
			sql.WriteString(" AND (directory = ? OR directory LIKE ?)")
			args = append(args, opts.Directory, opts.Directory+"/%")
		} else {
			sql.WriteString(" AND directory = ?")
			args = append(args, opts.Directory)
		}
	} else if !opts.Recursive {
		sql.WriteString(" AND directory = ''")
	}

	// Extension filter
	if len(opts.Extensions) > 0 {
		placeholders := make([]string, len(opts.Extensions))
		for i, ext := range opts.Extensions {
			placeholders[i] = "?"
			args = append(args, strings.ToLower(strings.TrimPrefix(ext, ".")))
		}
		sql.WriteString(" AND extension IN (" + strings.Join(placeholders, ",") + ")")
	}

	// Size filters
	if opts.MinSize > 0 {
		sql.WriteString(" AND size_bytes >= ?")
		args = append(args, opts.MinSize)
	}
	if opts.MaxSize > 0 {
		sql.WriteString(" AND size_bytes <= ?")
		args = append(args, opts.MaxSize)
	}

	// Sort
	sortColumn := map[string]string{
		"name":     "name",
		"size":     "size_bytes",
		"modified": "modified_at",
		"path":     "path",
	}[opts.SortBy]
	sql.WriteString(fmt.Sprintf(" ORDER BY %s %s", sortColumn, strings.ToUpper(opts.SortOrder)))

	// Pagination
	sql.WriteString(" LIMIT ? OFFSET ?")
	args = append(args, opts.Limit+1, opts.Offset) // +1 to check for more

	rows, err := idx.db.QueryContext(ctx, sql.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	files := idx.scanFileRows(rows)

	// Check if there are more results
	hasMore := len(files) > opts.Limit
	if hasMore {
		files = files[:opts.Limit]
	}

	// Get directories in the current path
	var directories []DirectoryInfo
	if !opts.Recursive {
		directories, _ = idx.getSubdirectories(ctx, opts.Directory)
	}

	// Get total counts
	var totalFiles, totalDirs int
	countSQL := `SELECT COUNT(*) FROM repository_files WHERE `
	if opts.Directory != "" {
		if opts.Recursive {
			countSQL += "directory = ? OR directory LIKE ?"
			_ = idx.db.QueryRowContext(ctx, countSQL, opts.Directory, opts.Directory+"/%").Scan(&totalFiles)
		} else {
			countSQL += "directory = ?"
			_ = idx.db.QueryRowContext(ctx, countSQL, opts.Directory).Scan(&totalFiles)
		}
	} else if !opts.Recursive {
		countSQL += "directory = ''"
		_ = idx.db.QueryRowContext(ctx, countSQL).Scan(&totalFiles)
	} else {
		_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_files").Scan(&totalFiles)
	}
	totalDirs = len(directories)

	return &FileList{
		Directory:   opts.Directory,
		Files:       files,
		Directories: directories,
		TotalFiles:  totalFiles,
		TotalDirs:   totalDirs,
		Pagination: PaginationInfo{
			Limit:   opts.Limit,
			Offset:  opts.Offset,
			HasMore: hasMore,
		},
	}, nil
}

// getSubdirectories returns immediate subdirectories of a path.
func (idx *SQLiteIndexer) getSubdirectories(ctx context.Context, parentPath string) ([]DirectoryInfo, error) {
	var sql string
	var args []interface{}

	if parentPath == "" {
		// For root, get top-level directories (non-empty paths without slashes)
		sql = `
			SELECT path, file_count, total_size_bytes, last_modified
			FROM repository_directories
			WHERE path != '' AND path NOT LIKE '%/%'
			ORDER BY path
		`
	} else {
		// For subdirectories, get immediate children
		sql = `
			SELECT path, file_count, total_size_bytes, last_modified
			FROM repository_directories
			WHERE path LIKE ? AND path NOT LIKE ?
			ORDER BY path
		`
		args = append(args, parentPath+"/%", parentPath+"/%/%")
	}

	rows, err := idx.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var dirs []DirectoryInfo
	for rows.Next() {
		var d DirectoryInfo
		var lastModified int64
		if err := rows.Scan(&d.Path, &d.FileCount, &d.TotalSizeBytes, &lastModified); err != nil {
			continue
		}
		d.Name = getBaseName(d.Path)
		d.LastModified = time.Unix(lastModified, 0)
		dirs = append(dirs, d)
	}

	return dirs, nil
}

// GetTree returns a hierarchical directory structure.
func (idx *SQLiteIndexer) GetTree(ctx context.Context, rootPath string, depth int) (*DirectoryTree, error) {
	if depth <= 0 {
		depth = 2
	}
	if depth > MaxTreeDepth {
		depth = MaxTreeDepth
	}

	// Build tree recursively
	tree, err := idx.buildTree(ctx, rootPath, depth)
	if err != nil {
		return nil, err
	}

	return tree, nil
}

// buildTree recursively builds a directory tree.
func (idx *SQLiteIndexer) buildTree(ctx context.Context, path string, remainingDepth int) (*DirectoryTree, error) {
	tree := &DirectoryTree{
		Path: path,
		Name: getBaseName(path),
		Type: "directory",
	}

	if path == "" {
		tree.Name = idx.scanner.RepoPath()
	}

	if remainingDepth <= 0 {
		return tree, nil
	}

	// Get subdirectories
	dirs, _ := idx.getSubdirectories(ctx, path)
	for _, d := range dirs {
		childTree, _ := idx.buildTree(ctx, d.Path, remainingDepth-1)
		if childTree != nil {
			childTree.FileCount = &d.FileCount
			childTree.TotalSizeBytes = &d.TotalSizeBytes
			tree.Children = append(tree.Children, *childTree)
		}
	}

	// Get files in this directory
	files, _ := idx.ListFiles(ctx, ListOptions{
		Directory: path,
		Recursive: false,
		Limit:     MaxListLimit,
	})

	for _, f := range files.Files {
		fileTree := DirectoryTree{
			Path:      f.Path,
			Name:      f.Name,
			Type:      "file",
			SizeBytes: &f.SizeBytes,
			Extension: &f.Extension,
		}
		tree.Children = append(tree.Children, fileTree)
	}

	return tree, nil
}

// GetStats returns aggregate statistics about the repository.
// Uses caching with event-driven invalidation for better performance.
func (idx *SQLiteIndexer) GetStats(ctx context.Context) (*RepositoryStats, error) {
	// Check if cached stats are valid
	if idx.isStatsCacheValid() {
		idx.statsMu.RLock()
		stats := idx.statsCache
		idx.statsMu.RUnlock()
		return stats, nil
	}

	// Compute fresh stats
	stats := &RepositoryStats{
		FilesByExtension: make(map[string]int),
	}

	// Get basic counts
	_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_files").Scan(&stats.TotalFiles)
	_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(DISTINCT directory) FROM repository_files").Scan(&stats.TotalDirectories)
	_ = idx.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(size_bytes), 0) FROM repository_files").Scan(&stats.TotalSizeBytes)
	_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_files WHERE git_tracked = 1").Scan(&stats.GitTrackedFiles)
	_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_files WHERE git_ignored = 1").Scan(&stats.GitIgnoredFiles)
	_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_files WHERE is_binary = 1").Scan(&stats.BinaryFiles)
	_ = idx.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_files WHERE is_sensitive = 1").Scan(&stats.SensitiveFiles)

	// Get files by extension
	rows, err := idx.db.QueryContext(ctx, `
		SELECT extension, COUNT(*) as count
		FROM repository_files
		WHERE extension != ''
		GROUP BY extension
		ORDER BY count DESC
		LIMIT 20
	`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var ext string
			var count int
			if rows.Scan(&ext, &count) == nil {
				stats.FilesByExtension[ext] = count
			}
		}
	}

	// Get largest files
	rows, err = idx.db.QueryContext(ctx, `
		SELECT id, path, name, extension, directory, size_bytes, modified_at,
		       is_binary, is_sensitive, git_tracked
		FROM repository_files
		ORDER BY size_bytes DESC
		LIMIT 10
	`)
	if err == nil {
		defer func() { _ = rows.Close() }()
		stats.LargestFiles = idx.scanFileRows(rows)
	}

	// Cache the stats
	idx.statsMu.Lock()
	idx.statsCache = stats
	idx.statsCacheTime = time.Now()
	idx.statsCacheValid = true
	idx.statsMu.Unlock()

	return stats, nil
}

// scanFileRows scans rows into FileInfo slice.
func (idx *SQLiteIndexer) scanFileRows(rows interface{ Next() bool; Scan(...interface{}) error }) []FileInfo {
	var results []FileInfo
	for rows.Next() {
		var f FileInfo
		var modifiedUnix int64
		var isBinary, isSensitive, gitTracked int

		err := rows.Scan(
			&f.ID, &f.Path, &f.Name, &f.Extension, &f.Directory,
			&f.SizeBytes, &modifiedUnix, &isBinary, &isSensitive, &gitTracked,
		)
		if err != nil {
			continue
		}

		f.ModifiedAt = time.Unix(modifiedUnix, 0)
		f.IsBinary = intToBool(isBinary)
		f.IsSensitive = intToBool(isSensitive)
		f.GitTracked = intToBool(gitTracked)

		results = append(results, f)
	}
	return results
}

// escapeFTSQuery escapes special FTS5 characters.
func escapeFTSQuery(query string) string {
	// Remove special FTS5 characters
	replacer := strings.NewReplacer(
		"*", "",
		"\"", "",
		"(", "",
		")", "",
		":", "",
		"-", " ",
		".", " ",
		"/", " ",
		"\\", " ",
	)
	escaped := replacer.Replace(query)
	escaped = strings.TrimSpace(escaped)

	// Split into words and rejoin
	words := strings.Fields(escaped)
	if len(words) == 0 {
		return ""
	}

	return strings.Join(words, " ")
}

// getBaseName returns the base name of a path.
func getBaseName(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}
