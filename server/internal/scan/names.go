package scan

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/audiora/audiora/server/internal/media"
)

// resolveNames fills in missing tags from the file's location on disk, which
// is how a lot of libraries are organised: Artist/Album/01 - Track.flac.
func resolveNames(probe *media.Probe, relPath string) (title, artist, album string) {
	title = strings.TrimSpace(probe.Title)
	artist = strings.TrimSpace(probe.Artist)
	album = strings.TrimSpace(probe.Album)

	dir := filepath.Base(filepath.Dir(relPath))
	parent := filepath.Base(filepath.Dir(filepath.Dir(relPath)))
	file := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))

	// A directory named "CD1" or "Disc 2" is a disc marker, not an album, so
	// it must not be promoted to the album name when the tag is missing.
	if isDiscDir(dir) {
		dir = ""
	}
	if isNoiseDir(parent) {
		parent = ""
	}

	if album == "" {
		album = firstNonEmpty(dir, parent)
	}
	if artist == "" {
		artist = firstNonEmpty(parent, dir)
	}
	if title == "" {
		// "03 - Title" or "03. Title" -> "Title"
		title = stripTrackNumber(file)
	}
	if title == "" {
		title = file
	}
	return title, artist, album
}

func isDiscDir(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"cd", "disc", "disk", "dvd", "part", "vol", "volume"} {
		if strings.HasPrefix(lower, prefix) {
			rest := lower[len(prefix):]
			// "cd1", "cd 1", "disc_2" all qualify; "cdbaby" does not.
			if rest == "" {
				return true
			}
			trimmed := strings.TrimLeft(rest, " _-")
			if trimmed == "" {
				return true
			}
			if _, err := parseLeadingInt(trimmed); err == nil {
				return true
			}
		}
	}
	return false
}

// isNoiseDir catches library roots that carry no artist information.
func isNoiseDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", ".", "..", "music", "musica", "audio", "media", "library", "songs", "musik", "音乐":
		return true
	}
	return false
}

// stripTrackNumber removes a leading "01 - ", "01. " or "01_" from a filename.
func stripTrackNumber(name string) string {
	trimmed := strings.TrimLeft(name, " ")
	digits := 0
	for digits < len(trimmed) && trimmed[digits] >= '0' && trimmed[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return name
	}
	rest := strings.TrimLeft(trimmed[digits:], " ._-")
	if rest == "" {
		return name
	}
	return rest
}

// trackNumberFromFilename recovers a track number for untagged files, where
// the only evidence is a leading "03" in the filename. It deliberately stops
// at a plausible bound: "2001 - Space Odyssey.flac" is a title, not track 20
// of a 2001-piece set.
func trackNumberFromFilename(relPath string) int {
	name := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	name = strings.TrimLeft(name, " ")

	end := 0
	for end < len(name) && name[end] >= '0' && name[end] <= '9' {
		end++
	}
	if end == 0 || end > 3 {
		return 0
	}
	// The digits must be followed by a separator, or they are part of a word.
	if end < len(name) {
		switch name[end] {
		case ' ', '-', '_', '.', ')':
			// A separator, so the digits are a track number.
		default:
			return 0
		}
	}
	n, err := parseLeadingInt(name[:end])
	if err != nil {
		return 0
	}
	return n
}

// albumKeyOf builds the unique key for an album. Including the artist means
// "Greatest Hits" by two different bands stays two albums.
func albumKeyOf(artist, title string) string {
	return strings.ToLower(strings.TrimSpace(artist)) + "\x00" + strings.ToLower(strings.TrimSpace(title))
}

// sortName produces an "Artist, The" style name for alphabetical ordering,
// so "The Beatles" files under B rather than T.
func sortName(name string) string {
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)
	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, article) {
			return strings.TrimSpace(trimmed[len(article):]) + ", " + strings.TrimSpace(trimmed[:len(article)-1])
		}
	}
	return trimmed
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// nullString maps "" to a SQL NULL, so empty colours are stored as NULL
// rather than an empty string the client has to special-case.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullInt maps 0 (an unknown year) to SQL NULL, so "no year" stays distinct
// from the year zero.
func nullInt(i int) any {
	if i <= 0 {
		return nil
	}
	return i
}

// sortStrings orders paths case-insensitively but deterministically, so a
// rescan of an unchanged library does no work in a different order.
func sortStrings(s []string) {
	sort.Slice(s, func(i, j int) bool {
		return strings.ToLower(s[i]) < strings.ToLower(s[j])
	})
}

func parseLeadingInt(s string) (int, error) {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return 0, errNotANumber
	}
	return n, nil
}

var errNotANumber = &numberError{}

type numberError struct{}

func (*numberError) Error() string { return "not a number" }

// titleCase is used only for the "Unknown artist" style fallbacks, where a
// readable label matters more than preserving the original casing.
func titleCase(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		out := r
		if unicode.IsSpace(prev) {
			out = unicode.ToUpper(r)
		} else {
			out = unicode.ToLower(r)
		}
		prev = r
		return out
	}, s)
}
