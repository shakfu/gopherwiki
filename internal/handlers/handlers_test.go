package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"bytes"
	"mime/multipart"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/models"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/testutil"
)

// --- JSON endpoint tests ---

func TestHealthCheck(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/health", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want %q", resp["status"], "ok")
	}
	if resp["version"] != "test" {
		t.Errorf("version = %q, want %q", resp["version"], "test")
	}
}

func TestPreview(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page first so the path is valid
	env.Store.Store("testpage.md", "# Existing", "init", storage.Author{Name: "test", Email: "test@test.com"})

	form := url.Values{"content": {"# Hello World\n\nThis is **bold**."}}
	req := httptest.NewRequest("POST", "/testpage/preview", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	content, ok := resp["preview_content"].(string)
	if !ok || content == "" {
		t.Error("preview_content should be non-empty")
	}
	if !strings.Contains(content, "Hello World") {
		t.Errorf("preview_content should contain 'Hello World', got %q", content)
	}
}

func TestDraftCRUD(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create page in storage
	env.Store.Store("draftpage.md", "# Draft Test", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// Save draft
	form := url.Values{
		"content":     {"# Draft Content"},
		"cursor_line": {"5"},
		"cursor_ch":   {"10"},
	}
	req := httptest.NewRequest("POST", "/draftpage/draft", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("save draft status = %d, want %d", w.Code, http.StatusOK)
	}

	var saveResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &saveResp)
	if saveResp["success"] != true {
		t.Error("save draft should return success=true")
	}

	// Load draft
	req = httptest.NewRequest("GET", "/draftpage/draft", nil)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("load draft status = %d, want %d", w.Code, http.StatusOK)
	}

	var loadResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &loadResp)
	if loadResp["found"] != true {
		t.Error("load draft should return found=true")
	}
	if loadResp["content"] != "# Draft Content" {
		t.Errorf("draft content = %q, want %q", loadResp["content"], "# Draft Content")
	}

	// Delete draft
	req = httptest.NewRequest("DELETE", "/draftpage/draft", nil)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("delete draft status = %d, want %d", w.Code, http.StatusOK)
	}

	// Verify deleted
	req = httptest.NewRequest("GET", "/draftpage/draft", nil)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	var verifyResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &verifyResp)
	if verifyResp["found"] == true {
		t.Error("draft should be deleted")
	}
}

// --- Auth handler tests ---

func TestLogin_Get(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/login", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestLogin_Post_InvalidCredentials(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"email":    {"bad@example.com"},
		"password": {"wrongpassword"},
	}
	req := httptest.NewRequest("POST", "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// Should re-render login form (200), not redirect
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (re-rendered form)", w.Code, http.StatusOK)
	}
}

func TestLogin_Post_ValidCredentials(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Register a user first (so we have valid credentials in the DB)
	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:     "valid@example.com",
		Approved:  true,
		AllowRead: true,
	})

	// Hash the password properly for login
	// We need to use the auth package to register properly
	// Since CreateTestUser stores password directly, we use the auth service
	env.Server.Auth.UpdatePassword(context.Background(), user.ID, "testpassword123")

	form := url.Values{
		"email":    {"valid@example.com"},
		"password": {"testpassword123"},
	}
	req := httptest.NewRequest("POST", "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect after login)", w.Code, http.StatusFound)
	}
}

func TestRegister_Get(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/register", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRegister_Post_Success(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"name":      {"New User"},
		"email":     {"newuser@example.com"},
		"password":  {"securepassword123"},
		"password2": {"securepassword123"},
	}
	req := httptest.NewRequest("POST", "/-/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect after register)", w.Code, http.StatusFound)
	}
}

func TestRegister_Post_PasswordMismatch(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"name":      {"New User"},
		"email":     {"newuser@example.com"},
		"password":  {"password123"},
		"password2": {"different456"},
	}
	req := httptest.NewRequest("POST", "/-/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// Should re-render form (200)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (re-rendered form)", w.Code, http.StatusOK)
	}
}

func TestRegister_Disabled(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.DisableRegistration = true

	req := httptest.NewRequest("GET", "/-/register", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect when disabled)", w.Code, http.StatusFound)
	}
}

func TestLogout(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/logout", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/" {
		t.Errorf("Location = %q, want %q", loc, "/")
	}
}

// --- Page handler tests ---

