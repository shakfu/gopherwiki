package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

func TestAPIPageSave_CreatesQmd(t *testing.T) {
	env, fake := approvalEnv(t)
	token := createAPIToken(t, env, false)

	body := `{"content":"---\ntitle: August\n---\n# August\n"}`
	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/august.qmd", body, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
	if !env.Store.Exists("kb/august.qmd") || env.Store.Exists("kb/august.md") {
		t.Fatal("the page should be stored as kb/august.qmd")
	}

	// Later requests address the page without the suffix.
	w = tokenRequest(t, env, "GET", "/-/api/v1/pages/kb/august", "", token)
	if w.Code != http.StatusOK {
		t.Errorf("GET without suffix: status = %d, want 200", w.Code)
	}

	// The agent's proposal cannot render until a reviewer approves it.
	w = postForm(env, "/kb/august/render", nil, loginAsReviewer(t, env))
	if w.Code != http.StatusForbidden || fake.calls != 0 {
		t.Errorf("render: status = %d, calls = %d; want 403, 0", w.Code, fake.calls)
	}
}

func TestEditorCreatesQmd(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, httptest.NewRequest("GET", "/analysis.qmd/edit", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET edit: status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `action="/analysis.qmd/save"`) {
		t.Fatal("the editor should save a new computational page under its .qmd path")
	}

	w = postForm(env, "/analysis.qmd/save", url.Values{"content": {"# Analysis\n"}}, nil)
	if w.Code != http.StatusFound {
		t.Fatalf("save: status = %d, want 302", w.Code)
	}
	if !env.Store.Exists("analysis.qmd") {
		t.Error("the page should be stored as analysis.qmd")
	}

	// Editing the existing page saves to its plain path.
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, httptest.NewRequest("GET", "/analysis/edit", nil))
	if !strings.Contains(w.Body.String(), `action="/analysis/save"`) {
		t.Error("an existing page should save under its page path")
	}
}
