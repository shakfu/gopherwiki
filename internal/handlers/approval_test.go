package handlers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

const qmdSource = "---\nengine: jupyter\n---\n# A\n"

// loginWith creates a user with the given options and returns session cookies.
func loginWith(t *testing.T, env *testutil.TestEnv, opts testutil.UserOpts) []*http.Cookie {
	t.Helper()
	user := testutil.CreateTestUser(t, env.DB, opts)
	env.Server.Auth.UpdatePassword(context.Background(), user.ID, "userpassword123")

	form := url.Values{"email": {opts.Email}, "password": {"userpassword123"}}
	req := httptest.NewRequest("POST", "/-/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("login failed: status = %d, want %d", w.Code, http.StatusFound)
	}
	return w.Result().Cookies()
}

func loginAsReviewer(t *testing.T, env *testutil.TestEnv) []*http.Cookie {
	t.Helper()
	return loginWith(t, env, testutil.UserOpts{
		Email: "reviewer@example.com", Approved: true, AllowRead: true, AllowWrite: true, AllowReview: true,
	})
}

// approvalEnv returns an environment that requires approval and holds one
// computational page, "analysis".
func approvalEnv(t *testing.T) (*testutil.TestEnv, *fakeRenderService) {
	t.Helper()
	env := testutil.SetupTestEnv(t)
	env.Server.Config.RenderApproval = true
	fake := &fakeRenderService{available: true}
	env.Server.RenderService = fake
	env.Store.StoreBytes("analysis.qmd", []byte(qmdSource), "init", author)
	return env, fake
}

