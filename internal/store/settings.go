package store

import (
	"database/sql"
	"errors"
)

// GetSetting returns the value stored under key. found is false when the key
// has never been set, which is different from being set to the empty string.
func (s *Store) GetSetting(key string) (value string, found bool, err error) {
	var v string
	err = s.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting stores value under key, replacing any existing value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

// GetBoolSetting returns a boolean setting, falling back to def when the key is
// unset or holds something other than "1" or "0".
//
// An unrecognised value falls back rather than reading as false, so a corrupt
// row fails towards the setting's default instead of silently flipping it —
// signups_enabled defaulting to false would lock everyone out of registering.
func (s *Store) GetBoolSetting(key string, def bool) (bool, error) {
	v, found, err := s.GetSetting(key)
	if err != nil {
		return def, err
	}
	switch {
	case !found:
		return def, nil
	case v == "1":
		return true, nil
	case v == "0":
		return false, nil
	default:
		return def, nil
	}
}

// SetBoolSetting stores a boolean setting as "1"/"0".
func (s *Store) SetBoolSetting(key string, value bool) error {
	if value {
		return s.SetSetting(key, "1")
	}
	return s.SetSetting(key, "0")
}
