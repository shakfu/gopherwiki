package handlers

import (
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// LoadTemplates loads templates from the given filesystem.
// Each page template is parsed separately with base.html to avoid conflicts.
func (s *Server) LoadTemplates(fsys fs.FS) error {
	funcMap := s.templateFuncs()

	slog.Info("loading templates")

	// Read shared template files
	baseContent, err := fs.ReadFile(fsys, "base.html")
	if err != nil {
		return fmt.Errorf("failed to read base.html: %w", err)
	}
	editorContent, err := fs.ReadFile(fsys, "editor.html")
	if err != nil {
		return fmt.Errorf("failed to read editor.html: %w", err)
	}
	pageContent, err := fs.ReadFile(fsys, "page.html")
	if err != nil {
		return fmt.Errorf("failed to read page.html: %w", err)
	}

	// Find all template files
	entries, err := fs.Glob(fsys, "*.html")
	if err != nil {
		return fmt.Errorf("failed to glob templates: %w", err)
	}

	// Create a map to store each page's template set
	s.TemplateMap = make(map[string]*template.Template)

	slog.Debug("loaded templates")
	for _, entry := range entries {
		name := path.Base(entry)
		// Skip shared templates
		if name == "base.html" || name == "editor.html" || name == "page.html" {
			continue
		}

		// Create a new template set for each page
		tmpl := template.New("base").Funcs(funcMap)

		// Parse base.html
		tmpl, err = tmpl.Parse(string(baseContent))
		if err != nil {
			return fmt.Errorf("failed to parse base.html for %s: %w", name, err)
		}

		// Parse editor.html (for editor_* defines)
		tmpl, err = tmpl.Parse(string(editorContent))
		if err != nil {
			return fmt.Errorf("failed to parse editor.html for %s: %w", name, err)
		}

		// Parse page.html (for page_* defines)
		tmpl, err = tmpl.Parse(string(pageContent))
		if err != nil {
			return fmt.Errorf("failed to parse page.html for %s: %w", name, err)
		}

		// Parse the specific page template (will override generic_content if defined)
		specificContent, err := fs.ReadFile(fsys, entry)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", name, err)
		}
		tmpl, err = tmpl.Parse(string(specificContent))
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", name, err)
		}

		s.TemplateMap[name] = tmpl
		slog.Debug("loaded template", "name", name)
	}

	// Also add editor.html and page.html to the map
	// They need themselves + base + an empty generic_content
	emptyGeneric := `{{define "generic_content"}}{{end}}`
	for _, shared := range []struct {
		name    string
		content []byte
	}{
		{"editor.html", editorContent},
		{"page.html", pageContent},
	} {
		tmpl := template.New("base").Funcs(funcMap)
		for _, content := range []string{string(baseContent), string(editorContent), string(pageContent), emptyGeneric} {
			var err error
			tmpl, err = tmpl.Parse(content)
			if err != nil {
				return fmt.Errorf("failed to parse shared template %s: %w", shared.name, err)
			}
		}
		s.TemplateMap[shared.name] = tmpl
		slog.Debug("loaded template", "name", shared.name)
	}

	return nil
}

// TemplateFuncsForTest exposes templateFuncs for external test packages.
func (s *Server) TemplateFuncsForTest() template.FuncMap {
	return s.templateFuncs()
}

// templateFuncs returns the template function map.
func (s *Server) templateFuncs() template.FuncMap {
	return template.FuncMap{
		"debugUnixtime": func(p string) string {
			if s.Config.Debug {
				return p + "?" + time.Now().Format("20060102150405")
			}
			if s.Version != "" {
				return p + "?" + s.Version
			}
			return p
		},
		"pluralize": util.Pluralize,
		"urlquote":  util.URLQuote,
		"formatDatetime": func(t time.Time, format string) string {
			return util.FormatDatetime(t, format)
		},
		"slugify": func(s string) string {
			return util.Slugify(s, true)
		},
		"safe": func(s string) template.HTML {
			return template.HTML(s)
		},
		"safeHTML": func(s string) template.HTML {
			return template.HTML(s)
		},
		"trimPrefix": strings.TrimPrefix,
		"urlFor": URLFor,
		"hasPermission": func(perm string, perms map[string]bool) bool {
			if perms == nil {
				return false
			}
			return perms[perm]
		},
		"renderPageTree": func(nodes []*wiki.PageTreeNode) template.HTML {
			if len(nodes) == 0 {
				return ""
			}
			var renderNodes func([]*wiki.PageTreeNode, int) string
			renderNodes = func(nodes []*wiki.PageTreeNode, depth int) string {
				var b strings.Builder
				for _, n := range nodes {
					if len(n.Children) > 0 {
						b.WriteString("<details")
						if depth == 0 {
							b.WriteString(" open")
						}
						b.WriteString("><summary>")
						if n.IsPage {
							b.WriteString(`<a href="/`)
							b.WriteString(template.HTMLEscapeString(n.Path))
							b.WriteString(`" class="sidebar-link">`)
							b.WriteString(template.HTMLEscapeString(n.Name))
							b.WriteString("</a>")
						} else {
							b.WriteString(template.HTMLEscapeString(n.Name))
						}
						b.WriteString("</summary>")
						b.WriteString(renderNodes(n.Children, depth+1))
						b.WriteString("</details>")
					} else if n.IsPage {
						b.WriteString(`<a href="/`)
						b.WriteString(template.HTMLEscapeString(n.Path))
						b.WriteString(`" class="sidebar-link" style="padding-left: `)
						b.WriteString(fmt.Sprintf("%d", (depth+1)*12))
						b.WriteString(`px;">`)
						b.WriteString(template.HTMLEscapeString(n.Name))
						b.WriteString("</a>")
					}
				}
				return b.String()
			}
			return template.HTML(renderNodes(nodes, 0))
		},
	}
}
