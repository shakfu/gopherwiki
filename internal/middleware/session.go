// Package middleware provides HTTP middleware for GopherWiki.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/gob"
	"log/slog"
	"net/http"

	"github.com/gorilla/sessions"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/models"
)

func init() {
	// Register custom types with gob so gorilla/sessions can
	// serialize them into cookies.
	gob.Register(FlashMessage{})
}

// Context keys for request context.
type contextKey string

const (
	// UserKey is the context key for the current user.
	UserKey contextKey = "user"
	// SessionKey is the context key for the session.
	SessionKey contextKey = "session"
	// FlashKey is the context key for flash messages.
	FlashKey contextKey = "flash"
	// CSRFContextKey is the context key for the current CSRF token.
	CSRFContextKey contextKey = "csrf"
)

const (
	// SessionName is the name of the session cookie.
	SessionName = "gopherwiki_session"
	// UserIDKey is the session key for the user ID.
	UserIDKey = "user_id"
	// CSRFCookieName is the cookie holding the CSRF token (double-submit pattern).
	CSRFCookieName = "gopherwiki_csrf"
	// CSRFFieldName is the form field carrying the CSRF token.
	CSRFFieldName = "csrf_token"
	// CSRFHeaderName is the request header carrying the CSRF token (for fetch/XHR/API).
	CSRFHeaderName = "X-CSRF-Token"
)

// csrfSafeMethods are HTTP methods that do not require a CSRF token.
var csrfSafeMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodOptions: true,
	http.MethodTrace:   true,
}

// generateCSRFToken returns a new random CSRF token, or "" if entropy is
// unavailable (in which case CSRF validation fails closed).
func generateCSRFToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// SessionManager handles session operations.
type SessionManager struct {
	store   sessions.Store
	queries *db.Queries
	secure  bool
}

// NewSessionManager creates a new SessionManager. When secure is true the
// session and CSRF cookies are marked Secure (only sent over HTTPS).
func NewSessionManager(secretKey string, secure bool, queries *db.Queries) *SessionManager {
	// Create cookie store with the secret key
	store := sessions.NewCookieStore([]byte(secretKey))
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}

	return &SessionManager{
		store:   store,
		queries: queries,
		secure:  secure,
	}
}

// Middleware returns the session middleware handler.
func (sm *SessionManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := sm.store.Get(r, SessionName)
		if err != nil {
			// Invalid session, create a new one
			slog.Warn("session error, creating new", "error", err)
			session, err = sm.store.New(r, SessionName)
			if err != nil {
				slog.Warn("failed to create new session", "error", err)
			}
		}

		// Get user from session
		var user *models.User
		if userID, ok := session.Values[UserIDKey].(int64); ok && userID > 0 {
			dbUser, err := sm.queries.GetUserByID(r.Context(), userID)
			if err == nil {
				user = models.NewUser(&dbUser)
			}
		}

		// If no user found, use anonymous user
		if user == nil {
			user = models.AnonymousUser()
		}

		// Add session and user to context
		ctx := context.WithValue(r.Context(), SessionKey, session)
		ctx = context.WithValue(ctx, UserKey, user)

		// Ensure a CSRF token exists in its own cookie (double-submit pattern).
		// Keeping it separate from the gorilla session avoids a competing
		// Set-Cookie when a handler also saves the session in the same request.
		csrfToken := ""
		if c, err := r.Cookie(CSRFCookieName); err == nil {
			csrfToken = c.Value
		}
		if csrfToken == "" {
			csrfToken = generateCSRFToken()
			http.SetCookie(w, &http.Cookie{
				Name:     CSRFCookieName,
				Value:    csrfToken,
				Path:     "/",
				MaxAge:   86400 * 30, // 30 days
				HttpOnly: true,
				Secure:   sm.secure,
				SameSite: http.SameSiteLaxMode,
			})
		}
		ctx = context.WithValue(ctx, CSRFContextKey, csrfToken)

		// Get flash messages and add to context
		flashes := session.Flashes()
		if len(flashes) > 0 {
			ctx = context.WithValue(ctx, FlashKey, flashes)
			// Save session to clear flashes
			session.Save(r, w)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetCSRFToken returns the CSRF token for the current request, or "".
func GetCSRFToken(r *http.Request) string {
	if t, ok := r.Context().Value(CSRFContextKey).(string); ok {
		return t
	}
	return ""
}

// CSRFProtect rejects state-changing requests (POST/PUT/DELETE/PATCH) that do
// not present the session's CSRF token, supplied either in the CSRFFieldName
// form field or the CSRFHeaderName header. It must run after Middleware so the
// token is present in the request context. Comparison is constant-time.
func (sm *SessionManager) CSRFProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if csrfSafeMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}

		expected := GetCSRFToken(r)
		got := r.Header.Get(CSRFHeaderName)
		if got == "" {
			// FormValue parses the body only for form content types, so JSON
			// API bodies are left intact for the handler to read.
			got = r.FormValue(CSRFFieldName)
		}

		if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			http.Error(w, "Forbidden - invalid or missing CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(r.Context()))
	})
}

// GetSession returns the session from the request context.
func GetSession(r *http.Request) *sessions.Session {
	if session, ok := r.Context().Value(SessionKey).(*sessions.Session); ok {
		return session
	}
	return nil
}

// GetUser returns the current user from the request context.
func GetUser(r *http.Request) *models.User {
	if user, ok := r.Context().Value(UserKey).(*models.User); ok {
		return user
	}
	return models.AnonymousUser()
}

// GetFlashes returns flash messages from the request context.
func GetFlashes(r *http.Request) []interface{} {
	if flashes, ok := r.Context().Value(FlashKey).([]interface{}); ok {
		return flashes
	}
	return nil
}

// Login sets the user ID in the session.
func (sm *SessionManager) Login(w http.ResponseWriter, r *http.Request, userID int64) error {
	session := GetSession(r)
	if session == nil {
		var err error
		session, err = sm.store.Get(r, SessionName)
		if err != nil {
			return err
		}
	}

	session.Values[UserIDKey] = userID
	return session.Save(r, w)
}

// Logout removes the user ID from the session.
func (sm *SessionManager) Logout(w http.ResponseWriter, r *http.Request) error {
	session := GetSession(r)
	if session == nil {
		var err error
		session, err = sm.store.Get(r, SessionName)
		if err != nil {
			return err
		}
	}

	delete(session.Values, UserIDKey)
	return session.Save(r, w)
}

// AddFlash adds a flash message to the session.
func (sm *SessionManager) AddFlash(w http.ResponseWriter, r *http.Request, message string, vars ...string) error {
	session := GetSession(r)
	if session == nil {
		var err error
		session, err = sm.store.Get(r, SessionName)
		if err != nil {
			return err
		}
	}

	session.AddFlash(message, vars...)
	return session.Save(r, w)
}

// FlashMessage represents a flash message with a category.
type FlashMessage struct {
	Category string
	Message  string
}

// AddFlashMessage adds a categorized flash message.
func (sm *SessionManager) AddFlashMessage(w http.ResponseWriter, r *http.Request, category, message string) error {
	session := GetSession(r)
	if session == nil {
		var err error
		session, err = sm.store.Get(r, SessionName)
		if err != nil {
			return err
		}
	}

	session.AddFlash(FlashMessage{Category: category, Message: message})
	return session.Save(r, w)
}
