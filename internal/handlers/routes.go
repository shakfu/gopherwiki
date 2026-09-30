package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// RouteInfo describes a named route for URL generation.
type RouteInfo struct {
	ParamName string // empty for static routes
	Pattern   string // fmt pattern (e.g. "/%s/edit") or literal path
	Fallback  string // URL when param is missing (parameterized routes only)
}

// RouteMap maps route names to their URL patterns.
// This is the single source of truth used by the urlFor template function.
var RouteMap = map[string]RouteInfo{
	// Static routes
	"index":     {Pattern: "/"},
	"login":     {Pattern: "/-/login"},
	"logout":    {Pattern: "/-/logout"},
	"register":  {Pattern: "/-/register"},
	"settings":  {Pattern: "/-/settings"},
	"search":    {Pattern: "/-/search"},
	"changelog": {Pattern: "/-/changelog"},
	"about":     {Pattern: "/-/about"},
	"pageindex": {Pattern: "/-/pageindex"},
	"issues":    {Pattern: "/-/issues"},
	"issue_new": {Pattern: "/-/issues/new"},

	// Parameterized routes
	"view":         {ParamName: "path", Pattern: "/%s", Fallback: "/"},
	"edit":         {ParamName: "path", Pattern: "/%s/edit", Fallback: "/edit"},
	"save":         {ParamName: "path", Pattern: "/%s/save", Fallback: "/save"},
	"history":      {ParamName: "path", Pattern: "/%s/history", Fallback: "/history"},
	"blame":        {ParamName: "path", Pattern: "/%s/blame", Fallback: "/blame"},
	"diff":         {ParamName: "path", Pattern: "/%s/diff", Fallback: "/diff"},
	"source":       {ParamName: "path", Pattern: "/%s/source", Fallback: "/source"},
	"export":       {ParamName: "path", Pattern: "/%s/export", Fallback: "/export"},
	"create":       {ParamName: "path", Pattern: "/%s/create", Fallback: "/-/create"},
	"attachments":  {ParamName: "pagepath", Pattern: "/%s/attachments", Fallback: "/attachments"},
	"static":       {ParamName: "filename", Pattern: "/static/%s", Fallback: "/static/"},
	"commit":       {ParamName: "revision", Pattern: "/-/commit/%s", Fallback: "/-/changelog"},
	"revert":       {ParamName: "revision", Pattern: "/-/commit/%s/revert", Fallback: "/-/changelog"},
	"issue":        {ParamName: "id", Pattern: "/-/issues/%s", Fallback: "/-/issues"},
	"issue_edit":   {ParamName: "id", Pattern: "/-/issues/%s/edit", Fallback: "/-/issues"},
	"issue_close":  {ParamName: "id", Pattern: "/-/issues/%s/close", Fallback: "/-/issues"},
	"issue_reopen": {ParamName: "id", Pattern: "/-/issues/%s/reopen", Fallback: "/-/issues"},
	"issue_delete": {ParamName: "id", Pattern: "/-/issues/%s/delete", Fallback: "/-/issues"},
}

// URLFor generates a URL for the named route with optional parameters.
func URLFor(name string, args ...string) string {
	route, ok := RouteMap[name]
	if !ok {
		return "/"
	}
	if route.ParamName == "" {
		return route.Pattern
	}
	if len(args) >= 2 && args[0] == route.ParamName {
		return fmt.Sprintf(route.Pattern, args[1])
	}
	return route.Fallback
}

