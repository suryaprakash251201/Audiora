package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestScanPopulatesSearchIndex is the guard for the most easily broken
// invariant in the schema: the FTS5 index is maintained by triggers, and
// nothing else in the system would notice if those triggers stopped firing.
// A library that imports cleanly but cannot be searched looks identical to a
// working one until a listener tries to find anything.
func TestScanPopulatesSearchIndex(t *testing.T) {
	s, database, music := newTestScanner(t)
	if !database.FTS {
		t.Skip("FTS5 is unavailable in this SQLite build; search uses the LIKE fallback")
	}

	albumDir := filepath.Join(music, "Nina Simone", "Pastel Blues")
	makeFLAC(t, filepath.Join(albumDir, "01 - Sinnerman.flac"), "Sinnerman", "Nina Simone", "Pastel Blues", 1)
	makeFLAC(t, filepath.Join(albumDir, "02 - Blue in Green.flac"), "Blue in Green", "Nina Simone", "Pastel Blues", 2)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}

	// The index must have exactly one row per track, and no more.
	var indexed int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tracks_fts`).Scan(&indexed); err != nil {
		t.Fatalf("count tracks_fts: %v", err)
	}
	var tracks int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&tracks); err != nil {
		t.Fatal(err)
	}
	if indexed != tracks {
		t.Errorf("tracks_fts has %d rows, tracks has %d; the sync triggers did not fire", indexed, tracks)
	}

	tests := []struct {
		query string
		want  string
	}{
		{"Sinnerman", "Sinnerman"},
		{"sinnerman", "Sinnerman"}, // case-insensitive
		{"SIMONE", "Sinnerman"},    // artist name is indexed too
		{"Pastel", "Sinnerman"},    // album name is indexed too
		{"Blue", "Blue in Green"},
		{"Sinn", "Sinnerman"}, // prefix match on the final term
		{"nonsense", ""},      // no match
	}

	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			rows, err := database.SearchTracks(tc.query, 10)
			if err != nil {
				t.Fatalf("search %q: %v", tc.query, err)
			}
			defer rows.Close()

			got := []string{}
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
					t.Fatal(err)
				}
				got = append(got, title)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}

			if tc.want == "" {
				if len(got) != 0 {
					t.Errorf("search %q returned %v, want no results", tc.query, got)
				}
				return
			}
			found := false
			for _, title := range got {
				if title == tc.want {
					found = true
				}
			}
			if !found {
				t.Errorf("search %q returned %v, want it to include %q", tc.query, got, tc.want)
			}
		})
	}
}

// TestSearchSurvivesReimport checks that the delete and insert triggers
// together leave the index in step after a file is re-scanned, which is the
// path a changed file takes.
func TestSearchSurvivesReimport(t *testing.T) {
	s, database, music := newTestScanner(t)
	if !database.FTS {
		t.Skip("FTS5 is unavailable in this SQLite build")
	}

	path := filepath.Join(music, "A", "Alb", "01 - Original.flac")
	makeFLAC(t, path, "Original", "A", "Alb", 1)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Re-encode with a different title, which changes the file and re-imports.
	makeFLAC(t, path, "Renamed", "A", "Alb", 1)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var indexed int
	database.QueryRow(`SELECT COUNT(*) FROM tracks_fts`).Scan(&indexed)
	if indexed != 1 {
		t.Errorf("tracks_fts has %d rows after a re-import, want 1 (the delete trigger did not fire)", indexed)
	}

	// The stale title must be gone from the index.
	rows, err := database.SearchTracks("Original", 10)
	if err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if rows.Next() {
		t.Error("the old title is still in the search index after a re-import")
	}

	rows2, err := database.SearchTracks("Renamed", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows2.Close()
	if !rows2.Next() {
		t.Error("the new title is missing from the search index")
	}
}

// TestSearchAfterTrackDeleted covers the cascade path: removing a file from
// the library must also remove it from the index.
func TestSearchAfterTrackDeleted(t *testing.T) {
	s, database, music := newTestScanner(t)
	if !database.FTS {
		t.Skip("FTS5 is unavailable in this SQLite build")
	}

	albumDir := filepath.Join(music, "A", "Alb")
	keep := filepath.Join(albumDir, "01 - Keep.flac")
	drop := filepath.Join(albumDir, "02 - Vanish.flac")
	makeFLAC(t, keep, "Keep", "A", "Alb", 1)
	makeFLAC(t, drop, "Vanish", "A", "Alb", 2)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(drop); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var indexed int
	database.QueryRow(`SELECT COUNT(*) FROM tracks_fts`).Scan(&indexed)
	if indexed != 1 {
		t.Errorf("tracks_fts has %d rows after a delete, want 1", indexed)
	}

	rows, err := database.SearchTracks("Vanish", 10)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Error("a deleted track is still in the search index")
	}
}
