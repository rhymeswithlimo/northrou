package recommend

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// seedRatedMovie inserts a playable movie with rating/revenue/country set.
func seedRatedMovie(t *testing.T, d *db.DB, title string, year int, genres []string, rating float64, votes int, revenue int64, country string) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	fileID, err := d.UpsertMediaFile(ctx, &model.MediaFile{Path: "/m/" + title + ".mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := &model.Movie{
		TMDBID: nextTMDB, Title: title, Year: year, Runtime: 120, OriginalLang: "en",
		Genres: genres, Rating: rating, Votes: votes, Popularity: rating * 10,
		Revenue: revenue, Country: country,
		File: &model.MediaFile{ID: fileID},
	}
	id, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seedShow inserts a show with one playable episode.
func seedShow(t *testing.T, d *db.DB, title string, year int, genres []string, rating float64, country string) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	show := &model.Show{
		TMDBID: nextTMDB, Title: title, Year: year, OriginalLang: "en",
		Genres: genres, Rating: rating, Popularity: rating * 10, Country: country,
	}
	showID, err := d.UpsertShow(ctx, show)
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := d.UpsertMediaFile(ctx, &model.MediaFile{Path: "/tv/" + title + "-s01e01.mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, &model.Episode{
		ShowID: showID, SeasonID: seasonID, Season: 1, Number: 1,
		File: &model.MediaFile{ID: fileID},
	}); err != nil {
		t.Fatal(err)
	}
	return showID
}

func TestColdStartCategories(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()

	// Five acclaimed 2000s American action blockbusters.
	for _, title := range []string{"Big One", "Bigger", "Biggest", "Huge Hit", "Mega"} {
		seedRatedMovie(t, d, title, 2005, []string{"Action"}, 8.0, 2000, 500_000_000, "US")
	}
	// Three top-rated American drama series.
	for _, title := range []string{"Prestige Drama", "Acclaimed Series", "Great Show"} {
		seedShow(t, d, title, 2010, []string{"Drama"}, 8.6, "US")
	}

	// No watch history => cold start.
	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Row{}
	for _, r := range rows {
		got[r.Key] = r
	}

	want := []string{
		"cold-acclaimed-films",   // rating>=7.5, votes>=500
		"cold-blockbusters-2000", // >=4 films in the decade
		"cold-toprated-tv",       // shows rating>=8
		"cold-country-tv-US",     // American TV Shows
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("expected cold-start row %q; got keys %v", k, rowKeys(rows))
		}
	}

	// The country row must be labelled naturally and carry show items.
	if r, ok := got["cold-country-tv-US"]; ok {
		if r.Title != "American TV Shows" {
			t.Errorf("country row title = %q, want %q", r.Title, "American TV Shows")
		}
		if len(r.Items) == 0 || r.Items[0].Kind != "show" {
			t.Errorf("country row should contain show items, got %+v", r.Items)
		}
	}
	// The blockbuster row must carry movie items, biggest first.
	if r, ok := got["cold-blockbusters-2000"]; ok {
		if len(r.Items) == 0 || r.Items[0].Kind != "movie" {
			t.Errorf("blockbuster row should contain movie items, got %+v", r.Items)
		}
	}
}