// Routes returns the Chi router with all routes configured.
func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()

	// Recover from handler panics so a single bad request cannot take down
	// the connection (or the process). Outermost so it wraps everything.
	r.Use(middleware.Recoverer)

	// Baseline security headers on every response.
	r.Use(securityHeaders)

	// Session middleware (adds user to context)
	r.Use(s.SessionManager.Middleware)

	// Bearer-token authentication for the JSON API (replaces the session user).
	r.Use(s.SessionManager.TokenAuth)

	// CSRF protection on state-changing requests. Disabled under Testing so the
	// existing handler tests need not perform the token dance; covered directly
	// by middleware-level tests.
	if !s.Config.Testing {
		r.Use(s.SessionManager.CSRFProtect)
	}

	// Static files (with long-lived cache headers)
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(s.StaticFS)))
	r.Handle("/static/*", staticCacheHandler(staticHandler))

	// Local Observable JS library mirror (offline OJS). Served from the operator-
	// provided directory when configured; the rendered OJS pages point here
	// instead of the Observable CDNs.
	if s.Config.OJSLibsDir != "" {
		libsHandler := http.StripPrefix("/ojs-libs/", http.FileServer(http.Dir(s.Config.OJSLibsDir)))
		r.Handle("/ojs-libs/*", staticCacheHandler(libsHandler))
	}

	// Special routes (starting with /-/)
	r.Route("/-", func(r chi.Router) {
		// Public routes (no permission required)
		r.Get("/login", s.handleLogin)
		r.Post("/login", s.handleLoginPost)
		r.Post("/logout", s.handleLogout)
		r.Get("/register", s.handleRegister)
		r.Post("/register", s.handleRegisterPost)
		r.Get("/health", s.handleHealthCheck)
		r.Get("/robots.txt", s.handleRobotsTxt)
		r.Get("/about", s.handleAbout)

		// Read-protected routes
		r.Group(func(r chi.Router) {
			r.Use(s.PermissionChecker.RequireRead)
			r.Get("/", s.handleIndex)
			r.Get("/search", s.handleSearch)
			r.Post("/search", s.handleSearch)
			r.Get("/search/partial", s.handleSearchPartial)
			r.Get("/search/dropdown", s.handleSearchDropdown)
			r.Get("/changelog", s.handleChangelog)
			r.Get("/commit/{revision}", s.handleCommit)
			r.Get("/pageindex", s.handlePageIndex)
			r.Get("/lint", s.handleLint)
			r.Get("/feed", s.handleFeed)
			r.Get("/feed.rss", s.handleFeed)
			r.Get("/feed.atom", s.handleAtomFeed)
			r.Get("/sitemap.xml", s.handleSitemap)
			r.Get("/settings", s.handleSettings)
			r.Post("/settings", s.handleSettingsPost)
			// Issue reading
			r.Get("/issues", s.handleIssueList)
			r.Get("/issues/{id}", s.handleIssueView)
		})

		// Write-protected routes
		r.Group(func(r chi.Router) {
			r.Use(s.PermissionChecker.RequireWrite)
			r.Get("/create", s.handleCreateForm)
			r.Post("/create", s.handleCreate)
			r.Get("/commit/{revision}/revert", s.handleRevertForm)
			r.Post("/commit/{revision}/revert", s.handleRevert)
			// Issue writing
			r.Get("/issues/new", s.handleIssueNew)
			r.Post("/issues/new", s.handleIssueCreate)
			r.Get("/issues/{id}/edit", s.handleIssueEdit)
			r.Post("/issues/{id}/edit", s.handleIssueUpdate)
			r.Post("/issues/{id}/close", s.handleIssueClose)
			r.Post("/issues/{id}/reopen", s.handleIssueReopen)
			r.Post("/issues/{id}/comment", s.handleIssueCommentCreate)
		})

		// Admin-protected routes
		r.Group(func(r chi.Router) {
			r.Use(s.PermissionChecker.RequireAdmin)
			r.Get("/admin", s.handleAdmin)
			r.Get("/admin/users", s.handleAdminUsers)
			r.Get("/admin/users/{id}", s.handleAdminUserEdit)
			r.Post("/admin/users/{id}", s.handleAdminUserSave)
			r.Post("/admin/users/{id}/delete", s.handleAdminUserDelete)
			r.Get("/admin/tokens", s.handleAdminTokens)
			r.Post("/admin/tokens", s.handleAdminTokenCreate)
			r.Post("/admin/tokens/{id}/delete", s.handleAdminTokenDelete)
			r.Get("/admin/settings", s.handleAdminSettings)
			r.Post("/admin/settings", s.handleAdminSettingsSave)
			r.Post("/admin/site-settings", s.handleAdminSiteSettingsSave)
			r.Post("/admin/issue-settings", s.handleAdminIssueSettingsSave)
			r.Post("/issues/{id}/delete", s.handleIssueDelete)
			r.Post("/issues/{id}/comment/{commentId}/delete", s.handleIssueCommentDelete)
		})

		// JSON API v1
		r.Route("/api/v1", func(r chi.Router) {
			// Read-protected API routes
			r.Group(func(r chi.Router) {
				r.Use(s.PermissionChecker.RequireRead)
				r.Get("/pages", s.handleAPIPageList)
				r.Get("/pages/*", s.handleAPIPage)
				r.Get("/search", s.handleAPISearch)
				r.Get("/changelog", s.handleAPIChangelog)
				r.Get("/lint", s.handleAPILint)
				r.Get("/issues", s.handleAPIIssueList)
				r.Get("/issues/{id}", s.handleAPIIssueGet)
				r.Get("/issues/{id}/comments", s.handleAPIIssueComments)
			})

			// Write-protected API routes
			r.Group(func(r chi.Router) {
				r.Use(s.PermissionChecker.RequireWrite)
				r.Put("/pages/*", s.handleAPIPage)
				r.Post("/batch", s.handleAPIBatchSave)
				r.Delete("/pages/*", s.handleAPIPage)
				r.Post("/issues", s.handleAPIIssueCreate)
				r.Put("/issues/{id}", s.handleAPIIssueUpdate)
				r.Post("/issues/{id}/close", s.handleAPIIssueClose)
				r.Post("/issues/{id}/reopen", s.handleAPIIssueReopen)
				r.Post("/issues/{id}/comments", s.handleAPIIssueCommentCreate)
			})

			// Admin-protected API routes
			r.Group(func(r chi.Router) {
				r.Use(s.PermissionChecker.RequireAdmin)
				r.Delete("/issues/{id}", s.handleAPIIssueDelete)
				r.Delete("/issues/{id}/comments/{commentId}", s.handleAPIIssueCommentDelete)
			})
		})
	})

	// Index/home page (read-protected)
	r.Group(func(r chi.Router) {
		r.Use(s.PermissionChecker.RequireRead)
		r.Get("/", s.handleIndex)
	})

	// Wiki page routes. A chi parameter cannot span "/", so a pattern such as
	// "/{path}/edit" never matched a nested page like "docs/setup". A
	// catch-all splits a known trailing action off the path instead.
	actions := s.pageActions()
	r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
		s.dispatchPage(w, r, actions)
	})

	return r
}

