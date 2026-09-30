package models

// Track is one playable audio file.
type Track struct {
	ID         int64   `json:"id"`
	Path       string  `json:"-"` // relative to the music root; never sent to clients
	Title      string  `json:"title"`
	DurationMS int64   `json:"durationMs"`
	Bitrate    int     `json:"bitrate"`
	Format     string  `json:"format"`
	HasLyrics  bool    `json:"hasLyrics"`
	TrackNo    *int    `json:"trackNo,omitempty"`
	DiscNo     int     `json:"discNo"`
	Year       *int    `json:"year,omitempty"`
	SampleRate int     `json:"sampleRate"`
	Channels   int     `json:"channels"`
	ArtistID   int64   `json:"artistId"`
	AlbumID    int64   `json:"albumId"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album"`
	CoverPath  *string `json:"coverPath"`
	CoverColor *string `json:"coverColor"`
}

// Album is a collection of tracks by one artist.
type Album struct {
	ID            int64   `json:"id"`
	ArtistID      int64   `json:"artistId"`
	Title         string  `json:"title"`
	Year          *int    `json:"year,omitempty"`
	CoverPath     *string `json:"coverPath"`
	DominantColor *string `json:"dominantColor"`
	TrackCount    int     `json:"trackCount"`
	DurationMS    int64   `json:"durationMs"`
	Artist        string  `json:"artist"`
}

// Artist groups albums and tracks.
type Artist struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	SortName   string `json:"sortName"`
	AlbumCount int    `json:"albumCount"`
	TrackCount int    `json:"trackCount"`
}

// Playlist is a user-curated, ordered list of tracks.
type Playlist struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	TrackCount  int     `json:"trackCount"`
	DurationMS  int64   `json:"durationMs"`
	CoverPath   *string `json:"coverPath"`
	CoverColor  *string `json:"coverColor"`
	CreatedAt   int64   `json:"createdAt"`
	UpdatedAt   int64   `json:"updatedAt"`
}

// User is an account. PasswordHash is never serialised.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	IsAdmin      bool   `json:"isAdmin"`
	CreatedAt    int64  `json:"createdAt"`
	LastLoginAt  *int64 `json:"lastLoginAt,omitempty"`
	PasswordHash string `json:"-"`
}

// ScanState is the progress of a library scan, streamed to the admin UI.
type ScanState struct {
	Running     bool   `json:"running"`
	Phase       string `json:"phase"`
	Processed   int    `json:"processed"`
	Total       int    `json:"total"`
	Added       int    `json:"added"`
	Updated     int    `json:"updated"`
	Removed     int    `json:"removed"`
	Skipped     int    `json:"skipped"`
	CurrentFile string `json:"currentFile"`
	Error       string `json:"error,omitempty"`
	StartedAt   int64  `json:"startedAt,omitempty"`
	FinishedAt  int64  `json:"finishedAt,omitempty"`
}

// LibraryStats powers the dashboard counters.
type LibraryStats struct {
	Artists   int   `json:"artists"`
	Albums    int   `json:"albums"`
	Tracks    int   `json:"tracks"`
	Duration  int64 `json:"durationMs"`
	SizeBytes int64 `json:"sizeBytes"`
}
