package handlers

import (
	"bytes"
	"database/sql"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"journall/internal/auth"

	_ "modernc.org/sqlite"
)

// testServer drives the real router over a migrated database with a live
// session, so these tests exercise routing, the auth middleware and template
// rendering together. Unit tests on individual functions cannot catch a route
// pointing at the wrong handler or a template that no longer parses.
type testServer struct {
	http  *httptest.Server
	token string
}

// newTestServer seeds an admin, starts the app, and logs that admin in.
func newTestServer(t *testing.T) *testServer {
	t.Helper()
	db := migratedDB(t)
	userID := seedUserRow(t, db, "a@example.com", 1, 0)
	return startServer(t, db, userID)
}

// newTestServerAs starts the app as a plain, non-admin user.
func newTestServerAs(t *testing.T, email string) *testServer {
	t.Helper()
	db := migratedDB(t)
	userID := seedUserRow(t, db, email, 0, 0)
	return startServer(t, db, userID)
}

// newTestServerLoggedOut starts the app with no session cookie.
func newTestServerLoggedOut(t *testing.T) *testServer {
	t.Helper()
	return startServer(t, migratedDB(t), 0)
}

func startServer(t *testing.T, db *sql.DB, userID int64) *testServer {
	t.Helper()

	srv := &testServer{http: httptest.NewServer(New(db).Routes())}
	t.Cleanup(srv.http.Close)

	if userID != 0 {
		token, err := auth.CreateSession(db, userID)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		srv.token = token
	}
	return srv
}

// migratedDB opens a database with the real migrations applied. Paths come
// from this file's location rather than a relative hop, so the test does not
// depend on the working directory the runner was started in. New() reads
// templates relative to the cwd, which has to be the repo root.
func migratedDB(t *testing.T) *sql.DB {
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
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	for _, m := range []string{"0001_init.up.sql", "0002_admin.up.sql", "0003_fts.up.sql"} {
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

func seedUserRow(t *testing.T, db *sql.DB, email string, isAdmin, disabled int) int64 {
	t.Helper()
	res, err := db.Exec(
		"INSERT INTO users (email, password_hash, is_admin, disabled) VALUES (?, 'x', ?, ?)",
		email, isAdmin, disabled,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func (s *testServer) do(t *testing.T, method, path string, form url.Values) *http.Response {
	t.Helper()

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequest(method, s.http.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return s.send(t, req)
}

func (s *testServer) send(t *testing.T, req *http.Request) *http.Response {
	t.Helper()
	if s.token != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: s.token})
	}
	rec := httptest.NewRecorder()
	s.http.Config.Handler.ServeHTTP(rec, req)
	return rec.Result()
}

func TestRoutesEntryLifecycle(t *testing.T) {
	srv := newTestServer(t)

	// Create
	rec := srv.do(t, "POST", "/entries", url.Values{
		"title": {"Sourdough"},
		"body":  {"baking bread"},
		"tags":  {"baking"},
	})
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("create: status %d, want 303", rec.StatusCode)
	}
	loc := rec.Header.Get("Location")
	if !strings.HasPrefix(loc, "/entries/") {
		t.Fatalf("create redirected to %q", loc)
	}

	// Read back through the view page.
	rec = srv.do(t, "GET", loc, nil)
	body := readBody(t, rec)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("view: status %d, want 200", rec.StatusCode)
	}
	if !strings.Contains(body, "Sourdough") || !strings.Contains(body, "baking bread") {
		t.Errorf("view page missing entry content")
	}

	// It shows on the home list and the tag page.
	if body := readBody(t, srv.do(t, "GET", "/", nil)); !strings.Contains(body, "/entries/") {
		t.Error("home page does not list the new entry")
	}
	if body := readBody(t, srv.do(t, "GET", "/tags/baking", nil)); !strings.Contains(body, "Sourdough") {
		t.Error("tag page does not list the entry")
	}

	// Edit form is reachable and round-trips an update.
	rec = srv.do(t, "GET", loc+"/edit", nil)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("edit form: status %d, want 200", rec.StatusCode)
	}
	if !strings.Contains(readBody(t, rec), "Sourdough") {
		t.Error("edit form is not populated with the entry")
	}

	rec = srv.do(t, "POST", loc, url.Values{
		"title": {"Rye loaf"},
		"body":  {"starter"},
		"tags":  {"baking"},
	})
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("update: status %d, want 303", rec.StatusCode)
	}
	if body := readBody(t, srv.do(t, "GET", "/", nil)); !strings.Contains(body, "Rye loaf") {
		t.Error("home page does not show the updated title")
	}
	// The old title must be gone from the search index too, not just the page.
	if body := readBody(t, srv.do(t, "GET", "/entries?q=sourdough", nil)); strings.Contains(body, "Rye loaf") {
		t.Error("search still matches the old title after update")
	}

	// Delete
	rec = srv.do(t, "POST", loc+"/delete", nil)
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("delete: status %d, want 303", rec.StatusCode)
	}
	// A deleted entry redirects away rather than 500ing.
	rec = srv.do(t, "GET", loc, nil)
	if rec.StatusCode != http.StatusSeeOther {
		t.Errorf("view after delete: status %d, want 303", rec.StatusCode)
	}
}

