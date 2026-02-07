package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
)

const issueTagsPreferenceKey = "issue_tags"
const issueCategoriesPreferenceKey = "issue_categories"

// getAvailableTags returns the configured issue tags from preferences or config.
func (s *Server) getAvailableTags(ctx context.Context) []string {
	// First try to get from preferences (allows runtime customization)
	pref, err := s.DB.Queries.GetPreference(ctx, issueTagsPreferenceKey)
	if err == nil && pref.Value.Valid && pref.Value.String != "" {
		return parseTags(pref.Value.String)
	}

	// Fall back to config
	return parseTags(s.Config.IssueTags)
}

// getAvailableCategories returns the configured issue categories from preferences or config.
func (s *Server) getAvailableCategories(ctx context.Context) []string {
	// First try to get from preferences (allows runtime customization)
	pref, err := s.DB.Queries.GetPreference(ctx, issueCategoriesPreferenceKey)
	if err == nil && pref.Value.Valid && pref.Value.String != "" {
		return parseTags(pref.Value.String)
	}

	// Fall back to config
	return parseTags(s.Config.IssueCategories)
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

	var issues []db.Issue
	var err error

	if statusFilter != "" && (statusFilter == "open" || statusFilter == "closed") {
		issues, err = s.DB.Queries.ListIssuesByStatus(ctx, statusFilter)
	} else {
		issues, err = s.DB.Queries.ListIssues(ctx)
	}

	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to list issues")
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

	// Get counts
	openCount, err := s.DB.Queries.CountIssuesByStatus(ctx, "open")
	if err != nil {
		slog.Warn("failed to count open issues", "error", err)
	}
	closedCount, err := s.DB.Queries.CountIssuesByStatus(ctx, "closed")
	if err != nil {
		slog.Warn("failed to count closed issues", "error", err)
	}

	// Group issues by category
	categoryMap := make(map[string][]map[string]interface{})
	categoryOrder := []string{}

	for _, issue := range issues {
		tags := parseTags(issue.Tags.String)
		category := ""
		if issue.Category.Valid {
			category = issue.Category.String
		}

		issueData := map[string]interface{}{
			"id":         issue.ID,
			"title":      issue.Title,
			"status":     issue.Status,
			"category":   category,
			"tags":       tags,
			"created_by": issue.CreatedByName.String,
			"created_at": issue.CreatedAt.Time,
			"updated_at": issue.UpdatedAt.Time,
		}

		if _, exists := categoryMap[category]; !exists {
			categoryOrder = append(categoryOrder, category)
		}
		categoryMap[category] = append(categoryMap[category], issueData)
	}

	// Build grouped list
	var groupedIssues []issuesByCategory
	for _, cat := range categoryOrder {
		groupedIssues = append(groupedIssues, issuesByCategory{
			Category: cat,
			Issues:   categoryMap[cat],
		})
	}

	// Also prepare flat list for backward compatibility
	var flatIssues []map[string]interface{}
	for _, group := range groupedIssues {
		flatIssues = append(flatIssues, group.Issues...)
	}

	data := NewGenericData("Issues")
	data["issues"] = flatIssues
	data["groupedIssues"] = groupedIssues
	data["statusFilter"] = statusFilter
	data["tagFilter"] = tagFilter
	data["categoryFilter"] = categoryFilter
	data["openCount"] = openCount
	data["closedCount"] = closedCount
	data["availableTags"] = s.getAvailableTags(ctx)
	data["availableCategories"] = s.getAvailableCategories(ctx)
	s.renderTemplate(w, r, "issues_list.html", data)
}

// handleIssueView handles viewing a single issue.
func (s *Server) handleIssueView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			s.renderError(w, r, http.StatusNotFound, "Issue not found")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Failed to get issue")
		return
	}

	// Render the description as markdown
	htmlContent := ""
	if issue.Description.Valid && issue.Description.String != "" {
		htmlContent, _, _ = s.Renderer.Render(issue.Description.String, "")
	}

	tags := parseTags(issue.Tags.String)

	user := middleware.GetUser(r)
	canEdit := user.IsAuthenticated()
	canDelete := user.Admin()

	data := NewGenericData(fmt.Sprintf("#%d - %s", issue.ID, issue.Title))
	data["issue"] = issue
	data["htmlcontent"] = template.HTML(htmlContent)
	data["tags"] = tags
	data["availableTags"] = s.getAvailableTags(ctx)
	data["availableCategories"] = s.getAvailableCategories(ctx)
	data["canEdit"] = canEdit
	data["canDelete"] = canDelete
	s.renderTemplate(w, r, "issues_view.html", data)
}

// handleIssueNew handles showing the new issue form.
func (s *Server) handleIssueNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := NewGenericData("New Issue")
	data["availableTags"] = s.getAvailableTags(ctx)
	data["availableCategories"] = s.getAvailableCategories(ctx)
	data["isEdit"] = false
	s.renderTemplate(w, r, "issues_form.html", data)
}

