// Package rendercache is the durable store for computational-page render
// output. It is a separate SQLite database from the primary application DB so
// that its churn (large blobs, writes, evictions) never fragments or bloats the
// content/users/issues store. See docs/computational-pages.md, Section 5.1.
//
// Entries are content-addressed: the key is a hash of the page source, the
// resolved render engine, and an environment fingerprint, so a change to any of
// those yields a fresh key and an old revision remains servable while cached.
// Reader page views serve the cached HTML blob; a miss yields the render-pending
// placeholder rather than triggering execution.
package rendercache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// schema creates the render cache table and its supporting indexes.
const schema = `
CREATE TABLE IF NOT EXISTS render_cache (
    key             TEXT PRIMARY KEY,
    pagepath        TEXT NOT NULL,
    source_revision TEXT NOT NULL DEFAULT '',
    engine          TEXT NOT NULL DEFAULT '',
    html            BLOB NOT NULL,
    size            INTEGER NOT NULL,
    -- Timestamps are epoch nanoseconds (INTEGER) rather than TIMESTAMP so that
    -- ORDER BY / range comparisons for LRU eviction are exact and not subject to
    -- the driver's variable-width time-string formatting.
    created_at      INTEGER NOT NULL,
    last_access     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_render_cache_last_access ON render_cache(last_access);
CREATE INDEX IF NOT EXISTS idx_render_cache_pagepath ON render_cache(pagepath);
`

// Entry is a cached render of one computational page.
type Entry struct {
	// Key is the content-addressed cache key (see Key).
	Key string
	// Pagepath is the page the render belongs to, retained for invalidation and
	// inspection.
	Pagepath string
	// SourceRevision is the git revision the rendered source came from.
	SourceRevision string
	// Engine is the resolved render engine ("knitr", "jupyter", or "").
	Engine string
	// HTML is the self-contained rendered output served to readers.
	HTML []byte
	// Size is the byte length of HTML, used for size-based eviction accounting.
	Size int64
	// CreatedAt is when the entry was first written.
	CreatedAt time.Time
	// LastAccess is when the entry was last read; drives LRU eviction.
	LastAccess time.Time
}

// Cache is the render-output store backed by a dedicated SQLite database.
type Cache struct {
	conn *sql.DB
}

// DefaultPath returns the render cache database path given the primary database
// path. The cache lives beside the primary DB as a sibling file so it is easy to
// locate and to delete wholesale. For in-memory or empty primary paths it
// returns an in-memory path.
func DefaultPath(primaryDBPath string) string {
	if primaryDBPath == "" || primaryDBPath == ":memory:" {
		return ":memory:"
	}
	dir := filepath.Dir(primaryDBPath)
	return filepath.Join(dir, "render-cache.sqlite")
}

// Open opens (creating if necessary) the render cache database at path. Use
// ":memory:" for an ephemeral cache (e.g. in tests). The schema is applied on
// open.
func Open(path string) (*Cache, error) {
	if path == "" {
		path = ":memory:"
	}

	connStr := path
	if path != ":memory:" {
		connStr = path + "?_journal_mode=WAL&_busy_timeout=5000"
	}

	conn, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("open render cache: %w", err)
	}
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("ping render cache: %w", err)
	}
	if _, err := conn.Exec(schema); err != nil {
		conn.Close()
		return nil, fmt.Errorf("init render cache schema: %w", err)
	}

	return &Cache{conn: conn}, nil
}

// Close closes the underlying database.
func (c *Cache) Close() error {
	return c.conn.Close()
}

