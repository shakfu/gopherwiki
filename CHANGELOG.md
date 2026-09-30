# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](http://keepachangelog.com/en/1.0.0/) and this project adheres to [Semantic Versioning](http://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **API tokens**: An admin can issue bearer tokens at `/-/admin/tokens` so a script or an external agent can use the JSON API without a browser session. A token writes pages only under its path prefix, must send the base `revision` to overwrite a page, cannot delete pages, and never has admin rights. Tokens are accepted only under `/-/api/`, so a token cannot trigger a computational render. See `docs/API.md` and `docs/dev/llm-wiki.md`.

- **Code approval for computational pages**: With `RENDER_APPROVAL_REQUIRED=true`, a `.qmd` page renders only after a user with the new review permission approves the hash of its source; any edit needs a new approval. Without the setting, any editor can render, as before. The approval covers the whole source because prose can hold inline expressions and raw HTML. The source view now has Approve and Render buttons. See `docs/computational-pages.md` section 7.1.

- **MCP server**: `/-/api/v1/mcp` serves the Model Context Protocol over Streamable HTTP, so an agent can connect with an API token. Its tools (search, read and write pages, read report runs, lint, open issues, and a guide) call the JSON API in-process, so token rules, permissions and hidden pages apply unchanged. The guide includes the page named by `GUIDE_PAGE` (default `Meta/Schema`) as the wiki's conventions for agents. It is hand-written on `net/http` rather than an SDK: stateless, one JSON reply per request, no SSE. See `docs/API.md`.

- **Batch save**: `POST /-/api/v1/batch` saves up to 100 pages in one commit, so one agent run is one revert. Every page is checked before anything is written; one failing page fails the batch. If the commit fails, the written files are restored. See `docs/API.md`.

- **Lint**: `/-/lint` and `GET /-/api/v1/lint` list broken wikilinks and orphan pages, and for agent pages: missing validation, missing or superseded cited runs, and numbers in the prose that occur in none of the cited runs. The number check compares values, so formatting differences do not count; a derived figure such as a growth rate is reported for the validator to check. See `docs/dev/llm-wiki.md`.

- **Validation of agent pages**: A page written through an API token is marked as an agent page and shows a banner until a user with the review permission validates its current revision. Any later commit, human or agent, voids the validation. With `HIDE_UNVALIDATED=true`, unvalidated agent pages are hidden from everyone except reviewers and API tokens: every page action answers 404, and the page and its commits are left out of search, the page index, the sidebar, backlinks, the sitemap, the changelog, feeds and the API. See `docs/dev/llm-wiki.md`.

- **Cited report runs**: A page can list report runs in its frontmatter as `sources: [{page: <path>, run: <id>}]`. The page view shows each cited run with its period, and marks it out of date when a later run of the same period exists; the page API returns the same state under `sources`. An agent's analysis thereby names the exact figures it was written from. Entries of other shapes are ignored, so pages already using a `sources` key keep their frontmatter.

- **Report runs**: Rendering a computational page with a period (`YYYY-MM`) stores the result permanently as a report run, with its executed markdown, source hash, revision, author and time. The page receives the period as the Quarto parameter `period`. A re-run keeps earlier runs, since the source data may have been restated. The page view shows the newest run and lists the others, and `GET /-/api/v1/pages/{path}/runs` exposes them. Runs live in the primary database rather than the render cache, which can evict entries. See `docs/computational-pages.md` section 5.1.1.

- **Creating computational pages**: A page path ending in `.qmd` creates a computational page, in the editor (`/reports/august.qmd/edit`) and through `PUT /-/api/v1/pages/reports/august.qmd`. Before, a `.qmd` page could only be added to the git repository directly.

- **Computational pages (Quarto)**: Pages stored with a `.qmd` extension are rendered by Quarto and may contain executable Python (Jupyter) and R (knitr) code cells whose results embed into the page. Execution is gated behind an authenticated render action and never runs on a reader's page view; the rendered output is cached in a separate SQLite database and served inside an isolated iframe. The feature is optional and feature-detected via `COMPUTATIONAL_PAGES_ENABLED`; without Quarto installed, `.qmd` pages show a render-pending placeholder and the rest of the wiki is unaffected. The render interpreters can be pinned with `RENDER_PYTHON` / `RENDER_R`. See `docs/computational-pages.md`.

- **Observable JS (OJS)**: `{ojs}` cells run client-side for interactive, reactive content (inputs, live-updating views, Plot/d3 charts). See the offline-libraries note under Security for air-gapped operation.

- **Page export**: Any page can be exported to PDF, HTML, DOCX, EPUB, and GitHub-Flavored Markdown through Quarto (enabled with `EXPORT_ENABLED`), plus a pure-Go Markdown ZIP of the page source and its attachments that works with no toolchain installed. Wikilinks, issue references, `==highlight==` marks, and (for HTML) Mermaid diagrams are translated on export so documents keep their meaning instead of showing raw wiki syntax.

