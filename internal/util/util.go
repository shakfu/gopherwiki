// Package util provides utility functions for GopherWiki.
package util

import (
	"fmt"
	"mime"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Empty checks if a string is empty or contains only whitespace.
func Empty(s string) bool {
	return strings.TrimSpace(s) == ""
}

// Slugify converts a string to a URL-friendly slug.
func Slugify(s string, keepSlashes bool) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")

	// Define allowed characters
	var allowedChars string
	if keepSlashes {
		allowedChars = `[^a-z0-9\-\/]`
	} else {
		allowedChars = `[^a-z0-9\-]`
	}

	// Remove non-allowed characters
	reg := regexp.MustCompile(allowedChars)
	s = reg.ReplaceAllString(s, "")

	// Remove consecutive hyphens
	reg = regexp.MustCompile(`-+`)
	s = reg.ReplaceAllString(s, "-")

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	return s
}

// SanitizePagename cleans up a page name.
func SanitizePagename(name string, handleMD bool) string {
	// Remove .md extension if requested
	if handleMD && strings.HasSuffix(strings.ToLower(name), ".md") {
		name = name[:len(name)-3]
	}

	// Trim whitespace
	name = strings.TrimSpace(name)

	// Remove leading/trailing slashes
	name = strings.Trim(name, "/")

	return name
}

// GetPagename extracts the page name from a path.
func GetPagename(pagepath string, full bool) string {
	if pagepath == "" {
		return ""
	}

	parts := SplitPath(pagepath)
	if len(parts) == 0 {
		return pagepath
	}

	if full {
		return pagepath
	}

	return parts[len(parts)-1]
}

// GetPagepath converts a page name to a path.
func GetPagepath(pagename string) string {
	return SanitizePagename(pagename, true)
}

// GetFilename converts a page path to a filename with .md extension.
func GetFilename(pagepath string) string {
	pagepath = SanitizePagename(pagepath, true)
	return pagepath + ".md"
}

// GetAttachmentDirectoryname returns the attachment directory for a page.
func GetAttachmentDirectoryname(filename string) string {
	// Remove .md extension
	if strings.HasSuffix(filename, ".md") {
		filename = filename[:len(filename)-3]
	}
	return filename
}

// GetPageDirectoryname returns the directory portion of a page path.
func GetPageDirectoryname(pagepath string) string {
	if pagepath == "" {
		return ""
	}
	dir := filepath.Dir(pagepath)
	if dir == "." {
		return ""
	}
	return dir
}

// SplitPath splits a path into its components.
func SplitPath(p string) []string {
	if p == "" {
		return nil
	}
	// Normalize to forward slashes
	p = filepath.ToSlash(p)
	// Split on slashes
	parts := strings.Split(p, "/")
	// Filter empty parts
	var result []string
	for _, part := range parts {
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

// JoinPath joins path components.
func JoinPath(parts []string) string {
	return strings.Join(parts, "/")
}

// GuessMimetype guesses the MIME type for a filename.
func GuessMimetype(filename string) string {
	ext := filepath.Ext(filename)
	if ext == "" {
		return "application/octet-stream"
	}

	// Check for markdown
	if ext == ".md" || ext == ".markdown" {
		return "text/markdown"
	}

	// Use Go's mime package
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return "application/octet-stream"
	}

	// Strip charset parameter for simplicity
	if idx := strings.Index(mimeType, ";"); idx >= 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	return mimeType
}

// SizeofFmt formats a byte size for human display.
func SizeofFmt(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

// FormatDatetime formats a time for display.
func FormatDatetime(t time.Time, format string) string {
	switch format {
	case "medium":
		return t.Format("2006-01-02 15:04")
	case "full":
		return t.Format("2006-01-02 15:04:05")
	case "deltanow":
		return StrfDeltaRound(time.Since(t), "second")
	default:
		return t.Format("2006-01-02 15:04:05")
	}
}

// StrfDeltaRound formats a duration with the given precision.
func StrfDeltaRound(d time.Duration, precision string) string {
	if d < 0 {
		d = -d
	}

	seconds := int(d.Seconds())
	minutes := seconds / 60
	hours := minutes / 60
	days := hours / 24

	if days > 0 {
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	if hours > 0 {
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	}
	if minutes > 0 {
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	}
	if seconds == 1 {
		return "1 second ago"
	}
	return fmt.Sprintf("%d seconds ago", seconds)
}

// Pluralize returns the singular or plural form based on count.
func Pluralize(count int, plural, singular string) string {
	if count == 1 {
		return singular
	}
	return plural
}

// URLQuote escapes quotes in a URL.
func URLQuote(s string) string {
	s = strings.ReplaceAll(s, "'", "%27")
	s = strings.ReplaceAll(s, "\"", "%22")
	return s
}

// GetHeader extracts the first header from markdown content.
func GetHeader(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}

// GetPagenameForTitle returns a display name for the page.
func GetPagenameForTitle(pagepath string, full bool, header string) string {
	if header != "" {
		return header
	}
	name := GetPagename(pagepath, full)

	// Capitalize first letter
	if len(name) > 0 {
		runes := []rune(name)
		runes[0] = unicode.ToUpper(runes[0])
		name = string(runes)
	}

	return name
}

// Breadcrumb represents a navigation breadcrumb.
type Breadcrumb struct {
	Name string
	Path string
}

// GetBreadcrumbs generates breadcrumbs for a page path.
func GetBreadcrumbs(pagepath string) []Breadcrumb {
	if pagepath == "" {
		return nil
	}

	parts := SplitPath(pagepath)
	var breadcrumbs []Breadcrumb
	var currentPath string

	for _, part := range parts {
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}
		breadcrumbs = append(breadcrumbs, Breadcrumb{
			Name: part,
			Path: currentPath,
		})
	}

	return breadcrumbs
}

// IntOrNil parses a string to int, returning nil on failure.
func IntOrNil(s string) *int {
	if s == "" {
		return nil
	}
	var i int
	if _, err := fmt.Sscanf(s, "%d", &i); err != nil {
		return nil
	}
	return &i
}
