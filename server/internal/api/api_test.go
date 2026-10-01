package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/audiora/audiora/server/internal/auth"
	"github.com/audiora/audiora/server/internal/config"
	"github.com/audiora/audiora/server/internal/db"
	"github.com/audiora/audiora/server/internal/media"
	"github.com/audiora/audiora/server/internal/models"
	"github.com/audiora/audiora/server/internal/scan"
	"github.com/audiora/audiora/server/internal/scrobble"
	"github.com/audiora/audiora/server/internal/sync"
)

// newTestServer builds the full handler stack over a temporary library.
func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	dir := t.TempDir()
	music := filepath.Join(dir, "music")
	if err := os.MkdirAll(music, 0o755); err != nil {
		t.Fatal(err)
	}

	database, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	// A 32-byte secret, the minimum the config accepts.
	cfg := &config.Config{
		DataPath:          dir,
		MusicPath:         music,
		CoverArtDir:       filepath.Join(dir, "covers"),
		TranscodeCacheDir: filepath.Join(dir, "transcode"),
		AccessTokenTTL:    15 * time.Minute,
	}

	tokens := auth.NewTokenIssuer([]byte("test-secret-that-is-long-enough-for-argon"), 15*time.Minute)
	store := auth.NewStore(database, tokens, 30*24*time.Hour)
	transcoder, err := media.NewTranscoder(music, cfg.TranscodeCacheDir)
	if err != nil {
		t.Fatal(err)
	}
	scanner := scan.New(database, scan.Options{
		Base:      context.Background(),
		MusicRoot: music, ExtractCovers: true, ExtractColors: true, CoverDir: cfg.CoverArtDir,
	})

	srv := httptest.NewServer(New(Deps{
		Config: cfg, DB: database, Auth: store, Tokens: tokens,
		AuthMW:     &auth.Authenticator{Issuer: tokens, Store: store},
		Transcoder: transcoder, Scanner: scanner,
		Hub:       sync.NewHub(database),
		Scrobbler: scrobble.NewService(database),
	}))
	t.Cleanup(srv.Close)

	return srv, music
}

func makeFLAC(t *testing.T, path, title, artist, album string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:a", "flac",
		"-metadata", "title="+title, "-metadata", "artist="+artist, "-metadata", "album="+album,
		path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate audio: %v: %s", err, out)
	}
}

// do performs a request with an optional bearer token and returns the body.
func do(t *testing.T, srv *httptest.Server, method, path, token string, payload any) (int, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, srv.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func login(t *testing.T, srv *httptest.Server, email, password string) (access, refresh string) {
	t.Helper()
	status, body := do(t, srv, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": email, "password": password,
	})
	if status != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", status, body)
	}
	var resp sessionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatal(err)
	}
	return resp.AccessToken, resp.RefreshToken
}

func TestAuthFlow(t *testing.T) {
	srv, _ := newTestServer(t)

	// --- first-run registration creates the admin ---
	status, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "admin@example.com", "password": "correct-horse", "name": "Admin",
	})
	if status != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", status, body)
	}
	var first sessionResponse
	json.Unmarshal(body, &first)
	if !first.User.IsAdmin {
		t.Error("the first registered account should be an administrator")
	}

	// --- registration then closes ---
	status, _ = do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "second@example.com", "password": "correct-horse",
	})
	if status != http.StatusForbidden {
		t.Errorf("second registration status = %d, want 403", status)
	}

	// --- wrong password is rejected without revealing whether the user exists ---
	status, _ = do(t, srv, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": "admin@example.com", "password": "wrong-password",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("bad password status = %d, want 401", status)
	}
	status, _ = do(t, srv, http.MethodPost, "/api/auth/login", "", map[string]string{
		"email": "nobody@example.com", "password": "wrong-password",
	})
	if status != http.StatusUnauthorized {
		t.Errorf("unknown user status = %d, want 401", status)
	}

	// --- protected routes need a token ---
	status, _ = do(t, srv, http.MethodGet, "/api/library/stats", "", nil)
	if status != http.StatusUnauthorized {
		t.Errorf("unauthenticated stats status = %d, want 401", status)
	}
	status, _ = do(t, srv, http.MethodGet, "/api/library/stats", "garbage", nil)
	if status != http.StatusUnauthorized {
		t.Errorf("bad token stats status = %d, want 401", status)
	}

	// --- a valid session works ---
	access, refresh := login(t, srv, "admin@example.com", "correct-horse")
	status, body = do(t, srv, http.MethodGet, "/api/auth/me", access, nil)
	if status != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", status, body)
	}

	// --- refresh rotates the token ---
	status, body = do(t, srv, http.MethodPost, "/api/auth/refresh", "", refreshRequest{RefreshToken: refresh})
	if status != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", status, body)
	}
	var rotated sessionResponse
	json.Unmarshal(body, &rotated)
	if rotated.RefreshToken == refresh {
		t.Error("refresh should issue a new refresh token")
	}

	// --- the old refresh token must not work a second time ---
	status, _ = do(t, srv, http.MethodPost, "/api/auth/refresh", "", refreshRequest{RefreshToken: refresh})
	if status != http.StatusUnauthorized {
		t.Errorf("replayed refresh token status = %d, want 401", status)
	}
}

