-- Full-text search over titles, artist names and album titles.
--
-- This is a plain FTS5 table, not an external-content one, and the
-- denormalisation is deliberate: artist and album live in their own tables,
-- so `content='tracks'` would be wrong (FTS5 requires every declared column
-- to exist in the content table, and tracks has only `title`). The three
-- triggers below keep a copy of the text instead, which costs a few hundred
-- bytes per track and buys a correct index.
--
-- Kept in its own migration because FTS5 is a compile-time option in some
-- SQLite builds. If the virtual table cannot be created the migrator logs a
-- warning and the server falls back to LIKE scans, which are slower but
-- always work. See internal/db.SearchTracks.

CREATE VIRTUAL TABLE tracks_fts USING fts5 (
    title,
    artist,
    album,
    tokenize='unicode61 remove_diacritics 2'
);

-- Populate from the artist and album tables rather than the tracks table,
-- because those hold the names the listener actually searches for.
CREATE TRIGGER tracks_fts_insert AFTER INSERT ON tracks BEGIN
    INSERT INTO tracks_fts(rowid, title, artist, album)
    SELECT t.id, t.title, a.name, al.title
    FROM tracks t
    JOIN artists a ON a.id = t.artist_id
    JOIN albums  al ON al.id = t.album_id
    WHERE t.id = new.id;
END;

-- A plain DELETE, not the external-content 'delete' command: that command
-- only exists for contentless and external-content tables, and would raise
-- an error against this one.
CREATE TRIGGER tracks_fts_delete AFTER DELETE ON tracks BEGIN
    DELETE FROM tracks_fts WHERE rowid = old.id;
END;

CREATE TRIGGER tracks_fts_update AFTER UPDATE ON tracks BEGIN
    DELETE FROM tracks_fts WHERE rowid = old.id;
    INSERT INTO tracks_fts(rowid, title, artist, album)
    SELECT t.id, t.title, a.name, al.title
    FROM tracks t
    JOIN artists a ON a.id = t.artist_id
    JOIN albums  al ON al.id = t.album_id
    WHERE t.id = new.id;
END;
