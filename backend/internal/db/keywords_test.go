package db

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func TestKeywordsRoundTrip(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	m := sampleMovie(1, "Inception")
	m.Keywords = []string{"dream", "heist", "subconscious"}
	id, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := d.getKeywords(ctx, "movie_keywords", "movie_id", id)
	if err != nil {
		t.Fatalf("getKeywords: %v", err)
	}
	// getKeywords orders by name.
	want := []string{"dream", "heist", "subconscious"}
	if len(got) != len(want) {
		t.Fatalf("keywords = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keywords[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// GetMovie hydrates keywords onto the detail model.
	full, err := d.GetMovie(ctx, id)
	if err != nil {
		t.Fatalf("GetMovie: %v", err)
	}
	if len(full.Keywords) != 3 {
		t.Fatalf("GetMovie keywords = %v, want 3", full.Keywords)
	}
}

func TestKeywordsReplaceOnReupsert(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	m := sampleMovie(1, "Inception")
	m.Keywords = []string{"dream", "heist"}
	id, _ := d.UpsertMovie(ctx, m)

	// Re-upsert with a different set; links are replaced, not accumulated.
	m.Keywords = []string{"heist", "time-travel"}
	if _, err := d.UpsertMovie(ctx, m); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _ := d.getKeywords(ctx, "movie_keywords", "movie_id", id)
	if len(got) != 2 || got[0] != "heist" || got[1] != "time-travel" {
		t.Fatalf("after re-upsert keywords = %v, want [heist time-travel]", got)
	}
}

func TestMissingKeywordsAndBackfill(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// One movie with keywords, one without.
	with := sampleMovie(1, "Has Keywords")
	with.Keywords = []string{"dream"}
	if _, err := d.UpsertMovie(ctx, with); err != nil {
		t.Fatal(err)
	}
	without := sampleMovie(2, "No Keywords")
	withoutID, err := d.UpsertMovie(ctx, without)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertShow(ctx, &model.Show{TMDBID: 10, Title: "Bare Show", Year: 2020}); err != nil {
		t.Fatal(err)
	}

	missing, err := d.MoviesMissingKeywords(ctx)
	if err != nil {
		t.Fatalf("MoviesMissingKeywords: %v", err)
	}
	if len(missing) != 1 || missing[0].ID != withoutID || missing[0].TMDBID != 2 {
		t.Fatalf("missing movies = %+v, want just the keyword-less one (tmdb 2)", missing)
	}
	missingShows, err := d.ShowsMissingKeywords(ctx)
	if err != nil {
		t.Fatalf("ShowsMissingKeywords: %v", err)
	}
	if len(missingShows) != 1 || missingShows[0].TMDBID != 10 {
		t.Fatalf("missing shows = %+v, want tmdb 10", missingShows)
	}

	// Backfill the keyword-less movie; it should drop out of the missing list.
	if err := d.SetMovieKeywords(ctx, withoutID, []string{"backfilled"}); err != nil {
		t.Fatalf("SetMovieKeywords: %v", err)
	}
	missing, _ = d.MoviesMissingKeywords(ctx)
	if len(missing) != 0 {
		t.Fatalf("after backfill, missing = %+v, want empty", missing)
	}
}
