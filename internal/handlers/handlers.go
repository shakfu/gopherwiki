// Package handlers provides HTTP handlers for GopherWiki.
package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/auth"
	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// Server holds all dependencies for HTTP handlers.
type Server struct {
	Config            *config.Config
	Storage           storage.Storage
	DB                *db.Database
	Renderer          *renderer.Renderer
	Templates         *template.Template
	TemplateMap       map[string]*template.Template
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

	s := &Server{
		Config:            cfg,
		Storage:           store,
		DB:                database,
		Renderer:          rend,
		Version:           version,
		Auth:              authService,
		SessionManager:    sessionManager,
		PermissionChecker: permChecker,
	}

	return s, nil
}

// LoadTemplates loads templates from the given directory.
// Each page template is parsed separately with base.html to avoid conflicts.
func (s *Server) LoadTemplates(templatesDir string) error {
	funcMap := s.templateFuncs()

	log.Printf("Loading templates from: %s", templatesDir)

	// Read shared template files
	baseContent, err := os.ReadFile(filepath.Join(templatesDir, "base.html"))
	if err != nil {
		return fmt.Errorf("failed to read base.html: %w", err)
	}
	editorContent, err := os.ReadFile(filepath.Join(templatesDir, "editor.html"))
	if err != nil {
		return fmt.Errorf("failed to read editor.html: %w", err)
	}
	pageContent, err := os.ReadFile(filepath.Join(templatesDir, "page.html"))
	if err != nil {
		return fmt.Errorf("failed to read page.html: %w", err)
	}

	// Find all template files
	pattern := filepath.Join(templatesDir, "*.html")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob templates: %w", err)
	}

	// Create a map to store each page's template set
	s.TemplateMap = make(map[string]*template.Template)

	log.Printf("Loaded templates:")
	for _, file := range files {
		name := filepath.Base(file)
		// Skip shared templates
		if name == "base.html" || name == "editor.html" || name == "page.html" {
			continue
		}

		// Create a new template set for each page
		tmpl := template.New("base").Funcs(funcMap)

		// Parse base.html
		tmpl, err = tmpl.Parse(string(baseContent))
		if err != nil {
			return fmt.Errorf("failed to parse base.html for %s: %w", name, err)
		}

		// Parse editor.html (for editor_* defines)
		tmpl, err = tmpl.Parse(string(editorContent))
		if err != nil {
			return fmt.Errorf("failed to parse editor.html for %s: %w", name, err)
		}

		// Parse page.html (for page_* defines)
		tmpl, err = tmpl.Parse(string(pageContent))
		if err != nil {
			return fmt.Errorf("failed to parse page.html for %s: %w", name, err)
		}

		// Parse the specific page template (will override generic_content if defined)
		specificContent, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", name, err)
		}
		tmpl, err = tmpl.Parse(string(specificContent))
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", name, err)
		}

		s.TemplateMap[name] = tmpl
		log.Printf("  - %s", name)
	}

	// Also add editor.html and page.html to the map
	// They need themselves + base + an empty generic_content
	emptyGeneric := `{{define "generic_content"}}{{end}}`
	for _, shared := range []struct {
		name    string
		content []byte
	}{
		{"editor.html", editorContent},
		{"page.html", pageContent},
	} {
		tmpl := template.New("base").Funcs(funcMap)
		tmpl, _ = tmpl.Parse(string(baseContent))
		tmpl, _ = tmpl.Parse(string(editorContent))
		tmpl, _ = tmpl.Parse(string(pageContent))
		tmpl, _ = tmpl.Parse(emptyGeneric)
		s.TemplateMap[shared.name] = tmpl
		log.Printf("  - %s", shared.name)
	}

	return nil
}