func TestViewPage_Exists(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page in storage
	_, err := env.Store.Store("testpage.md", "# Test Page\n\nContent here.", "created testpage", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store page: %v", err)
	}

	req := httptest.NewRequest("GET", "/testpage", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestViewPage_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/nonexistent", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestEditPage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("editme.md", "# Edit Me", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/editme/edit", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSavePage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"content": {"# New Page\n\nNew content."},
		"commit":  {"Created new page"},
	}
	req := httptest.NewRequest("POST", "/newpage/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect after save)", w.Code, http.StatusFound)
	}

	// Verify page was created
	if !env.Store.Exists("newpage.md") {
		t.Error("page should exist after save")
	}
}

func TestDeletePage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("deleteme.md", "# Delete Me", "init", storage.Author{Name: "test", Email: "test@test.com"})

	form := url.Values{"message": {"Deleted page"}}
	req := httptest.NewRequest("POST", "/deleteme/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect after delete)", w.Code, http.StatusFound)
	}

	if env.Store.Exists("deleteme.md") {
		t.Error("page should not exist after delete")
	}
}

func TestHistoryPage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("histpage.md", "# History", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/histpage/history", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSourcePage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("srcpage.md", "# Source Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/srcpage/source", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Permission enforcement tests ---

func injectUser(req *http.Request, user *models.User) *http.Request {
	if user == nil {
		user = models.AnonymousUser()
	}
	ctx := context.WithValue(req.Context(), middleware.UserKey, user)
	return req.WithContext(ctx)
}

func TestReadProtection(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.ReadAccess = "REGISTERED"

	env.Store.Store("protected.md", "# Protected", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// Anonymous request (no session cookie)
	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/-/login") {
		t.Errorf("Location = %q, should contain '/-/login'", loc)
	}
}

func TestWriteProtection(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.WriteAccess = "REGISTERED"

	form := url.Values{"content": {"# test"}}
	req := httptest.NewRequest("POST", "/somepage/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", w.Code, http.StatusFound)
	}
}

func TestUploadProtection(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.AttachmentAccess = "REGISTERED"

	req := httptest.NewRequest("POST", "/somepage/attachments", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", w.Code, http.StatusFound)
	}
}

func TestAdminProtection(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/admin", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login for anonymous)", w.Code, http.StatusFound)
	}
}

func TestAdminProtection_NonAdmin(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a non-admin user and set up a valid session
	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:     "nonadmin@example.com",
		Approved:  true,
		AllowRead: true,
	})

	// Simulate login by making a request with session
	// First, login to get a session cookie
	env.Server.Auth.UpdatePassword(context.Background(), user.ID, "testpassword123")

	loginForm := url.Values{
		"email":    {"nonadmin@example.com"},
		"password": {"testpassword123"},
	}
	loginReq := httptest.NewRequest("POST", "/-/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	env.Router.ServeHTTP(loginW, loginReq)

	// Extract session cookie
	cookies := loginW.Result().Cookies()

	// Now request admin page with session cookie
	req := httptest.NewRequest("GET", "/-/admin", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (forbidden for non-admin)", w.Code, http.StatusForbidden)
	}
}

// --- Feed/sitemap tests ---

func TestRSSFeed(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page to have some changelog data
	env.Store.Store("feedpage.md", "# Feed Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/-/feed.rss", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "rss+xml") {
		t.Errorf("Content-Type = %q, should contain 'rss+xml'", ct)
	}
}

func TestAtomFeed(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("feedpage.md", "# Feed Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/-/feed.atom", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "atom+xml") {
		t.Errorf("Content-Type = %q, should contain 'atom+xml'", ct)
	}
}

func TestSitemap(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("mappage.md", "# Map Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/-/sitemap.xml", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "xml") {
		t.Errorf("Content-Type = %q, should contain 'xml'", ct)
	}
}

func TestRobotsTxt(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/robots.txt", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Sitemap:") {
		t.Errorf("body should contain 'Sitemap:', got %q", body)
	}
}

// --- Additional handler tests ---

func TestAbout(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/about", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestSourcePage_Raw(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("rawsrc.md", "# Raw Source", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/rawsrc/source?raw=1", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain for raw source", ct)
	}
	if !strings.Contains(w.Body.String(), "# Raw Source") {
		t.Error("raw source should contain markdown content")
	}
}

// --- Test helpers for authenticated requests ---

// loginAsAdmin creates an admin user, logs in, and returns session cookies.
func loginAsAdmin(t *testing.T, env *testutil.TestEnv) []*http.Cookie {
	t.Helper()
	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:       "admin@example.com",
		Admin:       true,
		Approved:    true,
		AllowRead:   true,
		AllowWrite:  true,
		AllowUpload: true,
	})
	env.Server.Auth.UpdatePassword(context.Background(), user.ID, "adminpassword123")

	form := url.Values{
		"email":    {"admin@example.com"},
		"password": {"adminpassword123"},
	}
	req := httptest.NewRequest("POST", "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("admin login failed: status = %d, want %d", w.Code, http.StatusFound)
	}
	return w.Result().Cookies()
}

