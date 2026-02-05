// Package models provides domain models for GopherWiki.
package models

import (
	"database/sql"
	"time"

	"github.com/sa/gopherwiki/internal/db"
)

// User wraps the database user model with helper methods.
type User struct {
	*db.User
}

// NewUser creates a new User from a db.User.
func NewUser(u *db.User) *User {
	if u == nil {
		return nil
	}
	return &User{User: u}
}

// IsAnonymous returns true if this is not a real user.
func (u *User) IsAnonymous() bool {
	return u == nil || u.User == nil || u.ID == 0
}

// IsAuthenticated returns true if the user is logged in.
func (u *User) IsAuthenticated() bool {
	return !u.IsAnonymous()
}

// GetName returns the user's display name.
func (u *User) GetName() string {
	if u.IsAnonymous() {
		return "Anonymous"
	}
	return u.Name
}

// GetEmail returns the user's email.
func (u *User) GetEmail() string {
	if u.IsAnonymous() {
		return ""
	}
	return u.Email
}

// HasPasswordHash returns true if the user has a password set.
func (u *User) HasPasswordHash() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.PasswordHash.Valid && u.PasswordHash.String != ""
}

// GetPasswordHash returns the password hash or empty string.
func (u *User) GetPasswordHash() string {
	if u.IsAnonymous() || !u.PasswordHash.Valid {
		return ""
	}
	return u.PasswordHash.String
}

// Approved returns true if the user is approved.
func (u *User) Approved() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.IsApproved.Valid && u.IsApproved.Bool
}

// Admin returns true if the user is an admin.
func (u *User) Admin() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.IsAdmin.Valid && u.IsAdmin.Bool
}

// EmailIsConfirmed returns true if the user's email is confirmed.
func (u *User) EmailIsConfirmed() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.EmailConfirmed.Valid && u.EmailConfirmed.Bool
}

// CanRead returns true if the user has read permission.
func (u *User) CanRead() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.AllowRead.Valid && u.AllowRead.Bool
}

// CanWrite returns true if the user has write permission.
func (u *User) CanWrite() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.AllowWrite.Valid && u.AllowWrite.Bool
}

// CanUpload returns true if the user has upload permission.
func (u *User) CanUpload() bool {
	if u.IsAnonymous() {
		return false
	}
	return u.AllowUpload.Valid && u.AllowUpload.Bool
}

// GetFirstSeen returns when the user was first seen.
func (u *User) GetFirstSeen() time.Time {
	if u.IsAnonymous() || !u.FirstSeen.Valid {
		return time.Time{}
	}
	return u.FirstSeen.Time
}

// GetLastSeen returns when the user was last seen.
func (u *User) GetLastSeen() time.Time {
	if u.IsAnonymous() || !u.LastSeen.Valid {
		return time.Time{}
	}
	return u.LastSeen.Time
}

// AnonymousUser returns a user representing an anonymous/guest user.
func AnonymousUser() *User {
	return &User{User: nil}
}

// CreateUserParams holds parameters for creating a new user.
type CreateUserParams struct {
	Name           string
	Email          string
	PasswordHash   string
	IsApproved     bool
	IsAdmin        bool
	EmailConfirmed bool
	AllowRead      bool
	AllowWrite     bool
	AllowUpload    bool
}

// ToDBParams converts CreateUserParams to db.CreateUserParams.
func (p *CreateUserParams) ToDBParams() db.CreateUserParams {
	now := time.Now()
	return db.CreateUserParams{
		Name:           p.Name,
		Email:          p.Email,
		PasswordHash:   sql.NullString{String: p.PasswordHash, Valid: p.PasswordHash != ""},
		FirstSeen:      sql.NullTime{Time: now, Valid: true},
		LastSeen:       sql.NullTime{Time: now, Valid: true},
		IsApproved:     sql.NullBool{Bool: p.IsApproved, Valid: true},
		IsAdmin:        sql.NullBool{Bool: p.IsAdmin, Valid: true},
		EmailConfirmed: sql.NullBool{Bool: p.EmailConfirmed, Valid: true},
		AllowRead:      sql.NullBool{Bool: p.AllowRead, Valid: true},
		AllowWrite:     sql.NullBool{Bool: p.AllowWrite, Valid: true},
		AllowUpload:    sql.NullBool{Bool: p.AllowUpload, Valid: true},
	}
}

// UpdateUserParams holds parameters for updating a user.
type UpdateUserParams struct {
	ID             int64
	Name           string
	Email          string
	PasswordHash   string
	IsApproved     bool
	IsAdmin        bool
	EmailConfirmed bool
	AllowRead      bool
	AllowWrite     bool
	AllowUpload    bool
}

// ToDBParams converts UpdateUserParams to db.UpdateUserParams.
func (p *UpdateUserParams) ToDBParams() db.UpdateUserParams {
	return db.UpdateUserParams{
		ID:             p.ID,
		Name:           p.Name,
		Email:          p.Email,
		PasswordHash:   sql.NullString{String: p.PasswordHash, Valid: p.PasswordHash != ""},
		LastSeen:       sql.NullTime{Time: time.Now(), Valid: true},
		IsApproved:     sql.NullBool{Bool: p.IsApproved, Valid: true},
		IsAdmin:        sql.NullBool{Bool: p.IsAdmin, Valid: true},
		EmailConfirmed: sql.NullBool{Bool: p.EmailConfirmed, Valid: true},
		AllowRead:      sql.NullBool{Bool: p.AllowRead, Valid: true},
		AllowWrite:     sql.NullBool{Bool: p.AllowWrite, Valid: true},
		AllowUpload:    sql.NullBool{Bool: p.AllowUpload, Valid: true},
	}
}
