// Package main provides the entry point for GopherWiki.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sa/gopherwiki/internal/auth"
	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/handlers"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/web"
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

// initLogger configures the default slog logger based on config.
func initLogger(cfg *config.Config) {
	var level slog.Level
	switch strings.ToUpper(cfg.LogLevel) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.LogFormat) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// fatal logs an error message and exits the process.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func main() {
	// Parse command line flags
	host := flag.String("host", "", "Host/IP to bind to (default: all interfaces)")
	port := flag.Int("port", 8080, "HTTP server port")
	repoPath := flag.String("repo", "", "Path to wiki git repository")
	templatesPath := flag.String("templates", "", "Path to templates directory (overrides embedded)")
	staticPath := flag.String("static", "", "Path to static files directory (overrides embedded)")
	dbPath := flag.String("db", "", "Path to SQLite database file")
	initFile := flag.String("init", "", "Path to initialization JSON file (run once to set up site)")
	flag.Parse()

	// Load configuration
	cfg := config.Load()

	// Initialize structured logger
	initLogger(cfg)

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
			slog.Info("creating repository", "path", cfg.Repository)
			if err := os.MkdirAll(cfg.Repository, 0o755); err != nil {
				fatal("failed to create repository directory", "error", err)
			}
		}
	}

	// In dev mode, relax the SECRET_KEY requirement
	if cfg.DevMode {
		slog.Warn("DEV_MODE is enabled, do NOT use in production")
		if cfg.SecretKey == "CHANGE ME" || len(cfg.SecretKey) < 16 {
			cfg.SecretKey = "dev-mode-insecure-key-not-for-production"
			slog.Warn("using auto-generated development secret key")
		}
	}

	// Validate config (fatal in production)
	if err := cfg.Validate(); err != nil {
		fatal("configuration error", "error", err)
	}

	slog.Info("starting GopherWiki", "version", Version)

	// Initialize storage
	var store storage.Storage
	var err error

	// Check if repository is a git repo, if not, initialize it
	gitDir := filepath.Join(cfg.Repository, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		slog.Info("initializing git repository", "path", cfg.Repository)
		store, err = storage.NewGitStorage(cfg.Repository, true)
	} else {
		store, err = storage.NewGitStorage(cfg.Repository, false)
	}
	if err != nil {
		fatal("failed to initialize storage", "error", err)
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
		fatal("failed to open database", "error", err)
	}
	defer database.Close()

	// Run migrations
	if err := database.Migrate(context.Background()); err != nil {
		fatal("failed to run migrations", "error", err)
	}

	// Process init file if provided
	if *initFile != "" {
		if err := processInitFile(*initFile, database, cfg); err != nil {
			fatal("failed to process init file", "error", err)
		}
	}

	// Create server
	server, err := handlers.NewServer(cfg, store, database, Version)
	if err != nil {
		fatal("failed to create server", "error", err)
	}

	// Build search index on startup
	if err := server.Wiki.EnsureSearchIndex(context.Background()); err != nil {
		slog.Warn("failed to build search index", "error", err)
	}

	// Load templates: use filesystem override if provided, otherwise embedded
	var templatesFS fs.FS
	if *templatesPath != "" {
		slog.Info("loading templates from filesystem", "path", *templatesPath)
		templatesFS = os.DirFS(*templatesPath)
	} else {
		slog.Info("loading templates from embedded FS")
		templatesFS, err = fs.Sub(web.TemplatesFS, "templates")
		if err != nil {
			fatal("failed to access embedded templates", "error", err)
		}
	}
	if err := server.LoadTemplates(templatesFS); err != nil {
		fatal("failed to load templates", "error", err)
	}

	// Set static FS: use filesystem override if provided, otherwise embedded
	if *staticPath != "" {
		slog.Info("serving static files from filesystem", "path", *staticPath)
		server.StaticFS = os.DirFS(*staticPath)
	} else {
		slog.Info("serving static files from embedded FS")
		server.StaticFS, err = fs.Sub(web.StaticFS, "static")
		if err != nil {
			fatal("failed to access embedded static files", "error", err)
		}
	}

	// Create router
	router := server.Routes()

	// Check if repository is empty and create initial page
	files, _, err := store.List("", nil, nil)
	if err != nil {
		slog.Warn("failed to list repository files", "error", err)
	}
	// Filter out hidden files like .wiki.db
	var mdFiles []string
	for _, f := range files {
		if !strings.HasPrefix(f, ".") && strings.HasSuffix(f, ".md") {
			mdFiles = append(mdFiles, f)
		}
	}
	if len(mdFiles) == 0 {
		slog.Info("creating initial home page")
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
			slog.Warn("failed to create initial page", "error", err)
		}
	}

	// Start server with graceful shutdown
	addr := fmt.Sprintf("%s:%d", *host, *port)
	srv := &http.Server{Addr: addr, Handler: router}

	go func() {
		displayHost := *host
		if displayHost == "" {
			displayHost = "localhost"
		}
		slog.Info("server listening", "address", fmt.Sprintf("http://%s:%d", displayHost, *port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal("server error", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("received signal, shutting down", "signal", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}
	slog.Info("server stopped")
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
			slog.Info("setting site name", "name", initCfg.Site.Name)
			params := db.UpsertPreferenceParams{
				Name:  "site_name",
				Value: sql.NullString{String: initCfg.Site.Name, Valid: true},
			}
			if err := database.Queries.UpsertPreference(ctx, params); err != nil {
				return fmt.Errorf("failed to set site name: %w", err)
			}
		}
		if initCfg.Site.Logo != "" {
			slog.Info("setting site logo", "logo", initCfg.Site.Logo)
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
			slog.Info("setting issue tags", "tags", tags)
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
			slog.Info("setting issue categories", "categories", categories)
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
			slog.Info("creating admin user", "name", initCfg.Admin.Name, "email", initCfg.Admin.Email)

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
			slog.Info("admin user created successfully")
		} else if err != nil {
			return fmt.Errorf("failed to check for existing user: %w", err)
		} else {
			slog.Info("admin user already exists, skipping creation", "email", initCfg.Admin.Email)
		}
	}

	slog.Info("initialization complete")
	return nil
}
