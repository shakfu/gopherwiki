package renderer

import (
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
)

func TestRewriteWikiLinks(t *testing.T) {
	in := "See [[old-page]], [[Old Page|the page]], [[other]] and [[#123]]."
	out, changed := RewriteWikiLinks(in, "old-page", "new-page", false)
	if !changed {
		t.Fatal("expected changed=true")
	}
	want := "See [[new-page]], [[new-page|the page]], [[other]] and [[#123]]."
	if out != want {
		t.Errorf("got %q, want %q", out, want)
	}

	// No matching link -> unchanged.
	if _, changed := RewriteWikiLinks("just [[unrelated]] here", "old-page", "new-page", false); changed {
		t.Error("expected changed=false when nothing matches")
	}
}

func TestRenderCached(t *testing.T) {
	r := New(config.Default())

	// A cache hit returns the stored result and ignores new source for the
	// same key (proving the cached value is served, not re-rendered).
	first, _, _ := r.RenderCached("page@rev1", "# Original", "/p")
	second, _, _ := r.RenderCached("page@rev1", "# Completely Different", "/p")
	if first != second {
		t.Errorf("cache hit should return original render; got %q then %q", first, second)
	}
	if !strings.Contains(first, "Original") {
		t.Errorf("expected rendered Original heading, got %q", first)
	}

	// A different key renders the new content.
	other, _, _ := r.RenderCached("page@rev2", "# Completely Different", "/p")
	if !strings.Contains(other, "Different") {
		t.Errorf("new key should render new content, got %q", other)
	}

	// An empty key bypasses the cache entirely.
	a, _, _ := r.RenderCached("", "# AAA", "/p")
	b, _, _ := r.RenderCached("", "# BBB", "/p")
	if a == b {
		t.Error("empty key must render fresh each call")
	}
}

func TestRenderInlineMath(t *testing.T) {
	r := New(config.Default())

	html, _, req := r.Render(`Pythagoras: \(a^2 + b^2 = c^2\)`, "/test")
	if !strings.Contains(html, `\(a^2 + b^2 = c^2\)`) {
		t.Errorf("inline math delimiters not preserved, got: %s", html)
	}
	if !req.RequiresMathJax {
		t.Error("RequiresMathJax should be true when inline math is present")
	}

	// Single-line display delimiters \[...\] are also preserved.
	html, _, _ = r.Render(`Area \[\pi r^2\]`, "/test")
	if !strings.Contains(html, `\[\pi r^2\]`) {
		t.Errorf("display math delimiters not preserved, got: %s", html)
	}
}

func TestRenderMathBlock(t *testing.T) {
	r := New(config.Default())

	html, _, req := r.Render("```math\nE = mc^2\n```", "/test")
	if !req.RequiresMathJax {
		t.Error("RequiresMathJax should be true for a ```math block")
	}
	if !strings.Contains(html, `<div class="math-display">`) || !strings.Contains(html, `\[`) {
		t.Errorf("math block not converted to display math, got: %s", html)
	}
	if strings.Contains(html, `language-math`) {
		t.Errorf("math block should not remain a code block, got: %s", html)
	}
}

func TestRenderBackslashEscapeStillWorks(t *testing.T) {
	r := New(config.Default())

	// A non-math backslash escape must still behave as a normal escape and not
	// be swallowed by the math parser.
	html, _, req := r.Render(`\*not bold\*`, "/test")
	if strings.Contains(html, "<em>") || strings.Contains(html, "<strong>") {
		t.Errorf("escaped asterisks should not produce emphasis, got: %s", html)
	}
	if req.RequiresMathJax {
		t.Error("plain escaped text should not require MathJax")
	}
}

