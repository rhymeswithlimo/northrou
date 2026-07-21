package subtitles

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/language"
)

// ExternalSub is a subtitle file discovered on disk next to a video, with the
// metadata inferred from its name and location.
type ExternalSub struct {
	Path     string // absolute path to the sidecar file
	Language string // canonical language code, "" if unknown
	Forced   bool
	SDH      bool
	Format   string // subrip|ass|webvtt|vobsub
}

// subExts maps a subtitle file extension to our stored format tag. .idx is the
// VobSub index companion to .sub and is not a standalone track.
var subExts = map[string]string{
	".srt": "subrip",
	".ass": "ass",
	".ssa": "ass",
	".vtt": "webvtt",
	".sub": "vobsub",
}

// subDirNames are folder names (compared case-insensitively) that conventionally
// hold sidecar subtitles. People name this folder all sorts of ways, so we cover
// the common ones rather than assuming a single convention.
var subDirNames = map[string]bool{
	"sub": true, "subs": true,
	"subtitle": true, "subtitles": true,
	"srt": true, "srts": true,
	"vtt": true, "ass": true,
	"caption": true, "captions": true,
	"cc": true, "subrip": true,
}

// videoExtsForSidecar mirrors the scanner's video extensions closely enough to
// decide whether a folder holds exactly one video (kept local to avoid a
// scanner import cycle).
var videoExtsForSidecar = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true,
	".ts": true, ".m2ts": true, ".webm": true, ".wmv": true, ".mpg": true,
	".mpeg": true, ".flv": true,
}

var reSxxEyyKey = regexp.MustCompile(`(?i)s\d{1,2}e\d{1,3}`)

// DiscoverSidecars finds external subtitle files that belong to videoPath. It
// looks in the video's own directory and any adjacent Subs/ folder (including
// one level of per-release subfolders), attaching a file when its name matches
// the video's, or when the video's folder holds exactly one video (so loosely
// named subtitles like "English.srt" are unambiguous).
func DiscoverSidecars(videoPath string) []ExternalSub {
	videoDir := filepath.Dir(videoPath)
	stem := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	stemKey := normKey(stem)
	solo := dirVideoCount(videoDir) <= 1

	var out []ExternalSub
	seen := map[string]bool{}
	add := func(path string, ownerMatched bool) {
		ext := strings.ToLower(filepath.Ext(path))
		format, ok := subExts[ext]
		if !ok || seen[path] {
			return
		}
		// Attach when the filename identifies this video, or the folder is solo.
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if !ownerMatched && !solo && !stemMatches(stemKey, base) {
			return
		}
		seen[path] = true
		lang, forced, sdh := parseSubMeta(base, stem)
		out = append(out, ExternalSub{Path: path, Language: lang, Forced: forced, SDH: sdh, Format: format})
	}

	// 1. Files directly beside the video.
	for _, e := range readDirNames(videoDir) {
		if e.isDir {
			continue
		}
		add(filepath.Join(videoDir, e.name), false)
	}

	// 2. Adjacent Subs/ folders (and one level of per-release subfolders).
	for _, e := range readDirNames(videoDir) {
		if !e.isDir || !subDirNames[strings.ToLower(e.name)] {
			continue
		}
		subDir := filepath.Join(videoDir, e.name)
		for _, se := range readDirNames(subDir) {
			p := filepath.Join(subDir, se.name)
			if !se.isDir {
				add(p, false)
				continue
			}
			// Per-release subfolder: attach all its files when the folder name
			// identifies this video (bare names inside carry only language).
			if stemMatches(stemKey, se.name) {
				for _, fe := range readDirNames(p) {
					if !fe.isDir {
						add(filepath.Join(p, fe.name), true)
					}
				}
			}
		}
	}
	return out
}

type dirEntry struct {
	name  string
	isDir bool
}

func readDirNames(dir string) []dirEntry {
	fis, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]dirEntry, 0, len(fis))
	for _, fi := range fis {
		out = append(out, dirEntry{name: fi.Name(), isDir: fi.IsDir()})
	}
	return out
}

// dirVideoCount counts video files directly in dir (non-recursive).
func dirVideoCount(dir string) int {
	n := 0
	for _, e := range readDirNames(dir) {
		if !e.isDir && videoExtsForSidecar[strings.ToLower(filepath.Ext(e.name))] {
			n++
		}
	}
	return n
}

// normKey reduces a name to alphanumerics only, lower-cased, so release names
// compare regardless of separators and bracket tags.
func normKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stemMatches reports whether a subtitle/subfolder name identifies the video.
// It matches when either name is a prefix of the other's key (release names are
// often truncated in sidecars), or they share the same SxxEyy episode marker.
func stemMatches(stemKey, otherName string) bool {
	ok := normKey(otherName)
	if ok == "" || stemKey == "" {
		return false
	}
	if strings.HasPrefix(ok, stemKey) || strings.HasPrefix(stemKey, ok) {
		return true
	}
	a := reSxxEyyKey.FindString(strings.ToLower(otherName))
	b := reSxxEyyKey.FindString(strings.ToLower(stemKey))
	return a != "" && a == b
}

// subFlagTokens split a subtitle descriptor into lowercase alphabetic tokens.
var reNonAlpha = regexp.MustCompile(`[^a-zA-Z]+`)

// parseSubMeta infers language/forced/SDH from a subtitle file's base name.
// Tokens that are part of the video's own release name are ignored so a title
// like "The English Patient" isn't misread as an English descriptor; the
// remaining tokens (a trailing "eng", "SDH", "forced", etc.) carry the meaning.
func parseSubMeta(base, videoStem string) (lang string, forced, sdh bool) {
	low := strings.ToLower(base)
	forced = strings.Contains(low, "forced") || strings.Contains(low, "foreign")
	sdh = strings.Contains(low, "sdh") || strings.Contains(low, "hearing") ||
		containsToken(low, "hi") || containsToken(low, "cc")

	stem := map[string]bool{}
	for _, tok := range reNonAlpha.Split(strings.ToLower(videoStem), -1) {
		if tok != "" {
			stem[tok] = true
		}
	}

	// Multi-word language names first (e.g. "Latin American").
	if strings.Contains(low, "latin american") || strings.Contains(low, "castilian") {
		return "es", forced, sdh
	}
	for _, tok := range reNonAlpha.Split(base, -1) {
		if tok == "" || stem[strings.ToLower(tok)] {
			continue
		}
		if language.Known(tok) {
			return language.Code(tok), forced, sdh
		}
	}
	return "", forced, sdh
}

// containsToken reports whether word appears as a standalone token in s.
func containsToken(s, word string) bool {
	for _, tok := range reNonAlpha.Split(s, -1) {
		if strings.EqualFold(tok, word) {
			return true
		}
	}
	return false
}
