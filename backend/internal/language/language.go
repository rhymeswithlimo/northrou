// Package language normalizes the messy world of subtitle/audio language
// identifiers (ISO 639-1, ISO 639-2/B and /T, and free-text names like
// "English" or "Latin American") into a single canonical code, and provides
// display names and Tesseract OCR language mappings. It is the one source of
// truth shared by the scanner, subtitle pipeline, config, and API so language
// comparison ("does this track match the household's preferred language?")
// behaves the same everywhere.
package language

import "strings"

// lang is one language's canonical identity and its recognized aliases.
type lang struct {
	code      string   // canonical: ISO 639-1 where one exists, else 639-2/T
	name      string   // English display name
	tesseract string   // Tesseract traineddata name (ISO 639-2/T), "" if none
	aliases   []string // 639-1, 639-2/B, 639-2/T, and common free-text names
}

// table is deliberately curated to the languages that actually show up in
// personal libraries. Unknown codes pass through untouched rather than erroring.
var table = []lang{
	{"en", "English", "eng", []string{"en", "eng", "english"}},
	{"es", "Spanish", "spa", []string{"es", "spa", "esp", "spanish", "castilian", "latin american", "latin", "espanol", "español"}},
	{"fr", "French", "fra", []string{"fr", "fra", "fre", "french", "francais", "français"}},
	{"de", "German", "deu", []string{"de", "deu", "ger", "german", "deutsch"}},
	{"it", "Italian", "ita", []string{"it", "ita", "italian", "italiano"}},
	{"pt", "Portuguese", "por", []string{"pt", "por", "portuguese", "brazilian", "portugues", "português"}},
	{"nl", "Dutch", "nld", []string{"nl", "nld", "dut", "dutch", "nederlands"}},
	{"sv", "Swedish", "swe", []string{"sv", "swe", "swedish", "svenska"}},
	{"no", "Norwegian", "nor", []string{"no", "nor", "nob", "nno", "norwegian", "norsk"}},
	{"da", "Danish", "dan", []string{"da", "dan", "danish", "dansk"}},
	{"fi", "Finnish", "fin", []string{"fi", "fin", "finnish", "suomi"}},
	{"is", "Icelandic", "isl", []string{"is", "isl", "ice", "icelandic"}},
	{"ru", "Russian", "rus", []string{"ru", "rus", "russian"}},
	{"uk", "Ukrainian", "ukr", []string{"uk", "ukr", "ukrainian"}},
	{"pl", "Polish", "pol", []string{"pl", "pol", "polish", "polski"}},
	{"cs", "Czech", "ces", []string{"cs", "ces", "cze", "czech"}},
	{"sk", "Slovak", "slk", []string{"sk", "slk", "slo", "slovak"}},
	{"hu", "Hungarian", "hun", []string{"hu", "hun", "hungarian", "magyar"}},
	{"ro", "Romanian", "ron", []string{"ro", "ron", "rum", "romanian"}},
	{"el", "Greek", "ell", []string{"el", "ell", "gre", "greek"}},
	{"tr", "Turkish", "tur", []string{"tr", "tur", "turkish", "turkce", "türkçe"}},
	{"ar", "Arabic", "ara", []string{"ar", "ara", "arabic"}},
	{"he", "Hebrew", "heb", []string{"he", "heb", "hebrew"}},
	{"hi", "Hindi", "hin", []string{"hi", "hin", "hindi"}},
	{"th", "Thai", "tha", []string{"th", "tha", "thai"}},
	{"vi", "Vietnamese", "vie", []string{"vi", "vie", "vietnamese"}},
	{"id", "Indonesian", "ind", []string{"id", "ind", "indonesian", "bahasa"}},
	{"ja", "Japanese", "jpn", []string{"ja", "jpn", "jap", "japanese"}},
	{"ko", "Korean", "kor", []string{"ko", "kor", "korean"}},
	{"zh", "Chinese", "chi_sim", []string{"zh", "zho", "chi", "chinese", "mandarin", "cantonese"}},
}

var (
	byAlias = map[string]*lang{} // any recognized alias -> canonical entry
)

func init() {
	for i := range table {
		l := &table[i]
		for _, a := range l.aliases {
			byAlias[a] = l
		}
	}
}

// lookup finds the canonical entry for any token, or nil.
func lookup(token string) *lang {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" || t == "und" {
		return nil
	}
	return byAlias[t]
}

// Code normalizes any language token (code or name) to its canonical code.
// Unknown tokens are lower-cased and returned as-is, so unfamiliar 639-2 codes
// still compare consistently with themselves; empty/"und" normalize to "".
func Code(token string) string {
	if l := lookup(token); l != nil {
		return l.code
	}
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "und" {
		return ""
	}
	return t
}

// Match reports whether two language tokens refer to the same language, after
// canonicalization. Empty/unknown-on-both is not a match.
func Match(a, b string) bool {
	ca, cb := Code(a), Code(b)
	return ca != "" && ca == cb
}

// Name returns a human-readable language name, or the raw token when unknown.
func Name(token string) string {
	if l := lookup(token); l != nil {
		return l.name
	}
	return token
}

// Tesseract returns the Tesseract traineddata name for a language token,
// defaulting to English for empty/unknown so OCR always has a language.
func Tesseract(token string) string {
	if l := lookup(token); l != nil && l.tesseract != "" {
		return l.tesseract
	}
	if t := strings.ToLower(strings.TrimSpace(token)); t != "" && t != "und" {
		return t // assume already an ISO 639-2 traineddata name
	}
	return "eng"
}

// Known reports whether a token is a language we recognize (used to validate
// user-entered preferences).
func Known(token string) bool {
	return lookup(token) != nil
}
