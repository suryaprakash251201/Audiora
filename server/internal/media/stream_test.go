package media

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// makeTestAudio generates a real audio file with ffmpeg so the streaming
// tests exercise actual decoders and real bytes rather than a stub.
func makeTestAudio(t *testing.T, dir, name string, codecArgs ...string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available; skipping audio generation")
	}
	path := filepath.Join(dir, name)

	// A 3-second 440Hz tone, encoded losslessly so bitrate is predictable.
	args := []string{
		"-v", "error", "-y",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
	}
	if len(codecArgs) == 0 {
		codecArgs = []string{"-c:a", "flac"}
	}
	args = append(args, codecArgs...)
	args = append(args, path)

	cmd := exec.Command("ffmpeg", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate %s: %v: %s", name, err, stderr.String())
	}
	return path
}

func newTestTranscoder(t *testing.T) (*Transcoder, string) {
	t.Helper()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	root := t.TempDir()
	cache := t.TempDir()
	tr, err := NewTranscoder(root, cache)
	if err != nil {
		t.Fatalf("new transcoder: %v", err)
	}
	return tr, root
}

func trackFor(t *testing.T, root, relPath string) TrackRequest {
	t.Helper()
	info, err := os.Stat(filepath.Join(root, relPath))
	if err != nil {
		t.Fatalf("stat %s: %v", relPath, err)
	}
	return TrackRequest{
		ID:      1,
		RelPath: relPath,
		Size:    info.Size(),
		ModTime: info.ModTime().Unix(),
		Bitrate: 900,
	}
}

func TestPassthroughSupportsRangeRequests(t *testing.T) {
	tr, root := newTestTranscoder(t)
	makeTestAudio(t, root, "tone.flac")
	track := trackFor(t, root, "tone.flac")

	// A player needs three behaviours to scrub a lossless file: a plain GET,
	// a 206 for a byte range, and a correct Accept-Ranges advertisement.
	t.Run("full body", func(t *testing.T) {
		rec := httptest.NewRecorder()
		tr.Serve(rec, httptest.NewRequest(http.MethodGet, "/stream/1", nil), track, LosslessProfileName)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Accept-Ranges"); got != "bytes" {
			t.Errorf("Accept-Ranges = %q, want %q", got, "bytes")
		}
		if got := rec.Header().Get("Content-Type"); got != "audio/flac" {
			t.Errorf("Content-Type = %q, want audio/flac", got)
		}

		original, err := os.ReadFile(filepath.Join(root, "tone.flac"))
		if err != nil {
			t.Fatal(err)
		}
		if rec.Body.Len() != len(original) {
			t.Errorf("body length = %d, want %d (the whole file)", rec.Body.Len(), len(original))
		}
		if !bytes.Equal(rec.Body.Bytes(), original) {
			t.Error("body does not match the file on disk")
		}
	})

	t.Run("partial content", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/stream/1", nil)
		req.Header.Set("Range", "bytes=100-199")
		tr.Serve(rec, req, track, LosslessProfileName)

		if rec.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", rec.Code)
		}
		if rec.Body.Len() != 100 {
			t.Errorf("body length = %d, want 100", rec.Body.Len())
		}

		original, err := os.ReadFile(filepath.Join(root, "tone.flac"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rec.Body.Bytes(), original[100:200]) {
			t.Error("returned range does not match the corresponding bytes of the file")
		}

		cr := rec.Header().Get("Content-Range")
		if cr != "bytes 100-199/"+strconv.Itoa(len(original)) {
			t.Errorf("Content-Range = %q, want bytes 100-199/%d", cr, len(original))
		}
	})

	t.Run("open ended range", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/stream/1", nil)
		req.Header.Set("Range", "bytes=50-")
		tr.Serve(rec, req, track, LosslessProfileName)

		if rec.Code != http.StatusPartialContent {
			t.Fatalf("status = %d, want 206", rec.Code)
		}

		original, err := os.ReadFile(filepath.Join(root, "tone.flac"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(rec.Body.Bytes(), original[50:]) {
			t.Error("open-ended range did not return the tail of the file")
		}
	})
}