func TestRenderBasicMarkdown(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "heading",
			input:    "# Hello World",
			contains: []string{"<h1", "Hello World", "</h1>"},
		},
		{
			name:     "paragraph",
			input:    "This is a paragraph.",
			contains: []string{"<p>", "This is a paragraph.", "</p>"},
		},
		{
			name:     "bold",
			input:    "This is **bold** text.",
			contains: []string{"<strong>", "bold", "</strong>"},
		},
		{
			name:     "italic",
			input:    "This is *italic* text.",
			contains: []string{"<em>", "italic", "</em>"},
		},
		{
			name:     "link",
			input:    "[Example](https://example.com)",
			contains: []string{`<a href="https://example.com"`, "Example", "</a>"},
		},
		{
			name:     "code inline",
			input:    "Use `code` here.",
			contains: []string{"<code>", "code", "</code>"},
		},
		{
			name:     "unordered list",
			input:    "- Item 1\n- Item 2",
			contains: []string{"<ul>", "<li>", "Item 1", "Item 2", "</li>", "</ul>"},
		},
		{
			name:     "ordered list",
			input:    "1. First\n2. Second",
			contains: []string{"<ol>", "<li>", "First", "Second", "</li>", "</ol>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, _, _ := r.Render(tt.input, "/test")
			for _, want := range tt.contains {
				if !strings.Contains(html, want) {
					t.Errorf("Render(%q) should contain %q, got:\n%s", tt.input, want, html)
				}
			}
		})
	}
}

func TestRenderWikiLinks(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	tests := []struct {
		name     string
		input    string
		contains []string
	}{
		{
			name:     "simple wikilink",
			input:    "Link to [[Page Name]]",
			contains: []string{`<a href="/Page-Name"`, "Page Name", "</a>"},
		},
		{
			name:     "wikilink with custom text",
			input:    "Link to [[Target|Custom Text]]",
			contains: []string{`<a href="/Target"`, "Custom Text", "</a>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, _, _ := r.Render(tt.input, "/test")
			for _, want := range tt.contains {
				if !strings.Contains(html, want) {
					t.Errorf("Render(%q) should contain %q, got:\n%s", tt.input, want, html)
				}
			}
		})
	}
}

func TestRenderCodeBlocks(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	input := "```go\nfunc main() {}\n```"
	html, _, _ := r.Render(input, "/test")

	// Should have syntax highlighting wrapper
	if !strings.Contains(html, "copy-to-clipboard") {
		t.Errorf("Code block should have copy-to-clipboard wrapper")
	}

	// Should contain the code
	if !strings.Contains(html, "func") && !strings.Contains(html, "main") {
		t.Errorf("Code block should contain the code")
	}
}

func TestRenderMermaid(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	input := "```mermaid\ngraph TD;\n    A-->B;\n```"
	html, _, reqs := r.Render(input, "/test")

	if !reqs.RequiresMermaid {
		t.Error("Mermaid code block should set RequiresMermaid=true")
	}

	if !strings.Contains(html, "mermaid") {
		t.Errorf("Mermaid block should have mermaid class")
	}
}

func TestRenderTOC(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	input := "# Heading 1\n## Heading 2\n### Heading 3"
	_, toc, _ := r.Render(input, "/test")

	if len(toc) != 3 {
		t.Errorf("TOC should have 3 entries, got %d", len(toc))
	}

	if toc[0].Level != 1 || toc[0].Text != "Heading 1" {
		t.Errorf("First TOC entry should be level 1 'Heading 1', got level %d '%s'", toc[0].Level, toc[0].Text)
	}

	if toc[1].Level != 2 || toc[1].Text != "Heading 2" {
		t.Errorf("Second TOC entry should be level 2 'Heading 2', got level %d '%s'", toc[1].Level, toc[1].Text)
	}
}

func TestRenderTable(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	input := `| Header 1 | Header 2 |
|----------|----------|
| Cell 1   | Cell 2   |`

	html, _, _ := r.Render(input, "/test")

	contains := []string{"<table>", "<thead>", "<tbody>", "<th>", "<td>", "Header 1", "Cell 1"}
	for _, want := range contains {
		if !strings.Contains(html, want) {
			t.Errorf("Table should contain %q, got:\n%s", want, html)
		}
	}
}

func TestRenderTaskList(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	input := `- [ ] Unchecked
- [x] Checked`

	html, _, _ := r.Render(input, "/test")

	if !strings.Contains(html, `type="checkbox"`) {
		t.Errorf("Task list should contain checkboxes")
	}
}