// templateFuncs returns the template function map.
func (s *Server) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"debugUnixtime": func(path string) string {
			if s.Config.Debug {
				return path + "?" + time.Now().Format("20060102150405")
			}
			if s.Version != "" {
				return path + "?" + s.Version
			}
			return path
		},
		"pluralize": util.Pluralize,
		"urlquote":  util.URLQuote,
		"formatDatetime": func(t time.Time, format string) string {
			return util.FormatDatetime(t, format)
		},
		"slugify": func(s string) string {
			return util.Slugify(s, true)
		},
		"safe": func(s string) template.HTML {
			return template.HTML(s)
		},
		"urlFor": func(name string, args ...string) string {
			switch name {
			case "index":
				return "/"
			case "view":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1]
				}
				return "/"
			case "edit":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/edit"
				}
				return "/edit"
			case "save":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/save"
				}
				return "/save"
			case "history":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/history"
				}
				return "/history"
			case "blame":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/blame"
				}
				return "/blame"
			case "diff":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/diff"
				}
				return "/diff"
			case "source":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/source"
				}
				return "/source"
			case "static":
				if len(args) >= 2 && args[0] == "filename" {
					return "/static/" + args[1]
				}
				return "/static/"
			case "login":
				return "/-/login"
			case "logout":
				return "/-/logout"
			case "register":
				return "/-/register"
			case "settings":
				return "/-/settings"
			case "search":
				return "/-/search"
			case "changelog":
				return "/-/changelog"
			case "commit":
				if len(args) >= 2 && args[0] == "revision" {
					return "/-/commit/" + args[1]
				}
				return "/-/changelog"
			case "revert":
				if len(args) >= 2 && args[0] == "revision" {
					return "/-/commit/" + args[1] + "/revert"
				}
				return "/-/changelog"
			case "about":
				return "/-/about"
			case "create":
				if len(args) >= 2 && args[0] == "path" {
					return "/" + args[1] + "/create"
				}
				return "/-/create"
			case "attachments":
				if len(args) >= 2 && args[0] == "pagepath" {
					return "/" + args[1] + "/attachments"
				}
				return "/attachments"
			case "pageindex":
				return "/-/pageindex"
			case "issues":
				return "/-/issues"
			case "issue_new":
				return "/-/issues/new"
			case "issue":
				if len(args) >= 2 && args[0] == "id" {
					return "/-/issues/" + args[1]
				}
				return "/-/issues"
			case "issue_edit":
				if len(args) >= 2 && args[0] == "id" {
					return "/-/issues/" + args[1] + "/edit"
				}
				return "/-/issues"
			case "issue_close":
				if len(args) >= 2 && args[0] == "id" {
					return "/-/issues/" + args[1] + "/close"
				}
				return "/-/issues"
			case "issue_reopen":
				if len(args) >= 2 && args[0] == "id" {
					return "/-/issues/" + args[1] + "/reopen"
				}
				return "/-/issues"
			case "issue_delete":
				if len(args) >= 2 && args[0] == "id" {
					return "/-/issues/" + args[1] + "/delete"
				}
				return "/-/issues"
			default:
				return "/"
			}
		},
		"hasPermission": func(perm string) bool {
			// TODO: Implement proper permission checking
			return true
		},
		"osGetenv": os.Getenv,
	}
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

	// Add flash messages
	if flashes := middleware.GetFlashes(r); len(flashes) > 0 {
		data["flashes"] = flashes
	}

	// Get the template for this page
	tmpl, ok := s.TemplateMap[name]
	if !ok {
		log.Printf("Template not found: %s", name)
		http.Error(w, "Template not found: "+name, http.StatusInternalServerError)
		return
	}

	// Execute the base template
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		log.Printf("Template error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Routes returns the Chi router with all routes configured.
func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()

	// Session middleware (adds user to context)
	r.Use(s.SessionManager.Middleware)

	// Static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Special routes (starting with /-/)
	r.Route("/-", func(r chi.Router) {
		r.Get("/", s.handleIndex)
		r.Get("/about", s.handleAbout)
		r.Get("/search", s.handleSearch)
		r.Post("/search", s.handleSearch)
		r.Get("/changelog", s.handleChangelog)
		r.Get("/commit/{revision}", s.handleCommit)
		r.Get("/commit/{revision}/revert", s.handleRevertForm)
		r.Post("/commit/{revision}/revert", s.handleRevert)
		r.Get("/pageindex", s.handlePageIndex)
		r.Get("/login", s.handleLogin)
		r.Post("/login", s.handleLoginPost)
		r.Get("/logout", s.handleLogout)
		r.Get("/register", s.handleRegister)
		r.Post("/register", s.handleRegisterPost)
		r.Get("/settings", s.handleSettings)
		r.Post("/settings", s.handleSettingsPost)
		r.Get("/create", s.handleCreateForm)
		r.Post("/create", s.handleCreate)
		// Admin routes
		r.Get("/admin", s.handleAdmin)
		r.Get("/admin/users", s.handleAdminUsers)
		r.Get("/admin/users/{id}", s.handleAdminUserEdit)
		r.Post("/admin/users/{id}", s.handleAdminUserSave)
		r.Post("/admin/users/{id}/delete", s.handleAdminUserDelete)
		r.Get("/admin/settings", s.handleAdminSettings)
		r.Post("/admin/settings", s.handleAdminSettingsSave)
		r.Post("/admin/site-settings", s.handleAdminSiteSettingsSave)
		r.Post("/admin/issue-settings", s.handleAdminIssueSettingsSave)
		// Feeds
		r.Get("/feed", s.handleFeed)
		r.Get("/feed.rss", s.handleFeed)
		r.Get("/feed.atom", s.handleAtomFeed)
		// Utility routes
		r.Get("/robots.txt", s.handleRobotsTxt)
		r.Get("/sitemap.xml", s.handleSitemap)
		r.Get("/health", s.handleHealthCheck)
		// Issue tracker routes
		r.Get("/issues", s.handleIssueList)
		r.Get("/issues/new", s.handleIssueNew)
		r.Post("/issues/new", s.handleIssueCreate)
		r.Get("/issues/{id}", s.handleIssueView)
		r.Get("/issues/{id}/edit", s.handleIssueEdit)
		r.Post("/issues/{id}/edit", s.handleIssueUpdate)
		r.Post("/issues/{id}/close", s.handleIssueClose)
		r.Post("/issues/{id}/reopen", s.handleIssueReopen)
		r.Post("/issues/{id}/delete", s.handleIssueDelete)
	})

	// Index/home page
	r.Get("/", s.handleIndex)

	// Wiki page routes
	r.Route("/{path:.*}", func(r chi.Router) {
		r.Get("/", s.handleView)
		r.Get("/edit", s.handleEdit)
		r.Post("/save", s.handleSave)
		r.Get("/history", s.handleHistory)
		r.Get("/source", s.handleSource)
		r.Get("/create", s.handleCreate)
		r.Get("/delete", s.handleDeleteForm)
		r.Post("/delete", s.handleDelete)
		r.Get("/rename", s.handleRenameForm)
		r.Post("/rename", s.handleRename)
		r.Get("/attachments", s.handleAttachments)
		r.Post("/attachments", s.handleUploadAttachment)
		r.Get("/blame", s.handleBlame)
		r.Get("/diff", s.handleDiff)
		r.Post("/preview", s.handlePreview)
		r.Post("/draft", s.handleDraftSave)
		r.Get("/draft", s.handleDraftLoad)
		r.Delete("/draft", s.handleDraftDelete)
	})

	return r
}

