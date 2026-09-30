// Package handlers provides HTTP handlers for GopherWiki.
package handlers

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sa/gopherwiki/internal/auth"
	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/quarto"
	"github.com/sa/gopherwiki/internal/rendercache"
	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/wiki"
)

// RenderService performs gated rendering of computational pages and serves
// cached output. *quarto.Service is the production implementation; it is
// optional and nil when computational pages are disabled or the toolchain is
// absent.
type RenderService interface {
	// Available reports whether gated renders (code execution) can be performed
	// and cached.
	Available() bool
	// ExportAvailable reports whether Quarto exports can be generated (needs only
	// the toolchain, not the render cache or the execution feature flag).
	ExportAvailable() bool
	// Render executes a page and stores its output, returning the cache entry.
	Render(ctx context.Context, in quarto.Input) (rendercache.Entry, error)
	// Run executes a page without caching and returns its HTML and executed
	// markdown, for a report run the caller stores.
	Run(ctx context.Context, in quarto.Input) (html, markdown []byte, err error)
	// Cached returns the stored render for a page's current source, if present.
	Cached(ctx context.Context, source, engine string) (rendercache.Entry, bool, error)
	// Invalidate drops any cached renders for a page.
	Invalidate(ctx context.Context, pagepath string) error
	// Export renders a page to the named format, returning the bytes and the
	// resolved format descriptor. Never cached; execution is always disabled.
	Export(ctx context.Context, in quarto.Input, format string) ([]byte, quarto.ExportFormat, error)
	// ExportFormats lists the Quarto-produced export formats available.
	ExportFormats() []quarto.ExportFormat
}

// siteSettingsCacheTTL is how long cached site settings remain valid.
const siteSettingsCacheTTL = 60 * time.Second

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
	// RenderService is the optional computational-page renderer. Nil disables
	// the render endpoint and makes computational pages show the render-pending
	// placeholder.
	RenderService RenderService

	// Site settings cache
	ssMu       sync.RWMutex
	ssCache    *SiteSettings
	ssCachedAt time.Time
}

// NewServer creates a new Server with the given dependencies.
func NewServer(cfg *config.Config, store storage.Storage, database *db.Database, version string) (*Server, error) {
	rend := renderer.New(cfg)
	authService := auth.New(cfg, database.Queries)
	sessionManager := middleware.NewSessionManager(cfg.SecretKey, cfg.SecureCookie, database.Queries)
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
// Results are cached with a short TTL to avoid DB queries on every request.
func (s *Server) getSiteSettings(ctx context.Context) SiteSettings {
	s.ssMu.RLock()
	if s.ssCache != nil && time.Since(s.ssCachedAt) < siteSettingsCacheTTL {
		cached := *s.ssCache
		s.ssMu.RUnlock()
		return cached
	}
	s.ssMu.RUnlock()

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

	s.ssMu.Lock()
	s.ssCache = &settings
	s.ssCachedAt = time.Now()
	s.ssMu.Unlock()

	return settings
}

// InvalidateSiteSettingsCache clears the cached site settings.
func (s *Server) InvalidateSiteSettingsCache() {
	s.ssMu.Lock()
	s.ssCachedAt = time.Time{}
	s.ssMu.Unlock()
}

// renderTemplate renders a template with common context.
func (s *Server) renderTemplate(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add common context
	data["config"] = s.Config
	data["Version"] = s.Version
	data["csrf_token"] = middleware.GetCSRFToken(r)

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
		"review": s.PermissionChecker.HasPermission(r, middleware.PermissionReview),
	}

	// Add sidebar page tree when configured
	if s.Config.SidebarMenutreeMode != "" {
		if tree, err := s.Wiki.PageTree(r.Context()); err == nil {
			if tree = s.filterTree(s.visibleOrHide(r), tree); len(tree) > 0 {
				data["sidebar_tree"] = tree
			}
		}
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
	// Set ETag based on git commit revision for cache validation. For a
	// computational page the embedded iframe and its rendered output can change
	// without a new commit (a re-render, or a render-pipeline change), so fold the
	// render state into the ETag; otherwise a browser 304s and reuses stale page
	// chrome after a re-render.
	view, err := s.computationalView(r, page)
	if errors.Is(err, errRunNotFound) {
		s.renderNotFound(w, r, page)
		return
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to load report runs")
		return
	}

	cited, err := s.citedRuns(r.Context(), page)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to resolve cited report runs")
		return
	}

	validation, err := s.validationState(r.Context(), page)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to load validation state")
		return
	}
	// Validating a revision changes the page without a new commit.
	validatedSuffix := ""
	if validation.Validated {
		validatedSuffix = "-validated"
	}

	if page.Metadata != nil && page.Metadata.RevisionFull != "" {
		etag := `"` + page.Metadata.RevisionFull + view.etagSuffix() + citedETagSuffix(cited) + validatedSuffix + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	htmlContent, toc, libRequirements := s.renderPageContent(page, view)
	data := NewPageData(page, template.HTML(htmlContent), toc, libRequirements)
	data["export_formats"] = s.exportFormatLinks()
	data["report_runs"] = view.runs
	data["current_run"] = view.runID
	data["cited_runs"] = cited
	data["validation"] = validation

	// Fetch backlinks
	if backlinks, err := s.Wiki.Backlinks(r.Context(), page.Pagepath); err == nil {
		if backlinks = s.filterPaths(s.visibleOrHide(r), backlinks); len(backlinks) > 0 {
			data["backlinks"] = backlinks
		}
	}

	s.renderTemplate(w, r, "page.html", data)
}

