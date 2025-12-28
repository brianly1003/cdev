// Package imagestorage provides image storage management for cdev.
// It handles storing, retrieving, and cleaning up uploaded images
// with configurable limits, TTL, and security validations.
package imagestorage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Storage limits and configuration constants.
const (
	MaxImagesCount   = 50                // Max images in folder
	MaxTotalSizeMB   = 100               // Max total size in MB
	MaxSingleImageMB = 10                // Max single image in MB
	ImageTTL         = 1 * time.Hour     // Auto-cleanup after 1 hour
	CleanupInterval  = 5 * time.Minute   // Check every 5 minutes
	CdevImagesDir    = ".cdev/images"    // Relative path from repo root
)

// Supported image MIME types.
var supportedMimeTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// Magic bytes for image format validation.
var magicBytes = map[string][]byte{
	"image/jpeg": {0xFF, 0xD8, 0xFF},
	"image/png":  {0x89, 0x50, 0x4E, 0x47},
	"image/gif":  {0x47, 0x49, 0x46, 0x38},
	"image/webp": {0x52, 0x49, 0x46, 0x46}, // RIFF header
}

// StoredImage represents a stored image with metadata.
type StoredImage struct {
	ID          string    `json:"id"`
	LocalPath   string    `json:"local_path"`   // Relative path: .cdev/images/xxx.jpg
	FullPath    string    `json:"-"`            // Absolute path (internal use)
	MimeType    string    `json:"mime_type"`
	Size        int64     `json:"size"`
	ContentHash string    `json:"content_hash"` // SHA256 hash for deduplication
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// StorageStats contains current storage statistics.
type StorageStats struct {
	ImageCount     int       `json:"image_count"`
	TotalSizeBytes int64     `json:"total_size_bytes"`
	OldestImage    time.Time `json:"oldest_image"`
	NewestImage    time.Time `json:"newest_image"`
}

// Storage manages image storage with limits and cleanup.
type Storage struct {
	baseDir       string
	repoPath      string
	mu            sync.RWMutex
	cleanupDone   chan struct{}
	closeOnce     sync.Once           // Prevents double-close panic
	images        map[string]*StoredImage // id -> image
	hashIndex     map[string]string       // content_hash -> id (for dedup)
	totalSize     int64                   // Cached total size for performance
}

// New creates a new ImageStorage instance.
func New(repoPath string) (*Storage, error) {
	baseDir := filepath.Join(repoPath, CdevImagesDir)

	// Create directory if not exists
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create images directory: %w", err)
	}

	s := &Storage{
		baseDir:     baseDir,
		repoPath:    repoPath,
		cleanupDone: make(chan struct{}),
		images:      make(map[string]*StoredImage),
		hashIndex:   make(map[string]string),
	}

	// Load existing images into memory
	if err := s.loadExistingImages(); err != nil {
		log.Warn().Err(err).Msg("failed to load existing images")
	}

	// Start cleanup goroutine
	go s.cleanupLoop()

	log.Info().Str("dir", baseDir).Int("existing", len(s.images)).Msg("image storage initialized")

	return s, nil
}

// loadExistingImages scans the directory and loads existing images into memory.
func (s *Storage) loadExistingImages() error {
	files, err := os.ReadDir(s.baseDir)
	if err != nil {
		return err
	}

	var totalSize int64
	for _, f := range files {
		if f.IsDir() || strings.HasPrefix(f.Name(), ".") || strings.HasSuffix(f.Name(), ".tmp") {
			continue
		}

		info, err := f.Info()
		if err != nil {
			continue
		}

		// Extract ID from filename (img_<id>.<ext>)
		id := extractIDFromFilename(f.Name())
		if id == "" {
			continue
		}

		// Determine MIME type from extension
		mimeType := extensionToMimeType(filepath.Ext(f.Name()))

		img := &StoredImage{
			ID:        id,
			LocalPath: filepath.Join(CdevImagesDir, f.Name()),
			FullPath:  filepath.Join(s.baseDir, f.Name()),
			MimeType:  mimeType,
			Size:      info.Size(),
			CreatedAt: info.ModTime(),
			ExpiresAt: info.ModTime().Add(ImageTTL),
		}

		s.images[id] = img
		totalSize += info.Size()
	}
	s.totalSize = totalSize

	return nil
}

