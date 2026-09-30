package db

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	// Pure-Go SQLite. The cgo-based mattn/go-sqlite3 would be marginally
	// faster but forces CGO_ENABLED=1, which costs us the static binary.
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// DB wraps the SQLite handle. The library is small and read-mostly, so a
// single connection with WAL is more than enough and avoids a separate
// database container entirely.
type DB struct {
	*sql.DB
	// FTS is false when the SQLite build lacks FTS5, in which case search
	// degrades to LIKE scans.
	FTS bool
}

// Open connects to the database file and applies any pending migrations.
func Open(path string) (*DB, error) {
	// WAL lets the scanner write while players read. busy_timeout keeps a
	// write from failing outright when two of them collide.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// One writer. Reads are serialised too, but on a library of this size
	// that costs nothing and removes a whole class of locking bugs.
	conn.SetMaxOpenConns(1)

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	d := &DB{DB: conn}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	d.FTS = d.detectFTS()
	if !d.FTS {
		slog.Warn("SQLite build has no FTS5; search will use LIKE scans", "fallback", "slower")
	}
	return d, nil
}

func (d *DB) detectFTS() bool {
	var name string
	err := d.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='tracks_fts'`).Scan(&name)
	return err == nil
}

func (d *DB) migrate() error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		name       TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var exists int
		err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists > 0 {
			continue
		}

		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if _, err := d.Exec(string(body)); err != nil {
			// The FTS migration is the one allowed to fail, because FTS5 is
			// an optional SQLite build feature.
			if name == "0002_search.sql" {
				slog.Warn("could not create FTS index, falling back to LIKE search", "error", err)
				continue
			}
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := d.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, unixepoch())`, name); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		slog.Info("applied migration", "name", name)
	}
	return nil
}

// Time is the timestamp representation used throughout the schema:
// seconds since the Unix epoch, which sorts correctly as an integer.
type Time = int64

// Now returns the current time in the schema's representation.
func Now() Time { return nowUnix() }

// SearchTracks finds tracks matching a free-text query. With FTS5 available
// it runs a proper MATCH; otherwise it falls back to LIKE against the
// track, artist and album names.
func (d *DB) SearchTracks(query string, limit int) (*sql.Rows, error) {
	if d.FTS {
		// bm25() rather than the bare `rank` column: rank is only resolvable
		// when the FTS table is the sole table in the FROM clause, and this
		// query joins artists and albums. Lower bm25 means a better match.
		return d.Query(`
			SELECT t.id, t.title, t.path, t.duration_ms, t.bitrate, t.format, t.has_lyrics,
			       a.name AS artist, al.title AS album, al.cover_path, al.dominant_color
			FROM tracks_fts f
			JOIN tracks t ON t.id = f.rowid
			JOIN artists a ON a.id = t.artist_id
			JOIN albums  al ON al.id = t.album_id
			WHERE f.tracks_fts MATCH ?
			ORDER BY bm25(tracks_fts)
			LIMIT ?`, ftsQuery(query), limit)
	}

	like := "%" + query + "%"
	return d.Query(`
		SELECT t.id, t.title, t.path, t.duration_ms, t.bitrate, t.format, t.has_lyrics,
		       a.name AS artist, al.title AS album, al.cover_path, al.dominant_color
		FROM tracks t
		JOIN artists a ON a.id = t.artist_id
		JOIN albums  al ON al.id = t.album_id
		WHERE t.title LIKE ? OR a.name LIKE ? OR al.title LIKE ?
		ORDER BY t.title
		LIMIT ?`, like, like, like, limit)
}

// ftsQuery turns user input into a safe FTS5 MATCH expression. Bare user
// input is not valid FTS5 syntax: quotes, hyphens and colons all mean
// something, and a malformed expression is a runtime error rather than a
// no-match. So every term is quoted, and the last term gets a prefix match
// so results narrow as the user types.
func ftsQuery(input string) string {
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return `""`
	}
	terms := make([]string, 0, len(fields))
	for i, f := range fields {
		escaped := strings.NewReplacer(`"`, `""`).Replace(f)
		if i == len(fields)-1 {
			terms = append(terms, `"`+escaped+`"*`)
		} else {
			terms = append(terms, `"`+escaped+`"`)
		}
	}
	return strings.Join(terms, " ")
}