func sourceHash(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func postForm(env *testutil.TestEnv, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
	req := requestWithCookies("POST", path, strings.NewReader(form.Encode()), cookies)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	return w
}

func TestRenderBlockedUntilApproved(t *testing.T) {
	env, fake := approvalEnv(t)
	cookies := loginAsReviewer(t, env)

	w := postForm(env, "/analysis/render", nil, cookies)
	if w.Code != http.StatusForbidden {
		t.Fatalf("render before approval: status = %d, want 403", w.Code)
	}
	if fake.calls != 0 {
		t.Fatalf("Render calls = %d, want 0 before approval", fake.calls)
	}

	w = postForm(env, "/analysis/approve", url.Values{"hash": {sourceHash(qmdSource)}}, cookies)
	if w.Code != http.StatusFound {
		t.Fatalf("approve: status = %d, want 302; body: %s", w.Code, w.Body.String())
	}

	w = postForm(env, "/analysis/render", nil, cookies)
	if w.Code != http.StatusFound || fake.calls != 1 {
		t.Fatalf("render after approval: status = %d, calls = %d; want 302, 1", w.Code, fake.calls)
	}
}

func TestApprovalLapsesOnEdit(t *testing.T) {
	env, fake := approvalEnv(t)
	cookies := loginAsReviewer(t, env)

	postForm(env, "/analysis/approve", url.Values{"hash": {sourceHash(qmdSource)}}, cookies)
	env.Store.StoreBytes("analysis.qmd", []byte(qmdSource+"\nextra line\n"), "edit", author)

	w := postForm(env, "/analysis/render", nil, cookies)
	if w.Code != http.StatusForbidden || fake.calls != 0 {
		t.Errorf("render after edit: status = %d, calls = %d; want 403, 0", w.Code, fake.calls)
	}
}

func TestApproveRejectsStaleHash(t *testing.T) {
	env, fake := approvalEnv(t)
	cookies := loginAsReviewer(t, env)

	// The reviewer opened the old source; the page changed before they approved.
	stale := sourceHash(qmdSource)
	env.Store.StoreBytes("analysis.qmd", []byte(qmdSource+"\nimport os\n"), "edit", author)

	w := postForm(env, "/analysis/approve", url.Values{"hash": {stale}}, cookies)
	if w.Code != http.StatusConflict {
		t.Fatalf("approve with stale hash: status = %d, want 409", w.Code)
	}

	w = postForm(env, "/analysis/render", nil, cookies)
	if w.Code != http.StatusForbidden || fake.calls != 0 {
		t.Errorf("render: status = %d, calls = %d; want 403, 0", w.Code, fake.calls)
	}
}

func TestApproveRequiresReviewPermission(t *testing.T) {
	env, _ := approvalEnv(t)
	writer := loginWith(t, env, testutil.UserOpts{
		Email: "writer@example.com", Approved: true, AllowRead: true, AllowWrite: true,
	})
	form := url.Values{"hash": {sourceHash(qmdSource)}}

	if w := postForm(env, "/analysis/approve", form, writer); w.Code != http.StatusForbidden {
		t.Errorf("writer: status = %d, want 403", w.Code)
	}

	// Anonymous users may write in the test config, but never review.
	if w := postForm(env, "/analysis/approve", form, nil); w.Code != http.StatusFound {
		t.Errorf("anonymous: status = %d, want 302 to login", w.Code)
	}

	// A token is ignored outside the API, even an admin's.
	token := createAPIToken(t, env, true)
	req := httptest.NewRequest("POST", "/analysis/approve", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Errorf("token: status = %d, want 302 to login", w.Code)
	}

	approved, err := env.DB.Queries.IsCodeApproved(context.Background(), sourceHash(qmdSource))
	if err != nil || approved {
		t.Errorf("IsCodeApproved = %v, %v; want false", approved, err)
	}
}

func TestApproveRejectsPlainPage(t *testing.T) {
	env, _ := approvalEnv(t)
	cookies := loginAsReviewer(t, env)
	env.Store.Store("plain.md", "# Plain", "init", author)

	w := postForm(env, "/plain/approve", url.Values{"hash": {sourceHash("# Plain")}}, cookies)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestSourcePageApprovalControls(t *testing.T) {
	env, _ := approvalEnv(t)
	reviewer := loginAsReviewer(t, env)
	writer := loginWith(t, env, testutil.UserOpts{
		Email: "writer@example.com", Approved: true, AllowRead: true, AllowWrite: true,
	})

	get := func(cookies []*http.Cookie) string {
		w := httptest.NewRecorder()
		env.Router.ServeHTTP(w, requestWithCookies("GET", "/analysis/source", nil, cookies))
		if w.Code != http.StatusOK {
			t.Fatalf("GET source: status = %d, want 200", w.Code)
		}
		return w.Body.String()
	}

	body := get(writer)
	if !strings.Contains(body, "not approved") || strings.Contains(body, "/analysis/approve") || strings.Contains(body, "/analysis/render") {
		t.Error("a writer should see the unapproved notice, without approve or render controls")
	}

	body = get(reviewer)
	if !strings.Contains(body, "/analysis/approve") || !strings.Contains(body, sourceHash(qmdSource)) {
		t.Error("a reviewer should see the approve form carrying the source hash")
	}

	postForm(env, "/analysis/approve", url.Values{"hash": {sourceHash(qmdSource)}}, reviewer)
	body = get(writer)
	if !strings.Contains(body, "/analysis/render") || strings.Contains(body, "not approved") {
		t.Error("after approval a writer should see the render control")
	}
}

func TestAdminUserSave_ReviewPermission(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	cookies := loginAsAdmin(t, env)
	user := testutil.CreateTestUser(t, env.DB, testutil.UserOpts{Email: "rev@example.com", Approved: true})

	form := url.Values{"name": {"Rev"}, "is_approved": {"on"}, "allow_review": {"on"}}
	if w := postForm(env, fmt.Sprintf("/-/admin/users/%d", user.ID), form, cookies); w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}

	saved, err := env.Server.Auth.GetUserByID(context.Background(), user.ID)
	if err != nil || !saved.CanReview() {
		t.Fatalf("CanReview = false after save (err = %v)", err)
	}

	// A password or name change must not drop the permission.
	env.Server.Auth.UpdatePassword(context.Background(), user.ID, "anotherpassword123")
	env.Server.Auth.UpdateUserName(context.Background(), user.ID, "Renamed")
	saved, err = env.Server.Auth.GetUserByID(context.Background(), user.ID)
	if err != nil || !saved.CanReview() {
		t.Errorf("CanReview = false after password and name change (err = %v)", err)
	}
}
