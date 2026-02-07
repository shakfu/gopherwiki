// Package db provides database access for GopherWiki.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// NullBool converts a bool to sql.NullBool.
func NullBool(b bool) sql.NullBool {
	return sql.NullBool{Bool: b, Valid: true}
}

// NullString converts a string to sql.NullString.
func NullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

// NullInt64 converts an int64 to sql.NullInt64.
func NullInt64(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: true}
}

// NullTime converts a time.Time to sql.NullTime.
func NullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

// Database wraps the SQL database connection and queries.
type Database struct {
	conn    *sql.DB
	Queries *Queries
}

// Schema is the SQL schema for creating tables.
const Schema = `
CREATE TABLE IF NOT EXISTS preferences (
    name TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE IF NOT EXISTS user (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    first_seen TIMESTAMP,
    last_seen TIMESTAMP,
    is_approved BOOLEAN DEFAULT FALSE,
    is_admin BOOLEAN DEFAULT FALSE,
    email_confirmed BOOLEAN DEFAULT FALSE,
    allow_read BOOLEAN DEFAULT FALSE,
    allow_write BOOLEAN DEFAULT FALSE,
    allow_upload BOOLEAN DEFAULT FALSE
);

CREATE TABLE IF NOT EXISTS drafts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pagepath TEXT,
    revision TEXT,
    author_email TEXT,
    content TEXT,
    cursor_line INTEGER,
    cursor_ch INTEGER,
    datetime TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_drafts_pagepath ON drafts(pagepath);

CREATE TABLE IF NOT EXISTS cache (
    key TEXT PRIMARY KEY,
    value TEXT,
    datetime TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cache_key ON cache(key);

CREATE TABLE IF NOT EXISTS issues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    category TEXT,
    tags TEXT,
    created_by_name TEXT,
    created_by_email TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_issues_status ON issues(status);
CREATE INDEX IF NOT EXISTS idx_issues_created_at ON issues(created_at);
`

// Open opens a new database connection.
func Open(uri string) (*Database, error) {
	// Parse the URI to extract the database path
	// SQLite URIs are typically: sqlite:///path/to/db.sqlite or sqlite:///:memory:
	dbPath := uri
	if strings.HasPrefix(uri, "sqlite:///") {
		dbPath = strings.TrimPrefix(uri, "sqlite:///")
	} else if strings.HasPrefix(uri, "sqlite://") {
		dbPath = strings.TrimPrefix(uri, "sqlite://")
	}

	// For in-memory database
	if dbPath == ":memory:" || dbPath == "" {
		dbPath = ":memory:"
	}

	// Create connection string with options
	connStr := dbPath
	if dbPath != ":memory:" {
		// Add options for file-based database
		connStr = dbPath + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=1"
	} else {
		connStr = dbPath + "?_foreign_keys=1"
	}

	conn, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &Database{
		conn:    conn,
		Queries: New(conn),
	}

	return db, nil
}

// Migrate runs the schema migrations.
func (d *Database) Migrate(ctx context.Context) error {
	_, err := d.conn.ExecContext(ctx, Schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Run additional migrations for existing databases
	if err := d.runMigrations(ctx); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// migration represents a single versioned schema migration.
type migration struct {
	version int
	name    string
	fn      func(ctx context.Context, conn *sql.DB) error
}

// migrations is the ordered list of schema migrations.
// Each migration must be idempotent (safe to re-run on databases
// that previously used the ad-hoc migration approach).
var migrations = []migration{
	{1, "create FTS5 table", func(ctx context.Context, conn *sql.DB) error {
		_, err := conn.ExecContext(ctx,
			`CREATE VIRTUAL TABLE IF NOT EXISTS page_fts USING fts5(pagepath, title, content)`)
		return err
	}},
	{2, "create page_links table", func(ctx context.Context, conn *sql.DB) error {
		_, err := conn.ExecContext(ctx,
			`CREATE TABLE IF NOT EXISTS page_links (
				source_pagepath TEXT NOT NULL,
				target_pagepath TEXT NOT NULL,
				PRIMARY KEY (source_pagepath, target_pagepath)
			)`)
		if err != nil {
			return err
		}
		_, err = conn.ExecContext(ctx,
			`CREATE INDEX IF NOT EXISTS idx_page_links_target ON page_links(target_pagepath)`)
		return err
	}},
	{3, "add issues category column", func(ctx context.Context, conn *sql.DB) error {
		var count int
		if err := conn.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM pragma_table_info('issues') WHERE name='category'").Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := conn.ExecContext(ctx, "ALTER TABLE issues ADD COLUMN category TEXT"); err != nil {
				return err
			}
			if _, err := conn.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_issues_category ON issues(category)"); err != nil {
				return err
			}
		}
		return nil
	}},
	{4, "create issue_comments table", func(ctx context.Context, conn *sql.DB) error {
		if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS issue_comments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			issue_id INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
			content TEXT NOT NULL,
			author_name TEXT,
			author_email TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx,
			`CREATE INDEX IF NOT EXISTS idx_issue_comments_issue_id ON issue_comments(issue_id)`)
		return err
	}},
}

