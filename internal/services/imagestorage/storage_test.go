package imagestorage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test JPEG magic bytes
var testJPEGData = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

// Test PNG magic bytes
var testPNGData = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

// Test GIF magic bytes
var testGIFData = []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}

// Test WebP magic bytes (RIFF header)
var testWebPData = []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}

func setupTestStorage(t *testing.T) (*Storage, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "imagestorage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	storage, err := New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create storage: %v", err)
	}

	return storage, tmpDir
}

func cleanupTestStorage(storage *Storage, tmpDir string) {
	storage.Close()
	os.RemoveAll(tmpDir)
}

func TestNew(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Check that the images directory was created
	imagesDir := filepath.Join(tmpDir, CdevImagesDir)
	if _, err := os.Stat(imagesDir); os.IsNotExist(err) {
		t.Errorf("images directory was not created: %s", imagesDir)
	}
}

func TestNew_InvalidPath(t *testing.T) {
	// Try to create storage with an invalid path
	_, err := New("/nonexistent/path/that/cannot/be/created")
	if err == nil {
		t.Error("expected error for invalid path, got nil")
	}
}

func TestStore_JPEG(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Create test JPEG data with more bytes
	data := make([]byte, 100)
	copy(data, testJPEGData)

	img, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if img.ID == "" {
		t.Error("expected non-empty ID")
	}
	if img.MimeType != "image/jpeg" {
		t.Errorf("expected mime type image/jpeg, got %s", img.MimeType)
	}
	if img.Size != int64(len(data)) {
		t.Errorf("expected size %d, got %d", len(data), img.Size)
	}
	if img.LocalPath == "" {
		t.Error("expected non-empty LocalPath")
	}
	if img.ContentHash == "" {
		t.Error("expected non-empty ContentHash")
	}
}

func TestStore_PNG(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testPNGData)

	img, err := storage.Store(bytes.NewReader(data), "image/png")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if img.MimeType != "image/png" {
		t.Errorf("expected mime type image/png, got %s", img.MimeType)
	}
}

func TestStore_GIF(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testGIFData)

	img, err := storage.Store(bytes.NewReader(data), "image/gif")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if img.MimeType != "image/gif" {
		t.Errorf("expected mime type image/gif, got %s", img.MimeType)
	}
}

func TestStore_WebP(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testWebPData)

	img, err := storage.Store(bytes.NewReader(data), "image/webp")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if img.MimeType != "image/webp" {
		t.Errorf("expected mime type image/webp, got %s", img.MimeType)
	}
}

func TestStore_UnsupportedMimeType(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := []byte("not an image")
	_, err := storage.Store(bytes.NewReader(data), "image/bmp")
	if err == nil {
		t.Error("expected error for unsupported mime type")
	}
}

func TestStore_InvalidMagicBytes(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Data that doesn't match declared JPEG type
	data := []byte("this is not a jpeg image with valid headers")
	_, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err == nil {
		t.Error("expected error for invalid magic bytes")
	}
}

func TestStore_Deduplication(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testJPEGData)

	// Store the same image twice
	img1, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("first Store failed: %v", err)
	}

	img2, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("second Store failed: %v", err)
	}

	// Should return the same image (deduplication)
	if img1.ID != img2.ID {
		t.Errorf("expected same ID for duplicate content, got %s and %s", img1.ID, img2.ID)
	}

	// Only one image should be stored
	stats := storage.GetStats()
	if stats.ImageCount != 1 {
		t.Errorf("expected 1 image after dedup, got %d", stats.ImageCount)
	}
}

func TestGet(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testJPEGData)

	stored, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Get the image
	img, err := storage.Get(stored.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if img.ID != stored.ID {
		t.Errorf("expected ID %s, got %s", stored.ID, img.ID)
	}
}

func TestGet_NotFound(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	_, err := storage.Get("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent image")
	}
}

func TestGetLocalPath(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testJPEGData)

	stored, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	path, err := storage.GetLocalPath(stored.ID)
	if err != nil {
		t.Fatalf("GetLocalPath failed: %v", err)
	}

	if path != stored.LocalPath {
		t.Errorf("expected path %s, got %s", stored.LocalPath, path)
	}
}

