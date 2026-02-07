# GopherWiki UX Review

A review of the user interface from usability, layout, CSS, control selection, and interactive framework perspectives. The goal: improve usability while keeping the interface simple and readable.

---

## 1. Overall Assessment

GopherWiki delivers a functional, fast, wiki experience with a classic sidebar + content + optional right-column layout. It uses Halfmoon 1.1.1 as the CSS framework, HTMX for live search, CodeMirror 4.x for editing, and vanilla JS for everything else. Dark mode works well. The template structure is clean. There are, however, meaningful usability gaps across navigation, mobile experience, the editor, and visual hierarchy.

---

## 2. CSS Framework: Halfmoon 1.1.1

### Finding

Halfmoon is a Bootstrap-inspired framework last updated in 2021. It is no longer actively maintained. The `halfmoon-variables.css` file alone is 14,500+ lines. The project carries IE9/IE11 compatibility code (`.eot` fonts, `-ms-flexbox` prefixes, `polyfill.min.js`), none of which is needed in 2026.

### Concern

- No new features or bug fixes from upstream.
- Bulky variable file contributes to page weight without delivering proportional value.
- IE legacy support is pure dead weight.
- Bootstrap-class naming (`.btn-primary`, `.col-xl-3`, `.d-flex`) is familiar, but Halfmoon's implementation diverges in subtle ways from Bootstrap's, so developers hit unexpected behavior when they assume Bootstrap semantics.

### Recommendation

**Short term:** Strip IE polyfills (`polyfill.min.js`, `polyfill.e6.min.js`, `.eot`/`.svg` font-face declarations). This saves ~170KB of JS and simplifies the font stack. No user in 2026 needs this.

**Medium term:** Evaluate migrating to one of:
- **Pico CSS** -- classless/minimal, excellent readability defaults, tiny footprint. Aligns with the "simple and readable" goal.
- **Bootstrap 5.3+** -- if you want the utility-class ecosystem Halfmoon was imitating, the real thing is actively maintained and has native dark mode via `data-bs-theme`.
- **Plain CSS with custom properties** -- given that the project already leans heavily on CSS variables, cutting the framework entirely and keeping a thin utility layer is viable. The custom CSS in `gopherwiki.css` already overrides large swaths of Halfmoon.

The alternative framing: if Halfmoon is "good enough" and you do not plan heavy UI work, the cost of migration may exceed the benefit. But the dead-weight JS/CSS is worth trimming regardless.

---

## 3. Layout and Information Architecture

### 3.1 Sidebar

**Finding:** The sidebar mixes global navigation (Home, A-Z, Changelog, Issues, Create page, Admin) with the page tree and the site logo/about link at the bottom. On mobile, it overlays the content.

**Issues:**
- The "Create page" link sits between "Issues" and "Admin" with no visual grouping. Creating a page is a write action; the others are read/navigation. This violates the principle of grouping by intent.
- The sidebar footer (otter logo + site name linking to About) occupies prime space for low-value content. Most users will click "About" once, if ever.
- On mobile, the overlay sidebar requires the hamburger tap, then a link tap, then the sidebar auto-closes. Three taps to navigate. Consider whether the most common mobile actions (search, navigate to a known page) can be reached in fewer taps.

**Recommendations:**
- Group sidebar links: **Navigation** (Home, A-Z, Changelog, Issues) | **Actions** (Create page) | **Admin** (Admin, Settings). Use a subtle divider or heading.
- Move the About/logo link to a footer or the admin page. Free up sidebar space.
- Consider a bottom navigation bar on mobile for the 3-4 most used actions (Home, Search, Create, Issues), similar to GitHub Mobile or Notion Mobile. This eliminates the hamburger-then-navigate pattern.

### 3.2 Navbar

**Finding:** The navbar contains: hamburger button, site logo/name, editor toolbar (when editing), search input, three-dot dropdown menu, and context-specific buttons (Edit, Save, Preview, Cancel).

