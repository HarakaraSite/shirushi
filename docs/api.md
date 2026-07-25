# Shirushi API Reference

Base URL: `http://localhost:8181` (configurable with `SHIRUSHI_ADDR`)

All API endpoints return JSON (`Content-Type: application/json`). Authentication
is required except for `/api/login` and `/api/logout`. Unauthenticated requests
receive `401 Unauthorized`.

Japanese version: [api.ja.md](api.ja.md)

### Authentication methods

| Method | Use | Configuration |
|--------|-----|---------------|
| Session cookie | Web UI | Log in with `POST /api/login` |
| Bearer token | Clients that cannot use cookies, such as browser extensions | Start with the `SHIRUSHI_API_TOKEN` environment variable set |

A request is accepted when either method is valid.

**Using a Bearer token**

```
Authorization: Bearer <value of SHIRUSHI_API_TOKEN>
```

- Bearer authentication is disabled when `SHIRUSHI_API_TOKEN` is unset; cookie authentication remains available.
- A warning containing `SHIRUSHI_API_TOKEN` is logged at startup when it is unset.
- Generate the token with `openssl rand -hex 32` (a 256-bit value is recommended).
- The `Bearer ` prefix is case-sensitive; `bearer ` does not match.

---

## Authentication API

### Log in

```
POST /api/login
```

**Request**

```json
{
  "password": "yourpassword",
  "rememberMe": false
}
```

`rememberMe` is optional. When `true`, the login is kept for 30 days. The cookie
is removed when the browser closes if it is `false` or omitted, and the
server-side session expires after 24 hours.

**Response** `200 OK`

```json
{ "status": "ok" }
```

The server issues a `session` cookie (`HttpOnly` + `SameSite=Strict`). Its
expiration is 30 days when `rememberMe: true`; otherwise it is a session cookie.
`SHIRUSHI_COOKIE_SECURE=1` also adds the `Secure` attribute.

**Errors**

| Status | Meaning |
|--------|---------|
| `401` | Incorrect password |
| `429` | Five failures from the same IP within 15 minutes (locked for 15 minutes) |

---

### Log out

```
POST /api/logout
```

**Response** `200 OK`

```json
{ "status": "ok" }
```

Deletes the `session` cookie. No request body is required.

---

## Bookmarks

### List bookmarks

```
GET /api/bookmarks
```

**Query parameters**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `q` | string | — | Partial-match search across title, URL, and excerpt. A numeric value performs a date search (below). |
| `tag` | string | — | Filter by tag name. Use `__untagged__` to show only untagged bookmarks. |
| `date_from` | string | — | Start month of the registration date (`YYYY-MM`). |
| `date_to` | string | — | End month of the registration date (`YYYY-MM`, including the final day of that month). |
| `page` | int | `1` | Page number (one-based). |
| `limit` | int | `50` | Items per page (maximum `200`). |

**Date search (`q` parameter)**

| Input example | Result |
|---------------|--------|
| `202507` | Bookmarks registered in July 2025 |
| `2025` | Bookmarks registered in 2025 |

**Response** `200 OK`

```json
{
  "bookmarks": [
    {
      "id": 1,
      "url": "https://example.com",
      "title": "Example",
      "excerpt": "Description",
      "author": "",
      "public": 0,
      "has_content": false,
      "image_url": "https://example.com/og.png",
      "created_at": "2025-07-01T12:00:00Z",
      "modified_at": null,
      "tags": [
        { "id": 3, "name": "go" }
      ]
    }
  ],
  "total": 477
}
```

`total` is the total count after applying filters; use it to calculate the
number of pagination pages. Untagged bookmarks have `"tags": []`, never `null`.

---

### Create a bookmark

```
POST /api/bookmarks
```

**Request**

```json
{
  "url": "https://example.com",
  "title": "Example",
  "excerpt": "Description",
  "author": "",
  "image_url": "",
  "tags": [{ "id": 3 }]
}
```

`tags` is optional. When omitted, the bookmark is created without tags. `url`
must begin with `http` or `https`.

**Response** `201 Created`

Returns the created bookmark (`created_at` and `modified_at` are database values).

```json
{
  "id": 42,
  "url": "https://example.com",
  "title": "Example",
  "excerpt": "Description",
  "author": "",
  "public": 0,
  "has_content": false,
  "image_url": "",
  "created_at": "2025-07-01T12:00:00Z",
  "modified_at": null,
  "tags": [{ "id": 3, "name": "go" }]
}
```

**Errors**

| Status | Meaning |
|--------|---------|
| `400` | Empty `url`, or a scheme other than `http`/`https` |
| `409` | The URL is already registered |

---

### Update a bookmark

```
PUT /api/bookmarks/{id}
```

**Request**

```json
{
  "url": "https://example.com",
  "title": "New title",
  "excerpt": "New description",
  "author": "",
  "image_url": "",
  "tags": [{ "id": 3 }, { "id": 5 }]
}
```

When `tags` is **omitted**, tags are not changed. When `tags` is an **empty
array (`[]`)**, all tags are removed.

**Response** `200 OK`

Returns the updated bookmark (`modified_at` contains the update time).

**Errors**

| Status | Meaning |
|--------|---------|
| `400` | Invalid `url` |
| `404` | The specified ID does not exist |
| `409` | Another bookmark uses the same URL |

---

### Delete one bookmark

```
DELETE /api/bookmarks/{id}
```

**Response** `204 No Content`

**Errors**

| Status | Meaning |
|--------|---------|
| `404` | The specified ID does not exist |

---

