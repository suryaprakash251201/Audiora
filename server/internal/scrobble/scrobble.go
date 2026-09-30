// Package scrobble reports plays to Last.fm.
package scrobble

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/audiora/audiora/server/internal/db"
)

// Last.fm API endpoints. The desktop scrobbler API is stable and does not
// require per-user OAuth, which is the right fit for a self-hosted server.
const (
	authURL       = "https://www.last.fm/api/auth/?json=1"
	scrobbleURL   = "https://ws.audioscrobbler.com/2.0/"
	nowPlayingURL = "https://ws.audioscrobbler.com/2.0/"
)

// Setting keys in the user_settings table. They are exported because the HTTP
// layer writes them when a user connects their Last.fm account.
const (
	SettingAPIKey     = "lastfm.apiKey"
	SettingSecret     = "lastfm.secret"
	SettingSessionKey = "lastfm.sessionKey"
	SettingUsername   = "lastfm.username"
)

// Credentials are a user's Last.fm API keys plus the session they minted.
type Credentials struct {
	APIKey     string
	Secret     string
	SessionKey string
	Username   string
}

// Complete reports whether the user has everything needed to scrobble.
func (c Credentials) Complete() bool {
	return c.APIKey != "" && c.Secret != "" && c.SessionKey != ""
}

// maxAttempts is how many times a queued play is retried before it is
// dropped. Last.fm outages are usually short, so this spans a few days.
const maxAttempts = 8

// Service queues plays and drains them in the background.
type Service struct {
	db      *db.DB
	client  *http.Client
	stop    chan struct{}
	enabled bool
}

func NewService(database *db.DB) *Service {
	return &Service{
		db: database,
		// Last.fm is external and occasionally slow; a short timeout keeps a
		// hung request from stalling the drain loop.
		client: &http.Client{Timeout: 15 * time.Second},
		stop:   make(chan struct{}),
	}
}

// Queue records a play for later delivery. It is deliberately cheap and
// never blocks the HTTP request that is logging a play.
func (s *Service) Queue(userID, trackID, playedAt int64) error {
	if !s.isConfigured(userID) {
		return nil // nothing to do, and not an error
	}
	_, err := s.db.Exec(
		`INSERT INTO scrobble_queue (user_id, track_id, played_at) VALUES (?, ?, ?)`,
		userID, trackID, playedAt)
	return err
}

func (s *Service) isConfigured(userID int64) bool {
	creds, err := s.Credentials(userID)
	return err == nil && creds.Complete()
}

// Credentials reads a user's Last.fm keys from settings.
func (s *Service) Credentials(userID int64) (Credentials, error) {
	rows, err := s.db.Query(`SELECT key, value FROM user_settings WHERE user_id = ?`, userID)
	if err != nil {
		return Credentials{}, err
	}
	defer rows.Close()

	var c Credentials
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		switch k {
		case SettingAPIKey:
			c.APIKey = v
		case SettingSecret:
			c.Secret = v
		case SettingSessionKey:
			c.SessionKey = v
		case SettingUsername:
			c.Username = v
		}
	}
	return c, rows.Err()
}

