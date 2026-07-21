package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ParsedInfo is the result of parsing a scene-release filename. Technical tags
// (codec, resolution, HDR) are intentionally discarded here. Authoritative
// technical data comes from ffprobe. Only title/year/season/episode are kept
// to drive the TMDB lookup.
type ParsedInfo struct {
	Title     string
	Year      int
	IsEpisode bool
	Season    int
	Episode   int
	Episodes  []int // populated for multi-episode files (S01E01E02)
}

var (
	// SxxEyy (with optional additional Eyy for multi-episode files).
	reSeasonEp = regexp.MustCompile(`(?i)\bS(\d{1,2})[\s._-]?E(\d{1,3})((?:[\s._-]?E\d{1,3})*)\b`)
	// 1x05 style (episode may be a single digit: 1x5).
	reAltEp = regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)
	// Standalone episode E-numbers within a multi-episode suffix.
	reEpNum = regexp.MustCompile(`(?i)E(\d{1,3})`)
	// A 4-digit year in a plausible range.
	reYear = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	// "Season 1", "Season 01", "Season.1", "S01", "S1", "Series 1".
	reSeasonFolder = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._-]*0*(\d{1,3})\b`)
	// A bare episode number in a marker-less filename: "E07", "Episode 7", "- 07 -".
	reEpisodeLoose = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:e|ep|episode)[\s._-]*0*(\d{1,3})\b`)
	// A 4-digit year embedded in a folder name, e.g. "2001 - Sorcerers Stone".
	reFolderYear = regexp.MustCompile(`(?:^|[\s._(\[-])(19\d{2}|20\d{2})(?:[\s._)\]-]|$)`)
)

// genericFolder names never identify a show; they are containers, so title
// recovery walks past them to the real show folder.
var genericFolder = map[string]bool{
	"mkv": true, "mp4": true, "subs": true, "subtitles": true, "sub": true,
	"video": true, "videos": true, "media": true, "proxies": true, "extras": true,
}

