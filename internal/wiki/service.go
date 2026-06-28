package wiki

import (
	"context"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

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
	Snippet    string
	MatchCount int
}

// PageIndexEntry represents a page in the index.
type PageIndexEntry struct {
	Name string
	Path string
}

// PageTreeNode represents a node in the sidebar page tree.
type PageTreeNode struct {
	Name     string
	Path     string
	Children []*PageTreeNode
	IsPage   bool
}

// pageTreeCacheTTL is how long cached PageTree results remain valid.
const pageTreeCacheTTL = 30 * time.Second

// WikiService provides higher-level wiki operations on top of Storage.
type WikiService struct {
	store  storage.Storage
	config *config.Config
	db     *db.Database

	// pageTreeCache caches the page tree to avoid full repo scans on every request.
	ptMu      sync.RWMutex
	ptCache   []*PageTreeNode
	ptCachedAt time.Time
}

// NewWikiService creates a new WikiService.
func NewWikiService(store storage.Storage, cfg *config.Config, database *db.Database) *WikiService {
	return &WikiService{store: store, config: cfg, db: database}
}

// InvalidatePageTreeCache clears the cached page tree, forcing a rebuild on next access.
func (ws *WikiService) InvalidatePageTreeCache() {
	ws.ptMu.Lock()
	ws.ptCachedAt = time.Time{}
	ws.ptMu.Unlock()
}

// Search searches all markdown pages for the given query string.
// It tries FTS5 first, falling back to brute-force regex on error.
func (ws *WikiService) Search(ctx context.Context, query string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	if ws.db != nil {
		ftsResults, err := ws.db.SearchPages(ctx, query, maxSearchResults)
		if err != nil {
			// The raw query may be invalid FTS5 syntax (e.g. contains "+", ":",
			// or an unbalanced quote). Retry it as sanitized quoted terms before
			// resorting to a full-repo brute-force scan.
			ftsResults, err = ws.db.SearchPages(ctx, sanitizeFTSQuery(query), maxSearchResults)
		}
		if err == nil {
			// Note: an empty result set is a valid answer - do NOT fall back to
			// the O(n) brute-force scan just because FTS found nothing.
			results := make([]SearchResult, 0, len(ftsResults))
			for _, r := range ftsResults {
				pagename := r.Title
				if pagename == "" {
					pagename = util.GetPagename(r.Pagepath, false)
				}
				results = append(results, SearchResult{
					Pagename:   pagename,
					Pagepath:   r.Pagepath,
					Snippet:    r.Snippet,
					MatchCount: 1,
				})
			}
			return results, nil
		}
		slog.Warn("FTS5 search failed, falling back to brute-force", "error", err)
	}

	return ws.searchBruteForce(query)
}

// sanitizeFTSQuery turns arbitrary user input into a safe FTS5 MATCH expression
// by quoting each whitespace-separated term as a literal (doubling embedded
// quotes). This neutralizes FTS5 operators/special characters that would
// otherwise cause a syntax error, while preserving implicit AND-of-terms
// matching.
func sanitizeFTSQuery(query string) string {
	fields := strings.Fields(query)
	for i, f := range fields {
		fields[i] = `"` + strings.ReplaceAll(f, `"`, `""`) + `"`
	}
	return strings.Join(fields, " ")
}

// maxSearchResults is the maximum number of search results returned.
const maxSearchResults = 100

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

		if len(results) >= maxSearchResults {
			break
		}
	}

	return results, nil
}

// Page constructs a Page for the given pagepath and revision, loading its
// content and metadata from storage. This is the single entry point for reading
// pages so handlers don't construct pages directly from storage/config.
func (ws *WikiService) Page(pagepath, revision string) (*Page, error) {
	return NewPage(ws.store, ws.config, pagepath, revision)
}

// ResolveAttachment reports whether a view path refers to an existing
// attachment (e.g. "guide/diagram.png" maps to the "guide" page's attachment
// directory) and, if so, returns the storage path and filename to serve. This
// keeps the page-vs-attachment routing rule in the domain layer rather than the
// HTTP handler.
func (ws *WikiService) ResolveAttachment(path string) (attachmentPath, filename string, ok bool) {
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "", "", false
	}
	parentFilename := util.GetFilename(path[:idx])
	if !ws.config.RetainPageNameCase {
		parentFilename = strings.ToLower(parentFilename)
	}
	filename = path[idx+1:]
	attachmentPath = util.GetAttachmentDirectoryname(parentFilename) + "/" + filename
	if !ws.store.Exists(attachmentPath) {
		return "", "", false
	}
	return attachmentPath, filename, true
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
// Used at startup so a fresh database is populated without forcing a rebuild on
// every boot.
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

	return ws.RebuildIndex(ctx)
}

