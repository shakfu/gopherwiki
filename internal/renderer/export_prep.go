package renderer

import (
	"regexp"
	"strings"

	"github.com/sa/gopherwiki/internal/frontmatter"
)

// highlightRegex matches GopherWiki ==highlight== spans on a single line. The
// content is non-greedy so it closes at the next `==`, matching the on-site mark
// parser (which allows a lone `=` inside a span).
var highlightRegex = regexp.MustCompile("==(.+?)==")

// PrepareExportSource rewrites GopherWiki-specific markdown into constructs that
// Quarto/Pandoc understand, so exported documents keep their meaning instead of
// showing raw wiki syntax. It applies three transforms:
//
//   - [[wikilinks]] / [[Target|Label]] / [[#123]] -> standard markdown links,
//     absolute against baseURL (the site URL) or site-relative when baseURL is
//     empty. Targets map to page paths as the HTML renderer does (spaces ->
//     hyphens, case preserved); numeric issue refs map to /-/issues/N.
//   - ```mermaid fenced blocks -> Quarto ```{mermaid} diagram cells, but only
//     when renderMermaid is true. That rewrite is gated because Quarto renders
//     mermaid to an image via a headless browser for non-HTML targets, which
//     requires `quarto install chrome-headless-shell`; enabling it for those
//     formats without Chrome makes the whole export fail. Callers pass true only
//     for HTML export, where mermaid renders client-side with no extra tooling.
//   - ==highlight== -> Pandoc [text]{.mark} spans (rendered as <mark>).
//
// Math ($...$, $$...$$) needs no transform -- Pandoc handles it natively.
//
// Line endings are normalized (BOM stripped, CRLF -> LF) so structural detection
// is reliable, and the leading YAML frontmatter block is identified with the
// canonical frontmatter.Parse (so export agrees with how the page renderer and
// search treat frontmatter) and preserved verbatim. Fenced code blocks and
// inline code spans are left untouched (aside from converting a mermaid fence's
// own info string). Indented (4-space) code blocks are not specially detected.
func PrepareExportSource(content, baseURL string, renderMermaid bool) string {
	baseURL = strings.TrimRight(baseURL, "/")

	// Normalize so exact-string structural checks below are reliable regardless
	// of how the source was authored (Windows editors, external git imports).
	content = strings.TrimPrefix(content, "\ufeff")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	// Split off the frontmatter with the canonical parser and transform only the
	// body; the body it returns is a suffix of content, so the prefix is the
	// verbatim frontmatter block (empty when there is none).
	_, body := frontmatter.Parse(content)
	prefix := content[:len(content)-len(body)]

	lines := strings.Split(body, "\n")
	inFence := false
	var fenceChar byte
	var fenceLen int
	for i := range lines {
		trimmed := strings.TrimLeft(lines[i], " \t")
		if marker, ok := fenceMarker(trimmed); ok {
			if !inFence {
				inFence = true
				fenceChar = marker[0]
				fenceLen = len(marker)
				if renderMermaid {
					lines[i] = rewriteMermaidFence(lines[i], marker)
				}
			} else if marker[0] == fenceChar && len(marker) >= fenceLen && strings.Trim(trimmed, string(fenceChar)) == "" {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		lines[i] = rewriteInlineOutsideCode(lines[i], baseURL)
	}
	return prefix + strings.Join(lines, "\n")
}

// fenceMarker reports whether a leading-whitespace-trimmed line opens or closes a
// fenced code block, returning the leading run of fence characters (``` or ~~~,
// 3 or more).
func fenceMarker(trimmed string) (string, bool) {
	if len(trimmed) < 3 {
		return "", false
	}
	c := trimmed[0]
	if c != '`' && c != '~' {
		return "", false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == c {
		n++
	}
	if n < 3 {
		return "", false
	}
	return trimmed[:n], true
}

// rewriteMermaidFence converts a GopherWiki ```mermaid opening fence into a
// Quarto ```{mermaid} diagram cell, preserving indentation. Only backtick fences
// whose info string is exactly "mermaid" (case-insensitive) are converted.
func rewriteMermaidFence(line, marker string) string {
	if marker[0] != '`' {
		return line
	}
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	info := strings.TrimSpace(strings.TrimLeft(line, " \t")[len(marker):])
	if strings.EqualFold(info, "mermaid") {
		return indent + marker + "{mermaid}"
	}
	return line
}

// rewriteInlineOutsideCode applies the inline export rewrites (wikilinks and
// highlights) to a single line, skipping inline code spans. A code span opened
// by a run of n backticks is closed only by a run of exactly n backticks
// (CommonMark), so a longer backtick run inside the span does not end it early.
func rewriteInlineOutsideCode(line, baseURL string) string {
	if !strings.Contains(line, "[[") && !strings.Contains(line, "==") {
		return line
	}
	var b strings.Builder
	i := 0
	for i < len(line) {
		if line[i] == '`' {
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			n := j - i
			if cs, ce := exactBacktickRun(line, j, n); cs >= 0 {
				// Copy the whole code span verbatim.
				b.WriteString(line[i:ce])
				i = ce
				continue
			}
			// Unclosed run: emit the backticks literally and continue.
			b.WriteString(line[i:j])
			i = j
			continue
		}
		// Process the text chunk up to the next backtick (or end of line).
		chunk := line[i:]
		if k := strings.IndexByte(chunk, '`'); k >= 0 {
			chunk = chunk[:k]
		}
		b.WriteString(applyInlineRewrites(chunk, baseURL))
		i += len(chunk)
	}
	return b.String()
}

// exactBacktickRun finds, at or after index from, a maximal run of backticks
// whose length is exactly n, returning its [start, end) bounds. It returns
// (-1, -1) if none exists. Runs of a different length (the CommonMark rule for
// code-span closers) are skipped.
func exactBacktickRun(s string, from, n int) (int, int) {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		k := i
		for k < len(s) && s[k] == '`' {
			k++
		}
		if k-i == n {
			return i, k
		}
		i = k
	}
	return -1, -1
}

// applyInlineRewrites rewrites wikilinks then highlights in a code-free chunk.
func applyInlineRewrites(chunk, baseURL string) string {
	chunk = wikiLinkRegex.ReplaceAllStringFunc(chunk, func(m string) string {
		return wikilinkToMarkdown(m[2:len(m)-2], baseURL)
	})
	chunk = highlightRegex.ReplaceAllStringFunc(chunk, func(m string) string {
		return "[" + escapeSpanText(m[2:len(m)-2]) + "]{.mark}"
	})
	return chunk
}

// escapeSpanText escapes the bracket characters that would otherwise terminate a
// Pandoc bracketed span early.
func escapeSpanText(s string) string {
	s = strings.ReplaceAll(s, "[", `\[`)
	s = strings.ReplaceAll(s, "]", `\]`)
	return s
}

// wikilinkToMarkdown converts the inner text of a single [[...]] into a markdown
// link. inner is the content between the brackets (guaranteed to contain no ']').
func wikilinkToMarkdown(inner, baseURL string) string {
	target, label := inner, inner
	if idx := strings.Index(inner, "|"); idx >= 0 {
		target = strings.TrimSpace(inner[:idx])
		label = strings.TrimSpace(inner[idx+1:])
	} else {
		target = strings.TrimSpace(inner)
		label = target
	}
	if target == "" {
		return "[[" + inner + "]]" // malformed; leave as-is
	}

	var url string
	if len(target) > 1 && target[0] == '#' && isAllDigits(target[1:]) {
		url = baseURL + "/-/issues/" + target[1:]
	} else {
		url = baseURL + "/" + strings.ReplaceAll(target, " ", "-")
	}
	// Guard link-destination syntax against spaces/parens in a target.
	if strings.ContainsAny(url, " ()") {
		url = "<" + url + ">"
	}
	return "[" + label + "](" + url + ")"
}

// isAllDigits reports whether s is non-empty and all ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
