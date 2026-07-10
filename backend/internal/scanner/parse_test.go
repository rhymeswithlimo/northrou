package scanner

import "testing"

func TestParseMovies(t *testing.T) {
	cases := []struct {
		name  string
		title string
		year  int
	}{
		{"The.Social.Network.2010.2160p.BluRay.HEVC.DTS-HD.MA-FGT.mkv", "The Social Network", 2010},
		{"Blade Runner 2049 (2017) 1080p BluRay x264-GROUP.mkv", "Blade Runner 2049", 2017},
		{"Inception.2010.1080p.BluRay.x264.DTS-HD.MA.5.1-RARBG.mkv", "Inception", 2010},
		{"Dune.Part.Two.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR.HEVC-FLUX.mkv", "Dune Part Two", 2024},
		{"1917.2019.1080p.BluRay.DTS.x264-HDMaNiAcS.mkv", "1917", 2019},
		{"The Matrix (1999).mkv", "The Matrix", 1999},
		{"Parasite.2019.mkv", "Parasite", 2019},
		{"Interstellar.2014.IMAX.2160p.UHD.BluRay.x265.HDR.Atmos-TERMINAL.mkv", "Interstellar", 2014},
		{"Some_Movie_Title_2021_720p_WEBRip.mp4", "Some Movie Title", 2021},
	}
	for _, c := range cases {
		got := ParseFilename(c.name)
		if got.IsEpisode {
			t.Errorf("%s: classified as episode", c.name)
			continue
		}
		if got.Title != c.title || got.Year != c.year {
			t.Errorf("%s => title=%q year=%d; want title=%q year=%d",
				c.name, got.Title, got.Year, c.title, c.year)
		}
	}
}

func TestParseEpisodes(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		season  int
		episode int
	}{
		{"Breaking.Bad.S01E05.1080p.BluRay.x264-GROUP.mkv", "Breaking Bad", 1, 5},
		{"The.Wire.S03E12.HDTV.XviD.mkv", "The Wire", 3, 12},
		{"Game of Thrones - S08E06 - The Iron Throne.mkv", "Game of Thrones", 8, 6},
		{"Firefly.1x05.HDTV.mkv", "Firefly", 1, 5},
		{"Severance.S02E01.2160p.ATVP.WEB-DL.DDP5.1.Atmos.HEVC-NTb.mkv", "Severance", 2, 1},
		{"Show.Name.2005.S01E02.720p.mkv", "Show Name", 1, 2},
	}
	for _, c := range cases {
		got := ParseFilename(c.name)
		if !got.IsEpisode {
			t.Errorf("%s: not classified as episode", c.name)
			continue
		}
		if got.Title != c.title || got.Season != c.season || got.Episode != c.episode {
			t.Errorf("%s => title=%q S%dE%d; want title=%q S%dE%d",
				c.name, got.Title, got.Season, got.Episode, c.title, c.season, c.episode)
		}
	}
}

func TestParseMultiEpisode(t *testing.T) {
	got := ParseFilename("Firefly.S01E01E02.1080p.BluRay.mkv")
	if !got.IsEpisode || got.Season != 1 {
		t.Fatalf("unexpected: %+v", got)
	}
	if len(got.Episodes) != 2 || got.Episodes[0] != 1 || got.Episodes[1] != 2 {
		t.Errorf("expected episodes [1 2], got %v", got.Episodes)
	}
}

func TestParseShowYearKept(t *testing.T) {
	got := ParseFilename("Show.Name.2005.S01E02.720p.mkv")
	if got.Year != 2005 {
		t.Errorf("expected show year 2005, got %d", got.Year)
	}
}
