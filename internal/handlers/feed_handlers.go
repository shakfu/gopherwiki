package handlers

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"time"
)

// handleFeed handles the RSS feed.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	changelog, err := s.Wiki.Changelog(20)
	if err != nil {
		slog.Warn("failed to get changelog for feed", "error", err)
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
<channel>
<title>%s</title>
<link>%s</link>
<description>Recent changes</description>
`, html.EscapeString(s.Config.SiteName), s.Config.SiteURL)

	for _, entry := range changelog {
		fmt.Fprintf(w, `<item>
<title>%s</title>
<link>%s/-/commit/%s</link>
<pubDate>%s</pubDate>
<author>%s</author>
</item>
`, html.EscapeString(entry.Message), s.Config.SiteURL, entry.Revision, entry.Datetime.Format(time.RFC1123Z), html.EscapeString(entry.AuthorEmail))
	}

	fmt.Fprint(w, `</channel>
</rss>`)
}

// handleAtomFeed handles the Atom feed.
func (s *Server) handleAtomFeed(w http.ResponseWriter, r *http.Request) {
	changelog, err := s.Wiki.Changelog(20)
	if err != nil {
		slog.Warn("failed to get changelog for feed", "error", err)
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<title>%s</title>
<link href="%s"/>
<id>%s/</id>
`, html.EscapeString(s.Config.SiteName), s.Config.SiteURL, s.Config.SiteURL)

	if len(changelog) > 0 {
		fmt.Fprintf(w, `<updated>%s</updated>
`, changelog[0].Datetime.Format(time.RFC3339))
	}

	for _, entry := range changelog {
		fmt.Fprintf(w, `<entry>
<title>%s</title>
<link href="%s/-/commit/%s"/>
<id>%s/-/commit/%s</id>
<updated>%s</updated>
<author><name>%s</name></author>
</entry>
`, html.EscapeString(entry.Message), s.Config.SiteURL, entry.Revision, s.Config.SiteURL, entry.Revision, entry.Datetime.Format(time.RFC3339), html.EscapeString(entry.AuthorName))
	}

	fmt.Fprint(w, `</feed>`)
}

// handleRobotsTxt handles the robots.txt file.
func (s *Server) handleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, `User-agent: *
Allow: /
Sitemap: %s/-/sitemap.xml
`, s.Config.SiteURL)
}

// handleSitemap handles the sitemap.xml file.
func (s *Server) handleSitemap(w http.ResponseWriter, r *http.Request) {
	pages, err := s.Wiki.PageIndex()
	if err != nil {
		slog.Warn("failed to get page index for sitemap", "error", err)
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
`)

	for _, page := range pages {
		filename := page.Path + ".md"
		mtime, err := s.Storage.Mtime(filename)
		if err != nil {
			mtime = time.Time{}
		}
		fmt.Fprintf(w, `<url>
<loc>%s/%s</loc>
<lastmod>%s</lastmod>
</url>
`, s.Config.SiteURL, page.Path, mtime.Format("2006-01-02"))
	}

	fmt.Fprint(w, `</urlset>`)
}