// handleIndex handles the home page.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// Determine the home page path
	homePage := s.Config.HomePage
	if homePage == "" {
		homePage = "Home"
	}

	// If home page is a special route, redirect
	if strings.HasPrefix(homePage, "/-/") {
		http.Redirect(w, r, homePage, http.StatusFound)
		return
	}

	// Load the home page
	page, err := wiki.NewPage(s.Storage, s.Config, homePage, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !page.Exists {
		// Redirect to create page
		http.Redirect(w, r, "/"+homePage+"/edit", http.StatusFound)
		return
	}

	// Render the page
	s.renderPage(w, r, page)
}

// handleView handles viewing a wiki page.
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	if path == "" {
		s.handleIndex(w, r)
		return
	}

	revision := r.URL.Query().Get("revision")

	// Check if this is an attachment file request
	// Pattern: pagepath/filename.ext where pagepath/ directory exists
	if idx := strings.LastIndex(path, "/"); idx > 0 {
		parentPath := path[:idx]
		filename := path[idx+1:]

		// Check if there's a .md page for the parent and the file exists in attachment dir
		parentFilename := util.GetFilename(parentPath)
		if !s.Config.RetainPageNameCase {
			parentFilename = strings.ToLower(parentFilename)
		}
		attachmentDir := util.GetAttachmentDirectoryname(parentFilename)
		attachmentPath := attachmentDir + "/" + filename

		if s.Storage.Exists(attachmentPath) {
			s.serveAttachment(w, r, attachmentPath, filename)
			return
		}
	}

	page, err := wiki.NewPage(s.Storage, s.Config, path, revision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}

	s.renderPage(w, r, page)
}

// serveAttachment serves an attachment file from storage.
func (s *Server) serveAttachment(w http.ResponseWriter, r *http.Request, filepath, filename string) {
	content, err := s.Storage.LoadBytes(filepath, "")
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Set content type
	contentType := util.GuessMimetype(filename)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))

	// For images and PDFs, allow inline display; for others, suggest download
	if !strings.HasPrefix(contentType, "image/") && contentType != "application/pdf" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}

	w.Write(content)
}

// renderPage renders a wiki page.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page *wiki.Page) {
	htmlContent, toc, libRequirements := page.Render(s.Renderer)

	// Determine title
	title := page.Pagename
	if len(toc) > 0 {
		title = toc[0].Raw
	}
	if page.Revision != "" {
		title = title + " (" + page.Revision + ")"
	}

	data := map[string]interface{}{
		"templateType":         "page",
		"title":                title,
		"pagename":             page.Pagename,
		"pagepath":             page.Pagepath,
		"revision":             page.Revision,
		"htmlcontent":          template.HTML(htmlContent),
		"toc":                  toc,
		"breadcrumbs":          page.Breadcrumbs(),
		"library_requirements": libRequirements,
	}

	s.renderTemplate(w, r, "page.html", data)
}

// renderNotFound renders a 404 page for a missing wiki page.
func (s *Server) renderNotFound(w http.ResponseWriter, r *http.Request, page *wiki.Page) {
	w.WriteHeader(http.StatusNotFound)

	data := map[string]interface{}{
		"templateType": "generic",
		"pagename":     page.PagenameFull,
		"pagepath":     page.Pagepath,
	}

	s.renderTemplate(w, r, "page404.html", data)
}

// handleEdit handles the page editor.
func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	if path == "" {
		http.Redirect(w, r, "/-/create", http.StatusFound)
		return
	}

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	content := page.Content
	cursorLine := 0
	cursorCh := 0

	if !page.Exists {
		// New page template
		content = "# " + page.Pagename + "\n\n"
		cursorLine = 2
		cursorCh = 0
	}

	// Get attachments for file browser
	files, _ := page.Attachments(100, ".md")
	fileData := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		fileData = append(fileData, map[string]interface{}{
			"filename": f.Filename,
			"url":      "/" + f.Fullpath,
		})
	}

	revision := ""
	if page.Metadata != nil {
		revision = page.Metadata.Revision
	}

	data := map[string]interface{}{
		"templateType":   "editor",
		"pagename":       page.Pagename,
		"pagepath":       page.Pagepath,
		"content_editor": content,
		"cursor_line":    cursorLine,
		"cursor_ch":      cursorCh,
		"revision":       revision,
		"files":          fileData,
		"pages":          []string{}, // TODO: Page index
	}

	s.renderTemplate(w, r, "editor.html", data)
}

// handleSave handles saving a wiki page.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	path := chi.URLParam(r, "path")
	content := r.FormValue("content")
	message := r.FormValue("commit")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get author from logged-in user
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

	if message == "" {
		if page.Exists {
			message = "Updated " + page.Pagename
		} else {
			message = "Created " + page.Pagename
		}
	}

	_, err = page.Save(content, message, author)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/"+page.Pagepath, http.StatusFound)
}

