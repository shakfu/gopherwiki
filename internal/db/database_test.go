package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *Database {
	t.Helper()
	database, err := Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return database
}

func TestOpen_InMemory(t *testing.T) {
	database, err := Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	database.Close()
}

func TestOpen_InvalidPath(t *testing.T) {
	_, err := Open("sqlite:///nonexistent/deeply/nested/path/db.sqlite")
	if err == nil {
		t.Error("Open() should fail for invalid path")
	}
}

func TestMigrate(t *testing.T) {
	database := openTestDB(t)

	// Verify tables exist by querying them
	tables := []string{"preferences", "user", "drafts", "cache", "issues"}
	for _, table := range tables {
		var count int
		err := database.Conn().QueryRowContext(context.Background(),
			"SELECT COUNT(*) FROM "+table).Scan(&count)
		if err != nil {
			t.Errorf("table %q should exist after migrate: %v", table, err)
		}
	}
}

func TestSchemaVersion(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	version, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion failed: %v", err)
	}
	// Should be at the latest migration version (currently 4)
	if version != 4 {
		t.Errorf("SchemaVersion = %d, want 4", version)
	}
}

func TestMigrateIdempotent(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	// Run Migrate a second time -- should be a no-op
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}

	version, err := database.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion failed: %v", err)
	}
	if version != 4 {
		t.Errorf("SchemaVersion after re-migrate = %d, want 4", version)
	}
}

func TestMigrateCreatesExpectedTables(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	// Verify migration-created tables exist
	migrationTables := []string{"page_fts", "page_links", "schema_version"}
	for _, table := range migrationTables {
		var count int
		err := database.Conn().QueryRowContext(ctx,
			"SELECT COUNT(*) FROM "+table).Scan(&count)
		if err != nil {
			t.Errorf("table %q should exist after migrations: %v", table, err)
		}
	}

	// Verify issues.category column exists
	var count int
	err := database.Conn().QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pragma_table_info('issues') WHERE name='category'").Scan(&count)
	if err != nil {
		t.Fatalf("pragma_table_info query failed: %v", err)
	}
	if count != 1 {
		t.Error("issues table should have 'category' column after migration 3")
	}
}

func TestCreateUser(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	now := sql.NullTime{Time: time.Now(), Valid: true}
	params := CreateUserParams{
		Name:           "Alice",
		Email:          "alice@example.com",
		PasswordHash:   sql.NullString{String: "hash123", Valid: true},
		FirstSeen:      now,
		LastSeen:       now,
		IsApproved:     sql.NullBool{Bool: true, Valid: true},
		IsAdmin:        sql.NullBool{Bool: false, Valid: true},
		EmailConfirmed: sql.NullBool{Bool: true, Valid: true},
		AllowRead:      sql.NullBool{Bool: true, Valid: true},
		AllowWrite:     sql.NullBool{Bool: true, Valid: true},
		AllowUpload:    sql.NullBool{Bool: false, Valid: true},
	}

	user, err := database.Queries.CreateUser(ctx, params)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.ID == 0 {
		t.Error("user ID should be non-zero")
	}
	if user.Name != "Alice" {
		t.Errorf("Name = %q, want %q", user.Name, "Alice")
	}
	if user.Email != "alice@example.com" {
		t.Errorf("Email = %q, want %q", user.Email, "alice@example.com")
	}

	// Retrieve by email
	fetched, err := database.Queries.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail failed: %v", err)
	}
	if fetched.ID != user.ID {
		t.Errorf("fetched ID = %d, want %d", fetched.ID, user.ID)
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	params := CreateUserParams{
		Name:  "Alice",
		Email: "dup@example.com",
	}

	_, err := database.Queries.CreateUser(ctx, params)
	if err != nil {
		t.Fatalf("first CreateUser failed: %v", err)
	}

	params.Name = "Bob"
	_, err = database.Queries.CreateUser(ctx, params)
	if err == nil {
		t.Error("second CreateUser with same email should fail")
	}
}

func TestGetUserByID(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	created, _ := database.Queries.CreateUser(ctx, CreateUserParams{
		Name:  "Charlie",
		Email: "charlie@example.com",
	})

	fetched, err := database.Queries.GetUserByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetUserByID failed: %v", err)
	}
	if fetched.Name != "Charlie" {
		t.Errorf("Name = %q, want %q", fetched.Name, "Charlie")
	}
}

func TestListUsers(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	database.Queries.CreateUser(ctx, CreateUserParams{Name: "Alice", Email: "a@example.com"})
	database.Queries.CreateUser(ctx, CreateUserParams{Name: "Bob", Email: "b@example.com"})

	users, err := database.Queries.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers failed: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("ListUsers returned %d users, want 2", len(users))
	}
}

func TestDeleteUser(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	user, _ := database.Queries.CreateUser(ctx, CreateUserParams{
		Name:  "Delete Me",
		Email: "delete@example.com",
	})

	if err := database.Queries.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	_, err := database.Queries.GetUserByID(ctx, user.ID)
	if err == nil {
		t.Error("GetUserByID should fail after delete")
	}
}

