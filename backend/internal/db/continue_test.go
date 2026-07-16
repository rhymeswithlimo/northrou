package db

import (
	"context"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// seedProfile creates a viewer profile and returns its id. Watch history is
// per-profile, so every test here needs one.
func seedProfile(t *testing.T, d *DB) int64 {
	t.Helper()
	id, err := d.CreateProfile(context.Background(), "Viewer", "")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seedEpisode creates a show with one episode and returns the episode id.
func seedEpisode(t *testing.T, d *DB, tmdbID int64, showTitle string, season, number int) (showID, episodeID int64) {
	t.Helper()
	ctx := context.Background()
	showID, err := d.UpsertShow(ctx, &model.Show{TMDBID: tmdbID, Title: showTitle, Year: 2023})
	if err != nil {
		t.Fatal(err)
	}
	seasonID, err := d.UpsertSeason(ctx, showID, season)
	if err != nil {
		t.Fatal(err)
	}
	episodeID, err = d.UpsertEpisode(ctx, &model.Episode{
		ShowID: showID, SeasonID: seasonID, Season: season, Number: number, Title: "An Episode",
	})
	if err != nil {
		t.Fatal(err)
	}
	return showID, episodeID
}

func TestListInProgress(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	uid := seedProfile(t, d)

	movieID, err := d.UpsertMovie(ctx, sampleMovie(1, "Partway Movie"))
	if err != nil {
		t.Fatal(err)
	}
	doneID, err := d.UpsertMovie(ctx, sampleMovie(2, "Finished Movie"))
	if err != nil {
		t.Fatal(err)
	}
	barelyID, err := d.UpsertMovie(ctx, sampleMovie(3, "Barely Started"))
	if err != nil {
		t.Fatal(err)
	}
	showID, epID := seedEpisode(t, d, 10, "Silo", 3, 2)

	// partway through
	if _, err := d.UpsertWatchEvent(ctx, uid, "movie", movieID, 600, 3600, false); err != nil {
		t.Fatal(err)
	}
	// finished: must not appear
	if _, err := d.UpsertWatchEvent(ctx, uid, "movie", doneID, 3600, 3600, true); err != nil {
		t.Fatal(err)
	}
	// opened and closed: must not appear
	if _, err := d.UpsertWatchEvent(ctx, uid, "movie", barelyID, 5, 3600, false); err != nil {
		t.Fatal(err)
	}
	// an episode partway: the regression this whole path exists for
	if _, err := d.UpsertWatchEvent(ctx, uid, "episode", epID, 1200, 3600, false); err != nil {
		t.Fatal(err)
	}

	got, err := d.ListInProgress(ctx, uid, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	byTitle := map[string]InProgressItem{}
	for _, it := range got {
		byTitle[it.Title] = it
	}

	if len(got) != 2 {
		t.Fatalf("got %d items %v, want 2 (partway movie + partway episode)", len(got), byTitle)
	}
	if _, ok := byTitle["Finished Movie"]; ok {
		t.Error("a completed title must not be resumable")
	}
	if _, ok := byTitle["Barely Started"]; ok {
		t.Error("a title watched for 5 seconds must not pin itself to Continue Watching")
	}

	mv, ok := byTitle["Partway Movie"]
	if !ok {
		t.Fatal("expected the partway movie")
	}
	if mv.Kind != model.KindMovie || mv.PositionSec != 600 || mv.DurationSec != 3600 {
		t.Errorf("movie item = %+v, want kind=movie pos=600 dur=3600", mv)
	}

	// An episode's card shows the SHOW's title but resumes the EPISODE.
	ep, ok := byTitle["Silo"]
	if !ok {
		t.Fatal("expected the partway episode, keyed by its show title")
	}
	if ep.Kind != model.KindEpisode {
		t.Errorf("episode kind = %q, want episode", ep.Kind)
	}
	if ep.ID != epID {
		t.Errorf("episode id = %d, want the episode id %d", ep.ID, epID)
	}
	if ep.ShowID != showID {
		t.Errorf("show id = %d, want %d", ep.ShowID, showID)
	}
	if ep.Season != 3 || ep.Number != 2 {
		t.Errorf("episode = S%02dE%02d, want S03E02", ep.Season, ep.Number)
	}
}

func TestListInProgressIsPerProfile(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	a := seedProfile(t, d)
	b := seedProfile(t, d)

	id, err := d.UpsertMovie(ctx, sampleMovie(1, "Mine"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.UpsertWatchEvent(ctx, a, "movie", id, 600, 3600, false); err != nil {
		t.Fatal(err)
	}

	got, err := d.ListInProgress(ctx, b, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("profile B sees %d items, want 0: watch history must not leak between profiles", len(got))
	}
}

func TestListInProgressLimit(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	uid := seedProfile(t, d)

	for i := int64(1); i <= 5; i++ {
		id, err := d.UpsertMovie(ctx, sampleMovie(i, "Movie"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.UpsertWatchEvent(ctx, uid, "movie", id, 600, 3600, false); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.ListInProgress(ctx, uid, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("limit 2: got %d, want 2", len(got))
	}
}