// Store saves an image from a reader and returns the stored image info.
// It validates the content, checks limits, and handles deduplication.
func (s *Storage) Store(reader io.Reader, mimeType string) (*StoredImage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate MIME type
	ext, ok := supportedMimeTypes[mimeType]
	if !ok {
		return nil, fmt.Errorf("unsupported image type: %s (supported: jpeg, png, gif, webp)", mimeType)
	}

	// Read into memory (with size limit)
	maxBytes := int64(MaxSingleImageMB*1024*1024) + 1
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read image data: %w", err)
	}

	// Check size limit
	if int64(len(data)) > int64(MaxSingleImageMB*1024*1024) {
		return nil, fmt.Errorf("image exceeds maximum size of %dMB", MaxSingleImageMB)
	}

	// Validate magic bytes
	if !s.validateMagicBytes(data, mimeType) {
		return nil, fmt.Errorf("invalid image format: content does not match declared type %s", mimeType)
	}

	// Calculate content hash for deduplication
	hash := sha256.Sum256(data)
	contentHash := hex.EncodeToString(hash[:])

	// Check for duplicate
	if existingID, exists := s.hashIndex[contentHash]; exists {
		if img, ok := s.images[existingID]; ok {
			// Update expiration time on access
			img.ExpiresAt = time.Now().Add(ImageTTL)
			log.Debug().Str("id", existingID).Msg("returning existing image (dedup)")
			return img, nil
		}
	}

	// Check storage limits
	if ok, _ := s.canAcceptUploadLocked(int64(len(data))); !ok {
		// Try to free space by cleaning expired images
		s.cleanupExpiredLocked()

		// Check again
		if ok, _ = s.canAcceptUploadLocked(int64(len(data))); !ok {
			// Try LRU eviction
			s.evictOldestLocked()

			// Final check
			if ok, msg := s.canAcceptUploadLocked(int64(len(data))); !ok {
				return nil, fmt.Errorf("%s", msg)
			}
		}
	}

	// Generate unique filename
	id := uuid.New().String()[:12]
	filename := fmt.Sprintf("img_%s%s", id, ext)
	fullPath := filepath.Join(s.baseDir, filename)
	tempPath := fullPath + ".tmp"

	// Write to temp file first
	file, err := os.Create(tempPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create image file: %w", err)
	}

	_, err = file.Write(data)
	_ = file.Close()
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to write image data: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, fullPath); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to save image: %w", err)
	}

	// Set permissions (read-only for others)
	_ = os.Chmod(fullPath, 0644)

	now := time.Now()
	img := &StoredImage{
		ID:          id,
		LocalPath:   filepath.Join(CdevImagesDir, filename),
		FullPath:    fullPath,
		MimeType:    mimeType,
		Size:        int64(len(data)),
		ContentHash: contentHash,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ImageTTL),
	}

	// Add to indexes and update cached size
	s.images[id] = img
	s.hashIndex[contentHash] = id
	s.totalSize += img.Size

	log.Info().
		Str("id", id).
		Int64("size", img.Size).
		Str("mime", mimeType).
		Msg("image stored")

	return img, nil
}

// Get retrieves an image by ID.
func (s *Storage) Get(id string) (*StoredImage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	img, ok := s.images[id]
	if !ok {
		return nil, fmt.Errorf("image not found: %s", id)
	}

	// Verify file exists
	if _, err := os.Stat(img.FullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("image file not found: %s", id)
	}

	return img, nil
}

