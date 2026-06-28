// Package issues provides the business logic for the issue tracker, shared by
// the HTML and JSON API handlers so validation, lookups, and mutations live in
// one place instead of being duplicated across handler families.
package issues

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/util"
)

// Sentinel errors. Handlers map these to HTTP status codes (404, 400).
var (
	ErrNotFound        = errors.New("issue not found")
	ErrCommentNotFound = errors.New("comment not found")
)

// ValidationError is returned when user input fails validation. Handlers render
// its message to the user (HTTP 400 / flash message).
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

const (
	tagsPreferenceKey       = "issue_tags"
	categoriesPreferenceKey = "issue_categories"
)

// Service provides issue-tracker operations on top of the database.
type Service struct {
	db     *db.Database
	config *config.Config
}

// NewService creates a new issue Service.
func NewService(database *db.Database, cfg *config.Config) *Service {
	return &Service{db: database, config: cfg}
}

// Author identifies who created an issue or comment.
type Author struct {
	Name  string
	Email string
}

func (a Author) name() string {
	if a.Name == "" {
		return "Anonymous"
	}
	return a.Name
}

// Filter describes optional issue list filters.
type Filter struct {
	Status   string // "open" or "closed"; anything else means all
	Category string
	Tag      string
}

// Input holds the editable fields of an issue.
type Input struct {
	Title       string
	Description string
	Category    string
	Tags        []string
}

// List returns issues matching the filter. Status is applied in SQL; category
// and tag are applied in memory because tags are stored as a comma-separated
// string. (Pushing these into SQL is a future optimization.)
func (s *Service) List(ctx context.Context, f Filter) ([]db.Issue, error) {
	var (
		issues []db.Issue
		err    error
	)
	if f.Status == "open" || f.Status == "closed" {
		issues, err = s.db.Queries.ListIssuesByStatus(ctx, f.Status)
	} else {
		issues, err = s.db.Queries.ListIssues(ctx)
	}
	if err != nil {
		return nil, err
	}

	if f.Category != "" {
		filtered := issues[:0:0]
		for _, issue := range issues {
			if issue.Category.Valid && issue.Category.String == f.Category {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	if f.Tag != "" {
		filtered := issues[:0:0]
		for _, issue := range issues {
			if issue.Tags.Valid && util.ContainsTag(issue.Tags.String, f.Tag) {
				filtered = append(filtered, issue)
			}
		}
		issues = filtered
	}

	return issues, nil
}

// Counts returns the number of open and closed issues.
func (s *Service) Counts(ctx context.Context) (open, closed int64, err error) {
	open, err = s.db.Queries.CountIssuesByStatus(ctx, "open")
	if err != nil {
		return 0, 0, err
	}
	closed, err = s.db.Queries.CountIssuesByStatus(ctx, "closed")
	if err != nil {
		return 0, 0, err
	}
	return open, closed, nil
}

// Get returns a single issue, or ErrNotFound.
func (s *Service) Get(ctx context.Context, id int64) (db.Issue, error) {
	issue, err := s.db.Queries.GetIssue(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return db.Issue{}, ErrNotFound
	}
	return issue, err
}

// Create validates and creates a new issue.
func (s *Service) Create(ctx context.Context, in Input, author Author) (db.Issue, error) {
	if err := s.validate(ctx, in); err != nil {
		return db.Issue{}, err
	}

	now := time.Now()
	return s.db.Queries.CreateIssue(ctx, db.CreateIssueParams{
		Title:          strings.TrimSpace(in.Title),
		Description:    db.NullString(in.Description),
		Status:         "open",
		Category:       db.NullString(strings.TrimSpace(in.Category)),
		Tags:           db.NullString(strings.Join(in.Tags, ",")),
		CreatedByName:  db.NullString(author.name()),
		CreatedByEmail: db.NullString(author.Email),
		CreatedAt:      db.NullTime(now),
		UpdatedAt:      db.NullTime(now),
	})
}

// Update validates and updates an issue's editable fields, preserving status.
func (s *Service) Update(ctx context.Context, id int64, in Input) (db.Issue, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return db.Issue{}, err
	}
	if err := s.validate(ctx, in); err != nil {
		return db.Issue{}, err
	}

	if err := s.db.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
		Title:       strings.TrimSpace(in.Title),
		Description: db.NullString(in.Description),
		Status:      existing.Status,
		Category:    db.NullString(strings.TrimSpace(in.Category)),
		Tags:        db.NullString(strings.Join(in.Tags, ",")),
		UpdatedAt:   db.NullTime(time.Now()),
		ID:          id,
	}); err != nil {
		return db.Issue{}, err
	}
	return s.Get(ctx, id)
}

