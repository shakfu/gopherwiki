package renderer

import "testing"

func TestPrepareExportSourceWikilinks(t *testing.T) {
	cases := []struct {
		name    string
		content string
		baseURL string
		want    string
	}{
		{
			name:    "plain wikilink with site url",
			content: "See [[Page Name]] here.",
			baseURL: "https://wiki.example.com",
			want:    "See [Page Name](https://wiki.example.com/Page-Name) here.",
		},
		{
			name:    "labeled wikilink",
			content: "See [[Target Page|Custom Text]].",
			baseURL: "https://w.io",
			want:    "See [Custom Text](https://w.io/Target-Page).",
		},
		{
			name:    "issue reference",
			content: "Fixes [[#123]] now.",
			baseURL: "https://w.io",
			want:    "Fixes [#123](https://w.io/-/issues/123) now.",
		},
		{
			name:    "labeled issue reference",
			content: "See [[#456|the bug]].",
			baseURL: "https://w.io",
			want:    "See [the bug](https://w.io/-/issues/456).",
		},
		{
			name:    "empty base url yields relative link",
			content: "[[Home]]",
			baseURL: "",
			want:    "[Home](/Home)",
		},
		{
			name:    "trailing slash on base url is trimmed",
			content: "[[Home]]",
			baseURL: "https://w.io/",
			want:    "[Home](https://w.io/Home)",
		},
		{
			name:    "multiple links on one line",
			content: "[[A]] and [[B]] and [[C]]",
			baseURL: "",
			want:    "[A](/A) and [B](/B) and [C](/C)",
		},
		{
			name:    "inline code span is untouched",
			content: "Use `[[NotALink]]` verbatim but [[Real]] converts.",
			baseURL: "",
			want:    "Use `[[NotALink]]` verbatim but [Real](/Real) converts.",
		},
		{
			name:    "no wikilinks returns input unchanged",
			content: "Just some **markdown** text.",
			baseURL: "https://w.io",
			want:    "Just some **markdown** text.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PrepareExportSource(tc.content, tc.baseURL, false)
			if got != tc.want {
				t.Errorf("PrepareExportSource(, false)\n got  = %q\n want = %q", got, tc.want)
			}
		})
	}
}

func TestWikilinksToMarkdownSkipsFencedCode(t *testing.T) {
	content := "Intro [[One]].\n" +
		"```\n" +
		"code with [[Two]] literal\n" +
		"```\n" +
		"Outro [[Three]].\n"
	want := "Intro [One](/One).\n" +
		"```\n" +
		"code with [[Two]] literal\n" +
		"```\n" +
		"Outro [Three](/Three).\n"
	if got := PrepareExportSource(content, "", false); got != want {
		t.Errorf("fenced code not preserved\n got  = %q\n want = %q", got, want)
	}
}

func TestWikilinksToMarkdownSkipsFrontmatter(t *testing.T) {
	content := "---\n" +
		"title: A [[Bracketed]] Thing\n" +
		"---\n" +
		"Body [[Link]].\n"
	want := "---\n" +
		"title: A [[Bracketed]] Thing\n" +
		"---\n" +
		"Body [Link](/Link).\n"
	if got := PrepareExportSource(content, "", false); got != want {
		t.Errorf("frontmatter not preserved\n got  = %q\n want = %q", got, want)
	}
}

