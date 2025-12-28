// Package http implements the HTTP API server for cdev.
package http

import (
	"net/http"
	"time"

	"github.com/brianly1003/cdev/internal/server/http/middleware"
	"github.com/brianly1003/cdev/internal/services/imagestorage"
	"github.com/rs/zerolog/log"
)

// ImageHandler handles image upload operations.
type ImageHandler struct {
	storage     *imagestorage.Storage
	rateLimiter *middleware.RateLimiter
}

// NewImageHandler creates a new ImageHandler.
func NewImageHandler(storage *imagestorage.Storage) *ImageHandler {
	return &ImageHandler{
		storage: storage,
		rateLimiter: middleware.NewRateLimiter(
			middleware.WithMaxRequests(10),       // 10 uploads per minute
			middleware.WithWindow(1*time.Minute), // 1 minute
		),
	}
}

// Close cleans up resources.
func (h *ImageHandler) Close() {
	if h.rateLimiter != nil {
		h.rateLimiter.Close()
	}
}

// HandleImages handles POST /api/images (upload) and GET /api/images (list/get)
//
//	@Summary		List images
//	@Description	Returns list of all stored images or details of a specific image by ID
//	@Tags			images
//	@Produce		json
//	@Param			id			query		string	false	"Image ID to get specific image details"
//	@Success		200			{object}	ImageListResponse
//	@Failure		404			{object}	ErrorResponse		"Image not found"
//	@Failure		503			{object}	ErrorResponse		"Image storage not available"
//	@Router			/api/images [get]
func (h *ImageHandler) HandleImages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleUpload(w, r)
	case http.MethodGet:
		h.handleList(w, r)
	case http.MethodDelete:
		h.handleDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleUpload handles POST /api/images - upload a new image.
//
//	@Summary		Upload image
//	@Description	Upload an image for Claude to analyze (JPEG, PNG, GIF, WebP, max 10MB)
//	@Tags			images
//	@Accept			multipart/form-data
//	@Produce		json
//	@Param			file		formData	file	true	"Image file (JPEG, PNG, GIF, WebP, max 10MB)"
//	@Success		200			{object}	ImageUploadResponse
//	@Failure		400			{object}	ErrorResponse		"Invalid request or missing file"
//	@Failure		413			{object}	ErrorResponse		"Image too large"
//	@Failure		415			{object}	ErrorResponse		"Unsupported media type"
//	@Failure		429			{object}	ErrorResponse		"Rate limit exceeded"
//	@Failure		503			{object}	ErrorResponse		"Image storage not available"
//	@Router			/api/images [post]
func (h *ImageHandler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Image storage not available",
		})
		return
	}

	// Rate limiting by IP
	clientIP := middleware.IPKeyExtractor(r)
	if !h.rateLimiter.Allow(clientIP) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error":   "rate_limit_exceeded",
			"message": "Too many uploads. Please wait before trying again.",
		})
		return
	}

	// Limit request body size to prevent DoS (10MB + overhead)
	r.Body = http.MaxBytesReader(w, r.Body, int64(imagestorage.MaxSingleImageMB*1024*1024)+1024)

	// Parse multipart form
	if err := r.ParseMultipartForm(int64(imagestorage.MaxSingleImageMB * 1024 * 1024)); err != nil {
		if err.Error() == "http: request body too large" {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error":   "image_too_large",
				"message": "Image exceeds maximum size of 10MB",
			})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "Failed to parse multipart form: " + err.Error(),
		})
		return
	}

	// Get the file from the form
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_file",
			"message": "No file provided. Use 'file' field in multipart form.",
		})
		return
	}
	defer func() { _ = file.Close() }()

	// Validate Content-Type
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		// Try to detect from file extension
		mimeType = detectMimeTypeFromFilename(header.Filename)
	}

	// Store the image
	img, err := h.storage.Store(file, mimeType)
	if err != nil {
		log.Error().Err(err).Str("filename", header.Filename).Msg("failed to store image")

		// Determine appropriate error response
		errMsg := err.Error()
		status := http.StatusBadRequest

		if contains(errMsg, "unsupported image type") {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{
				"error":   "unsupported_type",
				"message": errMsg,
			})
			return
		}
		if contains(errMsg, "exceeds maximum size") {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{
				"error":   "image_too_large",
				"message": errMsg,
			})
			return
		}
		if contains(errMsg, "storage full") || contains(errMsg, "Too many images") {
			status = http.StatusInsufficientStorage
		}
		if contains(errMsg, "invalid image format") {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "invalid_format",
				"message": errMsg,
			})
			return
		}

		writeJSON(w, status, map[string]string{
			"error":   "storage_error",
			"message": errMsg,
		})
		return
	}

	log.Info().
		Str("id", img.ID).
		Str("filename", header.Filename).
		Int64("size", img.Size).
		Str("mime", img.MimeType).
		Msg("image uploaded successfully")

	writeJSON(w, http.StatusOK, ImageUploadResponse{
		Success:   true,
		ID:        img.ID,
		LocalPath: img.LocalPath,
		MimeType:  img.MimeType,
		Size:      img.Size,
		ExpiresAt: img.ExpiresAt.Unix(),
	})
}

