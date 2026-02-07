package handlers

import (
	"html/template"

	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// NewPageData creates template data for wiki page views.
func NewPageData(page *wiki.Page, htmlContent template.HTML,
	toc []renderer.TOCEntry, libReqs renderer.LibraryRequirements) map[string]interface{} {

	title := page.Pagename
	if len(toc) > 0 {
		title = toc[0].Raw
	}
	if page.Revision != "" {
		title = title + " (" + page.Revision + ")"
	}

	return map[string]interface{}{
		"templateType":         "page",
		"title":                title,
		"pagename":             page.Pagename,
		"pagepath":             page.Pagepath,
		"revision":             page.Revision,
		"htmlcontent":          htmlContent,
		"toc":                  toc,
		"breadcrumbs":          page.Breadcrumbs(),
		"library_requirements": libReqs,
	}
}

// NewEditorData creates template data for the editor.
func NewEditorData(page *wiki.Page, content string, cursorLine, cursorCh int,
	revision string, files []map[string]interface{}) map[string]interface{} {

	return map[string]interface{}{
		"templateType":   "editor",
		"pagename":       page.Pagename,
		"pagepath":       page.Pagepath,
		"content_editor": content,
		"cursor_line":    cursorLine,
		"cursor_ch":      cursorCh,
		"revision":       revision,
		"files":          files,
		"pages":          []string{},
	}
}

// NewGenericData creates template data for generic pages.
// Callers add handler-specific fields to the returned map.
func NewGenericData(title string) map[string]interface{} {
	return map[string]interface{}{
		"templateType": "generic",
		"title":        title,
	}
}

// NewNotFoundData creates template data for a 404 page.
func NewNotFoundData(page *wiki.Page) map[string]interface{} {
	return map[string]interface{}{
		"templateType": "generic",
		"pagename":     page.PagenameFull,
		"pagepath":     page.Pagepath,
	}
}

// NewPageViewData creates standard data for page-scoped views
// (history, source, blame, diff, attachments, etc.).
func NewPageViewData(title string, page *wiki.Page) map[string]interface{} {
	return map[string]interface{}{
		"templateType": "generic",
		"title":        title,
		"pagename":     page.Pagename,
		"pagepath":     page.Pagepath,
		"breadcrumbs":  util.GetBreadcrumbs(page.Pagepath),
	}
}