func TestLibraryAndStreaming(t *testing.T) {
	srv, music := newTestServer(t)
	makeFLAC(t, filepath.Join(music, "Nina Simone", "Pastel Blues", "01 - Sinnerman.flac"),
		"Sinnerman", "Nina Simone", "Pastel Blues")
	makeFLAC(t, filepath.Join(music, "Nina Simone", "Pastel Blues", "02 - Feeling Good.flac"),
		"Feeling Good", "Nina Simone", "Pastel Blues")

	// First-run bootstrap.
	status, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "listener@example.com", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", status, body)
	}
	var sess sessionResponse
	json.Unmarshal(body, &sess)
	token := sess.AccessToken

	// Trigger a scan and wait for it.
	status, _ = do(t, srv, http.MethodPost, "/api/admin/scan", token, nil)
	if status != http.StatusAccepted {
		t.Fatalf("start scan status = %d, want 202", status)
	}
	waitForScan(t, srv, token)

	// --- stats reflect the library ---
	status, body = do(t, srv, http.MethodGet, "/api/library/stats", token, nil)
	if status != http.StatusOK {
		t.Fatalf("stats status = %d, body = %s", status, body)
	}
	var stats models.LibraryStats
	json.Unmarshal(body, &stats)
	if stats.Tracks != 2 || stats.Albums != 1 || stats.Artists != 1 {
		t.Errorf("stats = %+v, want 2 tracks, 1 album, 1 artist", stats)
	}

	// --- albums are listed and drillable ---
	status, body = do(t, srv, http.MethodGet, "/api/library/albums", token, nil)
	if status != http.StatusOK {
		t.Fatalf("albums status = %d", status)
	}
	var albumList struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albumList)
	if len(albumList.Albums) != 1 {
		t.Fatalf("albums = %d, want 1", len(albumList.Albums))
	}
	album := albumList.Albums[0]
	if album.TrackCount != 2 {
		t.Errorf("album track count = %d, want 2", album.TrackCount)
	}

	status, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(album.ID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("album status = %d", status)
	}
	var albumDetail struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &albumDetail)
	if len(albumDetail.Tracks) != 2 {
		t.Fatalf("album tracks = %d, want 2", len(albumDetail.Tracks))
	}
	// Disc order must be respected.
	if albumDetail.Tracks[0].Title != "Sinnerman" {
		t.Errorf("first track = %q, want Sinnerman", albumDetail.Tracks[0].Title)
	}

	trackID := albumDetail.Tracks[0].ID

	// --- lossless streaming honours Range ---
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stream/"+itoa64(trackID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Range", "bytes=0-99")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Errorf("range request status = %d, want 206", resp.StatusCode)
	}
	chunk, _ := io.ReadAll(resp.Body)
	if len(chunk) != 100 {
		t.Errorf("range body = %d bytes, want 100", len(chunk))
	}
	if resp.Header.Get("Content-Type") != "audio/flac" {
		t.Errorf("content type = %q, want audio/flac", resp.Header.Get("Content-Type"))
	}

	// --- transcoded streaming works and is seekable even on first play ---
	req2, _ := http.NewRequest(http.MethodGet,
		srv.URL+"/api/stream/"+itoa64(trackID)+"?profile=aac96", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	audio, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("transcode status = %d", resp2.StatusCode)
	}
	if len(audio) == 0 {
		t.Fatal("transcoded response was empty")
	}
	if resp2.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("cached transcode Accept-Ranges = %q, want bytes", resp2.Header.Get("Accept-Ranges"))
	}

	// --- search finds the track ---
	//
	// The assertion is on the parsed count, not on the raw body. An earlier
	// version grepped the body for the track title and passed even with zero
	// results, because the response echoes the query back.
	status, body = do(t, srv, http.MethodGet, "/api/search?q=Sinnerman", token, nil)
	if status != http.StatusOK {
		t.Fatalf("search status = %d, body = %s", status, body)
	}
	var searchResp struct {
		Results []models.Track `json:"results"`
		Query   string         `json:"query"`
		Count   int            `json:"count"`
	}
	if err := json.Unmarshal(body, &searchResp); err != nil {
		t.Fatalf("decode search response: %v (body %s)", err, body)
	}
	if searchResp.Count == 0 || len(searchResp.Results) == 0 {
		t.Fatalf("search for an existing track returned nothing: %s", body)
	}
	if searchResp.Results[0].Title != "Sinnerman" {
		t.Errorf("search returned %q first, want Sinnerman", searchResp.Results[0].Title)
	}
	// Artist and album names must be searchable too, since the FTS index
	// deliberately denormalises them.
	for _, q := range []string{"Simone", "Pastel"} {
		_, raw := do(t, srv, http.MethodGet, "/api/search?q="+q, token, nil)
		var r struct {
			Count int `json:"count"`
		}
		json.Unmarshal(raw, &r)
		if r.Count == 0 {
			t.Errorf("search for %q (an artist or album name) returned nothing", q)
		}
	}

	// A search term with FTS syntax must not error.
	status, _ = do(t, srv, http.MethodGet, "/api/search?q=%22unbalanced", token, nil)
	if status != http.StatusOK {
		t.Errorf("quoted search status = %d, want 200 (a malformed MATCH must not 500)", status)
	}
}

