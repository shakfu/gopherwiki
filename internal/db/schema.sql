-- Schema for GopherWiki database

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
