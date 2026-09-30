# Makefile for GopherWiki (Go version)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
TAGS := -tags fts5
SECRET_KEY ?= ntjaMxdy1BehFb84

REPO ?= ./build/test-repo
PORT ?= 8080
HOST ?= 127.0.0.1
OPEN ?= 1

# Render cache for dev-compute. Kept in build/ rather than the default location
# beside the wiki database, so cache files never land inside the wiki repo.
RENDER_CACHE_PATH ?= $(CURDIR)/build/render-cache.db

.PHONY: all build build-editor run dev dev-compute demo-pages dev-token mcp-demo \
	test clean sqlc install reset fmt vet lint docker-build docker-run

all: build

install:
	@cd web/editor && bun install

reset:
	@rm -rf web/editor/node_modules
	@cd web/editor && bun install

build-editor:
	@bun build web/editor/editor.js --outfile=web/static/js/editor.bundle.js --minify --bundle

build: build-editor
	@go build $(TAGS) $(LDFLAGS) -o bin/gopherwiki ./cmd/gopherwiki

run: build
	@SECRET_KEY=$(SECRET_KEY) ./bin/gopherwiki -repo ./build/test-repo -port 8080

test:
	@go test $(TAGS) -v ./...

clean:
	@rm -rf bin/
	@rm -rf build/

sqlc:
	@sqlc generate

# Development helpers

# Open the dev URL once the server answers. Polls because `go run` compiles
# first; gives up after 60s so a failed start leaves no stray process.
define open_browser
	@if [ "$(OPEN)" = 1 ]; then \
		( for i in $$(seq 120); do \
			curl -so /dev/null http://$(HOST):$(PORT)/ && \
			{ $$(command -v open || command -v xdg-open) http://$(HOST):$(PORT)/; break; }; \
			sleep 0.5; \
		done ) & \
	fi
endef

dev:
	$(open_browser)
	@DEV_MODE=1 go run $(TAGS) $(LDFLAGS) ./cmd/gopherwiki -repo $(REPO) -host $(HOST) -port $(PORT)

# Development server with computational pages (.qmd execution via Quarto) and
# Quarto export both enabled. Interpreters are auto-detected: the project
# virtualenv is pinned as RENDER_PYTHON when present, and R when on PATH.
# Warns rather than fails when quarto is absent -- the app still runs, .qmd
# pages just show the render-pending placeholder.
dev-compute: demo-pages
	@command -v quarto >/dev/null 2>&1 || echo "warning: quarto not found on PATH; .qmd rendering and Quarto export will be unavailable"
	@mkdir -p $(dir $(RENDER_CACHE_PATH))
	$(open_browser)
	@DEV_MODE=1 \
		COMPUTATIONAL_PAGES_ENABLED=1 \
		EXPORT_ENABLED=1 \
		RENDER_CACHE_PATH=$(RENDER_CACHE_PATH) \
		RENDER_PYTHON="$$(test -x $(CURDIR)/.venv/bin/python && echo $(CURDIR)/.venv/bin/python)" \
		RENDER_R="$$(command -v R 2>/dev/null)" \
		go run $(TAGS) $(LDFLAGS) ./cmd/gopherwiki -repo $(REPO) -host $(HOST) -port $(PORT)

# Seed demonstration .qmd pages (plain, Python, Observable JS) into the dev
# repo. Idempotent: existing files are left untouched.
demo-pages:
	@./scripts/seed-demo-pages.sh $(REPO)

# Print a new API token for the dev database. Bound to EMAIL, or the first user.
PREFIX ?= agents
EMAIL ?=
dev-token:
	@./scripts/dev-token.sh $(REPO)/.wiki.db $(PREFIX) $(EMAIL)

# Demo wiki for testing MCP with an agent: pages, report runs, an agent user
# and its token. Seeded once into MCP_DEMO; delete that directory to reseed.
MCP_DEMO ?= build/mcp-demo
MCP_SEED_PORT ?= 18089
mcp-demo:
	@go build $(TAGS) $(LDFLAGS) -o build/gopherwiki-dev ./cmd/gopherwiki
	@./scripts/seed-mcp-demo.sh build/gopherwiki-dev $(MCP_DEMO) $(MCP_SEED_PORT)
	@echo "Browser login: admin@demo.local / demo-password (admin, reviewer)"
	@echo "Connect Claude Code:"
	@echo "  claude mcp add --transport http northwind http://$(HOST):$(PORT)/-/api/v1/mcp --header \"Authorization: Bearer $$(cat $(MCP_DEMO)/token)\""
	$(open_browser)
	@DEV_MODE=1 ./build/gopherwiki-dev -repo $(MCP_DEMO)/repo -host $(HOST) -port $(PORT)

fmt:
	@go fmt ./...

vet:
	@go vet $(TAGS) ./...

lint: fmt vet
	@echo "Linting complete"

# Docker
docker-build:
	@docker build -t gopherwiki:$(VERSION) .

docker-run:
	@docker run -p 8080:8080 -v $(PWD)/build/test-repo:/wiki gopherwiki:$(VERSION)
