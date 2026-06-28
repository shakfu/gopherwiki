package handlers

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/util"
	"github.com/sa/gopherwiki/internal/wiki"
)

// --- Response envelope ---

type apiResponse struct {
	Data       interface{}    `json:"data,omitempty"`
	Error      string         `json:"error,omitempty"`
	Pagination *apiPagination `json:"pagination,omitempty"`
}

// apiPagination describes the window returned by a list endpoint.
type apiPagination struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// Pagination defaults and bounds for list endpoints.
const (
	defaultAPILimit  = 50
	maxAPILimit      = 200
	maxChangelogScan = 1000 // changelog pagination is within the most recent N commits
)

// paginationParams reads limit/offset query parameters with sane defaults/caps.
func paginationParams(r *http.Request) (limit, offset int) {
	limit = defaultAPILimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxAPILimit {
		limit = maxAPILimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			offset = n
		}
	}
	return limit, offset
}

// paginate returns the [offset, offset+limit) window of items plus metadata.
func paginate[T any](items []T, limit, offset int) ([]T, apiPagination) {
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return items[offset:end], apiPagination{Total: total, Limit: limit, Offset: offset, HasMore: end < total}
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiResponse{Data: data})
}

// writeJSONPaginated writes a list response with pagination metadata.
func writeJSONPaginated(w http.ResponseWriter, status int, data interface{}, p apiPagination) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiResponse{Data: data, Pagination: &p})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(apiResponse{Error: message})
}

func decodeJSON(r *http.Request, dst interface{}) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dst)
}

// --- API data structs ---

// APIPage is the JSON representation of a wiki page.
type APIPage struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	Content  string      `json:"content"`
	Revision string      `json:"revision,omitempty"`
	Exists   bool        `json:"exists"`
	Metadata *APICommit  `json:"metadata,omitempty"`
}

// APICommit is the JSON representation of a commit.
type APICommit struct {
	Revision     string   `json:"revision"`
	RevisionFull string   `json:"revision_full"`
	Datetime     string   `json:"datetime"`
	AuthorName   string   `json:"author_name"`
	AuthorEmail  string   `json:"author_email"`
	Message      string   `json:"message"`
	Files        []string `json:"files,omitempty"`
}

// APISearchResult is the JSON representation of a search result.
type APISearchResult struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Snippet    string `json:"snippet,omitempty"`
	MatchCount int    `json:"match_count"`
}

// APIPageIndex is the JSON representation of a page index entry.
type APIPageIndex struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// APIIssue is the JSON representation of an issue.
type APIIssue struct {
	ID             int64    `json:"id"`
	Title          string   `json:"title"`
	Description    string   `json:"description"`
	Status         string   `json:"status"`
	Category       string   `json:"category"`
	Tags           []string `json:"tags"`
	CreatedByName  string   `json:"created_by_name"`
	CreatedByEmail string   `json:"created_by_email"`
	CreatedAt      string   `json:"created_at"`
	UpdatedAt      string   `json:"updated_at"`
}

// APIIssueInput is the JSON request body for creating/updating issues.
type APIIssueInput struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
}

// APIIssueComment is the JSON representation of an issue comment.
type APIIssueComment struct {
	ID          int64  `json:"id"`
	IssueID     int64  `json:"issue_id"`
	Content     string `json:"content"`
	AuthorName  string `json:"author_name"`
	AuthorEmail string `json:"author_email"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// APIIssueCommentInput is the JSON request body for creating a comment.
type APIIssueCommentInput struct {
	Content string `json:"content"`
}

// APISavePage is the JSON request body for saving a page.
type APISavePage struct {
	Content  string `json:"content"`
	Message  string `json:"message"`
	Revision string `json:"revision"`
}

// --- Conversion helpers ---

func commitToAPI(c *storage.CommitMetadata) *APICommit {
	if c == nil {
		return nil
	}
	return &APICommit{
		Revision:     c.Revision,
		RevisionFull: c.RevisionFull,
		Datetime:     c.Datetime.Format(time.RFC3339),
		AuthorName:   c.AuthorName,
		AuthorEmail:  c.AuthorEmail,
		Message:      c.Message,
		Files:        c.Files,
	}
}

func commitsToAPI(commits []storage.CommitMetadata) []APICommit {
	result := make([]APICommit, 0, len(commits))
	for i := range commits {
		result = append(result, *commitToAPI(&commits[i]))
	}
	return result
}

func pageToAPI(p *wiki.Page) APIPage {
	return APIPage{
		Path:     p.Pagepath,
		Name:     p.Pagename,
		Content:  p.Content,
		Revision: p.Revision,
		Exists:   p.Exists,
		Metadata: commitToAPI(p.Metadata),
	}
}

func searchResultToAPI(r wiki.SearchResult) APISearchResult {
	return APISearchResult{
		Name:       r.Pagename,
		Path:       r.Pagepath,
		Snippet:    r.Snippet,
		MatchCount: r.MatchCount,
	}
}

func pageIndexToAPI(e wiki.PageIndexEntry) APIPageIndex {
	return APIPageIndex{
		Name: e.Name,
		Path: e.Path,
	}
}

func issueToAPI(issue db.Issue) APIIssue {
	return APIIssue{
		ID:             issue.ID,
		Title:          issue.Title,
		Description:    issue.Description.String,
		Status:         issue.Status,
		Category:       issue.Category.String,
		Tags:           util.ParseTags(issue.Tags.String),
		CreatedByName:  issue.CreatedByName.String,
		CreatedByEmail: issue.CreatedByEmail.String,
		CreatedAt:      nullTimeToString(issue.CreatedAt),
		UpdatedAt:      nullTimeToString(issue.UpdatedAt),
	}
}

func issuesToAPI(issues []db.Issue) []APIIssue {
	result := make([]APIIssue, 0, len(issues))
	for _, issue := range issues {
		result = append(result, issueToAPI(issue))
	}
	return result
}

func nullTimeToString(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format(time.RFC3339)
}

func issueCommentToAPI(c *db.IssueComment) APIIssueComment {
	return APIIssueComment{
		ID:          c.ID,
		IssueID:     c.IssueID,
		Content:     c.Content,
		AuthorName:  c.AuthorName.String,
		AuthorEmail: c.AuthorEmail.String,
		CreatedAt:   nullTimeToString(c.CreatedAt),
		UpdatedAt:   nullTimeToString(c.UpdatedAt),
	}
}

func issueCommentsToAPI(comments []db.IssueComment) []APIIssueComment {
	result := make([]APIIssueComment, 0, len(comments))
	for i := range comments {
		result = append(result, issueCommentToAPI(&comments[i]))
	}
	return result
}
