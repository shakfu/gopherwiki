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
| 6 | Observable JS (client-side tier) | **Done** (required 3 fixes: sandbox, runtime injection, CDN CSP -- see below) |
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
  `RENDER_CACHE_PATH`, `RENDER_PYTHON`, `RENDER_R`, `EXPORT_ENABLED`.
- `cmd/gopherwiki/main.go` -- `setupRenderService` (detect, open cache, wire
  service; non-fatal if absent), `sqlitePath`.

## Endpoints

- `POST /{path}/render` -- write-gated gated render. 302 on success; 501 when
  disabled/unavailable; 400 for a non-computational page; 500 on render failure.
- `GET /{path}/rendered` -- read-gated. Serves cached self-contained HTML with a
  relaxed CSP and ETag (= cache key, 304-capable). 404 on miss / non-computational
  / disabled. Never triggers a render.

## Phase 6 -- Observable JS (client-side tier)

**History note:** Phase 6 was initially recorded as "done, no code needed" based
on seeing the Observable runtime *strings* in the rendered HTML. That was wrong --
OJS never actually executed. Making it work (verified with a headless browser,
including reactivity) required **three** fixes, because OJS is neither
self-contained nor runnable in an isolated frame:

1. **iframe sandbox (`render_handlers.go`).** The computational-render iframe
   omitted `allow-same-origin`; in an opaque-origin frame the runtime's inline
   `<script type="module">` blocks do not run. Added `allow-same-origin`. Security
   tradeoff: with `allow-scripts` this lets a rendered page's author JS reach the
   wiki origin -- acceptable only under the trusted-team model (Section 7); the
   hardened alternative is a separate serving origin (deferred).
2. **Runtime injection (`quarto/ojs.go`, `detect.go`, `render.go`).** Quarto's
   `--embed-resources` does **not** inline the OJS runtime -- it drops the runtime
   `<script>` and leaves `window._ojs` undefined, so the bootstrap throws. After
   render, `injectOJSRuntime` inserts Quarto's bundled `quarto-ojs-runtime.min.js`
   (located via `Capabilities.OJSRuntimePath`) as an inline module before the
   bootstrap. Guarded: no-op when there are no OJS cells, when the runtime is
   already present, or when the bundle can't be found (OJS degrades to inert
   source; Python/R/plain pages unaffected). The cache `Fingerprint` gained a
   pipeline-version suffix so pre-fix cached renders are regenerated.
3. **CSP CDN allowlist (`render_handlers.go`).** OJS loads its standard library
   (`Inputs`, `md`/marked), FileAttachments, and imported notebooks from the
   Observable/jsDelivr CDNs **at view time** -- it is not self-contained. The
   rendered-output CSP now allows `cdn.jsdelivr.net`,
   `cdn.observableusercontent.com`, and `api.observablehq.com`.

**Consequence:** by default, unlike frozen Python/R output, **OJS pages fetch
their libraries from the Observable/jsDelivr CDNs when viewed** (the runtime lazy-
loads `Inputs`, `md`, `Plot`, `d3`, etc.).

**Offline OJS (`OJS_LIBS_DIR`).** For air-gapped/offline operation, mirror those
libraries locally and set `OJS_LIBS_DIR` to the mirror directory
(`scripts/mirror-ojs-libs.sh <dir>` fetches the common baseline). Then:

- the wiki serves the mirror at `/ojs-libs/*` (`config.OJSLibsDir`, `routes.go`);
- `quarto.WithOJSLocalLibs("/ojs-libs")` makes `RenderHTML` rewrite the CDN
  origins in the rendered output to that local base (`rewriteOJSCDNs` in
  `ojs.go`: `cdn.jsdelivr.net/` -> `/ojs-libs/jsdelivr/`, plus
  observableusercontent and api.observablehq);
- the rendered-output CSP drops the CDN allowance (`renderedCSPLocal`);
- the cache fingerprint gains an `ojslocal` suffix so switching modes
  regenerates output.

