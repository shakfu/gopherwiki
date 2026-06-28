package wiki

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/storage"
)

func setupTestService(t *testing.T) (*WikiService, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gopherwiki-service-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	store, err := storage.NewGitStorage(tmpDir, true)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create GitStorage: %v", err)
	}

	database, err := db.Open("sqlite:///:memory:")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to open database: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		database.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := config.Default()
	cfg.Repository = tmpDir

	author := storage.Author{Name: "Test User", Email: "test@example.com"}

	// Create some test pages
	store.Store("home.md", "# Home\nWelcome to the wiki.\n", "Create home", author)
	store.Store("about.md", "# About\nThis is the about page.\n", "Create about", author)
	store.Store("guide.md", "# User Guide\nHow to use the wiki.\n", "Create guide", author)
	store.Store(".hidden.md", "# Hidden\nShould still be indexed.\n", "Create hidden", author)

	ws := NewWikiService(store, cfg, database)
	cleanup := func() {
		database.Close()
		os.RemoveAll(tmpDir)
	}

	return ws, cleanup
}

func TestWikiServiceSearch(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// Mirror a started-up service: the FTS index is populated from storage
	// (the seed pages are created via store.Store, which bypasses SavePage's
	// indexing).
	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("EnsureSearchIndex failed: %v", err)
	}

	t.Run("empty query returns nil", func(t *testing.T) {
		results, err := ws.Search(context.Background(), "")
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if results != nil {
			t.Errorf("Expected nil results for empty query, got %d results", len(results))
		}
	})

	t.Run("query matching multiple pages", func(t *testing.T) {
		results, err := ws.Search(context.Background(), "the")
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if len(results) < 2 {
			t.Errorf("Expected at least 2 results for 'the', got %d", len(results))
		}
	})

	t.Run("query matching single page", func(t *testing.T) {
		results, err := ws.Search(ctx, "Welcome")
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Expected 1 result for 'Welcome', got %d", len(results))
		}
		if results[0].Pagepath != "home" {
			t.Errorf("Expected pagepath 'home', got %q", results[0].Pagepath)
		}
		if results[0].MatchCount < 1 {
			t.Errorf("Expected at least 1 match, got %d", results[0].MatchCount)
		}
	})

	t.Run("case insensitive search", func(t *testing.T) {
		results, err := ws.Search(context.Background(), "welcome")
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("Expected 1 result for case-insensitive 'welcome', got %d", len(results))
		}
	})

	t.Run("no matches returns empty slice", func(t *testing.T) {
		results, err := ws.Search(context.Background(), "nonexistentterm12345")
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})

	t.Run("uses page header as name", func(t *testing.T) {
		results, err := ws.Search(ctx, "Welcome")
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("Expected 1 result, got %d", len(results))
		}
		if results[0].Pagename != "Home" {
			t.Errorf("Expected pagename 'Home' (from header), got %q", results[0].Pagename)
		}
	})
}

func TestWikiServicePageIndex(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	pages, err := ws.PageIndex(context.Background())
	if err != nil {
		t.Fatalf("PageIndex returned error: %v", err)
	}

	// Should list all .md files
	if len(pages) < 3 {
		t.Errorf("Expected at least 3 pages, got %d", len(pages))
	}

	// Verify structure
	found := false
	for _, p := range pages {
		if p.Path == "home" {
			found = true
			if p.Name == "" {
				t.Error("Page name should not be empty")
			}
		}
	}
	if !found {
		t.Error("Expected to find 'home' in page index")
	}
}

func TestWikiServiceChangelog(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	log, err := ws.Changelog(context.Background(),10)
	if err != nil {
		t.Fatalf("Changelog returned error: %v", err)
	}

	// We created 4 pages, so there should be 4 commits
	if len(log) != 4 {
		t.Errorf("Expected 4 changelog entries, got %d", len(log))
	}

	t.Run("respects maxCount", func(t *testing.T) {
		log, err := ws.Changelog(context.Background(),2)
		if err != nil {
			t.Fatalf("Changelog returned error: %v", err)
		}
		if len(log) != 2 {
			t.Errorf("Expected 2 changelog entries with maxCount=2, got %d", len(log))
		}
	})
}

func TestWikiServiceShowCommit(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	// Get a revision from the changelog
	log, err := ws.Changelog(context.Background(),1)
	if err != nil || len(log) == 0 {
		t.Fatal("Cannot get changelog for ShowCommit test")
	}

	meta, diff, err := ws.ShowCommit(context.Background(),log[0].Revision)
	if err != nil {
		t.Fatalf("ShowCommit returned error: %v", err)
	}
	if meta == nil {
		t.Fatal("ShowCommit returned nil metadata")
	}
	if meta.Revision != log[0].Revision {
		t.Errorf("Revision mismatch: got %q, want %q", meta.Revision, log[0].Revision)
	}
	_ = diff // diff content is opaque, just verify no error
}

