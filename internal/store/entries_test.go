package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return v
}

// newTestStore creates a store backed by a fresh on-disk database with the
// production schema applied. On-disk (rather than :memory:) because SQLite
// gives each :memory: connection its own database and modernc may open more
// than one.
func newTestStore(t *testing.T) (*Store, int64, int64) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Read the real migrations rather than restating the schema, so a column
	// or trigger added in migrations/ is covered by these tests automatically.
	for _, m := range []string{"0001_init.up.sql", "0002_admin.up.sql", "0003_fts.up.sql"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "migrations", m))
		if err != nil {
			t.Fatalf("read migration %s: %v", m, err)
		}
		if _, err := db.Exec(string(b)); err != nil {
			t.Fatalf("apply migration %s: %v", m, err)
		}
	}

	res, err := db.Exec("INSERT INTO users (email, password_hash) VALUES ('a@example.com', 'x')")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	userID, _ := res.LastInsertId()
	res, err = db.Exec("INSERT INTO users (email, password_hash) VALUES ('b@example.com', 'x')")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	otherID, _ := res.LastInsertId()

	return New(db), userID, otherID
}

func seed(t *testing.T, s *Store, userID int64, title, body, createdAt string) int64 {
	t.Helper()
	res, err := s.db.Exec(
		"INSERT INTO entries (user_id, title, body, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		userID, title, body, createdAt, createdAt,
	)
	if err != nil {
		t.Fatalf("seed entry: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func titles(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Title
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGetEntriesFilters(t *testing.T) {
	s, userID, _ := newTestStore(t)

	seed(t, s, userID, "Sourdough", "baking bread on tuesday", "2024-03-05 09:00:00")
	seed(t, s, userID, "Meeting notes", "discussed the roadmap", "2024-03-12 09:00:00")
	seed(t, s, userID, "Bread talk", "nothing to see", "2024-04-02 09:00:00")

	// No filters returns everything, newest first.
	all, err := s.GetEntries(userID, Filters{})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if want := []string{"Bread talk", "Meeting notes", "Sourdough"}; !equal(titles(all), want) {
		t.Errorf("order = %v, want %v", titles(all), want)
	}

	// Query matches title or body.
	got, err := s.GetEntries(userID, Filters{Query: "bread"})
	if err != nil {
		t.Fatalf("GetEntries query: %v", err)
	}
	if want := []string{"Bread talk", "Sourdough"}; !equal(titles(got), want) {
		t.Errorf("query 'bread' = %v, want %v", titles(got), want)
	}

	// Date selects a single day.
	got, err = s.GetEntries(userID, Filters{Date: "2024-03-05"})
	if err != nil {
		t.Fatalf("GetEntries date: %v", err)
	}
	if want := []string{"Sourdough"}; !equal(titles(got), want) {
		t.Errorf("date filter = %v, want %v", titles(got), want)
	}

	// Query and date compose.
	got, err = s.GetEntries(userID, Filters{Query: "bread", Date: "2024-04-02"})
	if err != nil {
		t.Fatalf("GetEntries combined: %v", err)
	}
	if want := []string{"Bread talk"}; !equal(titles(got), want) {
		t.Errorf("combined = %v, want %v", titles(got), want)
	}

	// A date with no matches is empty, not an error.
	got, err = s.GetEntries(userID, Filters{Date: "2024-01-01"})
	if err != nil {
		t.Fatalf("GetEntries empty date: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty date returned %d entries", len(got))
	}
}

// The date filter used to run in Go *after* a LIMIT 100 fetch of the newest
// entries, so a date with older-than-100 entries matched nothing at all.
// Filtering in SQL means the limit applies to the matching set.
func TestGetEntriesDateNotTruncatedByLimit(t *testing.T) {
	s, userID, _ := newTestStore(t)

	// 120 entries on the target date...
	for i := 0; i < 120; i++ {
		seed(t, s, userID, "old", "b", "2024-01-01 09:00:00")
	}
	// ...and 120 newer entries on a different date, which used to fill the
	// entire LIMIT window and hide every one of the target-date entries.
	for i := 0; i < 120; i++ {
		seed(t, s, userID, "new", "b", "2024-02-01 09:00:00")
	}

	got, err := s.GetEntries(userID, Filters{Date: "2024-01-01"})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("date filter returned nothing — matches were truncated by LIMIT")
	}
	if len(got) != DefaultLimit {
		t.Errorf("got %d entries, want %d (limit applies to the matching set)", len(got), DefaultLimit)
	}
	for _, e := range got {
		if e.Title != "old" {
			t.Errorf("date filter returned %q from another date", e.Title)
		}
	}
}

func TestGetEntriesLimit(t *testing.T) {
	s, userID, _ := newTestStore(t)
	for i := 0; i < 5; i++ {
		seed(t, s, userID, "e", "b", "2024-03-01 09:00:00")
	}

	got, _ := s.GetEntries(userID, Filters{Limit: 2})
	if len(got) != 2 {
		t.Errorf("Limit 2 returned %d entries", len(got))
	}

	// Negative limit means unbounded (used by tag pages).
	got, _ = s.GetEntries(userID, Filters{Limit: -1})
	if len(got) != 5 {
		t.Errorf("Limit -1 returned %d entries, want 5", len(got))
	}
}

// The index is an external-content FTS5 table, so SQLite does not maintain it
// for us — the migration's triggers are the only thing keeping it correct.
func TestSearchIndexFollowsWrites(t *testing.T) {
	s, userID, _ := newTestStore(t)

	id := seed(t, s, userID, "Sourdough", "starter", "2024-03-01 09:00:00")

	// Seeded via raw INSERT, so the AFTER INSERT trigger must have indexed it.
	got, err := s.GetEntries(userID, Filters{Query: "sourdough"})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if want := []string{"Sourdough"}; !equal(titles(got), want) {
		t.Fatalf("after insert = %v, want %v", titles(got), want)
	}

	// An update must remove the old text from the index, not just add the new.
	if _, err := s.UpdateEntry(userID, id, "Rye loaf", "starter"); err != nil {
		t.Fatalf("UpdateEntry: %v", err)
	}
	if got, _ := s.GetEntries(userID, Filters{Query: "sourdough"}); len(got) != 0 {
		t.Errorf("stale title still matches after update: %v", titles(got))
	}
	if got, _ := s.GetEntries(userID, Filters{Query: "rye"}); len(got) != 1 {
		t.Errorf("new title not indexed after update: %v", titles(got))
	}

	// And a delete must drop the row entirely.
	if _, err := s.DeleteEntry(userID, id); err != nil {
		t.Fatalf("DeleteEntry: %v", err)
	}
	if got, _ := s.GetEntries(userID, Filters{Query: "rye"}); len(got) != 0 {
		t.Errorf("deleted entry still matches: %v", titles(got))
	}
}

// A user deletion cascades to entries. Triggers do not fire for cascaded
// deletes on some paths, which would leave an orphan row in the index matching
// a nonexistent entry — worth pinning down.
func TestSearchIndexClearedByUserCascade(t *testing.T) {
	s, userID, _ := newTestStore(t)
	seed(t, s, userID, "Orphan candidate", "body", "2024-03-01 09:00:00")

	if _, err := s.db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := s.db.Exec("DELETE FROM users WHERE id = ?", userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM entries_fts").Scan(&n); err != nil {
		t.Fatalf("count fts: %v", err)
	}
	if n != 0 {
		t.Errorf("%d orphaned index rows survived the user cascade", n)
	}
}

// FTS5 tokenizes on word boundaries, so "loaf" finds "Rye loaf" but "af"
// does not. The LIKE fallback would have matched the substring, so pin the
// token behaviour down rather than leaving it implicit.
func TestSearchMatchesWholeWords(t *testing.T) {
	s, userID, _ := newTestStore(t)
	seed(t, s, userID, "Rye loaf", "baking notes", "2024-03-01 09:00:00")

	got, err := s.GetEntries(userID, Filters{Query: "loaf"})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if want := []string{"Rye loaf"}; !equal(titles(got), want) {
		t.Errorf("word query = %v, want %v", titles(got), want)
	}

	if got, _ := s.GetEntries(userID, Filters{Query: "af"}); len(got) != 0 {
		t.Errorf("substring 'af' matched %v, want nothing", titles(got))
	}

	// An explicit prefix query still reaches partial words.
	got, err = s.GetEntries(userID, Filters{Query: `"lo"*`})
	if err != nil {
		t.Fatalf("GetEntries prefix: %v", err)
	}
	if want := []string{"Rye loaf"}; !equal(titles(got), want) {
		t.Errorf("prefix query = %v, want %v", titles(got), want)
	}
}

// A stray quote or bare operator is a syntax error in FTS5, not a search for
// that literal text. Rather than 500, search falls back to a LIKE scan.
func TestSearchFallsBackOnSyntaxError(t *testing.T) {
	s, userID, _ := newTestStore(t)
	seed(t, s, userID, "Sourdough AND rye", "baking", "2024-03-01 09:00:00")
	seed(t, s, userID, "Plain", "nothing", "2024-03-02 09:00:00")

	// Each of these is a syntax error in FTS5 but a plain substring for LIKE.
	// The only requirement is that they don't error.
	for _, q := range []string{`"unbalanced`, "*", `"a" NEAR/ "b"`, "AND"} {
		if _, err := s.GetEntries(userID, Filters{Query: q}); err != nil {
			t.Errorf("query %q returned an error instead of falling back: %v", q, err)
		}
	}

	// "AND" alone finds the entry whose title contains it, via the LIKE path.
	got, err := s.GetEntries(userID, Filters{Query: "AND"})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if want := []string{"Sourdough AND rye"}; !equal(titles(got), want) {
		t.Errorf("fallback results = %v, want %v", titles(got), want)
	}
}

// A hyphen in an ordinary search term reads as a column filter in FTS5 — "mail"
// and "op" are not columns — which is a 500 waiting to happen without the
// fallback. This is the single most likely way a real user breaks the query.
func TestSearchFallbackOnHyphenatedWords(t *testing.T) {
	s, userID, _ := newTestStore(t)
	seed(t, s, userID, "Send e-mail to Sam", "the co-op shares", "2024-03-01 09:00:00")

	for _, tc := range []struct{ query, want string }{
		{"e-mail", "Send e-mail to Sam"},
		{"co-op", "Send e-mail to Sam"},
		{"mail", "Send e-mail to Sam"},
	} {
		got, err := s.GetEntries(userID, Filters{Query: tc.query})
		if err != nil {
			t.Errorf("query %q errored instead of falling back: %v", tc.query, err)
			continue
		}
		if want := []string{tc.want}; !equal(titles(got), want) {
			t.Errorf("query %q = %v, want %v", tc.query, titles(got), want)
		}
	}
}

// A malformed query must not leak another user's entries via the fallback —
// the fallback is the same query with a different WHERE, so scoping has to
// hold on both paths.
func TestSearchFallbackStillScopedToUser(t *testing.T) {
	s, userID, otherID := newTestStore(t)
	seed(t, s, userID, "mine", "b", "2024-03-01 09:00:00")
	seed(t, s, otherID, "theirs AND more", "b", "2024-03-01 09:00:00")

	got, err := s.GetEntries(userID, Filters{Query: "AND"})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	for _, e := range got {
		if e.Title == "theirs AND more" {
			t.Errorf("fallback returned another user's entry")
		}
	}
}

// A non-syntax failure must surface rather than being silently retried as a
// full table scan.
func TestSearchDoesNotMaskRealErrors(t *testing.T) {
	s, userID, _ := newTestStore(t)
	seed(t, s, userID, "Sourdough", "b", "2024-03-01 09:00:00")

	if _, err := s.db.Exec("DROP TABLE entries_fts"); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if _, err := s.GetEntries(userID, Filters{Query: "sourdough"}); err == nil {
		t.Error("search with a missing index returned no error")
	}
}

func TestGetEntriesIsolatesUsers(t *testing.T) {
	s, userID, otherID := newTestStore(t)

	seed(t, s, userID, "mine", "b", "2024-03-01 09:00:00")
	seed(t, s, otherID, "theirs", "b", "2024-03-01 09:00:00")

	got, err := s.GetEntries(userID, Filters{})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if want := []string{"mine"}; !equal(titles(got), want) {
		t.Errorf("got %v, want %v", titles(got), want)
	}
}

// hydrateTags used to run one query per entry; the batched version must
// produce identical results, including ordering by tag name.
func TestTagsHydrateBatched(t *testing.T) {
	s, userID, _ := newTestStore(t)

	a := seed(t, s, userID, "First", "b", "2024-03-01 09:00:00")
	b := seed(t, s, userID, "Second", "b", "2024-03-02 09:00:00")
	c := seed(t, s, userID, "Untagged", "b", "2024-03-03 09:00:00")

	s.SetTags(a, userID, "zebra, apple")
	s.SetTags(b, userID, "apple")

	got, err := s.GetEntries(userID, Filters{})
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}

	want := map[int64][]string{
		a: {"apple", "zebra"},
		b: {"apple"},
		c: nil,
	}
	for _, e := range got {
		var names []string
		for _, tag := range e.Tags {
			names = append(names, tag.Name)
		}
		if !equal(names, want[e.ID]) {
			t.Errorf("entry %d (%s) tags = %v, want %v", e.ID, e.Title, names, want[e.ID])
		}
	}
}

func TestSetTags(t *testing.T) {
	s, userID, _ := newTestStore(t)
	id := seed(t, s, userID, "T", "b", "2024-03-01 09:00:00")

	// Blank and whitespace-padded names are skipped, duplicates collapse.
	s.SetTags(id, userID, " one , , two ,one,  ")
	entry, err := s.GetEntry(userID, id)
	if err != nil {
		t.Fatalf("GetEntry: %v", err)
	}
	if len(entry.Tags) != 2 {
		t.Errorf("got %d tags, want 2: %+v", len(entry.Tags), entry.Tags)
	}

	// Setting tags replaces the previous set rather than adding to it.
	s.SetTags(id, userID, "three")
	entry, _ = s.GetEntry(userID, id)
	if len(entry.Tags) != 1 || entry.Tags[0].Name != "three" {
		t.Errorf("tags after replace = %+v, want [three]", entry.Tags)
	}

	// An empty string clears them all.
	s.SetTags(id, userID, "")
	entry, _ = s.GetEntry(userID, id)
	if len(entry.Tags) != 0 {
		t.Errorf("tags after clear = %+v, want none", entry.Tags)
	}
}

func TestGetEntryOwnership(t *testing.T) {
	s, userID, otherID := newTestStore(t)
	id := seed(t, s, otherID, "theirs", "b", "2024-03-01 09:00:00")

	if _, err := s.GetEntry(userID, id); err != sql.ErrNoRows {
		t.Errorf("GetEntry on another user's entry = %v, want ErrNoRows", err)
	}
}

func TestGetEntriesByTag(t *testing.T) {
	s, userID, _ := newTestStore(t)

	a := seed(t, s, userID, "Tagged", "b", "2024-03-01 09:00:00")
	seed(t, s, userID, "Untagged", "b", "2024-03-02 09:00:00")
	s.SetTags(a, userID, "recipe")

	got, err := s.GetEntries(userID, Filters{Tag: "recipe"})
	if err != nil {
		t.Fatalf("GetEntries by tag: %v", err)
	}
	if want := []string{"Tagged"}; !equal(titles(got), want) {
		t.Errorf("tag filter = %v, want %v", titles(got), want)
	}

	// An unknown tag yields nothing.
	got, _ = s.GetEntries(userID, Filters{Tag: "nope"})
	if len(got) != 0 {
		t.Errorf("unknown tag returned %d entries", len(got))
	}
}

func TestGetAllTagsAndEntryDates(t *testing.T) {
	s, userID, _ := newTestStore(t)

	a := seed(t, s, userID, "One", "b", "2024-03-05 09:00:00")
	seed(t, s, userID, "Two", "b", "2024-03-05 10:00:00")
	seed(t, s, userID, "Three", "b", "2024-03-09 09:00:00")
	s.SetTags(a, userID, "baking, daily")

	tags, err := s.GetAllTags(userID)
	if err != nil {
		t.Fatalf("GetAllTags: %v", err)
	}
	if want := []string{"baking", "daily"}; !equal(
		[]string{tags[0].Name, tags[1].Name}, want) {
		t.Errorf("tags = %v, want %v", tags, want)
	}

	dates, err := s.EntryDates(userID,
		mustTime(t, "2024-03-01T00:00:00Z"), mustTime(t, "2024-04-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("EntryDates: %v", err)
	}
	if !dates["2024-03-05"] || !dates["2024-03-09"] {
		t.Errorf("dates = %v, want 03-05 and 03-09", dates)
	}
	if len(dates) != 2 {
		t.Errorf("got %d dates, want 2 (same-day entries must collapse)", len(dates))
	}
}
