# Image Upload Integration Guide for cdev-ios

This guide explains how to integrate image upload functionality from the cdev-ios app to the cdev agent.

## Overview

The cdev agent provides HTTP endpoints for uploading images that can be used with Claude. Images are stored per-workspace in `{workspace}/.cdev/images/` with automatic cleanup after 1 hour.

> **Important:** All image operations require a `workspace_id` query parameter to specify which workspace the images belong to.

## API Endpoints

### Upload Image

```
POST /api/images?workspace_id=<workspace_id>
Content-Type: multipart/form-data
```

**Query Parameters:**
- `workspace_id` (required): The workspace ID where the image will be stored

**Request Body:**
- Field name: `file`
- Supported formats: JPEG, PNG, GIF, WebP
- Max file size: 10 MB
- Max total storage: 100 MB (50 images)

**Response (Success - 200):**
```json
{
  "success": true,
  "id": "abc123def456",
  "local_path": ".cdev/images/img_abc123def456.jpg",
  "mime_type": "image/jpeg",
  "size": 102400,
  "expires_at": 1705320600
}
```

> **Note:** `local_path` is a relative path within the workspace (e.g., `.cdev/images/...`). Claude running in that workspace can directly access it.

**Response (Rate Limited - 429):**
```json
{
  "error": "rate_limit_exceeded",
  "message": "Too many uploads. Please wait before trying again."
}
```
Header: `Retry-After: 60`

**Response (Too Large - 413):**
```json
{
  "error": "image_too_large",
  "message": "Image exceeds maximum size of 10MB"
}
```

**Response (Invalid Format - 415):**
```json
{
  "error": "unsupported_type",
  "message": "unsupported image type: application/pdf (supported: jpeg, png, gif, webp)"
}
```

### List Images

```
GET /api/images?workspace_id=<workspace_id>
```

**Query Parameters:**
- `workspace_id` (required): The workspace ID to list images for

**Response:**
```json
{
  "images": [
    {
      "id": "abc123def456",
      "local_path": ".cdev/images/img_abc123def456.jpg",
      "mime_type": "image/jpeg",
      "size": 102400,
      "created_at": 1705317000,
      "expires_at": 1705320600
    }
  ],
  "count": 1,
  "total_size_bytes": 102400,
  "max_images": 50,
  "max_total_size_mb": 100
}
```

### Get Single Image

```
GET /api/images?workspace_id=<workspace_id>&id=abc123def456
```

**Query Parameters:**
- `workspace_id` (required): The workspace ID
- `id` (required): The image ID

**Response:**
```json
{
  "id": "abc123def456",
  "local_path": ".cdev/images/img_abc123def456.jpg",
  "mime_type": "image/jpeg",
  "size": 102400,
  "created_at": 1705317000,
  "expires_at": 1705320600
}
```

### Delete Image

```
DELETE /api/images?workspace_id=<workspace_id>&id=abc123def456
```

**Query Parameters:**
- `workspace_id` (required): The workspace ID
- `id` (required): The image ID to delete

**Response:**
```json
{
  "success": true,
  "id": "abc123def456",
  "message": "Image deleted"
}
```

### Get Storage Stats

```
GET /api/images/stats?workspace_id=<workspace_id>
```

**Query Parameters:**
- `workspace_id` (required): The workspace ID to get stats for

**Response:**
```json
{
  "image_count": 5,
  "total_size_bytes": 512000,
  "max_images": 50,
  "max_total_size_mb": 100,
  "max_single_size_mb": 10,
  "can_accept_upload": true,
  "reason": ""
}
```

### Clear All Images

```
DELETE /api/images/all?workspace_id=<workspace_id>
```

**Query Parameters:**
- `workspace_id` (required): The workspace ID to clear images for

**Response:**
```json
{
  "success": true,
  "message": "All images cleared",
  "deleted": 5
}
```

---

## iOS Implementation

### Swift Upload Example