func TestRoutesSearchAndDate(t *testing.T) {
	srv := newTestServer(t)
	srv.do(t, "POST", "/entries", url.Values{"title": {"Bread"}, "body": {"sourdough starter"}})
	srv.do(t, "POST", "/entries", url.Values{"title": {"Meeting"}, "body": {"roadmap"}})

	for _, tc := range []struct {
		query string
		want  string
		avoid string
	}{
		{"/entries?q=bread", "Bread", "Meeting"},
		{"/entries?q=sourdough", "Bread", "Meeting"},
		{"/entries?q=roadmap", "Meeting", "Bread"},
		{"/entries?q=zzzz", "", "Bread"},
		// An unparseable query must render the page, not 500.
		{"/entries?q=" + url.QueryEscape(`"broken`), "", "Meeting"},
	} {
		rec := srv.do(t, "GET", tc.query, nil)
		if rec.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d, want 200", tc.query, rec.StatusCode)
			continue
		}
		body := readBody(t, rec)
		if tc.want != "" && !strings.Contains(body, tc.want) {
			t.Errorf("%s: missing %q", tc.query, tc.want)
		}
		if tc.avoid != "" && strings.Contains(body, tc.avoid) {
			t.Errorf("%s: unexpectedly contains %q", tc.query, tc.avoid)
		}
	}

	// The date filter renders its heading and back-link.
	today := time.Now().Format("2006-01-02")
	rec := srv.do(t, "GET", "/entries?date="+today, nil)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("date filter: status %d", rec.StatusCode)
	}
	if body := readBody(t, rec); !strings.Contains(body, "Entries for "+today) {
		t.Errorf("date page missing its heading")
	}
}

func TestRoutesAuthRequired(t *testing.T) {
	srv := newTestServerLoggedOut(t)

	for _, path := range []string{"/", "/entries", "/tags", "/calendar", "/export", "/import", "/admin"} {
		rec := srv.do(t, "GET", path, nil)
		if rec.StatusCode != http.StatusSeeOther {
			t.Errorf("%s: status %d, want 303 to login", path, rec.StatusCode)
			continue
		}
		if loc := rec.Header.Get("Location"); loc != "/login" {
			t.Errorf("%s: redirected to %q, want /login", path, loc)
		}
	}
}

// A non-admin signed in is refused the admin panel with a 403, not a redirect
// and not the page itself.
func TestRoutesAdminForbiddenForNonAdmin(t *testing.T) {
	srv := newTestServerAs(t, "plain@example.com")

	for _, path := range []string{"/admin"} {
		rec := srv.do(t, "GET", path, nil)
		if rec.StatusCode != http.StatusForbidden {
			t.Errorf("%s as non-admin: status %d, want 403", path, rec.StatusCode)
		}
	}
	// A non-admin cannot reach the admin POST routes either.
	for _, path := range []string{"/admin/users", "/admin/signups"} {
		rec := srv.do(t, "POST", path, url.Values{"email": {"x@example.com"}, "password": {"password123"}})
		if rec.StatusCode != http.StatusForbidden {
			t.Errorf("%s as non-admin: status %d, want 403", path, rec.StatusCode)
		}
	}
}

func TestRoutesHealthz(t *testing.T) {
	srv := newTestServer(t)
	rec := srv.do(t, "GET", "/healthz", nil)
	if rec.StatusCode != http.StatusOK {
		t.Errorf("status %d, want 200", rec.StatusCode)
	}
	if body := readBody(t, rec); body != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

// Exporting and re-importing through the HTTP layer is the path a user takes,
// and it must not require an admin.
func TestRoutesExportImportRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	srv.do(t, "POST", "/entries", url.Values{
		"title": {"Sourdough"},
		"body":  {"baking bread"},
		"tags":  {"baking"},
	})

	rec := srv.do(t, "GET", "/export", nil)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("export: status %d", rec.StatusCode)
	}
	zipData, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(zipData) == 0 {
		t.Fatal("export returned an empty body")
	}

	rec = srv.uploadZip(t, "/import", zipData)
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("import: status %d, want 303 (body %s)", rec.StatusCode, readBody(t, rec))
	}
	if loc := rec.Header.Get("Location"); !strings.Contains(loc, "imported=1") {
		t.Errorf("import redirected to %q, want imported=1", loc)
	}

	// Both the original and the imported copy are searchable.
	body := readBody(t, srv.do(t, "GET", "/entries?q=sourdough", nil))
	if strings.Count(body, "Sourdough") < 2 {
		t.Errorf("expected two entries titled Sourdough after round trip")
	}
}

