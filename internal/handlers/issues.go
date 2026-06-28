package handlers

import (
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/issues"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/util"
)

// issueAuthor builds an issues.Author from the request's user.
func (s *Server) issueAuthor(r *http.Request) issues.Author {
	user := middleware.GetUser(r)
	return issues.Author{Name: user.GetName(), Email: user.GetEmail()}
}

// renderIssueError maps a non-validation issue-service error to an HTML page.
func (s *Server) renderIssueError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, issues.ErrNotFound):
		s.renderError(w, r, http.StatusNotFound, "Issue not found")
	case errors.Is(err, issues.ErrCommentNotFound):
		s.renderError(w, r, http.StatusNotFound, "Comment not found")
	default:
		slog.Error("issue operation failed", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "Issue operation failed")
	}
}

// issuesByCategory groups issues by their category for display.
type issuesByCategory struct {
	Category string
	Issues   []map[string]interface{}
}

// handleIssueList handles listing all issues with optional filters.
func (s *Server) handleIssueList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	statusFilter := r.URL.Query().Get("status")
	tagFilter := r.URL.Query().Get("tag")
	categoryFilter := r.URL.Query().Get("category")

	issueList, err := s.Issues.List(ctx, issues.Filter{
		Status:   statusFilter,
		Category: categoryFilter,
		Tag:      tagFilter,
	})
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to list issues")
		return
	}

	openCount, closedCount, err := s.Issues.Counts(ctx)
	if err != nil {
		slog.Warn("failed to count issues", "error", err)
	}

	// Group issues by category for display.
	categoryMap := make(map[string][]map[string]interface{})
	var categoryOrder []string

	for _, issue := range issueList {
		category := ""
		if issue.Category.Valid {
			category = issue.Category.String
		}

		issueData := map[string]interface{}{
			"id":         issue.ID,
			"title":      issue.Title,
			"status":     issue.Status,
			"category":   category,
			"tags":       util.ParseTags(issue.Tags.String),
			"created_by": issue.CreatedByName.String,
			"created_at": issue.CreatedAt.Time,
			"updated_at": issue.UpdatedAt.Time,
		}

		if _, exists := categoryMap[category]; !exists {
			categoryOrder = append(categoryOrder, category)
		}
		categoryMap[category] = append(categoryMap[category], issueData)
	}

	var groupedIssues []issuesByCategory
	var flatIssues []map[string]interface{}
	for _, cat := range categoryOrder {
		groupedIssues = append(groupedIssues, issuesByCategory{Category: cat, Issues: categoryMap[cat]})
		flatIssues = append(flatIssues, categoryMap[cat]...)
	}

	data := NewGenericData("Issues")
	data["issues"] = flatIssues
	data["groupedIssues"] = groupedIssues
	data["statusFilter"] = statusFilter
	data["tagFilter"] = tagFilter
	data["categoryFilter"] = categoryFilter
	data["openCount"] = openCount
	data["closedCount"] = closedCount
	data["availableTags"] = s.Issues.AvailableTags(ctx)
	data["availableCategories"] = s.Issues.AvailableCategories(ctx)
	s.renderTemplate(w, r, "issues_list.html", data)
}

// handleIssueView handles viewing a single issue.
func (s *Server) handleIssueView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	issue, err := s.Issues.Get(ctx, id)
	if err != nil {
		s.renderIssueError(w, r, err)
		return
	}

	// Render the description as markdown.
	htmlContent := ""
	if issue.Description.Valid && issue.Description.String != "" {
		htmlContent, _, _ = s.Renderer.Render(issue.Description.String, "")
	}

	user := middleware.GetUser(r)

	comments, err := s.Issues.ListComments(ctx, issue.ID)
	if err != nil {
		slog.Warn("failed to list issue comments", "error", err)
	}

	type renderedComment struct {
		Comment     db.IssueComment
		HTMLContent template.HTML
	}
	var renderedComments []renderedComment
	for _, c := range comments {
		html := ""
		if c.Content != "" {
			html, _, _ = s.Renderer.Render(c.Content, "")
		}
		renderedComments = append(renderedComments, renderedComment{
			Comment:     c,
			HTMLContent: template.HTML(html),
		})
	}

	data := NewGenericData(fmt.Sprintf("#%d - %s", issue.ID, issue.Title))
	data["issue"] = issue
	data["htmlcontent"] = template.HTML(htmlContent)
	data["tags"] = util.ParseTags(issue.Tags.String)
	data["availableTags"] = s.Issues.AvailableTags(ctx)
	data["availableCategories"] = s.Issues.AvailableCategories(ctx)
	data["canEdit"] = user.IsAuthenticated()
	data["canDelete"] = user.Admin()
	data["comments"] = renderedComments
	data["comment_count"] = len(comments)
	s.renderTemplate(w, r, "issues_view.html", data)
}

