package handlers_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

// storeAnalysis writes an agent analysis page citing the given runs of
// "analysis".
func storeAnalysis(t *testing.T, env *testutil.TestEnv, runs ...int64) {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\ntitle: August analysis\nsources:\n")
	for _, id := range runs {
		fmt.Fprintf(&b, "  - page: analysis\n    run: %d\n", id)
	}
	b.WriteString("---\nRevenue rose.\n")
	if _, err := env.Store.Store("kb/august.md", b.String(), "analysis", author); err != nil {
		t.Fatalf("store analysis: %v", err)
	}
}

func TestProvenance_ViewListsCitedRuns(t *testing.T) {
	env, _ := runEnv(t)
	july := runPeriod(t, env, "2026-07")
	august := runPeriod(t, env, "2026-08")
	storeAnalysis(t, env, july, august, 999)

	body := getPath(env, "/kb/august").Body.String()
	for _, want := range []string{
		fmt.Sprintf(`href="/analysis?run=%d"`, july),
		"analysis, 2026-08 (run " + fmt.Sprint(august) + ")",
		"run 999. <strong>Not found.</strong>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("view missing %q", want)
		}
	}
	if strings.Contains(body, "Out of date") {
		t.Error("no cited run is out of date")
	}
	if strings.Contains(body, "sources:") {
		t.Error("the frontmatter should not be rendered as body text")
	}
}

func TestProvenance_RerunMarksOutOfDate(t *testing.T) {
	env, _ := runEnv(t)
	august := runPeriod(t, env, "2026-08")
	storeAnalysis(t, env, august)
	before := getPath(env, "/kb/august").Header().Get("ETag")

	// Another period does not affect the citation.
	runPeriod(t, env, "2026-09")
	if body := getPath(env, "/kb/august").Body.String(); strings.Contains(body, "Out of date") {
		t.Error("a run of another period must not mark the citation out of date")
	}

	rerun := runPeriod(t, env, "2026-08")
	w := getPath(env, "/kb/august")
	if !strings.Contains(w.Body.String(), fmt.Sprintf(`<strong>Out of date:</strong> <a href="/analysis?run=%d">`, rerun)) {
		t.Error("a re-run of the cited period should mark the citation out of date")
	}
	if w.Header().Get("ETag") == before {
		t.Error("the ETag should change when a cited run goes out of date")
	}
}

func TestProvenance_CitedPathIsSanitized(t *testing.T) {
	env, _ := runEnv(t)
	env.Store.Store("kb/x.md", "---\nsources:\n  - page: //evil.example\n    run: 1\n---\nText\n", "x", author)

	body := getPath(env, "/kb/x").Body.String()
	if strings.Contains(body, "//evil.example") {
		t.Error("a cited path must not render as a protocol-relative link")
	}
}

func TestProvenance_API(t *testing.T) {
	env, _ := runEnv(t)
	august := runPeriod(t, env, "2026-08")
	storeAnalysis(t, env, august)
	rerun := runPeriod(t, env, "2026-08")
	token := createAPIToken(t, env, false)

	w := tokenRequest(t, env, "GET", "/-/api/v1/pages/kb/august", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	sources := parseAPIResponse(t, w)["data"].(map[string]interface{})["sources"].([]interface{})
	if len(sources) != 1 {
		t.Fatalf("sources = %v, want one", sources)
	}
	src := sources[0].(map[string]interface{})
	if src["page"] != "analysis" || src["period"] != "2026-08" || int64(src["newer_run"].(float64)) != rerun {
		t.Errorf("source = %v", src)
	}
}
