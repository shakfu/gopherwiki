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
