package handlers_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

// lintFindings returns the API lint findings as "page check detail" strings.
func lintFindings(t *testing.T, env *testutil.TestEnv, token string) map[string]bool {
	t.Helper()
	var w = getPath(env, "/-/api/v1/lint")
	if token != "" {
		w = tokenRequest(t, env, "GET", "/-/api/v1/lint", "", token)
	}
	if w.Code != http.StatusOK {
		t.Fatalf("lint: status = %d; body: %s", w.Code, w.Body.String())
	}
	found := map[string]bool{}
	for _, item := range parseAPIResponse(t, w)["data"].([]interface{}) {
		i := item.(map[string]interface{})
		found[fmt.Sprintf("%s %s %s", i["page"], i["check"], i["detail"])] = true
	}
	return found
}

func expectFinding(t *testing.T, found map[string]bool, want string, present bool) {
	t.Helper()
	if found[want] != present {
		t.Errorf("finding %q present = %v, want %v; findings: %v", want, found[want], present, found)
	}
}

func TestLint_LinksAndOrphans(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	ctx := context.Background()
	env.Server.Wiki.SavePage(ctx, "home", "# Home\n\n[[a]]\n", "", "", author)
	env.Server.Wiki.SavePage(ctx, "a", "# A\n\n[[b]] [[missing]] [[b#section]]\n", "", "", author)
	env.Server.Wiki.SavePage(ctx, "b", "# B\n", "", "", author)
	env.Server.Wiki.SavePage(ctx, "c", "# C\n\n[[c]]\n", "", "", author)

	found := lintFindings(t, env, "")
	expectFinding(t, found, "a broken_link links to missing page missing", true)
	expectFinding(t, found, "c orphan no other page links here", true)
	expectFinding(t, found, "b orphan no other page links here", false)
	expectFinding(t, found, "home orphan no other page links here", false)
	if len(found) != 2 {
		t.Errorf("findings = %v, want exactly two", found)
	}
}

func TestLint_AgentPages(t *testing.T) {
	env, _ := runEnv(t)
	token := createAPIToken(t, env, false)
	august := runPeriod(t, env, "2026-08")

	put := func(path, content string) {
		t.Helper()
		body := fmt.Sprintf(`{"content":%q}`, content)
		if w := tokenRequest(t, env, "PUT", "/-/api/v1/pages/"+path, body, token); w.Code != http.StatusCreated {
			t.Fatalf("PUT %s: status = %d; body: %s", path, w.Code, w.Body.String())
		}
	}
	put("kb/august", fmt.Sprintf("---\nsources:\n  - page: analysis\n    run: %d\n---\nRevenue was 42 in 2026-08, up 12.5%% on July.\n", august))
	put("kb/nosource", "Revenue rose.\n")
	put("kb/missing", "---\nsources:\n  - page: analysis\n    run: 999\n---\nText.\n")

	found := lintFindings(t, env, token)
	expectFinding(t, found, "kb/august unmatched_number not found in any cited run: 12.5", true)
	expectFinding(t, found, "kb/nosource no_sources cites no report run in its frontmatter sources", true)
	expectFinding(t, found, "kb/missing missing_source analysis run 999 does not exist", true)
	for _, page := range []string{"kb/august", "kb/nosource", "kb/missing"} {
		unvalidated := false
		for f := range found {
			if strings.HasPrefix(f, page+" unvalidated ") {
				unvalidated = true
			}
		}
		if !unvalidated {
			t.Errorf("%s should be reported unvalidated", page)
		}
	}

	rerun := runPeriod(t, env, "2026-08")
	found = lintFindings(t, env, token)
	expectFinding(t, found, fmt.Sprintf("kb/august stale_source analysis run %d is superseded by run %d", august, rerun), true)
}

func TestLint_RespectsHiding(t *testing.T) {
	env, token := agentEnv(t)
	env.Server.Config.HideUnvalidated = true

	for f := range lintFindings(t, env, "") {
		if strings.Contains(f, "kb/august") {
			t.Errorf("a hidden page appears in lint for a reader: %q", f)
		}
	}
	agentSees := false
	for f := range lintFindings(t, env, token) {
		if strings.Contains(f, "kb/august unvalidated") {
			agentSees = true
		}
	}
	if !agentSees {
		t.Error("the agent should see lint findings for its own pages")
	}
}
