package language

import "testing"

func TestCode(t *testing.T) {
	cases := map[string]string{
		"English":         "en",
		"eng":             "en",
		"en":              "en",
		"nor":             "no",
		"spa":             "es",
		"Latin American":  "es",
		"pt-BR":           "pt-br", // unknown compound passes through lower-cased
		"":                "",
		"und":             "",
		"zz-unknown-code": "zz-unknown-code",
	}
	for in, want := range cases {
		if got := Code(in); got != want {
			t.Errorf("Code(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatch(t *testing.T) {
	if !Match("eng", "en") {
		t.Error("eng should match en")
	}
	if !Match("English", "eng") {
		t.Error("English should match eng")
	}
	if !Match("nor", "no") {
		t.Error("nor should match no")
	}
	if Match("en", "fr") {
		t.Error("en should not match fr")
	}
	if Match("", "en") {
		t.Error("empty should not match anything")
	}
}

func TestName(t *testing.T) {
	if got := Name("nor"); got != "Norwegian" {
		t.Errorf("Name(nor) = %q", got)
	}
	if got := Name("en"); got != "English" {
		t.Errorf("Name(en) = %q", got)
	}
	if got := Name("xx"); got != "xx" {
		t.Errorf("Name(xx) = %q, want passthrough", got)
	}
}

func TestTesseract(t *testing.T) {
	cases := map[string]string{
		"en": "eng", "es": "spa", "fr": "fra", "": "eng", "und": "eng",
		"nor": "nor", // Norwegian
	}
	for in, want := range cases {
		if got := Tesseract(in); got != want {
			t.Errorf("Tesseract(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKnown(t *testing.T) {
	for _, k := range []string{"en", "eng", "English", "nor", "Latin American"} {
		if !Known(k) {
			t.Errorf("Known(%q) = false", k)
		}
	}
	for _, k := range []string{"", "und", "zzz"} {
		if Known(k) {
			t.Errorf("Known(%q) = true", k)
		}
	}
}