// pageAction is the handler for one action on a wiki page, wrapped in the
// middleware that checks its permission.
type pageAction struct {
	require func(http.Handler) http.Handler
	handler http.HandlerFunc
}

// pageActions maps "METHOD action" to its handler. The action is the last path
// segment, as in "/docs/setup/edit".
func (s *Server) pageActions() map[string]pageAction {
	read := s.PermissionChecker.RequireRead
	write := s.PermissionChecker.RequireWrite
	return map[string]pageAction{
		"GET rendered":     {read, s.handleRendered},
		"GET export":       {read, s.handleExport},
		"GET history":      {read, s.handleHistory},
		"GET source":       {read, s.handleSource},
		"GET blame":        {read, s.handleBlame},
		"GET diff":         {read, s.handleDiff},
		"GET attachments":  {read, s.handleAttachments},
		"GET draft":        {read, s.handleDraftLoad},
		"GET edit":         {write, s.handleEdit},
		"POST save":        {write, s.handleSave},
		"GET create":       {write, s.handleCreate},
		"GET delete":       {write, s.handleDeleteForm},
		"POST delete":      {write, s.handleDelete},
		"GET rename":       {write, s.handleRenameForm},
		"POST rename":      {write, s.handleRename},
		"POST preview":     {write, s.handlePreview},
		"POST draft":       {write, s.handleDraftSave},
		"DELETE draft":     {write, s.handleDraftDelete},
		"POST render":      {write, s.handleRender},
		"POST approve":     {s.PermissionChecker.RequireReview, s.handleApprove},
		"POST validate":    {s.PermissionChecker.RequireReview, s.handleValidate},
		"POST attachments": {s.PermissionChecker.RequireUpload, s.handleUploadAttachment},
	}
}

// dispatchPage routes a request under a wiki page path. A GET whose last
// segment is not an action views the page, or serves an attachment. The page
// path is exposed to handlers as the "path" URL parameter.
func (s *Server) dispatchPage(w http.ResponseWriter, r *http.Request, actions map[string]pageAction) {
	path := chi.URLParam(r, "*")
	route := pageAction{s.PermissionChecker.RequireRead, s.handleView}
	found := r.Method == http.MethodGet
	view := true

	if i := strings.LastIndex(path, "/"); i > 0 {
		action := path[i+1:]
		if a, ok := actions[r.Method+" "+action]; ok {
			route, found, path, view = a, true, path[:i], false
		} else if isPageAction(actions, action) {
			found = false
		}
	}
	if !found {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	// Every action on a hidden page answers as if the page did not exist.
	hidden, err := s.pageHidden(r, path, view)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if hidden {
		http.NotFound(w, r)
		return
	}

	chi.RouteContext(r.Context()).URLParams.Add("path", path)
	route.require(route.handler).ServeHTTP(w, r)
}

// isPageAction reports whether name is an action for any method.
func isPageAction(actions map[string]pageAction, name string) bool {
	for key := range actions {
		if strings.HasSuffix(key, " "+name) {
			return true
		}
	}
	return false
}

// contentSecurityPolicy restricts where resources may be loaded from.
//
// All first-party assets (including the self-hosted MathJax and Mermaid bundles)
// are same-origin, and every script lives in an external file -- inline on*
// handlers and inline <script> blocks were removed in favour of delegated
// listeners (gopherwiki-actions.js) and externalized page scripts. That lets
// script-src be a strict 'self' with no 'unsafe-inline' and no 'unsafe-eval'
// (verified: app JS has no eval/Function; htmx uses no expression-filter
// triggers; MathJax's only `new Function` is a dead globalThis polyfill).
//   - style-src keeps 'unsafe-inline' because MathJax/Mermaid inject <style>
//     elements at runtime and several templates use inline style attributes;
//     CSP cannot nonce runtime-injected styles, so this one is unavoidable.
//   - img-src is permissive so wiki pages may embed external images; data:/blob:
//     cover MathJax/Mermaid and editor previews.
//   - default-src 'self' also constrains connect-src, so an injected script
//     (were one to slip past script-src) could not exfiltrate to a foreign
//     origin via fetch/XHR/beacon.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https: http:; " +
	"font-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'self'"

// securityHeaders sets baseline security response headers on every request.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}

// staticCacheHandler wraps a handler to add Cache-Control headers for static assets.
func staticCacheHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
