package main

import (
	"context"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/storage"
	"github.com/sa/gopherwiki/internal/wiki"
)

// TestPrepareRepositoryIndexesInitialPages checks that the first-start pages
// are searchable and linked on the first start, not only after a restart.
func TestPrepareRepositoryIndexesInitialPages(t *testing.T) {
	ctx := context.Background()
	store, err := storage.NewGitStorage(t.TempDir(), true)
	if err != nil {
		t.Fatalf("failed to create git storage: %v", err)
	}
	database, err := db.Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	cfg := config.Default()
	ws := wiki.NewWikiService(store, cfg, database)

	prepareRepository(ctx, store, ws, cfg)

	if count, err := database.PageIndexCount(ctx); err != nil || count != 2 {
		t.Errorf("PageIndexCount = %d, %v; want 2 (home and syntaxguide)", count, err)
	}
	backlinks, err := ws.Backlinks(ctx, "syntaxguide")
	if err != nil || len(backlinks) != 1 || backlinks[0] != "home" {
		t.Errorf("Backlinks(syntaxguide) = %v, %v; want [home]", backlinks, err)
	}
}
