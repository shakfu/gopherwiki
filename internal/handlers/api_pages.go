package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// handleAPIPageList handles GET /api/v1/pages -- lists all pages.
func (s *Server) handleAPIPageList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Wiki.PageIndex(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list pages")
		return
	}

	entries = s.filterIndex(s.visibleOrHide(r), entries)
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
	if !util.ValidPagepath(pagePath) {
		writeJSONError(w, http.StatusBadRequest, "invalid page path")
		return
	}

	// Resolve the sub-resource first, so the hidden check below covers the
	// page the handler loads. A "/runs/" segment is a run only with an ID.
	target := pagePath
	serve := func(page string) {
		switch r.Method {
		case http.MethodGet:
			s.handleAPIPageGet(w, r, page)
		case http.MethodPut:
			s.handleAPIPageSave(w, r, page)
		case http.MethodDelete:
			s.handleAPIPageDelete(w, r, page)
		default:
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
	switch {
	case strings.HasSuffix(pagePath, "/history"):
		target = strings.TrimSuffix(pagePath, "/history")
		serve = func(page string) { s.handleAPIPageHistory(w, r, page) }
	case strings.HasSuffix(pagePath, "/backlinks"):
		target = strings.TrimSuffix(pagePath, "/backlinks")
		serve = func(page string) { s.handleAPIPageBacklinks(w, r, page) }
	case strings.HasSuffix(pagePath, "/runs"):
		target = strings.TrimSuffix(pagePath, "/runs")
		serve = func(page string) { s.handleAPIPageRuns(w, r, page) }
	default:
		if i := strings.LastIndex(pagePath, "/runs/"); i > 0 {
			if id, err := strconv.ParseInt(pagePath[i+len("/runs/"):], 10, 64); err == nil {
				target = pagePath[:i]
				serve = func(page string) { s.handleAPIPageRun(w, r, page, id) }
			}
		}
	}

	if hidden, err := s.pageHidden(r, target, false); err != nil || hidden {
		writeJSONError(w, http.StatusNotFound, "page not found")
		return
	}
	serve(target)
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

	cited, err := s.citedRuns(r.Context(), page)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to resolve cited report runs")
		return
	}

	// ETag support. Cited runs are folded in: a new run of a cited report
	// changes the response without a new commit.
	if page.Metadata != nil && page.Metadata.RevisionFull != "" {
		etag := `"` + page.Metadata.RevisionFull + citedETagSuffix(cited) + `"`
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	result := pageToAPI(page)
	for _, c := range cited {
		result.Sources = append(result.Sources, APICitedRun(c))
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIPageSave handles PUT /api/v1/pages/{path} -- create or update page.
func (s *Server) handleAPIPageSave(w http.ResponseWriter, r *http.Request, pagePath string) {
	var input APISavePage
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if token := middleware.GetToken(r); token != nil {
		page, err := wiki.NewPage(s.Storage, s.Config, pagePath, "")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to load page")
			return
		}
		if !underPrefix(token.WritePrefix, page.Filename) {
			writeJSONError(w, http.StatusForbidden, "token may not write outside its prefix")
			return
		}
		// Without a base revision SavePage skips conflict detection, which
		// would let a token overwrite an edit it has never read.
		if page.Exists && input.Revision == "" {
			writeJSONError(w, http.StatusPreconditionRequired, "revision required to overwrite an existing page")
			return
		}
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

	if middleware.GetToken(r) != nil {
		if err := s.DB.Queries.MarkAgentPage(r.Context(), updated.Filename); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "page saved but failed to mark it for validation")
			return
		}
	}

	status := http.StatusOK
	if result.IsNew {
		status = http.StatusCreated
	}
	writeJSON(w, status, pageToAPI(updated))
}

// maxBatchPages bounds one batch save.
const maxBatchPages = 100

// APIBatchSave is the JSON request body for a batch save.
type APIBatchSave struct {
	Message string         `json:"message"`
	Pages   []APIBatchPage `json:"pages"`
}

// APIBatchPage is one page in a batch save.
type APIBatchPage struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

// handleAPIBatchSave handles POST /api/v1/batch -- save several pages in one
// commit. Every page is checked before anything is written; one failing page
// fails the batch.
func (s *Server) handleAPIBatchSave(w http.ResponseWriter, r *http.Request) {
	var input APIBatchSave
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(input.Pages) == 0 || len(input.Pages) > maxBatchPages {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("a batch holds 1 to %d pages", maxBatchPages))
		return
	}

	token := middleware.GetToken(r)
	edits := make([]wiki.PageEdit, 0, len(input.Pages))
	for _, p := range input.Pages {
		if p.Path == "" {
			writeJSONError(w, http.StatusBadRequest, "page path required")
			return
		}
		if !util.ValidPagepath(p.Path) {
			writeJSONError(w, http.StatusBadRequest, "invalid page path: "+p.Path)
			return
		}
		if hidden, err := s.pageHidden(r, p.Path, false); err != nil || hidden {
			writeJSONError(w, http.StatusNotFound, "page not found: "+p.Path)
			return
		}
		if token != nil {
			page, err := wiki.NewPage(s.Storage, s.Config, p.Path, "")
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to load page")
				return
			}
			if !underPrefix(token.WritePrefix, page.Filename) {
				writeJSONError(w, http.StatusForbidden, "token may not write outside its prefix: "+p.Path)
				return
			}
			if page.Exists && p.Revision == "" {
				writeJSONError(w, http.StatusPreconditionRequired, "revision required to overwrite an existing page: "+p.Path)
				return
			}
		}
		edits = append(edits, wiki.PageEdit{Pagepath: p.Path, Content: p.Content, BaseRevision: p.Revision})
	}

	changed, conflict, err := s.Wiki.SavePages(r.Context(), edits, input.Message, s.getAuthor(r))
	if errors.Is(err, wiki.ErrDuplicatePage) {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to save pages")
		return
	}
	if conflict != "" {
		writeJSONError(w, http.StatusConflict, "edit conflict: page was modified since your revision: "+conflict)
		return
	}

	pages := make([]APIPage, 0, len(edits))
	for _, e := range edits {
		page, err := wiki.NewPage(s.Storage, s.Config, e.Pagepath, "")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "pages saved but failed to reload")
			return
		}
		if token != nil {
			if err := s.DB.Queries.MarkAgentPage(r.Context(), page.Filename); err != nil {
				writeJSONError(w, http.StatusInternalServerError, "pages saved but failed to mark them for validation")
				return
			}
		}
		pages = append(pages, pageToAPI(page))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"changed": changed, "pages": pages})
}

