package subtitles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSubMeta(t *testing.T) {
	cases := []struct {
		base, stem string
		lang       string
		forced     bool
		sdh        bool
	}{
		{"2_English", "", "en", false, false},
		{"7_nor", "", "no", false, false},
		{"English(SDH)", "", "en", false, true},
		{"SDH.eng.HI", "", "en", false, true},
		{"Latin American.spa", "", "es", false, false},
		{"English", "", "en", false, false},
		// Trailing descriptor after the video stem carries the meaning.
		{"The.Day.of.the.Jackal.S01E01.KONTRAST.SDH.eng", "The.Day.of.the.Jackal.S01E01.KONTRAST", "en", false, true},
		{"Pacific.Rim.English-WWW.MY-SUBS.NET", "Pacific.Rim.2013.1080p", "en", false, false},
		{"Movie.2020.forced.en", "Movie.2020", "en", true, false},
	}
	for _, c := range cases {
		lang, forced, sdh := parseSubMeta(c.base, c.stem)
		if lang != c.lang || forced != c.forced || sdh != c.sdh {
			t.Errorf("parseSubMeta(%q,%q) = (%q,%v,%v), want (%q,%v,%v)",
				c.base, c.stem, lang, forced, sdh, c.lang, c.forced, c.sdh)
		}
	}
}

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// found indexes discovered subs by base filename for convenient assertions.
func found(subs []ExternalSub) map[string]ExternalSub {
	m := map[string]ExternalSub{}
	for _, s := range subs {
		m[filepath.Base(s.Path)] = s
	}
	return m
}

func TestDiscoverSoloFolderSidecar(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Annie", "Annie.2014.1080p.BluRay.x264.YIFY.mp4")
	write(t, video)
	write(t, filepath.Join(dir, "Annie", "Annie.2014.720p.BluRay.x264.[YTS.MX]-English.srt"))

	got := found(DiscoverSidecars(video))
	sub, ok := got["Annie.2014.720p.BluRay.x264.[YTS.MX]-English.srt"]
	if !ok {
		t.Fatalf("sidecar not discovered: %v", got)
	}
	if sub.Language != "en" {
		t.Errorf("language = %q, want en", sub.Language)
	}
}

func TestDiscoverSubsFolderBareNames(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "Sicario", "Sicario 2015 1080p BluRay x264 AC3-JYK.mp4")
	write(t, video)
	write(t, filepath.Join(dir, "Sicario", "Subs", "English.srt"))
	write(t, filepath.Join(dir, "Sicario", "Subs", "English(SDH).srt"))

	got := found(DiscoverSidecars(video))
	if len(got) != 2 {
		t.Fatalf("got %d subs, want 2: %v", len(got), got)
	}
	if got["English.srt"].Language != "en" || got["English.srt"].SDH {
		t.Errorf("English.srt wrong: %+v", got["English.srt"])
	}
	if !got["English(SDH).srt"].SDH {
		t.Errorf("English(SDH).srt should be SDH")
	}
}

func TestDiscoverVariedSubFolderNames(t *testing.T) {
	// The subtitle folder shows up under many names; all should be discovered.
	for _, folder := range []string{"Subs", "subs", "Subtitles", "Subtitle", "srt", "SRT", "Captions", "CC"} {
		dir := t.TempDir()
		video := filepath.Join(dir, "The Movie", "The.Movie.2021.1080p.mkv")
		write(t, video)
		write(t, filepath.Join(dir, "The Movie", folder, "English.srt"))

		got := found(DiscoverSidecars(video))
		if _, ok := got["English.srt"]; !ok {
			t.Errorf("folder %q: sidecar not discovered: %v", folder, got)
		}
	}
}

func TestDiscoverPerEpisodeSubfolder(t *testing.T) {
	dir := t.TempDir()
	release := "Normal.People.S01E01.1080p.WEBRip.x265-RARBG[eztv.re]"
	video := filepath.Join(dir, "Normal People", release+".mp4")
	// A second video so the folder is NOT treated as solo; association must come
	// from the per-episode subfolder name matching the video stem.
	write(t, filepath.Join(dir, "Normal People", "Normal.People.S01E02.1080p.WEBRip.x265-RARBG[eztv.re].mp4"))
	write(t, video)
	write(t, filepath.Join(dir, "Normal People", "Subs", release, "2_English.srt"))
	write(t, filepath.Join(dir, "Normal People", "Subs", release, "7_nor.srt"))
	// A different episode's subfolder must NOT attach to E01.
	write(t, filepath.Join(dir, "Normal People", "Subs", "Normal.People.S01E02.1080p.WEBRip.x265-RARBG[eztv.re]", "2_English.srt"))

	got := found(DiscoverSidecars(video))
	if len(got) != 2 {
		t.Fatalf("got %d subs, want 2: %v", len(got), got)
	}
	if got["2_English.srt"].Language != "en" {
		t.Errorf("2_English wrong lang: %q", got["2_English.srt"].Language)
	}
	if got["7_nor.srt"].Language != "no" {
		t.Errorf("7_nor wrong lang: %q", got["7_nor.srt"].Language)
	}
}

func TestDiscoverMultiVideoFolderNoCrossAttach(t *testing.T) {
	// Two videos in one folder, one loosely named sub matching neither stem:
	// it must be skipped rather than attached to an arbitrary episode.
	dir := t.TempDir()
	v1 := filepath.Join(dir, "Show", "Show.S01E01.mkv")
	write(t, v1)
	write(t, filepath.Join(dir, "Show", "Show.S01E02.mkv"))
	write(t, filepath.Join(dir, "Show", "random.srt"))

	if subs := DiscoverSidecars(v1); len(subs) != 0 {
		t.Fatalf("expected no cross-attached subs, got %v", subs)
	}
}
