package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// Agent pages are pages an API token has written. A reviewer validates one
// revision at a time; any later commit leaves the page unvalidated again. With
// HIDE_UNVALIDATED, an unvalidated agent page is hidden from everyone except
// reviewers and API tokens. See docs/dev/llm-wiki.md.

// validationState describes one revision of a page.
type validationState struct {
	Agent     bool   // an API token has written the page
	Validated bool   // Revision is validated
	Revision  string // the full revision the state refers to
	By        string
	At        time.Time
}

// validationState returns the state of the page's loaded revision.
func (s *Server) validationState(ctx context.Context, page *wiki.Page) (validationState, error) {
	var v validationState
	if !page.Exists || page.Metadata == nil {
		return v, nil
	}
	agent, err := s.DB.Queries.IsAgentPage(ctx, page.Filename)
	if err != nil || !agent {
		return v, err
	}
	v.Agent = true
	v.Revision = page.Metadata.RevisionFull
	v.By, v.At, err = s.DB.Queries.PageValidation(ctx, page.Filename, v.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	v.Validated = err == nil
	return v, err
}

// hideActive reports whether unvalidated agent pages are hidden from this
// request. Reviewers must see them to validate them; the agent must read its
// own pages to update them.
func (s *Server) hideActive(r *http.Request) bool {
	return s.Config.HideUnvalidated &&
		middleware.GetToken(r) == nil &&
		!s.PermissionChecker.HasPermission(r, middleware.PermissionReview)
}

// pageHidden reports whether the page at pagepath is hidden from this request.
// For a view, a path that is not a page may be an attachment, which is hidden
// with the page it belongs to.
func (s *Server) pageHidden(r *http.Request, pagepath string, view bool) (bool, error) {
	if !s.hideActive(r) {
		return false, nil
	}
	page, err := wiki.NewPage(s.Storage, s.Config, pagepath, "")
	if err != nil {
		return false, err
	}
	if !page.Exists && view {
		if i := strings.LastIndex(page.Pagepath, "/"); i > 0 {
			return s.pageHidden(r, page.Pagepath[:i], false)
		}
	}
	state, err := s.validationState(r.Context(), page)
	return state.Agent && !state.Validated, err
}

// visibility is the set of pages hidden from one request. The zero value
// hides nothing.
type visibility struct {
	keys  map[string]bool // pageKey of each hidden page
	files []string        // source filename of each hidden page
}

// pageKey normalizes a page path or filename for comparison with a hidden page.
func (s *Server) pageKey(p string) string {
	key := util.SanitizePagename(p, true)
	if !s.Config.RetainPageNameCase {
		key = strings.ToLower(key)
	}
	return key
}

// visibility computes the pages hidden from this request. It reads the
// current revision of every agent page, so it is computed only while hiding
// is active.
func (s *Server) visibility(r *http.Request) (visibility, error) {
	var v visibility
	if !s.hideActive(r) {
		return v, nil
	}
	files, err := s.DB.Queries.ListAgentPages(r.Context())
	if err != nil {
		return v, err
	}
	v.keys = map[string]bool{}
	for _, f := range files {
		meta, err := s.Storage.Metadata(f, "")
		if err != nil {
			continue // deleted or renamed
		}
		_, _, err = s.DB.Queries.PageValidation(r.Context(), f, meta.RevisionFull)
		if errors.Is(err, sql.ErrNoRows) {
			v.keys[s.pageKey(f)] = true
			v.files = append(v.files, f)
		} else if err != nil {
			return v, err
		}
	}
	return v, nil
}

// visibleOrHide returns the visibility for a listing. On error it logs and
// hides every agent page, so a failure never exposes one.
func (s *Server) visibleOrHide(r *http.Request) visibility {
	v, err := s.visibility(r)
	if err != nil {
		slog.Error("failed to compute page visibility", "error", err)
		files, _ := s.DB.Queries.ListAgentPages(r.Context())
		v = visibility{keys: map[string]bool{}, files: files}
		for _, f := range files {
			v.keys[s.pageKey(f)] = true
		}
	}
	return v
}

// hides reports whether the page at pagepath is hidden.
func (s *Server) hides(v visibility, pagepath string) bool {
	return v.keys[s.pageKey(pagepath)]
}

// filterPaths removes hidden pages from a list of page paths.
func (s *Server) filterPaths(v visibility, paths []string) []string {
	if len(v.keys) == 0 {
		return paths
	}
	var out []string
	for _, p := range paths {
		if !s.hides(v, p) {
			out = append(out, p)
		}
	}
	return out
}

// filterSearch removes hidden pages from search results.
func (s *Server) filterSearch(v visibility, results []wiki.SearchResult) []wiki.SearchResult {
	if len(v.keys) == 0 {
		return results
	}
	var out []wiki.SearchResult
	for _, res := range results {
		if !s.hides(v, res.Pagepath) {
			out = append(out, res)
		}
	}
	return out
}

// filterIndex removes hidden pages from a page index.
func (s *Server) filterIndex(v visibility, entries []wiki.PageIndexEntry) []wiki.PageIndexEntry {
	if len(v.keys) == 0 {
		return entries
	}
	var out []wiki.PageIndexEntry
	for _, e := range entries {
		if !s.hides(v, e.Path) {
			out = append(out, e)
		}
	}
	return out
}

// filterTree removes hidden pages from the sidebar tree. A hidden page with
// visible subpages stays as a plain folder. The input is the shared cached
// tree, so it is copied rather than modified.
func (s *Server) filterTree(v visibility, nodes []*wiki.PageTreeNode) []*wiki.PageTreeNode {
	if len(v.keys) == 0 {
		return nodes
	}
	var out []*wiki.PageTreeNode
	for _, n := range nodes {
		c := *n
		c.Children = s.filterTree(v, n.Children)
		if c.IsPage && s.hides(v, c.Path) {
			if len(c.Children) == 0 {
				continue
			}
			c.IsPage = false
		}
		out = append(out, &c)
	}
	return out
}

// filterCommits removes commits that touch a hidden page. Their messages and
// diffs would show the hidden content.
func (s *Server) filterCommits(v visibility, commits []storage.CommitMetadata) []storage.CommitMetadata {
	if len(v.files) == 0 {
		return commits
	}
	hidden := map[string]bool{}
	for _, f := range v.files {
		log, err := s.Storage.Log(f, 0)
		if err != nil {
			continue
		}
		for _, c := range log {
			hidden[c.RevisionFull] = true
		}
	}
	var out []storage.CommitMetadata
	for _, c := range commits {
		if !hidden[c.RevisionFull] {
			out = append(out, c)
		}
	}
	return out
}

// commitHidden reports whether a commit touches a hidden page.
func commitHidden(v visibility, meta *storage.CommitMetadata) bool {
	for _, f := range meta.Files {
		for _, h := range v.files {
			if f == h {
				return true
			}
		}
	}
	return false
}

// handleValidate records a reviewer's validation of an agent page's current
// revision. The form carries the revision the reviewer was shown; a mismatch
// means the page changed in between, and nothing is validated.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	page, err := wiki.NewPage(s.Storage, s.Config, chi.URLParam(r, "path"), "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}
	state, err := s.validationState(r.Context(), page)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to check validation")
		return
	}
	if !state.Agent {
		s.renderError(w, r, http.StatusBadRequest, "Only pages written by the agent need validation")
		return
	}
	if r.FormValue("revision") != state.Revision {
		s.renderError(w, r, http.StatusConflict, "The page changed after you opened it. Review the current revision and validate again.")
		return
	}

	by := middleware.GetUser(r).GetEmail()
	if err := s.DB.Queries.ValidatePage(r.Context(), page.Filename, state.Revision, by); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to record validation")
		return
	}
	slog.Info("page validated", "page", page.Pagepath, "revision", state.Revision, "by", by)

	s.SessionManager.AddFlashMessage(w, r, "success", "Revision validated")
	http.Redirect(w, r, "/"+page.Pagepath, http.StatusFound)
}
