package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// benchStore applies the real migrations so the benchmark measures the same
// schema the app runs on, FTS5 index included.
func benchStore(b testing.TB, n int) *Store {
	b.Helper()
	dir := b.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "b.db"))
	if err != nil {
		b.Fatal(err)
	}
	for _, m := range []string{"0001_init.up.sql", "0003_fts.up.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", m))
		if err != nil {
			b.Fatalf("read %s: %v", m, err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			b.Fatalf("migration %s: %v", m, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO users (email, password_hash) VALUES ('a','x')`); err != nil {
		b.Fatalf("seed user: %v", err)
	}
	st := New(db)
	for i := 0; i < n; i++ {
		id, _ := st.CreateEntry(1, fmt.Sprintf("entry %d", i), "body text here")
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

// Search goes through the FTS5 index rather than scanning every row with LIKE.
func BenchmarkGetEntriesQuery(b *testing.B) {
	for _, n := range []int{50, 100, 500} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			s := benchStore(b, n)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := s.GetEntries(1, Filters{Query: "entry"}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
