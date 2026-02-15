package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/testutil"
)

// --- Helpers ---

// apiGet performs a GET request and returns the response.
func apiGet(t *testing.T, env *testutil.TestEnv, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	return w
}

// apiRequest performs a request with the given method/body and returns the response.
func apiRequest(t *testing.T, env *testutil.TestEnv, method, path, body string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	return w
}

// parseAPIResponse parses a JSON API response envelope.
func parseAPIResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse JSON response: %v\nbody: %s", err, w.Body.String())
	}
	return resp
}

// createAPITestIssue inserts a test issue directly into the DB.
func createAPITestIssue(t *testing.T, env *testutil.TestEnv, title, description, status, category string, tags []string) int64 {
	t.Helper()
	now := sql.NullTime{Time: time.Now(), Valid: true}
	tagStr := strings.Join(tags, ",")
	params := db.CreateIssueParams{
		Title:          title,
		Description:    sql.NullString{String: description, Valid: description != ""},
		Status:         status,
		Category:       sql.NullString{String: category, Valid: category != ""},
		Tags:           sql.NullString{String: tagStr, Valid: tagStr != ""},
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

func createTestComment(t *testing.T, env *testutil.TestEnv, issueID int64, content, authorName, authorEmail string) db.IssueComment {
	t.Helper()
	now := time.Now()
	comment, err := env.DB.Queries.CreateIssueComment(context.Background(), db.CreateIssueCommentParams{
		IssueID:     issueID,
		Content:     content,
		AuthorName:  db.NullString(authorName),
		AuthorEmail: db.NullString(authorEmail),
		CreatedAt:   db.NullTime(now),
		UpdatedAt:   db.NullTime(now),
	})
	if err != nil {
		t.Fatalf("failed to create test comment: %v", err)
	}
	return comment
}

// --- Page API Tests ---

func TestAPIPageList(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("alpha.md", "# Alpha", "init", storage.Author{Name: "test", Email: "test@test.com"})
	env.Store.Store("beta.md", "# Beta", "init", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/pages", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be an array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 pages, got %d", len(data))
	}
}

func TestAPIPageGet(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("hello.md", "# Hello World\n\nContent here.", "created hello", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/pages/hello", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data should be an object, got %T", resp["data"])
	}
	if data["path"] != "hello" {
		t.Errorf("path = %v, want 'hello'", data["path"])
	}
	if data["exists"] != true {
		t.Errorf("exists = %v, want true", data["exists"])
	}
	content, _ := data["content"].(string)
	if !strings.Contains(content, "Hello World") {
		t.Errorf("content = %q, should contain 'Hello World'", content)
	}
}

func TestAPIPageGet_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/pages/nonexistent", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}

	resp := parseAPIResponse(t, w)
	if resp["error"] == nil || resp["error"] == "" {
		t.Error("response should contain error message")
	}
}

func TestAPIPageGet_WithRevision(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("versioned.md", "# Version 1", "v1", storage.Author{Name: "test", Email: "test@test.com"})
	meta, _ := env.Store.Metadata("versioned.md", "")
	rev1 := meta.Revision

	env.Store.Store("versioned.md", "# Version 2", "v2", storage.Author{Name: "test", Email: "test@test.com"})

	// Get at specific revision
	w := apiGet(t, env, "/-/api/v1/pages/versioned?revision="+rev1, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	content := data["content"].(string)
	if !strings.Contains(content, "Version 1") {
		t.Errorf("content = %q, should contain 'Version 1'", content)
	}
}

func TestAPIPageGet_ETag(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("etagapi.md", "# ETag Test", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// First request to get ETag
	w1 := apiGet(t, env, "/-/api/v1/pages/etagapi", nil)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", w1.Code, http.StatusOK)
	}

	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("response should include ETag header")
	}

	// Second request with If-None-Match
	req := httptest.NewRequest("GET", "/-/api/v1/pages/etagapi", nil)
	req.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	env.Router.ServeHTTP(w2, req)

	if w2.Code != http.StatusNotModified {
		t.Errorf("status = %d, want %d (304 Not Modified)", w2.Code, http.StatusNotModified)
	}
}