func TestRoutesExportFormats(t *testing.T) {
	srv := newTestServer(t)
	srv.do(t, "POST", "/entries", url.Values{"title": {"Sourdough"}, "body": {"baking"}})

	for _, tc := range []struct{ query, wantType string }{
		{"", "application/zip"},
		{"?format=json", "application/json"},
		{"?format=markdown", "text/markdown"},
	} {
		rec := srv.do(t, "GET", "/export"+tc.query, nil)
		if rec.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", tc.query, rec.StatusCode)
			continue
		}
		if ct := rec.Header.Get("Content-Type"); ct != tc.wantType {
			t.Errorf("%s: content type %q, want %q", tc.query, ct, tc.wantType)
		}
	}

	rec := srv.do(t, "GET", "/export?format=nope", nil)
	if rec.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown format: status %d, want 400", rec.StatusCode)
	}
}

func TestRoutesSignupToggle(t *testing.T) {
	srv := newTestServer(t)

	// Admin toggles signups off.
	rec := srv.do(t, "POST", "/admin/signups", nil)
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("toggle: status %d", rec.StatusCode)
	}

	// A new account can no longer be created.
	rec = srv.do(t, "POST", "/signup", url.Values{
		"email": {"b@example.com"}, "password": {"password123"}, "confirm": {"password123"},
	})
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("signup: status %d, want 200 rendering the form", rec.StatusCode)
	}
	if body := readBody(t, rec); !strings.Contains(body, "Signups are currently disabled") {
		t.Error("signup was allowed while signups were disabled")
	}

	// Toggle back on.
	srv.do(t, "POST", "/admin/signups", nil)
	rec = srv.do(t, "POST", "/signup", url.Values{
		"email": {"b@example.com"}, "password": {"password123"}, "confirm": {"password123"},
	})
	if rec.StatusCode != http.StatusSeeOther {
		t.Errorf("signup after re-enabling: status %d, want 303", rec.StatusCode)
	}
}

func TestRoutesAdminCreateUser(t *testing.T) {
	srv := newTestServer(t)

	rec := srv.do(t, "POST", "/admin/users", url.Values{
		"email": {"new@example.com"}, "password": {"password123"}, "is_admin": {"on"},
	})
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("create user: status %d (body %s)", rec.StatusCode, readBody(t, rec))
	}
	if body := readBody(t, srv.do(t, "GET", "/admin", nil)); !strings.Contains(body, "new@example.com") {
		t.Error("new user is not listed on the admin page")
	}

	// A short password is refused, with the reason on the page.
	rec = srv.do(t, "POST", "/admin/users", url.Values{
		"email": {"short@example.com"}, "password": {"abc"},
	})
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("short password: status %d, want 200", rec.StatusCode)
	}
	if body := readBody(t, rec); !strings.Contains(body, "at least 8 characters") {
		t.Error("short password was not explained on the page")
	}

	// A duplicate email reports an error rather than a silent success.
	rec = srv.do(t, "POST", "/admin/users", url.Values{
		"email": {"new@example.com"}, "password": {"password123"},
	})
	if body := readBody(t, rec); !strings.Contains(body, "already registered") {
		t.Error("duplicate email was not reported on the page")
	}
}

// An admin must not be able to disable their own account and lock everyone out.
func TestRoutesAdminCannotDisableSelf(t *testing.T) {
	srv := newTestServer(t)

	rec := srv.do(t, "POST", "/admin/users/1/disabled", nil)
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("self-disable: status %d, want 200 with an error", rec.StatusCode)
	}
	if body := readBody(t, rec); !strings.Contains(body, "your own account") {
		t.Error("self-disable was not refused")
	}

	// Still an admin afterwards.
	if rec := srv.do(t, "GET", "/admin", nil); rec.StatusCode != http.StatusOK {
		t.Errorf("/admin after self-disable attempt: status %d", rec.StatusCode)
	}
}

