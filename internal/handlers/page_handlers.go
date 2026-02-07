package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

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
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
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
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
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

	// Set content type and cache headers
	contentType := util.GuessMimetype(filename)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// For images and PDFs, allow inline display; for others, suggest download
	if !strings.HasPrefix(contentType, "image/") && contentType != "application/pdf" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	}

	w.Write(content)
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
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
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
	files, err := page.Attachments(100, ".md")
	if err != nil {
		slog.Warn("failed to load attachments", "error", err)
	}
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

	data := NewEditorData(page, content, cursorLine, cursorCh, revision, fileData)
	s.renderTemplate(w, r, "editor.html", data)
}

// handleSave handles saving a wiki page.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	path := chi.URLParam(r, "path")
	content := r.FormValue("content")
	message := r.FormValue("commit")
	formRevision := r.FormValue("revision")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Optimistic locking: reject saves where the base revision no longer matches HEAD
	if formRevision != "" && page.Exists && page.Metadata != nil && page.Metadata.Revision != formRevision {
		// Another user saved between edit-load and save-submit
		currentRevision := page.Metadata.Revision
		data := NewEditorData(page, content, 0, 0, currentRevision, nil)
		data["conflict_message"] = "Edit conflict: this page was modified by another user since you started editing. Your changes are preserved below. Please review and save again."
		w.WriteHeader(http.StatusConflict)
		s.renderTemplate(w, r, "editor.html", data)
		return
	}

	author := s.getAuthor(r)

	if message == "" {
		if page.Exists {
			message = "Updated " + page.Pagename
		} else {
			message = "Created " + page.Pagename
		}
	}

	_, err = page.Save(content, message, author)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.Wiki.IndexPage(r.Context(), page.Pagepath, content); err != nil {
		slog.Warn("failed to index page", "path", page.Pagepath, "error", err)
	}

	http.Redirect(w, r, "/"+page.Pagepath, http.StatusFound)
}

// handleHistory handles viewing page history.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !page.Exists || page.Metadata == nil {
		s.renderNotFound(w, r, page)
		return
	}

	log, err := page.History(0)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
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

	data := NewPageViewData(page.Pagename+" - History", page)
	data["log"] = logData
	data["rev_a"] = revA
	data["rev_b"] = revB
	s.renderTemplate(w, r, "history.html", data)
}

// handleSource handles viewing page source.
func (s *Server) handleSource(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	raw := r.URL.Query().Get("raw") != ""

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
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

	data := NewPageViewData(page.Pagename+" - Source", page)
	data["source"] = page.Content
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
	data := NewGenericData("Create a new page")
	s.renderTemplate(w, r, "create.html", data)
}

// handleDeleteForm handles the delete confirmation form.
func (s *Server) handleDeleteForm(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	data := NewGenericData("Delete " + page.Pagename + "?")
	data["pagename"] = page.Pagename
	data["pagepath"] = page.Pagepath
	s.renderTemplate(w, r, "delete.html", data)
}

// handleDelete handles deleting a page.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	message := r.FormValue("message")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	author := s.getAuthor(r)

	if message == "" {
		message = page.Pagename + " deleted."
	}

	if err := page.Delete(message, author, true); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.Wiki.RemovePageFromIndex(r.Context(), page.Pagepath); err != nil {
		slog.Warn("failed to remove page from index", "path", page.Pagepath, "error", err)
	}

	http.Redirect(w, r, "/-/changelog", http.StatusFound)
}

// handleRenameForm handles the rename form.
func (s *Server) handleRenameForm(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	data := NewGenericData("Rename " + page.Pagename)
	data["pagename"] = page.PagenameFull
	data["pagepath"] = page.Pagepath
	data["new_pagename"] = page.Pagepath
	s.renderTemplate(w, r, "rename.html", data)
}

