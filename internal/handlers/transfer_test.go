package handlers

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"journall/internal/store"

	_ "modernc.org/sqlite"
)

// newTestStore builds a Store over a database with the real migrations
// applied, so export/import sees the same schema as production. Paths come
// from this file's location rather than a relative hop, so the test does not
// depend on the working directory the runner was started in.
func newTestStore(t *testing.T) (*sql.DB, int64) {
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
	if _, err := db.Exec(`INSERT INTO users (id, email, password_hash) VALUES (1,'a@example.com','x')`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return db, 1
}

// transferHandler is the only part of Handler these tests touch: export and
// import use nothing but the store.
func transferHandler(t *testing.T, db *sql.DB) *Handler {
	t.Helper()
	return &Handler{store: store.New(db)}
}

func allEntries() store.Filters { return store.Filters{Limit: -1} }

func searching(q string) store.Filters { return store.Filters{Query: q} }

// mustDB builds a throwaway database for tests that only need a Handler to
// call writeZip on, and never inspect the result.
func mustDB(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := newTestStore(t)
	return db
}

func TestSlugify(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Sourdough Starter", "sourdough-starter"},
		{"  Trimmed  ", "trimmed"},
		{"Multiple   spaces", "multiple-spaces"},
		{"punctuation!? gone", "punctuation-gone"},
		{"--leading and trailing--", "leading-and-trailing"},
		{"Café crème", "caf-cr-me"},
		{"", ""},
		{"!!!", ""},
		{"MiXeD CaSe", "mixed-case"},
		{"snake_case and kebab-case", "snake-case-and-kebab-case"},
		{"2024-03-05 standup", "2024-03-05-standup"},
	} {
		if got := slugify(tc.in); got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Titles can be arbitrarily long, and a filename cannot. slugify truncates at
// 60 so the export does not hit filesystem limits.
func TestSlugifyTruncates(t *testing.T) {
	long := strings.Repeat("a", 100)
	if got := slugify(long); len(got) != 60 {
		t.Errorf("len = %d, want 60", len(got))
	}

	// Truncation must not leave a trailing dash, which would be an empty
	// segment in the filename.
	longDash := strings.Repeat("a", 59) + " " + strings.Repeat("b", 10)
	got := slugify(longDash)
	if strings.HasSuffix(got, "-") {
		t.Errorf("slugify(%q) = %q, ends in a dash", longDash, got)
	}
	if len(got) > 60 {
		t.Errorf("len = %d, want <= 60", len(got))
	}
}

// Two entries with the same title must not overwrite each other in the zip,
// or an export silently loses data.
func TestWriteZipCollisions(t *testing.T) {
	day := time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)
	entries := []Entry{
		{ID: 1, Title: "Sourdough", Body: "one", CreatedAt: day},
		{ID: 2, Title: "Sourdough", Body: "two", CreatedAt: day},
		{ID: 3, Title: "Sourdough", Body: "three", CreatedAt: day},
	}

	names, _ := writeZipToNames(t, entries)
	want := []string{
		"2024-03-05-sourdough.md",
		"2024-03-05-sourdough-2.md",
		"2024-03-05-sourdough-3.md",
	}
	assertSameOrder(t, names, want)
}

// Entries whose titles slugify to the same name collide even when the titles
// differ — the disambiguation has to be on the slug, not the title.
func TestWriteZipCollisionsAfterSlug(t *testing.T) {
	day := time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)
	entries := []Entry{
		{ID: 1, Title: "Hello, World", Body: "a", CreatedAt: day},
		{ID: 2, Title: "hello world", Body: "b", CreatedAt: day},
	}

	names, _ := writeZipToNames(t, entries)
	assertSameOrder(t, names, []string{"2024-03-05-hello-world.md", "2024-03-05-hello-world-2.md"})
}

// A title with nothing sluggable in it (all punctuation, or empty) has no
// filename to derive, so the entry id stands in.
func TestWriteZipEmptySlugFallsBackToID(t *testing.T) {
	day := time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)
	entries := []Entry{
		{ID: 7, Title: "!!!", Body: "a", CreatedAt: day},
		{ID: 8, Title: "", Body: "b", CreatedAt: day},
	}

	names, _ := writeZipToNames(t, entries)
	assertSameOrder(t, names, []string{"2024-03-05-entry-7.md", "2024-03-05-entry-8.md"})
}

