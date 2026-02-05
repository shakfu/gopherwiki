// Package db provides database access for GopherWiki.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

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
		connStr = dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
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

// runMigrations handles incremental schema changes for existing databases.
func (d *Database) runMigrations(ctx context.Context) error {
	// Check if issues table has category column, add if missing
	var count int
	err := d.conn.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('issues') WHERE name='category'").Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check issues schema: %w", err)
	}
	if count == 0 {
		_, err := d.conn.ExecContext(ctx, "ALTER TABLE issues ADD COLUMN category TEXT")
		if err != nil {
			return fmt.Errorf("failed to add category column: %w", err)
		}
		_, err = d.conn.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS idx_issues_category ON issues(category)")
		if err != nil {
			return fmt.Errorf("failed to create category index: %w", err)
		}
	}

	return nil
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
