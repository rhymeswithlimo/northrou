package recommend

import "strings"

// Keyword normalization. TMDB keywords are user-contributed and messy: the same
// idea shows up as "slow-burn", "slow burn", and "slowburn". Left raw, each is a
// separate token and the co-occurrence signal fractures. We store raw names in
// the DB and normalize here, at vector-build time, so the alias map can grow
// without a re-scan.
//
// normalizeKeyword lowercases, trims, and folds separators (space/underscore ->
// hyphen), then applies the hand-maintained alias map. Extend keywordAliases as
// collisions are noticed; a canonical value that isn't itself a key maps to
// itself.
func normalizeKeyword(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return ""
	}
	// Fold separators so "slow burn" / "slow_burn" collapse onto "slow-burn".
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if canon, ok := keywordAliases[s]; ok {
		return canon
	}
	return s
}

// keywordAliases folds known variants and near-synonyms onto one canonical form.
// Keep entries obvious and defensible; this is not a thesaurus. Left side is the
// already-separator-folded form (see normalizeKeyword).
var keywordAliases = map[string]string{
	"slowburn":            "slow-burn",
	"coming-of-age-story": "coming-of-age",
	"dystopian":           "dystopia",
	"dystopian-future":    "dystopia",
	"post-apocalyptic":    "post-apocalypse",
	"time-travel-story":   "time-travel",
	"based-on-a-true-story": "based-on-true-story",
	"based-on-novel":      "based-on-book",
	"based-on-a-novel":    "based-on-book",
	"based-on-a-book":     "based-on-book",
	"whodunit":            "murder-mystery",
	"buddy-cop":           "buddy",
	"female-protagonist":  "strong-female-lead",
	"anti-hero":           "antihero",
}