### Delete bookmarks in bulk

```
DELETE /api/bookmarks
```

**Request**

```json
{ "ids": [1, 2, 3] }
```

At most 1,000 bookmarks may be deleted at once.

**Response** `200 OK`

```json
{ "deleted": 3 }
```

---

## Tags

### List tags

```
GET /api/tags
```

**Query parameters**

| Parameter | Description |
|-----------|-------------|
| `all=1` | Return all tags, including unused tags. Without it, only tags used by bookmarks are returned. |

**Response** `200 OK`

```json
[
  { "id": 1, "name": "go" },
  { "id": 2, "name": "tech" }
]
```

Returns `[]`, never `null`, when there are no tags.

---

### Create a tag

```
POST /api/tags
```

**Request**

```json
{ "name": "newtagname" }
```

When a tag with the same name already exists, the existing tag is returned
instead of creating a new one; this is not an error. Both new and existing tags
produce **`201 Created`**. Clients must determine success using `res.ok`
(200–299), not `status === 201`.

**Response** `201 Created`

```json
{ "id": 5, "name": "newtagname" }
```

---

### Update a tag (rename)

```
PUT /api/tags/{id}
```

**Request**

```json
{ "name": "renamed" }
```

**Response** `200 OK`

```json
{ "id": 5, "name": "renamed" }
```

**Errors**

| Status | Meaning |
|--------|---------|
| `404` | The specified ID does not exist |

---

### Delete a tag

```
DELETE /api/tags/{id}
```

Related `bookmark_tags` records are removed automatically through CASCADE.

**Response** `204 No Content`

**Errors**

| Status | Meaning |
|--------|---------|
| `404` | The specified ID does not exist |

---

## Bookmark-tag associations

### Add one tag

```
POST /api/bookmarks/{id}/tags
```

**Request**

```json
{ "tag_id": 3 }
```

**Response** `204 No Content`

Returns `204` even when the association already exists; duplicate additions are ignored.

**Errors**

| Status | Meaning |
|--------|---------|
| `404` | The bookmark or tag does not exist |

---

### Remove one tag

```
DELETE /api/bookmarks/{id}/tags
```

**Request**

```json
{ "tag_id": 3 }
```

**Response** `204 No Content`

---

### Add tags in bulk

```
POST /api/bookmarks/bulk/tags
```

Adds multiple tags to multiple bookmarks in one request.

**Request**

```json
{
  "bookmark_ids": [1, 2, 3],
  "tag_ids": [10, 11]
}
```

- `bookmark_ids` and `tag_ids` may each contain at most 1,000 items.
- The number of `bookmark_ids × tag_ids` combinations may not exceed 5,000.
- Associations that already exist are skipped.

**Response** `204 No Content`

**Errors**

| Status | Meaning |
|--------|---------|
| `404` | Some specified bookmark ID or tag ID does not exist |

---

### Remove tags in bulk

```
DELETE /api/bookmarks/bulk/tags
```

**Request**

```json
{
  "bookmark_ids": [1, 2, 3],
  "tag_ids": [10, 11]
}
```

**Response** `204 No Content`

---

## Fetch metadata

```
POST /api/fetch-metadata
```

Fetches and returns OGP metadata for the specified URL. It does not save a
bookmark.

**Request**

```json
{ "url": "https://example.com" }
```

**Response** `200 OK`

```json
{
  "title": "Example Domain",
  "excerpt": "Page description",
  "author": "",
  "image_url": "https://example.com/og.png"
}
```

| Field | Source |
|-------|--------|
| `title` | Falls back from `og:title` to `<title>` |
| `excerpt` | Falls back from `og:description` to `meta[name=description]` |
| `author` | Falls back from `og:author` to `meta[name=author]` |
| `image_url` | `og:image` |

**Errors**

| Status | Meaning |
|--------|---------|
| `400` | Empty URL, or a scheme other than `http`/`https` |
| `502` | Could not access the external URL (timeout, SSRF block, non-HTML response, etc.) |

---

## Import and export

### Export

```
GET /api/export
```

Returns all bookmarks in Netscape Bookmark format (the format exported by
Chrome and Firefox).

**Response** `200 OK`

A file download named `shirushi-bookmarks.html` with `Content-Type: text/html`.

---

### Import

```
POST /api/import
```

Uploads and registers a Netscape Bookmark HTML file.

**Request**

Attach the HTML file in the `file` field as `multipart/form-data` (maximum 10 MB).

```bash
curl -b 'session=<token>' \
     -F 'file=@bookmarks.html' \
     http://localhost:8181/api/import
```

**Response** `200 OK`

```json
{ "imported": 42, "skipped": 5 }
```

`skipped` is the number of duplicate URLs skipped. After import, thumbnails
(OGP images) for newly registered bookmarks are fetched asynchronously in the
background, after the response has been returned.

---

## Common behavior

### Request-size limits

| API | Limit |
|-----|-------|
| JSON APIs (login, bookmarks, tags, etc.) | 1 MB |
| Import (`/api/import`) | 10 MB |

### Common errors

| Status | Meaning |
|--------|---------|
| `400 Bad Request` | Invalid request format, missing required field, or validation failure |
| `401 Unauthorized` | Missing or expired session cookie, or invalid Bearer token |
| `404 Not Found` | The specified ID does not exist |
| `409 Conflict` | Duplicate URL |
| `429 Too Many Requests` | Lockout caused by failed logins |
| `500 Internal Server Error` | Internal server error |
| `502 Bad Gateway` | Could not access the external URL (metadata fetching only) |
