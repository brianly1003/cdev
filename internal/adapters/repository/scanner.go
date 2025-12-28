package repository

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Scanner errors
var (
	ErrPathTraversal     = errors.New("path traversal detected")
	ErrSymlinkOutsideRepo = errors.New("symlink points outside repository")
	ErrTooManyFiles      = errors.New("too many files to scan")
	ErrIndexTooLarge     = errors.New("total index size exceeds limit")
	ErrScanTimeout       = errors.New("scan timeout exceeded")
	ErrInvalidPath       = errors.New("invalid path")
)

// Scanner handles file system scanning with security validations.
type Scanner struct {
	repoPath     string
	repoPathAbs  string
	skipDirs     map[string]bool
	binaryExts   map[string]bool
	sensitivePatterns []string
}

// NewScanner creates a new file scanner for the given repository path.
// If customSkipDirs is nil or empty, the default SkipDirectories list is used.
func NewScanner(repoPath string, customSkipDirs []string) (*Scanner, error) {
	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, err
	}

	// Use custom skip directories if provided, otherwise use defaults
	skipDirList := SkipDirectories
	if len(customSkipDirs) > 0 {
		skipDirList = customSkipDirs
	}

	// Build skip directories map
	skipDirs := make(map[string]bool)
	for _, dir := range skipDirList {
		skipDirs[dir] = true
	}

	// Build binary extensions map
	binaryExts := make(map[string]bool)
	for _, ext := range BinaryExtensions {
		binaryExts[strings.ToLower(ext)] = true
	}

	return &Scanner{
		repoPath:          repoPath,
		repoPathAbs:       absPath,
		skipDirs:          skipDirs,
		binaryExts:        binaryExts,
		sensitivePatterns: SensitivePatterns,
	}, nil
}

// ScanAll performs a full scan of the repository.
func (s *Scanner) ScanAll(ctx context.Context) ([]FileInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, ScanTimeout)
	defer cancel()

	var files []FileInfo
	var totalSize int64
	var fileCount int

	err := filepath.Walk(s.repoPathAbs, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ErrScanTimeout
		default:
		}

		if err != nil {
			log.Debug().Err(err).Str("path", path).Msg("error accessing path")
			return nil // Continue scanning other files
		}

		// Get relative path
		relPath, err := filepath.Rel(s.repoPathAbs, path)
		if err != nil {
			return nil
		}

		// Skip root
		if relPath == "." {
			return nil
		}

		// Check if directory should be skipped
		if info.IsDir() {
			if s.shouldSkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		// Enforce limits
		fileCount++
		if fileCount > MaxFilesPerScan {
			return ErrTooManyFiles
		}

		if totalSize > MaxTotalIndexSize {
			return ErrIndexTooLarge
		}

		// Skip files that are too large
		if info.Size() > MaxFileSize {
			log.Debug().Str("path", relPath).Int64("size", info.Size()).Msg("skipping large file")
			return nil
		}

		// Scan the file
		fileInfo, err := s.scanFile(path, relPath, info)
		if err != nil {
			log.Debug().Err(err).Str("path", relPath).Msg("error scanning file")
			return nil
		}

		files = append(files, fileInfo)
		totalSize += info.Size()

		return nil
	})

	if err != nil && !errors.Is(err, ErrScanTimeout) && !errors.Is(err, ErrTooManyFiles) {
		return nil, err
	}

	return files, nil
}

// ScanFile scans a single file and returns its metadata.
func (s *Scanner) ScanFile(ctx context.Context, relPath string) (*FileInfo, error) {
	// Validate path
	if err := s.ValidatePath(relPath); err != nil {
		return nil, err
	}

	absPath := filepath.Join(s.repoPathAbs, relPath)
	info, err := os.Lstat(absPath)
	if err != nil {
		return nil, err
	}

	fileInfo, err := s.scanFile(absPath, relPath, info)
	if err != nil {
		return nil, err
	}

	return &fileInfo, nil
}