func TestRoutesAdminDisablesOtherUser(t *testing.T) {
	srv := newTestServer(t)

	rec := srv.do(t, "POST", "/admin/users", url.Values{
		"email": {"other@example.com"}, "password": {"password123"},
	})
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("create user: status %d", rec.StatusCode)
	}

	rec = srv.do(t, "POST", "/admin/users/2/disabled", nil)
	if rec.StatusCode != http.StatusSeeOther {
		t.Fatalf("disable: status %d", rec.StatusCode)
	}

	// A disabled account cannot log in.
	rec = srv.do(t, "POST", "/login", url.Values{
		"email": {"other@example.com"}, "password": {"password123"},
	})
	if rec.StatusCode == http.StatusSeeOther && rec.Header.Get("Location") == "/" {
		t.Error("disabled account was able to log in")
	}
}

// The pages that only render data still have to render: a template typo in
// any of them is a 500 on a real page.
func TestRoutesStaticPagesRender(t *testing.T) {
	srv := newTestServer(t)
	srv.do(t, "POST", "/entries", url.Values{"title": {"Sourdough"}, "body": {"baking"}, "tags": {"baking"}})

	for _, path := range []string{
		"/", "/entries/new", "/entries/1", "/entries/1/edit",
		"/calendar", "/tags", "/tags/baking", "/import", "/admin",
		"/login", "/signup",
	} {
		rec := srv.do(t, "GET", path, nil)
		if rec.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, rec.StatusCode)
			continue
		}
		body := readBody(t, rec)
		if !strings.Contains(body, "</html>") {
			t.Errorf("GET %s: response is truncated", path)
		}
	}

	// The tag list shows the tag, the calendar renders a month grid, and the
	// new-entry form has the fields the create handler reads.
	if body := readBody(t, srv.do(t, "GET", "/tags", nil)); !strings.Contains(body, "/tags/baking") {
		t.Error("tags page does not link the tag")
	}
	if body := readBody(t, srv.do(t, "GET", "/calendar", nil)); !strings.Contains(body, "calendar") {
		t.Error("calendar page looks empty")
	}
	body := readBody(t, srv.do(t, "GET", "/entries/new", nil))
	for _, field := range []string{`name="title"`, `name="body"`, `name="tags"`} {
		if !strings.Contains(body, field) {
			t.Errorf("new-entry form is missing %s", field)
		}
	}
}

// Signing up with mismatched or short passwords must explain the problem
// rather than failing opaquely.
func TestRoutesSignupValidation(t *testing.T) {
	srv := newTestServerLoggedOut(t)

	rec := srv.do(t, "POST", "/signup", url.Values{
		"email": {"b@example.com"}, "password": {"password123"}, "confirm": {"different"},
	})
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("mismatched: status %d, want 200", rec.StatusCode)
	}
	if body := readBody(t, rec); !strings.Contains(body, "Passwords do not match") {
		t.Error("mismatched passwords were not explained")
	}

	rec = srv.do(t, "POST", "/signup", url.Values{
		"email": {"b@example.com"}, "password": {"short"}, "confirm": {"short"},
	})
	if body := readBody(t, rec); !strings.Contains(body, "at least 8 characters") {
		t.Error("short password was not explained")
	}
}

func TestRoutesLoginLogout(t *testing.T) {
	srv := newTestServer(t)

	rec := srv.do(t, "POST", "/login", url.Values{
		"email": {"a@example.com"}, "password": {"whatever"},
	})
	// The seeded hash is not a real bcrypt hash, so this must not succeed.
	if rec.StatusCode == http.StatusSeeOther && rec.Header.Get("Location") == "/" {
		t.Error("login succeeded with a bogus password hash")
	}

	rec = srv.do(t, "POST", "/logout", nil)
	if rec.StatusCode != http.StatusSeeOther {
		t.Errorf("logout: status %d, want 303", rec.StatusCode)
	}
}

func TestRoutesPreview(t *testing.T) {
	srv := newTestServer(t)

	rec := srv.do(t, "POST", "/preview", url.Values{"body": {"# Heading\n\ntext"}})
	if rec.StatusCode != http.StatusOK {
		t.Fatalf("preview: status %d", rec.StatusCode)
	}
	if body := readBody(t, rec); !strings.Contains(body, "<h1>Heading</h1>") {
		t.Errorf("preview did not render markdown: %s", body)
	}
}

func (s *testServer) uploadZip(t *testing.T, path string, data []byte) *http.Response {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	f, err := mw.CreateFormFile("file", "export.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", s.http.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return s.send(t, req)
}

func readBody(t *testing.T, rec *http.Response) string {
	t.Helper()
	defer rec.Body.Close()
	b, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}
