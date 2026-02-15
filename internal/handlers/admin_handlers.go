package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
)

// requireAdmin is a helper that checks admin access and redirects if not authorized.
func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login?next="+r.URL.Path, http.StatusFound)
		return false
	}
	if !user.Admin() {
		s.renderError(w, r, http.StatusForbidden, "Access denied")
		return false
	}
	return true
}

// handleAdmin handles the admin dashboard.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	// Get stats
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		slog.Error("failed to list users", "error", err)
	}
	pages, err := s.Wiki.PageIndex(r.Context())
	if err != nil {
		slog.Error("failed to get page index", "error", err)
	}
	pageCount := len(pages)

	data := NewGenericData("Admin Dashboard")
	data["user_count"] = len(users)
	data["page_count"] = pageCount
	data["version"] = s.Version
	s.renderTemplate(w, r, "admin.html", data)
}

// handleAdminUsers handles the user list page.
func (s *Server) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Failed to list users")
		return
	}

	data := NewGenericData("User Management")
	data["users"] = users
	s.renderTemplate(w, r, "admin_users.html", data)
}

// handleAdminUserEdit handles the user edit form.
func (s *Server) handleAdminUserEdit(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid user ID")
		return
	}

	user, err := s.Auth.GetUserByID(r.Context(), id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "User not found")
		return
	}

	data := NewGenericData("Edit User: " + user.GetName())
	data["edit_user"] = user
	s.renderTemplate(w, r, "admin_user_edit.html", data)
}

// handleAdminUserSave handles saving user changes.
func (s *Server) handleAdminUserSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid user ID")
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// Get current user to update
	user, err := s.Auth.GetUserByID(r.Context(), id)
	if err != nil {
		s.renderError(w, r, http.StatusNotFound, "User not found")
		return
	}

	// Update user fields
	name := r.FormValue("name")
	isApproved := r.FormValue("is_approved") == "on"
	isAdmin := r.FormValue("is_admin") == "on"
	allowRead := r.FormValue("allow_read") == "on"
	allowWrite := r.FormValue("allow_write") == "on"
	allowUpload := r.FormValue("allow_upload") == "on"

	params := db.UpdateUserParams{
		ID:             id,
		Name:           name,
		Email:          user.Email,
		PasswordHash:   user.PasswordHash,
		IsApproved:     db.NullBool(isApproved),
		IsAdmin:        db.NullBool(isAdmin),
		EmailConfirmed: user.EmailConfirmed,
		AllowRead:      db.NullBool(allowRead),
		AllowWrite:     db.NullBool(allowWrite),
		AllowUpload:    db.NullBool(allowUpload),
	}

	if err := s.DB.Queries.UpdateUser(r.Context(), params); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update user")
		http.Redirect(w, r, fmt.Sprintf("/-/admin/users/%d", id), http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "User updated successfully")
	http.Redirect(w, r, "/-/admin/users", http.StatusFound)
}

// handleAdminUserDelete handles deleting a user.
func (s *Server) handleAdminUserDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	idStr := chi.URLParam(r, "id")
	id, err := parseInt64(idStr)
	if err != nil {
		s.renderError(w, r, http.StatusBadRequest, "Invalid user ID")
		return
	}

	// Don't allow deleting yourself
	currentUser := middleware.GetUser(r)
	if currentUser.ID == id {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Cannot delete your own account")
		http.Redirect(w, r, "/-/admin/users", http.StatusFound)
		return
	}

	if err := s.Auth.DeleteUser(r.Context(), id); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to delete user")
	} else {
		s.SessionManager.AddFlashMessage(w, r, "success", "User deleted successfully")
	}

	http.Redirect(w, r, "/-/admin/users", http.StatusFound)
}

// handleAdminSettings handles the admin settings page.
func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	ctx := r.Context()

	// Get current site settings from preferences or config
	siteSettings := s.getSiteSettings(ctx)

	// Get current issue tags and categories from preferences or config
	issueTags := s.getAvailableTags(ctx)
	issueCategories := s.getAvailableCategories(ctx)

	data := NewGenericData("Site Settings")
	data["site_settings"] = s.Config
	data["current_site"] = siteSettings
	data["issue_tags"] = strings.Join(issueTags, ", ")
	data["issue_categories"] = strings.Join(issueCategories, ", ")
	s.renderTemplate(w, r, "admin_settings.html", data)
}

// handleAdminSettingsSave handles saving admin settings.
func (s *Server) handleAdminSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	// Note: Runtime config changes are limited. Most settings require restart.
	// This is a placeholder for future implementation.
	s.SessionManager.AddFlashMessage(w, r, "info", "Settings changes require a restart to take effect")
	http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
}

// handleAdminSiteSettingsSave handles saving site name and logo.
func (s *Server) handleAdminSiteSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	siteName := strings.TrimSpace(r.FormValue("site_name"))
	siteLogo := strings.TrimSpace(r.FormValue("site_logo"))

	ctx := r.Context()

	// Save site name to preferences
	if siteName != "" {
		params := db.UpsertPreferenceParams{
			Name:  "site_name",
			Value: db.NullString(siteName),
		}
		if err := s.DB.Queries.UpsertPreference(ctx, params); err != nil {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save site name")
			http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
			return
		}
	}

	// Save site logo to preferences (can be empty to use default)
	params := db.UpsertPreferenceParams{
		Name:  "site_logo",
		Value: db.NullString(siteLogo),
	}
	if err := s.DB.Queries.UpsertPreference(ctx, params); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save site logo")
		http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
		return
	}

	s.InvalidateSiteSettingsCache()
	s.SessionManager.AddFlashMessage(w, r, "success", "Site settings updated successfully")
	http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
}

// handleAdminIssueSettingsSave handles saving issue tracker configuration (categories and tags).
func (s *Server) handleAdminIssueSettingsSave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	categoriesInput := r.FormValue("issue_categories")
	tagsInput := r.FormValue("issue_tags")

	// Parse and clean the categories
	var cleanCategories []string
	for _, cat := range strings.Split(categoriesInput, ",") {
		cat = strings.TrimSpace(cat)
		if cat != "" {
			cleanCategories = append(cleanCategories, cat)
		}
	}

	// Parse and clean the tags
	var cleanTags []string
	for _, tag := range strings.Split(tagsInput, ",") {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}

	// Save categories to preferences
	catParams := db.UpsertPreferenceParams{
		Name:  "issue_categories",
		Value: db.NullString(strings.Join(cleanCategories, ",")),
	}
	if err := s.DB.Queries.UpsertPreference(ctx, catParams); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save issue categories")
		http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
		return
	}

	// Save tags to preferences
	tagParams := db.UpsertPreferenceParams{
		Name:  "issue_tags",
		Value: db.NullString(strings.Join(cleanTags, ",")),
	}
	if err := s.DB.Queries.UpsertPreference(ctx, tagParams); err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to save issue tags")
		http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
		return
	}

	s.SessionManager.AddFlashMessage(w, r, "success", "Issue settings updated successfully")
	http.Redirect(w, r, "/-/admin/settings", http.StatusFound)
}