// isValidImageID checks if the image ID has a valid format.
// Valid format: 12 characters, alphanumeric with dashes (UUID prefix format).
func isValidImageID(id string) bool {
	if len(id) == 0 || len(id) > 36 {
		return false
	}
	for _, c := range id {
		if !((c >= 'a' && c <= 'f') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// handleList handles GET /api/images - list all images or get single image.
func (h *ImageHandler) handleList(w http.ResponseWriter, r *http.Request) {
	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Image storage not available",
		})
		return
	}

	// Check if requesting a specific image
	imageID := r.URL.Query().Get("id")
	if imageID != "" {
		// Validate image ID format to prevent injection
		if !isValidImageID(imageID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "invalid_id",
				"message": "Invalid image ID format",
			})
			return
		}
		img, err := h.storage.Get(imageID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "image_not_found",
				"id":    imageID,
			})
			return
		}

		writeJSON(w, http.StatusOK, ImageInfoResponse{
			ID:        img.ID,
			LocalPath: img.LocalPath,
			MimeType:  img.MimeType,
			Size:      img.Size,
			CreatedAt: img.CreatedAt.Unix(),
			ExpiresAt: img.ExpiresAt.Unix(),
		})
		return
	}

	// List all images
	images := h.storage.List()
	stats := h.storage.GetStats()

	imageList := make([]ImageInfoResponse, 0, len(images))
	for _, img := range images {
		imageList = append(imageList, ImageInfoResponse{
			ID:        img.ID,
			LocalPath: img.LocalPath,
			MimeType:  img.MimeType,
			Size:      img.Size,
			CreatedAt: img.CreatedAt.Unix(),
			ExpiresAt: img.ExpiresAt.Unix(),
		})
	}

	writeJSON(w, http.StatusOK, ImageListResponse{
		Images:         imageList,
		Count:          len(imageList),
		TotalSizeBytes: stats.TotalSizeBytes,
		MaxImages:      imagestorage.MaxImagesCount,
		MaxTotalSizeMB: imagestorage.MaxTotalSizeMB,
	})
}

// handleDelete handles DELETE /api/images?id=... - delete an image.
func (h *ImageHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Image storage not available",
		})
		return
	}

	imageID := r.URL.Query().Get("id")
	if imageID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_id",
			"message": "id query parameter is required",
		})
		return
	}

	// Validate image ID format
	if !isValidImageID(imageID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_id",
			"message": "Invalid image ID format",
		})
		return
	}

	if err := h.storage.Delete(imageID); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "image_not_found",
			"id":    imageID,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"id":      imageID,
		"message": "Image deleted",
	})
}

// detectMimeTypeFromFilename attempts to determine MIME type from filename extension.
func detectMimeTypeFromFilename(filename string) string {
	if len(filename) < 4 {
		return ""
	}

	ext := ""
	for i := len(filename) - 1; i >= 0; i-- {
		if filename[i] == '.' {
			ext = filename[i:]
			break
		}
	}

	switch toLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return ""
	}
}

// toLower converts a string to lowercase without importing strings.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ImageUploadResponse represents the response after uploading an image.
type ImageUploadResponse struct {
	Success   bool   `json:"success" example:"true"`
	ID        string `json:"id" example:"abc123def456"`
	LocalPath string `json:"local_path" example:".cdev/images/img_abc123def456.jpg"`
	MimeType  string `json:"mime_type" example:"image/jpeg"`
	Size      int64  `json:"size" example:"102400"`
	ExpiresAt int64  `json:"expires_at" example:"1705320600"`
}