// seasonFromFolder parses a season number from a folder name in the common
// layouts. "Specials"/"Season 0" yield 0 with ok=true (a valid special season).
func seasonFromFolder(name string) (int, bool) {
	low := strings.ToLower(strings.TrimSpace(name))
	if low == "specials" || low == "special" {
		return 0, true
	}
	if m := reSeasonFolder.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// episodeFromName extracts a loose episode number from a marker-less filename.
func episodeFromName(base string) (int, bool) {
	if m := reEpisodeLoose.FindStringSubmatch(normalize(base)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// yearFromFolder finds a plausible year embedded in a folder name.
func yearFromFolder(name string) int {
	if m := reFolderYear.FindStringSubmatch(name); m != nil {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	return 0
}

// isGenericFolder reports whether a folder name is a container rather than a
// title (a season folder, or a known technical grouping like "MKV"/"Subs").
func isGenericFolder(name string) bool {
	if _, ok := seasonFromFolder(name); ok {
		return true
	}
	return genericFolder[strings.ToLower(strings.TrimSpace(name))]
}

// releaseTags are quality/source/codec/audio/HDR/edition tokens. The first one
// encountered marks the end of the title (and, for movies, the region after
// the year).
var releaseTags = map[string]bool{
	// resolution
	"2160p": true, "1440p": true, "1080p": true, "1080i": true, "720p": true,
	"576p": true, "480p": true, "360p": true, "4k": true, "uhd": true,
	// source
	"bluray": true, "blu-ray": true, "bdrip": true, "brrip": true, "bdremux": true,
	"remux": true, "web": true, "webrip": true, "web-dl": true, "webdl": true,
	"hdtv": true, "pdtv": true, "dvdrip": true, "dvd": true, "hddvd": true,
	"hdrip": true, "cam": true, "ts": true, "tc": true, "vodrip": true,
	// video codec
	"x264": true, "x265": true, "h264": true, "h265": true, "h": true,
	"hevc": true, "avc": true, "xvid": true, "divx": true, "vp9": true,
	"av1": true, "10bit": true, "8bit": true, "mpeg2": true, "hi10p": true,
	// audio
	"aac": true, "ac3": true, "eac3": true, "dd": true, "ddp": true, "ddp5": true,
	"dts": true, "dts-hd": true, "dtshd": true, "truehd": true, "atmos": true,
	"flac": true, "mp3": true, "opus": true, "ma": true, "lpcm": true,
	// hdr
	"hdr": true, "hdr10": true, "hdr10plus": true, "dv": true, "hlg": true,
	"sdr": true, "dovi": true,
	// editions / misc
	"proper": true, "repack": true, "extended": true, "unrated": true,
	"remastered": true, "limited": true, "internal": true, "complete": true,
	"dubbed": true, "subbed": true, "multi": true, "dual": true, "imax": true,
	"hybrid": true, "readnfo": true, "uncut": true, "theatrical": true,
}

// ParseFilename extracts title/year/season/episode from a media filename (the
// base name, with or without extension).
func ParseFilename(name string) ParsedInfo {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))

	// Episode detection first (SxxEyy / 1x05).
	if info, ok := parseEpisode(base); ok {
		return info
	}

	// Movie: normalize and find the title/year boundary.
	norm := normalize(base)
	tokens := strings.Fields(norm)

	tagIdx := len(tokens)
	for i, tok := range tokens {
		if releaseTags[strings.ToLower(strings.Trim(tok, "()[]{}"))] {
			tagIdx = i
			break
		}
	}

	head := tokens[:tagIdx]
	year := 0
	titleEnd := len(head)
	// The last year-like token in the head is the release year; title precedes it.
	for i := len(head) - 1; i >= 0; i-- {
		if y, ok := yearOf(head[i]); ok {
			year = y
			titleEnd = i
			break
		}
	}

	return ParsedInfo{
		Title: cleanTitle(strings.Join(head[:titleEnd], " ")),
		Year:  year,
	}
}

// parseEpisode handles TV episode filenames.
func parseEpisode(base string) (ParsedInfo, bool) {
	norm := normalize(base)

	if m := reSeasonEp.FindStringSubmatch(norm); m != nil {
		season, _ := strconv.Atoi(m[1])
		first, _ := strconv.Atoi(m[2])
		eps := []int{first}
		if m[3] != "" { // additional episodes in a multi-file
			for _, em := range reEpNum.FindAllStringSubmatch(m[3], -1) {
				if n, err := strconv.Atoi(em[1]); err == nil {
					eps = append(eps, n)
				}
			}
		}
		title, year := titleBefore(norm, reSeasonEp)
		return ParsedInfo{
			Title: title, IsEpisode: true, Season: season,
			Episode: first, Episodes: eps, Year: year,
		}, true
	}

	if m := reAltEp.FindStringSubmatch(norm); m != nil {
		season, _ := strconv.Atoi(m[1])
		ep, _ := strconv.Atoi(m[2])
		title, year := titleBefore(norm, reAltEp)
		return ParsedInfo{
			Title: title, IsEpisode: true, Season: season,
			Episode: ep, Episodes: []int{ep}, Year: year,
		}, true
	}
	return ParsedInfo{}, false
}

// titleBefore returns the cleaned title preceding the first match of re, plus
// any release year found in that region (the year is kept out of the title).
func titleBefore(norm string, re *regexp.Regexp) (string, int) {
	loc := re.FindStringIndex(norm)
	if loc == nil {
		return cleanTitle(norm), yearIn(norm)
	}
	head := norm[:loc[0]]
	year := 0
	// Drop a trailing year that belongs to the show, keep it out of the title.
	tokens := strings.Fields(head)
	for len(tokens) > 0 {
		if y, ok := yearOf(tokens[len(tokens)-1]); ok {
			year = y
			tokens = tokens[:len(tokens)-1]
			continue
		}
		break
	}
	return cleanTitle(strings.Join(tokens, " ")), year
}

// normalize converts separators to spaces so tokenization is uniform.
func normalize(s string) string {
	repl := strings.NewReplacer(".", " ", "_", " ")
	return strings.TrimSpace(repl.Replace(s))
}

// cleanTitle trims stray bracket/paren artifacts and collapses whitespace.
func cleanTitle(s string) string {
	s = strings.Trim(s, " -([{")
	s = strings.TrimRight(s, " -)]}")
	return strings.Join(strings.Fields(s), " ")
}

// yearOf parses a token as a plausible release year.
func yearOf(tok string) (int, bool) {
	tok = strings.Trim(tok, "()[]{}")
	if len(tok) != 4 {
		return 0, false
	}
	y, err := strconv.Atoi(tok)
	if err != nil || y < 1900 || y > 2099 {
		return 0, false
	}
	return y, true
}

// yearIn finds a year anywhere in s (used for show titles like "Show (2005)").
func yearIn(s string) int {
	if m := reYear.FindString(s); m != "" {
		y, _ := strconv.Atoi(m)
		return y
	}
	return 0
}
