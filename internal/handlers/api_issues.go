package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/issues"
)

// writeIssueServiceError maps an issue-service error to a JSON response.
func writeIssueServiceError(w http.ResponseWriter, err error) {
	var ve *issues.ValidationError
	switch {
	case errors.As(err, &ve):
		writeJSONError(w, http.StatusBadRequest, ve.Message)
	case errors.Is(err, issues.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "issue not found")
	case errors.Is(err, issues.ErrCommentNotFound):
		writeJSONError(w, http.StatusNotFound, "comment not found")
	default:
		writeJSONError(w, http.StatusInternalServerError, "issue operation failed")
	}
}

// handleAPIIssueList handles GET /api/v1/issues -- list issues with optional filters.
func (s *Server) handleAPIIssueList(w http.ResponseWriter, r *http.Request) {
	list, err := s.Issues.List(r.Context(), issues.Filter{
		Status:   r.URL.Query().Get("status"),
		Category: r.URL.Query().Get("category"),
		Tag:      r.URL.Query().Get("tag"),
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to list issues")
		return
	}

	limit, offset := paginationParams(r)
	page, meta := paginate(issuesToAPI(list), limit, offset)
	writeJSONPaginated(w, http.StatusOK, page, meta)
}

// handleAPIIssueGet handles GET /api/v1/issues/{id} -- get a single issue.
func (s *Server) handleAPIIssueGet(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	issue, err := s.Issues.Get(r.Context(), id)
	if err != nil {
		writeIssueServiceError(w, err)
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

	issue, err := s.Issues.Create(r.Context(), issues.Input{
		Title:       input.Title,
		Description: input.Description,
		Category:    input.Category,
		Tags:        input.Tags,
	}, s.issueAuthor(r))
	if err != nil {
		writeIssueServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueToAPI(issue))
}

// handleAPIIssueUpdate handles PUT /api/v1/issues/{id} -- update an issue.
func (s *Server) handleAPIIssueUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	var input APIIssueInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	updated, err := s.Issues.Update(r.Context(), id, issues.Input{
		Title:       input.Title,
		Description: input.Description,
		Category:    input.Category,
		Tags:        input.Tags,
	})
	if err != nil {
		writeIssueServiceError(w, err)
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
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	updated, err := s.Issues.SetStatus(r.Context(), id, status)
	if err != nil {
		writeIssueServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueToAPI(updated))
}

// handleAPIIssueDelete handles DELETE /api/v1/issues/{id} -- delete an issue (admin only).
func (s *Server) handleAPIIssueDelete(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	if err := s.Issues.Delete(r.Context(), id); err != nil {
		writeIssueServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// handleAPIIssueComments handles GET /api/v1/issues/{id}/comments -- list comments.
func (s *Server) handleAPIIssueComments(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	comments, err := s.Issues.ListComments(r.Context(), id)
	if err != nil {
		writeIssueServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, issueCommentsToAPI(comments))
}

// handleAPIIssueCommentCreate handles POST /api/v1/issues/{id}/comments.
func (s *Server) handleAPIIssueCommentCreate(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid issue ID")
		return
	}

	var input APIIssueCommentInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	comment, err := s.Issues.CreateComment(r.Context(), id, input.Content, s.issueAuthor(r))
	if err != nil {
		writeIssueServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, issueCommentToAPI(&comment))
}

// handleAPIIssueCommentDelete handles DELETE /api/v1/issues/{id}/comments/{commentId} -- admin only.
func (s *Server) handleAPIIssueCommentDelete(w http.ResponseWriter, r *http.Request) {
	commentID, err := parseInt64(chi.URLParam(r, "commentId"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid comment ID")
		return
	}

	if err := s.Issues.DeleteComment(r.Context(), commentID); err != nil {
		writeIssueServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}