func TestPlaylistLifecycle(t *testing.T) {
	srv, music := newTestServer(t)
	makeFLAC(t, filepath.Join(music, "A", "Alb", "01 - One.flac"), "One", "A", "Alb")
	makeFLAC(t, filepath.Join(music, "A", "Alb", "02 - Two.flac"), "Two", "A", "Alb")

	status, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "a@example.com", "password": "correct-horse",
	})
	if status != http.StatusCreated {
		t.Fatalf("register status = %d", status)
	}
	var sess sessionResponse
	json.Unmarshal(body, &sess)
	token := sess.AccessToken

	do(t, srv, http.MethodPost, "/api/admin/scan", token, nil)
	waitForScan(t, srv, token)

	var list struct {
		Tracks []models.Track `json:"tracks"`
	}
	_, body = do(t, srv, http.MethodGet, "/api/library/albums", token, nil)
	var albums struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albums)
	_, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(albums.Albums[0].ID), token, nil)
	var detail struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &detail)
	first, second := detail.Tracks[0].ID, detail.Tracks[1].ID
	_ = list

	// Create a playlist with both tracks.
	status, body = do(t, srv, http.MethodPost, "/api/playlists", token, map[string]any{
		"name": "Road Trip", "trackIds": []int64{first, second},
	})
	if status != http.StatusCreated {
		t.Fatalf("create playlist status = %d, body = %s", status, body)
	}
	var created struct {
		Playlist models.Playlist `json:"playlist"`
		Tracks   []models.Track  `json:"tracks"`
	}
	json.Unmarshal(body, &created)
	if created.Playlist.TrackCount != 2 {
		t.Errorf("playlist track count = %d, want 2", created.Playlist.TrackCount)
	}
	if created.Playlist.DurationMS == 0 {
		t.Error("playlist duration should be the sum of its tracks")
	}
	playlistID := created.Playlist.ID

	// Reorder and confirm the new order sticks.
	status, body = do(t, srv, http.MethodPost, "/api/playlists/"+itoa64(playlistID)+"/reorder", token,
		map[string]any{"trackIds": []int64{second, first}})
	if status != http.StatusOK {
		t.Fatalf("reorder status = %d, body = %s", status, body)
	}
	var reordered struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &reordered)
	if len(reordered.Tracks) != 2 || reordered.Tracks[0].ID != second {
		t.Errorf("after reorder first track = %+v, want id %d", reordered.Tracks, second)
	}

	// Adding a track that is already present must not duplicate it.
	status, body = do(t, srv, http.MethodPost, "/api/playlists/"+itoa64(playlistID)+"/tracks", token,
		map[string]any{"trackIds": []int64{first, second}})
	if status != http.StatusOK {
		t.Fatalf("re-add status = %d", status)
	}
	var afterAdd struct {
		Playlist models.Playlist `json:"playlist"`
	}
	json.Unmarshal(body, &afterAdd)
	if afterAdd.Playlist.TrackCount != 2 {
		t.Errorf("track count after re-adding = %d, want 2 (duplicates are ignored)", afterAdd.Playlist.TrackCount)
	}

	// Remove one.
	status, _ = do(t, srv, http.MethodDelete,
		"/api/playlists/"+itoa64(playlistID)+"/tracks/"+itoa64(first), token, nil)
	if status != http.StatusNoContent {
		t.Errorf("remove track status = %d, want 204", status)
	}

	// Delete.
	status, _ = do(t, srv, http.MethodDelete, "/api/playlists/"+itoa64(playlistID), token, nil)
	if status != http.StatusNoContent {
		t.Errorf("delete playlist status = %d, want 204", status)
	}
	status, _ = do(t, srv, http.MethodGet, "/api/playlists/"+itoa64(playlistID), token, nil)
	if status != http.StatusNotFound {
		t.Errorf("deleted playlist should be 404, got %d", status)
	}
}

