package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/models"
	"github.com/audiora/audiora/server/internal/scrobble"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type sessionResponse struct {
	User         *models.User `json:"user"`
	AccessToken  string       `json:"accessToken"`
	RefreshToken string       `json:"refreshToken"`
	// ExpiresAt is when the access token stops working, so the client can
	// refresh proactively instead of on a failed request.
	ExpiresAt int64 `json:"expiresAt"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Email) == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "missing_credentials", "email and password are required")
		return
	}

	user, err := s.Auth.Authenticate(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// Deliberately vague: a distinct "no such user" reply would let
			// anyone enumerate which addresses have accounts.
			writeError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
			return
		}
		writeError(w, http.StatusInternalServerError, "login_failed", "could not sign in")
		return
	}

	s.issueSession(w, r, user, http.StatusOK)
}

// handleRegister is open only while no accounts exist, so the first-run
// bootstrap cannot be raced by anyone who reaches the server later.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	count, err := s.Auth.UserCount()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not count users")
		return
	}
	if count > 0 {
		writeError(w, http.StatusForbidden, "registration_closed",
			"This server already has accounts. Ask the administrator for an invite.")
		return
	}

	var req struct {
		loginRequest
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	user, err := s.Auth.CreateUser(req.Email, req.Password, req.Name, true)
	if err != nil {
		if errors.Is(err, auth.ErrEmailTaken) {
			writeError(w, http.StatusConflict, "email_taken", "that email is already registered")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_registration", err.Error())
		return
	}
	s.issueSession(w, r, user, http.StatusCreated)
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "missing_token", "refreshToken is required")
		return
	}

	user, access, expires, refresh, err := s.Auth.Refresh(req.RefreshToken, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrTokenInvalid) {
			writeError(w, http.StatusUnauthorized, "invalid_token", "session expired, sign in again")
			return
		}
		writeError(w, http.StatusInternalServerError, "refresh_failed", "could not refresh session")
		return
	}

	writeJSON(w, http.StatusOK, sessionResponse{
		User: user, AccessToken: access, RefreshToken: refresh, ExpiresAt: expires.Unix(),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	// A malformed body is not worth failing the logout over: the client wants
	// to clear its state either way.
	if json.NewDecoder(r.Body).Decode(&req) == nil && req.RefreshToken != "" {
		if err := s.Auth.Revoke(req.RefreshToken); err != nil {
			writeError(w, http.StatusInternalServerError, "logout_failed", "could not end session")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFrom(r.Context())
	user, err := s.Auth.GetUser(claims.UserID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unknown_user", "account no longer exists")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, user *models.User, status int) {
	access, expires, refresh, err := s.Auth.IssueSession(user, r.UserAgent())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "session_failed", "could not start session")
		return
	}
	writeJSON(w, status, sessionResponse{
		User: user, AccessToken: access, RefreshToken: refresh, ExpiresAt: expires.Unix(),
	})
}

// handleLastFMSession exchanges a Last.fm username and password for a
// session key and stores it alongside the user's API keys.
func (s *Server) handleLastFMSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey   string `json:"apiKey"`
		Secret   string `json:"secret"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}

	sessionKey, err := scrobble.CreateSession(req.APIKey, req.Secret, req.Username, req.Password)
	if err != nil {
		// The upstream error is safe to pass back: it describes the
		// credentials the user just typed, not anything secret about us.
		writeError(w, http.StatusBadGateway, "lastfm_failed", err.Error())
		return
	}

	userID := auth.UserIDFrom(r.Context())
	settings := map[string]string{
		scrobble.SettingAPIKey:     req.APIKey,
		scrobble.SettingSecret:     req.Secret,
		scrobble.SettingSessionKey: sessionKey,
		scrobble.SettingUsername:   req.Username,
	}
	for k, v := range settings {
		if _, err := s.DB.Exec(`
			INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			userID, k, v, db.Now()); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not save Last.fm settings")
			return
		}
	}

	// The session key is a bearer credential, so it is not echoed back.
	writeJSON(w, http.StatusCreated, map[string]any{
		"connected": true,
		"username":  req.Username,
	})
}

// decodeJSON reads a JSON body, rejecting unknown fields so a client typo
// surfaces as an error rather than being silently ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// --- user settings (per-user key/value, used for Last.fm credentials) ---

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFrom(r.Context())
	rows, err := s.DB.Query(`SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read settings")
		return
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}

type settingsRequest struct {
	Settings map[string]string `json:"settings"`
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	userID := auth.UserIDFrom(r.Context())

	tx, err := s.DB.Begin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not save settings")
		return
	}
	defer tx.Rollback()

	for k, v := range req.Settings {
		if _, err := tx.Exec(`
			INSERT INTO user_settings (user_id, key, value, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			userID, k, v, db.Now()); err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "could not save settings")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not save settings")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
