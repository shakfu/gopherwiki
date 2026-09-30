package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/testutil"
)

// createAPIToken creates a user and a token for it with write prefix "kb".
func createAPIToken(t *testing.T, env *testutil.TestEnv, admin bool) string {
	t.Helper()
	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Name:       "Agent",
		Email:      "agent@example.com",
		Admin:      admin,
		Approved:   true,
		AllowRead:  true,
		AllowWrite: true,
	})
	token, err := env.DB.Queries.CreateAPIToken(context.Background(), user.ID, "test", "kb")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return token
}

// tokenRequest performs a JSON API request authenticated by a bearer token.
func tokenRequest(t *testing.T, env *testutil.TestEnv, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	return w
}

func TestAPIToken_WriteInsidePrefix(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)

	for _, path := range []string{"kb", "kb/reports/august"} {
		w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/"+path, `{"content":"# Report"}`, token)
		if w.Code != http.StatusCreated {
			t.Fatalf("PUT %s: status = %d, want 201; body: %s", path, w.Code, w.Body.String())
		}
		data := parseAPIResponse(t, w)["data"].(map[string]interface{})
		meta := data["metadata"].(map[string]interface{})
		if meta["author_email"] != "agent@example.com" {
			t.Errorf("PUT %s: commit author = %v, want the token's user", path, meta["author_email"])
		}
	}
}

func TestAPIToken_WriteOutsidePrefix(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)

	for _, path := range []string{"home", "kbx/page", "kb/../home", "other/kb/page"} {
		w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/"+path, `{"content":"x"}`, token)
		if w.Code != http.StatusForbidden {
			t.Errorf("PUT %s: status = %d, want 403; body: %s", path, w.Code, w.Body.String())
		}
	}
	if env.Store.Exists("home.md") {
		t.Error("a rejected write must not create the page")
	}
}

func TestAPIToken_OverwriteRequiresRevision(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)

	// A human writes the page first.
	human := storage.Author{Name: "Human", Email: "human@example.com"}
	if _, err := env.Store.Store("kb/page.md", "human text", "human edit", human); err != nil {
		t.Fatalf("failed to store page: %v", err)
	}

	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/page", `{"content":"agent text"}`, token)
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("overwrite without revision: status = %d, want 428; body: %s", w.Code, w.Body.String())
	}
	if content, _ := env.Store.Load("kb/page.md", ""); content != "human text" {
		t.Errorf("page content = %q, want the human text unchanged", content)
	}

	w = tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/page", `{"content":"agent text","revision":"stale00"}`, token)
	if w.Code != http.StatusConflict {
		t.Errorf("overwrite with stale revision: status = %d, want 409", w.Code)
	}

	w = tokenRequest(t, env, "GET", "/-/api/v1/pages/kb/page", "", token)
	data := parseAPIResponse(t, w)["data"].(map[string]interface{})
	revision := data["metadata"].(map[string]interface{})["revision"].(string)

	body := fmt.Sprintf(`{"content":"agent text","revision":%q}`, revision)
	w = tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/page", body, token)
	if w.Code != http.StatusOK {
		t.Errorf("overwrite with current revision: status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestAPIToken_CannotDelete(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)

	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/page", `{"content":"x"}`, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup PUT: status = %d", w.Code)
	}

	w = tokenRequest(t, env, "DELETE", "/-/api/v1/pages/kb/page", "", token)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
	if !env.Store.Exists("kb/page.md") {
		t.Error("the page should still exist")
	}
}

func TestAPIToken_NeverAdmin(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, true)
	id := createAPITestIssue(t, env, "Protected", "", "open", "", nil)

	w := tokenRequest(t, env, "DELETE", fmt.Sprintf("/-/api/v1/issues/%d", id), "", token)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestAPIToken_CanOpenIssue(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)

	w := tokenRequest(t, env, "POST", "/-/api/v1/issues", `{"title":"Broken link on Home"}`, token)
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestAPIToken_Invalid(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := tokenRequest(t, env, "GET", "/-/api/v1/pages", "", "gw_wrong")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAdminTokens_CreateAndRevoke(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)
	agent := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{
		Name: "Agent", Email: "agent@example.com", Approved: true, AllowWrite: true,
	})

	form := url.Values{
		"user_id":      {fmt.Sprint(agent.ID)},
		"label":        {"report agent"},
		"write_prefix": {"/KB/"},
	}
	req := requestWithCookies("POST", "/-/admin/tokens", strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create: status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	token := regexp.MustCompile(`gw_[A-Za-z0-9_-]+`).FindString(w.Body.String())
	if token == "" {
		t.Fatal("the response should show the new token once")
	}

	// The prefix is normalized to the lowercase page path.
	w = tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/page", `{"content":"x"}`, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("PUT with new token: status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	// The list shows the label but not the token.
	req = requestWithCookies("GET", "/-/admin/tokens", nil, cookies)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if body := w.Body.String(); !strings.Contains(body, "report agent") || strings.Contains(body, token) {
		t.Error("the list should show the label and must not show the token")
	}

	tokens, err := env.DB.Queries.ListAPITokens(context.Background())
	if err != nil || len(tokens) != 1 {
		t.Fatalf("ListAPITokens = %v, %v; want one token", tokens, err)
	}
	req = requestWithCookies("POST", fmt.Sprintf("/-/admin/tokens/%d/delete", tokens[0].ID), nil, cookies)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("revoke: status = %d, want 302", w.Code)
	}

	w = tokenRequest(t, env, "GET", "/-/api/v1/pages", "", token)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("revoked token: status = %d, want 401", w.Code)
	}
}

func TestAdminTokens_RejectsInvalidPrefix(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)
	agent := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{Name: "Agent", Email: "agent@example.com"})

	for _, prefix := range []string{"", "/", "..", "kb/../other", "../kb"} {
		form := url.Values{
			"user_id":      {fmt.Sprint(agent.ID)},
			"label":        {"label"},
			"write_prefix": {prefix},
		}
		req := requestWithCookies("POST", "/-/admin/tokens", strings.NewReader(form.Encode()), cookies)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		env.Router.ServeHTTP(w, req)
		if w.Code != http.StatusFound {
			t.Errorf("prefix %q: status = %d, want 302", prefix, w.Code)
		}
	}

	tokens, err := env.DB.Queries.ListAPITokens(context.Background())
	if err != nil || len(tokens) != 0 {
		t.Errorf("ListAPITokens = %v, %v; want no tokens", tokens, err)
	}
}

func TestAdminTokens_RequiresAdmin(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsUser(t, env, "regular@example.com")

	req := requestWithCookies("GET", "/-/admin/tokens", nil, cookies)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
