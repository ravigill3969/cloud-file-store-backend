````markdown
## CloudAPI – Developer Documentation

CloudAPI provides a **secure** and **scalable** REST interface for uploading, retrieving, and editing cloud-based image resources.

---

## Features

- **Secure API-key authentication**
- **Multi-format support**: `jpg`, `jpeg`, `png`, `gif`
- **Cloud-native design**

---

## Authentication

Sign up to receive your unique keys:

- **publicKey**
- **secretKey**

Both keys are **required** for every upload and edit request.

---

## Upload Image

**`POST`** `/api/file/upload/{publicKey}/secure/{secretKey}`

Upload a single image with `multipart/form-data`.

### Supported MIME Types

- `image/jpeg`
- `image/png`
- `image/gif`

### Request

- **Form field:** `file`
- **Max size:** `5 MB`

### Success Response

```json
{
  "status": "success",
  "message": "OK",
  "data": {
    "url": ""
  },
  "code": "OK",
  "timestamp": "2026-09-16T20:42:26.799009118Z"
}
```
````

### Error Responses

| Code  | Message                               | Cause                  |
| ----- | ------------------------------------- | ---------------------- |
| `400` | Could not parse multipart form        | Bad request format     |
| `400` | File not provided                     | Missing form field     |
| `400` | Image size exceeds 5MB limit          | File too large         |
| `400` | Filename missing in upload            | Form missing filename  |
| `400` | Invalid path                          | URL formatting issue   |
| `401` | Invalid public or secret key          | Auth key mismatch      |
| `401` | Post req limit reached for this month | Monthly quota exceeded |
| `415` | Unsupported media type                | Invalid file MIME type |
| `500` | Error reading file                    | File read failed       |
| `500` | Failed to generate presigned URL      | S3 config error        |
| `500` | Failed to upload file to S3           | Network/S3 issue       |
| `500` | Unable to save data                   | Database insert failed |

```json
<!--error example for file upload -->
{
  "status": "error",
  "message": "Image size exceeds 5MB limit",
  "code": "BAD_REQUEST",
  "timestamp": "2026-09-16T20:44:45.854425208Z"
}
```

---

## Retrieve Image

**`GET`** `/api/file/get-file/{id}`

Fetch a previously uploaded image.

### Success

Returns the image data directly.

### Error Responses

| Code  | Message               | Cause                 |
| ----- | --------------------- | --------------------- |
| `400` | Invalid URL structure | Improper route format |
| `400` | Invalid id            | ID missing or invalid |
| `404` | File not found        | ID not found in DB    |

---

## Edit Image

**`POST`** `/api/file/edit/{id}/{publicKey}/secure/{secretKey}`

Resize an uploaded image.

### Query Parameters

`?width=int&height=int`

| Param    | Description           |
| -------- | --------------------- |
| `width`  | New width (required)  |
| `height` | New height (required) |

### Success Response

```json
{
  "status": "success",
  "message": "code in integer",
  "data": {
    "url": "url"
  },
  "code": "code"
}
```

### 🔴 Error Responses

| Code  | Message                       | Cause                  |
| ----- | ----------------------------- | ---------------------- |
| `400` | Invalid URL                   | Malformed edit route   |
| `400` | Width and height are required | Missing query params   |
| `404` | Image not found               | ID doesn’t exist in DB |
| `403` | Insufficient quota            | Edit quota exceeded    |
| `500` | Image resize failed           | Resize process failed  |
| `500` | Failed to insert image        | Database save failed   |
| `500` | Server error                  | Unknown backend error  |

```json
{
  "status": "error",
  "message": "Image resize failed",
  "code": "INTERNAL_ERROR",
  "timestamp": "2026-09-16T21:05:33.933326843Z"
}
```

## DELETE IMAGE

** `DELETE` ** /api/file/delete/{id}/{publicKey}/secure/{secretKey}

---

## Video

### Upload Video

**`POST`** `/api/video/upload/{publicKey}/secure/{secretKey}`

Upload **one video at a time** with `multipart/form-data`.

- **Form field (filename):** `video`
- **Max size:** `50 MB`
- **One file at a time** – only the first file is uploaded

#### Supported MIME Types

- `video/mp4`
- `video/webm`
- `video/ogg`
- `video/quicktime`
- `video/x-msvideo`
- `video/x-ms-wmv`
- `video/mpeg`
- `video/3gpp`
- `video/3gpp2`
- `video/x-flv`
- `application/vnd.rn-realmedia`
- `video/x-matroska`

#### Success Response

```json
{
  "success": "http://localhost:8080/api/video/watch?vid=<id>",
  "error": ""
}
```

The `success` value is the stream URL, pass it to the watch endpoint below.

#### Error Responses

| Code  | Message                               | Cause                      |
| ----- | ------------------------------------- | -------------------------- |
| `400` | Invalid URL format                    | Malformed upload route     |
| `401` | Invalid keys                          | Wrong publicKey/secretKey  |
| `401` | Post req limit reached for this month | Monthly quota exceeded     |
| `400` | Could not parse multipart form        | Bad request format         |
| `400` | No video file provided                | `video` form field missing |
| `400` | filename is required                  | File has no name           |
| `400` | file size limit is 50MB               | File too large             |
| `400` | unsupported video format: <type>      | Invalid file MIME type     |
| `400` | failed to upload to S3: ...           | S3 upload failed           |
| `400` | failed to persist media metadata      | Database insert failed     |
| `500` | Failed to encode response: ...        | Could not encode the reply |

---

### Watch Video

**`GET`** `/api/video/watch/?vid={vid}`

Streams **one video** by id. `vid` is the id returned by the upload endpoint.

Returns the video bytes directly (not JSON), and supports `Range` requests for seeking.

#### Success Response

`200 OK` (or `206 Partial Content` when a `Range` header is sent)

- **Headers:** `Content-Type`, `Content-Length`, `Accept-Ranges: bytes`

#### Error Responses

| Code  | Message              | Cause                     |
| ----- | -------------------- | ------------------------- |
| `400` | Invalid Id           | `vid` query param is empty |
| `404` | Video not found      | `vid` not found in DB     |
| `500` | Error fetching video | S3 fetch failed           |

---

### Delete Video

**`DELETE`** `/api/video/delete/{publicKey}/secure/{secretKey}/{vid}`

Deletes **one video at a time** by id.

#### Success Response

```json
{
  "status": "success",
  "message": "OK",
  "data": "video deleted successfully",
  "code": "OK",
  "timestamp": "2026-09-16T20:42:26.799009118Z"
}
```

#### Error Responses

| Code  | Message                             | Cause                         |
| ----- | ----------------------------------- | ----------------------------- |
| `400` | Invalid URL format                  | Malformed delete route        |
| `401` | Invalid keys                        | Wrong publicKey/secretKey     |
| `500` | failed to delete video from DB: ... | `vid` not found, or not yours |
| `500` | unable to delete video from Cloud   | S3 delete failed              |

---

## 🛠️ Contact & Support

- **Support Portal** – [Visit](https://cloudapi.dev/support)
- **Email** – support@cloudapi.dev

---

> Built for developers. Powered by the cloud.

```

```
