package recommend

// Theme-row keyword filtering, shared by the cold-start library-frequency themes
// (catThemes) and the warm taste-weighted themes (genKeywordThemes). TMDB
// keywords include production/format/franchise tags that describe how a film was
// made, not what it's about; those make nonsense "Movies About X" rows and are
// filtered here. Values are in normalizeKeyword form.
var nonThematicKeywords = map[string]bool{
	"aftercreditsstinger":        true,
	"duringcreditsstinger":       true,
	"post-credits-scene":         true,
	"credits-scene":              true,
	"based-on-novel-or-book":     true,
	"based-on-novel":             true,
	"based-on-book":              true,
	"based-on-true-story":        true,
	"based-on-comic":             true,
	"based-on-comic-book":        true,
	"based-on-young-adult-novel": true,
	"based-on-video-game":        true,
	"based-on-play":              true,
	"woman-director":             true,
	"imax":                       true,
	"3d":                         true,
	"sequel":                     true,
	"prequel":                    true,
	"remake":                     true,
	"reboot":                     true,
	"live-action-remake":         true,
	"marvel-cinematic-universe":  true,
	"marvel-comic":               true,
	"dc-comics":                  true,
	"dc-extended-universe":       true,
	"shared-universe":            true,
}

// isThematicKeyword reports whether a normalized keyword names a subject/theme
// worth titling a row, rather than a production/format/franchise fact.
func isThematicKeyword(norm string) bool {
	return norm != "" && !nonThematicKeywords[norm]
}

// themeRowTitle is the shared copy for a keyword-driven row.
func themeRowTitle(norm string) string {
	return "Movies About " + humanKeyword(norm)
}
