package recommend

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// seed helpers ---------------------------------------------------------------

func newTestEngine(t *testing.T) (*Engine, *db.DB, int64) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "rec.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	pid, err := database.CreateProfile(context.Background(), "Viewer", "")
	if err != nil {
		t.Fatal(err)
	}
	return New(database), database, pid
}

var nextTMDB int64 = 1000

// seedMovie inserts a playable movie with the given attributes.
func seedMovie(t *testing.T, d *db.DB, title string, year int, genres []string, director model.Credit, collectionID int64) int64 {
	t.Helper()
	ctx := context.Background()
	nextTMDB++
	fileID, err := d.UpsertMediaFile(ctx, &model.MediaFile{Path: "/m/" + title + ".mkv", SizeBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	m := &model.Movie{
		TMDBID: nextTMDB, Title: title, Year: year, Runtime: 120,
		OriginalLang: "en", Genres: genres, CollectionID: collectionID,
		Crew: []model.Credit{director},
		File: &model.MediaFile{ID: fileID},
	}
	if collectionID != 0 {
		_ = d.UpsertCollection(ctx, collectionID, "Test Collection", "", "")
	}
	id, err := d.UpsertMovie(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var nolan = model.Credit{PersonID: 525, Name: "Christopher Nolan", Role: "Director"}
var villeneuve = model.Credit{PersonID: 137427, Name: "Denis Villeneuve", Role: "Director"}

// tests ----------------------------------------------------------------------

func TestRecordWatchBuildsPositiveAffinity(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()

	inception := seedMovie(t, d, "Inception", 2010, []string{"Science Fiction", "Action"}, nolan, 0)

	// Finish it.
	if err := e.RecordWatch(ctx, uid, model.KindMovie, inception, 118, 120); err != nil {
		t.Fatal(err)
	}
	p, err := e.LoadProfile(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if p.Affinity(DimGenre, "Science Fiction") <= 0 {
		t.Errorf("expected positive sci-fi affinity, got %f", p.Affinity(DimGenre, "Science Fiction"))
	}
	if p.Affinity(DimDirector, personKey(nolan.PersonID)) <= 0 {
		t.Error("expected positive Nolan affinity")
	}
	if p.Affinity(DimDecade, "2010") <= 0 {
		t.Error("expected positive 2010s affinity")
	}
}

func TestAbandonedWatchIsNegative(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()
	m := seedMovie(t, d, "Slow Drama", 1995, []string{"Drama"}, villeneuve, 0)

	// Watch only 15%.
	if err := e.RecordWatch(ctx, uid, model.KindMovie, m, 18, 120); err != nil {
		t.Fatal(err)
	}
	p, _ := e.LoadProfile(ctx, uid)
	if p.Affinity(DimGenre, "Drama") >= 0 {
		t.Errorf("expected negative Drama affinity for abandoned watch, got %f", p.Affinity(DimGenre, "Drama"))
	}
}

func TestHomeDirectorRowAndRelevance(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()

	watched := seedMovie(t, d, "Inception", 2010, []string{"Science Fiction"}, nolan, 0)
	seedMovie(t, d, "Interstellar", 2014, []string{"Science Fiction"}, nolan, 0)
	seedMovie(t, d, "Tenet", 2020, []string{"Science Fiction"}, nolan, 0)
	seedMovie(t, d, "Rom Com", 2003, []string{"Romance", "Comedy"}, villeneuve, 0)

	if err := e.RecordWatch(ctx, uid, model.KindMovie, watched, 120, 120); err != nil {
		t.Fatal(err)
	}

	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected home rows")
	}

	var hasDirectorRow, hasForYou bool
	for _, r := range rows {
		if r.Key == "director-"+personKey(nolan.PersonID) {
			hasDirectorRow = true
			// Watched film must be excluded from recommendations.
			for _, it := range r.Items {
				if it.ID == watched {
					t.Error("watched movie should not appear in recommendations")
				}
			}
		}
		if r.Key == "for-you" {
			hasForYou = true
		}
	}
	if !hasDirectorRow {
		t.Error("expected a 'More from Christopher Nolan' row")
	}
	if !hasForYou {
		t.Error("expected a 'Recommended for You' row")
	}

	// The top for-you item should be a Nolan sci-fi film, not the rom-com.
	for _, r := range rows {
		if r.Key == "for-you" && len(r.Items) > 0 {
			if r.Items[0].Title == "Rom Com" {
				t.Error("rom-com should not be the top recommendation for a sci-fi/Nolan fan")
			}
		}
	}
}

func TestColdStartRows(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()
	for i, title := range []string{"A", "B", "C", "D"} {
		seedMovie(t, d, title, 2000+i, []string{"Action"}, nolan, 0)
	}

	rows, err := e.Home(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("expected cold-start rows")
	}
	found := false
	for _, r := range rows {
		if strings.HasPrefix(r.Key, "cold-") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cold-start category rows, got %+v", rowKeys(rows))
	}
}

func TestCollectionCompletionRow(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()
	const coll = 999
	first := seedMovie(t, d, "Saga Part 1", 2001, []string{"Fantasy"}, nolan, coll)
	seedMovie(t, d, "Saga Part 2", 2002, []string{"Fantasy"}, nolan, coll)
	seedMovie(t, d, "Saga Part 3", 2003, []string{"Fantasy"}, nolan, coll)

	// Complete the first entry.
	if err := e.RecordWatch(ctx, uid, model.KindMovie, first, 120, 120); err != nil {
		t.Fatal(err)
	}
	rows, _ := e.Home(ctx, uid)
	found := false
	for _, r := range rows {
		if r.Key == "collection-999" {
			found = true
			if len(r.Items) != 2 {
				t.Errorf("expected 2 remaining collection items, got %d", len(r.Items))
			}
		}
	}
	if !found {
		t.Errorf("expected collection-completion row, got %+v", rowKeys(rows))
	}
}

func TestRewatchIncrementsTendency(t *testing.T) {
	e, d, uid := newTestEngine(t)
	ctx := context.Background()
	m := seedMovie(t, d, "Favorite", 2010, []string{"Action"}, nolan, 0)

	_ = e.RecordWatch(ctx, uid, model.KindMovie, m, 120, 120) // complete
	_ = e.RecordWatch(ctx, uid, model.KindMovie, m, 120, 120) // complete again => rewatch

	rt, _ := d.GetRewatchTendency(ctx, uid)
	if rt <= 0 {
		t.Errorf("expected rewatch tendency to rise after a rewatch, got %f", rt)
	}
}

func rowKeys(rows []Row) []string {
	var k []string
	for _, r := range rows {
		k = append(k, r.Key)
	}
	return k
}
