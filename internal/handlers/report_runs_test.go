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

// runEnv returns an environment with rendering available and one computational
// page, "analysis".
func runEnv(t *testing.T) (*testutil.TestEnv, *fakeRenderService) {
	t.Helper()
	env := testutil.SetupTestEnv(t)
	fake := &fakeRenderService{available: true}
	env.Server.RenderService = fake
	env.Store.StoreBytes("analysis.qmd", []byte(qmdSource), "init", author)
	return env, fake
}

// runPeriod runs "analysis" for a period and returns the new run's ID.
func runPeriod(t *testing.T, env *testutil.TestEnv, period string) int64 {
	t.Helper()
	w := postForm(env, "/analysis/render", url.Values{"period": {period}}, nil)
	if w.Code != http.StatusFound {
		t.Fatalf("run %s: status = %d, want 302; body: %s", period, w.Code, w.Body.String())
	}
	var id int64
	if _, err := fmt.Sscanf(w.Header().Get("Location"), "/analysis?run=%d", &id); err != nil {
		t.Fatalf("redirect %q does not name a run", w.Header().Get("Location"))
	}
	return id
}

func getPath(env *testutil.TestEnv, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestReportRun_StoresRecord(t *testing.T) {
	env, fake := runEnv(t)

	id := runPeriod(t, env, "2026-08")
	if fake.lastInput.Params["period"] != "2026-08" {
		t.Errorf("period param = %q, want 2026-08", fake.lastInput.Params["period"])
	}

	run, err := env.DB.Queries.GetReportRun(context.Background(), "analysis.qmd", id)
	if err != nil {
		t.Fatalf("GetReportRun: %v", err)
	}
	if run.Period != "2026-08" || run.SourceHash != sourceHash(qmdSource) || run.Markdown != "| 2026-08 | 42 |" || run.SourceRevision == "" {
		t.Errorf("unexpected run: %+v", run)
	}
	if _, ok, _ := fake.Cached(context.Background(), qmdSource, ""); ok {
		t.Error("a run must not go through the render cache")
	}
}

func TestReportRun_RejectsInvalidPeriod(t *testing.T) {
	env, fake := runEnv(t)

	for _, period := range []string{"2026-13", "2026-8", "August", "2026-08; rm -rf /", "2026-08-01"} {
		w := postForm(env, "/analysis/render", url.Values{"period": {period}}, nil)
		if w.Code != http.StatusBadRequest {
			t.Errorf("period %q: status = %d, want 400", period, w.Code)
		}
	}
	if fake.calls != 0 {
		t.Errorf("Run calls = %d, want 0", fake.calls)
	}
}

func TestReportRun_RequiresApproval(t *testing.T) {
	env, fake := approvalEnv(t)

	w := postForm(env, "/analysis/render", url.Values{"period": {"2026-08"}}, nil)
	if w.Code != http.StatusForbidden || fake.calls != 0 {
		t.Errorf("status = %d, calls = %d; want 403, 0", w.Code, fake.calls)
	}
}

func TestReportRun_RerunKeepsOldRecord(t *testing.T) {
	env, _ := runEnv(t)

	first := runPeriod(t, env, "2026-08")
	second := runPeriod(t, env, "2026-08")
	if first == second {
		t.Fatal("a re-run should create a new run")
	}
	runs, err := env.DB.Queries.ListReportRuns(context.Background(), "analysis.qmd")
	if err != nil || len(runs) != 2 || runs[0].ID != second {
		t.Errorf("ListReportRuns = %+v, %v; want both runs, newest first", runs, err)
	}
}

func TestReportRun_PageView(t *testing.T) {
	env, _ := runEnv(t)
	july := runPeriod(t, env, "2026-07")
	august := runPeriod(t, env, "2026-08")

	body := getPath(env, "/analysis").Body.String()
	if !strings.Contains(body, fmt.Sprintf("/analysis/rendered?run=%d", august)) {
		t.Error("the view should embed the newest run")
	}
	if !strings.Contains(body, "2026-07") || !strings.Contains(body, "2026-08") {
		t.Error("the view should list both runs")
	}

	body = getPath(env, fmt.Sprintf("/analysis?run=%d", july)).Body.String()
	if !strings.Contains(body, fmt.Sprintf("/analysis/rendered?run=%d", july)) {
		t.Error("?run= should embed the named run")
	}

	if w := getPath(env, "/analysis?run=999"); w.Code != http.StatusNotFound {
		t.Errorf("unknown run: status = %d, want 404", w.Code)
	}
}

func TestReportRun_ETagChangesWithNewRun(t *testing.T) {
	env, _ := runEnv(t)
	runPeriod(t, env, "2026-07")
	before := getPath(env, "/analysis").Header().Get("ETag")
	runPeriod(t, env, "2026-08")
	after := getPath(env, "/analysis").Header().Get("ETag")
	if before == "" || before == after {
		t.Errorf("ETag before = %q, after = %q; want a change", before, after)
	}
}

func TestReportRun_RenderedServesStoredRun(t *testing.T) {
	env, _ := runEnv(t)
	id := runPeriod(t, env, "2026-08")
	env.Store.Store("other.md", "# Other", "init", author)
	env.Store.StoreBytes("other2.qmd", []byte(qmdSource), "init", author)

	// Records stay readable when rendering is switched off.
	env.Server.RenderService = nil

	w := getPath(env, fmt.Sprintf("/analysis/rendered?run=%d", id))
	if w.Code != http.StatusOK || w.Body.String() != "<html>run 2026-08</html>" {
		t.Fatalf("status = %d, body = %q", w.Code, w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "'unsafe-inline'") {
		t.Error("stored runs should be served under the rendered-output CSP")
	}

	// A run is served only under its own page.
	if w := getPath(env, fmt.Sprintf("/other2/rendered?run=%d", id)); w.Code != http.StatusNotFound {
		t.Errorf("run under another page: status = %d, want 404", w.Code)
	}
}

func TestReportRun_API(t *testing.T) {
	env, _ := runEnv(t)
	july := runPeriod(t, env, "2026-07")
	august := runPeriod(t, env, "2026-08")
	token := createAPIToken(t, env, false)

	w := tokenRequest(t, env, "GET", "/-/api/v1/pages/analysis/runs", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d", w.Code)
	}
	runs := parseAPIResponse(t, w)["data"].([]interface{})
	if len(runs) != 2 {
		t.Fatalf("list returned %d runs, want 2", len(runs))
	}
	newest := runs[0].(map[string]interface{})
	if int64(newest["id"].(float64)) != august || newest["period"] != "2026-08" {
		t.Errorf("newest run = %v", newest)
	}
	if _, ok := newest["markdown"]; ok {
		t.Error("the list should not include markdown")
	}

	w = tokenRequest(t, env, "GET", fmt.Sprintf("/-/api/v1/pages/analysis/runs/%d", july), "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status = %d", w.Code)
	}
	run := parseAPIResponse(t, w)["data"].(map[string]interface{})
	if run["markdown"] != "| 2026-07 | 42 |" || run["source_hash"] != sourceHash(qmdSource) {
		t.Errorf("run = %v", run)
	}

	w = tokenRequest(t, env, "GET", "/-/api/v1/pages/analysis/runs/999", "", token)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown run: status = %d, want 404", w.Code)
	}
}
