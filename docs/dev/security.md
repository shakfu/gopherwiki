# Security review: `mcp` branch

Review of `mcp` against `main` at `b89f017`, 2026-10-01. Each finding was reproduced in a test or confirmed from the code. All three broke guarantees the branch adds: token write rules, the review gate, and `HIDE_UNVALIDATED`. All three are fixed on `mcp`, each with a test that fails on the old code.

For the computational pages threat model, see `docs/security.md`.

## 1. `..` in a page path skips the commit, the agent mark and the revision check

Severity: High. Status: fixed. Storage refuses non-canonical names, and the API answers 400 for a path with an empty, `.` or `..` segment. Tests: `TestNonCanonicalPathRejected`, `TestAPIToken_NonCanonicalPath`, `TestValidPagepath`.

`util.SanitizePagename` keeps `..` segments, so `page.Filename` stays unclean, such as `zz/../kb/sneaky.md`. Each layer then treats it differently:

- `underPrefix` (`internal/handlers/api_pages.go:289`) cleans the path, so the prefix check passes.
- The file is written at the clean path, `kb/sneaky.md`.
- `fileChanged` (`internal/storage/git.go:282`) looks up the unclean name in `worktree.Status()`, which is keyed by clean paths. It finds nothing, so no commit is made and the file stays in the worktree.
- The metadata lookup fails, so the conflict check is skipped. Any non-empty `revision`, such as `"x"`, passes.
- `MarkAgentPage` stores the unclean name, so `IsAgentPage` on the clean name is false. The page is never hidden and never needs validation.

Reachable through `PUT /-/api/v1/pages/...`, `POST /-/api/v1/batch` and the MCP `write_pages` tool. The write stays inside the token's prefix.

Exploit: a token with prefix `kb` sends `write_pages` with path `zz/../kb/august` and revision `"x"`. The validated page shows the new content under its "validated by" banner, because validation reads the last commit and the view reads the worktree. There is no history and no revert. A new page written this way is visible to anonymous readers with `HIDE_UNVALIDATED=true`.

Fix:

- Reject paths with an empty, `.` or `..` segment at the API boundary, or in `SanitizePagename`, before `NewPage`.
- Use one canonical filename for the prefix check, storage, the commit and `MarkAgentPage`.
- In `StoreBytes` and `StoreFiles`, clean the name before the status lookup. Treat "written but not in status" as an error and restore the file.

The web save path shares this storage code, so a session writer likely also gets an uncommitted write. That is a regression from `fileChanged`; on `main` the write was committed.

## 2. Page diff shows hidden agent pages

Severity: Medium. Status: fixed. The diff covers only the page's own file. Test: `TestHideUnvalidated_Diff`.

`handleDiff` (`internal/handlers/page_handlers.go:516`) passes the caller's `rev_a` and `rev_b` to `GitStorage.Diff` (`internal/storage/git.go:680`). That diffs the whole tree and returns every changed file. `dispatchPage` checks only the page in the URL. The branch filters hidden commits from the changelog, feeds and `/-/commit/{rev}` with `commitHidden`, but not from the diff.

Exploit: with `HIDE_UNVALIDATED=true`, any reader, anonymous if read access allows, requests `/home/diff?rev_a=HEAD~1&rev_b=HEAD` and steps back through `HEAD~N`. The response contains unvalidated agent pages.

Fix: limit the diff to `page.Filename`. While hiding is active, answer 404 when either revision is hidden by `commitHidden`.

The whole-tree diff exists on `main`. It leaks only because the branch adds hidden pages.

## 3. API serves hidden pages whose path contains `/runs/`

Severity: Medium. Status: fixed. The API resolves the sub-resource before the hidden check. Test: `TestHideUnvalidated_RunsSegment`, which also confirmed the `/history` and `/backlinks` variants on the old code.

The hidden check (`internal/handlers/api_pages.go:52`) cuts the path at the last `/runs/` whatever follows. Dispatch (`api_pages.go:77`) treats the suffix as a run ID only when it parses as an integer; otherwise it serves the full path. For `kb/runs/notes` it checks `kb`, which does not exist and so is not hidden, then serves `kb/runs/notes`.

Exploit: an agent writes `kb/runs/notes`. With `HIDE_UNVALIDATED=true`, anonymous `GET /kb/runs/notes` returns 404, but `GET /-/api/v1/pages/kb/runs/notes` returns 200 with the content.

Fix: pick the sub-resource first, treating the suffix as a run only when `ParseInt` succeeds. Then run `pageHidden` on the exact path the chosen handler loads.

## Checked, no finding

- MCP `apiCall`: `apiPath` escapes `?` and `#`, and every tool stays on `/-/api/v1/pages/*`. The inner request runs all middleware again.
- MCP CSRF: cookie-authenticated POSTs need a CSRF token. The `Origin == Host` allowance adds nothing a DNS-rebinding page could not already reach anonymously.
- Tokens:
  - Tokens are 32 random bytes, stored as SHA-256. SQL queries are parameterised.
  - Admin, review and delete are refused for tokens.
  - Tokens are ignored outside `/-/api/`.
- Templates: `html/template` escapes all new output. Report-run HTML uses the existing rendered-output CSP.
- Validate and approve require review permission, a CSRF token, and the revision or hash the reviewer saw.

Minor, not fixed: `GET /-/commit/{rev}/revert` shows metadata of hidden commits to writers. `sources` on a visible page names a hidden report page and its period.
