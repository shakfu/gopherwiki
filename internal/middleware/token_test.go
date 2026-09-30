package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sa/gopherwiki/internal/db"
)

// tokenTestHandler wires the middleware chain in route order and reports the
// authenticated user's email, or "token" + email for a token request.
func tokenTestHandler(sm *SessionManager) http.Handler {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := GetUser(r).GetEmail()
		if GetToken(r) != nil {
			who = "token:" + who
		}
		w.Write([]byte(who))
	})
	return sm.Middleware(sm.TokenAuth(sm.CSRFProtect(final)))
}

func createTestToken(t *testing.T, database *db.Database, userID int64) string {
	t.Helper()
	token, err := database.Queries.CreateAPIToken(context.Background(), userID, "test", "kb")
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}
	return token
}

func bearerRequest(method, path, token string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestTokenAuth_AuthenticatesAPIRequestWithoutCSRF(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)
	userID := createTestUser(t, database, "Agent", "agent@example.com")
	token := createTestToken(t, database, userID)

	w := httptest.NewRecorder()
	tokenTestHandler(sm).ServeHTTP(w, bearerRequest("PUT", "/-/api/v1/pages/kb/x", token))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "token:agent@example.com" {
		t.Errorf("authenticated as %q, want token:agent@example.com", got)
	}

	tokens, err := database.Queries.ListAPITokens(context.Background())
	if err != nil {
		t.Fatalf("ListAPITokens failed: %v", err)
	}
	if !tokens[0].LastUsedAt.Valid {
		t.Error("token use should be recorded")
	}
}

func TestTokenAuth_RejectsUnknownToken(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)

	w := httptest.NewRecorder()
	tokenTestHandler(sm).ServeHTTP(w, bearerRequest("GET", "/-/api/v1/pages", "gw_wrong"))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestTokenAuth_IgnoredOutsideAPI(t *testing.T) {
	database := openTestDB(t)
	sm := newTestSessionManager(t, database)
	userID := createTestUser(t, database, "Agent", "agent@example.com")
	token := createTestToken(t, database, userID)
	h := tokenTestHandler(sm)

	// A read stays anonymous.
	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerRequest("GET", "/kb/report", token))
	if got := w.Body.String(); got != "" {
		t.Errorf("authenticated as %q outside the API, want anonymous", got)
	}

	// A form endpoint such as render still needs a CSRF token.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, bearerRequest("POST", "/kb/report/render", token))
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestHasPermission_TokenNeverAdmin(t *testing.T) {
	admin := makeUser(struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}{ID: 1, Approved: true, Admin: true})
	pc := newChecker("ANONYMOUS", "REGISTERED", "REGISTERED")

	req := requestWithUser(admin)
	if !pc.HasPermission(req, PermissionAdmin) {
		t.Fatal("an admin session should have admin permission")
	}

	req = req.WithContext(context.WithValue(req.Context(), TokenKey, &db.APIToken{UserID: 1}))
	if pc.HasPermission(req, PermissionAdmin) {
		t.Error("an admin's token must not have admin permission")
	}
	if !pc.HasPermission(req, PermissionWrite) {
		t.Error("an admin's token should keep write permission")
	}
}

func TestCanReview(t *testing.T) {
	type opts = struct {
		ID          int64
		Approved    bool
		Admin       bool
		AllowRead   bool
		AllowWrite  bool
		AllowUpload bool
	}
	// Review is never open to anonymous users, whatever the access settings.
	pc := newChecker("ANONYMOUS", "ANONYMOUS", "ANONYMOUS")

	reviewer := makeUser(opts{ID: 1, Approved: true})
	reviewer.AllowReview = db.NullBool(true)
	unapproved := makeUser(opts{ID: 2})
	unapproved.AllowReview = db.NullBool(true)

	cases := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"anonymous", requestWithUser(nil), false},
		{"writer without review", requestWithUser(makeUser(opts{ID: 3, Approved: true, AllowWrite: true})), false},
		{"approved reviewer", requestWithUser(reviewer), true},
		{"unapproved reviewer", requestWithUser(unapproved), false},
		{"admin", requestWithUser(makeUser(opts{ID: 4, Admin: true})), true},
	}
	for _, c := range cases {
		if got := pc.HasPermission(c.req, PermissionReview); got != c.want {
			t.Errorf("%s: review permission = %v, want %v", c.name, got, c.want)
		}
	}

	req := requestWithUser(reviewer)
	req = req.WithContext(context.WithValue(req.Context(), TokenKey, &db.APIToken{UserID: 1}))
	if pc.HasPermission(req, PermissionReview) {
		t.Error("a reviewer's token must not have review permission")
	}
}