// GetLocalPath returns the local path for an image ID.
// This is the path that can be passed to Claude CLI.
func (s *Storage) GetLocalPath(id string) (string, error) {
	img, err := s.Get(id)
	if err != nil {
		return "", err
	}
	return img.LocalPath, nil
}

// Delete removes an image by ID.
func (s *Storage) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	img, ok := s.images[id]
	if !ok {
		return fmt.Errorf("image not found: %s", id)
	}

	// Remove file
	if err := os.Remove(img.FullPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete image file: %w", err)
	}

	// Remove from indexes and update cached size
	delete(s.hashIndex, img.ContentHash)
	delete(s.images, id)
	s.totalSize -= img.Size

	log.Info().Str("id", id).Msg("image deleted")

	return nil
}

// Clear removes all images.
func (s *Storage) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, img := range s.images {
		_ = os.Remove(img.FullPath)
		delete(s.images, id)
	}
	s.hashIndex = make(map[string]string)
	s.totalSize = 0

	log.Info().Msg("all images cleared")

	return nil
}

// GetStats returns current storage statistics.
func (s *Storage) GetStats() StorageStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := StorageStats{
		ImageCount:     len(s.images),
		TotalSizeBytes: s.totalSize,
	}

	for _, img := range s.images {
		if stats.OldestImage.IsZero() || img.CreatedAt.Before(stats.OldestImage) {
			stats.OldestImage = img.CreatedAt
		}
		if img.CreatedAt.After(stats.NewestImage) {
			stats.NewestImage = img.CreatedAt
		}
	}

	return stats
}

// CanAcceptUpload checks if the storage can accept an upload of the given size.
func (s *Storage) CanAcceptUpload(sizeBytes int64) (bool, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.canAcceptUploadLocked(sizeBytes)
}

func (s *Storage) canAcceptUploadLocked(sizeBytes int64) (bool, string) {
	// Check single file size
	if sizeBytes > int64(MaxSingleImageMB*1024*1024) {
		return false, fmt.Sprintf("Image exceeds maximum size of %dMB", MaxSingleImageMB)
	}

	// Check count limit
	if len(s.images) >= MaxImagesCount {
		return false, "Too many images. Please wait for cleanup or delete old images."
	}

	// Check total size limit using cached value (O(1) instead of O(n))
	if s.totalSize+sizeBytes > int64(MaxTotalSizeMB*1024*1024) {
		return false, "Image storage full. Please wait for cleanup."
	}

	return true, ""
}

// validateMagicBytes checks if the data starts with valid magic bytes for the MIME type.
func (s *Storage) validateMagicBytes(data []byte, mimeType string) bool {
	expected, ok := magicBytes[mimeType]
	if !ok {
		return false
	}

	if len(data) < len(expected) {
		return false
	}

	for i, b := range expected {
		if data[i] != b {
			return false
		}
	}

	// Additional validation for WebP: check for "WEBP" signature at offset 8
	if mimeType == "image/webp" {
		if len(data) < 12 {
			return false
		}
		// Check "WEBP" at offset 8
		if data[8] != 'W' || data[9] != 'E' || data[10] != 'B' || data[11] != 'P' {
			return false
		}
	}

	return true
}

