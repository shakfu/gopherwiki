package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/quarto"
	"github.com/sa/gopherwiki/internal/rendercache"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/testutil"
)

// fakeRenderService implements RenderService for handler tests without invoking
// Quarto.
type fakeRenderService struct {
	available   bool
	calls       int
	lastInput   quarto.Input
	err         error
	cachedEntry rendercache.Entry
	cachedOK    bool
}

func (f *fakeRenderService) Available() bool { return f.available }

func (f *fakeRenderService) Render(ctx context.Context, in quarto.Input) (rendercache.Entry, error) {
	f.calls++
	f.lastInput = in
	if f.err != nil {
		return rendercache.Entry{}, f.err
	}
	return rendercache.Entry{Key: "k", Pagepath: in.Pagepath, HTML: []byte("<html>ok</html>")}, nil
}

func (f *fakeRenderService) Cached(ctx context.Context, source, engine string) (rendercache.Entry, bool, error) {
	return f.cachedEntry, f.cachedOK, nil
}

func (f *fakeRenderService) Invalidate(ctx context.Context, pagepath string) error { return nil }

func (f *fakeRenderService) Export(ctx context.Context, in quarto.Input, format string) ([]byte, quarto.ExportFormat, error) {
	if f.err != nil {
		return nil, quarto.ExportFormat{}, f.err
	}
	return []byte("EXPORT:" + format), quarto.ExportFormat{Name: format, Ext: "bin", MediaType: "application/octet-stream"}, nil
}

func (f *fakeRenderService) ExportFormats() []quarto.ExportFormat {
	return []quarto.ExportFormat{{Name: "pdf", Label: "PDF", Ext: "pdf", MediaType: "application/pdf"}}
}

var author = storage.Author{Name: "test", Email: "test@test.com"}

func TestRenderEndpointDisabledWhenNoService(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	// No RenderService set on the server.

	req := httptest.NewRequest("POST", "/analysis/render", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
}

func TestRenderEndpointDisabledWhenUnavailable(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{available: false}

	req := httptest.NewRequest("POST", "/analysis/render", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
}

func TestRenderEndpointRendersComputationalPage(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	fake := &fakeRenderService{available: true}
	env.Server.RenderService = fake

	req := httptest.NewRequest("POST", "/analysis/render", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusFound, w.Body.String())
	}
	if fake.calls != 1 {
		t.Fatalf("Render calls = %d, want 1", fake.calls)
	}
	if fake.lastInput.Pagepath != "analysis" {
		t.Errorf("Input.Pagepath = %q, want analysis", fake.lastInput.Pagepath)
	}
	if fake.lastInput.Engine != "jupyter" {
		t.Errorf("Input.Engine = %q, want jupyter (from frontmatter)", fake.lastInput.Engine)
	}
	if loc := w.Header().Get("Location"); loc != "/analysis" {
		t.Errorf("redirect Location = %q, want /analysis", loc)
	}
}

func TestRenderEndpointRejectsPlainPage(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("plain.md", "# Plain\n", "init", author)
	fake := &fakeRenderService{available: true}
	env.Server.RenderService = fake

	req := httptest.NewRequest("POST", "/plain/render", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if fake.calls != 0 {
		t.Errorf("Render should not be called for a plain page, calls = %d", fake.calls)
	}
}

func TestRenderEndpointRenderFailure(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{available: true, err: context.DeadlineExceeded}

	req := httptest.NewRequest("POST", "/analysis/render", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestRenderedServesCachedBlob(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{
		available:   true,
		cachedOK:    true,
		cachedEntry: rendercache.Entry{Key: "abc", HTML: []byte("<!DOCTYPE html><html><body>RENDERED</body></html>")},
	}

	req := httptest.NewRequest("GET", "/analysis/rendered", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	// The rendered-output document needs a relaxed CSP (Quarto inlines scripts
	// and uses eval); the app's strict policy never allows unsafe-eval.
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "unsafe-eval") {
		t.Errorf("expected relaxed CSP with unsafe-eval for rendered output, got %q", csp)
	}
	if etag := w.Header().Get("ETag"); etag != `"abc"` {
		t.Errorf("ETag = %q, want \"abc\"", etag)
	}
	if !strings.Contains(w.Body.String(), "RENDERED") {
		t.Errorf("body should contain the cached blob, got %q", w.Body.String())
	}
}

func TestRenderedETag304(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{
		available:   true,
		cachedOK:    true,
		cachedEntry: rendercache.Entry{Key: "abc", HTML: []byte("<html>x</html>")},
	}

	req := httptest.NewRequest("GET", "/analysis/rendered", nil)
	req.Header.Set("If-None-Match", `"abc"`)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", w.Code)
	}
}

func TestRenderedMissReturns404(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{available: true, cachedOK: false}

	req := httptest.NewRequest("GET", "/analysis/rendered", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestRenderedRejectsPlainPage(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("plain.md", "# Plain\n", "init", author)
	env.Server.RenderService = &fakeRenderService{
		available: true, cachedOK: true,
		cachedEntry: rendercache.Entry{Key: "x", HTML: []byte("<html>x</html>")},
	}

	req := httptest.NewRequest("GET", "/plain/rendered", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (plain page has no rendered output)", w.Code)
	}
}

func TestViewComputationalCacheHitShowsIframe(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{
		available: true, cachedOK: true,
		cachedEntry: rendercache.Entry{Key: "abc", HTML: []byte("<html>out</html>")},
	}

	req := httptest.NewRequest("GET", "/analysis", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `class="computational-render"`) {
		t.Errorf("expected iframe embedding, body missing iframe")
	}
	if !strings.Contains(body, `src="/analysis/rendered"`) {
		t.Errorf("iframe should point at the rendered endpoint")
	}
}

func TestViewComputationalCacheMissShowsPlaceholder(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.StoreBytes("analysis.qmd", []byte("---\nengine: jupyter\n---\n# A\n"), "init", author)
	env.Server.RenderService = &fakeRenderService{available: true, cachedOK: false}

	req := httptest.NewRequest("GET", "/analysis", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "computational-pending") {
		t.Errorf("expected render-pending placeholder on cache miss")
	}
	if strings.Contains(body, "computational-render") {
		t.Errorf("should not embed iframe on cache miss")
	}
}