// handleHistory handles viewing page history.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !page.Exists || page.Metadata == nil {
		s.renderNotFound(w, r, page)
		return
	}

	log, err := page.History(0)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Add URLs to log entries
	logData := make([]map[string]interface{}, 0, len(log))
	for _, entry := range log {
		logData = append(logData, map[string]interface{}{
			"revision":     entry.Revision,
			"datetime":     entry.Datetime,
			"author_name":  entry.AuthorName,
			"author_email": entry.AuthorEmail,
			"message":      entry.Message,
			"url":          "/" + page.Pagepath + "?revision=" + entry.Revision,
		})
	}

	revA := ""
	revB := ""
	if len(log) > 1 {
		revB = log[0].Revision
		revA = log[1].Revision
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        page.Pagename + " - History",
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
		"log":          logData,
		"rev_a":        revA,
		"rev_b":        revB,
		"breadcrumbs":  page.Breadcrumbs(),
	}

	s.renderTemplate(w, r, "history.html", data)
}

// handleSource handles viewing page source.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	raw := r.URL.Query().Get("raw") != ""

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}

	if raw {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(page.Content))
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        page.Pagename + " - Source",
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
		"source":       page.Content,
		"breadcrumbs":  page.Breadcrumbs(),
	}

	s.renderTemplate(w, r, "source.html", data)
}

// handleCreate handles creating a new page.
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	if path == "" {
		path = r.FormValue("pagepath")
	}

	if path == "" {
		http.Redirect(w, r, "/-/create", http.StatusFound)
		return
	}

	// Redirect to edit page
	http.Redirect(w, r, "/"+path+"/edit", http.StatusFound)
}

// handleCreateForm handles the create page form.
func (s *Server) handleCreateForm(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Create a new page",
	}
	s.renderTemplate(w, r, "create.html", data)
}

// handleDeleteForm handles the delete confirmation form.
func (s *Server) handleDeleteForm(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Delete " + page.Pagename + "?",
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
	}

	s.renderTemplate(w, r, "delete.html", data)
}

// handleDelete handles deleting a page.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	message := r.FormValue("message")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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

	if message == "" {
		message = page.Pagename + " deleted."
	}

	if err := page.Delete(message, author, true); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/-/changelog", http.StatusFound)
}

// handleRenameForm handles the rename form.
func (s *Server) handleRenameForm(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Rename " + page.Pagename,
		"pagename":     page.PagenameFull,
		"pagepath":     page.Pagepath,
		"new_pagename": page.Pagepath,
	}

	s.renderTemplate(w, r, "rename.html", data)
}

// handleRename handles renaming a page.
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	newPagename := r.FormValue("new_pagename")
	message := r.FormValue("message")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

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

	if message == "" {
		message = "Renamed " + page.Pagename + " to " + newPagename
	}

	if err := page.Rename(newPagename, message, author); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/"+newPagename, http.StatusFound)
}

// handleAttachments handles viewing attachments.
func (s *Server) handleAttachments(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	files, _ := page.Attachments(0, ".md")
	fileData := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		fileData = append(fileData, map[string]interface{}{
			"filename": f.Filename,
			"url":      "/" + f.Fullpath,
			"mimetype": f.Mimetype,
		})
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        page.Pagename + " - Attachments",
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
		"files":        fileData,
		"breadcrumbs":  page.Breadcrumbs(),
	}

	s.renderTemplate(w, r, "attachments.html", data)
}

// handleUploadAttachment handles file uploads.
func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	// Parse multipart form (max 32MB)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Failed to parse form: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Failed to get file: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Get author from logged-in user
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

	message := r.FormValue("message")
	if message == "" {
		message = "Added " + header.Filename
	}

	// Save attachment
	attachmentPath := page.AttachmentDirectoryname + "/" + header.Filename
	_, err = s.Storage.StoreBytes(attachmentPath, content, message, author)
	if err != nil {
		http.Error(w, "Failed to save file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "File uploaded successfully")
	http.Redirect(w, r, "/"+page.Pagepath+"/attachments", http.StatusFound)
}

// handleBlame handles viewing blame information.
func (s *Server) handleBlame(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	revision := r.URL.Query().Get("revision")

	page, err := wiki.NewPage(s.Storage, s.Config, path, revision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !page.Exists || page.Metadata == nil {
		s.renderNotFound(w, r, page)
		return
	}

	blame, err := page.Blame()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        page.Pagename + " - Blame",
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
		"blame":        blame,
		"breadcrumbs":  page.Breadcrumbs(),
	}

	s.renderTemplate(w, r, "blame.html", data)
}

// handleDiff handles viewing diffs between revisions.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	revA := r.URL.Query().Get("rev_a")
	revB := r.URL.Query().Get("rev_b")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}

	diff, err := s.Storage.Diff(revA, revB)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Parse diff for display
	diffLines := parseDiff(diff)

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        page.Pagename + " - Diff",
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
		"diff":         diff,
		"diff_lines":   diffLines,
		"rev_a":        revA,
		"rev_b":        revB,
		"breadcrumbs":  page.Breadcrumbs(),
	}

	s.renderTemplate(w, r, "diff.html", data)
}