// ImageInfoResponse represents information about a stored image.
type ImageInfoResponse struct {
	ID        string `json:"id" example:"abc123def456"`
	LocalPath string `json:"local_path" example:".cdev/images/img_abc123def456.jpg"`
	MimeType  string `json:"mime_type" example:"image/jpeg"`
	Size      int64  `json:"size" example:"102400"`
	CreatedAt int64  `json:"created_at" example:"1705317000"`
	ExpiresAt int64  `json:"expires_at" example:"1705320600"`
}

// ImageListResponse represents the list of stored images.
type ImageListResponse struct {
	Images         []ImageInfoResponse `json:"images"`
	Count          int                 `json:"count" example:"5"`
	TotalSizeBytes int64               `json:"total_size_bytes" example:"512000"`
	MaxImages      int                 `json:"max_images" example:"50"`
	MaxTotalSizeMB int                 `json:"max_total_size_mb" example:"100"`
}

// ValidateImagePath handles GET /api/images/validate?path=...
//
//	@Summary		Validate image path
//	@Description	Validates that an image path is safe and accessible
//	@Tags			images
//	@Produce		json
//	@Param			path	query		string	true	"Image path to validate"
//	@Success		200		{object}	ImageValidateResponse
//	@Failure		400		{object}	ErrorResponse	"Invalid path"
//	@Router			/api/images/validate [get]
func (h *ImageHandler) ValidateImagePath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Image storage not available",
		})
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "missing_path",
			"message": "path query parameter is required",
		})
		return
	}

	if err := h.storage.ValidatePath(path); err != nil {
		writeJSON(w, http.StatusBadRequest, ImageValidateResponse{
			Valid:   false,
			Path:    path,
			Message: err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, ImageValidateResponse{
		Valid: true,
		Path:  path,
	})
}

// ImageValidateResponse represents the response for path validation.
type ImageValidateResponse struct {
	Valid   bool   `json:"valid" example:"true"`
	Path    string `json:"path" example:".cdev/images/img_abc123.jpg"`
	Message string `json:"message,omitempty" example:"invalid path: path traversal not allowed"`
}

// HandleImageStats handles GET /api/images/stats
//
//	@Summary		Get image storage stats
//	@Description	Returns current storage statistics
//	@Tags			images
//	@Produce		json
//	@Success		200	{object}	ImageStatsResponse
//	@Failure		503	{object}	ErrorResponse	"Image storage not available"
//	@Router			/api/images/stats [get]
func (h *ImageHandler) HandleImageStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Image storage not available",
		})
		return
	}

	stats := h.storage.GetStats()

	canAccept, reason := h.storage.CanAcceptUpload(int64(imagestorage.MaxSingleImageMB * 1024 * 1024))

	writeJSON(w, http.StatusOK, ImageStatsResponse{
		ImageCount:      stats.ImageCount,
		TotalSizeBytes:  stats.TotalSizeBytes,
		MaxImages:       imagestorage.MaxImagesCount,
		MaxTotalSizeMB:  imagestorage.MaxTotalSizeMB,
		MaxSingleSizeMB: imagestorage.MaxSingleImageMB,
		CanAcceptUpload: canAccept,
		Reason:          reason,
	})
}

// ImageStatsResponse represents the storage statistics.
type ImageStatsResponse struct {
	ImageCount      int    `json:"image_count" example:"5"`
	TotalSizeBytes  int64  `json:"total_size_bytes" example:"512000"`
	MaxImages       int    `json:"max_images" example:"50"`
	MaxTotalSizeMB  int    `json:"max_total_size_mb" example:"100"`
	MaxSingleSizeMB int    `json:"max_single_size_mb" example:"10"`
	CanAcceptUpload bool   `json:"can_accept_upload" example:"true"`
	Reason          string `json:"reason,omitempty" example:""`
}

// HandleClearImages handles DELETE /api/images/all - clear all images.
//
//	@Summary		Clear all images
//	@Description	Removes all stored images
//	@Tags			images
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Failure		503	{object}	ErrorResponse	"Image storage not available"
//	@Router			/api/images/all [delete]
func (h *ImageHandler) HandleClearImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if h.storage == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "Image storage not available",
		})
		return
	}

	statsBefore := h.storage.GetStats()
	countBefore := statsBefore.ImageCount

	if err := h.storage.Clear(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "clear_failed",
			"message": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "All images cleared",
		"deleted": countBefore,
	})
}
