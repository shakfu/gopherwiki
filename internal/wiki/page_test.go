package wiki

import (
	"os"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/frontmatter"
	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/storage"
)

// setupPageStore creates a git-backed store in a temp dir for page tests.
func setupPageStore(t *testing.T) (storage.Storage, *config.Config) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gopherwiki-page-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	store, err := storage.NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("failed to create GitStorage: %v", err)
	}

	cfg := config.Default()
	cfg.Repository = tmpDir
	return store, cfg
}

var testAuthor = storage.Author{Name: "Test", Email: "test@example.com"}

func TestNewPageResolvesMarkdown(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.Store("notes.md", "# Notes\nplain page\n", "create", testAuthor)

	page, err := NewPage(store, cfg, "notes", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if !page.Exists {
		t.Fatal("expected page to exist")
	}
	if page.Filename != "notes.md" {
		t.Errorf("Filename = %q, want %q", page.Filename, "notes.md")
	}
	if page.IsComputational {
		t.Error("plain markdown page should not be computational")
	}
}

func TestNewPageResolvesQuarto(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.StoreBytes("analysis.qmd", []byte("---\ntitle: Analysis\n---\n# A\n"), "create", testAuthor)

	page, err := NewPage(store, cfg, "analysis", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if !page.Exists {
		t.Fatal("expected .qmd page to exist and be resolved")
	}
	if page.Filename != "analysis.qmd" {
		t.Errorf("Filename = %q, want %q", page.Filename, "analysis.qmd")
	}
	if !page.IsComputational {
		t.Error("expected .qmd page to be computational")
	}
	// Attachment directory drops the extension, matching a .md page's dir.
	if page.AttachmentDirectoryname != "analysis" {
		t.Errorf("AttachmentDirectoryname = %q, want %q", page.AttachmentDirectoryname, "analysis")
	}
}

func TestNewPagePrefersMarkdownOnTie(t *testing.T) {
	store, cfg := setupPageStore(t)
	// Both extensions present: .md wins per resolution priority.
	store.Store("dup.md", "# Md\n", "create md", testAuthor)
	store.StoreBytes("dup.qmd", []byte("# Qmd\n"), "create qmd", testAuthor)

	page, err := NewPage(store, cfg, "dup", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if page.Filename != "dup.md" {
		t.Errorf("Filename = %q, want %q (.md should win tie)", page.Filename, "dup.md")
	}
	if page.IsComputational {
		t.Error("resolved page is .md, should not be computational")
	}
}

func TestNewPageMissingDefaultsToMarkdown(t *testing.T) {
	store, cfg := setupPageStore(t)

	page, err := NewPage(store, cfg, "ghost", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if page.Exists {
		t.Fatal("expected non-existent page")
	}
	if page.Filename != "ghost.md" {
		t.Errorf("Filename = %q, want %q (default extension)", page.Filename, "ghost.md")
	}
	if page.IsComputational {
		t.Error("new page defaults to markdown, not computational")
	}
}

func TestRenderDispatchesQuartoToPlaceholder(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.StoreBytes("report.qmd", []byte("---\ntitle: R\n---\n```{python}\nprint(1)\n```\n"), "create", testAuthor)
	rnd := renderer.New(cfg)

	page, err := NewPage(store, cfg, "report", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}

	html, toc, _ := page.Render(rnd)
	if !strings.Contains(html, "computational-pending") {
		t.Errorf("expected render-pending placeholder, got: %q", html)
	}
	// The placeholder must not execute or leak the source code cell.
	if strings.Contains(html, "print(1)") {
		t.Errorf("placeholder must not render source cells, got: %q", html)
	}
	if toc != nil {
		t.Errorf("placeholder should have no TOC, got %v", toc)
	}
}

func TestRenderMarkdownUsesGoldmark(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.Store("plain.md", "# Plain\n\nSome **bold** text.\n", "create", testAuthor)
	rnd := renderer.New(cfg)

	page, err := NewPage(store, cfg, "plain", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}

	html, _, _ := page.Render(rnd)
	if !strings.Contains(html, "<strong>bold</strong>") {
		t.Errorf("expected goldmark-rendered HTML, got: %q", html)
	}
	if strings.Contains(html, "computational-pending") {
		t.Error("plain page should not show the computational placeholder")
	}
}

func TestRenamePreservesQuartoExtension(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.StoreBytes("draft.qmd", []byte("# Draft\n"), "create", testAuthor)

	page, err := NewPage(store, cfg, "draft", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if err := page.Rename("final", "rename", testAuthor); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if !store.Exists("final.qmd") {
		t.Error("renamed computational page should keep its .qmd extension")
	}
	if store.Exists("final.md") {
		t.Error("rename must not convert a .qmd page into .md")
	}

	moved, err := NewPage(store, cfg, "final", "")
	if err != nil {
		t.Fatalf("NewPage after rename: %v", err)
	}
	if !moved.IsComputational {
		t.Error("renamed page should still be computational")
	}
}

func TestPageFrontmatterTitleOverridesHeading(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.Store("doc.md", "---\ntitle: Fancy Title\n---\n# Plain Heading\n\nBody.\n", "create", testAuthor)

	page, err := NewPage(store, cfg, "doc", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if page.Frontmatter == nil {
		t.Fatal("expected parsed frontmatter")
	}
	if page.Pagename != "Fancy Title" {
		t.Errorf("Pagename = %q, want %q (frontmatter title wins over heading)", page.Pagename, "Fancy Title")
	}
	if page.Body != "# Plain Heading\n\nBody.\n" {
		t.Errorf("Body = %q, want frontmatter stripped", page.Body)
	}
}

func TestPageFrontmatterStrippedFromRender(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.Store("doc.md", "---\ntitle: T\n---\n# Heading\n\nText.\n", "create", testAuthor)
	rnd := renderer.New(cfg)

	page, err := NewPage(store, cfg, "doc", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	html, _, _ := page.Render(rnd)
	if strings.Contains(html, "title: T") {
		t.Errorf("rendered HTML leaked frontmatter: %q", html)
	}
	if !strings.Contains(html, "<h1") || !strings.Contains(html, "Heading") {
		t.Errorf("expected heading in rendered body, got %q", html)
	}
}

func TestPageHeadingTitleWhenNoFrontmatter(t *testing.T) {
	store, cfg := setupPageStore(t)
	store.Store("doc.md", "# Just Heading\n\nBody.\n", "create", testAuthor)

	page, err := NewPage(store, cfg, "doc", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if page.Frontmatter != nil {
		t.Errorf("expected nil frontmatter, got %+v", page.Frontmatter)
	}
	if page.Pagename != "Just Heading" {
		t.Errorf("Pagename = %q, want %q (heading title)", page.Pagename, "Just Heading")
	}
	if page.Body != page.Content {
		t.Errorf("Body should equal Content when there is no frontmatter")
	}
}

func TestPageComputationalFrontmatterExposed(t *testing.T) {
	store, cfg := setupPageStore(t)
	content := "---\ntitle: Analysis\nengine: jupyter\nexecute:\n  freeze: auto\n---\n```{python}\nprint(1)\n```\n"
	store.StoreBytes("analysis.qmd", []byte(content), "create", testAuthor)

	page, err := NewPage(store, cfg, "analysis", "")
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	if !page.IsComputational {
		t.Fatal("expected computational page")
	}
	if page.Frontmatter == nil {
		t.Fatal("expected frontmatter on computational page")
	}
	if page.Frontmatter.Engine != "jupyter" {
		t.Errorf("Engine = %q, want jupyter", page.Frontmatter.Engine)
	}
	if page.Frontmatter.Execute.Freeze != frontmatter.FreezeAuto {
		t.Errorf("Freeze = %q, want auto", page.Frontmatter.Execute.Freeze)
	}
	if page.Pagename != "Analysis" {
		t.Errorf("Pagename = %q, want %q", page.Pagename, "Analysis")
	}
}
