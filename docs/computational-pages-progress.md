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
| 6 | Observable JS (client-side tier) | **Done** (no server code needed -- see below) |
| -- | Export (PDF / HTML / DOCX / EPUB / GFM / Markdown ZIP) | **Done** |

`make test` is green across all 13 packages; `go vet -tags fts5 ./...` is clean.
The quarto package's integration tests now execute real `quarto` renders (they
skip when the binary is absent): plain HTML, OJS, and PDF/GFM export.
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
  with `renderEnv(interp)` (allowlisted env, no app secrets; forwards
  `QUARTO_PYTHON`/`QUARTO_R`/`QUARTO_R_HOME` and applies explicit overrides);
  `Interpreters` + `WithInterpreters` option; `Renderer.RenderHTML`
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
  `RENDER_CACHE_PATH`, `RENDER_PYTHON`, `RENDER_R`.
- `cmd/gopherwiki/main.go` -- `setupRenderService` (detect, open cache, wire
  service; non-fatal if absent), `sqlitePath`.

## Endpoints

- `POST /{path}/render` -- write-gated gated render. 302 on success; 501 when
  disabled/unavailable; 400 for a non-computational page; 500 on render failure.
- `GET /{path}/rendered` -- read-gated. Serves cached self-contained HTML with a
  relaxed CSP and ETag (= cache key, 304-capable). 404 on miss / non-computational
  / disabled. Never triggers a render.

## Phase 6 -- Observable JS (client-side tier)

OJS needs **no new server code**. Because a `.qmd` page is rendered by Quarto to
self-contained HTML (`--embed-resources`) and served inside the iframe, Quarto
already compiles `{ojs}` cells to client-side JavaScript and embeds the
Observable runtime in the output. The iframe sandbox grants `allow-scripts`, and
the relaxed CSP on `/{path}/rendered` already permits the `unsafe-inline` /
`unsafe-eval` the runtime uses. A pure-OJS page also needs no server engine
(no Jupyter/knitr), so it renders even on a host without a language runtime.

Verified by `TestIntegrationRealQuartoOJS` (skips without quarto): an `{ojs}`
page renders and the Observable runtime is present in the output.

Known limitation: the rendered-output CSP keeps `connect-src` scoped to
`'self' data: blob:`. A *standalone* OJS page that fetches its own data from an
external origin (e.g. `d3.csv("https://...")`) is therefore blocked. This is a
deliberate anti-exfiltration default; relaxing it is a per-deployment security
decision, not enabled by default.

## Export (PDF / HTML / DOCX / EPUB / GFM / Markdown ZIP)

- `internal/quarto/export.go` -- `ExportFormat` registry (`pdf`=typst-pdf,
  `html`, `docx`, `epub`, `gfm`), `Capabilities.ExportFormats()` (returns the
  set when quarto is available -- Typst and Pandoc ship bundled inside quarto,
  so quarto-present implies all formats producible), `Service.Export`,
  `Service.ExportFormats`. Exports are produced on demand and **not cached**.
- `internal/quarto/render.go` -- `RenderHTML` and the new `RenderTo` now share a
  `renderToFile` core. `RenderTo` always passes `--no-execute` so export never
  runs page code (export must not be a backdoor around gated execution);
  `--embed-resources` is added only for HTML.
- `internal/handlers/export_handlers.go` -- `handleExport` (`GET
  /{path}/export?format=<name>`, read-gated). Markdown ZIP (`md-zip`) is
  produced in-process with `archive/zip` (page source + attachments) and needs
  **no toolchain**, so it works even when the render service is absent. Quarto
  formats require the render service; an unknown format is a 400, a missing one
  a 400, a Quarto format with no service a 501.
- `internal/handlers/routes.go` -- `GET /{path}/export` (read group) + `export`
  entry in `RouteMap`.
- `web/templates/page.html` -- an "Export as" section in the page dropdown,
  populated from `data["export_formats"]` (md-zip always; Quarto formats when
  available).

Live-verified against the running binary: md-zip (zip with the source), gfm
(markdown), pdf (`%PDF-1.7`, `Content-Disposition: report.pdf`), docx (valid
`PK` zip), plus the dropdown listing all six formats.

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

**Execution gap -- CLOSED for Python (2026-07-04).** Jupyter was installed into
a project venv (`uv add jupyter`), and a real `{python}` page was rendered
end-to-end through the gated `POST /render`: the cells executed and their
computed output (`The answer is 42` from a `print`, `385` from
`sum(i**2 for i in range(1,11))`) appeared in the served `/rendered` HTML
(~1.18 MB, 200). This used the minimal `renderEnv()` allowlist unchanged -- the
venv's Python was discovered via the forwarded `PATH` (the server was started
with `.venv/bin` on `PATH`); no `QUARTO_PYTHON` was needed. R execution remains
undemonstrated: R is present but lacks `knitr`/`rmarkdown`/`reticulate`
(`install.packages("knitr")` compiles, slow).

Interpreter pinning (implemented 2026-07-04): `renderEnv()` now also forwards
`QUARTO_PYTHON`, `QUARTO_R`, `QUARTO_R_HOME` from the server environment when
present, and two config keys pin them explicitly: `RENDER_PYTHON` (->
`QUARTO_PYTHON`) and `RENDER_R` (-> `QUARTO_R`), threaded via
`quarto.WithInterpreters`. Explicit config overrides ambient. Verified
end-to-end: with the venv **absent from PATH**, `RENDER_PYTHON=.../.venv/bin/python`
alone made a real `{python}` render succeed (302 -> 200, computed output present).
Application secrets still never reach the render subprocess (the env is a strict
allowlist). `RENDER_R` remains untested for lack of `knitr` on this host.

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
- **Export capability is coupled to the computational-pages flag.** Quarto-based
  export is offered only when the render service is wired, i.e. when
  `COMPUTATIONAL_PAGES_ENABLED=1` and quarto is detected -- even though export
  runs `--no-execute` and never executes code. Decoupling export from the
  compute flag (so PDF/DOCX export could be enabled without enabling gated
  execution) is a possible refinement. Markdown ZIP is already independent.
- **Export re-renders from source with `--no-execute`.** A computational page
  exported to PDF/DOCX therefore shows its code cells but not their computed
  output (the cached executed HTML is not reused for non-HTML formats). Wiring
  `freeze`/`_freeze/` would let export reuse frozen results; deferred.
- **Export fidelity for wiki-specific markdown.** Export runs the raw source
  through Quarto/Pandoc, which does not understand GopherWiki extensions
  (`[[wikilinks]]`, the mermaid/mathjax wiring goldmark applies). Plain prose,
  headings, lists, tables, code, and images export faithfully; wiki-specific
  constructs may not. Markdown ZIP sidesteps this by shipping the raw source.

## Next steps to resume

The Quarto feature (Phases 1-6 + export) is complete and green. Remaining
optional follow-ups:

1. Demonstrate real `{r}` execution once `knitr`/`rmarkdown` are installed
   (Python is verified end-to-end incl. `RENDER_PYTHON` pinning; only R remains
   unexercised here). `RENDER_R` is wired but untested for lack of knitr.
2. Consider the other refinements above (decouple export from the compute flag;
   reuse frozen output for non-HTML export; improve export fidelity for
   wikilinks).
3. Design Section 5.3 Option B (splice rendered output into the wiki chrome
   instead of an iframe), if unified theming becomes a priority.