func TestWikiServiceDiff(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	log, err := ws.Changelog(context.Background(),10)
	if err != nil || len(log) < 2 {
		t.Fatal("Need at least 2 commits for diff test")
	}

	diff, err := ws.Diff(context.Background(),log[1].Revision, log[0].Revision)
	if err != nil {
		t.Fatalf("Diff returned error: %v", err)
	}
	if diff == "" {
		t.Error("Expected non-empty diff")
	}
}

// --- FTS5 search index tests ---

func TestFTS5Search_Basic(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Index pages
	if err := ws.IndexPage(ctx, "home", "# Home\nWelcome to the wiki.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}
	if err := ws.IndexPage(ctx, "about", "# About\nThis is the about page.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}

	// Search for a term in one page
	results, err := ws.Search(ctx, "Welcome")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result for 'Welcome', got %d", len(results))
	}
	if results[0].Pagepath != "home" {
		t.Errorf("Expected pagepath 'home', got %q", results[0].Pagepath)
	}
	if results[0].Pagename != "Home" {
		t.Errorf("Expected pagename 'Home', got %q", results[0].Pagename)
	}
}

func TestFTS5Search_NoResults(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Index a page
	if err := ws.IndexPage(ctx, "home", "# Home\nWelcome to the wiki.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}

	// Search for nonexistent term
	results, err := ws.Search(ctx, "zzzznonexistent99999")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestFTS5Search_EmptyIndexReturnsEmpty(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	// With an unbuilt index, an FTS query that matches nothing returns empty -
	// it must NOT fall back to an O(n) brute-force scan just because the index
	// is empty. Building the index makes the pages findable.
	results, err := ws.Search(ctx, "Welcome")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results from an unbuilt index, got %d", len(results))
	}

	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("EnsureSearchIndex failed: %v", err)
	}
	results, err = ws.Search(ctx, "Welcome")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].Pagepath != "home" {
		t.Fatalf("expected to find home after indexing, got %+v", results)
	}
}

func TestSearch_SanitizesInvalidFTSQuery(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()

	if err := ws.IndexPage(ctx, "lang", "# Lang\nAbout the alpha release.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}

	// "alpha)" is invalid raw FTS5 (unbalanced paren); the sanitized retry
	// should still match the indexed page instead of erroring or scanning.
	results, err := ws.Search(ctx, "alpha)")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 || results[0].Pagepath != "lang" {
		t.Fatalf("expected to find 'lang' via sanitized query, got %+v", results)
	}
}

func TestSanitizeFTSQuery(t *testing.T) {
	cases := map[string]string{
		"hello world": `"hello" "world"`,
		"c++":         `"c++"`,
		`a"b`:         `"a""b"`,
		"":            "",
		"  spaced  ":  `"spaced"`,
	}
	for in, want := range cases {
		if got := sanitizeFTSQuery(in); got != want {
			t.Errorf("sanitizeFTSQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFTS5_IndexAndRemove(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Index a page
	if err := ws.IndexPage(ctx, "testpage", "# Test Page\nUnique keyword xylophone.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}

	// Verify searchable
	results, err := ws.Search(ctx, "xylophone")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 result for 'xylophone', got %d", len(results))
	}

	// Remove from index
	if err := ws.RemovePageFromIndex(ctx, "testpage"); err != nil {
		t.Fatalf("RemovePageFromIndex failed: %v", err)
	}

	// Verify gone from FTS index (brute-force won't find it either since it's not in git)
	results, err = ws.Search(ctx, "xylophone")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results after removal, got %d", len(results))
	}
}

func TestResolveAttachment(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()
	author := storage.Author{Name: "Test", Email: "test@example.com"}

	// "guide" page already exists from the seed; add an attachment in its dir.
	if _, err := ws.store.StoreBytes("guide/diagram.png", []byte("img"), "add", author); err != nil {
		t.Fatalf("StoreBytes failed: %v", err)
	}

	t.Run("existing attachment resolves", func(t *testing.T) {
		path, filename, ok := ws.ResolveAttachment("guide/diagram.png")
		if !ok {
			t.Fatal("expected attachment to resolve")
		}
		if filename != "diagram.png" || path != "guide/diagram.png" {
			t.Errorf("got path=%q filename=%q", path, filename)
		}
	})

	t.Run("missing attachment does not resolve", func(t *testing.T) {
		if _, _, ok := ws.ResolveAttachment("guide/nope.png"); ok {
			t.Error("expected missing attachment not to resolve")
		}
	})

	t.Run("top-level page path does not resolve", func(t *testing.T) {
		if _, _, ok := ws.ResolveAttachment("guide"); ok {
			t.Error("a bare page path should not resolve as an attachment")
		}
	})
}

func TestRebuildIndex_ClearsStaleEntries(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	author := storage.Author{Name: "Test", Email: "test@example.com"}

	// Populate the index from the seeded pages.
	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("EnsureSearchIndex failed: %v", err)
	}
	if results, _ := ws.Search(ctx, "Welcome"); len(results) == 0 {
		t.Fatal("expected to find seeded home page before deletion")
	}

	// Delete a page directly in storage (simulating an out-of-band change that
	// leaves the index stale), then rebuild.
	if err := ws.store.Delete("home.md", "remove home", author); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if err := ws.RebuildIndex(ctx); err != nil {
		t.Fatalf("RebuildIndex failed: %v", err)
	}

	results, err := ws.Search(ctx, "Welcome")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	for _, r := range results {
		if r.Pagepath == "home" {
			t.Error("RebuildIndex should have cleared the deleted home page from the index")
		}
	}
}

func TestRevertRepairsIndex(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	author := storage.Author{Name: "Test", Email: "test@example.com"}

	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("EnsureSearchIndex failed: %v", err)
	}

	// Add a page with a unique word and index it, then confirm it's findable.
	if _, err := ws.SavePage(ctx, "temp", "# Temp\nContains qwertyuiop.\n", "add temp", "", author); err != nil {
		t.Fatalf("SavePage failed: %v", err)
	}
	if results, _ := ws.Search(ctx, "qwertyuiop"); len(results) != 1 {
		t.Fatalf("expected 1 result for the new page, got %d", len(results))
	}

	// Find the commit that added it and revert it.
	commits, err := ws.Changelog(ctx, 1)
	if err != nil || len(commits) == 0 {
		t.Fatalf("Changelog failed: %v", err)
	}
	if err := ws.Revert(ctx, commits[0].Revision, "revert temp", author); err != nil {
		t.Fatalf("Revert failed: %v", err)
	}

	// After revert, the page is gone from git AND the (rebuilt) index.
	results, err := ws.Search(ctx, "qwertyuiop")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected reverted page to be gone from the index, got %d results", len(results))
	}
}

