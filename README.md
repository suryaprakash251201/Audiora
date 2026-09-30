# Audiora 2.0

Self-hosted music streaming for the formats you actually own — FLAC, MP3, WAV,
M4A, AAC, OGG and Opus — with a glass interface and a companion iOS/Android app
built from the same codebase.

Point it at a folder. It reads your tags, extracts your cover art, works out a
colour from each album to theme the interface, and streams the original file
untouched unless your connection argues otherwise.

```
docker compose up -d
```

---

## What it does

**Plays your files, not a re-encode.** Lossless requests stream the original
FLAC or WAV byte for byte with full HTTP range support, so scrubbing is exact.
A 320 kbps MP3 stays a 320 kbps MP3.

**Adapts to the connection.** The client picks a profile from what the Network
Information API reports. A 40 MB FLAC is fine on wifi and ruinous on mobile
data, so phones get a small AAC transcode, which the server generates once and
caches. Uncompressed WAV is never sent over the wire in automatic mode.

**Multiple people, one library.** Everyone shares the same music. Favourites,
playlists and listening history are per person. Administrators can add people,
scan the library and clear the cache.

**Synced lyrics.** Reads LRC files sitting next to your audio, plus lyrics
embedded in FLAC, ID3 and MP4 tags, and scrolls them in time with playback. No
external service, no API key.

**Plays on one device at a time.** Start something on your phone and the laptop
pauses. The server elects a single playback controller rather than letting two
devices play out loud at once.

**Scrobbles to Last.fm.** Optional, per person, using the official desktop
scrobbler API. A play counts after four minutes or half the track, and queued
plays are retried through a database-backed queue so a Last.fm outage loses
nothing.

---

## Requirements

- Docker with Compose v2
- A domain with DNS pointing at the server, and inbound ports 80 and 443
  (Caddy needs them to issue a certificate)
- Your music, somewhere on disk

A Raspberry Pi 4 or a small VPS is enough. The Go server is a single static
binary of about 18 MB and runs comfortably in a few hundred megabytes.

---

## Getting started

**1. Configure it.**

```bash
cp .env.example .env
```

Edit `.env`. The only value you must change is `AUDIORA_SECRET`, which signs
access tokens:

```bash
openssl rand -base64 48
```

Also set `MUSIC_PATH` to wherever your music is, and `AUDIORA_DOMAIN` to the
name you will reach this server on. Set `ADMIN_EMAIL` and `ADMIN_PASSWORD` to
have the first account created for you, or leave them blank and create it from
the sign-in screen.

**2. Start it.**

```bash
docker compose up -d
docker compose logs -f
```

Caddy requests a certificate on first boot. The first request to
`https://your-domain` shows the sign-in screen.

**3. Import your music.** Sign in, go to **Settings → Library scan**, and press
**Scan now**. A scheduled rescan runs every 30 minutes by default, so files
added over SMB appear on their own.

---

## How it is put together

Three containers. No database server, no Redis, no object store.

```
                     ┌──────────────┐
   your browser ───▶ │    caddy     │  TLS, routing, access log
   your phone   ───▶ │              │
                     └──┬────────┬──┘
              /api/*    │        │  everything else
                        ▼        ▼
                  ┌──────────┐  ┌──────┐
                  │  server  │  │ web  │  Go API + audio    static SPA
                  │  :8080   │  │ :80  │
                  └────┬─────┘  └──────┘
                       │ ro
                  ┌────▼─────┐
                  │ /music   │  your files, never written to
                  └──────────┘
```