// runMigrations runs versioned schema migrations, tracking progress
// in a schema_version table. Each migration runs at most once.
func (d *Database) runMigrations(ctx context.Context) error {
	if _, err := d.conn.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL DEFAULT 0)`); err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	// Seed with version 0 if empty (fresh database or migrating from ad-hoc system)
	if _, err := d.conn.ExecContext(ctx,
		`INSERT INTO schema_version (version) SELECT 0 WHERE NOT EXISTS (SELECT 1 FROM schema_version)`); err != nil {
		return fmt.Errorf("failed to seed schema_version: %w", err)
	}

	var current int
	if err := d.conn.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		slog.Info("running migration", "version", m.version, "name", m.name)
		if err := m.fn(ctx, d.conn); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.version, m.name, err)
		}
		if _, err := d.conn.ExecContext(ctx, `UPDATE schema_version SET version = ?`, m.version); err != nil {
			return fmt.Errorf("failed to update schema version to %d: %w", m.version, err)
		}
	}

	return nil
}

// SchemaVersion returns the current schema version.
func (d *Database) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	err := d.conn.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version)
	if err != nil {
		return 0, err
	}
	return version, nil
}

// Close closes the database connection.
func (d *Database) Close() error {
	return d.conn.Close()
}

// Conn returns the underlying database connection.
func (d *Database) Conn() *sql.DB {
	return d.conn
}

// BeginTx starts a new transaction.
func (d *Database) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return d.conn.BeginTx(ctx, nil)
}

// WithTx returns queries that use the given transaction.
func (d *Database) WithTx(tx *sql.Tx) *Queries {
	return d.Queries.WithTx(tx)
}

// PageSearchResult represents a single FTS5 search result.
type PageSearchResult struct {
	Pagepath string
	Title    string
	Snippet  string
	Rank     float64
}

// PageIndexData holds data for indexing a page.
type PageIndexData struct {
	Pagepath string
	Title    string
	Content  string
}

// SearchPages searches the FTS5 index and returns ranked results with snippets.
func (d *Database) SearchPages(ctx context.Context, query string, limit int) ([]PageSearchResult, error) {
	rows, err := d.conn.QueryContext(ctx,
		`SELECT pagepath, title, snippet(page_fts, 2, '<mark>', '</mark>', '...', 40) as snippet, rank FROM page_fts WHERE page_fts MATCH ? ORDER BY rank LIMIT ?`,
		query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []PageSearchResult
	for rows.Next() {
		var r PageSearchResult
		if err := rows.Scan(&r.Pagepath, &r.Title, &r.Snippet, &r.Rank); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// UpsertPageIndex inserts or replaces a page in the FTS5 index.
func (d *Database) UpsertPageIndex(ctx context.Context, pagepath, title, content string) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM page_fts WHERE pagepath = ?`, pagepath); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO page_fts(pagepath, title, content) VALUES(?, ?, ?)`, pagepath, title, content); err != nil {
		return err
	}
	return tx.Commit()
}

// DeletePageIndex removes a page from the FTS5 index.
func (d *Database) DeletePageIndex(ctx context.Context, pagepath string) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM page_fts WHERE pagepath = ?`, pagepath)
	return err
}

