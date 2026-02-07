package wiki

import (
	"context"
	"log/slog"
	"regexp"
	"strings"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
)

// SearchResult represents a single search result.
type SearchResult struct {
	Pagename   string
	Pagepath   string
	MatchCount int
}

// PageIndexEntry represents a page in the index.
type PageIndexEntry struct {
	Name string
	Path string
}

// WikiService provides higher-level wiki operations on top of Storage.
type WikiService struct {
	store  storage.Storage
	config *config.Config
	db     *db.Database
}

// NewWikiService creates a new WikiService.
func NewWikiService(store storage.Storage, cfg *config.Config, database *db.Database) *WikiService {
	return &WikiService{store: store, config: cfg, db: database}
}

// Search searches all markdown pages for the given query string.
// It tries FTS5 first, falling back to brute-force regex on error.
func (ws *WikiService) Search(query string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	if ws.db != nil {
		ftsResults, err := ws.db.SearchPages(context.Background(), query, 100)
		if err == nil && len(ftsResults) > 0 {
			var results []SearchResult
			for _, r := range ftsResults {
				pagename := r.Title
				if pagename == "" {
					pagename = util.GetPagename(r.Pagepath, false)
				}
				results = append(results, SearchResult{
					Pagename:   pagename,
					Pagepath:   r.Pagepath,
					MatchCount: 1,
				})
			}
			return results, nil
		}
		// On error or empty results, fall through to brute-force
		if err != nil {
			slog.Warn("FTS5 search failed, falling back to brute-force", "error", err)
		}
	}

	return ws.searchBruteForce(query)
}

// searchBruteForce performs an O(n) scan of all pages for the query.
func (ws *WikiService) searchBruteForce(query string) ([]SearchResult, error) {
	files, _, err := ws.store.List("", nil, nil)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))

	var results []SearchResult
	for _, f := range files {
		if !strings.HasSuffix(f, ".md") {
			continue
		}

		content, err := ws.store.Load(f, "")
		if err != nil {
			continue
		}

		matches := re.FindAllStringIndex(content, -1)
		if len(matches) == 0 {
			continue
		}

		pagepath := strings.TrimSuffix(f, ".md")
		pagename := util.GetPagename(pagepath, false)

		if header := util.GetHeader(content); header != "" {
			pagename = header
		}

		results = append(results, SearchResult{
			Pagename:   pagename,
			Pagepath:   pagepath,
			MatchCount: len(matches),
		})
	}

	return results, nil
}

// Backlinks returns all pages that link to the given page.
func (ws *WikiService) Backlinks(ctx context.Context, pagepath string) ([]string, error) {
	if ws.db == nil {
		return nil, nil
	}
	return ws.db.GetBacklinks(ctx, pagepath)
}

// IndexPage adds or updates a page in the FTS5 search index and page links.
func (ws *WikiService) IndexPage(ctx context.Context, pagepath, content string) error {
	if ws.db == nil {
		return nil
	}
	title := util.GetHeader(content)
	if title == "" {
		title = util.GetPagename(pagepath, false)
	}
	if err := ws.db.UpsertPageIndex(ctx, pagepath, title, content); err != nil {
		return err
	}
	targets := renderer.ExtractWikiLinks(content, ws.config.RetainPageNameCase)
	return ws.db.UpsertPageLinks(ctx, pagepath, targets)
}

// RemovePageFromIndex removes a page from the FTS5 search index and page links.
func (ws *WikiService) RemovePageFromIndex(ctx context.Context, pagepath string) error {
	if ws.db == nil {
		return nil
	}
	if err := ws.db.DeletePageIndex(ctx, pagepath); err != nil {
		return err
	}
	return ws.db.DeletePageLinks(ctx, pagepath)
}

// EnsureSearchIndex rebuilds the FTS5 index from git storage if it is empty.
func (ws *WikiService) EnsureSearchIndex(ctx context.Context) error {
	if ws.db == nil {
		return nil
	}

	count, err := ws.db.PageIndexCount(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	files, _, err := ws.store.List("", nil, nil)
	if err != nil {
		return err
	}

	var pages []db.PageIndexData
	var links []db.PageLinkData
	for _, f := range files {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		content, err := ws.store.Load(f, "")
		if err != nil {
			continue
		}
		pagepath := strings.TrimSuffix(f, ".md")
		title := util.GetHeader(content)
		if title == "" {
			title = util.GetPagename(pagepath, false)
		}
		pages = append(pages, db.PageIndexData{
			Pagepath: pagepath,
			Title:    title,
			Content:  content,
		})
		targets := renderer.ExtractWikiLinks(content, ws.config.RetainPageNameCase)
		if len(targets) > 0 {
			links = append(links, db.PageLinkData{
				Source:  pagepath,
				Targets: targets,
			})
		}
	}

	if len(pages) == 0 {
		return nil
	}

	if err := ws.db.RebuildPageIndex(ctx, pages); err != nil {
		return err
	}
	return ws.db.RebuildPageLinks(ctx, links)
}

// Changelog returns recent commit history for the entire repository.
func (ws *WikiService) Changelog(maxCount int) ([]storage.CommitMetadata, error) {
	return ws.store.Log("", maxCount)
}

// PageIndex lists all markdown pages in the repository.
func (ws *WikiService) PageIndex() ([]PageIndexEntry, error) {
	files, _, err := ws.store.List("", nil, nil)
	if err != nil {
		return nil, err
	}

	var pages []PageIndexEntry
	for _, f := range files {
		if !strings.HasSuffix(f, ".md") {
			continue
		}
		pagepath := strings.TrimSuffix(f, ".md")
		pages = append(pages, PageIndexEntry{
			Name: util.GetPagename(pagepath, false),
			Path: pagepath,
		})
	}

	return pages, nil
}

// ShowCommit returns metadata and diff for a specific commit.
func (ws *WikiService) ShowCommit(revision string) (*storage.CommitMetadata, string, error) {
	return ws.store.ShowCommit(revision)
}

// Revert reverts a commit.
func (ws *WikiService) Revert(revision, message string, author storage.Author) error {
	return ws.store.Revert(revision, message, author)
}

// Diff returns the diff between two revisions.
func (ws *WikiService) Diff(revA, revB string) (string, error) {
	return ws.store.Diff(revA, revB)
}
