package handlers

import (
	"log/slog"
	"net/http"

	"github.com/sa/gopherwiki/internal/auth"
	"github.com/sa/gopherwiki/internal/middleware"
)

// handleLogin handles the login page.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to home
	user := middleware.GetUser(r)
	if user.IsAuthenticated() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	next := r.URL.Query().Get("next")
	if next == "" {
		next = "/"
	}

	data := NewGenericData("Login")
	data["next"] = next
	s.renderTemplate(w, r, "login.html", data)
}

// handleLoginPost handles login form submission.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	email := r.FormValue("email")
	password := r.FormValue("password")
	next := r.FormValue("next")
	if next == "" {
		next = "/"
	}

	user, err := s.Auth.Authenticate(r.Context(), email, password)
	if err != nil {
		s.SessionManager.AddFlashMessage(w, r, "danger", "Invalid email or password")
		data := NewGenericData("Login")
		data["email"] = email
		data["next"] = next
		data["error"] = err.Error()
		s.renderTemplate(w, r, "login.html", data)
		return
	}

	// Login successful
	if err := s.SessionManager.Login(w, r, user.ID); err != nil {
		slog.Error("session login error", "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "Session error")
		return
	}

	// Update last seen
	s.Auth.UpdateUserLastSeen(r.Context(), user.ID)

	http.Redirect(w, r, next, http.StatusFound)
}

// handleLogout handles logout.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.SessionManager.Logout(w, r); err != nil {
		slog.Warn("session logout error", "error", err)
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleRegister handles the registration page.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	// If registration is disabled, redirect to login
	if s.Config.DisableRegistration {
		http.Redirect(w, r, "/-/login", http.StatusFound)
		return
	}

	// If already logged in, redirect to home
	user := middleware.GetUser(r)
	if user.IsAuthenticated() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	data := NewGenericData("Register")
	s.renderTemplate(w, r, "register.html", data)
}

// handleRegisterPost handles registration form submission.
func (s *Server) handleRegisterPost(w http.ResponseWriter, r *http.Request) {
	// If registration is disabled, redirect to login
	if s.Config.DisableRegistration {
		http.Redirect(w, r, "/-/login", http.StatusFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	name := r.FormValue("name")
	email := r.FormValue("email")
	password := r.FormValue("password")
	password2 := r.FormValue("password2")

	// Validate passwords match
	if password != password2 {
		data := NewGenericData("Register")
		data["name"] = name
		data["email"] = email
		data["error"] = "Passwords do not match"
		s.renderTemplate(w, r, "register.html", data)
		return
	}

	// Validate password length
	if len(password) < 8 {
		data := NewGenericData("Register")
		data["name"] = name
		data["email"] = email
		data["error"] = "Password must be at least 8 characters"
		s.renderTemplate(w, r, "register.html", data)
		return
	}

	// Register user
	user, err := s.Auth.Register(r.Context(), name, email, password)
	if err != nil {
		errMsg := "Registration failed"
		if err == auth.ErrEmailExists {
			errMsg = "Email already registered"
		}
		data := NewGenericData("Register")
		data["name"] = name
		data["email"] = email
		data["error"] = errMsg
		s.renderTemplate(w, r, "register.html", data)
		return
	}

	// Auto-login if approved
	if user.Approved() {
		if err := s.SessionManager.Login(w, r, user.ID); err != nil {
			slog.Error("session login error after registration", "error", err)
		}
		s.SessionManager.AddFlashMessage(w, r, "success", "Registration successful! Welcome to the wiki.")
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// If not auto-approved, show message and redirect to login
	s.SessionManager.AddFlashMessage(w, r, "info", "Registration successful! Please wait for admin approval.")
	http.Redirect(w, r, "/-/login", http.StatusFound)
}

// handleSettings handles the settings page.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login?next=/-/settings", http.StatusFound)
		return
	}

	data := NewGenericData("Settings")
	data["user_name"] = user.GetName()
	data["user_email"] = user.GetEmail()
	s.renderTemplate(w, r, "settings.html", data)
}

// handleSettingsPost handles settings form submission.
func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)
	if !user.IsAuthenticated() {
		http.Redirect(w, r, "/-/login", http.StatusFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	action := r.FormValue("action")

	switch action {
	case "update_name":
		name := r.FormValue("name")
		if name != "" {
			if err := s.Auth.UpdateUserName(r.Context(), user.ID, name); err != nil {
				s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update name")
			} else {
				s.SessionManager.AddFlashMessage(w, r, "success", "Name updated successfully")
			}
		}

	case "change_password":
		currentPassword := r.FormValue("current_password")
		newPassword := r.FormValue("new_password")
		confirmPassword := r.FormValue("confirm_password")

		// Verify current password
		if !auth.CheckPassword(currentPassword, user.GetPasswordHash()) {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Current password is incorrect")
			http.Redirect(w, r, "/-/settings", http.StatusFound)
			return
		}

		// Check new passwords match
		if newPassword != confirmPassword {
			s.SessionManager.AddFlashMessage(w, r, "danger", "New passwords do not match")
			http.Redirect(w, r, "/-/settings", http.StatusFound)
			return
		}

		// Check password length
		if len(newPassword) < 8 {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Password must be at least 8 characters")
			http.Redirect(w, r, "/-/settings", http.StatusFound)
			return
		}

		// Update password
		if err := s.Auth.UpdatePassword(r.Context(), user.ID, newPassword); err != nil {
			s.SessionManager.AddFlashMessage(w, r, "danger", "Failed to update password")
		} else {
			s.SessionManager.AddFlashMessage(w, r, "success", "Password updated successfully")
		}
	}

	http.Redirect(w, r, "/-/settings", http.StatusFound)
}
