-- Full-text search index over entry titles and bodies. External-content FTS5:
-- entries stays the source of truth and the index is kept in sync by triggers.
CREATE VIRTUAL TABLE entries_fts USING fts5(
    title, body,
    content='entries',
    content_rowid='id',
    tokenize='porter unicode61'
);

INSERT INTO entries_fts(rowid, title, body) SELECT id, title, body FROM entries;

-- External-content tables are not maintained by SQLite itself; without these
-- the index silently goes stale after any write.
CREATE TRIGGER entries_fts_insert AFTER INSERT ON entries BEGIN
    INSERT INTO entries_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;

CREATE TRIGGER entries_fts_delete AFTER DELETE ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, body)
    VALUES ('delete', old.id, old.title, old.body);
END;

CREATE TRIGGER entries_fts_update AFTER UPDATE ON entries BEGIN
    INSERT INTO entries_fts(entries_fts, rowid, title, body)
    VALUES ('delete', old.id, old.title, old.body);
    INSERT INTO entries_fts(rowid, title, body) VALUES (new.id, new.title, new.body);
END;