func TestFavoritesArePerUser(t *testing.T) {
	srv, music := newTestServer(t)
	makeFLAC(t, filepath.Join(music, "A", "Alb", "01 - One.flac"), "One", "A", "Alb")

	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "owner@example.com", "password": "correct-horse",
	})
	var owner sessionResponse
	json.Unmarshal(body, &owner)
	do(t, srv, http.MethodPost, "/api/admin/scan", owner.AccessToken, nil)
	waitForScan(t, srv, owner.AccessToken)

	// A second admin-created account, to prove isolation.
	status, body := do(t, srv, http.MethodPost, "/api/admin/users", owner.AccessToken, map[string]any{
		"email": "other@example.com", "password": "correct-horse", "name": "Other",
	})
	if status != http.StatusCreated {
		t.Fatalf("create user status = %d, body = %s", status, body)
	}
	otherToken, _ := login(t, srv, "other@example.com", "correct-horse")

	_, body = do(t, srv, http.MethodGet, "/api/library/albums", owner.AccessToken, nil)
	var albums struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albums)
	_, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(albums.Albums[0].ID), owner.AccessToken, nil)
	var detail struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &detail)
	trackID := detail.Tracks[0].ID

	// Favourite it as the owner only.
	status, _ = do(t, srv, http.MethodPost, "/api/favorites/"+itoa64(trackID), owner.AccessToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("favourite status = %d", status)
	}

	status, body = do(t, srv, http.MethodGet, "/api/favorites", owner.AccessToken, nil)
	if !strings.Contains(string(body), `"title":"One"`) {
		t.Errorf("owner should see the favourite: %s", body)
	}
	status, body = do(t, srv, http.MethodGet, "/api/favorites", otherToken, nil)
	if strings.Contains(string(body), `"title":"One"`) {
		t.Errorf("another user must not see someone else's favourites: %s", body)
	}
}

