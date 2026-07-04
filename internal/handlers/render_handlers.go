package handlers

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/quarto"
	"github.com/sa/gopherwiki/internal/wiki"
)

// ojsCDNs are the content-delivery origins the Observable JS runtime loads from
// at view time. OJS is not fully self-contained: its standard library (Inputs,
// md/marked, etc.), FileAttachment resolution, and imported notebooks are fetched
// from these origins on demand, so they must be allowed for OJS cells to work.
// This is acceptable under the trusted-editing-team model (readers' browsers
// fetch from these well-known Observable/jsDelivr origins); it also means OJS
// pages, unlike frozen Python/R output, require network access when viewed.
const ojsCDNs = "https://cdn.jsdelivr.net https://cdn.observableusercontent.com https://api.observablehq.com"

// buildRenderedCSP is the CSP applied to the raw rendered-output response. Quarto's
// self-contained HTML inlines its scripts, styles, and resources (as data: URIs)
// and some libraries use eval, so this document needs a far more permissive
// policy than the rest of the app. cdnAllow optionally widens it to the
// Observable CDNs so OJS cells can load their libraries; in local-libs mode it is
// empty and the libraries load from the wiki's own origin. Scoped to the iframe
// document only; the surrounding wiki page keeps the strict policy.
func buildRenderedCSP(cdnAllow string) string {
	return "default-src 'self' data: blob:; " +
		"script-src 'self' 'unsafe-inline' 'unsafe-eval' data: blob:" + cdnAllow + "; " +
		"style-src 'self' 'unsafe-inline' data:" + cdnAllow + "; " +
		"img-src 'self' data: blob:" + cdnAllow + "; font-src 'self' data:" + cdnAllow + "; " +
		"connect-src 'self' data: blob:" + cdnAllow + "; frame-ancestors 'self'"
}

var (
	// renderedCSPWithCDN allows the Observable CDNs (default OJS mode).
	renderedCSPWithCDN = buildRenderedCSP(" " + ojsCDNs)
	// renderedCSPLocal keeps everything same-origin (OJS local-libs mode).
	renderedCSPLocal = buildRenderedCSP("")
)

// renderedContentSecurityPolicy returns the CSP for rendered output, allowing the
// Observable CDNs only when OJS libraries are not mirrored locally.
func (s *Server) renderedContentSecurityPolicy() string {
	if s.Config.OJSLibsDir != "" {
		return renderedCSPLocal
	}
	return renderedCSPWithCDN
}

// handleRender is the authenticated, gated render action for a computational
// (.qmd) page. It executes the page's code via Quarto and stores the resulting
// self-contained HTML in the render cache. Execution happens only here, never on
// a reader's page view. The route is write-protected; the endpoint additionally
// requires that computational rendering is enabled and available.
func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	if s.RenderService == nil || !s.RenderService.Available() {
		s.renderError(w, r, http.StatusNotImplemented, "Computational page rendering is not enabled on this instance")
		return
	}

	path := chi.URLParam(r, "path")
	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}
	if !page.IsComputational {
		s.renderError(w, r, http.StatusBadRequest, "This page is not a computational page")
		return
	}

	in := quarto.Input{
		Pagepath:       page.Pagepath,
		Source:         page.Content,
		Engine:         pageEngine(page),
		SourceRevision: pageRevision(page),
	}
	if _, err := s.RenderService.Render(r.Context(), in); err != nil {
		slog.Error("computational render failed", "page", page.Pagepath, "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "Render failed: "+err.Error())
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Page rendered successfully")
	http.Redirect(w, r, "/"+page.Pagepath, http.StatusFound)
}

// handleRendered serves the cached, self-contained HTML for a computational
// page as a standalone document. It is embedded by the page view in an iframe so
// that Quarto's own styling stays isolated from the wiki chrome. A cache miss
// returns 404; the page view shows the render-pending placeholder in that case.
// This endpoint never triggers a render.
func (s *Server) handleRendered(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !page.Exists || !page.IsComputational {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if s.RenderService == nil || !s.RenderService.Available() {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	entry, ok, err := s.RenderService.Cached(r.Context(), page.Content, pageEngine(page))
	if err != nil || !ok {
		http.Error(w, "Not rendered yet", http.StatusNotFound)
		return
	}

	// The output is content-addressed by the cache key, so it can be cached
	// strongly and revalidated cheaply with an ETag.
	etag := `"` + entry.Key + `"`
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", s.renderedContentSecurityPolicy())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(entry.HTML)))
	w.Write(entry.HTML)
}

// computationalIframe returns the iframe markup that embeds a computational
// page's rendered output (served by handleRendered) inside the wiki chrome.
//
// The iframe is sandboxed to allow scripts (Quarto output is interactive, e.g.
// Observable JS, plotly, mermaid) and same-origin. allow-same-origin is required
// for the Observable runtime to execute -- its inline `<script type="module">`
// blocks do not run in an opaque-origin (sandbox-without-same-origin) frame, so
// OJS cells would otherwise render as inert source text. Because the frame is
// same-origin with the wiki, allow-same-origin + allow-scripts lets the rendered
// document's author-authored JavaScript reach the wiki origin (DOM, non-HttpOnly
// cookies, session-authenticated fetches). This is acceptable only under the
// trusted-editing-team model this feature already assumes (see
// docs/computational-pages.md Section 7, where OJS is explicitly "trusted author
// JavaScript"). The hardened alternative -- serving rendered output from a
// separate origin so allow-same-origin cannot reach the wiki -- is deferred.
func computationalIframe(pageViewURL string) string {
	src := html.EscapeString(pageViewURL + "/rendered")
	return `<iframe class="computational-render" src="` + src + `" ` +
		`title="Rendered computational output" loading="lazy" ` +
		`sandbox="allow-scripts allow-same-origin allow-popups allow-downloads" ` +
		`style="width:100%;min-height:80vh;border:0;"></iframe>`
}

// pageEngine returns the render engine declared in a page's frontmatter, or "".
func pageEngine(page *wiki.Page) string {
	if page.Frontmatter != nil {
		return page.Frontmatter.Engine
	}
	return ""
}

// pageRevision returns the git revision the page's current content came from.
func pageRevision(page *wiki.Page) string {
	if page.Metadata != nil {
		return page.Metadata.RevisionFull
	}
	return ""
}