func TestRenamePage_UpdatesIndex(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	author := storage.Author{Name: "Test", Email: "test@example.com"}

	if _, err := ws.SavePage(ctx, "oldname", "# Old Name\nUnique term plutonium.\n", "add", "", author); err != nil {
		t.Fatalf("SavePage failed: %v", err)
	}

	newPath, err := ws.RenamePage(ctx, "oldname", "newname", "rename", author)
	if err != nil {
		t.Fatalf("RenamePage failed: %v", err)
	}
	if newPath != "newname" {
		t.Errorf("newPath = %q, want %q", newPath, "newname")
	}

	results, err := ws.Search(ctx, "plutonium")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result after rename, got %d", len(results))
	}
	if results[0].Pagepath != "newname" {
		t.Errorf("result pagepath = %q, want %q (old entry should be gone, new one present)", results[0].Pagepath, "newname")
	}
}

func TestRenamePage_RewritesInboundLinks(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()
	ctx := context.Background()
	author := storage.Author{Name: "Test", Email: "test@example.com"}

	if _, err := ws.SavePage(ctx, "oldname", "# Old\nbody\n", "add", "", author); err != nil {
		t.Fatalf("SavePage oldname: %v", err)
	}
	if _, err := ws.SavePage(ctx, "linker", "See [[oldname]] and [[oldname|the old page]].\n", "add", "", author); err != nil {
		t.Fatalf("SavePage linker: %v", err)
	}

	// Sanity: the linker is a backlink of the old page.
	if bl, _ := ws.Backlinks(ctx, "oldname"); len(bl) != 1 || bl[0] != "linker" {
		t.Fatalf("pre-rename backlinks(oldname) = %v, want [linker]", bl)
	}

	if _, err := ws.RenamePage(ctx, "oldname", "newname", "rename", author); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	// The referencing page's links were rewritten to the new name.
	content, err := ws.store.Load("linker.md", "")
	if err != nil {
		t.Fatalf("Load linker: %v", err)
	}
	if strings.Contains(content, "[[oldname]]") || strings.Contains(content, "[[oldname|") {
		t.Errorf("linker still references the old name: %q", content)
	}
	if !strings.Contains(content, "[[newname]]") || !strings.Contains(content, "[[newname|the old page]]") {
		t.Errorf("linker not rewritten to new name: %q", content)
	}

	// Backlinks now resolve under the new name, not the old one.
	if bl, _ := ws.Backlinks(ctx, "newname"); len(bl) != 1 || bl[0] != "linker" {
		t.Errorf("post-rename backlinks(newname) = %v, want [linker]", bl)
	}
	if bl, _ := ws.Backlinks(ctx, "oldname"); len(bl) != 0 {
		t.Errorf("post-rename backlinks(oldname) = %v, want []", bl)
	}
}

