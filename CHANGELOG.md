# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Navbar search dropdown**: Live search results appear in a dropdown below the navbar search input as you type (HTMX, 300ms debounce, up to 8 results). Supports click-outside and Escape to dismiss. Links to full search page via "View all results" footer.
- **YAML configuration file support**: New `-config` flag (or `CONFIG_FILE` env var) to load settings from a YAML file. Precedence: defaults < config file < environment variables < CLI flags.
- **`Host` and `Port` in Config**: Server host/port are now part of the config struct, settable via config file, `HOST`/`PORT` env vars, or `-host`/`-port` CLI flags.
- **Issue Comments**: Discussion threads on issues with full CRUD support. Comments are rendered as markdown. Admin-only delete. Cascade delete when parent issue is removed. Available via both HTML form and JSON API (`/-/api/v1/issues/{id}/comments`).
- **`DEV_MODE` in dev target**: `make dev` now sets `DEV_MODE=1` and binds to `127.0.0.1` to prevent accidental network exposure during development.
- **SQLite foreign key enforcement**: Enabled `_foreign_keys=1` on all database connections so `ON DELETE CASCADE` constraints are honored.

### Removed

- **`SQLALCHEMY_DATABASE_URI` fallback**: Legacy Python env var is no longer supported. Use `DATABASE_URI` instead.
- **Redundant `REPOSITORY` fallback in main.go**: The env var is already loaded via `config.LoadFromEnv()`.

## [v0.1.0] - 2026-02-05

### Initial Release

GopherWiki is a Go translation of [An Otter Wiki](https://github.com/redimp/otterwiki), a Python-based wiki application.

### Features

- **Core Wiki Functionality**
  - View, create, edit, and delete wiki pages
  - Markdown rendering with goldmark
  - WikiLinks support (`[[Page]]` and `[[Page|Title]]`)
  - Page attachments with image thumbnails
  - Full-text search across pages

- **Git-Based Storage**
  - All content stored in Git repository
  - Full page history with diff view
  - Blame view showing line-by-line authorship
  - Revert to previous revisions

- **User Management**
  - User registration and authentication
  - Configurable access control (ANONYMOUS, REGISTERED, APPROVED)
  - Admin panel for user management

- **Extended Markdown**
  - Tables (GFM style)
  - Task lists (`- [x] done`)
  - Footnotes
  - Syntax highlighting with Chroma
  - Mermaid diagram support
  - GitHub-style alerts (`> [!NOTE]`)
  - Highlighted text (`==marked==`)
  - Table of contents generation

- **Editor Features**
  - CodeMirror-based editor
  - Draft autosave
  - Live preview

- **Additional Features**
  - RSS and Atom feeds
  - Sitemap generation
  - Dark mode support
  - Customizable sidebar
  - Health check endpoint
  - Single binary deployment with embedded assets

### Technology Stack

- Go with Chi router
- goldmark for Markdown
- Chroma for syntax highlighting
- go-git for Git operations
- SQLite with sqlc
- gorilla/sessions for session management
