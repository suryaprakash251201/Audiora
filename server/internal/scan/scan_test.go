package scan

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/audiora/audiora/server/internal/db"
)

// makeFLAC generates a short FLAC with the given tags, so scanner tests
// exercise real metadata extraction rather than a hand-written struct.
func makeFLAC(t *testing.T, path string, title, artist, album string, track int) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "flac",
		"-metadata", "title="+title,
		"-metadata", "artist="+artist,
		"-metadata", "album="+album,
		"-metadata", "track="+itoa(track),
		path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v: %s", path, err, out)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func newTestScanner(t *testing.T) (*Scanner, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	music := filepath.Join(dir, "music")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}
	return New(database, Options{
		MusicRoot:     music,
		ExtractCovers: true,
		ExtractColors: true,
		CoverDir:      filepath.Join(dir, "covers"),
	}), database, music
}

func TestScanImportsAlbumAndTracks(t *testing.T) {
	s, database, music := newTestScanner(t)

	albumDir := filepath.Join(music, "Miles Davis", "Kind of Blue")
	makeFLAC(t, filepath.Join(albumDir, "01 - So What.flac"), "So What", "Miles Davis", "Kind of Blue", 1)
	makeFLAC(t, filepath.Join(albumDir, "02 - Blue in Green.flac"), "Blue in Green", "Miles Davis", "Kind of Blue", 2)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("scan: %v", err)
	}

	var tracks, albums, artists int
	database.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&tracks)
	database.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums)
	database.QueryRow(`SELECT COUNT(*) FROM artists`).Scan(&artists)

	if tracks != 2 {
		t.Errorf("tracks = %d, want 2", tracks)
	}
	if albums != 1 {
		t.Errorf("albums = %d, want 1", albums)
	}
	if artists != 1 {
		t.Errorf("artists = %d, want 1", artists)
	}

	// The artist should be stored for display but sorted under B.
	var name, sortName string
	if err := database.QueryRow(`SELECT name, sort_name FROM artists`).Scan(&name, &sortName); err != nil {
		t.Fatal(err)
	}
	if name != "Miles Davis" {
		t.Errorf("artist name = %q, want %q", name, "Miles Davis")
	}
	if sortName != "Miles Davis" {
		t.Errorf("sort_name = %q, want %q", sortName, "Miles Davis")
	}

	// Track order and duration must survive the import.
	var title string
	var duration int64
	if err := database.QueryRow(`SELECT title, duration_ms FROM tracks WHERE track_no = 1`).Scan(&title, &duration); err != nil {
		t.Fatal(err)
	}
	if title != "So What" {
		t.Errorf("title = %q, want %q", title, "So What")
	}
	if duration < 900 || duration > 1100 {
		t.Errorf("duration = %dms, want about 1000ms", duration)
	}
}

func TestRescanIsIncremental(t *testing.T) {
	s, _, music := newTestScanner(t)
	albumDir := filepath.Join(music, "Artist", "Album")
	makeFLAC(t, filepath.Join(albumDir, "01 - One.flac"), "One", "Artist", "Album", 1)

	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("first scan: %v", err)
	}
	first := s.State()
	if first.Added != 1 {
		t.Fatalf("first scan added = %d, want 1", first.Added)
	}

	// A second scan over an unchanged library must not re-import anything.
	if err := s.Run(context.Background()); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	second := s.State()
	if second.Added != 0 {
		t.Errorf("second scan added = %d, want 0", second.Added)
	}
	if second.Updated != 0 {
		t.Errorf("second scan updated = %d, want 0", second.Updated)
	}
	if second.Skipped != 1 {
		t.Errorf("second scan skipped = %d, want 1 (the unchanged file)", second.Skipped)
	}
}

func TestChangedFileIsReimported(t *testing.T) {
	s, database, music := newTestScanner(t)
	albumDir := filepath.Join(music, "Artist", "Album")
	path := filepath.Join(albumDir, "01 - One.flac")
	makeFLAC(t, path, "One", "Artist", "Album", 1)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Re-encode at a different length, which changes size and mtime.
	makeFLAC(t, path, "One Remastered", "Artist", "Album", 1)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var title string
	if err := database.QueryRow(`SELECT title FROM tracks`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "One Remastered" {
		t.Errorf("title = %q, want the updated tag %q", title, "One Remastered")
	}
	if s.State().Updated != 1 {
		t.Errorf("updated = %d, want 1", s.State().Updated)
	}
}

func TestDeletedFileIsPruned(t *testing.T) {
	s, database, music := newTestScanner(t)
	albumDir := filepath.Join(music, "Artist", "Album")
	keep := filepath.Join(albumDir, "01 - Keep.flac")
	drop := filepath.Join(albumDir, "02 - Drop.flac")
	makeFLAC(t, keep, "Keep", "Artist", "Album", 1)
	makeFLAC(t, drop, "Drop", "Artist", "Album", 2)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var before int
	database.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&before)
	if before != 2 {
		t.Fatalf("tracks before = %d, want 2", before)
	}

	if err := os.Remove(drop); err != nil {
		t.Fatal(err)
	}
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var after int
	database.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&after)
	if after != 1 {
		t.Errorf("tracks after = %d, want 1", after)
	}
	var title string
	database.QueryRow(`SELECT title FROM tracks`).Scan(&title)
	if title != "Keep" {
		t.Errorf("remaining track = %q, want Keep", title)
	}
}

