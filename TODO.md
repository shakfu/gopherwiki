# TODO

## High Priority

- [ ] Add CSRF protection to forms
- [ ] Set `Secure` flag on session cookies for production
- [ ] Fix timing attack in authentication (compare dummy hash on user-not-found)
- [ ] URL-encode login redirect `next` parameter and validate against open redirects
- [ ] Sanitize upload filenames in handler (strip directory components, reject suspicious characters)
- [ ] Implement email sending for password recovery and notifications
- [ ] Add user email confirmation flow

## Architecture / Refactoring

- [x] Clean up remaining Python configuration naming leftovers (SQLALCHEMY_DATABASE_URI fallback, etc.)
- [x] Configuration file support (YAML/TOML alternative to env-only, with env vars taking precedence)

## Features

- [ ] Implement Git HTTP server for clone/pull/push
- [ ] Implement admin settings persistence (currently read-only from env)
- [ ] Add folder/namespace management
- [ ] Implement attachment deletion
- [ ] Add bulk operations in admin panel
- [ ] Webhook/notification support (page edits, issue updates, registrations)
- [ ] Page export (PDF, Markdown ZIP, static HTML)

## Editor

- [ ] Split-pane preview (show editor and preview side-by-side)
- [ ] Drag-and-drop images (upload as attachments directly from editor)
- [ ] Auto-complete for wikilinks (dropdown of existing pages when typing `[[`)
- [ ] WebSocket/SSE for live preview (replace HTMX POST round-trip)

## Issue Tracker

- [ ] Assignees (assign issues to registered users)
- [ ] Due dates and milestones
- [ ] Markdown preview in issue forms
- [ ] Cross-references between issues (`closes #123`)
- [ ] Activity log on issues (status changes, edits)

## Markdown Extensions

- [ ] Add spoiler blocks (`>!` syntax)
- [ ] Add abbreviation support
- [ ] Add frontmatter parsing and display

## UI/UX

- [ ] Remove `user-scalable=0` from viewport meta tag (accessibility: allows pinch-to-zoom)
- [ ] Cap content line length (`max-width: 50rem` on content area for readability)
- [ ] Add page action bar below breadcrumbs (History, Attachments, Source, Blame as visible links)
- [ ] Add mobile editor toolbar (horizontally-scrollable row with common formatting actions)
- [ ] Navbar search dropdown via HTMX (show 5-8 quick results without full page navigation)
- [ ] Pre-fill commit message with auto-generated default (e.g. "Update PageName")
- [ ] Subset Font Awesome to used icons only, or switch to inline SVG icons
- [ ] Expand HTMX usage for issue close/reopen and comment submission (avoid full-page reloads)
- [ ] Replace `confirm()` dialogs with inline confirmation patterns (delete actions, draft discard)
- [ ] Add skip-to-content link and `aria-label` on search input and icon-only buttons
- [ ] Add collapsible TOC (`<details>`) for screens under 1200px
- [ ] Add "From" / "To" column headers on history comparison radio buttons
- [ ] Replace "Supports FTS5 search syntax" help text with user-friendly tips
- [ ] Group sidebar links by intent (Navigation vs Actions vs Admin)
- [ ] Improve mobile responsiveness (editor, diff views, blame views, collapsible sidebar, touch-friendly buttons)
- [ ] Dark mode toggle (persisted in user prefs or localStorage, ensure custom CSS respects dark mode classes)
- [ ] Better 404 experience (fuzzy search suggestions for similar page names, recent pages, prominent "Create this page" button)
- [ ] Diff view enhancement (syntax-highlighted diffs, side-by-side option, word-level highlighting)
- [ ] User profile/activity page (recent edits, created pages, issue activity)
- [ ] Breadcrumb enhancement (show page title from `# heading` instead of filesystem name)
- [ ] Recent changes widget (dashboard/sidebar showing 5 most recent changes)
- [ ] Add keyboard shortcuts documentation
- [ ] Implement sidebar state persistence

## Performance

- [ ] Add page content caching
- [ ] Add lazy loading for large page lists

## Testing

- [ ] Increase test coverage to 80%+
- [ ] Add end-to-end tests
- [ ] Add benchmarks for critical paths

## Documentation

- [ ] Add deployment guides (nginx, caddy reverse proxy)

## DevOps

- [ ] Health check improvements (verify database connectivity, git repo accessibility, disk space)
- [ ] Makefile improvements (add `make lint`, `make coverage`, `make release`, pin Go version)