// loginAsUser creates a regular user, logs in, and returns session cookies.
func loginAsUser(t *testing.T, env *testutil.TestEnv, email string) []*http.Cookie {
	t.Helper()
	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:       email,
		Approved:    true,
		AllowRead:   true,
		AllowWrite:  true,
		AllowUpload: true,
	})
	env.Server.Auth.UpdatePassword(context.Background(), user.ID, "userpassword123")

	form := url.Values{
		"email":    {email},
		"password": {"userpassword123"},
	}
	req := httptest.NewRequest("POST", "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("user login failed: status = %d, want %d", w.Code, http.StatusFound)
	}
	return w.Result().Cookies()
}

// requestWithCookies creates a request and attaches session cookies.
func requestWithCookies(method, path string, body *strings.Reader, cookies []*http.Cookie) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

// createTestIssue inserts an issue directly into the DB and returns its ID.
func createTestIssue(t *testing.T, env *testutil.TestEnv, title, description, status string) int64 {
	t.Helper()
	now := sql.NullTime{Time: time.Now(), Valid: true}
	params := db.CreateIssueParams{
		Title:          title,
		Description:    sql.NullString{String: description, Valid: description != ""},
		Status:         status,
		CreatedByName:  sql.NullString{String: "Test User", Valid: true},
		CreatedByEmail: sql.NullString{String: "test@test.com", Valid: true},
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	issue, err := env.DB.Queries.CreateIssue(context.Background(), params)
	if err != nil {
		t.Fatalf("failed to create test issue: %v", err)
	}
	return issue.ID
}

// --- Issue tracker handler tests ---

func TestIssueList(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	createTestIssue(t, env, "First Issue", "Description", "open")

	req := httptest.NewRequest("GET", "/-/issues", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "First Issue") {
		t.Error("issue list should contain 'First Issue'")
	}
}

func TestIssueCreate(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"title":       {"Bug: something broken"},
		"description": {"Details of the bug"},
	}
	req := httptest.NewRequest("POST", "/-/issues/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after create)", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/-/issues/") {
		t.Errorf("Location = %q, should redirect to issue view", loc)
	}

	// Verify issue exists in DB
	issues, err := env.DB.Queries.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("failed to list issues: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Title != "Bug: something broken" {
		t.Errorf("issue title = %q, want %q", issues[0].Title, "Bug: something broken")
	}
}

func TestIssueCreate_EmptyTitle(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"title":       {""},
		"description": {"No title given"},
	}
	req := httptest.NewRequest("POST", "/-/issues/new", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect back for validation)", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/-/issues/new" {
		t.Errorf("Location = %q, want %q", loc, "/-/issues/new")
	}

	// Verify no issue was created
	issues, err := env.DB.Queries.ListIssues(context.Background())
	if err != nil {
		t.Fatalf("failed to list issues: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(issues))
	}
}

func TestIssueView(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "View This Issue", "Some description", "open")

	req := httptest.NewRequest("GET", fmt.Sprintf("/-/issues/%d", id), nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "View This Issue") {
		t.Error("issue view should contain 'View This Issue'")
	}
}

func TestIssueView_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/issues/999", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestIssueEdit(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "Edit This Issue", "Original description", "open")
	cookies := loginAsUser(t, env, "issueuser@example.com")

	req := requestWithCookies("GET", fmt.Sprintf("/-/issues/%d/edit", id), nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Edit This Issue") {
		t.Error("edit form should contain issue title")
	}
}

func TestIssueUpdate(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "Old Title", "Old description", "open")
	cookies := loginAsUser(t, env, "issueuser2@example.com")

	form := url.Values{
		"title":       {"Updated Title"},
		"description": {"Updated description"},
	}
	req := requestWithCookies("POST", fmt.Sprintf("/-/issues/%d/edit", id), strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after update)", w.Code, http.StatusFound)
	}

	// Verify update
	issue, err := env.DB.Queries.GetIssue(context.Background(), id)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue.Title != "Updated Title" {
		t.Errorf("title = %q, want %q", issue.Title, "Updated Title")
	}
}