func TestTranscodeProducesPlayableFileAndCachesIt(t *testing.T) {
	tr, root := newTestTranscoder(t)
	makeTestAudio(t, root, "tone.flac")
	track := trackFor(t, root, "tone.flac")

	rec := httptest.NewRecorder()
	tr.Serve(rec, httptest.NewRequest(http.MethodGet, "/stream/1", nil), track, "aac96")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatal("transcoded body was empty")
	}
	if got := rec.Header().Get("Content-Type"); got != "audio/mp4" {
		t.Errorf("Content-Type = %q, want audio/mp4", got)
	}

	// The transcode must be valid audio, not merely non-empty bytes.
	cachePath := filepath.Join(tr.cacheDir, cacheFileName(track.ID, "aac96", track.Size, track.ModTime))
	probe, err := ProbeFile(context.Background(), cachePath)
	if err != nil {
		t.Fatalf("cached transcode is not readable by ffprobe: %v", err)
	}
	if probe.DurationMS < 2500 || probe.DurationMS > 3500 {
		t.Errorf("transcoded duration = %dms, want roughly 3000ms", probe.DurationMS)
	}
	if probe.Codec != "aac" {
		t.Errorf("transcoded codec = %q, want aac", probe.Codec)
	}

	// +faststart is what lets a client begin playing the cached file without
	// downloading it whole, so the moov atom must precede the media data.
	if !hasBoxBeforeMdat(cachePath, "moov") {
		t.Error("moov atom is not at the front of the file; faststart did not apply")
	}

	// A second request must be served from the cache. It gains full range
	// support and runs no ffmpeg.
	rec2 := httptest.NewRecorder()
	tr.Serve(rec2, httptest.NewRequest(http.MethodGet, "/stream/1", nil), track, "aac96")
	if got := rec2.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("cached response Accept-Ranges = %q, want bytes", got)
	}
	if rec2.Body.Len() != rec.Body.Len() {
		t.Errorf("cached body length = %d, want %d", rec2.Body.Len(), rec.Body.Len())
	}
}

// hasBoxBeforeMdat reports whether a named top-level MP4 box appears before
// the first "mdat" box, which is the signature of a faststart file.
func hasBoxBeforeMdat(path, box string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(data) < 16 {
		return false
	}
	for off := 0; off+8 <= len(data); {
		size := int(data[off])<<24 | int(data[off+1])<<16 | int(data[off+2])<<8 | int(data[off+3])
		name := string(data[off+4 : off+8])
		if name == "mdat" {
			return false
		}
		if name == box {
			return true
		}
		if size < 8 {
			return false
		}
		off += size
	}
	return false
}

func TestSeekWorksOnFirstPlay(t *testing.T) {
	tr, root := newTestTranscoder(t)
	makeTestAudio(t, root, "tone.flac")
	track := trackFor(t, root, "tone.flac")

	// A seek that lands on a cold cache. The server transcodes the whole
	// track and then answers the range, so the player gets a 206 rather than
	// an unseekable stream.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/stream/1", nil)
	req.Header.Set("Range", "bytes=100-199")
	tr.Serve(rec, req, track, "aac96")

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 for a seek into an uncached track", rec.Code)
	}
	if rec.Body.Len() != 100 {
		t.Errorf("body length = %d, want 100", rec.Body.Len())
	}
}

func TestCachedTranscodeSupportsRangeRequests(t *testing.T) {
	tr, root := newTestTranscoder(t)
	makeTestAudio(t, root, "tone.flac")
	track := trackFor(t, root, "tone.flac")

	// Populate the cache.
	tr.Serve(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/s/1", nil), track, "aac96")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/s/1", nil)
	req.Header.Set("Range", "bytes=10-19")
	tr.Serve(rec, req, track, "aac96")

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 on a cached transcode", rec.Code)
	}
	if rec.Body.Len() != 10 {
		t.Errorf("body length = %d, want 10", rec.Body.Len())
	}
}

func TestConcurrentRequestsShareOneTranscode(t *testing.T) {
	tr, root := newTestTranscoder(t)
	makeTestAudio(t, root, "tone.flac")
	track := trackFor(t, root, "tone.flac")

	// Two simultaneous misses must not race into a half-written cache file
	// or produce two ffmpeg processes fighting over the same temp path.
	const n = 4
	done := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			rec := httptest.NewRecorder()
			tr.Serve(rec, httptest.NewRequest(http.MethodGet, "/s/1", nil), track, "aac96")
			done <- rec.Body.Len()
		}()
	}

	var first int
	for i := 0; i < n; i++ {
		size := <-done
		if size == 0 {
			t.Fatal("a concurrent request received an empty body")
		}
		if first == 0 {
			first = size
		}
	}
}

func TestMissingFileReturnsGone(t *testing.T) {
	tr, _ := newTestTranscoder(t)
	// Recorded in the database but never actually written to disk.
	track := TrackRequest{ID: 9, RelPath: "ghost.flac", Size: 100, ModTime: 1}

	rec := httptest.NewRecorder()
	tr.Serve(rec, httptest.NewRequest(http.MethodGet, "/s/9", nil), track, LosslessProfileName)

	if rec.Code != http.StatusGone {
		t.Errorf("status = %d, want 410 for a file missing from disk", rec.Code)
	}
}

func TestPathTraversalIsRejected(t *testing.T) {
	tr, _ := newTestTranscoder(t)
	// These would all escape the music root if safeJoin did its job wrong.
	for _, rel := range []string{
		"../../../../etc/passwd",
		"..\\..\\windows\\system32\\config\\sam",
		"/etc/passwd",
	} {
		t.Run(rel, func(t *testing.T) {
			track := TrackRequest{ID: 1, RelPath: rel, Size: 1, ModTime: 1}
			rec := httptest.NewRecorder()
			tr.Serve(rec, httptest.NewRequest(http.MethodGet, "/s/1", nil), track, LosslessProfileName)

			if rec.Code == http.StatusOK {
				t.Errorf("traversal %q was served with 200; body: %q", rel, rec.Body.String())
			}
		})
	}
}
