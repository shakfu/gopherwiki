package renderer

import (
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
)

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