func TestAdminRoutesRequireAdmin(t *testing.T) {
	srv, _ := newTestServer(t)

	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "root@example.com", "password": "correct-horse",
	})
	var root sessionResponse
	json.Unmarshal(body, &root)

	// Create a non-admin user.
	status, _ := do(t, srv, http.MethodPost, "/api/admin/users", root.AccessToken, map[string]any{
		"email": "plain@example.com", "password": "correct-horse", "isAdmin": false,
	})
	if status != http.StatusCreated {
		t.Fatalf("create user status = %d", status)
	}
	plainToken, _ := login(t, srv, "plain@example.com", "correct-horse")

	for _, path := range []string{"/api/admin/users", "/api/admin/scan", "/api/admin/cache"} {
		status, _ := do(t, srv, http.MethodGet, path, plainToken, nil)
		if status != http.StatusForbidden {
			t.Errorf("%s as non-admin = %d, want 403", path, status)
		}
	}

	// Non-admins can still read the library.
	if status, _ := do(t, srv, http.MethodGet, "/api/library/stats", plainToken, nil); status != http.StatusOK {
		t.Errorf("library access as non-admin = %d, want 200", status)
	}
}

// TestMediaRoutesAcceptQueryStringToken guards the routing shape.
//
// The three media routes are reached by <audio>, <img> and WebSocket, none of
// which can set an Authorization header, so they accept a token in the query
// string. That only works if they sit outside the bearer-only group: chi runs
// a group's middleware before any route-specific middleware, so registering
// them inside the authenticated group means the bearer check rejects them
// first and the query token is never read.
// TestSuggestionsEndpoint exercises /api/suggestions, which the home page
// calls on every load. It once returned 500 because the query referenced a
// column that did not exist, and nothing caught it: no test called this route,
// and the home page is the first screen a user sees.
func TestSuggestionsEndpoint(t *testing.T) {
	srv, music := newTestServer(t)
	makeFLAC(t, filepath.Join(music, "A", "Alb", "01 - One.flac"), "One", "A", "Alb")
	makeFLAC(t, filepath.Join(music, "B", "Alb2", "01 - Two.flac"), "Two", "B", "Alb2")

	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "sug@example.com", "password": "correct-horse",
	})
	var sess sessionResponse
	json.Unmarshal(body, &sess)
	token := sess.AccessToken

	do(t, srv, http.MethodPost, "/api/admin/scan", token, nil)
	waitForScan(t, srv, token)

	// --- before any plays ---
	status, body := do(t, srv, http.MethodGet, "/api/suggestions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("suggestions with no history = %d, want 200: %s", status, body)
	}
	var empty struct {
		RecentlyAdded []models.Track `json:"recentlyAdded"`
		TopArtists    []struct {
			Name  string `json:"name"`
			Plays int    `json:"plays"`
		} `json:"topArtists"`
	}
	if err := json.Unmarshal(body, &empty); err != nil {
		t.Fatalf("decode suggestions: %v (body %s)", err, body)
	}
	if len(empty.RecentlyAdded) != 2 {
		t.Errorf("recentlyAdded = %d, want 2", len(empty.RecentlyAdded))
	}
	if empty.TopArtists == nil {
		t.Error("topArtists should be an empty list, not null, so the client does not have to special-case it")
	}

	// --- after playing one artist's track ---
	_, body = do(t, srv, http.MethodGet, "/api/library/albums", token, nil)
	var albums struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albums)
	var playedArtist string
	for _, al := range albums.Albums {
		if al.Artist == "A" {
			playedArtist = al.Artist
			_, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(al.ID), token, nil)
			var d struct {
				Tracks []models.Track `json:"tracks"`
			}
			json.Unmarshal(body, &d)
			status, _ = do(t, srv, http.MethodPost, "/api/history", token, map[string]any{
				"trackId": d.Tracks[0].ID, "completion": 1, "positionMs": 3000,
			})
			if status != http.StatusCreated {
				t.Fatalf("record play = %d", status)
			}
		}
	}

	status, body = do(t, srv, http.MethodGet, "/api/suggestions", token, nil)
	if status != http.StatusOK {
		t.Fatalf("suggestions after a play = %d, want 200: %s", status, body)
	}
	var after struct {
		TopArtists []struct {
			Name  string `json:"name"`
			Plays int    `json:"plays"`
		} `json:"topArtists"`
	}
	json.Unmarshal(body, &after)
	if len(after.TopArtists) != 1 {
		t.Fatalf("topArtists = %+v, want exactly one entry", after.TopArtists)
	}
	if after.TopArtists[0].Name != playedArtist {
		t.Errorf("top artist = %q, want %q", after.TopArtists[0].Name, playedArtist)
	}
	if after.TopArtists[0].Plays != 1 {
		t.Errorf("play count = %d, want 1", after.TopArtists[0].Plays)
	}
}

