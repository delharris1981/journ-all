package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"modernc.org/sqlite"
)

// sqliteTimeFormat is how SQLite renders datetime('now') values, which is
// what the entries table stores.
const sqliteTimeFormat = "2006-01-02 15:04:05"

// DefaultLimit caps list queries. Pass Filters{Limit: 0} for no cap.
const DefaultLimit = 100

type Entry struct {
	ID        int64
	Title     string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
	Tags      []Tag
}

type Tag struct {
	ID   int64
	Name string
}

// Filters selects which entries to return. Every field is optional and the
// zero value matches all of the user's entries.
type Filters struct {
	Query string // substring match on title or body
	Tag   string // exact tag name
	Date  string // YYYY-MM-DD, matched against created_at
	Limit int    // 0 means DefaultLimit, negative means no limit
}

func (f Filters) limit() int {
	switch {
	case f.Limit < 0:
		return -1
	case f.Limit == 0:
		return DefaultLimit
	default:
		return f.Limit
	}
}

// GetEntries returns the user's entries matching the filters, newest first,
// each with its tags hydrated.
//
// A text query goes through the FTS5 index. FTS5 query syntax is close enough
// to plain text that ordinary searches just work, but a stray quote or
// operator ("AND", "NEAR/", an unbalanced paren) is a syntax error, and SQLite
// would fail the whole request. Rather than 500 on that, fall back to the
// substring scan the LIKE path used to do.
func (s *Store) GetEntries(userID int64, f Filters) ([]Entry, error) {
	entries, err := s.listEntries(userID, f, true)
	if err != nil && f.Query != "" && isSyntaxError(err) {
		entries, err = s.listEntries(userID, f, false)
	}
	if err != nil {
		return nil, err
	}
	s.hydrateTags(entries)
	return entries, nil
}

// ftsSyntaxErrors are the messages SQLite uses for a MATCH expression it
// cannot make sense of. Each one is triggered by something an ordinary person
// types into a search box, and the LIKE fallback can still answer all of them:
//
//	"fts5: syntax error"      bare AND/OR/NOT/NEAR, unbalanced parens
//	"unrecognized token"      an unknown query prefix such as : or ^
//	"unterminated string"     an odd number of double quotes
//	"unknown special query"   a leading *, e.g. "*thing"
//	"no such column"          the column-filter form, which ordinary words hit
//	                          constantly: "e-mail" parses as column "mail",
//	                          "co-op" as "op", "col:value" as "col"
var ftsSyntaxErrors = []string{
	"fts5: syntax error",
	"unrecognized token",
	"unterminated string",
	"unknown special query",
	"no such column",
}