// handleIssueCreate handles creating a new issue.
func (s *Server) handleIssueCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := r.FormValue("description")
	category := strings.TrimSpace(r.FormValue("category"))
	tags := r.Form["tags"]

	if title == "" {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Title is required")
		http.Redirect(w, r, "/-/issues/new", http.StatusFound)
		return
	}

	// Validate category if categories are configured
	availableCategories := s.getAvailableCategories(ctx)
	if len(availableCategories) > 0 && category == "" {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Category is required")
		http.Redirect(w, r, "/-/issues/new", http.StatusFound)
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
		Description:    db.NullString(description),
		Status:         "open",
		Category:       db.NullString(category),
		Tags:           db.NullString(strings.Join(tags, ",")),
		CreatedByName:  db.NullString(createdByName),
		CreatedByEmail: db.NullString(createdByEmail),
		CreatedAt:      db.NullTime(now),
		UpdatedAt:      db.NullTime(now),
	}

	issue, err := s.DB.Queries.CreateIssue(ctx, params)
	if err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to create issue")
		http.Redirect(w, r, "/-/issues/new", http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue created successfully")
	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", issue.ID), http.StatusFound)
}

// handleIssueEdit handles showing the edit issue form.
func (s *Server) handleIssueEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			s.renderError(w, r, http.StatusNotFound, "Issue not found")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Failed to get issue")
		return
	}

	tags := parseTags(issue.Tags.String)

	data := NewGenericData(fmt.Sprintf("Edit Issue #%d", issue.ID))
	data["issue"] = issue
	data["tags"] = tags
	data["availableTags"] = s.getAvailableTags(ctx)
	data["availableCategories"] = s.getAvailableCategories(ctx)
	data["isEdit"] = true
	s.renderTemplate(w, r, "issues_form.html", data)
}

// handleIssueUpdate handles updating an issue.
func (s *Server) handleIssueUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Get existing issue
	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			s.renderError(w, r, http.StatusNotFound, "Issue not found")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Failed to get issue")
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	description := r.FormValue("description")
	category := strings.TrimSpace(r.FormValue("category"))
	tags := r.Form["tags"]

	if title == "" {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Title is required")
		http.Redirect(w, r, fmt.Sprintf("/-/issues/%d/edit", id), http.StatusFound)
		return
	}

	params := db.UpdateIssueParams{
		Title:       title,
		Description: db.NullString(description),
		Status:      issue.Status,
		Category:    db.NullString(category),
		Tags:        db.NullString(strings.Join(tags, ",")),
		UpdatedAt:   db.NullTime(time.Now()),
		ID:          id,
	}

	if err := s.DB.Queries.UpdateIssue(ctx, params); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update issue")
		http.Redirect(w, r, fmt.Sprintf("/-/issues/%d/edit", id), http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue updated successfully")
	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
}

// handleIssueClose handles closing an issue.
func (s *Server) handleIssueClose(w http.ResponseWriter, r *http.Request) {
	s.updateIssueStatus(w, r, "closed")
}

// handleIssueReopen handles reopening an issue.
func (s *Server) handleIssueReopen(w http.ResponseWriter, r *http.Request) {
	s.updateIssueStatus(w, r, "open")
}

// updateIssueStatus updates the status of an issue.
func (s *Server) updateIssueStatus(w http.ResponseWriter, r *http.Request, status string) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			s.renderError(w, r, http.StatusNotFound, "Issue not found")
			return
		}
		s.renderError(w, r, http.StatusInternalServerError, "Failed to get issue")
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
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update issue status")
		http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
		return
	}

	if status == "closed" {
		s.SessionManager.AddFlashMessage(w, r, "success", "Issue closed")
	} else {
		s.SessionManager.AddFlashMessage(w, r, "success", "Issue reopened")
	}
	http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
}

// handleIssueDelete handles deleting an issue (admin only).
func (s *Server) handleIssueDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid issue ID")
		return
	}

	if err := s.DB.Queries.DeleteIssue(ctx, id); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to delete issue")
		http.Redirect(w, r, fmt.Sprintf("/-/issues/%d", id), http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue deleted")
	http.Redirect(w, r, "/-/issues", http.StatusFound)
}

// parseTags parses a comma-separated tag string into a slice.
func parseTags(tags string) []string {
	if tags == "" {
		return nil
	}
	parts := strings.Split(tags, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// containsTag checks if a comma-separated tag string contains a specific tag.
func containsTag(tags, tag string) bool {
	for _, t := range parseTags(tags) {
		if t == tag {
			return true
		}
	}
	return false
}

// GetIssueByID is a helper for other packages to get an issue by ID.
func (s *Server) GetIssueByID(ctx context.Context, id int64) (*db.Issue, error) {
	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	return &issue, nil
}
