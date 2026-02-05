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
