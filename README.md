# GopherWiki

GopherWiki is a wiki that stores its pages as Markdown files in a Git repository. Every edit is a commit. It compiles to a single Go binary with embedded assets.

It started as a Go port of [Otter Wiki](https://github.com/redimp/otterwiki).

## Features

- Markdown editor with preview and draft autosave

- Wikilinks, tables, footnotes, alerts, Mermaid diagrams, MathJax, syntax highlighting

- Page history, diff, blame and revert

- Full-text search, page index, sidebar page tree, backlinks

- Attachments with image thumbnails

- Issue tracker with comments

- Access control per action, user approval, reviewer role

- RSS/Atom feeds and sitemap

- JSON API (`/-/api/v1/`) with bearer tokens scoped to a write prefix

- MCP server (`/-/api/v1/mcp`) for AI agents, with human validation of agent-written pages and a lint report

- Optional computational pages: `.qmd` pages rendered by Quarto, with Python, R and Observable JS, and stored monthly report runs

- Optional export to PDF, HTML, DOCX, EPUB and GFM; Markdown ZIP always

## Quick start

```bash
make build
SECRET_KEY=$(openssl rand -hex 32) REPOSITORY=./repository ./bin/gopherwiki
```

Open <http://localhost:8080> and register. The first user becomes an admin.

With Docker:

```bash
SECRET_KEY=$(openssl rand -hex 32) docker compose up -d
```

## Connecting an agent

An admin creates a token at `/-/admin/tokens`. Then, for Claude Code:

```bash
claude mcp add --transport http gopherwiki https://wiki.example.com/-/api/v1/mcp \
  --header "Authorization: Bearer gw_..."
```

See the agent section of the admin guide before connecting one.

## Documentation

| Document | Audience |
|-|-|
| [User guide](docs/user-guide.md) | Readers, editors, reviewers |
| [Admin guide](docs/admin-guide.md) | Operators: install, configuration, users, agents |
| [API](docs/API.md) | JSON API and MCP reference |
| [Developer guide](docs/dev/dev-guide.md) | Building, testing, testing MCP |
| [Computational pages](docs/computational-pages.md) | Quarto integration design |
| [Security](docs/security.md) | Hardening for computational pages |
| [LLM wiki design](docs/dev/llm-wiki.md) | Design of the agent features |
| [Changelog](CHANGELOG.md) | Changes by release |

## Development

```bash
make test      # run all tests
make dev       # run from source on 127.0.0.1:8080 with DEV_MODE
make mcp-demo  # run a seeded demo wiki for testing MCP with an agent
```

## Technology

[Chi](https://github.com/go-chi/chi) routing, [goldmark](https://github.com/yuin/goldmark) Markdown, [Chroma](https://github.com/alecthomas/chroma) highlighting, [go-git](https://github.com/go-git/go-git) storage, SQLite via [go-sqlite3](https://github.com/mattn/go-sqlite3), [gorilla/sessions](https://github.com/gorilla/sessions), Go `html/template`.

## License

MIT.
