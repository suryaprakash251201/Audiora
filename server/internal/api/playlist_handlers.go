package api

import (
	"database/sql"
	"net/http"
	"strings"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/models"
)

// playlistColumns picks the representative cover for a playlist: the art of
// the most recently added track, which is what a listener expects to see.
const playlistColumns = `
	p.id, p.name, p.description, p.created_at, p.updated_at,
	(SELECT COUNT(*) FROM playlist_tracks pt WHERE pt.playlist_id = p.id),
	(SELECT COALESCE(SUM(t.duration_ms), 0)
	   FROM playlist_tracks pt JOIN tracks t ON t.id = pt.track_id
	  WHERE pt.playlist_id = p.id),
	(SELECT al.cover_path FROM playlist_tracks pt
	   JOIN tracks t ON t.id = pt.track_id
	   JOIN albums al ON al.id = t.album_id
	  WHERE pt.playlist_id = p.id AND al.cover_path IS NOT NULL
	  ORDER BY pt.position LIMIT 1),
	(SELECT al.dominant_color FROM playlist_tracks pt
	   JOIN tracks t ON t.id = pt.track_id
	   JOIN albums al ON al.id = t.album_id
	  WHERE pt.playlist_id = p.id AND al.dominant_color IS NOT NULL
	  ORDER BY pt.position LIMIT 1)`

func scanPlaylist(rows interface{ Scan(...any) error }) (models.Playlist, error) {
	var p models.Playlist
	err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.UpdatedAt,
		&p.TrackCount, &p.DurationMS, &p.CoverPath, &p.CoverColor)
	return p, err
}

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())
	rows, err := s.DB.Query(`SELECT `+playlistColumns+`
		FROM playlists p WHERE p.user_id = ?
		ORDER BY p.updated_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list playlists")
		return
	}
	defer rows.Close()

	out := []models.Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"playlists": out})
}

type createPlaylistRequest struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TrackIDs    []int64 `json:"trackIds"`
}

func (s *Server) handleCreatePlaylist(w http.ResponseWriter, r *http.Request) {
	var req createPlaylistRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "playlist name is required")
		return
	}

	userID := auth.UserIDFrom(r.Context())
	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not create playlist")
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(`INSERT INTO playlists (user_id, name, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, userID, name, req.Description, db.Now(), db.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not create playlist")
		return
	}
	id, _ := res.LastInsertId()

	if err := insertPlaylistTracks(tx, id, req.TrackIDs, 0); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add tracks")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not create playlist")
		return
	}

	s.writePlaylist(w, r, id, http.StatusCreated)
}

type updatePlaylistRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

func (s *Server) handleUpdatePlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "playlist id must be a positive integer")
		return
	}
	var req updatePlaylistRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Description == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "nothing to update")
		return
	}

	userID := auth.UserIDFrom(r.Context())
	if !s.ownsPlaylist(userID, id) {
		writeError(w, http.StatusNotFound, "not_found", "no such playlist")
		return
	}

	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "playlist name cannot be empty")
			return
		}
		if _, err := s.DB.Exec(`UPDATE playlists SET name = ?, updated_at = ? WHERE id = ?`,
			strings.TrimSpace(*req.Name), db.Now(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not rename playlist")
			return
		}
	} else {
		if _, err := s.DB.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, db.Now(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not update playlist")
			return
		}
	}
	if req.Description != nil {
		if _, err := s.DB.Exec(`UPDATE playlists SET description = ? WHERE id = ?`, *req.Description, id); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not update playlist")
			return
		}
	}

	s.writePlaylist(w, r, id, http.StatusOK)
}

func (s *Server) handleGetPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "playlist id must be a positive integer")
		return
	}
	s.writePlaylist(w, r, id, http.StatusOK)
}

func (s *Server) writePlaylist(w http.ResponseWriter, r *http.Request, id int64, status int) {
	userID := auth.UserIDFrom(r.Context())

	rows, err := s.DB.Query(`SELECT `+playlistColumns+` FROM playlists p WHERE p.id = ? AND p.user_id = ?`, id, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read playlist")
		return
	}
	playlists, err := collectPlaylists(rows)
	if err != nil || len(playlists) == 0 {
		writeError(w, http.StatusNotFound, "not_found", "no such playlist")
		return
	}

	trackRows, err := s.DB.Query(`
		SELECT `+trackColumns+`
		FROM playlist_tracks pt
		JOIN tracks t ON t.id = pt.track_id
		JOIN artists a ON a.id = t.artist_id
		JOIN albums  al ON al.id = t.album_id
		WHERE pt.playlist_id = ?
		ORDER BY pt.position`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read playlist tracks")
		return
	}
	tracks, err := collectTracks(trackRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read playlist tracks")
		return
	}

	writeJSON(w, status, map[string]any{"playlist": playlists[0], "tracks": tracks})
}