func TestWikilinksToMarkdownTildeFence(t *testing.T) {
	content := "~~~\n[[Nope]]\n~~~\n[[Yes]]"
	want := "~~~\n[[Nope]]\n~~~\n[Yes](/Yes)"
	if got := PrepareExportSource(content, "", false); got != want {
		t.Errorf("tilde fence not preserved\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceMermaidFence(t *testing.T) {
	content := "Before.\n" +
		"```mermaid\n" +
		"graph LR\n" +
		"  A --> B\n" +
		"```\n" +
		"After.\n"
	want := "Before.\n" +
		"```{mermaid}\n" +
		"graph LR\n" +
		"  A --> B\n" +
		"```\n" +
		"After.\n"
	if got := PrepareExportSource(content, "", true); got != want {
		t.Errorf("mermaid fence not converted\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceMermaidPreservesIndentAndCase(t *testing.T) {
	content := "  ```Mermaid\n  graph TD\n  ```\n"
	want := "  ```{mermaid}\n  graph TD\n  ```\n"
	if got := PrepareExportSource(content, "", true); got != want {
		t.Errorf("indented/mixed-case mermaid not converted\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceMermaidGatedOff(t *testing.T) {
	// With renderMermaid false (non-HTML export), a mermaid fence must be left
	// untouched so the export does not require a headless browser and never fails.
	content := "```mermaid\ngraph LR\n  A --> B\n```\n"
	if got := PrepareExportSource(content, "", false); got != content {
		t.Errorf("mermaid fence should be untouched when gated off\n got  = %q\n want = %q", got, content)
	}
}

func TestPrepareExportSourceLeavesNonMermaidFence(t *testing.T) {
	content := "```python\nprint('mermaid')\n```\n"
	if got := PrepareExportSource(content, "", true); got != content {
		t.Errorf("non-mermaid fence altered\n got  = %q\n want = %q", got, content)
	}
}

func TestPrepareExportSourceHighlight(t *testing.T) {
	content := "This is ==important== and `==literal==` stays."
	want := "This is [important]{.mark} and `==literal==` stays."
	if got := PrepareExportSource(content, "", false); got != want {
		t.Errorf("highlight not converted / code not preserved\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceCombined(t *testing.T) {
	// Wikilink and highlight on one line, then a mermaid block (renderMermaid on).
	content := "See [[Home]] which is ==key==.\n```mermaid\nA-->B\n```\n"
	want := "See [Home](/Home) which is [key]{.mark}.\n```{mermaid}\nA-->B\n```\n"
	if got := PrepareExportSource(content, "", true); got != want {
		t.Errorf("combined transforms wrong\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceCodeSpanWithLongerBacktickRun(t *testing.T) {
	// A single-backtick code span whose content contains a longer backtick run
	// must not be closed early (CommonMark: close on an EXACTLY-equal run). The
	// [[Home]] is inside the span and must stay literal.
	content := "Type `x``` [[Home]]` to see it."
	if got := PrepareExportSource(content, "", false); got != content {
		t.Errorf("code span with longer inner run mishandled\n got  = %q\n want = %q", got, content)
	}
}

func TestPrepareExportSourceNormalizesCRLF(t *testing.T) {
	// CRLF must not defeat fence detection: everything after the code block must
	// still be rewritten, and the code block content left literal.
	content := "intro [[A]]\r\n```\r\ncode [[B]]\r\n```\r\nafter [[C]]\r\n"
	want := "intro [A](/A)\n```\ncode [[B]]\n```\nafter [C](/C)\n"
	if got := PrepareExportSource(content, "", false); got != want {
		t.Errorf("CRLF not normalized / fence broken\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceStripsBOMAndKeepsFrontmatter(t *testing.T) {
	content := "\ufeff---\ntitle: T\n---\nBody [[X]].\n"
	want := "---\ntitle: T\n---\nBody [X](/X).\n"
	if got := PrepareExportSource(content, "", false); got != want {
		t.Errorf("BOM/frontmatter handling wrong\n got  = %q\n want = %q", got, want)
	}
}

func TestPrepareExportSourceHighlightEdgeCases(t *testing.T) {
	cases := []struct{ content, want string }{
		{"==x = y==", "[x = y]{.mark}"}, // interior '=' allowed (matches mark parser)
		{"==a]b==", `[a\]b]{.mark}`},    // ']' escaped so the span isn't cut short
		{"==a[b==", `[a\[b]{.mark}`},    // '[' escaped too
	}
	for _, tc := range cases {
		if got := PrepareExportSource(tc.content, "", false); got != tc.want {
			t.Errorf("highlight edge\n content = %q\n got  = %q\n want = %q", tc.content, got, tc.want)
		}
	}
}

func TestWikilinksToMarkdownSpaceInTargetUsesAngleBrackets(t *testing.T) {
	// A target that still contains characters needing protection in a link
	// destination should be wrapped in angle brackets. Parens qualify.
	content := "[[Foo (bar)]]"
	got := PrepareExportSource(content, "", false)
	want := "[Foo (bar)](</Foo-(bar)>)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
