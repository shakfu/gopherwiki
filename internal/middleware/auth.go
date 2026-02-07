package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/models"
)

// Permission levels
const (
	PermissionRead   = "read"
	PermissionWrite  = "write"
	PermissionUpload = "upload"
	PermissionAdmin  = "admin"
)

// PermissionChecker provides permission checking middleware.
type PermissionChecker struct {
	config         *config.Config
	sessionManager *SessionManager
}

// NewPermissionChecker creates a new PermissionChecker.
func NewPermissionChecker(cfg *config.Config, sm *SessionManager) *PermissionChecker {
	return &PermissionChecker{
		config:         cfg,
		sessionManager: sm,
	}
}

// RequireRead returns middleware that requires read permission.
func (pc *PermissionChecker) RequireRead(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pc.HasPermission(r, PermissionRead) {
			pc.handleUnauthorized(w, r, PermissionRead)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireWrite returns middleware that requires write permission.
func (pc *PermissionChecker) RequireWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pc.HasPermission(r, PermissionWrite) {
			pc.handleUnauthorized(w, r, PermissionWrite)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireUpload returns middleware that requires upload permission.
func (pc *PermissionChecker) RequireUpload(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pc.HasPermission(r, PermissionUpload) {
			pc.handleUnauthorized(w, r, PermissionUpload)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin returns middleware that requires admin permission.
func (pc *PermissionChecker) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !pc.HasPermission(r, PermissionAdmin) {
			pc.handleUnauthorized(w, r, PermissionAdmin)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth returns middleware that requires authentication.
func (pc *PermissionChecker) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user.IsAnonymous() {
			http.Redirect(w, r, "/-/login?next="+r.URL.Path, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// HasPermission checks if the current user has the specified permission.
func (pc *PermissionChecker) HasPermission(r *http.Request, permission string) bool {
	user := GetUser(r)

	switch permission {
	case PermissionRead:
		return pc.canRead(user)
	case PermissionWrite:
		return pc.canWrite(user)
	case PermissionUpload:
		return pc.canUpload(user)
	case PermissionAdmin:
		return pc.canAdmin(user)
	default:
		return false
	}
}

// canRead checks if the user can read.
func (pc *PermissionChecker) canRead(user *User) bool {
	// Check config access level
	switch pc.config.ReadAccess {
	case "ANONYMOUS":
		return true
	case "REGISTERED":
		if user.IsAnonymous() {
			return false
		}
		// Registered user - check if approved and has read permission
		return user.Approved() || user.CanRead() || user.Admin()
	case "APPROVED":
		if user.IsAnonymous() {
			return false
		}
		return user.Approved() || user.Admin()
	case "ADMIN":
		if user.IsAnonymous() {
			return false
		}
		return user.Admin()
	default:
		return true // Default to anonymous access
	}
}

// canWrite checks if the user can write.
func (pc *PermissionChecker) canWrite(user *User) bool {
	// Check config access level
	switch pc.config.WriteAccess {
	case "ANONYMOUS":
		return true
	case "REGISTERED":
		if user.IsAnonymous() {
			return false
		}
		// Registered user - check if approved and has write permission
		return (user.Approved() && user.CanWrite()) || user.Admin()
	case "APPROVED":
		if user.IsAnonymous() {
			return false
		}
		return (user.Approved() && user.CanWrite()) || user.Admin()
	case "ADMIN":
		if user.IsAnonymous() {
			return false
		}
		return user.Admin()
	default:
		return true // Default to anonymous access
	}
}

// canUpload checks if the user can upload.
func (pc *PermissionChecker) canUpload(user *User) bool {
	// Check config access level
	switch pc.config.AttachmentAccess {
	case "ANONYMOUS":
		return true
	case "REGISTERED":
		if user.IsAnonymous() {
			return false
		}
		// Registered user - check if approved and has upload permission
		return (user.Approved() && user.CanUpload()) || user.Admin()
	case "APPROVED":
		if user.IsAnonymous() {
			return false
		}
		return (user.Approved() && user.CanUpload()) || user.Admin()
	case "ADMIN":
		if user.IsAnonymous() {
			return false
		}
		return user.Admin()
	default:
		return true // Default to anonymous access
	}
}

// canAdmin checks if the user is an admin.
func (pc *PermissionChecker) canAdmin(user *User) bool {
	if user.IsAnonymous() {
		return false
	}
	return user.Admin()
}

// handleUnauthorized handles unauthorized access.
func (pc *PermissionChecker) handleUnauthorized(w http.ResponseWriter, r *http.Request, permission string) {
	user := GetUser(r)

	if isAPIRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		if user.IsAnonymous() {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "authentication required"})
		} else {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "insufficient permissions"})
		}
		return
	}

	if user.IsAnonymous() {
		// Not logged in - redirect to login
		http.Redirect(w, r, "/-/login?next="+r.URL.Path, http.StatusFound)
		return
	}

	// Logged in but not authorized - show 403
	http.Error(w, "Forbidden: insufficient permissions", http.StatusForbidden)
}

// isAPIRequest checks if the request is for the JSON API.
func isAPIRequest(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/-/api/")
}

// User is an alias to models.User for use in this package.
type User = models.User
