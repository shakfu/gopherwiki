#!/usr/bin/env bash
#
# Seed demonstration computational pages (.qmd) into a wiki repository.
#
# Usage: scripts/seed-demo-pages.sh [repo-dir]
#
# Creates the repo (git init) if absent and writes one .qmd page per execution
# tier. Existing files are never overwritten, so local edits survive re-runs.
# Pages are left in the working tree uncommitted; the wiki reads the working
# tree, and saving a page from the UI commits it.

set -euo pipefail

REPO="${1:-build/test-repo}"

if [ ! -d "$REPO/.git" ]; then
    mkdir -p "$REPO"
    git -C "$REPO" init -q
    echo "initialized git repo at $REPO"
fi

write_page() {
    local name="$1"
    if [ -e "$REPO/$name" ]; then
        echo "skip    $REPO/$name (exists)"
        return
    fi
    cat > "$REPO/$name"
    echo "created $REPO/$name"
}

write_page compute-plain.qmd <<'EOF'
---
title: Plain Quarto Page
---

# Plain Quarto Page

No code cells, so this page needs no language runtime. It exercises the render
pipeline itself: Quarto produces self-contained HTML that the wiki caches and
serves inside an iframe.

Math is Pandoc-native: $E = mc^2$.

Use **Render** in the page menu to (re)generate the output, and **Export as**
to download PDF, DOCX, EPUB, HTML, GFM, or a Markdown ZIP.
EOF

write_page compute-python.qmd <<'EOF'
---
title: Python Computation
---

# Python Computation

Requires Jupyter on the render host. `make dev-compute` pins the interpreter to
the project virtualenv via `RENDER_PYTHON` when `.venv/bin/python` exists.

```{python}
total = sum(i**2 for i in range(1, 11))
print(f"Sum of the first ten squares: {total}")
```

```{python}
#| echo: false
import platform
print(f"Executed by Python {platform.python_version()}")
```
EOF

write_page compute-ojs.qmd <<'EOF'
---
title: Observable JS
---

# Observable JS

Observable cells run in the reader's browser, so this page needs no server-side
language runtime. Move the slider and the value below updates reactively.

```{ojs}
viewof n = Inputs.range([0, 100], {value: 8, step: 1, label: "n"})
```

```{ojs}
md`**${n}** squared is **${n * n}**.`
```

By default the Observable runtime pulls its standard library from the
Observable/jsDelivr CDNs at view time. For offline use, mirror them with
`scripts/mirror-ojs-libs.sh <dir>` and set `OJS_LIBS_DIR`.
EOF

echo
echo "demo pages seeded in $REPO"