func TestDelete(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	data := make([]byte, 100)
	copy(data, testJPEGData)

	stored, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(stored.FullPath); os.IsNotExist(err) {
		t.Fatal("file should exist before delete")
	}

	// Delete the image
	err = storage.Delete(stored.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify file is deleted
	if _, err := os.Stat(stored.FullPath); !os.IsNotExist(err) {
		t.Error("file should be deleted")
	}

	// Verify get fails
	_, err = storage.Get(stored.ID)
	if err == nil {
		t.Error("expected error when getting deleted image")
	}
}

func TestDelete_NotFound(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	err := storage.Delete("nonexistent")
	if err == nil {
		t.Error("expected error when deleting non-existent image")
	}
}

func TestClear(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Store multiple images
	for i := 0; i < 3; i++ {
		data := make([]byte, 100+i)
		copy(data, testJPEGData)
		_, err := storage.Store(bytes.NewReader(data), "image/jpeg")
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}
	}

	stats := storage.GetStats()
	if stats.ImageCount != 3 {
		t.Errorf("expected 3 images, got %d", stats.ImageCount)
	}

	// Clear all
	err := storage.Clear()
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	stats = storage.GetStats()
	if stats.ImageCount != 0 {
		t.Errorf("expected 0 images after clear, got %d", stats.ImageCount)
	}
}

func TestGetStats(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Store some images
	for i := 0; i < 3; i++ {
		data := make([]byte, 100+i*10)
		copy(data, testJPEGData)
		_, err := storage.Store(bytes.NewReader(data), "image/jpeg")
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}
	}

	stats := storage.GetStats()
	if stats.ImageCount != 3 {
		t.Errorf("expected 3 images, got %d", stats.ImageCount)
	}
	if stats.TotalSizeBytes != 100+110+120 {
		t.Errorf("expected total size %d, got %d", 100+110+120, stats.TotalSizeBytes)
	}
	if stats.OldestImage.IsZero() {
		t.Error("expected non-zero OldestImage")
	}
	if stats.NewestImage.IsZero() {
		t.Error("expected non-zero NewestImage")
	}
}

func TestCanAcceptUpload(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Should be able to accept small upload
	ok, msg := storage.CanAcceptUpload(1024)
	if !ok {
		t.Errorf("should accept small upload: %s", msg)
	}

	// Should reject file larger than max single size
	ok, msg = storage.CanAcceptUpload(int64(MaxSingleImageMB*1024*1024) + 1)
	if ok {
		t.Error("should reject file larger than max single size")
	}
}

func TestValidatePath(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Store an image to create a valid path
	data := make([]byte, 100)
	copy(data, testJPEGData)
	stored, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Valid path
	err = storage.ValidatePath(stored.LocalPath)
	if err != nil {
		t.Errorf("ValidatePath failed for valid path: %v", err)
	}

	// Path traversal attack
	err = storage.ValidatePath(".cdev/images/../../../etc/passwd")
	if err == nil {
		t.Error("should reject path traversal")
	}

	// Wrong prefix
	err = storage.ValidatePath("some/other/path.jpg")
	if err == nil {
		t.Error("should reject path not in images directory")
	}

	// Invalid extension
	err = storage.ValidatePath(".cdev/images/file.txt")
	if err == nil {
		t.Error("should reject invalid extension")
	}
}

func TestList(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Store multiple images with small delay
	var ids []string
	for i := 0; i < 3; i++ {
		data := make([]byte, 100+i*10)
		copy(data, testJPEGData)
		img, err := storage.Store(bytes.NewReader(data), "image/jpeg")
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}
		ids = append(ids, img.ID)
		time.Sleep(10 * time.Millisecond) // Small delay for ordering
	}

	images := storage.List()
	if len(images) != 3 {
		t.Errorf("expected 3 images, got %d", len(images))
	}

	// Should be sorted newest first
	if images[0].ID != ids[2] {
		t.Error("images should be sorted newest first")
	}
}

func TestExtractIDFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"img_abc123.jpg", "abc123"},
		{"img_xyz789.png", "xyz789"},
		{"notanimage.jpg", ""},
		{"", ""},
		{"img_.jpg", ""},
	}

	for _, tt := range tests {
		result := extractIDFromFilename(tt.filename)
		if result != tt.expected {
			t.Errorf("extractIDFromFilename(%q) = %q, want %q", tt.filename, result, tt.expected)
		}
	}
}

