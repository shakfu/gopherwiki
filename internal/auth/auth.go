// Package auth provides authentication functionality for GopherWiki.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/models"
)

// Common errors.
var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrEmailExists        = errors.New("email already registered")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrUserNotApproved    = errors.New("user not approved")
	ErrEmailNotConfirmed  = errors.New("email not confirmed")
)

// Auth provides authentication operations.
type Auth struct {
	config  *config.Config
	queries *db.Queries
}

// New creates a new Auth instance.
func New(cfg *config.Config, queries *db.Queries) *Auth {
	return &Auth{
		config:  cfg,
		queries: queries,
	}
}

// HashPassword hashes a password using bcrypt.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword compares a password against a hash.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// GenerateToken generates a random token for email confirmation or password reset.
func GenerateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// Authenticate validates credentials and returns the user.
func (a *Auth) Authenticate(ctx context.Context, email, password string) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	dbUser, err := a.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	user := models.NewUser(&dbUser)

	if !user.HasPasswordHash() {
		return nil, ErrInvalidCredentials
	}

	if !CheckPassword(password, user.GetPasswordHash()) {
		return nil, ErrInvalidCredentials
	}

	// Check if user is approved (if approval is required)
	if !a.config.AutoApproval && !user.Approved() {
		return nil, ErrUserNotApproved
	}

	// Check if email confirmation is required
	if a.config.EmailNeedsConfirmation && !user.EmailIsConfirmed() {
		return nil, ErrEmailNotConfirmed
	}

	return user, nil
}

// Register creates a new user account.
func (a *Auth) Register(ctx context.Context, name, email, password string) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)

	// Check if email already exists
	_, err := a.queries.GetUserByEmail(ctx, email)
	if err == nil {
		return nil, ErrEmailExists
	}

	// Hash password
	hash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	// Determine permissions based on config
	isApproved := a.config.AutoApproval
	isAdmin := false
	allowRead := false
	allowWrite := false
	allowUpload := false

	// Check if this is the first user (make them admin)
	isFirstUser := false
	count, err := a.queries.CountUsers(ctx)
	if err == nil && count == 0 {
		isFirstUser = true
		isAdmin = true
		isApproved = true
		// First user gets all permissions
		allowRead = true
		allowWrite = true
		allowUpload = true
	} else if isApproved {
		// Apply default permissions for approved users
		// Based on the access settings, if REGISTERED can do it, grant it
		allowRead = a.config.ReadAccess == "ANONYMOUS" || a.config.ReadAccess == "REGISTERED"
		allowWrite = a.config.WriteAccess == "ANONYMOUS" || a.config.WriteAccess == "REGISTERED"
		allowUpload = a.config.AttachmentAccess == "ANONYMOUS" || a.config.AttachmentAccess == "REGISTERED"
	}

	// Create user
	params := models.CreateUserParams{
		Name:           name,
		Email:          email,
		PasswordHash:   hash,
		IsApproved:     isApproved,
		IsAdmin:        isAdmin,
		EmailConfirmed: isFirstUser, // First user (admin) has email auto-confirmed
		AllowRead:      allowRead,
		AllowWrite:     allowWrite,
		AllowUpload:    allowUpload,
	}

	dbUser, err := a.queries.CreateUser(ctx, params.ToDBParams())
	if err != nil {
		return nil, err
	}

	return models.NewUser(&dbUser), nil
}

// GetUserByID retrieves a user by ID.
func (a *Auth) GetUserByID(ctx context.Context, id int64) (*models.User, error) {
	dbUser, err := a.queries.GetUserByID(ctx, id)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return models.NewUser(&dbUser), nil
}

// GetUserByEmail retrieves a user by email.
func (a *Auth) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	dbUser, err := a.queries.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return models.NewUser(&dbUser), nil
}

// UpdatePassword updates a user's password.
func (a *Auth) UpdatePassword(ctx context.Context, userID int64, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	dbUser, err := a.queries.GetUserByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	user := models.NewUser(&dbUser)
	params := models.UpdateUserParams{
		ID:             user.ID,
		Name:           user.Name,
		Email:          user.Email,
		PasswordHash:   hash,
		IsApproved:     user.Approved(),
		IsAdmin:        user.Admin(),
		EmailConfirmed: user.EmailIsConfirmed(),
		AllowRead:      user.CanRead(),
		AllowWrite:     user.CanWrite(),
		AllowUpload:    user.CanUpload(),
	}

	return a.queries.UpdateUser(ctx, params.ToDBParams())
}

// UpdateUserLastSeen updates the user's last seen timestamp.
func (a *Auth) UpdateUserLastSeen(ctx context.Context, userID int64) error {
	return a.queries.UpdateUserLastSeen(ctx, db.UpdateUserLastSeenParams{
		LastSeen: db.NullTime(time.Now()),
		ID:       userID,
	})
}

// UpdateUserName updates the user's display name.
func (a *Auth) UpdateUserName(ctx context.Context, userID int64, name string) error {
	dbUser, err := a.queries.GetUserByID(ctx, userID)
	if err != nil {
		return ErrUserNotFound
	}

	user := models.NewUser(&dbUser)
	params := models.UpdateUserParams{
		ID:             user.ID,
		Name:           strings.TrimSpace(name),
		Email:          user.Email,
		PasswordHash:   user.GetPasswordHash(),
		IsApproved:     user.Approved(),
		IsAdmin:        user.Admin(),
		EmailConfirmed: user.EmailIsConfirmed(),
		AllowRead:      user.CanRead(),
		AllowWrite:     user.CanWrite(),
		AllowUpload:    user.CanUpload(),
	}

	return a.queries.UpdateUser(ctx, params.ToDBParams())
}

// ListUsers returns all users.
func (a *Auth) ListUsers(ctx context.Context) ([]*models.User, error) {
	dbUsers, err := a.queries.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]*models.User, len(dbUsers))
	for i, u := range dbUsers {
		u := u // capture loop variable
		users[i] = models.NewUser(&u)
	}
	return users, nil
}

// DeleteUser deletes a user by ID.
func (a *Auth) DeleteUser(ctx context.Context, userID int64) error {
	return a.queries.DeleteUser(ctx, userID)
}
