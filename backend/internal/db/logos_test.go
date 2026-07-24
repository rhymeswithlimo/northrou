package db

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// TestLogoPathRoundTrip guards the "a read query must select what its model
// claims to return" trap: the scanner writes LogoPath, so GetMovie/GetShow must
// read it back or the detail logo is silently empty everywhere.
func TestLogoPathRoundTrip(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()

	m := sampleMovie(1, "Blade Runner 2049")
	m.LogoPath = "w500/movie-logo.png"
	mid, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	gotMovie, err := d.GetMovie(ctx, mid)
	if err != nil {
		t.Fatal(err)
	}
	if gotMovie.LogoPath != "w500/movie-logo.png" {
		t.Fatalf("movie LogoPath = %q, want w500/movie-logo.png", gotMovie.LogoPath)
	}

	s := &model.Show{TMDBID: 2, Title: "Severance", Year: 2022, LogoPath: "w500/show-logo.png"}
	sid, err := d.UpsertShow(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	gotShow, err := d.GetShow(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if gotShow.LogoPath != "w500/show-logo.png" {
		t.Fatalf("show LogoPath = %q, want w500/show-logo.png", gotShow.LogoPath)
	}

	// An updated logo overwrites, not appends, through the ON CONFLICT path.
	m.LogoPath = "w500/movie-logo-v2.png"
	if _, err := d.UpsertMovie(ctx, m); err != nil {
		t.Fatal(err)
	}
	if again, _ := d.GetMovie(ctx, mid); again.LogoPath != "w500/movie-logo-v2.png" {
		t.Fatalf("movie LogoPath after re-upsert = %q, want w500/movie-logo-v2.png", again.LogoPath)
	}
}
