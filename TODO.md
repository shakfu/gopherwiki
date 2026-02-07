# TODO

## High Priority

- [ ] Add CSRF protection to forms
- [ ] Implement email sending for password recovery and notifications
- [ ] Add user email confirmation flow
- [ ] CI/CD pipeline (GitHub Actions: test, vet, lint, build, Docker image on tagged releases)

## Architecture / Refactoring

- [ ] Clean up remaining Python configuration naming leftovers (SQLALCHEMY_DATABASE_URI fallback, etc.)
- [ ] Consolidate `sql.Null*` conversions (`db.NullBool`, `db.NullString`, etc.) into shared `db` package -- currently duplicated across handlers and models
- [ ] Configuration file support (YAML/TOML alternative to env-only, with env vars taking precedence)

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

- [ ] Add API documentation
- [ ] Add deployment guides (nginx, caddy reverse proxy)

## DevOps

- [ ] Health check improvements (verify database connectivity, git repo accessibility, disk space)
- [ ] Makefile improvements (add `make lint`, `make coverage`, `make release`, pin Go version)
