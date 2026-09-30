package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/media"
)

// --- users ---

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Auth.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not list users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	IsAdmin  bool   `json:"isAdmin"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Password == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "password is required")
		return
	}

	user, err := s.Auth.CreateUser(req.Email, req.Password, req.Name, req.IsAdmin)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_user", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, user)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "user id must be a positive integer")
		return
	}

	// Refuse to delete the account making the request, and refuse to remove
	// the last administrator, so an instance cannot be left unmanageable.
	if auth.UserIDFrom(r.Context()) == id {
		writeError(w, http.StatusBadRequest, "cannot_delete_self", "you cannot delete your own account")
		return
	}
	var remainingAdmins int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE is_admin = 1 AND id != ?`, id).Scan(&remainingAdmins); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not delete user")
		return
	}
	var targetIsAdmin bool
	if err := s.DB.QueryRow(`SELECT is_admin FROM users WHERE id = ?`, id).Scan(&targetIsAdmin); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such user")
		return
	}
	if targetIsAdmin && remainingAdmins == 0 {
		writeError(w, http.StatusBadRequest, "last_admin", "at least one administrator must remain")
		return
	}

	if err := s.Auth.DeleteUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

// handleResetPassword sets a new password and revokes that user's sessions,
// which is what an admin expects a password reset to do.
func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt64(r, "id")
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_id", "user id must be a positive integer")
		return
	}
	var req resetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.Auth.SetPassword(id, req.Password); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_password", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- library scanning ---

// handleStartScan kicks off a scan. It returns 409 when one is already
// running so a double-tap does not start two walks of the same tree.
func (s *Server) handleStartScan(w http.ResponseWriter, r *http.Request) {
	if !s.Scanner.Start(r.Context()) {
		writeError(w, http.StatusConflict, "scan_running", "a scan is already in progress")
		return
	}
	writeJSON(w, http.StatusAccepted, s.Scanner.State())
}

func (s *Server) handleScanState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Scanner.State())
}

func (s *Server) handleCancelScan(w http.ResponseWriter, r *http.Request) {
	if !s.Scanner.IsRunning() {
		writeError(w, http.StatusConflict, "not_scanning", "no scan is running")
		return
	}
	s.Scanner.Cancel()
	writeJSON(w, http.StatusAccepted, s.Scanner.State())
}

// --- transcode cache ---

func (s *Server) handleCacheInfo(w http.ResponseWriter, r *http.Request) {
	size, err := s.Transcoder.CacheSize()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not read cache size")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sizeBytes": size,
		"directory": s.Config.TranscodeCacheDir,
		"prewarm":   s.Config.PrewarmTranscode,
		"profiles":  profileSummaries(),
	})
}

func (s *Server) handleClearCache(w http.ResponseWriter, r *http.Request) {
	// Seven days is long enough that anything still useful survives, and
	// short enough that the directory cannot grow without bound.
	removed, err := s.Transcoder.PruneCache(7 * 24 * time.Hour)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "could not clear cache")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"removed": removed})
}

// profileSummaries describes the available transcode targets so the client
// does not have to hardcode their bitrates and container types.
func profileSummaries() []map[string]any {
	out := []map[string]any{
		{"name": media.LosslessProfileName, "mimeType": "", "kbps": 0,
			"description": "Original file, no re-encoding"},
	}
	for _, p := range media.Profiles {
		out = append(out, map[string]any{
			"name":        p.Name,
			"mimeType":    p.MimeType,
			"kbps":        p.EstimatedKbps,
			"description": fmt.Sprintf("%d kbps %s", p.EstimatedKbps, strings.TrimPrefix(p.MimeType, "audio/")),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["kbps"].(int) < out[j]["kbps"].(int)
	})
	return out
}
