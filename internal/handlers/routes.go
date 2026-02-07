package handlers

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
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

	// Session middleware (adds user to context)
	r.Use(s.SessionManager.Middleware)

	// Static files (with long-lived cache headers)
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(s.StaticFS)))
	r.Handle("/static/*", staticCacheHandler(staticHandler))

	// Special routes (starting with /-/)
	r.Route("/-", func(r chi.Router) {
		// Public routes (no permission required)
		r.Get("/login", s.handleLogin)
		r.Post("/login", s.handleLoginPost)
		r.Get("/logout", s.handleLogout)
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
			r.Get("/changelog", s.handleChangelog)
			r.Get("/commit/{revision}", s.handleCommit)
			r.Get("/pageindex", s.handlePageIndex)
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
				r.Get("/issues", s.handleAPIIssueList)
				r.Get("/issues/{id}", s.handleAPIIssueGet)
				r.Get("/issues/{id}/comments", s.handleAPIIssueComments)
			})

			// Write-protected API routes
			r.Group(func(r chi.Router) {
				r.Use(s.PermissionChecker.RequireWrite)
				r.Put("/pages/*", s.handleAPIPage)
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

	// Wiki page routes
	r.Route("/{path:.*}", func(r chi.Router) {
		// Read-protected page routes
		r.Group(func(r chi.Router) {
			r.Use(s.PermissionChecker.RequireRead)
			r.Get("/", s.handleView)
			r.Get("/history", s.handleHistory)
			r.Get("/source", s.handleSource)
			r.Get("/blame", s.handleBlame)
			r.Get("/diff", s.handleDiff)
			r.Get("/attachments", s.handleAttachments)
			r.Get("/draft", s.handleDraftLoad)
			// Catch-all for attachment files and nested page paths.
			// Chi static routes above take priority over this parameterized route.
			r.Get("/{subpath}", s.handleView)
		})

		// Write-protected page routes
		r.Group(func(r chi.Router) {
			r.Use(s.PermissionChecker.RequireWrite)
			r.Get("/edit", s.handleEdit)
			r.Post("/save", s.handleSave)
			r.Get("/create", s.handleCreate)
			r.Get("/delete", s.handleDeleteForm)
			r.Post("/delete", s.handleDelete)
			r.Get("/rename", s.handleRenameForm)
			r.Post("/rename", s.handleRename)
			r.Post("/preview", s.handlePreview)
			r.Post("/draft", s.handleDraftSave)
			r.Delete("/draft", s.handleDraftDelete)
		})

		// Upload-protected page routes
		r.Group(func(r chi.Router) {
			r.Use(s.PermissionChecker.RequireUpload)
			r.Post("/attachments", s.handleUploadAttachment)
		})
	})

	return r
}

// staticCacheHandler wraps a handler to add Cache-Control headers for static assets.
func staticCacheHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
