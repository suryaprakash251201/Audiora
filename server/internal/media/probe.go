// Package media reads technical metadata out of audio files using ffprobe and
// ffmpeg. Doing this with the external tools rather than a Go tag parser is
// deliberate: those parsers each handle a subset of formats, whereas ffprobe
// understands MP3, FLAC, WAV, M4A, OGG and Opus uniformly, including the
// awkward cases (ALAC in an M4A, VBR MP3 duration, embedded cover art that
// is PNG in a FLAC and JPEG in an ID3 tag).
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Probe is the subset of ffprobe output the scanner cares about.
type Probe struct {
	Format     string // container extension, lower case, e.g. "flac"
	Codec      string // audio codec, e.g. "flac", "mp3", "aac", "alac", "pcm_s16le"
	DurationMS int64
	Bitrate    int // kbps
	SampleRate int
	Channels   int
	SizeBytes  int64

	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	TrackNo     int
	TrackTotal  int
	DiscNo      int
	DiscTotal   int
	Year        int
	Genre       string
	// Lyrics is the embedded lyric payload if the file carries one.
	Lyrics string

	// HasCover reports whether any attached picture stream exists.
	HasCover bool
}

// ffprobeJSON mirrors the parts of ffprobe's output we read. Field tags match
// ffprobe's own naming so the mapping stays obvious.
type ffprobeJSON struct {
	Streams []struct {
		CodecType   string            `json:"codec_type"`
		CodecName   string            `json:"codec_name"`
		SampleRate  string            `json:"sample_rate"`
		Channels    int               `json:"channels"`
		Duration    string            `json:"duration"`
		BitRate     string            `json:"bit_rate"`
		Disposition map[string]int    `json:"disposition"`
		Tags        map[string]string `json:"tags"`
	} `json:"streams"`
	Format struct {
		Filename string            `json:"filename"`
		Format   string            `json:"format_name"`
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Size     string            `json:"size"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// ErrUnsupported marks a file ffprobe could not make sense of. The scanner
// skips these rather than aborting, so one corrupt file in a large library
// does not stop the import.
var ErrUnsupported = errors.New("unsupported or unreadable audio file")

// probeTimeout stops a pathological file from hanging a scan forever.
const probeTimeout = 30 * time.Second

// ProbeFile runs ffprobe against a file and normalises the result.
func ProbeFile(ctx context.Context, path string) (*Probe, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// A file being copied into the library right now can fail transiently.
		// Report it as unsupported so the caller skips and retries next scan.
		return nil, fmt.Errorf("%w: ffprobe: %v: %s", ErrUnsupported, err, strings.TrimSpace(stderr.String()))
	}

	var raw ffprobeJSON
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return nil, fmt.Errorf("%w: decode ffprobe output: %v", ErrUnsupported, err)
	}
	return normaliseProbe(&raw)
}

func normaliseProbe(raw *ffprobeJSON) (*Probe, error) {
	p := &Probe{DiscNo: 1, DiscTotal: 1}

	p.SizeBytes = parseInt64(raw.Format.Size)
	if b := parseInt(raw.Format.BitRate); b > 0 {
		// ffprobe reports the overall bitrate; for a single audio stream
		// that is close enough to drive the "is this a huge file" decision.
		p.Bitrate = b / 1000
	}
	if d := parseFloat(raw.Format.Duration); d > 0 {
		p.DurationMS = int64(d * 1000)
	}

	// Format name looks like "mp3", "flac", "wav", "mov,mp4,m4a,3gp,3g2,mj2".
	// The first comma-separated entry is the primary one.
	if raw.Format.Format != "" {
		p.Format = strings.ToLower(strings.Split(raw.Format.Format, ",")[0])
	}

	formatTags := lowercaseKeys(raw.Format.Tags)

	for _, s := range raw.Streams {
		if s.CodecType == "video" && s.Disposition["attached_pic"] == 1 {
			p.HasCover = true
		}
		if s.CodecType != "audio" {
			continue
		}
		p.Codec = s.CodecName
		p.SampleRate = parseInt(s.SampleRate)
		p.Channels = s.Channels
		// Prefer the stream's own duration: for some containers the format
		// duration includes padding or a video track.
		if d := parseFloat(s.Duration); d > 0 {
			p.DurationMS = int64(d * 1000)
		}
		if b := parseInt(s.BitRate); b > 0 && p.Bitrate == 0 {
			p.Bitrate = b / 1000
		}
		// Stream-level tags win over format-level ones, since a VBR MP3 puts
		// the real Xing/Info duration in the stream.
		applyTags(p, lowercaseKeys(s.Tags))
	}
	applyTags(p, formatTags)

	if p.DurationMS <= 0 {
		return nil, fmt.Errorf("%w: no positive duration reported", ErrUnsupported)
	}
	return p, nil
}

// applyTags copies tag values onto the probe, ignoring blanks so that a
// format-level tag never wipes out a stream-level one.
func applyTags(p *Probe, tags map[string]string) {
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(tags[k]); v != "" {
				return v
			}
		}
		return ""
	}

	if v := first("title"); v != "" {
		p.Title = v
	}
	if v := first("artist", "trackartist", "performer"); v != "" {
		p.Artist = v
	}
	if v := first("album_artist", "albumartist", "album artist", "ensemble"); v != "" {
		p.AlbumArtist = v
	}
	if v := first("album", "album_title", "wm/albumtitle"); v != "" {
		p.Album = v
	}
	if v := first("lyrics", "unsyncedlyrics", "lyrics-eng", "unsyncedlyrics-eng", "©lyr", "wm/lyrics"); v != "" {
		p.Lyrics = v
	}
	if v := first("genre", "wm/genre"); v != "" {
		p.Genre = v
	}
	if v := parseInt(first("date", "year", "originaldate", "wm/year")); v > 0 {
		p.Year = v
	}
	// Track number is often "3" or "3/12".
	if raw := first("track", "tracknumber", "tracktotal", "wm/tracknumber"); raw != "" {
		num, total := splitFraction(raw)
		if num > 0 {
			p.TrackNo = num
		}
		if total > 0 {
			p.TrackTotal = total
		}
	}
	if raw := first("disc", "discnumber", "disctotal", "wm/partofset"); raw != "" {
		num, total := splitFraction(raw)
		if num > 0 {
			p.DiscNo = num
		}
		if total > 0 {
			p.DiscTotal = total
		}
	}
}

// splitFraction parses "3/12" or "3" into its numerator and denominator.
// A track number like "A1" is also accepted, since that is what a lot of
// classical releases use.
func splitFraction(raw string) (num, total int) {
	raw = strings.TrimSpace(raw)
	// Strip any leading disc/track letter prefix: "A1" -> "1".
	if len(raw) > 1 && (raw[0] < '0' || raw[0] > '9') {
		if i := strings.IndexFunc(raw, func(r rune) bool { return r >= '0' && r <= '9' }); i >= 0 {
			raw = raw[i:]
		}
	}

	parts := strings.SplitN(raw, "/", 2)
	num = parseInt(parts[0])
	if len(parts) == 2 {
		total = parseInt(parts[1])
	}
	return num, total
}

func lowercaseKeys(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}

func parseInt(s string) int {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return v
}

func parseInt64(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}