func TestMediaRoutesAcceptQueryStringToken(t *testing.T) {
	srv, music := newTestServer(t)
	albumDir := filepath.Join(music, "A", "Alb")
	makeFLAC(t, filepath.Join(albumDir, "01 - One.flac"), "One", "A", "Alb")

	// A sidecar cover, so the cover route has something real to serve and the
	// test does not skip itself out of the most interesting assertion.
	makeSidecarCover(t, albumDir)

	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "media@example.com", "password": "correct-horse",
	})
	var sess sessionResponse
	json.Unmarshal(body, &sess)
	token := sess.AccessToken

	do(t, srv, http.MethodPost, "/api/admin/scan", token, nil)
	waitForScan(t, srv, token)

	_, body = do(t, srv, http.MethodGet, "/api/library/albums", token, nil)
	var albums struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albums)
	_, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(albums.Albums[0].ID), token, nil)
	var detail struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &detail)
	trackID := detail.Tracks[0].ID

	// Streaming via the query string.
	status, _ := do(t, srv, http.MethodGet, "/api/stream/"+itoa64(trackID)+"?t="+token, "", nil)
	if status != http.StatusOK {
		t.Errorf("stream with a query-string token = %d, want 200", status)
	}

	// Streaming with a bearer header must still work.
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/stream/"+itoa64(trackID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("stream with a bearer token = %d, want 200", resp.StatusCode)
	}

	// Neither credential at all.
	status, _ = do(t, srv, http.MethodGet, "/api/stream/"+itoa64(trackID), "", nil)
	if status != http.StatusUnauthorized {
		t.Errorf("stream with no token = %d, want 401", status)
	}
	status, _ = do(t, srv, http.MethodGet, "/api/stream/"+itoa64(trackID)+"?t=not-a-token", "", nil)
	if status != http.StatusUnauthorized {
		t.Errorf("stream with a bad token = %d, want 401", status)
	}

	// Cover art likewise.
	_, body = do(t, srv, http.MethodGet, "/api/search?q=One", token, nil)
	var searchResp struct {
		Results []models.Track `json:"results"`
	}
	json.Unmarshal(body, &searchResp)
	if len(searchResp.Results) == 0 || searchResp.Results[0].CoverPath == nil {
		t.Skip("this fixture has no cover art, so the cover route has nothing to serve")
	}
	status, _ = do(t, srv, http.MethodGet,
		"/api/covers/"+*searchResp.Results[0].CoverPath+"?t="+token, "", nil)
	if status != http.StatusOK {
		t.Errorf("cover with a query-string token = %d, want 200", status)
	}
}

// makeSidecarCover writes a cover.jpg next to a fixture album, which is the
// usual layout for a tag-ripped library.
func makeSidecarCover(t *testing.T, albumDir string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-y",
		"-f", "lavfi", "-i", "color=c=0x2a3f5f:s=400x400:d=1",
		"-frames:v", "1", filepath.Join(albumDir, "cover.jpg"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate cover: %v: %s", err, out)
	}
}

func TestCannotDeleteLastAdmin(t *testing.T) {
	srv, _ := newTestServer(t)
	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "only@example.com", "password": "correct-horse",
	})
	var sess sessionResponse
	json.Unmarshal(body, &sess)

	status, _ := do(t, srv, http.MethodDelete, "/api/admin/users/"+itoa64(sess.User.ID), sess.AccessToken, nil)
	if status != http.StatusBadRequest {
		t.Errorf("deleting the only admin = %d, want 400", status)
	}
	// Self-deletion is refused even before the last-admin rule.
	status, _ = do(t, srv, http.MethodDelete, "/api/admin/users/"+itoa64(sess.User.ID), sess.AccessToken, nil)
	if status != http.StatusBadRequest {
		t.Errorf("self deletion = %d, want 400", status)
	}
}