- **Frontmatter parsing**: Leading YAML frontmatter is parsed and its `title` is used for the page title and search index.

### Fixed

- **First start**: A new wiki's home and syntax guide pages were missing from search, backlinks and lint until the second start. The search index was built before those pages were created, and creating them did not index them.

- **Links in code**: Backlinks, orphan and broken-link checks counted `[[...]]` inside inline code and code blocks, and inside `==highlight==`, where the page shows no link. Links are now taken from the parsed page, so they match the rendered links. The page index is rebuilt once at the next start to drop the false links.

- **Backlinks on mixed-case URLs**: "What links here" was empty when a page was opened with capitals in its URL, such as `/Home`. Link targets are stored lowercased, but the lookup used the path as typed.

- **Saving unchanged content**: Saving a page without changes failed with "cannot create empty commit". go-git's `Status.File` reports an unchanged tracked file as untracked, so the no-change check never matched.

- **Actions on nested pages**: Edit, save, source, history and every other page action returned 404 for a page below the top level, such as `docs/setup/edit`. A chi URL parameter cannot contain `/`, so `/{path}/edit` matched only single-segment paths; only viewing worked, through a separate catch-all. Page routes now split a known trailing action off the full path.

- **Nested pages with attachments**: Viewing a nested page that had attachments returned 404, because its attachment directory was mistaken for an attachment file.

- **Frontmatter-aware search**: The search index now prefers a frontmatter `title` and strips the YAML frontmatter block from the indexed content, so raw metadata is neither indexed nor matched by search.

### Security

- **Page paths with `..`**: An API token could write a page through a path such as `x/../kb/page`. The prefix check cleaned the path but storage and the git status lookup did not, so the write was never committed, skipped the revision check, and was not marked for validation. Storage now refuses non-canonical names, and the API answers 400 for a path with an empty, `.` or `..` segment.

- **Hidden pages in diffs and the API**: With `HIDE_UNVALIDATED=true`, `/<page>/diff` showed every file changed between two revisions, including hidden agent pages; it now shows only the page's own file. The page API checked hiding on the path before a `/runs/` segment, so a hidden page such as `kb/runs/notes` was served; it now checks the page the handler loads. See `docs/dev/security.md`.

- **Export is opt-in**: Quarto-produced export requires the explicit `EXPORT_ENABLED` setting; merely having Quarto on the host does not expose export endpoints or run detection at startup. Markdown ZIP remains available regardless.

- **Computational render isolation**: Rendered `.qmd` output is served in a sandboxed iframe under a relaxed Content-Security-Policy scoped to that document only, while the surrounding wiki keeps the strict policy. Render subprocesses run with a minimal environment that excludes application secrets, plus wall-clock and concurrency limits. Interactive output (Observable JS) additionally requires `allow-same-origin` on the iframe and access to the Observable CDNs; this and the whole feature assume a trusted editing team (no untrusted-author sandboxing) as documented in `docs/computational-pages.md`.

- **Offline Observable JS libraries**: OJS pages load their standard library (Inputs, Plot, d3, marked, and similar) from the Observable/jsDelivr CDNs at view time by default. Setting `OJS_LIBS_DIR` to a local mirror (generated by `scripts/mirror-ojs-libs.sh`) makes the wiki serve those libraries itself at `/ojs-libs/` and rewrites rendered pages to load them from there, removing the per-reader CDN fetch and enabling air-gapped deployments; the rendered-output CSP then drops the CDN allowance entirely. Mirror versions are pinned to the Quarto version in use and are regenerated on upgrade.

### Changed

- **Render-aware caching for computational pages**: A computational page's ETag now reflects its current render state, so re-rendered output is not masked by a stale browser cache.

### Removed

- **Unimplemented settings**: `AUTH_METHOD`, `AUTH_HEADERS_*`, `MAIL_*`, `NOTIFY_*`, `GIT_WEB_SERVER`, `GIT_REMOTE_*`, `HTML_EXTRA_*`, `ROBOTS_TXT`, `COMMIT_MESSAGE`, `WIKILINK_STYLE`, `MINIFY_HTML`, `MAX_FORM_MEMORY_SIZE`, `SITE_ICON`, `HIDE_LOGO`, `TREAT_UNDERSCORE_AS_SPACE_FOR_TITLES`, and the `SIDEBAR_*` settings other than `SIDEBAR_MENUTREE_MODE` were parsed but never read. The Otter Wiki examples in `docs/auth_examples/`, `docs/custom_css_example/` and `docs/custom_html_example/` were removed with them.

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
