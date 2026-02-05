# Makefile for GopherWiki (Go version)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: all build run test clean sqlc

all: build

build:
	@go build $(LDFLAGS) -o bin/gopherwiki ./cmd/gopherwiki

run: build
	@./bin/gopherwiki -repo ./test-repo -port 8080

test:
	@go test -v ./...

clean:
	@rm -rf bin/
	@rm -rf test-repo/

sqlc:
	@sqlc generate

# Development helpers
dev:
	@go run $(LDFLAGS) ./cmd/gopherwiki -repo ./test-repo -port 8080

fmt:
	@go fmt ./...

vet:
	@go vet ./...

lint: fmt vet
	@echo "Linting complete"

# Docker
docker-build:
	@docker build -t gopherwiki:$(VERSION) .

docker-run:
	@docker run -p 8080:8080 -v $(PWD)/test-repo:/wiki gopherwiki:$(VERSION)