func TestAPIPageSave_Create(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	body := `{"content":"# New API Page\n\nCreated via API.","message":"API create"}`
	w := apiRequest(t, env, "PUT", "/-/api/v1/pages/apipage", body, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["exists"] != true {
		t.Error("page should exist after creation")
	}

	// Verify in storage
	if !env.Store.Exists("apipage.md") {
		t.Error("page should exist in storage")
	}
}

func TestAPIPageSave_Update(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("updateme.md", "# Original", "init", storage.Author{Name: "test", Email: "test@test.com"})
	meta, _ := env.Store.Metadata("updateme.md", "")

	body := fmt.Sprintf(`{"content":"# Updated via API","message":"API update","revision":"%s"}`, meta.Revision)
	w := apiRequest(t, env, "PUT", "/-/api/v1/pages/updateme", body, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	content, _ := env.Store.Load("updateme.md", "")
	if !strings.Contains(content, "Updated via API") {
		t.Errorf("content = %q, should contain 'Updated via API'", content)
	}
}

func TestAPIPageSave_Conflict(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("conflict.md", "# Original", "init", storage.Author{Name: "test", Email: "test@test.com"})
	meta, _ := env.Store.Metadata("conflict.md", "")
	staleRevision := meta.Revision

	// Another user saves
	env.Store.Store("conflict.md", "# Modified by other", "other edit", storage.Author{Name: "other", Email: "other@test.com"})

	body := fmt.Sprintf(`{"content":"# My conflicting edit","revision":"%s"}`, staleRevision)
	w := apiRequest(t, env, "PUT", "/-/api/v1/pages/conflict", body, nil)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}

	resp := parseAPIResponse(t, w)
	if resp["error"] == nil || resp["error"] == "" {
		t.Error("response should contain error message about conflict")
	}
}

func TestAPIPageSave_InvalidJSON(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiRequest(t, env, "PUT", "/-/api/v1/pages/badpage", "not json", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIPageDelete(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("deletable.md", "# Delete Me", "init", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiRequest(t, env, "DELETE", "/-/api/v1/pages/deletable", "", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if env.Store.Exists("deletable.md") {
		t.Error("page should not exist after delete")
	}
}

func TestAPIPageDelete_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiRequest(t, env, "DELETE", "/-/api/v1/pages/nosuchpage", "", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIPageHistory(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("histapi.md", "# V1", "first commit", storage.Author{Name: "test", Email: "test@test.com"})
	env.Store.Store("histapi.md", "# V2", "second commit", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/pages/histapi/history", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be an array, got %T", resp["data"])
	}
	if len(data) < 2 {
		t.Errorf("expected at least 2 history entries, got %d", len(data))
	}
}

func TestAPIPageHistory_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/pages/nopage/history", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIPageBacklinks(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Create a page that links to another
	env.Store.Store("source.md", "# Source\n\nLinks to [[target]]", "init", storage.Author{Name: "test", Email: "test@test.com"})
	env.Store.Store("target.md", "# Target", "init", storage.Author{Name: "test", Email: "test@test.com"})

	// Index the source page so backlinks are recorded
	env.Server.Wiki.IndexPage(context.Background(), "source", "# Source\n\nLinks to [[target]]")

	w := apiGet(t, env, "/-/api/v1/pages/target/backlinks", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be an array, got %T", resp["data"])
	}
	if len(data) != 1 {
		t.Errorf("expected 1 backlink, got %d", len(data))
	}
}

func TestAPIPageNestedPath(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("docs/getting-started.md", "# Getting Started", "init", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/pages/docs/getting-started", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["path"] != "docs/getting-started" {
		t.Errorf("path = %v, want 'docs/getting-started'", data["path"])
	}
}

// --- Search API Tests ---

func TestAPISearch(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("findme.md", "# Find Me\n\nThis contains the keyword apitarget.", "init", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/search?q=apitarget", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be an array, got %T", resp["data"])
	}
	if len(data) == 0 {
		t.Error("expected at least 1 search result")
	}
}

func TestAPISearch_Empty(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/search?q=", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(data))
	}
}

func TestAPISearch_NoResults(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/search?q=zzzznonexistent99999", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 0 {
		t.Errorf("expected 0 results, got %d", len(data))
	}
}

// --- Changelog API Tests ---

func TestAPIChangelog(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("clpage.md", "# CL", "API changelog test", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/changelog", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be an array, got %T", resp["data"])
	}
	if len(data) == 0 {
		t.Error("expected at least 1 changelog entry")
	}

	// Verify commit structure
	entry := data[0].(map[string]interface{})
	if entry["message"] == nil {
		t.Error("commit entry should have a message field")
	}
	if entry["revision"] == nil {
		t.Error("commit entry should have a revision field")
	}
}

