package auth

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestDB applies the real migrations so these tests see the same session
// and user columns as production, including the disabled flag that gates login.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..")

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, m := range []string{"0001_init.up.sql", "0002_admin.up.sql"} {
		body, err := os.ReadFile(filepath.Join(root, "migrations", m))
		if err != nil {
			t.Fatalf("read %s: %v", m, err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("apply %s: %v", m, err)
		}
	}
	return db
}

func seedUser(t *testing.T, db *sql.DB, email string, isAdmin, disabled bool) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO users (email, password_hash, is_admin, disabled) VALUES (?, 'x', ?, ?)",
		email, boolToInt(isAdmin), boolToInt(disabled),
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !CheckPassword(hash, "correct horse battery") {
		t.Error("CheckPassword rejected the right password")
	}
	if CheckPassword(hash, "wrong password") {
		t.Error("CheckPassword accepted the wrong password")
	}
	if CheckPassword(hash, "") {
		t.Error("CheckPassword accepted an empty password")
	}

	// bcrypt salts, so the same password hashes differently each time.
	other, _ := HashPassword("correct horse battery")
	if hash == other {
		t.Error("two hashes of the same password are identical — no salt")
	}

	// The hash must not contain the password in the clear.
	if strings.Contains(hash, "correct horse") {
		t.Error("hash contains the plaintext password")
	}
}

func TestCheckPasswordRejectsMalformedHash(t *testing.T) {
	for _, hash := range []string{"", "not-a-hash", "$2a$short"} {
		if CheckPassword(hash, "anything") {
			t.Errorf("CheckPassword(%q) returned true for a malformed hash", hash)
		}
	}
}

func TestCreateAndResolveSession(t *testing.T) {
	db := newTestDB(t)
	userID := seedUser(t, db, "a@example.com", false, false)

	token, err := CreateSession(db, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if token == "" {
		t.Fatal("CreateSession returned an empty token")
	}

	got, err := UserIDFromSession(db, token)
	if err != nil {
		t.Fatalf("UserIDFromSession: %v", err)
	}
	if got != userID {
		t.Errorf("user id = %d, want %d", got, userID)
	}
}

func TestCreateSessionTokensAreUnique(t *testing.T) {
	db := newTestDB(t)
	userID := seedUser(t, db, "a@example.com", false, false)

	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		token, err := CreateSession(db, userID)
		if err != nil {
			t.Fatal(err)
		}
		if seen[token] {
			t.Fatalf("CreateSession repeated a token after %d draws", i)
		}
		seen[token] = true
	}
}

func TestUserIDFromSessionUnknownToken(t *testing.T) {
	db := newTestDB(t)

	if _, err := UserIDFromSession(db, "nope"); err != sql.ErrNoRows {
		t.Errorf("unknown token = %v, want ErrNoRows", err)
	}
}

func TestDeleteSession(t *testing.T) {
	db := newTestDB(t)
	userID := seedUser(t, db, "a@example.com", false, false)
	token, _ := CreateSession(db, userID)

	if err := DeleteSession(db, token); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := UserIDFromSession(db, token); err != sql.ErrNoRows {
		t.Errorf("deleted token still resolves (err %v)", err)
	}
}

// An expired session must not authenticate, or logout-by-timeout never happens.
func TestExpiredSessionRejected(t *testing.T) {
	db := newTestDB(t)
	userID := seedUser(t, db, "a@example.com", false, false)

	token := "expired-token"
	if _, err := db.Exec(
		"INSERT INTO sessions (token, user_id, expires_at) VALUES (?, ?, ?)",
		token, userID, time.Now().Add(-time.Hour).Format(time.RFC3339),
	); err != nil {
		t.Fatal(err)
	}

	if _, err := UserIDFromSession(db, token); err != sql.ErrNoRows {
		t.Errorf("expired session = %v, want ErrNoRows", err)
	}
}

// Disabling an account has to invalidate its live sessions immediately, not
// just block new logins.
func TestDisabledUserSessionRejected(t *testing.T) {
	db := newTestDB(t)
	userID := seedUser(t, db, "a@example.com", false, false)
	token, _ := CreateSession(db, userID)

	if _, err := UserIDFromSession(db, token); err != nil {
		t.Fatalf("session before disable: %v", err)
	}

	if _, err := db.Exec("UPDATE users SET disabled = 1 WHERE id = ?", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := UserIDFromSession(db, token); err != sql.ErrNoRows {
		t.Errorf("disabled user's session = %v, want ErrNoRows", err)
	}
}

// Deleting a user cascades to their sessions, so a deleted account cannot keep
// using an old cookie.
func TestDeletedUserSessionRejected(t *testing.T) {
	db := newTestDB(t)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	userID := seedUser(t, db, "a@example.com", false, false)
	token, _ := CreateSession(db, userID)

	if _, err := db.Exec("DELETE FROM users WHERE id = ?", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := UserIDFromSession(db, token); err != sql.ErrNoRows {
		t.Errorf("deleted user's session = %v, want ErrNoRows", err)
	}
}

func TestIsAdmin(t *testing.T) {
	db := newTestDB(t)
	admin := seedUser(t, db, "admin@example.com", true, false)
	plain := seedUser(t, db, "user@example.com", false, false)

	if ok, err := IsAdmin(db, admin); err != nil || !ok {
		t.Errorf("IsAdmin(admin) = %v, %v; want true", ok, err)
	}
	if ok, err := IsAdmin(db, plain); err != nil || ok {
		t.Errorf("IsAdmin(plain) = %v, %v; want false", ok, err)
	}
	// A missing user is an error, not a silent false that would read as
	// "not an admin" and let a caller treat the request as anonymous.
	if _, err := IsAdmin(db, 999); err == nil {
		t.Error("IsAdmin on an unknown user returned no error")
	}
}

func TestMiddleware(t *testing.T) {
	db := newTestDB(t)
	userID := seedUser(t, db, "a@example.com", false, false)
	token, _ := CreateSession(db, userID)

	// A shared handler that records what Middleware put in the context, so a
	// subtest can assert both that the request was refused and that nothing was
	// left in the context.
	var seen int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = GetUserID(r)
		w.WriteHeader(http.StatusOK)
	})
	h := Middleware(db, next)

	t.Run("valid session passes through", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: token})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
		if seen != userID {
			t.Errorf("user id in context = %d, want %d", seen, userID)
		}
	})

	// Without a usable session every route redirects to the login page.
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no cookie", ""},
		{"unknown token", "nope"},
	} {
		t.Run(tc.name+" redirects", func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tc.token != "" {
				req.AddCookie(&http.Cookie{Name: "session", Value: tc.token})
			}
			seen = 0
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusSeeOther {
				t.Errorf("status = %d, want 303", rec.Code)
			}
			if loc := rec.Header().Get("Location"); loc != "/login" {
				t.Errorf("redirect to %q, want /login", loc)
			}
			// The downstream handler must not run at all.
			if seen != 0 {
				t.Errorf("downstream ran with user id %d, want 0", seen)
			}
		})
	}
}

func TestGetUserIDWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	if got := GetUserID(req); got != 0 {
		t.Errorf("GetUserID outside Middleware = %d, want 0", got)
	}
}
