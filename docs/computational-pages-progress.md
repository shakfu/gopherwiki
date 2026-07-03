# Computational Pages -- Implementation Progress / Handoff

Resume document for the Quarto computational-pages feature. Design spec is in
[computational-pages.md](computational-pages.md); this file tracks what is built,
what remains, and how to continue.

Last updated: 2026-07-04.

## Status at a glance

| Phase | Scope | State |
|-------|-------|-------|
| 1 | `.qmd` page model + extension dispatch (`internal/util`, `wiki.Page`) | **Done** |
| 2 | Frontmatter parsing (`internal/frontmatter`) | **Done** |
| 3 | Separate SQLite render cache (`internal/rendercache`) | **Done** |
| 4 | Gated Quarto render + endpoint (`internal/quarto`, config, handler) | **Done** |
| 5 | Serve cached output via iframe (`/{path}/rendered`, view branch) | **Done** |
| 6 | Observable JS (client-side tier) | Not started |
| -- | Export (PDF / DOCX / EPUB / Markdown ZIP) | Not started |

`make test` is green across all 13 packages; `go vet -tags fts5 ./...` is clean.
Build/test require the `fts5` build tag (already in the Makefile: `TAGS := -tags fts5`).

All work is **uncommitted** in the working tree (per project rule: the user
commits, not the assistant). Nothing has been staged.

## Architecture recap (what was built)

Hybrid renderer: goldmark stays the in-process renderer for plain `.md` pages;
Quarto renders `.qmd` pages **offline** via an authenticated, gated action, and
the self-contained HTML is stored in a separate SQLite cache and served to
readers inside an iframe. On-view execution never happens. The whole feature is
optional and feature-detected -- absent Quarto, the app runs unchanged and
`.qmd` pages show a render-pending placeholder.

Data flow: author `.qmd` -> editor `POST /{path}/render` -> `quarto.Service`
(concurrency-capped) -> `quarto.Renderer` executes in an isolated temp dir with
a minimal env -> self-contained HTML -> `rendercache` (content-hash key) ->
reader `GET /{path}` embeds `GET /{path}/rendered` in an iframe.

## Packages and key files

- `internal/util/util.go` -- extension model: `DefaultMarkdownExtension`,
  `QuartoExtension`, `MarkdownExtensions()`, `IsMarkdownFile`, `IsQuartoFile`,
  `StripMarkdownExtension`, `CandidateFilenames`. `SanitizePagename`,
  `GetAttachmentDirectoryname`, `GuessMimetype` handle `.qmd`.
- `internal/wiki/page.go` -- `Page.IsComputational`, `Page.Body`,
  `Page.Frontmatter`; `resolveFilename` (prefers existing file, `.md` before
  `.qmd`); `Render` dispatches (placeholder for `.qmd`, goldmark on `Body` for
  `.md`); `Rename` preserves extension. Title precedence: frontmatter `title` >
  `# heading` > filename.
- `internal/wiki/service.go` -- enumeration (search/index/tree) recognizes
  `.qmd` via the util helpers.
- `internal/frontmatter/` -- `Parse(content) -> (*Frontmatter, body)`. Typed
  `Title`, `Engine`, `Execute{Enabled, Freeze}`, `Raw`. `Freeze` normalizes
  `auto`/`true`/`false`. Conservative detection (leading `---`, closing
  `---`/`...`, valid YAML mapping) else content untouched.
