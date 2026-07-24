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
	HDRNone        HDRType = ""
	HDR10          HDRType = "hdr10"
	HDR10Plus      HDRType = "hdr10plus"
	HDRDolbyVision HDRType = "dolbyvision"
	HDRHLG         HDRType = "hlg"
)

// Profile is a viewer under the account, in the style of Netflix profiles. It
// carries a display name and optional avatar and owns all per-viewer state
// (watch history, taste profile, home rows). Profiles have no email and no
// password; every profile may administer from a local connection (see the auth
// package).
type Profile struct {
	ID        int64
	Name      string
	Avatar    string // optional; empty when unset
	CreatedAt time.Time
	// Preferred audio/subtitle languages (ISO-639). Empty means "use the server
	// default"; each viewer sets their own, Netflix-style.
	PreferredAudioLang    string
	PreferredSubtitleLang string
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
	ID            int64
	TMDBID        int64
	Title         string
	Year          int
	Overview      string
	Runtime       int // minutes
	Genres        []string
	Keywords      []string // TMDB keyword tags (thematic/tonal signal)
	Companies     []string // production company names (for studio rows)
	Tagline       string
	Certification string // e.g. "R", resolved to one country
	CollectionID  int64  // TMDB collection id, 0 if none
	PosterPath    string // local cached path
	BackdropPath  string
	Cast          []Credit
	Crew          []Credit
	OriginalLang  string
	Rating        float64 // TMDB vote average (0-10)
	Votes         int     // TMDB vote count
	Popularity    float64 // TMDB popularity
	Revenue       int64   // box office revenue (USD), 0 if unknown
	Country       string  // primary production country (ISO 3166-1, e.g. "US")
	AddedAt       time.Time
	File          *MediaFile
}

// Show is a matched TV series.
type Show struct {
	ID            int64
	TMDBID        int64
	Title         string
	Year          int
	Overview      string
	Genres        []string
	Keywords      []string // TMDB keyword tags (thematic/tonal signal)
	Companies     []string // production company names (for studio rows)
	Creators      []string // created_by names (for "Created by X" rows)
	Tagline       string
	Certification string // e.g. "TV-MA", resolved to one country
	PosterPath    string
	BackdropPath  string
	OriginalLang  string
	Rating        float64 // TMDB vote average (0-10)
	Popularity    float64 // TMDB popularity
	Country       string  // primary origin country (ISO 3166-1, e.g. "US")
	AddedAt       time.Time
	Cast          []Credit
	Crew          []Credit
	Seasons       []Season
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
	ID        int64
	ShowID    int64
	SeasonID  int64
	Season    int
	Number    int
	Title     string
	Overview  string
	Runtime   int
	StillPath string // local cached path
	AirDate   string // ISO date, empty when unknown
	File      *MediaFile
}

// MediaFile is one physical media file on disk with authoritative technical
// metadata from ffprobe.
type MediaFile struct {
	ID        int64
	Path      string
	SizeBytes int64
	ModTime   time.Time
	Container string  // "matroska,webm", "mov,mp4", ...
	Duration  float64 // seconds
	Video     VideoStream
	Audio     []AudioStream
	Subtitles []SubtitleStream
}

// VideoStream describes the primary video track.
type VideoStream struct {
	Index    int
	Codec    string // "hevc", "h264", "av1", ...
	Width    int
	Height   int
	HDR      HDRType
	BitRate  int64
	Profile  string
	PixFmt   string // ffprobe pix_fmt, e.g. "yuv420p10le"
	BitDepth int    // luma bit depth (8, 10, 12); 0 if unknown
	// Dolby Vision. DVProfile is the DV profile number (5, 7, 8, ...); 0 if not
	// DV. DVBLCompat is the base-layer signal compatibility id: 1 = HDR10, 4 =
	// HLG, 2/0 = DV-only. Together they say whether a non-DV player can still
	// show the stream as ordinary HDR.
	DVProfile  int
	DVBLCompat int
}

// DVCrossCompatible reports whether a Dolby Vision stream carries a standard HDR
// base layer (profile 8 with HDR10 or HLG compatibility) that a non-DV but
// HDR-capable client can play directly.
func (v VideoStream) DVCrossCompatible() bool {
	return v.HDR == HDRDolbyVision && (v.DVBLCompat == 1 || v.DVBLCompat == 4)
}

// DVDualLayer reports whether a Dolby Vision stream is dual-layer (profile 7),
// which most players and all browsers cannot handle without transcoding.
func (v VideoStream) DVDualLayer() bool {
	return v.HDR == HDRDolbyVision && v.DVProfile == 7
}

// AudioStream describes one audio track.
type AudioStream struct {
	Index         int
	Codec         string // "truehd", "dts", "eac3", "aac", ...
	Profile       string // e.g. "DTS-HD MA", used to detect lossless/Atmos
	Channels      int
	ChannelLayout string
	Language      string
	Title         string // stream title tag, e.g. "Commentary by Director"
	Atmos         bool   // Dolby Atmos / object audio present
	Commentary    bool   // director/cast commentary track (demoted in selection)
	Default       bool
	BitRate       int64
}

// SubtitleStream describes one subtitle track as found in the container.
type SubtitleStream struct {
	Index    int
	Codec    string // "subrip", "ass", "hdmv_pgs_subtitle", ...
	Language string
	Title    string
	Forced   bool
	SDH      bool // hearing-impaired / SDH track
	Default  bool
}

// Credit is a cast or crew member.
type Credit struct {
	PersonID    int64
	Name        string
	Role        string // character (cast) or job (crew), e.g. "Director"
	Order       int
	ProfilePath string // local cached headshot path; empty when none
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
	ID          int64     `json:"id"`
	Path        string    `json:"path"`
	Kind        MediaKind `json:"kind"`
	Reason      string    `json:"reason"`
	ParsedTitle string    `json:"parsed_title"`
	ParsedYear  int       `json:"parsed_year"`
	FoundAt     time.Time `json:"found_at"`
}