// handleRename handles renaming a page.
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	newPagename := r.FormValue("new_pagename")
	message := r.FormValue("message")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	author := s.getAuthor(r)

	if message == "" {
		message = "Renamed " + page.Pagename + " to " + newPagename
	}

	if err := page.Rename(newPagename, message, author); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.Wiki.RemovePageFromIndex(r.Context(), path); err != nil {
		slog.Warn("failed to remove old page from index", "path", path, "error", err)
	}
	if content, err := s.Storage.Load(util.GetFilename(newPagename), ""); err == nil {
		if err := s.Wiki.IndexPage(r.Context(), newPagename, content); err != nil {
			slog.Warn("failed to index renamed page", "path", newPagename, "error", err)
		}
	}

	http.Redirect(w, r, "/"+newPagename, http.StatusFound)
}

// handleAttachments handles viewing attachments.
func (s *Server) handleAttachments(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	files, err := page.Attachments(0, ".md")
	if err != nil {
		slog.Warn("failed to load attachments", "error", err)
	}
	fileData := make([]map[string]interface{}, 0, len(files))
	for _, f := range files {
		fileData = append(fileData, map[string]interface{}{
			"filename": f.Filename,
			"url":      "/" + f.Fullpath,
			"mimetype": f.Mimetype,
		})
	}

	data := NewPageViewData(page.Pagename+" - Attachments", page)
	data["files"] = fileData
	s.renderTemplate(w, r, "attachments.html", data)
}

// handleUploadAttachment handles file uploads.
func (s *Server) handleUploadAttachment(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")

	// Parse multipart form (max 32MB)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Failed to parse form: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Failed to get file: "+err.Error())
		return
	}
	defer file.Close()

	// Read file content
	content, err := io.ReadAll(file)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to read file: "+err.Error())
		return
	}

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	author := s.getAuthor(r)

	message := r.FormValue("message")
	if message == "" {
		message = "Added " + header.Filename
	}

	// Save attachment
	attachmentPath := page.AttachmentDirectoryname + "/" + header.Filename
	_, err = s.Storage.StoreBytes(attachmentPath, content, message, author)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to save file: "+err.Error())
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
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !page.Exists || page.Metadata == nil {
		s.renderNotFound(w, r, page)
		return
	}

	blame, err := page.Blame()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	data := NewPageViewData(page.Pagename+" - Blame", page)
	data["blame"] = blame
	s.renderTemplate(w, r, "blame.html", data)
}

// handleDiff handles viewing diffs between revisions.
func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "path")
	revA := r.URL.Query().Get("rev_a")
	revB := r.URL.Query().Get("rev_b")

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}

	diff, err := s.Wiki.Diff(revA, revB)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// Parse diff for display
	diffLines := parseDiff(diff)

	data := NewPageViewData(page.Pagename+" - Diff", page)
	data["diff"] = diff
	data["diff_lines"] = diffLines
	data["rev_a"] = revA
	data["rev_b"] = revB
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

	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		http.Error(w, "Invalid page path", http.StatusBadRequest)
		return
	}

	htmlContent, _, libRequirements := s.Renderer.Render(content, page.PageViewURL)

	type previewResponse struct {
		PreviewContent      string `json:"preview_content"`
		PreviewToc          string `json:"preview_toc"`
		LibraryRequirements struct {
			RequiresMermaid bool `json:"requires_mermaid"`
			RequiresMathJax bool `json:"requires_mathjax"`
		} `json:"library_requirements"`
	}

	resp := previewResponse{
		PreviewContent: htmlContent,
	}
	resp.LibraryRequirements.RequiresMermaid = libRequirements.RequiresMermaid
	resp.LibraryRequirements.RequiresMathJax = libRequirements.RequiresMathJax
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAbout handles the about page.
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	data := NewGenericData("About")
	data["version"] = s.Version
	s.renderTemplate(w, r, "about.html", data)
}

// handleSearch handles search.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("query")
	if query == "" {
		query = r.URL.Query().Get("q")
	}

	var results []wiki.SearchResult
	if query != "" {
		var err error
		results, err = s.Wiki.Search(query)
		if err != nil {
			slog.Warn("search failed", "query", query, "error", err)
		}
	}

	data := NewGenericData("Search")
	data["query"] = query
	data["results"] = results
	s.renderTemplate(w, r, "search.html", data)
}

