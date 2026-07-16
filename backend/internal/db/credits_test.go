package db

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// The scanner has always written cast, crew and rating; credits.go had only the
// writer half and GetMovie/GetShow never selected vote_average, so all three
// came back empty from the API no matter what was in the database. These tests
// pin the round trip.

func TestGetMovieReturnsCreditsAndRating(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	m := sampleMovie(1, "Inception")
	m.Rating = 8.4
	m.Votes = 1200
	m.Tagline = "Your mind is the scene of the crime."
	m.Certification = "PG-13"
	m.Cast = []model.Credit{
		{PersonID: 10, Name: "Leonardo DiCaprio", Role: "Cobb", Order: 0, ProfilePath: "p/leo.jpg"},
		{PersonID: 11, Name: "Elliot Page", Role: "Ariadne", Order: 1},
	}
	m.Crew = []model.Credit{
		{PersonID: 12, Name: "Christopher Nolan", Role: "Director"},
	}

	id, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}

	got, err := d.GetMovie(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	if got.Rating != 8.4 {
		t.Errorf("Rating = %v, want 8.4 (stored since migration 2, never selected)", got.Rating)
	}
	if got.Tagline != m.Tagline {
		t.Errorf("Tagline = %q, want %q", got.Tagline, m.Tagline)
	}
	if got.Certification != "PG-13" {
		t.Errorf("Certification = %q, want PG-13", got.Certification)
	}

	if len(got.Cast) != 2 {
		t.Fatalf("Cast has %d entries, want 2", len(got.Cast))
	}
	// Billing order, not insertion or id order.
	if got.Cast[0].Name != "Leonardo DiCaprio" {
		t.Errorf("Cast[0] = %q, want Leonardo DiCaprio (ord 0 first)", got.Cast[0].Name)
	}
	if got.Cast[0].Role != "Cobb" {
		t.Errorf("Cast[0].Role = %q, want Cobb", got.Cast[0].Role)
	}
	if got.Cast[0].ProfilePath != "p/leo.jpg" {
		t.Errorf("Cast[0].ProfilePath = %q, want p/leo.jpg", got.Cast[0].ProfilePath)
	}

	if len(got.Crew) != 1 || got.Crew[0].Role != "Director" {
		t.Fatalf("Crew = %+v, want one Director", got.Crew)
	}
	if got.Crew[0].Name != "Christopher Nolan" {
		t.Errorf("Crew[0] = %q, want Christopher Nolan", got.Crew[0].Name)
	}
}

func TestGetShowReturnsCreditsAndEpisodeExtras(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	showID, err := d.UpsertShow(ctx, &model.Show{
		TMDBID: 1, Title: "Silo", Year: 2023,
		Rating: 8.1, Tagline: "The truth will surface.", Certification: "TV-MA",
		Cast: []model.Credit{{PersonID: 20, Name: "Rebecca Ferguson", Role: "Juliette", Order: 0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertEpisode(ctx, &model.Episode{
		ShowID: showID, SeasonID: seasonID, Season: 1, Number: 1,
		Title: "Freedom Day", StillPath: "s/e1.jpg", AirDate: "2023-05-05",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := d.GetShow(ctx, showID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Rating != 8.1 {
		t.Errorf("Rating = %v, want 8.1", got.Rating)
	}
	if got.Certification != "TV-MA" {
		t.Errorf("Certification = %q, want TV-MA", got.Certification)
	}
	if len(got.Cast) != 1 || got.Cast[0].Name != "Rebecca Ferguson" {
		t.Fatalf("Cast = %+v, want Rebecca Ferguson", got.Cast)
	}
	if len(got.Seasons) != 1 || len(got.Seasons[0].Episodes) != 1 {
		t.Fatal("expected one season with one episode")
	}
	ep := got.Seasons[0].Episodes[0]
	if ep.StillPath != "s/e1.jpg" {
		t.Errorf("StillPath = %q, want s/e1.jpg", ep.StillPath)
	}
	if ep.AirDate != "2023-05-05" {
		t.Errorf("AirDate = %q, want 2023-05-05", ep.AirDate)
	}
}

func TestUpsertPersonKeepsExistingHeadshot(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	// The same person credited on two titles, where only the first payload
	// carried a headshot. The second write must not blank it.
	first := sampleMovie(1, "First")
	first.Cast = []model.Credit{{PersonID: 30, Name: "Someone", Role: "A", ProfilePath: "p/face.jpg"}}
	first.Crew = nil
	if _, err := d.UpsertMovie(ctx, first); err != nil {
		t.Fatal(err)
	}

	second := sampleMovie(2, "Second")
	second.Cast = []model.Credit{{PersonID: 30, Name: "Someone", Role: "B", ProfilePath: ""}}
	second.Crew = nil
	id, err := d.UpsertMovie(ctx, second)
	if err != nil {
		t.Fatal(err)
	}

	got, err := d.GetMovie(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cast) != 1 {
		t.Fatalf("Cast = %+v, want one entry", got.Cast)
	}
	if got.Cast[0].ProfilePath != "p/face.jpg" {
		t.Errorf("ProfilePath = %q, want the previously stored p/face.jpg", got.Cast[0].ProfilePath)
	}
}
