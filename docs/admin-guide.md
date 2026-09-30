# Admin guide

This guide covers installing, configuring and operating GopherWiki. For page editing, see `docs/user-guide.md`. For the JSON API and MCP protocol details, see `docs/API.md`.

## Install

### Binary

Requires Go 1.24+, a C compiler (SQLite uses CGO) and [Bun](https://bun.sh) for the editor bundle.

```bash
make build                      # writes bin/gopherwiki
SECRET_KEY=$(openssl rand -hex 32) REPOSITORY=/var/lib/gopherwiki/repo ./bin/gopherwiki
```

`REPOSITORY` is created and initialized as a Git repository if missing.

### Docker

```bash
SECRET_KEY=$(openssl rand -hex 32) docker compose up -d
```

`docker-compose.yml` stores the repository and database under `./app-data`.

## First admin

The first user to register becomes an admin with all permissions. Register right after the first start, before exposing the site.

To create the admin without the web form, pass an init file once:

```json
{
  "admin": {"name": "Admin", "email": "admin@example.com", "password": "..."},
  "site": {"name": "My Wiki"},
  "issue": {"tags": ["bug", "question"], "categories": []}
}
```

```bash
./bin/gopherwiki -init init.json
```

An existing user with the same email is left unchanged.

## Configuration

Precedence: defaults < YAML file < environment variables < command-line flags.

### Environment variables

| Variable | Default | Description |
|-|-|-|
| `SECRET_KEY` | none | Session signing key, 16+ characters. Required unless `DEV_MODE` |
| `REPOSITORY` | none | Path to the wiki Git repository. Required |
| `DATABASE_URI` | `sqlite:///<REPOSITORY>/.wiki.db` | SQLite database |
| `HOST`, `PORT` | all interfaces, `8080` | Listen address |
| `SITE_NAME` | `GopherWiki` | Shown in the header |
| `SITE_URL` | `http://localhost:8080` | Public URL. Used for feeds, the sitemap, secure cookies and the MCP origin check |
| `SITE_LOGO` | none | Logo URL |
| `HOME_PAGE` | `Home` | Landing page. A value starting with `/-/` redirects there |
| `READ_ACCESS` | `ANONYMOUS` | Who may read. See [Access control](#access-control) |
| `WRITE_ACCESS` | `ANONYMOUS` | Who may write |
| `ATTACHMENT_ACCESS` | `ANONYMOUS` | Who may upload |
| `AUTO_APPROVAL` | `true` | Approve new registrations automatically |
| `DISABLE_REGISTRATION` | `false` | Close the registration form |
| `RETAIN_PAGE_NAME_CASE` | `false` | Keep upper case in page file names |
| `SIDEBAR_MENUTREE_MODE` | `SORTED` | Page tree in the sidebar. Empty hides it |
| `ISSUE_TAGS` | `bug,feature,improvement,question,documentation` | Issue tags |
| `ISSUE_CATEGORIES` | none | Mutually exclusive issue categories |
| `COOKIE_SECURE` | on when `SITE_URL` is `https://` | Mark the session cookie `Secure` |
| `LOG_LEVEL`, `LOG_FORMAT` | `INFO`, `text` | `DEBUG`/`INFO`/`WARN`/`ERROR`; `text` or `json` |
| `DEV_MODE` | `false` | Local development only. See `docs/dev/dev-guide.md` |

The code defaults are permissive: anyone may write. `docker-compose.yml` sets `WRITE_ACCESS` and `ATTACHMENT_ACCESS` to `REGISTERED`. Set them explicitly in production.

Site name, logo, issue tags and categories can also be changed at `/-/admin/settings`. Those values are stored in the database and override the environment.

Agent and computational page variables are listed in their own sections below.

### Config file

Pass `-config <file>` or set `CONFIG_FILE`. The file supports a subset of settings:

```yaml
port: 8080
host: "0.0.0.0"
base_url: "https://wiki.example.com"      # SITE_URL
repository_path: "/var/lib/gopherwiki/repo"
database_path: "sqlite:///var/lib/gopherwiki/wiki.db"
session_secret: "..."                     # SECRET_KEY
registration_enabled: true                # inverse of DISABLE_REGISTRATION
auto_approval: false
read_access: "APPROVED"
write_access: "APPROVED"
attachment_access: "APPROVED"
site_name: "My Wiki"
landing_page: "Home"                      # HOME_PAGE
log_level: "INFO"
log_format: "json"
```

### Flags

| Flag | Description |
|-|-|
| `-config` | YAML config file |
| `-host`, `-port` | Listen address |
| `-repo` | Repository path |
| `-db` | SQLite database file |
| `-init` | Init JSON file, see [First admin](#first-admin) |
| `-templates`, `-static` | Directories that replace the embedded templates or static files |

## Access control

`READ_ACCESS`, `WRITE_ACCESS` and `ATTACHMENT_ACCESS` each take one level:

| Level | Who |
|-|-|
| `ANONYMOUS` | Everyone, logged in or not |
| `REGISTERED` | Logged-in users with the matching permission |
| `APPROVED` | Logged-in, approved users with the matching permission |
| `ADMIN` | Admins only |

Admins always pass. For a private wiki, set all three to `APPROVED` and `AUTO_APPROVAL=false`.

### Users

Manage users at `/-/admin/users`. Per user:

| Flag | Grants |
|-|-|
| Approved | Account approval |
| Admin | Everything, including `/-/admin` |
| Can Read, Can Write, Can Upload | The matching action, subject to the access level |
| Can Review | Approve `.qmd` sources and validate agent pages |

There is no admin form to create a user. Users register themselves. An init file creates only the admin.

## Agents and MCP

An external AI agent can read and write the wiki through the JSON API or the MCP (Model Context Protocol) endpoint at `/-/api/v1/mcp`. The agent runs outside GopherWiki. Design rationale: `docs/dev/llm-wiki.md`.

### Setup

1. **Create an agent user.** Register an account such as `agent@example.com`. Approve it and give it Can Read and Can Write. Do not make it an admin. Commits made with its token are authored by this user, so history separates agent edits from human edits.

2. **Create a token** at `/-/admin/tokens`. Pick the agent user, a label and a write prefix such as `agents`. Copy the token (`gw_...`). It is shown once.

3. **Give reviewers Can Review** at `/-/admin/users`.

4. **Write the conventions page** named by `GUIDE_PAGE` (default `Meta/Schema`). The MCP `guide` tool returns it to the agent. Put house style, page layout and naming rules there.

5. **Connect the agent.** For Claude Code:

   ```bash
   claude mcp add --transport http gopherwiki https://wiki.example.com/-/api/v1/mcp \
     --header "Authorization: Bearer gw_..."
   ```

   Any MCP client that supports the Streamable HTTP transport and custom headers works.

### What a token can do

- Read everything its user can read.

- Create and update pages at or below its write prefix. Other paths return 403. The prefix cannot be empty.

- Overwrite a page only by sending its current revision.

- Open issues and comment on them.

- It cannot delete pages, validate, approve, render, upload, or use admin rights, even if its user is an admin.

Revoke a token at `/-/admin/tokens`. Revocation takes effect on the next request.

### Settings

| Variable | Default | Description |
|-|-|-|
| `GUIDE_PAGE` | `Meta/Schema` | Page returned by the MCP `guide` tool. A missing page is skipped |
| `HIDE_UNVALIDATED` | `false` | Hide unvalidated agent pages from users without Can Review |

With `HIDE_UNVALIDATED=true`, a hidden page answers 404 and is left out of search, the page index, the sidebar, backlinks, the sitemap, the changelog, feeds and the API. Each listing then reads the current revision of every agent page, so it costs more on a wiki with many agent pages.

### Review workflow

Reviewers use `/-/lint` to find unvalidated agent pages and unsupported figures, then validate each page from its banner. See "Reviewing agent pages" in `docs/user-guide.md`.

### Risks

- **Prompt injection.** Page content reaches an agent that holds a write token. The write prefix limits what an injected instruction can change.

- **Unreviewed content.** Validation happens after the write. Without `HIDE_UNVALIDATED`, readers see agent pages before review.

- **MCP without a token.** The MCP endpoint requires read access, and POST requests without a token need a CSRF token. CLI agents therefore need a token.

## Computational pages and export

Both need [Quarto](https://quarto.org) on the host. Without it, `.qmd` pages show a placeholder and export offers only Markdown ZIP.

| Variable | Default | Description |
|-|-|-|
| `COMPUTATIONAL_PAGES_ENABLED` | `false` | Render `.qmd` pages on request |
| `EXPORT_ENABLED` | `false` | Offer PDF, HTML, DOCX, EPUB and GFM export |
| `RENDER_APPROVAL_REQUIRED` | `false` | Require a reviewer to approve each `.qmd` source before it renders |
| `QUARTO_PATH` | `quarto` | Quarto binary |
| `RENDER_PYTHON`, `RENDER_R` | discovered | Pin the interpreters |
| `RENDER_TIMEOUT_SECONDS` | `120` | Limit per render |
| `RENDER_CONCURRENCY` | `2` | Concurrent renders |
| `RENDER_CACHE_PATH` | `render-cache.sqlite` beside the database | Render cache |
| `OJS_LIBS_DIR` | none | Local mirror of the Observable JS libraries, made by `scripts/mirror-ojs-libs.sh` |

Rendering runs page code on the server with write permission. Read `docs/security.md` before enabling it. Full design: `docs/computational-pages.md`.

## Backup

Back up three things:

- The Git repository (`REPOSITORY`). It holds all pages and attachments.

- The SQLite database (`DATABASE_URI`). It holds users, tokens, issues, approvals, validations and report runs. Report runs are not reproducible if source data changed.

- The render cache is optional. It is rebuilt by rendering again.

Use `sqlite3 <db> ".backup <file>"` for a consistent copy while the server runs.

## Health check

`GET /-/health` answers when the server is up.
