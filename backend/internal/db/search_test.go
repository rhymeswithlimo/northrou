package db

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func TestSearch(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	for _, m := range []struct {
		tmdb  int64
		title string
	}{
		{1, "The Thing"},
		{2, "Breathe"},
		{3, "Inception"},
		{4, "100% Wolf"},
	} {
		mv := sampleMovie(m.tmdb, m.title)
		if _, err := d.UpsertMovie(ctx, mv); err != nil {
			t.Fatalf("upsert %s: %v", m.title, err)
		}
	}
	if _, err := d.UpsertShow(ctx, &model.Show{TMDBID: 10, Title: "The Bear", Year: 2022}); err != nil {
		t.Fatalf("upsert show: %v", err)
	}

	tests := []struct {
		name  string
		query string
		want  []string // titles, in order
	}{
		{"empty query returns nothing", "", nil},
		{"whitespace only returns nothing", "   ", nil},
		{"matches across movies and shows", "the", []string{"The Bear", "The Thing", "Breathe"}},
		{"prefix matches sort before substring", "the", []string{"The Bear", "The Thing", "Breathe"}},
		{"case insensitive", "INCEPTION", []string{"Inception"}},
		{"substring match", "cept", []string{"Inception"}},
		{"no match", "zzzz", nil},
		// A bare % would match everything if wildcards were not escaped.
		{"percent is literal, not a wildcard", "%", []string{"100% Wolf"}},
		{"underscore is literal, not a wildcard", "_", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := d.Search(ctx, tc.query, 0)
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(got) != len(tc.want) {
				var titles []string
				for _, g := range got {
					titles = append(titles, g.Title)
				}
				t.Fatalf("got %v, want %v", titles, tc.want)
			}
			for i, w := range tc.want {
				if got[i].Title != w {
					t.Errorf("result %d = %q, want %q", i, got[i].Title, w)
				}
			}
		})
	}
}

func TestSearchLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	for i := int64(1); i <= 5; i++ {
		if _, err := d.UpsertMovie(ctx, sampleMovie(i, "Movie "+string(rune('A'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.Search(ctx, "Movie", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("limit 2: got %d results, want 2", len(got))
	}
}

func TestSearchKind(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	if _, err := d.UpsertMovie(ctx, sampleMovie(1, "Dune")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertShow(ctx, &model.Show{TMDBID: 2, Title: "Dune Prophecy", Year: 2024}); err != nil {
		t.Fatal(err)
	}

	got, err := d.Search(ctx, "Dune", 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]model.MediaKind{}
	for _, r := range got {
		kinds[r.Title] = r.Kind
	}
	if kinds["Dune"] != model.KindMovie {
		t.Errorf("Dune kind = %q, want movie", kinds["Dune"])
	}
	if kinds["Dune Prophecy"] != model.KindShow {
		t.Errorf("Dune Prophecy kind = %q, want show", kinds["Dune Prophecy"])
	}
}

func TestSimilarMovies(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// sampleMovie gives everything Sci-Fi + Thriller, so genre overlap alone
	// links them all; the collection is what must outrank it.
	if err := d.UpsertCollection(ctx, 99, "The Target Collection", "", ""); err != nil {
		t.Fatal(err)
	}

	target := sampleMovie(1, "Target")
	target.CollectionID = 99
	targetID, err := d.UpsertMovie(ctx, target)
	if err != nil {
		t.Fatal(err)
	}

	sequel := sampleMovie(2, "Sequel")
	sequel.CollectionID = 99
	if _, err := d.UpsertMovie(ctx, sequel); err != nil {
		t.Fatal(err)
	}

	// Higher rating, but no collection: must still lose to the sequel.
	other := sampleMovie(3, "Unrelated Sibling")
	other.Rating = 9.9
	if _, err := d.UpsertMovie(ctx, other); err != nil {
		t.Fatal(err)
	}

	// Shares no genre at all: must not appear.
	odd := sampleMovie(4, "Documentary")
	odd.Genres = []string{"Documentary"}
	if _, err := d.UpsertMovie(ctx, odd); err != nil {
		t.Fatal(err)
	}

	got, err := d.SimilarMovies(ctx, targetID, 0)
	if err != nil {
		t.Fatalf("similar: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected results")
	}
	if got[0].Title != "Sequel" {
		t.Errorf("first result = %q, want Sequel (same collection outranks rating)", got[0].Title)
	}
	for _, r := range got {
		if r.ID == targetID {
			t.Error("similar must not include the target itself")
		}
		if r.Title == "Documentary" {
			t.Error("similar must not include a title sharing no genre")
		}
	}
}

func TestSimilarShows(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	id, err := d.UpsertShow(ctx, &model.Show{TMDBID: 1, Title: "Silo", Genres: []string{"Sci-Fi", "Drama"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertShow(ctx, &model.Show{TMDBID: 2, Title: "Severance", Genres: []string{"Sci-Fi", "Drama"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertShow(ctx, &model.Show{TMDBID: 3, Title: "Bake Off", Genres: []string{"Reality"}}); err != nil {
		t.Fatal(err)
	}

	got, err := d.SimilarShows(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Severance" {
		var titles []string
		for _, g := range got {
			titles = append(titles, g.Title)
		}
		t.Fatalf("got %v, want [Severance]", titles)
	}
}