// ValidatePath checks if a path is valid and within the images directory.
// This prevents path traversal attacks using multiple techniques:
// 1. Clean the path to resolve "..", ".", and redundant separators
// 2. Verify the cleaned path is still under the images directory
// 3. Check the path doesn't escape via symlinks
func (s *Storage) ValidatePath(path string) error {
	// Clean the path to resolve "..", ".", etc.
	cleanPath := filepath.Clean(path)

	// Reject if path contains null bytes (can bypass some checks)
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("invalid path: null bytes not allowed")
	}

	// Check if cleaned path still starts with correct prefix
	// This catches ".." traversal attempts
	if !strings.HasPrefix(cleanPath, CdevImagesDir) {
		return fmt.Errorf("invalid path: must be within %s", CdevImagesDir)
	}

	// Ensure there are no extra path components after cleaning
	// e.g., reject ".cdev/images/../../../etc/passwd"
	relPath := strings.TrimPrefix(cleanPath, CdevImagesDir)
	relPath = strings.TrimPrefix(relPath, string(filepath.Separator))
	if strings.Contains(relPath, string(filepath.Separator)) {
		return fmt.Errorf("invalid path: subdirectories not allowed")
	}

	// Check extension
	ext := strings.ToLower(filepath.Ext(cleanPath))
	valid := false
	for _, e := range supportedMimeTypes {
		if e == ext {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid path: unsupported extension %s", ext)
	}

	// Build full path and verify it's still under the base directory
	fullPath := filepath.Join(s.repoPath, cleanPath)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Verify the absolute path is under the base directory
	// This catches symlink escapes
	absBase, _ := filepath.Abs(s.baseDir)
	if !strings.HasPrefix(absPath, absBase+string(filepath.Separator)) && absPath != absBase {
		return fmt.Errorf("invalid path: escapes images directory")
	}

	// Verify file exists and is a regular file (not symlink to outside)
	info, err := os.Lstat(fullPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("image not found at path: %s", path)
	}
	if err != nil {
		return fmt.Errorf("cannot access path: %w", err)
	}

	// Reject symlinks (could point outside the images directory)
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("invalid path: symlinks not allowed")
	}

	return nil
}

// Close stops the storage and cleanup goroutine.
// Safe to call multiple times.
func (s *Storage) Close() {
	s.closeOnce.Do(func() {
		close(s.cleanupDone)
	})
}

// cleanupLoop periodically cleans up expired images.
func (s *Storage) cleanupLoop() {
	ticker := time.NewTicker(CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.cleanupDone:
			return
		case <-ticker.C:
			s.Cleanup()
		}
	}
}

// Cleanup removes expired images.
func (s *Storage) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupExpiredLocked()
}

func (s *Storage) cleanupExpiredLocked() {
	now := time.Now()
	var removed int

	for id, img := range s.images {
		if now.After(img.ExpiresAt) {
			_ = os.Remove(img.FullPath)
			delete(s.hashIndex, img.ContentHash)
			delete(s.images, id)
			s.totalSize -= img.Size
			removed++
		}
	}

	if removed > 0 {
		log.Info().Int("count", removed).Msg("cleaned up expired images")
	}
}

// evictOldestLocked removes the oldest image to free space.
func (s *Storage) evictOldestLocked() {
	if len(s.images) == 0 {
		return
	}

	// Find oldest image
	var oldestID string
	var oldestTime time.Time

	for id, img := range s.images {
		if oldestID == "" || img.CreatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = img.CreatedAt
		}
	}

	if oldestID != "" {
		img := s.images[oldestID]
		_ = os.Remove(img.FullPath)
		delete(s.hashIndex, img.ContentHash)
		delete(s.images, oldestID)
		s.totalSize -= img.Size
		log.Info().Str("id", oldestID).Msg("evicted oldest image (LRU)")
	}
}

// List returns all stored images sorted by creation time (newest first).
func (s *Storage) List() []*StoredImage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	images := make([]*StoredImage, 0, len(s.images))
	for _, img := range s.images {
		images = append(images, img)
	}

	sort.Slice(images, func(i, j int) bool {
		return images[i].CreatedAt.After(images[j].CreatedAt)
	})

	return images
}

// extractIDFromFilename extracts the ID from a filename like "img_abc123def.jpg".
func extractIDFromFilename(filename string) string {
	if !strings.HasPrefix(filename, "img_") {
		return ""
	}
	// Remove "img_" prefix and extension
	name := strings.TrimPrefix(filename, "img_")
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext)
}

// extensionToMimeType converts a file extension to MIME type.
func extensionToMimeType(ext string) string {
	ext = strings.ToLower(ext)
	for mime, e := range supportedMimeTypes {
		if e == ext {
			return mime
		}
	}
	return "application/octet-stream"
}
