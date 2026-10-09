package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// benchStore mirrors the production schema, including the entry_tags primary
// key that provides the index on entry_id.
func benchStore(b testing.TB, n int) *Store {
	dir := b.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "b.db"))
	if err != nil {
		b.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL)`,
		`CREATE TABLE entries (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL DEFAULT '', body TEXT NOT NULL DEFAULT '',
		 created_at TEXT NOT NULL DEFAULT (datetime('now')), updated_at TEXT NOT NULL DEFAULT (datetime('now')))`,
		`CREATE TABLE tags (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL)`,
		`CREATE TABLE entry_tags (
			entry_id INTEGER NOT NULL REFERENCES entries(id) ON DELETE CASCADE,
			tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
			PRIMARY KEY (entry_id, tag_id))`,
		`CREATE INDEX idx_entries_user ON entries(user_id)`,
		`CREATE INDEX idx_entries_created ON entries(created_at)`,
		`INSERT INTO users (email, password_hash) VALUES ('a','x')`,
	} {
		if _, err := db.Exec(s); err != nil {
			b.Fatalf("schema: %v", err)
		}
	}
	st := New(db)
	for i := 0; i < n; i++ {
		id, _ := st.CreateEntry(1, "title", "body")
		st.SetTags(id, 1, "alpha,beta")
	}
	return st
}

// Tags are hydrated in one query for the whole page rather than one per
// entry, so cost scales with rows returned, not query count.
func BenchmarkGetEntries(b *testing.B) {
	for _, n := range []int{50, 100, 500} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s := benchStore(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.GetEntries(1, Filters{Limit: -1}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