// An entry with no timestamp has no date prefix; that is a store quirk, but
// writeZip must not panic on it.
func TestWriteZipZeroDate(t *testing.T) {
	names, _ := writeZipToNames(t, []Entry{{ID: 1, Title: "No date", Body: "b"}})

	if want := "no-date.md"; names[0] != want {
		t.Errorf("name = %q, want %q", names[0], want)
	}
}

func TestWriteZipEmptyList(t *testing.T) {
	var buf bytes.Buffer
	h := transferHandler(t, mustDB(t))
	if err := h.writeZip(&buf, nil); err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}
	if len(zr.File) != 0 {
		t.Errorf("empty export wrote %d files", len(zr.File))
	}
}

func TestWriteZipContent(t *testing.T) {
	day := time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)
	entries := []Entry{{ID: 1, Title: "Sourdough", Body: "baking bread", CreatedAt: day}}

	files := writeZipToFiles(t, entries)
	if got, want := files["2024-03-05-sourdough.md"], "# Sourdough\n\nbaking bread\n"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
}

func TestTitleAndBody(t *testing.T) {
	for _, tc := range []struct {
		name, in, wantTitle, wantBody string
	}{
		{"heading", "# Sourdough\n\nbaking bread", "Sourdough", "baking bread"},
		{"crlf", "# Sourdough\r\n\r\nbaking bread", "Sourdough", "baking bread"},
		{"no blank line", "# Sourdough\nbaking bread", "Sourdough", "baking bread"},
		{"heading only", "# Sourdough", "Sourdough", ""},
		{"no heading returns whole file", "baking bread\n", "", "baking bread\n"},
		{"empty", "", "", ""},
		// An empty heading yields an empty title, so importEntries falls back
		// to the filename — but the body is still the real content, not the
		// heading line.
		{"empty heading", "# \n\nbody", "", "body"},
	} {
		title, body := titleAndBody(tc.in)
		if title != tc.wantTitle {
			t.Errorf("%s: title = %q, want %q", tc.name, title, tc.wantTitle)
		}
		if body != tc.wantBody {
			t.Errorf("%s: body = %q, want %q", tc.name, body, tc.wantBody)
		}
	}
}

// Only a leading "# " counts. A "#" further down the file is body text, and
// "#tag" is not a heading.
func TestTitleAndBodyIgnoresLaterHeadings(t *testing.T) {
	in := "intro text\n\n# Not a title\n\nmore"
	title, body := titleAndBody(in)
	if title != "" {
		t.Errorf("title = %q, want empty", title)
	}
	if body != in {
		t.Errorf("body = %q, want the whole file", body)
	}
}

// A zip written by export must import back into the same entries. This is the
// one guarantee that makes export worth having.
func TestExportImportRoundTrip(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	original := []Entry{
		{ID: 1, Title: "Sourdough", Body: "baking bread", CreatedAt: time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)},
		{ID: 2, Title: "Café crème!", Body: "two\n\nparagraphs", CreatedAt: time.Date(2024, 3, 6, 9, 0, 0, 0, time.UTC)},
		{ID: 3, Title: "Meeting notes", Body: "discussed the roadmap", CreatedAt: time.Date(2024, 3, 7, 9, 0, 0, 0, time.UTC)},
	}

	var buf bytes.Buffer
	if err := h.writeZip(&buf, original); err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	n, err := h.importEntries(userID, buf.Bytes())
	if err != nil {
		t.Fatalf("importEntries: %v", err)
	}
	if n != len(original) {
		t.Fatalf("imported %d entries, want %d", n, len(original))
	}

	imported, err := h.store.GetEntries(userID, allEntries())
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}

	byTitle := map[string]string{}
	for _, e := range imported {
		byTitle[e.Title] = e.Body
	}
	for _, want := range original {
		body, ok := byTitle[want.Title]
		if !ok {
			t.Errorf("entry %q missing after round trip", want.Title)
			continue
		}
		if strings.TrimRight(body, "\n") != strings.TrimRight(want.Body, "\n") {
			t.Errorf("entry %q body = %q, want %q", want.Title, body, want.Body)
		}
	}
}