func TestRenderFootnotes(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	input := `Here is a footnote[^1].

[^1]: This is the footnote content.`

	html, _, _ := r.Render(input, "/test")

	if !strings.Contains(html, "footnote") {
		t.Errorf("Should render footnotes, got:\n%s", html)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Hello World", "hello-world"},
		{"Test 123", "test-123"},
		{"Special!@#Characters", "specialcharacters"},
		{"Multiple   Spaces", "multiple-spaces"},
		{"  Trim Spaces  ", "trim-spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Slugify(tt.input)
			if got != tt.want {
				t.Errorf("Slugify(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRenderRawHTMLStripped(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	tests := []struct {
		name        string
		input       string
		notContains []string
	}{
		{
			name:        "script tag",
			input:       `<script>alert(1)</script>`,
			notContains: []string{"<script>", "</script>"},
		},
		{
			name:        "inline script in markdown",
			input:       "Hello <script>alert('xss')</script> world",
			notContains: []string{"<script>", "</script>"},
		},
		{
			name:        "iframe tag",
			input:       `<iframe src="https://evil.com"></iframe>`,
			notContains: []string{"<iframe"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, _, _ := r.Render(tt.input, "/test")
			for _, notWant := range tt.notContains {
				if strings.Contains(html, notWant) {
					t.Errorf("Render(%q) should NOT contain %q (XSS), got:\n%s", tt.input, notWant, html)
				}
			}
		})
	}
}

func TestExtractWikiLinks(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		retainCase bool
		want       []string
	}{
		{
			name:    "simple link",
			content: "See [[Page Name]] for details.",
			want:    []string{"page-name"},
		},
		{
			name:    "pipe syntax extracts target only",
			content: "See [[Target|Display Text]] here.",
			want:    []string{"target"},
		},
		{
			name:    "deduplication",
			content: "Link to [[Foo]] and again [[Foo]].",
			want:    []string{"foo"},
		},
		{
			name:    "issue ref excluded",
			content: "See [[#123]] and [[Page]].",
			want:    []string{"page"},
		},
		{
			name:    "retain case",
			content: "See [[CamelCase]] link.",
			retainCase: true,
			want:    []string{"CamelCase"},
		},
		{
			name:    "lowercase by default",
			content: "See [[CamelCase]] link.",
			want:    []string{"camelcase"},
		},
		{
			name:    "no links",
			content: "No links here.",
			want:    nil,
		},
		{
			name:    "multiple distinct links",
			content: "[[Alpha]] and [[Beta]] and [[Gamma]]",
			want:    []string{"alpha", "beta", "gamma"},
		},
		{
			name:    "spaces become hyphens",
			content: "[[Hello World]]",
			want:    []string{"hello-world"},
		},
		{
			name:    "issue ref with custom text excluded",
			content: "[[#456|bug report]]",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractWikiLinks(tt.content, tt.retainCase)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractWikiLinks() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractWikiLinks()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRenderIssueRefs(t *testing.T) {
	cfg := config.Default()
	r := New(cfg)

	tests := []struct {
		name     string
		input    string
		contains []string
		notContains []string
	}{
		{
			name:     "simple issue ref",
			input:    "See issue [[#123]]",
			contains: []string{`<a href="/-/issues/123"`, `class="issue-ref"`, "#123", "</a>"},
		},
		{
			name:     "issue ref with custom text",
			input:    "See [[#456|the bug report]]",
			contains: []string{`<a href="/-/issues/456"`, `class="issue-ref"`, "the bug report", "</a>"},
		},
		{
			name:     "multiple issue refs",
			input:    "See [[#1]] and [[#2]]",
			contains: []string{`href="/-/issues/1"`, `href="/-/issues/2"`},
		},
		{
			name:     "issue ref does not match wikilink",
			input:    "See [[PageName]]",
			contains: []string{`<a href="/PageName"`, "PageName"},
			notContains: []string{"issue-ref"},
		},
		{
			name:     "issue ref with invalid id is not parsed",
			input:    "See [[#abc]]",
			notContains: []string{"issue-ref", `href="/-/issues/`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			html, _, _ := r.Render(tt.input, "/test")
			for _, want := range tt.contains {
				if !strings.Contains(html, want) {
					t.Errorf("Render(%q) should contain %q, got:\n%s", tt.input, want, html)
				}
			}
			for _, notWant := range tt.notContains {
				if strings.Contains(html, notWant) {
					t.Errorf("Render(%q) should NOT contain %q, got:\n%s", tt.input, notWant, html)
				}
			}
		})
	}
}