**Issues:**
- The search input is always visible on desktop, but has no `<label>` or `aria-label`. Screen readers see an unlabeled form control. The "/" keyboard hint (CSS pseudo-element) is a nice touch but is invisible to assistive technology.
- The three-dot dropdown (`fa-ellipsis-v`) contains a mix of page-specific actions (Attachments, History, Blame, Rename, Delete) and global actions (Toggle dark mode, Login/Logout, Settings). The page actions and global actions are separated only by a `dropdown-divider`, but the divider appears only on page views. On non-page views, the dropdown still uses the same three-dot icon but contains completely different items. This inconsistency requires the user to remember which context they are in.
- The `page_navbardropdown` always includes "Login" at the bottom, even when the user is already authenticated (unlike the non-page dropdown which conditionally shows Login/Logout). This appears to be a bug.

**Recommendations:**
- Add `aria-label="Search wiki"` to the search input.
- Split the three-dot menu: keep page actions in the three-dot dropdown (or better, surface them as a small toolbar below the breadcrumbs -- see Section 3.4). Move dark mode toggle to the sidebar footer or a dedicated icon button in the navbar. Move Login/Logout/Settings to a user avatar/icon dropdown, separate from page actions.
- Fix the Login/Logout inconsistency in `page_navbardropdown`.

### 3.3 Content Area

**Finding:** The main content column uses a fluid container with a two-column layout: main content + optional right-side extra nav (TOC or editor attachments). The extra nav is hidden below `xl` breakpoint (1200px).

**Issues:**
- The TOC panel is `position: fixed` with `max-height: 95%` and hidden scrollbars. If the TOC is longer than the viewport, the user cannot scroll it (scrollbar is invisible and `scrollbar-width: none` is set). This silently clips content.
- The TOC disappears entirely on screens narrower than 1200px. There is no fallback. Many laptop screens are 1366px or 1440px wide, which means the TOC is visible, but on a 13" MacBook at default scaling (effective ~1280px wide minus sidebar width), the content area is quite narrow with the sidebar + TOC both consuming space. On screens under 1200px, users lose the TOC entirely with no alternative way to see the page outline.
- There is no "back to top" affordance on long pages.

**Recommendations:**
- Make the TOC scrollable by restoring a thin scrollbar (or at minimum, show it on hover).
- Provide a collapsible TOC at the top of the page for screens under 1200px. A `<details><summary>Table of Contents</summary>...</details>` element is zero-JS, accessible, and takes no space when collapsed.
- Add a subtle "back to top" floating button that appears after scrolling past the first viewport-height. This is especially useful on long wiki pages.

### 3.4 Page Action Discoverability

**Finding:** To access History, Blame, Attachments, Rename, or Delete for a page, the user must open the three-dot dropdown in the navbar. The Edit button is the only page action with a dedicated visible button.

**Issue:** The three-dot pattern works for overflow/secondary actions, but History and Attachments are primary actions on a wiki page. Burying them behind a menu adds a click and requires memorization.

**Recommendation:** Add a small secondary action bar below the breadcrumbs (or at the top of the page content area) with text links or small buttons: `History | Attachments | Source | Blame`. Keep Rename and Delete in the overflow dropdown since they are destructive/rare. This pattern is used by GitHub (tabs above repo content), MediaWiki (tabs above article), and Gitea (tabs above file content). It is the standard wiki convention and users will look for it.

---

## 4. Typography and Readability

### Finding

Base font size is set to `1.4rem` (the Halfmoon default using a 10px root scaling). Paragraph and list text renders at `1.6rem`. The heading scale runs from `3.6rem` (h1) down to `1.6rem` (h6). The font is the system stack with Roboto as a named fallback.

### Issues

- The `h6` size (`1.6rem`) is identical to body text size. There is no visual distinction between h6 and a paragraph. This makes the heading hierarchy ambiguous at the bottom end.
- `max-scale=1.0, user-scalable=0` in the viewport meta tag **disables pinch-to-zoom on mobile**. This is a significant accessibility violation (WCAG 1.4.4). Users with low vision rely on zoom.
- Line length is unconstrained. On wide screens (1920px+) with the sidebar open but no TOC, the content column can exceed 120 characters per line. Optimal reading width is 50-75 characters. Long lines measurably increase reading difficulty.

