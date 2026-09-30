# LLM-maintained content: design note

Status: proposal, not implemented.

Background: Karpathy's [LLM Wiki gist](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f),
[nashsu/llm_wiki](https://github.com/nashsu/llm_wiki) and
[lucasastorian/llmwiki](https://github.com/lucasastorian/llmwiki).

## Decisions

- The LLM runs outside gopherwiki. Gopherwiki adds no provider config, no chat
  and no vector store. It exposes an authenticated API, later wrapped as MCP.
- One agent, managed by the sysadmin. It is not a resource offered to users.
- The agent writes only under a configured path prefix (the "agent prefix").
  Outside the prefix it opens an issue instead of editing.
- Read access stays instance-wide (`READ_ACCESS=APPROVED`). Every approved
  user may read everything the agent writes.
- A human validates agent-written content.
- Figures never pass through the LLM on their way to the reader. See "Data
  flow".
- The agent runs on a zero-retention cloud provider or on a local model. The
  API and MCP endpoint therefore assume no specific client.

Target use case: a monthly company report on financial and sales data, with a
short agent-written analysis that a human validates.

## Data flow

```text
ERP --cron sync--> reporting DB (read-only views) --SQL--> .qmd render --> report
                                                                 |
                                         executed markdown <-----+
                                                 |
                                               agent --> analysis page --> human validates
```

- A cron job syncs the ERP into a reporting database (Postgres or SQLite).
  The database exposes read-only views. This part involves no agent.
- A human-written or human-vetted `.qmd` queries the views. Its code is
  approved by hash and does not change between months.
- Each report runs once for its period. The result is a frozen record.
- Stage 1: the agent reads the frozen results as markdown, with no code, and
  writes prose only. It may read several months and report trends and
  anomalies.
- Stage 2 (later): the agent also queries the views through a database MCP
  server for further analysis.

## Features, in build order

### 1. Agent token (implemented)

Usage is in `docs/API.md`.

- Table `api_tokens(id, user_id, label, token_hash, write_prefix, created_at,
  last_used_at)`. Revoking deletes the row.
- The token is 32 random bytes. The table stores its SHA-256.
- The admin panel creates and revokes tokens. There is no self-service page.
- A request with `Authorization: Bearer` skips the CSRF check. It carries no
  cookie, so CSRF does not apply.
- A token write outside `write_prefix` returns 403.
- A token is accepted only under `/-/api/`. Render, upload, rename and revert
  are form endpoints, so a token cannot reach them. The agent cannot run a
  `.qmd`.
- A token cannot delete pages and never has admin rights.
- A path ending in `.qmd` creates a computational page, through the API and
  the editor.
- A token write to an existing page must send `revision`. `SavePage` checks the
  base revision only when one is supplied. Requiring it forces the agent to
  read a human's edit before overwriting it.
- The token's user is the commit author. History and blame then separate agent
  edits from human edits.

### 2. Executed report as markdown (implemented)

The agent needs the report's figures without the code.

- A report run stores Quarto's executed markdown beside the HTML, from the
  same execution.
- `GET /-/api/v1/pages/{path}/runs/{id}` returns it. Usage is in
  `docs/API.md`.
- The existing GFM export does not serve: it runs with `--no-execute`.

### 3. Provenance (implemented, without the table)

- The analysis is a separate `.md` page under the agent prefix. It can span
  several months.
- Frontmatter `sources:` is a list of `{page, run}`: each report page and
  the ID of the report run the agent read. Runs never change, so the ID
  identifies the figures.
- An analysis is out of date when a report it cites has a newer run for the
  same period. The page view and the page API show this per source.
- The state is resolved from the page's own frontmatter when it is read.
  A `page_sources` table is needed only for lookups across pages, such as
  lint or "which analyses cite this run". It is deferred to lint.
- Not built: stage 2 `{query, hash}` entries.

### 4. Validation (implemented)

Validation binds to a git revision. Any later commit voids it, human commits
included.

- An agent page is a page an API token has written, recorded in
  `agent_pages` on the first token write. The mark is permanent: revoking the
  token or changing its prefix does not end the need for validation. Renaming
  the page drops the mark.
- Table `page_validations(filename, revision, validated_by, validated_at)`.
  A page is validated when a row matches its current revision.
- Only a session-authenticated user with the reviewer permission can
  validate, from the page view. The form carries the revision shown; a
  mismatch validates nothing. A token request cannot validate.
- Readers see an agent page with a banner: unvalidated, or validated by whom
  and when.
- `HIDE_UNVALIDATED=true` hides unvalidated agent pages from everyone except
  reviewers and API tokens. A hidden page answers 404 on every page action,
  its attachments included. It is left out of search, the page index, the
  sidebar, backlinks, the sitemap and the API. Commits that touch it are left
  out of the changelog and feeds, and their commit view answers 404.
- Cost: while hiding is active, each listing reads the current revision of
  every agent page, and the changelog walks each hidden page's history.

The state lives in the database, not in frontmatter. The agent writes page
content, so it could write a frontmatter flag itself.

### 5. Lint (implemented)

`GET /-/api/v1/lint` and a page at `/-/lint`. Deterministic; no model call.

- `broken_link`: a wikilink whose target page does not exist.
- `orphan`: no other page links here. The home page is exempt.
- `unvalidated`: an agent page whose current revision is not validated.
- `no_sources`, `missing_source`, `stale_source`: an agent prose page that
  cites no run, cites a run that does not exist, or cites a run superseded by
  a later run of the same period.
- `unmatched_number`: a number in an agent prose page that occurs in none of
  the cited runs' executed markdown, nor in their periods. Numbers compare by
  value, so `4,500,000` matches `4500000`. A derived figure, or `4.5M` for
  `4500000`, is reported; the finding asks the validator to check it.
- Agent `.qmd` pages are checked for validation only.
- Findings on pages hidden from the request are left out.
- The `page_sources` table was not needed: the provenance checks cover agent
  pages only, and lint reads their frontmatter directly.

### 6. Batch save (implemented)

One agent run can touch several pages. One commit for the run makes it one
revert. `POST /-/api/v1/batch` saves up to 100 pages in one commit. Every
page is checked first, under the same token rules as a single save; one
failing page fails the batch and nothing is written. `Storage.Commit` was not
reused: it commits files written earlier, outside the storage lock. The new
`StoreFiles` writes and commits under one lock and restores the files if the
commit fails.

### 7. MCP adapter and guide page

`/-/mcp` wraps the endpoints above: guide, search, read, rendered, write,
backlinks, lint, changelog. The guide tool returns a wiki page holding the
conventions (for example `Meta/Schema`) plus the page index.

Two files from the gist are not needed. The git changelog replaces `log.md`.
`PageIndex` replaces a hand-maintained `index.md`.

## Quarto work the data flow depends on

Independent of the agent.

- Database access. The render environment forwards only `PATH`, `HOME`,
  `TMPDIR`, `LANG`, `LC_ALL` and the Quarto interpreter variables
  (`internal/quarto/render.go`). Because `HOME` is forwarded, libpq clients can
  read `~/.pgpass` and `~/.pg_service.conf`, so a Postgres connection may need
  no code change. Not tested.
- Code approval (implemented; see `docs/computational-pages.md` section 7.1).
  A `.qmd` renders only when the hash of its source is on an approved list.
  Any edit changes the hash and needs a new approval.
- Reviewer permission (implemented). A per-user flag, `allow_review`, beside
  `allow_read`, `allow_write` and `allow_upload`. Only a reviewer or an admin
  approves a code hash or validates agent prose. A user with write permission
  may run an approved hash for a period.
- Scope. Every render can reach the database, so approval applies to every
  `.qmd`, not only agent-proposed ones. `RENDER_APPROVAL_REQUIRED` turns the
  requirement on; instances without it keep today's behaviour.
- Not yet built: the approval view shows the full source only, not the diff
  from the last approved hash.
- Report runs (implemented; see `docs/computational-pages.md` section 5.1.1).
  The period is a render parameter outside the hash, so one approval covers
  every month. The server accepts only a `YYYY-MM` value. A run is stored
  permanently in the primary database with its HTML, executed markdown, source
  hash and run time. Running a period again adds a run and keeps the old one.
- Data freshness still applies to renders without a period: the render cache
  key covers source, engine and environment, not data.

## Agent-proposed reports

The agent may save a `.qmd` with code and prose under its prefix. Only a human
can render it.

- A proposed `.qmd` is a page whose source hash is not approved. It shows the
  existing render-pending placeholder. No new mechanism is needed beyond code
  approval.
- Review comes before the run. Code executes at render with database access,
  so checking the output afterwards is too late to stop it.
- The approval view shows the full source, or the diff from the last approved
  hash. The approver holds the reviewer permission.
- If the agent edits an approved page, the hash changes and approval lapses.
  Earlier frozen records stay.
- The hash covers the whole source. Prose can hold inline expressions and raw
  HTML, so the executable part cannot be separated reliably.

Consequences:

- A `.qmd` with month-specific narrative needs a new approval each month. A
  reusable report keeps its prose period-neutral; the monthly narrative goes in
  the agent's `.md` analysis.
- The agent writes the prose before any render. Text such as "revenue rose"
  beside an inline value can disagree with the computed value. The human check
  after render must cover this.

## Stage 2: agent queries the views over MCP

The database MCP server is separate from gopherwiki.

- The agent uses its own read-only database role, with a statement timeout.
- Figures from an agent query occur in no frozen report. The lint number
  check reports them, and the validator cannot verify them by reading.
- The agent records each query in the page's `sources` as `{query, hash}` so a
  human can re-run it.
- A query worth keeping becomes a vetted report: the agent proposes a `.qmd`
  and a human approves it.

## Later: per-page agent support

The preferred end state lets content editors opt a page into agent support.

- Frontmatter `managed: agent` replaces the prefix check.
- A token write may not add, change or remove `managed` on an existing page.
- Tokens become per-user.

## Open questions

1. One reviewer permission covers code approval and prose validation. A
   reviewer who cannot read code can still approve it. Split the flag if the
   two groups differ.
2. Derived figures are numbers calculated from stored ones: a month-over-month
   change, a margin, a rolling mean, a deviation from it. Assumed: approved
   code calculates them and the agent only cites them. Not confirmed.

## Known risks

- Prompt injection. Page content reaches an agent that holds a write token.
  The prefix and the `.md`-only rule limit the damage to agent prose.
- Transcribed figures. In a separate `.md` page the agent copies digits from
  the executed report. The lint number check catches mismatches; it is
  imperfect on rounding and number formats.