// RebuildPageIndex replaces the entire FTS5 index with the given pages.
func (d *Database) RebuildPageIndex(ctx context.Context, pages []PageIndexData) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM page_fts`); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO page_fts(pagepath, title, content) VALUES(?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range pages {
		if _, err := stmt.ExecContext(ctx, p.Pagepath, p.Title, p.Content); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// UpsertPageLinks replaces all outgoing links for a source page.
func (d *Database) UpsertPageLinks(ctx context.Context, source string, targets []string) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links WHERE source_pagepath = ?`, source); err != nil {
		return err
	}

	if len(targets) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO page_links(source_pagepath, target_pagepath) VALUES(?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for _, target := range targets {
			if _, err := stmt.ExecContext(ctx, source, target); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// DeletePageLinks removes all outgoing links for a source page.
func (d *Database) DeletePageLinks(ctx context.Context, source string) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM page_links WHERE source_pagepath = ?`, source)
	return err
}

// GetBacklinks returns all source pages that link to the given target.
func (d *Database) GetBacklinks(ctx context.Context, target string) ([]string, error) {
	rows, err := d.conn.QueryContext(ctx,
		`SELECT source_pagepath FROM page_links WHERE target_pagepath = ? ORDER BY source_pagepath`, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		sources = append(sources, s)
	}
	return sources, rows.Err()
}

// PageLinkData holds data for rebuilding page links.
type PageLinkData struct {
	Source  string
	Targets []string
}

// RebuildPageLinks replaces the entire page_links table with the given data.
func (d *Database) RebuildPageLinks(ctx context.Context, links []PageLinkData) error {
	tx, err := d.conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM page_links`); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT OR IGNORE INTO page_links(source_pagepath, target_pagepath) VALUES(?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, link := range links {
		for _, target := range link.Targets {
			if _, err := stmt.ExecContext(ctx, link.Source, target); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

// PageIndexCount returns the number of rows in the FTS5 index.
func (d *Database) PageIndexCount(ctx context.Context) (int64, error) {
	var count int64
	err := d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM page_fts`).Scan(&count)
	return count, err
}

// IssueComment represents a comment on an issue.
type IssueComment struct {
	ID          int64
	IssueID     int64
	Content     string
	AuthorName  sql.NullString
	AuthorEmail sql.NullString
	CreatedAt   sql.NullTime
	UpdatedAt   sql.NullTime
}

// CreateIssueComment inserts a new comment on the given issue.
func (d *Database) CreateIssueComment(ctx context.Context, issueID int64, content, authorName, authorEmail string) (*IssueComment, error) {
	now := time.Now()
	result, err := d.conn.ExecContext(ctx,
		`INSERT INTO issue_comments (issue_id, content, author_name, author_email, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		issueID, content, authorName, authorEmail, now, now)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &IssueComment{
		ID:          id,
		IssueID:     issueID,
		Content:     content,
		AuthorName:  sql.NullString{String: authorName, Valid: authorName != ""},
		AuthorEmail: sql.NullString{String: authorEmail, Valid: authorEmail != ""},
		CreatedAt:   sql.NullTime{Time: now, Valid: true},
		UpdatedAt:   sql.NullTime{Time: now, Valid: true},
	}, nil
}

// ListIssueComments returns all comments for the given issue, ordered by creation time.
func (d *Database) ListIssueComments(ctx context.Context, issueID int64) ([]IssueComment, error) {
	rows, err := d.conn.QueryContext(ctx,
		`SELECT id, issue_id, content, author_name, author_email, created_at, updated_at
		 FROM issue_comments WHERE issue_id = ? ORDER BY created_at ASC`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []IssueComment
	for rows.Next() {
		var c IssueComment
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Content, &c.AuthorName, &c.AuthorEmail, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// GetIssueComment returns a single comment by ID.
func (d *Database) GetIssueComment(ctx context.Context, id int64) (*IssueComment, error) {
	var c IssueComment
	err := d.conn.QueryRowContext(ctx,
		`SELECT id, issue_id, content, author_name, author_email, created_at, updated_at
		 FROM issue_comments WHERE id = ?`, id).
		Scan(&c.ID, &c.IssueID, &c.Content, &c.AuthorName, &c.AuthorEmail, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// DeleteIssueComment removes a single comment by ID.
func (d *Database) DeleteIssueComment(ctx context.Context, id int64) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM issue_comments WHERE id = ?`, id)
	return err
}

// DeleteIssueComments removes all comments for the given issue.
func (d *Database) DeleteIssueComments(ctx context.Context, issueID int64) error {
	_, err := d.conn.ExecContext(ctx, `DELETE FROM issue_comments WHERE issue_id = ?`, issueID)
	return err
}

// CountIssueComments returns the number of comments on the given issue.
func (d *Database) CountIssueComments(ctx context.Context, issueID int64) (int64, error) {
	var count int64
	err := d.conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM issue_comments WHERE issue_id = ?`, issueID).Scan(&count)
	return count, err
}