// importEntries only looks at .md files. A zip with anything else in it is
// normal — a stray README, a nested directory — and must not become an entry.
func TestImportEntriesSkipsNonMarkdown(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	zipData := buildZip(t, map[string]string{
		"notes.md":       "# Notes\n\nbody",
		"README.txt":     "not an entry",
		"photo.png":      "not an entry",
		"sub/dir.md":     "# Nested\n\nbody",
		"archive.tar.md": "# Actually markdown\n\nbody",
		"UPPERCASE.MD":   "# Upper\n\nbody",
		"no-extension":   "ignored",
		"two.two.md.md":  "# Double\n\nbody",
	})

	n, err := h.importEntries(userID, zipData)
	if err != nil {
		t.Fatalf("importEntries: %v", err)
	}

	// notes.md, sub/dir.md, archive.tar.md, UPPERCASE.MD, two.two.md.md
	if n != 5 {
		t.Errorf("imported %d entries, want 5", n)
	}
}

func TestImportEntriesRejectsBadZip(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	if _, err := h.importEntries(userID, []byte("this is not a zip")); err == nil {
		t.Error("importEntries accepted data that is not a zip")
	}
}

// An empty zip is valid and imports nothing, rather than erroring.
func TestImportEntriesEmptyZip(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	n, err := h.importEntries(userID, buildZip(t, nil))
	if err != nil {
		t.Fatalf("importEntries: %v", err)
	}
	if n != 0 {
		t.Errorf("imported %d entries from an empty zip, want 0", n)
	}
}

// A file with no "# " heading falls back to its filename.
func TestImportEntriesTitleFromFilename(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	zipData := buildZip(t, map[string]string{
		"2024-03-05-my-note.md": "just a body, no heading",
	})

	if _, err := h.importEntries(userID, zipData); err != nil {
		t.Fatalf("importEntries: %v", err)
	}

	got, err := h.store.GetEntries(userID, allEntries())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("imported %d entries, want 1", len(got))
	}
	if want := "2024-03-05-my-note"; got[0].Title != want {
		t.Errorf("title = %q, want %q", got[0].Title, want)
	}
	if got[0].Body != "just a body, no heading" {
		t.Errorf("body = %q", got[0].Body)
	}
}

func TestImportEntriesDuplicateTitlesInZip(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	// writeZip disambiguates filenames, but a hand-made zip may not.
	zipData := buildZip(t, map[string]string{
		"a.md": "# Same\n\nfirst",
		"b.md": "# Same\n\nsecond",
	})

	if _, err := h.importEntries(userID, zipData); err != nil {
		t.Fatalf("importEntries: %v", err)
	}
	got, err := h.store.GetEntries(userID, allEntries())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("imported %d entries with the same title, want 2", len(got))
	}
}

// Imported entries belong to the user who uploaded them and are searchable.
func TestImportEntriesIndexedForUser(t *testing.T) {
	db, userID := newTestStore(t)
	h := transferHandler(t, db)

	zipData := buildZip(t, map[string]string{"n.md": "# Sourdough\n\nbaking bread"})
	if _, err := h.importEntries(userID, zipData); err != nil {
		t.Fatalf("importEntries: %v", err)
	}

	got, err := h.store.GetEntries(userID, searching("sourdough"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("imported entry is not in the search index (got %d matches)", len(got))
	}
}

func writeZipToNames(t *testing.T, entries []Entry) ([]string, map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	h := transferHandler(t, mustDB(t))
	if err := h.writeZip(&buf, entries); err != nil {
		t.Fatalf("writeZip: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("output is not a valid zip: %v", err)
	}
	var names []string
	files := map[string]string{}
	for _, f := range zr.File {
		names = append(names, f.Name)
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}
	return names, files
}

func writeZipToFiles(t *testing.T, entries []Entry) map[string]string {
	t.Helper()
	_, files := writeZipToNames(t, entries)
	return files
}

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func assertSameOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d files %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("file %d = %q, want %q", i, got[i], want[i])
		}
	}
}
