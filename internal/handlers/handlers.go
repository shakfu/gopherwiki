// Package handlers provides HTTP handlers for GopherWiki.
package handlers

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/sa/gopherwiki/internal/auth"
	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/wiki"
)

// Server holds all dependencies for HTTP handlers.
type Server struct {
	Config            *config.Config
	Storage           storage.Storage
	Wiki              *wiki.WikiService
	DB                *db.Database
	Renderer          *renderer.Renderer
	Templates         *template.Template
	TemplateMap       map[string]*template.Template
	StaticFS          fs.FS
	Version           string
	Auth              *auth.Auth
	SessionManager    *middleware.SessionManager
	PermissionChecker *middleware.PermissionChecker
}

// NewServer creates a new Server with the given dependencies.
func NewServer(cfg *config.Config, store storage.Storage, database *db.Database, version string) (*Server, error) {
	rend := renderer.New(cfg)
	authService := auth.New(cfg, database.Queries)
	sessionManager := middleware.NewSessionManager(cfg.SecretKey, database.Queries)
	permChecker := middleware.NewPermissionChecker(cfg, sessionManager)

	wikiService := wiki.NewWikiService(store, cfg, database)

	s := &Server{
		Config:            cfg,
		Storage:           store,
		Wiki:              wikiService,
		DB:                database,
		Renderer:          rend,
		Version:           version,
		Auth:              authService,
		SessionManager:    sessionManager,
		PermissionChecker: permChecker,
	}

	return s, nil
}

// SiteSettings holds customizable site settings that can be changed at runtime.
type SiteSettings struct {
	Name string
	Logo string
}

// getSiteSettings returns site settings from preferences or config.
func (s *Server) getSiteSettings(ctx context.Context) SiteSettings {
	settings := SiteSettings{
		Name: s.Config.SiteName,
		Logo: s.Config.SiteLogo,
	}

	// Try to get site name from preferences
	if pref, err := s.DB.Queries.GetPreference(ctx, "site_name"); err == nil && pref.Value.Valid && pref.Value.String != "" {
		settings.Name = pref.Value.String
	}

	// Try to get site logo from preferences
	if pref, err := s.DB.Queries.GetPreference(ctx, "site_logo"); err == nil && pref.Value.Valid && pref.Value.String != "" {
		settings.Logo = pref.Value.String
	}

	return settings
}

// renderTemplate renders a template with common context.
func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add common context
	data["config"] = s.Config
	data["Version"] = s.Version

	// Add site settings (from preferences or config)
	data["site"] = s.getSiteSettings(r.Context())

	// Add auth context from session
	user := middleware.GetUser(r)
	data["current_user"] = map[string]interface{}{
		"is_authenticated": user.IsAuthenticated(),
		"is_anonymous":     user.IsAnonymous(),
		"is_approved":      user.Approved(),
		"is_admin":         user.Admin(),
		"name":             user.GetName(),
		"email":            user.GetEmail(),
	}
	data["auth_supported_features"] = map[string]bool{
		"logout":   true,
		"register": !s.Config.DisableRegistration,
	}

	// Add permission context for templates
	data["permissions"] = map[string]bool{
		"read":   s.PermissionChecker.HasPermission(r, middleware.PermissionRead),
		"write":  s.PermissionChecker.HasPermission(r, middleware.PermissionWrite),
		"upload": s.PermissionChecker.HasPermission(r, middleware.PermissionUpload),
		"admin":  s.PermissionChecker.HasPermission(r, middleware.PermissionAdmin),
	}

	// Add flash messages
	if flashes := middleware.GetFlashes(r); len(flashes) > 0 {
		data["flashes"] = flashes
	}

	// Get the template for this page
	tmpl, ok := s.TemplateMap[name]
	if !ok {
		slog.Error("template not found", "name", name)
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}

	// Execute the base template
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("template execution error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderPage renders a wiki page.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page *wiki.Page) {
	// Set ETag based on git commit revision for cache validation
	if page.Metadata != nil && page.Metadata.RevisionFull != "" {
		etag := `"` + page.Metadata.RevisionFull + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	htmlContent, toc, libRequirements := page.Render(s.Renderer)
	data := NewPageData(page, template.HTML(htmlContent), toc, libRequirements)

	// Fetch backlinks
	if backlinks, err := s.Wiki.Backlinks(r.Context(), page.Pagepath); err == nil && len(backlinks) > 0 {
		data["backlinks"] = backlinks
	}

	s.renderTemplate(w, r, "page.html", data)
}

// renderNotFound renders a 404 page for a missing wiki page.
func (s *Server) renderNotFound(w http.ResponseWriter, r *http.Request, page *wiki.Page) {
	w.WriteHeader(http.StatusNotFound)
	data := NewNotFoundData(page)
	s.renderTemplate(w, r, "page404.html", data)
}

// renderError renders a styled error page with the given HTTP status code.
func (s *Server) renderError(w http.ResponseWriter, r *http.Request, code int, message string) {
	title := http.StatusText(code)
	if title == "" {
		title = "Error"
	}
	w.WriteHeader(code)
	data := NewGenericData(title)
	data["error_title"] = title
	data["error_message"] = message
	s.renderTemplate(w, r, "error.html", data)
}

// getAuthor extracts a storage.Author from the request's authenticated user,
// falling back to "Anonymous" for unauthenticated users.
func (s *Server) getAuthor(r *http.Request) storage.Author {
	user := middleware.GetUser(r)
	author := storage.Author{
		Name:  user.GetName(),
		Email: user.GetEmail(),
	}
	if author.Name == "" {
		author.Name = "Anonymous"
	}
	if author.Email == "" {
		author.Email = "anonymous@example.com"
	}
	return author
}

// parseInt64 parses a string to int64.
func parseInt64(s string) (int64, error) {
	var i int64
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