func TestFTS5_EnsureSearchIndex(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Verify index is empty
	count, err := ws.db.PageIndexCount(ctx)
	if err != nil {
		t.Fatalf("PageIndexCount failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("Expected 0 indexed pages initially, got %d", count)
	}

	// Rebuild from git storage
	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("EnsureSearchIndex failed: %v", err)
	}

	// Verify all pages are indexed (4 .md files in setupTestService)
	count, err = ws.db.PageIndexCount(ctx)
	if err != nil {
		t.Fatalf("PageIndexCount failed: %v", err)
	}
	if count != 4 {
		t.Errorf("Expected 4 indexed pages after rebuild, got %d", count)
	}

	// Verify search works via FTS5 now
	results, err := ws.Search(ctx, "Welcome")
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'Welcome' after rebuild, got %d", len(results))
	}

	// Call EnsureSearchIndex again -- should be a no-op (count > 0)
	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("Second EnsureSearchIndex failed: %v", err)
	}
	count2, _ := ws.db.PageIndexCount(ctx)
	if count2 != count {
		t.Errorf("Second EnsureSearchIndex changed count from %d to %d", count, count2)
	}
}

func TestBacklinks(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()

	// Index pages with wikilinks
	if err := ws.IndexPage(ctx, "home", "# Home\nSee [[about]] and [[guide]].\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}
	if err := ws.IndexPage(ctx, "faq", "# FAQ\nCheck [[about]] for more info.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}
	if err := ws.IndexPage(ctx, "about", "# About\nThis is the about page.\n"); err != nil {
		t.Fatalf("IndexPage failed: %v", err)
	}

	t.Run("backlinks for about", func(t *testing.T) {
		backlinks, err := ws.Backlinks(ctx, "about")
		if err != nil {
			t.Fatalf("Backlinks returned error: %v", err)
		}
		if len(backlinks) != 2 {
			t.Fatalf("Expected 2 backlinks for 'about', got %d: %v", len(backlinks), backlinks)
		}
		// Sorted: faq, home
		if backlinks[0] != "faq" || backlinks[1] != "home" {
			t.Errorf("Expected [faq home], got %v", backlinks)
		}
	})

	t.Run("backlinks for guide", func(t *testing.T) {
		backlinks, err := ws.Backlinks(ctx, "guide")
		if err != nil {
			t.Fatalf("Backlinks returned error: %v", err)
		}
		if len(backlinks) != 1 || backlinks[0] != "home" {
			t.Errorf("Expected [home], got %v", backlinks)
		}
	})

	t.Run("no backlinks", func(t *testing.T) {
		backlinks, err := ws.Backlinks(ctx, "home")
		if err != nil {
			t.Fatalf("Backlinks returned error: %v", err)
		}
		if len(backlinks) != 0 {
			t.Errorf("Expected 0 backlinks for 'home', got %v", backlinks)
		}
	})

	t.Run("remove page removes its outgoing links", func(t *testing.T) {
		if err := ws.RemovePageFromIndex(ctx, "home"); err != nil {
			t.Fatalf("RemovePageFromIndex failed: %v", err)
		}
		backlinks, err := ws.Backlinks(ctx, "about")
		if err != nil {
			t.Fatalf("Backlinks returned error: %v", err)
		}
		if len(backlinks) != 1 || backlinks[0] != "faq" {
			t.Errorf("Expected [faq] after removing home, got %v", backlinks)
		}
	})
}

func TestBacklinks_EnsureSearchIndex(t *testing.T) {
	ws, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	author := storage.Author{Name: "Test", Email: "test@example.com"}

	// Create pages with wikilinks in git storage
	ws.store.Store("links.md", "# Links\nSee [[home]] and [[about]].\n", "Create links", author)

	// EnsureSearchIndex should also populate page_links
	if err := ws.EnsureSearchIndex(ctx); err != nil {
		t.Fatalf("EnsureSearchIndex failed: %v", err)
	}

	backlinks, err := ws.Backlinks(ctx, "home")
	if err != nil {
		t.Fatalf("Backlinks returned error: %v", err)
	}
	if len(backlinks) != 1 || backlinks[0] != "links" {
		t.Errorf("Expected [links] backlink for home, got %v", backlinks)
	}
}
