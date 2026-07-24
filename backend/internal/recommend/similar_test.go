package recommend

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// seedMovieKW inserts a playable movie with keywords, a director, and an
// optional collection, returning its local id.
func seedMovieKW(t *testing.T, e *Engine, title string, genres, keywords []string, director model.Credit, collectionID int64) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	fileID, err := e.db.UpsertMediaFile(ctx, &model.MediaFile{Path: "/m/" + title + ".mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if collectionID != 0 {
		_ = e.db.UpsertCollection(ctx, collectionID, "Nolan Set", "", "")
	}
	m := &model.Movie{
		TMDBID: nextTMDB, Title: title, Year: 2010, Runtime: 120, OriginalLang: "en",
		Genres: genres, Keywords: keywords, CollectionID: collectionID,
		Crew: []model.Credit{director},
		File: &model.MediaFile{ID: fileID},
	}
	id, err := e.db.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSimilarMoviesRanksThemeAndPriors(t *testing.T) {
	e, _, _ := newTestEngine(t)
	ctx := context.Background()

	target := seedMovieKW(t, e, "Inception", []string{"Science Fiction"},
		[]string{"heist", "dream", "subconscious"}, nolan, 77)
	// Same collection, few shared themes: should rank first via collection prior.
	sibling := seedMovieKW(t, e, "Inception 2", []string{"Science Fiction"},
		[]string{"sequel"}, nolan, 77)
	// Strong thematic match, different collection/director.
	thematic := seedMovieKW(t, e, "Heist Dream", []string{"Thriller"},
		[]string{"heist", "dream", "caper"}, villeneuve, 0)
	// Unrelated.
	_ = seedMovieKW(t, e, "Wedding Season", []string{"Romance"},
		[]string{"romance", "wedding"}, villeneuve, 0)

	got, err := e.SimilarMovies(ctx, target, 12)
	if err != nil {
		t.Fatalf("SimilarMovies: %v", err)
	}
	if len(got) < 2 {
		t.Fatalf("got %d results, want >= 2", len(got))
	}
	// Same-collection sibling ranks first with a collection reason.
	if got[0].ID != sibling {
		t.Fatalf("first result id = %d, want sibling %d (collection prior)", got[0].ID, sibling)
	}
	if got[0].Reason != "From the same collection" {
		t.Fatalf("sibling reason = %q, want collection reason", got[0].Reason)
	}
	// The thematic match appears and cites a shared theme.
	var found bool
	for _, r := range got {
		if r.ID == thematic {
			found = true
			if r.Reason == "" {
				t.Fatalf("thematic match should carry a theme reason, got empty")
			}
		}
	}
	if !found {
		t.Fatal("thematically similar movie missing from results")
	}
}

func TestSimilarMoviesFallsBackWithoutKeywords(t *testing.T) {
	e, _, _ := newTestEngine(t)
	ctx := context.Background()

	// No keywords anywhere -> empty vector space -> genre-overlap fallback.
	target := seedMovieKW(t, e, "A", []string{"Action", "Thriller"}, nil, nolan, 0)
	_ = seedMovieKW(t, e, "B", []string{"Action"}, nil, villeneuve, 0)
	_ = seedMovieKW(t, e, "C", []string{"Romance"}, nil, villeneuve, 0)

	got, err := e.SimilarMovies(ctx, target, 12)
	if err != nil {
		t.Fatalf("SimilarMovies fallback: %v", err)
	}
	// B shares a genre with A; the fallback should surface it.
	var sawB bool
	for _, r := range got {
		if r.Title == "B" {
			sawB = true
		}
	}
	if !sawB {
		t.Fatalf("genre fallback should surface the same-genre movie, got %+v", got)
	}
}
