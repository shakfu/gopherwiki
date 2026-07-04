package handlers_test

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/testutil"
)

func TestExportMarkdownZipWorksWithoutRenderService(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("notes.md", "# Notes\n\nbody\n", "init", author)
	// No RenderService: md-zip is pure Go and must still work.

	req := httptest.NewRequest("GET", "/notes/export?format=md-zip", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="notes.zip"` {
		t.Errorf("Content-Disposition = %q", cd)
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	found := false
	for _, f := range zr.File {
		if f.Name == "notes.md" {
			found = true
			rc, _ := f.Open()
			content, _ := io.ReadAll(rc)
			rc.Close()
			if !bytes.Contains(content, []byte("# Notes")) {
				t.Errorf("zipped source = %q, want it to contain the page body", content)
			}
		}
	}
	if !found {
		t.Error("zip did not contain the page source notes.md")
	}
}

func TestExportQuartoFormatServesDownload(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\n", "init", author)
	env.Server.RenderService = &fakeRenderService{exportAvailable: true}

	req := httptest.NewRequest("GET", "/report/export?format=pdf", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="report.bin"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if body := w.Body.String(); body != "EXPORT:pdf" {
		t.Errorf("body = %q, want EXPORT:pdf", body)
	}
}

func TestExportRewritesWikilinksInSource(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\nSee [[Other Page]] and [[#7]].\n", "init", author)
	fake := &fakeRenderService{exportAvailable: true}
	env.Server.RenderService = fake

	req := httptest.NewRequest("GET", "/report/export?format=gfm", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	// Links are absolute against the configured SiteURL, so assert on the
	// label and path portions rather than a fixed base.
	src := fake.lastExportInput.Source
	if !strings.Contains(src, "[Other Page](") || !strings.Contains(src, "/Other-Page)") {
		t.Errorf("wikilink not rewritten in export source: %q", src)
	}
	if !strings.Contains(src, "[#7](") || !strings.Contains(src, "/-/issues/7)") {
		t.Errorf("issue ref not rewritten in export source: %q", src)
	}
	if strings.Contains(src, "[[") {
		t.Errorf("literal [[ remained in export source: %q", src)
	}
}

func TestExportWorksWhenExecutionDisabled(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\n", "init", author)
	// Toolchain present for export, but gated execution is OFF (no cache):
	// export must still succeed. This is the decoupling guarantee.
	env.Server.RenderService = &fakeRenderService{available: false, exportAvailable: true}

	req := httptest.NewRequest("GET", "/report/export?format=pdf", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "EXPORT:pdf" {
		t.Errorf("body = %q, want EXPORT:pdf", body)
	}
}

func TestExportQuartoFormatDisabledWhenExportOff(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\n", "init", author)
	// Toolchain present (execution may be on) but export not opted into.
	env.Server.RenderService = &fakeRenderService{available: true, exportAvailable: false}

	// Quarto format is refused...
	req := httptest.NewRequest("GET", "/report/export?format=pdf", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("pdf status = %d, want %d", w.Code, http.StatusNotImplemented)
	}

	// ...but the pure-Go Markdown ZIP still works.
	req = httptest.NewRequest("GET", "/report/export?format=md-zip", nil)
	w = httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("md-zip status = %d, want 200", w.Code)
	}
}

func TestExportQuartoFormatDisabledWithoutService(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\n", "init", author)
	// No RenderService: a Quarto-only format is unavailable.

	req := httptest.NewRequest("GET", "/report/export?format=pdf", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotImplemented)
	}
}

func TestExportUnknownFormatIsBadRequest(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\n", "init", author)
	env.Server.RenderService = &fakeRenderService{exportAvailable: true}

	req := httptest.NewRequest("GET", "/report/export?format=bogus", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestExportMissingFormatIsBadRequest(t *testing.T) {
	env := testutil.SetupTestEnv(t)
	env.Store.Store("report.md", "# Report\n", "init", author)

	req := httptest.NewRequest("GET", "/report/export", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestExportMissingPageIsNotFound(t *testing.T) {
	env := testutil.SetupTestEnv(t)

	req := httptest.NewRequest("GET", "/ghost/export?format=md-zip", nil)
	w := httptest.NewRecorder()
	env.Router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
