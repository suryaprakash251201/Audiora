package media

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Profile is a named transcode target. The client picks one; the server
// decides whether a given request can be satisfied by the original file.
type Profile struct {
	Name      string
	Extension string
	MimeType  string
	// Muxer is the ffmpeg container name, which is not the same as the file
	// extension: AAC in MP4 is written by the "ipod" muxer, not "mp4".
	Muxer string
	// Args are the ffmpeg output flags. Input flags are added by the server.
	Args []string
	// EstimatedKbps drives the "will this fit on my data plan" check and the
	// progress estimate in the admin UI.
	EstimatedKbps int
}

// Profiles are the transcode targets offered to clients.
//
// There is no Opus-in-MP4 profile on purpose: Safari, and therefore iOS and
// the Capacitor shell, cannot decode it. Aac96 is the safe default that works
// everywhere, and Opus64 is the better-quality-per-byte option on Chromium
// and Firefox.
var Profiles = map[string]*Profile{
	"aac96": {
		Name:          "aac96",
		Extension:     ".m4a",
		MimeType:      "audio/mp4",
		Muxer:         "ipod",
		EstimatedKbps: 96,
		Args: []string{
			"-c:a", "aac", "-b:a", "96k",
			"-movflags", "+faststart",
		},
	},
	"opus64": {
		Name:          "opus64",
		Extension:     ".ogg",
		MimeType:      "audio/ogg",
		Muxer:         "opus",
		EstimatedKbps: 64,
		Args: []string{
			"-c:a", "libopus", "-b:a", "64k",
			"-vbr", "on", "-compression_level", "10",
		},
	},
	"aac320": {
		Name:          "aac320",
		Extension:     ".m4a",
		MimeType:      "audio/mp4",
		Muxer:         "ipod",
		EstimatedKbps: 320,
		Args: []string{
			"-c:a", "aac", "-b:a", "320k",
			"-movflags", "+faststart",
		},
	},
}

// LosslessProfileName is the sentinel clients pass to request the untouched
// original file.
const LosslessProfileName = "lossless"

// LookupProfile resolves a requested profile name. Unknown names fall back to
// aac96 rather than failing, because a client asking for something we do not
// have should still get music.
func LookupProfile(name string) *Profile {
	if name == "" {
		return Profiles["aac96"]
	}
	if p, ok := Profiles[strings.ToLower(name)]; ok {
		return p
	}
	return Profiles["aac96"]
}

// ShouldTranscode decides whether a request for profile p against a source
// file actually needs ffmpeg, or whether the original will do.
//
// Two cases short-circuit to passthrough:
//   - the client asked for lossless
//   - the source is already at or below the profile's bitrate, in which case
//     re-encoding would be a pure quality loss for no size benefit
func ShouldTranscode(profileName, sourceExt string, sourceBitrateKbps int) bool {
	if strings.EqualFold(profileName, LosslessProfileName) {
		return false
	}
	p := LookupProfile(profileName)
	if sourceBitrateKbps > 0 && sourceBitrateKbps <= p.EstimatedKbps+16 {
		return false
	}
	// A PCM WAV with no bitrate estimate would otherwise always transcode,
	// which is correct: a WAV is almost always larger than any profile.
	return true
}

// transcodeArgs builds the ffmpeg command line for one file, writing the
// result to destPath.
//
// The output is always a real file rather than a pipe. That is a deliberate
// constraint, not a shortcut: ffmpeg's MP4 muxer refuses to write to a
// non-seekable stream ("muxer does not support non seekable output"), so a
// pipe is impossible for the AAC profile no matter which movflags are used.
// Since a pipe is off the table, writing the whole file first costs only a
// couple of seconds at ~90x realtime and buys correct range seeking on first
// play rather than just on replays.
func transcodeArgs(inputPath string, p *Profile, destPath string) []string {
	args := []string{"-v", "error", "-nostdin", "-y", "-i", inputPath}
	// -vn drops any cover art; the UI already serves it separately and a
	// video stream in an audio file confuses some decoders.
	args = append(args, "-vn", "-map", "0:a:0")
	args = append(args, p.Args...)
	args = append(args, "-f", p.Muxer, destPath)
	return args
}

// cacheFileName is the on-disk name for a transcode of one track at one
// profile. The source size and modification time are folded in, so replacing
// a file in the music library produces a different cache entry instead of
// serving the old audio.
func cacheFileName(trackID int64, profileName string, sourceSize int64, sourceModTime int64) string {
	return fmt.Sprintf("%d-%s-%d-%d%s", trackID, profileName, sourceSize, sourceModTime, profileExtension(profileName))
}

func profileExtension(profileName string) string {
	if p, ok := Profiles[strings.ToLower(profileName)]; ok {
		return p.Extension
	}
	return ".m4a"
}

func mimeForProfile(profileName string) string {
	if p, ok := Profiles[strings.ToLower(profileName)]; ok {
		return p.MimeType
	}
	return "audio/mp4"
}

// safeJoin resolves a library-relative path inside root and refuses anything
// that escapes it. The scanner only ever writes relative paths, but this is
// the boundary that keeps a crafted request from reading /etc/passwd.
func safeJoin(root, relative string) (string, error) {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve music root: %w", err)
	}
	// Slash-separated regardless of host OS, since the paths come from the
	// database as stored by the scanner.
	rel := filepath.FromSlash(relative)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	full := filepath.Join(cleanRoot, rel)

	// filepath.Join already cleans the result, so a ".." prefix would have
	// been resolved. Re-check the relationship to be certain.
	inside, err := filepath.Rel(cleanRoot, full)
	if err != nil || strings.HasPrefix(inside, "..") || filepath.IsAbs(inside) {
		return "", fmt.Errorf("path escapes music root")
	}
	return full, nil
}
