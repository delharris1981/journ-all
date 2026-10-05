ALTER TABLE users ADD COLUMN is_admin INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT OR IGNORE INTO settings (key, value) VALUES ('signups_enabled', '1');

-- The first account becomes the admin for databases created before this migration.
UPDATE users SET is_admin = 1 WHERE id = (SELECT MIN(id) FROM users);
