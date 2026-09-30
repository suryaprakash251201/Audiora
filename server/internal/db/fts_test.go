package db

import (
	"path/filepath"
	"testing"
)

// matchCount runs a MATCH and returns how many rows came back. It always
// closes the Rows before returning: the pool is capped at a single
// connection, so an unexhausted Rows left open would deadlock the next query.
func matchCount(t *testing.T, d *DB, expr string) int {
	t.Helper()
	rows, err := d.Query(`SELECT rowid FROM tracks_fts WHERE tracks_fts MATCH ?`, expr)
	if err != nil {
		t.Fatalf("MATCH %s: %v", expr, err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("MATCH %s: %v", expr, err)
	}
	return n
}

// TestFTSInsertTriggerFires inserts a track by hand and checks the index.
// This isolates the trigger from the scanner, so a failure points at either
// the trigger or the way the scanner writes, rather than at both.
func TestFTSInsertTriggerFires(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer d.Close()
	if !d.FTS {
		t.Skip("FTS5 unavailable")
	}

	// The minimum graph the trigger's joins need.
	res, err := d.Exec(`INSERT INTO artists (name_key, name, sort_name) VALUES ('nina', 'Nina Simone', 'Nina Simone')`)
	if err != nil {
		t.Fatal(err)
	}
	artistID, _ := res.LastInsertId()

	_, err = d.Exec(`INSERT INTO albums (artist_id, album_key, title, added_at) VALUES (?, ?, ?, 0)`,
		artistID, "nina pastel", "Pastel Blues")
	if err != nil {
		t.Fatal(err)
	}
	var albumID int64
	if err := d.QueryRow(`SELECT id FROM albums WHERE album_key = ?`, "nina pastel").Scan(&albumID); err != nil {
		t.Fatal(err)
	}

	res, err = d.Exec(`
		INSERT INTO tracks (path, file_size, mod_time, format, duration_ms, bitrate,
		                    sample_rate, channels, title, disc_no, artist_id, album_id, added_at)
		VALUES ('a/01.flac', 100, 0, 'flac', 5000, 900, 44100, 2, 'Sinnerman', 1, ?, ?, 0)`,
		artistID, albumID)
	if err != nil {
		t.Fatal(err)
	}
	trackID, _ := res.LastInsertId()

	var indexed int
	if err := d.QueryRow(`SELECT COUNT(*) FROM tracks_fts`).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != 1 {
		var sqlText string
		d.QueryRow(`SELECT COALESCE(sql,'') FROM sqlite_master WHERE name='tracks_fts_insert'`).Scan(&sqlText)
		t.Fatalf("tracks_fts has %d rows after a direct insert, want 1.\ntrigger sql: %s", indexed, sqlText)
	}

	// Title, artist and album must all be searchable, not just the title.
	for _, expr := range []string{`"sinnerman"*`, `"simone"*`, `"pastel"*`} {
		if n := matchCount(t, d, expr); n == 0 {
			t.Errorf("MATCH %s returned nothing; title, artist and album must all be indexed", expr)
		}
	}

	// SearchTracks is the code path the API uses, and it joins artists and
	// albums. That join is what previously broke the rank ordering, so it is
	// worth exercising directly.
	rows, err := d.SearchTracks("sinnerman", 10)
	if err != nil {
		t.Fatalf("SearchTracks: %v", err)
	}
	var found int
	for rows.Next() {
		var id int64
		var title, path string
		var duration, bitrate int
		var format string
		var hasLyrics int
		var artist, album string
		var coverPath, dominantColor *string
		if err := rows.Scan(&id, &title, &path, &duration, &bitrate, &format,
			&hasLyrics, &artist, &album, &coverPath, &dominantColor); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		found++
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if found != 1 {
		t.Errorf("SearchTracks returned %d rows, want 1", found)
	}

	_ = trackID
}

// TestFTSUpsertPathIsIndexed covers the scanner's actual write pattern: an
// INSERT ... ON CONFLICT DO UPDATE, which takes the insert path only the very
// first time and the update path afterwards.
func TestFTSUpsertPathIsIndexed(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.FTS {
		t.Skip("FTS5 unavailable")
	}

	d.Exec(`INSERT INTO artists (name_key, name, sort_name) VALUES ('a', 'Artist', 'Artist')`)
	var artistID int64
	d.QueryRow(`SELECT id FROM artists WHERE name_key='a'`).Scan(&artistID)
	d.Exec(`INSERT INTO albums (artist_id, album_key, title, added_at) VALUES (?, ?, ?, 0)`,
		artistID, "artist album", "Album")
	var albumID int64
	d.QueryRow(`SELECT id FROM albums WHERE album_key=?`, "artist album").Scan(&albumID)

	upsert := func(title string) {
		t.Helper()
		_, err := d.Exec(`
			INSERT INTO tracks (path, file_size, mod_time, format, duration_ms, bitrate,
			                    sample_rate, channels, title, disc_no, artist_id, album_id, added_at)
			VALUES ('a/01.flac', 100, 0, 'flac', 5000, 900, 44100, 2, ?, 1, ?, ?, 0)
			ON CONFLICT(path) DO UPDATE SET title = excluded.title`,
			title, artistID, albumID)
		if err != nil {
			t.Fatal(err)
		}
	}

	upsert("Original Title")

	var indexed int
	d.QueryRow(`SELECT COUNT(*) FROM tracks_fts`).Scan(&indexed)
	if indexed != 1 {
		t.Errorf("after the first upsert tracks_fts has %d rows, want 1", indexed)
	}

	// Re-import with a new title: the update path must replace the entry,
	// not add a second one.
	upsert("Changed Title")

	d.QueryRow(`SELECT COUNT(*) FROM tracks_fts`).Scan(&indexed)
	if indexed != 1 {
		t.Errorf("after the second upsert tracks_fts has %d rows, want 1 (the update trigger should replace, not append)", indexed)
	}

	rows, err := d.SearchTracks("Changed Title", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Error("searching the new title finds nothing")
	}
	rows.Close()

	rows2, err := d.SearchTracks("Original", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	if rows2.Next() {
		t.Error("the old title is still searchable after the upsert")
	}
}