// --- Issue API Tests ---

func TestAPIIssueList(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	createAPITestIssue(t, env, "First API Issue", "desc", "open", "", nil)
	createAPITestIssue(t, env, "Second API Issue", "desc", "closed", "", nil)

	w := apiGet(t, env, "/-/api/v1/issues", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 2 {
		t.Errorf("expected 2 issues, got %d", len(data))
	}
}

func TestAPIIssueList_StatusFilter(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	createAPITestIssue(t, env, "Open Issue", "", "open", "", nil)
	createAPITestIssue(t, env, "Closed Issue", "", "closed", "", nil)

	w := apiGet(t, env, "/-/api/v1/issues?status=open", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 open issue, got %d", len(data))
	}
}

func TestAPIIssueList_CategoryFilter(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	createAPITestIssue(t, env, "Bug", "", "open", "bug", nil)
	createAPITestIssue(t, env, "Feature", "", "open", "feature", nil)

	w := apiGet(t, env, "/-/api/v1/issues?category=bug", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 bug issue, got %d", len(data))
	}
}

func TestAPIIssueList_TagFilter(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	createAPITestIssue(t, env, "Tagged", "", "open", "", []string{"urgent"})
	createAPITestIssue(t, env, "Untagged", "", "open", "", nil)

	w := apiGet(t, env, "/-/api/v1/issues?tag=urgent", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Errorf("expected 1 tagged issue, got %d", len(data))
	}
}

func TestAPIIssueGet(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "View Me", "A description", "open", "bug", []string{"urgent"})

	w := apiGet(t, env, fmt.Sprintf("/-/api/v1/issues/%d", id), nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["title"] != "View Me" {
		t.Errorf("title = %v, want 'View Me'", data["title"])
	}
	if data["status"] != "open" {
		t.Errorf("status = %v, want 'open'", data["status"])
	}
}

func TestAPIIssueGet_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/issues/9999", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIIssueGet_InvalidID(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/issues/abc", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIIssueCreate(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	body := `{"title":"API Created Issue","description":"Created via API","category":"bug","tags":["urgent","help"]}`
	w := apiRequest(t, env, "POST", "/-/api/v1/issues", body, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["title"] != "API Created Issue" {
		t.Errorf("title = %v, want 'API Created Issue'", data["title"])
	}
	if data["status"] != "open" {
		t.Errorf("status = %v, want 'open'", data["status"])
	}

	// Verify in DB
	issues, _ := env.DB.Queries.ListIssues(context.Background())
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue in DB, got %d", len(issues))
	}
}

func TestAPIIssueCreate_EmptyTitle(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	body := `{"title":"","description":"no title"}`
	w := apiRequest(t, env, "POST", "/-/api/v1/issues", body, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIIssueCreate_InvalidJSON(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiRequest(t, env, "POST", "/-/api/v1/issues", "not json", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIIssueUpdate(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "Old Title", "old desc", "open", "", nil)

	body := `{"title":"New Title","description":"new desc","category":"feature","tags":["low"]}`
	w := apiRequest(t, env, "PUT", fmt.Sprintf("/-/api/v1/issues/%d", id), body, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["title"] != "New Title" {
		t.Errorf("title = %v, want 'New Title'", data["title"])
	}
}

func TestAPIIssueUpdate_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	body := `{"title":"Updated"}`
	w := apiRequest(t, env, "PUT", "/-/api/v1/issues/9999", body, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIIssueClose(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "Close Me", "", "open", "", nil)

	w := apiRequest(t, env, "POST", fmt.Sprintf("/-/api/v1/issues/%d/close", id), "", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "closed" {
		t.Errorf("status = %v, want 'closed'", data["status"])
	}

	// Verify in DB
	issue, _ := env.DB.Queries.GetIssue(context.Background(), id)
	if issue.Status != "closed" {
		t.Errorf("DB status = %q, want 'closed'", issue.Status)
	}
}

func TestAPIIssueReopen(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "Reopen Me", "", "closed", "", nil)

	w := apiRequest(t, env, "POST", fmt.Sprintf("/-/api/v1/issues/%d/reopen", id), "", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["status"] != "open" {
		t.Errorf("status = %v, want 'open'", data["status"])
	}
}

func TestAPIIssueDelete_Admin(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "Delete Me", "", "open", "", nil)
	cookies := loginAsAdmin(t, env)

	w := apiRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d", id), "", cookies)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify deleted from DB
	_, err := env.DB.Queries.GetIssue(context.Background(), id)
	if err == nil {
		t.Error("issue should be deleted from DB")
	}
}

func TestAPIIssueDelete_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)

	w := apiRequest(t, env, "DELETE", "/-/api/v1/issues/9999", "", cookies)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// --- Permission enforcement tests ---

func TestAPIPermission_ReadProtected(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.ReadAccess = "REGISTERED"

	// Anonymous request to read-protected endpoint should get JSON 401
	w := apiGet(t, env, "/-/api/v1/pages", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	resp := parseAPIResponse(t, w)
	if resp["error"] != "authentication required" {
		t.Errorf("error = %v, want 'authentication required'", resp["error"])
	}
}

func TestAPIPermission_WriteProtected(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.WriteAccess = "REGISTERED"

	// Anonymous request to write-protected endpoint should get JSON 401
	body := `{"content":"# Test"}`
	w := apiRequest(t, env, "PUT", "/-/api/v1/pages/testpage", body, nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestAPIPermission_AdminProtected(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "Protected", "", "open", "", nil)

	// Non-admin user should get JSON 403
	cookies := loginAsUser(t, env, "regular@example.com")
	w := apiRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d", id), "", cookies)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	resp := parseAPIResponse(t, w)
	if resp["error"] != "insufficient permissions" {
		t.Errorf("error = %v, want 'insufficient permissions'", resp["error"])
	}
}

func TestAPIPermission_AdminProtected_Anonymous(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	id := createAPITestIssue(t, env, "Protected", "", "open", "", nil)

	// Anonymous request to admin-protected endpoint should get JSON 401
	w := apiRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d", id), "", nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// --- Issue Comment API Tests ---

func TestAPIIssueCommentList(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	issueID := createAPITestIssue(t, env, "Commented Issue", "desc", "open", "", nil)

	// Create comments directly via DB
	createTestComment(t, env, issueID, "First comment", "Alice", "alice@test.com")
	createTestComment(t, env, issueID, "Second comment", "Bob", "bob@test.com")

	w := apiGet(t, env, fmt.Sprintf("/-/api/v1/issues/%d/comments", issueID), nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	resp := parseAPIResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("data should be an array, got %T", resp["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 comments, got %d", len(data))
	}

	// Verify ordering (first comment first)
	first := data[0].(map[string]interface{})
	if first["content"] != "First comment" {
		t.Errorf("first comment content = %v, want 'First comment'", first["content"])
	}
}

func TestAPIIssueCommentCreate(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	issueID := createAPITestIssue(t, env, "Comment Target", "desc", "open", "", nil)

	body := `{"content":"A new comment via API"}`
	w := apiRequest(t, env, "POST", fmt.Sprintf("/-/api/v1/issues/%d/comments", issueID), body, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	resp := parseAPIResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["content"] != "A new comment via API" {
		t.Errorf("content = %v, want 'A new comment via API'", data["content"])
	}
	if data["issue_id"] != float64(issueID) {
		t.Errorf("issue_id = %v, want %d", data["issue_id"], issueID)
	}

	// Verify in DB
	comments, _ := env.DB.Queries.ListIssueComments(context.Background(), issueID)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment in DB, got %d", len(comments))
	}
}

func TestAPIIssueCommentCreate_EmptyContent(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	issueID := createAPITestIssue(t, env, "Comment Target", "desc", "open", "", nil)

	body := `{"content":""}`
	w := apiRequest(t, env, "POST", fmt.Sprintf("/-/api/v1/issues/%d/comments", issueID), body, nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPIIssueCommentCreate_IssueNotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	body := `{"content":"orphan comment"}`
	w := apiRequest(t, env, "POST", "/-/api/v1/issues/9999/comments", body, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIIssueCommentDelete_Admin(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	issueID := createAPITestIssue(t, env, "Comment Delete", "desc", "open", "", nil)
	comment := createTestComment(t, env, issueID, "Delete me", "Test", "test@test.com")

	cookies := loginAsAdmin(t, env)

	w := apiRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d/comments/%d", issueID, comment.ID), "", cookies)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify deleted
	comments, _ := env.DB.Queries.ListIssueComments(context.Background(), issueID)
	if len(comments) != 0 {
		t.Errorf("expected 0 comments after delete, got %d", len(comments))
	}
}

func TestAPIIssueCommentDelete_NotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	issueID := createAPITestIssue(t, env, "Comment Target", "desc", "open", "", nil)
	cookies := loginAsAdmin(t, env)

	w := apiRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d/comments/9999", issueID), "", cookies)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestAPIIssueDelete_CascadesComments(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	issueID := createAPITestIssue(t, env, "Cascade Test", "desc", "open", "", nil)

	// Add comments
	createTestComment(t, env, issueID, "Comment 1", "A", "a@test.com")
	createTestComment(t, env, issueID, "Comment 2", "B", "b@test.com")

	cookies := loginAsAdmin(t, env)

	// Delete the issue
	w := apiRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d", issueID), "", cookies)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d\nbody: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify comments are cascade-deleted
	comments, _ := env.DB.Queries.ListIssueComments(context.Background(), issueID)
	if len(comments) != 0 {
		t.Errorf("expected 0 comments after issue delete (cascade), got %d", len(comments))
	}
}

// --- Response format verification ---

func TestAPIResponseFormat_Success(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	env.Store.Store("fmtpage.md", "# Format Test", "init", storage.Author{Name: "test", Email: "test@test.com"})

	w := apiGet(t, env, "/-/api/v1/pages/fmtpage", nil)

	resp := parseAPIResponse(t, w)
	if _, ok := resp["data"]; !ok {
		t.Error("success response should have 'data' key")
	}
	if errMsg, ok := resp["error"]; ok && errMsg != "" {
		t.Error("success response should not have non-empty 'error' key")
	}
}

func TestAPIResponseFormat_Error(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := apiGet(t, env, "/-/api/v1/pages/nonexistent", nil)

	resp := parseAPIResponse(t, w)
	if _, ok := resp["error"]; !ok {
		t.Error("error response should have 'error' key")
	}
	errMsg, _ := resp["error"].(string)
	if errMsg == "" {
		t.Error("error message should not be empty")
	}
}