### Recommendations

- Remove `maximum-scale=1.0, user-scalable=0` from the viewport meta tag. This is the single highest-impact accessibility fix in this review.
- Set `max-width: 50rem` (or similar) on the `.content` or `.page` container to cap line length at readable widths.
- Give `h6` a distinct size (e.g., `1.4rem`) or, more pragmatically, use a different styling approach for h6 (bold + same size, or small caps) to distinguish it from body text.

---

## 5. The Editor

### 5.1 CodeMirror Version

**Finding:** The editor uses CodeMirror 4.x (legacy). CodeMirror 6 has been the recommended version since 2022. CM4 receives no updates.

**Concern:** CM4 lacks mobile input handling improvements, accessibility features, and performance optimizations present in CM6. The `inputStyle: 'contenteditable'` option in the configuration partially mitigates mobile issues but is a workaround, not a solution.

**Recommendation:** Plan a migration to CodeMirror 6. CM6 has a completely different API, so this is a non-trivial change, but it fixes real mobile editing bugs and significantly improves accessibility (screen reader support, ARIA roles). If migrating is not feasible short-term, at minimum update to the latest CM5 release (5.65.x), which is the maintained legacy branch.

### 5.2 Toolbar UX

**Finding:** The editor toolbar is 20+ icon-only buttons in the navbar, grouped by thin CSS dividers. All buttons are `hidden-sm-and-down`, meaning mobile users have zero access to formatting tools.

**Issues:**
- 20+ icons with no labels is a high cognitive load. Users must hover each icon to learn its function (via `title` attribute). On touch devices, `title` attributes are not surfaced.
- The entire toolbar vanishes on mobile. A mobile user editing a page can only type raw markdown. They cannot insert a link, image, table, heading, bold, or any other formatted element without memorizing markdown syntax.
- There is no visual feedback for toggle states (bold, italic, code). The user does not know if the cursor is inside a bold region.

**Recommendations:**
- Move the toolbar from the navbar into a dedicated bar below the "Editing: PageName" heading, directly above the editor. This is the universal convention (VS Code, Google Docs, Notion, GitHub, every WYSIWYG and code editor). Placing it in the navbar is unusual and means it competes for space with site navigation.
- On mobile, show a single-row horizontally-scrollable toolbar with the most common actions (bold, italic, link, heading, list, code). This is the pattern used by GitHub's mobile editor and StackEdit.
- For less common actions (table, diagram, footnote, collapsible section, alerts), use a "+" or "..." overflow button that opens a menu.
- Add active state styling to toggle buttons when the cursor is in a matching context (bold, italic, code). CM4's `getState()` API supports this -- `gopherwiki_editor._getState()` already exists but is not used for toolbar state.

### 5.3 Save Flow

**Finding:** Clicking Save opens a modal dialog requiring a commit message before submission. The commit message field is `required`.

**Issues:**
- Every save requires a commit message. For frequent small edits (fixing a typo, adding a sentence), this creates friction. In practice, users will type "update" or "." to dismiss the modal as fast as possible, making the commit history useless.
- The modal uses `onclick="gopherwiki.toggleModal('modal-commit')"` for both open and close. If the user accidentally clicks the Save button twice quickly, the modal opens and immediately closes (or vice versa). This is a toggle anti-pattern on confirmation dialogs.

