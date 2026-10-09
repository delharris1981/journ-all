package store

import (
	"database/sql"
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

	schema := []string{
		`CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE tags (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			UNIQUE(user_id, name)
		)`,
		`CREATE TABLE entry_tags (
			entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (entry_id, tag_id)
		)`,
	}
	for _, s := range schema {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("schema: %v", err)
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
