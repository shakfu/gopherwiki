package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

// agentEnv returns an environment in which the agent has written kb/august,
// which links to Home, and a human has written Home.
func agentEnv(t *testing.T) (*testutil.TestEnv, string) {
	t.Helper()
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)
	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/august", `{"content":"# August\n\nRevenue rose zebra. See [[Home]].\n"}`, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent write: status = %d; body: %s", w.Code, w.Body.String())
	}
	env.Server.Wiki.SavePage(context.Background(), "Home", "# Home\n", "", "", author)
	return env, token
}

// currentRevision returns the full revision of a page.
func currentRevision(t *testing.T, env *testutil.TestEnv, filename string) string {
	t.Helper()
	meta, err := env.Store.Metadata(filename, "")
	if err != nil {
		t.Fatalf("metadata %s: %v", filename, err)
	}
	return meta.RevisionFull
}

func getWith(env *testutil.TestEnv, path string, cookies []*http.Cookie) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, requestWithCookies("GET", path, nil, cookies))
	return w
}

func TestValidation_BannerAndValidate(t *testing.T) {
	env, _ := agentEnv(t)
	reviewer := loginAsReviewer(t, env)

	body := getPath(env, "/kb/august").Body.String()
	if !strings.Contains(body, "has not been validated") || strings.Contains(body, "/kb/august/validate") {
		t.Error("a reader should see the unvalidated banner without the validate form")
	}
	if strings.Contains(getPath(env, "/Home").Body.String(), "Written by the agent") {
		t.Error("a human page should have no agent banner")
	}

	revision := currentRevision(t, env, "kb/august.md")
	w := getWith(env, "/kb/august", reviewer)
	if !strings.Contains(w.Body.String(), revision) {
		t.Fatal("a reviewer should see the validate form carrying the revision")
	}
	etag := w.Header().Get("ETag")

	w = postForm(env, "/kb/august/validate", url.Values{"revision": {revision}}, reviewer)
	if w.Code != http.StatusFound {
		t.Fatalf("validate: status = %d, want 302; body: %s", w.Code, w.Body.String())
	}
	w = getPath(env, "/kb/august")
	if !strings.Contains(w.Body.String(), "validated by reviewer@example.com") {
		t.Error("the view should name the validator")
	}
	if w.Header().Get("ETag") == etag {
		t.Error("the ETag should change when the revision is validated")
	}

	// Any later commit leaves the page unvalidated again, even a human one.
	env.Server.Wiki.SavePage(context.Background(), "kb/august", "# August\n\nEdited.\n", "", "", author)
	if !strings.Contains(getPath(env, "/kb/august").Body.String(), "has not been validated") {
		t.Error("a new commit should void the validation")
	}
}

func TestValidation_Rejections(t *testing.T) {
	env, token := agentEnv(t)
	reviewer := loginAsReviewer(t, env)
	writer := loginWith(t, env, testutil.UserOpts{Email: "writer@example.com", Approved: true, AllowRead: true, AllowWrite: true})
	revision := currentRevision(t, env, "kb/august.md")
	form := url.Values{"revision": {revision}}

	if w := postForm(env, "/kb/august/validate", url.Values{"revision": {"stale"}}, reviewer); w.Code != http.StatusConflict {
		t.Errorf("stale revision: status = %d, want 409", w.Code)
	}
	if w := postForm(env, "/kb/august/validate", form, writer); w.Code != http.StatusForbidden {
		t.Errorf("writer: status = %d, want 403", w.Code)
	}
	if w := postForm(env, "/Home/validate", url.Values{"revision": {currentRevision(t, env, "home.md")}}, reviewer); w.Code != http.StatusBadRequest {
		t.Errorf("human page: status = %d, want 400", w.Code)
	}

	req := httptest.NewRequest("POST", "/kb/august/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound || !strings.HasPrefix(w.Header().Get("Location"), "/-/login") {
		t.Errorf("token: status = %d, want a redirect to login", w.Code)
	}

	if !strings.Contains(getPath(env, "/kb/august").Body.String(), "has not been validated") {
		t.Error("no rejected request may validate the page")
	}
}

func TestValidation_MarkSurvivesTokenRevocation(t *testing.T) {
	env, _ := agentEnv(t)
	tokens, _ := env.DB.Queries.ListAPITokens(context.Background())
	for _, tok := range tokens {
		env.DB.Queries.DeleteAPIToken(context.Background(), tok.ID)
	}
	if !strings.Contains(getPath(env, "/kb/august").Body.String(), "has not been validated") {
		t.Error("revoking the token must not end the need for validation")
	}
}