**Recommendations:**
- Pre-fill the commit message with an auto-generated default (e.g., "Update PageName") and make it editable but not required. This way, quick saves work with one click (Tab + Enter or just Enter), and users who want descriptive messages can still write them. This is the convention in most wiki engines (MediaWiki, Gitea wiki, Notion's version history).
- Replace the toggle behavior with explicit open/close handlers. The open handler should focus the commit message input for keyboard efficiency.

### 5.4 Draft Management

**Finding:** Drafts auto-save every 30 seconds (with a 5-second debounce on changes). On editor load, if a draft exists, a `confirm()` dialog asks whether to restore it.

**Issues:**
- The browser-native `confirm()` dialog is jarring and cannot be styled. It blocks the page and provides no context (e.g., when the draft was last saved, how it differs from the current version).
- If the user clicks "Cancel" on the confirm dialog, the draft is deleted immediately. There is no way to recover it after this point. The user may not have understood that "Cancel" means "delete my draft permanently."

**Recommendations:**
- Replace the `confirm()` dialog with an inline banner at the top of the editor: "A draft from [time] was found. [Restore] [Discard]". This is non-blocking, styled consistently with the app, and gives the user context.
- On "Discard," do not delete the draft immediately. Instead, keep it for the duration of the editing session and show a small "Undo" toast. Delete the draft only when the user saves or leaves the page.

### 5.5 Preview

**Finding:** Preview replaces the editor content area with rendered HTML. The user clicks "Preview" (eye icon) to toggle, and "Edit" (pencil icon) to return.

**Issue:** On anything wider than a phone screen, side-by-side preview is significantly more useful than toggle-based preview. The user can see their markdown and the result simultaneously, which reduces the edit-preview-edit cycle from three clicks to zero.

**Recommendation:** On screens wider than ~900px, offer an optional split-pane mode (editor left, preview right) in addition to the toggle. On narrow screens, keep the toggle. This is the pattern used by GitHub's markdown editor, HackMD, and StackEdit.

---

## 6. Search

### Finding

Search uses HTMX with a 300ms debounce for live results. The full search page has a text input, a submit button, and a results area. The navbar also has a search input.

### Issues

- The navbar search input and the search page input are separate. Typing in the navbar search and pressing Enter navigates to the search page, but the search page has its own input that drives the live HTMX results. If the user types in the navbar, they see no live results -- they must navigate first, then the search page loads with their query. This is two different search experiences depending on where the user starts.
- The search results show page name, snippet, and path. But there is no way to search within the current results (filter/refine) or to know which pages were recently modified. Adding a "Last modified: X ago" line to each result would help users find the right version of information.
- The "Supports FTS5 search syntax" help text is opaque to non-technical users. Most wiki users do not know what FTS5 is.

### Recommendations

- Unify the search experience: make the navbar search input also use HTMX to show a dropdown of 5-8 quick results below the input, similar to GitHub's command palette or Slack's search bar. This saves a full page navigation for the common case of "I know roughly what I'm looking for."
- Replace the FTS5 help text with something actionable: "Use quotes for exact phrases. Prefix with `-` to exclude terms."
- Consider adding a "last modified" timestamp to search results.

---

## 7. Issue Tracker

### Finding

The issue tracker supports status filtering (open/closed), category filtering, tag filtering, and displays issues in a list with status icons, title, metadata, and tags.

### Issues

- There is no pagination. If the wiki accumulates hundreds of issues, the page will become very long. The `issue-list` renders all matching issues in a single DOM.
- The issue view page uses `confirm()` dialogs for delete actions (both issue and comment deletion). As with the editor draft dialog, these are jarring and unstyled.
- Creating a new issue navigates to a separate page (`/-/issues/new`). For a lightweight issue tracker, an inline form (expand in place) would reduce context switching.
- There is no way to sort issues (by date, by title, by recent activity). The default appears to be creation order.

### Recommendations

- Add pagination or "load more" behavior after 50 issues per page.
- Replace `confirm()` with inline confirmation patterns (e.g., "Are you sure? [Yes, delete] [Cancel]" that replaces the delete button temporarily). This is less disruptive and stylable.
- Add sort controls (newest, oldest, recently updated) to the issue list header.
- Adding inline issue creation is optional but would meaningfully reduce friction for quick bug reports.

---

## 8. History and Diff

### Finding

The history page shows a table with radio buttons for selecting two revisions to compare. The diff page shows a unified diff with color-coded lines.

### Issues

- The history table has two radio button columns (rev_a, rev_b) with no column headers explaining what each column means. A new user has to guess that the left column is "from" and the right column is "to."
- The radio buttons are small, untouched by custom CSS, and hard to tap on mobile.
- The diff view uses inline CSS (`style="color: green"`, etc. via template rendering) rather than the CSS classes defined in `history.css`. This means the diff colors do not adapt to dark mode unless the inline styles happen to work in both themes.

### Recommendations

- Add column headers to the radio button columns: "From" and "To" (or "Old" and "New").
- Increase the tap target for radio buttons on mobile. Wrap each radio in a `<label>` that spans the entire cell.
- Ensure diff coloring uses CSS classes exclusively (the `diff-add`, `diff-remove`, `diff-header` classes already exist in `history.css`) so dark mode works correctly.

---

## 9. Mobile Experience

### Findings Across All Pages

- The `user-scalable=0` viewport restriction disables zoom (addressed in Section 4).
- The editor toolbar is completely hidden on mobile (addressed in Section 5.2).
- The sidebar requires a hamburger tap to access any navigation link.
- Tables (history, blame, attachments, admin users) have no explicit responsive handling. They overflow horizontally, which is acceptable but not ideal.
- The search input is visible on mobile in the navbar, which is good.
- Form inputs and buttons use the default Halfmoon sizing. Tap targets appear adequate (~44px) on most controls.
- The `position: fixed` extra-nav (TOC) is hidden on mobile with no replacement.

### Summary Recommendation

The mobile experience is functional but minimal. The two highest-impact improvements are:
1. Restore pinch-to-zoom (remove `user-scalable=0`).
2. Provide a mobile editor toolbar (even a minimal one).

Beyond those, adding a bottom navigation bar and a collapsible TOC for narrow screens would meaningfully improve mobile usability without adding complexity.

---

## 10. Accessibility

### Positive Patterns Already Present

- Semantic HTML: `<nav>`, `<form>`, `<table>`, `<ol>` used appropriately.
- `aria-label="breadcrumb"` on breadcrumb navigation.
- `aria-labelledby` on dropdown menus.
- Color + text for status indicators (issue open/closed uses icon + color, not color alone).
- Form labels are present on most inputs.
- `role="alert"` on error/flash messages.

### Gaps

| Issue | Severity | Location |
|-------|----------|----------|
| `user-scalable=0` disables zoom | High | `base.html` meta viewport |
| Search input has no `aria-label` | Medium | `base.html` navbar search |
| Editor toolbar buttons are icon-only with no `aria-label` (only `title`) | Medium | `editor.html` toolbar |
| `confirm()` dialogs cannot be controlled by assistive technology | Low | Various delete actions |
| Skip-to-content link is missing | Medium | `base.html` |
| Focus management after HTMX content swap is absent | Low | Search results |
| No `aria-current="page"` on active sidebar links | Low | `base.html` sidebar |

### Recommendations (Priority Order)

1. Remove `user-scalable=0` from the viewport meta tag.
2. Add a skip-to-content link as the first focusable element in `<body>`: `<a href="#column-main" class="sr-only sr-only-focusable">Skip to content</a>`.
3. Add `aria-label` to the search input and all icon-only buttons.
4. Add `aria-current="page"` to the active sidebar link.
5. After HTMX swaps search results, set focus to the results region (use `htmx:afterSwap` event).

---

## 11. Performance Considerations Affecting UX

### Asset Weight

| Asset | Size | Loaded |
|-------|------|--------|
| Mermaid 11.6.0 | ~2.5MB | Conditionally (pages with diagrams) |
| CodeMirror 4.x + modes | ~650KB | Editor only |
| Halfmoon CSS + variables | ~200KB | Every page |
| Font Awesome (all) | ~200KB (woff2) | Every page |
| Polyfills (IE) | ~170KB | Every page |
| HTMX | ~50KB | Every page |
| Halfmoon JS | ~11KB | Every page |

**Key issue:** Font Awesome loads the entire icon set (~1,600 icons) for a project that uses approximately 25-30 distinct icons. This is ~180KB of wasted font data on every page load.

### Recommendations

- Remove polyfills (~170KB saved).
- Subset Font Awesome to only the icons used, or switch to inline SVG icons (zero additional requests, smaller total payload, better rendering). Tools like `fontawesome-subset` or manual SVG extraction make this straightforward.
- Mermaid at 2.5MB is large but loaded conditionally -- this is already the right approach.
- Consider lazy-loading Halfmoon JS since most of its functionality (modal, sidebar toggle) is only needed after user interaction.

---

## 12. Interaction Framework Assessment

### Current Stack

- **Halfmoon.js** for sidebar toggle, dark mode toggle, modals
- **HTMX 2.0.6** for live search
- **Vanilla JS** for editor behavior, draft management, preview, keyboard shortcuts, Sortable.js for table editing

### Assessment

HTMX is underutilized. It is loaded on every page but only used on the search page. There are several interactions that would benefit from HTMX's declarative approach:

- **Issue close/reopen**: Currently a full page POST + redirect. Could be an `hx-post` that swaps the status badge and button in place.
- **Issue comment submission**: Currently a full page POST + redirect + scroll to anchor. Could be an `hx-post` that appends the new comment to the list.
- **Page delete confirmation**: Currently navigates to a separate confirmation page. Could be a modal or inline confirmation loaded via `hx-get`.
- **Attachment upload**: Currently a full page POST + redirect. Could show upload progress and append the new file to the list.
- **Pagination** (if added): Natural fit for `hx-get` with `hx-swap="innerHTML"`.

### Recommendation

Expand HTMX usage incrementally to reduce full-page reloads for common actions. The infrastructure is already in place (HTMX is loaded, the partial-rendering pattern exists for search). Each incremental adoption is a small, low-risk change. Prioritize issue close/reopen and comment submission as the highest-friction full-page-reload interactions.

The vanilla JS for the editor is appropriate -- HTMX is not the right tool for rich text editing.

---

## 13. Quick Wins (Minimal Effort, High Impact)

These changes are small, self-contained, and immediately improve usability:

1. **Remove `user-scalable=0`** from viewport meta tag. One line change. Fixes a real accessibility barrier.
2. **Add `aria-label="Search wiki"` to the search input.** One attribute.
3. **Pre-fill commit message** with "Update PageName" in the editor save modal. Reduces friction on every save.
4. **Add column headers "From" / "To"** to the history comparison radio buttons. Two `<th>` elements.
5. **Add `max-width: 50rem`** to `.page` or `.content` to cap line length. One CSS rule.
6. **Replace "Supports FTS5 search syntax"** with user-friendly text.
7. **Remove polyfill JS files** from `base.html`. Delete two `<script>` tags (if they exist; they may only be referenced from specific pages).

---

## 14. Summary of Priorities

| Priority | Area | Effort | Impact |
|----------|------|--------|--------|
| 1 | Remove `user-scalable=0` | Trivial | High (accessibility) |
| 2 | Cap content line length | Trivial | High (readability) |
| 3 | Add page action bar below breadcrumbs | Low | High (discoverability) |
| 4 | Mobile editor toolbar | Medium | High (mobile editing) |
| 5 | Navbar search dropdown (HTMX) | Medium | Medium (search speed) |
| 6 | Pre-fill commit message | Low | Medium (editor friction) |
| 7 | Strip IE polyfills + subset Font Awesome | Low | Medium (performance) |
| 8 | Split-pane editor preview | Medium | Medium (editor UX) |
| 9 | Expand HTMX for issues | Medium | Medium (interaction speed) |
| 10 | Replace `confirm()` with inline patterns | Low | Low-Medium (polish) |
| 11 | Migrate from Halfmoon | High | Medium (long-term maintainability) |
| 12 | Migrate to CodeMirror 6 | High | Medium (mobile editing, a11y) |
