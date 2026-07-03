# Computational Pages (Quarto Integration)

Status: design proposal. Not yet implemented.

This document specifies how GopherWiki integrates [Quarto](https://quarto.org)
to support **computational pages** -- wiki pages whose code cells execute and
embed their results (plots, tables, output) into the rendered page. It also
covers the related bulk-export feature (PDF / DOCX / EPUB / Markdown ZIP), which
falls out of the same pipeline.

The design is deliberately conservative: it adds Quarto as an *optional,
render-time* capability without disturbing the existing goldmark renderer, the
single-binary serving deployment, or the git-backed storage model.

---

## 1. Goals and non-goals

**Goals**

- Author pages containing executable Python, R, and Observable JS (OJS).
- Execute server-side languages (Python, R) only on an explicit, authenticated
  render action -- never on a reader's page view.
- Serve pre-computed ("frozen") output to readers from a cache: fast and
  independent of any compute toolchain on the serving node. Derived output stays
  out of git.
- Export any page to PDF, HTML, DOCX, EPUB, and Markdown ZIP via the same
  Quarto pipeline.
- Preserve the pure-Go, single-binary deployment for the common case (plain
  markdown pages) and for all serving nodes.

**Non-goals**

- Sandboxing untrusted code. This design assumes a **trusted editing team**
  (see Section 7). Open/untrusted authoring would require a per-execution
  isolation subsystem (container / gVisor / firecracker / nsjail) that is
  explicitly out of scope.
- Live, on-view execution of Python/R. Reads always serve frozen output.
- Replacing goldmark. Plain pages continue to render in-process.

---

## 2. The two execution tiers

Quarto's three target languages do not share one execution model. They split
into two tiers, and the split drives the whole architecture.

| Language | Engine | Runs where | Runs when |
|----------|--------|------------|-----------|
| Python | Jupyter | server | gated render only |
| R | knitr | server | gated render only |
| Observable JS | *(none)* | reader's browser | every page view |

### Tier 1 -- Server compute (Python, R)

Executed by Quarto at render time and **frozen** into git. Reader views serve
the frozen artifact. This tier carries all the weight: toolchain dependencies,
latency, reproducibility, and the (mitigated) code-execution risk.

**One engine per document.** Quarto uses either knitr *or* jupyter for a given
document, never both:

- A page with `{r}` cells uses **knitr** (knitr can additionally run `{python}`
  cells via `reticulate`).
- A page with `{python}` cells and no R uses **jupyter**.

Each computational page therefore resolves to a single engine, auto-detected
from its cells or set explicitly via frontmatter (`engine: knitr | jupyter`).
Supporting Python *and* R as first-class languages means both toolchains must be
present on the render host.

### Tier 2 -- Client reactive (Observable JS)

OJS is **not server compute**. It compiles to JavaScript and executes in the
reader's browser via the Observable reactive runtime. There is no kernel and no
freeze step for the computation itself. For a trusted-team wiki it is
effectively free: enable the OJS runtime in served pages.

OJS's only server touchpoint is `ojs_define()`, which embeds Python/R-computed
*data* into the page at render time. So:

- OJS-with-data (`ojs_define`) piggybacks on a Tier 1 render.
- OJS-standalone (fetches its own data, pure interactivity) needs nothing
  server-side.

Because OJS ships author-written JavaScript to readers, it is a stored-XSS
vector under an untrusted-author model. Acceptable here only because the editing
team is trusted (Section 7).

---

## 3. Hybrid renderer architecture

GopherWiki does **not** switch to Quarto wholesale. It dispatches per page:

```
                        +-------------------+
   request for page --> |  render dispatch  |
                        +-------------------+
                          |               |
              plain page  |               |  computational page
                          v               v
                  +--------------+   +------------------------+
                  |   goldmark   |   |  serve cached render   |
                  | (in-process) |   |  from SQLite blob      |
                  +--------------+   +------------------------+
                                        |            ^
                                cache   |            | (offline, gated)
                                miss -> render     +------------------+
                                pending placeholder |  Quarto render   |
                                                    |  execute+freeze  |
                                                    +------------------+
                                                            |
                                                    write blob -> SQLite
```

- **Plain pages** (the overwhelming majority) render via goldmark, in-process,
  exactly as today. No external dependency, no latency change, full existing
  feature set (wikilinks, mermaid, mathjax, chroma, TOC, footnotes).
- **Computational pages** are rendered by Quarto **offline**, at an explicit
  gated render step. Their self-contained HTML output is stored in a SQLite blob
  cache keyed by content hash. Reader views serve the cached blob; no execution
  occurs on view. A cache miss serves a "render pending" placeholder (Section 5).

One sentence: *goldmark by default; Quarto only for flagged pages, only on an
authenticated render, output cached as a SQLite blob and served statically.*

### Deployment consequence

Only the **render host** needs the heavy toolchain (Quarto + Jupyter/Python +
R/knitr). Because reads serve cached output from SQLite, **serving nodes stay
pure-Go and lightweight** -- they read a blob, never a kernel. The render step
can run on:

- a dedicated render worker,
- CI on commit, or
- an author's machine,

decoupled from the serving fleet. The single-binary *serving* deployment is
preserved; the compute dependency is quarantined to the render path.

---

## 4. Identifying a computational page

**Decision (proposed): file extension.** A page stored as `page.qmd` is
computational and routed to Quarto; `page.md` is plain and routed to goldmark.

Rationale:

- Explicit and self-describing; no need to parse cell contents on every request
  to choose a renderer.
- Matches Quarto's own convention (`.qmd`).
- Keeps the dispatch decision a cheap filesystem/extension check in the storage
  layer.

Alternatives considered:

- *Frontmatter key* (`engine:` / `execute:`): requires parsing YAML before
  dispatch; a page can silently become computational via an edit, which
  complicates caching and the "is this frozen?" check.
- *Cell sniffing* (scan for `` ```{python} ``): most implicit, most expensive,
  and ambiguous (a fenced code *listing* vs. an executable cell).

**Storage impact.** `internal/util` currently hardcodes the `.md` extension
(`GetFilename`, page-name round-tripping). Supporting `.qmd` requires:

- `GetFilename` / filename resolution to consider both `.md` and `.qmd`.
- Wikilink resolution (`[[Page]]`) to resolve a target that may exist as either
  extension.
- The render dispatcher to select goldmark vs. Quarto from the resolved
  extension.

---

## 5. Render pipeline (Tier 1)

The gated render is an authenticated, server-side action. Sketch:

1. **Trigger.** An authenticated editor invokes a "Render" action on a `.qmd`
   page (button in the UI; also exposable as an API endpoint and as a CI step).
   Not reachable by anonymous readers.
2. **Isolation.** Materialize the page source (and any referenced data
   attachments) into a per-render temporary working directory. Never render in
   the live repo working tree.
3. **Execute + freeze.** Invoke Quarto with freezing enabled:

   ```yaml
   execute:
     freeze: auto   # re-run only when the page source changes
   ```

   `freeze: auto` re-executes only when the source changed since the last
   freeze; unchanged pages reuse cached results. `freeze: true` never re-runs.
   Quarto writes execution artifacts under `_freeze/`.
4. **Resource limits.** Apply wall-clock, CPU, and memory ceilings to the
   render process. Keep secrets (session keys, DB path/credentials, admin
   tokens) **out of the render environment** -- code runs with the author's
   trust, not the server's.
5. **Concurrency cap.** Serialize or bound concurrent renders (a semaphore).
   Rendering is seconds-to-minutes of CPU-heavy work; it must not contend with
   request serving.
6. **Cache the output.** Render **self-contained** (`--embed-resources`) so
   figures inline as data URIs and the result is a single HTML blob. Write that
   blob to the SQLite render cache (Section 5.1), keyed by content hash. Nothing
   derived is committed to git.

### 5.1 Output cache: SQLite blob store

Derived output stays **out of git** -- committing ~1 MB generated blobs plus
binary `_freeze/` artifacts would bloat history permanently, pollute the
user-facing page **History** view with derived-content diffs, and invite merge
conflicts on files nobody edits. Instead:

- **Store:** a SQLite `render_cache` table. SQLite is already a dependency, so
  this adds no new infrastructure and preserves the single-binary serving story.
  Serving nodes read a blob, never run a kernel.
- **Separate database file.** The cache lives in its **own** SQLite file, not
  the primary `internal/db` database. The cache is churny (writes, evictions,
  large blobs) while the content/users/issues DB is small and durable; isolating
  them keeps cache growth and `VACUUM` from fragmenting the primary store, makes
  "clear cache" a single-file delete, and keeps backups of real data lean.
- **Key:** a content hash of `(source + resolved engine + environment
  fingerprint)`. Content-addressing means an old page revision remains servable
  while its blob is cached, and is re-derivable from that revision's source on a
  miss.
- **Value + metadata:** the self-contained HTML blob plus columns for source
  revision, engine, byte size, and last-access time (for LRU eviction via
  `DELETE WHERE last_access < ?` and size accounting via `SELECT sum(size)`).

Why SQLite over a dedicated key/value store: the workload is read-mostly with
rare, already-serialized writes and ~1 MB values. SQLite's single-writer limit
never binds here, and it gives transactional consistency with the relational
data and SQL-driven eviction that an embedded KV (bbolt/Badger) would force us
to hand-roll -- for no new dependency. A dedicated KV only pays off under
write-heavy or huge-value workloads we do not have; a shared multi-node cache,
if ever needed, points to an object store (S3/minio) for the blobs, not a KV.

The **`_freeze/` execution cache** is separate: it is a compute *input*, not
served output. It lives as a persistent working cache on the render host (a
named volume, not `tmp`), never in git and never in the SQLite output cache. If
renders run on ephemeral CI, persist `_freeze/` as a build cache to avoid
re-executing unchanged pages.

### 5.2 Cache miss: render-pending placeholder

With output in a cache rather than git, a reader can hit a computational page
whose blob was never rendered or has been evicted. Because on-view execution is
forbidden (Tier 1 rule), a miss must **not** trigger a render. Instead the page
serves a **"render pending" placeholder** within the normal wiki chrome --
stating that the page contains computations an editor must render, and offering
the "Render" action to authenticated editors. This is the one behavioral cost of
moving output out of git (git-committed output was always present on clone); it
is acceptable and must be an explicit, styled state rather than a blank or an
error.

### 5.3 Serving cached output -- open integration wrinkle

Quarto emits a **standalone, themed HTML document** (its own Bootstrap CSS/JS;
~1 MB self-contained). Splicing that into GopherWiki's chrome (navbar, sidebar,
breadcrumbs) needs a decision:

- **Option A -- iframe embed.** Serve the standalone Quarto HTML inside an
  `<iframe>` within the wiki chrome. Simplest; fully CSS/JS-isolated. Costs:
  breaks unified in-page navigation, wikilink continuity, and responsive
  behavior; double scrollbars.
- **Option B -- splice into chrome.** Extract Quarto's `<body>` and required
  head assets and inject them into GopherWiki's page template. Best UX and
  unified theming. Costs: asset-collision management (Bootstrap vs. Pico.css),
  and tracking Quarto's required JS deps (plotly, mermaid, mathjax, OJS
  runtime).

This wrinkle affects only presentation of computational pages, not the compute
pipeline, and can be decided independently. Recommendation: ship Option A first
(fast, correct, isolated), migrate to Option B if unified theming becomes a
priority.

---

## 6. Export (PDF / DOCX / EPUB / HTML / Markdown ZIP)

The same Quarto invocation, parameterized by output format, yields the bulk
export feature (`TODO.md`: "Page export"). Quarto renders ~30 formats from a
single `quarto render --to <token>` call.

| Format | `--to` token | Toolchain beyond Quarto | Notes |
|--------|--------------|--------------------------|-------|
| HTML | `html` | none | self-contained with `--embed-resources` |
| PDF | `typst-pdf` | Typst (bundled) | ~1.6 s/page in local POC; typst avoids LaTeX startup |
| DOCX | `docx` | none (Pandoc only) | office interchange |
| EPUB | `epub` | none (Pandoc only) | e-reader |
| Markdown flavors | `gfm`, `commonmark`, `mediawiki`, ... | none | paste-elsewhere; instant |

**Markdown ZIP** is orthogonal and stays **pure Go**: zip the raw source files
from the git repo via `archive/zip`. No Quarto dependency; works on any node.

**Capability gating.** At startup, detect `quarto` on `PATH`, then probe for
Typst/LaTeX and each engine. Advertise only the formats whose toolchain is
present. On a bare serving node with no toolchain, Markdown ZIP still works and
computational pages still serve their cached HTML blob; live export formats are
simply not offered there (they belong on the render host).

**Execution safety for export.** Export always renders with `--no-execute`
unless it is itself a gated render by a trusted editor. Exporting a page must
not become a backdoor around the gated-execution rule.

---

## 7. Security model

The design rests on an explicit assumption: **the editing team is trusted.**

Consequences of that assumption:

- **No sandbox subsystem.** Server code (Python/R) runs with the render
  process's privileges. Mitigations are operational, not architectural:
  resource limits (wall-clock/CPU/memory), no secrets in the render
  environment, and running the render path on a host that holds nothing
  sensitive.
- **Residual risk is author mistakes**, not malice: an accidental infinite
  loop, a runaway allocation, a destructive shell call. Resource ceilings and
  an isolated working directory bound the blast radius.
- **OJS is trusted author JavaScript** shipped to readers -- acceptable only
  under the trusted-team assumption.

If the trust boundary ever changes to open/untrusted authoring, this design does
**not** cover it. Untrusted execution requires per-execution isolation
(container / microVM / nsjail), network egress control, and a hardened data
path -- a separate, substantial project. That boundary must be a conscious
product decision, not an emergent one.

Invariants enforced in code (not left to flags):

- Reader page-views never trigger Python/R execution; a cache miss serves the
  render-pending placeholder, never an on-view render.
- Non-gated renders (e.g. export requested by a non-editor) use `--no-execute`.
- Render never inherits application secrets via environment.

---

## 8. Relationship to existing backlog

This work absorbs and converges with several standing `TODO.md` items:

- **"Frontmatter parsing and display"** -- required here anyway (Quarto keys off
  YAML frontmatter for title, `engine`, `execute`, `freeze`). Adopting `.qmd`
  pulls this in rather than adding scope.
- **"Page export (PDF, Markdown ZIP, static HTML)"** -- delivered as a byproduct
  of the Quarto pipeline (Section 6), extended to DOCX/EPUB at near-zero cost.

---

## 9. Phased plan

1. **Page model + dispatch.** Support `.qmd` alongside `.md` in
   `internal/util` filename resolution and wikilink lookup. Add the
   goldmark-vs-Quarto render dispatcher keyed on extension.
2. **Frontmatter.** Parse YAML frontmatter; expose `engine`, `execute`,
   `freeze`. (Shared with the standing frontmatter TODO.)
3. **Render cache.** Add the separate SQLite `render_cache` database (schema,
   content-hash keying, LRU eviction). Independent of the render engine, so it
   can land before Tier 1.
4. **Gated render (Tier 1).** Authenticated render endpoint: temp-dir
   isolation, `freeze: auto` (persistent `_freeze/` volume), resource limits,
   concurrency cap. Write the self-contained HTML blob to the render cache.
5. **Serve cached output.** Present the cached blob in the wiki; serve the
   render-pending placeholder on a miss. Start with iframe embed
   (Section 5.3, Option A).
6. **Export.** Format-parameterized export against a capability-gated allowlist
   (default: `pdf, html, docx, epub, gfm` + pure-Go `md-zip`).
7. **OJS (Tier 2).** Enable the client-side OJS runtime in served pages; wire
   the `ojs_define` data bridge. Cheap; last.

---

## 10. Open decisions

- **Computational flag:** `.qmd` extension (recommended) vs. frontmatter key vs.
  cell-sniffing.
- **Cached-output embedding:** iframe (recommended first) vs. spliced-into-chrome
  (Section 5.3).
- **Cache eviction policy:** size cap, age cap, or keep-latest-revision-only;
  and whether a fresh clone/deploy pre-warms the cache by re-rendering.
- **Engine scope at launch:** Python (jupyter) *and* R (knitr) from day one, or
  start with one to halve the render-host toolchain.
- **Render trigger surface:** UI button only, or also API + CI-on-commit.
- **Environment management for compute:** how Python/R package environments are
  provisioned on the render host (per-page vs. global) -- deferred, but required
  before Tier 1 is production-usable.
