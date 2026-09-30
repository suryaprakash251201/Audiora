// Package scan walks the music library and reconciles it with the database.
package scan

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/media"
	"github.com/audiora/audiora/server/internal/models"
)

// Options configures a scan run.
type Options struct {
	MusicRoot string
	// Base is the lifetime a scan is allowed to use. It must NOT be an HTTP
	// request context: that is cancelled the moment the handler returns, which
	// would kill every scan the moment it was started. Leave nil for
	// context.Background.
	Base          context.Context
	ExtractCovers bool
	ExtractColors bool
	// OnProgress is called as the scan advances. It must be cheap and must
	// not block: the admin UI polls or subscribes to it.
	OnProgress func(models.ScanState)
	// CoverDir is where extracted artwork is written.
	CoverDir string
}

// Scanner imports audio files into the database.
type Scanner struct {
	db        *db.DB
	extractor *media.Extractor
	opts      Options

	mu      sync.Mutex
	state   models.ScanState
	running bool
	cancel  context.CancelFunc
	// done is closed when the current run finishes, so callers can wait.
	done chan struct{}
}

// New creates a Scanner.
func New(database *db.DB, opts Options) *Scanner {
	return &Scanner{
		db:        database,
		opts:      opts,
		extractor: media.NewExtractor(opts.CoverDir, opts.ExtractCovers, opts.ExtractColors),
		state:     models.ScanState{Phase: "idle"},
	}
}

// State returns a snapshot of scan progress for the admin UI.
func (s *Scanner) State() models.ScanState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// IsRunning reports whether a scan is in progress.
func (s *Scanner) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Start launches a scan in the background. Returns false if one is already
// running, so a double-click on the admin button cannot start two walks of
// the same tree.
//
// The parent argument is deliberately ignored in favour of Options.Base: see
// the note there about request contexts.
func (s *Scanner) Start(parent context.Context) bool {
	base := s.opts.Base
	if base == nil {
		base = context.Background()
	}
	_ = parent

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(base)
	s.running = true
	s.cancel = cancel
	s.done = make(chan struct{})
	s.state = models.ScanState{Running: true, Phase: "walking", StartedAt: db.Now()}
	done := s.done
	s.mu.Unlock()

	go func() {
		defer close(done)
		defer cancel()
		err := s.Run(ctx)
		s.mu.Lock()
		s.running = false
		s.cancel = nil
		s.state.Running = false
		s.state.FinishedAt = db.Now()
		if err != nil && !errors.Is(err, context.Canceled) {
			s.state.Error = err.Error()
			s.state.Phase = "failed"
		} else if errors.Is(err, context.Canceled) {
			s.state.Phase = "cancelled"
		} else {
			// Run sets "done" itself; it is reasserted here because that value
			// is what the admin UI polls, and it must survive the wrapper.
			s.state.Phase = "done"
		}
		s.mu.Unlock()

		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("library scan failed", "err", err)
		}
	}()
	return true
}

// Cancel stops a running scan. The database is left consistent because each
// track is committed independently.
func (s *Scanner) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Wait blocks until the current scan run finishes.
func (s *Scanner) Wait() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done == nil {
		return
	}
	<-done
}

// Run performs one full reconciliation pass. It is exported for tests and for
// the CLI's synchronous scan command.
func (s *Scanner) Run(ctx context.Context) error {
	// Reset counters here rather than only in Start, so a direct Run (the
	// CLI, or a test) reports this pass and not a running total.
	s.mu.Lock()
	s.state = models.ScanState{Running: true, Phase: "walking", StartedAt: db.Now()}
	s.mu.Unlock()

	if err := s.prepareSeenTable(); err != nil {
		return err
	}
	defer s.dropSeenTable()

	files, err := s.collectFiles(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.state.Total = len(files)
	s.state.Phase = "importing"
	s.mu.Unlock()
	s.report()

	// A fresh context for artwork, so cancelling during import does not kill
	// an in-flight ffmpeg halfway and leave a truncated cover on disk.
	coverCtx, cancelCovers := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelCovers()

	for _, relPath := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.importFile(ctx, coverCtx, relPath)
	}

	// Anything no longer on disk is removed. This is what makes a scan
	// accurate rather than merely additive.
	if err := s.pruneMissing(); err != nil {
		return fmt.Errorf("prune removed files: %w", err)
	}

	s.mu.Lock()
	s.state.Phase = "done"
	s.state.CurrentFile = ""
	s.mu.Unlock()
	s.report()
	return nil
}