Verified end-to-end with a headless Chromium, **CDNs hard-blocked**: all five
libraries load from `/ojs-libs/`, the slider/markdown/Plot cells render, zero
console errors, zero external requests. In the default (no `OJS_LIBS_DIR`) mode
the slider renders and reactivity works (n=80 -> "n squared is 6400").
Caveat: the mirrored versions are pinned by the Quarto version and the set by the
Observable features pages use, so the mirror is regenerated per Quarto upgrade /
when new libraries are used (see the script header).

A pure-OJS page needs no server engine (no Jupyter/knitr), so it renders on a host
without a language runtime.

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
  **no toolchain**, so it works even when quarto is absent. Quarto formats
  require only `ExportAvailable()` (toolchain detected -- not the compute flag or
  cache); an unknown format is a 400, a missing one a 400, a Quarto format with
  no toolchain a 501.
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
with `.venv/bin` on `PATH`); no `QUARTO_PYTHON` was needed.

**Execution gap -- CLOSED for R (2026-07-04).** `knitr` + `rmarkdown` were
installed (`install.packages`), and a real `{r}` page rendered end-to-end through
`POST /render`: the knitr engine executed the cells and their output
(`The R sum is 55` from `sum(1:10)`, plus `summary()` stats) appeared in the
served `/rendered` HTML (200). Verified rigorously that `RENDER_R` pinning drives
interpreter selection: with `/opt/homebrew/bin` removed from `PATH` so R was
**not** discoverable (quarto still at `/usr/local/bin`), `RENDER_R=/opt/homebrew/bin/R`
alone made the render succeed. Both `RENDER_PYTHON` and `RENDER_R` pinning are now
proven the same way (interpreter off PATH, pin is the only route).

Interpreter pinning (implemented 2026-07-04): `renderEnv()` now also forwards
`QUARTO_PYTHON`, `QUARTO_R`, `QUARTO_R_HOME` from the server environment when
present, and two config keys pin them explicitly: `RENDER_PYTHON` (->
`QUARTO_PYTHON`) and `RENDER_R` (-> `QUARTO_R`), threaded via
`quarto.WithInterpreters`. Explicit config overrides ambient. Verified
end-to-end: with the venv **absent from PATH**, `RENDER_PYTHON=.../.venv/bin/python`
alone made a real `{python}` render succeed (302 -> 200, computed output present).
Application secrets still never reach the render subprocess (the env is a strict
allowlist). Both `RENDER_PYTHON` and `RENDER_R` are now verified end-to-end.

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
- **Frozen-output embedding** is iframe-only (design Section 5.3 Option A);
  splice-into-chrome (Option B) not done.
- **Export re-renders from source with `--no-execute`.** A computational page
  exported to PDF/DOCX therefore shows its code cells but not their computed
  output (the cached executed HTML is not reused for non-HTML formats). Wiring
  `freeze`/`_freeze/` would let export reuse frozen results; deferred.
- **Export fidelity for wiki-specific markdown.** Export runs the source through
  Quarto/Pandoc via `renderer.PrepareExportSource` (see refinement below), which
  now translates wikilinks/issue-refs, `==highlight==`, and (for HTML)
  mermaid diagrams; math needs no translation (Pandoc-native). Remaining
  untranslated: mermaid diagrams in non-HTML formats (rendered as code listings
  -- rendering them to images would require `quarto install chrome-headless-shell`
  on the render host). Markdown ZIP ships the raw source untouched.

## Recent refinements (2026-07-04)

- **Export decoupled from the compute flag, behind its own opt-in.** Quarto
  export is independent of `COMPUTATIONAL_PAGES_ENABLED` but requires its own
  explicit flag, `EXPORT_ENABLED` (default off). A plain-markdown wiki can offer
  PDF/DOCX/EPUB/HTML/GFM export without enabling code execution, while a host that
  merely has quarto on `PATH` does **not** silently expose export endpoints or
  pay a startup detection cost. Mechanics: `setupRenderService` runs only when
  `QuartoEnabled || ExportEnabled`; it opens the render cache only when execution
  is enabled (so the service can have a nil cache), and `WithExport(cfg.ExportEnabled)`
  sets the export capability. `Service.ExportAvailable()` = toolchain present AND
  export opted in; `Available()` = toolchain + cache (gates execution). The export
  handler and menu key off `ExportAvailable()`, the render endpoints off
  `Available()`; Markdown ZIP is pure-Go and always available. Verified live: with
  neither flag, no quarto detection runs and quarto export returns 501 (md-zip
  still 200); with `EXPORT_ENABLED=1`, export works while `.qmd` execution stays
  gated behind `COMPUTATIONAL_PAGES_ENABLED`.
