package db

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func TestCompaniesRoundTrip(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	m := sampleMovie(1, "Iron Man")
	m.Companies = []string{"Marvel Studios", "Paramount"}
	id, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.getCompanies(ctx, "movie_companies", "movie_id", id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "Marvel Studios" { // ordered by name
		t.Fatalf("companies = %v, want [Marvel Studios Paramount]", got)
	}

	// GetMovie hydrates companies; features loader exposes them too.
	full, _ := d.GetMovie(ctx, id)
	if len(full.Companies) != 2 {
		t.Fatalf("GetMovie companies = %v", full.Companies)
	}
	feats, _ := d.LoadMovieFeatures(ctx)
	if len(feats) != 1 || len(feats[0].Companies) != 2 {
		t.Fatalf("feature companies = %+v", feats)
	}
}

func TestShowCreatorsAndCompanies(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	s := &model.Show{
		TMDBID: 10, Title: "Stranger Things", Year: 2016,
		Companies: []string{"Netflix"},
		Creators:  []string{"Matt Duffer", "Ross Duffer"},
	}
	id, err := d.UpsertShow(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	creators, err := d.getShowCreators(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(creators) != 2 || creators[0] != "Matt Duffer" {
		t.Fatalf("creators = %v", creators)
	}
	feats, _ := d.LoadShowFeatures(ctx)
	if len(feats) != 1 || len(feats[0].Creators) != 2 || len(feats[0].Companies) != 1 {
		t.Fatalf("show feature = %+v", feats)
	}
}

func TestMissingCompaniesBackfillList(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	with := sampleMovie(1, "Has Studio")
	with.Companies = []string{"A24"}
	if _, err := d.UpsertMovie(ctx, with); err != nil {
		t.Fatal(err)
	}
	without := sampleMovie(2, "No Studio")
	withoutID, err := d.UpsertMovie(ctx, without)
	if err != nil {
		t.Fatal(err)
	}

	missing, err := d.MoviesMissingCompanies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].ID != withoutID {
		t.Fatalf("missing = %+v, want just the studio-less movie", missing)
	}
	if err := d.SetMovieCompanies(ctx, withoutID, []string{"Blumhouse"}); err != nil {
		t.Fatal(err)
	}
	if missing, _ = d.MoviesMissingCompanies(ctx); len(missing) != 0 {
		t.Fatalf("after backfill, missing = %+v, want empty", missing)
	}
}
