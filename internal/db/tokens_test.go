package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

func createTokenTestUser(t *testing.T, database *Database) User {
	t.Helper()
	user, err := database.Queries.CreateUser(context.Background(), CreateUserParams{
		Name:  "Agent",
		Email: "agent@example.com",
	})
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	return user
}

func TestAPIToken_CreateGetDelete(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	user := createTokenTestUser(t, database)

	plain, err := database.Queries.CreateAPIToken(ctx, user.ID, "report agent", "kb")
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}
	if !strings.HasPrefix(plain, "gw_") {
		t.Errorf("token = %q, want gw_ prefix", plain)
	}

	token, err := database.Queries.GetAPIToken(ctx, plain)
	if err != nil {
		t.Fatalf("GetAPIToken failed: %v", err)
	}
	if token.UserID != user.ID || token.UserName != "Agent" || token.Label != "report agent" || token.WritePrefix != "kb" {
		t.Errorf("unexpected token: %+v", token)
	}
	if token.LastUsedAt.Valid {
		t.Error("a new token should have no last-used time")
	}

	if err := database.Queries.TouchAPIToken(ctx, token.ID); err != nil {
		t.Fatalf("TouchAPIToken failed: %v", err)
	}
	tokens, err := database.Queries.ListAPITokens(ctx)
	if err != nil {
		t.Fatalf("ListAPITokens failed: %v", err)
	}
	if len(tokens) != 1 || !tokens[0].LastUsedAt.Valid {
		t.Errorf("ListAPITokens = %+v, want one token with a last-used time", tokens)
	}

	if err := database.Queries.DeleteAPIToken(ctx, token.ID); err != nil {
		t.Fatalf("DeleteAPIToken failed: %v", err)
	}
	if _, err := database.Queries.GetAPIToken(ctx, plain); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetAPIToken after delete: err = %v, want sql.ErrNoRows", err)
	}
}

func TestAPIToken_PlaintextNotStored(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	user := createTokenTestUser(t, database)

	plain, err := database.Queries.CreateAPIToken(ctx, user.ID, "label", "kb")
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	var stored string
	if err := database.Conn().QueryRowContext(ctx, `SELECT token_hash FROM api_tokens`).Scan(&stored); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if stored == plain || strings.Contains(stored, strings.TrimPrefix(plain, "gw_")) {
		t.Error("the database must store a hash, not the plaintext token")
	}
}

func TestAPIToken_UnknownToken(t *testing.T) {
	database := openTestDB(t)

	if _, err := database.Queries.GetAPIToken(context.Background(), "gw_unknown"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("err = %v, want sql.ErrNoRows", err)
	}
}

func TestAPIToken_DeletedWithUser(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	user := createTokenTestUser(t, database)

	plain, err := database.Queries.CreateAPIToken(ctx, user.ID, "label", "kb")
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}
	if err := database.Queries.DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("DeleteUser failed: %v", err)
	}

	if _, err := database.Queries.GetAPIToken(ctx, plain); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("token should be deleted with its user: err = %v", err)
	}
}

func TestMigrate_AddsAllowReviewToLegacyUserTable(t *testing.T) {
	database, err := Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	ctx := context.Background()

	// A database created before the review permission existed.
	if _, err := database.Conn().ExecContext(ctx, `CREATE TABLE user (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT,
		first_seen TIMESTAMP,
		last_seen TIMESTAMP,
		is_approved BOOLEAN DEFAULT FALSE,
		is_admin BOOLEAN DEFAULT FALSE,
		email_confirmed BOOLEAN DEFAULT FALSE,
		allow_read BOOLEAN DEFAULT FALSE,
		allow_write BOOLEAN DEFAULT FALSE,
		allow_upload BOOLEAN DEFAULT FALSE
	)`); err != nil {
		t.Fatalf("failed to create legacy table: %v", err)
	}
	if _, err := database.Conn().ExecContext(ctx, `INSERT INTO user (name, email) VALUES ('Old', 'old@example.com')`); err != nil {
		t.Fatalf("failed to insert legacy user: %v", err)
	}

	if err := database.Migrate(ctx); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	user, err := database.Queries.GetUserByEmail(ctx, "old@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail failed after migrate: %v", err)
	}
	if user.AllowReview.Bool {
		t.Error("a legacy user should not gain review permission")
	}
}