// collectFiles walks the tree and returns the supported audio files, sorted
// so that a scan is deterministic and album art extraction for a given album
// is attempted from a predictable first track.
func (s *Scanner) collectFiles(ctx context.Context) ([]string, error) {
	var out []string

	err := filepath.WalkDir(s.opts.MusicRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable directory should not abort the whole scan; log
			// and continue, which matters on network mounts.
			if path != s.opts.MusicRoot {
				slog.Warn("skipping unreadable path", "path", path, "err", err)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			// Skip hidden and system directories outright: .git, .Trash,
			// @eaDir and friends can be enormous and hold no music.
			name := d.Name()
			if path != s.opts.MusicRoot && (strings.HasPrefix(name, ".") || strings.EqualFold(name, "@eaDir")) {
				return fs.SkipDir
			}
			return nil
		}
		if !media.IsSupported(d.Name()) {
			return nil
		}

		rel, relErr := filepath.Rel(s.opts.MusicRoot, path)
		if relErr != nil {
			slog.Warn("could not make path relative", "path", path, "err", relErr)
			return nil
		}
		// Store slash-separated paths so a database written on Windows can be
		// read on a Linux container and vice versa.
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk music root: %w", err)
	}

	sortStrings(out)
	return out, nil
}

// importFile probes and stores one audio file, or records why it was skipped.
func (s *Scanner) importFile(ctx, coverCtx context.Context, relPath string) {
	s.mu.Lock()
	s.state.CurrentFile = relPath
	s.mu.Unlock()

	abs := filepath.Join(s.opts.MusicRoot, filepath.FromSlash(relPath))
	info, err := os.Stat(abs)
	if err != nil {
		s.skip()
		return
	}

	// Fast path: unchanged file, already in the database. This is what keeps
	// a routine rescan of a large library fast.
	if s.unchanged(relPath, info) {
		s.mu.Lock()
		s.state.Processed++
		s.state.Skipped++
		s.mu.Unlock()
		s.markSeen(relPath)
		return
	}

	probe, err := media.ProbeFile(ctx, abs)
	if err != nil {
		slog.Warn("could not read audio file", "path", relPath, "err", err)
		s.skip()
		return
	}

	title, artist, album := resolveNames(probe, relPath)
	albumArtist := firstNonEmpty(probe.AlbumArtist, artist)

	// For untagged files the track number is recoverable from the filename,
	// and it matters: without it the UI cannot order a disc at all.
	if probe.TrackNo == 0 {
		probe.TrackNo = trackNumberFromFilename(relPath)
	}

	artistID, err := s.upsertArtist(albumArtist)
	if err != nil {
		slog.Error("upsert artist", "artist", albumArtist, "err", err)
		s.skip()
		return
	}
	albumID, err := s.upsertAlbum(artistID, albumArtist, album, probe.Year)
	if err != nil {
		slog.Error("upsert album", "album", album, "err", err)
		s.skip()
		return
	}

	trackID, isNew, err := s.upsertTrack(relPath, info, probe, title, artist, albumID, artistID)
	if err != nil {
		slog.Error("upsert track", "path", relPath, "err", err)
		s.skip()
		return
	}

	s.attachGenres(trackID, probe.Genre)
	s.attachLyrics(trackID, abs, probe.Lyrics)
	s.ensureAlbumArt(coverCtx, albumID, albumKeyOf(albumArtist, album), abs)

	s.markSeen(relPath)

	s.mu.Lock()
	s.state.Processed++
	if isNew {
		s.state.Added++
	} else {
		s.state.Updated++
	}
	s.mu.Unlock()
	s.report()
}

func (s *Scanner) skip() {
	s.mu.Lock()
	s.state.Processed++
	s.state.Skipped++
	s.mu.Unlock()
	s.report()
}

func (s *Scanner) report() {
	if s.opts.OnProgress != nil {
		s.opts.OnProgress(s.State())
	}
}