func TestHideUnvalidated(t *testing.T) {
	env, token := agentEnv(t)
	env.Server.Config.HideUnvalidated = true
	env.Store.StoreBytes("kb/august/chart.png", []byte("\x89PNG"), "attach", author)
	reviewer := loginAsReviewer(t, env)

	for _, path := range []string{"/kb/august", "/kb/august/source", "/kb/august/history", "/kb/august/chart.png"} {
		if w := getPath(env, path); w.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, w.Code)
		}
	}
	if w := postForm(env, "/kb/august/save", url.Values{"content": {"x"}}, nil); w.Code != http.StatusNotFound {
		t.Errorf("POST save: status = %d, want 404", w.Code)
	}

	agentCommit := currentRevision(t, env, "kb/august.md")
	listings := map[string]string{
		"/-/search?q=zebra":              "kb/august",
		"/-/search/dropdown?q=zebra":     "kb/august",
		"/-/pageindex":                   "kb/august",
		"/-/sitemap.xml":                 "kb/august",
		"/Home":                          "kb/august", // backlinks and sidebar
		"/-/changelog":                   agentCommit[:6],
		"/-/feed.rss":                    agentCommit[:6],
		"/-/feed.atom":                   agentCommit[:6],
		"/-/api/v1/pages":                "kb/august",
		"/-/api/v1/search?q=zebra":       "kb/august",
		"/-/api/v1/changelog":            agentCommit[:6],
		"/-/api/v1/pages/home/backlinks": "kb/august",
	}
	// Control: without hiding, every listing shows the page or its commit.
	env.Server.Config.HideUnvalidated = false
	for path, secret := range listings {
		if body := getPath(env, path).Body.String(); !strings.Contains(body, secret) {
			t.Errorf("control: GET %s should show %s", path, secret)
		}
	}
	env.Server.Config.HideUnvalidated = true
	for path, secret := range listings {
		if body := getPath(env, path).Body.String(); strings.Contains(body, secret) {
			t.Errorf("GET %s exposes %s", path, secret)
		}
	}
	if w := getPath(env, "/-/commit/"+agentCommit); w.Code != http.StatusNotFound {
		t.Errorf("commit view: status = %d, want 404", w.Code)
	}
	if w := getPath(env, "/-/api/v1/pages/kb/august"); w.Code != http.StatusNotFound {
		t.Errorf("API page: status = %d, want 404", w.Code)
	}

	// Reviewers and the agent still see it.
	if w := getWith(env, "/kb/august", reviewer); w.Code != http.StatusOK {
		t.Errorf("reviewer: status = %d, want 200", w.Code)
	}
	if !strings.Contains(getWith(env, "/-/search?q=zebra", reviewer).Body.String(), "kb/august") {
		t.Error("a reviewer's search should include the page")
	}
	if w := tokenRequest(t, env, "GET", "/-/api/v1/pages/kb/august", "", token); w.Code != http.StatusOK {
		t.Errorf("token: status = %d, want 200", w.Code)
	}

	// Validation publishes it.
	postForm(env, "/kb/august/validate", url.Values{"revision": {agentCommit}}, reviewer)
	if w := getPath(env, "/kb/august"); w.Code != http.StatusOK {
		t.Errorf("after validation: status = %d, want 200", w.Code)
	}
	if !strings.Contains(getPath(env, "/-/pageindex").Body.String(), "kb/august") {
		t.Error("after validation the page index should list the page")
	}
}

func TestHideUnvalidated_TreeKeepsVisibleSubpages(t *testing.T) {
	env, token := agentEnv(t)
	env.Server.Config.HideUnvalidated = true
	env.Server.Wiki.SavePage(context.Background(), "kb", "# KB index\n", "", "", author)
	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb", fmt.Sprintf(`{"content":"# KB\n","revision":%q}`, currentRevision(t, env, "kb.md")[:6]), token)
	if w.Code != http.StatusOK {
		t.Fatalf("agent update of kb: status = %d; body: %s", w.Code, w.Body.String())
	}
	env.Server.Wiki.SavePage(context.Background(), "kb/public", "# Public\n", "", "", author)
	env.Server.Wiki.InvalidatePageTreeCache()

	body := getPath(env, "/Home").Body.String()
	if !strings.Contains(body, `href="/kb/public"`) {
		t.Error("a visible subpage of a hidden page should stay in the sidebar")
	}
	if strings.Contains(body, `href="/kb"`) {
		t.Error("the hidden page itself should not be linked from the sidebar")
	}
}

// TestHideUnvalidated_Diff checks that the diff of a visible page leaves out a
// hidden agent page changed between the same revisions. The diff used to
// cover the whole tree.
func TestHideUnvalidated_Diff(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.HideUnvalidated = true
	token := createAPIToken(t, env, false)
	env.Server.Wiki.SavePage(context.Background(), "Home", "# Home\n", "", "", author)
	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/august", `{"content":"# August\n\nRevenue rose zebra.\n"}`, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent write: status = %d; body: %s", w.Code, w.Body.String())
	}
	env.Server.Wiki.SavePage(context.Background(), "Home", "# Home\n\nEdited.\n", "", "", author)

	w = getWith(env, "/home/diff?rev_a=HEAD~2&rev_b=HEAD", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("diff: status = %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Edited.") || strings.Contains(body, "zebra") {
		t.Errorf("diff of home must show home's change and not the hidden page; body contains Edited=%v zebra=%v",
			strings.Contains(body, "Edited."), strings.Contains(body, "zebra"))
	}
}

// TestHideUnvalidated_RunsSegment checks that a hidden page whose path holds a
// "/runs/" segment stays hidden in the API. The hidden check used to run on
// the path before that segment.
func TestHideUnvalidated_RunsSegment(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Server.Config.HideUnvalidated = true
	token := createAPIToken(t, env, false)
	w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/kb/runs/notes", `{"content":"# Notes\n\nsecret zebra\n"}`, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("agent write: status = %d; body: %s", w.Code, w.Body.String())
	}

	for _, path := range []string{"kb/runs/notes", "kb/runs/notes/history", "kb/runs/notes/backlinks"} {
		if w := getWith(env, "/-/api/v1/pages/"+path, nil); w.Code != http.StatusNotFound {
			t.Errorf("anonymous GET %s: status = %d, want 404; body: %s", path, w.Code, w.Body.String())
		}
	}
	if w := tokenRequest(t, env, "GET", "/-/api/v1/pages/kb/runs/notes", "", token); w.Code != http.StatusOK {
		t.Errorf("token GET kb/runs/notes: status = %d, want 200", w.Code)
	}
}
