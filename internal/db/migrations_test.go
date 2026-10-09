package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"journall/internal/store"
)

// chdirRepo moves to the repo root, because Open resolves migrations relative
// to the working directory.
func chdirRepo(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	if err := os.Chdir(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
}

func seedUser(t *testing.T, d *sql.DB) {
	t.Helper()
	if _, err := d.Exec(`INSERT OR IGNORE INTO users (id, email, password_hash) VALUES (1,'a@example.com','x')`); err != nil {
		t.Fatal(err)
	}
}

// An existing database upgrading to 0003 has to have its current entries
// copied into the new index, or search silently returns nothing for every
// entry written before the upgrade until each one is edited.
func TestUpgradeBackfillsSearchIndex(t *testing.T) {
	chdirRepo(t)
	path := filepath.Join(t.TempDir(), "app.db")

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedUser(t, d)
	if _, err := d.Exec(`INSERT INTO entries (id, user_id, title, body) VALUES (1,1,'Sourdough starter','feeding schedule')`); err != nil {
		t.Fatal(err)
	}

	// Rewind to pre-0003: drop the index and tell migrate the schema is at v2.
	for _, q := range []string{
		`DROP TRIGGER entries_fts_insert`,
		`DROP TRIGGER entries_fts_delete`,
		`DROP TRIGGER entries_fts_update`,
		`DROP TABLE entries_fts`,
		`DELETE FROM schema_migrations`,
		`INSERT INTO schema_migrations (version, dirty) VALUES (2, 0)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	d.Close()

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	defer upgraded.Close()

	got, err := store.New(upgraded).GetEntries(1, store.Filters{Query: "sourdough"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("pre-existing entry not backfilled into the search index (got %d matches)", len(got))
	}
}

// Reopening a migrated database must be a no-op, not a repeat of 0003.
func TestOpenIsIdempotent(t *testing.T) {
	chdirRepo(t)
	path := filepath.Join(t.TempDir(), "app.db")

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	seedUser(t, d)
	if _, err := d.Exec(`INSERT INTO entries (user_id, title, body) VALUES (1,'Rye loaf','starter')`); err != nil {
		t.Fatal(err)
	}
	d.Close()

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	var n int
	if err := reopened.QueryRow(`SELECT count(*) FROM entries_fts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("entries_fts holds %d rows after reopening, want 1", n)
	}
	got, err := store.New(reopened).GetEntries(1, store.Filters{Query: "rye"})
	if err != nil || len(got) != 1 {
		t.Errorf("search after reopen returned %d entries (err %v), want 1", len(got), err)
	}
}
