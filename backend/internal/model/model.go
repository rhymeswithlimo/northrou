// Package model holds Northrou's core domain types, shared across the scanner,
// database, API, transcoder, and recommendation engine. These are plain data
// types with no persistence or transport logic.
package model

import "time"

// MediaKind distinguishes the top-level library types.
type MediaKind string

const (
	KindMovie   MediaKind = "movie"
	KindShow    MediaKind = "show"
	KindEpisode MediaKind = "episode"
)

// HDRType classifies the high-dynamic-range format of a video stream, as
// determined authoritatively by ffprobe (not filename tags).
type HDRType string

const (
	HDRNone       HDRType = ""
	HDR10         HDRType = "hdr10"
	HDR10Plus     HDRType = "hdr10plus"
	HDRDolbyVision HDRType = "dolbyvision"
	HDRHLG        HDRType = "hlg"
)

// User is a household account. The first account created by the setup wizard
// is the admin.
type User struct {
	ID           int64
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

// Library is a configured root folder of a given kind.
type Library struct {
	ID   int64
	Name string
	Kind MediaKind // KindMovie or KindShow
	Path string
}

// Movie is a matched film with TMDB metadata plus its physical file.
type Movie struct {
	ID          int64
	TMDBID      int64
	Title       string
	Year        int
	Overview    string
	Runtime     int // minutes
	Genres      []string
	CollectionID int64 // TMDB collection id, 0 if none
	PosterPath  string // local cached path
	BackdropPath string
	Cast        []Credit
	Crew        []Credit
	OriginalLang string
	Rating      float64 // TMDB vote average (0-10)
	Votes       int     // TMDB vote count
	Popularity  float64 // TMDB popularity
	Revenue     int64   // box office revenue (USD), 0 if unknown
	Country     string  // primary production country (ISO 3166-1, e.g. "US")
	AddedAt     time.Time
	File        *MediaFile
}

// Show is a matched TV series.
type Show struct {
	ID           int64
	TMDBID       int64
	Title        string
	Year         int
	Overview     string
	Genres       []string
	PosterPath   string
	BackdropPath string
	OriginalLang string
	Rating       float64 // TMDB vote average (0-10)
	Popularity   float64 // TMDB popularity
	Country      string  // primary origin country (ISO 3166-1, e.g. "US")
	AddedAt      time.Time
	Cast         []Credit
	Crew         []Credit
	Seasons      []Season
}

// Season groups episodes.
type Season struct {
	ID       int64
	ShowID   int64
	Number   int
	Episodes []Episode
}

// Episode is a single TV episode with its physical file.
type Episode struct {
	ID       int64
	ShowID   int64
	SeasonID int64
	Season   int
	Number   int
	Title    string
	Overview string
	Runtime  int
	File     *MediaFile
}

// MediaFile is one physical media file on disk with authoritative technical
// metadata from ffprobe.
type MediaFile struct {
	ID        int64
	Path      string
	SizeBytes int64
	ModTime   time.Time
	Container string // "matroska,webm", "mov,mp4", ...
	Duration  float64 // seconds
	Video     VideoStream
	Audio     []AudioStream
	Subtitles []SubtitleStream
}

// VideoStream describes the primary video track.
type VideoStream struct {
	Index   int
	Codec   string // "hevc", "h264", "av1", ...
	Width   int
	Height  int
	HDR     HDRType
	BitRate int64
	Profile string
}

// AudioStream describes one audio track.
type AudioStream struct {
	Index     int
	Codec     string // "truehd", "dts", "eac3", "aac", ...
	Profile   string // e.g. "DTS-HD MA", used to detect lossless/Atmos
	Channels  int
	ChannelLayout string
	Language  string
	Atmos     bool // Dolby Atmos / object audio present
	Default   bool
	BitRate   int64
}

// SubtitleStream describes one subtitle track as found in the container.
type SubtitleStream struct {
	Index    int
	Codec    string // "subrip", "ass", "hdmv_pgs_subtitle", ...
	Language string
	Title    string
	Forced   bool
	Default  bool
}

// Credit is a cast or crew member.
type Credit struct {
	PersonID int64
	Name     string
	Role     string // character (cast) or job (crew), e.g. "Director"
	Order    int
}

// WatchEvent records progress against a playable item (movie or episode).
type WatchEvent struct {
	ID           int64
	UserID       int64
	MediaKind    MediaKind // KindMovie or KindEpisode
	MediaID      int64
	PositionSec  float64
	DurationSec  float64
	Completed    bool
	RewatchCount int
	UpdatedAt    time.Time
}

// Completion returns the fraction [0,1] of the item watched.
func (w WatchEvent) Completion() float64 {
	if w.DurationSec <= 0 {
		return 0
	}
	c := w.PositionSec / w.DurationSec
	if c > 1 {
		return 1
	}
	return c
}

// UnmatchedFile is a scanned file the scanner could not confidently match to
// TMDB, surfaced in the UI for manual correction.
type UnmatchedFile struct {
	ID       int64
	Path     string
	Kind     MediaKind
	Reason   string
	ParsedTitle string
	ParsedYear  int
	FoundAt  time.Time
}