func TestUpsertPreference(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	// Insert
	err := database.Queries.UpsertPreference(ctx, UpsertPreferenceParams{
		Name:  "theme",
		Value: sql.NullString{String: "dark", Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertPreference (insert) failed: %v", err)
	}

	pref, err := database.Queries.GetPreference(ctx, "theme")
	if err != nil {
		t.Fatalf("GetPreference failed: %v", err)
	}
	if pref.Value.String != "dark" {
		t.Errorf("Value = %q, want %q", pref.Value.String, "dark")
	}

	// Update
	err = database.Queries.UpsertPreference(ctx, UpsertPreferenceParams{
		Name:  "theme",
		Value: sql.NullString{String: "light", Valid: true},
	})
	if err != nil {
		t.Fatalf("UpsertPreference (update) failed: %v", err)
	}

	pref, err = database.Queries.GetPreference(ctx, "theme")
	if err != nil {
		t.Fatalf("GetPreference after update failed: %v", err)
	}
	if pref.Value.String != "light" {
		t.Errorf("Value after update = %q, want %q", pref.Value.String, "light")
	}
}

func TestGetPreference_NotFound(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	_, err := database.Queries.GetPreference(ctx, "nonexistent")
	if err == nil {
		t.Error("GetPreference should fail for missing preference")
	}
}

func TestCreateIssue(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	now := sql.NullTime{Time: time.Now(), Valid: true}
	params := CreateIssueParams{
		Title:          "Bug report",
		Description:    sql.NullString{String: "Something is broken", Valid: true},
		Status:         "open",
		Category:       sql.NullString{String: "bug", Valid: true},
		Tags:           sql.NullString{String: "critical", Valid: true},
		CreatedByName:  sql.NullString{String: "Alice", Valid: true},
		CreatedByEmail: sql.NullString{String: "alice@example.com", Valid: true},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	issue, err := database.Queries.CreateIssue(ctx, params)
	if err != nil {
		t.Fatalf("CreateIssue failed: %v", err)
	}

	if issue.ID == 0 {
		t.Error("issue ID should be non-zero")
	}
	if issue.Title != "Bug report" {
		t.Errorf("Title = %q, want %q", issue.Title, "Bug report")
	}

	// Retrieve by ID
	fetched, err := database.Queries.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if fetched.Title != "Bug report" {
		t.Errorf("fetched Title = %q, want %q", fetched.Title, "Bug report")
	}
}

func TestListIssuesByStatus(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	now := sql.NullTime{Time: time.Now(), Valid: true}
	database.Queries.CreateIssue(ctx, CreateIssueParams{
		Title: "Open issue", Status: "open", CreatedAt: now, UpdatedAt: now,
	})
	database.Queries.CreateIssue(ctx, CreateIssueParams{
		Title: "Closed issue", Status: "closed", CreatedAt: now, UpdatedAt: now,
	})

	open, err := database.Queries.ListIssuesByStatus(ctx, "open")
	if err != nil {
		t.Fatalf("ListIssuesByStatus(open) failed: %v", err)
	}
	if len(open) != 1 {
		t.Errorf("open issues = %d, want 1", len(open))
	}

	closed, err := database.Queries.ListIssuesByStatus(ctx, "closed")
	if err != nil {
		t.Fatalf("ListIssuesByStatus(closed) failed: %v", err)
	}
	if len(closed) != 1 {
		t.Errorf("closed issues = %d, want 1", len(closed))
	}
}

func TestDraftCRUD(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	now := sql.NullTime{Time: time.Now(), Valid: true}
	pagepath := sql.NullString{String: "testpage", Valid: true}
	email := sql.NullString{String: "user@example.com", Valid: true}

	// Create
	draft, err := database.Queries.CreateDraft(ctx, CreateDraftParams{
		Pagepath:    pagepath,
		Revision:    sql.NullString{String: "abc123", Valid: true},
		AuthorEmail: email,
		Content:     sql.NullString{String: "# Draft content", Valid: true},
		CursorLine:  sql.NullInt64{Int64: 5, Valid: true},
		CursorCh:    sql.NullInt64{Int64: 10, Valid: true},
		Datetime:    now,
	})
	if err != nil {
		t.Fatalf("CreateDraft failed: %v", err)
	}
	if draft.ID == 0 {
		t.Error("draft ID should be non-zero")
	}

	// Load
	loaded, err := database.Queries.GetDraft(ctx, GetDraftParams{
		Pagepath:    pagepath,
		AuthorEmail: email,
	})
	if err != nil {
		t.Fatalf("GetDraft failed: %v", err)
	}
	if loaded.Content.String != "# Draft content" {
		t.Errorf("Content = %q, want %q", loaded.Content.String, "# Draft content")
	}

	// Delete
	err = database.Queries.DeleteDraft(ctx, DeleteDraftParams{
		Pagepath:    pagepath,
		AuthorEmail: email,
	})
	if err != nil {
		t.Fatalf("DeleteDraft failed: %v", err)
	}

	// Verify deleted
	_, err = database.Queries.GetDraft(ctx, GetDraftParams{
		Pagepath:    pagepath,
		AuthorEmail: email,
	})
	if err == nil {
		t.Error("GetDraft should fail after delete")
	}
}

func TestCountUsers(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	count, err := database.Queries.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers failed: %v", err)
	}
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	database.Queries.CreateUser(ctx, CreateUserParams{Name: "A", Email: "a@x.com"})
	database.Queries.CreateUser(ctx, CreateUserParams{Name: "B", Email: "b@x.com"})

	count, err = database.Queries.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers failed: %v", err)
	}
	if count != 2 {
		t.Errorf("count after 2 inserts = %d, want 2", count)
	}
}

func TestPageLinksCRUD(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	t.Run("upsert and get backlinks", func(t *testing.T) {
		// Page "home" links to "about" and "guide"
		err := database.UpsertPageLinks(ctx, "home", []string{"about", "guide"})
		if err != nil {
			t.Fatalf("UpsertPageLinks failed: %v", err)
		}

		// Page "faq" also links to "about"
		err = database.UpsertPageLinks(ctx, "faq", []string{"about"})
		if err != nil {
			t.Fatalf("UpsertPageLinks failed: %v", err)
		}

		// Get backlinks for "about" -- should be "faq" and "home"
		backlinks, err := database.GetBacklinks(ctx, "about")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 2 {
			t.Fatalf("expected 2 backlinks for 'about', got %d: %v", len(backlinks), backlinks)
		}
		if backlinks[0] != "faq" || backlinks[1] != "home" {
			t.Errorf("expected [faq home], got %v", backlinks)
		}

		// Get backlinks for "guide" -- should be "home" only
		backlinks, err = database.GetBacklinks(ctx, "guide")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 1 || backlinks[0] != "home" {
			t.Errorf("expected [home], got %v", backlinks)
		}
	})

	t.Run("upsert replaces old links", func(t *testing.T) {
		// Update "home" to only link to "contact"
		err := database.UpsertPageLinks(ctx, "home", []string{"contact"})
		if err != nil {
			t.Fatalf("UpsertPageLinks failed: %v", err)
		}

		// "about" backlinks should now only be "faq"
		backlinks, err := database.GetBacklinks(ctx, "about")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 1 || backlinks[0] != "faq" {
			t.Errorf("expected [faq] after upsert, got %v", backlinks)
		}

		// "contact" should have "home"
		backlinks, err = database.GetBacklinks(ctx, "contact")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 1 || backlinks[0] != "home" {
			t.Errorf("expected [home], got %v", backlinks)
		}
	})

	t.Run("delete removes links", func(t *testing.T) {
		err := database.DeletePageLinks(ctx, "faq")
		if err != nil {
			t.Fatalf("DeletePageLinks failed: %v", err)
		}

		backlinks, err := database.GetBacklinks(ctx, "about")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 0 {
			t.Errorf("expected 0 backlinks after delete, got %v", backlinks)
		}
	})

	t.Run("rebuild replaces everything", func(t *testing.T) {
		links := []PageLinkData{
			{Source: "alpha", Targets: []string{"beta", "gamma"}},
			{Source: "beta", Targets: []string{"gamma"}},
		}
		err := database.RebuildPageLinks(ctx, links)
		if err != nil {
			t.Fatalf("RebuildPageLinks failed: %v", err)
		}

		// "gamma" should have backlinks from "alpha" and "beta"
		backlinks, err := database.GetBacklinks(ctx, "gamma")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 2 {
			t.Fatalf("expected 2 backlinks for 'gamma', got %d", len(backlinks))
		}

		// Old data should be gone
		backlinks, err = database.GetBacklinks(ctx, "contact")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 0 {
			t.Errorf("expected 0 backlinks for 'contact' after rebuild, got %v", backlinks)
		}
	})

	t.Run("no backlinks returns empty", func(t *testing.T) {
		backlinks, err := database.GetBacklinks(ctx, "nonexistent")
		if err != nil {
			t.Fatalf("GetBacklinks failed: %v", err)
		}
		if len(backlinks) != 0 {
			t.Errorf("expected 0 backlinks, got %v", backlinks)
		}
	})
}

func TestDeleteIssue(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()

	now := sql.NullTime{Time: time.Now(), Valid: true}
	issue, _ := database.Queries.CreateIssue(ctx, CreateIssueParams{
		Title: "To Delete", Status: "open", CreatedAt: now, UpdatedAt: now,
	})

	if err := database.Queries.DeleteIssue(ctx, issue.ID); err != nil {
		t.Fatalf("DeleteIssue failed: %v", err)
	}

	_, err := database.Queries.GetIssue(ctx, issue.ID)
	if err == nil {
		t.Error("GetIssue should fail after delete")
	}
}
