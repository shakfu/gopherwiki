#!/usr/bin/env bash
# Seed a demo wiki for testing the MCP server with an agent such as Claude Code.
#
# Usage: scripts/seed-mcp-demo.sh BINARY DIR PORT
#
# Creates DIR/repo (pages), DIR/repo/.wiki.db (users, report runs, agent pages)
# and DIR/token (the agent's API token). It starts BINARY on PORT briefly so
# the schema is migrated and the agent pages are written through the real MCP
# endpoint. Does nothing if DIR/repo exists; delete DIR to reseed.
set -euo pipefail

bin=${1:?usage: seed-mcp-demo.sh BINARY DIR PORT}
dir=${2:?usage: seed-mcp-demo.sh BINARY DIR PORT}
port=${3:?usage: seed-mcp-demo.sh BINARY DIR PORT}
repo=$dir/repo
db=$repo/.wiki.db
here=$(cd "$(dirname "$0")" && pwd)

if [ -d "$repo" ]; then
	echo "$repo exists; delete $dir to reseed"
	exit 0
fi
for cmd in git sqlite3 curl jq shasum openssl; do
	command -v $cmd >/dev/null || { echo "$cmd not found" >&2; exit 1; }
done

mkdir -p "$repo"
git -C "$repo" init -q
commit() {
	git -C "$repo" add -A
	git -C "$repo" -c user.name="Demo Editor" -c user.email=editor@demo.local commit -q -m "$1"
}
page() {
	mkdir -p "$(dirname "$repo/$1")"
	cat >"$repo/$1"
}

# --- Human-written pages -------------------------------------------------------

page home.md <<'EOF'
# Northwind Coffee Roasters

Internal wiki of a small coffee roaster with three sales regions.

- Products: [[Products/Espresso Blend]], [[Products/Single Origin]], [[Products/Cold Brew]]
- Team: [[Team/Sales]]
- Monthly sales report: [[Reports/Sales]]
- Agent analyses live under `analysis/`. See [[Meta/Schema]] for the rules.
EOF

page meta/schema.md <<'EOF'
# Wiki conventions for agents

## Where to write

- Write monthly analyses at `analysis/YYYY-MM`, for example `analysis/2026-08`.
- Write multi-month analyses at `analysis/trend-YYYY-MM-to-YYYY-MM`.
- Never edit product, team or report pages. Open an issue instead.

## Page layout

Each analysis has this frontmatter and these sections:

    ---
    title: Sales analysis, August 2026
    sources:
      - page: reports/sales
        run: <id>
    ---

    ## Summary
    ## Regions
    ## Risks

- Cite the newest run of each period you use. `list_runs` returns runs newest first.
- Copy figures exactly as they appear in the run, including thousands separators.
- Do not compute new figures. If a change matters, describe it in words.
- Keep each analysis under 300 words.
- Link pages with double-bracket wikilinks, as `home` does. Lint and backlinks ignore Markdown links.

## Commits

- Save all pages of one task in a single `write_pages` call, so the task is one commit.
EOF

page products/espresso-blend.md <<'EOF'
# Espresso Blend

House espresso. Brazil and Ethiopia, medium-dark roast.

| Size | Price (EUR) |
|-|-|
| 250 g | 9.50 |
| 1 kg | 32.00 |

Best seller in [[Team/Sales|all regions]].
EOF

page products/single-origin.md <<'EOF'
# Single Origin

Rotating single-origin lot. Currently Kenya Nyeri AA, light roast.

| Size | Price (EUR) |
|-|-|
| 250 g | 12.00 |
EOF

page products/cold-brew.md <<'EOF'
# Cold Brew

Bottled cold brew, 330 ml. Sold from May to September.

Price: 3.20 EUR per bottle. Wholesale price: 1.90 EUR.

See the [[Seasonal Menu]] for availability.
EOF

page team/sales.md <<'EOF'
# Sales team

| Region | Lead |
|-|-|
| North | Ana |
| South | Ben |
| West | Chloe |

Monthly figures are in [[Reports/Sales]].
EOF

page team/onboarding.md <<'EOF'
# Onboarding

Checklist for new staff. No page links here yet.
EOF

page reports/sales.qmd <<'EOF'
---
title: Monthly sales
params:
  period: "2026-08"
---

Revenue and units by region for the period `{python} params["period"]`.

```{python}
import pandas as pd
import sqlite3

conn = sqlite3.connect("/srv/reporting/sales.db")
df = pd.read_sql(
    "SELECT region, SUM(revenue) AS revenue, SUM(units) AS units "
    "FROM sales_view WHERE period = ? GROUP BY region",
    conn, params=[params["period"]])
df
```
EOF
commit "Seed Northwind demo pages"

