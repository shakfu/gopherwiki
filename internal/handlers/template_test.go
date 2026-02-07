package handlers_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/testutil"
)

// TestURLForRouteSync verifies that every urlFor route name produces a URL
// that matches at least one registered chi route. This catches drift between
// the urlFor function in template.go and the route definitions in routes.go.
func TestURLForRouteSync(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	// Collect all registered route patterns
	type routeEntry struct {
		method  string
		pattern string
	}
	var registered []routeEntry
	err := chi.Walk(env.Router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		registered = append(registered, routeEntry{method: method, pattern: route})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk failed: %v", err)
	}

	// Get the urlFor function from the exported test helper
	funcMap := env.Server.TemplateFuncsForTest()
	urlForFn := funcMap["urlFor"].(func(string, ...string) string)

	// Route names and their test parameters.
	// For parameterized routes we supply the param name and a dummy value.
	type testCase struct {
		name   string
		args   []string
		method string // expected HTTP method, default GET
	}

	cases := []testCase{
		// Static routes
		{name: "index"},
		{name: "login"},
		{name: "logout"},
		{name: "register"},
		{name: "settings"},
		{name: "search"},
		{name: "changelog"},
		{name: "about"},
		{name: "pageindex"},
		{name: "issues"},
		{name: "issue_new"},

		// Parameterized routes
		{name: "view", args: []string{"path", "testpage"}},
		{name: "edit", args: []string{"path", "testpage"}},
		{name: "save", args: []string{"path", "testpage"}, method: "POST"},
		{name: "history", args: []string{"path", "testpage"}},
		{name: "blame", args: []string{"path", "testpage"}},
		{name: "diff", args: []string{"path", "testpage"}},
		{name: "source", args: []string{"path", "testpage"}},
		{name: "create", args: []string{"path", "testpage"}},
		{name: "attachments", args: []string{"pagepath", "testpage"}},
		{name: "static", args: []string{"filename", "style.css"}},
		{name: "commit", args: []string{"revision", "abc123"}},
		{name: "revert", args: []string{"revision", "abc123"}},
		{name: "issue", args: []string{"id", "1"}},
		{name: "issue_edit", args: []string{"id", "1"}},
		{name: "issue_close", args: []string{"id", "1"}, method: "POST"},
		{name: "issue_reopen", args: []string{"id", "1"}, method: "POST"},
		{name: "issue_delete", args: []string{"id", "1"}, method: "POST"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := urlForFn(tc.name, tc.args...)
			if url == "" || url == "/" && tc.name != "index" && tc.name != "view" {
				// "/" is valid for index and view-without-args, but suspicious for others
				if tc.name != "index" {
					// For view with args, the URL should not be "/"
					if len(tc.args) > 0 {
						t.Errorf("urlFor(%q, %v) returned %q, expected a real path", tc.name, tc.args, url)
						return
					}
				}
			}

			method := "GET"
			if tc.method != "" {
				method = tc.method
			}

			// Use chi's route matching to verify the URL resolves
			rctx := chi.NewRouteContext()
			ok := env.Router.Match(rctx, method, url)
			if !ok {
				t.Errorf("urlFor(%q, %v) = %q does not match any registered %s route", tc.name, tc.args, url, method)
				t.Logf("Registered routes:")
				for _, r := range registered {
					t.Logf("  %s %s", r.method, r.pattern)
				}
			}
		})
	}
}
