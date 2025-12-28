# Image Upload Integration Guide for cdev-ios

This guide explains how to integrate image upload functionality from the cdev-ios app to the cdev agent.

## Overview

The cdev agent provides HTTP endpoints for uploading images that can be used with Claude. Images are stored temporarily in `.cdev/images/` with automatic cleanup after 1 hour.

## API Endpoints

### Upload Image

```
POST /api/images
Content-Type: multipart/form-data
```

**Request:**
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
GET /api/images
```

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
GET /api/images?id=abc123def456
```

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
DELETE /api/images?id=abc123def456
```

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
GET /api/images/stats
```

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
DELETE /api/images/all
```

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

    init(host: String, port: Int) {
        self.baseURL = URL(string: "http://\(host):\(port)")!
    }

    func uploadImage(_ imageData: Data, mimeType: String) async throws -> ImageUploadResponse {
        let url = baseURL.appendingPathComponent("api/images")
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

1. **Upload**: Image stored in `.cdev/images/` with unique ID
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