// isSyntaxError reports whether err is a malformed MATCH expression, which is
// the only class of failure the LIKE fallback can rescue. Anything else — a
// closed database, a missing index table — is worth surfacing.
//
// modernc.org/sqlite reports errors as *sqlite.Error carrying SQLite's numeric
// code rather than as one of the database/sql sentinels, and the code is a
// generic SQLITE_ERROR for all of these, so matching is on the message.
func isSyntaxError(err error) bool {
	var serr *sqlite.Error
	if !errors.As(err, &serr) {
		return false
	}
	msg := strings.ToLower(serr.Error())
	for _, m := range ftsSyntaxErrors {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

// listEntries runs the entry query. When useFTS is false a text query falls
// back to a LIKE substring scan instead of the search index.
func (s *Store) listEntries(userID int64, f Filters, useFTS bool) ([]Entry, error) {
	var where []string
	var args []any

	where = append(where, "e.user_id = ?")
	args = append(args, userID)

	if f.Query != "" {
		if useFTS {
			where = append(where, "e.id IN (SELECT rowid FROM entries_fts WHERE entries_fts MATCH ?)")
			args = append(args, f.Query)
		} else {
			where = append(where, "(e.title LIKE ? OR e.body LIKE ?)")
			like := "%" + f.Query + "%"
			args = append(args, like, like)
		}
	}
	if f.Tag != "" {
		where = append(where, `EXISTS (
			SELECT 1 FROM entry_tags et
			JOIN tags t ON t.id = et.tag_id
			WHERE et.entry_id = e.id AND t.name = ?
		)`)
		args = append(args, f.Tag)
	}
	if f.Date != "" {
		where = append(where, "date(e.created_at) = ?")
		args = append(args, f.Date)
	}

	// SQLite treats a negative LIMIT as unbounded, so -1 needs no special case.
	q := fmt.Sprintf(`
		SELECT e.id, e.title, e.body, e.created_at, e.updated_at
		FROM entries e
		WHERE %s
		ORDER BY e.created_at DESC
		LIMIT ?
	`, strings.Join(where, " AND "))
	args = append(args, f.limit())

	return s.queryEntries(q, args...)
}

// GetEntry returns a single entry owned by userID.
func (s *Store) GetEntry(userID, id int64) (*Entry, error) {
	entries, err := s.queryEntries(`
		SELECT e.id, e.title, e.body, e.created_at, e.updated_at
		FROM entries e
		WHERE e.id = ? AND e.user_id = ?
	`, id, userID)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, sql.ErrNoRows
	}
	s.hydrateTags(entries)
	return &entries[0], nil
}

// EntryDates returns the set of dates (YYYY-MM-DD) in [first, end) on which the
// user has at least one entry. Used by the calendar to mark days.
func (s *Store) EntryDates(userID int64, first, end time.Time) (map[string]bool, error) {
	rows, err := s.db.Query(
		"SELECT DISTINCT date(created_at) FROM entries WHERE user_id = ? AND created_at >= ? AND created_at < ?",
		userID, first.Format(time.RFC3339), end.Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dates := map[string]bool{}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err == nil {
			dates[d] = true
		}
	}
	return dates, rows.Err()
}

func (s *Store) GetAllTags(userID int64) ([]Tag, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.name FROM tags t
		WHERE t.user_id = ?
		ORDER BY t.name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// SetTags replaces an entry's tags with the comma-separated list, creating
// tags that don't exist yet. Blank names are skipped.
func (s *Store) SetTags(entryID, userID int64, tagStr string) {
	s.db.Exec("DELETE FROM entry_tags WHERE entry_id = ?", entryID)
	if strings.TrimSpace(tagStr) == "" {
		return
	}
	for _, name := range strings.Split(tagStr, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var tagID int64
		err := s.db.QueryRow(
			"INSERT INTO tags (user_id, name) VALUES (?, ?) ON CONFLICT(user_id, name) DO UPDATE SET name = excluded.name RETURNING id",
			userID, name,
		).Scan(&tagID)
		if err != nil {
			continue
		}
		s.db.Exec("INSERT OR IGNORE INTO entry_tags (entry_id, tag_id) VALUES (?, ?)", entryID, tagID)
	}
}

func (s *Store) queryEntries(q string, args ...any) ([]Entry, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
		var createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.Title, &e.Body, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		e.CreatedAt, _ = time.Parse(sqliteTimeFormat, createdAt)
		e.UpdatedAt, _ = time.Parse(sqliteTimeFormat, updatedAt)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// hydrateTags fills in Tags for each entry using one query for the whole page.
// It mutates the slice in place and skips the query entirely when empty.
func (s *Store) hydrateTags(entries []Entry) {
	if len(entries) == 0 {
		return
	}

	byEntry := map[int64][]Tag{}
	args := make([]any, len(entries))
	placeholders := make([]string, len(entries))
	for i, e := range entries {
		args[i] = e.ID
		placeholders[i] = "?"
	}

	rows, err := s.db.Query(fmt.Sprintf(`
		SELECT et.entry_id, t.id, t.name FROM tags t
		JOIN entry_tags et ON et.tag_id = t.id
		WHERE et.entry_id IN (%s)
		ORDER BY t.name
	`, strings.Join(placeholders, ", ")), args...)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var entryID int64
		var t Tag
		if err := rows.Scan(&entryID, &t.ID, &t.Name); err != nil {
			continue
		}
		byEntry[entryID] = append(byEntry[entryID], t)
	}

	for i := range entries {
		entries[i].Tags = byEntry[entries[i].ID]
	}
}
