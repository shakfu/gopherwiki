package handlers_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

func commitCount(t *testing.T, env *testutil.TestEnv) int {
	t.Helper()
	log, _ := env.Store.Log("", 0)
	return len(log)
}

func TestBatchSave_OneCommit(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)
	// The initial commit cannot be reverted, so the batch must not be first.
	env.Store.Store("home.md", "# Home", "init", author)
	before := commitCount(t, env)

	body := `{"message":"August run","pages":[{"path":"kb/august","content":"# August\n"},{"path":"kb/index","content":"[[kb/august]]\n"}]}`
	w := tokenRequest(t, env, "POST", "/-/api/v1/batch", body, token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if data := parseAPIResponse(t, w)["data"].(map[string]interface{}); data["changed"] != true || len(data["pages"].([]interface{})) != 2 {
		t.Errorf("response = %v", data)
	}
	if got := commitCount(t, env); got != before+1 {
		t.Errorf("commits = %d, want %d", got, before+1)
	}
	for _, f := range []string{"kb/august.md", "kb/index.md"} {
		if agent, _ := env.DB.Queries.IsAgentPage(context.Background(), f); !agent {
			t.Errorf("%s should be marked as an agent page", f)
		}
	}
	if w := tokenRequest(t, env, "GET", "/-/api/v1/search?q=August", "", token); w.Code != http.StatusOK {
		t.Errorf("search: status = %d", w.Code)
	}

	// One revert undoes the whole run.
	log, _ := env.Store.Log("", 1)
	w = postForm(env, "/-/commit/"+log[0].Revision+"/revert", url.Values{"message": {"undo"}}, loginAsAdmin(t, env))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/-/changelog" {
		t.Fatalf("revert: status = %d, location %q", w.Code, w.Header().Get("Location"))
	}
	if env.Store.Exists("kb/august.md") || env.Store.Exists("kb/index.md") {
		t.Error("the revert should remove both pages")
	}

	// An unchanged batch makes no commit.
	env.Store.Store("kb/same.md", "same", "init", author)
	before = commitCount(t, env)
	rev, _ := env.Store.Metadata("kb/same.md", "")
	w = tokenRequest(t, env, "POST", "/-/api/v1/batch", `{"pages":[{"path":"kb/same","content":"same","revision":"`+rev.Revision+`"}]}`, token)
	if w.Code != http.StatusOK || parseAPIResponse(t, w)["data"].(map[string]interface{})["changed"] != false {
		t.Errorf("unchanged batch: status = %d, body %s", w.Code, w.Body.String())
	}
	if commitCount(t, env) != before {
		t.Error("an unchanged batch must not commit")
	}
}

func TestBatchSave_Rejections(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	token := createAPIToken(t, env, false)
	env.Store.Store("kb/existing.md", "human", "init", author)
	before := commitCount(t, env)

	cases := []struct {
		name, body string
		want       int
	}{
		{"outside prefix", `{"pages":[{"path":"kb/a","content":"a"},{"path":"home","content":"x"}]}`, http.StatusForbidden},
		{"overwrite without revision", `{"pages":[{"path":"kb/a","content":"a"},{"path":"kb/existing","content":"x"}]}`, http.StatusPreconditionRequired},
		{"stale revision", `{"pages":[{"path":"kb/a","content":"a"},{"path":"kb/existing","content":"x","revision":"stale0"}]}`, http.StatusConflict},
		{"duplicate page", `{"pages":[{"path":"kb/a","content":"a"},{"path":"KB/A","content":"b"}]}`, http.StatusBadRequest},
		{"empty batch", `{"pages":[]}`, http.StatusBadRequest},
		{"missing path", `{"pages":[{"content":"a"}]}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		if w := tokenRequest(t, env, "POST", "/-/api/v1/batch", c.body, token); w.Code != c.want {
			t.Errorf("%s: status = %d, want %d; body: %s", c.name, w.Code, c.want, w.Body.String())
		}
	}
	if commitCount(t, env) != before || env.Store.Exists("kb/a.md") {
		t.Error("a rejected batch must write nothing")
	}
	if content, _ := env.Store.Load("kb/existing.md", ""); content != "human" {
		t.Errorf("kb/existing = %q, want unchanged", content)
	}
}