```swift
import Foundation

class ImageUploader {
    private let baseURL: URL
    private let workspaceID: String

    init(host: String, port: Int, workspaceID: String) {
        self.baseURL = URL(string: "http://\(host):\(port)")!
        self.workspaceID = workspaceID
    }

    func uploadImage(_ imageData: Data, mimeType: String) async throws -> ImageUploadResponse {
        var components = URLComponents(url: baseURL.appendingPathComponent("api/images"), resolvingAgainstBaseURL: false)!
        components.queryItems = [URLQueryItem(name: "workspace_id", value: workspaceID)]
        let url = components.url!
        var request = URLRequest(url: url)
        request.httpMethod = "POST"

        let boundary = UUID().uuidString
        request.setValue("multipart/form-data; boundary=\(boundary)", forHTTPHeaderField: "Content-Type")

        var body = Data()

        // File field
        body.append("--\(boundary)\r\n".data(using: .utf8)!)
        body.append("Content-Disposition: form-data; name=\"file\"; filename=\"image.\(fileExtension(for: mimeType))\"\r\n".data(using: .utf8)!)
        body.append("Content-Type: \(mimeType)\r\n\r\n".data(using: .utf8)!)
        body.append(imageData)
        body.append("\r\n".data(using: .utf8)!)
        body.append("--\(boundary)--\r\n".data(using: .utf8)!)

        request.httpBody = body

        let (data, response) = try await URLSession.shared.data(for: request)

        guard let httpResponse = response as? HTTPURLResponse else {
            throw ImageUploadError.invalidResponse
        }

        switch httpResponse.statusCode {
        case 200:
            return try JSONDecoder().decode(ImageUploadResponse.self, from: data)
        case 413:
            throw ImageUploadError.tooLarge
        case 415:
            throw ImageUploadError.unsupportedFormat
        case 429:
            let retryAfter = httpResponse.value(forHTTPHeaderField: "Retry-After") ?? "60"
            throw ImageUploadError.rateLimited(retryAfter: Int(retryAfter) ?? 60)
        case 507:
            throw ImageUploadError.storageFull
        default:
            throw ImageUploadError.serverError(statusCode: httpResponse.statusCode)
        }
    }

    private func fileExtension(for mimeType: String) -> String {
        switch mimeType {
        case "image/jpeg": return "jpg"
        case "image/png": return "png"
        case "image/gif": return "gif"
        case "image/webp": return "webp"
        default: return "jpg"
        }
    }
}

struct ImageUploadResponse: Codable {
    let success: Bool
    let id: String
    let localPath: String
    let mimeType: String
    let size: Int64
    let expiresAt: Int64

    enum CodingKeys: String, CodingKey {
        case success, id
        case localPath = "local_path"
        case mimeType = "mime_type"
        case size
        case expiresAt = "expires_at"
    }
}

enum ImageUploadError: Error {
    case invalidResponse
    case tooLarge
    case unsupportedFormat
    case rateLimited(retryAfter: Int)
    case storageFull
    case serverError(statusCode: Int)
}
```

### Upload with Progress Tracking

```swift
class ImageUploadTask: NSObject, URLSessionTaskDelegate {
    private var progressHandler: ((Double) -> Void)?

    func uploadWithProgress(
        _ imageData: Data,
        to url: URL,
        mimeType: String,
        progress: @escaping (Double) -> Void
    ) async throws -> ImageUploadResponse {
        self.progressHandler = progress

        let request = createMultipartRequest(url: url, imageData: imageData, mimeType: mimeType)

        let session = URLSession(configuration: .default, delegate: self, delegateQueue: nil)
        let (data, response) = try await session.upload(for: request, from: request.httpBody!)

        guard let httpResponse = response as? HTTPURLResponse, httpResponse.statusCode == 200 else {
            throw ImageUploadError.serverError(statusCode: (response as? HTTPURLResponse)?.statusCode ?? 0)
        }

        return try JSONDecoder().decode(ImageUploadResponse.self, from: data)
    }

    func urlSession(_ session: URLSession, task: URLSessionTask, didSendBodyData bytesSent: Int64, totalBytesSent: Int64, totalBytesExpectedToSend: Int64) {
        let progress = Double(totalBytesSent) / Double(totalBytesExpectedToSend)
        progressHandler?(progress)
    }
}
```

---

## Using Images with Claude

After uploading an image, use the `local_path` from the response when sending prompts to Claude:

```swift
// After upload succeeds:
let imagePath = uploadResponse.localPath // ".cdev/images/img_abc123def456.jpg"

// Send to Claude via agent/run
let prompt = "Please analyze this image: \(imagePath)"
// Or use structured prompt with image reference
```

The cdev agent will pass the image path to Claude CLI, which can then analyze the image content.

---

## Rate Limiting

- **Limit**: 10 uploads per minute per IP address
- **Window**: 1 minute sliding window
- **Response**: HTTP 429 with `Retry-After` header

**Best Practices:**
1. Check `/api/images/stats` before uploading to verify storage availability
2. Implement exponential backoff when rate limited
3. Show upload progress to users for large images
4. Handle all error responses gracefully

---

## Image Lifecycle