# --- Server: migrate the schema and create the admin ---------------------------

cat >"$dir/init.json" <<'EOF'
{"admin": {"name": "Demo Admin", "email": "admin@demo.local", "password": "demo-password"},
 "site": {"name": "Northwind Wiki"}}
EOF

DEV_MODE=1 "$bin" -repo "$repo" -host 127.0.0.1 -port "$port" -init "$dir/init.json" >"$dir/seed.log" 2>&1 &
pid=$!
trap 'kill $pid 2>/dev/null || true' EXIT
for _ in $(seq 60); do
	curl -so /dev/null "http://127.0.0.1:$port/-/health" && break
	sleep 0.25
done
curl -so /dev/null "http://127.0.0.1:$port/-/health" || { echo "server did not start; see $dir/seed.log" >&2; exit 1; }

# --- Agent user and token ------------------------------------------------------

sqlite3 "$db" "INSERT INTO user (name, email, is_approved, email_confirmed, allow_read, allow_write, first_seen)
	VALUES ('Agent', 'agent@demo.local', 1, 1, 1, 1, datetime('now'));"
token=$("$here/dev-token.sh" "$db" analysis agent@demo.local)
echo "$token" >"$dir/token"

# --- Report runs ---------------------------------------------------------------
# Rendering needs Quarto and the reporting database, so the runs are inserted
# as if rendered. Run 3 restates run 2 (South corrected), so run 2 is stale.

hash=$(shasum -a 256 "$repo/reports/sales.qmd" | cut -d' ' -f1)
rev=$(git -C "$repo" rev-parse HEAD)
run() { # period, days ago, markdown
	local html="<!doctype html><meta charset=utf-8><title>Sales $1</title><body><h1>Monthly sales, $1</h1><pre>$3</pre>"
	sqlite3 "$db" "INSERT INTO report_runs (filename, period, source_hash, source_revision, html, markdown, run_by, run_at)
		VALUES ('reports/sales.qmd', '$1', '$hash', '$rev', CAST('$html' AS BLOB), '$3', 'admin@demo.local', datetime('now', '-$2 days'));"
}
run 2026-07 60 "Revenue and units by region for the period 2026-07.

| region | revenue | units |
|-|-|-|
| North | 182,400 | 4,560 |
| South | 143,900 | 3,598 |
| West | 97,300 | 2,433 |
| Total | 423,600 | 10,591 |"
run 2026-08 30 "Revenue and units by region for the period 2026-08.

| region | revenue | units |
|-|-|-|
| North | 191,200 | 4,780 |
| South | 138,500 | 3,463 |
| West | 104,800 | 2,620 |
| Total | 434,500 | 10,863 |"
run 2026-08 5 "Revenue and units by region for the period 2026-08.

| region | revenue | units |
|-|-|-|
| North | 191,200 | 4,780 |
| South | 141,700 | 3,543 |
| West | 104,800 | 2,620 |
| Total | 437,700 | 10,943 |"

# --- Agent pages, written through MCP ------------------------------------------
# analysis/2026-07 is clean. analysis/2026-08 cites the superseded run 2 and
# contains a derived percentage, so lint reports stale_source and
# unmatched_number.

cat >"$dir/2026-07.md" <<'EOF'
---
title: Sales analysis, July 2026
sources:
  - page: reports/sales
    run: 1
---

## Summary

Revenue was 423,600 on 10,591 units.

## Regions

North led with 182,400. South followed with 143,900 and West with 97,300.

## Risks

West remains the smallest region.
EOF
cat >"$dir/2026-08.md" <<'EOF'
---
title: Sales analysis, August 2026
sources:
  - page: reports/sales
    run: 2
---

## Summary

Revenue was 434,500 on 10,863 units, up 2.6% on July.

## Regions

North grew to 191,200. South fell to 138,500. West grew to 104,800.

## Risks

South declined for the second month.
EOF
args=$(jq -n --rawfile a "$dir/2026-07.md" --rawfile b "$dir/2026-08.md" \
	'{message: "Add July and August analyses", pages: [{path: "analysis/2026-07", content: $a}, {path: "analysis/2026-08", content: $b}]}')
body=$(jq -n --argjson args "$args" '{jsonrpc: "2.0", id: 1, method: "tools/call", params: {name: "write_pages", arguments: $args}}')
reply=$(curl -s "http://127.0.0.1:$port/-/api/v1/mcp" -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -d "$body")
[ "$(jq -r .result.isError <<<"$reply")" = false ] || { echo "writing agent pages failed: $reply" >&2; exit 1; }
rm -f "$dir/2026-07.md" "$dir/2026-08.md" "$dir/init.json"

echo "Seeded $dir"