// CreateSession exchanges an API key and secret for a Last.fm session key.
// This is the token the official desktop scrobbler uses, and it means a user
// can authorise Audiora from any browser without an OAuth dance.
func CreateSession(apiKey, secret, username, password string) (string, error) {
	if apiKey == "" || secret == "" {
		return "", errors.New("an API key and shared secret are required")
	}
	if username == "" || password == "" {
		return "", errors.New("a Last.fm username and password are required")
	}

	values := url.Values{}
	values.Set("method", "auth.getMobileSession")
	values.Set("api_key", apiKey)
	values.Set("username", username)
	values.Set("password", md5Hex(password))
	values.Set("api_sig", sign(map[string]string{
		"method":   "auth.getMobileSession",
		"username": username,
		"password": md5Hex(password),
		"api_key":  apiKey,
	}, secret))

	body, err := postForm(authURL, values)
	if err != nil {
		return "", err
	}

	var resp struct {
		Session struct {
			Name       string `json:"name"`
			Key        string `json:"key"`
			Subscriber int    `json:"subscriber"`
		} `json:"session"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("could not read Last.fm response: %w", err)
	}
	if resp.Error != 0 {
		// Last.fm error 4 is "invalid username or password", by far the most
		// common, and worth calling out specifically.
		if resp.Error == 4 {
			return "", errors.New("Last.fm rejected that username or password")
		}
		return "", fmt.Errorf("Last.fm error %d: %s", resp.Error, resp.Message)
	}
	if resp.Session.Key == "" {
		return "", errors.New("Last.fm did not return a session key")
	}
	return resp.Session.Key, nil
}

// NowPlaying announces the current track. Best effort: a failure here must
// never interrupt playback, so errors are logged and swallowed.
func (s *Service) NowPlaying(userID, trackID int64) {
	creds, err := s.Credentials(userID)
	if err != nil || !creds.Complete() {
		return
	}
	track, ok := s.trackInfo(trackID)
	if !ok {
		return
	}

	values := url.Values{}
	values.Set("method", "track.updateNowPlaying")
	values.Set("artist", track.Artist)
	values.Set("track", track.Title)
	values.Set("album", track.Album)
	values.Set("duration", fmt.Sprint(track.DurationMS/1000))
	values.Set("api_key", creds.APIKey)
	values.Set("sk", creds.SessionKey)
	values.Set("format", "json")
	values.Set("json", "1")

	if _, err := postForm(nowPlayingURL, values); err != nil {
		slog.Debug("now playing update failed", "err", err)
	}
}

// trackMetadata is the subset of a track Last.fm needs.
type trackMetadata struct {
	Artist     string
	Title      string
	Album      string
	DurationMS int64
}

func (s *Service) trackInfo(trackID int64) (trackMetadata, bool) {
	var t trackMetadata
	err := s.db.QueryRow(`
		SELECT a.name, t.title, al.title, t.duration_ms
		FROM tracks t
		JOIN artists a ON a.id = t.artist_id
		JOIN albums  al ON al.id = t.album_id
		WHERE t.id = ?`, trackID).
		Scan(&t.Artist, &t.Title, &t.Album, &t.DurationMS)
	return t, err == nil
}

// Start runs the background drain loop until the service is stopped.
func (s *Service) Start() {
	go func() {
		// A ticker rather than a tight loop: Last.fm explicitly asks clients
		// not to submit more than once every five seconds.
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ticker.C:
				if err := s.drain(); err != nil {
					slog.Warn("scrobble queue drain failed", "err", err)
				}
			}
		}
	}()
}

func (s *Service) Stop() { close(s.stop) }

// queueEntry is one pending play.
type queueEntry struct {
	ID       int64
	UserID   int64
	TrackID  int64
	PlayedAt int64
}

// drain submits queued plays, oldest first. An entry that fails is retried
// with backoff and eventually abandoned rather than blocking the queue.
func (s *Service) drain() error {
	rows, err := s.db.Query(`
		SELECT q.id, q.user_id, q.track_id, q.played_at
		FROM scrobble_queue q
		JOIN user_settings us ON us.user_id = q.user_id
		WHERE us.key = ? AND us.value != ''
		ORDER BY q.played_at
		LIMIT 50`, SettingSessionKey)
	if err != nil {
		return fmt.Errorf("read scrobble queue: %w", err)
	}

	entries := []queueEntry{}
	for rows.Next() {
		var e queueEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.TrackID, &e.PlayedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}
	rows.Close()

	for _, e := range entries {
		if err := s.submit(e); err != nil {
			s.fail(e, err)
			// A network or auth failure will affect every later entry too, so
			// stop here and let the next tick retry from the same place.
			if isFatal(err) {
				return err
			}
			continue
		}
		s.succeed(e)
	}
	return nil
}

// submit posts one play to Last.fm.
func (s *Service) submit(e queueEntry) error {
	creds, err := s.Credentials(e.UserID)
	if err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}
	if !creds.Complete() {
		// The user removed their keys; the entry is simply not deliverable.
		return nil
	}
	track, ok := s.trackInfo(e.TrackID)
	if !ok {
		// The track was deleted from the library after being queued.
		return nil
	}

	// Last.fm wants the timestamp the track finished, not when we submit.
	timestamp := time.Unix(e.PlayedAt, 0)

	values := url.Values{}
	values.Set("method", "track.scrobble")
	values.Set("artist", track.Artist)
	values.Set("track", track.Title)
	values.Set("album", track.Album)
	values.Set("timestamp", fmt.Sprint(e.PlayedAt))
	values.Set("duration", fmt.Sprint(track.DurationMS/1000))
	values.Set("chosenByUser", "0")
	values.Set("api_key", creds.APIKey)
	values.Set("sk", creds.SessionKey)
	values.Set("format", "json")
	values.Set("json", "1")
	values.Set("api_sig", sign(map[string]string{
		"method":       "track.scrobble",
		"artist":       track.Artist,
		"track":        track.Title,
		"album":        track.Album,
		"timestamp":    fmt.Sprint(e.PlayedAt),
		"chosenByUser": "0",
	}, creds.Secret))

	body, err := postForm(scrobbleURL, values)
	if err != nil {
		return err
	}

	var resp struct {
		Scrobbles struct {
			Attr []struct {
				Accepted int    `json:"accepted"`
				Ignored  string `json:"ignoredMessage"`
			} `json:"@attr"`
		} `json:"scrobbles"`
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("could not read Last.fm response: %w", err)
	}
	if resp.Error != 0 {
		return &lastfmError{code: resp.Error, message: resp.Message}
	}
	if len(resp.Scrobbles.Attr) > 0 && resp.Scrobbles.Attr[0].Accepted == 0 {
		// The request was well-formed but Last.fm declined the play, which is
		// not worth retrying.
		slog.Info("Last.fm ignored a play", "reason", resp.Scrobbles.Attr[0].Ignored)
	}
	_ = timestamp
	return nil
}

type lastfmError struct {
	code    int
	message string
}

func (e *lastfmError) Error() string {
	return fmt.Sprintf("Last.fm error %d: %s", e.code, e.message)
}

// isFatal reports whether retrying later could plausibly succeed. Auth and
// rate-limit errors are not worth hammering.
func isFatal(err error) bool {
	var lf *lastfmError
	if errors.As(err, &lf) {
		switch lf.code {
		case 9, 10, 26: // invalid session key, bad method, temporarily unavailable
			return true
		}
		return true
	}
	// Transport-level errors are worth another attempt later.
	return true
}

func (s *Service) succeed(e queueEntry) {
	if _, err := s.db.Exec(`DELETE FROM scrobble_queue WHERE id = ?`, e.ID); err != nil {
		slog.Warn("could not clear scrobble", "id", e.ID, "err", err)
		return
	}
	// Mark the matching history row so the UI can show it as delivered.
	_, _ = s.db.Exec(`UPDATE play_history SET scrobbled = 1
		WHERE user_id = ? AND track_id = ? AND scrobbled = 0`, e.UserID, e.TrackID)
}

func (s *Service) fail(e queueEntry, cause error) {
	// Back off by pushing the attempt into the future: entries are only
	// retried once they are old enough to be picked up again.
	attempts := 1
	_ = s.db.QueryRow(`SELECT attempts FROM scrobble_queue WHERE id = ?`, e.ID).Scan(&attempts)
	attempts++

	if attempts > maxAttempts {
		slog.Warn("giving up on a scrobble", "track", e.TrackID, "attempts", attempts, "err", cause)
		_, _ = s.db.Exec(`DELETE FROM scrobble_queue WHERE id = ?`, e.ID)
		return
	}

	// Move the entry's played_at back to age it out of the immediate window.
	backoff := time.Duration(attempts*attempts) * time.Minute
	newTime := time.Now().Add(-backoff).Unix()
	_, _ = s.db.Exec(`UPDATE scrobble_queue SET attempts = ?, last_error = ?,
		played_at = MIN(played_at, ?) WHERE id = ?`,
		attempts, cause.Error(), newTime, e.ID)

	slog.Debug("scrobble retry scheduled", "track", e.TrackID, "attempt", attempts, "in", backoff)
}

// sign builds the Last.fm API signature: the parameters sorted by key,
// concatenated as key+value, suffixed with the shared secret, then MD5'd.
func sign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sortStrings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString(params[k])
	}
	b.WriteString(secret)
	return md5Hex(b.String())
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// postForm submits form-encoded values and returns the response body.
func postForm(endpoint string, values url.Values) ([]byte, error) {
	resp, err := http.PostForm(endpoint, values)
	if err != nil {
		return nil, fmt.Errorf("Last.fm request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("could not read Last.fm response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Last.fm returned HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
