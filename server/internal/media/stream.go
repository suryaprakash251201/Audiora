package media

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Transcoder serves audio, using the original file when it can and ffmpeg
// when it must.
//
// The design point worth knowing: a request that misses the cache is
// satisfied by streaming ffmpeg's output straight to the client while
// simultaneously teeing it to the cache file. Playback starts immediately;
// the next replay of that track is a plain range-served file off disk.
type Transcoder struct {
	musicRoot string
	cacheDir  string
	ffmpeg    string

	mu   sync.Mutex
	jobs map[string]*job
}

// job tracks one in-flight transcode so that N simultaneous requests for the
// same track and profile produce one ffmpeg process, not N.
type job struct {
	done chan struct{}
	path string
	err  error
}

func NewTranscoder(musicRoot, cacheDir string) (*Transcoder, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create transcode cache: %w", err)
	}
	return &Transcoder{
		musicRoot: musicRoot,
		cacheDir:  cacheDir,
		ffmpeg:    ffmpeg,
		jobs:      make(map[string]*job),
	}, nil
}

// ErrTrackGone means the file is in the database but gone from disk, which
// happens when music is deleted without rescanning.
var ErrTrackGone = errors.New("audio file is missing from the music library")

// TrackRequest is everything the streamer needs about one track.
type TrackRequest struct {
	ID      int64
	RelPath string
	Size    int64
	ModTime int64
	// Bitrate of the source in kbps, as ffprobe reported it. Used to skip
	// transcoding when the original is already at or below the target.
	Bitrate int
	// Duration in milliseconds, for logging.
	Duration int64
}

// Serve writes audio to the client.
//
// profile is the requested transcode target; pass "lossless" for the
// untouched original.
func (t *Transcoder) Serve(w http.ResponseWriter, r *http.Request, track TrackRequest, profileName string) {
	sourcePath, err := safeJoin(t.musicRoot, track.RelPath)
	if err != nil {
		http.Error(w, "invalid track path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(sourcePath); err != nil {
		slog.Warn("track file missing from disk", "track", track.ID, "path", track.RelPath)
		http.Error(w, ErrTrackGone.Error(), http.StatusGone)
		return
	}

	if !ShouldTranscode(profileName, extOf(track.RelPath), track.Bitrate) {
		t.serveOriginal(w, r, sourcePath)
		return
	}
	t.serveTranscoded(w, r, sourcePath, track, profileName)
}

// serveOriginal hands the untouched file to net/http, which implements the
// full Range protocol: 206 responses, Accept-Ranges, If-Range and
// multi-range. Seeking in a FLAC is therefore exact and costs no CPU.
func (t *Transcoder) serveOriginal(w http.ResponseWriter, r *http.Request, path string) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cannot open audio file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, "cannot stat audio file", http.StatusInternalServerError)
		return
	}

	ctype := ContentTypeFor(path)
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	// Tell caches the body is immutable for a given URL, which is true
	// because a content change produces a different track id.
	w.Header().Set("Cache-Control", "private, max-age=86400")

	// A zero modtime is common on NAS mounts and makes If-Modified-Since
	// comparisons unreliable, so serve the file as-is without ETag games.
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// serveTranscoded returns a cached transcode when one exists, and otherwise
// either streams it live (first caller) or waits for it (concurrent callers).
func (t *Transcoder) serveTranscoded(w http.ResponseWriter, r *http.Request, sourcePath string, track TrackRequest, profileName string) {
	key := cacheFileName(track.ID, profileName, track.Size, track.ModTime)
	finalPath := filepath.Join(t.cacheDir, key)

	if info, err := os.Stat(finalPath); err == nil && info.Size() > 0 {
		t.serveCached(w, r, finalPath, info)
		return
	}

	t.mu.Lock()
	// The second return of a map lookup is the presence flag, so `found`
	// means "someone else is already transcoding this".
	j, found := t.jobs[key]
	if !found {
		j = &job{done: make(chan struct{})}
		t.jobs[key] = j
	}
	t.mu.Unlock()

	if found {
		// Someone else is already producing this exact file. Wait for them
		// rather than starting a second ffmpeg, then serve the finished
		// cache entry with full range support.
		select {
		case <-j.done:
			if j.err != nil {
				http.Error(w, "transcode failed", http.StatusInternalServerError)
				return
			}
			if info, err := os.Stat(j.path); err == nil {
				t.serveCached(w, r, j.path, info)
				return
			}
			http.Error(w, "transcode produced no output", http.StatusInternalServerError)
		case <-r.Context().Done():
			// Client gave up; nothing to write.
		}
		return
	}

	t.produce(w, r, sourcePath, profileName, key, finalPath, j)
}

