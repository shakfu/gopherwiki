// Package main provides the entry point for GopherWiki.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/handlers"
	"github.com/sa/gopherwiki/internal/storage"
)

// Version is set at build time.
var Version = "dev"

func main() {
	// Parse command line flags
	port := flag.Int("port", 8080, "HTTP server port")
	repoPath := flag.String("repo", "", "Path to wiki git repository")
	templatesPath := flag.String("templates", "", "Path to templates directory")
	staticPath := flag.String("static", "", "Path to static files directory")
	dbPath := flag.String("db", "", "Path to SQLite database file")
	flag.Parse()

	// Load configuration
	cfg := config.Load()

	// Override config from command line
	if *repoPath != "" {
		cfg.Repository = *repoPath
	}
	if cfg.Repository == "" {
		cfg.Repository = os.Getenv("REPOSITORY")
	}

	// Create repository if it doesn't exist
	if cfg.Repository != "" {
		if _, err := os.Stat(cfg.Repository); os.IsNotExist(err) {
			log.Printf("Creating repository at %s", cfg.Repository)
			if err := os.MkdirAll(cfg.Repository, 0o755); err != nil {
				log.Fatalf("Failed to create repository directory: %v", err)
			}
		}
	}

	// Validate config
	if err := cfg.Validate(); err != nil {
		// For development, allow running without validation
		if cfg.SecretKey == "CHANGE ME" {
			cfg.SecretKey = "development-secret-key-do-not-use-in-production"
			log.Printf("Warning: Using development secret key. Set SECRET_KEY for production.")
		}
		if cfg.Repository == "" {
			log.Fatalf("Configuration error: %v", err)
		}
	}

	log.Printf("*** Starting GopherWiki %s", Version)

	// Initialize storage
	var store storage.Storage
	var err error

	// Check if repository is a git repo, if not, initialize it
	gitDir := filepath.Join(cfg.Repository, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		log.Printf("Initializing git repository at %s", cfg.Repository)
		store, err = storage.NewGitStorage(cfg.Repository, true)
	} else {
		store, err = storage.NewGitStorage(cfg.Repository, false)
	}
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	// Initialize database
	dbURI := cfg.DatabaseURI
	if *dbPath != "" {
		dbURI = "sqlite:///" + *dbPath
	}
	if dbURI == "" || dbURI == "sqlite:///:memory:" {
		// Default to file in repository
		dbURI = "sqlite:///" + filepath.Join(cfg.Repository, ".wiki.db")
	}

	database, err := db.Open(dbURI)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.Migrate(context.Background()); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Create server
	server, err := handlers.NewServer(cfg, store, database, Version)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Determine templates path
	tplPath := *templatesPath
	if tplPath == "" {
		// Try relative paths
		candidates := []string{
			"web/templates",
			"../../web/templates",
			filepath.Join(filepath.Dir(os.Args[0]), "web/templates"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				tplPath = c
				break
			}
		}
	}
	if tplPath == "" {
		log.Fatalf("Templates directory not found. Use -templates flag.")
	}

	if err := server.LoadTemplates(tplPath); err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}

	// Determine static files path
	statPath := *staticPath
	if statPath == "" {
		// Try to find static files from otterwiki
		candidates := []string{
			"web/static",
			"../../web/static",
			"otterwiki/static",
			"../../otterwiki/static",
			filepath.Join(filepath.Dir(os.Args[0]), "web/static"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				statPath = c
				break
			}
		}
	}

	// Setup static file serving
	if statPath != "" {
		log.Printf("Serving static files from %s", statPath)
	}

	// Create router
	router := server.Routes()

	// Override static handler if we found a path
	if statPath != "" {
		absStatPath, _ := filepath.Abs(statPath)
		router.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir(absStatPath))))
	}

	// Check if repository is empty and create initial page
	files, _, _ := store.List("", nil, nil)
	// Filter out hidden files like .wiki.db
	var mdFiles []string
	for _, f := range files {
		if !strings.HasPrefix(f, ".") && strings.HasSuffix(f, ".md") {
			mdFiles = append(mdFiles, f)
		}
	}
	if len(mdFiles) == 0 {
		log.Printf("Creating initial home page")
		content := `# Welcome to GopherWiki

This is your new wiki. Start editing this page or create new pages.

## Getting Started

- Click the edit button (pencil icon) to edit this page
- Use [[WikiLinks]] to link to other pages
- Markdown formatting is fully supported

## Features

- **Markdown**: Full markdown support with extensions
- **Git Backend**: All changes are versioned with git
- **WikiLinks**: [[Link to pages]] with double brackets
- **Attachments**: Upload and embed images and files
- **History**: View and compare page revisions

Enjoy your wiki!
`
		author := storage.Author{
			Name:  "GopherWiki",
			Email: "noreply@gopherwiki",
		}
		filename := "home.md"
		if cfg.RetainPageNameCase {
			filename = "Home.md"
		}
		if _, err := store.Store(filename, content, "Initial commit", author); err != nil {
			log.Printf("Warning: Failed to create initial page: %v", err)
		}
	}

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Server listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