```
audiora/
├── docker-compose.yml      caddy + web + server
├── Caddyfile               TLS and routing
├── Makefile                development helpers
├── scripts/
│   ├── make-fixtures.ps1    builds a small test library
│   └── e2e.ps1             end-to-end test against a real server
├── server/                 Go 1.26, one static binary
│   ├── cmd/audiora/        entry point
│   ├── internal/
│   │   ├── config/         environment -> validated config
│   │   ├── db/             schema, migrations, FTS search
│   │   ├── auth/           argon2id passwords, JWT, sessions
│   │   ├── media/          ffprobe, cover art, transcoding
│   │   ├── scan/           filesystem walk and reconciliation
│   │   ├── api/            HTTP handlers
│   │   ├── sync/           cross-device playback state
│   │   └── scrobble/       Last.fm
│   └── Dockerfile
└── web/                    React 19 + Tailwind 4 + Capacitor 8
    ├── src/lib/            api client, player, queue, quality, lyrics, sync
    ├── src/components/     player bar, now playing, track list, queue
    ├── src/pages/          library, album, artist, search, playlists, settings
    ├── src/styles/         the design system
    ├── capacitor.config.ts
    └── Dockerfile
```

State lives in one volume: a SQLite database, extracted cover art, and the
transcode cache. Your music is mounted read-only and never copied anywhere.

---

## The parts worth knowing about

### Transcoding, and why it is not streamed live

The obvious design is to pipe ffmpeg's output straight to the client. That does
not work: **ffmpeg's MP4 muxer refuses to write to a non-seekable stream**, and
it does so regardless of which `movflags` are used, so AAC cannot be piped at
all.

So Audiora transcodes to a file first, then serves that file with full range
support. It costs about 2.7 seconds for a four-minute track at ~90x realtime,
and in exchange **seeking works on the very first play**, not just on replays.
Cached transcodes use `+faststart` so playback begins before the whole file
arrives.

Concurrent requests for the same uncached track share one ffmpeg process.

### True gapless is not achievable here

An HTML5 `<audio>` element cannot do sample-accurate gapless playback. That
needs Web Audio and `MediaSource` with frame-level scheduling. Audiora
implements a 350 ms equal-power crossfade between two audio elements, which
removes the audible click at a track boundary but is not gapless. Album replay
will have a small gap.

### Glass without melting a phone

Naive glassmorphism uses `backdrop-filter` everywhere and drops frames on
mid-range phones. Two rules keep it smooth, and both are load-bearing:

- At most two or three elements on screen may use `backdrop-filter` at once.
  The large ambient backdrops are CSS radial gradients derived from each
  album's dominant colour instead, which looks almost identical and costs
  nothing per frame.
- The full-screen Now Playing backdrop is a pre-scaled, pre-blurred copy of
  the artwork rather than a filter over it, so the compositor never re-blurs
  during animation.

All colour is in OKLCH so gradients between two brand colours stay perceptually
even, and the artwork-derived accent is converted from sRGB to OKLCH at runtime.

### The scanner

Walks `MUSIC_PATH`, probes each file with ffprobe, and reconciles with the
database:

- Skips files whose size and mtime are unchanged, so a rescan of a large
  library is fast.
- Extracts embedded cover art, or prefers a sidecar `cover.jpg` when there is
  one. Everything is normalised to JPEG, downscaled to 1000px.
- Samples a 4x4 grid of the artwork and picks the most *saturated* pixel rather
  than averaging, because averaging album art reliably produces mud.
- Falls back to the file path for missing tags, and infers the track number
  from a leading `03` in the filename.
- Attaches lyrics from a sidecar `.lrc` or `.txt`, or from embedded tags.
- **Removes** tracks whose files have disappeared, so a scan is accurate rather
  than merely additive.
- Skips hidden directories and `@eaDir`.

### Security notes

- Passwords are argon2id. Access tokens are short-lived JWTs; refresh tokens are
  opaque, stored hashed, and rotated on every use. Replaying a spent refresh
  token revokes every session for that user, because it indicates theft.
- Streaming, cover art and the sync socket accept a token in the query string,
  because `<audio>`, `<img>` and `WebSocket` cannot set an `Authorization`
  header. These are the only three routes that allow it, and Caddy is
  configured not to log query strings.
