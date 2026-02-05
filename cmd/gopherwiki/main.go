// Package main provides the entry point for GopherWiki.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sa/gopherwiki/internal/auth"
	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/handlers"
	"github.com/sa/gopherwiki/internal/storage"
)

// InitConfig represents the initialization configuration from JSON.
type InitConfig struct {
	Site  *InitSite  `json:"site,omitempty"`
	Admin *InitAdmin `json:"admin,omitempty"`
	Issue *InitIssue `json:"issue,omitempty"`
}

// InitSite holds site branding settings.
type InitSite struct {
	Name string `json:"name,omitempty"`
	Logo string `json:"logo,omitempty"`
}

// InitAdmin holds initial admin user settings.
type InitAdmin struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// InitIssue holds issue tracker settings.
type InitIssue struct {
	Tags       []string `json:"tags,omitempty"`
	Categories []string `json:"categories,omitempty"`
}

// Version is set at build time.
var Version = "dev"

func main() {
	// Parse command line flags
	port := flag.Int("port", 8080, "HTTP server port")
	repoPath := flag.String("repo", "", "Path to wiki git repository")
	templatesPath := flag.String("templates", "", "Path to templates directory")
	staticPath := flag.String("static", "", "Path to static files directory")
	dbPath := flag.String("db", "", "Path to SQLite database file")
	initFile := flag.String("init", "", "Path to initialization JSON file (run once to set up site)")
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

	// Process init file if provided
	if *initFile != "" {
		if err := processInitFile(*initFile, database, cfg); err != nil {
			log.Fatalf("Failed to process init file: %v", err)
		}
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

// processInitFile reads and applies initialization settings from a JSON file.
func processInitFile(filePath string, database *db.Database, cfg *config.Config) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read init file: %w", err)
	}

	var initCfg InitConfig
	if err := json.Unmarshal(data, &initCfg); err != nil {
		return fmt.Errorf("failed to parse init file: %w", err)
	}

	ctx := context.Background()

	// Process site settings
	if initCfg.Site != nil {
		if initCfg.Site.Name != "" {
			log.Printf("Setting site name to: %s", initCfg.Site.Name)
			params := db.UpsertPreferenceParams{
				Name:  "site_name",
				Value: sql.NullString{String: initCfg.Site.Name, Valid: true},
			}
			if err := database.Queries.UpsertPreference(ctx, params); err != nil {
				return fmt.Errorf("failed to set site name: %w", err)
			}
		}
		if initCfg.Site.Logo != "" {
			log.Printf("Setting site logo to: %s", initCfg.Site.Logo)
			params := db.UpsertPreferenceParams{
				Name:  "site_logo",
				Value: sql.NullString{String: initCfg.Site.Logo, Valid: true},
			}
			if err := database.Queries.UpsertPreference(ctx, params); err != nil {
				return fmt.Errorf("failed to set site logo: %w", err)
			}
		}
	}

	// Process issue tracker settings
	if initCfg.Issue != nil {
		if len(initCfg.Issue.Tags) > 0 {
			tags := strings.Join(initCfg.Issue.Tags, ",")
			log.Printf("Setting issue tags to: %s", tags)
			params := db.UpsertPreferenceParams{
				Name:  "issue_tags",
				Value: sql.NullString{String: tags, Valid: true},
			}
			if err := database.Queries.UpsertPreference(ctx, params); err != nil {
				return fmt.Errorf("failed to set issue tags: %w", err)
			}
		}
		if len(initCfg.Issue.Categories) > 0 {
			categories := strings.Join(initCfg.Issue.Categories, ",")
			log.Printf("Setting issue categories to: %s", categories)
			params := db.UpsertPreferenceParams{
				Name:  "issue_categories",
				Value: sql.NullString{String: categories, Valid: true},
			}
			if err := database.Queries.UpsertPreference(ctx, params); err != nil {
				return fmt.Errorf("failed to set issue categories: %w", err)
			}
		}
	}

	// Process admin user
	if initCfg.Admin != nil {
		if initCfg.Admin.Email == "" || initCfg.Admin.Password == "" {
			return fmt.Errorf("admin email and password are required")
		}

		// Check if user already exists
		_, err := database.Queries.GetUserByEmail(ctx, initCfg.Admin.Email)
		if err == sql.ErrNoRows {
			// Create admin user
			log.Printf("Creating admin user: %s (%s)", initCfg.Admin.Name, initCfg.Admin.Email)

			passwordHash, err := auth.HashPassword(initCfg.Admin.Password)
			if err != nil {
				return fmt.Errorf("failed to hash password: %w", err)
			}

			now := time.Now()
			params := db.CreateUserParams{
				Name:           initCfg.Admin.Name,
				Email:          initCfg.Admin.Email,
				PasswordHash:   sql.NullString{String: passwordHash, Valid: true},
				FirstSeen:      sql.NullTime{Time: now, Valid: true},
				LastSeen:       sql.NullTime{Time: now, Valid: true},
				IsApproved:     sql.NullBool{Bool: true, Valid: true},
				IsAdmin:        sql.NullBool{Bool: true, Valid: true},
				EmailConfirmed: sql.NullBool{Bool: true, Valid: true},
				AllowRead:      sql.NullBool{Bool: true, Valid: true},
				AllowWrite:     sql.NullBool{Bool: true, Valid: true},
				AllowUpload:    sql.NullBool{Bool: true, Valid: true},
			}

			if _, err := database.Queries.CreateUser(ctx, params); err != nil {
				return fmt.Errorf("failed to create admin user: %w", err)
			}
			log.Printf("Admin user created successfully")
		} else if err != nil {
			return fmt.Errorf("failed to check for existing user: %w", err)
		} else {
			log.Printf("Admin user %s already exists, skipping creation", initCfg.Admin.Email)
		}
	}

	log.Printf("Initialization complete")
	return nil
}
