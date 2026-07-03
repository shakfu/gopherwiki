// Package frontmatter parses the optional YAML metadata block at the head of a
// wiki page. The block follows the Jekyll/Quarto convention: a leading line of
// exactly "---", a YAML mapping, and a closing line of "---" (or "...").
//
// For plain markdown pages the block supplies display metadata (e.g. title).
// For computational (Quarto) pages it also carries the render controls
// (engine, execute options, freeze) consumed by the offline render pipeline.
// See docs/computational-pages.md.
package frontmatter

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Freeze mirrors Quarto's execute.freeze setting, which may be the string
// "auto" or a boolean. It is normalized to one of the constants below.
type Freeze string

const (
	// FreezeUnset means no freeze value was specified.
	FreezeUnset Freeze = ""
	// FreezeAuto re-executes a page only when its source changes.
	FreezeAuto Freeze = "auto"
	// FreezeTrue never re-executes (serves the last frozen result).
	FreezeTrue Freeze = "true"
	// FreezeFalse always re-executes on render.
	FreezeFalse Freeze = "false"
)

// ExecuteOptions models the subset of Quarto's `execute:` block relevant to the
// render pipeline.
type ExecuteOptions struct {
	// Enabled corresponds to execute.enabled. A nil pointer means unspecified.
	Enabled *bool `yaml:"enabled"`
	// Freeze corresponds to execute.freeze.
	Freeze Freeze `yaml:"freeze"`
}

// Frontmatter holds the parsed metadata block. Explicitly modeled fields cover
// what the wiki consumes today; Raw retains the full decoded mapping for keys
// not yet modeled.
type Frontmatter struct {
	// Title overrides the page display title when set.
	Title string `yaml:"title"`
	// Engine selects the Quarto execution engine ("knitr" or "jupyter"). Empty
	// means auto-detect.
	Engine string `yaml:"engine"`
	// Execute carries the render controls.
	Execute ExecuteOptions `yaml:"execute"`
	// Raw is the full decoded mapping, for fields not explicitly modeled.
	Raw map[string]any `yaml:"-"`
}

// UnmarshalYAML normalizes the freeze scalar, which YAML may present as either a
// string ("auto") or a boolean (true/false).
func (f *Freeze) UnmarshalYAML(value *yaml.Node) error {
	switch strings.ToLower(value.Value) {
	case "auto":
		*f = FreezeAuto
	case "true":
		*f = FreezeTrue
	case "false":
		*f = FreezeFalse
	default:
		// Unknown values are retained verbatim so callers can decide.
		*f = Freeze(value.Value)
	}
	return nil
}

// Parse splits an optional leading YAML frontmatter block from content. It
// returns the parsed frontmatter (nil when there is no valid block) and the
// remaining body with the block removed. Detection is conservative: a leading
// "---" that lacks a closing delimiter, or whose block is not a valid YAML
// mapping, is treated as ordinary body content (e.g. a thematic break) and the
// original content is returned unchanged.
func Parse(content string) (*Frontmatter, string) {
	// Tolerate a leading UTF-8 BOM.
	trimmed := strings.TrimPrefix(content, "\ufeff")

	// The block must begin at the very start with a "---" line.
	if !hasDelimiterAt(trimmed, 0) {
		return nil, content
	}

	// Content after the opening delimiter line.
	rest := trimmed[len(firstLine(trimmed)):]
	rest = strings.TrimPrefix(rest, "\n")

	// Find the closing delimiter line ("---" or "...").
	yamlBlock, remainder, ok := splitAtClosingDelimiter(rest)
	if !ok {
		return nil, content
	}

	// The block must decode as a YAML mapping. A scalar or sequence (or a parse
	// error) means this is not frontmatter.
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(yamlBlock), &raw); err != nil {
		return nil, content
	}

	fm := &Frontmatter{Raw: raw}
	if err := yaml.Unmarshal([]byte(yamlBlock), fm); err != nil {
		return nil, content
	}

	return fm, remainder
}

// hasDelimiterAt reports whether the line beginning at offset i is exactly a
// frontmatter delimiter ("---").
func hasDelimiterAt(s string, i int) bool {
	line := strings.TrimSuffix(firstLine(s[i:]), "\n")
	return strings.TrimRight(line, "\r") == "---"
}

// firstLine returns the first line of s including nothing past the newline.
func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx+1]
	}
	return s
}

// splitAtClosingDelimiter scans lines for a closing "---" or "..." delimiter. It
// returns the YAML block before the delimiter, the body after it, and whether a
// delimiter was found.
func splitAtClosingDelimiter(s string) (block, remainder string, ok bool) {
	var yamlLines []string
	rest := s
	for len(rest) > 0 {
		line := firstLine(rest)
		lineContent := strings.TrimRight(strings.TrimSuffix(line, "\n"), "\r")
		if lineContent == "---" || lineContent == "..." {
			remainder = rest[len(line):]
			return strings.Join(yamlLines, "\n"), remainder, true
		}
		yamlLines = append(yamlLines, strings.TrimSuffix(line, "\n"))
		rest = rest[len(line):]
	}
	return "", "", false
}
