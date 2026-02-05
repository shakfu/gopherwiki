// Package renderer provides markdown rendering for GopherWiki.
package renderer

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	highlighting "github.com/yuin/goldmark-highlighting/v2"

	"github.com/sa/gopherwiki/internal/config"
)

// TOCEntry represents an entry in the table of contents.
type TOCEntry struct {
	Index  int
	Text   string
	Level  int
	Raw    string
	Anchor string
}

// LibraryRequirements tracks which JS libraries are needed.
type LibraryRequirements struct {
	RequiresMermaid bool
	RequiresMathJax bool
}

// Renderer handles markdown to HTML conversion.
type Renderer struct {
	config   *config.Config
	markdown goldmark.Markdown
}

// New creates a new Renderer with the given configuration.
func New(cfg *config.Config) *Renderer {
	// Set up syntax highlighting with Chroma
	highlightOpts := []highlighting.Option{
		highlighting.WithStyle("github"),
		highlighting.WithFormatOptions(
			chromahtml.WithClasses(true),
			chromahtml.WithLineNumbers(false),
		),
		// Custom code block wrapper for copy-to-clipboard
		highlighting.WithWrapperRenderer(func(w util.BufWriter, context highlighting.CodeBlockContext, entering bool) {
			lang, _ := context.Language()
			langStr := string(lang)

			if entering {
				// Skip wrapper for mermaid blocks - they need special handling
				if langStr == "mermaid" {
					w.WriteString(`<pre><code class="language-mermaid">`)
					return
				}
				w.WriteString(`<div class="copy-to-clipboard-outer"><div class="copy-to-clipboard-inner"><button class="btn alt-dm btn-xsm copy-to-clipboard" type="button" onclick="otterwiki.copy_to_clipboard(this);"><i class="fa fa-copy" aria-hidden="true" alt="Copy to clipboard"></i></button></div><pre class="copy-to-clipboard code highlight">`)
				if langStr != "" {
					w.WriteString(fmt.Sprintf(`<code class="language-%s">`, langStr))
				} else {
					w.WriteString(`<code>`)
				}
			} else {
				if langStr == "mermaid" {
					w.WriteString(`</code></pre>`)
					return
				}
				w.WriteString(`</code></pre></div>`)
			}
		}),
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Typographer,
			extension.Footnote,
			highlighting.NewHighlighting(highlightOpts...),
			&WikiLinkExtension{},
			&TaskListExtension{},
			&AlertExtension{},
			&MarkExtension{},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithHardWraps(),
			goldmarkhtml.WithXHTML(),
			goldmarkhtml.WithUnsafe(),
		),
	)

	return &Renderer{
		config:   cfg,
		markdown: md,
	}
}

// Ensure chroma and styles are used (for CSS generation)
var _ = chroma.Coalesce
var _ = styles.Get

// Render converts markdown to HTML with TOC extraction.
func (r *Renderer) Render(source string, pageURL string) (string, []TOCEntry, LibraryRequirements) {
	requirements := LibraryRequirements{}

	// Ensure trailing newline
	if len(source) == 0 || source[len(source)-1] != '\n' {
		source += "\n"
	}

	sourceBytes := []byte(source)

	// Parse the document
	ctx := parser.NewContext()
	doc := r.markdown.Parser().Parse(text.NewReader(sourceBytes), parser.WithContext(ctx))

	// Extract TOC from headings
	toc := extractTOC(doc, sourceBytes)

	// Check for mermaid and math blocks
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if cb, ok := n.(*ast.FencedCodeBlock); ok {
			lang := string(cb.Language(sourceBytes))
			if lang == "mermaid" {
				requirements.RequiresMermaid = true
			} else if lang == "math" {
				requirements.RequiresMathJax = true
			}
		}
		return ast.WalkContinue, nil
	})

	// Render to HTML
	var buf bytes.Buffer
	if err := r.markdown.Renderer().Render(&buf, sourceBytes, doc); err != nil {
		return html.EscapeString(source), nil, requirements
	}

	htmlContent := buf.String()

	// Post-process for mermaid blocks
	htmlContent = processMermaidBlocks(htmlContent)

	// Post-process for code blocks (add copy button wrapper)
	htmlContent = processCodeBlocks(htmlContent)

	return htmlContent, toc, requirements
}

// extractTOC extracts the table of contents from headings.
func extractTOC(doc ast.Node, source []byte) []TOCEntry {
	var toc []TOCEntry
	anchors := make(map[string]int)
	index := 0

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}

		if heading, ok := n.(*ast.Heading); ok {
			// Extract text content
			var textBuf bytes.Buffer
			for c := heading.FirstChild(); c != nil; c = c.NextSibling() {
				if t, ok := c.(*ast.Text); ok {
					textBuf.Write(t.Segment.Value(source))
				}
			}
			text := textBuf.String()
			raw := text

			// Generate anchor
			anchor := slugify(text)
			if count, exists := anchors[anchor]; exists {
				anchors[anchor] = count + 1
				anchor = fmt.Sprintf("%s-%d", anchor, count+1)
			} else {
				anchors[anchor] = 0
			}

			toc = append(toc, TOCEntry{
				Index:  index,
				Text:   text,
				Level:  heading.Level,
				Raw:    raw,
				Anchor: anchor,
			})
			index++
		}

		return ast.WalkContinue, nil
	})

	return toc
}

// slugify converts text to a URL-friendly anchor.
func slugify(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")

	// Remove non-alphanumeric characters except hyphens
	reg := regexp.MustCompile(`[^a-z0-9\-]`)
	s = reg.ReplaceAllString(s, "")

	// Remove consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	s = reg.ReplaceAllString(s, "-")

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	return s
}