// scanFile extracts metadata from a file.
func (s *Scanner) scanFile(absPath, relPath string, info os.FileInfo) (FileInfo, error) {
	ext := strings.ToLower(filepath.Ext(relPath))
	name := filepath.Base(relPath)
	dir := filepath.Dir(relPath)
	if dir == "." {
		dir = ""
	}

	// Get file ID (inode on Unix, file ID on Windows) for rename detection
	fileID, _ := GetFileID(absPath)

	fileInfo := FileInfo{
		Path:        relPath,
		Name:        name,
		Directory:   dir,
		Extension:   strings.TrimPrefix(ext, "."),
		SizeBytes:   info.Size(),
		ModifiedAt:  info.ModTime(),
		IndexedAt:   time.Now(),
		IsSymlink:   info.Mode()&os.ModeSymlink != 0,
		IsBinary:    s.isBinaryFile(ext, name),
		IsSensitive: s.isSensitiveFile(name, relPath),
		FileID:      fileID,
	}

	// Check symlink safety
	if fileInfo.IsSymlink {
		if err := s.validateSymlink(absPath); err != nil {
			fileInfo.IsSensitive = true // Mark as sensitive if symlink is suspicious
		}
	}

	// Calculate content hash and count lines in a single pass (only for small text files)
	// This avoids opening the file twice, significantly improving I/O performance
	if !fileInfo.IsBinary && info.Size() < 1024*1024 { // < 1MB
		if hash, lines, err := s.hashAndCountLines(absPath); err == nil {
			fileInfo.ContentHash = hash
			fileInfo.LineCount = lines
		}
	}

	return fileInfo, nil
}

// ValidatePath ensures the path is within the repository and safe.
func (s *Scanner) ValidatePath(relPath string) error {
	// Normalize path
	cleanPath := filepath.Clean(relPath)

	// Check for path traversal attempts
	if strings.HasPrefix(cleanPath, "..") || strings.Contains(cleanPath, "/../") {
		return ErrPathTraversal
	}

	// Convert to absolute and verify it's within repo
	absPath := filepath.Join(s.repoPathAbs, cleanPath)
	absPath, err := filepath.Abs(absPath)
	if err != nil {
		return ErrInvalidPath
	}

	if !strings.HasPrefix(absPath, s.repoPathAbs) {
		return ErrPathTraversal
	}

	return nil
}

// validateSymlink checks if a symlink points within the repository.
func (s *Scanner) validateSymlink(path string) error {
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}

	// Resolve relative symlinks
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}

	// Get absolute path of target
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}

	// Check if target is within repository
	if !strings.HasPrefix(absTarget, s.repoPathAbs) {
		return ErrSymlinkOutsideRepo
	}

	return nil
}

// shouldSkipDir returns true if the directory should be skipped.
func (s *Scanner) shouldSkipDir(name string) bool {
	// Skip hidden directories
	if strings.HasPrefix(name, ".") {
		return true
	}
	return s.skipDirs[name]
}

// isBinaryFile returns true if the file is likely binary based on extension.
func (s *Scanner) isBinaryFile(ext, name string) bool {
	if s.binaryExts[ext] {
		return true
	}
	// Check for files without extension that are typically binary
	if ext == "" {
		lowerName := strings.ToLower(name)
		if lowerName == "license" || lowerName == "readme" || lowerName == "changelog" {
			return false
		}
	}
	return false
}

// isSensitiveFile returns true if the file matches sensitive patterns.
func (s *Scanner) isSensitiveFile(name, path string) bool {
	lowerName := strings.ToLower(name)
	lowerPath := strings.ToLower(path)

	for _, pattern := range s.sensitivePatterns {
		// Check name match
		if matched, _ := filepath.Match(strings.ToLower(pattern), lowerName); matched {
			return true
		}
		// Check path match
		if matched, _ := filepath.Match(strings.ToLower(pattern), lowerPath); matched {
			return true
		}
		// Check if pattern is contained in name
		if strings.Contains(lowerName, strings.ToLower(strings.Trim(pattern, "*."))) {
			if strings.Contains(pattern, "secret") || strings.Contains(pattern, "credential") ||
				strings.Contains(pattern, "password") || strings.Contains(pattern, "token") {
				return true
			}
		}
	}

	return false
}

// hashAndCountLines calculates SHA256 hash and counts lines in a single file read.
// This is more efficient than separate calculateHash and countLines calls as it
// only opens the file once and reads through it once.
func (s *Scanner) hashAndCountLines(path string) (hash string, lines int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	lineCount := 0
	buf := make([]byte, 32*1024)
	newline := []byte{'\n'}

	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
			lineCount += bytes.Count(buf[:n], newline)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", 0, readErr
		}
	}

	return hex.EncodeToString(h.Sum(nil)), lineCount, nil
}


// IsGitRepo checks if the repository path is a git repository.
func (s *Scanner) IsGitRepo() bool {
	gitPath := filepath.Join(s.repoPathAbs, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// RepoPath returns the absolute repository path.
func (s *Scanner) RepoPath() string {
	return s.repoPathAbs
}
