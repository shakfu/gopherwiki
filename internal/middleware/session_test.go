package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/sessions"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/models"
)

// openTestDB creates an in-memory SQLite database with schema for testing.
func openTestDB(t *testing.T) *db.Database {
	t.Helper()
	database, err := db.Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// createTestUser inserts a user into the test DB and returns its ID.
func createTestUser(t *testing.T, database *db.Database, name, email string) int64 {
	t.Helper()
	now := time.Now()
	params := db.CreateUserParams{
		Name:         name,
		Email:        email,
		PasswordHash: db.NullString("hash"),
		FirstSeen:    db.NullTime(now),
		LastSeen:     db.NullTime(now),
		IsApproved:   db.NullBool(true),
		IsAdmin:      db.NullBool(false),
		AllowRead:    db.NullBool(true),
		AllowWrite:   db.NullBool(true),
		AllowUpload:  db.NullBool(true),
	}
	user, err := database.Queries.CreateUser(context.Background(), params)
	if err != nil {
		t.Fatalf("failed to create user: %v", err)
	}
	return user.ID
}

func newTestSessionManager(t *testing.T, database *db.Database) *SessionManager {
	t.Helper()
	return NewSessionManager("test-secret-key-for-tests", database.Queries)
}

// --- NewSessionManager tests ---

func TestNewSessionManager(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	if sm.store == nil {
		t.Fatal("store should not be nil")
	}
	if sm.queries == nil {
		t.Fatal("queries should not be nil")
	}
}

// --- Middleware tests ---

func TestMiddleware_AnonymousUser(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	var gotUser *models.User
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, r)

	if gotUser == nil {
		t.Fatal("user should not be nil")
	}
	if !gotUser.IsAnonymous() {
		t.Error("user should be anonymous when no session cookie is present")
	}
}

func TestMiddleware_AuthenticatedUser(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)
	userID := createTestUser(t, database, "Alice", "alice@example.com")

	// First request: log in (sets session cookie)
	var setCookies []*http.Cookie
	loginHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := sm.Login(w, r, userID); err != nil {
			t.Fatalf("login failed: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/login", nil)
	loginHandler.ServeHTTP(w, r)
	setCookies = w.Result().Cookies()

	// Second request: use the session cookie
	var gotUser *models.User
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	handler.ServeHTTP(w, r)

	if gotUser == nil {
		t.Fatal("user should not be nil")
	}
	if gotUser.IsAnonymous() {
		t.Error("user should be authenticated after login")
	}
	if gotUser.GetName() != "Alice" {
		t.Errorf("user name = %q, want %q", gotUser.GetName(), "Alice")
	}
	if gotUser.GetEmail() != "alice@example.com" {
		t.Errorf("user email = %q, want %q", gotUser.GetEmail(), "alice@example.com")
	}
}

func TestMiddleware_InvalidSessionRecovery(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	var gotUser *models.User
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	// Add a corrupted session cookie
	r.AddCookie(&http.Cookie{Name: SessionName, Value: "invalid-garbage-data"})
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotUser == nil {
		t.Fatal("user should not be nil even with bad session")
	}
	if !gotUser.IsAnonymous() {
		t.Error("user should be anonymous after session recovery")
	}
}

func TestMiddleware_DeletedUserBecomesAnonymous(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)
	userID := createTestUser(t, database, "Bob", "bob@example.com")

	// Log in
	var setCookies []*http.Cookie
	loginHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Login(w, r, userID)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/login", nil)
	loginHandler.ServeHTTP(w, r)
	setCookies = w.Result().Cookies()

	// Delete user from DB
	database.Queries.DeleteUser(context.Background(), userID)

	// Use the old session cookie -- user no longer exists
	var gotUser *models.User
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	handler.ServeHTTP(w, r)

	if gotUser == nil {
		t.Fatal("user should not be nil")
	}
	if !gotUser.IsAnonymous() {
		t.Error("user should be anonymous when DB user is deleted")
	}
}

func TestMiddleware_SessionInContext(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	var gotSession *sessions.Session
	handler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSession = GetSession(r)
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	handler.ServeHTTP(w, r)

	if gotSession == nil {
		t.Fatal("session should be in context after middleware")
	}
}

