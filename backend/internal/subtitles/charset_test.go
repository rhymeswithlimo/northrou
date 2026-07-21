package subtitles

import (
	"testing"
	"unicode/utf8"
)

func TestToUTF8(t *testing.T) {
	// Windows-1252 encodes é as 0xE9; naive UTF-8 reading would mojibake it.
	cp1252 := []byte{'C', 'a', 'f', 0xE9}
	out := toUTF8(cp1252)
	if !utf8.Valid(out) {
		t.Fatalf("output not valid UTF-8: %v", out)
	}
	if string(out) != "Café" {
		t.Errorf("got %q, want Café", string(out))
	}
}

func TestToUTF8StripsBOM(t *testing.T) {
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...)
	if got := string(toUTF8(withBOM)); got != "hello" {
		t.Errorf("BOM not stripped: %q", got)
	}
}

func TestToUTF8PassesValidUTF8(t *testing.T) {
	in := []byte("already utf-8 é")
	if got := string(toUTF8(in)); got != "already utf-8 é" {
		t.Errorf("valid UTF-8 altered: %q", got)
	}
}
