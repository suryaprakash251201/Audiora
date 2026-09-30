-- Audiora 2.0 initial schema.
--
-- The library (artists, albums, tracks) is global and shared by every user.
-- Everything that differs per person (favorites, playlists, play history,
-- Last.fm credentials) is keyed by user_id.

CREATE TABLE users (
    id            INTEGER PRIMARY KEY,
    email         TEXT    NOT NULL UNIQUE,
    password_hash TEXT    NOT NULL,
    name          TEXT    NOT NULL DEFAULT '',
    is_admin      INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL,
    last_login_at INTEGER
);

CREATE TABLE refresh_tokens (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT    NOT NULL UNIQUE,
    user_agent TEXT    NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);

CREATE TABLE artists (
    id         INTEGER PRIMARY KEY,
    -- Lower-cased name; the unique key that merges "Bach" and "bach" into one artist.
    name_key   TEXT    NOT NULL UNIQUE,
    name       TEXT    NOT NULL,
    sort_name  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_artists_sort ON artists(sort_name);

CREATE TABLE albums (
    id             INTEGER PRIMARY KEY,
    artist_id      INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    -- Album key is lower-cased "<artist>\x00<title>" so that two albums with
    -- the same title by different artists never merge.
    album_key      TEXT    NOT NULL UNIQUE,
    title          TEXT    NOT NULL,
    year           INTEGER,
    -- Cover art on disk, relative to the covers directory. NULL when the
    -- file had no embedded art and no sidecar image was found.
    cover_path     TEXT,
    -- Dominant colour as #rrggbb, used to theme the UI.
    dominant_color TEXT,
    added_at       INTEGER NOT NULL
);
CREATE INDEX idx_albums_artist ON albums(artist_id);
CREATE INDEX idx_albums_title  ON albums(title);

CREATE TABLE tracks (
    id          INTEGER PRIMARY KEY,
    -- Path relative to MUSIC_PATH. The library is mounted read-only, so
    -- absolute paths are not stable between hosts and are never stored.
    path        TEXT    NOT NULL UNIQUE,
    file_size   INTEGER NOT NULL,
    mod_time    INTEGER NOT NULL,
    format      TEXT    NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    bitrate     INTEGER NOT NULL DEFAULT 0,
    sample_rate INTEGER NOT NULL DEFAULT 0,
    channels    INTEGER NOT NULL DEFAULT 0,
    title       TEXT    NOT NULL,
    track_no    INTEGER,
    disc_no     INTEGER NOT NULL DEFAULT 1,
    year        INTEGER,
    artist_id   INTEGER NOT NULL REFERENCES artists(id) ON DELETE CASCADE,
    album_id    INTEGER NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
    has_lyrics  INTEGER NOT NULL DEFAULT 0,
    added_at    INTEGER NOT NULL
);
CREATE INDEX idx_tracks_album  ON tracks(album_id, disc_no, track_no);
CREATE INDEX idx_tracks_artist ON tracks(artist_id, album_id, track_no);
CREATE INDEX idx_tracks_title  ON tracks(title);

CREATE TABLE track_lyrics (
    track_id INTEGER PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE,
    -- Raw text, either an LRC document or plain text. Parsing happens on the client.
    raw      TEXT NOT NULL,
    source   TEXT NOT NULL
);

CREATE TABLE genres (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE track_genres (
    track_id INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genres(id) ON DELETE CASCADE,
    PRIMARY KEY (track_id, genre_id)
);

CREATE TABLE playlists (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    description TEXT    NOT NULL DEFAULT '',
    -- Track whose art represents the playlist in the grid.
    cover_track_id INTEGER REFERENCES tracks(id) ON DELETE SET NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
);
CREATE INDEX idx_playlists_user ON playlists(user_id, updated_at DESC);

CREATE TABLE playlist_tracks (
    playlist_id INTEGER NOT NULL REFERENCES playlists(id) ON DELETE CASCADE,
    track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    added_at    INTEGER NOT NULL,
    PRIMARY KEY (playlist_id, track_id)
);
CREATE INDEX idx_playlist_tracks_pos ON playlist_tracks(playlist_id, position);

CREATE TABLE favorites (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id   INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, track_id)
);

CREATE TABLE play_history (
    id         INTEGER PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id   INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at  INTEGER NOT NULL,
    -- Fraction of the track actually heard, 0.0-1.0. Drives the
    -- "listened to 30 minutes this week" style stats.
    completion  REAL    NOT NULL DEFAULT 0,
    -- Set once the play has been accepted by Last.fm (or marked as skipped
    -- there), so the UI can show what is still pending.
    scrobbled  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_play_history_user_time ON play_history(user_id, played_at DESC);
CREATE INDEX idx_play_history_track     ON play_history(track_id);

-- Per-user key/value settings, e.g. the Last.fm API credentials.
CREATE TABLE user_settings (
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT    NOT NULL,
    value      TEXT    NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, key)
);

-- Tracks waiting to be pushed to Last.fm. A background worker drains this,
-- so a network failure never loses a play.
CREATE TABLE scrobble_queue (
    id          INTEGER PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    track_id    INTEGER NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
    played_at   INTEGER NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    last_error  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_scrobble_queue_user ON scrobble_queue(user_id, played_at);

-- One row per user: whichever device currently owns playback, plus the
-- authoritative playback state that every other device mirrors.
CREATE TABLE sync_state (
    user_id     INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    -- device_id of the playback controller, empty when nobody is playing.
    controller  TEXT    NOT NULL DEFAULT '',
    -- Monotonic counter. Clients ignore any update with a seq they have seen.
    seq         INTEGER NOT NULL DEFAULT 0,
    state_json  TEXT    NOT NULL DEFAULT '{}',
    updated_at  INTEGER NOT NULL DEFAULT 0
);
