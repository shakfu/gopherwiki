package frontmatter

import "testing"

func TestParseNoFrontmatter(t *testing.T) {
	content := "# Just a heading\n\nSome text.\n"
	fm, body := Parse(content)
	if fm != nil {
		t.Errorf("expected nil frontmatter, got %+v", fm)
	}
	if body != content {
		t.Errorf("body should be unchanged, got %q", body)
	}
}

func TestParseBasic(t *testing.T) {
	content := "---\ntitle: My Page\n---\n# Heading\n\nBody.\n"
	fm, body := Parse(content)
	if fm == nil {
		t.Fatal("expected frontmatter, got nil")
	}
	if fm.Title != "My Page" {
		t.Errorf("Title = %q, want %q", fm.Title, "My Page")
	}
	if body != "# Heading\n\nBody.\n" {
		t.Errorf("body = %q, want stripped content", body)
	}
	if fm.Raw["title"] != "My Page" {
		t.Errorf("Raw[title] = %v, want %q", fm.Raw["title"], "My Page")
	}
}

func TestParseQuartoControls(t *testing.T) {
	content := "---\ntitle: Analysis\nengine: jupyter\nexecute:\n  enabled: true\n  freeze: auto\n---\n# A\n"
	fm, body := Parse(content)
	if fm == nil {
		t.Fatal("expected frontmatter")
	}
	if fm.Engine != "jupyter" {
		t.Errorf("Engine = %q, want jupyter", fm.Engine)
	}
	if fm.Execute.Enabled == nil || !*fm.Execute.Enabled {
		t.Errorf("Execute.Enabled = %v, want true", fm.Execute.Enabled)
	}
	if fm.Execute.Freeze != FreezeAuto {
		t.Errorf("Execute.Freeze = %q, want %q", fm.Execute.Freeze, FreezeAuto)
	}
	if body != "# A\n" {
		t.Errorf("body = %q, want %q", body, "# A\n")
	}
}

func TestParseFreezeBoolean(t *testing.T) {
	// freeze given as a YAML boolean, not the string "auto".
	content := "---\nexecute:\n  freeze: true\n---\nbody\n"
	fm, _ := Parse(content)
	if fm == nil {
		t.Fatal("expected frontmatter")
	}
	if fm.Execute.Freeze != FreezeTrue {
		t.Errorf("Execute.Freeze = %q, want %q", fm.Execute.Freeze, FreezeTrue)
	}
}

func TestParseUnclosedIsNotFrontmatter(t *testing.T) {
	// Leading --- with no closing delimiter is a thematic break, not metadata.
	content := "---\nsome text that never closes\nmore text\n"
	fm, body := Parse(content)
	if fm != nil {
		t.Errorf("expected nil frontmatter for unclosed block, got %+v", fm)
	}
	if body != content {
		t.Errorf("body should be unchanged for unclosed block")
	}
}

func TestParseNonMappingIsNotFrontmatter(t *testing.T) {
	// A closed block whose contents are a scalar/sequence, not a mapping.
	content := "---\n- just\n- a\n- list\n---\nbody\n"
	fm, body := Parse(content)
	if fm != nil {
		t.Errorf("expected nil frontmatter for non-mapping block, got %+v", fm)
	}
	if body != content {
		t.Errorf("body should be unchanged for non-mapping block, got %q", body)
	}
}

func TestParseEmptyBlock(t *testing.T) {
	content := "---\n---\nbody after empty block\n"
	fm, body := Parse(content)
	if fm == nil {
		t.Fatal("empty mapping block is still valid frontmatter")
	}
	if fm.Title != "" {
		t.Errorf("Title = %q, want empty", fm.Title)
	}
	if body != "body after empty block\n" {
		t.Errorf("body = %q, want stripped", body)
	}
}

func TestParseClosingDots(t *testing.T) {
	// YAML permits "..." as the closing document marker.
	content := "---\ntitle: T\n...\nbody\n"
	fm, body := Parse(content)
	if fm == nil || fm.Title != "T" {
		t.Fatalf("expected title T, got %+v", fm)
	}
	if body != "body\n" {
		t.Errorf("body = %q, want %q", body, "body\n")
	}
}

func TestParseCRLF(t *testing.T) {
	content := "---\r\ntitle: Win\r\n---\r\n# Body\r\n"
	fm, _ := Parse(content)
	if fm == nil || fm.Title != "Win" {
		t.Fatalf("expected title Win with CRLF delimiters, got %+v", fm)
	}
}
