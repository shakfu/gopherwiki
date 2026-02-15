# GopherWiki API v1

All endpoints are under `/-/api/v1/`. Responses use a JSON envelope:

```json
{"data": ...}   // on success
{"error": "..."}  // on failure
```

Authentication uses the same session cookies as the web UI. API requests that fail authentication receive JSON 401/403 responses instead of HTML redirects.

---

## Pages

### List all pages

```
GET /-/api/v1/pages
```

**Response** `200 OK`

```json
{
  "data": [
    {"name": "Welcome", "path": "Welcome"},
    {"name": "Getting Started", "path": "guides/Getting-Started"}
  ]
}
```

### Get a page

```
GET /-/api/v1/pages/{path}
```

| Parameter  | In    | Description                          |
|------------|-------|--------------------------------------|
| `path`     | URL   | Page path (e.g. `guides/Setup`)      |
| `revision` | Query | Optional git revision to retrieve    |

Supports `ETag` / `If-None-Match` for cache validation (returns `304` when unchanged).

**Response** `200 OK`

```json
{
  "data": {
    "path": "Welcome",
    "name": "Welcome",
    "content": "# Welcome\n\nHello world.",
    "revision": "a1b2c3",
    "exists": true,
    "metadata": {
      "revision": "a1b2c3",
      "revision_full": "a1b2c3d4e5f6...",
      "datetime": "2026-01-15T10:30:00Z",
      "author_name": "Alice",
      "author_email": "alice@example.com",
      "message": "Updated Welcome"
    }
  }
}
```

### Create or update a page

```
PUT /-/api/v1/pages/{path}
```

**Request body**

```json
{
  "content": "# Page Title\n\nNew content here.",
  "message": "Optional commit message",
  "revision": "a1b2c3"
}
```

| Field      | Required | Description                                         |
|------------|----------|-----------------------------------------------------|
| `content`  | Yes      | Markdown content                                    |
| `message`  | No       | Git commit message (auto-generated if omitted)      |
| `revision` | No       | Base revision for conflict detection                 |

**Responses**

- `201 Created` -- new page created
- `200 OK` -- existing page updated
- `409 Conflict` -- page was modified since the given `revision`

### Delete a page

```
DELETE /-/api/v1/pages/{path}
```

**Response** `200 OK`

```json
{"data": {"deleted": true}}
```

### Get page history

```
GET /-/api/v1/pages/{path}/history
```

**Response** `200 OK`

```json
{
  "data": [
    {
      "revision": "a1b2c3",
      "revision_full": "a1b2c3d4e5f6...",
      "datetime": "2026-01-15T10:30:00Z",
      "author_name": "Alice",
      "author_email": "alice@example.com",
      "message": "Updated Welcome"
    }
  ]
}
```

### Get page backlinks

```
GET /-/api/v1/pages/{path}/backlinks
```

Returns pages that link to the given page via `[[wikilinks]]`.

**Response** `200 OK`

```json
{"data": ["guides/Setup", "FAQ"]}
```

---

## Search

### Search pages

```
GET /-/api/v1/search?q={query}
```

Uses FTS5 full-text search with fallback to brute-force regex matching.

**Response** `200 OK`

```json
{
  "data": [
    {
      "name": "Welcome",
      "path": "Welcome",
      "snippet": "...matching <mark>text</mark>...",
      "match_count": 1
    }
  ]
}
```

---

## Changelog

### Get recent changes

```
GET /-/api/v1/changelog
```

Returns the 100 most recent commits across the entire wiki.

**Response** `200 OK` -- array of commit objects (same shape as page history entries).

---

## Issues

### List issues

```
GET /-/api/v1/issues
```

| Parameter  | In    | Description                              |
|------------|-------|------------------------------------------|
| `status`   | Query | Filter by `open` or `closed`             |
| `tag`      | Query | Filter by tag name                       |
| `category` | Query | Filter by category name                  |

**Response** `200 OK`

```json
{
  "data": [
    {
      "id": 1,
      "title": "Fix navigation bug",
      "description": "The sidebar breaks on mobile.",
      "status": "open",
      "category": "bug",
      "tags": ["ui", "mobile"],
      "created_by_name": "Alice",
      "created_by_email": "alice@example.com",
      "created_at": "2026-01-10T09:00:00Z",
      "updated_at": "2026-01-12T14:30:00Z"
    }
  ]
}
```

### Get an issue

```
GET /-/api/v1/issues/{id}
```

**Response** `200 OK` -- single issue object.

### Create an issue

```
POST /-/api/v1/issues
```

**Request body**

```json
{
  "title": "New feature request",
  "description": "Markdown description here.",
  "category": "feature",
  "tags": ["enhancement"]
}
```

| Field         | Required | Description           |
|---------------|----------|-----------------------|
| `title`       | Yes      | Issue title           |
| `description` | No       | Markdown description  |
| `category`    | No       | Category name         |
| `tags`        | No       | Array of tag strings  |

**Response** `201 Created` -- the created issue object.

### Update an issue

```
PUT /-/api/v1/issues/{id}
```

Same request body as create. Status is preserved (use close/reopen endpoints to change status).

**Response** `200 OK` -- the updated issue object.

### Close an issue

```
POST /-/api/v1/issues/{id}/close
```

**Response** `200 OK` -- the updated issue object with `status: "closed"`.

### Reopen an issue

```
POST /-/api/v1/issues/{id}/reopen
```

**Response** `200 OK` -- the updated issue object with `status: "open"`.

### Delete an issue (admin only)

```
DELETE /-/api/v1/issues/{id}
```

Cascade-deletes all comments on the issue.

**Response** `200 OK`

```json
{"data": {"deleted": true}}
```

---

## Issue Comments

### List comments

```
GET /-/api/v1/issues/{id}/comments
```

**Response** `200 OK`

```json
{
  "data": [
    {
      "id": 1,
      "issue_id": 42,
      "content": "This is a comment.",
      "author_name": "Bob",
      "author_email": "bob@example.com",
      "created_at": "2026-01-11T11:00:00Z",
      "updated_at": "2026-01-11T11:00:00Z"
    }
  ]
}
```

### Create a comment

```
POST /-/api/v1/issues/{id}/comments
```

**Request body**

```json
{"content": "Comment text here."}
```

**Response** `201 Created` -- the created comment object.

### Delete a comment (admin only)

```
DELETE /-/api/v1/issues/{id}/comments/{commentId}
```

**Response** `200 OK`

```json
{"data": {"deleted": true}}
```

---

## Error responses

All errors return the appropriate HTTP status code with a JSON body:

```json
{"error": "description of what went wrong"}
```

| Status | Meaning                                    |
|--------|--------------------------------------------|
| 400    | Bad request (invalid input)                |
| 401    | Not authenticated                          |
| 403    | Forbidden (insufficient permissions)       |
| 404    | Resource not found                         |
| 409    | Conflict (edit conflict on page save)      |
| 500    | Internal server error                      |