// underPrefix reports whether a page file lies under a token's write prefix.
// The filename is cleaned first so "prefix/../other" does not match.
func underPrefix(prefix, filename string) bool {
	if prefix == "" {
		return false
	}
	pagepath := util.StripMarkdownExtension(path.Clean(filename))
	return pagepath == prefix || strings.HasPrefix(pagepath, prefix+"/")
}

// handleAPIPageDelete handles DELETE /api/v1/pages/{path} -- delete page.
func (s *Server) handleAPIPageDelete(w http.ResponseWriter, r *http.Request, pagePath string) {
	if middleware.GetToken(r) != nil {
		writeJSONError(w, http.StatusForbidden, "tokens may not delete pages")
		return
	}

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

	backlinks = s.filterPaths(s.visibleOrHide(r), backlinks)
	if backlinks == nil {
		backlinks = []string{}
	}
	writeJSON(w, http.StatusOK, backlinks)
}

// handleAPIPageRuns handles GET /api/v1/pages/{path}/runs -- a page's report
// runs, newest first, without their output.
func (s *Server) handleAPIPageRuns(w http.ResponseWriter, r *http.Request, pagePath string) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page, err := wiki.NewPage(s.Storage, s.Config, pagePath, "")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load page")
		return
	}
	runs, err := s.DB.Queries.ListReportRuns(r.Context(), page.Filename)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}
	result := make([]APIReportRun, 0, len(runs))
	for _, run := range runs {
		result = append(result, reportRunToAPI(run))
	}
	writeJSON(w, http.StatusOK, result)
}

// handleAPIPageRun handles GET /api/v1/pages/{path}/runs/{id} -- one run with
// its executed markdown.
func (s *Server) handleAPIPageRun(w http.ResponseWriter, r *http.Request, pagePath string, id int64) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	page, err := wiki.NewPage(s.Storage, s.Config, pagePath, "")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load page")
		return
	}
	run, err := s.DB.Queries.GetReportRun(r.Context(), page.Filename, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to load run")
		return
	}
	result := reportRunToAPI(run)
	result.Markdown = run.Markdown
	writeJSON(w, http.StatusOK, result)
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

	results = s.filterSearch(s.visibleOrHide(r), results)
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
	changelog = s.filterCommits(s.visibleOrHide(r), changelog)

	writeJSON(w, http.StatusOK, commitsToAPI(changelog))
}
