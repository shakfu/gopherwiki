// Package wiki provides wiki page operations.
package wiki

import (
	"log/slog"
	"strings"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/renderer"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
)

// Page represents a wiki page.
type Page struct {
	Pagepath              string
	Pagename              string
	PagenameFull          string
	Filename              string
	AttachmentDirectoryname string
	Revision              string
	Content               string
	Metadata              *storage.CommitMetadata
	Exists                bool
	PageViewURL           string

	store  storage.Storage
	config *config.Config
}

// NewPage creates a new Page object.
func NewPage(store storage.Storage, cfg *config.Config, pagepath string, revision string) (*Page, error) {
	pagepath = util.SanitizePagename(pagepath, true)
	pagename := util.GetPagename(pagepath, false)
	pagenameFull := util.GetPagename(pagepath, true)

	// Handle case sensitivity
	filename := util.GetFilename(pagepath)
	if !cfg.RetainPageNameCase {
		filename = strings.ToLower(filename)
	}

	p := &Page{
		Pagepath:              pagepath,
		Pagename:              pagename,
		PagenameFull:          pagenameFull,
		Filename:              filename,
		AttachmentDirectoryname: util.GetAttachmentDirectoryname(filename),
		Revision:              revision,
		PageViewURL:           "/" + pagepath,
		store:                 store,
		config:                cfg,
	}

	// Load content and metadata
	if err := p.load(); err != nil {
		if err == storage.ErrNotFound {
			p.Exists = false
		} else {
			return nil, err
		}
	} else {
		p.Exists = true

		// Update pagename from header if content exists
		if p.Content != "" {
			header := util.GetHeader(p.Content)
			if header != "" {
				p.Pagename = util.GetPagenameForTitle(p.Pagepath, false, header)
				p.PagenameFull = util.GetPagenameForTitle(p.Pagepath, true, header)
			}
		}
	}

	return p, nil
}

// load loads the page content and metadata from storage.
func (p *Page) load() error {
	content, err := p.store.Load(p.Filename, p.Revision)
	if err != nil {
		return err
	}
	p.Content = content

	metadata, err := p.store.Metadata(p.Filename, p.Revision)
	if err != nil {
		// Page exists but not in git (e.g., untracked file)
		return nil
	}
	p.Metadata = metadata

	return nil
}

// Breadcrumbs returns the navigation breadcrumbs for this page.
func (p *Page) Breadcrumbs() []util.Breadcrumb {
	return util.GetBreadcrumbs(p.Pagepath)
}

// Render renders the page content to HTML.
func (p *Page) Render(r *renderer.Renderer) (string, []renderer.TOCEntry, renderer.LibraryRequirements) {
	if p.Content == "" {
		return "", nil, renderer.LibraryRequirements{}
	}
	return r.Render(p.Content, p.PageViewURL)
}

// Save saves the page content.
func (p *Page) Save(content, message string, author storage.Author) (bool, error) {
	changed, err := p.store.Store(p.Filename, content, message, author)
	if err != nil {
		return false, err
	}
	if changed {
		p.Content = content
		p.Exists = true
	}
	return changed, nil
}

// Delete deletes the page and optionally its attachments.
func (p *Page) Delete(message string, author storage.Author, recursive bool) error {
	var files []string

	if p.Exists {
		files = append(files, p.Filename)
	}

	if recursive {
		// Delete attachment directory
		if p.store.IsDir(p.AttachmentDirectoryname) && !p.store.IsEmptyDir(p.AttachmentDirectoryname) {
			files = append(files, p.AttachmentDirectoryname)
		}
	}

	if len(files) == 0 {
		return nil
	}

	// Delete each file
	for _, f := range files {
		if err := p.store.Delete(f, message, author); err != nil {
			return err
		}
	}

	return nil
}

// Rename renames the page.
func (p *Page) Rename(newPagename, message string, author storage.Author) error {
	newFilename := util.GetFilename(newPagename)
	if !p.config.RetainPageNameCase {
		newFilename = strings.ToLower(newFilename)
	}

	// Check for attachments
	files, dirs, err := p.store.List(p.AttachmentDirectoryname, nil, nil)
	if err != nil {
		slog.Warn("failed to list attachments", "directory", p.AttachmentDirectoryname, "error", err)
	}
	hasAttachments := len(files)+len(dirs) > 0

	if hasAttachments {
		newAttachmentDir := util.GetAttachmentDirectoryname(newFilename)
		if err := p.store.Rename(p.AttachmentDirectoryname, newAttachmentDir, message, author); err != nil {
			return err
		}
	}

	if p.Exists {
		if err := p.store.Rename(p.Filename, newFilename, message, author); err != nil {
			return err
		}
	}

	return nil
}

// History returns the commit history for this page.
func (p *Page) History(maxCount int) ([]storage.CommitMetadata, error) {
	return p.store.Log(p.Filename, maxCount)
}

// Blame returns blame information for this page.
func (p *Page) Blame() ([]storage.BlameLine, error) {
	return p.store.Blame(p.Filename, p.Revision)
}

// Attachments returns the attachments for this page.
func (p *Page) Attachments(maxCount int, excludeExtensions string) ([]Attachment, error) {
	depth := 0
	files, _, err := p.store.List(p.AttachmentDirectoryname, &depth, nil)
	if err != nil {
		return nil, err
	}

	var attachments []Attachment
	for _, f := range files {
		if excludeExtensions != "" && strings.HasSuffix(f, excludeExtensions) {
			continue
		}
		if maxCount > 0 && len(attachments) >= maxCount {
			break
		}

		a := NewAttachment(p.store, p.Pagepath, f, "")
		attachments = append(attachments, *a)
	}

	return attachments, nil
}

// Attachment represents a file attached to a page.
type Attachment struct {
	Pagepath   string
	Filename   string
	Filepath   string
	Fullpath   string
	Directory  string
	Revision   string
	Mimetype   string
	Metadata   *storage.CommitMetadata

	store storage.Storage
}

// NewAttachment creates a new Attachment. Metadata is not loaded eagerly;
// call LoadMetadata() if you need commit information.
func NewAttachment(store storage.Storage, pagepath, filename, revision string) *Attachment {
	pageFilename := util.GetFilename(pagepath)
	directory := util.GetAttachmentDirectoryname(pageFilename)
	filepath := directory + "/" + filename

	return &Attachment{
		Pagepath:  pagepath,
		Filename:  filename,
		Filepath:  filepath,
		Fullpath:  pagepath + "/" + filename,
		Directory: directory,
		Revision:  revision,
		Mimetype:  util.GuessMimetype(filename),
		store:     store,
	}
}

// LoadMetadata explicitly loads commit metadata for this attachment.
func (a *Attachment) LoadMetadata() error {
	meta, err := a.store.Metadata(a.Filepath, a.Revision)
	if err != nil {
		return err
	}
	a.Metadata = meta
	return nil
}

// Exists checks if the attachment exists.
func (a *Attachment) Exists() bool {
	return a.store.Exists(a.Filepath)
}

// Load loads the attachment content.
func (a *Attachment) Load() ([]byte, error) {
	return a.store.LoadBytes(a.Filepath, a.Revision)
}
