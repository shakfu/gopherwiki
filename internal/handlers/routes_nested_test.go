package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

func TestNestedPageActions(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("a/b/c.md", "# C", "init", author)
	env.Store.StoreBytes("a/b/c/chart.png", []byte("\x89PNG"), "attach", author)

	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/a/b/c", http.StatusOK},
		{"GET", "/a/b/c/source", http.StatusOK},
		{"GET", "/a/b/c/history", http.StatusOK},
		{"GET", "/a/b/c/edit", http.StatusOK},
		{"GET", "/a/b/c/attachments", http.StatusOK},
		{"GET", "/a/b/c/chart.png", http.StatusOK},
		{"POST", "/a/b/c/source", http.StatusMethodNotAllowed},
		{"POST", "/a/b/c", http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		env.Router.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Errorf("%s %s: status = %d, want %d", c.method, c.path, w.Code, c.want)
		}
	}
}

func TestNestedPageSave(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	w := postForm(env, "/kb/reports/august/save", map[string][]string{"content": {"# August"}}, nil)
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if !env.Store.Exists("kb/reports/august.md") {
		t.Error("the nested page should be created")
	}
}