// RebuildIndex unconditionally rebuilds the FTS5 search index and backlink graph
// from git storage. It is the single source of truth for repairing derived
// state and is invoked at startup (when empty), after a revert, and by the admin
// reindex action. Because it replaces the whole index, it also clears entries
// for pages that no longer exist.
func (ws *WikiService) RebuildIndex(ctx context.Context) error {
	if ws.db == nil {
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

	if err := ws.db.RebuildPageIndex(ctx, pages); err != nil {
		return err
	}
	return ws.db.RebuildPageLinks(ctx, links)
}

// Changelog returns recent commit history for the entire repository.
func (ws *WikiService) Changelog(ctx context.Context, maxCount int) ([]storage.CommitMetadata, error) {
	return ws.store.Log("", maxCount)
}

// PageIndex lists all markdown pages in the repository.
func (ws *WikiService) PageIndex(ctx context.Context) ([]PageIndexEntry, error) {
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

// PageTree builds a hierarchical tree of all pages for sidebar navigation.
// Results are cached with a short TTL to avoid scanning the repo on every request.
func (ws *WikiService) PageTree(ctx context.Context) ([]*PageTreeNode, error) {
	ws.ptMu.RLock()
	if ws.ptCache != nil && time.Since(ws.ptCachedAt) < pageTreeCacheTTL {
		cached := ws.ptCache
		ws.ptMu.RUnlock()
		return cached, nil
	}
	ws.ptMu.RUnlock()

	tree, err := ws.buildPageTree(ctx)
	if err != nil {
		return nil, err
	}

	ws.ptMu.Lock()
	ws.ptCache = tree
	ws.ptCachedAt = time.Now()
	ws.ptMu.Unlock()

	return tree, nil
}

// buildPageTree constructs the page tree from scratch.
func (ws *WikiService) buildPageTree(ctx context.Context) ([]*PageTreeNode, error) {
	entries, err := ws.PageIndex(ctx)
	if err != nil {
		return nil, err
	}

	root := &PageTreeNode{}
	nodeMap := map[string]*PageTreeNode{"": root}

	// Ensure parent nodes exist for every path segment.
	ensureNode := func(pathStr string) *PageTreeNode {
		if n, ok := nodeMap[pathStr]; ok {
			return n
		}
		parts := strings.Split(pathStr, "/")
		current := root
		for i, part := range parts {
			key := strings.Join(parts[:i+1], "/")
			if child, ok := nodeMap[key]; ok {
				current = child
			} else {
				child := &PageTreeNode{Name: part, Path: key}
				current.Children = append(current.Children, child)
				nodeMap[key] = child
				current = child
			}
		}
		return current
	}

	for _, e := range entries {
		node := ensureNode(e.Path)
		node.Name = e.Name
		node.IsPage = true
	}

	// Sort children alphabetically at every level.
	var sortTree func(nodes []*PageTreeNode)
	sortTree = func(nodes []*PageTreeNode) {
		sort.Slice(nodes, func(i, j int) bool {
			return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
		})
		for _, n := range nodes {
			if len(n.Children) > 0 {
				sortTree(n.Children)
			}
		}
	}
	sortTree(root.Children)

	return root.Children, nil
}

// ShowCommit returns metadata and diff for a specific commit.
func (ws *WikiService) ShowCommit(ctx context.Context, revision string) (*storage.CommitMetadata, string, error) {
	return ws.store.ShowCommit(revision)
}

// Revert reverts a commit and repairs derived state. A revert can touch many
// files at once, so the whole search index and backlink graph are rebuilt and
// the page-tree cache invalidated to keep search, backlinks, and the sidebar
// consistent with the reverted content.
func (ws *WikiService) Revert(ctx context.Context, revision, message string, author storage.Author) error {
	if err := ws.store.Revert(revision, message, author); err != nil {
		return err
	}
	if err := ws.RebuildIndex(ctx); err != nil {
		slog.Warn("failed to rebuild search index after revert", "error", err)
	}
	ws.InvalidatePageTreeCache()
	return nil
}

// SavePageResult holds the outcome of a SavePage operation.
type SavePageResult struct {
	Page     *Page
	Changed  bool
	IsNew    bool
	Conflict bool
}

// SavePage saves a wiki page with conflict detection and search indexing.
// If baseRevision is non-empty and doesn't match the current HEAD revision,
// a conflict is returned without saving.
func (ws *WikiService) SavePage(ctx context.Context, pagepath, content, message, baseRevision string, author storage.Author) (*SavePageResult, error) {
	page, err := NewPage(ws.store, ws.config, pagepath, "")
	if err != nil {
		return nil, err
	}

	// Optimistic locking: reject saves where the base revision no longer matches HEAD
	if baseRevision != "" && page.Exists && page.Metadata != nil && page.Metadata.Revision != baseRevision {
		return &SavePageResult{Page: page, Conflict: true}, nil
	}

	isNew := !page.Exists

	if message == "" {
		if isNew {
			message = "Created " + page.Pagename
		} else {
			message = "Updated " + page.Pagename
		}
	}

	changed, err := page.Save(content, message, author)
	if err != nil {
		return nil, err
	}

	if err := ws.IndexPage(ctx, page.Pagepath, content); err != nil {
		slog.Warn("failed to index page", "path", page.Pagepath, "error", err)
	}

	if changed {
		ws.InvalidatePageTreeCache()
	}

	return &SavePageResult{Page: page, Changed: changed, IsNew: isNew}, nil
}

// DeletePage deletes a wiki page (and its attachments) and removes it from the search index.
func (ws *WikiService) DeletePage(ctx context.Context, pagepath, message string, author storage.Author) error {
	page, err := NewPage(ws.store, ws.config, pagepath, "")
	if err != nil {
		return err
	}

	if !page.Exists {
		return storage.ErrNotFound
	}

	if message == "" {
		message = page.Pagename + " deleted."
	}

	if err := page.Delete(message, author, true); err != nil {
		return err
	}

	if err := ws.RemovePageFromIndex(ctx, page.Pagepath); err != nil {
		slog.Warn("failed to remove page from index", "path", page.Pagepath, "error", err)
	}

	ws.InvalidatePageTreeCache()

	return nil
}

// RenamePage renames a page (and its attachments) and keeps the derived state
// consistent: the old page is dropped from the search index and backlink graph,
// the new page is indexed, inbound [[wikilinks]] in pages that referenced the
// old name are rewritten to the new name (so links and backlinks stay valid),
// and the page-tree cache is invalidated. It returns the normalized pagepath of
// the renamed page.
func (ws *WikiService) RenamePage(ctx context.Context, pagepath, newPagename, message string, author storage.Author) (string, error) {
	page, err := NewPage(ws.store, ws.config, pagepath, "")
	if err != nil {
		return "", err
	}

	if message == "" {
		message = "Renamed " + page.Pagename + " to " + newPagename
	}

	// Capture pages linking to the old name before the index is altered.
	backlinks, blErr := ws.Backlinks(ctx, pagepath)
	if blErr != nil {
		slog.Warn("failed to load backlinks for rename", "path", pagepath, "error", blErr)
	}

	if err := page.Rename(newPagename, message, author); err != nil {
		return "", err
	}

	// Maintain derived state for the renamed page itself.
	if err := ws.RemovePageFromIndex(ctx, pagepath); err != nil {
		slog.Warn("failed to remove old page from index", "path", pagepath, "error", err)
	}

	newPath := newPagename
	if newPage, err := NewPage(ws.store, ws.config, newPagename, ""); err == nil {
		newPath = newPage.Pagepath
		if newPage.Exists {
			if err := ws.IndexPage(ctx, newPage.Pagepath, newPage.Content); err != nil {
				slog.Warn("failed to index renamed page", "path", newPage.Pagepath, "error", err)
			}
		}
	}

	ws.rewriteInboundLinks(ctx, backlinks, pagepath, newPagename, page.Pagename, author)

	ws.InvalidatePageTreeCache()
	return newPath, nil
}

// rewriteInboundLinks updates [[oldPath]] wikilinks to point at newName in each
// of the given source pages, committing and re-indexing the ones that change.
func (ws *WikiService) rewriteInboundLinks(ctx context.Context, sources []string, oldPath, newName, oldName string, author storage.Author) {
	for _, src := range sources {
		if src == oldPath {
			continue // the renamed page itself was already moved
		}
		srcPage, err := NewPage(ws.store, ws.config, src, "")
		if err != nil || !srcPage.Exists {
			continue
		}
		newContent, changed := renderer.RewriteWikiLinks(srcPage.Content, oldPath, newName, ws.config.RetainPageNameCase)
		if !changed {
			continue
		}
		msg := "Update links: " + oldName + " renamed to " + newName
		if _, err := srcPage.Save(newContent, msg, author); err != nil {
			slog.Warn("failed to rewrite links in referencing page", "path", src, "error", err)
			continue
		}
		if err := ws.IndexPage(ctx, srcPage.Pagepath, newContent); err != nil {
			slog.Warn("failed to reindex referencing page", "path", src, "error", err)
		}
	}
}

// Diff returns the diff between two revisions.
func (ws *WikiService) Diff(ctx context.Context, revA, revB string) (string, error) {
	return ws.store.Diff(revA, revB)
}