// unchanged reports whether the database already matches what is on disk.
func (s *Scanner) unchanged(relPath string, info os.FileInfo) bool {
	var size, modTime int64
	err := s.db.QueryRow(`SELECT file_size, mod_time FROM tracks WHERE path = ?`, relPath).Scan(&size, &modTime)
	if err != nil {
		return false
	}
	return size == info.Size() && modTime == info.ModTime().Unix()
}

func (s *Scanner) upsertArtist(name string) (int64, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		key = "unknown artist"
	}
	_, err := s.db.Exec(
		`INSERT INTO artists (name_key, name, sort_name) VALUES (?, ?, ?)
		 ON CONFLICT(name_key) DO UPDATE SET name = excluded.name`,
		key, name, sortName(name))
	if err != nil {
		return 0, err
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM artists WHERE name_key = ?`, key).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// upsertAlbum finds or creates an album. The key includes the artist so two
// albums sharing a title never merge. The year is taken from the first track
// that carries one, and left alone if a later track has none: an album's
// release year is a property of the release, not of any single file.
func (s *Scanner) upsertAlbum(artistID int64, albumArtist, title string, year int) (int64, error) {
	key := albumKeyOf(albumArtist, title)
	_, err := s.db.Exec(`
		INSERT INTO albums (artist_id, album_key, title, year, added_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(album_key) DO UPDATE SET year = COALESCE(albums.year, excluded.year)`,
		artistID, key, title, nullInt(year), db.Now())
	if err != nil {
		return 0, err
	}
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM albums WHERE album_key = ?`, key).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

// upsertTrack writes the track row, replacing any previous data for the path.
// It reports whether the track is new to the library.
func (s *Scanner) upsertTrack(relPath string, info os.FileInfo, probe *media.Probe, title, artist string, albumID, artistID int64) (int64, bool, error) {
	var existingID int64
	err := s.db.QueryRow(`SELECT id FROM tracks WHERE path = ?`, relPath).Scan(&existingID)
	isNew := err != nil

	hasLyrics := 0
	if strings.TrimSpace(probe.Lyrics) != "" {
		hasLyrics = 1
	}

	var trackNo, year any
	if probe.TrackNo > 0 {
		trackNo = probe.TrackNo
	}
	if probe.Year > 0 {
		year = probe.Year
	}

	_, err = s.db.Exec(`
		INSERT INTO tracks (path, file_size, mod_time, format, duration_ms, bitrate,
		                    sample_rate, channels, title, track_no, disc_no, year,
		                    artist_id, album_id, has_lyrics, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
		    file_size   = excluded.file_size,
		    mod_time    = excluded.mod_time,
		    format      = excluded.format,
		    duration_ms = excluded.duration_ms,
		    bitrate     = excluded.bitrate,
		    sample_rate = excluded.sample_rate,
		    channels    = excluded.channels,
		    title       = excluded.title,
		    track_no    = excluded.track_no,
		    disc_no     = excluded.disc_no,
		    year        = excluded.year,
		    artist_id   = excluded.artist_id,
		    album_id    = excluded.album_id,
		    has_lyrics  = excluded.has_lyrics`,
		relPath, info.Size(), info.ModTime().Unix(), probe.Format, probe.DurationMS,
		probe.Bitrate, probe.SampleRate, probe.Channels, title, trackNo, probe.DiscNo,
		year, artistID, albumID, hasLyrics, db.Now())
	if err != nil {
		return 0, false, err
	}

	var trackID int64
	if err := s.db.QueryRow(`SELECT id FROM tracks WHERE path = ?`, relPath).Scan(&trackID); err != nil {
		return 0, false, err
	}
	return trackID, isNew, nil
}

// ensureAlbumArt attaches artwork to an album, once. The album's own key is
// the cache key, so the extractor's fast path prevents re-running ffmpeg on
// every scan.
func (s *Scanner) ensureAlbumArt(ctx context.Context, albumID int64, albumKey, audioPath string) {
	var existing sql.NullString
	if err := s.db.QueryRow(`SELECT cover_path FROM albums WHERE id = ?`, albumID).Scan(&existing); err == nil && existing.Valid {
		return
	}

	sidecar, hasSidecar := media.FindSidecarCover(filepath.Dir(audioPath))

	res, err := s.extractor.Extract(ctx, albumKey, audioPath, sidecarPath(sidecar, hasSidecar))
	if err != nil {
		slog.Warn("cover art extraction failed", "album", albumKey, "err", err)
		return
	}
	if res.FileName == "" {
		return
	}

	_, err = s.db.Exec(`UPDATE albums SET cover_path = ?, dominant_color = ? WHERE id = ?`,
		res.FileName, nullString(res.Color), albumID)
	if err != nil {
		slog.Warn("could not save cover art", "album", albumKey, "err", err)
	}
}

func sidecarPath(path string, ok bool) string {
	if !ok {
		return ""
	}
	return path
}

// attachLyrics prefers an embedded lyric payload, falling back to a sidecar
// .lrc or .txt file next to the audio. A timestamped LRC is what the client
// needs for the scrolling pane, so its presence also sets has_lyrics.
func (s *Scanner) attachLyrics(trackID int64, audioPath, embedded string) {
	raw, source := strings.TrimSpace(embedded), "embedded"
	if raw == "" {
		raw, source = readSidecarLyrics(audioPath)
	}
	if raw == "" {
		_, _ = s.db.Exec(`DELETE FROM track_lyrics WHERE track_id = ?`, trackID)
		return
	}
	_, err := s.db.Exec(`
		INSERT INTO track_lyrics (track_id, raw, source) VALUES (?, ?, ?)
		ON CONFLICT(track_id) DO UPDATE SET raw = excluded.raw, source = excluded.source`,
		trackID, raw, source)
	if err != nil {
		slog.Warn("could not save lyrics", "track", trackID, "err", err)
		return
	}
	_, _ = s.db.Exec(`UPDATE tracks SET has_lyrics = 1 WHERE id = ?`, trackID)
}

// readSidecarLyrics looks for "track.lrc" then "track.txt" beside the audio.
func readSidecarLyrics(audioPath string) (string, string) {
	base := strings.TrimSuffix(audioPath, filepath.Ext(audioPath))
	for _, ext := range []string{".lrc", ".LRC", ".txt"} {
		data, err := os.ReadFile(base + ext)
		if err == nil && len(data) > 0 {
			return string(data), "sidecar"
		}
	}
	return "", ""
}

func (s *Scanner) attachGenres(trackID int64, genre string) {
	genre = strings.TrimSpace(genre)
	if genre == "" {
		return
	}
	// Genres are frequently a semicolon-separated list.
	for _, g := range strings.Split(genre, ";") {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO genres (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, g); err != nil {
			continue
		}
		var genreID int64
		if err := s.db.QueryRow(`SELECT id FROM genres WHERE name = ?`, g).Scan(&genreID); err != nil {
			continue
		}
		_, _ = s.db.Exec(
			`INSERT INTO track_genres (track_id, genre_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			trackID, genreID)
	}
}

// prepareSeenTable creates the scratch table used to work out which database
// rows no longer have a file behind them.
func (s *Scanner) prepareSeenTable() error {
	_, err := s.db.Exec(`CREATE TEMP TABLE IF NOT EXISTS seen_paths (path TEXT PRIMARY KEY)`)
	if err != nil {
		return fmt.Errorf("create seen_paths: %w", err)
	}
	_, err = s.db.Exec(`DELETE FROM seen_paths`)
	return err
}

func (s *Scanner) markSeen(relPath string) {
	_, _ = s.db.Exec(`INSERT INTO seen_paths (path) VALUES (?) ON CONFLICT(path) DO NOTHING`, relPath)
}

// pruneMissing deletes tracks whose files have disappeared. Cascades clear out
// the derived lyric, favourite, playlist and history rows with them.
func (s *Scanner) pruneMissing() error {
	res, err := s.db.Exec(`DELETE FROM tracks WHERE path NOT IN (SELECT path FROM seen_paths)`)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		slog.Info("removed tracks that are no longer on disk", "count", n)
		s.mu.Lock()
		s.state.Removed = int(n)
		s.mu.Unlock()
		s.report()
	}
	return nil
}

func (s *Scanner) dropSeenTable() {
	_, _ = s.db.Exec(`DROP TABLE IF EXISTS seen_paths`)
}