// handleIssueNew handles showing the new issue form.
func (s *Server) handleIssueNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := NewGenericData("New Issue")
	data["availableTags"] = s.Issues.AvailableTags(ctx)
	data["availableCategories"] = s.Issues.AvailableCategories(ctx)
	data["isEdit"] = false
	s.renderTemplate(w, r, "issues_form.html", data)
}

// issueInputFromForm builds an issues.Input from the request form.
func issueInputFromForm(r *http.Request) issues.Input {
	return issues.Input{
		Title:       r.FormValue("title"),
		Description: r.FormValue("description"),
		Category:    r.FormValue("category"),
		Tags:        r.Form["tags"],
	}
}

// handleIssueCreate handles creating a new issue.
func (s *Server) handleIssueCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	issue, err := s.Issues.Create(r.Context(), issueInputFromForm(r), s.issueAuthor(r))
	if err != nil {
		var ve *issues.ValidationError
		if errors.As(err, &ve) {
			s.SessionManager.AddFlashMessage(w, r, "danger", ve.Message)
			http.Redirect(w, r, "/-/issues/new", http.StatusFound)
			return
		}
		s.renderIssueError(w, r, err)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue created successfully")
	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", issue.ID), http.StatusFound)
}

// handleIssueEdit handles showing the edit issue form.
func (s *Server) handleIssueEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	issue, err := s.Issues.Get(ctx, id)
	if err != nil {
		s.renderIssueError(w, r, err)
		return
	}

	data := NewGenericData(fmt.Sprintf("Edit Issue #%d", issue.ID))
	data["issue"] = issue
	data["tags"] = util.ParseTags(issue.Tags.String)
	data["availableTags"] = s.Issues.AvailableTags(ctx)
	data["availableCategories"] = s.Issues.AvailableCategories(ctx)
	data["isEdit"] = true
	s.renderTemplate(w, r, "issues_form.html", data)
}

// handleIssueUpdate handles updating an issue.
func (s *Server) handleIssueUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if _, err := s.Issues.Update(r.Context(), id, issueInputFromForm(r)); err != nil {
		var ve *issues.ValidationError
		if errors.As(err, &ve) {
			s.SessionManager.AddFlashMessage(w, r, "danger", ve.Message)
			http.Redirect(w, r, fmt.Sprintf("/-/issues/%d/edit", id), http.StatusFound)
			return
		}
		s.renderIssueError(w, r, err)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
}

// handleIssueClose handles closing an issue.
func (s *Server) handleIssueClose(w http.ResponseWriter, r *http.Request) {
	s.updateIssueStatus(w, r, "closed", "Issue closed")
}

// handleIssueReopen handles reopening an issue.
func (s *Server) handleIssueReopen(w http.ResponseWriter, r *http.Request) {
	s.updateIssueStatus(w, r, "open", "Issue reopened")
}

// updateIssueStatus updates the status of an issue and flashes a message.
func (s *Server) updateIssueStatus(w http.ResponseWriter, r *http.Request, status, successMsg string) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	if _, err := s.Issues.SetStatus(r.Context(), id, status); err != nil {
		if errors.Is(err, issues.ErrNotFound) {
			s.renderIssueError(w, r, err)
			return
		}
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update issue status")
		http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", successMsg)
	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
}

// handleIssueDelete handles deleting an issue (admin only).
func (s *Server) handleIssueDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	if err := s.Issues.Delete(r.Context(), id); err != nil {
		if errors.Is(err, issues.ErrNotFound) {
			s.renderIssueError(w, r, err)
			return
		}
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to delete issue")
		http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue deleted")
	http.Redirect(w, r, "/-/issues", http.StatusFound)
}

// handleIssueCommentCreate handles creating a new comment on an issue.
func (s *Server) handleIssueCommentCreate(w http.ResponseWriter, r *http.Request) {
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	comment, err := s.Issues.CreateComment(r.Context(), id, r.FormValue("content"), s.issueAuthor(r))
	if err != nil {
		var ve *issues.ValidationError
		if errors.As(err, &ve) {
			s.SessionManager.AddFlashMessage(w, r, "danger", ve.Message)
			http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
			return
		}
		s.renderIssueError(w, r, err)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d#comment-%d", id, comment.ID), http.StatusFound)
}

// handleIssueCommentDelete handles deleting a comment (admin only).
func (s *Server) handleIssueCommentDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	id, err := parseInt64(chi.URLParam(r, "id"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}
	commentID, err := parseInt64(chi.URLParam(r, "commentId"))
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid comment ID")
		return
	}

	if err := s.Issues.DeleteComment(r.Context(), commentID); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to delete comment")
	} else {
		s.SessionManager.AddFlashMessage(w, r, "success", "Comment deleted")
	}

	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
}