// handlePreview handles live preview rendering.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	content := r.FormValue("content")
	path := chi.URLParam(r, "path")

	page, _ := wiki.NewPage(s.Storage, s.Config, path, "")

	htmlContent, toc, libRequirements := s.Renderer.Render(content, page.PageViewURL)

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"preview_content":"` + template.JSEscapeString(htmlContent) + `","preview_toc":"","library_requirements":{"requires_mermaid":` + boolToString(libRequirements.RequiresMermaid) + `,"requires_mathjax":` + boolToString(libRequirements.RequiresMathJax) + `}}`))
	_ = toc
}

func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// handleAbout handles the about page.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "About",
		"version":      s.Version,
	}
	s.renderTemplate(w, r, "about.html", data)
}

// handleSearch handles search.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")

	var keys [][]interface{}

	if query != "" {
		// List all markdown files
		files, _, err := s.Storage.List("", nil, nil)
		if err == nil {
			// Compile regex (fall back to literal search on invalid regex)
			var re *regexp.Regexp
			re, err = regexp.Compile("(?i)" + query)
			if err != nil {
				re = regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
			}

			for _, f := range files {
				if !strings.HasSuffix(f, ".md") {
					continue
				}

				content, err := s.Storage.Load(f, "")
				if err != nil {
					continue
				}

				matches := re.FindAllStringIndex(content, -1)
				if len(matches) > 0 {
					pagepath := strings.TrimSuffix(f, ".md")
					pagename := util.GetPagename(pagepath, false)

					// Check for title in content
					header := util.GetHeader(content)
					if header != "" {
						pagename = header
					}

					// keys format: [sort_key, match_count, pagepath, pagename]
					keys = append(keys, []interface{}{
						pagename,
						len(matches),
						pagepath,
						pagename,
					})
				}
			}
		}
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Search",
		"query":        query,
		"keys":         keys,
	}

	s.renderTemplate(w, r, "search.html", data)
}

// handleChangelog handles the changelog.
func (s *Server) handleChangelog(w http.ResponseWriter, r *http.Request) {
	log, err := s.Storage.Log("", 100)
	if err != nil {
		log = []storage.CommitMetadata{}
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Changelog",
		"log":          log,
	}

	s.renderTemplate(w, r, "changelog.html", data)
}

// handleCommit handles viewing a specific commit.
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	revision := chi.URLParam(r, "revision")

	meta, diff, err := s.Storage.ShowCommit(revision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	// Parse diff for display
	diffLines := parseDiff(diff)

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Commit " + meta.Revision,
		"commit":       meta,
		"diff":         diff,
		"diff_lines":   diffLines,
	}

	s.renderTemplate(w, r, "commit.html", data)
}

// handleRevertForm handles the revert confirmation form.
func (s *Server) handleRevertForm(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login?next="+r.URL.Path, http.StatusFound)
		return
	}

	revision := chi.URLParam(r, "revision")

	meta, _, err := s.Storage.ShowCommit(revision)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Revert Commit " + meta.Revision,
		"commit":       meta,
	}

	s.renderTemplate(w, r, "revert.html", data)
}

// handleRevert handles reverting a commit.
func (s *Server) handleRevert(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login?next="+r.URL.Path, http.StatusFound)
		return
	}

	revision := chi.URLParam(r, "revision")
	message := r.FormValue("message")

	author := storage.Author{
		Name:  user.GetName(),
		Email: user.GetEmail(),
	}

	if err := s.Storage.Revert(revision, message, author); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to revert commit: "+err.Error())
		http.Redirect(w, r, "/-/commit/"+revision, http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Commit reverted successfully")
	http.Redirect(w, r, "/-/changelog", http.StatusFound)
}

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type    string // "add", "remove", "context", "header"
	Content string
}

// parseDiff parses a diff string into structured lines.
func parseDiff(diff string) []DiffLine {
	var lines []DiffLine
	for _, line := range strings.Split(diff, "\n") {
		var dl DiffLine
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			dl.Type = "header"
		} else if strings.HasPrefix(line, "@@") {
			dl.Type = "header"
		} else if strings.HasPrefix(line, "+") {
			dl.Type = "add"
		} else if strings.HasPrefix(line, "-") {
			dl.Type = "remove"
		} else {
			dl.Type = "context"
		}
		dl.Content = line
		lines = append(lines, dl)
	}
	return lines
}

// handlePageIndex handles the page index.
func (s *Server) handlePageIndex(w http.ResponseWriter, r *http.Request) {
	// List all markdown files
	files, _, err := s.Storage.List("", nil, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var pages []map[string]string
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			pagepath := strings.TrimSuffix(f, ".md")
			pages = append(pages, map[string]string{
				"name": util.GetPagename(pagepath, false),
				"path": pagepath,
			})
		}
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Page Index",
		"pages":        pages,
	}

	s.renderTemplate(w, r, "pageindex.html", data)
}

// handleLogin handles the login page.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to home
	user := middleware.GetUser(r)
	if user.IsAuthenticated() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	next := r.URL.Query().Get("next")
	if next == "" {
		next = "/"
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Login",
		"next":         next,
	}
	s.renderTemplate(w, r, "login.html", data)
}

// handleLoginPost handles login form submission.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}

	user, err := s.Auth.Authenticate(r.Context(), email, password)
	if err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Invalid email or password")
		data := map[string]interface{}{
			"templateType": "generic",
			"title":        "Login",
			"email":        email,
			"next":         next,
			"error":        err.Error(),
		}
		s.renderTemplate(w, r, "login.html", data)
		return
	}

	// Login successful
	if err := s.SessionManager.Login(w, r, user.ID); err != nil {
		log.Printf("Session login error: %v", err)
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	// Update last seen
	s.Auth.UpdateUserLastSeen(r.Context(), user.ID)

	http.Redirect(w, r, next, http.StatusFound)
}

// handleLogout handles logout.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.SessionManager.Logout(w, r); err != nil {
		log.Printf("Session logout error: %v", err)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleRegister handles the registration page.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// If registration is disabled, redirect to login
	if s.Config.DisableRegistration {
		http.Redirect(w, r, "/-/login", http.StatusFound)
		return
	}

	// If already logged in, redirect to home
	user := middleware.GetUser(r)
	if user.IsAuthenticated() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Register",
	}
	s.renderTemplate(w, r, "register.html", data)
}

// handleRegisterPost handles registration form submission.
func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	// If registration is disabled, redirect to login
	if s.Config.DisableRegistration {
		http.Redirect(w, r, "/-/login", http.StatusFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	// Validate passwords match
	if password != password2 {
		data := map[string]interface{}{
			"templateType": "generic",
			"title":        "Register",
			"name":         name,
			"email":        email,
			"error":        "Passwords do not match",
		}
		s.renderTemplate(w, r, "register.html", data)
		return
	}

	// Validate password length
	if len(password) < 8 {
		data := map[string]interface{}{
			"templateType": "generic",
			"title":        "Register",
			"name":         name,
			"email":        email,
			"error":        "Password must be at least 8 characters",
		}
		s.renderTemplate(w, r, "register.html", data)
		return
	}

	// Register user
	user, err := s.Auth.Register(r.Context(), name, email, password)
	if err != nil {
		errMsg := "Registration failed"
		if err == auth.ErrEmailExists {
			errMsg = "Email already registered"
		}
		data := map[string]interface{}{
			"templateType": "generic",
			"title":        "Register",
			"name":         name,
			"email":        email,
			"error":        errMsg,
		}
		s.renderTemplate(w, r, "register.html", data)
		return
	}

	// Auto-login if approved
	if user.Approved() {
		if err := s.SessionManager.Login(w, r, user.ID); err != nil {
			log.Printf("Session login error after registration: %v", err)
		}
		s.SessionManager.AddFlashMessage(w, r, "success", "Registration successful! Welcome to the wiki.")
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// If not auto-approved, show message and redirect to login
	s.SessionManager.AddFlashMessage(w, r, "info", "Registration successful! Please wait for admin approval.")
	http.Redirect(w, r, "/-/login", http.StatusFound)
}

// handleSettings handles the settings page.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login?next=/-/settings", http.StatusFound)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Settings",
		"user_name":    user.GetName(),
		"user_email":   user.GetEmail(),
	}
	s.renderTemplate(w, r, "settings.html", data)
}

// handleSettingsPost handles settings form submission.
func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login", http.StatusFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	action := r.FormValue("action")

	switch action {
	case "update_name":
		name := r.FormValue("name")
		if name != "" {
			if err := s.Auth.UpdateUserName(r.Context(), user.ID, name); err != nil {
				s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update name")
			} else {
				s.SessionManager.AddFlashMessage(w, r, "success", "Name updated successfully")
			}
		}

	case "change_password":
		currentPassword := r.FormValue("current_password")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("confirm_password")

		// Verify current password
		if !auth.CheckPassword(currentPassword, user.GetPasswordHash()) {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Current password is incorrect")
			http.Redirect(w, r, "/-/settings", http.StatusFound)
			return
		}

		// Check new passwords match
		if newPassword != confirmPassword {
			s.SessionManager.AddFlashMessage(w, r, "danger", "New passwords do not match")
			http.Redirect(w, r, "/-/settings", http.StatusFound)
			return
		}

		// Check password length
		if len(newPassword) < 8 {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Password must be at least 8 characters")
			http.Redirect(w, r, "/-/settings", http.StatusFound)
			return
		}

		// Update password
		if err := s.Auth.UpdatePassword(r.Context(), user.ID, newPassword); err != nil {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update password")
		} else {
			s.SessionManager.AddFlashMessage(w, r, "success", "Password updated successfully")
		}
	}

	http.Redirect(w, r, "/-/settings", http.StatusFound)
}

// requireAdmin is a helper that checks admin access and redirects if not authorized.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login?next="+r.URL.Path, http.StatusFound)
		return false
	}
	if !user.Admin() {
		http.Error(w, "Access denied", http.StatusForbidden)
		return false
	}
	return true
}

// handleAdmin handles the admin dashboard.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	// Get stats
	users, _ := s.Auth.ListUsers(r.Context())
	files, _, _ := s.Storage.List("", nil, nil)

	pageCount := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			pageCount++
		}
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Admin Dashboard",
		"user_count":   len(users),
		"page_count":   pageCount,
		"version":      s.Version,
	}
	s.renderTemplate(w, r, "admin.html", data)
}

// handleAdminUsers handles the user list page.
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "Failed to list users", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "User Management",
		"users":        users,
	}
	s.renderTemplate(w, r, "admin_users.html", data)
}

// handleAdminUserEdit handles the user edit form.
func (s *Server) handleAdminUserEdit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	user, err := s.Auth.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	data := map[string]interface{}{
		"templateType": "generic",
		"title":        "Edit User: " + user.GetName(),
		"edit_user":    user,
	}
	s.renderTemplate(w, r, "admin_user_edit.html", data)
}

// handleAdminUserSave handles saving user changes.
func (s *Server) handleAdminUserSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get current user to update
	user, err := s.Auth.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Update user fields
	name := r.FormValue("name")
	isApproved := r.FormValue("is_approved") == "on"
	isAdmin := r.FormValue("is_admin") == "on"
	allowRead := r.FormValue("allow_read") == "on"
	allowWrite := r.FormValue("allow_write") == "on"
	allowUpload := r.FormValue("allow_upload") == "on"

	params := db.UpdateUserParams{
		ID:             id,
		Name:           name,
		Email:          user.Email,
		PasswordHash:   user.PasswordHash,
		IsApproved:     toSqlNullBool(isApproved),
		IsAdmin:        toSqlNullBool(isAdmin),
		EmailConfirmed: user.EmailConfirmed,
		AllowRead:      toSqlNullBool(allowRead),
		AllowWrite:     toSqlNullBool(allowWrite),
		AllowUpload:    toSqlNullBool(allowUpload),
	}

	if err := s.DB.Queries.UpdateUser(r.Context(), params); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update user")
		http.Redirect(w, r, fmt.Sprintf("/-/admin/users/%d", id), http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "User updated successfully")
	http.Redirect(w, r, "/-/admin/users", http.StatusFound)
}

// handleAdminUserDelete handles deleting a user.
func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusBadRequest)
		return
	}

	// Don't allow deleting yourself
	currentUser := middleware.GetUser(r)
	if currentUser.ID == id {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Cannot delete your own account")
		http.Redirect(w, r, "/-/admin/users", http.StatusFound)
		return
	}

	if err := s.Auth.DeleteUser(r.Context(), id); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to delete user")
	} else {
		s.SessionManager.AddFlashMessage(w, r, "success", "User deleted successfully")
	}

	http.Redirect(w, r, "/-/admin/users", http.StatusFound)
}

// handleAdminSettings handles the admin settings page.
func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	ctx := r.Context()

	// Get current site settings from preferences or config
	siteSettings := s.getSiteSettings(ctx)

	// Get current issue tags and categories from preferences or config
	issueTags := s.getAvailableTags(ctx)
	issueCategories := s.getAvailableCategories(ctx)

	data := map[string]interface{}{
		"templateType":     "generic",
		"title":            "Site Settings",
		"site_settings":    s.Config,
		"current_site":     siteSettings,
		"issue_tags":       strings.Join(issueTags, ", "),
		"issue_categories": strings.Join(issueCategories, ", "),
	}
	s.renderTemplate(w, r, "admin_settings.html", data)
}

// handleAdminSettingsSave handles saving admin settings.
func (s *Server) handleAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	// Note: Runtime config changes are limited. Most settings require restart.
	// This is a placeholder for future implementation.
	s.SessionManager.AddFlashMessage(w, r, "info", "Settings changes require a restart to take effect")
	http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
}

// handleAdminSiteSettingsSave handles saving site name and logo.
func (s *Server) handleAdminSiteSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	siteName := strings.TrimSpace(r.FormValue("site_name"))
	siteLogo := strings.TrimSpace(r.FormValue("site_logo"))

	ctx := r.Context()

	// Save site name to preferences
	if siteName != "" {
		params := db.UpsertPreferenceParams{
			Name:  "site_name",
			Value: toSqlNullString(siteName),
		}
		if err := s.DB.Queries.UpsertPreference(ctx, params); err != nil {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save site name")
			http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
			return
		}
	}

	// Save site logo to preferences (can be empty to use default)
	params := db.UpsertPreferenceParams{
		Name:  "site_logo",
		Value: toSqlNullString(siteLogo),
	}
	if err := s.DB.Queries.UpsertPreference(ctx, params); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save site logo")
		http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Site settings updated successfully")
	http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
}

// handleAdminIssueSettingsSave handles saving issue tracker configuration (categories and tags).
func (s *Server) handleAdminIssueSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	categoriesInput := r.FormValue("issue_categories")
	tagsInput := r.FormValue("issue_tags")

	// Parse and clean the categories
	var cleanCategories []string
	for _, cat := range strings.Split(categoriesInput, ",") {
		cat = strings.TrimSpace(cat)
		if cat != "" {
			cleanCategories = append(cleanCategories, cat)
		}
	}

	// Parse and clean the tags
	var cleanTags []string
	for _, tag := range strings.Split(tagsInput, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}

	// Save categories to preferences
	catParams := db.UpsertPreferenceParams{
		Name:  "issue_categories",
		Value: toSqlNullString(strings.Join(cleanCategories, ",")),
	}
	if err := s.DB.Queries.UpsertPreference(ctx, catParams); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save issue categories")
		http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
		return
	}

	// Save tags to preferences
	tagParams := db.UpsertPreferenceParams{
		Name:  "issue_tags",
		Value: toSqlNullString(strings.Join(cleanTags, ",")),
	}
	if err := s.DB.Queries.UpsertPreference(ctx, tagParams); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save issue tags")
		http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue settings updated successfully")
	http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
}

// handleFeed handles the RSS feed.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	changelog, _ := s.Storage.Log("", 20)

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
<title>%s</title>
<link>%s</link>
<description>Recent changes</description>
`, html.EscapeString(s.Config.SiteName), s.Config.SiteURL)

	for _, entry := range changelog {
		fmt.Fprintf(w, `<item>
<title>%s</title>
<link>%s/-/commit/%s</link>
<pubDate>%s</pubDate>
<author>%s</author>
</item>
`, html.EscapeString(entry.Message), s.Config.SiteURL, entry.Revision, entry.Datetime.Format(time.RFC1123Z), html.EscapeString(entry.AuthorEmail))
	}

	fmt.Fprint(w, `</channel>
</rss>`)
}

