# GopherWiki

GopherWiki is a wiki for collaborative content management. Content is stored in a Git repository, keeping track of all changes. [Markdown](https://daringfireball.net/projects/markdown) is used as the markup language.

GopherWiki is written in [Go](https://go.dev/) using [Chi](https://github.com/go-chi/chi) for routing, [goldmark](https://github.com/yuin/goldmark) for Markdown rendering, and [go-git](https://github.com/go-git/go-git) for version control. It compiles to a single binary with embedded assets for easy deployment.

This project is a Go translation of [An Otter Wiki](https://github.com/redimp/otterwiki), maintaining feature parity while leveraging Go's single-binary deployment advantage.

## Features

- Minimalistic interface with dark mode
- Markdown editor with syntax highlighting and table support
- Customizable sidebar with menu and page index
- Live search dropdown in the navbar with HTMX
- Full changelog and page history with diff view
- User authentication with configurable access control
- Page attachments with image thumbnails
- Extended Markdown: tables, footnotes, alerts, mermaid diagrams, syntax highlighting
- Issue tracker with comments and discussion threads
- Draft autosave
- RSS/Atom feeds
- JSON API (`/-/api/v1/`) for pages, search, changelog, and issues
- Single binary deployment

## Installation

### Using Go

```bash
# Clone the repository
git clone https://github.com/yourusername/gopherwiki.git
cd gopherwiki

# Build
make build

# Set required environment variables and run
export SECRET_KEY=$(openssl rand -hex 32)
export REPOSITORY="./repository"
./gopherwiki
```

Or run directly with `go run`:

```bash
SECRET_KEY="your-secret-key-at-least-16-chars" REPOSITORY="/tmp/wiki-repo" go run ./cmd/gopherwiki
```

The `REPOSITORY` directory will be initialized as a Git repository if it doesn't exist.

### Using Docker

```bash
docker-compose up -d
```

Access the wiki at http://localhost:8080

### docker-compose.yml

```yaml
services:
  web:
    build: .
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./app-data:/app-data
    environment:
      - SECRET_KEY=your-secure-random-key
      - SITE_NAME=GopherWiki
      - SITE_URL=http://localhost:8080
      - READ_ACCESS=ANONYMOUS
      - WRITE_ACCESS=REGISTERED
```

## Configuration

GopherWiki can be configured via a YAML config file, environment variables, or command-line flags. When multiple sources set the same value, the precedence is: **defaults < config file < environment variables < CLI flags**.

| Variable | Default | Description |
|----------|---------|-------------|
| `SECRET_KEY` | (required) | Secret key for session encryption. Generate with `openssl rand -base64 32` |
| `SITE_NAME` | GopherWiki | Name displayed in the header |
| `SITE_URL` | http://localhost:8080 | Public URL for feeds and sitemap |
| `HOME_PAGE` | Home | Default landing page |
| `REPOSITORY` | ./repository | Path to Git repository |
| `DATABASE_URI` | sqlite://gopherwiki.db | SQLite database path |
| `READ_ACCESS` | ANONYMOUS | Who can read: ANONYMOUS, REGISTERED, or APPROVED |
| `WRITE_ACCESS` | REGISTERED | Who can write: ANONYMOUS, REGISTERED, or APPROVED |
| `ATTACHMENT_ACCESS` | REGISTERED | Who can upload: ANONYMOUS, REGISTERED, or APPROVED |
| `AUTO_APPROVAL` | true | Auto-approve new registrations |
| `DISABLE_REGISTRATION` | false | Disable new user registration |
| `DEV_MODE` | false | Relaxes secret key validation for local development |

### Config File

Pass a YAML config file with `-config` or the `CONFIG_FILE` environment variable:

```bash
gopherwiki -config /etc/gopherwiki/config.yml
```

Example `config.yml`:

```yaml
# Server
port: 8080
host: "0.0.0.0"
base_url: "https://wiki.example.com"
dev_mode: false

# Storage
repository_path: "/var/lib/gopherwiki/repo"
database_path: "sqlite:///var/lib/gopherwiki/wiki.db"

# Auth
session_secret: "your-secret-key-at-least-16-chars"
auth_method: ""
registration_enabled: true
auto_approval: true

# Permissions
read_access: "ANONYMOUS"
write_access: "REGISTERED"
attachment_access: "REGISTERED"

# Wiki
site_name: "My Wiki"
landing_page: "Home"
site_lang: "en"

# Logging
log_level: "INFO"
log_format: "text"
```

Only the fields you want to override need to be present -- omitted fields keep their defaults. Environment variables and CLI flags still override any values set in the file.

### Generating a Secret Key

```bash
# Using OpenSSL
openssl rand -base64 32

# Using Go
go run -e 'import "crypto/rand"; import "encoding/base64"; b := make([]byte, 32); rand.Read(b); println(base64.StdEncoding.EncodeToString(b))'
```

### Command-Line Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-config` | | Path to YAML configuration file |
| `-host` | (all interfaces) | Host/IP to bind to |
| `-port` | 8080 | HTTP server port |
| `-repo` | | Path to wiki Git repository |
| `-db` | | Path to SQLite database file |
| `-templates` | | Path to templates directory (overrides embedded) |
| `-static` | | Path to static files directory (overrides embedded) |
| `-init` | | Path to initialization JSON file (run once to set up site) |

## Development

```bash
# Run tests
make test

# Build
make build

# Run in development mode (localhost only, DEV_MODE=1)
make dev

# Run with default settings (all interfaces)
make run
```

## Technology Stack

- **Web Framework**: [Chi](https://github.com/go-chi/chi) - lightweight, idiomatic router
- **Markdown**: [goldmark](https://github.com/yuin/goldmark) - CommonMark compliant with extensions
- **Syntax Highlighting**: [Chroma](https://github.com/alecthomas/chroma) - pure Go highlighter
- **Git**: [go-git](https://github.com/go-git/go-git) - pure Go Git implementation
- **Database**: SQLite via [go-sqlite3](https://github.com/mattn/go-sqlite3)
- **Sessions**: [gorilla/sessions](https://github.com/gorilla/sessions)
- **Templates**: Go html/template with embedded assets

## License

GopherWiki is open-source software licensed under the MIT License.