func TestIssueClose(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "Close Me", "", "open")
	cookies := loginAsUser(t, env, "closer@example.com")

	req := requestWithCookies("POST", fmt.Sprintf("/-/issues/%d/close", id), strings.NewReader(""), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after close)", w.Code, http.StatusFound)
	}

	issue, err := env.DB.Queries.GetIssue(context.Background(), id)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue.Status != "closed" {
		t.Errorf("status = %q, want %q", issue.Status, "closed")
	}
}

func TestIssueReopen(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "Reopen Me", "", "closed")
	cookies := loginAsUser(t, env, "reopener@example.com")

	req := requestWithCookies("POST", fmt.Sprintf("/-/issues/%d/reopen", id), strings.NewReader(""), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after reopen)", w.Code, http.StatusFound)
	}

	issue, err := env.DB.Queries.GetIssue(context.Background(), id)
	if err != nil {
		t.Fatalf("failed to get issue: %v", err)
	}
	if issue.Status != "open" {
		t.Errorf("status = %q, want %q", issue.Status, "open")
	}
}

func TestIssueDelete_Admin(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "Delete Me", "", "open")
	cookies := loginAsAdmin(t, env)

	req := requestWithCookies("POST", fmt.Sprintf("/-/issues/%d/delete", id), strings.NewReader(""), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after delete)", w.Code, http.StatusFound)
	}

	// Verify deleted
	_, err := env.DB.Queries.GetIssue(context.Background(), id)
	if err == nil {
		t.Error("issue should be deleted")
	}
}

func TestIssueDelete_NonAdmin(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createTestIssue(t, env, "Cannot Delete", "", "open")
	cookies := loginAsUser(t, env, "regular@example.com")

	req := requestWithCookies("POST", fmt.Sprintf("/-/issues/%d/delete", id), strings.NewReader(""), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// Non-admin should be forbidden
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (forbidden for non-admin)", w.Code, http.StatusForbidden)
	}

	// Verify issue still exists
	_, err := env.DB.Queries.GetIssue(context.Background(), id)
	if err != nil {
		t.Error("issue should still exist after non-admin delete attempt")
	}
}

// --- Search handler tests ---

func TestSearch_WithResults(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("searchable.md", "# Searchable\n\nThis contains the keyword findme.", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/-/search?query=findme", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "searchable") && !strings.Contains(body, "Searchable") {
		t.Error("search results should include the matching page")
	}
}

func TestSearch_NoResults(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/search?query=nonexistentkeyword12345", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Rename handler tests ---

func TestRenamePage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("oldname.md", "# Old Name\n\nContent.", "init", storage.Author{Name: "test", Email: "test@test.com"})

	form := url.Values{
		"new_pagename": {"newname"},
		"message":      {"Renamed page"},
	}
	req := httptest.NewRequest("POST", "/oldname/rename", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after rename)", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/newname" {
		t.Errorf("Location = %q, want %q", loc, "/newname")
	}

	// Verify old page gone, new page exists
	if env.Store.Exists("oldname.md") {
		t.Error("old page should not exist after rename")
	}
	if !env.Store.Exists("newname.md") {
		t.Error("new page should exist after rename")
	}
}

// --- Revert handler tests ---

func TestRevert(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page, then edit it to create a second commit
	_, err := env.Store.Store("revertme.md", "# Original Content", "initial commit", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store page: %v", err)
	}
	_, err = env.Store.Store("revertme.md", "# Modified Content", "second commit", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store second version: %v", err)
	}

	// Get the second commit's revision from the log
	log, err := env.Store.Log("revertme.md", 2)
	if err != nil {
		t.Fatalf("failed to get log: %v", err)
	}
	if len(log) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(log))
	}
	// log[0] is the most recent commit (second commit)
	secondRev := log[0].Revision

	// Verify current content is the modified version
	content, _ := env.Store.Load("revertme.md", "")
	if !strings.Contains(content, "Modified") {
		t.Fatalf("expected 'Modified' in content before revert, got %q", content)
	}

	// Login as a user (revert requires authentication)
	cookies := loginAsUser(t, env, "reverter@example.com")

	// Revert the second commit
	form := url.Values{"message": {"Reverting second commit"}}
	req := requestWithCookies("POST", "/-/commit/"+secondRev+"/revert", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect after revert)", w.Code, http.StatusFound)
	}
}

// --- Read-only view handler tests ---

func TestChangelog(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("clpage.md", "# Changelog Page", "a commit message", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/-/changelog", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "a commit message") {
		t.Error("changelog should contain the commit message")
	}
}

func TestPageIndex(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("indexed.md", "# Indexed Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/-/pageindex", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "indexed") {
		t.Error("page index should contain the page name")
	}
}

