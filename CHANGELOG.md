# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/)
and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Admin "Rebuild Search Index" action**: A maintenance button in Admin Settings rebuilds the full-text search index and backlink graph from the Git repository on demand - the recovery path when derived state diverges from the repo.

### Fixed

- **Revert left search and backlinks stale**: Reverting a commit updated Git but not the derived state, so search results, backlinks, and the sidebar reflected pre-revert content. `Revert` now rebuilds the index and invalidates the page-tree cache.
- **Search index could not self-heal**: A failed incremental index update previously persisted until the database was deleted (the only automatic rebuild required a completely empty index). An unconditional `RebuildIndex` now backs both the admin action and post-revert repair.
- **Rename did not refresh the sidebar**: Renaming a page left the cached page tree stale until its TTL expired; the cache is now invalidated immediately. Rename's index maintenance also moved behind the wiki service for consistency with save/delete.

## [0.1.1]

### Security

- **Content-Security-Policy and security headers**: Every response now carries `Content-Security-Policy`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: SAMEORIGIN`, and `Referrer-Policy`. The CSP uses a strict `script-src 'self'` (no `unsafe-inline`, no `unsafe-eval`); `style-src` retains `'unsafe-inline'` only because MathJax/Mermaid inject styles at runtime.
- **Self-hosted MathJax and Mermaid**: These libraries are now served from `/static/` instead of a third-party CDN (`cdn.jsdelivr.net`), removing a per-reader IP/referrer leak, enabling air-gapped deployments, and allowing the strict CSP above.
- **Stored XSS in search snippets fixed**: FTS snippets are HTML-escaped before the `<mark>` highlight markers are restored, so page content containing markup (e.g. `<img onerror=...>`) can no longer execute when shown in search results.
- **SVG/active-content attachments forced to download**: Non-raster attachments (notably SVG) are served with `Content-Disposition: attachment` and `nosniff` so they cannot execute script in the wiki origin.
- **Upload filename sanitization**: Attachment filenames are reduced to their base name and reject empty/`.`/`..`/separator/null-byte values, preventing path traversal or overwriting page files.
- **Login open-redirect fixed**: The `next` parameter is now accepted only as a local (single-leading-slash) path; off-site and protocol-relative targets fall back to `/`.
- **Secure session cookies**: New `COOKIE_SECURE` setting (auto-enabled when `SITE_URL` is `https://`, forced off in dev) marks the session cookie `Secure`.
- **Login timing oracle removed**: Authentication performs a constant dummy bcrypt comparison when the email is unknown, so response time no longer reveals whether an account exists.
- **CSRF protection**: State-changing requests (POST/PUT/DELETE) require a per-session CSRF token, supplied via a hidden form field or the `X-CSRF-Token` header. Logout is now a POST.
- **Hardened development mode**: `DEV_MODE` generates a random per-process session key instead of a shared hardcoded one, and refuses to bind to non-loopback interfaces.

### Fixed

- **Math rendering was broken**: `` ```math `` blocks rendered as plain code (MathJax skips `<pre>`/`<code>`) and inline `\(...\)` lost its backslashes to Markdown escaping before MathJax ran, so no math displayed. Display blocks are now rewritten into `\[...\]` inside a `<div>` (mirroring the Mermaid handling) and a goldmark inline extension preserves `\(...\)` / single-line `\[...\]`. Inline math now also flags the page as needing MathJax.
- **Draft autosave accumulated duplicate rows**: `UpsertDraft` had no real conflict target and the `drafts` table lacked a unique index, so every autosave inserted a new row and the editor could reload stale content. Added a unique index on `(pagepath, author_email)` (with a de-duplicating migration) and a proper upsert.
- **No panic recovery or server timeouts**: Added `Recoverer` middleware and `ReadHeaderTimeout`/`ReadTimeout`/`IdleTimeout` on the HTTP server.

### Changed

- **Inline scripts and event handlers removed**: All inline `on*` handlers and the editor's inline `<script>` were moved to external files (`gopherwiki-actions.js`, `editor-page.js`) using delegated listeners and `data-*` attributes, enabling the strict CSP.

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

## [0.1.0]

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
