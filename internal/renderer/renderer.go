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
				w.WriteString(`<div class="copy-to-clipboard-outer"><div class="copy-to-clipboard-inner"><button class="btn alt-dm btn-xsm copy-to-clipboard" type="button" onclick="gopherwiki.copy_to_clipboard(this);"><i class="fa fa-copy" aria-hidden="true" alt="Copy to clipboard"></i></button></div><pre class="copy-to-clipboard code highlight">`)
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
			&IssueRefExtension{},
			&WikiLinkExtension{},
			&MarkExtension{},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithHardWraps(),
			goldmarkhtml.WithXHTML(),
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

var (
	slugNonAlphanumRegex = regexp.MustCompile(`[^a-z0-9\-]`)
	slugMultiHyphenRegex = regexp.MustCompile(`-+`)
	mermaidBlockRegex    = regexp.MustCompile(`<pre><code class="language-mermaid">([\s\S]*?)</code></pre>`)
	wikiLinkRegex        = regexp.MustCompile(`\[\[([^\]]+)\]\]`)
)

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
	s = slugNonAlphanumRegex.ReplaceAllString(s, "")

	// Remove consecutive hyphens
	s = slugMultiHyphenRegex.ReplaceAllString(s, "-")

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	return s
}

// processMermaidBlocks converts mermaid code blocks to proper format.
func processMermaidBlocks(html string) string {
	// Convert <pre><code class="language-mermaid">...</code></pre>
	// to <pre class="mermaid">...</pre>
	return mermaidBlockRegex.ReplaceAllString(html, `<pre class="mermaid">$1</pre>`)
}

// Slugify is exported for use in templates.
func Slugify(s string) string {
	return slugify(s)
}

// ExtractWikiLinks extracts normalized wikilink targets from markdown content.
// If retainCase is false, targets are lowercased. Issue refs ([[#123]]) are skipped.
func ExtractWikiLinks(content string, retainCase bool) []string {
	matches := wikiLinkRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		inner := m[1]

		// Skip issue refs like [[#123]]
		if len(inner) > 0 && inner[0] == '#' {
			continue
		}

		// Extract target (before pipe if present)
		target := inner
		if idx := strings.Index(inner, "|"); idx >= 0 {
			target = inner[:idx]
		}
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		// Normalize: spaces to hyphens
		target = strings.ReplaceAll(target, " ", "-")

		if !retainCase {
			target = strings.ToLower(target)
		}

		if !seen[target] {
			seen[target] = true
			result = append(result, target)
		}
	}

	return result
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

// IssueRefExtension implements the goldmark extension for [[#123]] issue references.
type IssueRefExtension struct{}

func (e *IssueRefExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(
			util.Prioritized(&issueRefParser{}, 198), // Lower number = higher priority, runs before WikiLink (199)
		),
	)
	m.Renderer().AddOptions(
		renderer.WithNodeRenderers(
			util.Prioritized(&issueRefRenderer{}, 198),
		),
	)
}

// IssueRef AST node for [[#123]] references
var KindIssueRef = ast.NewNodeKind("IssueRef")

type IssueRef struct {
	ast.BaseInline
	IssueID  string
	LinkText string
}

func (n *IssueRef) Kind() ast.NodeKind {
	return KindIssueRef
}

func (n *IssueRef) Dump(source []byte, level int) {
	m := map[string]string{
		"IssueID":  n.IssueID,
		"LinkText": n.LinkText,
	}
	ast.DumpHelper(n, source, level, m, nil)
}

type issueRefParser struct{}

func (p *issueRefParser) Trigger() []byte {
	return []byte{'['}
}

func (p *issueRefParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if len(line) < 5 || line[0] != '[' || line[1] != '[' || line[2] != '#' {
		return nil
	}

	// Find closing ]]
	end := bytes.Index(line[3:], []byte("]]"))
	if end < 0 {
		return nil
	}

	content := string(line[3 : 3+end])

	// Check for pipe separator: [[#123|Custom Text]]
	issueID := content
	linkText := "#" + content
	if idx := strings.Index(content, "|"); idx >= 0 {
		issueID = strings.TrimSpace(content[:idx])
		linkText = strings.TrimSpace(content[idx+1:])
	}

	// Validate that issueID contains only digits
	for _, c := range issueID {
		if c < '0' || c > '9' {
			return nil
		}
	}

	if issueID == "" {
		return nil
	}

	block.Advance(5 + end) // [[ + # + content + ]]

	return &IssueRef{
		IssueID:  issueID,
		LinkText: linkText,
	}
}

type issueRefRenderer struct{}

func (r *issueRefRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindIssueRef, r.renderIssueRef)
}

func (r *issueRefRenderer) renderIssueRef(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}

	ir := n.(*IssueRef)

	// Output: <a href="/-/issues/123" class="issue-ref">#123</a>
	_, _ = w.WriteString(fmt.Sprintf(`<a href="/-/issues/%s" class="issue-ref">%s</a>`, html.EscapeString(ir.IssueID), html.EscapeString(ir.LinkText)))

	return ast.WalkContinue, nil
}