func TestCommitView(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	_, err := env.Store.Store("commitpage.md", "# Commit Page", "test commit for view", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store page: %v", err)
	}

	meta, err := env.Store.Metadata("commitpage.md", "")
	if err != nil {
		t.Fatalf("failed to get metadata: %v", err)
	}

	req := httptest.NewRequest("GET", "/-/commit/"+meta.Revision, nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "test commit for view") {
		t.Error("commit view should contain the commit message")
	}
}

func TestBlamePage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("blamepage.md", "# Blame Page\n\nLine two.", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/blamepage/blame", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestDiffPage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	_, err := env.Store.Store("diffpage.md", "# Version A", "version A", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store version A: %v", err)
	}
	_, err = env.Store.Store("diffpage.md", "# Version B", "version B", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store version B: %v", err)
	}

	// Get revisions from the log
	logEntries, err := env.Store.Log("diffpage.md", 2)
	if err != nil {
		t.Fatalf("failed to get log: %v", err)
	}
	if len(logEntries) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(logEntries))
	}
	revB := logEntries[0].Revision // most recent
	revA := logEntries[1].Revision // older

	req := httptest.NewRequest("GET", "/diffpage/diff?rev_a="+revA+"&rev_b="+revB, nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Version A") && !strings.Contains(body, "Version B") {
		t.Error("diff view should show revision content")
	}
}

// --- Admin dashboard test ---

func TestAdminDashboard(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	cookies := loginAsAdmin(t, env)

	req := requestWithCookies("GET", "/-/admin", nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Admin") {
		t.Error("admin dashboard should contain 'Admin'")
	}
}

// --- Issue new form test ---

func TestIssueNewForm(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	cookies := loginAsUser(t, env, "issuecreator@example.com")

	req := requestWithCookies("GET", "/-/issues/new", nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Group A: Admin User Management ---

func TestAdminUsers_List(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	// Create an extra user to verify listing
	testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:    "listed@example.com",
		Name:     "Listed User",
		Approved: true,
	})

	req := requestWithCookies("GET", "/-/admin/users", nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Listed User") {
		t.Error("admin users page should contain 'Listed User'")
	}
}

func TestAdminUserEdit(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:    "editable@example.com",
		Name:     "Editable User",
		Approved: true,
	})

	req := requestWithCookies("GET", fmt.Sprintf("/-/admin/users/%d", user.ID), nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Editable User") {
		t.Error("admin user edit page should contain user name")
	}
}

func TestAdminUserSave(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:    "saveme@example.com",
		Name:     "Save Me",
		Approved: true,
	})

	form := url.Values{
		"name":         {"Updated Name"},
		"is_approved":  {"on"},
		"is_admin":     {"on"},
		"allow_read":   {"on"},
		"allow_write":  {"on"},
		"allow_upload": {"on"},
	}
	req := requestWithCookies("POST", fmt.Sprintf("/-/admin/users/%d", user.ID), strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify the update persisted
	updated, err := env.DB.Queries.GetUserByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("failed to get user: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("name = %q, want %q", updated.Name, "Updated Name")
	}
	if !updated.IsAdmin.Bool {
		t.Error("user should be admin after update")
	}
}

func TestAdminUserSave_NonAdmin(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsUser(t, env, "regular@example.com")

	// Try to hit admin endpoint as non-admin
	form := url.Values{"name": {"Hacked"}}
	req := requestWithCookies("POST", "/-/admin/users/1", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d (forbidden for non-admin)", w.Code, http.StatusForbidden)
	}
}

func TestAdminUserDelete(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Email:    "deletable@example.com",
		Name:     "Deletable User",
		Approved: true,
	})

	req := requestWithCookies("POST", fmt.Sprintf("/-/admin/users/%d/delete", user.ID), strings.NewReader(""), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify user is deleted
	_, err := env.DB.Queries.GetUserByID(context.Background(), user.ID)
	if err == nil {
		t.Error("user should be deleted from DB")
	}
}