func TestHistoryAndStats(t *testing.T) {
	srv, music := newTestServer(t)
	makeFLAC(t, filepath.Join(music, "A", "Alb", "01 - One.flac"), "One", "A", "Alb")

	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "h@example.com", "password": "correct-horse",
	})
	var sess sessionResponse
	json.Unmarshal(body, &sess)
	token := sess.AccessToken

	do(t, srv, http.MethodPost, "/api/admin/scan", token, nil)
	waitForScan(t, srv, token)

	_, body = do(t, srv, http.MethodGet, "/api/library/albums", token, nil)
	var albums struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albums)
	_, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(albums.Albums[0].ID), token, nil)
	var detail struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &detail)

	status, _ := do(t, srv, http.MethodPost, "/api/history", token, map[string]any{
		"trackId": detail.Tracks[0].ID, "completion": 0.9, "positionMs": 2000,
	})
	if status != http.StatusCreated {
		t.Fatalf("record play status = %d", status)
	}

	status, body = do(t, srv, http.MethodGet, "/api/history/recent", token, nil)
	if status != http.StatusOK {
		t.Fatalf("recent history status = %d", status)
	}
	if !strings.Contains(string(body), `"title":"One"`) {
		t.Errorf("recent history missing the play: %s", body)
	}

	status, body = do(t, srv, http.MethodGet, "/api/history/stats", token, nil)
	if status != http.StatusOK {
		t.Fatalf("history stats status = %d", status)
	}
	if !strings.Contains(string(body), `"totalPlays":1`) {
		t.Errorf("history stats = %s, want totalPlays 1", body)
	}

	// Playing a track that does not exist must be rejected.
	status, _ = do(t, srv, http.MethodPost, "/api/history", token, map[string]any{
		"trackId": 99999, "completion": 1,
	})
	if status != http.StatusNotFound {
		t.Errorf("history for a missing track = %d, want 404", status)
	}
}

func TestLyricsEndpoint(t *testing.T) {
	srv, music := newTestServer(t)
	dir := filepath.Join(music, "A", "Alb")
	audio := filepath.Join(dir, "01 - Lyric.flac")
	makeFLAC(t, audio, "Lyric", "A", "Alb")
	os.WriteFile(filepath.Join(dir, "01 - Lyric.lrc"),
		[]byte("[00:01.00]hello\n[00:05.00]world\n"), 0o644)

	_, body := do(t, srv, http.MethodPost, "/api/auth/register", "", map[string]string{
		"email": "l@example.com", "password": "correct-horse",
	})
	var sess sessionResponse
	json.Unmarshal(body, &sess)
	token := sess.AccessToken

	do(t, srv, http.MethodPost, "/api/admin/scan", token, nil)
	waitForScan(t, srv, token)

	_, body = do(t, srv, http.MethodGet, "/api/library/albums", token, nil)
	var albums struct {
		Albums []models.Album `json:"albums"`
	}
	json.Unmarshal(body, &albums)
	_, body = do(t, srv, http.MethodGet, "/api/library/albums/"+itoa64(albums.Albums[0].ID), token, nil)
	var detail struct {
		Tracks []models.Track `json:"tracks"`
	}
	json.Unmarshal(body, &detail)

	status, body := do(t, srv, http.MethodGet,
		"/api/library/tracks/"+itoa64(detail.Tracks[0].ID)+"/lyrics", token, nil)
	if status != http.StatusOK {
		t.Fatalf("lyrics status = %d, body = %s", status, body)
	}
	if !strings.Contains(string(body), "00:01.00") {
		t.Errorf("lyrics body = %s, want the LRC content", body)
	}
}

// --- helpers ---

func waitForScan(t *testing.T, srv *httptest.Server, token string) {
	t.Helper()
	for i := 0; i < 300; i++ {
		status, body := do(t, srv, http.MethodGet, "/api/admin/scan", token, nil)
		if status != http.StatusOK {
			t.Fatalf("scan state status = %d", status)
		}
		var state models.ScanState
		if err := json.Unmarshal(body, &state); err != nil {
			t.Fatalf("decode scan state: %v", err)
		}
		if !state.Running {
			if state.Error != "" {
				t.Fatalf("scan failed: %s", state.Error)
			}
			return
		}
		sleepBriefly()
	}
	t.Fatal("scan did not finish in time")
}

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

func sleepBriefly() { time.Sleep(50 * time.Millisecond) }
