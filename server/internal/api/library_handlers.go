package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/media"
	"github.com/audiora/audiora/server/internal/models"
	"github.com/go-chi/chi/v5"
)

// maxPageSize caps how many rows one request can pull. Without it a client
// asking for limit=1000000 would allocate a gigabyte of Go structs.
const maxPageSize = 500

// --- library browsing ---

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var stats models.LibraryStats
	err := s.DB.QueryRow(`
		SELECT (SELECT COUNT(*) FROM artists),
		       (SELECT COUNT(*) FROM albums),
		       (SELECT COUNT(*) FROM tracks),
		       (SELECT COALESCE(SUM(duration_ms), 0) FROM tracks),
		       (SELECT COALESCE(SUM(file_size), 0) FROM tracks)`).
		Scan(&stats.Artists, &stats.Albums, &stats.Tracks, &stats.Duration, &stats.SizeBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read library stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAlbums(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 100, 1, maxPageSize)
	offset := queryInt(r, "offset", 0, 0, 1<<30)

	// Albums are huge on a real library, so sort the covers and counts in a
	// subquery rather than grouping over the full track join on every row.
	query := `
		SELECT a.id, a.artist_id, a.title, a.year, a.cover_path, a.dominant_color,
		       COUNT(t.id), COALESCE(SUM(t.duration_ms), 0), ar.name
		FROM albums a
		JOIN artists ar ON ar.id = a.artist_id
		LEFT JOIN tracks t ON t.album_id = a.id
		GROUP BY a.id
		ORDER BY COALESCE(a.year, 0) DESC, ar.sort_name, a.title
		LIMIT ? OFFSET ?`

	rows, err := s.DB.Query(query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list albums")
		return
	}
	defer rows.Close()

	out := []models.Album{}
	for rows.Next() {
		var al models.Album
		if err := rows.Scan(&al.ID, &al.ArtistID, &al.Title, &al.Year, &al.CoverPath,
			&al.DominantColor, &al.TrackCount, &al.DurationMS, &al.Artist); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not read album")
			return
		}
		out = append(out, al)
	}
	writeJSON(w, http.StatusOK, map[string]any{"albums": out, "count": len(out)})
}

func (s *Server) handleAlbum(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "album id must be a positive integer")
		return
	}

	var al models.Album
	err := s.DB.QueryRow(`
		SELECT a.id, a.artist_id, a.title, a.year, a.cover_path, a.dominant_color,
		       COUNT(t.id), COALESCE(SUM(t.duration_ms), 0), ar.name
		FROM albums a
		JOIN artists ar ON ar.id = a.artist_id
		LEFT JOIN tracks t ON t.album_id = a.id
		WHERE a.id = ?
		GROUP BY a.id`, id).
		Scan(&al.ID, &al.ArtistID, &al.Title, &al.Year, &al.CoverPath,
			&al.DominantColor, &al.TrackCount, &al.DurationMS, &al.Artist)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "no such album")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read album")
		return
	}

	tracks, err := s.queryTracks(`WHERE t.album_id = ? ORDER BY t.disc_no, COALESCE(t.track_no, 9999), t.title`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list album tracks")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"album": al, "tracks": tracks})
}

func (s *Server) handleArtists(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 200, 1, maxPageSize)
	offset := queryInt(r, "offset", 0, 0, 1<<30)

	query := `
		SELECT ar.id, ar.name, ar.sort_name,
		       COUNT(DISTINCT al.id), COUNT(t.id)
		FROM artists ar
		LEFT JOIN albums al ON al.artist_id = ar.id
		LEFT JOIN tracks t ON t.artist_id = ar.id
		GROUP BY ar.id
		ORDER BY ar.sort_name, ar.name
		LIMIT ? OFFSET ?`

	rows, err := s.DB.Query(query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list artists")
		return
	}
	defer rows.Close()

	out := []models.Artist{}
	for rows.Next() {
		var a models.Artist
		if err := rows.Scan(&a.ID, &a.Name, &a.SortName, &a.AlbumCount, &a.TrackCount); err != nil {
			continue
		}
		out = append(out, a)
	}
	writeJSON(w, http.StatusOK, map[string]any{"artists": out, "count": len(out)})
}

