package http

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"testing"

	"github.com/brianly1003/cdev/internal/services/imagestorage"
)

// Test image data (valid JPEG magic bytes)
var testJPEGData = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

func setupTestImageHandler(t *testing.T) (*ImageHandler, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "image-handler-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	storage, err := imagestorage.New(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create storage: %v", err)
	}

	handler := NewImageHandler(storage)
	return handler, tmpDir
}

func cleanupTestImageHandler(handler *ImageHandler, tmpDir string) {
	handler.Close()
	os.RemoveAll(tmpDir)
}

func createMultipartRequest(t *testing.T, fieldName, filename, mimeType string, data []byte) (*http.Request, error) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Create form file with proper Content-Type header
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="`+fieldName+`"; filename="`+filename+`"`)
	h.Set("Content-Type", mimeType)

	part, err := writer.CreatePart(h)
	if err != nil {
		return nil, err
	}

	_, err = part.Write(data)
	if err != nil {
		return nil, err
	}

	err = writer.Close()
	if err != nil {
		return nil, err
	}

	req := httptest.NewRequest(http.MethodPost, "/api/images", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req, nil
}

func TestNewImageHandler(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	if handler.storage == nil {
		t.Error("expected storage to be set")
	}
	if handler.rateLimiter == nil {
		t.Error("expected rateLimiter to be set")
	}
}

func TestHandleImages_Upload(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Create test image data
	data := make([]byte, 100)
	copy(data, testJPEGData)

	req, err := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ImageUploadResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Success {
		t.Error("expected success to be true")
	}
	if resp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if resp.LocalPath == "" {
		t.Error("expected non-empty LocalPath")
	}
	if resp.MimeType != "image/jpeg" {
		t.Errorf("expected mime type image/jpeg, got %s", resp.MimeType)
	}
}

func TestHandleImages_Upload_MissingFile(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Request without file
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/images", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleImages_Upload_InvalidMimeType(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Create file with invalid mime type
	data := []byte("not an image")
	req, err := createMultipartRequest(t, "file", "test.txt", "text/plain", data)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	// Should fail because of unsupported mime type
	if rec.Code != http.StatusUnsupportedMediaType && rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 415 or 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImages_Upload_InvalidMagicBytes(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Create file claiming to be JPEG but with wrong magic bytes
	data := []byte("this is not a valid jpeg file at all")
	req, err := createMultipartRequest(t, "file", "fake.jpg", "image/jpeg", data)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid magic bytes, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleImages_List(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// First upload an image
	data := make([]byte, 100)
	copy(data, testJPEGData)

	req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	// Now list images
	req = httptest.NewRequest(http.MethodGet, "/api/images", nil)
	rec = httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp ImageListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Count != 1 {
		t.Errorf("expected 1 image, got %d", resp.Count)
	}
	if len(resp.Images) != 1 {
		t.Errorf("expected 1 image in list, got %d", len(resp.Images))
	}
}

func TestHandleImages_GetSingle(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// First upload an image
	data := make([]byte, 100)
	copy(data, testJPEGData)

	req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	var uploadResp ImageUploadResponse
	json.NewDecoder(rec.Body).Decode(&uploadResp)

	// Get single image by ID
	req = httptest.NewRequest(http.MethodGet, "/api/images?id="+uploadResp.ID, nil)
	rec = httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp ImageInfoResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != uploadResp.ID {
		t.Errorf("expected ID %s, got %s", uploadResp.ID, resp.ID)
	}
}

func TestHandleImages_GetSingle_NotFound(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Use valid ID format (hex chars only) that doesn't exist
	req := httptest.NewRequest(http.MethodGet, "/api/images?id=abcd1234ef56", nil)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleImages_Delete(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// First upload an image
	data := make([]byte, 100)
	copy(data, testJPEGData)

	req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	var uploadResp ImageUploadResponse
	json.NewDecoder(rec.Body).Decode(&uploadResp)

	// Delete the image
	req = httptest.NewRequest(http.MethodDelete, "/api/images?id="+uploadResp.ID, nil)
	rec = httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Verify it's deleted
	req = httptest.NewRequest(http.MethodGet, "/api/images?id="+uploadResp.ID, nil)
	rec = httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 after delete, got %d", rec.Code)
	}
}

func TestHandleImages_Delete_MissingID(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	req := httptest.NewRequest(http.MethodDelete, "/api/images", nil)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleImages_Delete_NotFound(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Use valid ID format (hex chars only) that doesn't exist
	req := httptest.NewRequest(http.MethodDelete, "/api/images?id=abcd1234ef56", nil)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rec.Code)
	}
}

func TestHandleImages_MethodNotAllowed(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	req := httptest.NewRequest(http.MethodPatch, "/api/images", nil)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rec.Code)
	}
}

func TestValidateImagePath(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// First upload an image
	data := make([]byte, 100)
	copy(data, testJPEGData)

	req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	var uploadResp ImageUploadResponse
	json.NewDecoder(rec.Body).Decode(&uploadResp)

	// Validate valid path
	req = httptest.NewRequest(http.MethodGet, "/api/images/validate?path="+uploadResp.LocalPath, nil)
	rec = httptest.NewRecorder()
	handler.ValidateImagePath(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ImageValidateResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if !resp.Valid {
		t.Error("expected valid to be true")
	}
}

func TestValidateImagePath_Invalid(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Path traversal attempt
	req := httptest.NewRequest(http.MethodGet, "/api/images/validate?path=.cdev/images/../../../etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ValidateImagePath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for path traversal, got %d", rec.Code)
	}

	var resp ImageValidateResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Valid {
		t.Error("expected valid to be false for path traversal")
	}
}

func TestValidateImagePath_MissingPath(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	req := httptest.NewRequest(http.MethodGet, "/api/images/validate", nil)
	rec := httptest.NewRecorder()
	handler.ValidateImagePath(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestHandleImageStats(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Upload some images
	for i := 0; i < 3; i++ {
		data := make([]byte, 100+i*10)
		copy(data, testJPEGData)
		req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
		rec := httptest.NewRecorder()
		handler.HandleImages(rec, req)
	}

	// Get stats
	req := httptest.NewRequest(http.MethodGet, "/api/images/stats", nil)
	rec := httptest.NewRecorder()
	handler.HandleImageStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp ImageStatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ImageCount != 3 {
		t.Errorf("expected 3 images, got %d", resp.ImageCount)
	}
	if resp.MaxImages != imagestorage.MaxImagesCount {
		t.Errorf("expected max images %d, got %d", imagestorage.MaxImagesCount, resp.MaxImages)
	}
	if resp.MaxTotalSizeMB != imagestorage.MaxTotalSizeMB {
		t.Errorf("expected max total size %d, got %d", imagestorage.MaxTotalSizeMB, resp.MaxTotalSizeMB)
	}
}

func TestHandleClearImages(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Upload some images
	for i := 0; i < 3; i++ {
		data := make([]byte, 100+i*10)
		copy(data, testJPEGData)
		req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
		rec := httptest.NewRecorder()
		handler.HandleImages(rec, req)
	}

	// Clear all images
	req := httptest.NewRequest(http.MethodDelete, "/api/images/all", nil)
	rec := httptest.NewRecorder()
	handler.HandleClearImages(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Verify all images are cleared
	req = httptest.NewRequest(http.MethodGet, "/api/images", nil)
	rec = httptest.NewRecorder()
	handler.HandleImages(rec, req)

	var resp ImageListResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Count != 0 {
		t.Errorf("expected 0 images after clear, got %d", resp.Count)
	}
}

func TestDetectMimeTypeFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"test.jpg", "image/jpeg"},
		{"test.jpeg", "image/jpeg"},
		{"test.png", "image/png"},
		{"test.gif", "image/gif"},
		{"test.webp", "image/webp"},
		{"test.JPG", "image/jpeg"},
		{"test.PNG", "image/png"},
		{"test.txt", ""},
		{"test", ""},
		{"", ""},
	}

	for _, tt := range tests {
		result := detectMimeTypeFromFilename(tt.filename)
		if result != tt.expected {
			t.Errorf("detectMimeTypeFromFilename(%q) = %q, want %q", tt.filename, result, tt.expected)
		}
	}
}

func TestToLower(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ABC", "abc"},
		{"abc", "abc"},
		{"AbC", "abc"},
		{"123", "123"},
		{"", ""},
		{"ABC123xyz", "abc123xyz"},
	}

	for _, tt := range tests {
		result := toLower(tt.input)
		if result != tt.expected {
			t.Errorf("toLower(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "hello", true},
		{"hello world", "xyz", false},
		{"hello", "hello", true},
		{"", "", true},
		{"hello", "", true},
		{"", "a", false},
	}

	for _, tt := range tests {
		result := contains(tt.s, tt.substr)
		if result != tt.expected {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
		}
	}
}

func TestHandleImages_NilStorage(t *testing.T) {
	handler := &ImageHandler{
		storage:     nil,
		rateLimiter: nil,
	}

	// Test POST
	req := httptest.NewRequest(http.MethodPost, "/api/images", nil)
	rec := httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 for nil storage, got %d", rec.Code)
	}

	// Test GET
	req = httptest.NewRequest(http.MethodGet, "/api/images", nil)
	rec = httptest.NewRecorder()
	handler.HandleImages(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503 for nil storage, got %d", rec.Code)
	}
}

func TestHandleImages_RateLimiting(t *testing.T) {
	handler, tmpDir := setupTestImageHandler(t)
	defer cleanupTestImageHandler(handler, tmpDir)

	// Reset rate limiter for test
	handler.rateLimiter.ResetAll()

	// Make multiple requests to trigger rate limiting
	for i := 0; i < 12; i++ {
		data := make([]byte, 100+i)
		copy(data, testJPEGData)

		req, _ := createMultipartRequest(t, "file", "test.jpg", "image/jpeg", data)
		req.RemoteAddr = "192.168.1.1:1234"
		rec := httptest.NewRecorder()

		handler.HandleImages(rec, req)

		if i < 10 && rec.Code != http.StatusOK {
			t.Errorf("request %d should succeed, got %d", i+1, rec.Code)
		}
		if i >= 10 && rec.Code != http.StatusTooManyRequests {
			t.Errorf("request %d should be rate limited, got %d", i+1, rec.Code)
		}
	}
}

// Benchmark tests
func BenchmarkHandleImages_Upload(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "image-handler-bench-*")
	defer os.RemoveAll(tmpDir)

	storage, _ := imagestorage.New(tmpDir)
	handler := NewImageHandler(storage)
	defer handler.Close()

	// Create test data
	data := make([]byte, 1024) // 1KB
	copy(data, testJPEGData)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.jpg")
		part.Write(data)
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/images", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()

		handler.HandleImages(rec, req)

		// Read body to complete the request
		io.Copy(io.Discard, rec.Body)
	}
}

func BenchmarkHandleImages_List(b *testing.B) {
	tmpDir, _ := os.MkdirTemp("", "image-handler-bench-*")
	defer os.RemoveAll(tmpDir)

	storage, _ := imagestorage.New(tmpDir)
	handler := NewImageHandler(storage)
	defer handler.Close()

	// Upload some images first
	for i := 0; i < 10; i++ {
		data := make([]byte, 100+i*10)
		copy(data, testJPEGData)

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, _ := writer.CreateFormFile("file", "test.jpg")
		part.Write(data)
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/api/images", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		rec := httptest.NewRecorder()
		handler.HandleImages(rec, req)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/images", nil)
		rec := httptest.NewRecorder()
		handler.HandleImages(rec, req)
		io.Copy(io.Discard, rec.Body)
	}
}