func collectPlaylists(rows *sql.Rows) ([]models.Playlist, error) {
	defer rows.Close()
	out := []models.Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type addTracksRequest struct {
	TrackIDs []int64 `json:"trackIds"`
}

func (s *Server) handleAddToPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "playlist id must be a positive integer")
		return
	}
	userID := auth.UserIDFrom(r.Context())
	if !s.ownsPlaylist(userID, id) {
		writeError(w, http.StatusNotFound, "not_found", "no such playlist")
		return
	}

	var req addTracksRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.TrackIDs) == 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "trackIds is required")
		return
	}

	// New tracks go on the end, so positions never collide with existing rows.
	var maxPos sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MAX(position) FROM playlist_tracks WHERE playlist_id = ?`, id).Scan(&maxPos); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add tracks")
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add tracks")
		return
	}
	defer tx.Rollback()

	if err := insertPlaylistTracks(tx, id, req.TrackIDs, int(maxPos.Int64)); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add tracks")
		return
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, db.Now(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add tracks")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add tracks")
		return
	}

	s.writePlaylist(w, r, id, http.StatusOK)
}

// insertPlaylistTracks appends tracks, skipping ids already present so that
// adding a whole album twice does not duplicate it.
func insertPlaylistTracks(tx *sql.Tx, playlistID int64, trackIDs []int64, fromPos int) error {
	pos := fromPos
	for _, trackID := range trackIDs {
		if trackID <= 0 {
			continue
		}
		pos++
		// A track that no longer exists in the library is skipped rather than
		// failing the whole request.
		_, err := tx.Exec(`
			INSERT INTO playlist_tracks (playlist_id, track_id, position, added_at)
			SELECT ?, id, ?, ? FROM tracks WHERE id = ?
			ON CONFLICT(playlist_id, track_id) DO NOTHING`,
			playlistID, pos, db.Now(), trackID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) handleRemoveFromPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "playlist id must be a positive integer")
		return
	}
	trackID, ok := pathInt64(r, "trackId")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "track id must be a positive integer")
		return
	}

	userID := auth.UserIDFrom(r.Context())
	if !s.ownsPlaylist(userID, id) {
		writeError(w, http.StatusNotFound, "not_found", "no such playlist")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM playlist_tracks WHERE playlist_id = ? AND track_id = ?`, id, trackID); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not remove track")
		return
	}
	if _, err := s.DB.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, db.Now(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not update playlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type reorderRequest struct {
	TrackIDs []int64 `json:"trackIds"`
}

// handleReorderPlaylist rewrites positions from the client's ordering. The
// full ordered id list is sent rather than a move instruction because it is
// simple, idempotent, and cannot drift out of sync with the client's view.
func (s *Server) handleReorderPlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "playlist id must be a positive integer")
		return
	}
	userID := auth.UserIDFrom(r.Context())
	if !s.ownsPlaylist(userID, id) {
		writeError(w, http.StatusNotFound, "not_found", "no such playlist")
		return
	}

	var req reorderRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not reorder")
		return
	}
	defer tx.Rollback()

	// Two passes so the new positions cannot collide with the old ones while
	// the unique-ish ordering is being rewritten.
	if _, err := tx.Exec(`UPDATE playlist_tracks SET position = position + 1000000 WHERE playlist_id = ?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not reorder")
		return
	}
	for i, trackID := range req.TrackIDs {
		if _, err := tx.Exec(
			`UPDATE playlist_tracks SET position = ? WHERE playlist_id = ? AND track_id = ?`,
			i+1, id, trackID); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not reorder")
			return
		}
	}
	if _, err := tx.Exec(`UPDATE playlists SET updated_at = ? WHERE id = ?`, db.Now(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not reorder")
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not reorder")
		return
	}

	s.writePlaylist(w, r, id, http.StatusOK)
}

func (s *Server) handleDeletePlaylist(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "playlist id must be a positive integer")
		return
	}
	userID := auth.UserIDFrom(r.Context())
	if !s.ownsPlaylist(userID, id) {
		writeError(w, http.StatusNotFound, "not_found", "no such playlist")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM playlists WHERE id = ? AND user_id = ?`, id, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not delete playlist")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ownsPlaylist is the authorisation check for every playlist mutation. It
// returns false for another user's playlist so the handler can answer 404
// rather than 403, which avoids confirming that the id exists.
func (s *Server) ownsPlaylist(userID, playlistID int64) bool {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM playlists WHERE id = ? AND user_id = ?`, playlistID, userID).Scan(&n)
	return err == nil && n > 0
}

// --- favorites ---

func (s *Server) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())
	rows, err := s.DB.Query(`
		SELECT `+trackColumns+`
		FROM favorites f
		JOIN tracks t ON t.id = f.track_id
		JOIN artists a ON a.id = t.artist_id
		JOIN albums  al ON al.id = t.album_id
		WHERE f.user_id = ?
		ORDER BY f.created_at DESC`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list favourites")
		return
	}
	tracks, err := collectTracks(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list favourites")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tracks": tracks})
}

func (s *Server) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	trackID, ok := pathInt64(r, "trackId")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "track id must be a positive integer")
		return
	}
	_, err := s.DB.Exec(`
		INSERT INTO favorites (user_id, track_id, created_at) VALUES (?, ?, ?)
		ON CONFLICT(user_id, track_id) DO NOTHING`,
		auth.UserIDFrom(r.Context()), trackID, db.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not add favourite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	trackID, ok := pathInt64(r, "trackId")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "track id must be a positive integer")
		return
	}
	if _, err := s.DB.Exec(`DELETE FROM favorites WHERE user_id = ? AND track_id = ?`,
		auth.UserIDFrom(r.Context()), trackID); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not remove favourite")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
