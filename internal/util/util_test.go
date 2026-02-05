package util

import (
	"testing"
	"time"
)

func TestEmpty(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"   ", true},
		{"\t\n", true},
		{"hello", false},
		{" hello ", false},
	}

	for _, tt := range tests {
		got := Empty(tt.input)
		if got != tt.want {
			t.Errorf("Empty(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input       string
		keepSlashes bool
		want        string
	}{
		{"Hello World", false, "hello-world"},
		{"Test 123", false, "test-123"},
		{"Special!@#Characters", false, "specialcharacters"},
		{"Multiple   Spaces", false, "multiple-spaces"},
		{"  Trim Spaces  ", false, "trim-spaces"},
		{"path/to/page", true, "path/to/page"},
		{"path/to/page", false, "pathtopage"},
		{"UPPERCASE", false, "uppercase"},
	}

	for _, tt := range tests {
		got := Slugify(tt.input, tt.keepSlashes)
		if got != tt.want {
			t.Errorf("Slugify(%q, %v) = %q, want %q", tt.input, tt.keepSlashes, got, tt.want)
		}
	}
}

func TestSanitizePagename(t *testing.T) {
	tests := []struct {
		input    string
		handleMD bool
		want     string
	}{
		{"test.md", true, "test"},
		{"test.md", false, "test.md"},
		{"  test  ", true, "test"},
		{"/test/", true, "test"},
		{"path/to/page.md", true, "path/to/page"},
	}

	for _, tt := range tests {
		got := SanitizePagename(tt.input, tt.handleMD)
		if got != tt.want {
			t.Errorf("SanitizePagename(%q, %v) = %q, want %q", tt.input, tt.handleMD, got, tt.want)
		}
	}
}

func TestGetPagename(t *testing.T) {
	tests := []struct {
		input string
		full  bool
		want  string
	}{
		{"path/to/page", false, "page"},
		{"path/to/page", true, "path/to/page"},
		{"single", false, "single"},
		{"", false, ""},
	}

	for _, tt := range tests {
		got := GetPagename(tt.input, tt.full)
		if got != tt.want {
			t.Errorf("GetPagename(%q, %v) = %q, want %q", tt.input, tt.full, got, tt.want)
		}
	}
}

func TestGetFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"test", "test.md"},
		{"path/to/page", "path/to/page.md"},
		{"already.md", "already.md"},
	}

	for _, tt := range tests {
		got := GetFilename(tt.input)
		if got != tt.want {
			t.Errorf("GetFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetAttachmentDirectoryname(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"page.md", "page"},
		{"path/to/page.md", "path/to/page"},
		{"noext", "noext"},
	}

	for _, tt := range tests {
		got := GetAttachmentDirectoryname(tt.input)
		if got != tt.want {
			t.Errorf("GetAttachmentDirectoryname(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"path/to/page", []string{"path", "to", "page"}},
		{"single", []string{"single"}},
		{"", nil},
		{"/leading/slash", []string{"leading", "slash"}},
	}

	for _, tt := range tests {
		got := SplitPath(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("SplitPath(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("SplitPath(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestGuessMimetype(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"test.md", "text/markdown"},
		{"test.txt", "text/plain"},
		{"test.html", "text/html"},
		{"test.png", "image/png"},
		{"test.jpg", "image/jpeg"},
		{"test.pdf", "application/pdf"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		got := GuessMimetype(tt.input)
		if got != tt.want {
			t.Errorf("GuessMimetype(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSizeofFmt(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{100, "100 B"},
		{1024, "1.0 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{1024 * 1024 * 1024, "1.0 GiB"},
	}

	for _, tt := range tests {
		got := SizeofFmt(tt.input)
		if got != tt.want {
			t.Errorf("SizeofFmt(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPluralize(t *testing.T) {
	tests := []struct {
		count    int
		plural   string
		singular string
		want     string
	}{
		{0, "items", "item", "items"},
		{1, "items", "item", "item"},
		{2, "items", "item", "items"},
		{100, "pages", "page", "pages"},
	}

	for _, tt := range tests {
		got := Pluralize(tt.count, tt.plural, tt.singular)
		if got != tt.want {
			t.Errorf("Pluralize(%d, %q, %q) = %q, want %q", tt.count, tt.plural, tt.singular, got, tt.want)
		}
	}
}

func TestGetHeader(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"# Hello World\n\nContent", "Hello World"},
		{"No header here", ""},
		{"## Level 2 Header", ""},
		{"  # Indented header", "Indented header"}, // Whitespace is trimmed
		{"# First\n# Second", "First"},
	}

	for _, tt := range tests {
		got := GetHeader(tt.input)
		if got != tt.want {
			t.Errorf("GetHeader(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestGetBreadcrumbs(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"path/to/page", 3},
		{"single", 1},
		{"", 0},
	}

	for _, tt := range tests {
		got := GetBreadcrumbs(tt.input)
		if len(got) != tt.want {
			t.Errorf("GetBreadcrumbs(%q) length = %d, want %d", tt.input, len(got), tt.want)
		}
	}

	// Test specific breadcrumb values
	bc := GetBreadcrumbs("path/to/page")
	if bc[0].Name != "path" || bc[0].Path != "path" {
		t.Errorf("First breadcrumb = %+v, want Name=path, Path=path", bc[0])
	}
	if bc[2].Name != "page" || bc[2].Path != "path/to/page" {
		t.Errorf("Last breadcrumb = %+v, want Name=page, Path=path/to/page", bc[2])
	}
}

func TestFormatDatetime(t *testing.T) {
	testTime := time.Date(2024, 6, 15, 14, 30, 45, 0, time.UTC)

	tests := []struct {
		format string
		want   string
	}{
		{"medium", "2024-06-15 14:30"},
		{"full", "2024-06-15 14:30:45"},
	}

	for _, tt := range tests {
		got := FormatDatetime(testTime, tt.format)
		if got != tt.want {
			t.Errorf("FormatDatetime(time, %q) = %q, want %q", tt.format, got, tt.want)
		}
	}
}

func TestURLQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal", "normal"},
		{"with'quote", "with%27quote"},
		{`with"double`, `with%22double`},
		{`both'"quotes`, `both%27%22quotes`},
	}

	for _, tt := range tests {
		got := URLQuote(tt.input)
		if got != tt.want {
			t.Errorf("URLQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
