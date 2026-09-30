package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// Lint checks. All are deterministic; none calls a model.
const (
	lintBrokenLink      = "broken_link"      // a wikilink to a page that does not exist
	lintOrphan          = "orphan"           // no other page links here
	lintUnvalidated     = "unvalidated"      // agent page whose current revision is not validated
	lintNoSources       = "no_sources"       // agent page that cites no report run
	lintMissingSource   = "missing_source"   // cited run does not exist
	lintStaleSource     = "stale_source"     // cited run has a later run of the same period
	lintUnmatchedNumber = "unmatched_number" // number in agent prose found in no cited run
)

// lintIssue is one finding about one page.
type lintIssue struct {
	Page   string `json:"page"`
	Check  string `json:"check"`
	Detail string `json:"detail"`
}

// numberPattern matches a standalone number, with optional thousands
// separators and decimals. A digit inside a word, as in "Q3", does not match.
var numberPattern = regexp.MustCompile(`\b\d[\d,]*(?:\.\d+)?\b`)

// numbers returns the canonical form of each number in text, so "4,500,000"
// matches "4500000" and "08" matches "8".
func numbers(text string) []string {
	var out []string
	for _, m := range numberPattern.FindAllString(text, -1) {
		f, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", ""), 64)
		if err != nil {
			continue
		}
		out = append(out, strconv.FormatFloat(f, 'f', -1, 64))
	}
	return out
}

// lint runs every check and returns the findings, sorted by page and check.
// Pages hidden from the request are left out.
func (s *Server) lint(r *http.Request) ([]lintIssue, error) {
	ctx := r.Context()
	var issues []lintIssue

	pages, err := s.Wiki.PageIndex(ctx)
	if err != nil {
		return nil, err
	}
	exists := map[string]bool{}
	for _, p := range pages {
		exists[s.pageKey(p.Path)] = true
	}

	links, err := s.DB.AllPageLinks(ctx)
	if err != nil {
		return nil, err
	}
	linked := map[string]bool{}
	for _, l := range links {
		source := s.pageKey(l[0])
		target, _, _ := strings.Cut(l[1], "#")
		target = s.pageKey(target)
		if target == "" || target == source {
			continue
		}
		linked[target] = true
		if !exists[target] {
			issues = append(issues, lintIssue{Page: l[0], Check: lintBrokenLink, Detail: "links to missing page " + l[1]})
		}
	}

	// The home page is the entry point; handleIndex uses the same default.
	homePage := s.Config.HomePage
	if homePage == "" {
		homePage = "Home"
	}
	home := s.pageKey(homePage)
	for _, p := range pages {
		if key := s.pageKey(p.Path); !linked[key] && key != home {
			issues = append(issues, lintIssue{Page: p.Path, Check: lintOrphan, Detail: "no other page links here"})
		}
	}

	agentIssues, err := s.lintAgentPages(ctx)
	if err != nil {
		return nil, err
	}
	issues = append(issues, agentIssues...)

	v := s.visibleOrHide(r)
	var visible []lintIssue
	for _, i := range issues {
		if !s.hides(v, i.Page) {
			visible = append(visible, i)
		}
	}
	sort.SliceStable(visible, func(a, b int) bool {
		if visible[a].Page != visible[b].Page {
			return visible[a].Page < visible[b].Page
		}
		return visible[a].Check < visible[b].Check
	})
	return visible, nil
}

// lintAgentPages checks validation and provenance of every agent page.
func (s *Server) lintAgentPages(ctx context.Context) ([]lintIssue, error) {
	files, err := s.DB.Queries.ListAgentPages(ctx)
	if err != nil {
		return nil, err
	}

	var issues []lintIssue
	for _, f := range files {
		page, err := wiki.NewPage(s.Storage, s.Config, util.StripMarkdownExtension(f), "")
		if err != nil {
			return nil, err
		}
		if !page.Exists {
			continue // deleted or renamed
		}
		add := func(check, detail string) {
			issues = append(issues, lintIssue{Page: page.Pagepath, Check: check, Detail: detail})
		}

		state, err := s.validationState(ctx, page)
		if err != nil {
			return nil, err
		}
		if !state.Validated {
			rev := state.Revision
			if len(rev) > 6 {
				rev = rev[:6]
			}
			add(lintUnvalidated, "revision "+rev+" is not validated")
		}

		// Proposed reports are code, not prose citing figures.
		if page.IsComputational {
			continue
		}
		cited, err := s.citedRuns(ctx, page)
		if err != nil {
			return nil, err
		}
		if len(cited) == 0 {
			add(lintNoSources, "cites no report run in its frontmatter sources")
			continue
		}

		known := map[string]bool{}
		for _, c := range cited {
			ref := c.Page + " run " + strconv.FormatInt(c.Run, 10)
			if c.Period == "" {
				add(lintMissingSource, ref+" does not exist")
				continue
			}
			if c.NewerRun != 0 {
				add(lintStaleSource, ref+" is superseded by run "+strconv.FormatInt(c.NewerRun, 10))
			}
			target, err := wiki.NewPage(s.Storage, s.Config, c.Page, "")
			if err != nil {
				return nil, err
			}
			md, err := s.DB.Queries.ReportRunMarkdown(ctx, target.Filename, c.Run)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			for _, n := range numbers(md + " " + c.Period) {
				known[n] = true
			}
		}

		var unmatched []string
		seen := map[string]bool{}
		for _, n := range numbers(page.Body) {
			if !known[n] && !seen[n] {
				seen[n] = true
				unmatched = append(unmatched, n)
			}
		}
		if len(unmatched) > 0 {
			add(lintUnmatchedNumber, "not found in any cited run: "+strings.Join(unmatched, ", "))
		}
	}
	return issues, nil
}

// handleLint shows the lint findings.
func (s *Server) handleLint(w http.ResponseWriter, r *http.Request) {
	issues, err := s.lint(r)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, "Lint failed: "+err.Error())
		return
	}
	data := NewGenericData("Lint")
	data["issues"] = issues
	s.renderTemplate(w, r, "lint.html", data)
}

// handleAPILint handles GET /api/v1/lint.
func (s *Server) handleAPILint(w http.ResponseWriter, r *http.Request) {
	issues, err := s.lint(r)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "lint failed")
		return
	}
	if issues == nil {
		issues = []lintIssue{}
	}
	writeJSON(w, http.StatusOK, issues)
}
