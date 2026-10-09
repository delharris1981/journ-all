package store

// CreateEntry inserts a new entry and returns its id.
func (s *Store) CreateEntry(userID int64, title, body string) (int64, error) {
	res, err := s.db.Exec(
		"INSERT INTO entries (user_id, title, body) VALUES (?, ?, ?)",
		userID, title, body,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateEntry writes title, body and updated_at for an entry the user owns.
// It reports false when no row matched — a wrong id, or someone else's entry.
func (s *Store) UpdateEntry(userID, id int64, title, body string) (bool, error) {
	res, err := s.db.Exec(
		"UPDATE entries SET title = ?, body = ?, updated_at = datetime('now') WHERE id = ? AND user_id = ?",
		title, body, id, userID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteEntry removes an entry the user owns, reporting whether a row went.
func (s *Store) DeleteEntry(userID, id int64) (bool, error) {
	res, err := s.db.Exec("DELETE FROM entries WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
