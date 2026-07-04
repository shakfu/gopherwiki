package handlers

import (
	"archive/zip"
	"bytes"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/quarto"
	"github.com/sa/gopherwiki/internal/wiki"
)

// markdownZipFormat is the identifier for the pure-Go Markdown ZIP export. It is
// always available (it needs no toolchain) and is handled without the Quarto
// render service.
const markdownZipFormat = "md-zip"

// exportLink is a single entry in the page view's "Export as" menu.
type exportLink struct {
	Format string
	Label  string
}

// exportFormatLinks returns the export options offered for a page view. The
// pure-Go Markdown ZIP is always available; the Quarto-produced formats are
// offered only when the render service (and thus the toolchain) is present.
func (s *Server) exportFormatLinks() []exportLink {
	links := []exportLink{}
	if s.RenderService != nil && s.RenderService.Available() {
		for _, f := range s.RenderService.ExportFormats() {
			links = append(links, exportLink{Format: f.Name, Label: f.Label})
		}
	}
	links = append(links, exportLink{Format: markdownZipFormat, Label: "Markdown (ZIP)"})
	return links
}

// knownExportFormat reports whether name is one of the given Quarto formats.
func knownExportFormat(formats []quarto.ExportFormat, name string) bool {
	for _, f := range formats {
		if f.Name == name {
			return true
		}
	}
	return false
}

// handleExport serves a downloadable export of a page in the format given by the
// `format` query parameter. Markdown ZIP is produced in-process (pure Go);
// every other format is produced by Quarto with execution disabled, so export
// never runs page code. The route is read-protected.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	pagePath := chi.URLParam(r, "path")
	page, err := wiki.NewPage(s.Storage, s.Config, pagePath, "")
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if !page.Exists {
		s.renderNotFound(w, r, page)
		return
	}

	format := r.URL.Query().Get("format")
	if format == "" {
		s.renderError(w, r, http.StatusBadRequest, "No export format specified")
		return
	}

	if format == markdownZipFormat {
		s.exportMarkdownZip(w, r, page)
		return
	}

	if s.RenderService == nil || !s.RenderService.Available() {
		s.renderError(w, r, http.StatusNotImplemented, "Export is not enabled on this instance")
		return
	}

	if !knownExportFormat(s.RenderService.ExportFormats(), format) {
		s.renderError(w, r, http.StatusBadRequest, "Unknown export format: "+format)
		return
	}

	in := quarto.Input{
		Pagepath:       page.Pagepath,
		Source:         page.Content,
		Engine:         pageEngine(page),
		SourceRevision: pageRevision(page),
	}
	data, f, err := s.RenderService.Export(r.Context(), in, format)
	if err != nil {
		slog.Error("page export failed", "page", page.Pagepath, "format", format, "error", err)
		s.renderError(w, r, http.StatusInternalServerError, "Export failed: "+err.Error())
		return
	}

	serveDownload(w, data, f.MediaType, exportFilename(page, f.Ext))
}

// exportMarkdownZip bundles a page's raw source plus any attachments into a ZIP
// archive. It is pure Go and requires no external toolchain, so it works on any
// node regardless of whether Quarto is present.
func (s *Server) exportMarkdownZip(w http.ResponseWriter, r *http.Request, page *wiki.Page) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	srcName := path.Base(page.Filename)
	fw, err := zw.Create(srcName)
	if err == nil {
		_, err = fw.Write([]byte(page.Content))
	}
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Export failed: "+err.Error())
		return
	}

	if attachments, err := page.Attachments(0, ""); err == nil {
		for _, a := range attachments {
			content, err := a.Load()
			if err != nil {
				slog.Debug("skipping unreadable attachment in export", "page", page.Pagepath, "file", a.Filename, "error", err)
				continue
			}
			aw, err := zw.Create("attachments/" + a.Filename)
			if err != nil {
				continue
			}
			aw.Write(content)
		}
	}

	if err := zw.Close(); err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Export failed: "+err.Error())
		return
	}

	serveDownload(w, buf.Bytes(), "application/zip", exportFilename(page, "zip"))
}

// serveDownload writes bytes as a file download with the given media type and
// filename.
func serveDownload(w http.ResponseWriter, data []byte, mediaType, filename string) {
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}

// exportFilename derives a safe download filename from a page path and an
// extension, e.g. ("docs/Report", "pdf") -> "Report.pdf".
func exportFilename(page *wiki.Page, ext string) string {
	base := path.Base(page.Pagepath)
	if base == "." || base == "/" || base == "" {
		base = "page"
	}
	base = strings.Map(func(r rune) rune {
		switch r {
		case '"', '\\', '/', '\n', '\r', 0:
			return '_'
		}
		return r
	}, base)
	return base + "." + ext
}
