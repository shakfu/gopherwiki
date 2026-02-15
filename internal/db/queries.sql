-- name: GetPreference :one
SELECT name, value FROM preferences WHERE name = ? LIMIT 1;

-- name: ListPreferences :many
SELECT name, value FROM preferences ORDER BY name;

-- name: UpsertPreference :exec
INSERT INTO preferences (name, value) VALUES (?, ?)
ON CONFLICT(name) DO UPDATE SET value = excluded.value;

-- name: DeletePreference :exec
DELETE FROM preferences WHERE name = ?;

-- User queries

-- name: GetUserByID :one
SELECT * FROM user WHERE id = ? LIMIT 1;

-- name: GetUserByEmail :one
SELECT * FROM user WHERE email = ? LIMIT 1;

-- name: ListUsers :many
SELECT * FROM user ORDER BY name;

-- name: CreateUser :one
INSERT INTO user (
    name, email, password_hash, first_seen, last_seen,
    is_approved, is_admin, email_confirmed, allow_read, allow_write, allow_upload
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateUser :exec
UPDATE user SET
    name = ?,
    email = ?,
    password_hash = ?,
    last_seen = ?,
    is_approved = ?,
    is_admin = ?,
    email_confirmed = ?,
    allow_read = ?,
    allow_write = ?,
    allow_upload = ?
WHERE id = ?;

-- name: UpdateUserLastSeen :exec
UPDATE user SET last_seen = ? WHERE id = ?;

-- name: DeleteUser :exec
DELETE FROM user WHERE id = ?;

-- name: CountUsers :one
SELECT COUNT(*) FROM user;

-- name: CountAdmins :one
SELECT COUNT(*) FROM user WHERE is_admin = TRUE;

-- Drafts queries

-- name: GetDraft :one
SELECT * FROM drafts WHERE pagepath = ? AND author_email = ? LIMIT 1;

-- name: GetDraftByID :one
SELECT * FROM drafts WHERE id = ? LIMIT 1;

-- name: ListDraftsByPagepath :many
SELECT * FROM drafts WHERE pagepath = ? ORDER BY datetime DESC;

-- name: CreateDraft :one
INSERT INTO drafts (
    pagepath, revision, author_email, content, cursor_line, cursor_ch, datetime
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateDraft :exec
UPDATE drafts SET
    revision = ?,
    content = ?,
    cursor_line = ?,
    cursor_ch = ?,
    datetime = ?
WHERE id = ?;

-- name: UpsertDraft :exec
INSERT INTO drafts (pagepath, revision, author_email, content, cursor_line, cursor_ch, datetime)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO UPDATE SET
    revision = excluded.revision,
    content = excluded.content,
    cursor_line = excluded.cursor_line,
    cursor_ch = excluded.cursor_ch,
    datetime = excluded.datetime
WHERE pagepath = excluded.pagepath AND author_email = excluded.author_email;

-- name: DeleteDraft :exec
DELETE FROM drafts WHERE pagepath = ? AND author_email = ?;

-- name: DeleteDraftByID :exec
DELETE FROM drafts WHERE id = ?;

-- name: DeleteExpiredAnonymousDrafts :exec
DELETE FROM drafts WHERE author_email LIKE 'anonymous_uid:%' AND datetime < ?;

-- Cache queries

-- name: GetCache :one
SELECT key, value, datetime FROM cache WHERE key = ? LIMIT 1;

-- name: SetCache :exec
INSERT INTO cache (key, value, datetime) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, datetime = excluded.datetime;

-- name: DeleteCache :exec
DELETE FROM cache WHERE key = ?;

-- name: ClearExpiredCache :exec
DELETE FROM cache WHERE datetime < ?;

-- Issue queries

-- name: GetIssue :one
SELECT * FROM issues WHERE id = ?;

-- name: ListIssues :many
SELECT * FROM issues ORDER BY category, created_at DESC;

-- name: ListIssuesByStatus :many
SELECT * FROM issues WHERE status = ? ORDER BY category, created_at DESC;

-- name: ListIssuesByCategory :many
SELECT * FROM issues WHERE category = ? ORDER BY created_at DESC;

-- name: ListIssuesByCategoryAndStatus :many
SELECT * FROM issues WHERE category = ? AND status = ? ORDER BY created_at DESC;

-- name: CreateIssue :one
INSERT INTO issues (title, description, status, category, tags, created_by_name, created_by_email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING *;

-- name: UpdateIssue :exec
UPDATE issues SET title = ?, description = ?, status = ?, category = ?, tags = ?, updated_at = ? WHERE id = ?;

-- name: DeleteIssue :exec
DELETE FROM issues WHERE id = ?;

-- name: CountIssuesByStatus :one
SELECT COUNT(*) FROM issues WHERE status = ?;

-- name: ListDistinctCategories :many
SELECT DISTINCT category FROM issues WHERE category IS NOT NULL AND category != '' ORDER BY category;

-- Issue Comment queries

-- name: CreateIssueComment :one
INSERT INTO issue_comments (issue_id, content, author_name, author_email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?) RETURNING *;

-- name: ListIssueComments :many
SELECT * FROM issue_comments WHERE issue_id = ? ORDER BY created_at ASC;

-- name: GetIssueComment :one
SELECT * FROM issue_comments WHERE id = ?;

-- name: DeleteIssueComment :exec
DELETE FROM issue_comments WHERE id = ?;

-- name: DeleteIssueComments :exec
DELETE FROM issue_comments WHERE issue_id = ?;

-- name: CountIssueComments :one
SELECT COUNT(*) FROM issue_comments WHERE issue_id = ?;
