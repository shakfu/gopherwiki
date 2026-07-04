#!/usr/bin/env bash
#
# mirror-ojs-libs.sh -- download the Observable JS libraries GopherWiki's OJS
# pages need, into a local mirror directory, for offline / air-gapped operation.
#
# Point the server at the result with OJS_LIBS_DIR=<dir>; rendered OJS pages then
# load their libraries from the wiki (/ojs-libs/...) instead of the Observable
# CDNs, and the rendered-output CSP drops the CDN allowance.
#
# Usage:   scripts/mirror-ojs-libs.sh <mirror-dir>
# Example: scripts/mirror-ojs-libs.sh ./ojs-libs && OJS_LIBS_DIR=$PWD/ojs-libs ...
#
# IMPORTANT: the exact library VERSIONS are pinned by the Quarto version you
# render with, and the SET depends on which Observable features your pages use
# (Inputs, Plot, d3, ...). The list below is the common baseline for Quarto
# 1.9.x. To capture the precise set your wiki needs, render your OJS pages once
# with CDN access and note the requested https://cdn.jsdelivr.net/npm/... URLs
# (browser devtools Network tab), then add any missing ones here.

set -euo pipefail

dir="${1:?usage: mirror-ojs-libs.sh <mirror-dir>}"

# jsDelivr npm packages the Observable runtime loads on demand (Quarto 1.9.x).
urls=(
  "https://cdn.jsdelivr.net/npm/@observablehq/inputs@0.10.6/dist/inputs.min.js"
  "https://cdn.jsdelivr.net/npm/@observablehq/plot@0.6.11/dist/plot.umd.min.js"
  "https://cdn.jsdelivr.net/npm/d3@7.8.5/dist/d3.min.js"
  "https://cdn.jsdelivr.net/npm/htl@0.3.1/dist/htl.min.js"
  "https://cdn.jsdelivr.net/npm/marked@0.3.12/marked.min.js"
)

for u in "${urls[@]}"; do
  # https://cdn.jsdelivr.net/<rest> -> <dir>/jsdelivr/<rest>
  rest="${u#https://cdn.jsdelivr.net/}"
  dest="$dir/jsdelivr/$rest"
  mkdir -p "$(dirname "$dest")"
  echo "  fetching $rest"
  curl -fsSL -o "$dest" "$u"
done

echo "Mirror written to: $dir"
echo "Run the server with: OJS_LIBS_DIR=$(cd "$dir" && pwd)"