// handleAtomFeed handles the Atom feed.
func (s *Server) handleAtomFeed(w http.ResponseWriter, r *http.Request) {
	changelog, _ := s.Storage.Log("", 20)

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<title>%s</title>
<link href="%s"/>
<id>%s/</id>
`, html.EscapeString(s.Config.SiteName), s.Config.SiteURL, s.Config.SiteURL)

	if len(changelog) > 0 {
		fmt.Fprintf(w, `<updated>%s</updated>
`, changelog[0].Datetime.Format(time.RFC3339))
	}

	for _, entry := range changelog {
		fmt.Fprintf(w, `<entry>
<title>%s</title>
<link href="%s/-/commit/%s"/>
<id>%s/-/commit/%s</id>
<updated>%s</updated>
<author><name>%s</name></author>
</entry>
`, html.EscapeString(entry.Message), s.Config.SiteURL, entry.Revision, s.Config.SiteURL, entry.Revision, entry.Datetime.Format(time.RFC3339), html.EscapeString(entry.AuthorName))
	}

	fmt.Fprint(w, `</feed>`)
}

// handleRobotsTxt handles the robots.txt file.
func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, `User-agent: *
Allow: /
Sitemap: %s/-/sitemap.xml
`, s.Config.SiteURL)
}

// handleSitemap handles the sitemap.xml file.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	files, _, _ := s.Storage.List("", nil, nil)

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`)

	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			pagepath := strings.TrimSuffix(f, ".md")
			mtime, _ := s.Storage.Mtime(f)
			fmt.Fprintf(w, `<url>
<loc>%s/%s</loc>
<lastmod>%s</lastmod>
</url>
`, s.Config.SiteURL, pagepath, mtime.Format("2006-01-02"))
		}
	}

	fmt.Fprint(w, `</urlset>`)
}