// processMermaidBlocks converts mermaid code blocks to proper format.
func processMermaidBlocks(html string) string {
	// Convert <pre><code class="language-mermaid">...</code></pre>
	// to <pre class="mermaid">...</pre>
	re := regexp.MustCompile(`<pre><code class="language-mermaid">([\s\S]*?)</code></pre>`)
	return re.ReplaceAllString(html, `<pre class="mermaid">$1</pre>`)
}

// processCodeBlocks wraps code blocks with copy button structure.
// Note: With highlighting extension, this is now handled by the WrapperRenderer
func processCodeBlocks(htmlContent string) string {
	// No-op since highlighting extension handles wrapping
	return htmlContent
}

// Slugify is exported for use in templates.
func Slugify(s string) string {
	return slugify(s)
}

// WikiLinkExtension implements the goldmark extension for [[wikilinks]].
type WikiLinkExtension struct{}

func (e *WikiLinkExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&wikiLinkParser{}, 199),
		),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&wikiLinkRenderer{}, 199),
		),
	)
}

// WikiLink AST node
var KindWikiLink = ast.NewNodeKind("WikiLink")

type WikiLink struct {
	ast.BaseInline
	Target    string
	LinkText  string
}

func (n *WikiLink) Kind() ast.NodeKind {
	return KindWikiLink
}

func (n *WikiLink) Dump(source []byte, level int) {
	m := map[string]string{
		"Target":   n.Target,
		"LinkText": n.LinkText,
	}
	ast.DumpHelper(n, source, level, m, nil)
}

type wikiLinkParser struct{}

func (p *wikiLinkParser) Trigger() []byte {
	return []byte{'['}
}

func (p *wikiLinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 4 || line[0] != '[' || line[1] != '[' {
		return nil
	}

	// Find closing ]]
	end := bytes.Index(line[2:], []byte("]]"))
	if end < 0 {
		return nil
	}

	content := string(line[2 : 2+end])

	// Check for pipe separator
	target := content
	text := content
	if idx := strings.Index(content, "|"); idx >= 0 {
		target = strings.TrimSpace(content[:idx])
		text = strings.TrimSpace(content[idx+1:])
	}

	block.Advance(4 + end) // [[ + content + ]]

	return &WikiLink{
		Target:   target,
		LinkText: text,
	}
}

type wikiLinkRenderer struct{}

func (r *wikiLinkRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindWikiLink, r.renderWikiLink)
}

func (r *wikiLinkRenderer) renderWikiLink(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	wl := n.(*WikiLink)

	// Convert target to URL path
	target := strings.ReplaceAll(wl.Target, " ", "-")
	target = "/" + target

	_, _ = w.WriteString(fmt.Sprintf(`<a href="%s">%s</a>`, html.EscapeString(target), html.EscapeString(wl.LinkText)))

	return ast.WalkContinue, nil
}

// TaskListExtension adds task list support to goldmark.
type TaskListExtension struct{}

func (e *TaskListExtension) Extend(m goldmark.Markdown) {
	// GFM extension already includes task lists
}

// AlertExtension adds GitHub-style alerts (> [!NOTE], [!WARNING], etc.)
type AlertExtension struct{}

func (e *AlertExtension) Extend(m goldmark.Markdown) {
	// Alert processing is done in post-processing via regex
	// This is a placeholder for future AST-based implementation
}

// MarkExtension adds ==highlight== syntax support
type MarkExtension struct{}

func (e *MarkExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&markParser{}, 200),
		),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&markRenderer{}, 200),
		),
	)
}

// Mark AST node for ==highlighted text==
var KindMark = ast.NewNodeKind("Mark")

type Mark struct {
	ast.BaseInline
}

func (n *Mark) Kind() ast.NodeKind {
	return KindMark
}

func (n *Mark) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type markParser struct{}

func (p *markParser) Trigger() []byte {
	return []byte{'='}
}

func (p *markParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) < 4 || line[0] != '=' || line[1] != '=' {
		return nil
	}

	// Find closing ==
	end := bytes.Index(line[2:], []byte("=="))
	if end < 0 || end == 0 {
		return nil
	}

	mark := &Mark{}

	// Create text segment for content between == ==
	contentStart := segment.Start + 2
	contentEnd := segment.Start + 2 + end
	mark.AppendChild(mark, ast.NewTextSegment(text.NewSegment(contentStart, contentEnd)))

	block.Advance(4 + end) // Skip == + content + ==

	return mark
}

type markRenderer struct{}

func (r *markRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindMark, r.renderMark)
}

func (r *markRenderer) renderMark(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		w.WriteString("<mark>")
	} else {
		w.WriteString("</mark>")
	}
	return ast.WalkContinue, nil
}

// processAlerts converts GitHub-style alerts to styled divs
func processAlerts(htmlContent string) string {
	// Pattern: <blockquote>\n<p>[!TYPE]</p> or <blockquote>\n<p>[!TYPE]\nContent</p>
	alertTypes := map[string]string{
		"NOTE":      "info",
		"TIP":       "success",
		"IMPORTANT": "primary",
		"WARNING":   "warning",
		"CAUTION":   "danger",
	}

	for alertType, alertClass := range alertTypes {
		// Match blockquotes starting with [!TYPE]
		pattern := fmt.Sprintf(`<blockquote>\s*<p>\[!%s\]`, alertType)
		re := regexp.MustCompile(pattern)

		htmlContent = re.ReplaceAllStringFunc(htmlContent, func(match string) string {
			return fmt.Sprintf(`<div class="alert alert-%s" role="alert"><strong>%s:</strong> `, alertClass, strings.Title(strings.ToLower(alertType)))
		})
	}

	// Close the alert divs (replace </blockquote> after an alert start)
	// This is a simplified approach - a proper AST-based solution would be more robust
	return htmlContent
}
