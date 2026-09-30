package media

import (
	"strings"
)

// Audio formats Audiora knows how to serve. Anything outside this set is
// ignored by the scanner, which keeps `.cue`, `.log`, `.jpg` and stray
// playlist files out of the library.
var supportedExtensions = map[string]string{
	".mp3":  "audio/mpeg",
	".flac": "audio/flac",
	".wav":  "audio/wav",
	".m4a":  "audio/mp4",
	".mp4":  "audio/mp4",
	".aac":  "audio/aac",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".opus": "audio/ogg",
	".mka":  "audio/x-matroska",
}

// IsSupported reports whether a filename looks like playable audio we handle.
func IsSupported(filename string) bool {
	_, ok := supportedExtensions[strings.ToLower(extOf(filename))]
	return ok
}

// ContentTypeFor maps a filename to the MIME type used when serving the
// original file. Returns "" for extensions we do not recognise.
func ContentTypeFor(filename string) string {
	return supportedExtensions[strings.ToLower(extOf(filename))]
}

// IsLossless reports whether a file is already in a compressed-lossless or
// uncompressed format. These are the files worth transcoding away from when a
// client is on a metered connection: a 40MB FLAC and a 90MB WAV carry
// roughly the same audio.
func IsLossless(ext string) bool {
	switch ext {
	case ".flac", ".wav", ".mka":
		return true
	}
	return false
}

func extOf(filename string) string {
	idx := strings.LastIndex(filename, ".")
	if idx < 0 {
		return ""
	}
	return filename[idx:]
}