// errRunNotFound reports a ?run= that names no run of the page.
var errRunNotFound = errors.New("report run not found")

// computationalView is what a computational page's view embeds.
type computationalView struct {
	src   string         // iframe source; "" shows the page's own rendering
	key   string         // identifies the embedded output in the page ETag
	runs  []db.ReportRun // the page's report runs, newest first
	runID int64          // the run shown, or 0
}

// etagSuffix folds the embedded output into the page ETag, so a new run or
// render is not masked by a 304.
func (v computationalView) etagSuffix() string {
	if v.key == "" {
		return ""
	}
	return "-" + v.key
}

// computationalView picks the output a computational page shows: the run named
// by ?run=, else the newest run, else the cached render of the current source.
// Runs are permanent records, so they are shown even when rendering is off.
func (s *Server) computationalView(r *http.Request, page *wiki.Page) (computationalView, error) {
	var v computationalView
	if !page.IsComputational {
		return v, nil
	}

	runs, err := s.DB.Queries.ListReportRuns(r.Context(), page.Filename)
	if err != nil {
		return v, err
	}
	v.runs = runs

	if param := r.URL.Query().Get("run"); param != "" {
		id, _ := parseInt64(param)
		for _, run := range runs {
			if run.ID == id {
				v.runID = id
			}
		}
		if v.runID == 0 {
			return v, errRunNotFound
		}
	} else if len(runs) > 0 {
		v.runID = runs[0].ID
	}
	if v.runID != 0 {
		v.src = fmt.Sprintf("%s/rendered?run=%d", page.PageViewURL, v.runID)
		v.key = fmt.Sprintf("run%d", v.runID)
		return v, nil
	}

	if s.RenderService == nil || !s.RenderService.Available() {
		return v, nil
	}
	if entry, ok, err := s.RenderService.Cached(r.Context(), page.Content, pageEngine(page)); err == nil && ok {
		v.src = page.PageViewURL + "/rendered"
		v.key = entry.Key
	} else {
		// The placeholder view must revalidate once a render lands.
		v.key = "pending"
	}
	return v, nil
}

// citedRun is the state of one report run that a page cites in its frontmatter
// `sources`.
type citedRun struct {
	Page     string // sanitized path of the cited page
	Run      int64
	Period   string // "" when the page has no run with that ID
	NewerRun int64  // the newest later run of the same period, or 0
}

// citedRuns resolves the report runs a page cites. A cited run is out of date
// when a later run exists for the same period, as after a re-run on restated
// data.
func (s *Server) citedRuns(ctx context.Context, page *wiki.Page) ([]citedRun, error) {
	var cited []citedRun
	for _, src := range page.Frontmatter.ReportSources() {
		target, err := wiki.NewPage(s.Storage, s.Config, src.Page, "")
		if err != nil {
			return nil, err
		}
		c := citedRun{Page: target.Pagepath, Run: src.Run}
		runs, err := s.DB.Queries.ListReportRuns(ctx, target.Filename)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if run.ID == src.Run {
				c.Period = run.Period
			}
		}
		// Runs are newest first, so the first later match is the newest.
		for _, run := range runs {
			if c.Period != "" && run.Period == c.Period && run.ID > src.Run {
				c.NewerRun = run.ID
				break
			}
		}
		cited = append(cited, c)
	}
	return cited, nil
}

// citedETagSuffix folds the state of cited runs into the page ETag, so a new
// run of a cited report is not masked by a 304.
func citedETagSuffix(cited []citedRun) string {
	var b strings.Builder
	for _, c := range cited {
		fmt.Fprintf(&b, "-%d.%d", c.Run, c.NewerRun)
		if c.Period == "" {
			b.WriteString("x")
		}
	}
	return b.String()
}

// renderPageContent produces the main HTML content for a page view. Plain pages
// render in-process via goldmark. A computational page with output embeds it via
// an iframe pointing at the rendered-output endpoint; otherwise it falls back to
// the render-pending placeholder. On-view execution never happens here.
func (s *Server) renderPageContent(page *wiki.Page, view computationalView) (string, []renderer.TOCEntry, renderer.LibraryRequirements) {
	if view.src != "" {
		return computationalIframe(view.src), nil, renderer.LibraryRequirements{}
	}
	return page.Render(s.Renderer)
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
	return strconv.ParseInt(s, 10, 64)
}

