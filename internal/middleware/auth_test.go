package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sa/gopherwiki/internal/config"
	"github.com/sa/gopherwiki/internal/db"
	"github.com/sa/gopherwiki/internal/models"
)

// makeUser creates a models.User with given properties for testing.
func makeUser(opts struct {
	ID          int64
	Approved    bool
	Admin       bool
	AllowRead   bool
	AllowWrite  bool
	AllowUpload bool
}) *models.User {
	return models.NewUser(&db.User{
		ID:          opts.ID,
		IsApproved:  sql.NullBool{Bool: opts.Approved, Valid: true},
		IsAdmin:     sql.NullBool{Bool: opts.Admin, Valid: true},
		AllowRead:   sql.NullBool{Bool: opts.AllowRead, Valid: true},
		AllowWrite:  sql.NullBool{Bool: opts.AllowWrite, Valid: true},
		AllowUpload: sql.NullBool{Bool: opts.AllowUpload, Valid: true},
	})
}

// requestWithUser creates a request with user injected into context.
func requestWithUser(user *models.User) *http.Request {
	req := httptest.NewRequest("GET", "/test", nil)
	if user == nil {
		user = models.AnonymousUser()
	}
	ctx := context.WithValue(req.Context(), UserKey, user)
	return req.WithContext(ctx)
}

// newChecker creates a PermissionChecker with the given access config.
func newChecker(readAccess, writeAccess, attachmentAccess string) *PermissionChecker {
	cfg := config.Default()
	cfg.ReadAccess = readAccess
	cfg.WriteAccess = writeAccess
	cfg.AttachmentAccess = attachmentAccess
	return NewPermissionChecker(cfg, nil)
}

// --- canRead tests ---

func TestCanRead_Anonymous_AllowsAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if !pc.HasPermission(r, PermissionRead) {
		t.Error("ANONYMOUS config should allow anonymous read")
	}
}

func TestCanRead_Anonymous_BlocksRegistered(t *testing.T) {
	pc := newChecker("REGISTERED", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, PermissionRead) {
		t.Error("REGISTERED config should block anonymous read")
	}
}

func TestCanRead_Registered_AllowsApprovedWithRead(t *testing.T) {
	pc := newChecker("REGISTERED", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, false, false})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionRead) {
		t.Error("REGISTERED config should allow approved user with read permission")
	}
}

func TestCanRead_Registered_BlocksUnapproved(t *testing.T) {
	pc := newChecker("REGISTERED", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, false, false, false, false, false})
	r := requestWithUser(user)

	if pc.HasPermission(r, PermissionRead) {
		t.Error("REGISTERED config should block unapproved user without read permission")
	}
}

func TestCanRead_Approved_AllowsApproved(t *testing.T) {
	pc := newChecker("APPROVED", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, false, false, false})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionRead) {
		t.Error("APPROVED config should allow approved user")
	}
}

func TestCanRead_Approved_BlocksAnonymous(t *testing.T) {
	pc := newChecker("APPROVED", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, PermissionRead) {
		t.Error("APPROVED config should block anonymous")
	}
}

func TestCanRead_Approved_BlocksUnapproved(t *testing.T) {
	pc := newChecker("APPROVED", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, false, false, false, false, false})
	r := requestWithUser(user)

	if pc.HasPermission(r, PermissionRead) {
		t.Error("APPROVED config should block unapproved user")
	}
}

func TestCanRead_Admin_AllowsAdmin(t *testing.T) {
	pc := newChecker("ADMIN", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, true, true, true, true})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionRead) {
		t.Error("ADMIN config should allow admin user")
	}
}

func TestCanRead_Admin_BlocksNonAdmin(t *testing.T) {
	pc := newChecker("ADMIN", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, true, true})
	r := requestWithUser(user)

	if pc.HasPermission(r, PermissionRead) {
		t.Error("ADMIN config should block non-admin user")
	}
}

func TestCanRead_Admin_BlocksAnonymous(t *testing.T) {
	pc := newChecker("ADMIN", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, PermissionRead) {
		t.Error("ADMIN config should block anonymous")
	}
}

// --- canWrite tests ---

func TestCanWrite_Anonymous_AllowsAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if !pc.HasPermission(r, PermissionWrite) {
		t.Error("ANONYMOUS write should allow anonymous")
	}
}

func TestCanWrite_Registered_BlocksAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "REGISTERED", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, PermissionWrite) {
		t.Error("REGISTERED write should block anonymous")
	}
}