func TestAdminUserDelete_Self(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	// Get the admin user ID from the DB
	users, err := env.Server.Auth.ListUsers(context.Background())
	if err != nil || len(users) == 0 {
		t.Fatal("expected at least one user (the admin)")
	}
	adminID := users[0].ID

	req := requestWithCookies("POST", fmt.Sprintf("/-/admin/users/%d/delete", adminID), strings.NewReader(""), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// Should redirect (not delete self)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify user still exists
	_, err = env.DB.Queries.GetUserByID(context.Background(), adminID)
	if err != nil {
		t.Error("admin user should still exist after self-delete attempt")
	}
}

func TestAdminSettings_Get(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	req := requestWithCookies("GET", "/-/admin/settings", nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAdminSiteSettingsSave(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	form := url.Values{
		"site_name": {"My Test Wiki"},
		"site_logo": {"/static/logo.png"},
	}
	req := requestWithCookies("POST", "/-/admin/site-settings", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify preference was saved
	pref, err := env.DB.Queries.GetPreference(context.Background(), "site_name")
	if err != nil {
		t.Fatalf("failed to get preference: %v", err)
	}
	if pref.Value.String != "My Test Wiki" {
		t.Errorf("site_name = %q, want %q", pref.Value.String, "My Test Wiki")
	}

	logoPref, err := env.DB.Queries.GetPreference(context.Background(), "site_logo")
	if err != nil {
		t.Fatalf("failed to get logo preference: %v", err)
	}
	if logoPref.Value.String != "/static/logo.png" {
		t.Errorf("site_logo = %q, want %q", logoPref.Value.String, "/static/logo.png")
	}
}

// --- Group B: User Settings ---

func TestSettings_Get(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsUser(t, env, "settings@example.com")

	req := requestWithCookies("GET", "/-/settings", nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "settings@example.com") {
		t.Error("settings page should contain the user's email")
	}
}

func TestSettings_Get_Anonymous(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/settings", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// Anonymous users should be redirected to login
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect to login)", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "/-/login") {
		t.Errorf("Location = %q, should redirect to login", loc)
	}
}

func TestSettings_UpdateName(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsUser(t, env, "namechange@example.com")

	form := url.Values{
		"action": {"update_name"},
		"name":   {"New Display Name"},
	}
	req := requestWithCookies("POST", "/-/settings", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Look up user in DB to verify name changed
	users, err := env.Server.Auth.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("failed to list users: %v", err)
	}
	found := false
	for _, u := range users {
		if u.GetEmail() == "namechange@example.com" {
			if u.GetName() != "New Display Name" {
				t.Errorf("name = %q, want %q", u.GetName(), "New Display Name")
			}
			found = true
		}
	}
	if !found {
		t.Error("user 'namechange@example.com' not found")
	}
}

func TestSettings_ChangePassword(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsUser(t, env, "pwchange@example.com")

	form := url.Values{
		"action":           {"change_password"},
		"current_password": {"userpassword123"},
		"new_password":     {"newsecurepassword456"},
		"confirm_password": {"newsecurepassword456"},
	}
	req := requestWithCookies("POST", "/-/settings", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify the new password works by logging in again
	loginForm := url.Values{
		"email":    {"pwchange@example.com"},
		"password": {"newsecurepassword456"},
	}
	loginReq := httptest.NewRequest("POST", "/-/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginW := httptest.NewRecorder()
	env.Router.ServeHTTP(loginW, loginReq)

	if loginW.Code != http.StatusFound {
		t.Errorf("login with new password failed: status = %d, want %d", loginW.Code, http.StatusFound)
	}
}

// --- Group C: Page Create & Attachments ---

func TestCreateForm(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/-/create", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestCreatePage(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"pagepath": {"brand-new-page"},
	}
	req := httptest.NewRequest("POST", "/-/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// handleCreate redirects to /{path}/edit
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/brand-new-page/edit" {
		t.Errorf("Location = %q, want %q", loc, "/brand-new-page/edit")
	}
}

func TestAttachmentsList(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("attachpage.md", "# Attachment Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/attachpage/attachments", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestUploadAttachment(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page first
	env.Store.Store("uploadpage.md", "# Upload Page", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// Build multipart form with a file
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	part.Write([]byte("hello world file content"))
	writer.Close()

	req := httptest.NewRequest("POST", "/uploadpage/attachments", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify the file was stored (attachment dir is the page filename without .md)
	if !env.Store.Exists("uploadpage/test.txt") {
		t.Error("uploaded file should exist in storage")
	}
}

func TestServeAttachment(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page and an attachment
	env.Store.Store("mypage.md", "# My Page", "init", storage.Author{Name: "test", Email: "test@test.com"})
	env.Store.StoreBytes("mypage/report.pdf", []byte("fake-pdf-content"), "add attachment", storage.Author{Name: "test", Email: "test@test.com"})

	// Test 1: Request the attachment URL
	req := httptest.NewRequest("GET", "/mypage/report.pdf", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("attachment: status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	if w.Body.String() != "fake-pdf-content" {
		t.Errorf("body = %q, want %q", w.Body.String(), "fake-pdf-content")
	}

	// Test 2: Non-existent attachment returns 404
	req = httptest.NewRequest("GET", "/mypage/nonexistent.pdf", nil)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("missing attachment: status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestNestedPageRouting(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("docs/getting-started.md", "# Getting Started", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/docs/getting-started", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("nested page: status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Group D: Security Regression Tests ---

func TestXSS_ScriptTagStripped(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Store a page with XSS payload in markdown
	env.Store.Store("xsstest.md", "# Test\n\n<script>alert(1)</script>", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/xsstest", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if strings.Contains(body, "<script>") {
		t.Error("rendered page must not contain raw <script> tags (XSS vulnerability)")
	}
}

func TestPathTraversal_ViewBlocked(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Attempt to traverse to system files via the wiki view handler
	req := httptest.NewRequest("GET", "/..%2f..%2f..%2fetc%2fpasswd", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	body := w.Body.String()
	// Must not contain system file content
	if strings.Contains(body, "root:") {
		t.Error("path traversal should be blocked, but system file content was returned")
	}
}

func TestPathTraversal_SaveBlocked(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	form := url.Values{
		"content": {"# Evil"},
		"commit":  {"evil commit"},
	}
	req := httptest.NewRequest("POST", "/..%2f..%2fevil/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	// The storage layer should reject the path traversal attempt.
	// Either it returns an error page or redirects, but must not succeed.
	if env.Store.Exists("../../evil.md") {
		t.Error("path traversal save should be blocked, but file was created")
	}
}

func TestSearch_ReDoS_Safe(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page to search
	env.Store.Store("redospage.md", "# ReDoS Test\n\naaaaaaaaaaaa", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// Use a pattern known to cause ReDoS in naive regex engines
	req := httptest.NewRequest("GET", "/-/search?query=(a%2B)%2B%24", nil)
	w := httptest.NewRecorder()

	// If this hangs, the test will be killed by the test timeout
	env.Router.ServeHTTP(w, req)

	// We just need it to return without hanging or erroring
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Group E: ETag Caching ---

func TestETag_Present(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("etagpage.md", "# ETag Test", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/etagpage", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Error("response should include an ETag header")
	}
}

func TestETag_NotModified(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("etagpage2.md", "# ETag 304 Test", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// First request to get the ETag
	req1 := httptest.NewRequest("GET", "/etagpage2", nil)
	w1 := httptest.NewRecorder()
	env.Router.ServeHTTP(w1, req1)

	if w1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", w1.Code, http.StatusOK)
	}
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("first response should include an ETag header")
	}

	// Second request with If-None-Match
	req2 := httptest.NewRequest("GET", "/etagpage2", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	env.Router.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d (304 Not Modified)", w2.Code, http.StatusNotModified)
	}
}

// --- Group F: Issue Filters ---

func TestIssueList_StatusFilter(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	createTestIssue(t, env, "Open Issue", "desc", "open")
	createTestIssue(t, env, "Closed Issue", "desc", "closed")

	req := httptest.NewRequest("GET", "/-/issues?status=open", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Open Issue") {
		t.Error("filtered list should contain 'Open Issue'")
	}
	if strings.Contains(body, "Closed Issue") {
		t.Error("filtered list should NOT contain 'Closed Issue' when filtering by status=open")
	}
}

func TestIssueList_CategoryFilter(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create issues with categories
	now := sql.NullTime{Time: time.Now(), Valid: true}
	env.DB.Queries.CreateIssue(context.Background(), db.CreateIssueParams{
		Title:          "Bug Issue",
		Description:    sql.NullString{String: "a bug", Valid: true},
		Status:         "open",
		Category:       sql.NullString{String: "bug", Valid: true},
		CreatedByName:  sql.NullString{String: "Test", Valid: true},
		CreatedByEmail: sql.NullString{String: "test@test.com", Valid: true},
		CreatedAt:      now,
		UpdatedAt:      now,
	})
	env.DB.Queries.CreateIssue(context.Background(), db.CreateIssueParams{
		Title:          "Feature Issue",
		Description:    sql.NullString{String: "a feature", Valid: true},
		Status:         "open",
		Category:       sql.NullString{String: "feature", Valid: true},
		CreatedByName:  sql.NullString{String: "Test", Valid: true},
		CreatedByEmail: sql.NullString{String: "test@test.com", Valid: true},
		CreatedAt:      now,
		UpdatedAt:      now,
	})

	req := httptest.NewRequest("GET", "/-/issues?category=bug", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Bug Issue") {
		t.Error("filtered list should contain 'Bug Issue'")
	}
	if strings.Contains(body, "Feature Issue") {
		t.Error("filtered list should NOT contain 'Feature Issue' when filtering by category=bug")
	}
}

// --- Admin Issue Settings ---

func TestAdminIssueSettingsSave(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	form := url.Values{
		"issue_categories": {"bug, feature, enhancement"},
		"issue_tags":       {"urgent, low-priority, help-wanted"},
	}
	req := requestWithCookies("POST", "/-/admin/issue-settings", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}

	// Verify categories preference was saved
	catPref, err := env.DB.Queries.GetPreference(context.Background(), "issue_categories")
	if err != nil {
		t.Fatalf("failed to get categories preference: %v", err)
	}
	if !strings.Contains(catPref.Value.String, "bug") {
		t.Errorf("categories = %q, should contain 'bug'", catPref.Value.String)
	}

	// Verify tags preference was saved
	tagPref, err := env.DB.Queries.GetPreference(context.Background(), "issue_tags")
	if err != nil {
		t.Fatalf("failed to get tags preference: %v", err)
	}
	if !strings.Contains(tagPref.Value.String, "urgent") {
		t.Errorf("tags = %q, should contain 'urgent'", tagPref.Value.String)
	}
}

// --- Delete and Rename form tests ---

func TestDeleteForm(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("fordelete.md", "# For Delete", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/fordelete/delete", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestRenameForm(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("forrename.md", "# For Rename", "init", storage.Author{Name: "test", Email: "test@test.com"})

	req := httptest.NewRequest("GET", "/forrename/rename", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

// --- Edit conflict detection tests ---

func TestSavePage_ConflictDetected(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page and get its revision
	_, err := env.Store.Store("conflictpage.md", "# Original", "initial", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store page: %v", err)
	}
	meta, err := env.Store.Metadata("conflictpage.md", "")
	if err != nil {
		t.Fatalf("failed to get metadata: %v", err)
	}
	staleRevision := meta.Revision

	// Simulate another user saving (changing the HEAD revision)
	_, err = env.Store.Store("conflictpage.md", "# Modified by other user", "other edit", storage.Author{Name: "other", Email: "other@test.com"})
	if err != nil {
		t.Fatalf("failed to store second version: %v", err)
	}

	// Attempt to save with the stale revision
	form := url.Values{
		"content":  {"# My conflicting edit"},
		"commit":   {"my edit"},
		"revision": {staleRevision},
	}
	req := httptest.NewRequest("POST", "/conflictpage/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d (conflict)", w.Code, http.StatusConflict)
	}

	// The response should contain the user's submitted content (preserved)
	body := w.Body.String()
	if !strings.Contains(body, "My conflicting edit") {
		t.Error("conflict response should preserve the user's submitted content")
	}
	if !strings.Contains(body, "Edit conflict") {
		t.Error("conflict response should contain conflict warning message")
	}

	// Verify the original (other user's) content is still in storage
	content, err := env.Store.Load("conflictpage.md", "")
	if err != nil {
		t.Fatalf("failed to load page: %v", err)
	}
	if !strings.Contains(content, "Modified by other user") {
		t.Errorf("storage content = %q, want 'Modified by other user'", content)
	}
}

func TestSavePage_NoConflict(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page and get its revision
	_, err := env.Store.Store("noconflict.md", "# Original", "initial", storage.Author{Name: "test", Email: "test@test.com"})
	if err != nil {
		t.Fatalf("failed to store page: %v", err)
	}
	meta, err := env.Store.Metadata("noconflict.md", "")
	if err != nil {
		t.Fatalf("failed to get metadata: %v", err)
	}
	currentRevision := meta.Revision

	// Save with the correct (current) revision
	form := url.Values{
		"content":  {"# Updated content"},
		"commit":   {"normal edit"},
		"revision": {currentRevision},
	}
	req := httptest.NewRequest("POST", "/noconflict/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect after save)", w.Code, http.StatusFound)
	}

	// Verify content was saved
	content, err := env.Store.Load("noconflict.md", "")
	if err != nil {
		t.Fatalf("failed to load page: %v", err)
	}
	if !strings.Contains(content, "Updated content") {
		t.Errorf("storage content = %q, want 'Updated content'", content)
	}
}

func TestSavePage_EmptyRevisionBypass(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Save a new page with no revision field (backward compat / new page creation)
	form := url.Values{
		"content": {"# Brand New Page"},
		"commit":  {"created page"},
	}
	req := httptest.NewRequest("POST", "/emptyrevpage/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect after save)", w.Code, http.StatusFound)
	}

	// Verify page was created
	if !env.Store.Exists("emptyrevpage.md") {
		t.Error("page should exist after save with empty revision")
	}
}