func TestMiddleware_FlashMessages(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	// Request 1: add a flash
	var setCookies []*http.Cookie
	addFlashHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.AddFlash(w, r, "hello flash")
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/add-flash", nil)
	addFlashHandler.ServeHTTP(w, r)
	setCookies = w.Result().Cookies()

	// Request 2: flash should be available in context
	var gotFlashes []interface{}
	readFlashHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFlashes = GetFlashes(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/read-flash", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	readFlashHandler.ServeHTTP(w, r)

	if len(gotFlashes) != 1 {
		t.Fatalf("flash count = %d, want 1", len(gotFlashes))
	}
	if gotFlashes[0] != "hello flash" {
		t.Errorf("flash = %v, want %q", gotFlashes[0], "hello flash")
	}

	// Request 3: flash should be cleared after being read
	setCookies = w.Result().Cookies()

	var gotFlashes2 []interface{}
	readAgainHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFlashes2 = GetFlashes(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/read-flash-again", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	readAgainHandler.ServeHTTP(w, r)

	if len(gotFlashes2) != 0 {
		t.Errorf("flash count after clearing = %d, want 0", len(gotFlashes2))
	}
}

// --- GetUser / GetSession / GetFlashes without middleware ---

func TestGetUser_NoContext(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	user := GetUser(r)

	if user == nil {
		t.Fatal("GetUser should never return nil")
	}
	if !user.IsAnonymous() {
		t.Error("GetUser without middleware should return anonymous")
	}
}

func TestGetSession_NoContext(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	session := GetSession(r)

	if session != nil {
		t.Error("GetSession without middleware should return nil")
	}
}

func TestGetFlashes_NoContext(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	flashes := GetFlashes(r)

	if flashes != nil {
		t.Error("GetFlashes without middleware should return nil")
	}
}

// --- Login / Logout tests ---

func TestLogin_And_Logout(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)
	userID := createTestUser(t, database, "Carol", "carol@example.com")

	// Login
	var setCookies []*http.Cookie
	loginHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := sm.Login(w, r, userID); err != nil {
			t.Fatalf("login failed: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	loginHandler.ServeHTTP(w, r)
	setCookies = w.Result().Cookies()

	// Verify logged in
	var gotUser *models.User
	checkHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	checkHandler.ServeHTTP(w, r)

	if gotUser.IsAnonymous() {
		t.Fatal("user should be authenticated after login")
	}

	// Logout
	logoutHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := sm.Logout(w, r); err != nil {
			t.Fatalf("logout failed: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/logout", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	logoutHandler.ServeHTTP(w, r)
	setCookies = w.Result().Cookies()

	// Verify logged out
	var gotUser2 *models.User
	checkHandler2 := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser2 = GetUser(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	checkHandler2.ServeHTTP(w, r)

	if !gotUser2.IsAnonymous() {
		t.Error("user should be anonymous after logout")
	}
}

// --- AddFlashMessage tests ---

func TestAddFlashMessage(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	var setCookies []*http.Cookie
	addHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.AddFlashMessage(w, r, "success", "Page saved")
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	addHandler.ServeHTTP(w, r)
	setCookies = w.Result().Cookies()

	// Read the flash
	var gotFlashes []interface{}
	readHandler := sm.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFlashes = GetFlashes(r)
		w.WriteHeader(http.StatusOK)
	}))

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	for _, c := range setCookies {
		r.AddCookie(c)
	}
	readHandler.ServeHTTP(w, r)

	if len(gotFlashes) != 1 {
		t.Fatalf("flash count = %d, want 1", len(gotFlashes))
	}

	msg, ok := gotFlashes[0].(FlashMessage)
	if !ok {
		t.Fatalf("flash type = %T, want FlashMessage", gotFlashes[0])
	}
	if msg.Category != "success" {
		t.Errorf("flash category = %q, want %q", msg.Category, "success")
	}
	if msg.Message != "Page saved" {
		t.Errorf("flash message = %q, want %q", msg.Message, "Page saved")
	}
}

// --- Login without middleware (fallback to store.Get) ---

func TestLogin_WithoutMiddleware(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)
	userID := createTestUser(t, database, "Dave", "dave@example.com")

	// Login without session middleware (GetSession returns nil, falls back to store.Get)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/login", nil)
	err := sm.Login(w, r, userID)
	if err != nil {
		t.Fatalf("Login without middleware should succeed, got: %v", err)
	}
}

func TestLogout_WithoutMiddleware(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/logout", nil)
	err := sm.Logout(w, r)
	if err != nil {
		t.Fatalf("Logout without middleware should succeed, got: %v", err)
	}
}

func TestAddFlash_WithoutMiddleware(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	err := sm.AddFlash(w, r, "test flash")
	if err != nil {
		t.Fatalf("AddFlash without middleware should succeed, got: %v", err)
	}
}

func TestAddFlashMessage_WithoutMiddleware(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	err := sm.AddFlashMessage(w, r, "danger", "Something failed")
	if err != nil {
		t.Fatalf("AddFlashMessage without middleware should succeed, got: %v", err)
	}
}