func TestCanWrite_Registered_AllowsApprovedWithWrite(t *testing.T) {
	pc := newChecker("ANONYMOUS", "REGISTERED", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, false, true, false})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionWrite) {
		t.Error("REGISTERED write should allow approved user with write permission")
	}
}

func TestCanWrite_Registered_BlocksApprovedWithoutWrite(t *testing.T) {
	pc := newChecker("ANONYMOUS", "REGISTERED", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, false, false})
	r := requestWithUser(user)

	if pc.HasPermission(r, PermissionWrite) {
		t.Error("REGISTERED write should block approved user without write permission")
	}
}

func TestCanWrite_Admin_AllowsAdmin(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ADMIN", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, true, false, false, false})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionWrite) {
		t.Error("ADMIN write should allow admin")
	}
}

func TestCanWrite_Admin_BlocksNonAdmin(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ADMIN", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, true, true})
	r := requestWithUser(user)

	if pc.HasPermission(r, PermissionWrite) {
		t.Error("ADMIN write should block non-admin")
	}
}

// --- canUpload tests ---

func TestCanUpload_Anonymous_AllowsAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if !pc.HasPermission(r, PermissionUpload) {
		t.Error("ANONYMOUS attachment should allow anonymous")
	}
}

func TestCanUpload_Registered_BlocksAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "REGISTERED")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, PermissionUpload) {
		t.Error("REGISTERED attachment should block anonymous")
	}
}

func TestCanUpload_Registered_AllowsApprovedWithUpload(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "REGISTERED")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, false, false, true})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionUpload) {
		t.Error("REGISTERED attachment should allow approved user with upload permission")
	}
}

// --- canAdmin tests ---

func TestCanAdmin_AllowsAdmin(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, true, true, true, true})
	r := requestWithUser(user)

	if !pc.HasPermission(r, PermissionAdmin) {
		t.Error("admin user should have admin permission")
	}
}

func TestCanAdmin_BlocksAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, PermissionAdmin) {
		t.Error("anonymous should not have admin permission")
	}
}

func TestCanAdmin_BlocksNonAdmin(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, true, true})
	r := requestWithUser(user)

	if pc.HasPermission(r, PermissionAdmin) {
		t.Error("non-admin should not have admin permission")
	}
}

// --- Middleware integration tests ---

func TestRequireRead_Blocks(t *testing.T) {
	pc := newChecker("REGISTERED", "ANONYMOUS", "ANONYMOUS")

	called := false
	handler := pc.RequireRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	w := httptest.NewRecorder()
	r := requestWithUser(models.AnonymousUser())
	handler.ServeHTTP(w, r)

	if called {
		t.Error("handler should not be called when read permission denied")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect)", w.Code, http.StatusFound)
	}
}

func TestRequireRead_Allows(t *testing.T) {
	pc := newChecker("REGISTERED", "ANONYMOUS", "ANONYMOUS")

	called := false
	handler := pc.RequireRead(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, false, false})
	r := requestWithUser(user)
	handler.ServeHTTP(w, r)

	if !called {
		t.Error("handler should be called when read permission granted")
	}
}

func TestRequireWrite_Forbidden(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ADMIN", "ANONYMOUS")

	called := false
	handler := pc.RequireWrite(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	w := httptest.NewRecorder()
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, true, false, true, true, true})
	r := requestWithUser(user)
	handler.ServeHTTP(w, r)

	if called {
		t.Error("handler should not be called when write permission denied")
	}
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestRequireAdmin_Redirect(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")

	handler := pc.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := requestWithUser(models.AnonymousUser())
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d (redirect for anonymous)", w.Code, http.StatusFound)
	}
}

func TestRequireAuth_Allows(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")

	called := false
	handler := pc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	user := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{1, false, false, false, false, false})
	r := requestWithUser(user)
	handler.ServeHTTP(w, r)

	if !called {
		t.Error("handler should be called for authenticated user")
	}
}

func TestRequireAuth_BlocksAnonymous(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")

	called := false
	handler := pc.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	w := httptest.NewRecorder()
	r := requestWithUser(models.AnonymousUser())
	handler.ServeHTTP(w, r)

	if called {
		t.Error("handler should not be called for anonymous user")
	}
	if w.Code != http.StatusFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusFound)
	}
}

func TestHasPermission_UnknownPermission(t *testing.T) {
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")
	r := requestWithUser(models.AnonymousUser())

	if pc.HasPermission(r, "nonexistent") {
		t.Error("unknown permission should return false")
	}
}