func TestSameAlbumTitleDifferentArtistsDoNotMerge(t *testing.T) {
	s, database, music := newTestScanner(t)
	makeFLAC(t, filepath.Join(music, "Nirvana", "Greatest Hits", "01 - A.flac"), "A", "Nirvana", "Greatest Hits", 1)
	makeFLAC(t, filepath.Join(music, "Cher", "Greatest Hits", "01 - B.flac"), "B", "Cher", "Greatest Hits", 1)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var albums int
	database.QueryRow(`SELECT COUNT(*) FROM albums`).Scan(&albums)
	if albums != 2 {
		t.Errorf("albums = %d, want 2 (same title, different artists must not merge)", albums)
	}
}

func TestMissingTagsFallBackToPath(t *testing.T) {
	s, database, music := newTestScanner(t)
	// An untagged file in Artist/Album/03 - Untitled.flac.
	dir := filepath.Join(music, "Boards of Canada", "Geogaddi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// sine has no metadata, so every tag comes back empty.
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
		"-c:a", "flac", filepath.Join(dir, "03 - Untitled.flac"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg: %v: %s", err, out)
	}

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var title, artist, album string
	var trackNo any
	if err := database.QueryRow(`
		SELECT t.title, a.name, al.title, t.track_no
		FROM tracks t JOIN artists a ON a.id = t.artist_id JOIN albums al ON al.id = t.album_id`).
		Scan(&title, &artist, &album, &trackNo); err != nil {
		t.Fatal(err)
	}

	if title != "Untitled" {
		t.Errorf("title = %q, want %q (number stripped from filename)", title, "Untitled")
	}
	if artist != "Boards of Canada" {
		t.Errorf("artist = %q, want the containing directory", artist)
	}
	if album != "Geogaddi" {
		t.Errorf("album = %q, want the containing directory", album)
	}
	if trackNo != int64(3) {
		t.Errorf("track_no = %v, want 3 (not inferable from an untagged file, so 0 is also valid)", trackNo)
	}
}

func TestHiddenDirectoriesAreSkipped(t *testing.T) {
	s, database, music := newTestScanner(t)
	makeFLAC(t, filepath.Join(music, "Artist", "Album", "01 - Real.flac"), "Real", "Artist", "Album", 1)
	// Junk that should never be imported.
	makeFLAC(t, filepath.Join(music, ".git", "junk.flac"), "Junk", "Artist", "Album", 1)
	makeFLAC(t, filepath.Join(music, "@eaDir", "junk.flac"), "Junk", "Artist", "Album", 1)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	database.QueryRow(`SELECT COUNT(*) FROM tracks`).Scan(&n)
	if n != 1 {
		t.Errorf("tracks = %d, want 1 (hidden and @eaDir directories must be skipped)", n)
	}
}

func TestSidecarLyricsAreAttached(t *testing.T) {
	s, database, music := newTestScanner(t)
	dir := filepath.Join(music, "Artist", "Album")
	audio := filepath.Join(dir, "01 - Song.flac")
	makeFLAC(t, audio, "Song", "Artist", "Album", 1)

	lrc := "[00:12.00] First line\n[00:15.50] Second line\n"
	if err := os.WriteFile(filepath.Join(dir, "01 - Song.lrc"), []byte(lrc), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	var raw, source string
	if err := database.QueryRow(`SELECT raw, source FROM track_lyrics`).Scan(&raw, &source); err != nil {
		t.Fatalf("no lyrics row: %v", err)
	}
	if raw != lrc {
		t.Errorf("lyrics = %q, want %q", raw, lrc)
	}
	if source != "sidecar" {
		t.Errorf("source = %q, want sidecar", source)
	}

	var hasLyrics int
	database.QueryRow(`SELECT has_lyrics FROM tracks`).Scan(&hasLyrics)
	if hasLyrics != 1 {
		t.Errorf("has_lyrics = %d, want 1", hasLyrics)
	}
}

func TestPathsAreStoredSlashSeparated(t *testing.T) {
	s, database, music := newTestScanner(t)
	makeFLAC(t, filepath.Join(music, "Artist", "Album", "01 - Song.flac"), "Song", "Artist", "Album", 1)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var path string
	database.QueryRow(`SELECT path FROM tracks`).Scan(&path)
	if filepath.IsAbs(path) {
		t.Errorf("path = %q, want a relative path", path)
	}
	// A database written on Windows must be readable by a Linux container.
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' {
			t.Errorf("path = %q contains a backslash; stored paths must use forward slashes", path)
			break
		}
	}
}
