// Package testutil provides shared test setup for GopherWiki tests.
package testutil

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/handlers"
	"github.com/sa/gopherwiki/internal/middleware"
	"github.com/sa/gopherwiki/internal/models"
	"github.com/sa/gopherwiki/internal/storage"
)

// TestEnv bundles all test dependencies.
type TestEnv struct {
	Server *handlers.Server
	Router chi.Router
	DB     *db.Database
	Store  storage.Storage
	TmpDir string
}

// SetupTestEnv creates a fully wired Server with:
// - temp git repo (initialized)
// - in-memory SQLite (migrated)
// - loaded templates (found via runtime.Caller)
// - default config with ReadAccess="ANONYMOUS", WriteAccess="ANONYMOUS"
// Returns TestEnv and a cleanup function.
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	tmpDir := t.TempDir()

	// Initialize git repo
	store, err := storage.NewGitStorage(tmpDir, true)
	if err != nil {
		t.Fatalf("failed to create git storage: %v", err)
	}

	// Open in-memory SQLite
	database, err := db.Open("sqlite:///:memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	// Run migrations
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	// Config with permissive defaults for testing
	cfg := config.Default()
	cfg.SecretKey = "test-secret-key-1234567890"
	cfg.Repository = tmpDir
	cfg.ReadAccess = "ANONYMOUS"
	cfg.WriteAccess = "ANONYMOUS"
	cfg.AttachmentAccess = "ANONYMOUS"
	cfg.AutoApproval = true
	cfg.Testing = true

	// Create server
	srv, err := handlers.NewServer(cfg, store, database, "test")
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Load templates from the project's web/templates directory
	templatesDir := findTemplatesDir(t)
	if err := srv.LoadTemplates(os.DirFS(templatesDir)); err != nil {
		t.Fatalf("failed to load templates: %v", err)
	}

	// Set static FS for routes
	staticDir := filepath.Join(filepath.Dir(templatesDir), "static")
	srv.StaticFS = os.DirFS(staticDir)

	router := srv.Routes()

	return &TestEnv{
		Server: srv,
		Router: router,
		DB:     database,
		Store:  store,
		TmpDir: tmpDir,
	}
}

// findTemplatesDir locates the web/templates directory relative to the source file.
func findTemplatesDir(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get caller info")
	}
	// Walk up from internal/testutil/testutil.go to project root
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	dir := filepath.Join(projectRoot, "web", "templates")
	return dir
}

// UserOpts configures test user properties.
type UserOpts struct {
	Name, Email, Password string
	Admin, Approved       bool
	AllowRead             bool
	AllowWrite            bool
	AllowUpload           bool
}

// CreateTestUser inserts a user into the DB and returns the models.User.
func CreateTestUser(t *testing.T, database *db.Database, opts UserOpts) *models.User {
	t.Helper()

	if opts.Name == "" {
		opts.Name = "Test User"
	}
	if opts.Email == "" {
		opts.Email = "test@example.com"
	}
	if opts.Password == "" {
		opts.Password = "testpassword123"
	}

	now := db.NullTime(time.Time{})

	params := db.CreateUserParams{
		Name:           opts.Name,
		Email:          opts.Email,
		PasswordHash:   db.NullString(opts.Password),
		FirstSeen:      now,
		LastSeen:       now,
		IsApproved:     db.NullBool(opts.Approved),
		IsAdmin:        db.NullBool(opts.Admin),
		EmailConfirmed: db.NullBool(true),
		AllowRead:      db.NullBool(opts.AllowRead),
		AllowWrite:     db.NullBool(opts.AllowWrite),
		AllowUpload:    db.NullBool(opts.AllowUpload),
	}

	dbUser, err := database.Queries.CreateUser(context.Background(), params)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	return models.NewUser(&dbUser)
}

// RequestWithUser creates an http.Request with the given user injected into context.
// Uses middleware.UserKey context key.
func RequestWithUser(method, path string, body io.Reader, user *models.User) *http.Request {
	req, _ := http.NewRequest(method, path, body)
	if user == nil {
		user = models.AnonymousUser()
	}
	ctx := context.WithValue(req.Context(), middleware.UserKey, user)
	return req.WithContext(ctx)
}