// SetStatus changes an issue's status (e.g. "open"/"closed"), preserving other
// fields.
func (s *Service) SetStatus(ctx context.Context, id int64, status string) (db.Issue, error) {
	existing, err := s.Get(ctx, id)
	if err != nil {
		return db.Issue{}, err
	}

	if err := s.db.Queries.UpdateIssue(ctx, db.UpdateIssueParams{
		Title:       existing.Title,
		Description: existing.Description,
		Status:      status,
		Category:    existing.Category,
		Tags:        existing.Tags,
		UpdatedAt:   db.NullTime(time.Now()),
		ID:          id,
	}); err != nil {
		return db.Issue{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes an issue (cascading to its comments via the DB FK).
func (s *Service) Delete(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.db.Queries.DeleteIssue(ctx, id)
}

// validate enforces required fields: a title is always required, and a category
// is required when categories are configured.
func (s *Service) validate(ctx context.Context, in Input) error {
	if strings.TrimSpace(in.Title) == "" {
		return &ValidationError{Message: "Title is required"}
	}
	if len(s.AvailableCategories(ctx)) > 0 && strings.TrimSpace(in.Category) == "" {
		return &ValidationError{Message: "Category is required"}
	}
	return nil
}

// AvailableTags returns the configured tags (runtime preference, else config).
func (s *Service) AvailableTags(ctx context.Context) []string {
	return s.preferenceList(ctx, tagsPreferenceKey, s.config.IssueTags)
}

// AvailableCategories returns the configured categories (preference, else config).
func (s *Service) AvailableCategories(ctx context.Context) []string {
	return s.preferenceList(ctx, categoriesPreferenceKey, s.config.IssueCategories)
}

func (s *Service) preferenceList(ctx context.Context, key, fallback string) []string {
	if pref, err := s.db.Queries.GetPreference(ctx, key); err == nil && pref.Value.Valid && pref.Value.String != "" {
		return util.ParseTags(pref.Value.String)
	}
	return util.ParseTags(fallback)
}

// --- Comments ---

// ListComments returns the comments on an issue, or ErrNotFound if the issue
// does not exist.
func (s *Service) ListComments(ctx context.Context, issueID int64) ([]db.IssueComment, error) {
	if _, err := s.Get(ctx, issueID); err != nil {
		return nil, err
	}
	return s.db.Queries.ListIssueComments(ctx, issueID)
}

// CreateComment validates and creates a comment on an issue.
func (s *Service) CreateComment(ctx context.Context, issueID int64, content string, author Author) (db.IssueComment, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return db.IssueComment{}, &ValidationError{Message: "Comment cannot be empty"}
	}
	if _, err := s.Get(ctx, issueID); err != nil {
		return db.IssueComment{}, err
	}

	now := time.Now()
	return s.db.Queries.CreateIssueComment(ctx, db.CreateIssueCommentParams{
		IssueID:     issueID,
		Content:     content,
		AuthorName:  db.NullString(author.name()),
		AuthorEmail: db.NullString(author.Email),
		CreatedAt:   db.NullTime(now),
		UpdatedAt:   db.NullTime(now),
	})
}

// GetComment returns a single comment, or ErrCommentNotFound.
func (s *Service) GetComment(ctx context.Context, commentID int64) (db.IssueComment, error) {
	comment, err := s.db.Queries.GetIssueComment(ctx, commentID)
	if errors.Is(err, sql.ErrNoRows) {
		return db.IssueComment{}, ErrCommentNotFound
	}
	return comment, err
}

// DeleteComment removes a comment, returning ErrCommentNotFound if absent.
func (s *Service) DeleteComment(ctx context.Context, commentID int64) error {
	if _, err := s.GetComment(ctx, commentID); err != nil {
		return err
	}
	return s.db.Queries.DeleteIssueComment(ctx, commentID)
}