- A login for an unknown address performs a dummy hash comparison, so response
  timing does not reveal which emails have accounts.

---

## Development

Docker is for running Audiora. These targets are for working on the code.

```bash
make install      # install frontend dependencies
make help         # list every target
```

**Frontend with hot reload.** Run the Go server and Vite side by side; Vite
proxies the API so the browser sees one origin and CORS never appears.

```bash
cd server && go run ./cmd/audiora     # terminal 1
make dev                              # terminal 2
```

**Tests.**

```bash
make test          # Go suite + frontend suite
make check         # vet, typecheck, everything
```

The Go suite covers range-request handling against real generated audio,
transcode caching, the scanner, the schema and the whole API including
authentication and authorisation. The frontend suite covers the pure logic where
bugs hide: the LRC parser, the queue reducer and the quality picker.

**End-to-end.** Builds a small library and exercises a real running server.

```bash
powershell -File scripts/make-fixtures.ps1
powershell -File scripts/e2e.ps1
```

---

## Mobile apps

The same React build is wrapped for iOS and Android with Capacitor, so the
interface is identical and there is one codebase to maintain.

```bash
make cap-add        # once, creates android/ and ios/
make cap-android    # build, sync, open in Android Studio
make cap-ios        # build, sync, open in Xcode
```

Lock-screen and notification controls go through
`@capgo/capacitor-media-session`, which drives Android's MediaSession and iOS's
`MPNowPlayingInfoCenter`. In the browser the standard Web MediaSession API is
used instead.

Set the API address at build time so the app knows where your server is:

```bash
cd web && VITE_API_BASE="https://music.example.com" npm run build
```

**Note on background audio.** The app plays through an HTML5 audio element
inside the WebView, so the plugin relays media-button presses back into
JavaScript. That works while the app is in the foreground and while iOS permits
the WebView to keep running, but it is not as robust as a native audio
pipeline: aggressive backgrounding can interrupt playback, and closing the app
stops it. Genuine uninterrupted background playback would need a native
audio engine, which is a much larger undertaking than a web wrapper.

---

## Configuration

Everything is environment variables; see `.env.example` for the full annotated
list. The ones worth knowing:

| Variable | Default | What it does |
| --- | --- | --- |
| `AUDIORA_SECRET` | — | **Required.** Signs access tokens. Changing it logs everyone out. |
| `AUDIORA_DOMAIN` | — | **Required.** The name Caddy issues a certificate for. |
| `MUSIC_PATH` | `/srv/music` | Where your music is. Mounted read-only. |
| `DATA_PATH` | `/srv/audiora-data` | Database, cover art, transcode cache. |
| `ACCESS_TOKEN_TTL_MIN` | `15` | Access token lifetime. |
| `SCAN_INTERVAL_MIN` | `30` | Rescan interval. `0` disables it. |
| `EXTRACT_COVER_ART` | `true` | Extract artwork during scans. |
| `EXTRACT_COLORS` | `true` | Compute the per-album accent colour. |
| `PREWARM_TRANSCODE` | `false` | Transcode the whole library after a scan. Uses disk and CPU. |
| `CORS_ORIGINS` | the site's own origin | Add others for a separate native build. |

---

## Operating notes

**Backups.** Everything worth keeping is in the data volume. The music is
untouched, so you only need the database and cover art:

```bash
docker run --rm -v audiora_audiora_data:/data -v "$PWD:/backup" alpine \
  tar czf /backup/audiora-backup.tar.gz -C /data .
```

**Disk.** The transcode cache grows with the library. Audiora prunes entries
older than 30 days automatically, and **Settings → Clear** removes anything
unused for a week.

**Upgrading.** `git pull && docker compose up -d --build`. Migrations apply
themselves on startup.

**Deleting everything.** `make nuke` removes the database, cover art and cache.
Your music is never touched.

---

## Licence

Released under the MIT licence. Your music remains yours; Audiora never uploads
it anywhere.
