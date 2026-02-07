package handlers

import (
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
)

// handleAPIIssueList handles GET /api/v1/issues -- list issues with optional filters.
func (s *Server) handleAPIIssueList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	statusFilter := r.URL.Query().Get("status")
	tagFilter := r.URL.Query().Get("tag")
	categoryFilter := r.URL.Query().Get("category")

	var issues []db.Issue
	var err error

	if statusFilter != "" && (statusFilter == "open" || statusFilter == "closed") {
		issues, err = s.DB.Queries.ListIssuesByStatus(ctx, statusFilter)
	} else {
		issues, err = s.DB.Queries.ListIssues(ctx)
	}

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	// Filter by category if specified
	if categoryFilter != "" {
		var filtered []db.Issue
		for _, issue := range issues {
			if issue.Category.Valid && issue.Category.String == categoryFilter {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	// Filter by tag if specified
	if tagFilter != "" {
		var filtered []db.Issue
		for _, issue := range issues {
			if issue.Tags.Valid && containsTag(issue.Tags.String, tagFilter) {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	writeJSON(w, http.StatusOK, issuesToAPI(issues))
}

// handleAPIIssueGet handles GET /api/v1/issues/{id} -- get a single issue.
func (s *Server) handleAPIIssueGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	writeJSON(w, http.StatusOK, issueToAPI(issue))
}

// handleAPIIssueCreate handles POST /api/v1/issues -- create a new issue.
func (s *Server) handleAPIIssueCreate(w http.ResponseWriter, r *http.Request) {
	var input APIIssueInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required")
		return
	}

	user := middleware.GetUser(r)
	createdByName := user.GetName()
	createdByEmail := user.GetEmail()
	if createdByName == "" {
		createdByName = "Anonymous"
	}

	now := time.Now()
	params := db.CreateIssueParams{
		Title:          title,
		Description:    db.NullString(input.Description),
		Status:         "open",
		Category:       db.NullString(input.Category),
		Tags:           db.NullString(strings.Join(input.Tags, ",")),
		CreatedByName:  db.NullString(createdByName),
		CreatedByEmail: db.NullString(createdByEmail),
		CreatedAt:      db.NullTime(now),
		UpdatedAt:      db.NullTime(now),
	}

	issue, err := s.DB.Queries.CreateIssue(r.Context(), params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create issue")
		return
	}

	writeJSON(w, http.StatusCreated, issueToAPI(issue))
}

// handleAPIIssueUpdate handles PUT /api/v1/issues/{id} -- update an issue.
func (s *Server) handleAPIIssueUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	var input APIIssueInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		writeJSONError(w, http.StatusBadRequest, "title is required")
		return
	}

	params := db.UpdateIssueParams{
		Title:       title,
		Description: db.NullString(input.Description),
		Status:      issue.Status,
		Category:    db.NullString(input.Category),
		Tags:        db.NullString(strings.Join(input.Tags, ",")),
		UpdatedAt:   db.NullTime(time.Now()),
		ID:          id,
	}

	if err := s.DB.Queries.UpdateIssue(ctx, params); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update issue")
		return
	}

	updated, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "issue updated but failed to reload")
		return
	}

	writeJSON(w, http.StatusOK, issueToAPI(updated))
}

// handleAPIIssueClose handles POST /api/v1/issues/{id}/close.
func (s *Server) handleAPIIssueClose(w http.ResponseWriter, r *http.Request) {
	s.apiUpdateIssueStatus(w, r, "closed")
}

// handleAPIIssueReopen handles POST /api/v1/issues/{id}/reopen.
func (s *Server) handleAPIIssueReopen(w http.ResponseWriter, r *http.Request) {
	s.apiUpdateIssueStatus(w, r, "open")
}

// apiUpdateIssueStatus is a helper for close/reopen API endpoints.
func (s *Server) apiUpdateIssueStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	params := db.UpdateIssueParams{
		Title:       issue.Title,
		Description: issue.Description,
		Status:      status,
		Category:    issue.Category,
		Tags:        issue.Tags,
		UpdatedAt:   db.NullTime(time.Now()),
		ID:          id,
	}

	if err := s.DB.Queries.UpdateIssue(ctx, params); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to update issue status")
		return
	}

	updated, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "status updated but failed to reload")
		return
	}

	writeJSON(w, http.StatusOK, issueToAPI(updated))
}

// handleAPIIssueComments handles GET /api/v1/issues/{id}/comments -- list comments for an issue.
func (s *Server) handleAPIIssueComments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	// Verify issue exists
	if _, err := s.DB.Queries.GetIssue(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	comments, err := s.DB.ListIssueComments(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}

	writeJSON(w, http.StatusOK, issueCommentsToAPI(comments))
}

// handleAPIIssueCommentCreate handles POST /api/v1/issues/{id}/comments -- create a comment.
func (s *Server) handleAPIIssueCommentCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	var input APIIssueCommentInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		writeJSONError(w, http.StatusBadRequest, "content is required")
		return
	}

	// Verify issue exists
	if _, err := s.DB.Queries.GetIssue(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	user := middleware.GetUser(r)
	authorName := user.GetName()
	authorEmail := user.GetEmail()
	if authorName == "" {
		authorName = "Anonymous"
	}

	comment, err := s.DB.CreateIssueComment(ctx, id, content, authorName, authorEmail)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to create comment")
		return
	}

	writeJSON(w, http.StatusCreated, issueCommentToAPI(comment))
}

// handleAPIIssueCommentDelete handles DELETE /api/v1/issues/{id}/comments/{commentId} -- admin only.
func (s *Server) handleAPIIssueCommentDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	commentIdStr := chi.URLParam(r, "commentId")
	commentId, err := parseInt64(commentIdStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid comment ID")
		return
	}

	// Verify comment exists
	if _, err := s.DB.GetIssueComment(ctx, commentId); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "comment not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get comment")
		return
	}

	if err := s.DB.DeleteIssueComment(ctx, commentId); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete comment")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleAPIIssueDelete handles DELETE /api/v1/issues/{id} -- delete an issue (admin only).
func (s *Server) handleAPIIssueDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	// Verify issue exists first
	if _, err := s.DB.Queries.GetIssue(ctx, id); err != nil {
		if err == sql.ErrNoRows {
			writeJSONError(w, http.StatusNotFound, "issue not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to get issue")
		return
	}

	if err := s.DB.Queries.DeleteIssue(ctx, id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to delete issue")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
