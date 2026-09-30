# User guide

This guide covers reading, editing and reviewing pages. Operators should read `docs/admin-guide.md`.

What you can do depends on your permissions. An admin sets them per user. Actions you lack permission for are hidden or return an error.

## Accounts

- Register at `/-/register` and log in at `/-/login`.

- A new account may need an admin's approval before you can read or write.

- Change your name and password at `/-/settings`.

## Pages

A page is a Markdown file in a Git repository. Every save is a commit.

- **View** a page at its path, for example `/docs/setup`.

- **Create** a page with the create shortcut in the sidebar, or open `/<path>/edit` for a path that does not exist yet.

- **Edit** with the pencil icon, or `/<path>/edit`. Enter a commit message when you save.

- **Rename** and **delete** from the page menu. Both are commits.

- **Drafts** are saved every 30 seconds while you edit. When you reopen the editor, it offers to restore the draft.

- **Nested pages**: a path with `/` creates a page below another page, such as `docs/setup`.

Page names are lowercased in the file name unless the admin set `RETAIN_PAGE_NAME_CASE`.

### Frontmatter

A page may start with a YAML block. A `title` key sets the page title and the title used by search.

```markdown
---
title: Quarterly review
---
```

## Markdown syntax

The wiki creates a `SyntaxGuide` page on first start. It lists every supported feature with examples. In brief:

| Feature | Syntax |
|-|-|
| Link to a page | `[[PageName]]` or `[[Target Page\|Label]]` |
| Link to an issue | `[[#42]]` or `[[#42\|Label]]` |
| Tables, task lists, footnotes | GitHub-flavored Markdown |
| Diagrams | a fenced `mermaid` code block |
| Math | `$inline$` and `$$display$$` (MathJax) |
| Highlight | `==text==` |

## History

- **History**: `/<path>/history` lists the commits that changed a page.

- **Diff**: compare two revisions from the history view.

- **Blame**: `/<path>/blame` shows who last changed each line.

- **Changelog**: `/-/changelog` lists all commits. A commit view offers **Revert**.

- **Backlinks**: the page view lists pages that link to the current page.

## Attachments

Upload files at `/<path>/attachments`. Embed an image with standard Markdown image syntax. SVG and other non-raster files are served as downloads, not rendered inline.

## Search

Type in the navbar search box for live results. `/-/search` gives the full result list.

## Issues

`/-/issues` is a built-in issue tracker. You can open issues, comment, tag and close them. Reference an issue from a page with `[[#<id>]]`.

## Export

The page menu offers export formats:

- **Markdown ZIP**: the page source and its attachments. Always available.

- **PDF, HTML, DOCX, EPUB, GFM**: only when the admin enabled Quarto export.

## Feeds

Recent changes are published at `/-/feed.rss` and `/-/feed.atom`.

## Keyboard shortcuts

| Key | Action |
|-|-|
| `/` | Focus search |
| `E` | Edit current page |
| `C` | Create new page |
| `[` | Toggle sidebar |
| `]` | Toggle table of contents |

## Computational pages

Available only when the admin enabled them. A page whose path ends in `.qmd` is a computational page. Quarto renders it, and its Python or R code cells run on the server.

- Create one by editing a path such as `/reports/sales.qmd/edit`.

- Code runs only when someone presses **Render** on the source view (`/<path>/source`). Viewing a page never runs code.

- Until the first render, the page shows a render-pending placeholder.

- Observable JS (`{ojs}`) cells run in the reader's browser.

### Code approval

If the admin set `RENDER_APPROVAL_REQUIRED`, a reviewer must approve the source before it can render. The source view shows **Approve this source** to reviewers. Any edit changes the source and needs a new approval.

### Report runs

Enter a period (`YYYY-MM`) before pressing **Render** to create a report run. The page receives the period as the Quarto parameter `period`. The run is stored permanently. Rendering the same period again adds a new run and keeps the old one.

The page view shows the newest run and lists the others. Open an older run with `?run=<id>`.

Design details are in `docs/computational-pages.md`.

## Agent pages

An admin can connect an AI agent to the wiki. The agent writes pages under one path prefix. Each page it writes is an agent page.

- An agent page shows a banner: **not validated**, or **validated by** a named reviewer and date.

- Validation applies to one revision. Any later commit, by a human or the agent, voids it.

- If the admin set `HIDE_UNVALIDATED`, unvalidated agent pages are hidden from readers who are not reviewers.

- An agent page can cite report runs in a **Sources** list. A source marked **Out of date** has a newer run for the same period.

- When the agent may not edit a page, it opens an issue instead.

### Reviewing agent pages

You need the review permission.

1. Open `/-/lint`. It lists unvalidated agent pages and other findings.

2. Open each page. Check its prose against its cited runs.

3. Check each `unmatched_number` finding. It marks a number in the prose that does not occur in any cited run. A derived figure, such as a growth rate, is also reported.

4. Press **Validate this revision** in the banner.

Other lint findings:

| Finding | Meaning |
|-|-|
| `broken_link` | A wikilink to a page that does not exist |
| `orphan` | No other page links here with a `[[wikilink]]`. Markdown links are not counted |
| `no_sources` | An agent page cites no run |
| `missing_source` | A cited run does not exist |
| `stale_source` | A cited run has a newer run for the same period |