// Key computes the content-addressed cache key from the page source, the
// resolved render engine, and an environment fingerprint. Any change to those
// inputs produces a different key. The fingerprint lets callers invalidate the
// cache when the compute environment (package/toolchain versions) changes.
func Key(source, engine, envFingerprint string) string {
	h := sha256.New()
	// Length-prefix each field so distinct field boundaries cannot collide
	// (e.g. ("ab","c") vs ("a","bc")).
	for _, field := range []string{source, engine, envFingerprint} {
		fmt.Fprintf(h, "%d:", len(field))
		h.Write([]byte(field))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Put writes an entry, replacing any existing row with the same key. Size is
// derived from HTML when zero; timestamps default to now when zero.
func (c *Cache) Put(ctx context.Context, e Entry) error {
	if e.Key == "" {
		return fmt.Errorf("render cache: empty key")
	}
	size := e.Size
	if size == 0 {
		size = int64(len(e.HTML))
	}
	now := time.Now()
	created := e.CreatedAt
	if created.IsZero() {
		created = now
	}
	lastAccess := e.LastAccess
	if lastAccess.IsZero() {
		lastAccess = now
	}

	_, err := c.conn.ExecContext(ctx,
		`INSERT INTO render_cache
			(key, pagepath, source_revision, engine, html, size, created_at, last_access)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
			pagepath=excluded.pagepath,
			source_revision=excluded.source_revision,
			engine=excluded.engine,
			html=excluded.html,
			size=excluded.size,
			created_at=excluded.created_at,
			last_access=excluded.last_access`,
		e.Key, e.Pagepath, e.SourceRevision, e.Engine, e.HTML, size, created.UnixNano(), lastAccess.UnixNano())
	if err != nil {
		return fmt.Errorf("render cache put: %w", err)
	}
	return nil
}

// Get returns the entry for key. On a hit it refreshes last_access to now so
// eviction favors genuinely cold entries. The bool reports whether a row was
// found.
func (c *Cache) Get(ctx context.Context, key string) (Entry, bool, error) {
	var e Entry
	var createdNanos, lastAccessNanos int64
	err := c.conn.QueryRowContext(ctx,
		`SELECT key, pagepath, source_revision, engine, html, size, created_at, last_access
		 FROM render_cache WHERE key = ?`, key).
		Scan(&e.Key, &e.Pagepath, &e.SourceRevision, &e.Engine, &e.HTML, &e.Size, &createdNanos, &lastAccessNanos)
	if err == sql.ErrNoRows {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, fmt.Errorf("render cache get: %w", err)
	}
	e.CreatedAt = time.Unix(0, createdNanos)

	now := time.Now()
	if _, err := c.conn.ExecContext(ctx,
		`UPDATE render_cache SET last_access = ? WHERE key = ?`, now.UnixNano(), key); err != nil {
		return Entry{}, false, fmt.Errorf("render cache touch: %w", err)
	}
	e.LastAccess = now
	return e, true, nil
}

// DeleteByPage removes all cached renders for a page path. Used to invalidate a
// page's output when its source is edited or the page is deleted.
func (c *Cache) DeleteByPage(ctx context.Context, pagepath string) (int64, error) {
	res, err := c.conn.ExecContext(ctx,
		`DELETE FROM render_cache WHERE pagepath = ?`, pagepath)
	if err != nil {
		return 0, fmt.Errorf("render cache delete by page: %w", err)
	}
	return res.RowsAffected()
}

// Count returns the number of cached entries.
func (c *Cache) Count(ctx context.Context) (int64, error) {
	var n int64
	err := c.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM render_cache`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("render cache count: %w", err)
	}
	return n, nil
}

// TotalSize returns the sum of cached blob sizes in bytes.
func (c *Cache) TotalSize(ctx context.Context) (int64, error) {
	var total sql.NullInt64
	err := c.conn.QueryRowContext(ctx, `SELECT SUM(size) FROM render_cache`).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("render cache total size: %w", err)
	}
	return total.Int64, nil
}

// EvictBySize deletes least-recently-accessed entries until the total cached
// size is at most maxBytes. It returns the number of entries removed. A
// non-positive maxBytes clears the cache.
func (c *Cache) EvictBySize(ctx context.Context, maxBytes int64) (int64, error) {
	total, err := c.TotalSize(ctx)
	if err != nil {
		return 0, err
	}
	if total <= maxBytes {
		return 0, nil
	}

	// Delete oldest-first until within budget. Doing this in one statement keeps
	// the accounting consistent: order by last_access ascending, subtract sizes
	// via a running total, and remove rows while the retained total would still
	// exceed the budget.
	res, err := c.conn.ExecContext(ctx,
		`DELETE FROM render_cache WHERE key IN (
			SELECT key FROM (
				SELECT key,
				       SUM(size) OVER (ORDER BY last_access DESC, key
				                       ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running
				FROM render_cache
			)
			WHERE running > ?
		)`, maxBytes)
	if err != nil {
		return 0, fmt.Errorf("render cache evict by size: %w", err)
	}
	return res.RowsAffected()
}

// EvictByAge deletes entries whose last_access is strictly older than the cutoff.
// It returns the number of entries removed.
func (c *Cache) EvictByAge(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := c.conn.ExecContext(ctx,
		`DELETE FROM render_cache WHERE last_access < ?`, olderThan.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("render cache evict by age: %w", err)
	}
	return res.RowsAffected()
}

// Clear removes every cached entry.
func (c *Cache) Clear(ctx context.Context) error {
	if _, err := c.conn.ExecContext(ctx, `DELETE FROM render_cache`); err != nil {
		return fmt.Errorf("render cache clear: %w", err)
	}
	return nil
}

// setLastAccess overrides an entry's last_access timestamp. It exists to make
// eviction behavior deterministic in tests; it is not part of the public API.
func (c *Cache) setLastAccess(ctx context.Context, key string, t time.Time) error {
	_, err := c.conn.ExecContext(ctx,
		`UPDATE render_cache SET last_access = ? WHERE key = ?`, t.UnixNano(), key)
	return err
}
