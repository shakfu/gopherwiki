# Build stage
FROM golang:1.24-alpine AS builder

# Install git (required for go-git) and gcc (for CGO/SQLite)
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with CGO enabled for SQLite
RUN CGO_ENABLED=1 GOOS=linux go build -tags fts5 -ldflags="-s -w" -o gopherwiki ./cmd/gopherwiki

# Runtime stage
FROM alpine:latest

# Install git (needed for git operations) and ca-certificates
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Copy binary from builder
COPY --from=builder /app/gopherwiki .

# Copy static files and templates
COPY --from=builder /app/web ./web

# Create data directory for wiki repository and database
RUN mkdir -p /app-data/repository && \
    addgroup -g 1000 gopherwiki && \
    adduser -u 1000 -G gopherwiki -h /app -D gopherwiki && \
    chown -R gopherwiki:gopherwiki /app-data

VOLUME /app-data

# Set environment variables
ENV REPOSITORY=/app-data/repository
ENV DATABASE_URI=sqlite:///app-data/gopherwiki.db
ENV SITE_NAME="GopherWiki"
ENV SITE_URL="http://localhost:8080"

# Switch to non-root user
USER gopherwiki

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/-/health || exit 1

# Run the application
CMD ["./gopherwiki"]
