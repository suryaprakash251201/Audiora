// Package api wires the HTTP surface together.
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/config"
	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/media"
	"github.com/audiora/audiora/server/internal/models"
	"github.com/audiora/audiora/server/internal/scan"
	"github.com/audiora/audiora/server/internal/scrobble"
	"github.com/audiora/audiora/server/internal/sync"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

// Deps is everything the HTTP handlers need.
type Deps struct {
	Config     *config.Config
	DB         *db.DB
	Auth       *auth.Store
	Tokens     *auth.TokenIssuer
	AuthMW     *auth.Authenticator
	Transcoder *media.Transcoder
	Scanner    *scan.Scanner
	Hub        *sync.Hub
	Scrobbler  *scrobble.Service
}

// Server holds the HTTP handler tree.
type Server struct {
	Deps
	mux *chi.Mux
}

// New builds the router.
func New(deps Deps) *Server {
	s := &Server{Deps: deps, mux: chi.NewRouter()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.Use(chimw.RequestID)
	s.mux.Use(chimw.RealIP)
	s.mux.Use(chimw.Recoverer)
	s.mux.Use(s.cors)
	s.mux.Use(s.logRequests)

	s.mux.Get("/healthz", s.handleHealth)

	s.mux.Route("/api", func(r chi.Router) {
		// --- Public ---
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/refresh", s.handleRefresh)
		r.Post("/auth/register", s.handleRegister)

		// --- Authenticated ---
		r.Group(func(r chi.Router) {
			r.Use(s.AuthMW.Middleware)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)

			r.Get("/library/stats", s.handleStats)
			r.Get("/library/albums", s.handleAlbums)
			r.Get("/library/albums/{id}", s.handleAlbum)
			r.Get("/library/artists", s.handleArtists)
			r.Get("/library/artists/{id}", s.handleArtist)
			r.Get("/library/tracks/{id}", s.handleTrack)
			r.Get("/library/tracks/{id}/lyrics", s.handleLyrics)
			r.Get("/search", s.handleSearch)
			r.Get("/suggestions", s.handleSuggestions)

			r.Get("/playlists", s.handleListPlaylists)
			r.Post("/playlists", s.handleCreatePlaylist)
			r.Get("/playlists/{id}", s.handleGetPlaylist)
			r.Patch("/playlists/{id}", s.handleUpdatePlaylist)
			r.Delete("/playlists/{id}", s.handleDeletePlaylist)
			r.Post("/playlists/{id}/tracks", s.handleAddToPlaylist)
			r.Delete("/playlists/{id}/tracks/{trackId}", s.handleRemoveFromPlaylist)
			r.Post("/playlists/{id}/reorder", s.handleReorderPlaylist)

			r.Get("/favorites", s.handleListFavorites)
			r.Post("/favorites/{trackId}", s.handleAddFavorite)
			r.Delete("/favorites/{trackId}", s.handleRemoveFavorite)

			r.Post("/history", s.handleRecordPlay)
			r.Get("/history/recent", s.handleRecentHistory)
			r.Get("/history/stats", s.handleHistoryStats)

			// Playback sync across devices.
			r.Get("/sync/state", s.handleSyncState)

			// Per-user settings (Last.fm credentials).
			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Post("/scrobble/session", s.handleLastFMSession)

			// --- Admin ---
			r.Route("/admin", func(r chi.Router) {
				r.Use(s.AuthMW.RequireAdmin)
				r.Get("/users", s.handleListUsers)
				r.Post("/users", s.handleCreateUser)
				r.Delete("/users/{id}", s.handleDeleteUser)
				r.Post("/users/{id}/password", s.handleResetPassword)
				r.Post("/scan", s.handleStartScan)
				r.Get("/scan", s.handleScanState)
				r.Delete("/scan", s.handleCancelScan)
				r.Get("/cache", s.handleCacheInfo)
				r.Delete("/cache", s.handleClearCache)
			})
		})

		// --- Media ---
		//
		// These live outside the authenticated group on purpose. chi runs a
		// group's middleware before any route-specific one, so registering
		// them inside with an extra MediaMiddleware would still be rejected
		// first by the bearer-only check. This group is the only place they
		// are authenticated, and it accepts a query-string token because
		// <audio>, <img> and WebSocket cannot send an Authorization header.
		r.Group(func(r chi.Router) {
			r.Use(s.AuthMW.MediaMiddleware)
			r.Get("/stream/{id}", s.handleStream)
			r.Get("/covers/{name}", s.handleCover)
			r.Get("/sync/ws", s.handleSyncSocket)
		})
	})
}

// --- middleware ---

// cors allows the web app and the Capacitor shell to call the API.
//
// The mobile app loads from capacitor://localhost or http://localhost, which
// is not a real origin, so the Origin header can be absent. In that case the
// request is not a browser one and CORS does not apply.
func (s *Server) cors(next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(s.Config.CORSOrigins))
	for _, o := range s.Config.CORSOrigins {
		allowed[o] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			// Echoing the origin rather than "*" is required because the
			// mobile client sends an Authorization header.
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Range")
			// Without this the browser cannot read the byte offset of a
			// 206 response, which would break seeking.
			w.Header().Set("Access-Control-Expose-Headers",
				"Content-Range, Content-Length, Accept-Ranges, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		// Streaming responses are long-lived, so logging them on completion
		// would produce nothing useful; the request-id line is enough.
		if ww.Status() >= 400 || r.URL.Path == "/healthz" {
			slog.Info("request",
				"method", r.Method, "path", r.URL.Path, "status", ww.Status(),
				"duration", time.Since(start).Round(time.Millisecond),
				"requestId", chimw.GetReqID(r.Context()))
		}
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"scanning":   s.Scanner.IsRunning(),
		"ftsEnabled": s.DB.FTS,
	})
}

// --- helpers ---

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent, so there is nothing to do but log.
		slog.Error("could not write response body", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: code, Message: message})
}

// pathInt64 reads a positive integer path parameter.
func pathInt64(r *http.Request, key string) (int64, bool) {
	raw := chi.URLParam(r, key)
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func queryInt(r *http.Request, key string, fallback, min, max int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// trackColumns is the shared SELECT list for returning a full track. Keeping
// it in one place means every endpoint serialises a track identically.
const trackColumns = `
	t.id, t.title, t.duration_ms, t.bitrate, t.format, t.has_lyrics,
	t.track_no, t.disc_no, t.year, t.sample_rate, t.channels,
	t.artist_id, t.album_id, a.name, al.title, al.cover_path, al.dominant_color`

const trackJoin = `
	FROM tracks t
	JOIN artists a ON a.id = t.artist_id
	JOIN albums  al ON al.id = t.album_id`

// scanTrack reads one row produced by trackColumns.
func scanTrack(rows interface{ Scan(...any) error }) (models.Track, error) {
	var t models.Track
	err := rows.Scan(&t.ID, &t.Title, &t.DurationMS, &t.Bitrate, &t.Format, &t.HasLyrics,
		&t.TrackNo, &t.DiscNo, &t.Year, &t.SampleRate, &t.Channels,
		&t.ArtistID, &t.AlbumID, &t.Artist, &t.Album, &t.CoverPath, &t.CoverColor)
	return t, err
}

func collectTracks(rows *sql.Rows) ([]models.Track, error) {
	defer rows.Close()
	out := []models.Track{}
	for rows.Next() {
		t, err := scanTrack(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func isNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