1. **Upload**: Image stored in `{workspace}/.cdev/images/` with unique ID
2. **Deduplication**: Identical images (by SHA256 hash) return existing entry
3. **TTL**: Images expire after 1 hour (auto-cleaned every 5 minutes)
4. **LRU Eviction**: When storage is full, oldest images are evicted

---

## Security Notes

1. **Magic Bytes Validation**: Server validates file content matches declared MIME type
2. **Path Traversal Protection**: Image paths are validated to stay within `.cdev/images/`
3. **No Symlinks**: Symbolic links are rejected
4. **Rate Limiting**: Prevents abuse (10 uploads/minute/IP)

For full security analysis, see [IMAGE-UPLOAD-SECURITY-ANALYSIS.md](../security/IMAGE-UPLOAD-SECURITY-ANALYSIS.md)

---

## cdev-ios Implementation Requirements

This section outlines what the cdev-ios team needs to implement for image upload support.

### Files/Classes to Create

| File | Purpose |
|------|---------|
| `ImageUploadService.swift` | Core upload/management service |
| `ImageUploadResponse.swift` | Codable response models |
| `ImagePickerView.swift` | UI for selecting images (PHPicker or UIImagePickerController) |
| `ImageAttachmentView.swift` | UI for showing attached images in prompt composer |
| `ImageGalleryView.swift` | UI for viewing/managing uploaded images |

### API Integration Notes

**Important:** All image operations use **HTTP REST API**, not JSON-RPC WebSocket.

```swift
// Base URL from connection settings
let baseURL = "http://\(host):\(port)"

// Endpoints to implement (all require workspace_id parameter):
POST   /api/images?workspace_id=xxx           // Upload (multipart/form-data)
GET    /api/images?workspace_id=xxx           // List all images
GET    /api/images?workspace_id=xxx&id=yyy    // Get single image info
DELETE /api/images?workspace_id=xxx&id=yyy    // Delete single image
GET    /api/images/stats?workspace_id=xxx     // Get storage statistics
DELETE /api/images/all?workspace_id=xxx       // Clear all images
GET    /api/images/validate?workspace_id=xxx  // Validate image path (optional)
```

### Response Models

```swift
// MARK: - Upload Response
struct ImageUploadResponse: Codable {
    let success: Bool
    let id: String
    let localPath: String
    let mimeType: String
    let size: Int64
    let expiresAt: Int64

    enum CodingKeys: String, CodingKey {
        case success, id, size
        case localPath = "local_path"
        case mimeType = "mime_type"
        case expiresAt = "expires_at"
    }
}

// MARK: - Image Info (for list/get)
struct ImageInfoResponse: Codable {
    let id: String
    let localPath: String
    let mimeType: String
    let size: Int64
    let createdAt: Int64
    let expiresAt: Int64

    enum CodingKeys: String, CodingKey {
        case id, size
        case localPath = "local_path"
        case mimeType = "mime_type"
        case createdAt = "created_at"
        case expiresAt = "expires_at"
    }
}

// MARK: - List Response
struct ImageListResponse: Codable {
    let images: [ImageInfoResponse]
    let count: Int
    let totalSizeBytes: Int64
    let maxImages: Int
    let maxTotalSizeMB: Int

    enum CodingKeys: String, CodingKey {
        case images, count
        case totalSizeBytes = "total_size_bytes"
        case maxImages = "max_images"
        case maxTotalSizeMB = "max_total_size_mb"
    }
}

// MARK: - Stats Response
struct ImageStatsResponse: Codable {
    let imageCount: Int
    let totalSizeBytes: Int64
    let maxImages: Int
    let maxTotalSizeMB: Int
    let maxSingleSizeMB: Int
    let canAcceptUpload: Bool
    let reason: String?

    enum CodingKeys: String, CodingKey {
        case reason
        case imageCount = "image_count"
        case totalSizeBytes = "total_size_bytes"
        case maxImages = "max_images"
        case maxTotalSizeMB = "max_total_size_mb"
        case maxSingleSizeMB = "max_single_size_mb"
        case canAcceptUpload = "can_accept_upload"
    }
}

// MARK: - Error Response
struct ImageErrorResponse: Codable {
    let error: String
    let message: String?
}
```

### Error Handling Matrix

