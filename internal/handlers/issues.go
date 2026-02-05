package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"html/template"
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
		http.Error(w, "Failed to list issues", http.StatusInternalServerError)
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
	openCount, _ := s.DB.Queries.CountIssuesByStatus(ctx, "open")
	closedCount, _ := s.DB.Queries.CountIssuesByStatus(ctx, "closed")

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

	data := map[string]interface{}{
		"templateType":        "generic",
		"title":               "Issues",
		"issues":              flatIssues,
		"groupedIssues":       groupedIssues,
		"statusFilter":        statusFilter,
		"tagFilter":           tagFilter,
		"categoryFilter":      categoryFilter,
		"openCount":           openCount,
		"closedCount":         closedCount,
		"availableTags":       s.getAvailableTags(ctx),
		"availableCategories": s.getAvailableCategories(ctx),
	}

	s.renderTemplate(w, r, "issues_list.html", data)
}

// handleIssueView handles viewing a single issue.
func (s *Server) handleIssueView(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Issue not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get issue", http.StatusInternalServerError)
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

	data := map[string]interface{}{
		"templateType":        "generic",
		"title":               fmt.Sprintf("#%d - %s", issue.ID, issue.Title),
		"issue":               issue,
		"htmlcontent":         template.HTML(htmlContent),
		"tags":                tags,
		"availableTags":       s.getAvailableTags(ctx),
		"availableCategories": s.getAvailableCategories(ctx),
		"canEdit":             canEdit,
		"canDelete":           canDelete,
	}

	s.renderTemplate(w, r, "issues_view.html", data)
}

// handleIssueNew handles showing the new issue form.
func (s *Server) handleIssueNew(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := map[string]interface{}{
		"templateType":        "generic",
		"title":               "New Issue",
		"availableTags":       s.getAvailableTags(ctx),
		"availableCategories": s.getAvailableCategories(ctx),
		"isEdit":              false,
	}

	s.renderTemplate(w, r, "issues_form.html", data)
}

// handleIssueCreate handles creating a new issue.
func (s *Server) handleIssueCreate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		Description:    toSqlNullString(description),
		Status:         "open",
		Category:       toSqlNullString(category),
		Tags:           toSqlNullString(strings.Join(tags, ",")),
		CreatedByName:  toSqlNullString(createdByName),
		CreatedByEmail: toSqlNullString(createdByEmail),
		CreatedAt:      toSqlNullTime(now),
		UpdatedAt:      toSqlNullTime(now),
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
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Issue not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get issue", http.StatusInternalServerError)
		return
	}

	tags := parseTags(issue.Tags.String)

	data := map[string]interface{}{
		"templateType":        "generic",
		"title":               fmt.Sprintf("Edit Issue #%d", issue.ID),
		"issue":               issue,
		"tags":                tags,
		"availableTags":       s.getAvailableTags(ctx),
		"availableCategories": s.getAvailableCategories(ctx),
		"isEdit":              true,
	}

	s.renderTemplate(w, r, "issues_form.html", data)
}

// handleIssueUpdate handles updating an issue.
func (s *Server) handleIssueUpdate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get existing issue
	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Issue not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get issue", http.StatusInternalServerError)
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
		Description: toSqlNullString(description),
		Status:      issue.Status,
		Category:    toSqlNullString(category),
		Tags:        toSqlNullString(strings.Join(tags, ",")),
		UpdatedAt:   toSqlNullTime(time.Now()),
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
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
		return
	}

	issue, err := s.DB.Queries.GetIssue(ctx, id)
	if err != nil {
		if err == sql.ErrNoRows {
			http.Error(w, "Issue not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to get issue", http.StatusInternalServerError)
		return
	}

	params := db.UpdateIssueParams{
		Title:       issue.Title,
		Description: issue.Description,
		Status:      status,
		Category:    issue.Category,
		Tags:        issue.Tags,
		UpdatedAt:   toSqlNullTime(time.Now()),
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
		http.Error(w, "Invalid issue ID", http.StatusBadRequest)
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
