package handlers

import "strings"

// DiffLine represents a single line in a diff.
type DiffLine struct {
	Type    string // "add", "remove", "context", "header"
	Content string
}

// parseDiff parses a diff string into structured lines.
func parseDiff(diff string) []DiffLine {
	var lines []DiffLine
	for _, line := range strings.Split(diff, "\n") {
		var dl DiffLine
		if strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") {
			dl.Type = "header"
		} else if strings.HasPrefix(line, "@@") {
			dl.Type = "header"
		} else if strings.HasPrefix(line, "+") {
			dl.Type = "add"
		} else if strings.HasPrefix(line, "-") {
			dl.Type = "remove"
		} else {
			dl.Type = "context"
		}
		dl.Content = line
		lines = append(lines, dl)
	}
	return lines
}
