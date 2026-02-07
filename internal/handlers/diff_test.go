package handlers

import (
	"testing"
)

func TestParseDiff_Empty(t *testing.T) {
	lines := parseDiff("")
	if len(lines) != 1 {
		t.Fatalf("parseDiff(\"\") returned %d lines, want 1", len(lines))
	}
	if lines[0].Type != "context" {
		t.Errorf("empty line type = %q, want %q", lines[0].Type, "context")
	}
}

func TestParseDiff_AddLines(t *testing.T) {
	lines := parseDiff("+added line")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Type != "add" {
		t.Errorf("type = %q, want %q", lines[0].Type, "add")
	}
	if lines[0].Content != "+added line" {
		t.Errorf("content = %q, want %q", lines[0].Content, "+added line")
	}
}

func TestParseDiff_RemoveLines(t *testing.T) {
	lines := parseDiff("-removed line")
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	if lines[0].Type != "remove" {
		t.Errorf("type = %q, want %q", lines[0].Type, "remove")
	}
}

func TestParseDiff_Headers(t *testing.T) {
	diff := "--- a/file.txt\n+++ b/file.txt\n@@ -1,3 +1,4 @@"
	lines := parseDiff(diff)

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}

	for i, line := range lines {
		if line.Type != "header" {
			t.Errorf("line %d type = %q, want %q", i, line.Type, "header")
		}
	}
}

func TestParseDiff_Mixed(t *testing.T) {
	diff := `--- a/file.txt
+++ b/file.txt
@@ -1,3 +1,4 @@
 context line
-old line
+new line
+another new line`

	lines := parseDiff(diff)

	expected := []struct {
		typ     string
		content string
	}{
		{"header", "--- a/file.txt"},
		{"header", "+++ b/file.txt"},
		{"header", "@@ -1,3 +1,4 @@"},
		{"context", " context line"},
		{"remove", "-old line"},
		{"add", "+new line"},
		{"add", "+another new line"},
	}

	if len(lines) != len(expected) {
		t.Fatalf("got %d lines, want %d", len(lines), len(expected))
	}

	for i, want := range expected {
		if lines[i].Type != want.typ {
			t.Errorf("line %d type = %q, want %q", i, lines[i].Type, want.typ)
		}
		if lines[i].Content != want.content {
			t.Errorf("line %d content = %q, want %q", i, lines[i].Content, want.content)
		}
	}
}