func TestExtensionToMimeType(t *testing.T) {
	tests := []struct {
		ext      string
		expected string
	}{
		{".jpg", "image/jpeg"},
		{".jpeg", "application/octet-stream"}, // Only .jpg is in supportedMimeTypes
		{".png", "image/png"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".JPG", "image/jpeg"},
		{".PNG", "image/png"},
		{".txt", "application/octet-stream"},
		{"", "application/octet-stream"},
	}

	for _, tt := range tests {
		result := extensionToMimeType(tt.ext)
		if result != tt.expected {
			t.Errorf("extensionToMimeType(%q) = %q, want %q", tt.ext, result, tt.expected)
		}
	}
}

func TestValidateMagicBytes(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	tests := []struct {
		name     string
		data     []byte
		mimeType string
		expected bool
	}{
		{"valid JPEG", testJPEGData, "image/jpeg", true},
		{"valid PNG", testPNGData, "image/png", true},
		{"valid GIF", testGIFData, "image/gif", true},
		{"valid WebP", testWebPData, "image/webp", true},
		{"invalid JPEG", []byte{0x00, 0x00, 0x00}, "image/jpeg", false},
		{"invalid PNG", []byte{0xFF, 0xD8, 0xFF}, "image/png", false},
		{"too short", []byte{0xFF}, "image/jpeg", false},
		{"unknown type", testJPEGData, "image/bmp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := storage.validateMagicBytes(tt.data, tt.mimeType)
			if result != tt.expected {
				t.Errorf("validateMagicBytes() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLoadExistingImages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "imagestorage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create images directory and place a test file
	imagesDir := filepath.Join(tmpDir, CdevImagesDir)
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		t.Fatalf("failed to create images dir: %v", err)
	}

	// Create a valid image file
	testFile := filepath.Join(imagesDir, "img_test123.jpg")
	data := make([]byte, 100)
	copy(data, testJPEGData)
	if err := os.WriteFile(testFile, data, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Create storage - it should load existing images
	storage, err := New(tmpDir)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	// Verify the image was loaded
	stats := storage.GetStats()
	if stats.ImageCount != 1 {
		t.Errorf("expected 1 image loaded, got %d", stats.ImageCount)
	}

	// Verify we can get the image
	img, err := storage.Get("test123")
	if err != nil {
		t.Fatalf("failed to get loaded image: %v", err)
	}

	if img.MimeType != "image/jpeg" {
		t.Errorf("expected mime type image/jpeg, got %s", img.MimeType)
	}
}

func TestCleanup(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Store an image
	data := make([]byte, 100)
	copy(data, testJPEGData)
	img, err := storage.Store(bytes.NewReader(data), "image/jpeg")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Manually set expiration in the past
	storage.mu.Lock()
	if storedImg, ok := storage.images[img.ID]; ok {
		storedImg.ExpiresAt = time.Now().Add(-time.Hour)
	}
	storage.mu.Unlock()

	// Run cleanup
	storage.Cleanup()

	// Verify image was cleaned up
	_, err = storage.Get(img.ID)
	if err == nil {
		t.Error("expected error getting expired image after cleanup")
	}

	stats := storage.GetStats()
	if stats.ImageCount != 0 {
		t.Errorf("expected 0 images after cleanup, got %d", stats.ImageCount)
	}
}

func TestEvictOldest(t *testing.T) {
	storage, tmpDir := setupTestStorage(t)
	defer cleanupTestStorage(storage, tmpDir)

	// Store multiple images with different times
	var firstID string
	for i := 0; i < 3; i++ {
		data := make([]byte, 100+i*10)
		copy(data, testJPEGData)
		img, err := storage.Store(bytes.NewReader(data), "image/jpeg")
		if err != nil {
			t.Fatalf("Store failed: %v", err)
		}
		if i == 0 {
			firstID = img.ID
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Evict oldest
	storage.mu.Lock()
	storage.evictOldestLocked()
	storage.mu.Unlock()

	// First image should be evicted
	_, err := storage.Get(firstID)
	if err == nil {
		t.Error("expected oldest image to be evicted")
	}

	stats := storage.GetStats()
	if stats.ImageCount != 2 {
		t.Errorf("expected 2 images after eviction, got %d", stats.ImageCount)
	}
}
