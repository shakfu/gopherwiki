package models

import (
	"database/sql"
	"testing"

	"github.com/sa/gopherwiki/internal/db"
)

func TestAnonymousUser(t *testing.T) {
	user := AnonymousUser()

	if !user.IsAnonymous() {
		t.Error("AnonymousUser().IsAnonymous() should be true")
	}
	if user.IsAuthenticated() {
		t.Error("AnonymousUser().IsAuthenticated() should be false")
	}
	if user.GetName() != "Anonymous" {
		t.Errorf("AnonymousUser().GetName() = %q, want %q", user.GetName(), "Anonymous")
	}
	if user.GetEmail() != "" {
		t.Errorf("AnonymousUser().GetEmail() = %q, want empty", user.GetEmail())
	}
}

func TestNewUser_Nil(t *testing.T) {
	user := NewUser(nil)
	if user != nil {
		t.Error("NewUser(nil) should return nil")
	}
}

func TestNewUser_Properties(t *testing.T) {
	dbUser := &db.User{
		ID:    1,
		Name:  "Alice",
		Email: "alice@example.com",
	}
	user := NewUser(dbUser)

	if user.IsAnonymous() {
		t.Error("authenticated user should not be anonymous")
	}
	if !user.IsAuthenticated() {
		t.Error("authenticated user should be authenticated")
	}
	if user.GetName() != "Alice" {
		t.Errorf("GetName() = %q, want %q", user.GetName(), "Alice")
	}
	if user.GetEmail() != "alice@example.com" {
		t.Errorf("GetEmail() = %q, want %q", user.GetEmail(), "alice@example.com")
	}
}

func TestUser_Approved(t *testing.T) {
	approved := NewUser(&db.User{
		ID:         1,
		IsApproved: sql.NullBool{Bool: true, Valid: true},
	})
	if !approved.Approved() {
		t.Error("approved user should return Approved()=true")
	}

	unapproved := NewUser(&db.User{
		ID:         2,
		IsApproved: sql.NullBool{Bool: false, Valid: true},
	})
	if unapproved.Approved() {
		t.Error("unapproved user should return Approved()=false")
	}

	// NullBool not valid
	nullApproved := NewUser(&db.User{
		ID:         3,
		IsApproved: sql.NullBool{Valid: false},
	})
	if nullApproved.Approved() {
		t.Error("user with null approved should return Approved()=false")
	}
}

func TestUser_Admin(t *testing.T) {
	admin := NewUser(&db.User{
		ID:      1,
		IsAdmin: sql.NullBool{Bool: true, Valid: true},
	})
	if !admin.Admin() {
		t.Error("admin user should return Admin()=true")
	}

	nonAdmin := NewUser(&db.User{
		ID:      2,
		IsAdmin: sql.NullBool{Bool: false, Valid: true},
	})
	if nonAdmin.Admin() {
		t.Error("non-admin user should return Admin()=false")
	}
}

func TestUser_Permissions(t *testing.T) {
	user := NewUser(&db.User{
		ID:          1,
		AllowRead:   sql.NullBool{Bool: true, Valid: true},
		AllowWrite:  sql.NullBool{Bool: false, Valid: true},
		AllowUpload: sql.NullBool{Bool: true, Valid: true},
	})

	if !user.CanRead() {
		t.Error("user with AllowRead=true should return CanRead()=true")
	}
	if user.CanWrite() {
		t.Error("user with AllowWrite=false should return CanWrite()=false")
	}
	if !user.CanUpload() {
		t.Error("user with AllowUpload=true should return CanUpload()=true")
	}
}

func TestUser_AnonymousPredicates(t *testing.T) {
	user := AnonymousUser()

	if user.Approved() {
		t.Error("anonymous user should not be approved")
	}
	if user.Admin() {
		t.Error("anonymous user should not be admin")
	}
	if user.CanRead() {
		t.Error("anonymous user should not have CanRead")
	}
	if user.CanWrite() {
		t.Error("anonymous user should not have CanWrite")
	}
	if user.CanUpload() {
		t.Error("anonymous user should not have CanUpload")
	}
	if user.HasPasswordHash() {
		t.Error("anonymous user should not have password hash")
	}
	if user.EmailIsConfirmed() {
		t.Error("anonymous user should not have email confirmed")
	}
}

func TestUser_PasswordHash(t *testing.T) {
	user := NewUser(&db.User{
		ID:           1,
		PasswordHash: sql.NullString{String: "hashed", Valid: true},
	})

	if !user.HasPasswordHash() {
		t.Error("user with password hash should return HasPasswordHash()=true")
	}
	if user.GetPasswordHash() != "hashed" {
		t.Errorf("GetPasswordHash() = %q, want %q", user.GetPasswordHash(), "hashed")
	}

	noHash := NewUser(&db.User{
		ID:           2,
		PasswordHash: sql.NullString{Valid: false},
	})
	if noHash.HasPasswordHash() {
		t.Error("user without password hash should return HasPasswordHash()=false")
	}
	if noHash.GetPasswordHash() != "" {
		t.Errorf("GetPasswordHash() = %q, want empty", noHash.GetPasswordHash())
	}
}

func TestCreateUserParams_ToDBParams(t *testing.T) {
	params := &CreateUserParams{
		Name:         "Bob",
		Email:        "bob@example.com",
		PasswordHash: "hash123",
		IsApproved:   true,
		IsAdmin:      false,
		AllowRead:    true,
		AllowWrite:   true,
		AllowUpload:  false,
	}

	dbParams := params.ToDBParams()

	if dbParams.Name != "Bob" {
		t.Errorf("Name = %q, want %q", dbParams.Name, "Bob")
	}
	if dbParams.Email != "bob@example.com" {
		t.Errorf("Email = %q, want %q", dbParams.Email, "bob@example.com")
	}
	if !dbParams.PasswordHash.Valid || dbParams.PasswordHash.String != "hash123" {
		t.Errorf("PasswordHash = %v, want valid with 'hash123'", dbParams.PasswordHash)
	}
	if !dbParams.IsApproved.Bool {
		t.Error("IsApproved should be true")
	}
	if dbParams.IsAdmin.Bool {
		t.Error("IsAdmin should be false")
	}
	if !dbParams.AllowRead.Bool {
		t.Error("AllowRead should be true")
	}
}