| HTTP Code | Error Key | User Message | Action |
|-----------|-----------|--------------|--------|
| 400 | `missing_workspace_id` | "Workspace ID required" | Ensure workspace_id param is included |
| 400 | `invalid_workspace` | "Workspace not found" | Verify workspace is registered |
| 429 | `rate_limit_exceeded` | "Too many uploads. Please wait." | Show countdown timer using `Retry-After` header |
| 413 | `image_too_large` | "Image exceeds 10MB limit" | Suggest compressing or choosing smaller image |
| 415 | `unsupported_type` | "Unsupported format" | Show supported formats: JPEG, PNG, GIF, WebP |
| 507 | `storage_full` | "Storage full" | Offer to clear old images |
| 503 | `service_unavailable` | "Image service unavailable" | Check cdev server is running |

### User Flow Implementation

```
┌─────────────────────────────────────────────────────────────┐
│                    Image Attachment Flow                     │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  1. User taps "📎 Attach" button in prompt composer         │
│                        ↓                                    │
│  2. Show PHPickerViewController (iOS 14+)                   │
│     - Filter: images only                                   │
│     - Selection: single or multiple                         │
│                        ↓                                    │
│  3. User selects image(s)                                   │
│                        ↓                                    │
│  4. Call GET /api/images/stats?workspace_id=xxx             │
│     - Check canAcceptUpload == true                         │
│     - If false, show reason to user                         │
│                        ↓                                    │
│  5. Upload via POST /api/images?workspace_id=xxx            │
│     - Show progress indicator                               │
│     - Handle errors gracefully                              │
│                        ↓                                    │
│  6. On success:                                             │
│     - Store localPath from response                         │
│     - Show thumbnail in composer                            │
│     - Enable "remove" button on thumbnail                   │
│                        ↓                                    │
│  7. User types prompt text                                  │
│                        ↓                                    │
│  8. User taps "Send"                                        │
│     - Construct prompt with image path(s)                   │
│     - Send via agent/run or session/start                   │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Prompt Construction

When sending a prompt with images, include the `local_path` in the prompt text:

```swift
// Single image
func constructPrompt(text: String, imagePath: String?) -> String {
    guard let path = imagePath else { return text }
    return "\(text)\n\nImage: \(path)"
}

// Multiple images
func constructPrompt(text: String, imagePaths: [String]) -> String {
    guard !imagePaths.isEmpty else { return text }

    let imageRefs = imagePaths.enumerated()
        .map { "Image \($0.offset + 1): \($0.element)" }
        .joined(separator: "\n")

    return "\(text)\n\n\(imageRefs)"
}

// Example usage:
let prompt = constructPrompt(
    text: "What's in this screenshot?",
    imagePath: ".cdev/images/img_abc123def456.jpg"
)
// Result: "What's in this screenshot?\n\nImage: .cdev/images/img_abc123def456.jpg"
```

### UI Components Checklist

- [ ] **Attachment Button** - Add to prompt composer toolbar
- [ ] **Image Thumbnail** - Show selected image with remove button
- [ ] **Upload Progress** - Progress bar or spinner during upload
- [ ] **Error Alert** - Handle all error cases with clear messages
- [ ] **Image Gallery View** - List uploaded images with delete option
- [ ] **Storage Indicator** - Show used/available storage (optional)
- [ ] **Clear All Button** - In settings or gallery view

### Key Constraints

| Constraint | Value | Notes |
|------------|-------|-------|
| Max single image | 10 MB | Check before upload |
| Max total storage | 100 MB | Check via /api/images/stats |
| Max image count | 50 | Check via /api/images/stats |
| Rate limit | 10/minute | Handle 429 with backoff |
| TTL | 1 hour | Images auto-expire |
| Formats | JPEG, PNG, GIF, WebP | Validate before upload |

### What cdev Server Handles (No iOS Implementation Needed)

- ✅ Per-workspace image storage in `{workspace}/.cdev/images/`
- ✅ Automatic cleanup (auto-expires after 1 hour)
- ✅ Deduplication (same image returns existing entry)
- ✅ Path traversal protection
- ✅ Magic bytes validation (content matches MIME type)
- ✅ LRU eviction when storage is full

### Testing Checklist

- [ ] Upload JPEG, PNG, GIF, WebP successfully with workspace_id
- [ ] Reject PDF, SVG, BMP with proper error
- [ ] Handle 10MB+ file with 413 error
- [ ] Handle rapid uploads with 429 rate limit
- [ ] Handle missing workspace_id with 400 error
- [ ] Handle invalid workspace_id with 400 error
- [ ] Verify image path works in Claude prompt
- [ ] Test multiple images in single prompt
- [ ] Delete single image with workspace_id
- [ ] Clear all images with workspace_id
- [ ] Check storage stats accuracy for specific workspace
- [ ] Test with poor network (timeout handling)
- [ ] Test upload progress updates
- [ ] Verify images are isolated per workspace