// handleSearchPartial returns only the search results fragment for HTMX requests.
func (s *Server) handleSearchPartial(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")

	var results []wiki.SearchResult
	if query != "" {
		var err error
		results, err = s.Wiki.Search(query)
		if err != nil {
			slog.Warn("search failed", "query", query, "error", err)
		}
	}

	data := map[string]interface{}{
		"query":   query,
		"results": results,
	}

	tmpl, ok := s.TemplateMap["search.html"]
	if !ok {
		http.Error(w, "template not found", http.StatusInternalServerError)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "search_results", data); err != nil {
		slog.Error("template execution error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleChangelog handles the changelog.
func (s *Server) handleChangelog(w http.ResponseWriter, r *http.Request) {
	changelog, err := s.Wiki.Changelog(100)
	if err != nil {
		changelog = []storage.CommitMetadata{}
	}

	data := NewGenericData("Changelog")
	data["log"] = changelog
	s.renderTemplate(w, r, "changelog.html", data)
}

// handleCommit handles viewing a specific commit.
func (s *Server) handleCommit(w http.ResponseWriter, r *http.Request) {
	revision := chi.URLParam(r, "revision")

	meta, diff, err := s.Wiki.ShowCommit(revision)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, err.Error())
		return
	}

	// Parse diff for display
	diffLines := parseDiff(diff)

	data := NewGenericData("Commit " + meta.Revision)
	data["commit"] = meta
	data["diff"] = diff
	data["diff_lines"] = diffLines
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

	meta, _, err := s.Wiki.ShowCommit(revision)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, err.Error())
		return
	}

	data := NewGenericData("Revert Commit " + meta.Revision)
	data["commit"] = meta
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

	author := s.getAuthor(r)

	if err := s.Wiki.Revert(revision, message, author); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to revert commit: "+err.Error())
		http.Redirect(w, r, "/-/commit/"+revision, http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Commit reverted successfully")
	http.Redirect(w, r, "/-/changelog", http.StatusFound)
}

// handlePageIndex handles the page index.
func (s *Server) handlePageIndex(w http.ResponseWriter, r *http.Request) {
	entries, err := s.Wiki.PageIndex()
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	var pages []map[string]string
	for _, entry := range entries {
		pages = append(pages, map[string]string{
			"name": entry.Name,
			"path": entry.Path,
		})
	}

	data := NewGenericData("Page Index")
	data["pages"] = pages
	s.renderTemplate(w, r, "pageindex.html", data)
}

// handleHealthCheck handles the health check endpoint.
func (s *Server) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": s.Version,
	})
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
	page, err := wiki.NewPage(s.Storage, s.Config, path, "")
	if err != nil {
		slog.Warn("failed to create page for draft", "path", path, "error", err)
	}
	revision := ""
	if page.Metadata != nil {
		revision = page.Metadata.Revision
	}

	// Upsert draft
	params := db.UpsertDraftParams{
		Pagepath:    db.NullString(path),
		Revision:    db.NullString(revision),
		AuthorEmail: db.NullString(authorEmail),
		Content:     db.NullString(content),
		CursorLine:  db.NullInt64(cursorLine),
		CursorCh:    db.NullInt64(cursorCh),
		Datetime:    db.NullTime(time.Now()),
	}

	if err := s.DB.Queries.UpsertDraft(r.Context(), params); err != nil {
		slog.Error("failed to save draft", "path", path, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "Failed to save draft"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
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
		Pagepath:    db.NullString(path),
		AuthorEmail: db.NullString(authorEmail),
	}

	draft, err := s.DB.Queries.GetDraft(r.Context(), params)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"found": false})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"found":       true,
		"content":     draft.Content.String,
		"cursor_line": draft.CursorLine.Int64,
		"cursor_ch":   draft.CursorCh.Int64,
		"revision":    draft.Revision.String,
	})
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
		Pagepath:    db.NullString(path),
		AuthorEmail: db.NullString(authorEmail),
	}

	if err := s.DB.Queries.DeleteDraft(r.Context(), params); err != nil {
		slog.Warn("failed to delete draft", "path", path, "error", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}
