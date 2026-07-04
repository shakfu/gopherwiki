package quarto

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sa/gopherwiki/internal/rendercache"
)

func TestRenderToBuildsArgsAndDisablesExecution(t *testing.T) {
	fr := &fakeRunner{html: "PDFBYTES"}
	r := &Renderer{caps: availableCaps(), runner: fr, timeout: time.Second}

	pdf, _ := lookupFormat("pdf")
	out, err := r.RenderTo(context.Background(), Input{Source: "---\ntitle: T\n---\n# H\n"}, pdf)
	if err != nil {
		t.Fatalf("RenderTo: %v", err)
	}
	if string(out) != "PDFBYTES" {
		t.Errorf("out = %q", out)
	}

	joined := strings.Join(fr.gotArgs, " ")
	for _, want := range []string{"render", "index.qmd", "--to typst-pdf", "--no-execute", "--output index.pdf"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
	// PDF must not carry the HTML-only self-containment flag.
	if strings.Contains(joined, "--embed-resources") {
		t.Errorf("args %q should not contain --embed-resources for pdf", joined)
	}
}

func TestRenderToHTMLEmbedsResources(t *testing.T) {
	fr := &fakeRunner{html: "x"}
	r := &Renderer{caps: availableCaps(), runner: fr, timeout: time.Second}

	htmlFmt, _ := lookupFormat("html")
	if _, err := r.RenderTo(context.Background(), Input{Source: "s"}, htmlFmt); err != nil {
		t.Fatalf("RenderTo: %v", err)
	}
	joined := strings.Join(fr.gotArgs, " ")
	for _, want := range []string{"--to html", "--no-execute", "--embed-resources", "--output index.html"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestCapabilitiesExportFormats(t *testing.T) {
	if got := (Capabilities{Available: false}).ExportFormats(); got != nil {
		t.Errorf("unavailable caps should export nothing, got %v", got)
	}
	got := availableCaps().ExportFormats()
	if len(got) != len(exportFormats) {
		t.Fatalf("format count = %d, want %d", len(got), len(exportFormats))
	}
	// Returned slice must be a copy: mutating it must not corrupt the registry.
	got[0].Name = "mutated"
	if exportFormats[0].Name == "mutated" {
		t.Error("ExportFormats returned a slice aliasing the package registry")
	}
}

func TestServiceExportAvailableRequiresOptIn(t *testing.T) {
	cache, err := rendercache.Open(":memory:")
	if err != nil {
		t.Fatalf("cache open: %v", err)
	}
	defer cache.Close()

	// Toolchain present but export not enabled -> unavailable.
	off := NewService(availableCaps(), cache, time.Second, 1)
	if off.ExportAvailable() {
		t.Error("ExportAvailable() should be false without WithExport")
	}
	// Explicitly enabled -> available.
	on := NewService(availableCaps(), cache, time.Second, 1, WithExport(true))
	if !on.ExportAvailable() {
		t.Error("ExportAvailable() should be true with WithExport(true)")
	}
	// Enabled but no toolchain -> still unavailable.
	noTool := NewService(Capabilities{Available: false}, cache, time.Second, 1, WithExport(true))
	if noTool.ExportAvailable() {
		t.Error("ExportAvailable() should be false without a toolchain")
	}
}

func TestServiceExportUnknownFormat(t *testing.T) {
	fr := &fakeHTMLRenderer{html: "x"}
	s, _ := newServiceWithFake(t, fr, 1)
	if _, _, err := s.Export(context.Background(), Input{Source: "s"}, "nope"); err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestServiceExportSuccess(t *testing.T) {
	fr := &fakeHTMLRenderer{html: "OUT"}
	s, _ := newServiceWithFake(t, fr, 1)

	out, f, err := s.Export(context.Background(), Input{Pagepath: "p", Source: "s"}, "docx")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if f.Name != "docx" || f.Ext != "docx" {
		t.Errorf("format descriptor = %+v", f)
	}
	if !strings.Contains(string(out), "docx") {
		t.Errorf("out = %q, expected to reflect the docx --to token", out)
	}
}

func TestServiceExportUnavailableWithoutQuarto(t *testing.T) {
	s := NewService(Capabilities{Available: false}, nil, time.Second, 1)
	if _, _, err := s.Export(context.Background(), Input{Source: "s"}, "pdf"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if got := s.ExportFormats(); got != nil {
		t.Errorf("ExportFormats on unavailable service = %v, want nil", got)
	}
}

// --- Real-quarto integration coverage (skipped when quarto is absent). ---

func TestIntegrationRealQuartoExportGFM(t *testing.T) {
	caps := Detect(context.Background(), "")
	if !caps.Available {
		t.Skip("quarto not installed; skipping real export integration test")
	}
	s := NewService(caps, nil, 90*time.Second, 1)
	// A page with a code cell verifies --no-execute: the cell must appear as a
	// literal code block in the output, not as executed output.
	src := "---\ntitle: Export\n---\n\n# Hello\n\n```{python}\nprint('should not run')\n```\n"
	out, f, err := s.Export(context.Background(), Input{Pagepath: "itest", Source: src}, "gfm")
	if err != nil {
		t.Fatalf("real export: %v", err)
	}
	if f.Ext != "md" {
		t.Errorf("gfm ext = %q, want md", f.Ext)
	}
	got := string(out)
	if !strings.Contains(got, "Hello") {
		t.Errorf("export missing heading; got %q", got)
	}
	if !strings.Contains(got, "should not run") {
		t.Errorf("export missing un-executed code cell; --no-execute may have failed; got %q", got)
	}
}

func TestIntegrationRealQuartoExportPDF(t *testing.T) {
	caps := Detect(context.Background(), "")
	if !caps.Available {
		t.Skip("quarto not installed; skipping real export integration test")
	}
	s := NewService(caps, nil, 120*time.Second, 1)
	src := "---\ntitle: Export\n---\n\n# Hello\n\nSome text.\n"
	out, f, err := s.Export(context.Background(), Input{Pagepath: "itest", Source: src}, "pdf")
	if err != nil {
		t.Fatalf("real pdf export: %v", err)
	}
	if f.MediaType != "application/pdf" {
		t.Errorf("media type = %q", f.MediaType)
	}
	if !strings.HasPrefix(string(out), "%PDF-") {
		t.Errorf("output is not a PDF (missing %%PDF- header); got %d bytes", len(out))
	}
}

func TestIntegrationRealQuartoOJS(t *testing.T) {
	caps := Detect(context.Background(), "")
	if !caps.Available {
		t.Skip("quarto not installed; skipping real OJS integration test")
	}
	cache, err := rendercache.Open(":memory:")
	if err != nil {
		t.Fatalf("cache open: %v", err)
	}
	defer cache.Close()
	s := NewService(caps, cache, 120*time.Second, 1)
	// A pure Observable JS page needs no server engine (no Jupyter/knitr): it
	// compiles to client-side JavaScript with the Observable runtime embedded in
	// the self-contained HTML. This is the Tier 2 (client reactive) path.
	src := "---\ntitle: OJS\n---\n\n# Reactive\n\n```{ojs}\nmd`1 + 1 = ${1 + 1}`\n```\n"
	entry, err := s.Render(context.Background(), Input{Pagepath: "ojs", Source: src})
	if err != nil {
		t.Fatalf("real OJS render: %v", err)
	}
	// The runtime must be injected (Quarto's --embed-resources does not inline
	// it), so window._ojs is defined and the cells can actually execute.
	if caps.OJSRuntimePath == "" {
		t.Skip("OJS runtime bundle not found next to quarto; skipping injection assertion")
	}
	if !strings.Contains(string(entry.HTML), ojsRuntimePresentMarker) {
		t.Errorf("rendered OJS page is missing the injected runtime (%q); got %d bytes", ojsRuntimePresentMarker, len(entry.HTML))
	}
	if !strings.Contains(string(entry.HTML), ojsBootstrapMarker) {
		t.Errorf("rendered OJS page is missing the OJS bootstrap")
	}
}