func (s *Server) handleArtist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "artist id must be a positive integer")
		return
	}

	var a models.Artist
	err := s.DB.QueryRow(`SELECT id, name, sort_name FROM artists WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.SortName)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "no such artist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read artist")
		return
	}

	albumRows, err := s.DB.Query(`
		SELECT a.id, a.artist_id, a.title, a.year, a.cover_path, a.dominant_color,
		       COUNT(t.id), COALESCE(SUM(t.duration_ms), 0), ar.name
		FROM albums a
		JOIN artists ar ON ar.id = a.artist_id
		LEFT JOIN tracks t ON t.album_id = a.id
		WHERE a.artist_id = ?
		GROUP BY a.id
		ORDER BY COALESCE(a.year, 0), a.title`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list artist albums")
		return
	}
	defer albumRows.Close()

	albums := []models.Album{}
	for albumRows.Next() {
		var al models.Album
		if err := albumRows.Scan(&al.ID, &al.ArtistID, &al.Title, &al.Year, &al.CoverPath,
			&al.DominantColor, &al.TrackCount, &al.DurationMS, &al.Artist); err != nil {
			continue
		}
		albums = append(albums, al)
	}

	tracks, err := s.queryTracks(`WHERE t.artist_id = ? ORDER BY al.title, t.disc_no, COALESCE(t.track_no, 9999)`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list artist tracks")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"artist": a, "albums": albums, "tracks": tracks})
}

func (s *Server) handleTrack(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "track id must be a positive integer")
		return
	}
	rows, err := s.DB.Query(`SELECT `+trackColumns+trackJoin+` WHERE t.id = ?`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read track")
		return
	}
	tracks, err := collectTracks(rows)
	if err != nil || len(tracks) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such track")
		return
	}
	writeJSON(w, http.StatusOK, tracks[0])
}

func (s *Server) handleLyrics(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "track id must be a positive integer")
		return
	}

	var raw, source string
	err := s.DB.QueryRow(`SELECT raw, source FROM track_lyrics WHERE track_id = ?`, id).Scan(&raw, &source)
	if isNotFound(err) {
		// A 200 with a null body is friendlier than a 404: the client can
		// treat "no lyrics" as an ordinary state rather than an error.
		writeJSON(w, http.StatusOK, map[string]any{"lyrics": nil, "source": ""})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read lyrics")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lyrics": raw, "source": source})
}

// queryTracks runs trackColumns+trackJoin with an arbitrary WHERE/ORDER.
func (s *Server) queryTracks(whereAndOrder string, args ...any) ([]models.Track, error) {
	rows, err := s.DB.Query(`SELECT `+trackColumns+trackJoin+` `+whereAndOrder, args...)
	if err != nil {
		return nil, err
	}
	return collectTracks(rows)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []any{}, "query": ""})
		return
	}
	limit := queryInt(r, "limit", 40, 1, maxPageSize)

	rows, err := s.DB.SearchTracks(q, limit)
	if err != nil {
		slog.Error("search query failed", "query", q, "err", err)
		writeError(w, http.StatusInternalServerError, "search_failed", "search is unavailable")
		return
	}
	defer rows.Close()

	// The column list here must stay in step with db.SearchTracks, in both
	// order and arity. A mismatch makes rows.Scan fail for every row, and
	// because that failure is per-row it looks exactly like "no results".
	results := []models.Track{}
	for rows.Next() {
		var t models.Track
		var path string
		err := rows.Scan(
			&t.ID, &t.Title, &path, &t.DurationMS, &t.Bitrate, &t.Format, &t.HasLyrics,
			&t.Artist, &t.Album, &t.CoverPath, &t.CoverColor)
		if err != nil {
			slog.Error("could not read a search result row", "err", err)
			writeError(w, http.StatusInternalServerError, "search_failed", "search is unavailable")
			return
		}
		results = append(results, t)
	}
	if err := rows.Err(); err != nil {
		slog.Error("search rows failed", "err", err)
		writeError(w, http.StatusInternalServerError, "search_failed", "search is unavailable")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results, "query": q, "count": len(results)})
}

// handleSuggestions powers the "jump to" strip: recently added albums and
// the most-played artists for this user.
func (s *Server) handleSuggestions(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())

	recent, err := s.queryTracks(`
		ORDER BY t.added_at DESC LIMIT 12`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not build suggestions")
		return
	}

	// The most-played artists for this user. The recency tiebreaker uses the
	// latest play in the group rather than the row's own timestamp, so an
	// artist played often years ago does not outrank one played often today.
	rows, err := s.DB.Query(`
		SELECT a.name, COUNT(*) AS plays
		FROM play_history h
		JOIN tracks t ON t.id = h.track_id
		JOIN artists a ON a.id = t.artist_id
		WHERE h.user_id = ?
		GROUP BY a.id
		ORDER BY plays DESC, MAX(h.played_at) DESC
		LIMIT 10`, userID)
	if err != nil {
		slog.Error("could not build top artists", "err", err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not build suggestions")
		return
	}
	defer rows.Close()

	type artistPlays struct {
		Name  string `json:"name"`
		Plays int    `json:"plays"`
	}
	top := []artistPlays{}
	for rows.Next() {
		var ap artistPlays
		if err := rows.Scan(&ap.Name, &ap.Plays); err != nil {
			slog.Error("could not read a top artist row", "err", err)
			writeError(w, http.StatusInternalServerError, "server_error", "could not build suggestions")
			return
		}
		top = append(top, ap)
	}
	if err := rows.Err(); err != nil {
		slog.Error("top artist rows failed", "err", err)
		writeError(w, http.StatusInternalServerError, "server_error", "could not build suggestions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"recentlyAdded": recent, "topArtists": top})
}

// --- cover art ---

// handleCover serves extracted artwork from the data directory.
func (s *Server) handleCover(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_name", "cover name is required")
		return
	}

	// The name comes from the URL but still has to be confined to the cover
	// directory. Rejecting any separator is enough, since every generated
	// name is a bare hex hash with a .jpg suffix.
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		writeError(w, http.StatusBadRequest, "bad_name", "invalid cover name")
		return
	}

	path := filepath.Join(s.Config.CoverArtDir, name)
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such cover")
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read cover")
		return
	}

	// Every stored cover is normalised to JPEG at import time, so the type
	// does not have to be guessed.
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "private, max-age=604800")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// --- streaming ---

// handleStream serves audio. The profile query parameter selects a transcode
// target; omitting it, or passing "lossless", serves the original file.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "track id must be a positive integer")
		return
	}

	var req media.TrackRequest
	err := s.DB.QueryRow(`SELECT id, path, file_size, mod_time, bitrate, duration_ms FROM tracks WHERE id = ?`, id).
		Scan(&req.ID, &req.RelPath, &req.Size, &req.ModTime, &req.Bitrate, &req.Duration)
	if isNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "no such track")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read track")
		return
	}

	// No profile at all means "give me the original file". Clients that want
	// something smaller always say so explicitly, and defaulting to a
	// transcode here would silently spend CPU and stall the first play of
	// every track for anyone using the bare URL.
	profile := r.URL.Query().Get("profile")
	if profile == "" {
		profile = r.URL.Query().Get("quality")
	}
	if profile == "" {
		profile = media.LosslessProfileName
	}
	s.Transcoder.Serve(w, r, req, profile)
}

// --- history ---

type recordPlayRequest struct {
	TrackID    int64   `json:"trackId"`
	Completion float64 `json:"completion"`
	PositionMS int64   `json:"positionMs"`
}

// handleRecordPlay logs a completed (or abandoned) play. It also queues a
// scrobble when the listener got far enough in, which is the rule the
// official scrobbler uses.
func (s *Server) handleRecordPlay(w http.ResponseWriter, r *http.Request) {
	var req recordPlayRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.TrackID <= 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "trackId is required")
		return
	}
	if req.Completion < 0 {
		req.Completion = 0
	}
	if req.Completion > 1 {
		req.Completion = 1
	}

	userID := auth.UserIDFrom(r.Context())

	// Confirm the track exists so a bad id cannot bloat the history table.
	var duration int64
	if err := s.DB.QueryRow(`SELECT duration_ms FROM tracks WHERE id = ?`, req.TrackID).Scan(&duration); isNotFound(err) {
		writeError(w, http.StatusNotFound, "not_found", "no such track")
		return
	}

	if _, err := s.DB.Exec(
		`INSERT INTO play_history (user_id, track_id, played_at, completion) VALUES (?, ?, ?, ?)`,
		userID, req.TrackID, db.Now(), req.Completion); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not record play")
		return
	}

	// A play counts for Last.fm past four minutes or half the track, so that
	// skipping a long track does not scrobble it.
	qualified := duration > 0 && (req.PositionMS >= 4*60*1000 || req.Completion >= 0.5)
	if qualified && s.Scrobbler != nil {
		if err := s.Scrobbler.Queue(userID, req.TrackID, db.Now()); err != nil {
			// Failing to queue a scrobble must not fail the play; the worker
			// retries and the history row is already safe.
			writeJSON(w, http.StatusAccepted, map[string]any{"recorded": true, "scrobbled": false})
			return
		}
	}

	writeJSON(w, http.StatusCreated, map[string]any{"recorded": true, "scrobbled": qualified})
}

func (s *Server) handleRecentHistory(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())
	limit := queryInt(r, "limit", 20, 1, 100)

	rows, err := s.DB.Query(`
		SELECT `+trackColumns+`
		FROM (
			SELECT track_id, MAX(played_at) AS last_played
			FROM play_history WHERE user_id = ?
			GROUP BY track_id
			ORDER BY last_played DESC
			LIMIT ?
		) h
		JOIN tracks t ON t.id = h.track_id
		JOIN artists a ON a.id = t.artist_id
		JOIN albums  al ON al.id = t.album_id
		ORDER BY h.last_played DESC`, userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read history")
		return
	}
	tracks, err := collectTracks(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}

func (s *Server) handleHistoryStats(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())

	var totalPlays, totalMs int64
	err := s.DB.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(t.duration_ms), 0)
		FROM play_history h JOIN tracks t ON t.id = h.track_id
		WHERE h.user_id = ?`, userID).Scan(&totalPlays, &totalMs)
	if err != nil && !isNotFound(err) {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read stats")
		return
	}

	weekAgo := db.Now() - 7*24*3600
	var weekPlays int64
	s.DB.QueryRow(`SELECT COUNT(*) FROM play_history WHERE user_id = ? AND played_at > ?`, userID, weekAgo).Scan(&weekPlays)

	writeJSON(w, http.StatusOK, map[string]any{
		"totalPlays":      totalPlays,
		"totalDurationMs": totalMs,
		"playsThisWeek":   weekPlays,
	})
}
