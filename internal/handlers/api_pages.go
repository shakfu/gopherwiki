package handlers

import (
	"net/http"
	"strings"

	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/wiki"
)

// handleAPIPageList handles GET /api/v1/pages -- lists all pages.
func (s *Server) handleAPIPageList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Wiki.PageIndex(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list pages")
		return
	}

	result := make([]APIPageIndex, 0, len(entries))
	for _, e := range entries {
		result = append(result, pageIndexToAPI(e))
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIPage is the wildcard handler for /api/v1/pages/*.
// It dispatches to sub-resources (history, backlinks) based on suffix,
// or handles the page itself.
func (s *Server) handleAPIPage(w http.ResponseWriter, r *http.Request) {
	// Extract the path after /api/v1/pages/
	fullPath := r.URL.Path
	prefix := "/-/api/v1/pages/"
	if !strings.HasPrefix(fullPath, prefix) {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	pagePath := strings.TrimPrefix(fullPath, prefix)
	if pagePath == "" {
		writeJSONError(w, http.StatusBadRequest, "page path required")
		return
	}

	// Dispatch sub-resources by suffix
	switch {
	case strings.HasSuffix(pagePath, "/history"):
		pagePath = strings.TrimSuffix(pagePath, "/history")
		s.handleAPIPageHistory(w, r, pagePath)
		return
	case strings.HasSuffix(pagePath, "/backlinks"):
		pagePath = strings.TrimSuffix(pagePath, "/backlinks")
		s.handleAPIPageBacklinks(w, r, pagePath)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleAPIPageGet(w, r, pagePath)
	case http.MethodPut:
		s.handleAPIPageSave(w, r, pagePath)
	case http.MethodDelete:
		s.handleAPIPageDelete(w, r, pagePath)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// handleAPIPageGet handles GET /api/v1/pages/{path} -- get page content.
func (s *Server) handleAPIPageGet(w http.ResponseWriter, r *http.Request, pagePath string) {
	revision := r.URL.Query().Get("revision")

	page, err := wiki.NewPage(s.Storage, s.Config, pagePath, revision)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load page")
		return
	}

	if !page.Exists {
		writeJSONError(w, http.StatusNotFound, "page not found")
		return
	}

	// ETag support
	if page.Metadata != nil && page.Metadata.RevisionFull != "" {
		etag := `"` + page.Metadata.RevisionFull + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	writeJSON(w, http.StatusOK, pageToAPI(page))
}

// handleAPIPageSave handles PUT /api/v1/pages/{path} -- create or update page.
func (s *Server) handleAPIPageSave(w http.ResponseWriter, r *http.Request, pagePath string) {
	var input APISavePage
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	author := s.getAuthor(r)

	result, err := s.Wiki.SavePage(r.Context(), pagePath, input.Content, input.Message, input.Revision, author)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save page")
		return
	}

	if result.Conflict {
		writeJSONError(w, http.StatusConflict, "edit conflict: page was modified since your revision")
		return
	}

	// Reload page to get updated metadata
	updated, err := wiki.NewPage(s.Storage, s.Config, pagePath, "")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "page saved but failed to reload")
		return
	}

	status := http.StatusOK
	if result.IsNew {
		status = http.StatusCreated
	}
	writeJSON(w, status, pageToAPI(updated))
}

// handleAPIPageDelete handles DELETE /api/v1/pages/{path} -- delete page.
func (s *Server) handleAPIPageDelete(w http.ResponseWriter, r *http.Request, pagePath string) {
	author := s.getAuthor(r)

	if err := s.Wiki.DeletePage(r.Context(), pagePath, "", author); err != nil {
		if err == storage.ErrNotFound {
			writeJSONError(w, http.StatusNotFound, "page not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to delete page")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleAPIPageHistory handles GET /api/v1/pages/{path}/history.
func (s *Server) handleAPIPageHistory(w http.ResponseWriter, r *http.Request, pagePath string) {
	page, err := wiki.NewPage(s.Storage, s.Config, pagePath, "")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load page")
		return
	}

	if !page.Exists {
		writeJSONError(w, http.StatusNotFound, "page not found")
		return
	}

	log, err := page.History(0)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to get history")
		return
	}

	writeJSON(w, http.StatusOK, commitsToAPI(log))
}

// handleAPIPageBacklinks handles GET /api/v1/pages/{path}/backlinks.
func (s *Server) handleAPIPageBacklinks(w http.ResponseWriter, r *http.Request, pagePath string) {
	backlinks, err := s.Wiki.Backlinks(r.Context(), pagePath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to get backlinks")
		return
	}

	if backlinks == nil {
		backlinks = []string{}
	}
	writeJSON(w, http.StatusOK, backlinks)
}

// handleAPISearch handles GET /api/v1/search?q=...
func (s *Server) handleAPISearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, []APISearchResult{})
		return
	}

	results, err := s.Wiki.Search(r.Context(), query)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "search failed")
		return
	}

	apiResults := make([]APISearchResult, 0, len(results))
	for _, r := range results {
		apiResults = append(apiResults, searchResultToAPI(r))
	}
	writeJSON(w, http.StatusOK, apiResults)
}

// handleAPIChangelog handles GET /api/v1/changelog.
func (s *Server) handleAPIChangelog(w http.ResponseWriter, r *http.Request) {
	changelog, err := s.Wiki.Changelog(r.Context(), 100)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to get changelog")
		return
	}

	writeJSON(w, http.StatusOK, commitsToAPI(changelog))
}
