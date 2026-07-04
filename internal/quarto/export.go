package quarto

import (
	"context"
	"fmt"
)

// ExportFormat describes one output format the Quarto pipeline can produce for a
// page. To is the `quarto render --to` token; Ext is the output file extension
// (without a leading dot); MediaType is the HTTP Content-Type to serve it with.
// Binary marks non-text output (PDF/DOCX/EPUB). EmbedResources adds
// --embed-resources so HTML output is a single self-contained file.
type ExportFormat struct {
	Name           string // stable identifier used in URLs (e.g. "pdf")
	Label          string // human label for menus (e.g. "PDF")
	To             string // quarto --to token (e.g. "typst-pdf")
	Ext            string // output file extension, no dot (e.g. "pdf")
	MediaType      string
	Binary         bool
	EmbedResources bool
}

// exportFormats is the ordered registry of Quarto-produced export formats. PDF
// uses the Typst engine (bundled with Quarto, so no LaTeX is required);
// DOCX/EPUB/GFM use the bundled Pandoc; HTML is made self-contained with
// --embed-resources. The pure-Go Markdown ZIP export is handled outside this
// package (it needs no toolchain) and is intentionally not listed here.
var exportFormats = []ExportFormat{
	{Name: "pdf", Label: "PDF", To: "typst-pdf", Ext: "pdf", MediaType: "application/pdf", Binary: true},
	{Name: "html", Label: "HTML", To: "html", Ext: "html", MediaType: "text/html; charset=utf-8", EmbedResources: true},
	{Name: "docx", Label: "Word (DOCX)", To: "docx", Ext: "docx", MediaType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Binary: true},
	{Name: "epub", Label: "EPUB", To: "epub", Ext: "epub", MediaType: "application/epub+zip", Binary: true},
	{Name: "gfm", Label: "Markdown (GFM)", To: "gfm", Ext: "md", MediaType: "text/markdown; charset=utf-8"},
}

// ExportFormats returns the Quarto-produced export formats available on this
// host. Typst and Pandoc ship bundled inside Quarto, so a usable quarto binary
// implies every listed format is producible; an unavailable toolchain yields
// nil. The pure-Go Markdown ZIP export is always available and handled
// separately by the HTTP layer.
func (c Capabilities) ExportFormats() []ExportFormat {
	if !c.Available {
		return nil
	}
	out := make([]ExportFormat, len(exportFormats))
	copy(out, exportFormats)
	return out
}

// lookupFormat finds an export format by its Name.
func lookupFormat(name string) (ExportFormat, bool) {
	for _, f := range exportFormats {
		if f.Name == name {
			return f, true
		}
	}
	return ExportFormat{}, false
}

// ExportFormats returns the formats this service can export a page to.
func (s *Service) ExportFormats() []ExportFormat {
	return s.caps.ExportFormats()
}

// Export renders a page to the named format and returns the output bytes along
// with the resolved format descriptor. Concurrency is bounded by the same
// semaphore as gated renders. Unlike Render, exports are produced on demand and
// never cached. Execution is always disabled (see Renderer.RenderTo): exporting
// must not become a backdoor around the gated-execution rule.
func (s *Service) Export(ctx context.Context, in Input, format string) ([]byte, ExportFormat, error) {
	if !s.caps.Available {
		return nil, ExportFormat{}, ErrUnavailable
	}
	f, ok := lookupFormat(format)
	if !ok {
		return nil, ExportFormat{}, fmt.Errorf("quarto: unknown export format %q", format)
	}

	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return nil, ExportFormat{}, ctx.Err()
	}

	out, err := s.renderer.RenderTo(ctx, in, f)
	if err != nil {
		return nil, ExportFormat{}, err
	}
	return out, f, nil
}