- `internal/rendercache/` -- separate SQLite DB. `render_cache(key, pagepath,
  source_revision, engine, html BLOB, size, created_at, last_access)`.
  `Key(source, engine, envFingerprint)` = length-prefixed SHA-256. `Put`, `Get`
  (LRU touch), `DeleteByPage`, `TotalSize`, `EvictBySize` (windowed-SUM LRU),
  `EvictByAge`, `Clear`, `DefaultPath`. **Timestamps are epoch-nanosecond
  INTEGERs** (mattn driver's variable-width time strings break `ORDER BY`).
- `internal/quarto/detect.go` -- `Detect(ctx, path)` (feature-detect via
  `quarto --version`), `Capabilities`, `Fingerprint()`.
- `internal/quarto/render.go` -- `Runner` interface (exec seam) + `execRunner`
  with `renderEnv()` (minimal env, no app secrets); `Renderer.RenderHTML`
  (temp-dir isolation, `quarto render --to html --embed-resources`, cleanup);
  `Service` (concurrency semaphore, cache write) with `Render`, `Cached`,
  `CachedKey`, `Invalidate`, `Available`.
- `internal/handlers/handlers.go` -- `RenderService` interface + optional
  `Server.RenderService` field (nil disables); `renderPageContent` (iframe on
  cache hit, placeholder on miss).
- `internal/handlers/render_handlers.go` -- `handleRender` (POST, write-gated,
  501 when disabled, 400 non-computational), `handleRendered` (GET, serves blob
  with relaxed CSP + ETag), `computationalIframe`, `renderedContentSecurityPolicy`.
- `internal/handlers/routes.go` -- `GET /{path}/rendered` (read group),
  `POST /{path}/render` (write group).
- `internal/config/config.go` -- fields + env: `COMPUTATIONAL_PAGES_ENABLED`,
  `QUARTO_PATH`, `RENDER_TIMEOUT_SECONDS`, `RENDER_CONCURRENCY`,
  `RENDER_CACHE_PATH`.
- `cmd/gopherwiki/main.go` -- `setupRenderService` (detect, open cache, wire
  service; non-fatal if absent), `sqlitePath`.

## Endpoints

- `POST /{path}/render` -- write-gated gated render. 302 on success; 501 when
  disabled/unavailable; 400 for a non-computational page; 500 on render failure.
- `GET /{path}/rendered` -- read-gated. Serves cached self-contained HTML with a
  relaxed CSP and ETag (= cache key, 304-capable). 404 on miss / non-computational
  / disabled. Never triggers a render.

## Pre-existing issues found and fixed (working tree)

- **`.gitignore`**: the unanchored `gopherwiki` pattern matched the
  `cmd/gopherwiki/` directory, silently ignoring `cmd/gopherwiki/syntax_guide.md`
  (the `//go:embed` source), so `cmd/gopherwiki` did not build at HEAD. Fixed:
  `gopherwiki` -> `/gopherwiki`, and added `bin/`.
- **`cmd/gopherwiki/syntax_guide.md`**: missing at HEAD; restored from git object
  `3ddf2cd`. Now trackable after the gitignore fix (still untracked -- needs
  `git add`).

## Live end-to-end verification (2026-07-04)

Ran the real binary against a real git repo with `COMPUTATIONAL_PAGES_ENABLED=1`.
Verified: startup detection; placeholder before render; `POST /render` (with CSRF
double-submit) -> 302; `/rendered` -> 200 self-contained HTML (~1.18 MB, correct
DOCTYPE); iframe embedding after render; relaxed CSP + ETag headers; 304
revalidation; 403 when POSTing render without a CSRF token.

**Execution gap:** neither engine's runtime is installed on this machine --
Jupyter is absent, and R lacks `rmarkdown`/`knitr`. An `{r}` render therefore
failed with a clean 500 (nothing cached, error logged) -- the failure path is
correct, but an actual code cell executing + embedding output was **not** yet
demonstrated end-to-end. To close this: `python3 -m pip install jupyter` (fast)
or R `install.packages("rmarkdown")` (slow, compiles).

### Reproduce the live run

```
go build -tags fts5 -o /tmp/gw ./cmd/gopherwiki
mkdir -p /tmp/wiki && cd /tmp/wiki && git init -q
printf -- '---\ntitle: T\n---\n# Hello\nRendered by **Quarto**.\n' > plain.qmd
git add -A && git commit -qm seed
REPOSITORY=/tmp/wiki COMPUTATIONAL_PAGES_ENABLED=1 DEV_MODE=1 HOST=127.0.0.1 PORT=8099 /tmp/gw &
# CSRF double-submit: GET to obtain the gopherwiki_csrf cookie, then send it as X-CSRF-Token
curl -s -c j.txt http://127.0.0.1:8099/plain >/dev/null
TOKEN=$(grep gopherwiki_csrf j.txt | awk '{print $7}')
curl -s -b j.txt -H "X-CSRF-Token: $TOKEN" -X POST http://127.0.0.1:8099/plain/render
curl -s http://127.0.0.1:8099/plain/rendered | head -c 60   # self-contained HTML
```

Note: with the default `DATABASE_URI=sqlite:///:memory:` the render cache is also
in-memory (not durable). Use a file `DATABASE_URI` or set `RENDER_CACHE_PATH` for
a persistent cache.

## Open items / known limitations

- **Env fingerprint = quarto version only** (`Capabilities.Fingerprint`); it does
  not include per-engine package versions, so changing installed Python/R packages
  does not invalidate the cache. Tied to the "environment management" open
  decision in the design doc.
- **`freeze` / persistent `_freeze/` not wired**: each gated render executes
  fresh. The content-hash cache already gives "unchanged source -> no re-render"
  at the HTTP layer, so in-Quarto freeze is only a partial-re-execution
  optimization -- deferred.
- **Search indexing** (`wiki/service.go IndexPage`) still titles/indexes from raw
  content via `GetHeader`; it does not prefer a frontmatter `title` or strip the
  block from the FTS index. Minor follow-up.
- **Frozen-output embedding** is iframe-only (design Section 5.3 Option A);
  splice-into-chrome (Option B) not done.

## Next steps to resume

1. **Commit** the Phase 1-5 work (user action). Remember `git add
   cmd/gopherwiki/syntax_guide.md` (newly un-ignored) alongside the new packages
   and modified files.
2. **Phase 6 (OJS)**: enable the client-side Observable runtime in served output
   and wire the `ojs_define` data bridge. Cheap; no server engine.
3. **Export**: format-parameterized `quarto render --to <token>` against a
   capability-gated allowlist (default `pdf, html, docx, epub, gfm`), plus a
   pure-Go Markdown ZIP (`archive/zip`). See design Section 6.
4. Optional: install a language runtime and demonstrate real code execution
   end-to-end (currently blocked by environment, see above).

## Uncommitted files (as of handoff)

New: `internal/frontmatter/`, `internal/rendercache/`, `internal/quarto/`,
`internal/handlers/render_handlers.go` (+`_test.go`),
`internal/wiki/page_test.go`, `docs/computational-pages.md`,
`docs/computational-pages-progress.md`, `cmd/gopherwiki/syntax_guide.md` (restored).

Modified: `.gitignore`, `go.mod`, `go.sum`, `internal/util/util.go` (+test),
`internal/wiki/page.go`, `internal/wiki/service.go`,
`internal/handlers/feed_handlers.go`, `internal/handlers/handlers.go`,
`internal/handlers/routes.go`, `internal/config/config.go`,
`cmd/gopherwiki/main.go`, `TODO.md`.
