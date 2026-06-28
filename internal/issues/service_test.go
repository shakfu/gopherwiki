package issues

import (
	"context"
	"errors"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	database, err := db.Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Default()
	return NewService(database, cfg)
}

func TestCreateAndGet(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()

	issue, err := s.Create(ctx, Input{Title: "First", Description: "body", Tags: []string{"bug"}}, Author{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if issue.CreatedByName.String != "Anonymous" {
		t.Errorf("author name = %q, want Anonymous fallback", issue.CreatedByName.String)
	}

	got, err := s.Get(ctx, issue.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "First" {
		t.Errorf("title = %q, want First", got.Title)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := newTestService(t)
	if _, err := s.Get(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestCreate_TitleRequired(t *testing.T) {
	s := newTestService(t)
	_, err := s.Create(context.Background(), Input{Title: "   "}, Author{})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want ValidationError", err)
	}
}

func TestCreate_CategoryRequiredWhenConfigured(t *testing.T) {
	s := newTestService(t)
	s.config.IssueCategories = "Bug,Feature"
	ctx := context.Background()

	// Missing category is rejected.
	if _, err := s.Create(ctx, Input{Title: "x"}, Author{}); err == nil {
		t.Fatal("expected category-required validation error")
	}
	// Providing a category succeeds.
	if _, err := s.Create(ctx, Input{Title: "x", Category: "Bug"}, Author{}); err != nil {
		t.Fatalf("Create with category: %v", err)
	}
}

func TestSetStatus(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	issue, _ := s.Create(ctx, Input{Title: "x"}, Author{})

	closed, err := s.SetStatus(ctx, issue.ID, "closed")
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if closed.Status != "closed" {
		t.Errorf("status = %q, want closed", closed.Status)
	}

	if _, err := s.SetStatus(ctx, 999, "closed"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetStatus on missing issue err = %v, want ErrNotFound", err)
	}
}

func TestListFilters(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	a, _ := s.Create(ctx, Input{Title: "open-bug", Tags: []string{"bug"}}, Author{})
	s.Create(ctx, Input{Title: "open-feature", Tags: []string{"feature"}}, Author{})
	s.SetStatus(ctx, a.ID, "closed")

	if got, _ := s.List(ctx, Filter{Status: "closed"}); len(got) != 1 || got[0].Title != "open-bug" {
		t.Errorf("status filter: got %+v", got)
	}
	if got, _ := s.List(ctx, Filter{Tag: "feature"}); len(got) != 1 || got[0].Title != "open-feature" {
		t.Errorf("tag filter: got %+v", got)
	}
	if got, _ := s.List(ctx, Filter{}); len(got) != 2 {
		t.Errorf("no filter: got %d, want 2", len(got))
	}
}

func TestComments(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	issue, _ := s.Create(ctx, Input{Title: "x"}, Author{})

	// Comment on a missing issue is not found.
	if _, err := s.CreateComment(ctx, 999, "hi", Author{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("comment on missing issue err = %v, want ErrNotFound", err)
	}
	// Empty comment is rejected.
	if _, err := s.CreateComment(ctx, issue.ID, "  ", Author{}); err == nil {
		t.Error("expected validation error for empty comment")
	}

	c, err := s.CreateComment(ctx, issue.ID, "hello", Author{Name: "Bob"})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if list, _ := s.ListComments(ctx, issue.ID); len(list) != 1 {
		t.Errorf("ListComments len = %d, want 1", len(list))
	}
	if err := s.DeleteComment(ctx, c.ID); err != nil {
		t.Fatalf("DeleteComment: %v", err)
	}
	if err := s.DeleteComment(ctx, c.ID); !errors.Is(err, ErrCommentNotFound) {
		t.Errorf("second delete err = %v, want ErrCommentNotFound", err)
	}
}

func TestDeleteCascadesComments(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	issue, _ := s.Create(ctx, Input{Title: "x"}, Author{})
	s.CreateComment(ctx, issue.ID, "a", Author{})

	if err := s.Delete(ctx, issue.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, issue.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("issue still present after delete")
	}
}
