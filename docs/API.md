# GopherWiki API v1

All endpoints are under `/-/api/v1/`. Responses use a JSON envelope:

```json
{"data": ...}   // on success
{"error": "..."}  // on failure
```

Authentication uses the same session cookies as the web UI, or an API token. API requests that fail authentication receive JSON 401/403 responses instead of HTML redirects.

## API tokens

An admin creates and revokes tokens at `/-/admin/tokens`. The token is shown once. Send it as a bearer credential:

```text
Authorization: Bearer gw_...
```

A token request acts as the token's user and needs no CSRF token. It is limited in four ways:

- It may create and update pages only at or below the token's write prefix. Other paths return `403`.
- Updating an existing page requires `revision`. Without it the response is `428 Precondition Required`. Use `metadata.revision` from the page's `GET` response.
- It may not delete pages.
- It never has admin rights, and it is accepted only under `/-/api/`.

An unknown or revoked token returns `401`.

A page written with a token becomes an agent page. It needs validation by a reviewer, and with `HIDE_UNVALIDATED=true` it answers `404` to other users until its current revision is validated. Tokens always see agent pages. See `docs/dev/llm-wiki.md`.

---

## Pages

### List all pages

```text
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

```text
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

**Cited report runs.** A page can cite report runs in its frontmatter:

```yaml
sources:
  - page: reports/sales
    run: 12
```

The response then has a `sources` list with each run's state: `period` is empty when the run does not exist, and `newer_run` is set when a later run of the same period exists. Entries of other shapes are ignored.

```json
"sources": [{"page": "reports/sales", "run": 12, "period": "2026-08", "newer_run": 15}]
```

### Create or update a page

```text
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

A path ending in `.qmd`, such as `reports/august.qmd`, creates a computational page. Later requests may omit the suffix. The suffix is ignored when a page already exists at that path.

### Save several pages in one commit

```text
POST /-/api/v1/batch
```

```json
{
  "message": "August run",
  "pages": [
    {"path": "kb/august", "content": "# August\n..."},
    {"path": "kb/index", "content": "...", "revision": "a1b2c3"}
  ]
}
```

Saves up to 100 pages as one commit, so one revert undoes them all. Each page follows the rules of a single save, including the token rules. Every page is checked before anything is written; if one fails, nothing is saved.

**Responses**

- `200 OK` -- `{"changed": true, "pages": [...]}`; `changed` is false and no commit is made when no content changed

- `400 Bad Request` -- empty batch, more than 100 pages, a missing path, or the same page twice

- `409 Conflict` -- a page was modified since its `revision`; the error names the page

### Delete a page

```text
DELETE /-/api/v1/pages/{path}
```

**Response** `200 OK`

```json
{"data": {"deleted": true}}
```

### Get page history

```text
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

```text
GET /-/api/v1/pages/{path}/backlinks
```

Returns pages that link to the given page via `[[wikilinks]]`.

**Response** `200 OK`

```json
{"data": ["guides/Setup", "FAQ"]}
```

---

### List report runs

```text
GET /-/api/v1/pages/{path}/runs
```

Returns the page's report runs, newest first. See `docs/computational-pages.md` section 5.1.1.

**Response** `200 OK`

```json
{"data": [
  {"id": 12, "period": "2026-08", "source_hash": "9f2c...", "source_revision": "a1b2c3d4...",
   "run_by": "alice@example.com", "run_at": "2026-09-02T08:15:00Z"}
]}
```

### Get a report run

```text
GET /-/api/v1/pages/{path}/runs/{id}
```

Returns one run with `markdown`, the executed page: the source with each cell's output in place. Returns `404` when the page has no run with that ID.

---

## Search

### Search pages

```text
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

## Lint

### Get lint findings

```text
GET /-/api/v1/lint
```

Returns deterministic findings, sorted by page: `broken_link`, `orphan`, and for agent pages `unvalidated`, `no_sources`, `missing_source`, `stale_source` and `unmatched_number`. See `docs/dev/llm-wiki.md`. The same list is shown at `/-/lint`.

**Response** `200 OK`

```json
{"data": [
  {"page": "kb/august", "check": "unmatched_number", "detail": "not found in any cited run: 12.5"}
]}
```

---

## Changelog

### Get recent changes

```text
GET /-/api/v1/changelog
```

Returns the 100 most recent commits across the entire wiki.

**Response** `200 OK` -- array of commit objects (same shape as page history entries).

---

## Issues

### List issues

```text
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

```text
GET /-/api/v1/issues/{id}
```

**Response** `200 OK` -- single issue object.

### Create an issue

```text
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

```text
PUT /-/api/v1/issues/{id}
```

Same request body as create. Status is preserved (use close/reopen endpoints to change status).

**Response** `200 OK` -- the updated issue object.

### Close an issue

```text
POST /-/api/v1/issues/{id}/close
```

**Response** `200 OK` -- the updated issue object with `status: "closed"`.

### Reopen an issue

```text
POST /-/api/v1/issues/{id}/reopen
```

**Response** `200 OK` -- the updated issue object with `status: "open"`.

### Delete an issue (admin only)

```text
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

```text
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

```text
POST /-/api/v1/issues/{id}/comments
```

**Request body**

```json
{"content": "Comment text here."}
```

**Response** `201 Created` -- the created comment object.

### Delete a comment (admin only)

```text
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
