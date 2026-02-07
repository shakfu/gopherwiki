# Makefile for GopherWiki (Go version)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"
TAGS := -tags fts5

.PHONY: all build run test clean sqlc

all: build

build:
	@go build $(TAGS) $(LDFLAGS) -o bin/gopherwiki ./cmd/gopherwiki

run: build
	@./bin/gopherwiki -repo ./test-repo -port 8080

test:
	@go test $(TAGS) -v ./...

clean:
	@rm -rf bin/
	@rm -rf test-repo/

sqlc:
	@sqlc generate

# Development helpers
dev:
	@DEV_MODE=1 go run $(TAGS) $(LDFLAGS) ./cmd/gopherwiki -repo ./test-repo -host 127.0.0.1 -port 8080

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
	@docker run -p 8080:8080 -v $(PWD)/test-repo:/wiki gopherwiki:$(VERSION)
