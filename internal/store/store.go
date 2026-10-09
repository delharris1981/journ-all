package store

import "database/sql"

// Store owns all SQL access for the app. Handlers call it; they never build
// queries themselves.
type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}
