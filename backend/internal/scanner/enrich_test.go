package scanner

import (
	"path/filepath"
	"testing"
)

func TestSeasonFromFolder(t *testing.T) {
	cases := map[string]struct {
		n  int
		ok bool
	}{
		"Season 1":         {1, true},
		"Season 01":        {1, true},
		"Season.1":         {1, true},
		"S01":              {1, true},
		"S1":               {1, true},
		"Series 2":         {2, true},
		"Season 1 (2019)":  {1, true},
		"Specials":         {0, true},
		"MKV":              {0, false},
		"Andor":            {0, false},
	}
	for name, want := range cases {
		n, ok := seasonFromFolder(name)
		if n != want.n || ok != want.ok {
			t.Errorf("seasonFromFolder(%q) = (%d,%v), want (%d,%v)", name, n, ok, want.n, want.ok)
		}
	}
}

func TestYearFromFolder(t *testing.T) {
	cases := map[string]int{
		"2001 - Sorcerers Stone - Harry Potter": 2001,
		"The Matrix (1999)":                     1999,
		"Andor":                                 0,
		"Room 237":                              0, // not a plausible year token here
	}
	for name, want := range cases {
		if got := yearFromFolder(name); got != want {
			t.Errorf("yearFromFolder(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestEnrichEpisodeFromPath(t *testing.T) {
	sep := string(filepath.Separator)
	j := func(parts ...string) string { return sep + filepath.Join(parts...) }

	cases := []struct {
		path    string
		title   string
		season  int
		episode int
	}{
		// S01 folder, marker-less filename with a loose episode number.
		{j("media", "Shows", "Andor", "S01", "Andor E05.mkv"), "Andor", 1, 5},
		// Intermediate "MKV" container folder between the season and the file.
		{j("media", "Shows", "Arrow", "S02", "MKV", "Arrow Episode 4.mkv"), "Arrow", 2, 4},
		// "Season 03" long form.
		{j("srv", "TV", "The Wire", "Season 03", "The Wire E11.mkv"), "The Wire", 3, 11},
		// No season folder at all: title from the show folder, season 0.
		{j("tv", "Chernobyl", "Chernobyl.mkv"), "Chernobyl", 0, 0},
	}
	for _, c := range cases {
		got := enrichEpisodeFromPath(c.path, ParsedInfo{})
		if !got.IsEpisode {
			t.Errorf("%s: not an episode", c.path)
			continue
		}
		if got.Title != c.title || got.Season != c.season || got.Episode != c.episode {
			t.Errorf("%s => title=%q S%dE%d; want title=%q S%dE%d",
				c.path, got.Title, got.Season, got.Episode, c.title, c.season, c.episode)
		}
	}
}

func TestYearFromAncestors(t *testing.T) {
	sep := string(filepath.Separator)
	p := sep + filepath.Join("Films", "Harry Potter",
		"2001 - Sorcerers Stone - Harry Potter", "Harry.Potter.mkv")
	if got := yearFromAncestors(p); got != 2001 {
		t.Errorf("yearFromAncestors = %d, want 2001", got)
	}
}

func TestParseAltEpisodeSingleDigit(t *testing.T) {
	got := ParseFilename("Firefly.1x5.HDTV.mkv")
	if !got.IsEpisode || got.Season != 1 || got.Episode != 5 {
		t.Errorf("1x5 parse wrong: %+v", got)
	}
}

func TestParseLowercaseEpisode(t *testing.T) {
	got := ParseFilename("arcane.s02e02.1080p.web.h264-successfulcrab.mp4")
	if !got.IsEpisode || got.Season != 2 || got.Episode != 2 {
		t.Errorf("lowercase parse wrong: %+v", got)
	}
}