- **Frontmatter-aware search indexing.** `wiki/service.go` now derives the search
  title and indexed body via `indexTitleAndBody`: it prefers a frontmatter
  `title`, then a heading, then the page name, and strips the YAML frontmatter
  block from the FTS body and brute-force grep so raw metadata is not searchable.
  Applied in `IndexPage`, `EnsureSearchIndex`, and `searchBruteForce`.
- **Export source preparation** (`renderer.PrepareExportSource`, in
  `export_prep.go`). A single frontmatter/fence/code-span-aware pass over the
  source before the Quarto call, applying three transforms so exports keep their
  meaning instead of emitting raw wiki syntax:
  - `[[Target]]` / `[[Target|Label]]` / `[[#123]]` -> standard markdown links,
    absolute against `SITE_URL` (site-relative when unset), matching the HTML
    renderer's path mapping (spaces->hyphens, case preserved; issue refs ->
    `/-/issues/N`).
  - ` ==highlight== ` -> Pandoc `[text]{.mark}` spans (render as `<mark>`).
  - ```` ```mermaid ```` -> ```` ```{mermaid} ```` diagram cells, **gated to HTML
    export only** (`renderMermaid` arg = `format == "html"`). Non-HTML mermaid
    rendering needs a headless browser; enabling it for PDF/DOCX without one makes
    the whole export fail, so those formats keep the plain fence (code listing).
    Math ($...$) is Pandoc-native and untouched.

  Skips fenced code blocks and inline code spans. Applied in `handleExport` for
  Quarto formats only (Markdown ZIP keeps raw source). Verified live: HTML export
  of a page with a wikilink, `==highlight==`, `$E=mc^2$`, and a ```` ```mermaid ````
  block rendered the diagram (`<pre class="mermaid">`), `<mark>`, and MathJax; PDF
  and DOCX of the same page succeeded (mermaid as a code listing) rather than
  failing.

  Hardened after a code review found several bugs (all fixed, with tests):
  - **Line endings / BOM.** Input is normalized (strip a leading BOM, CRLF->LF)
    before structural detection, so CRLF-authored pages (JSON API saves, external
    git imports) no longer defeat fence detection -- previously a `` ```\r `` never
    closed, leaving everything after the first code block un-rewritten.
  - **Frontmatter.** The leading block is now identified with the canonical
    `frontmatter.Parse` (BOM/CRLF/YAML-validity aware) instead of a hand-rolled
    `lines[0] == "---"` check, so export agrees with how the page renderer and
    search treat frontmatter; the block is preserved verbatim.
  - **Inline code spans.** A span opened by N backticks now closes only on a run
    of exactly N (CommonMark), via `exactBacktickRun`; previously a longer inner
    run closed the span early and rewrote wikilinks/highlights that were inside
    inline code.
  - **Highlight.** The regex allows a lone `=` inside a span (matching the mark
    parser), and `[`/`]` in span text are escaped so `==a]b==` no longer produces
    a truncated Pandoc span.

## Next steps to resume

The Quarto feature (Phases 1-6 + export) is complete and green. Both `{python}`
and `{r}` execution, and both `RENDER_PYTHON`/`RENDER_R` interpreter pinning, are
verified end-to-end. Remaining optional follow-ups:

1. Reuse frozen output for non-HTML export so a computational page exported to
   PDF/DOCX carries its computed results, not just its code (needs
   `freeze`/`_freeze/` wiring).
2. Optionally render mermaid diagrams in non-HTML export by detecting/installing
   `chrome-headless-shell` and passing `renderMermaid` for those formats too
   (today they degrade to code listings). Remaining goldmark-only constructs are
   otherwise covered.
3. Design Section 5.3 Option B (splice rendered output into the wiki chrome
   instead of an iframe), if unified theming becomes a priority.