// handleHealthCheck handles the health check endpoint.
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","version":"%s"}`, s.Version)
}

// parseInt64 parses a string to int64.
func parseInt64(s string) (int64, error) {
	var i int64
	_, err := fmt.Sscanf(s, "%d", &i)
	return i, err
}

// toSqlNullBool converts a bool to sql.NullBool.
func toSqlNullBool(b bool) sql.NullBool {
	return sql.NullBool{Bool: b, Valid: true}
}

// toSqlNullString converts a string to sql.NullString.
func toSqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

// toSqlNullInt64 converts an int64 to sql.NullInt64.
func toSqlNullInt64(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: true}
}

// toSqlNullTime converts a time.Time to sql.NullTime.
func toSqlNullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

// handleDraftSave saves a draft for the current user.
func (s *Server) handleDraftSave(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	content := r.FormValue("content")
	cursorLine, _ := parseInt64(r.FormValue("cursor_line"))
	cursorCh, _ := parseInt64(r.FormValue("cursor_ch"))

	user := middleware.GetUser(r)
	authorEmail := user.GetEmail()
	if authorEmail == "" {
		// For anonymous users, use session-based identifier
		authorEmail = "anonymous@example.com"
	}

	// Get current revision
	page, _ := wiki.NewPage(s.Storage, s.Config, path, "")
	revision := ""
	if page.Metadata != nil {
		revision = page.Metadata.Revision
	}

	// Upsert draft
	params := db.UpsertDraftParams{
		Pagepath:    toSqlNullString(path),
		Revision:    toSqlNullString(revision),
		AuthorEmail: toSqlNullString(authorEmail),
		Content:     toSqlNullString(content),
		CursorLine:  toSqlNullInt64(cursorLine),
		CursorCh:    toSqlNullInt64(cursorCh),
		Datetime:    toSqlNullTime(time.Now()),
	}

	if err := s.DB.Queries.UpsertDraft(r.Context(), params); err != nil {
		log.Printf("Failed to save draft: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"success":false,"error":"Failed to save draft"}`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"success":true}`)
}

// handleDraftLoad loads a draft for the current user.
func (s *Server) handleDraftLoad(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	user := middleware.GetUser(r)
	authorEmail := user.GetEmail()
	if authorEmail == "" {
		authorEmail = "anonymous@example.com"
	}

	params := db.GetDraftParams{
		Pagepath:    toSqlNullString(path),
		AuthorEmail: toSqlNullString(authorEmail),
	}

	draft, err := s.DB.Queries.GetDraft(r.Context(), params)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"found":false}`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"found":true,"content":%q,"cursor_line":%d,"cursor_ch":%d,"revision":%q}`,
		draft.Content.String,
		draft.CursorLine.Int64,
		draft.CursorCh.Int64,
		draft.Revision.String)
}

// handleDraftDelete deletes a draft for the current user.
func (s *Server) handleDraftDelete(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	user := middleware.GetUser(r)
	authorEmail := user.GetEmail()
	if authorEmail == "" {
		authorEmail = "anonymous@example.com"
	}

	params := db.DeleteDraftParams{
		Pagepath:    toSqlNullString(path),
		AuthorEmail: toSqlNullString(authorEmail),
	}

	s.DB.Queries.DeleteDraft(r.Context(), params)

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"success":true}`)
}