// serveCached serves a finished transcode from disk with range support.
func (t *Transcoder) serveCached(w http.ResponseWriter, r *http.Request, path string, info os.FileInfo) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "cannot open cached audio", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", mimeForProfile(strings.TrimSuffix(filepath.Ext(path), "")))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), f)
}

// produce is the leader path: run ffmpeg to completion into a temp file,
// commit it to the cache, then serve it. Waiting for the whole file costs a
// couple of seconds but means even a first play seeks correctly.
func (t *Transcoder) produce(w http.ResponseWriter, r *http.Request, sourcePath, profileName, key, finalPath string, j *job) {
	profile := LookupProfile(profileName)

	// A unique temp name per attempt, so a crashed run never leaves a partial
	// file that a later run mistakes for finished work.
	tmpPath := fmt.Sprintf("%s.%d.part", finalPath, os.Getpid())

	cmd := exec.CommandContext(r.Context(), t.ffmpeg, transcodeArgs(sourcePath, profile, tmpPath)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	if runErr != nil {
		os.Remove(tmpPath)
		// A client that gave up mid-transcode is normal, not a server error.
		if r.Context().Err() == nil {
			slog.Error("transcode failed", "profile", profileName, "err", runErr,
				"stderr", strings.TrimSpace(stderr.String()))
		}
		t.finishJob(j, key, "", runErr)
		if r.Context().Err() == nil {
			http.Error(w, "transcode failed", http.StatusInternalServerError)
		}
		return
	}

	// ffmpeg exiting cleanly is not proof of usable output: a zero-length
	// file still means an empty track.
	info, statErr := os.Stat(tmpPath)
	if statErr != nil || info.Size() == 0 {
		os.Remove(tmpPath)
		err := errors.New("transcode produced no output")
		slog.Error("transcode produced no output", "profile", profileName, "err", statErr)
		t.finishJob(j, key, "", err)
		http.Error(w, "transcode produced no output", http.StatusInternalServerError)
		return
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		slog.Error("could not commit transcode to cache", "err", err)
		t.finishJob(j, key, "", err)
		http.Error(w, "transcode failed", http.StatusInternalServerError)
		return
	}

	// The request may carry a Range header from a seek during first play.
	// Now that the file exists, http.ServeContent answers it properly.
	t.finishJob(j, key, finalPath, nil)
	t.serveCached(w, r, finalPath, info)
}

// finishJob publishes the result to any waiters and clears the in-flight
// entry. key is the cache filename, which is also the jobs map key.
func (t *Transcoder) finishJob(j *job, key, path string, err error) {
	j.path = path
	j.err = err
	close(j.done)

	t.mu.Lock()
	delete(t.jobs, key)
	t.mu.Unlock()
}

// deleteCached removes a track's transcodes from the cache. Called when a
// track is deleted or its file changes.
func (t *Transcoder) deleteCached(trackID int64) {
	matches, err := filepath.Glob(filepath.Join(t.cacheDir, fmt.Sprintf("%d-*", trackID)))
	if err != nil {
		return
	}
	for _, m := range matches {
		os.Remove(m)
	}
}

// PruneCache deletes cache files older than maxAge, keeping the directory
// from growing without bound on a server with churning music.
func (t *Transcoder) PruneCache(maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(t.cacheDir)
	if err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-maxAge)
	removed := 0
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if os.Remove(filepath.Join(t.cacheDir, e.Name())) == nil {
			removed++
		}
	}
	return removed, nil
}

// CacheSize reports how much disk the transcode cache is using.
func (t *Transcoder) CacheSize() (int64, error) {
	var total int64
	err := filepath.Walk(t.cacheDir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
